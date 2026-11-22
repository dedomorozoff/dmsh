package config

import (
	"errors"
	"strings"
	"testing"
)

// stubExec answers hardware-detection commands without touching the host.
func stubExec(t *testing.T, replies map[string]string) {
	t.Helper()
	old := executeCommand
	executeCommand = func(name string, args ...string) (string, error) {
		key := strings.TrimSpace(name + " " + strings.Join(args, " "))
		if out, ok := replies[key]; ok {
			return out, nil
		}
		return "", errors.New("command not available in test")
	}
	t.Cleanup(func() { executeCommand = old })
}

const (
	wmiGPU    = "powershell -Command Get-WmiObject Win32_VideoController | Select-Object -ExpandProperty Name"
	wmiRAM    = "powershell -Command (Get-CimInstance Win32_PhysicalMemory | Measure-Object -Property Capacity -Sum).Sum / 1GB"
	nvidiaSMI = "nvidia-smi --query-gpu=name --format=csv,noheader"
)

func TestDetectWindowsGPU(t *testing.T) {
	cases := []struct {
		name   string
		reply  string
		want   string
		layers int
	}{
		{"nvidia", "NVIDIA GeForce RTX 4070", "nvidia", 32},
		{"amd", "AMD Radeon RX 7800", "amd", 16},
		{"intel", "Intel UHD Graphics", "intel", 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stubExec(t, map[string]string{wmiGPU: tc.reply})
			got, _, layers := detectWindowsGPU()
			if got != tc.want || layers != tc.layers {
				t.Fatalf("detectWindowsGPU = %q/%d, want %q/%d", got, layers, tc.want, tc.layers)
			}
		})
	}
	stubExec(t, nil)
	if got, _, layers := detectWindowsGPU(); got != "cpu" || layers != 0 {
		t.Fatalf("fallback = %q/%d, want cpu/0", got, layers)
	}
}

func TestDetectLinuxGPU(t *testing.T) {
	stubExec(t, map[string]string{nvidiaSMI: "Tesla T4"})
	if got, _, layers := detectLinuxGPU(); got != "nvidia" || layers != 32 {
		t.Fatalf("nvidia path = %q/%d", got, layers)
	}

	t.Run("lspci", func(t *testing.T) {
		stubExec(t, map[string]string{"lspci": "00:02.0 VGA compatible controller: Intel UHD"})
		if got, _, layers := detectLinuxGPU(); got != "intel" || layers != 8 {
			t.Fatalf("intel path = %q/%d", got, layers)
		}
	})
	stubExec(t, nil)
	if got, _, layers := detectLinuxGPU(); got != "cpu" || layers != 0 {
		t.Fatalf("fallback = %q/%d, want cpu/0", got, layers)
	}
}

func TestDetectDarwinGPU(t *testing.T) {
	stubExec(t, map[string]string{"system_profiler SPHardwareDataType": "Model Name: MacBook Pro\nChip: Apple M2"})
	if got, _, layers := detectDarwinGPU(); got != "apple" || layers != 32 {
		t.Fatalf("apple silicon path = %q/%d", got, layers)
	}
	t.Run("displays", func(t *testing.T) {
		stubExec(t, map[string]string{"system_profiler SPDisplaysDataType": "Vendor: NVIDIA"})
		if got, _, layers := detectDarwinGPU(); got != "nvidia" || layers != 32 {
			t.Fatalf("nvidia path = %q/%d", got, layers)
		}
	})
	stubExec(t, nil)
	if got, _, layers := detectDarwinGPU(); got != "cpu" || layers != 0 {
		t.Fatalf("fallback = %q/%d, want cpu/0", got, layers)
	}
}

func TestDetectRAM(t *testing.T) {
	stubExec(t, map[string]string{wmiRAM: "16.4"})
	if got := detectWindowsRAM(); got != 16 {
		t.Fatalf("windows WMI RAM = %d, want 16", got)
	}

	stubExec(t, map[string]string{"systeminfo": "Total Physical Memory:    16,384 MB"})
	if got := detectWindowsRAM(); got != 16 {
		t.Fatalf("windows systeminfo RAM = %d, want 16", got)
	}

	stubExec(t, map[string]string{"sysctl -n hw.memsize": "17179869184"})
	if got := detectDarwinRAM(); got != 16 {
		t.Fatalf("darwin RAM = %d, want 16", got)
	}

	stubExec(t, nil)
	if got := detectWindowsRAM(); got != 8 {
		t.Fatalf("windows fallback RAM = %d, want 8", got)
	}
	if got := detectDarwinRAM(); got != 8 {
		t.Fatalf("darwin fallback RAM = %d, want 8", got)
	}
}
