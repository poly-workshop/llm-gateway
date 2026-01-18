package llmgateway

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/poly-workshop/llm-gateway/internal/domain/llm"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/auth"
)

// Service hosts application-level use cases for the LLM gateway.
// It should depend only on domain concepts (no protobuf / HTTP / gRPC).
type Service struct {
	mu sync.RWMutex

	providers map[string]Provider

	// models maps routed model ID (provider/model) to its metadata and optional upstream mapping.
	models map[string]ModelSpec

	// generations stores generation records for generation queries.
	generations GenerationRepository

	// usageSink publishes usage events to an external sink (e.g., Redis Stream).
	usageSink UsageSink
}

type ModelSpec struct {
	ID              string
	Name            string
	Provider        string
	Capabilities    []string
	MaxOutputTokens uint32

	// UpstreamModel overrides the model name sent to upstream provider.
	// If empty, the part after "provider/" in ID will be used.
	UpstreamModel string
}

func NewService(providers map[string]Provider, models []ModelSpec, generations GenerationRepository, usageSink UsageSink) *Service {
	mm := make(map[string]ModelSpec, len(models))
	for _, m := range models {
		mm[m.ID] = m
	}
	return &Service{providers: providers, models: mm, generations: generations, usageSink: usageSink}
}

// ReplaceConfig replaces providers and models atomically.
// This is used by the data-plane to reload runtime config managed by the admin control-plane.
func (s *Service) ReplaceConfig(providers map[string]Provider, models []ModelSpec) {
	if s == nil {
		return
	}
	mm := make(map[string]ModelSpec, len(models))
	for _, m := range models {
		if m.ID == "" {
			continue
		}
		mm[m.ID] = m
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if providers != nil {
		s.providers = providers
	}
	s.models = mm
}

func (s *Service) ListModels(_ context.Context) ([]llm.Model, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]llm.Model, 0, len(s.models))
	for _, m := range s.models {
		out = append(out, llm.Model{
			ID:              m.ID,
			Name:            m.Name,
			Provider:        m.Provider,
			Capabilities:    m.Capabilities,
			MaxOutputTokens: m.MaxOutputTokens,
		})
	}
	return out, nil
}

