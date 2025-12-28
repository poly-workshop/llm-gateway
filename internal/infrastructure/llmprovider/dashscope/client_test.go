package dashscope

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/poly-workshop/llm-gateway/internal/domain/llm"
)

func TestProvider_CreateChatCompletion(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer testkey" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req["model"] != "qwen-turbo" {
			t.Fatalf("unexpected model: %#v", req["model"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "id":"chatcmpl_x",
  "created": 123,
  "model":"qwen-turbo",
  "choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],
  "usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}
}`))
	}))
	t.Cleanup(srv.Close)

	p := NewProvider(srv.URL, "testkey", 2*time.Second)
	res, err := p.CreateChatCompletion(context.Background(), llm.ChatCompletionRequest{
		Model: "qwen-turbo",
		Messages: []llm.ChatMessage{
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("CreateChatCompletion error: %v", err)
	}
	if res.ID != "chatcmpl_x" || res.Model != "qwen-turbo" {
		t.Fatalf("unexpected response: %+v", res)
	}
	if len(res.Choices) != 1 || res.Choices[0].Message.Content != "hi" {
		t.Fatalf("unexpected choices: %+v", res.Choices)
	}
	if res.Usage.TotalTokens != 3 {
		t.Fatalf("unexpected usage: %+v", res.Usage)
	}
}

func TestProvider_CreateEmbeddings(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer testkey" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req["model"] != "text-embedding-v3" {
			t.Fatalf("unexpected model: %#v", req["model"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "model":"text-embedding-v3",
  "data":[{"index":0,"embedding":[0.1,0.2]}]
}`))
	}))
	t.Cleanup(srv.Close)

	p := NewProvider(srv.URL, "testkey", 2*time.Second)
	res, err := p.CreateEmbeddings(context.Background(), llm.EmbeddingsRequest{
		Model: "text-embedding-v3",
		Input: []string{"hello"},
	})
	if err != nil {
		t.Fatalf("CreateEmbeddings error: %v", err)
	}
	if res.Model != "text-embedding-v3" || len(res.Data) != 1 || len(res.Data[0].Vector) != 2 {
		t.Fatalf("unexpected response: %+v", res)
	}
}

