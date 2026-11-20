package cli

import (
	"strings"

	"github.com/dedomorozoff/dmsh/internal/model"
	"github.com/spf13/cobra"
)

func completeDownloadedModels(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	d := model.New("")
	models, err := d.ListAllModels()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	out := make([]string, 0, len(models))
	for _, m := range models {
		if strings.HasPrefix(m.Name, toComplete) {
			out = append(out, m.Name)
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

var configSetKeys = []string{
	"provider",
	"remote_model",
	"search_model",
	"remote_base_url",
	"remote_api_key",
	"tools_enabled",
	"proxy_mode",
	"proxy",
	"proxy_proto",
	"proxy_host",
	"proxy_port",
	"proxy_user",
	"proxy_password",
	"no_proxy",
	"model_path",
	"default_model",
	"threads",
	"ctx_size",
	"gpu_layers",
	"max_tokens",
	"temperature",
	"top_p",
	"shell",
	"history_file",
	"dry_run",
	"mode",
	"mouse",
	"danger_patterns",
	"suspicious_patterns",
}

func completeConfigSet(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0:
		out := make([]string, 0, len(configSetKeys))
		for _, k := range configSetKeys {
			if strings.HasPrefix(k, toComplete) {
				out = append(out, k)
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	case 1:
		return completeConfigValue(cmd, args[0], toComplete)
	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

func completeConfigValue(cmd *cobra.Command, key, toComplete string) ([]string, cobra.ShellCompDirective) {
	switch normalizeConfigKey(key) {
	case "provider":
		return filterPrefix([]string{"auto", "local", "ollama", "pollinations"}, toComplete), cobra.ShellCompDirectiveNoFileComp
	case "mode":
		return filterPrefix([]string{"ai", "help", "shell"}, toComplete), cobra.ShellCompDirectiveNoFileComp
	case "proxymode":
		return filterPrefix([]string{"auto", "custom", "off"}, toComplete), cobra.ShellCompDirectiveNoFileComp
	case "proxyproto":
		return filterPrefix([]string{"http", "https", "socks5", "socks5h"}, toComplete), cobra.ShellCompDirectiveNoFileComp
	case "dryrun", "tools", "toolsenabled", "mouse":
		return filterPrefix([]string{"false", "true"}, toComplete), cobra.ShellCompDirectiveNoFileComp
	case "defaultmodel":
		vals, _ := completeDownloadedModels(cmd, nil, toComplete)
		return vals, cobra.ShellCompDirectiveNoFileComp
	case "modelpath":
		vals, _ := completeDownloadedModels(cmd, nil, toComplete)
		// Allow falling back to file completion for arbitrary paths.
		return vals, cobra.ShellCompDirectiveDefault
	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

func filterPrefix(values []string, prefix string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if strings.HasPrefix(v, prefix) {
			out = append(out, v)
		}
	}
	return out
}

func registerCommonFlagCompletions(cmd *cobra.Command) {
	_ = cmd.RegisterFlagCompletionFunc("provider", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{"auto", "local", "ollama", "pollinations"}, cobra.ShellCompDirectiveNoFileComp
	})
	_ = cmd.RegisterFlagCompletionFunc("proxy-mode", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{"auto", "custom", "off"}, cobra.ShellCompDirectiveNoFileComp
	})
	_ = cmd.RegisterFlagCompletionFunc("proxy-proto", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{"http", "https", "socks5", "socks5h"}, cobra.ShellCompDirectiveNoFileComp
	})
}
