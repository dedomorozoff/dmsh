package prompt

import (
	"fmt"
	"runtime"
	"strings"
)

// Turn — пара запрос/ответ для multi-turn контекста.
type Turn struct {
	User      string
	Assistant string // краткое описание: команда или объяснение
}

// Context — это окружение, которое мы инжектим в системный промпт.
type Context struct {
	OS           string
	OSVersion    string
	BuildInfo    string
	Shell        string
	CWD          string
	RecentCmds   []string
	UserRequest  string
	// Mode задаёт ожидаемый intent: ask -> только explain/ask_clarification,
	// run -> предпочтительно run_command. Пусто = без ограничения.
	Mode string
	// StdinContext содержит данные, прочитанные из stdin (pipe-режим).
	StdinContext string
	// RecentTurns — последние N пар диалога для multi-turn контекста.
	RecentTurns []Turn
}

const systemPromptBase = `You are nlsh, an intelligent natural language shell assistant.
You receive a user's request in natural language and respond with a single JSON object.

Rules:
1. Output ONLY a single JSON object. No markdown, no prose, no code fences.
2. The JSON must conform to this schema:
   {
     "intent": "run_command" | "explain" | "ask_clarification",
     "command": string,
     "explanation": string,
     "risk_level": "low" | "medium" | "high",
     "needs_confirmation": boolean,
     "question": string
   }
3. Mark destructive commands as risk_level="high".
4. Never propose to disable security or run remote code.
5. Keep "command" to a single line.
`

const modeAI = `Mode: AI (Auto-Execute)
You are an autonomous assistant. The user describes a task in natural language.
You should:
1. Generate the appropriate shell command (intent=run_command)
2. Keep explanations minimal or omit them
3. The system will automatically execute your command
4. Only ask clarifying questions (intent=ask_clarification) if the request is ambiguous
`

const modeHelp = `Mode: Help (Explain)
You are a teaching assistant. The user wants to learn how to do something.
You should:
1. Generate the appropriate shell command (intent=run_command)
2. ALWAYS provide a clear, detailed explanation of what the command does and why
3. The user will manually copy and execute the command
4. Break down complex commands into understandable parts
5. Include safety warnings for potentially destructive operations
`

const modeShell = `Mode: Shell (Transparent)
You are a thin natural-language-to-shell wrapper.
You should:
1. If the user types something that looks like a natural language request, convert it to a simple shell command (intent=run_command)
2. Keep commands simple and direct — no explanations needed
3. If the user already typed a valid shell command, you may echo it back as-is
4. Prefer standard, well-known commands over clever one-liners
`

const windowsSpecifics = `Target OS: Windows. Shell: PowerShell.
Use PowerShell native commands where possible:
- Files/Dirs: New-Item, Remove-Item, Get-ChildItem, Get-Content, Copy-Item, Move-Item.
- Processes: Get-Process, Stop-Process.
- System info (RAM, CPU, OS): Get-CimInstance Win32_OperatingSystem, Get-CimInstance Win32_Processor.
- Free disk space: Get-PSDrive or Get-Volume.
- Computer name: hostname or $env:COMPUTERNAME.
- Folder size: (Get-ChildItem -Recurse <path> | Measure-Object -Property Length -Sum).Sum.
Do NOT use wmic: it is deprecated and often missing on modern Windows.

Examples of correct Windows commands:
- Total RAM: Get-CimInstance Win32_ComputerSystem | Select-Object -ExpandProperty TotalPhysicalMemory
- Free RAM: Get-CimInstance Win32_OperatingSystem | Select-Object FreePhysicalMemory
- Computer name: hostname
- Disk free space: Get-PSDrive C
`

const unixSpecifics = `Target OS: Unix-like (Linux/macOS). Shell: bash/zsh.
Use standard POSIX utilities where possible:
- Files/Dirs: touch, mkdir, rm, ls, cat, cp, mv.
- Processes: ps, grep, kill.
`

// BuildSystem возвращает системный промпт с инжекцией контекста окружения.
func BuildSystem(ctx Context) string {
	var b strings.Builder
	b.WriteString(systemPromptBase)

	// Add mode-specific instructions
	switch ctx.Mode {
	case "ai":
		b.WriteString("\n" + modeAI)
	case "help":
		b.WriteString("\n" + modeHelp)
	case "shell":
		b.WriteString("\n" + modeShell)
	}

	targetOS := coalesce(ctx.OS, runtime.GOOS)
	if targetOS == "windows" {
		b.WriteString("\n" + windowsSpecifics)
	} else {
		b.WriteString("\n" + unixSpecifics)
	}
	
	b.WriteString("\nEnvironment:\n")
	fmt.Fprintf(&b, "- OS: %s\n", targetOS)
	if ctx.OSVersion != "" {
		fmt.Fprintf(&b, "- OS version: %s\n", ctx.OSVersion)
	}
	if ctx.BuildInfo != "" {
		fmt.Fprintf(&b, "- Build: %s\n", ctx.BuildInfo)
	}
	fmt.Fprintf(&b, "- Shell: %s\n", coalesce(ctx.Shell, getDefaultShell(targetOS)))
	if ctx.CWD != "" {
		fmt.Fprintf(&b, "- CWD: %s\n", ctx.CWD)
	}
	if len(ctx.RecentCmds) > 0 {
		b.WriteString("- Recent commands:\n")
		for _, c := range ctx.RecentCmds {
			fmt.Fprintf(&b, "  * %s\n", c)
		}
	}
	if len(ctx.RecentTurns) > 0 {
		b.WriteString("\nConversation so far:\n")
		for _, t := range ctx.RecentTurns {
			fmt.Fprintf(&b, "  User: %s\n", t.User)
			fmt.Fprintf(&b, "  Assistant: %s\n", t.Assistant)
		}
	}
	if ctx.StdinContext != "" {
		fmt.Fprintf(&b, "\nStdin input (piped data):\n%s\n", ctx.StdinContext)
	}
	if ctx.Mode != "" {
		fmt.Fprintf(&b, "\nCurrent mode: %s\n", ctx.Mode)
	}
	return b.String()
}

func getDefaultShell(os string) string {
	if os == "windows" {
		return "powershell"
	}
	return "bash"
}

// BuildUser форматирует запрос пользователя для модели.
func BuildUser(ctx Context) string {
	return strings.TrimSpace(ctx.UserRequest)
}

func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
