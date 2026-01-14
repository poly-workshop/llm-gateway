package openaiwire

import "github.com/poly-workshop/llm-gateway/internal/domain/llm"

// ImageURL represents an image URL with optional detail level for vision models.
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

// ContentPart represents a part of a multimodal message content.
type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// Message represents an OpenAI-compatible chat message.
// It supports both simple text content and multimodal content.
// Content field is used for text-only, ContentParts for multimodal.
type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []ContentPart
	Name    string `json:"name,omitempty"`
}

// ResponseMessage represents a message in the response from the API.
type ResponseMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

// Usage represents token usage information.
type Usage struct {
	PromptTokens     uint32 `json:"prompt_tokens"`
	CompletionTokens uint32 `json:"completion_tokens"`
	TotalTokens      uint32 `json:"total_tokens"`
}

// Choice represents a single completion choice.
type Choice struct {
	Index        uint32          `json:"index"`
	Message      ResponseMessage `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

// ChatCompletionRequest represents an OpenAI-compatible chat completion request.
type ChatCompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   uint32    `json:"max_tokens,omitempty"`
	User        string    `json:"user,omitempty"`
}

// ChatCompletionResponse represents an OpenAI-compatible chat completion response.
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// DeltaMessage represents an incremental message in a streaming chunk.
type DeltaMessage struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// ChunkChoice represents a single choice in a streaming chunk.
type ChunkChoice struct {
	Index        uint32       `json:"index"`
	Delta        DeltaMessage `json:"delta"`
	FinishReason string       `json:"finish_reason,omitempty"`
}

// ChatCompletionChunk represents a chunk in the streaming response.
type ChatCompletionChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
}

// ConvertDomainMessages converts domain ChatMessages to openaiwire Messages.
// This handles both simple text messages and multimodal messages with content parts.
func ConvertDomainMessages(domainMessages []llm.ChatMessage) []Message {
	msgs := make([]Message, 0, len(domainMessages))
	for _, m := range domainMessages {
		var content any
		if len(m.ContentParts) > 0 {
			// Multimodal message with content parts (for vision models).
			parts := make([]ContentPart, 0, len(m.ContentParts))
			for _, cp := range m.ContentParts {
				part := ContentPart{Type: cp.Type, Text: cp.Text}
				if cp.ImageURL != nil {
					part.ImageURL = &ImageURL{URL: cp.ImageURL.URL, Detail: cp.ImageURL.Detail}
				}
				parts = append(parts, part)
			}
			content = parts
		} else {
			// Simple text message.
			content = m.Content
		}
		msgs = append(msgs, Message{Role: m.Role, Content: content, Name: m.Name})
	}
	return msgs
}
