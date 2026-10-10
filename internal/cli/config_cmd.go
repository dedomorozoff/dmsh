package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/netproxy"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
		Long:  "View and edit configuration parameters for dmsh",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "=== Current Configuration ===")
			fmt.Fprintf(out, "  %-20s %v\n", "provider:", cfg.Provider)
			fmt.Fprintf(out, "  %-20s %v\n", "remote_model:", cfg.RemoteModel)
			fmt.Fprintf(out, "  %-20s %v\n", "search_model:", cfg.SearchModel)
			if cfg.RemoteBaseURL != "" {
				fmt.Fprintf(out, "  %-20s %v\n", "remote_base_url:", cfg.RemoteBaseURL)
			}
			if cfg.APIKeySource() != "" {
				fmt.Fprintf(out, "  %-20s %s\n", "remote_api_key:", cfg.MaskedAPIKey())
			}
			fmt.Fprintf(out, "  %-20s %v\n", "tools_enabled:", cfg.ToolsEnabled)
			fmt.Fprintf(out, "  %-20s %v\n", "proxy_mode:", cfg.ProxyMode)
			if cfg.ProxyHost != "" {
				fmt.Fprintf(out, "  %-20s %v\n", "proxy_proto:", cfg.Proxy().Proto)
				fmt.Fprintf(out, "  %-20s %v\n", "proxy_host:", cfg.ProxyHost)
				if cfg.ProxyPort > 0 {
					fmt.Fprintf(out, "  %-20s %v\n", "proxy_port:", cfg.ProxyPort)
				}
				if cfg.ProxyUser != "" {
					fmt.Fprintf(out, "  %-20s %v\n", "proxy_user:", cfg.ProxyUser)
					fmt.Fprintf(out, "  %-20s %v\n", "proxy_password:", netproxy.Settings{Password: cfg.ProxyPassword}.MaskedPassword())
				}
			}
			if cfg.NoProxy != "" {
				fmt.Fprintf(out, "  %-20s %v\n", "no_proxy:", cfg.NoProxy)
			}
			if eff, err := cfg.Proxy().Effective(remoteEndpoint(cfg.Provider, cfg.RemoteBaseURL)); err == nil {
				fmt.Fprintf(out, "  %-20s %s\n", "proxy in effect:", proxyRouteLabel(eff))
			}
			fmt.Fprintf(out, "  %-20s %v\n", "model_path:", cfg.ModelPath)
			fmt.Fprintf(out, "  %-20s %v\n", "default_model:", cfg.DefaultModel)
			fmt.Fprintf(out, "  %-20s %v\n", "threads:", cfg.Threads)
			fmt.Fprintf(out, "  %-20s %v\n", "ctx_size:", cfg.CtxSize)
			fmt.Fprintf(out, "  %-20s %v\n", "gpu_layers:", cfg.GPULayers)
			fmt.Fprintf(out, "  %-20s %v\n", "max_tokens:", cfg.MaxTokens)
			fmt.Fprintf(out, "  %-20s %.2f\n", "temperature:", cfg.Temperature)
			fmt.Fprintf(out, "  %-20s %.2f\n", "top_p:", cfg.TopP)
			fmt.Fprintf(out, "  %-20s %v\n", "shell:", cfg.Shell)
			fmt.Fprintf(out, "  %-20s %v\n", "history_file:", cfg.HistoryFile)
			fmt.Fprintf(out, "  %-20s %v\n", "dry_run:", cfg.DryRun)
			fmt.Fprintf(out, "  %-20s %v\n", "mode:", cfg.Mode)

			if len(cfg.DangerPatterns) > 0 {
				fmt.Fprintln(out, "  danger_patterns:")
				for _, pat := range cfg.DangerPatterns {
					fmt.Fprintf(out, "    - %q\n", pat)
				}
			} else {
				fmt.Fprintf(out, "  %-20s %v\n", "danger_patterns:", "none")
			}

			if len(cfg.SuspiciousPatterns) > 0 {
				fmt.Fprintln(out, "  suspicious_patterns:")
				for _, pat := range cfg.SuspiciousPatterns {
					fmt.Fprintf(out, "    - %q\n", pat)
				}
			} else {
				fmt.Fprintf(out, "  %-20s %v\n", "suspicious_patterns:", "none")
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set configuration parameter",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			key := normalizeConfigKey(args[0])
			val := args[1]
			out := cmd.OutOrStdout()

			switch key {
			case "provider":
				p := config.Provider(strings.ToLower(val))
				if !p.Valid() {
					return fmt.Errorf("invalid provider %q, allowed: local, pollinations, ollama, auto", val)
				}
				cfg.Provider = p
			case "remotemodel":
				cfg.RemoteModel = val
			case "apikey", "remoteapikey":
				cfg.RemoteAPIKey = val
			case "searchmodel":
				cfg.SearchModel = val
			case "remotebaseurl":
				cfg.RemoteBaseURL = val
			case "proxymode":
				m, err := netproxy.ParseMode(val)
				if err != nil {
					return err
				}
				cfg.ProxyMode = m
			case "proxy", "proxyaddress":
				p := cfg.Proxy()
				if err := p.ApplyURL(val); err != nil {
					return err
				}
				cfg.SetProxy(p)
			case "proxyproto":
				proto := strings.ToLower(val)
				if !netproxy.ValidProto(proto) {
					return fmt.Errorf("invalid proxy protocol %q, allowed: http, https, socks5, socks5h", val)
				}
				cfg.ProxyProto = proto
			case "proxyhost":
				cfg.ProxyHost = val
			case "proxyport":
				v, err := strconv.Atoi(val)
				if err != nil {
					return fmt.Errorf("invalid int value for proxy_port: %w", err)
				}
				cfg.ProxyPort = v
			case "proxyuser", "proxylogin":
				cfg.ProxyUser = val
			case "proxypassword", "proxypass":
				cfg.ProxyPassword = val
			case "noproxy", "noproxies":
				cfg.NoProxy = val
			case "tools", "toolsenabled":
				v, err := strconv.ParseBool(val)
				if err != nil {
					return fmt.Errorf("invalid bool value for tools_enabled: %w", err)
				}
				cfg.ToolsEnabled = v
			case "modelpath":
				cfg.ModelPath = val
			case "defaultmodel":
				cfg.DefaultModel = val
			case "threads":
				v, err := strconv.Atoi(val)
				if err != nil {
					return fmt.Errorf("invalid int value for threads: %w", err)
				}
				cfg.Threads = v
			case "ctxsize":
				v, err := strconv.Atoi(val)
				if err != nil {
					return fmt.Errorf("invalid int value for ctx_size: %w", err)
				}
				cfg.CtxSize = v
			case "gpulayers":
				v, err := strconv.Atoi(val)
				if err != nil {
					return fmt.Errorf("invalid int value for gpu_layers: %w", err)
				}
				cfg.GPULayers = v
			case "maxtokens":
				v, err := strconv.Atoi(val)
				if err != nil {
					return fmt.Errorf("invalid int value for max_tokens: %w", err)
				}
				cfg.MaxTokens = v
			case "temperature":
				v, err := strconv.ParseFloat(val, 32)
				if err != nil {
					return fmt.Errorf("invalid float value for temperature: %w", err)
				}
				cfg.Temperature = float32(v)
			case "topp":
				v, err := strconv.ParseFloat(val, 32)
				if err != nil {
					return fmt.Errorf("invalid float value for top_p: %w", err)
				}
				cfg.TopP = float32(v)
			case "shell":
				cfg.Shell = val
			case "historyfile":
				cfg.HistoryFile = val
			case "dryrun":
				v, err := strconv.ParseBool(val)
				if err != nil {
					return fmt.Errorf("invalid bool value for dry_run: %w", err)
				}
				cfg.DryRun = v
			case "mode":
				m := config.Mode(val)
				if m != config.ModeAI && m != config.ModeHelp && m != config.ModeShell {
					return fmt.Errorf("invalid mode %q, allowed: ai, help, shell", val)
				}
				cfg.Mode = m
			case "dangerpatterns":
				// Split by comma if multiple values, otherwise assign single
				if val == "" {
					cfg.DangerPatterns = nil
				} else {
					cfg.DangerPatterns = splitCSV(val)
				}
			case "suspiciouspatterns":
				if val == "" {
					cfg.SuspiciousPatterns = nil
				} else {
					cfg.SuspiciousPatterns = splitCSV(val)
				}
			default:
				return fmt.Errorf("unknown configuration key %q", args[0])
			}

			// Не даём записать конфиг, который dmsh не сможет применить:
			// битый адрес прокси иначе обнаружился бы первым запросом к
			// модели, а не здесь.
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("invalid configuration: %w", err)
			}

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			fmt.Fprintf(out, "Successfully set %s to %v\n", args[0], val)
			return nil
		},
	})

	return cmd
}

func normalizeConfigKey(k string) string {
	k = strings.ToLower(k)
	k = strings.ReplaceAll(k, "-", "")
	k = strings.ReplaceAll(k, "_", "")
	return k
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
