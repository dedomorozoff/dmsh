package llm

// Значения по умолчанию для локального сервера Ollama.
const (
	// DefaultOllamaBaseURL — адрес, который слушает `ollama serve`. Ollama
	// говорит на OpenAI-совместимом диалекте, поэтому отдельный протокол
	// не нужен: тот же chat/completions, только без ключа.
	DefaultOllamaBaseURL = "http://127.0.0.1:11434/v1"
	// DefaultOllamaModel — модель, которую чаще всего тянут для работы с
	// командной строкой. Любую другую можно задать через remote_model или
	// флаг --remote-model.
	DefaultOllamaModel = "qwen2.5-coder"
)

// NewOllama создаёт движок локального сервера Ollama. Ключ не нужен и не
// используется: адрес или модель могут оказаться неверными, и это станет
// ясно на первом запросе, с подсказкой в тексте ошибки.
func NewOllama(p Params) (Engine, error) {
	// Заголовок Authorization у Ollama не нужен, а POLLINATIONS_API_KEY
	// может быть задан в окружении: отправлять ключ Pollinations
	// локальному серверу нельзя.
	p.APIKey = ""
	return newOpenAIEngine(remoteProviderOllama, DefaultOllamaBaseURL, DefaultOllamaModel, p)
}
