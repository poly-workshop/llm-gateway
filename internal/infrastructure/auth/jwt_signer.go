package auth

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrInvalidJWTSigningKey = errors.New("invalid jwt signing key")
)

type JWTSigner struct {
	issuer     string
	audience   string
	privateKey ed25519.PrivateKey
	defaultTTL time.Duration
}

func NewJWTSigner(issuer, audience, privateKeyPEM string, defaultTTL time.Duration) (*JWTSigner, error) {
	if issuer == "" {
		return nil, fmt.Errorf("jwt issuer is empty")
	}
	if audience == "" {
		return nil, fmt.Errorf("jwt audience is empty")
	}
	if privateKeyPEM == "" {
		return nil, fmt.Errorf("jwt private key is empty")
	}
	if defaultTTL <= 0 {
		defaultTTL = 15 * time.Minute
	}
	priv, err := parseEd25519PrivateKeyFromPEM(privateKeyPEM)
	if err != nil {
		return nil, err
	}
	return &JWTSigner{issuer: issuer, audience: audience, privateKey: priv, defaultTTL: defaultTTL}, nil
}

func (s *JWTSigner) Sign(subject string, ttl time.Duration, allowedModels []string, now time.Time) (tokenString string, exp time.Time, err error) {
	if s == nil {
		return "", time.Time{}, fmt.Errorf("jwt signer is nil")
	}
	if subject == "" {
		return "", time.Time{}, fmt.Errorf("missing subject")
	}
	if ttl <= 0 {
		ttl = s.defaultTTL
	}
	exp = now.Add(ttl)

	claims := TokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   subject,
			Audience:  jwt.ClaimStrings{s.audience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}

	if len(allowedModels) > 0 {
		claims.Models = make([]string, 0, len(allowedModels))
		seen := make(map[string]struct{}, len(allowedModels))
		for _, id := range allowedModels {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			claims.Models = append(claims.Models, id)
		}
	}

	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	out, err := tok.SignedString(s.privateKey)
	if err != nil {
		return "", time.Time{}, err
	}
	return out, exp, nil
}

func parseEd25519PrivateKeyFromPEM(pemText string) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, fmt.Errorf("decode private key pem: %w", ErrInvalidJWTSigningKey)
	}

	// Ed25519 private keys are typically PKCS8.
	privAny, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	priv, ok := privAny.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not ed25519")
	}
	return priv, nil
}
