// Package netproxy строит HTTP-клиенты с учётом настроек прокси.
//
// Он нужен везде, где dmsh ходит в сеть: удалённый LLM-API, движок
// websearch и скачивание GGUF. Настройки задаются один раз (конфиг, флаги
// или экран настроек в TUI) и применяются ко всем клиентам одинаково.
//
// Настройки хранятся по полям (протокол, хост, порт, логин, пароль), а не
// одним URL: в TUI их удобно заполнять по одному, и опечатку в протоколе
// видно сразу. Полный адрес тоже принимается — Normalize() раскладывает
// его по полям.
//
// Три режима:
//   - auto   — HTTP_PROXY/HTTPS_PROXY/ALL_PROXY/NO_PROXY из окружения;
//   - off    — соединение напрямую, переменные окружения игнорируются;
//   - custom — адрес из полей Proto/Host/Port/User/Password.
package netproxy

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Mode — как выбирается прокси для исходящих запросов.
type Mode string

const (
	// ModeAuto — переменные окружения, при их отсутствии соединение прямое.
	ModeAuto Mode = "auto"
	// ModeOff — прокси не используется никогда.
	ModeOff Mode = "off"
	// ModeCustom — всегда адрес из полей Settings.
	ModeCustom Mode = "custom"
)

// Modes — порядок режимов для перебора стрелками в TUI.
var Modes = []Mode{ModeAuto, ModeOff, ModeCustom}

// Протоколы прокси. socks5 разрешает имя на стороне прокси, socks5h —
// всегда у него; для публичных адресов это важно, иначе локальный DNS не
// нужен вообще.
const (
	ProtoHTTP    = "http"
	ProtoHTTPS   = "https"
	ProtoSOCKS5  = "socks5"
	ProtoSOCKS5H = "socks5h"
)

// Protos — порядок протоколов для перебора стрелками в TUI.
func Protos() []string { return []string{ProtoHTTP, ProtoHTTPS, ProtoSOCKS5, ProtoSOCKS5H} }

// ValidProto сообщает, что протокол поддерживается. Пустое значение
// невалидно: его подставляет Normalize.
func ValidProto(p string) bool {
	switch p {
	case ProtoHTTP, ProtoHTTPS, ProtoSOCKS5, ProtoSOCKS5H:
		return true
	default:
		return false
	}
}

// NextProto/PrevProto перебирают протокол по кругу.
func NextProto(p string) string { return cycleProto(p, 1) }

func PrevProto(p string) string { return cycleProto(p, -1) }

func cycleProto(p string, dir int) string {
	protos := Protos()
	for i, v := range protos {
		if v == p {
			return protos[(i+dir+len(protos))%len(protos)]
		}
	}
	return protos[0]
}

// Valid сообщает, что режим известен. Пустое значение невалидно: его
// нормализует Normalize.
func (m Mode) Valid() bool {
	switch m {
	case ModeAuto, ModeOff, ModeCustom:
		return true
	default:
		return false
	}
}

// ParseMode приводит строку к известному режиму.
func ParseMode(s string) (Mode, error) {
	m := Mode(strings.ToLower(strings.TrimSpace(s)))
	if m == "" {
		return ModeAuto, nil
	}
	if !m.Valid() {
		return "", fmt.Errorf("invalid proxy_mode %q (expected auto, off or custom)", s)
	}
	return m, nil
}

// Next/Prev возвращают соседний режим по кругу — для ←/→ в TUI.
func (m Mode) Next() Mode { return cycleMode(m, 1) }

func (m Mode) Prev() Mode { return cycleMode(m, -1) }

func cycleMode(m Mode, dir int) Mode {
	for i, v := range Modes {
		if v == m {
			return Modes[(i+dir+len(Modes))%len(Modes)]
		}
	}
	return Modes[0]
}

