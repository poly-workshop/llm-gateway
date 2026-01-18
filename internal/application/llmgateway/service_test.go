package llmgateway

import (
	"context"
	"io"
	"testing"

	"github.com/poly-workshop/llm-gateway/internal/domain/llm"
)

// mockUsageSink implements UsageSink for testing
type mockUsageSink struct {
	events []llm.UsageEvent
}

func (m *mockUsageSink) Publish(_ context.Context, event llm.UsageEvent) error {
	m.events = append(m.events, event)
	return nil
}

func (m *mockUsageSink) Close(_ context.Context) error {
	return nil
}

// mockProvider implements Provider for testing
type mockProvider struct {
	streamChatCompletionFunc func(req llm.ChatCompletionRequest) (llm.ChatCompletionStream, error)
}

func (m *mockProvider) CreateChatCompletion(_ context.Context, req llm.ChatCompletionRequest) (llm.ChatCompletionResponse, error) {
	return llm.ChatCompletionResponse{
		ID:      "chatcmpl-test",
		Created: 1234567890,
		Model:   req.Model,
		Choices: []llm.ChatCompletionChoice{
			{
				Index: 0,
				Message: llm.ChatMessage{
					Role:    "assistant",
					Content: "Test response",
				},
				FinishReason: "stop",
			},
		},
		Usage: llm.TokenUsage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}, nil
}

func (m *mockProvider) CreateEmbeddings(_ context.Context, req llm.EmbeddingsRequest) (llm.EmbeddingsResponse, error) {
	return llm.EmbeddingsResponse{}, nil
}

func (m *mockProvider) StreamChatCompletion(_ context.Context, req llm.ChatCompletionRequest) (llm.ChatCompletionStream, error) {
	if m.streamChatCompletionFunc != nil {
		return m.streamChatCompletionFunc(req)
	}
	return &mockStream{
		chunks: []llm.ChatCompletionChunk{
			{
				ID:      "chatcmpl-test-stream",
				Object:  "chat.completion.chunk",
				Created: 1234567890,
				Model:   req.Model,
				Choices: []llm.ChatCompletionChunkChoice{
					{
						Index: 0,
						Delta: llm.ChatMessageDelta{
							Role:    "assistant",
							Content: "Test",
						},
					},
				},
			},
			{
				ID:      "chatcmpl-test-stream",
				Object:  "chat.completion.chunk",
				Created: 1234567890,
				Model:   req.Model,
				Choices: []llm.ChatCompletionChunkChoice{
					{
						Index: 0,
						Delta: llm.ChatMessageDelta{
							Content: " response",
						},
						FinishReason: "stop",
					},
				},
				// Add usage in final chunk
				Usage: &llm.TokenUsage{
					PromptTokens:     15,
					CompletionTokens: 25,
					TotalTokens:      40,
				},
			},
		},
	}, nil
}

type mockStream struct {
	chunks []llm.ChatCompletionChunk
	index  int
}

func (s *mockStream) Recv() (llm.ChatCompletionChunk, error) {
	if s.index >= len(s.chunks) {
		return llm.ChatCompletionChunk{}, io.EOF
	}
	chunk := s.chunks[s.index]
	s.index++
	return chunk, nil
}

func (s *mockStream) Close() error {
	return nil
}

