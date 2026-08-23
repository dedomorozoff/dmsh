# AGENTS.md

## Project Context
- Project: `dmsh` (Direct Model Shell).
- Stack: Go + `llama.cpp` via CGO (no HTTP bridge).
- Primary target: Cross-platform behavior and OS-native commands (Windows, macOS, Linux).

## Architecture Rules
- Keep LLM integration behind `internal/llm` interface (`Engine`).
- Maintain two build paths:
  - default/stub build (no CGO),
  - real llama build (`-tags llama`).
- JSON contract is mandatory between model and app logic.
- Safety policy layer must run before any command execution.

## Safety Rules
- Never auto-execute high-risk commands.
- Default mode should remain safe (`dry-run` unless explicitly changed).
- Extend denylist/suspicious patterns conservatively with tests.

## Code Guidelines
- Small, testable packages with clear boundaries:
  - `internal/prompt` for schema/prompting,
  - `internal/policy` for risk rules,
  - `internal/executor` for command execution,
  - `internal/cli` for interaction flow.
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
