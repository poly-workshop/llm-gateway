package llmgateway

import (
	"context"
	"fmt"
	"strings"

	"github.com/poly-workshop/llm-gateway/internal/domain/llm"
)

// Service hosts application-level use cases for the LLM gateway.
// It should depend only on domain concepts (no protobuf / HTTP / gRPC).
type Service struct {
	providers map[string]Provider

	// models maps routed model ID (provider/model) to its metadata and optional upstream mapping.
	models map[string]ModelSpec

	// generations stores generation records for generation queries.
	generations GenerationRepository
}

type ModelSpec struct {
	ID           string
	Name         string
	Provider     string
	Capabilities []string

	// UpstreamModel overrides the model name sent to upstream provider.
	// If empty, the part after "provider/" in ID will be used.
	UpstreamModel string
}

func NewService(providers map[string]Provider, models []ModelSpec, generations GenerationRepository) *Service {
	mm := make(map[string]ModelSpec, len(models))
	for _, m := range models {
		mm[m.ID] = m
	}
	return &Service{providers: providers, models: mm, generations: generations}
}

func (s *Service) ListModels(_ context.Context) ([]llm.Model, error) {
	out := make([]llm.Model, 0, len(s.models))
	for _, m := range s.models {
		out = append(out, llm.Model{
			ID:           m.ID,
			Name:         m.Name,
			Provider:     m.Provider,
			Capabilities: m.Capabilities,
		})
	}
	return out, nil
}

func (s *Service) GetModel(_ context.Context, id string) (llm.Model, error) {
	if id == "" {
		return llm.Model{}, llm.InvalidArgument("id is required")
	}
	m, ok := s.models[id]
	if !ok {
		return llm.Model{}, llm.InvalidArgument("unknown model: " + id)
	}
	return llm.Model{
		ID:           m.ID,
		Name:         m.Name,
		Provider:     m.Provider,
		Capabilities: m.Capabilities,
	}, nil
}

func (s *Service) CreateEmbeddings(ctx context.Context, req llm.EmbeddingsRequest) (llm.EmbeddingsResponse, error) {
	if req.Model == "" {
		return llm.EmbeddingsResponse{}, llm.InvalidArgument("model is required")
	}
	if len(req.Input) == 0 {
		return llm.EmbeddingsResponse{}, llm.InvalidArgument("input is required")
	}

	routedModel := req.Model
	p, upstreamModel, err := s.resolveProviderAndUpstreamModel(routedModel)
	if err != nil {
		return llm.EmbeddingsResponse{}, err
	}
	req.Model = upstreamModel
	resp, err := p.CreateEmbeddings(ctx, req)
	if err != nil {
		return llm.EmbeddingsResponse{}, err
	}

	// Save generation record for generation queries (best-effort).
	if s.generations != nil {
		gen := s.buildGenerationFromEmbeddings(routedModel, resp)
		_ = s.generations.Save(ctx, gen) // Best effort, don't fail the request.
	}

	return resp, nil
}

func (s *Service) CreateChatCompletion(ctx context.Context, req llm.ChatCompletionRequest) (llm.ChatCompletionResponse, error) {
	if req.Model == "" {
		return llm.ChatCompletionResponse{}, llm.InvalidArgument("model is required")
	}
	if len(req.Messages) == 0 {
		return llm.ChatCompletionResponse{}, llm.InvalidArgument("messages is required")
	}

	routedModel := req.Model
	p, upstreamModel, err := s.resolveProviderAndUpstreamModel(routedModel)
	if err != nil {
		return llm.ChatCompletionResponse{}, err
	}
	req.Model = upstreamModel
	resp, err := p.CreateChatCompletion(ctx, req)
	if err != nil {
		return llm.ChatCompletionResponse{}, err
	}

	// Save generation record for generation queries (best-effort).
	if s.generations != nil {
		gen := s.buildGenerationFromChat(routedModel, resp)
		_ = s.generations.Save(ctx, gen) // Best effort, don't fail the request.
	}

	return resp, nil
}

func (s *Service) resolveProviderAndUpstreamModel(routedModel string) (Provider, string, error) {
	// If explicitly declared in model specs, prefer that.
	if m, ok := s.models[routedModel]; ok {
		p := s.providers[m.Provider]
		if p == nil {
			return nil, "", fmt.Errorf("no provider configured: %s", m.Provider)
		}
		if m.UpstreamModel != "" {
			return p, m.UpstreamModel, nil
		}
		// Fallthrough: derive upstream model from ID suffix.
	}

	parts := strings.SplitN(routedModel, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, "", llm.InvalidArgument("invalid model format, expected provider/model")
	}
	providerName := parts[0]
	upstreamModel := parts[1]

	p := s.providers[providerName]
	if p == nil {
		return nil, "", llm.InvalidArgument("unknown provider: " + providerName)
	}
	return p, upstreamModel, nil
}

// GetGeneration retrieves a generation record by ID.
func (s *Service) GetGeneration(ctx context.Context, id string) (llm.Generation, error) {
	if id == "" {
		return llm.Generation{}, llm.InvalidArgument("id is required")
	}
	if s.generations == nil {
		return llm.Generation{}, llm.InvalidArgument("generation repository not configured")
	}
	return s.generations.Get(ctx, id)
}

// buildGenerationFromChat creates a generation record from a chat completion response.
func (s *Service) buildGenerationFromChat(routedModel string, resp llm.ChatCompletionResponse) llm.Generation {
	return llm.Generation{
		ID:      resp.ID,
		Model:   routedModel,
		Created: resp.Created,
		Usage:   resp.Usage,
	}
}

// buildGenerationFromEmbeddings creates a generation record from an embeddings response.
func (s *Service) buildGenerationFromEmbeddings(routedModel string, resp llm.EmbeddingsResponse) llm.Generation {
	usage := llm.TokenUsage{
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: 0,
		TotalTokens:      resp.Usage.TotalTokens,
	}
	return llm.Generation{
		ID:      resp.ID,
		Model:   routedModel,
		Created: 0, // Embeddings response doesn't include created timestamp.
		Usage:   usage,
	}
}