func (s *Service) GetModel(_ context.Context, id string) (llm.Model, error) {
	if id == "" {
		return llm.Model{}, llm.InvalidArgument("id is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.models[id]
	if !ok {
		return llm.Model{}, llm.InvalidArgument("unknown model: " + id)
	}
	return llm.Model{
		ID:              m.ID,
		Name:            m.Name,
		Provider:        m.Provider,
		Capabilities:    m.Capabilities,
		MaxOutputTokens: m.MaxOutputTokens,
	}, nil
}

func (s *Service) CreateEmbeddings(ctx context.Context, req llm.EmbeddingsRequest) (llm.EmbeddingsResponse, error) {
	startTime := time.Now()

	if req.Model == "" {
		return llm.EmbeddingsResponse{}, llm.InvalidArgument("model is required")
	}
	if len(req.Input) == 0 {
		return llm.EmbeddingsResponse{}, llm.InvalidArgument("input is required")
	}

	routedModel := req.Model
	s.mu.RLock()
	p, upstreamModel, err := s.resolveProviderAndUpstreamModel(routedModel)
	s.mu.RUnlock()
	if err != nil {
		// Publish error event
		event := s.buildUsageEvent(ctx, startTime, "", routedModel, llm.TokenUsage{}, "error", "invalid_request_error", err.Error())
		s.publishUsageEvent(ctx, event)
		return llm.EmbeddingsResponse{}, err
	}
	req.Model = upstreamModel
	resp, err := p.CreateEmbeddings(ctx, req)
	if err != nil {
		// Publish error event (use resp.ID if available, otherwise empty)
		requestID := ""
		if resp.ID != "" {
			requestID = resp.ID
		}
		event := s.buildUsageEvent(ctx, startTime, requestID, routedModel, llm.TokenUsage{}, "error", "provider_error", err.Error())
		s.publishUsageEvent(ctx, event)
		return llm.EmbeddingsResponse{}, err
	}

	// Save generation record for generation queries (best-effort).
	if s.generations != nil {
		gen := s.buildGenerationFromEmbeddings(routedModel, resp)
		_ = s.generations.Save(ctx, gen) // Best effort, don't fail the request.
	}

	// Publish success event
	usage := llm.TokenUsage{
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: 0,
		TotalTokens:      resp.Usage.TotalTokens,
	}
	event := s.buildUsageEvent(ctx, startTime, resp.ID, routedModel, usage, "success", "", "")
	s.publishUsageEvent(ctx, event)

	return resp, nil
}

func (s *Service) CreateChatCompletion(ctx context.Context, req llm.ChatCompletionRequest) (llm.ChatCompletionResponse, error) {
	startTime := time.Now()

	if req.Model == "" {
		return llm.ChatCompletionResponse{}, llm.InvalidArgument("model is required")
	}
	if len(req.Messages) == 0 {
		return llm.ChatCompletionResponse{}, llm.InvalidArgument("messages is required")
	}

	routedModel := req.Model
	s.mu.RLock()
	// Validate max_tokens limit before resolving provider
	if modelSpec, ok := s.models[routedModel]; ok && modelSpec.MaxOutputTokens > 0 {
		if req.MaxTokens > modelSpec.MaxOutputTokens {
			s.mu.RUnlock()
			errMsg := fmt.Sprintf("max_tokens (%d) exceeds model limit (%d)", req.MaxTokens, modelSpec.MaxOutputTokens)
			// Publish error event
			event := s.buildUsageEvent(ctx, startTime, "", routedModel, llm.TokenUsage{}, "error", "invalid_request_error", errMsg)
			s.publishUsageEvent(ctx, event)
			return llm.ChatCompletionResponse{}, llm.InvalidArgument(errMsg)
		}
	}
	p, upstreamModel, err := s.resolveProviderAndUpstreamModel(routedModel)
	s.mu.RUnlock()
	if err != nil {
		// Publish error event
		event := s.buildUsageEvent(ctx, startTime, "", routedModel, llm.TokenUsage{}, "error", "invalid_request_error", err.Error())
		s.publishUsageEvent(ctx, event)
		return llm.ChatCompletionResponse{}, err
	}
	req.Model = upstreamModel
	resp, err := p.CreateChatCompletion(ctx, req)
	if err != nil {
		// Publish error event (use resp.ID if available, otherwise empty)
		requestID := ""
		if resp.ID != "" {
			requestID = resp.ID
		}
		event := s.buildUsageEvent(ctx, startTime, requestID, routedModel, llm.TokenUsage{}, "error", "provider_error", err.Error())
		s.publishUsageEvent(ctx, event)
		return llm.ChatCompletionResponse{}, err
	}

	// Save generation record for generation queries (best-effort).
	if s.generations != nil {
		gen := s.buildGenerationFromChat(routedModel, resp)
		_ = s.generations.Save(ctx, gen) // Best effort, don't fail the request.
	}

	// Publish success event
	event := s.buildUsageEvent(ctx, startTime, resp.ID, routedModel, resp.Usage, "success", "", "")
	s.publishUsageEvent(ctx, event)

	return resp, nil
}

func (s *Service) StreamChatCompletion(ctx context.Context, req llm.ChatCompletionRequest) (llm.ChatCompletionStream, error) {
	startTime := time.Now()

	if req.Model == "" {
		return nil, llm.InvalidArgument("model is required")
	}
	if len(req.Messages) == 0 {
		return nil, llm.InvalidArgument("messages is required")
	}

	routedModel := req.Model
	s.mu.RLock()
	// Validate max_tokens limit before resolving provider
	if modelSpec, ok := s.models[routedModel]; ok && modelSpec.MaxOutputTokens > 0 {
		if req.MaxTokens > modelSpec.MaxOutputTokens {
			s.mu.RUnlock()
			errMsg := fmt.Sprintf("max_tokens (%d) exceeds model limit (%d)", req.MaxTokens, modelSpec.MaxOutputTokens)
			// Publish error event
			event := s.buildUsageEvent(ctx, startTime, "", routedModel, llm.TokenUsage{}, "error", "invalid_request_error", errMsg)
			s.publishUsageEvent(ctx, event)
			return nil, llm.InvalidArgument(errMsg)
		}
	}
	p, upstreamModel, err := s.resolveProviderAndUpstreamModel(routedModel)
	s.mu.RUnlock()
	if err != nil {
		// Publish error event
		event := s.buildUsageEvent(ctx, startTime, "", routedModel, llm.TokenUsage{}, "error", "invalid_request_error", err.Error())
		s.publishUsageEvent(ctx, event)
		return nil, err
	}
	req.Model = upstreamModel
	stream, err := p.StreamChatCompletion(ctx, req)
	if err != nil {
		// Publish error event
		event := s.buildUsageEvent(ctx, startTime, "", routedModel, llm.TokenUsage{}, "error", "provider_error", err.Error())
		s.publishUsageEvent(ctx, event)
		return nil, err
	}

	// Wrap stream to capture generation data for tracking and usage events
	return newTrackingStream(ctx, stream, routedModel, startTime, s.generations, s.usageSink, s), nil
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

// trackingStream wraps a ChatCompletionStream to capture generation data and usage events.
type trackingStream struct {
	ctx          context.Context
	underlying   llm.ChatCompletionStream
	routedModel  string
	startTime    time.Time
	generations  GenerationRepository
	usageSink    UsageSink
	service      *Service // Reference to service for helper methods
	lastChunk    *llm.ChatCompletionChunk
	streamClosed bool
	usageSaved   bool // Track whether usage event has been published
}

func newTrackingStream(
	ctx context.Context,
	underlying llm.ChatCompletionStream,
	routedModel string,
	startTime time.Time,
	generations GenerationRepository,
	usageSink UsageSink,
	service *Service,
) *trackingStream {
	return &trackingStream{
		ctx:         ctx,
		underlying:  underlying,
		routedModel: routedModel,
		startTime:   startTime,
		generations: generations,
		usageSink:   usageSink,
		service:     service,
	}
}

func (t *trackingStream) Recv() (llm.ChatCompletionChunk, error) {
	chunk, err := t.underlying.Recv()
	if err != nil {
		// On EOF or error, try to save generation record if we have data
		if !t.streamClosed && t.lastChunk != nil && t.lastChunk.Usage != nil {
			t.saveGeneration()
		}
		t.streamClosed = true
		return chunk, err
	}

	// Keep track of the last chunk (which may contain usage info)
	t.lastChunk = &chunk

	// If this chunk has usage info, it's likely the final data chunk
	if chunk.Usage != nil {
		t.saveGeneration()
	}

	return chunk, nil
}

func (t *trackingStream) Close() error {
	// Try to save generation record before closing if we haven't already
	if !t.streamClosed && t.lastChunk != nil && t.lastChunk.Usage != nil {
		t.saveGeneration()
	}
	t.streamClosed = true
	return t.underlying.Close()
}

func (t *trackingStream) saveGeneration() {
	if t.lastChunk == nil || t.lastChunk.Usage == nil {
		return
	}

	// Prevent duplicate saves/publishes
	if t.usageSaved {
		return
	}
	t.usageSaved = true

	gen := llm.Generation{
		ID:      t.lastChunk.ID,
		Model:   t.routedModel,
		Created: t.lastChunk.Created,
		Usage:   *t.lastChunk.Usage,
	}

	// Best effort, don't fail the stream if generation save fails
	if t.generations != nil {
		_ = t.generations.Save(t.ctx, gen)
	}

	// Publish usage event (check Usage again to be safe)
	if t.service != nil && t.usageSink != nil && t.lastChunk.Usage != nil {
		event := t.service.buildUsageEvent(
			t.ctx,
			t.startTime,
			t.lastChunk.ID,
			t.routedModel,
			*t.lastChunk.Usage,
			"success",
			"",
			"",
		)
		_ = t.usageSink.Publish(t.ctx, event)
	}
}

// publishUsageEvent publishes a usage event to the configured sink (best-effort).
// This is called after a request completes to record audit metadata.
func (s *Service) publishUsageEvent(ctx context.Context, event llm.UsageEvent) {
	if s == nil || s.usageSink == nil {
		return
	}
	// Fire-and-forget: don't block or fail the request if publishing fails.
	_ = s.usageSink.Publish(ctx, event)
}

// buildUsageEvent creates a usage event from request context and response metadata.
// Timestamp represents the event creation time (approximates request completion time).
// For precise completion time, use startTime + latency, but this is close enough for auditing.
func (s *Service) buildUsageEvent(
	ctx context.Context,
	startTime time.Time,
	requestID string,
	routedModel string,
	usage llm.TokenUsage,
	status string,
	errorType string,
	errorMessage string,
) llm.UsageEvent {
	provider := s.extractProviderFromModel(routedModel)
	latencyMs := time.Since(startTime).Milliseconds()

	return llm.UsageEvent{
		Timestamp:    time.Now().UTC(), // Event creation time (approximates request completion)
		RequestID:    requestID,
		Subject:      auth.SubjectFromContext(ctx),
		JTI:          auth.JTIFromContext(ctx),
		Model:        routedModel,
		Provider:     provider,
		UsageTokens:  usage,
		LatencyMs:    latencyMs,
		Status:       status,
		ErrorType:    errorType,
		ErrorMessage: errorMessage,
	}
}

// extractProviderFromModel extracts the provider name from a routed model ID.
func (s *Service) extractProviderFromModel(routedModel string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// First check if it's a configured model
	if m, ok := s.models[routedModel]; ok {
		return m.Provider
	}

	// Otherwise, parse from model ID (e.g., "dashscope/qwen-turbo" -> "dashscope")
	parts := strings.SplitN(routedModel, "/", 2)
	if len(parts) >= 1 {
		return parts[0]
	}
	return "unknown"
}
