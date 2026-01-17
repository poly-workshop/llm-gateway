package httpgateway

import (
	"encoding/json"
	"errors"

	"github.com/poly-workshop/llm-gateway/internal/domain/llm"
)

type Model struct {
	ID           string   `json:"id"`
	Object       string   `json:"object,omitempty"`
	Name         string   `json:"name,omitempty"`
	Provider     string   `json:"provider,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type ListModelsResponse struct {
	Object string  `json:"object,omitempty"`
	Data   []Model `json:"data"`
}

type GetModelResponse struct {
	Model Model `json:"model"`
}

type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// FunctionCall represents a function call made by the assistant.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolCall represents a tool call made by the assistant.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// ToolFunction defines a function that can be called.
type ToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

// Tool represents a tool that can be used by the model.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ChatMessageIn struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Name       string          `json:"name,omitempty"`
	ToolCalls  []ToolCall      `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

type ChatMessageOut struct {
	Role      string     `json:"role"`
	Content   string     `json:"content,omitempty"`
	Name      string     `json:"name,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type TokenUsage struct {
	PromptTokens     uint32 `json:"prompt_tokens"`
	CompletionTokens uint32 `json:"completion_tokens"`
	TotalTokens      uint32 `json:"total_tokens"`
}

type ChatCompletionChoice struct {
	Index        uint32         `json:"index"`
	Message      ChatMessageOut `json:"message"`
	FinishReason string         `json:"finish_reason,omitempty"`
}

type CreateChatCompletionRequest struct {
	Model       string          `json:"model"`
	Messages    []ChatMessageIn `json:"messages"`
	Temperature float64         `json:"temperature,omitempty"`
	MaxTokens   uint32          `json:"max_tokens,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
	User        string          `json:"user,omitempty"`
	Tools       []Tool          `json:"tools,omitempty"`
	ToolChoice  any             `json:"tool_choice,omitempty"`
}

type CreateChatCompletionResponse struct {
	ID      string                 `json:"id"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   TokenUsage             `json:"usage"`
}

type CreateEmbeddingsRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
	User  string   `json:"user,omitempty"`
}

type Embedding struct {
	Index     uint32    `json:"index"`
	Embedding []float32 `json:"embedding"`
}

type EmbeddingsUsage struct {
	PromptTokens uint32 `json:"prompt_tokens"`
	TotalTokens  uint32 `json:"total_tokens"`
}

type CreateEmbeddingsResponse struct {
	ID    string          `json:"id"`
	Model string          `json:"model"`
	Data  []Embedding     `json:"data"`
	Usage EmbeddingsUsage `json:"usage"`
}

type Generation struct {
	ID      string     `json:"id"`
	Model   string     `json:"model"`
	Created int64      `json:"created"`
	Usage   TokenUsage `json:"usage"`
}

type GetGenerationResponse struct {
	Generation Generation `json:"generation"`
}

// ChatCompletionChunkDelta represents the delta in a streaming chunk.
type ChatCompletionChunkDelta struct {
	Role      string          `json:"role,omitempty"`
	Content   string          `json:"content,omitempty"`
	ToolCalls []ToolCallDelta `json:"tool_calls,omitempty"`
}

// ToolCallDelta represents incremental tool call data in a streaming chunk.
type ToolCallDelta struct {
	Index    uint32                `json:"index"`
	ID       string                `json:"id,omitempty"`
	Type     string                `json:"type,omitempty"`
	Function *FunctionCallDelta    `json:"function,omitempty"`
}

// FunctionCallDelta represents incremental function call data.
type FunctionCallDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ChatCompletionChunkChoiceOut represents a choice in a streaming chunk.
type ChatCompletionChunkChoiceOut struct {
	Index        uint32                   `json:"index"`
	Delta        ChatCompletionChunkDelta `json:"delta"`
	FinishReason string                   `json:"finish_reason,omitempty"`
}

// ChatCompletionChunkResponse represents a streaming chunk response.
type ChatCompletionChunkResponse struct {
	ID      string                         `json:"id"`
	Object  string                         `json:"object"`
	Created int64                          `json:"created"`
	Model   string                         `json:"model"`
	Choices []ChatCompletionChunkChoiceOut `json:"choices"`
	Usage   *TokenUsage                    `json:"usage,omitempty"` // Optional usage, typically in final chunk
}

func (r CreateChatCompletionRequest) toDomainMessages() ([]llm.ChatMessage, error) {
	if len(r.Messages) == 0 {
		return nil, errors.New("messages is required")
	}
	out := make([]llm.ChatMessage, 0, len(r.Messages))
	for _, m := range r.Messages {
		if m.Role == "" {
			return nil, errors.New("message.role is required")
		}
		msg := llm.ChatMessage{
			Role:       m.Role,
			Name:       m.Name,
			ToolCallID: m.ToolCallID,
		}

		// Convert tool calls
		if len(m.ToolCalls) > 0 {
			msg.ToolCalls = make([]llm.ToolCall, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				msg.ToolCalls = append(msg.ToolCalls, llm.ToolCall{
					ID:   tc.ID,
					Type: tc.Type,
					Function: llm.FunctionCall{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				})
			}
		}

		// content can be either a string or an array of content parts.
		if len(m.Content) == 0 || string(m.Content) == "null" {
			out = append(out, msg)
			continue
		}

		var s string
		if err := json.Unmarshal(m.Content, &s); err == nil {
			msg.Content = s
			out = append(out, msg)
			continue
		}

		var parts []ContentPart
		if err := json.Unmarshal(m.Content, &parts); err == nil {
			msg.ContentParts = make([]llm.ContentPart, 0, len(parts))
			for _, p := range parts {
				cp := llm.ContentPart{Type: p.Type, Text: p.Text}
				if p.ImageURL != nil {
					cp.ImageURL = &llm.ImageURL{URL: p.ImageURL.URL, Detail: p.ImageURL.Detail}
				}
				msg.ContentParts = append(msg.ContentParts, cp)
			}
			out = append(out, msg)
			continue
		}

		return nil, errors.New("message.content must be a string or an array")
	}
	return out, nil
}
