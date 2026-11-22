package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/dedomorozoff/dmsh/internal/tools"
)

func TestPlanGenerationRequestAndMissingStore(t *testing.T) {
	req := planGenerationRequest("show files")
	if !strings.Contains(req, "intent=explain") || !strings.Contains(req, "show files") {
		t.Fatalf("plan request = %q", req)
	}
	if _, _, err := executePlan(context.Background(), &session{}, &rootFlags{}, io.Discard, io.Discard); err == nil {
		t.Fatal("a missing todo store must be reported")
	}
}

func TestParsePlanSteps(t *testing.T) {
	got := parsePlanSteps("1. list files\n2) read the log\n- run tests\n* commit\n• done\n\n   \n")
	want := []string{"list files", "read the log", "run tests", "commit", "done"}
	if len(got) != len(want) {
		t.Fatalf("steps = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("step %d = %q, want %q", i, got[i], want[i])
		}
	}
	if got := parsePlanSteps("plain line"); len(got) != 1 || got[0] != "plain line" {
		t.Fatalf("unformatted steps = %v", got)
	}
}

func TestPlanStepLimits(t *testing.T) {
	lines := make([]string, 25)
	for i := range lines {
		lines[i] = "step"
	}
	if got := parsePlanSteps(strings.Join(lines, "\n")); len(got) != planMaxSteps {
		t.Fatalf("plan must be capped at %d steps, got %d", planMaxSteps, len(got))
	}

	steps := make([]string, tools.MaxTodoItems+1)
	for i := range steps {
		steps[i] = "step"
	}
	s := &session{todos: tools.NewTodoStore()}
	if _, err := fillPlan(s, steps); err == nil {
		t.Fatal("an oversized plan must be rejected")
	}
}

func TestExecutePlanSkipsStepsWithoutCommand(t *testing.T) {
	eng := &captureEngine{tokens: []string{`{"intent":"explain","explanation":"no command"}`}}
	s := newToolSession(t, eng)
	if _, err := fillPlan(s, []string{"step"}); err != nil {
		t.Fatal(err)
	}
	var out, errW bytes.Buffer
	executed, failed, err := executePlan(context.Background(), s, &rootFlags{cfg: s.cfg}, &out, &errW)
	if err != nil || executed != 0 || failed != 1 {
		t.Fatalf("executePlan = %d/%d/%v", executed, failed, err)
	}
}

func TestFillPlanReplacesOldChecklist(t *testing.T) {
	s := &session{todos: tools.NewTodoStore()}
	if _, err := s.todos.Add("old step"); err != nil {
		t.Fatal(err)
	}
	items, err := fillPlan(s, []string{"one", "two"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Task != "one" || items[1].Task != "two" {
		t.Fatalf("items = %v", items)
	}
	all := s.todos.Items()
	if len(all) != 2 || all[0].Task != "one" {
		t.Fatalf("stale todo survived: %v", all)
	}
	if _, err := fillPlan(&session{}, []string{"x"}); err == nil {
		t.Fatal("a missing todo store must be reported")
	}
}

func TestExecutePlanDryRun(t *testing.T) {
	eng := &captureEngine{tokens: []string{`{"intent":"run_command","command":"echo hi","risk_level":"low"}`}}
	s := newToolSession(t, eng)
	s.cfg.DryRun = true
	if _, err := fillPlan(s, []string{"say hi"}); err != nil {
		t.Fatal(err)
	}
	var out, errW bytes.Buffer
	executed, failed, err := executePlan(context.Background(), s, &rootFlags{cfg: s.cfg}, &out, &errW)
	if err != nil || executed != 1 || failed != 0 {
		t.Fatalf("executePlan = %d/%d/%v; out=%q err=%q", executed, failed, err, out.String(), errW.String())
	}
	if s.todos.Open() != 1 {
		t.Fatal("dry-run steps must stay open")
	}
}

func TestPlanSlashOpensTodoMenu(t *testing.T) {
	m := newTestTui()
	m.s.todos = tools.NewTodoStore()
	m.s.engine = &captureEngine{tokens: []string{`{"intent":"explain","explanation":"1. list files\n2. read log"}`}}

	mm, cmd := m.handleSlash("/plan show files")
	if cmd == nil {
		t.Fatal("plan generation must return a command")
	}
	msg, ok := cmd().(planReadyMsg)
	if !ok || msg.err != nil {
		t.Fatalf("plan message = %#v", msg)
	}
	updated, _ := mm.(tuiModel).Update(msg)
	got := updated.(tuiModel)
	if got.state != tuiTodoMenu {
		t.Fatalf("state = %v, want the todo menu", got.state)
	}
	items := got.s.todos.Items()
	if len(items) != 2 || items[0].Task != "list files" {
		t.Fatalf("plan steps = %v", items)
	}
}

func TestPlanRunSlashReportsResult(t *testing.T) {
	m := newTestTui()
	m.s.todos = tools.NewTodoStore()
	if _, err := m.s.todos.Add("say hi"); err != nil {
		t.Fatal(err)
	}
	m.s.engine = &captureEngine{tokens: []string{`{"intent":"run_command","command":"echo hi","risk_level":"low"}`}}
	m.rf.cfg.DryRun = true
	m.s.cfg.DryRun = true

	mm, cmd := m.handleSlash("/plan run")
	if cmd == nil {
		t.Fatal("plan run must return a command")
	}
	msg, ok := cmd().(planRunDoneMsg)
	if !ok || msg.err != nil || msg.executed != 1 || msg.failed != 0 {
		t.Fatalf("plan run message = %#v", msg)
	}
	updated, _ := mm.(tuiModel).Update(msg)
	if got := updated.(tuiModel); got.planRunning {
		t.Fatal("planRunning must be cleared after the run")
	}
}