// Settings — настройки прокси для всех сетевых клиентов dmsh.
type Settings struct {
	// Mode выбирает источник адреса прокси.
	Mode Mode
	// Proto — протокол прокси: http, https, socks5 или socks5h.
	Proto string
	// Host — адрес прокси: имя или IP.
	Host string
	// Port — порт; 0 означает «не указан», порт по умолчанию не
	// подставляется: угадывать его опаснее, чем дать соединению упасть.
	Port int
	// User и Password — логин и пароль, если прокси их требует.
	User     string
	Password string
	// NoProxy — список хостов через запятую, которые обходят прокси:
	// "localhost, 127.0.0.1, .corp.example, 10.0.0.0/8".
	NoProxy string
	// URL — полный адрес одним куском. Это вход: Normalize() раскладывает
	// его по полям, чтобы дальше везде работали только поля.
	URL string
}

// Normalize приводит настройки к каноничному виду: пустой режим означает
// auto, протокол по умолчанию http, а полный адрес раскладывается по
// полям. Режим custom без хоста НЕ схлопывается в auto: это заведомо
// сломанная настройка, и молча уйти мимо прокси хуже, чем сказать об
// ошибке — её поймает Validate.
func (s Settings) Normalize() Settings {
	s.Mode = Mode(strings.ToLower(strings.TrimSpace(string(s.Mode))))
	s.Proto = strings.ToLower(strings.TrimSpace(s.Proto))
	s.Host = strings.ToLower(strings.TrimSpace(s.Host))
	s.User = strings.TrimSpace(s.User)
	s.NoProxy = strings.TrimSpace(s.NoProxy)
	if s.Mode == "" {
		s.Mode = ModeAuto
	}
	if s.Proto == "" {
		s.Proto = ProtoHTTP
	}
	if strings.TrimSpace(s.URL) != "" {
		if s.Host == "" {
			if err := s.ApplyURL(s.URL); err == nil {
				s.URL = ""
			}
		} else {
			// Поля уже заданы — они и есть источник истины.
			s.URL = ""
		}
	}
	return s
}

// ApplyURL разбирает полный адрес и заполняет поля. Схема необязательна:
// "127.0.0.1:8080" — это http, а "socks5://10.0.0.1:1080" — socks5.
func (s *Settings) ApplyURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return errors.New("proxy address is empty")
	}
	body := raw
	if !strings.Contains(raw, "://") {
		body = ProtoHTTP + "://" + raw
	}
	u, err := url.Parse(body)
	if err != nil {
		return fmt.Errorf("invalid proxy address %q: %w", raw, err)
	}
	proto := strings.ToLower(u.Scheme)
	if !ValidProto(proto) {
		return fmt.Errorf("proxy protocol %q is not supported (expected http, https, socks5 or socks5h)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("proxy address %q has no host", raw)
	}
	port := 0
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil {
			return fmt.Errorf("invalid proxy port %q in %q", p, raw)
		}
		port = n
	}
	user, password := "", ""
	if u.User != nil {
		user = u.User.Username()
		password, _ = u.User.Password()
	}

	s.Proto = proto
	s.Host = strings.ToLower(host)
	s.Port = port
	s.User = user
	s.Password = password
	s.URL = ""
	return nil
}

// Address собирает адрес из полей. Пустая строка — хост не задан. Пароль в
// адресе есть, поэтому печатать его напрямую нельзя — только через Redact.
func (s Settings) Address() string {
	if strings.TrimSpace(s.URL) != "" {
		return strings.TrimSpace(s.URL)
	}
	if strings.TrimSpace(s.Host) == "" {
		return ""
	}
	host := s.Host
	if s.Port > 0 {
		host = net.JoinHostPort(host, strconv.Itoa(s.Port))
	}
	addr := s.Proto + "://" + host
	if s.User != "" || s.Password != "" {
		addr = s.Proto + "://" + url.UserPassword(s.User, s.Password).String() + "@" + host
	}
	return addr
}

