package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Mode определяет режим работы nlsh.
type Mode string

const (
	ModeAI       Mode = "ai"       // Полная автономия, команда выполняется автоматически
	ModeHelp     Mode = "help"     // Генерация команд + объяснения, пользователь выполняет вручную
	ModeShell    Mode = "shell"    // Прозрачный проход через оболочку
)

// Config описывает рантайм-настройки nlsh. Поля сознательно плоские,
// чтобы их легко было пробрасывать из флагов CLI и из JSON-файла.
type Config struct {
	ModelPath   string  `json:"model_path"`
	DefaultModel string `json:"default_model"`
	Threads     int     `json:"threads"`
	CtxSize     int     `json:"ctx_size"`
	GPULayers   int     `json:"gpu_layers"`
	MaxTokens   int     `json:"max_tokens"`
	Temperature float32 `json:"temperature"`
	TopP        float32 `json:"top_p"`
	Shell       string  `json:"shell"`
	HistoryFile        string   `json:"history_file"`
	DryRun             bool     `json:"dry_run"`
	Mode               Mode     `json:"mode"`
	DangerPatterns     []string          `json:"danger_patterns,omitempty"`
	SuspiciousPatterns []string          `json:"suspicious_patterns,omitempty"`
	Aliases            map[string]string `json:"aliases,omitempty"`
}

// HardwareInfo содержит информацию о возможностях системы.
type HardwareInfo struct {
	CPUCores    int
	RAMGB       int
	GPULayers   int
	GPUName     string
	HasGPU      bool
	GPUType     string // "cpu", "nvidia", "amd", "apple", "intel"
}

// DetectHardware определяет возможности системы.
func DetectHardware() HardwareInfo {
	hw := HardwareInfo{
		CPUCores: runtime.NumCPU(),
		RAMGB:    DetectRAMGB(),
	}

	// Detect GPU
	hw.GPUType, hw.GPUName, hw.GPULayers = detectGPU()
	hw.HasGPU = hw.GPUType != "cpu"

	return hw
}

// Default возвращает дефолтную конфигурацию. Значения подобраны под
// маленькие instruct-модели 3B-8B в Q4_K_M и слабое железо.
func Default() Config {
	hw := DetectHardware()

	// Auto-tune based on hardware
	gpuLayers := 0
	if hw.HasGPU {
		// Use GPU if available
		gpuLayers = hw.GPULayers
	}

	return Config{
		Threads:     hw.CPUCores,
		CtxSize:     4096,
		GPULayers:   gpuLayers,
		MaxTokens:   512,
		Temperature: 0.2,
		TopP:        0.9,
		Shell:       defaultShell(),
		HistoryFile: defaultHistoryFile(),
		DryRun:      false,
		Mode:        ModeAI,
	}
}

// Load читает конфиг из ~/.config/nlsh/config.json, если он есть, и
// мерджит его поверх дефолтов. Отсутствие файла — не ошибка.
func Load() (Config, error) {
	cfg := Default()
	path, err := userConfigPath()
	if err != nil {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if len(data) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}

	// Auto-update GPU layers if not set (0) and GPU is available
	hw := DetectHardware()
	if cfg.GPULayers == 0 && hw.HasGPU {
		cfg.GPULayers = hw.GPULayers
	}

	return cfg, nil
}

// Save сохраняет конфигурацию в ~/.config/nlsh/config.json
func Save(cfg Config) error {
	path, err := userConfigPath()
	if err != nil {
		return fmt.Errorf("config path: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func userConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "nlsh", "config.json"), nil
}

func defaultShell() string {
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	return "/bin/sh"
}

func defaultHistoryFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "nlsh", "history.jsonl")
}

// DetectRAMGB определяет объем RAM в гигабайтах.
func DetectRAMGB() int {
	if runtime.GOOS == "windows" {
		return detectWindowsRAM()
	}
	if runtime.GOOS == "darwin" {
		return detectDarwinRAM()
	}
	return detectLinuxRAM()
}

// detectGPU определяет GPU и количество слоев для GPU inference.
func detectGPU() (gpuType, gpuName string, gpuLayers int) {
	if runtime.GOOS == "windows" {
		return detectWindowsGPU()
	}
	if runtime.GOOS == "darwin" {
		return detectDarwinGPU()
	}
	return detectLinuxGPU()
}

