package llm

type Model struct {
	ID              string
	Name            string
	Provider        string
	Capabilities    []string
	MaxOutputTokens uint32
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

// FunctionCall represents a function call made by the assistant.
type FunctionCall struct {
	Name      string
	Arguments string // JSON-encoded arguments
}

// ToolCall represents a tool call made by the assistant.
type ToolCall struct {
	ID       string
	Type     string // "function"
	Function FunctionCall
}

// ToolFunction defines a function that can be called.
type ToolFunction struct {
	Name        string
	Description string
	Parameters  any // JSON schema object
}

// Tool represents a tool that can be used by the model.
type Tool struct {
	Type     string // "function"
	Function ToolFunction
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
	// Tool calls made by the assistant (for tool use).
	ToolCalls []ToolCall
	// Tool call ID (for tool response messages).
	ToolCallID string
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

	// Tools available for the model to call.
	Tools []Tool
	// Controls which (if any) tool is called by the model.
	// "none" means the model will not call any tool and instead generates a message.
	// "auto" means the model can pick between generating a message or calling one or more tools.
	// "required" means the model must call one or more tools.
	// Specifying a particular tool via {"type": "function", "function": {"name": "my_function"}} forces the model to call that tool.
	ToolChoice any
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

// ToolCallDelta represents incremental tool call data in a streaming chunk.
type ToolCallDelta struct {
	Index    uint32
	ID       string
	Type     string
	Function *FunctionCallDelta
}

// FunctionCallDelta represents incremental function call data.
type FunctionCallDelta struct {
	Name      string
	Arguments string
}

// ChatMessageDelta represents incremental content in a streaming chunk.
type ChatMessageDelta struct {
	Role      string
	Content   string
	ToolCalls []ToolCallDelta
}

// ChatCompletionChunkChoice represents a single choice in a streaming chunk.
type ChatCompletionChunkChoice struct {
	Index        uint32
	Delta        ChatMessageDelta
	FinishReason string
}

// ChatCompletionChunk represents a chunk in the streaming response.
type ChatCompletionChunk struct {
	ID      string
	Object  string
	Created int64
	Model   string
	Choices []ChatCompletionChunkChoice
	Usage   *TokenUsage // Optional usage information, typically in the final chunk
}

// ChatCompletionStream represents a stream of chat completion chunks.
type ChatCompletionStream interface {
	// Recv receives the next chunk from the stream.
	// Returns io.EOF when the stream is complete.
	Recv() (ChatCompletionChunk, error)
	// Close closes the stream.
	Close() error
}
