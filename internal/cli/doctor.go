package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/model"
	"github.com/dedomorozoff/dmsh/internal/redact"
	"github.com/spf13/cobra"
)

type doctorStatus string

const (
	doctorOK   doctorStatus = "ok"
	doctorWarn doctorStatus = "warn"
	doctorFail doctorStatus = "fail"
)

type doctorCheck struct {
	Name    string       `json:"name"`
	Status  doctorStatus `json:"status"`
	Message string       `json:"message"`
	Detail  string       `json:"detail,omitempty"`
	Hint    string       `json:"hint,omitempty"`
}

func newDoctorCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check dmsh configuration and environment",
		Long:  "Run diagnostics for config, model files, provider/proxy settings, shell, and local environment.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			checks := runDoctor(ctx)
			if jsonOut {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetEscapeHTML(false)
				return enc.Encode(map[string]any{
					"ok":     doctorOverallOK(checks),
					"checks": checks,
				})
			}
			printDoctor(cmd.OutOrStdout(), checks)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output diagnostics as JSON")
	return cmd
}

func runDoctor(ctx context.Context) []doctorCheck {
	cfg, cfgErr := config.Load()
	checks := make([]doctorCheck, 0, 16)
	checks = append(checks, doctorConfigCheck(cfg, cfgErr))
	checks = append(checks, doctorFilePermChecks(cfg)...)
	checks = append(checks, doctorProviderCheck(cfg)...)
	if c := doctorProviderBuildCheck(cfg); c.Name != "" {
		checks = append(checks, c)
	}
	checks = append(checks, doctorModelCheck(cfg))
	checks = append(checks, doctorBuildCheck())
	checks = append(checks, doctorShellCheck(cfg))
	checks = append(checks, doctorProxyCheck(cfg))
	checks = append(checks, doctorNetworkCheck(ctx, cfg)...)
	checks = append(checks, doctorEnvCheck())
	redactDoctorChecks(checks, cfg)
	return checks
}

func redactDoctorChecks(checks []doctorCheck, cfg config.Config) {
	secrets := []string{cfg.APIKey(), cfg.RemoteAPIKey, cfg.ProxyPassword}
	for i := range checks {
		checks[i].Message = redact.Text(checks[i].Message, secrets...)
		checks[i].Detail = redact.Text(checks[i].Detail, secrets...)
		checks[i].Hint = redact.Text(checks[i].Hint, secrets...)
		checks[i].Detail = redact.URL(checks[i].Detail)
	}
}

func doctorConfigCheck(cfg config.Config, cfgErr error) doctorCheck {
	path, err := configPath()
	if err != nil {
		return doctorCheck{Name: "config", Status: doctorWarn, Message: "cannot determine config path", Detail: err.Error(), Hint: "check HOME/XDG_CONFIG_HOME/AppData environment"}
	}
	if cfgErr != nil {
		return doctorCheck{Name: "config", Status: doctorFail, Message: "config is invalid", Detail: cfgErr.Error() + " (" + path + ")", Hint: "fix the config file or run `dmsh config show`"}
	}
	if _, err := os.Stat(path); errorsIsNotExist(err) {
		return doctorCheck{Name: "config", Status: doctorOK, Message: "using defaults (no config file)", Detail: path}
	}
	return doctorCheck{Name: "config", Status: doctorOK, Message: "config loaded", Detail: path}
}

