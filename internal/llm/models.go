package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// modelListTimeout ограничивает запрос каталога: список должен прийти быстро
// или не прийти совсем, а висеть он не должен.
const modelListTimeout = 15 * time.Second

// RemoteModel — одна позиция каталога моделей удалённого провайдера.
type RemoteModel struct {
	// ID уходит в поле model запроса: у Ollama это «qwen3:8b», у Pollinations
	// «openai/gpt-5.4-nano».
	ID string
	// Note — необязательная подпись провайдера (владелец, размер). Пустая
	// строка ничего не добавляет.
	Note string
}

// maxRemoteModels ограничивает каталог: у Pollinations их сотни, а меню
// рисуется целиком в каждом кадре.
const maxRemoteModels = 500

// ListRemoteModels возвращает каталог моделей провайдера по его
// OpenAI-совместимому маршруту /models.
//
// Ключ в запрос не добавляется: по документации и Pollinations, и Ollama
// список моделей открыт, а лишний заголовок с секретом в запросе, который
// ничего не защищает, только расширяет утечку.
func ListRemoteModels(p Params) ([]RemoteModel, error) {
	if p.Provider == "" || p.Provider == ProviderLocal || p.Provider == ProviderAuto {
		return nil, fmt.Errorf("llm: provider %q has no remote model catalog", p.Provider)
	}
	base := remoteBase(p)
	client, err := p.Proxy.Client(modelListTimeout)
	if err != nil {
		return nil, fmt.Errorf("llm: proxy: %w", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, routeURL(base, "/models"), nil)
	if err != nil {
		return nil, fmt.Errorf("llm: new request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm: %s model list from %s failed: %w%s",
			remoteProviderName(p.Provider), base, err, transportHint(remoteProviderName(p.Provider)))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxRemoteErrorBody))
		msg := strings.TrimSpace(string(snippet))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return nil, fmt.Errorf("llm: %s model list HTTP %d: %s",
			remoteProviderName(p.Provider), resp.StatusCode, msg)
	}

	var parsed struct {
		Data []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("llm: decode model list: %w", err)
	}

	// Провайдеры отдают и id, и name (Ollama — оба, у Pollinations id с
	// publisher). Порядок сервера произвольный, поэтому сортируем: список
	// длинный, а искать его глазами нельзя.
	out := make([]RemoteModel, 0, len(parsed.Data))
	seen := make(map[string]bool, len(parsed.Data))
	for _, it := range parsed.Data {
		id := strings.TrimSpace(it.ID)
		if id == "" {
			id = strings.TrimSpace(it.Name)
		}
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, RemoteModel{ID: id, Note: strings.TrimSpace(it.OwnedBy)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if len(out) > maxRemoteModels {
		out = out[:maxRemoteModels]
	}
	return out, nil
}

// remoteBase возвращает адрес провайдера с учётом его собственного дефолта:
// список моделей у Ollama и у Pollinations живёт по разным адресам.
func remoteBase(p Params) string {
	if base := strings.TrimSpace(p.RemoteBaseURL); base != "" {
		return strings.TrimRight(base, "/")
	}
	if p.Provider == ProviderOllama {
		return DefaultOllamaBaseURL
	}
	return DefaultPollinationsBaseURL
}

// remoteProviderName — имя провайдера для текста ошибок.
func remoteProviderName(p Provider) string {
	if p == ProviderOllama {
		return remoteProviderOllama
	}
	return remoteProviderPollinations
}
