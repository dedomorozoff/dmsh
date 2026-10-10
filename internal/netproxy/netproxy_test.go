package netproxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		name string
		in   Settings
		want Settings
	}{
		{"empty mode means auto", Settings{}, Settings{Mode: ModeAuto, Proto: ProtoHTTP}},
		{"custom without host stays broken", Settings{Mode: ModeCustom}, Settings{Mode: ModeCustom, Proto: ProtoHTTP}},
		{"legacy url is expanded into fields", Settings{Mode: ModeCustom, URL: "  socks5://Proxy:1080  "}, Settings{Mode: ModeCustom, Proto: ProtoSOCKS5, Host: "proxy", Port: 1080}},
		{"url without scheme defaults to http", Settings{Mode: ModeCustom, URL: "127.0.0.1:8080"}, Settings{Mode: ModeCustom, Proto: ProtoHTTP, Host: "127.0.0.1", Port: 8080}},
		{"url credentials become fields", Settings{Mode: ModeCustom, URL: "http://user:pass@proxy:3128"}, Settings{Mode: ModeCustom, Proto: ProtoHTTP, Host: "proxy", Port: 3128, User: "user", Password: "pass"}},
		{"fields win over url", Settings{Mode: ModeCustom, Host: "field", URL: "http://url:1"}, Settings{Mode: ModeCustom, Proto: ProtoHTTP, Host: "field"}},
		{"host is lowercased", Settings{Mode: ModeCustom, Host: "Proxy.EXAMPLE.com"}, Settings{Mode: ModeCustom, Proto: ProtoHTTP, Host: "proxy.example.com"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.in.Normalize()
			if got != tc.want {
				t.Fatalf("Normalize() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestApplyURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    Settings
		wantErr string
	}{
		{"http", "http://proxy:8080", Settings{Proto: ProtoHTTP, Host: "proxy", Port: 8080}, ""},
		{"socks5", "socks5://10.0.0.1:1080", Settings{Proto: ProtoSOCKS5, Host: "10.0.0.1", Port: 1080}, ""},
		{"socks5h", "socks5h://10.0.0.1:1080", Settings{Proto: ProtoSOCKS5H, Host: "10.0.0.1", Port: 1080}, ""},
		{"credentials", "http://user:pass@proxy:3128", Settings{Proto: ProtoHTTP, Host: "proxy", Port: 3128, User: "user", Password: "pass"}, ""},
		{"no scheme", "localhost:8080", Settings{Proto: ProtoHTTP, Host: "localhost", Port: 8080}, ""},
		{"no port", "socks5://proxy", Settings{Proto: ProtoSOCKS5, Host: "proxy"}, ""},
		{"empty", "   ", Settings{}, "empty"},
		{"unknown scheme", "ftp://proxy:21", Settings{}, "not supported"},
		{"no host", "http://", Settings{}, "no host"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var s Settings
			err := s.ApplyURL(tc.raw)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("ApplyURL(%q) error = %v, want it to mention %q", tc.raw, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ApplyURL(%q): %v", tc.raw, err)
			}
			if s != tc.want {
				t.Fatalf("ApplyURL(%q) = %+v, want %+v", tc.raw, s, tc.want)
			}
		})
	}
}

func TestAddressComposition(t *testing.T) {
	s := Settings{Proto: ProtoSOCKS5, Host: "proxy.local", Port: 1080, User: "u", Password: "p"}
	if got := s.Address(); got != "socks5://u:p@proxy.local:1080" {
		t.Fatalf("Address() = %q", got)
	}
	if got := (Settings{Proto: ProtoHTTP, Host: "proxy"}).Address(); got != "http://proxy" {
		t.Fatalf("Address() without port = %q", got)
	}
	if got := (Settings{}).Address(); got != "" {
		t.Fatalf("Address() without host = %q, want empty", got)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		in      Settings
		wantErr string
	}{
		{"auto is valid", Settings{Mode: ModeAuto}, ""},
		{"off is valid", Settings{Mode: ModeOff}, ""},
		{"custom with http", Settings{Mode: ModeCustom, Host: "127.0.0.1", Port: 8080}, ""},
		{"custom with socks5", Settings{Mode: ModeCustom, Proto: ProtoSOCKS5, Host: "127.0.0.1", Port: 1080}, ""},
		{"custom without port", Settings{Mode: ModeCustom, Host: "proxy"}, ""},
		{"custom with credentials", Settings{Mode: ModeCustom, Host: "p", Port: 1, User: "u", Password: "s"}, ""},
		{"unknown mode", Settings{Mode: "sometimes"}, "invalid proxy_mode"},
		{"unknown proto", Settings{Mode: ModeCustom, Proto: "ftp", Host: "p"}, "invalid proxy_proto"},
		{"bad port", Settings{Mode: ModeCustom, Host: "p", Port: 70000}, "invalid proxy_port"},
		{"custom without host", Settings{Mode: ModeCustom}, "proxy_host is required"},
		{"no_proxy with scheme", Settings{Mode: ModeAuto, NoProxy: "http://localhost"}, "without a scheme"},
		{"no_proxy bad cidr", Settings{Mode: ModeAuto, NoProxy: "10.0.0.0/99"}, "not a valid CIDR"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.in.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestResolverModeOffIgnoresEnvironment(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://env-proxy:3128")
	resolver, err := Settings{Mode: ModeOff, Host: "my-proxy", Port: 8080}.Resolver()
	if err != nil {
		t.Fatalf("Resolver() error = %v", err)
	}
	got, err := resolver(mustRequest(t, "http://example.com/x"))
	if err != nil {
		t.Fatalf("resolver error = %v", err)
	}
	if got != nil {
		t.Fatalf("proxy = %v, want nil (direct connection)", got)
	}
}

func TestResolverAutoUsesEnvironment(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://env-proxy:3128")
	resolver, err := Settings{Mode: ModeAuto}.Resolver()
	if err != nil {
		t.Fatalf("Resolver() error = %v", err)
	}
	got, err := resolver(mustRequest(t, "https://example.com/x"))
	if err != nil {
		t.Fatalf("resolver error = %v", err)
	}
	if got == nil || got.Host != "env-proxy:3128" {
		t.Fatalf("proxy = %v, want env-proxy:3128", got)
	}
}

func TestResolverCustomReturnsConfiguredProxy(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://env-proxy:3128")
	resolver, err := Settings{Mode: ModeCustom, Proto: ProtoSOCKS5, Host: "my-proxy", Port: 1080}.Resolver()
	if err != nil {
		t.Fatalf("Resolver() error = %v", err)
	}
	got, err := resolver(mustRequest(t, "https://example.com/x"))
	if err != nil {
		t.Fatalf("resolver error = %v", err)
	}
	if got == nil || got.Scheme != "socks5" || got.Host != "my-proxy:1080" {
		t.Fatalf("proxy = %v, want socks5://my-proxy:1080", got)
	}
}

func TestResolverCustomUsesCredentials(t *testing.T) {
	resolver, err := Settings{Mode: ModeCustom, Host: "proxy", Port: 3128, User: "u", Password: "p"}.Resolver()
	if err != nil {
		t.Fatalf("Resolver() error = %v", err)
	}
	got, err := resolver(mustRequest(t, "https://example.com/x"))
	if err != nil {
		t.Fatalf("resolver error = %v", err)
	}
	if got == nil || got.User == nil {
		t.Fatalf("proxy = %v, want credentials in the proxy url", got)
	}
	if pass, _ := got.User.Password(); pass != "p" {
		t.Fatalf("proxy password = %q, want p", pass)
	}
}

func TestResolverNoProxyBypass(t *testing.T) {
	p := Settings{Mode: ModeCustom, Host: "my-proxy", Port: 8080, NoProxy: "localhost, .corp.example, 10.0.0.0/8, api.example.com:8443"}
	resolver, err := p.Resolver()
	if err != nil {
		t.Fatalf("Resolver() error = %v", err)
	}
	bypassed := []string{
		"http://localhost:3000/openai",
		"https://a.corp.example/v1",
		"https://corp.example/v1",
		"https://10.1.2.3/v1",
	}
	for _, raw := range bypassed {
		got, err := resolver(mustRequest(t, raw))
		if err != nil {
			t.Fatalf("resolver(%s) error = %v", raw, err)
		}
		if got != nil {
			t.Fatalf("resolver(%s) = %v, want direct connection", raw, got)
		}
	}
	proxied := []string{"https://example.com/v1", "https://notcorp.example.com/v1", "https://api.example.com/v1", "https://11.1.2.3/v1"}
	for _, raw := range proxied {
		got, err := resolver(mustRequest(t, raw))
		if err != nil {
			t.Fatalf("resolver(%s) error = %v", raw, err)
		}
		if got == nil || got.Host != "my-proxy:8080" {
			t.Fatalf("resolver(%s) = %v, want my-proxy:8080", raw, got)
		}
	}
}

func TestClientGoesThroughProxy(t *testing.T) {
	var seen string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Host
		w.WriteHeader(http.StatusTeapot)
	}))
	defer proxy.Close()

	client, err := Settings{Mode: ModeCustom, URL: proxy.URL}.Client(0)
	if err != nil {
		t.Fatalf("Client() error = %v", err)
	}
	resp, err := client.Get("http://target.invalid/v1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusTeapot)
	}
	if seen != "target.invalid" {
		t.Fatalf("proxy saw host %q, want target.invalid", seen)
	}
}

func TestEffectiveHidesCredentials(t *testing.T) {
	got, err := Settings{Mode: ModeCustom, Host: "proxy", Port: 8080, User: "user", Password: "secret"}.Effective("https://example.com/v1")
	if err != nil {
		t.Fatalf("Effective() error = %v", err)
	}
	if got != "http://proxy:8080" {
		t.Fatalf("Effective() = %q, want http://proxy:8080 (no credentials)", got)
	}
}

func TestEffectiveDirect(t *testing.T) {
	got, err := Settings{Mode: ModeOff}.Effective("https://example.com/v1")
	if err != nil {
		t.Fatalf("Effective() error = %v", err)
	}
	if got != "" {
		t.Fatalf("Effective() = %q, want empty (direct)", got)
	}
}

func TestMaskedPassword(t *testing.T) {
	if got := (Settings{}).MaskedPassword(); got != "" {
		t.Fatalf("MaskedPassword() = %q, want empty", got)
	}
	got := Settings{Password: "secret"}.MaskedPassword()
	if got == "" || strings.Contains(got, "secret") {
		t.Fatalf("MaskedPassword() = %q, want a mask", got)
	}
}

func TestCycle(t *testing.T) {
	if got := ModeAuto.Next(); got != ModeOff {
		t.Fatalf("ModeAuto.Next() = %q, want off", got)
	}
	if got := ModeOff.Next(); got != ModeCustom {
		t.Fatalf("ModeOff.Next() = %q, want custom", got)
	}
	if got := ModeCustom.Next(); got != ModeAuto {
		t.Fatalf("ModeCustom.Next() = %q, want auto", got)
	}
	if got := ModeAuto.Prev(); got != ModeCustom {
		t.Fatalf("ModeAuto.Prev() = %q, want custom", got)
	}
	protos := Protos()
	if got := NextProto(ProtoSOCKS5); got != ProtoSOCKS5H {
		t.Fatalf("NextProto(socks5) = %q, want socks5h", got)
	}
	if got := PrevProto(ProtoHTTP); got != ProtoSOCKS5H {
		t.Fatalf("PrevProto(http) = %q, want socks5h", got)
	}
	if got := NextProto(protos[len(protos)-1]); got != ProtoHTTP {
		t.Fatalf("NextProto(socks5h) = %q, want http (wrap)", got)
	}
}

// Enabled отвечает на вопрос «пойдёт ли запрос через прокси» до первой
// ошибки соединения: в auto адрес берётся из окружения, и «прокси нет» на
// самом деле означает «прокси есть, но не виден».
func TestEnabled(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://env-proxy:3128")
	if !(Settings{Mode: ModeAuto}).Enabled() {
		t.Fatal("auto with HTTPS_PROXY set must report a proxy")
	}
	if (Settings{Mode: ModeCustom, Host: "my-proxy", Port: 1080}).Enabled() != true {
		t.Fatal("custom with a host must report a proxy")
	}
	if (Settings{Mode: ModeCustom}).Enabled() {
		t.Fatal("custom without a host is not a working proxy")
	}
	if (Settings{Mode: ModeOff, Host: "my-proxy"}).Enabled() {
		t.Fatal("mode=off must never report a proxy")
	}
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("https_proxy", "")
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("http_proxy", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("all_proxy", "")
	if (Settings{Mode: ModeAuto}).Enabled() {
		t.Fatal("auto without proxy variables must report no proxy")
	}
}

func mustRequest(t *testing.T, raw string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		t.Fatalf("NewRequest(%q) error = %v", raw, err)
	}
	return req
}