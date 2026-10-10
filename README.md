# dmsh — Direct Model Shell

`dmsh` is a shell where you talk to your system in natural language.
A local LLM (GGUF via `llama.cpp`) is embedded directly into the binary
through CGO — no HTTP server, no external processes, no cloud.

It can also talk to a local [Ollama](https://ollama.ai) server, or to the
[Pollinations](https://pollinations.ai) text API. See
[Providers](#providers).

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
| `ollama` | local [Ollama](https://ollama.ai) server over its OpenAI-compatible endpoint; no API key |
| `pollinations` | Pollinations API; needs an API key, local models are ignored |

```bash
dmsh config set provider ollama           # a local Ollama server
dmsh --provider ollama --remote-model qwen3:8b "list big files"
dmsh --remote-base-url http://host:8080/v1 "..."   # any OpenAI-compatible endpoint
dmsh config set provider pollinations     # remote only
dmsh --provider auto "list big files"     # per-run override
dmsh --remote-model openai/gpt-5.4-nano "..."       # any model from the catalog
dmsh --search-model gemini-search "..."   # model behind the websearch tool
```

### Ollama

`ollama serve` listens on `http://127.0.0.1:11434`; dmsh talks to its
OpenAI-compatible `/v1` endpoint, so no key is involved and nothing leaves
the machine. The model defaults to `qwen2.5-coder` — pick another with
`--remote-model` (the name must match `ollama list`, tags included):

```bash
ollama pull qwen2.5-coder
dmsh --provider ollama "find *.go files changed last week"
```

If the server is not running or the model is not pulled, the error says
exactly which of the two it is. Tool calling needs a model that supports it
(`ollama list` shows capabilities).

### Choosing a model

`Ctrl+O` (or `/models` from the palette) opens the model menu. With a
remote provider it shows that provider's live catalog instead of local
files:

- the list is fetched from the provider's `/models` route (no key needed —
  both Pollinations and Ollama serve it openly) and sorted by id;
- typing filters it (`qwen`, `coder`, `5.4`), `Backspace` edits the filter,
  `Esc` clears the filter and closes the window;
- `Enter` switches the session to the chosen model and rebuilds the engine.
  The choice is session-only — `Ctrl+P` → setup → **apply** writes it to
  `config.json` (the same rule the local menu follows);
- if the fetch fails, the error stays on screen: an unreachable Ollama and a
  missing key look very different.

For Pollinations this is the practical way to pick a model from its catalog
of several hundred entries without copying names by hand.

### Pollinations

The default endpoint is `https://gen.pollinations.ai/v1`. Model IDs follow
`publisher/model` (for example `openai/gpt-5.4-nano`); the model catalog is
at <https://gen.pollinations.ai/models>.

Generation requires an API key — take one at
<https://enter.pollinations.ai/keys>. dmsh reads it from the
`POLLINATIONS_API_KEY` environment variable first, and only then from
`remote_api_key` in `config.json`, so the secret does not have to live on
disk at all:

```bash
export POLLINATIONS_API_KEY=sk_...
dmsh --provider pollinations "list big files"
```

Without a key the request goes out anonymous and the API answers 401; dmsh
turns that into a message naming the variable and the page where the key is
issued. The key itself is never printed — only its source (`dmsh /model`,
`Ctrl+P` → setup).

With `provider=auto` the fallback happens once, at session start, and is
reported in the output — a local model that fails to load is never silently
replaced.

`/model` (REPL) shows the active provider, model and endpoint. In the TUI,
`Ctrl+P` → **setup** opens the same choice as a window: provider (`auto`,
`local`, `ollama`, `pollinations`), remote model, local GGUF, endpoint and
API key. `←/→` switches the provider, `Enter` edits a field, **apply**
rebuilds the engine for this session and writes `config.json`; a failed
switch keeps the engine that was already working. The status line shows
`proxy:on` / `proxy:off`, so it is visible before the first connection error
rather than after it.

## Proxy

Every outgoing request — LLM API, `websearch`, GGUF download — goes through
one proxy setting, chosen by `proxy_mode`:

| Mode | Behaviour |
|------|-----------|
| `auto` (default) | `HTTPS_PROXY` / `HTTP_PROXY` / `ALL_PROXY` / `NO_PROXY` from the environment, direct connection if unset |
| `off` | always direct, environment variables ignored |
| `custom` | the address from `proxy_proto` + `proxy_host` + `proxy_port` + `proxy_user` + `proxy_password` |

`proxy_proto` is `http`, `https`, `socks5` or `socks5h` (`socks5h` resolves
the host name at the proxy, so the local DNS is not needed at all). The port
is left to the protocol default when `proxy_port` is `0`.

`no_proxy` lists hosts that bypass the proxy, comma-separated: `localhost`,
`127.0.0.1`, `api.example.com:8443`, `.corp.example` (host and subdomains),
`10.0.0.0/8`.

```bash
# whole address at once — the same fields, filled from one string
dmsh --proxy socks5://127.0.0.1:1080 "list big files"
dmsh --proxy http://user:pass@proxy.corp:3128 "..."

# or field by field
dmsh --proxy-mode custom --proxy-proto socks5 --proxy-host 10.0.0.1 \
      --proxy-port 1080 --proxy-user dmsh --proxy-password secret "..."

dmsh config set proxy-host 10.0.0.1
dmsh config set proxy-port 1080
dmsh config set proxy-proto socks5
dmsh config set proxy-mode custom
```

In the TUI press `Ctrl+P` and pick **settings: proxy and endpoint**. The
window lists one field per row — mode, protocol, host, port, login,
password, bypass list, endpoint — so a typo in the protocol or the port is
visible immediately. `←/→` switches the mode and the protocol, `Enter` edits
a field, **test connection** sends one real request and reports the route,
and **save** applies the settings to the running session and writes
`config.json`. Edits stay local until you save, and the window always shows
whether the next request goes direct or through which proxy. The password is
shown as a mask everywhere except while typing it.

The old single `proxy_url` setting is still accepted: it is expanded into
the fields on load and replaced by them on the next save.

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
  available for the `pollinations` provider — that model is hosted there.

Disable all tools with `--no-tools` or `dmsh config set tools false`.

## REPL

Start with `dmsh repl`. The interface is a full-screen Bubble Tea TUI
with scrollback, live streaming and a status line. Three modes:

- **AI** (default) — generates commands and executes them automatically
  after the safety check; anything non-trivial asks for confirmation.
- **Help** — shows command + explanation, you run it yourself. The
  `run_command` tool is not even advertised to the model in this mode.
- **Terminal** — a real shell session in a pseudo-terminal, drawn **inside
  the app window**. Colours, interactive programs and window size all work,
  and dmsh keeps its status line: `Shift+Tab` closes the shell and returns to
  the prompt.

Switch with `Shift+Tab` (cycles ai → help → terminal), the command palette
on `Ctrl+P`, `/mode` (opens the mode list) or `/mode ai|help|shell`.

### Slash commands

| Command | Description |
|---------|-------------|
| `/help` | full help |
| `/model` | show the model currently in use |
| `/models` | model menu: local .gguf files (install / switch) or, with a remote provider, its live catalog |
| `/setup` | how dmsh connects to the LLM: provider (auto / local / pollinations), remote model, local GGUF, endpoint — pick one and it rebuilds the engine and writes `config.json` |
| `/settings`, `/proxy` | network settings window: proxy mode, protocol, host, port, login, password, bypass list, endpoint, connection test |
| `/stats` | session statistics |
| `/export` | copy last command to clipboard or `/export last > file` |
| `/alias` | list aliases; `/alias name="request"`; `/alias -d name` |
| `/history` | show history |
| `/cd [path]` | change directory |
| `/pwd` | show current directory |
| `/clear` | clear screen |
| `/shell` | open a shell in this window (also switches to terminal mode) |
| `/bind keys` | show keybindings |
| `/mode` | open the mode list and pick one (`/mode ai|help|shell`) |
| `!command` | execute command directly |
| `/exit`, `/quit` | exit |

Plain words like `help`, `clear`, `pwd`, `history`, `exit`, `quit`,
`cd`, `which` work too, without the slash.

### Keybindings

| Key | Action |
|-----|--------|
| `F1` / `/help` | show help (works from any screen) |
| `Ctrl+P` | command palette as a modal window: every command, mode, the setup and the settings screens, filter by typing |
| `Ctrl+Q` | exit |
| `Esc` / `Ctrl+C` | cancel / stop streaming |
| `Ctrl+A/E/U/K` | start/end/delete-to-start/delete-to-end of line |
| `Ctrl+R/S` | history search |
| `Ctrl+N` | next history entry |
| `Alt+B/F/D` | move / delete by word |
| `Ctrl+W` | delete word back |
| `Ctrl+L` | clear screen |
| `Ctrl+O` | model menu: local .gguf files (install / switch) or, with a remote provider, its live model catalog |
| `Tab` | complete slash command |
| `Shift+Tab` | cycle modes: ai → help → terminal |
| `↑/↓` | previous / next command from history (persisted across sessions) |
| `PgUp/PgDn` | scroll output |
| any key (terminal mode) | goes to the shell; `Shift+Tab` returns to the prompt |

## Other commands

| Command | Description |
|---------|-------------|
| `dmsh info` | system info: OS, CPU, RAM, GPU + auto-tuned settings |
| `dmsh version` | version, build date, platform, build tags |
| `dmsh config show/set` | view and edit configuration (incl. custom `danger_patterns` / `suspicious_patterns`) |
| `dmsh history` | command history |
| `dmsh audit` | audit log of executed commands (add `--json` for raw lines) |

Common flags (all subcommands): `--model`, `--provider`, `--remote-model`,
`--remote-base-url`, `--no-tools`, `--proxy-mode`, `--proxy`,
`--proxy-proto`, `--proxy-host`, `--proxy-port`, `--proxy-user`,
`--proxy-password`, `--no-proxy`, `--threads`, `--ctx-size`,
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
internal/llm/           llama.cpp (CGO) + OpenAI-compatible remote engines
internal/prompt/        system prompt + JSON contract
internal/policy/        safety gate (denylist + risk scoring)
internal/executor/      shell command execution + pseudo-terminal sessions
internal/vt/            minimal terminal screen for the embedded shell
internal/feedback/      post-execution analysis of stdout/stderr/exit code
internal/config/        config loading/saving, hardware detection
internal/model/         model download/management
internal/netproxy/      one proxy setting for every outgoing request
third_party/llama.cpp/  pinned llama.cpp submodule
```

## Status

Early beta. Safety gate errs on the side of blocking; review commands in
Help mode if you don't trust auto-execution yet.
