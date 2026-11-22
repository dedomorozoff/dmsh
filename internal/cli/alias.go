package cli

import (
	"strings"

	"github.com/dedomorozoff/dmsh/internal/config"
)

// expandAlias replaces a whole request with its saved alias. Aliases are
// shortcuts for natural-language requests, not shell aliases: they are
// expanded before the prompt is built, so the model never sees the alias.
func expandAlias(cfg config.Config, input string) string {
	key := strings.ToLower(strings.TrimSpace(input))
	if key == "" || strings.HasPrefix(key, "/") || strings.HasPrefix(key, "!") {
		return input
	}
	value, ok := cfg.Aliases[key]
	if !ok {
		return input
	}
	if value = strings.TrimSpace(value); value == "" {
		return input
	}
	return value
}
