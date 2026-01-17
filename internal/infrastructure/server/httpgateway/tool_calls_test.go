package httpgateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/poly-workshop/llm-gateway/internal/application/llmgateway"
	"github.com/poly-workshop/llm-gateway/internal/domain/llm"
)

// TestToolCalls_RequestResponse validates that tool calls work in non-streaming mode
func TestToolCalls_RequestResponse(t *testing.T) {
	// Setup: Create a mock provider that returns a tool call
	provider := &mockProvider{
		createChatCompletionFunc: func(req llm.ChatCompletionRequest) (llm.ChatCompletionResponse, error) {
			// Verify tools were passed through
			if len(req.Tools) == 0 {
				t.Error("expected tools to be passed to provider")
			}
			if len(req.Tools) > 0 && req.Tools[0].Function.Name != "get_weather" {
				t.Errorf("expected tool name 'get_weather', got %s", req.Tools[0].Function.Name)
			}

			// Return a response with a tool call
			return llm.ChatCompletionResponse{
				ID:      "chatcmpl-tool-test",
				Created: 1234567890,
				Model:   req.Model,
				Choices: []llm.ChatCompletionChoice{
					{
						Index: 0,
						Message: llm.ChatMessage{
							Role:    "assistant",
							Content: "",
							ToolCalls: []llm.ToolCall{
								{
									ID:   "call_abc123",
									Type: "function",
									Function: llm.FunctionCall{
										Name:      "get_weather",
										Arguments: `{"location":"San Francisco","unit":"celsius"}`,
									},
								},
							},
						},
						FinishReason: "tool_calls",
					},
				},
				Usage: llm.TokenUsage{
					PromptTokens:     50,
					CompletionTokens: 20,
					TotalTokens:      70,
				},
			}, nil
		},
	}

	providers := map[string]llmgateway.Provider{
		"test": provider,
	}
	models := []llmgateway.ModelSpec{
		{
			ID:           "test/model",
			Name:         "model",
			Provider:     "test",
			Capabilities: []string{"text"},
		},
	}
	appSvc := llmgateway.NewService(providers, models, nil, nil)
	srv := &Server{app: appSvc}

	// Test: Request with tools
	reqBody := CreateChatCompletionRequest{
		Model: "test/model",
		Messages: []ChatMessageIn{
			{
				Role:    "user",
				Content: json.RawMessage(`"What's the weather in San Francisco?"`),
			},
		},
		Tools: []Tool{
			{
				Type: "function",
				Function: ToolFunction{
					Name:        "get_weather",
					Description: "Get the current weather in a location",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"location": map[string]any{
								"type":        "string",
								"description": "The city and state, e.g. San Francisco, CA",
							},
							"unit": map[string]any{
								"type": "string",
								"enum": []string{"celsius", "fahrenheit"},
							},
						},
						"required": []string{"location"},
					},
				},
			},
		},
		ToolChoice: "auto",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleCreateChatCompletion(w, req)

	// Assert: Should return 200
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Parse response
	var resp CreateChatCompletionResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify tool calls in response
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}

	choice := resp.Choices[0]
	if len(choice.Message.ToolCalls) == 0 {
		t.Fatal("expected tool calls in response")
	}

	toolCall := choice.Message.ToolCalls[0]
	if toolCall.ID != "call_abc123" {
		t.Errorf("expected tool call ID 'call_abc123', got '%s'", toolCall.ID)
	}
	if toolCall.Type != "function" {
		t.Errorf("expected tool call type 'function', got '%s'", toolCall.Type)
	}
	if toolCall.Function.Name != "get_weather" {
		t.Errorf("expected function name 'get_weather', got '%s'", toolCall.Function.Name)
	}
	if toolCall.Function.Arguments != `{"location":"San Francisco","unit":"celsius"}` {
		t.Errorf("unexpected function arguments: %s", toolCall.Function.Arguments)
	}
	if choice.FinishReason != "tool_calls" {
		t.Errorf("expected finish reason 'tool_calls', got '%s'", choice.FinishReason)
	}
}

