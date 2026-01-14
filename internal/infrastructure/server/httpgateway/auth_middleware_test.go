package httpgateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/poly-workshop/llm-gateway/internal/infrastructure/auth"
)

func TestAuthMiddleware_AllowsJWTFromCookie(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	privPEM := pemPKCS8PrivateKey(t, priv)
	pubPEM := pemPKIXPublicKey(t, pub)

	signer, err := auth.NewJWTSigner("llm-gateway-admin", "llm-gateway", privPEM, 15*time.Minute)
	if err != nil {
		t.Fatalf("NewJWTSigner: %v", err)
	}
	verifier, err := auth.NewJWTVerifier("llm-gateway-admin", "llm-gateway", pubPEM)
	if err != nil {
		t.Fatalf("NewJWTVerifier: %v", err)
	}
	mgr := auth.NewManager(true, verifier)

	now := time.Now()
	token, _, err := signer.Sign("svc-a", 30*time.Second, nil, now)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	s := &Server{authMgr: mgr}
	wrapped := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sub := auth.SubjectFromContext(r.Context())
		if sub != "svc-a" {
			http.Error(w, "bad subject", http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "http://example.com/v1/models", nil)
	r.AddCookie(&http.Cookie{Name: jwtCookieName, Value: token})
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthMiddleware_AllowsJWTFromAuthorizationHeader(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	privPEM := pemPKCS8PrivateKey(t, priv)
	pubPEM := pemPKIXPublicKey(t, pub)

	signer, err := auth.NewJWTSigner("llm-gateway-admin", "llm-gateway", privPEM, 15*time.Minute)
	if err != nil {
		t.Fatalf("NewJWTSigner: %v", err)
	}
	verifier, err := auth.NewJWTVerifier("llm-gateway-admin", "llm-gateway", pubPEM)
	if err != nil {
		t.Fatalf("NewJWTVerifier: %v", err)
	}
	mgr := auth.NewManager(true, verifier)

	now := time.Now()
	token, _, err := signer.Sign("svc-a", 30*time.Second, nil, now)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	s := &Server{authMgr: mgr}
	wrapped := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "http://example.com/v1/models", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthMiddleware_HeaderInvalidCookieValid_Allows(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	privPEM := pemPKCS8PrivateKey(t, priv)
	pubPEM := pemPKIXPublicKey(t, pub)

	signer, err := auth.NewJWTSigner("llm-gateway-admin", "llm-gateway", privPEM, 15*time.Minute)
	if err != nil {
		t.Fatalf("NewJWTSigner: %v", err)
	}
	verifier, err := auth.NewJWTVerifier("llm-gateway-admin", "llm-gateway", pubPEM)
	if err != nil {
		t.Fatalf("NewJWTVerifier: %v", err)
	}
	mgr := auth.NewManager(true, verifier)

	now := time.Now()
	token, _, err := signer.Sign("svc-a", 30*time.Second, nil, now)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	s := &Server{authMgr: mgr}
	wrapped := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "http://example.com/v1/models", nil)
	r.Header.Set("Authorization", "Basic abc")
	r.AddCookie(&http.Cookie{Name: jwtCookieName, Value: token})
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func pemPKIXPublicKey(t *testing.T, pub ed25519.PublicKey) string {
	t.Helper()
	b, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: b}))
}

func pemPKCS8PrivateKey(t *testing.T, priv ed25519.PrivateKey) string {
	t.Helper()
	b, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b}))
}
