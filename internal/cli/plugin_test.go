package cli

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/llm"
	"github.com/dedomorozoff/dmsh/internal/tools"
)

func withPlugin(s *session) *session {
	s.plugins = map[string]tools.Plugin{
		"plugin_demo": {Name: "demo", Path: "dmsh-demo"},
	}
	s.registerOptionalTools()
	return s
}

func TestPluginToolSpecAndHelpMode(t *testing.T) {
	s := withPlugin(newToolSession(t, &captureEngine{}))

	found := false
	for _, spec := range s.toolSpecs() {
		if spec.Name == "plugin_demo" {
			found = true
		}
	}
	if !found {
		t.Fatal("an allowlisted plugin must be advertised to the model")
	}

	s.cfg.Mode = config.ModeHelp
	for _, spec := range s.toolSpecs() {
		if spec.Name == "plugin_demo" || spec.Name == tools.NameRunCommand {
			t.Fatalf("help mode must not advertise %q", spec.Name)
		}
	}
}

func TestPluginToolGates(t *testing.T) {
	ctx := context.Background()
	call := func(args string) llm.ToolCall {
		return llm.ToolCall{Name: "plugin_demo", Arguments: `{"args":"` + args + `"}`}
	}

	blocked := withPlugin(newToolSession(t, &captureEngine{}))
	blocked.cfg.DangerPatterns = []string{"forbidden"}
	res := decodeToolResult(t, blocked.runToolCall(ctx, call("forbidden"), io.Discard))
	if res.OK || !strings.Contains(res.Error, "blocked by security policy") {
		t.Fatalf("policy gate result = %+v", res)
	}

	dry := withPlugin(newToolSession(t, &captureEngine{}))
	dry.cfg.DryRun = true
	res = decodeToolResult(t, dry.runToolCall(ctx, call("safe"), io.Discard))
	if res.OK || !strings.Contains(res.Error, "dry-run") {
		t.Fatalf("dry-run gate result = %+v", res)
	}

	confirm := withPlugin(newToolSession(t, &captureEngine{}))
	res = decodeToolResult(t, confirm.runToolCall(ctx, call("safe"), io.Discard))
	if res.OK || !strings.Contains(res.Error, "confirmation is required") {
		t.Fatalf("confirmation gate result = %+v", res)
	}

	unknown := decodeToolResult(t, confirm.runToolCall(ctx, llm.ToolCall{
		Name: "plugin_unknown", Arguments: `{}`,
	}, io.Discard))
	if unknown.OK || !strings.Contains(unknown.Error, "not allowlisted") {
		t.Fatalf("unknown plugin result = %+v", unknown)
	}
}
