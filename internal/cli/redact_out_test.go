package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/netproxy"
)

// Secrets must not leak through notices or the setup screen, including the
// key taken from the environment and credentials embedded in URLs.
func TestSensitiveValuesNeverReachOutput(t *testing.T) {
	const envKey = "sk_leaktest_env_123456789"
	t.Setenv(config.APIKeyEnv, envKey)

	cfg := config.Config{
		Provider:      config.ProviderPollinations,
		RemoteModel:   "openai",
		RemoteAPIKey:  "sk_leaktest_cfg_987654321",
		RemoteBaseURL: "https://urluser:urlpass_leaktest@api.example.test/v1",
		ProxyMode:     netproxy.ModeCustom,
		ProxyProto:    netproxy.ProtoHTTP,
		ProxyHost:     "proxy.example.test",
		ProxyPort:     8080,
		ProxyUser:     "proxyuser_leaktest",
		ProxyPassword: "proxypass_leaktest",
	}
	s := &session{
		cfg:    cfg,
		notice: "endpoint " + cfg.RemoteBaseURL + " api_key=" + envKey + " password=" + cfg.ProxyPassword,
	}

	var out bytes.Buffer
	s.printNotice(&out)
	showSetup(&out, s)

	for _, secret := range []string{
		envKey, cfg.RemoteAPIKey, "urluser", "urlpass_leaktest",
		cfg.ProxyUser, cfg.ProxyPassword,
	} {
		if strings.Contains(out.String(), secret) {
			t.Errorf("secret %q leaked into output:\n%s", secret, out.String())
		}
	}
}

func TestRedactOutputPatterns(t *testing.T) {
	cfg := config.Config{RemoteAPIKey: "literal_leaktest_key"}
	got := redactOutput(cfg, "Authorization: Bearer bearer_leaktest_token api_key=literal_leaktest_key https://u:p@host/x")
	for _, secret := range []string{"bearer_leaktest_token", "literal_leaktest_key", "u:p"} {
		if strings.Contains(got, secret) {
			t.Errorf("secret %q survived redaction: %s", secret, got)
		}
	}
}
