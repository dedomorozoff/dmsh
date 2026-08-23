package cli

import (
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strings"
)

func osName() string {
	switch runtime.GOOS {
	case "linux":
		return "Linux"
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	default:
		return runtime.GOOS
	}
}

// osVersion возвращает детальную версию ОС для промпта модели.
func osVersion() string {
	switch runtime.GOOS {
	case "windows":
		out, err := exec.Command("cmd", "/c", "ver").Output()
		if err != nil {
			return "Windows"
		}
		return strings.TrimSpace(string(out))
	case "linux":
		data, err := os.ReadFile("/etc/os-release")
		if err != nil {
			return "Linux"
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
			}
		}
		return "Linux"
	case "darwin":
		out, err := exec.Command("sw_vers", "-productVersion").Output()
		if err != nil {
			return "macOS"
		}
		return "macOS " + strings.TrimSpace(string(out))
	default:
		return osName()
	}
}

// buildInfo возвращает информацию о сборке бинарника для промпта модели.
func buildInfo() string {
	var b strings.Builder
	b.WriteString("dmsh " + Version)
	b.WriteString("; " + runtime.Version())
	if d := strings.TrimSpace(BuildDate); d != "" {
		b.WriteString("; built=" + d)
	}
	b.WriteString("; GOOS=" + runtime.GOOS)
	b.WriteString("; GOARCH=" + runtime.GOARCH)
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "-tags" {
				b.WriteString("; tags=" + s.Value)
			}
		}
	}
	return b.String()
}