// detectWindowsGPU определяет GPU на Windows.
func detectWindowsGPU() (gpuType, gpuName string, gpuLayers int) {
	// Try WMI for NVIDIA GPU
	output, err := executeCommand("powershell", "-Command", "Get-WmiObject Win32_VideoController | Select-Object -ExpandProperty Name")
	if err == nil && output != "" {
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			line = strings.ToLower(line)
			if strings.Contains(line, "nvidia") {
				return "nvidia", strings.TrimSpace(line), 32
			}
			if strings.Contains(line, "amd") || strings.Contains(line, "radeon") {
				return "amd", strings.TrimSpace(line), 16
			}
			if strings.Contains(line, "intel") {
				return "intel", strings.TrimSpace(line), 8
			}
		}
	}

	// Fallback: return CPU
	return "cpu", "CPU only", 0
}

// detectLinuxGPU определяет GPU на Linux.
func detectLinuxGPU() (gpuType, gpuName string, gpuLayers int) {
	// Try nvidia-smi for NVIDIA
	output, err := executeCommand("nvidia-smi", "--query-gpu=name", "--format=csv,noheader")
	if err == nil && output != "" {
		return "nvidia", strings.TrimSpace(output), 32
	}

	// Try lspci for AMD/Intel
	output, err = executeCommand("lspci")
	if err == nil {
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			line = strings.ToLower(line)
			if strings.Contains(line, "amd") || strings.Contains(line, "radeon") {
				return "amd", strings.TrimSpace(line), 16
			}
			if strings.Contains(line, "intel") {
				return "intel", strings.TrimSpace(line), 8
			}
		}
	}

	// Fallback: return CPU
	return "cpu", "CPU only", 0
}

// detectDarwinGPU определяет GPU на macOS.
func detectDarwinGPU() (gpuType, gpuName string, gpuLayers int) {
	// Try system_profiler for Apple Silicon
	output, err := executeCommand("system_profiler", "SPHardwareDataType")
	if err == nil {
		if strings.Contains(output, "Apple") {
			return "apple", "Apple Silicon", 32
		}
	}

	// Try system_profiler for GPU
	output, err = executeCommand("system_profiler", "SPDisplaysDataType")
	if err == nil {
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			line = strings.ToLower(line)
			if strings.Contains(line, "nvidia") {
				return "nvidia", strings.TrimSpace(line), 32
			}
			if strings.Contains(line, "amd") || strings.Contains(line, "radeon") {
				return "amd", strings.TrimSpace(line), 16
			}
			if strings.Contains(line, "intel") {
				return "intel", strings.TrimSpace(line), 8
			}
		}
	}

	// Fallback: return Apple Silicon if M1/M2/M3
	output, err = executeCommand("sysctl", "-n", "machdep.cpu.brand_string")
	if err == nil && strings.Contains(strings.ToLower(output), "apple") {
		return "apple", "Apple Silicon", 32
	}

	// Last fallback: return CPU
	return "cpu", "CPU only", 0
}

// detectWindowsRAM определяет RAM на Windows через WMI.
func detectWindowsRAM() int {
	// Try WMI first
	output, err := executeCommand("powershell", "-Command", "(Get-CimInstance Win32_PhysicalMemory | Measure-Object -Property Capacity -Sum).Sum / 1GB")
	if err == nil && output != "" {
		if ram, err := parseFloat(output); err == nil {
			return int(ram)
		}
	}

	// Fallback: try to read from system info
	output, err = executeCommand("systeminfo")
	if err == nil {
		// Parse "Total Physical Memory" from systeminfo
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			if strings.Contains(line, "Total Physical Memory") {
				// Extract number from "Total Physical Memory:    16,384 MB"
				var ram int
				_, _ = fmt.Sscanf(line, "%*s %*s %d", &ram)
				return ram / 1024 // Convert MB to GB
			}
		}
	}

	// Last resort: return 8GB as default
	return 8
}

// detectLinuxRAM определяет RAM на Linux.
func detectLinuxRAM() int {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 8 // Default fallback
	}

	// Parse MemTotal:    16384000 kB
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "MemTotal:") {
			var memKB int
			_, _ = fmt.Sscanf(line, "MemTotal: %d kB", &memKB)
			return memKB / 1024 / 1024 // Convert to GB
		}
	}

	return 8 // Default fallback
}

// detectDarwinRAM определяет RAM на macOS.
func detectDarwinRAM() int {
	output, err := executeCommand("sysctl", "-n", "hw.memsize")
	if err == nil && output != "" {
		var memBytes int64
		_, _ = fmt.Sscanf(output, "%d", &memBytes)
		return int(memBytes / 1024 / 1024 / 1024) // Convert to GB
	}

	return 8 // Default fallback
}

// executeCommand выполняет команду и возвращает её вывод.
func executeCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

// parseFloat парсит float из строки.
func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}
