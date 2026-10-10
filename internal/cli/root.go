package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/llm"
	"github.com/dedomorozoff/dmsh/internal/model"
	"github.com/dedomorozoff/dmsh/internal/netproxy"
	"github.com/spf13/cobra"
)

// rootFlags holds common flags shared across subcommands.
type rootFlags struct {
	cfg     config.Config
	preview bool
	autoYes bool
	noTools bool
}

// providerValue — pflag.Value для провайдера инференса: принимает только
// известные значения и сразу приводит регистр к нижнему.
type providerValue struct{ p *config.Provider }

func (v providerValue) String() string { return string(*v.p) }

func (v providerValue) Set(s string) error {
	norm := config.Provider(strings.ToLower(strings.TrimSpace(s)))
	if !norm.Valid() {
		return fmt.Errorf("invalid provider %q (expected local, pollinations, ollama or auto)", s)
	}
	*v.p = norm
	return nil
}

func (v providerValue) Type() string { return "provider" }

// proxyModeValue — pflag.Value для режима прокси: принимает только auto,
// off и custom.
type proxyModeValue struct{ m *netproxy.Mode }

func (v proxyModeValue) String() string { return string(*v.m) }

func (v proxyModeValue) Set(s string) error {
	m, err := netproxy.ParseMode(s)
	if err != nil {
		return err
	}
	*v.m = m
	return nil
}

func (v proxyModeValue) Type() string { return "proxy-mode" }

// proxyProtoValue — pflag.Value для протокола прокси.
type proxyProtoValue struct{ s *string }

func (v proxyProtoValue) String() string { return *v.s }

func (v proxyProtoValue) Set(s string) error {
	p := strings.ToLower(strings.TrimSpace(s))
	if !netproxy.ValidProto(p) {
		return fmt.Errorf("invalid proxy protocol %q (expected http, https, socks5 or socks5h)", s)
	}
	*v.s = p
	return nil
}

func (v proxyProtoValue) Type() string { return "proxy-proto" }

// proxyAddressValue — pflag.Value для --proxy: принимает адрес целиком и
// раскладывает его по полям (протокол, хост, порт, логин, пароль).
type proxyAddressValue struct{ cfg *config.Config }

func (v proxyAddressValue) String() string { return v.cfg.Proxy().Address() }

func (v proxyAddressValue) Set(s string) error {
	p := v.cfg.Proxy()
	if err := p.ApplyURL(s); err != nil {
		return err
	}
	v.cfg.SetProxy(p)
	return nil
}

func (v proxyAddressValue) Type() string { return "proxy-address" }

