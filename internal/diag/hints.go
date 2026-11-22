// Package diag holds the single wording for user-facing diagnostics. Both the
// provider layer (which knows the failing cause) and the CLI layer (which
// prints the final message) use these constants, so one cause has one hint.
package diag

const (
	HintUsage             = "run with --help for usage"
	HintConfig            = "check configuration with `dmsh config show` or `dmsh doctor`"
	HintProxy             = "proxy is unavailable or misconfigured: check proxy settings with `dmsh config show` or `dmsh doctor`"
	HintAPIKeyRequired    = "set POLLINATIONS_API_KEY (get one at https://enter.pollinations.ai/keys) or configure remote_api_key; run `dmsh doctor`"
	HintOllamaNotRunning  = "Ollama is not responding: start it with `ollama serve` (default http://127.0.0.1:11434); run `dmsh doctor`"
	HintOllamaModel       = "pull the model first with `ollama pull <model>`"
	HintNoNetwork         = "no network connection or remote endpoint unreachable: check network/proxy/provider settings; run `dmsh doctor`"
	HintLocalModelMissing = "model is not downloaded: run `dmsh model download` or switch provider with `/setup`; run `dmsh doctor`"
	HintNoCGO             = "local inference is unavailable in this build: rebuild with `-tags llama` or use a remote provider; run `dmsh doctor`"
	HintOutOfPollen       = "out of Pollen — top up at https://enter.pollinations.ai/pollen or pick another model with --remote-model"
	HintRateLimited       = "rate limited — wait a moment, or lower --max-tokens"
	HintUnknownModel      = "unknown model or endpoint — see the catalog at https://gen.pollinations.ai/models"
	HintProvider          = "check provider/model/network with `dmsh config show` or `dmsh doctor`"
	HintDoctor            = "run `dmsh doctor` for diagnostics"
)
