package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/dedomorozoff/dmsh/internal/netproxy"
)

// DefaultRemoteModel — модель Pollinations, если пользователь не выбрал другую.
const DefaultRemoteModel = "openai"

// DefaultOllamaModel — модель Ollama по умолчанию. Пустое значение remote_model
// означает «покажи то, что нужно провайдеру», а не «всегда openai»: у Ollama
// своей модели нет, и запрос с чужим именем падает с 404.
const DefaultOllamaModel = "qwen2.5-coder"

// DefaultSearchModel — модель с веб-поиском, используемая инструментом
// websearch.
const DefaultSearchModel = "gemini-search"

// APIKeyEnv — переменная окружения с ключом удалённого провайдера. Она
// важнее файла конфига: ключ не обязан лежать на диске.
const APIKeyEnv = "POLLINATIONS_API_KEY"

// DefaultModelFor возвращает модель по умолчанию для провайдера. Локальный
// GGUF и не-OLLama провайдеры отдают DefaultRemoteModel.
func DefaultModelFor(p Provider) string {
	if p == ProviderOllama {
		return DefaultOllamaModel
	}
	return DefaultRemoteModel
}

// Mode определяет режим работы dmsh.
type Mode string

const (
	ModeAI    Mode = "ai"    // Полная автономия, команда выполняется автоматически
	ModeHelp  Mode = "help"  // Генерация команд + объяснения, пользователь выполняет вручную
	ModeShell Mode = "shell" // Прозрачный проход через оболочку
)

// Provider определяет, откуда берётся инференс.
type Provider string

const (
	// ProviderLocal — только локальный GGUF через llama.cpp. Без модели
	// запуск падает с понятной ошибкой.
	ProviderLocal Provider = "local"
	// ProviderPollinations — удалённый OpenAI-совместимый API Pollinations,
	// локальные модели игнорируются.
	ProviderPollinations Provider = "pollinations"
	// ProviderOllama — локальный сервер Ollama через его
	// OpenAI-совместимый endpoint. Ключ не нужен.
	ProviderOllama Provider = "ollama"
	// ProviderAuto — локальный GGUF, если он есть, иначе Pollinations.
	// Решение принимается один раз при старте сессии.
	ProviderAuto Provider = "auto"
)

// Valid сообщает, что провайдер известен. Пустое значение невалидно:
// оно нормализуется в ProviderAuto в Validate.
func (p Provider) Valid() bool {
	switch p {
	case ProviderLocal, ProviderPollinations, ProviderOllama, ProviderAuto:
		return true
	default:
		return false
	}
}

// Remote сообщает, что провайдер работает по сети. У локального GGUF нет
// адреса: его нельзя пересобирать при смене прокси, и показывать ему
// endpoint бессмысленно.
func (p Provider) Remote() bool { return p != "" && p != ProviderLocal }

// Config описывает рантайм-настройки dmsh. Поля сознательно плоские,
// чтобы их легко было пробрасывать из флагов CLI и из JSON-файла.
type Config struct {
	ModelPath     string   `json:"model_path"`
	DefaultModel  string   `json:"default_model"`
	Provider      Provider `json:"provider"`
	RemoteModel   string   `json:"remote_model,omitempty"`
	RemoteBaseURL string   `json:"remote_base_url,omitempty"`
	// RemoteAPIKey — ключ удалённого провайдера (Pollinations требует его
	// для генерации). Пусто: запрос уходит без заголовка и сервер сам
	// объяснит, что ключ нужен. Переменная окружения важнее файла.
	RemoteAPIKey       string            `json:"remote_api_key,omitempty"`
	SearchModel        string            `json:"search_model,omitempty"`
	ToolsEnabled       bool              `json:"tools_enabled"`
	Threads            int               `json:"threads"`
	CtxSize            int               `json:"ctx_size"`
	GPULayers          int               `json:"gpu_layers"`
	MaxTokens          int               `json:"max_tokens"`
	Temperature        float32           `json:"temperature"`
	TopP               float32           `json:"top_p"`
	Shell              string            `json:"shell"`
	HistoryFile        string            `json:"history_file"`
	AuditFile          string            `json:"audit_file"`
	DryRun             bool              `json:"dry_run"`
	Mode               Mode              `json:"mode"`
	ResumeSession      bool              `json:"resume_session"`
	DangerPatterns     []string          `json:"danger_patterns,omitempty"`
	SuspiciousPatterns []string          `json:"suspicious_patterns,omitempty"`
	Allowlist          []string          `json:"allowlist,omitempty"`
	Aliases            map[string]string `json:"aliases,omitempty"`

	// Сеть: прокси применяется ко всем удалённым запросам (LLM-API,
	// websearch, скачивание GGUF). Поля плоские — так же, как остальные,
	// чтобы их легко было задать флагом или из JSON.
	ProxyMode     netproxy.Mode `json:"proxy_mode,omitempty"`
	ProxyProto    string        `json:"proxy_proto,omitempty"`
	ProxyHost     string        `json:"proxy_host,omitempty"`
	ProxyPort     int           `json:"proxy_port,omitempty"`
	ProxyUser     string        `json:"proxy_user,omitempty"`
	ProxyPassword string        `json:"proxy_password,omitempty"`
	NoProxy       string        `json:"no_proxy,omitempty"`

	// ProxyURL — старый единый адрес. Сохраняется только для чтения:
	// Proxy() раскладывает его по полям выше, а SetProxy записывает
	// обратно уже поля, поэтому в конфиге он постепенно исчезает сам.
	ProxyURL string `json:"proxy_url,omitempty"`
}