// TestStreamChatCompletion_PublishesUsageEvent verifies that StreamChatCompletion
// publishes a success usage event when the stream completes successfully.
func TestStreamChatCompletion_PublishesUsageEvent(t *testing.T) {
	// Setup: Create a service with a mock provider and usage sink
	provider := &mockProvider{}
	providers := map[string]Provider{
		"test": provider,
	}
	models := []ModelSpec{
		{
			ID:           "test/model",
			Name:         "model",
			Provider:     "test",
			Capabilities: []string{"text"},
		},
	}
	sink := &mockUsageSink{}
	appSvc := NewService(providers, models, nil, sink)

	// Test: Stream chat completion
	ctx := context.Background()
	stream, err := appSvc.StreamChatCompletion(ctx, llm.ChatCompletionRequest{
		Model:    "test/model",
		Messages: []llm.ChatMessage{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("StreamChatCompletion failed: %v", err)
	}
	defer stream.Close()

	// Read all chunks from the stream
	for {
		_, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Recv failed: %v", err)
		}
	}

	// Close the stream to ensure final processing
	if err := stream.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Assert: Usage event should be published
	if len(sink.events) == 0 {
		t.Fatal("Expected usage event to be published, but no events were captured")
	}

	// Verify the event
	event := sink.events[0]
	if event.Status != "success" {
		t.Errorf("Expected status 'success', got '%s'", event.Status)
	}
	if event.Model != "test/model" {
		t.Errorf("Expected model 'test/model', got '%s'", event.Model)
	}
	if event.Provider != "test" {
		t.Errorf("Expected provider 'test', got '%s'", event.Provider)
	}
	if event.RequestID != "chatcmpl-test-stream" {
		t.Errorf("Expected request ID 'chatcmpl-test-stream', got '%s'", event.RequestID)
	}
	if event.UsageTokens.TotalTokens != 40 {
		t.Errorf("Expected total tokens 40, got %d", event.UsageTokens.TotalTokens)
	}
	if event.ErrorType != "" || event.ErrorMessage != "" {
		t.Errorf("Expected no error info, got type='%s' message='%s'", event.ErrorType, event.ErrorMessage)
	}
}

// TestStreamChatCompletion_PublishesOnlyOnce verifies that usage events are
// published exactly once, not multiple times for the same stream.
func TestStreamChatCompletion_PublishesOnlyOnce(t *testing.T) {
	// Setup: Create a service with a mock provider and usage sink
	provider := &mockProvider{}
	providers := map[string]Provider{
		"test": provider,
	}
	models := []ModelSpec{
		{
			ID:           "test/model",
			Name:         "model",
			Provider:     "test",
			Capabilities: []string{"text"},
		},
	}
	sink := &mockUsageSink{}
	appSvc := NewService(providers, models, nil, sink)

	// Test: Stream chat completion
	ctx := context.Background()
	stream, err := appSvc.StreamChatCompletion(ctx, llm.ChatCompletionRequest{
		Model:    "test/model",
		Messages: []llm.ChatMessage{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("StreamChatCompletion failed: %v", err)
	}
	defer stream.Close()

	// Read all chunks from the stream
	for {
		_, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Recv failed: %v", err)
		}
	}

	// Close the stream
	if err := stream.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Assert: Usage event should be published exactly once
	if len(sink.events) != 1 {
		t.Errorf("Expected exactly 1 usage event, but got %d events", len(sink.events))
		for i, evt := range sink.events {
			t.Logf("Event %d: Status=%s, Model=%s, Tokens=%d", i, evt.Status, evt.Model, evt.UsageTokens.TotalTokens)
		}
	}
}

// TestStreamChatCompletion_NoUsageInChunks verifies that when the stream doesn't
// include usage information, no usage event is published.
func TestStreamChatCompletion_NoUsageInChunks(t *testing.T) {
	// Setup: Create a provider that returns chunks without usage
	provider := &mockProvider{
		streamChatCompletionFunc: func(req llm.ChatCompletionRequest) (llm.ChatCompletionStream, error) {
			return &mockStream{
				chunks: []llm.ChatCompletionChunk{
					{
						ID:      "chatcmpl-no-usage",
						Object:  "chat.completion.chunk",
						Created: 1234567890,
						Model:   req.Model,
						Choices: []llm.ChatCompletionChunkChoice{
							{
								Index: 0,
								Delta: llm.ChatMessageDelta{
									Role:    "assistant",
									Content: "Response without usage",
								},
								FinishReason: "stop",
							},
						},
						// No Usage field
					},
				},
			}, nil
		},
	}
	providers := map[string]Provider{
		"test": provider,
	}
	models := []ModelSpec{
		{
			ID:           "test/model",
			Name:         "model",
			Provider:     "test",
			Capabilities: []string{"text"},
		},
	}
	sink := &mockUsageSink{}
	appSvc := NewService(providers, models, nil, sink)

	// Test: Stream chat completion
	ctx := context.Background()
	stream, err := appSvc.StreamChatCompletion(ctx, llm.ChatCompletionRequest{
		Model:    "test/model",
		Messages: []llm.ChatMessage{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("StreamChatCompletion failed: %v", err)
	}
	defer stream.Close()

	// Read all chunks
	for {
		_, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Recv failed: %v", err)
		}
	}

	// Close the stream
	if err := stream.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Assert: No usage event should be published when there's no usage data
	if len(sink.events) != 0 {
		t.Errorf("Expected no usage event when no usage data, but got %d events", len(sink.events))
	}
}
