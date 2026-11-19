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
	checks = append(checks, doctorModelCheck(cfg))
	checks = append(checks, doctorBuildCheck())
	checks = append(checks, doctorShellCheck(cfg))
	checks = append(checks, doctorProxyCheck(cfg))
	checks = append(checks, doctorNetworkCheck(ctx, cfg)...)
	checks = append(checks, doctorEnvCheck())
	return checks
}

func doctorConfigCheck(cfg config.Config, cfgErr error) doctorCheck {
	path, err := configPath()
	if err != nil {
		return doctorCheck{Name: "config", Status: doctorWarn, Message: "cannot determine config path", Detail: err.Error()}
	}
	if cfgErr != nil {
		return doctorCheck{Name: "config", Status: doctorFail, Message: "config is invalid", Detail: cfgErr.Error() + " (" + path + ")"}
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
				out = append(out, doctorCheck{Name: p.name, Status: doctorWarn, Message: "file permissions are too open", Detail: fmt.Sprintf("%s (%04o, want 0600)", p.path, perm)})
				continue
			}
		}
		out = append(out, doctorCheck{Name: p.name, Status: doctorOK, Message: "permissions look good", Detail: p.path})
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
			out = append(out, doctorCheck{Name: "api key", Status: status, Message: msg, Detail: "set POLLINATIONS_API_KEY or remote_api_key in config"})
		} else {
			out = append(out, doctorCheck{Name: "api key", Status: doctorOK, Message: "API key configured", Detail: cfg.APIKeySource()})
		}
	case config.ProviderLocal:
		if strings.TrimSpace(cfg.ModelPath) == "" {
			out = append(out, doctorCheck{Name: "local model", Status: doctorWarn, Message: "model_path is not set", Detail: "set --model or download a GGUF into the models directory"})
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
			return doctorCheck{Name: "model", Status: doctorWarn, Message: "configured model_path is not accessible", Detail: cfg.ModelPath + ": " + err.Error()}
		}
		if info.IsDir() {
			return doctorCheck{Name: "model", Status: doctorWarn, Message: "model_path points to a directory", Detail: cfg.ModelPath}
		}
		detail := fmt.Sprintf("%s (%d MB)", cfg.ModelPath, info.Size()/1024/1024)
		if checked, verr := model.VerifySHA256Sidecar(cfg.ModelPath); verr != nil {
			return doctorCheck{Name: "model", Status: doctorFail, Message: "model checksum verification failed", Detail: detail + ": " + verr.Error()}
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
		return doctorCheck{Name: "model", Status: doctorWarn, Message: "no local GGUF models found", Detail: modelDir}
	}
	return doctorCheck{Name: "model", Status: doctorOK, Message: fmt.Sprintf("%d local GGUF model(s) found", len(models)), Detail: modelDir}
}

func doctorBuildCheck() doctorCheck {
	tags := buildTags()
	if strings.Contains(tags, "llama") {
		return doctorCheck{Name: "build", Status: doctorOK, Message: "llama.cpp build detected", Detail: "tags=" + tags + ", " + runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH}
	}
	return doctorCheck{Name: "build", Status: doctorWarn, Message: "stub build (no local llama.cpp inference)", Detail: "rebuild with -tags llama for local GGUF inference; remote providers still work"}
}

func doctorShellCheck(cfg config.Config) doctorCheck {
	shell := strings.TrimSpace(cfg.Shell)
	if shell == "" {
		return doctorCheck{Name: "shell", Status: doctorWarn, Message: "shell is not configured"}
	}
	// На Windows PowerShell/cmd обычно доступны без полного пути.
	if filepath.IsAbs(shell) {
		if _, err := os.Stat(shell); err != nil {
			return doctorCheck{Name: "shell", Status: doctorWarn, Message: "configured shell not found", Detail: shell}
		}
		return doctorCheck{Name: "shell", Status: doctorOK, Message: "shell found", Detail: shell}
	}
	if _, err := exec.LookPath(shell); err != nil {
		return doctorCheck{Name: "shell", Status: doctorWarn, Message: "configured shell is not in PATH", Detail: shell}
	}
	return doctorCheck{Name: "shell", Status: doctorOK, Message: "shell available", Detail: shell}
}

func doctorProxyCheck(cfg config.Config) doctorCheck {
	p := cfg.Proxy()
	endpoint := remoteEndpoint(cfg.Provider, cfg.RemoteBaseURL)
	via, err := p.Effective(endpoint)
	if err != nil {
		return doctorCheck{Name: "proxy", Status: doctorFail, Message: "proxy configuration is invalid", Detail: err.Error()}
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
		checks = append(checks, doctorCheck{Name: "network", Status: doctorWarn, Message: "cannot reach remote endpoint", Detail: host + ": " + err.Error()})
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