// ProxyURL разбирает адрес режима custom.
func (s Settings) ProxyURL() (*url.URL, error) {
	s = s.Normalize()
	raw := s.Address()
	if raw == "" {
		return nil, fmt.Errorf("proxy_host is required when proxy_mode=custom")
	}
	if !strings.Contains(raw, "://") {
		raw = ProtoHTTP + "://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy address %q: %w", raw, err)
	}
	switch strings.ToLower(u.Scheme) {
	case ProtoHTTP, ProtoHTTPS, ProtoSOCKS5, ProtoSOCKS5H:
	default:
		return nil, fmt.Errorf("proxy protocol %q is not supported (expected http, https, socks5 or socks5h)", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("proxy address %q has no host", raw)
	}
	return u, nil
}

// Validate проверяет настройки и разбирает список исключений. Протокол и
// порт проверяются всегда, даже вне режима custom: опечатка должна
// обнаруживаться при вводе, а не через час на первом запросе.
func (s Settings) Validate() error {
	s = s.Normalize()
	if !s.Mode.Valid() {
		return fmt.Errorf("invalid proxy_mode %q (expected auto, off or custom)", s.Mode)
	}
	if !ValidProto(s.Proto) {
		return fmt.Errorf("invalid proxy_proto %q (expected http, https, socks5 or socks5h)", s.Proto)
	}
	if s.Port < 0 || s.Port > 65535 {
		return fmt.Errorf("invalid proxy_port %d (expected 0-65535)", s.Port)
	}
	if s.Mode == ModeCustom {
		if _, err := s.ProxyURL(); err != nil {
			return err
		}
	}
	if _, err := newBypass(s.NoProxy); err != nil {
		return err
	}
	return nil
}

// Resolver возвращает функцию выбора прокси для запроса — ту самую, что
// http.Transport ждёт в поле Proxy. Её же использует Effective для
// показа того, что реально уйдёт в сеть.
func (s Settings) Resolver() (func(*http.Request) (*url.URL, error), error) {
	s = s.Normalize()
	bypass, err := newBypass(s.NoProxy)
	if err != nil {
		return nil, err
	}

	switch s.Mode {
	case ModeOff:
		// nil означает «соединение напрямую» и не даёт транспорту
		// подставить переменные окружения.
		return func(*http.Request) (*url.URL, error) { return nil, nil }, nil
	case ModeCustom:
		proxyURL, err := s.ProxyURL()
		if err != nil {
			return nil, err
		}
		return func(req *http.Request) (*url.URL, error) {
			if bypass.match(requestHost(req)) {
				return nil, nil
			}
			return proxyURL, nil
		}, nil
	default:
		return func(req *http.Request) (*url.URL, error) {
			if bypass.match(requestHost(req)) {
				return nil, nil
			}
			return http.ProxyFromEnvironment(req)
		}, nil
	}
}

// Transport возвращает копию стандартного транспорта с учётом прокси.
// Таймауты и пул соединений остаются дефолтными: стриминг ответа не
// должен обрываться по ResponseHeaderTimeout.
func (s Settings) Transport() (*http.Transport, error) {
	resolver, err := s.Resolver()
	if err != nil {
		return nil, err
	}
	tr, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("unexpected default transport %T", http.DefaultTransport)
	}
	clone := tr.Clone()
	clone.Proxy = resolver
	return clone, nil
}

// Client создаёт HTTP-клиент с таймаутом на весь запрос.
func (s Settings) Client(timeout time.Duration) (*http.Client, error) {
	tr, err := s.Transport()
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: tr, Timeout: timeout}, nil
}

// Effective возвращает адрес прокси, который будет использован для запроса
// на rawURL. Пустая строка означает прямое соединение.
func (s Settings) Effective(rawURL string) (string, error) {
	resolver, err := s.Resolver()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(rawURL) == "" {
		return "", nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid url %q: %w", rawURL, err)
	}
	proxyURL, err := resolver(&http.Request{URL: u})
	if err != nil || proxyURL == nil {
		return "", err
	}
	return Redact(proxyURL.String()), nil
}

// Enabled сообщает, настроен ли прокси вообще. Пользователю нужно это до
// первой ошибки соединения: в режиме auto адрес берётся из окружения, и
// «прокси нет» на самом деле означает «прокси есть, но не виден».
func (s Settings) Enabled() bool {
	s = s.Normalize()
	switch s.Mode {
	case ModeOff:
		return false
	case ModeCustom:
		return strings.TrimSpace(s.Host) != ""
	default:
		return envProxy() != ""
	}
}

