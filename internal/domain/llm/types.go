package llm

type Model struct {
	ID           string
	Name         string
	Provider     string
	Capabilities []string
}

type Embedding struct {
	Index  uint32
	Vector []float32
}

// ImageURL represents an image URL with optional detail level for vision models.
type ImageURL struct {
	URL    string
	Detail string // "auto", "low", or "high"
}

// ContentPart represents a part of a multimodal message content.
type ContentPart struct {
	Type     string // "text" or "image_url"
	Text     string
	ImageURL *ImageURL
}

// OpenAI-style chat message.
// Supports both simple text content and multimodal content (text + images).
type ChatMessage struct {
	Role string
	// Simple text content (for text-only messages).
	Content string
	// Multimodal content parts (for vision models).
	// If provided, this takes precedence over the Content field.
	ContentParts []ContentPart
	Name         string
}

type TokenUsage struct {
	PromptTokens     uint32
	CompletionTokens uint32
	TotalTokens      uint32
}

type ChatCompletionChoice struct {
	Index        uint32
	Message      ChatMessage
	FinishReason string
}

type ChatCompletionRequest struct {
	// Routed model id, e.g. "dashscope/qwen-turbo".
	Model string

	Messages []ChatMessage

	Temperature float64
	MaxTokens   uint32
	User        string
}

type ChatCompletionResponse struct {
	ID      string
	Created int64
	Model   string

	Choices []ChatCompletionChoice
	Usage   TokenUsage
}

type EmbeddingsRequest struct {
	// Routed model id, e.g. "dashscope/text-embedding-v3".
	Model string
	Input []string
	User  string
}

// EmbeddingsUsage represents token usage for embeddings (input only).
type EmbeddingsUsage struct {
	PromptTokens uint32
	TotalTokens  uint32
}

type EmbeddingsResponse struct {
	ID    string
	Model string
	Data  []Embedding
	Usage EmbeddingsUsage
}

// Generation represents a completed generation with usage information.
type Generation struct {
	ID      string
	Model   string
	Created int64
	Usage   TokenUsage
}
