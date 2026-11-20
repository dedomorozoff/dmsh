package redact

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
)

var (
	// urlCreds matches userinfo in URLs (scheme://user:pass@host).
	urlCreds = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)([^@\s/]+)@`)
	// bearer matches Authorization bearer tokens.
	bearer = regexp.MustCompile(`(?i)(Bearer\s+)[^\s"']+`)
	// inlineAssign matches common secret assignments in commands/text.
	inlineAssign = regexp.MustCompile(`(?i)(\b[\w]*(?:api[_-]?key|secret|password|passwd|pwd|token)[\w]*\s*[=:]\s*)([^\s"']+|"[^"]*"|'[^']*')`)
)

// Text redacts secrets from arbitrary text: URL credentials, bearer tokens,
// common inline secret assignments, and any provided literal secret values.
func Text(s string, secrets ...string) string {
	if s == "" {
		return s
	}
	s = urlCreds.ReplaceAllString(s, `${1}[REDACTED]@`)
	s = bearer.ReplaceAllString(s, `${1}[REDACTED]`)
	s = inlineAssign.ReplaceAllStringFunc(s, func(m string) string {
		sub := inlineAssign.FindStringSubmatch(m)
		if len(sub) != 3 {
			return m
		}
		val := sub[2]
		// Avoid mangling values already redacted by earlier passes.
		if strings.Contains(val, "[REDACTED]") {
			return m
		}
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
			return sub[1] + val[:1] + "[REDACTED]" + val[len(val)-1:]
		}
		return sub[1] + "[REDACTED]"
	})
	s = Secrets(s, secrets...)
	return s
}

// Secrets replaces each non-empty secret literal with [REDACTED].
// Longer secrets are replaced first to avoid partial overlaps.
func Secrets(s string, secrets ...string) string {
	if s == "" || len(secrets) == 0 {
		return s
	}
	uniq := make([]string, 0, len(secrets))
	seen := map[string]bool{}
	for _, sec := range secrets {
		sec = strings.TrimSpace(sec)
		if sec == "" || len(sec) < 2 || seen[sec] {
			continue
		}
		seen[sec] = true
		uniq = append(uniq, sec)
	}
	if len(uniq) == 0 {
		return s
	}
	sort.Slice(uniq, func(i, j int) bool { return len(uniq[i]) > len(uniq[j]) })
	for _, sec := range uniq {
		s = strings.ReplaceAll(s, sec, "[REDACTED]")
	}
	return s
}

// URL redacts user credentials from a URL string. If parsing fails but the
// string looks like it contains URL credentials, it falls back to text redaction.
func URL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return urlCreds.ReplaceAllString(raw, `${1}[REDACTED]@`)
	}
	// Build a readable form without percent-encoding the placeholder.
	host := u.Host
	s := u.Scheme + "://[REDACTED]@" + host
	if u.Path != "" {
		s += u.EscapedPath()
	}
	if u.RawQuery != "" {
		s += "?" + u.RawQuery
	}
	if u.Fragment != "" {
		s += "#" + u.Fragment
	}
	return s
}