// Describe — короткое описание режима для интерфейса. Пароль убирается:
// описание печатается на экран и попадает в вывод сессии.
func (s Settings) Describe() string {
	s = s.Normalize()
	switch s.Mode {
	case ModeOff:
		return "direct connection, proxy ignored"
	case ModeCustom:
		return Redact(s.Address())
	default:
		if v := envProxy(); v != "" {
			return "from environment: " + Redact(v)
		}
		return "no proxy in environment (direct connection)"
	}
}

// envProxy показывает, что реально задано в окружении: режим auto молча
// выглядит как «прокси нет», а прокси может быть в HTTPS_PROXY.
func envProxy() string {
	for _, key := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}

// Redact убирает логин и пароль из адреса прокси. Используется везде,
// где адрес показывается пользователю: пароль из URL — секрет, даже если
// он уже лежит в конфиге.
func Redact(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.User == nil || raw == "" {
		return raw
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host}).String()
}

// MaskedPassword показывает, что пароль задан, не показывая его. В
// настройках пароль виден только при вводе.
func (s Settings) MaskedPassword() string {
	if s.Password == "" {
		return ""
	}
	return strings.Repeat("*", 8)
}

// requestHost возвращает host[:port] запроса в том виде, в каком его
// ждёт список исключений.
func requestHost(req *http.Request) string {
	if req == nil || req.URL == nil {
		return ""
	}
	return req.URL.Host
}

// bypass — список исключений из NoProxy: хосты, поддомены, host:port и
// CIDR. Семантика повторяет NO_PROXY: запись с портом действует только на
// этот порт, точка в начале — на поддомены.
type bypass struct {
	all     bool
	hosts   map[string]bool
	hostpor map[string]bool
	suffix  []string
	cidrs   []*net.IPNet
}

func newBypass(list string) (*bypass, error) {
	b := &bypass{hosts: map[string]bool{}, hostpor: map[string]bool{}}
	for _, raw := range strings.Split(list, ",") {
		entry := strings.ToLower(strings.TrimSpace(raw))
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "://") {
			return nil, fmt.Errorf("no_proxy entry %q must be a host, host:port or CIDR without a scheme", raw)
		}
		if strings.ContainsAny(entry, " \t") {
			return nil, fmt.Errorf("no_proxy entry %q must not contain spaces", raw)
		}
		switch {
		case entry == "*":
			b.all = true
		case strings.Contains(entry, "/"):
			_, network, err := net.ParseCIDR(entry)
			if err != nil {
				return nil, fmt.Errorf("no_proxy entry %q is not a valid CIDR: %w", raw, err)
			}
			b.cidrs = append(b.cidrs, network)
		case strings.HasPrefix(entry, "."):
			b.suffix = append(b.suffix, strings.TrimPrefix(entry, "."))
		default:
			if host, port, err := net.SplitHostPort(entry); err == nil && host != "" && port != "" {
				b.hostpor[entry] = true
				continue
			}
			b.hosts[entry] = true
		}
	}
	return b, nil
}

// match сообщает, что хост обходит прокси. Пустой хост не совпадает ни с
// чем: неизвестный адрес лучше пустить через прокси, чем увести мимо.
func (b *bypass) match(hostport string) bool {
	if b == nil {
		return false
	}
	h := strings.ToLower(strings.TrimSpace(hostport))
	if h == "" {
		return false
	}
	if b.all || b.hostpor[h] {
		return true
	}
	host := h
	if hh, _, err := net.SplitHostPort(h); err == nil {
		host = hh
	}
	host = strings.TrimSuffix(host, ".")
	if b.hosts[host] {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		for _, n := range b.cidrs {
			if n.Contains(ip) {
				return true
			}
		}
	}
	for _, s := range b.suffix {
		if host == s || strings.HasSuffix(host, "."+s) {
			return true
		}
	}
	return false
}