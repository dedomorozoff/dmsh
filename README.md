# dmsh — Direct Model Shell

`dmsh` is a shell where you talk to your system in natural language.
A local LLM (GGUF via `llama.cpp`) is embedded directly into the binary
through CGO — no HTTP server, no external processes, no cloud.

If no local GGUF is available, dmsh can talk to the free
[Pollinations](https://pollinations.ai) text API instead (anonymous, no
token). See [Providers](#providers).

> Cross-platform: Linux, macOS, Windows.

## How it works

1. You type a request like `show all txt files in this dir`.
2. The model returns a strict JSON response (command + explanation + risk).
3. The safety policy layer checks the command (denylist + risk scoring:
   `rm -rf /`, `mkfs`, fork bombs, etc. are blocked outright;
   `sudo`, package changes, etc. require confirmation).
4. If approved, the command runs in your shell and the result is shown.

By default AI-suggested commands **are executed automatically** if they
pass the safety check and are rated low-risk. Pass `--dry-run` (or set
`dry_run` in the config) to preview commands without executing them.

For an extra review layer, pass `--preview`: before execution dmsh shows the
command with its options highlighted and the risk level, then asks for an
explicit y/N confirmation.

## Install

### Packages

Grab a package from [GitHub Releases](https://github.com/dedomorozoff/dmsh/releases):

- `dmsh-<ver>-amd64.deb` — Debian/Ubuntu
- `dmsh-<ver>-1-x86_64.pkg.tar.zst` — Arch Linux (`sudo pacman -U <file>`)
- `dmsh-<ver>-linux-amd64.tar.gz` — generic Linux
- Windows and macOS archives are also attached.

### Arch Linux (from source)

```bash
make dist-arch   # builds the package from the current tree and installs it
```

### From source

```bash
git clone --recurse-submodules https://github.com/dedomorozoff/dmsh.git
cd dmsh
make llama       # build llama.cpp static libs (~10-15 min)
make build       # build bin/dmsh
```

Requirements: Go 1.23+, C/C++ toolchain, CMake, Git.

## Quick start

```bash
dmsh model          # interactive wizard: pick and download a GGUF model
dmsh repl           # start interactive mode
```

Or run one-off requests:

```bash
dmsh ask "how do I find big files?"   # explain only, nothing executes
dmsh run "list png files"             # suggest a command, execute after review
dmsh "show last 20 lines of syslog"   # bare query = one-shot run
cat error.log | dmsh "what's wrong here?"   # stdin is passed to the model
```

Point at a specific model with `--model /path/to/model.gguf`.

## Models

Bare `dmsh model` opens an interactive download wizard. Subcommands:

| Command | Description |
|---------|-------------|
| `dmsh model list` | recommended + downloaded models |
| `dmsh model download [<n>/name/url]` | download from list or direct .gguf URL |
| `dmsh model use <name>` | set downloaded model as default |
| `dmsh model path [name]` | print path to a downloaded model |
| `dmsh pull [...]` | shortcut for `model download` |

Recommended models: Qwopus3.5-9B-coder (Q3/Q4/Q5), Qwen3 4B/8B,
Qwen3 1.7B, Qwen2.5 1.5B/0.5B, Llama 3.2 1B. The wizard shows the RAM
each option needs next to its name; without an explicit choice dmsh
falls back to the smallest model (Qwen2.5 0.5B).

Config and models live under:

| OS | Path |
|----|------|
| Linux | `~/.config/dmsh/` (`config.json`, `models/`) |
| macOS | `~/Library/Application Support/dmsh/` |
| Windows | `%AppData%\dmsh\` |

History is stored alongside (`history.jsonl`). Every executed command is also
logged to `audit.jsonl` (timestamp, command, source, risk, policy decision,
exit code) for accountability. Set `resume_session: true` in the config to
persist the multi-turn dialogue context across restarts.

## Providers

Inference source is chosen by the `provider` setting:

| Provider | Behaviour |
|----------|-----------|
| `auto` (default) | local GGUF if one is found, otherwise Pollinations |
| `local` | local GGUF only; fails with a hint if none is present |
| `pollinations` | Pollinations API; local models are ignored |

```bash
dmsh config set provider pollinations     # remote only
dmsh --provider auto "list big files"     # per-run override
dmsh --remote-model mistral "..."         # pick another remote model
dmsh --search-model gemini-search "..."   # model behind the websearch tool
dmsh --remote-base-url http://host:8080/v1 "..."   # any OpenAI-compatible endpoint
```

Pollinations is used anonymously: dmsh never sends API tokens and adds no
`Authorization` header. With `provider=auto` the fallback happens once, at
session start, and is reported in the output — a local model that fails to
load is never silently replaced.

`/model` (REPL) shows the active provider, model and endpoint.

## Tools (function calling)

Remote providers can call tools before answering. dmsh offers:

| Tool | What it does |
|------|--------------|
| `run_command` | run one shell command and return exit code, stdout and stderr |
| `read_file` | read a UTF-8 text file (truncated at 8 KB) |
| `list_dir` | list directory entries with size and mtime |
| `system_info` | OS, arch, CPU count, shell, cwd |
| `todo` | keep a checklist of the current task (`add` / `list` / `done` / `clear`) |
| `ask` | ask the user one focused question and wait for the answer |
| `websearch` | search the web through the provider's search model |

Notes:

- The loop is bounded (12 steps), and `run_command` goes through exactly the
  same path as a normal answer: denylist → allowlist → confirmation (unless
  `--yes`) → audit log. In `--dry-run` mode tools refuse to execute commands
  and the model is told to return the command in its JSON answer instead.
- `todo` lives in the session only (up to 50 items). Look at it with `/todo`,
  drop it with `/todo clear`.
- `ask` needs a terminal to read from: it works in the REPL and in one-shot
  mode. In the TUI there is no way to block mid-task, so the model is told to
  use `intent=ask_clarification` instead. When stdin is piped into dmsh it is
  already spent on the prompt, so `ask` is not offered at all.
- `websearch` issues a second request to the same endpoint with a search
  model (`gemini-search` by default, `--search-model` to change). It is only
  available for the remote provider.

Disable all tools with `--no-tools` or `dmsh config set tools false`.

## REPL

Start with `dmsh repl`. The interface is a full-screen Bubble Tea TUI
with scrollback, live streaming and a status line. Three modes:

- **AI** (default) — generates commands and executes them automatically
  after the safety check; anything non-trivial asks for confirmation.
- **Help** — shows command + explanation, you run it yourself. The
  `run_command` tool is not even advertised to the model in this mode.
- **Terminal** — a real shell session in a pseudo-terminal. Press Enter on
  an empty line (or run `/shell`) and dmsh hands the whole terminal over to
  your shell: colours, interactive programs and window size all work. Exit
  the shell to return.

Switch with `Shift+Tab` (cycles ai → help → terminal), the command palette
on `Ctrl+P`, or `/mode ai|help|shell`.

### Slash commands

| Command | Description |
|---------|-------------|
| `/help` | full help |
| `/model` | show the model currently in use |
| `/stats` | session statistics |
| `/export` | copy last command to clipboard or `/export last > file` |
| `/alias` | list aliases; `/alias name="request"`; `/alias -d name` |
| `/history` | show history |
| `/cd [path]` | change directory |
| `/pwd` | show current directory |
| `/clear` | clear screen |
| `/shell` | open a terminal session (also switches to terminal mode) |
| `/bind keys` | show keybindings |
| `/mode` | show/switch mode (`/mode ai|help|shell`) |
| `!command` | execute command directly |
| `/exit`, `/quit` | exit |

Plain words like `help`, `clear`, `pwd`, `history`, `exit`, `quit`,
`cd`, `which` work too, without the slash.

### Keybindings

| Key | Action |
|-----|--------|
| `F1` / `/help` | show help (works from any screen) |
| `Ctrl+P` | command palette: every command and mode, filter by typing |
| `Ctrl+Q` | exit |
| `Esc` / `Ctrl+C` | cancel / stop streaming |
| `Ctrl+A/E/U/K` | start/end/delete-to-start/delete-to-end of line |
| `Ctrl+R/S` | history search |
| `Ctrl+N` | next history entry |
| `Alt+B/F/D` | move / delete by word |
| `Ctrl+W` | delete word back |
| `Ctrl+L` | clear screen |
| `Ctrl+O` | model menu (install / switch model) |
| `Tab` | complete slash command |
| `Shift+Tab` | cycle modes: ai → help → terminal |
| `↑/↓` | previous / next command from history (persisted across sessions) |
| `PgUp/PgDn` | scroll output |
| `Enter` (empty line, terminal mode) | hand the terminal over to a real shell |

## Other commands

| Command | Description |
|---------|-------------|
| `dmsh info` | system info: OS, CPU, RAM, GPU + auto-tuned settings |
| `dmsh version` | version, build date, platform, build tags |
| `dmsh config show/set` | view and edit configuration (incl. custom `danger_patterns` / `suspicious_patterns`) |
| `dmsh history` | command history |
| `dmsh audit` | audit log of executed commands (add `--json` for raw lines) |

Common flags (all subcommands): `--model`, `--provider`, `--remote-model`,
`--remote-base-url`, `--no-tools`, `--threads`, `--ctx-size`,
`--gpu-layers`, `--max-tokens`, `--temperature`, `--top-p`, `--shell`,
`--dry-run`, `--preview`, `--yes`.

## Auto-detection

dmsh detects hardware on first run and tunes settings:

| Component | Method |
|-----------|--------|
| CPU cores | `runtime.NumCPU()` |
| RAM | WMI (Windows), `/proc/meminfo` (Linux), `sysctl` (macOS) |
| GPU | WMI (Windows), `nvidia-smi` then `lspci` (Linux), `system_profiler` (macOS) |

Default GPU layers: NVIDIA 32, AMD 16, Intel 8, Apple Silicon 32,
CPU-only 0. Override via flags or config file.

## Safety

The policy layer runs before any execution:

- **Denylist** — known-destructive patterns are hard-blocked with no
  override (`rm -rf /`, `mkfs`, `dd of=/dev/*`, fork bombs, piping curl
  into shell, disabling firewalls, registry/system tampering on
  Windows, ...).
- **Risk scoring** — suspicious patterns (`sudo`, package installs,
  `systemctl`, forced pushes, destructive docker ops, ...) raise the
  risk level and trigger a y/N confirmation.
- **Confirmation** — required whenever risk is above low or the model
  itself flags uncertainty. Low-risk commands may run without asking.
- **Allowlist** — add trusted commands to config (`allowlist: ["git status"]`)
  to always treat them as low-risk (no confirmation).
- **Audit log** — every executed command is written to `audit.jsonl` with its
  risk, policy decision and exit code; view it with `dmsh audit` or `/audit`.

You can extend both lists via config (`danger_patterns`,
`suspicious_patterns`). Blocked patterns also apply to commands the
model suggests as corrections.

Note: out of the box dmsh is *not* dry-run. If you want a strict
review-everything workflow, use Help mode or `--dry-run`; add `--preview`
for a highlighted review step or `--yes` to auto-approve in scripts.

## Building

```bash
make llama GPU=cuda    # CUDA support
make llama GPU=metal   # Metal (Apple Silicon)
make llama GPU=vulkan  # Vulkan
make build-stub        # no LLM (no CGO) — useful for CI
make build-all         # cross-compile binaries for all platforms
make dist-deb          # .deb package
make dist-rpm          # .rpm package
make dist-arch         # Arch package (builds and installs)
make dist-macos        # macOS archive
make dist-windows      # Windows archive
make dist-all          # everything except Arch
make test              # go test ./...
```

See `make help` for flags and details.

## Project structure

```
cmd/dmsh/               CLI entry point
internal/cli/           cobra commands, Bubble Tea TUI
internal/llm/           CGO wrapper over llama.cpp
internal/prompt/        system prompt + JSON contract
internal/policy/        safety gate (denylist + risk scoring)
internal/executor/      shell command execution
internal/feedback/      post-execution analysis of stdout/stderr/exit code
internal/config/        config loading/saving, hardware detection
internal/model/         model download/management
third_party/llama.cpp/  pinned llama.cpp submodule
```

## Status

Early beta. Safety gate errs on the side of blocking; review commands in
Help mode if you don't trust auto-execution yet.
