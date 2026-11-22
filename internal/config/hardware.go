package config

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

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
		// Parse "Total Physical Memory:    16,384 MB"
		// systeminfo форматирует числа с запятыми и пробелами в зависимости от локали,
		// поэтому сначала удаляем разделители тысяч перед парсингом.
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			if strings.Contains(line, "Total Physical Memory") {
				colonIdx := strings.Index(line, ":")
				if colonIdx == -1 {
					continue
				}
				numPart := strings.TrimSpace(line[colonIdx+1:])
				// Удаляем суффикс " MB" и разделители тысяч (запятые/точки/пробелы)
				numPart = strings.TrimSuffix(strings.TrimSpace(numPart), "MB")
				numPart = strings.ReplaceAll(numPart, ",", "")
				numPart = strings.ReplaceAll(numPart, ".", "")
				numPart = strings.TrimSpace(numPart)
				var ram int
				if _, err := fmt.Sscanf(numPart, "%d", &ram); err == nil && ram > 0 {
					return ram / 1024 // Convert MB to GB
				}
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

// executeCommand выполняет команду и возвращает её вывод. Переменная, чтобы
// тесты могли подменить вывод системных утилит.
var executeCommand = func(name string, args ...string) (string, error) {
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
