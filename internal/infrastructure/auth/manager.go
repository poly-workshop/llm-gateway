package auth

import (
	"context"
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

type Manager struct {
	required bool
	verifier *JWTVerifier

	mu sync.RWMutex

	usageCallbackStore    UsageCallbackStore
	usageCallbackCacheTTL time.Duration

	// usageCallbackAllowlist is a cache:
	// subject -> set(url)
	usageCallbackAllowlist map[string]map[string]struct{}
	// usageCallbackCacheExp stores cache expiration timestamps per subject.
	usageCallbackCacheExp map[string]time.Time
}

func NewManager(required bool, verifier *JWTVerifier, usageCallbackStore UsageCallbackStore, usageCallbackCacheTTL time.Duration) *Manager {
	if usageCallbackCacheTTL <= 0 {
		usageCallbackCacheTTL = 30 * time.Second
	}
	return &Manager{
		required:               required,
		verifier:               verifier,
		usageCallbackStore:     usageCallbackStore,
		usageCallbackCacheTTL:  usageCallbackCacheTTL,
		usageCallbackAllowlist: make(map[string]map[string]struct{}),
		usageCallbackCacheExp:  make(map[string]time.Time),
	}
}

func (m *Manager) Required() bool { return m != nil && m.required }

func (m *Manager) Close(ctx context.Context) error {
	if m == nil || m.usageCallbackStore == nil {
		return nil
	}
	return m.usageCallbackStore.Close(ctx)
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

func (m *Manager) SetUsageCallbackAllowlist(ctx context.Context, subject string, urls []string) error {
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
	out := make([]string, 0, len(set))
	for u := range set {
		out = append(out, u)
	}
	sort.Strings(out)

	if m.usageCallbackStore != nil {
		if ctx == nil {
			ctx = context.Background()
		}
		if err := m.usageCallbackStore.SetAllowlist(ctx, subject, out); err != nil {
			return err
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(set) == 0 {
		delete(m.usageCallbackAllowlist, subject)
		delete(m.usageCallbackCacheExp, subject)
		return nil
	}
	m.usageCallbackAllowlist[subject] = set
	m.usageCallbackCacheExp[subject] = time.Now().Add(m.usageCallbackCacheTTL)
	return nil
}

func (m *Manager) IsUsageCallbackAllowed(ctx context.Context, subject, url string) bool {
	if subject == "" || url == "" {
		return false
	}
	_ = m.ensureUsageCallbackAllowlist(ctx, subject)
	m.mu.RLock()
	defer m.mu.RUnlock()
	set, ok := m.usageCallbackAllowlist[subject]
	if !ok || len(set) == 0 {
		return false
	}
	_, ok = set[url]
	return ok
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

func (m *Manager) UsageCallbackAllowlist(ctx context.Context, subject string) []string {
	if subject == "" {
		return nil
	}
	_ = m.ensureUsageCallbackAllowlist(ctx, subject)
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

func (m *Manager) ensureUsageCallbackAllowlist(ctx context.Context, subject string) error {
	if m == nil || m.usageCallbackStore == nil {
		return nil
	}
	if subject == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	now := time.Now()

	m.mu.RLock()
	exp, ok := m.usageCallbackCacheExp[subject]
	_, has := m.usageCallbackAllowlist[subject]
	m.mu.RUnlock()

	if ok && has && now.Before(exp) {
		return nil
	}

	urls, err := m.usageCallbackStore.GetAllowlist(ctx, subject)
	if err != nil {
		return err
	}

	set := make(map[string]struct{}, len(urls))
	for _, u := range urls {
		if u == "" {
			continue
		}
		set[u] = struct{}{}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(set) == 0 {
		delete(m.usageCallbackAllowlist, subject)
		delete(m.usageCallbackCacheExp, subject)
		return nil
	}
	m.usageCallbackAllowlist[subject] = set
	m.usageCallbackCacheExp[subject] = time.Now().Add(m.usageCallbackCacheTTL)
	return nil
}
