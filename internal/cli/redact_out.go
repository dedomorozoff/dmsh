package cli

import (
	"net/url"

	"github.com/dedomorozoff/dmsh/internal/config"
	"github.com/dedomorozoff/dmsh/internal/redact"
)

// secretValues collects literal secrets for the current config. Known patterns
// (URL userinfo, bearer tokens, key=value) are handled by redact.Text itself;
// this list covers values that would otherwise leak verbatim.
func secretValues(cfg config.Config) []string {
	vals := []string{cfg.APIKey(), cfg.RemoteAPIKey, cfg.ProxyPassword}
	p := cfg.Proxy()
	vals = append(vals, p.User, p.Password)
	for _, raw := range []string{cfg.RemoteBaseURL, p.Address()} {
		u, err := url.Parse(raw)
		if err != nil || u.User == nil {
			continue
		}
		vals = append(vals, u.User.Username())
		if pass, ok := u.User.Password(); ok {
			vals = append(vals, pass)
		}
	}
	return vals
}

// redactOutput sanitizes text before it reaches a user-facing writer.
func redactOutput(cfg config.Config, s string) string {
	return redact.Text(s, secretValues(cfg)...)
}
