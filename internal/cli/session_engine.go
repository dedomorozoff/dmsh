package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/llm"
	"github.com/dedomorozoff/dmsh/internal/model"
	"github.com/dedomorozoff/dmsh/internal/netproxy"
)

// newEngine создаёт движок для текущего провайдера и конфигурации.
func (s *session) newEngine() (llm.Engine, error) {
	return llm.New(llm.Params{
		Provider:      toLLMProvider(s.provider),
		ModelPath:     s.cfg.ModelPath,
		Threads:       s.cfg.Threads,
		CtxSize:       s.cfg.CtxSize,
		GPULayers:     s.cfg.GPULayers,
		RemoteModel:   s.cfg.RemoteModel,
		RemoteBaseURL: s.cfg.RemoteBaseURL,
		APIKey:        s.cfg.APIKey(),
		Proxy:         s.cfg.Proxy(),
	})
}

// switchProvider меняет способ подключения к LLM на лету: провайдер,
// модель, endpoint и ключ собираются в готовый конфиг, и только он уходит в
// движок. Старый движок закрывается после того, как новый загрузился, иначе
// ошибка загрузки оставила бы сессию без модели.
//
// «auto» здесь не остаётся отдельным провайдером: resolveProvider один раз
// выбирает local или удалённый, и именно этим выбором живёт сессия.
func (s *session) switchProvider(cfg config.Config) (config.Provider, string, error) {
	provider, notice, err := resolveProvider(cfg)
	if err != nil {
		return "", "", err
	}
	cfg.Provider = provider
	cfg, err = normalizeForProvider(cfg, provider)
	if err != nil {
		return "", "", err
	}

	s.mu.Lock()
	prevCfg, prevProvider, prevNotice := s.cfg, s.provider, s.notice
	s.cfg, s.provider, s.notice = cfg, provider, notice
	s.mu.Unlock()

	eng, err := s.newEngine()
	if err != nil {
		s.mu.Lock()
		s.cfg, s.provider, s.notice = prevCfg, prevProvider, prevNotice
		s.mu.Unlock()
		return "", "", fmt.Errorf("load model: %w", err)
	}
	old := s.engine
	oldSearch := s.searchEngine
	s.mu.Lock()
	s.engine, s.searchEngine = eng, nil
	s.mu.Unlock()
	if old != nil && old != eng {
		_ = old.Close()
	}
	if oldSearch != nil {
		_ = oldSearch.Close()
	}
	return provider, notice, nil
}

// applyProxy сохраняет настройки прокси и пересоздаёт сетевые движки, чтобы
// следующий запрос уже ушёл через него. Некорректные настройки не
// применяются: иначе пользователь потерял бы рабочее соединение и не смог
// бы вернуться.
//
// Локальный провайдер не трогаем: llama.cpp не ходит в сеть, а перезагрузка
// модели на каждый чих в настройках — это минуты ожидания впустую.
func (s *session) applyProxy(p netproxy.Settings) error {
	if err := p.Validate(); err != nil {
		return err
	}
	p = p.Normalize()

	s.mu.Lock()
	s.cfg.SetProxy(p)
	provider := s.provider
	s.mu.Unlock()

	if !provider.Remote() {
		return nil
	}
	return s.resetRemoteEngines()
}

// resetRemoteEngines пересоздаёт движок основного провайдера и сбрасывает
// движок websearch: у обоих зашит свой HTTP-клиент, поэтому после смены
// прокси или endpoint'а они должны быть собраны заново.
func (s *session) resetRemoteEngines() error {
	eng, err := s.newEngine()
	if err != nil {
		return fmt.Errorf("load model: %w", err)
	}
	old := s.engine
	oldSearch := s.searchEngine

	s.mu.Lock()
	s.engine = eng
	s.searchEngine = nil
	s.mu.Unlock()

	if old != nil && old != eng {
		_ = old.Close()
	}
	if oldSearch != nil {
		_ = oldSearch.Close()
	}
	return nil
}

// applyRemoteBaseURL меняет endpoint удалённого провайдера. Адрес
// проверяется до пересборки движков: битый endpoint не должен оставлять
// сессию без модели.
func (s *session) applyRemoteBaseURL(base string) error {
	cfg := s.cfg
	cfg.RemoteBaseURL = base
	if err := cfg.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	s.cfg.RemoteBaseURL = base
	s.mu.Unlock()
	return s.resetRemoteEngines()
}

// currentProvider — единый ответ на «какой провайдер сейчас»: явный выбор
// сессии, затем её конфиг, затем конфиг оболочки (флаг/файл), и только потом
// auto. Один и тот же порядок везде, где показывается живой провайдер:
// /model, /models, меню моделей, статусная строка.
func currentProvider(s *session, shell config.Provider) config.Provider {
	if s != nil {
		if s.provider.Valid() {
			return s.provider
		}
		if s.cfg.Provider.Valid() {
			return s.cfg.Provider
		}
	}
	if shell.Valid() {
		return shell
	}
	return config.ProviderAuto
}

// toLLMProvider переводит провайдера конфига в тип движка.
func toLLMProvider(p config.Provider) llm.Provider {
	switch p {
	case config.ProviderPollinations:
		return llm.ProviderPollinations
	case config.ProviderOllama:
		return llm.ProviderOllama
	case config.ProviderLocal:
		return llm.ProviderLocal
	default:
		return llm.ProviderAuto
	}
}

