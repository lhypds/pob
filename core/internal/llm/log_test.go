package llm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pob/core/internal/applog"
)

// fakeSettings is a machine configured however a test needs it.
type fakeSettings struct {
	key, url, model string
	in, out         float64
}

func (f fakeSettings) APIKey() string            { return f.key }
func (f fakeSettings) BaseURL() string           { return f.url }
func (f fakeSettings) Model() string             { return f.model }
func (f fakeSettings) InputPricePer1M() float64  { return f.in }
func (f fakeSettings) OutputPricePer1M() float64 { return f.out }

// readLLMLog points applog at a fresh root, runs the call, and gives back what
// landed in llm.log.
func readLLMLog(t *testing.T, run func()) string {
	t.Helper()
	root := t.TempDir()
	applog.Init(root)
	run()
	data, err := os.ReadFile(filepath.Join(root, "llm.log"))
	if err != nil {
		t.Fatalf("llm.log was not written: %v", err)
	}
	return string(data)
}

// stubProvider answers like an OpenAI-compatible endpoint, with the usage block
// it is given.
func stubProvider(t *testing.T, usage map[string]any) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"role": "assistant", "content": `{"value":"-120"}`},
			}},
			"usage": usage,
		})
	}))
	t.Cleanup(server.Close)
	return server
}

func mustContain(t *testing.T, log string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(log, want) {
			t.Errorf("llm.log has no %q in:\n%s", want, log)
		}
	}
}

// Every request writes a block naming what it was for, where it went, what it
// carried and what came back.
func TestLLMLogRecordsTheCall(t *testing.T) {
	usage := map[string]any{
		"prompt_tokens":             1843.0,
		"completion_tokens":         27.0,
		"total_tokens":              1870.0,
		"prompt_tokens_details":     map[string]any{"cached_tokens": 512.0},
		"completion_tokens_details": map[string]any{"reasoning_tokens": 8.0},
	}
	server := stubProvider(t, usage)

	log := readLLMLog(t, func() {
		client := New(fakeSettings{key: "sk-test", url: server.URL, model: "gpt-5.6"})
		messages := []map[string]any{
			{"role": "system", "content": "you are a thing"},
			{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "what is the offset?"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,AAAA"}},
			}},
		}
		if result := client.Chat("macro slot ::the x offset::", messages, nil, map[string]any{"type": "object"}); !result.Success {
			t.Fatalf("Chat failed: %s", result.Error)
		}
	})

	mustContain(t, log,
		"macro slot ::the x offset::",
		server.URL+"/chat/completions",
		"model      gpt-5.6",
		"2 messages, 1 image, json_schema",
		"status     ok",
		"1870 tokens = 1843 prompt (512 cached) + 27 completion (8 reasoning)",
		`{"value":"-120"}`,
		"duration",
	)
}

// With prices set, the block says what the call cost.
func TestLLMLogPricesTheCall(t *testing.T) {
	server := stubProvider(t, map[string]any{
		"prompt_tokens":     1_000_000.0,
		"completion_tokens": 500_000.0,
		"total_tokens":      1_500_000.0,
	})

	log := readLLMLog(t, func() {
		client := New(fakeSettings{key: "sk-test", url: server.URL, model: "gpt-5.6", in: 2.5, out: 10})
		client.Chat("a call", []map[string]any{{"role": "user", "content": "hi"}}, nil, nil)
	})

	// 1M prompt at $2.50/M + 0.5M completion at $10/M = $2.50 + $5.00.
	mustContain(t, log, "$7.500000 estimated", "in 1000000 × $2.5/M", "out 500000 × $10/M")
}

// A provider that reports what it charged is believed over any arithmetic here:
// its number is the one on the bill.
func TestLLMLogPrefersTheProvidersCost(t *testing.T) {
	server := stubProvider(t, map[string]any{
		"prompt_tokens":     100.0,
		"completion_tokens": 10.0,
		"cost":              0.001234,
	})

	log := readLLMLog(t, func() {
		client := New(fakeSettings{key: "sk-test", url: server.URL, model: "m", in: 999, out: 999})
		client.Chat("a call", []map[string]any{{"role": "user", "content": "hi"}}, nil, nil)
	})

	mustContain(t, log, "$0.001234 reported by the provider")
}

// With no prices anywhere, the tokens are still logged — that is what a bill is
// worked out from — and the line says what to set to see money.
func TestLLMLogWithoutPrices(t *testing.T) {
	server := stubProvider(t, map[string]any{"prompt_tokens": 10.0, "completion_tokens": 2.0})

	log := readLLMLog(t, func() {
		client := New(fakeSettings{key: "sk-test", url: server.URL, model: "m"})
		client.Chat("a call", []map[string]any{{"role": "user", "content": "hi"}}, nil, nil)
	})

	mustContain(t, log, "12 tokens = 10 prompt", "set price_input_per_1m and price_output_per_1m")
}

// A call that failed is a call someone will come looking for, so it is logged
// too — including the one that never left the machine for want of a key.
func TestLLMLogRecordsFailures(t *testing.T) {
	t.Run("the provider refused", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limit"}}`))
		}))
		defer server.Close()

		log := readLLMLog(t, func() {
			client := New(fakeSettings{key: "sk-test", url: server.URL, model: "m"})
			client.Chat("a call", []map[string]any{{"role": "user", "content": "hi"}}, nil, nil)
		})
		mustContain(t, log, "status     failed", "HTTP 429", "rate limit")
	})

	t.Run("no API key", func(t *testing.T) {
		log := readLLMLog(t, func() {
			client := New(fakeSettings{url: "http://example.invalid", model: "m"})
			client.Chat("a call", []map[string]any{{"role": "user", "content": "hi"}}, nil, nil)
		})
		mustContain(t, log, "status     failed", "API key not configured")
	})
}

// An error or an answer with newlines in it stays on its own line, so the block
// keeps its shape.
func TestLLMLogKeepsBlocksOnOneLinePerField(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("something\nbroke\nacross lines"))
	}))
	defer server.Close()

	log := readLLMLog(t, func() {
		client := New(fakeSettings{key: "sk-test", url: server.URL, model: "m"})
		client.Chat("a call", []map[string]any{{"role": "user", "content": "hi"}}, nil, nil)
	})

	for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
		if strings.HasPrefix(line, "  error") && !strings.Contains(line, "something broke across lines") {
			t.Errorf("the error was not flattened onto its field: %q", line)
		}
	}
	if strings.Count(log, "\n  error") != 1 {
		t.Errorf("the error spans more than its own line:\n%s", log)
	}
}
