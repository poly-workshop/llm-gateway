package auth

import "context"

// UsageCallbackStore persists per-subject usage callback allowlists.
// This is dynamic configuration and should be shared across horizontally-scaled instances.
type UsageCallbackStore interface {
	// GetAllowlist returns the allowed callback URLs for the subject.
	// If no allowlist is configured, it returns (nil, nil).
	GetAllowlist(ctx context.Context, subject string) ([]string, error)
	// SetAllowlist replaces the allowlist for subject.
	// If urls is empty, the allowlist is cleared (deleted).
	SetAllowlist(ctx context.Context, subject string, urls []string) error
	// Close releases underlying resources.
	Close(ctx context.Context) error
}
