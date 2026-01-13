package auth

import (
	"context"
	"errors"
	"time"
)

var (
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrForbidden       = errors.New("forbidden")
)

type Manager struct {
	required bool
	verifier *JWTVerifier
}

func NewManager(required bool, verifier *JWTVerifier) *Manager {
	return &Manager{
		required: required,
		verifier: verifier,
	}
}

func (m *Manager) Required() bool { return m != nil && m.required }

func (m *Manager) Close(ctx context.Context) error {
	return nil
}

func (m *Manager) AuthenticateJWT(ctx context.Context, token string, now time.Time) (subject string, allowedModels []string, ok bool) {
	if m == nil || m.verifier == nil {
		// If not required, allow anonymous access (subject empty).
		return "", nil, !m.Required()
	}
	sub, models, err := m.verifier.Verify(token, now)
	if err != nil || sub == "" {
		return "", nil, false
	}
	return sub, models, true
}
