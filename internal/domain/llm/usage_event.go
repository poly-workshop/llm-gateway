package llm

import "time"

// UsageEvent represents a minimal audit event for LLM usage.
// This event is logged to an external sink (e.g., Redis Stream) for downstream auditing/billing.
// It intentionally excludes prompt/completion content to minimize data size and protect privacy.
type UsageEvent struct {
	// Timestamp of when the request completed.
	Timestamp time.Time `json:"timestamp"`

	// RequestID is the unique ID of the generation (chat completion or embeddings).
	RequestID string `json:"request_id"`

	// Subject is the authenticated user/service identifier (from JWT).
	Subject string `json:"subject,omitempty"`

	// JTI is the JWT token ID (jti claim), if present.
	JTI string `json:"jti,omitempty"`

	// Model is the routed model ID (e.g., "dashscope/qwen-turbo").
	Model string `json:"model"`

	// Provider is the upstream provider name (e.g., "dashscope", "openrouter").
	Provider string `json:"provider"`

	// UsageTokens contains token usage statistics.
	UsageTokens TokenUsage `json:"usage_tokens"`

	// LatencyMs is the request latency in milliseconds.
	LatencyMs int64 `json:"latency_ms"`

	// Status indicates whether the request succeeded or failed.
	Status string `json:"status"` // "success" or "error"

	// ErrorType is the error type if the request failed (e.g., "invalid_request_error").
	ErrorType string `json:"error_type,omitempty"`

	// ErrorMessage is a brief error description if the request failed.
	ErrorMessage string `json:"error_message,omitempty"`
}
