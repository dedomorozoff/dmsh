package llm

import "testing"

// Запрос уходит на /chat/completions: база задаётся так же, как в OpenAI
// SDK (https://gen.pollinations.ai/v1, http://127.0.0.1:11434/v1), а сам
// маршрут дописывает клиент. Уже готовый адрес не дописывается дважды.
func TestChatURL(t *testing.T) {
	cases := map[string]string{
		"https://gen.pollinations.ai/v1":               "https://gen.pollinations.ai/v1/chat/completions",
		"https://gen.pollinations.ai/v1/":              "https://gen.pollinations.ai/v1/chat/completions",
		"http://127.0.0.1:11434/v1":                    "http://127.0.0.1:11434/v1/chat/completions",
		"https://text.pollinations.ai/openai":          "https://text.pollinations.ai/openai/chat/completions",
		"https://api.example.com/v1/chat/completions":  "https://api.example.com/v1/chat/completions",
		"https://api.example.com/v1/chat/completions/": "https://api.example.com/v1/chat/completions",
		"http://10.0.0.5:8080":                         "http://10.0.0.5:8080/chat/completions",
	}
	for base, want := range cases {
		if got := chatURL(base); got != want {
			t.Errorf("chatURL(%q) = %q, want %q", base, got, want)
		}
	}
}
