package httpgateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/poly-workshop/llm-gateway/internal/application/llmgateway"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/auth"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/config"
)

func TestCORSMiddleware_Preflight_DoesNotRequireAuth(t *testing.T) {
	appSvc := llmgateway.NewService(nil, nil, nil)
	authMgr := auth.NewManager(true, nil) // required, but no verifier => would 401 if auth ran

	s, err := New(":0", appSvc, authMgr, config.CORSConfig{
		Enabled:          true,
		AllowOrigins:     []string{"http://localhost:5173"},
		AllowMethods:     []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:     []string{"Authorization", "Content-Type"},
		AllowCredentials: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Ensure CORS runs before auth.
	h := s.corsMiddleware(s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	r := httptest.NewRequest(http.MethodOptions, "http://example.com/v1/chat/completions", nil)
	r.Header.Set("Origin", "http://localhost:5173")
	r.Header.Set("Access-Control-Request-Method", "POST")
	r.Header.Set("Access-Control-Request-Headers", "authorization, content-type")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("allow-origin mismatch: %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("allow-credentials mismatch: %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatalf("expected allow-methods")
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Fatalf("expected allow-headers")
	}
}
