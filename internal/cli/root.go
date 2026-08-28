package cli

import (
	"strings"

	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/spf13/cobra"
)

// rootFlags holds common flags shared across subcommands.
type rootFlags struct {
	cfg config.Config
}

// NewRootCmd assembles the root cobra command.
func NewRootCmd() *cobra.Command {
	cfg, _ := config.Load()
	rf := &rootFlags{cfg: cfg}

	cmd := &cobra.Command{
		Use:   "dmsh [query]",
		Short: "Direct Model Shell — talk to your system naturally",
		Long:  "dmsh is a local LLM-powered shell assistant. Uses llama.cpp for on-device inference — no cloud, no HTTP server.",
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
	}

	pf := cmd.PersistentFlags()
	pf.StringVar(&rf.cfg.ModelPath, "model", rf.cfg.ModelPath, "path to GGUF model file")
	pf.IntVar(&rf.cfg.Threads, "threads", rf.cfg.Threads, "number of inference threads")
	pf.IntVar(&rf.cfg.CtxSize, "ctx-size", rf.cfg.CtxSize, "context size in tokens")
	pf.IntVar(&rf.cfg.GPULayers, "gpu-layers", rf.cfg.GPULayers, "number of layers offloaded to GPU (0 = CPU only)")
	pf.IntVar(&rf.cfg.MaxTokens, "max-tokens", rf.cfg.MaxTokens, "max tokens in model response")
	pf.Float32Var(&rf.cfg.Temperature, "temperature", rf.cfg.Temperature, "sampling temperature")
	pf.Float32Var(&rf.cfg.TopP, "top-p", rf.cfg.TopP, "top-p sampling threshold")
	pf.StringVar(&rf.cfg.Shell, "shell", rf.cfg.Shell, "shell for command execution")
	pf.BoolVar(&rf.cfg.DryRun, "dry-run", rf.cfg.DryRun, "show commands without executing them")

	cmd.AddCommand(newAskCmd(rf))
	cmd.AddCommand(newRunCmd(rf))
	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newConfigCmd())
	cmd.AddCommand(newHistoryCmd())
	addModelCommand(cmd, rf)

	return cmd
}
