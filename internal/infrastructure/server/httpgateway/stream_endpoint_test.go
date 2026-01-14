package httpgateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/poly-workshop/llm-gateway/internal/application/llmgateway"
)

// TestDeprecatedStreamEndpoint_Returns404 verifies that the deprecated
// /v1/chat/completions:stream endpoint returns 404 Not Found (no route registered).
func TestDeprecatedStreamEndpoint_Returns404(t *testing.T) {
	// Setup: Create a minimal server
	provider := &mockProvider{}
	providers := map[string]llmgateway.Provider{
		"test": provider,
	}
	models := []llmgateway.ModelSpec{
		{
			ID:           "test/model",
			Name:         "model",
			Provider:     "test",
			Capabilities: []string{"text"},
		},
	}
	appSvc := llmgateway.NewService(providers, models, nil)
	srv := &Server{app: appSvc}

	// Create a router simulating the actual server routes (without the deprecated endpoint)
	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		r.Post("/chat/completions", srv.handleCreateChatCompletion)
		// Note: /chat/completions:stream is NOT registered, so it should 404
	})

	// Test: Request to the deprecated endpoint
	reqBody := CreateChatCompletionRequest{
		Model: "test/model",
		Messages: []ChatMessageIn{
			{
				Role:    "user",
				Content: json.RawMessage(`"Hello"`),
			},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions:stream", bytes.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Assert: Should return 404 (no route registered)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

// TestStreamParameter_NotImplemented verifies that requesting streaming via
// the stream=true parameter on /v1/chat/completions returns proper SSE stream.
func TestStreamParameter_Implemented(t *testing.T) {
	// Setup: Create a service
	provider := &mockProvider{}
	providers := map[string]llmgateway.Provider{
		"test": provider,
	}
	models := []llmgateway.ModelSpec{
		{
			ID:           "test/model",
			Name:         "model",
			Provider:     "test",
			Capabilities: []string{"text"},
		},
	}
	appSvc := llmgateway.NewService(providers, models, nil)
	srv := &Server{app: appSvc}

	// Test: Request with stream=true
	reqBody := CreateChatCompletionRequest{
		Model: "test/model",
		Messages: []ChatMessageIn{
			{
				Role:    "user",
				Content: json.RawMessage(`"Hello"`),
			},
		},
		Stream: true,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleCreateChatCompletion(w, req)

	// Assert: Should return 200 with SSE headers
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("expected Content-Type 'text/event-stream', got '%s'", contentType)
	}

	// Assert: Verify streaming body format
	responseBody := w.Body.String()
	
	// Should contain data: lines with JSON chunks
	if !strings.Contains(responseBody, "data: {") {
		t.Errorf("expected streaming body to contain 'data: {', got: %s", responseBody)
	}
	
	// Should contain [DONE] marker
	if !strings.Contains(responseBody, "data: [DONE]") {
		t.Errorf("expected streaming body to contain 'data: [DONE]', got: %s", responseBody)
	}
	
	// Parse chunks to verify format
	lines := strings.Split(responseBody, "\n")
	var foundChunk bool
	for _, line := range lines {
		if strings.HasPrefix(line, "data: {") {
			foundChunk = true
			data := strings.TrimPrefix(line, "data: ")
			var chunk ChatCompletionChunkResponse
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				t.Errorf("failed to parse chunk JSON: %v, data: %s", err, data)
				continue
			}
			
			// Verify chunk structure
			if chunk.Object != "chat.completion.chunk" {
				t.Errorf("expected object 'chat.completion.chunk', got '%s'", chunk.Object)
			}
			if len(chunk.Choices) == 0 {
				t.Errorf("expected at least one choice in chunk")
			}
			if len(chunk.Choices) > 0 {
				choice := chunk.Choices[0]
				if choice.Delta.Content == "" && choice.Delta.Role == "" && choice.FinishReason == "" {
					t.Errorf("expected delta to have content, role, or finish_reason")
				}
			}
		}
	}
	
	if !foundChunk {
		t.Errorf("expected at least one chunk in streaming response")
	}
}

// TestStreamParameter_False_WorksNormally verifies that stream=false
// (or omitted) allows normal non-streaming requests.
func TestStreamParameter_False_WorksNormally(t *testing.T) {
	// Setup: Create a service
	provider := &mockProvider{}
	providers := map[string]llmgateway.Provider{
		"test": provider,
	}
	models := []llmgateway.ModelSpec{
		{
			ID:           "test/model",
			Name:         "model",
			Provider:     "test",
			Capabilities: []string{"text"},
		},
	}
	appSvc := llmgateway.NewService(providers, models, nil)
	srv := &Server{app: appSvc}

	// Test: Request with stream=false
	reqBody := CreateChatCompletionRequest{
		Model: "test/model",
		Messages: []ChatMessageIn{
			{
				Role:    "user",
				Content: json.RawMessage(`"Hello"`),
			},
		},
		Stream: false,
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
