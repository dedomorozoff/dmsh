# Tasks — Batch 2

Status: 2026-11-22, all items closed.

- `[x]` 1. Stdin pipe — `oneshot.go` reads non-TTY stdin as context (8 KiB cap), unregisters the `ask` tool. `TestAskUnregisteredWhenStdinPiped`.
- `[x]` 2. `/retry` — works in TUI (`tui_slash.go`): re-submits the last request with an "alternative approach" hint. The unreachable line-based handler and its dead `retryPending`/`ErrRetry` state were removed.
- `[x]` 3. `/stats` — `showStats` + TUI `/stats`: requests/errors, commands, tool steps, TTFT, stream counters.
- `[x]` 4. `/export` — `handleExport` + `export_clip.go`: clipboard or `/export last > file`; ANSI stripped.
- `[x]` 5. `/alias` — create/list/delete, persisted in `cfg.Aliases`; `expandAlias` now expands a whole request before the prompt in every path (`askStream`). `TestExpandAlias`, `TestAskStreamUsesExpandedAlias`.
- `[x]` 6. Multi-turn контекст разговора — `prompt.Turn` / `RecentTurns` + `addTurn` / `loadTurns`, last 5 turns persisted per named session.
- `[x]` 7. Тесты и верификация — `go test ./... -count=1`, `go vet ./...`, targeted `go test -race`, CI matrix on Linux/macOS/Windows.
