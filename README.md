# nlsh — Natural Language Shell

`nlsh` is a shell where you talk to your system in natural language.
A local LLM (GGUF via `llama.cpp`) is embedded directly into the binary
through CGO — no HTTP server, no external processes, no cloud.

> Cross-platform: Linux, macOS, Windows.

## How it works

1. You type a request like `show all txt files in this dir`.
2. The model returns a strict JSON response (command + explanation).
3. The safety policy layer checks the command (denylist + risk scoring:
   `rm -rf /`, `mkfs`, fork bombs, etc. are blocked).
4. The command runs in your shell and the result is shown.

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
nlsh model wizard    # pick and download a GGUF model (interactive)
nlsh repl            # start interactive mode
```

Or run one-off requests:

```bash
nlsh ask "how do I find big files?"     # explain only, nothing executes
nlsh run "list png files"               # suggest a command, run after confirm
```

Point at a specific model with `--model /path/to/model.gguf`.

## Models

`nlsh model` opens an interactive wizard; subcommands:

| Command | Description |
|---------|-------------|
| `nlsh model list` | recommended + downloaded models |
| `nlsh model download [<n>/name/url]` | download from list or direct .gguf URL |
| `nlsh model use <name>` | set downloaded model as default |
| `nlsh model path <name>` | print path to a downloaded model |
| `nlsh pull [...]` | shortcut for `model download` |

Recommended models include Qwopus3.5-9B-coder (Q3/Q4/Q5), Qwen3 4B/8B,
Qwen3 1.7B, Qwen2.5 1.5B/0.5B, Llama 3.2 1B. nlsh suggests one based on
your RAM. Config and downloaded models live under
`~/.config/nlsh/` (`config.json`, `models/`) on Linux.

## REPL

Start with `nlsh repl`. Three modes:

- **AI** (default) — generates and executes commands automatically.
- **Help** — shows command + explanation, you run it yourself.
- **Shell** — direct execution, natural language still understood.

Switch with `/mode ai|help|shell` or `/1`, `/2`, `/3`.

### Slash commands

| Command | Description |
|---------|-------------|
| `/help` | full help |
| `/model` | show the model currently in use |
| `/stats` | session statistics |
| `/retry` | re-run last request with alternate approach |
| `/export` | copy last command to clipboard or `/export last > file` |
| `/alias` | list aliases; `/alias name="request"`; `/alias -d name` |
| `/history` | show history |
| `/cd [path]` | change directory |
| `/pwd` | show current directory |
| `/clear` | clear screen |
| `/bind keys` | show keybindings |
| `/mode` | show/switch mode |
| `!command` | execute command directly |
| `/exit` | exit |

Bash-like keybindings are supported: `Ctrl+A/E/U/K/L/R/S/P/N`, `Ctrl+W`,
`Alt+B/F/D`. `Ctrl+C` interrupts the current operation, `Ctrl+D` exits.

## Other commands

| Command | Description |
|---------|-------------|
| `nlsh info` | system info: OS, CPU, RAM, GPU + auto-tuned settings |
| `nlsh version` | version, build date, platform, build tags |
| `nlsh config show/set` | view and edit configuration |
| `nlsh history` | command history |

## Auto-detection

nlsh detects hardware on first run and tunes settings:

| Component | Method |
|-----------|--------|
| CPU cores | `runtime.NumCPU()` |
| RAM | WMI (Windows), `/proc/meminfo` (Linux), `sysctl` (macOS) |
| GPU | `nvidia-smi`, `lspci`, `system_profiler` |

Default GPU layers: NVIDIA 32, AMD 16, Intel 8, Apple Silicon 32,
CPU-only 0. Override via flags or config file.

## Building

```bash
make llama GPU=cuda    # CUDA support
make llama GPU=metal   # Metal (Apple Silicon)
make llama GPU=vulkan  # Vulkan
make build-stub        # no LLM (no CGO) — useful for CI
make build-all         # cross-compile binaries for all platforms
make dist-deb          # .deb package
make dist-arch         # Arch package (builds and installs)
make dist-all          # everything
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
internal/config/        config loading/saving
internal/model/         model download/management
third_party/llama.cpp/  pinned llama.cpp submodule
```

## Status

Early beta. Safety gate errs on the side of blocking; review commands in
Help mode if you don't trust auto-execution yet.
