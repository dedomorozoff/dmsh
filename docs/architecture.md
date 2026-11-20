# dmsh Architecture

This document describes the high-level runtime flow and main components of dmsh.

## Overview

`dmsh` is a Go CLI/TUI that turns natural-language requests into structured model responses and (when allowed) shell actions. It supports:

- local inference via `llama.cpp` (CGO build tag `llama`)
- OpenAI-compatible remote providers (Ollama, Pollinations)
- an embedded terminal mode inside the TUI (PTY + in-app VT rendering)

## High-level flow

```text
User input (CLI arg / REPL/TUI / stdin)
        │
        ▼
CLI / session (internal/cli)
  - mode/provider/model resolution
  - prompt context assembly
        │
        ▼
Prompt & JSON contract (internal/prompt)
  - system/user prompt construction
  - strict JSON schema expectations and parsing/repair handling
        │
        ▼
LLM engine (internal/llm)
  - Engine / ToolEngine interfaces
  - local (llama.cpp, build-tagged) or remote OpenAI-compatible engines
  - streaming and tool-calling (when supported/enabled)
        │
        ▼
Tool dispatch / command proposal (internal/cli, internal/tools)
  - tool schemas and safe tool implementations
  - session-owned side-effecting tools (run_command, ask, websearch, etc.)
        │
        ▼
Policy & confirmation (internal/policy, internal/cli)
  - deny/suspicious patterns, risk classification
  - allowlist / dry-run / preview / explicit confirmation rules
        │
        ▼
Executor (internal/executor)
  - buffered command execution
  - interactive PTY terminal session for shell mode (internal/vt renders output)
        │
        ▼
Audit, history, feedback (internal/cli, internal/feedback)
  - command/history/audit persistence (private permissions)
  - post-execution analysis/hints
  - user-visible output / JSON result
```

## Key components

### `cmd/dmsh`
- Process entrypoint.
- CLI JSON error envelope, exit-code/kind/hint classification (via `internal/cli` helpers).

### `internal/cli`
- Cobra commands (`ask`, `run`, `repl`/interactive, `model`, `config`, `doctor`, completions, etc.).
- Session lifecycle: provider/model/proxy resolution, engine lifecycle, history/audit/turns, streaming, tool loop.
- TUI (Bubble Tea): modes (AI/help/terminal), modals/palette, search/copy, mouse toggle, confirmations, rendering and input handling.
- Debug logging (`--debug`) and stats.

### `internal/prompt`
- Prompt construction and JSON response contract.
- Parsing/validation/repair-oriented handling for model outputs.

### `internal/llm`
- Common interfaces: `Engine`, `ToolEngine`.
- Provider selection and implementations:
  - local llama.cpp engine (behind `llama` build tag)
  - shared OpenAI-compatible chat/completions engine for Ollama and Pollinations (different defaults/endpoints/error hints)
- Remote model listing and request/stream handling.

### `internal/tools`
- Tool schemas and safe/read-only tool implementations.
- Registry and restrictions (e.g. working roots).
- Session/UI-specific side effects (like `run_command`, `ask`, `websearch`) are enforced/coordinated by CLI/session code.

### `internal/policy`
- Safety evaluation before command execution.
- Denylist/suspicious/risk rules, allowlist-aware decisions.
- Used consistently for direct execution and tool-driven command execution.

### `internal/executor`
- Command execution (`Run`) and interactive terminal sessions (`StartTerminal`).
- Shell wrapping/unwrapping helpers and platform-specific PTY implementations (Unix/Windows ConPTY).

### `internal/vt`
- Minimal terminal screen/emulation layer used to render embedded shell output inside the TUI.

### `internal/config`
- Config load/save/validate/migrate (`config_version`), defaults, path resolution, provider/model/proxy settings.
- Secret handling helpers (masking) and environment-based key resolution.

### `internal/netproxy`
- Single proxy settings abstraction (`auto`/`off`/`custom`) applied to outbound HTTP clients (LLM, websearch, downloads).
- URL/address normalization and redaction helpers.

### `internal/model`
- Local GGUF model discovery/download/verify/remove/prune helpers.

### `internal/feedback`
- Post-execution stdout/stderr/exit-code analysis and user-facing hints.

### `internal/redact`
- Centralized redaction for URL credentials, bearer tokens, common secret assignments, and configured secret literals.

## Build modes

- **Stub/default build**: compiles without CGO/llama; local inference unavailable, remote providers still usable.
- **`-tags llama` build**: enables embedded local llama.cpp inference.

## Security & privacy boundaries

- Policy checks gate command execution; dry-run/confirmation flows prevent unintended execution in restricted contexts.
- Audit/history/config files are written with private permissions where applicable.
- Secrets should be redacted in persisted logs, diagnostics, and user-visible error paths.
- Terminal mode is an explicit user shell session inside the app window and bypasses the LLM, not the OS shell.
