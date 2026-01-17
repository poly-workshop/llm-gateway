package httpgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/poly-workshop/llm-gateway/internal/application/llmgateway"
	"github.com/poly-workshop/llm-gateway/internal/domain/llm"
)

// mockProvider implements llmgateway.Provider for testing
type mockProvider struct {
	createChatCompletionFunc func(req llm.ChatCompletionRequest) (llm.ChatCompletionResponse, error)
	streamChatCompletionFunc func(req llm.ChatCompletionRequest) (llm.ChatCompletionStream, error)
}

func (m *mockProvider) CreateChatCompletion(_ context.Context, req llm.ChatCompletionRequest) (llm.ChatCompletionResponse, error) {
	if m.createChatCompletionFunc != nil {
		return m.createChatCompletionFunc(req)
	}
	return llm.ChatCompletionResponse{
		ID:      "chatcmpl-test",
		Created: 1234567890,
		Model:   req.Model,
		Choices: []llm.ChatCompletionChoice{
			{
				Index: 0,
				Message: llm.ChatMessage{
					Role:    "assistant",
					Content: "Test response",
				},
				FinishReason: "stop",
			},
		},
		Usage: llm.TokenUsage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}, nil
}

func (m *mockProvider) CreateEmbeddings(_ context.Context, req llm.EmbeddingsRequest) (llm.EmbeddingsResponse, error) {
	return llm.EmbeddingsResponse{}, nil
}

func (m *mockProvider) StreamChatCompletion(_ context.Context, req llm.ChatCompletionRequest) (llm.ChatCompletionStream, error) {
	if m.streamChatCompletionFunc != nil {
		return m.streamChatCompletionFunc(req)
	}
	return &mockStream{
		chunks: []llm.ChatCompletionChunk{
			{
				ID:      "chatcmpl-test-stream",
				Object:  "chat.completion.chunk",
				Created: 1234567890,
				Model:   req.Model,
				Choices: []llm.ChatCompletionChunkChoice{
					{
						Index: 0,
						Delta: llm.ChatMessageDelta{
							Role:    "assistant",
							Content: "Test",
						},
					},
				},
			},
			{
				ID:      "chatcmpl-test-stream",
				Object:  "chat.completion.chunk",
				Created: 1234567890,
				Model:   req.Model,
				Choices: []llm.ChatCompletionChunkChoice{
					{
						Index: 0,
						Delta: llm.ChatMessageDelta{
							Content: " streaming",
						},
					},
				},
			},
			{
				ID:      "chatcmpl-test-stream",
				Object:  "chat.completion.chunk",
				Created: 1234567890,
				Model:   req.Model,
				Choices: []llm.ChatCompletionChunkChoice{
					{
						Index: 0,
						Delta: llm.ChatMessageDelta{
							Content: " response",
						},
						FinishReason: "stop",
					},
				},
			},
		},
	}, nil
}

type mockStream struct {
	chunks []llm.ChatCompletionChunk
	index  int
}

func (s *mockStream) Recv() (llm.ChatCompletionChunk, error) {
	if s.index >= len(s.chunks) {
		return llm.ChatCompletionChunk{}, io.EOF
	}
	chunk := s.chunks[s.index]
	s.index++
	return chunk, nil
}

func (s *mockStream) Close() error {
	return nil
}

