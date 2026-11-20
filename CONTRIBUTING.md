# Contributing to dmsh

Thanks for your interest in improving dmsh.

## Prerequisites

- Go 1.23+
- Git
- For the local llama.cpp build path: C/C++ toolchain and CMake
- Optional: `golangci-lint` for lint checks

Clone with submodules when working on the local inference build:

```bash
git clone --recurse-submodules https://github.com/dedomorozoff/dmsh.git
cd dmsh
```

## Build & test

Stub path (no CGO / no local llama.cpp runtime) — should always work:

```bash
go build ./...
go test ./...
```

Local llama.cpp path:

```bash
make llama
make build
# or
go build -tags llama ./cmd/dmsh
```

Useful targets:

```bash
make help
make test
make lint
make fmt
```

## Development guidelines

- Follow the architecture and safety rules in `AGENTS.md`.
- Keep LLM integration behind `internal/llm` interfaces (`Engine`, `ToolEngine`); provider-specific behavior belongs in provider implementations.
- Route outbound network access (LLM API, websearch, GGUF downloads) through `internal/netproxy` settings.
- Keep the JSON contract between model and app logic strict; update prompt/schema parsing and tests together when it changes.
- Run all command execution through the policy/safety path (`internal/policy`, confirmation/allowlist/audit flow). Do not bypass it for tool calls.
- Preserve TUI behavior constraints (modal handling, terminal-in-window behavior, platform-specific PTY differences).
- Redact secrets (API keys, proxy credentials, URL userinfo, bearer tokens) in logs, audit/history, diagnostics, and errors.
- Prefer small, testable packages and explicit, deterministic behavior.

## Code style

- Use `gofmt`/`go fmt ./...`.
- Keep comments short and only where logic is non-obvious.
- Follow existing naming and structure in `internal/*` packages.
- When changing CGO/llama code, validate memory lifecycle and cancellation behavior.

## Tests

- Add or update tests with behavior changes.
- Run at least `go build ./...` and `go test ./...` before submitting.
- For platform-sensitive changes (PTY/terminal, paths, permissions), verify on the relevant OS when possible.
- CI/build paths should remain reproducible.

## Submitting changes

1. Create a focused branch/commit set.
2. Ensure build/tests pass.
3. Update docs (`README.md`, `docs/`, `CHANGELOG.md`) when user-visible behavior, flags, config, or contracts change.
4. Open a pull request with:
   - what changed and why
   - how it was tested
   - any migration/config or platform notes

## Reporting security issues

Please do **not** open public issues for suspected vulnerabilities. See `SECURITY.md` for reporting instructions.
