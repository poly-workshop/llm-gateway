package httpgateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
// the stream=true parameter on /v1/chat/completions returns 501 Not Implemented.
func TestStreamParameter_NotImplemented(t *testing.T) {
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

	// Assert: Should return 501 Not Implemented
	if w.Code != http.StatusNotImplemented {
		t.Errorf("expected status 501, got %d", w.Code)
	}

	var errResp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	errObj, ok := errResp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected error object in response")
	}

	msg, ok := errObj["message"].(string)
	if !ok {
		t.Fatalf("expected error message in response")
	}
	expectedMsg := "streaming not yet implemented"
	if msg != expectedMsg {
		t.Errorf("expected error message '%s', got: %s", expectedMsg, msg)
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
