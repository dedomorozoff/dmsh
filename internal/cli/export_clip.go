package cli

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// copyToClipboard копирует текст в буфер обмена.
// Поддерживает Windows (clip), macOS (pbcopy), Linux (xclip/xsel).
func copyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("clip")
	case "darwin":
		cmd = exec.Command("pbcopy")
	default:
		// Linux: пробуем xclip, затем xsel
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else if _, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.Command("xsel", "--clipboard", "--input")
		} else {
			return fmt.Errorf("clipboard not available (install xclip or xsel)")
		}
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