func TestMaxTokens_ExceedsLimit(t *testing.T) {
	// Setup: Create a service with a model that has max_output_tokens = 100
	provider := &mockProvider{}
	providers := map[string]llmgateway.Provider{
		"test": provider,
	}
	models := []llmgateway.ModelSpec{
		{
			ID:              "test/model-limited",
			Name:            "model-limited",
			Provider:        "test",
			Capabilities:    []string{"text"},
			MaxOutputTokens: 100,
		},
	}
	appSvc := llmgateway.NewService(providers, models, nil, nil)
	srv := &Server{app: appSvc}

	// Test: Request with max_tokens > limit (101 > 100)
	reqBody := CreateChatCompletionRequest{
		Model: "test/model-limited",
		Messages: []ChatMessageIn{
			{
				Role:    "user",
				Content: json.RawMessage(`"Hello"`),
			},
		},
		MaxTokens: 101,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleCreateChatCompletion(w, req)

	// Assert: Should return 400 with OpenAI-compatible error
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var errResp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	errObj, ok := errResp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected error object in response")
	}

	if errType, ok := errObj["type"].(string); !ok || errType != "invalid_request_error" {
		t.Errorf("expected error type 'invalid_request_error', got %v", errObj["type"])
	}

	msg, ok := errObj["message"].(string)
	if !ok {
		t.Fatalf("expected error message in response")
	}
	// The error message should be clean without "invalid argument: " prefix
	expectedMsg := "max_tokens (101) exceeds model limit (100)"
	if msg != expectedMsg {
		t.Errorf("expected error message '%s', got: %s", expectedMsg, msg)
	}
}

func TestMaxTokens_WithinLimit(t *testing.T) {
	// Setup: Create a service with a model that has max_output_tokens = 100
	provider := &mockProvider{}
	providers := map[string]llmgateway.Provider{
		"test": provider,
	}
	models := []llmgateway.ModelSpec{
		{
			ID:              "test/model-limited",
			Name:            "model-limited",
			Provider:        "test",
			Capabilities:    []string{"text"},
			MaxOutputTokens: 100,
		},
	}
	appSvc := llmgateway.NewService(providers, models, nil, nil)
	srv := &Server{app: appSvc}

	// Test: Request with max_tokens <= limit (50 <= 100)
	reqBody := CreateChatCompletionRequest{
		Model: "test/model-limited",
		Messages: []ChatMessageIn{
			{
				Role:    "user",
				Content: json.RawMessage(`"Hello"`),
			},
		},
		MaxTokens: 50,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleCreateChatCompletion(w, req)

	// Assert: Should return 200
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMaxTokens_NoLimitConfigured(t *testing.T) {
	// Setup: Create a service with a model that has max_output_tokens = 0 (no limit)
	provider := &mockProvider{}
	providers := map[string]llmgateway.Provider{
		"test": provider,
	}
	models := []llmgateway.ModelSpec{
		{
			ID:              "test/model-unlimited",
			Name:            "model-unlimited",
			Provider:        "test",
			Capabilities:    []string{"text"},
			MaxOutputTokens: 0, // No limit
		},
	}
	appSvc := llmgateway.NewService(providers, models, nil, nil)
	srv := &Server{app: appSvc}

	// Test: Request with large max_tokens
	reqBody := CreateChatCompletionRequest{
		Model: "test/model-unlimited",
		Messages: []ChatMessageIn{
			{
				Role:    "user",
				Content: json.RawMessage(`"Hello"`),
			},
		},
		MaxTokens: 10000,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleCreateChatCompletion(w, req)

	// Assert: Should return 200 (no limit enforced)
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMaxTokens_MissingMaxTokens(t *testing.T) {
	// Setup: Create a service with a model that has max_output_tokens = 100
	provider := &mockProvider{}
	providers := map[string]llmgateway.Provider{
		"test": provider,
	}
	models := []llmgateway.ModelSpec{
		{
			ID:              "test/model-limited",
			Name:            "model-limited",
			Provider:        "test",
			Capabilities:    []string{"text"},
			MaxOutputTokens: 100,
		},
	}
	appSvc := llmgateway.NewService(providers, models, nil, nil)
	srv := &Server{app: appSvc}

	// Test: Request without max_tokens (0 means use provider's default)
	reqBody := CreateChatCompletionRequest{
		Model: "test/model-limited",
		Messages: []ChatMessageIn{
			{
				Role:    "user",
				Content: json.RawMessage(`"Hello"`),
			},
		},
		MaxTokens: 0, // 0 = use provider default, not enforced by gateway
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleCreateChatCompletion(w, req)

	// Assert: Should return 200 (allow provider default)
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}
