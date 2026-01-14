package openaiwire

// EmbeddingRequest represents an OpenAI-compatible embeddings request.
type EmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
	User  string   `json:"user,omitempty"`
}

// EmbeddingDatum represents a single embedding in the response.
type EmbeddingDatum struct {
	Index     uint32    `json:"index"`
	Embedding []float32 `json:"embedding"`
}

// EmbeddingUsage represents token usage for embeddings.
type EmbeddingUsage struct {
	PromptTokens uint32 `json:"prompt_tokens"`
	TotalTokens  uint32 `json:"total_tokens"`
}

// EmbeddingResponse represents an OpenAI-compatible embeddings response.
type EmbeddingResponse struct {
	ID    string           `json:"id"`
	Model string           `json:"model"`
	Data  []EmbeddingDatum `json:"data"`
	Usage EmbeddingUsage   `json:"usage"`
}
