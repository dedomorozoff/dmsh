# nlsh — Natural Language Shell

`nlsh` is a shell where you talk to your system in natural language.
A local LLM (GGUF via `llama.cpp`) is embedded directly into the binary
through CGO — no HTTP server, no external processes, no cloud.

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

## Install

### Packages

Grab a package from [GitHub Releases](https://github.com/dedomorozoff/nlsh/releases):

- `nlsh-<ver>-amd64.deb` — Debian/Ubuntu
- `nlsh-<ver>-1-x86_64.pkg.tar.zst` — Arch Linux (`sudo pacman -U <file>`)
- `nlsh-<ver>-linux-amd64.tar.gz` — generic Linux
- Windows and macOS archives are also attached.

### Arch Linux (from source)

```bash
make dist-arch   # builds the package from the current tree and installs it
```

### From source

```bash
git clone --recurse-submodules https://github.com/dedomorozoff/nlsh.git
cd nlsh
make llama       # build llama.cpp static libs (~10-15 min)
make build       # build bin/nlsh
```

Requirements: Go 1.23+, C/C++ toolchain, CMake, Git.

## Quick start

```bash
nlsh model          # interactive wizard: pick and download a GGUF model
nlsh repl           # start interactive mode
```

Or run one-off requests:

```bash
nlsh ask "how do I find big files?"   # explain only, nothing executes
nlsh run "list png files"             # suggest a command, execute after review
nlsh "show last 20 lines of syslog"   # bare query = one-shot run
cat error.log | nlsh "what's wrong here?"   # stdin is passed to the model
```

Point at a specific model with `--model /path/to/model.gguf`.

## Models

Bare `nlsh model` opens an interactive download wizard. Subcommands:

| Command | Description |
|---------|-------------|
| `nlsh model list` | recommended + downloaded models |
| `nlsh model download [<n>/name/url]` | download from list or direct .gguf URL |
| `nlsh model use <name>` | set downloaded model as default |
| `nlsh model path [name]` | print path to a downloaded model |
| `nlsh pull [...]` | shortcut for `model download` |

Recommended models: Qwopus3.5-9B-coder (Q3/Q4/Q5), Qwen3 4B/8B,
Qwen3 1.7B, Qwen2.5 1.5B/0.5B, Llama 3.2 1B. The wizard shows the RAM
each option needs next to its name; without an explicit choice nlsh
falls back to the smallest model (Qwen2.5 0.5B).

Config and models live under:

| OS | Path |
|----|------|
| Linux | `~/.config/nlsh/` (`config.json`, `models/`) |
| macOS | `~/Library/Application Support/nlsh/` |
| Windows | `%AppData%\nlsh\` |

History is stored alongside (`history.jsonl`).

## REPL

Start with `nlsh repl`. Three modes:

- **AI** (default) — generates commands and executes them automatically
  after the safety check; anything non-trivial asks for confirmation.
- **Help** — shows command + explanation, you run it yourself.
- **Shell** — shell-like input passes straight to the OS; natural
  language is still understood and routed through the model.

Switch with `/mode ai|help|shell`, `/mode 1|2|3` or `/1`, `/2`, `/3`.

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
| `/bind keys` | show keybindings |
| `/mode` | show/switch mode |
| `!command` | execute command directly |
| `/exit`, `/quit` | exit |

Plain words like `help`, `clear`, `pwd`, `history`, `exit`, `quit`,
`cd`, `which` work too, without the slash.

Bash-like keybindings are supported: `Ctrl+A/E/U/K/L/R/S/P/N`, `Ctrl+W`,
`Alt+B/F/D`. `Ctrl+C` interrupts the current operation, `Ctrl+D` exits.

## Other commands

| Command | Description |
|---------|-------------|
| `nlsh info` | system info: OS, CPU, RAM, GPU + auto-tuned settings |
| `nlsh version` | version, build date, platform, build tags |
| `nlsh config show/set` | view and edit configuration (incl. custom `danger_patterns` / `suspicious_patterns`) |
| `nlsh history` | command history |

Common flags (all subcommands): `--model`, `--threads`, `--ctx-size`,
`--gpu-layers`, `--max-tokens`, `--temperature`, `--top-p`, `--shell`,
`--dry-run`.

## Auto-detection

nlsh detects hardware on first run and tunes settings:

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

You can extend both lists via config (`danger_patterns`,
`suspicious_patterns`). Blocked patterns also apply to commands the
model suggests as corrections.

Note: out of the box nlsh is *not* dry-run. If you want a strict
review-everything workflow, use Help mode or `--dry-run`.

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
cmd/nlsh/               CLI entry point
internal/cli/           cobra commands, REPL
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