// APIKey возвращает ключ удалённого провайдера. Переменная окружения важнее
// файла: так ключ можно вообще не хранить на диске. Пустая строка означает
// «запрос без заголовка» — не ошибку.
func (c Config) APIKey() string {
	if k := strings.TrimSpace(os.Getenv(APIKeyEnv)); k != "" {
		return k
	}
	return strings.TrimSpace(c.RemoteAPIKey)
}

// MaskedAPIKey показывает, что ключ задан, не показывая его: значение
// попадает в вывод сессии и на скриншоты.
func (c Config) MaskedAPIKey() string {
	if c.APIKey() == "" {
		return ""
	}
	return strings.Repeat("*", 8)
}

// APIKeySource отвечает, откуда взят ключ: "env" или "config". Пустая
// строка — ключа нет.
func (c Config) APIKeySource() string {
	switch {
	case c.APIKey() == "":
		return ""
	case strings.TrimSpace(os.Getenv(APIKeyEnv)) != "":
		return "env"
	default:
		return "config"
	}
}

// Proxy собирает настройки прокси из плоских полей конфига.
func (c Config) Proxy() netproxy.Settings {
	return netproxy.Settings{
		Mode:     c.ProxyMode,
		Proto:    c.ProxyProto,
		Host:     c.ProxyHost,
		Port:     c.ProxyPort,
		User:     c.ProxyUser,
		Password: c.ProxyPassword,
		NoProxy:  c.NoProxy,
		URL:      c.ProxyURL,
	}.Normalize()
}

// SetProxy раскладывает настройки прокси обратно в плоские поля. Старый
// ProxyURL при этом очищается: держать адрес в двух местах — значит
// получить два разных прокси из одного файла.
func (c *Config) SetProxy(p netproxy.Settings) {
	p = p.Normalize()
	c.ProxyMode = p.Mode
	c.ProxyProto = p.Proto
	c.ProxyHost = p.Host
	c.ProxyPort = p.Port
	c.ProxyUser = p.User
	c.ProxyPassword = p.Password
	c.NoProxy = p.NoProxy
	c.ProxyURL = ""
}

// HardwareInfo содержит информацию о возможностях системы.
type HardwareInfo struct {
	CPUCores  int
	RAMGB     int
	GPULayers int
	GPUName   string
	HasGPU    bool
	GPUType   string // "cpu", "nvidia", "amd", "apple", "intel"
}

// hwOnce гарантирует однократный запуск детекции железа за процесс.
var (
	hwOnce   sync.Once
	hwCached HardwareInfo
)