// NewRootCmd assembles the root cobra command.
func NewRootCmd() *cobra.Command {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "dmsh: "+err.Error())
	}
	rf := &rootFlags{cfg: cfg}

	cmd := &cobra.Command{
		Use:   "dmsh [query]",
		Short: "Direct Model Shell — talk to your system naturally",
		Long:  "dmsh is an LLM-powered shell assistant. Inference runs on a local GGUF via llama.cpp, on a local Ollama server, or remotely via Pollinations; with provider=auto, a local GGUF is preferred and Pollinations is the fallback.",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return runInteractive(cmd, rf)
			}
			input := strings.TrimSpace(strings.Join(args, " "))
			if input == "" {
				return runInteractive(cmd, rf)
			}
			return runOneShot(cmd, rf, input)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			if rf.noTools {
				rf.cfg.ToolsEnabled = false
			}
			// Флаги разобраны только сейчас, поэтому прокси ставим здесь:
			// скачивание GGUF идёт мимо сессии и должно видеть те же
			// настройки, что и удалённый API.
			if err := model.SetProxy(rf.cfg.Proxy()); err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "dmsh: proxy: "+err.Error())
			}
		},
	}

	pf := cmd.PersistentFlags()
	pf.Var(providerValue{&rf.cfg.Provider}, "provider", "inference provider: local, ollama, pollinations or auto")
	pf.StringVar(&rf.cfg.RemoteModel, "remote-model", rf.cfg.RemoteModel, "model name for the remote provider")
	pf.StringVar(&rf.cfg.RemoteBaseURL, "remote-base-url", rf.cfg.RemoteBaseURL, "base URL of an OpenAI-compatible endpoint (default "+llm.DefaultPollinationsBaseURL+", ollama: "+llm.DefaultOllamaBaseURL+")")
	pf.StringVar(&rf.cfg.RemoteAPIKey, "api-key", rf.cfg.RemoteAPIKey, "API key for the remote provider (prefer the "+config.APIKeyEnv+" env var)")
	pf.StringVar(&rf.cfg.SearchModel, "search-model", rf.cfg.SearchModel, "model used by the websearch tool (default "+config.DefaultSearchModel+")")
	pf.Var(proxyModeValue{&rf.cfg.ProxyMode}, "proxy-mode", "proxy source: auto (HTTP_PROXY/NO_PROXY), off or custom")
	pf.Var(proxyAddressValue{&rf.cfg}, "proxy", "whole proxy address at once: http://, https://, socks5:// or socks5h://")
	pf.Var(proxyProtoValue{&rf.cfg.ProxyProto}, "proxy-proto", "proxy protocol: http, https, socks5 or socks5h")
	pf.StringVar(&rf.cfg.ProxyHost, "proxy-host", rf.cfg.ProxyHost, "proxy host: name or IP")
	pf.IntVar(&rf.cfg.ProxyPort, "proxy-port", rf.cfg.ProxyPort, "proxy port (0 = default for the protocol)")
	pf.StringVar(&rf.cfg.ProxyUser, "proxy-user", rf.cfg.ProxyUser, "proxy login")
	pf.StringVar(&rf.cfg.ProxyPassword, "proxy-password", rf.cfg.ProxyPassword, "proxy password")
	pf.StringVar(&rf.cfg.NoProxy, "no-proxy", rf.cfg.NoProxy, "hosts that bypass the proxy, comma-separated (host, host:port, .suffix or CIDR)")
	pf.BoolVar(&rf.noTools, "no-tools", false, "disable LLM tool (function) calls")
	pf.StringVar(&rf.cfg.ModelPath, "model", rf.cfg.ModelPath, "path to GGUF model file")
	pf.IntVar(&rf.cfg.Threads, "threads", rf.cfg.Threads, "number of inference threads")
	pf.IntVar(&rf.cfg.CtxSize, "ctx-size", rf.cfg.CtxSize, "context size in tokens")
	pf.IntVar(&rf.cfg.GPULayers, "gpu-layers", rf.cfg.GPULayers, "number of layers offloaded to GPU (0 = CPU only)")
	pf.IntVar(&rf.cfg.MaxTokens, "max-tokens", rf.cfg.MaxTokens, "max tokens in model response")
	pf.Float32Var(&rf.cfg.Temperature, "temperature", rf.cfg.Temperature, "sampling temperature")
	pf.Float32Var(&rf.cfg.TopP, "top-p", rf.cfg.TopP, "top-p sampling threshold")
	pf.StringVar(&rf.cfg.Shell, "shell", rf.cfg.Shell, "shell for command execution")
	pf.BoolVar(&rf.cfg.DryRun, "dry-run", rf.cfg.DryRun, "show commands without executing them")
	pf.BoolVar(&rf.preview, "preview", false, "show the command with highlighted options and require confirmation before executing")
	pf.BoolVar(&rf.autoYes, "yes", false, "automatically approve confirmations (for scripting)")

	cmd.AddCommand(newAskCmd(rf))
	cmd.AddCommand(newRunCmd(rf))
	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newConfigCmd())
	cmd.AddCommand(newHistoryCmd())
	cmd.AddCommand(newAuditCmd())
	addModelCommand(cmd, rf)

	return cmd
}
