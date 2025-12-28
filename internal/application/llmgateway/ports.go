package llmgateway

import (
	"context"

	"github.com/poly-workshop/llm-gateway/internal/domain/llm"
)

// Provider is an application port for upstream LLM providers (e.g. DashScope).
// Implementations live in infrastructure.
type Provider interface {
	CreateChatCompletion(ctx context.Context, req llm.ChatCompletionRequest) (llm.ChatCompletionResponse, error)
	CreateEmbeddings(ctx context.Context, req llm.EmbeddingsRequest) (llm.EmbeddingsResponse, error)
}

// GenerationRepository is an application port for storing and retrieving generation records.
// Implementations live in infrastructure (e.g. in-memory, database).
type GenerationRepository interface {
	Save(ctx context.Context, gen llm.Generation) error
	Get(ctx context.Context, id string) (llm.Generation, error)
}