// resolveProvider выбирает провайдера инференса. Для ProviderAuto локальный
// GGUF используется только если файл действительно найден, иначе
// включается Pollinations и возвращается notice для показа пользователю.
// Скрытого отката при ошибке локального инференса нет: выбор делается
// один раз, здесь.
func resolveProvider(cfg config.Config) (config.Provider, string, error) {
	switch cfg.Provider {
	case config.ProviderPollinations:
		notice := ""
		if cfg.APIKey() == "" {
			notice = "no " + config.APIKeyEnv + " set — Pollinations needs a key for generation " +
				"(https://enter.pollinations.ai/keys)"
		}
		return config.ProviderPollinations, notice, nil
	case config.ProviderOllama:
		// Сервер может быть не запущен: проверять его здесь нечем, зато
		// можно один раз сказать, что он должен быть запущен.
		return config.ProviderOllama,
			"using a local Ollama server at " + remoteEndpoint(config.ProviderOllama, cfg.RemoteBaseURL) +
				" (start it with `ollama serve`)", nil
	case config.ProviderLocal:
		if _, err := resolveModelPath(cfg); err != nil {
			return "", "", fmt.Errorf("provider=local, but no GGUF model found: %w\n"+
				"  download one:   dmsh model download\n"+
				"  or go remote:   dmsh config set provider auto", err)
		}
		return config.ProviderLocal, "", nil
	case "", config.ProviderAuto:
		if _, err := resolveModelPath(cfg); err == nil {
			return config.ProviderLocal, "", nil
		}
		return config.ProviderPollinations,
			"no local GGUF model found — using Pollinations (provider=auto)", nil
	default:
		return "", "", fmt.Errorf("unknown provider %q", cfg.Provider)
	}
}

// printNotice показывает предупреждение о выбранном провайдере, если оно есть.
func (s *session) printNotice(out io.Writer) {
	if s.notice == "" {
		return
	}
	fmt.Fprintf(out, "%s[dmsh]%s %s%s%s\n", yellow, reset, yellow, redactOutput(s.cfg, s.notice), reset)
}

func resolveModelPath(cfg config.Config) (string, error) {
	if cfg.ModelPath != "" {
		if _, err := os.Stat(cfg.ModelPath); err == nil {
			return cfg.ModelPath, nil
		}
		d := model.New("")
		if d.Exists(cfg.ModelPath) {
			return d.ModelPath(cfg.ModelPath), nil
		}
	}

	if cfg.DefaultModel != "" {
		d := model.New("")
		if d.Exists(cfg.DefaultModel) {
			return d.ModelPath(cfg.DefaultModel), nil
		}
	}

	d := model.New("")
	available := d.ListModels()
	if len(available) > 0 {
		return d.ModelPath(available[0].Name), nil
	}

	all, _ := d.ListAllModels()
	if len(all) > 0 {
		return d.ModelPath(all[0].Name), nil
	}

	return "", errors.New("model not found, run: dmsh model")
}

func (s *session) close() {
	if s.engine != nil {
		_ = s.engine.Close()
	}
	if s.searchEngine != nil {
		_ = s.searchEngine.Close()
		s.searchEngine = nil
	}
	if s.debug != nil {
		s.debug.close()
	}
	// Save history
	_ = s.saveHistory()
	s.saveTurns()
}

// setRemoteModel меняет модель удалённого провайдера: имя модели зашито в
// движок, поэтому он пересобирается. Ошибка откатывает модель — сессия
// остаётся на той, что работает.
func (s *session) setRemoteModel(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("model name is empty")
	}
	s.mu.Lock()
	if strings.TrimSpace(s.cfg.RemoteModel) == name {
		s.mu.Unlock()
		return nil
	}
	prev := s.cfg.RemoteModel
	s.cfg.RemoteModel = name
	s.mu.Unlock()

	if err := s.resetRemoteEngines(); err != nil {
		s.mu.Lock()
		s.cfg.RemoteModel = prev
		s.mu.Unlock()
		return err
	}
	return nil
}

// switchModel заменяет загруженную модель на лету. При ошибке загрузки
// старая модель остаётся на месте и продолжает работать. Переключение на
// локальную модель всегда переводит сессию на провайдер local.
func (s *session) switchModel(path string) error {
	if s.engine == nil {
		return errors.New("no engine loaded")
	}
	eng, err := llm.New(llm.Params{
		Provider:  llm.ProviderLocal,
		ModelPath: path,
		Threads:   s.cfg.Threads,
		CtxSize:   s.cfg.CtxSize,
		GPULayers: s.cfg.GPULayers,
	})
	if err != nil {
		return fmt.Errorf("load model: %w", err)
	}
	// Закрываем старый движок только после успешной загрузки нового,
	// чтобы не оставить s.engine указывающим на закрытый объект.
	old := s.engine
	s.engine = eng
	s.cfg.ModelPath = path
	s.provider = config.ProviderLocal
	s.cfg.Provider = config.ProviderLocal
	s.notice = ""
	_ = old.Close()
	return nil
}
