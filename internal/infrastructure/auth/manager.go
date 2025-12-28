package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"sync"
	"time"
)

var (
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrForbidden       = errors.New("forbidden")
)

type ServiceToken struct {
	Name  string
	Token string
}

type TemporaryCredentials struct {
	AccessKeyID     string
	AccessKeySecret string
	ExpiresAt       time.Time
	Subject         string // e.g. service token name
}

type tempRecord struct {
	secret    string
	expiresAt time.Time
	subject   string
}

type Manager struct {
	enabled bool

	serviceTokens map[string]ServiceToken // token -> info
	tempTTL       time.Duration

	mu    sync.RWMutex
	temps map[string]tempRecord // accessKeyID -> record

	usageCallbackAllowlist map[string]map[string]struct{} // subject -> set(url)
}

func NewManager(serviceTokens []ServiceToken, tempTTL time.Duration) *Manager {
	st := make(map[string]ServiceToken, len(serviceTokens))
	for _, t := range serviceTokens {
		if t.Token == "" {
			continue
		}
		st[t.Token] = t
	}
	if tempTTL <= 0 {
		tempTTL = 15 * time.Minute
	}
	return &Manager{
		enabled:                len(st) > 0,
		serviceTokens:          st,
		tempTTL:                tempTTL,
		temps:                  make(map[string]tempRecord),
		usageCallbackAllowlist: make(map[string]map[string]struct{}),
	}
}

func (m *Manager) Enabled() bool { return m != nil && m.enabled }

func (m *Manager) AuthenticateServiceToken(_ context.Context, token string) (subject string, ok bool) {
	if !m.Enabled() {
		return "", true
	}
	if token == "" {
		return "", false
	}
	t, ok := m.serviceTokens[token]
	if !ok {
		return "", false
	}
	if t.Name != "" {
		return t.Name, true
	}
	return "service", true
}

func (m *Manager) IssueTemporaryCredentials(_ context.Context, serviceToken string) (TemporaryCredentials, error) {
	if !m.Enabled() {
		return TemporaryCredentials{}, fmt.Errorf("%w: auth not configured", ErrForbidden)
	}
	subject, ok := m.AuthenticateServiceToken(context.Background(), serviceToken)
	if !ok {
		return TemporaryCredentials{}, ErrUnauthenticated
	}

	akid, err := randHex(16)
	if err != nil {
		return TemporaryCredentials{}, err
	}
	secret, err := randB64(32)
	if err != nil {
		return TemporaryCredentials{}, err
	}
	exp := time.Now().Add(m.tempTTL)

	m.mu.Lock()
	m.temps[akid] = tempRecord{secret: secret, expiresAt: exp, subject: subject}
	m.mu.Unlock()

	return TemporaryCredentials{
		AccessKeyID:     akid,
		AccessKeySecret: secret,
		ExpiresAt:       exp,
		Subject:         subject,
	}, nil
}

func (m *Manager) SetUsageCallbackAllowlist(subject string, urls []string) error {
	if !m.Enabled() {
		return fmt.Errorf("%w: auth not configured", ErrForbidden)
	}
	if subject == "" {
		return fmt.Errorf("%w: missing subject", ErrForbidden)
	}
	set := make(map[string]struct{}, len(urls))
	for _, u := range urls {
		if u == "" {
			continue
		}
		if err := validateCallbackURL(u); err != nil {
			return err
		}
		set[u] = struct{}{}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(set) == 0 {
		delete(m.usageCallbackAllowlist, subject)
		return nil
	}
	m.usageCallbackAllowlist[subject] = set
	return nil
}

func (m *Manager) IsUsageCallbackAllowed(subject, url string) bool {
	if !m.Enabled() {
		return false
	}
	if subject == "" || url == "" {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	set, ok := m.usageCallbackAllowlist[subject]
	if !ok || len(set) == 0 {
		return false
	}
	_, ok = set[url]
	return ok
}

type SignatureInput struct {
	AccessKeyID string
	Signature   string // hex(HMAC-SHA256)
	Timestamp   int64  // unix seconds
	Nonce       string
	CallbackURL string

	// HTTP signing context (if present).
	HTTPMethod string
	HTTPPath   string
	HTTPQuery  string
	BodySHA256 string // hex(sha256(body))

	// gRPC signing context (fallback).
	GRPCFullMethod string
}

func (m *Manager) AuthenticateSignature(_ context.Context, in SignatureInput, now time.Time) (subject string, ok bool) {
	if !m.Enabled() {
		return "", true
	}
	if in.AccessKeyID == "" || in.Signature == "" || in.Timestamp == 0 || in.Nonce == "" {
		return "", false
	}
	// Allow small clock skew.
	ts := time.Unix(in.Timestamp, 0)
	if ts.Before(now.Add(-5*time.Minute)) || ts.After(now.Add(5*time.Minute)) {
		return "", false
	}

	m.mu.RLock()
	rec, ok := m.temps[in.AccessKeyID]
	m.mu.RUnlock()
	if !ok {
		return "", false
	}
	if now.After(rec.expiresAt) {
		return "", false
	}

	canonical := canonicalString(in)
	expected := hmacSHA256Hex(rec.secret, canonical)
	// Constant time compare on bytes.
	a, errA := hex.DecodeString(expected)
	b, errB := hex.DecodeString(in.Signature)
	if errA != nil || errB != nil {
		return "", false
	}
	if !hmac.Equal(a, b) {
		return "", false
	}
	return rec.subject, true
}

func canonicalString(in SignatureInput) string {
	// Prefer HTTP canonicalization when we have enough context.
	if in.HTTPMethod != "" && in.HTTPPath != "" {
		resource := in.HTTPPath
		if in.HTTPQuery != "" {
			resource = resource + "?" + in.HTTPQuery
		}
		// Include callback URL if present to prevent tampering.
		return fmt.Sprintf("%d\n%s\n%s\n%s\n%s\n%s", in.Timestamp, in.Nonce, in.HTTPMethod, resource, in.BodySHA256, in.CallbackURL)
	}
	// gRPC fallback: bind to fullMethod only.
	return fmt.Sprintf("%d\n%s\nGRPC\n%s\n%s", in.Timestamp, in.Nonce, in.GRPCFullMethod, in.CallbackURL)
}

func hmacSHA256Hex(secret, msg string) string {
	h := hmac.New(sha256.New, []byte(secret))
	_, _ = h.Write([]byte(msg))
	return hex.EncodeToString(h.Sum(nil))
}

func randHex(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func randB64(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func validateCallbackURL(raw string) error {
	// Keep it intentionally strict: only allow http/https absolute URLs.
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: invalid callback url", ErrForbidden)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: callback url scheme must be http or https", ErrForbidden)
	}
	if u.Host == "" {
		return fmt.Errorf("%w: callback url host is empty", ErrForbidden)
	}
	return nil
}

func (m *Manager) UsageCallbackAllowlist(subject string) []string {
	if !m.Enabled() {
		return nil
	}
	if subject == "" {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	set, ok := m.usageCallbackAllowlist[subject]
	if !ok || len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for u := range set {
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}
