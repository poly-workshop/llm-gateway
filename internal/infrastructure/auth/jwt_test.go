package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"
)

func TestJWT_Ed25519_SignAndVerify(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	privPEM := pemPKCS8PrivateKey(t, priv)
	pubPEM := pemPKIXPublicKey(t, pub)

	signer, err := NewJWTSigner("llm-gateway-admin", "llm-gateway", privPEM, 15*time.Minute)
	if err != nil {
		t.Fatalf("NewJWTSigner: %v", err)
	}
	verifier, err := NewJWTVerifier("llm-gateway-admin", "llm-gateway", pubPEM)
	if err != nil {
		t.Fatalf("NewJWTVerifier: %v", err)
	}

	now := time.Now()
	token, _, err := signer.Sign("svc-a", 30*time.Second, []string{"openrouter/openai/gpt-4o"}, now)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	sub, jti, models, err := verifier.Verify(token, now)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if sub != "svc-a" {
		t.Fatalf("subject mismatch: got %q", sub)
	}
	// JTI is optional and may be empty
	_ = jti
	if len(models) != 1 || models[0] != "openrouter/openai/gpt-4o" {
		t.Fatalf("models mismatch: got %v", models)
	}
}

func TestJWT_AudienceMismatch(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	privPEM := pemPKCS8PrivateKey(t, priv)
	pubPEM := pemPKIXPublicKey(t, pub)

	signer, err := NewJWTSigner("llm-gateway-admin", "llm-gateway", privPEM, 15*time.Minute)
	if err != nil {
		t.Fatalf("NewJWTSigner: %v", err)
	}
	verifier, err := NewJWTVerifier("llm-gateway-admin", "wrong-aud", pubPEM)
	if err != nil {
		t.Fatalf("NewJWTVerifier: %v", err)
	}

	now := time.Now()
	token, _, err := signer.Sign("svc-a", 30*time.Second, nil, now)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	_, _, _, err = verifier.Verify(token, now)
	if err == nil {
		t.Fatalf("expected verify error")
	}
}

func TestJWT_Expired(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	privPEM := pemPKCS8PrivateKey(t, priv)
	pubPEM := pemPKIXPublicKey(t, pub)

	signer, err := NewJWTSigner("llm-gateway-admin", "llm-gateway", privPEM, 15*time.Minute)
	if err != nil {
		t.Fatalf("NewJWTSigner: %v", err)
	}
	verifier, err := NewJWTVerifier("llm-gateway-admin", "llm-gateway", pubPEM)
	if err != nil {
		t.Fatalf("NewJWTVerifier: %v", err)
	}

	now := time.Now()
	token, _, err := signer.Sign("svc-a", 1*time.Second, nil, now)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	_, _, _, err = verifier.Verify(token, now.Add(2*time.Second))
	if err == nil {
		t.Fatalf("expected verify error for expired token")
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