func doctorFilePermChecks(cfg config.Config) []doctorCheck {
	paths := []struct {
		name string
		path string
	}{
		{"config file", mustConfigPath()},
		{"history file", cfg.HistoryFile},
		{"audit file", cfg.AuditFile},
		{"session file", sessionFile()},
	}
	var out []doctorCheck
	for _, p := range paths {
		if strings.TrimSpace(p.path) == "" {
			continue
		}
		info, err := os.Stat(p.path)
		if err != nil {
			if errorsIsNotExist(err) {
				continue
			}
			out = append(out, doctorCheck{Name: p.name, Status: doctorWarn, Message: "cannot stat file", Detail: err.Error()})
			continue
		}
		// Чувствительные файлы должны быть закрыты для других пользователей.
		if runtime.GOOS != "windows" {
			if perm := info.Mode().Perm(); perm&0o077 != 0 {
				out = append(out, doctorCheck{Name: p.name, Status: doctorWarn, Message: "file permissions are too open", Detail: fmt.Sprintf("%s (%04o, want 0600)", p.path, perm), Hint: "restrict permissions to 0600"})
				continue
			}
		}
		out = append(out, doctorCheck{Name: p.name, Status: doctorOK, Message: "permissions look good", Detail: p.path})
	}
	if dir := strings.TrimSpace(filepath.Dir(mustConfigPath())); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			if runtime.GOOS != "windows" {
				if perm := info.Mode().Perm(); perm&0o077 != 0 {
					out = append(out, doctorCheck{Name: "config dir", Status: doctorWarn, Message: "directory permissions are too open", Detail: fmt.Sprintf("%s (%04o, want 0700)", dir, perm), Hint: "restrict permissions to 0700"})
				} else {
					out = append(out, doctorCheck{Name: "config dir", Status: doctorOK, Message: "permissions look good", Detail: dir})
				}
			} else {
				out = append(out, doctorCheck{Name: "config dir", Status: doctorOK, Message: "directory exists", Detail: dir})
			}
		}
	}
	return out
}

func doctorProviderCheck(cfg config.Config) []doctorCheck {
	var out []doctorCheck
	out = append(out, doctorCheck{
		Name:    "provider",
		Status:  doctorOK,
		Message: fmt.Sprintf("provider=%s, mode=%s, tools=%v", cfg.Provider, cfg.Mode, cfg.ToolsEnabled),
		Detail:  fmt.Sprintf("remote_model=%s, base_url=%s", cfg.RemoteModel, remoteEndpoint(cfg.Provider, cfg.RemoteBaseURL)),
	})
	switch cfg.Provider {
	case config.ProviderPollinations, config.ProviderAuto:
		if cfg.APIKey() == "" {
			status := doctorWarn
			msg := "POLLINATIONS_API_KEY is not set (required for Pollinations generation)"
			if cfg.Provider == config.ProviderAuto {
				msg = "POLLINATIONS_API_KEY is not set (auto may fall back to local model if available)"
			}
			out = append(out, doctorCheck{Name: "api key", Status: status, Message: msg, Detail: "set POLLINATIONS_API_KEY or remote_api_key in config", Hint: "configure POLLINATIONS_API_KEY or remote_api_key"})
		} else {
			out = append(out, doctorCheck{Name: "api key", Status: doctorOK, Message: "API key configured", Detail: cfg.APIKeySource()})
		}
	case config.ProviderLocal:
		if strings.TrimSpace(cfg.ModelPath) == "" {
			out = append(out, doctorCheck{Name: "local model", Status: doctorWarn, Message: "model_path is not set", Detail: "set --model or download a GGUF into the models directory", Hint: "run `dmsh model download` or set provider/model in /setup"})
		}
	}
	return out
}

