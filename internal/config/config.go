package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/dedomorozoff/dmsh/internal/fsutil"
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

// CurrentConfigVersion - текущая версия схемы config.json.
const CurrentConfigVersion = 1

// Config описывает рантайм-настройки dmsh. Поля сознательно плоские,
// чтобы их легко было пробрасывать из флагов CLI и из JSON-файла.
type Config struct {
	ConfigVersion int      `json:"config_version,omitempty"`
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

	// Mouse — мышь в TUI: клик по папке в статусной строке открывает выбор
	// каталога. Выключается, когда нужно штатное выделение текста в
	// терминале: пока мышь включена, терминал отправляет события мыши
	// приложению вместо собственного выделения.
	Mouse bool `json:"mouse"`

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
		ConfigVersion: CurrentConfigVersion,
		Provider:      ProviderAuto,
		// RemoteModel намеренно пуст: дефолт зависит от провайдера
		// (DefaultModelFor), и зашивать его сюда значило бы тащить Pollinations
		// в Ollama. Validate подставляет нужный.
		SearchModel: DefaultSearchModel,
		Threads:     hw.CPUCores,
		CtxSize:     4096,
		GPULayers:   gpuLayers,
		MaxTokens:   1024,
		Temperature: 0.2,
		TopP:        0.9,
		Shell:       defaultShell(),
		HistoryFile: defaultHistoryFile(),
		AuditFile:   defaultAuditFile(),
		// Безопасный дефолт: команды показываются, но не выполняются, пока
		// пользователь явно не снимет dry-run (флаг --dry-run=false или
		// настройка в конфиге). Совпадает с политикой AGENTS.md.
		DryRun:       true,
		ToolsEnabled: true,
		Mode:         ModeAI,
		Mouse:        true,
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
	if err := migrate(&cfg, data); err != nil {
		return cfg, fmt.Errorf("migrate config %s: %w", path, err)
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
	if cfg.ConfigVersion < CurrentConfigVersion {
		cfg.ConfigVersion = CurrentConfigVersion
	}
	path, err := userConfigPath()
	if err != nil {
		return fmt.Errorf("config path: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// В конфиге может лежать пароль прокси: файл и каталог закрыты для
	// остальных пользователей.
	if err := fsutil.WritePrivate(path, data); err != nil {
		return fmt.Errorf("config: %w", err)
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

// ConfigExists reports whether the user has ever saved a config. A missing
// file means the safe defaults are in force and the user has not made an
// explicit choice about dry-run yet.
func ConfigExists() bool {
	path, err := userConfigPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
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