// TestToolCalls_Streaming validates that tool calls work in streaming mode
func TestToolCalls_Streaming(t *testing.T) {
	// Setup: Create a mock provider that returns streaming tool calls
	provider := &mockProvider{
		streamChatCompletionFunc: func(req llm.ChatCompletionRequest) (llm.ChatCompletionStream, error) {
			// Verify tools were passed through
			if len(req.Tools) == 0 {
				t.Error("expected tools to be passed to provider")
			}

			return &mockStream{
				chunks: []llm.ChatCompletionChunk{
					{
						ID:      "chatcmpl-stream-tool",
						Object:  "chat.completion.chunk",
						Created: 1234567890,
						Model:   req.Model,
						Choices: []llm.ChatCompletionChunkChoice{
							{
								Index: 0,
								Delta: llm.ChatMessageDelta{
									Role: "assistant",
									ToolCalls: []llm.ToolCallDelta{
										{
											Index: 0,
											ID:    "call_xyz789",
											Type:  "function",
											Function: &llm.FunctionCallDelta{
												Name:      "get_weather",
												Arguments: "",
											},
										},
									},
								},
							},
						},
					},
					{
						ID:      "chatcmpl-stream-tool",
						Object:  "chat.completion.chunk",
						Created: 1234567890,
						Model:   req.Model,
						Choices: []llm.ChatCompletionChunkChoice{
							{
								Index: 0,
								Delta: llm.ChatMessageDelta{
									ToolCalls: []llm.ToolCallDelta{
										{
											Index: 0,
											Function: &llm.FunctionCallDelta{
												Arguments: `{"location":"`,
											},
										},
									},
								},
							},
						},
					},
					{
						ID:      "chatcmpl-stream-tool",
						Object:  "chat.completion.chunk",
						Created: 1234567890,
						Model:   req.Model,
						Choices: []llm.ChatCompletionChunkChoice{
							{
								Index: 0,
								Delta: llm.ChatMessageDelta{
									ToolCalls: []llm.ToolCallDelta{
										{
											Index: 0,
											Function: &llm.FunctionCallDelta{
												Arguments: `New York"}`,
											},
										},
									},
								},
								FinishReason: "tool_calls",
							},
						},
					},
				},
			}, nil
		},
	}

	providers := map[string]llmgateway.Provider{
		"test": provider,
	}
	models := []llmgateway.ModelSpec{
		{
			ID:           "test/model",
			Name:         "model",
			Provider:     "test",
			Capabilities: []string{"text"},
		},
	}
	appSvc := llmgateway.NewService(providers, models, nil, nil)
	srv := &Server{app: appSvc}

	// Test: Streaming request with tools
	reqBody := CreateChatCompletionRequest{
		Model: "test/model",
		Messages: []ChatMessageIn{
			{
				Role:    "user",
				Content: json.RawMessage(`"What's the weather?"`),
			},
		},
		Stream: true,
		Tools: []Tool{
			{
				Type: "function",
				Function: ToolFunction{
					Name:        "get_weather",
					Description: "Get weather",
				},
			},
		},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleCreateChatCompletion(w, req)

	// Assert: Should return 200 with SSE
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	if w.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected Content-Type 'text/event-stream', got '%s'", w.Header().Get("Content-Type"))
	}

	// Parse streaming response
	responseBody := w.Body.String()
	lines := strings.Split(responseBody, "\n")

	foundToolCall := false
	for _, line := range lines {
		if strings.HasPrefix(line, "data: {") {
			data := strings.TrimPrefix(line, "data: ")
			var chunk ChatCompletionChunkResponse
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				t.Errorf("failed to parse chunk: %v, data: %s", err, data)
				continue
			}

			if len(chunk.Choices) > 0 && len(chunk.Choices[0].Delta.ToolCalls) > 0 {
				foundToolCall = true
				toolCall := chunk.Choices[0].Delta.ToolCalls[0]
				
				// Verify structure
				if toolCall.Type != "" && toolCall.Type != "function" {
					t.Errorf("expected tool call type 'function', got '%s'", toolCall.Type)
				}
			}
		}
	}

	if !foundToolCall {
		t.Error("expected to find tool call in streaming response")
	}
}

// TestToolCalls_ToolResponseMessage validates that tool response messages are properly handled
func TestToolCalls_ToolResponseMessage(t *testing.T) {
	provider := &mockProvider{
		createChatCompletionFunc: func(req llm.ChatCompletionRequest) (llm.ChatCompletionResponse, error) {
			// Verify that tool response message was passed correctly
			foundToolMessage := false
			for _, msg := range req.Messages {
				if msg.Role == "tool" && msg.ToolCallID == "call_abc123" {
					foundToolMessage = true
					if msg.Content != "Sunny, 72°F" {
						t.Errorf("unexpected tool message content: %s", msg.Content)
					}
				}
			}
			if !foundToolMessage {
				t.Error("expected to find tool message in request")
			}

			return llm.ChatCompletionResponse{
				ID:      "chatcmpl-final",
				Created: 1234567890,
				Model:   req.Model,
				Choices: []llm.ChatCompletionChoice{
					{
						Index: 0,
						Message: llm.ChatMessage{
							Role:    "assistant",
							Content: "The weather in San Francisco is sunny and 72°F.",
						},
						FinishReason: "stop",
					},
				},
				Usage: llm.TokenUsage{
					PromptTokens:     80,
					CompletionTokens: 15,
					TotalTokens:      95,
				},
			}, nil
		},
	}

	providers := map[string]llmgateway.Provider{
		"test": provider,
	}
	models := []llmgateway.ModelSpec{
		{
			ID:           "test/model",
			Name:         "model",
			Provider:     "test",
			Capabilities: []string{"text"},
		},
	}
	appSvc := llmgateway.NewService(providers, models, nil, nil)
	srv := &Server{app: appSvc}

	// Test: Request with tool response message
	reqBody := CreateChatCompletionRequest{
		Model: "test/model",
		Messages: []ChatMessageIn{
			{
				Role:    "user",
				Content: json.RawMessage(`"What's the weather in San Francisco?"`),
			},
			{
				Role:    "assistant",
				Content: json.RawMessage(`""`),
				ToolCalls: []ToolCall{
					{
						ID:   "call_abc123",
						Type: "function",
						Function: FunctionCall{
							Name:      "get_weather",
							Arguments: `{"location":"San Francisco"}`,
						},
					},
				},
			},
			{
				Role:       "tool",
				Content:    json.RawMessage(`"Sunny, 72°F"`),
				ToolCallID: "call_abc123",
			},
		},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleCreateChatCompletion(w, req)

	// Assert: Should return 200
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp CreateChatCompletionResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}

	choice := resp.Choices[0]
	if choice.Message.Content == "" {
		t.Error("expected non-empty content in final response")
	}
	if choice.FinishReason != "stop" {
		t.Errorf("expected finish reason 'stop', got '%s'", choice.FinishReason)
	}
}