func doctorModelCheck(cfg config.Config) doctorCheck {
	dl := model.New("")
	if err := dl.EnsureDir(); err != nil {
		return doctorCheck{Name: "models dir", Status: doctorWarn, Message: "cannot access models directory", Detail: err.Error()}
	}
	modelDir := filepath.Dir(dl.ModelPath("_placeholder_.gguf"))
	if strings.TrimSpace(cfg.ModelPath) != "" {
		info, err := os.Stat(cfg.ModelPath)
		if err != nil {
			return doctorCheck{Name: "model", Status: doctorWarn, Message: "configured model_path is not accessible", Detail: cfg.ModelPath + ": " + err.Error(), Hint: "check model_path in config or download a model"}
		}
		if info.IsDir() {
			return doctorCheck{Name: "model", Status: doctorWarn, Message: "model_path points to a directory", Detail: cfg.ModelPath, Hint: "set model_path to a .gguf file"}
		}
		detail := fmt.Sprintf("%s (%d MB)", cfg.ModelPath, info.Size()/1024/1024)
		if checked, verr := model.VerifySHA256Sidecar(cfg.ModelPath); verr != nil {
			return doctorCheck{Name: "model", Status: doctorFail, Message: "model checksum verification failed", Detail: detail + ": " + verr.Error(), Hint: "re-download or verify the model file and .sha256 sidecar"}
		} else if checked {
			detail += ", sha256 verified"
		} else {
			detail += ", no .sha256 sidecar"
		}
		return doctorCheck{Name: "model", Status: doctorOK, Message: "configured model file found", Detail: detail}
	}
	models, err := dl.ListAllModels()
	if err != nil {
		return doctorCheck{Name: "model", Status: doctorWarn, Message: "cannot list local models", Detail: err.Error()}
	}
	if len(models) == 0 {
		check := doctorCheck{Name: "model", Status: doctorWarn, Message: "no local GGUF models found", Detail: modelDir, Hint: "run `dmsh model download` or set a remote provider"}
		if !dirWritable(modelDir) {
			check.Status = doctorFail
			check.Message = "models directory is not writable"
			check.Hint = "fix permissions for the models directory"
		}
		return check
	}
	detail := modelDir
	if !dirWritable(modelDir) {
		return doctorCheck{Name: "model", Status: doctorWarn, Message: fmt.Sprintf("%d local GGUF model(s) found, but models directory is not writable", len(models)), Detail: detail, Hint: "fix permissions for the models directory"}
	}
	return doctorCheck{Name: "model", Status: doctorOK, Message: fmt.Sprintf("%d local GGUF model(s) found", len(models)), Detail: detail}
}

func dirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".dmsh-write-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

func doctorBuildCheck() doctorCheck {
	tags := buildTags()
	if strings.Contains(tags, "llama") {
		return doctorCheck{Name: "build", Status: doctorOK, Message: "llama.cpp build detected", Detail: "tags=" + tags + ", " + runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH}
	}
	return doctorCheck{Name: "build", Status: doctorWarn, Message: "stub build (no local llama.cpp inference)", Detail: "rebuild with -tags llama for local GGUF inference; remote providers still work", Hint: "rebuild with -tags llama if you need local inference"}
}

func doctorProviderBuildCheck(cfg config.Config) doctorCheck {
	if cfg.Provider == config.ProviderLocal && !strings.Contains(buildTags(), "llama") {
		return doctorCheck{Name: "provider/build", Status: doctorFail, Message: "local provider selected but this is a stub build", Detail: "rebuild with -tags llama or switch provider with `dmsh config set provider auto`", Hint: "rebuild with -tags llama or switch to a remote provider"}
	}
	return doctorCheck{}
}

func doctorShellCheck(cfg config.Config) doctorCheck {
	shell := strings.TrimSpace(cfg.Shell)
	if shell == "" {
		return doctorCheck{Name: "shell", Status: doctorWarn, Message: "shell is not configured", Hint: "set shell in config"}
	}
	// На Windows PowerShell/cmd обычно доступны без полного пути.
	if filepath.IsAbs(shell) {
		if _, err := os.Stat(shell); err != nil {
			return doctorCheck{Name: "shell", Status: doctorWarn, Message: "configured shell not found", Detail: shell, Hint: "fix shell path in config"}
		}
		return doctorCheck{Name: "shell", Status: doctorOK, Message: "shell found", Detail: shell}
	}
	if _, err := exec.LookPath(shell); err != nil {
		return doctorCheck{Name: "shell", Status: doctorWarn, Message: "configured shell is not in PATH", Detail: shell, Hint: "install the shell or update config"}
	}
	return doctorCheck{Name: "shell", Status: doctorOK, Message: "shell available", Detail: shell}
}

func doctorProxyCheck(cfg config.Config) doctorCheck {
	p := cfg.Proxy()
	endpoint := remoteEndpoint(cfg.Provider, cfg.RemoteBaseURL)
	via, err := p.Effective(endpoint)
	if err != nil {
		return doctorCheck{Name: "proxy", Status: doctorFail, Message: "proxy configuration is invalid", Detail: err.Error(), Hint: "check proxy settings with `dmsh config show` or /settings"}
	}
	return doctorCheck{Name: "proxy", Status: doctorOK, Message: "proxy route resolved", Detail: fmt.Sprintf("endpoint=%s, route=%s", endpoint, proxyRouteLabel(via))}
}

