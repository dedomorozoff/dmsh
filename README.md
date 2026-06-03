# nlsh — Natural Language Shell

`nlsh` is a shell where you can communicate with the system in natural language.
A local lightweight LLM (GGUF via `llama.cpp`) is embedded directly into the binary
via CGO — no HTTP server and no external processes.

> Cross-platform (Windows, macOS, Linux).

## Features (MVP)

- `nlsh ask "..."` — explain what and how to do, without executing anything.
- `nlsh run "..."` — suggest a shell command and execute it after confirmation.
- `nlsh repl` — interactive mode with history and bash-like keybindings.
- `nlsh info` — show system information and auto-detected GPU/CPU settings.
- Hard JSON contract for model responses.
- Safety policy gate against dangerous commands (`rm -rf /`, `mkfs`, fork bombs, etc.).

## Auto-Detection

nlsh automatically detects your system capabilities:

| Component | Detection Method |
|-----------|------------------|
| CPU Cores | `runtime.NumCPU()` |
| RAM | OS-specific (WMI on Windows, `/proc/meminfo` on Linux, `sysctl` on macOS) |
| GPU | `nvidia-smi` (NVIDIA), `lspci` (AMD/Intel), `system_profiler` (macOS) |

GPU layers are automatically set based on detected GPU:
- NVIDIA: 32 layers
- AMD: 16 layers  
- Intel: 8 layers
- Apple Silicon: 32 layers
- CPU only: 0 layers

You can override these values via CLI flags or config file.

## REPL Features

### Bash-like Keybindings (via readline)

| Keybinding | Action |
|------------|--------|
| `Ctrl+A` | move to beginning of line |
| `Ctrl+E` | move to end of line |
| `Ctrl+U` | delete to beginning of line |
| `Ctrl+K` | delete to end of line |
| `Ctrl+L` | clear screen |
| `Ctrl+R` | reverse history search |
| `Ctrl+S` | forward history search |
| `Ctrl+P` | previous command |
| `Ctrl+N` | next command |
| `Alt+B` | backward by word |
| `Alt+F` | forward by word |
| `Alt+D` | delete word forward |
| `Ctrl+W` | delete word backward |

### Special Keys

| Key | Action |
|-----|--------|
| `Ctrl+C` | interrupt current operation (does not exit REPL) |
| `Ctrl+D` | exit REPL (EOF) |

### Slash Commands

| Command | Description |
|---------|-------------|
| `/help` | show help |
| `/exit` | exit REPL |
| `/cd [path]` | change directory |
| `/clear` | clear screen |
| `/pwd` | show current directory |
| `/history` | show history |
| `/bind keys` | show keybindings list |
| `!command` | execute command directly |

## Requirements

- Go 1.22+
- C/C++ toolchain with submodules
- GGUF-model (Qwen2.5-3B-Instruct, Llama-3.2-3B-Instruct, Phi-3.5-mini)

## Building

### Quick Start

```bash
git submodule update --init --recursive
make llama       # build static libllama
make build       # build nlsh binary
```

### Build for All Platforms (Local Release)

To build binaries for all platforms locally (useful when GitHub Actions billing is exhausted):

```bash
make build-all   # builds Windows, Linux, macOS (amd64 & arm64)
```

This creates:
- `bin/nlsh-windows-amd64.exe`
- `bin/nlsh-linux-amd64`
- `bin/nlsh-macos-amd64`
- `bin/nlsh-macos-arm64`

### GPU Acceleration

```bash
make llama GPU=cuda    # build with CUDA support
make llama GPU=metal   # build with Metal support (Apple Silicon)
make llama GPU=vulkan  # build with Vulkan support
```

### Build Stub (No LLM)

```bash
make build-stub      # build without llama.cpp (no LLM support)
```

Details on flags/speedups — in `Makefile` (`make help`).

## Usage

```bash
nlsh --model /path/to/model.gguf repl
```

## Project Structure

```
cmd/nlsh/            CLI entry point
internal/cli/        cobra commands (ask, run, repl)
internal/llm/        CGO wrapper over llama.cpp
internal/prompt/     system prompt + JSON response contract
internal/policy/     safety gate (denylist + risk scoring)
internal/executor/   shell command execution
internal/config/     config loading
third_party/llama.cpp/  submodule with llama.cpp
```

## Status

Early Beta.
