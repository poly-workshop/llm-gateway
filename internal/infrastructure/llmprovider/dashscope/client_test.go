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

func TestProvider_StreamChatCompletion(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer testkey" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			t.Fatalf("unexpected accept header: %q", got)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req["model"] != "qwen-turbo" {
			t.Fatalf("unexpected model: %#v", req["model"])
		}
		if req["stream"] != true {
			t.Fatalf("expected stream=true, got: %#v", req["stream"])
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		// Send first chunk with role
		_, _ = w.Write([]byte(`data: {"id":"chatcmpl_stream","object":"chat.completion.chunk","created":123,"model":"qwen-turbo","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}` + "\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		// Send second chunk with content
		_, _ = w.Write([]byte(`data: {"id":"chatcmpl_stream","object":"chat.completion.chunk","created":123,"model":"qwen-turbo","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}` + "\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		// Send final chunk with finish_reason
		_, _ = w.Write([]byte(`data: {"id":"chatcmpl_stream","object":"chat.completion.chunk","created":123,"model":"qwen-turbo","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		// Send [DONE] marker
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(srv.Close)

	p := NewProvider(srv.URL, "testkey", 2*time.Second)
	stream, err := p.StreamChatCompletion(context.Background(), llm.ChatCompletionRequest{
		Model: "qwen-turbo",
		Messages: []llm.ChatMessage{
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("StreamChatCompletion error: %v", err)
	}
	defer stream.Close()

	// Read first chunk
	chunk1, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv chunk1 error: %v", err)
	}
	if chunk1.ID != "chatcmpl_stream" || chunk1.Model != "qwen-turbo" {
		t.Fatalf("unexpected chunk1: %+v", chunk1)
	}
	if len(chunk1.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(chunk1.Choices))
	}
	if chunk1.Choices[0].Delta.Role != "assistant" || chunk1.Choices[0].Delta.Content != "Hello" {
		t.Fatalf("unexpected chunk1 delta: %+v", chunk1.Choices[0].Delta)
	}

	// Read second chunk
	chunk2, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv chunk2 error: %v", err)
	}
	if chunk2.Choices[0].Delta.Content != " world" {
		t.Fatalf("unexpected chunk2 delta: %+v", chunk2.Choices[0].Delta)
	}

	// Read third chunk
	chunk3, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv chunk3 error: %v", err)
	}
	if chunk3.Choices[0].FinishReason != "stop" {
		t.Fatalf("unexpected chunk3 finish_reason: %+v", chunk3.Choices[0].FinishReason)
	}

	// Read [DONE] marker
	_, err = stream.Recv()
	if err == nil {
		t.Fatalf("expected EOF after [DONE], got nil")
	}
	// io.EOF is expected here
}
