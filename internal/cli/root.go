package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/llm"
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
		return fmt.Errorf("invalid provider %q (expected local, pollinations or auto)", s)
	}
	*v.p = norm
	return nil
}

func (v providerValue) Type() string { return "provider" }

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
		Long:  "dmsh is an LLM-powered shell assistant. Inference runs on a local GGUF via llama.cpp; with provider=auto, and no local model, it uses the Pollinations API instead.",
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
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			if rf.noTools {
				rf.cfg.ToolsEnabled = false
			}
		},
	}

	pf := cmd.PersistentFlags()
	pf.Var(providerValue{&rf.cfg.Provider}, "provider", "inference provider: local, pollinations or auto")
	pf.StringVar(&rf.cfg.RemoteModel, "remote-model", rf.cfg.RemoteModel, "model name for the remote provider")
	pf.StringVar(&rf.cfg.RemoteBaseURL, "remote-base-url", rf.cfg.RemoteBaseURL, "base URL of an OpenAI-compatible endpoint (default "+llm.DefaultPollinationsBaseURL+")")
	pf.StringVar(&rf.cfg.SearchModel, "search-model", rf.cfg.SearchModel, "model used by the websearch tool (default "+config.DefaultSearchModel+")")
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
