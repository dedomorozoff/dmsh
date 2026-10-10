# AGENTS.md

## Project Context
- Project: `dmsh` (Direct Model Shell).
- Stack: Go + `llama.cpp` via CGO (no HTTP bridge), plus OpenAI-compatible
  providers (`ollama`, `pollinations`) selected by `provider`.
- Primary target: Cross-platform behavior and OS-native commands (Windows, macOS, Linux).

## Architecture Rules
- Keep LLM integration behind `internal/llm` interfaces: `Engine` for
  single-turn requests and `ToolEngine` (function calling) for providers that
  support tools. `llm.New` dispatches by provider; the local implementation
  stays behind the `llama` build tag.
- All outbound network access goes through one `netproxy.Settings`
  (`internal/netproxy`): LLM API, `websearch` and GGUF downloads. Modes are
  `auto` (environment), `off` (direct) and `custom` (protocol + host + port
  + login + password fields); a change of proxy or endpoint rebuilds the
  remote engines, never the local one. Credentials inside a proxy URL are
  redacted everywhere they are printed.
- Provider choice is resolved once per session in `internal/cli`
  (`resolveProvider`): `local`, `ollama`, `pollinations` or `auto`. `auto`
  means "local if a GGUF exists, otherwise remote" — never a silent retry
  after a local inference failure. `normalizeForProvider` then applies that
  provider's own defaults (model, no local model path): Ollama and
  Pollinations defaults must not leak into each other. The `setup` screen
  (`/setup`, Ctrl+P) is the only place that changes it mid-session, and it
  goes through `session.switchProvider`: the engine is rebuilt first and only
  then the config file is written, so a failure leaves the session working.
- Ollama and Pollinations speak the same OpenAI chat/completions dialect and
  share one engine (`internal/llm/openai.go`): they differ only in the base
  URL, the default model and the error hints. A base URL is a *base*, like in
  any OpenAI SDK; `routeURL` appends the route (`/chat/completions`,
  `/models`) and does not double it if the setting already holds the full
  endpoint.
- The model catalog (`llm.ListRemoteModels`) is fetched from the provider
  without the API key — both serve `/models` openly — and never mixed into
  the local .gguf list: `Ctrl+O` shows one or the other by the same rule
  as `/model` (`showsRemoteProvider`: auto with a local GGUF is local).
  A session-level model switch goes through
  `session.setRemoteModel` and is not written to disk; only the `setup`
  screen persists it.
- Remote providers may need an API key (`Params.APIKey` →
  `Authorization: Bearer`). Pollinations requires one for generation; Ollama
  never receives it, even if `POLLINATIONS_API_KEY` is set — `NewOllama`
  drops it. `config.Config.APIKey()` reads the environment first, so the
  secret need not be stored; the key is never printed, only its source.
- The status line carries `proxy:on|off` (`netproxy.Settings.Enabled`):
  whether the next request uses a proxy must be visible before the first
  connection error, not after.
- Maintain two build paths:
  - default/stub build (no CGO),
  - real llama build (`-tags llama`).
- JSON contract is mandatory between model and app logic.
- Safety policy layer must run before any command execution.

## TUI Rules
- Three modes, one cycle: `Shift+Tab` (ai → help → terminal), the command
  palette on `Ctrl+P`, `/mode` (opens the mode list) or `/mode ai|help|shell`.
  Numeric `/1`, `/2`, `/3` are gone; do not reintroduce them.
- `F1` (help) and `Ctrl+Q` (exit) are global: they must be handled before
  any state switch, otherwise they get swallowed by menus, search,
  confirmation and streaming. Inside the terminal F1 closes the session and
  says so — the transcript is not on screen there.
- Modal windows (`internal/cli/modal.go`): palette, settings, setup and the
  mode list are drawn over the finished frame; the palette keeps its query in
  the normal input line, and entries execute through `handleSlash` so the
  palette and slash commands cannot drift apart. Every new modal must be
  added to `modalCases()` in `modal_test.go`.
- Long lists (`paletteModal`, the `/` menu) must scroll with the selection
  through `scrollWindow`; a fixed head of the list plus a "(+N more)" hint is
  the bug this replaced.
- Terminal mode lives **inside the app window**: `executor.StartTerminal`
  returns a `TerminalSession` (read/write/resize/wait/close), `internal/cli`
  paints its output through `internal/vt` — a small screen emulator — and
  `Shift+Tab` closes the pty and returns to the prompt. Never hand the real
  terminal over with `tea.Exec` again: that threw away the whole window.
- Pseudo-terminals are platform-specific: `creack/pty` on unix,
  `UserExistsError/conpty` on Windows (ConPTY needs Windows 10 1809+).
  ConPTY `Close` must be called exactly once — `TerminalSession.Close` and
  `terminalPane.close` count the calls themselves; a double close corrupts
  the process heap.

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
  is entered deliberately (Shift+Tab, `/mode shell` or `/shell`), never
  implicitly, and text typed in it must never fall through to the model.
- Tools that need the user (`ask`) must degrade to a JSON answer instead of
  blocking: no input reader (TUI) or spent stdin (pipe) both mean
  `intent=ask_clarification`.

## Code Guidelines
- Small, testable packages with clear boundaries:
  - `internal/prompt` for schema/prompting,
  - `internal/policy` for risk rules,
  - `internal/executor` for command execution,
  - `internal/vt` for the terminal screen of the embedded shell,
  - `internal/tools` for tool schemas, read-only tools and the todo store,
  - `internal/cli` for interaction flow.
- Tool side effects that need session state (`run_command`, `ask`,
  `websearch`) are implemented in `internal/cli`; `internal/tools` owns the
  schemas and the safe implementations.
- `websearch` belongs to Pollinations only: its search model is hosted there.
  Ollama and the local GGUF must refuse it instead of silently answering from
  memory.
- `internal/executor` also owns the interactive path: `terminal.go` declares
  the shared surface, `terminal_unix.go` and `terminal_windows.go` implement
  it. `Run` buffers output; `StartTerminal` gives a caller the pty itself.
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
