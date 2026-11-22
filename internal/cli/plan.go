package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/dedomorozoff/dmsh/internal/prompt"
	"github.com/dedomorozoff/dmsh/internal/tools"
)

// planMaxSteps keeps a generated plan reviewable: a long checklist is harder
// to verify than to just run the request step by step.
const planMaxSteps = 20

var planStepPrefix = regexp.MustCompile(`^\s*(?:\d+[.)]|[-*•])\s*`)

// planGenerationRequest asks for a step list using the existing JSON contract:
// an explain response whose explanation holds one step per line. No new
// contract field is needed, so parsing and validation stay in one place.
func planGenerationRequest(request string) string {
	return "Planning mode: do not execute anything. Reply with intent=explain. " +
		"Put one concrete shell step per line in explanation, numbered 1., 2., ... " +
		"No prose before or after the list.\n\nTask: " + strings.TrimSpace(request)
}

// parsePlanSteps turns a numbered or bulleted explanation into plain steps.
func parsePlanSteps(text string) []string {
	var steps []string
	for _, line := range strings.Split(text, "\n") {
		step := strings.TrimSpace(planStepPrefix.ReplaceAllString(line, ""))
		if step == "" {
			continue
		}
		steps = append(steps, step)
		if len(steps) >= planMaxSteps {
			break
		}
	}
	return steps
}

// fillPlan replaces the current checklist with the generated steps. The old
// list belongs to the previous plan and must not leak into the new one.
func fillPlan(s *session, steps []string) ([]tools.TodoItem, error) {
	if s == nil || s.todos == nil {
		return nil, errors.New("todo store is not available")
	}
	s.todos.Clear()
	items := make([]tools.TodoItem, 0, len(steps))
	for _, step := range steps {
		item, err := s.todos.Add(step)
		if err != nil {
			return items, err
		}
		items = append(items, item)
	}
	return items, nil
}

// executePlan walks the open steps one at a time. Each step goes back through
// askStream and the normal command path, so policy, confirmation and audit
// behave exactly as for a hand-typed request.
func executePlan(ctx context.Context, s *session, rf *rootFlags, out, errW io.Writer) (executed, failed int, err error) {
	if s == nil || s.todos == nil {
		return 0, 0, errors.New("todo store is not available")
	}
	steps := s.todos.Items()
	open := 0
	for _, step := range steps {
		if !step.Done {
			open++
		}
	}
	for _, step := range steps {
		if step.Done {
			continue
		}
		fmt.Fprintf(out, "\n[plan %d/%d] %s\n", executed+failed+1, open, step.Task)
		resp, _, aerr := s.askStream(ctx, "run", "Execute this step: "+step.Task, out)
		if aerr != nil {
			failed++
			fmt.Fprintf(errW, "step %d failed: %v\n", step.ID, aerr)
			continue
		}
		if resp.Intent != prompt.IntentRunCommand || strings.TrimSpace(resp.Command) == "" {
			failed++
			fmt.Fprintf(errW, "step %d produced no command (%s)\n", step.ID, resp.Intent)
			continue
		}
		if cerr := runCommandWithCorrection(ctx, s, rf, resp, out, errW); cerr != nil {
			failed++
			fmt.Fprintf(errW, "step %d failed: %v\n", step.ID, cerr)
			continue
		}
		if !rf.cfg.DryRun {
			s.todos.Done(step.ID)
		}
		executed++
	}
	return executed, failed, nil
}
