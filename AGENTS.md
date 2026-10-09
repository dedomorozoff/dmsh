# AGENTS.md

## Project Context
- Project: `dmsh` (Direct Model Shell).
- Stack: Go + `llama.cpp` via CGO (no HTTP bridge), plus an optional
  OpenAI-compatible remote provider (Pollinations) selected by `provider`.
- Primary target: Cross-platform behavior and OS-native commands (Windows, macOS, Linux).

## Architecture Rules
- Keep LLM integration behind `internal/llm` interfaces: `Engine` for
  single-turn requests and `ToolEngine` (function calling) for providers that
  support tools. `llm.New` dispatches by provider; the local implementation
  stays behind the `llama` build tag.
- Provider choice is resolved once per session in `internal/cli`
  (`resolveProvider`): `local`, `pollinations` or `auto`. `auto` means
  "local if a GGUF exists, otherwise remote" — never a silent retry after a
  local inference failure.
- Remote providers are anonymous: no tokens are read, stored or sent.
- Maintain two build paths:
  - default/stub build (no CGO),
  - real llama build (`-tags llama`).
- JSON contract is mandatory between model and app logic.
- Safety policy layer must run before any command execution.

## TUI Rules
- Three modes, one cycle: `Shift+Tab` (ai → help → terminal), the command
  palette on `Ctrl+P`, or `/mode ai|help|shell`. Numeric `/1`, `/2`, `/3`
  are gone; do not reintroduce them.
- `F1` (help) and `Ctrl+Q` (exit) are global: they must be handled before
  any state switch, otherwise they get swallowed by menus, search,
  confirmation and streaming.
- The command palette keeps its query in the normal input line and renders
  through `paletteRows`; entries execute through `handleSlash` so the
  palette and slash commands cannot drift apart.
- Terminal mode hands the terminal to the shell via `tea.Exec`, which
  releases and restores it. Interactive programs only work because of that
  handoff — do not replace it with plain `exec.Cmd` wiring.
- Pseudo-terminals are platform-specific: `creack/pty` on unix,
  `UserExistsError/conpty` on Windows (ConPTY needs Windows 10 1809+).
  ConPTY `Close` must be called exactly once: a double close corrupts the
  process heap.

## Safety Rules
- Never auto-execute high-risk commands.
- Default mode should remain safe (`dry-run` unless explicitly changed).
- Extend denylist/suspicious patterns conservatively with tests.
- `run_command` tool calls must reuse `policy.Evaluate` + allowlist +
  confirmation + audit log. In `dry-run` they are refused, and commands
  needing confirmation are refused when no input reader is attached (TUI),
  so the model returns them as JSON instead.
- In help mode `run_command` must be absent from the schemas
  (`tools.SpecsWithout`), not merely refused at call time: a tool the model
  can see will be called. `runCommandAllowed` re-checks the mode anyway,
  because it can change between two steps of the same dialog.
- Terminal mode bypasses the LLM entirely and is the user's own shell; it
  is entered deliberately (empty line or `/shell`), never implicitly.
- Tools that need the user (`ask`) must degrade to a JSON answer instead of
  blocking: no input reader (TUI) or spent stdin (pipe) both mean
  `intent=ask_clarification`.

## Code Guidelines
- Small, testable packages with clear boundaries:
  - `internal/prompt` for schema/prompting,
  - `internal/policy` for risk rules,
  - `internal/executor` for command execution,
  - `internal/tools` for tool schemas, read-only tools and the todo store,
  - `internal/cli` for interaction flow.
- Tool side effects that need session state (`run_command`, `ask`,
  `websearch`) are implemented in `internal/cli`; `internal/tools` owns the
  schemas and the safe implementations.
- `internal/executor` also owns the interactive path: `terminal.go` declares
  the shared surface, `terminal_unix.go` and `terminal_windows.go` implement
  it. `Run` buffers output; `RunTerminal` needs a real tty.
- Prefer explicit errors and deterministic behavior over "smart" hidden magic.
- Keep comments short and only where logic is non-obvious.

## Build and Test
- Stub path should always pass:
  - `go build ./...`
  - `go test ./...`
- Llama path should be documented and reproducible via `Makefile`.
- Keep `third_party/llama.cpp` pinned via submodule commit.

## Operational Notes
- Preserve backwards compatibility of CLI flags where possible.
- If changing JSON contract fields, update parser validation + tests together.
- If touching CGO layer, validate memory lifecycle (`New`/`Generate`/`Close`) and cancellation behavior.
