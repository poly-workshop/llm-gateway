package auth

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrInvalidJWT = errors.New("invalid jwt")
)

type JWTVerifier struct {
	issuer   string
	audience string
	pubKey   ed25519.PublicKey
}

// TokenClaims extends standard JWT registered claims with optional app-specific claims.
// Models is an optional allowlist of routed model IDs (e.g. "openrouter/openai/gpt-4o").
// If empty, the token is not restricted.
type TokenClaims struct {
	jwt.RegisteredClaims
	Models []string `json:"models,omitempty"`
}

func NewJWTVerifier(issuer, audience, publicKeyPEM string) (*JWTVerifier, error) {
	if issuer == "" {
		return nil, fmt.Errorf("jwt issuer is empty")
	}
	if audience == "" {
		return nil, fmt.Errorf("jwt audience is empty")
	}
	if publicKeyPEM == "" {
		return nil, fmt.Errorf("jwt public key is empty")
	}
	pub, err := parseEd25519PublicKeyFromPEM(publicKeyPEM)
	if err != nil {
		return nil, err
	}
	return &JWTVerifier{issuer: issuer, audience: audience, pubKey: pub}, nil
}

func (v *JWTVerifier) Verify(tokenString string, now time.Time) (subject string, jti string, allowedModels []string, err error) {
	if v == nil {
		return "", "", nil, fmt.Errorf("%w: verifier is nil", ErrInvalidJWT)
	}
	if tokenString == "" {
		return "", "", nil, fmt.Errorf("%w: missing token", ErrInvalidJWT)
	}

	claims := &TokenClaims{}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithAudience(v.audience),
		jwt.WithIssuer(v.issuer),
		jwt.WithTimeFunc(func() time.Time { return now }),
	)
	tok, err := parser.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		return v.pubKey, nil
	})
	if err != nil || tok == nil || !tok.Valid {
		return "", "", nil, fmt.Errorf("%w: %v", ErrInvalidJWT, err)
	}
	if claims.Subject == "" {
		return "", "", nil, fmt.Errorf("%w: missing sub", ErrInvalidJWT)
	}

	// Normalize allowlist (drop empty strings). If empty => unrestricted.
	if len(claims.Models) > 0 {
		out := make([]string, 0, len(claims.Models))
		for _, id := range claims.Models {
			if id == "" {
				continue
			}
			out = append(out, id)
		}
		allowedModels = out
	}
	return claims.Subject, claims.ID, allowedModels, nil
}

func parseEd25519PublicKeyFromPEM(pemText string) (ed25519.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, fmt.Errorf("decode public key pem: %w", ErrInvalidJWT)
	}
	pubAny, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	pub, ok := pubAny.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key is not ed25519")
	}
	return pub, nil
}
