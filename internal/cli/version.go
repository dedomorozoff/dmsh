package cli

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/spf13/cobra"
)

// Version is set via -ldflags at build time.
var Version = "dev"

// BuildDate is set via -ldflags at build time (RFC3339 UTC).
var BuildDate = ""

// displayVersion возвращает человекочитаемую версию.
// Из git describe ("v0.1.3-48-g2c82189-dirty") оставляем только номер тега.
func displayVersion() string {
	v := strings.TrimSpace(Version)
	if v == "" {
		return "dev"
	}
	if i := strings.IndexByte(v, '-'); i > 0 {
		v = v[:i]
	}
	v = strings.TrimPrefix(v, "v")
	if v == "" {
		return "dev"
	}
	return v
}

// buildTags возвращает список go build-тегов (например "llama") или "stub".
func buildTags() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "-tags" && s.Value != "" {
				return s.Value
			}
		}
	}
	return "stub"
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "dmsh %s\n", displayVersion())
			if d := strings.TrimSpace(BuildDate); d != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  Built:    %s\n", d)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  Go:       %s\n", runtime.Version())
			fmt.Fprintf(cmd.OutOrStdout(), "  Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
			fmt.Fprintf(cmd.OutOrStdout(), "  Build:    %s\n", buildTags())
		},
	}
}

func newInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show system and configuration info",
		Run: func(cmd *cobra.Command, _ []string) {
			hw := config.DetectHardware()
			cfg, _ := config.Load()

			fmt.Fprintln(cmd.OutOrStdout(), "=== System Information ===")
			fmt.Fprintf(cmd.OutOrStdout(), "OS:           %s (%s)\n", osName(), osVersion())
			fmt.Fprintf(cmd.OutOrStdout(), "Build:        %s\n", buildInfo())
			fmt.Fprintf(cmd.OutOrStdout(), "CPU Cores:    %d\n", hw.CPUCores)
			fmt.Fprintf(cmd.OutOrStdout(), "RAM:          %d GB\n", hw.RAMGB)
			fmt.Fprintf(cmd.OutOrStdout(), "GPU:          %s\n", hw.GPUName)
			fmt.Fprintf(cmd.OutOrStdout(), "GPU Type:     %s\n", hw.GPUType)
			fmt.Fprintf(cmd.OutOrStdout(), "GPU Layers:   %d\n", hw.GPULayers)
			fmt.Fprintln(cmd.OutOrStdout(), "")
			fmt.Fprintln(cmd.OutOrStdout(), "=== Current Config ===")
			fmt.Fprintf(cmd.OutOrStdout(), "Threads:      %d\n", cfg.Threads)
			fmt.Fprintf(cmd.OutOrStdout(), "Ctx Size:     %d\n", cfg.CtxSize)
			fmt.Fprintf(cmd.OutOrStdout(), "GPU Layers:   %d\n", cfg.GPULayers)
			fmt.Fprintf(cmd.OutOrStdout(), "Max Tokens:   %d\n", cfg.MaxTokens)
			fmt.Fprintf(cmd.OutOrStdout(), "Temperature:  %.2f\n", cfg.Temperature)
			fmt.Fprintf(cmd.OutOrStdout(), "Top P:        %.2f\n", cfg.TopP)
			fmt.Fprintf(cmd.OutOrStdout(), "Shell:        %s\n", cfg.Shell)
			fmt.Fprintf(cmd.OutOrStdout(), "Dry Run:      %t\n", cfg.DryRun)
		},
	}
}
