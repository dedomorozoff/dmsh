package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/dedomorozoff/dmsh/internal/llm"
)

// PluginPrefix is the naming convention for external dmsh helpers in PATH.
const PluginPrefix = "dmsh-"

// Plugin — discovered, allowlisted executable exposed to the model.
type Plugin struct {
	Name string // suffix, e.g. "git"
	Path string // absolute path to dmsh-git
}

// PluginToolName converts a plugin suffix into a tool name. Only simple names
// become tools: the model sees a short, predictable identifier.
func PluginToolName(name string) string {
	name = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(name, PluginPrefix)))
	if name == "" {
		return ""
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		return ""
	}
	return "plugin_" + name
}

// DiscoverPlugins resolves allowlisted names against PATH. Nothing is executed
// during discovery: an empty allowlist means the feature is off.
func DiscoverPlugins(pathList string, allowlist []string) []Plugin {
	var out []Plugin
	seen := map[string]bool{}
	for _, raw := range allowlist {
		name := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(raw, PluginPrefix)))
		if name == "" || seen[name] || PluginToolName(name) == "" {
			continue
		}
		seen[name] = true
		path := findPlugin(pathList, name)
		if path != "" {
			out = append(out, Plugin{Name: name, Path: path})
		}
	}
	return out
}

func findPlugin(pathList, name string) string {
	for _, dir := range filepath.SplitList(pathList) {
		if dir == "" {
			continue
		}
		base := PluginPrefix + name
		candidates := []string{filepath.Join(dir, base)}
		if runtime.GOOS == "windows" {
			candidates = append(candidates, filepath.Join(dir, base+".exe"))
		}
		for _, path := range candidates {
			info, err := os.Stat(path)
			if err != nil || info.IsDir() || !info.Mode().IsRegular() {
				continue
			}
			if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
				continue
			}
			return path
		}
	}
	return ""
}

// PluginArgs — аргументы инструмента плагина.
type PluginArgs struct {
	Args string `json:"args,omitempty"`
}

// PluginSpec describes one external tool. The model gets only the name and a
// plain string of arguments; the binary itself is resolved by the app.
func PluginSpec(name string) llm.ToolSpec {
	return llm.ToolSpec{
		Name:        PluginToolName(name),
		Description: "Run the allowlisted external helper " + PluginPrefix + name + " with the given arguments.",
		Parameters: objectSchema(map[string]any{
			"args": map[string]any{
				"type":        "string",
				"description": "Space-separated arguments for the helper (quotes group words).",
			},
		}, "args"),
	}
}

// SplitArgs splits plugin arguments without a shell. Quotes group words, so a
// path with spaces stays a single argv entry and metacharacters stay literal.
func SplitArgs(raw string) ([]string, error) {
	var args []string
	var cur strings.Builder
	var quote rune
	escaped, started := false, false
	for _, r := range raw {
		if escaped {
			cur.WriteRune(r)
			escaped, started = false, true
			continue
		}
		switch {
		case quote != 0:
			switch r {
			case quote:
				quote = 0
			case '\\':
				escaped = true
			default:
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, started = r, true
		case r == '\\':
			escaped, started = true, true
		case r == ' ' || r == '\t' || r == '\n':
			if started {
				args = append(args, cur.String())
				cur.Reset()
				started = false
			}
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	if escaped {
		cur.WriteRune('\\')
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote", quote)
	}
	if started {
		args = append(args, cur.String())
	}
	return args, nil
}