func doctorNetworkCheck(ctx context.Context, cfg config.Config) []doctorCheck {
	// Сеть — опциональная проверка: таймаут короткий, чтобы doctor не зависал.
	endpoint := remoteEndpoint(cfg.Provider, cfg.RemoteBaseURL)
	host := endpointHost(endpoint)
	if host == "" {
		return []doctorCheck{{Name: "network", Status: doctorWarn, Message: "cannot determine remote endpoint host", Detail: endpoint}}
	}

	checks := make([]doctorCheck, 0, 1)
	dctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	d := net.Dialer{Timeout: 2 * time.Second}
	conn, err := d.DialContext(dctx, "tcp", host)
	if err != nil {
		checks = append(checks, doctorCheck{Name: "network", Status: doctorWarn, Message: "cannot reach remote endpoint", Detail: host + ": " + err.Error(), Hint: "check network/proxy/provider endpoint settings"})
		return checks
	}
	_ = conn.Close()
	checks = append(checks, doctorCheck{Name: "network", Status: doctorOK, Message: "remote endpoint reachable", Detail: host})
	return checks
}

func endpointHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Минимальный парсинг URL без лишних зависимостей.
	if i := strings.Index(raw, "://"); i >= 0 {
		raw = raw[i+3:]
	}
	if i := strings.IndexAny(raw, "/?#"); i >= 0 {
		raw = raw[:i]
	}
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "[") {
		if i := strings.Index(raw, "]"); i >= 0 {
			host := raw[1:i]
			port := "443"
			if len(raw) > i+2 && raw[i+1] == ':' {
				port = raw[i+2:]
			}
			return net.JoinHostPort(host, port)
		}
	}
	if strings.Contains(raw, ":") {
		return raw
	}
	// Прямой HTTP в локальной разработке встречается, но по умолчанию 443.
	return net.JoinHostPort(raw, "443")
}

func doctorEnvCheck() doctorCheck {
	keys := []string{config.APIKeyEnv, "HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY", "NO_PROXY"}
	var present []string
	for _, k := range keys {
		if strings.TrimSpace(os.Getenv(k)) != "" {
			present = append(present, k)
		}
	}
	if len(present) == 0 {
		return doctorCheck{Name: "environment", Status: doctorOK, Message: "no relevant proxy/API env vars set"}
	}
	return doctorCheck{Name: "environment", Status: doctorOK, Message: "relevant env vars set", Detail: strings.Join(present, ", ")}
}

func doctorOverallOK(checks []doctorCheck) bool {
	for _, c := range checks {
		if c.Status == doctorFail {
			return false
		}
	}
	return true
}

func printDoctor(out io.Writer, checks []doctorCheck) {
	fmt.Fprintln(out, "=== dmsh doctor ===")
	var fails, warns int
	for _, c := range checks {
		mark := "OK"
		switch c.Status {
		case doctorWarn:
			mark, warns = "WARN", warns+1
		case doctorFail:
			mark, fails = "FAIL", fails+1
		}
		if c.Detail != "" {
			fmt.Fprintf(out, "[%s] %-14s %s (%s)\n", mark, c.Name, c.Message, c.Detail)
		} else {
			fmt.Fprintf(out, "[%s] %-14s %s\n", mark, c.Name, c.Message)
		}
		if c.Hint != "" {
			fmt.Fprintf(out, "       hint: %s\n", c.Hint)
		}
	}
	fmt.Fprintf(out, "\nSummary: %d ok, %d warning(s), %d failure(s)\n", len(checks)-warns-fails, warns, fails)
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "dmsh", "config.json"), nil
}

func mustConfigPath() string {
	p, _ := configPath()
	return p
}

func errorsIsNotExist(err error) bool {
	return err != nil && os.IsNotExist(err)
}