// DetectHardware определяет возможности системы. Результат кешируется —
// внешние процессы (nvidia-smi, powershell) запускаются не более одного раза.
func DetectHardware() HardwareInfo {
	hwOnce.Do(func() {
		hw := HardwareInfo{
			CPUCores: runtime.NumCPU(),
			RAMGB:    DetectRAMGB(),
		}
		hw.GPUType, hw.GPUName, hw.GPULayers = detectGPU()
		hw.HasGPU = hw.GPUType != "cpu"
		hwCached = hw
	})
	return hwCached
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
		Provider: ProviderAuto,
		// RemoteModel намеренно пуст: дефолт зависит от провайдера
		// (DefaultModelFor), и зашивать его сюда значило бы тащить Pollinations
		// в Ollama. Validate подставляет нужный.
		SearchModel:  DefaultSearchModel,
		Threads:      hw.CPUCores,
		CtxSize:      4096,
		GPULayers:    gpuLayers,
		MaxTokens:    512,
		Temperature:  0.2,
		TopP:         0.9,
		Shell:        defaultShell(),
		HistoryFile:  defaultHistoryFile(),
		AuditFile:    defaultAuditFile(),
		DryRun:       false,
		ToolsEnabled: true,
		Mode:         ModeAI,
		ProxyMode:    netproxy.ModeAuto,
		ProxyProto:   netproxy.ProtoHTTP,
	}
}

// Load читает конфиг из ~/.config/dmsh/config.json, если он есть, и
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

	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Validate проверяет согласованность конфига. Пустой mode нормализуется к
// ModeAI; некорректные значения и некомпилируемые пользовательские regex
// возвращаются как ошибка, чтобы не применять молча невалидный конфиг.
func (c *Config) Validate() error {
	switch c.Mode {
	case "":
		c.Mode = ModeAI
	case ModeAI, ModeHelp, ModeShell:
	default:
		return fmt.Errorf("invalid mode %q (expected ai, help or shell)", c.Mode)
	}

	switch c.Provider {
	case "":
		c.Provider = ProviderAuto
	case ProviderLocal, ProviderPollinations, ProviderOllama, ProviderAuto:
	default:
		return fmt.Errorf("invalid provider %q (expected local, pollinations, ollama or auto)", c.Provider)
	}

	c.RemoteModel = strings.TrimSpace(c.RemoteModel)
	if c.RemoteModel == "" {
		c.RemoteModel = DefaultModelFor(c.Provider)
	}
	c.SearchModel = strings.TrimSpace(c.SearchModel)
	if c.SearchModel == "" {
		c.SearchModel = DefaultSearchModel
	}
	c.RemoteAPIKey = strings.TrimSpace(c.RemoteAPIKey)
	if raw := strings.TrimSpace(c.RemoteBaseURL); raw != "" {
		if err := ValidateRemoteBaseURL(raw); err != nil {
			return err
		}
	}

	// Прокси: пустой режим означает auto, неверный протокол, порт или
	// список исключений — ошибка. Молча уйти мимо прокси нельзя: запрос
	// либо не дойдёт вообще, либо уйдёт туда, куда не собирались.
	proxy := c.Proxy()
	if err := proxy.Validate(); err != nil {
		return err
	}
	c.SetProxy(proxy)

	if c.Threads < 0 || c.CtxSize < 0 || c.GPULayers < 0 || c.MaxTokens < 0 {
		return errors.New("threads, ctx_size, gpu_layers and max_tokens must be >= 0")
	}
	if c.Temperature < 0 || c.Temperature > 2 {
		return fmt.Errorf("temperature %.2f out of range [0, 2]", c.Temperature)
	}
	if c.TopP <= 0 || c.TopP > 1 {
		return fmt.Errorf("top_p %.2f out of range (0, 1]", c.TopP)
	}

	for _, pat := range append(append([]string{}, c.DangerPatterns...), c.SuspiciousPatterns...) {
		if strings.TrimSpace(pat) == "" {
			continue
		}
		if _, err := regexp.Compile(pat); err != nil {
			return fmt.Errorf("invalid regex pattern %q: %w", pat, err)
		}
	}
	return nil
}

// ValidateRemoteBaseURL проверяет адрес удалённого провайдера. Вынесено
// отдельно, потому что адрес меняется и из флагов, и в экране настроек, а
// правила должны быть одни и те же.
func ValidateRemoteBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid remote_base_url %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("remote_base_url must be http(s), got %q", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("remote_base_url %q has no host", raw)
	}
	return nil
}

// Save сохраняет конфигурацию в ~/.config/dmsh/config.json
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
	return filepath.Join(dir, "dmsh", "config.json"), nil
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
	return filepath.Join(dir, "dmsh", "history.jsonl")
}

func defaultAuditFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "dmsh", "audit.jsonl")
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
