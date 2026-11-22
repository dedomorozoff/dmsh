# Changelog

All notable changes to this project should be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project aims to follow [Conventional Commits](https://www.conventionalcommits.org/) for commit messages.

## [Unreleased]

### Added
- `--debug` / `--debug-file` options for redacted debug logging (request timings, provider/tool diagnostics).
- Extended session statistics (`/stats`): request errors, tool steps, TTFT, request/stream timing and stream counters.
- Config schema versioning with version-dispatch migrations (`config_version`): legacy `proxy_url` is normalized on load and future versions are rejected explicitly.
- Unified CLI error hint/kind/exit-code classification helpers for more consistent diagnostics.
- Centralized secret redaction utilities used for audit/history and diagnostics paths.
- Project documentation: `CONTRIBUTING.md`, `SECURITY.md`, `docs/architecture.md`.
- Release integrity gate: every release now ships verified `SHA256SUMS`, a GPG signature (`SHA256SUMS.asc`) and a CycloneDX SBOM; the publish step fails without them. Reproducible stub build via `make build-stub-repro`.
- Shared diagnostics wording (`internal/diag`) used by both provider errors and CLI hints, so one cause has one phrase.

### Changed
- TUI and session internals refactored into smaller modules for maintainability (`tui_*`, `session_*`).
- Doctor diagnostics enriched with hints and redacted sensitive details while preserving JSON check output.
- CLI/provider error hints improved for missing model/build, proxy issues, auth failures, Ollama availability, and network errors.

### Fixed
- Ctrl+W / Alt+Backspace now actually deletes the previous word in the TUI input; the old helper rebuilt the same slice.
- `/alias` entries now expand before the prompt in one-shot and TUI requests; removed unreachable `/retry` state from the line-based handler and fixed the stale `--session` help.
- Aligned README and REPL help with the safe dry-run default; the refusal message now points to `--dry-run=false` and first runs get a one-time notice about the compatibility flag.
- Redacted secrets in provider notices, setup/model screens and download output; URL credentials are masked at the endpoint source.
- Prevented configured secrets and URL credentials from being persisted in history/audit entries.
- Redacted credentials in proxy-related error messages and doctor details.

## [0.8.x] - recent work (summary)

### Added
- Model management improvements (verify/remove/prune, catalog handling), transcript search/copy in TUI, shell completions, and CLI JSON output modes for key commands.
- Provider/setup improvements, proxy settings plumbing, and doctor diagnostics.

### Changed
- TUI rendering/performance and interaction improvements (modal flows, action bar behavior, mouse toggle, command handling refinements).

### Fixed
- Background command/output handling in TUI, command wrapping/truncation issues, and platform-specific test/build stability fixes.

> Note: This changelog is being normalized; older historical entries may be incomplete. See git history for detailed per-commit changes.
