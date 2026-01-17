package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/poly-workshop/llm-gateway/internal/domain/llm"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/llmprovider/openaiwire"
)

// Provider implements application.llmgateway.Provider for OpenRouter API.
type Provider struct {
	baseURL string
	apiKey  string

	httpClient          *http.Client
	streamingHTTPClient *http.Client
}

func NewProvider(baseURL, apiKey string, timeout time.Duration) *Provider {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &Provider{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		// Streaming client with no timeout to prevent premature stream termination
		streamingHTTPClient: &http.Client{
			Timeout: 0,
		},
	}
}

func (p *Provider) CreateChatCompletion(ctx context.Context, req llm.ChatCompletionRequest) (llm.ChatCompletionResponse, error) {
	msgs := openaiwire.ConvertDomainMessages(req.Messages)

	// Convert tools
	var tools []openaiwire.Tool
	if len(req.Tools) > 0 {
		tools = make([]openaiwire.Tool, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, openaiwire.Tool{
				Type: t.Type,
				Function: openaiwire.ToolFunction{
					Name:        t.Function.Name,
					Description: t.Function.Description,
					Parameters:  t.Function.Parameters,
				},
			})
		}
	}

	body := openaiwire.ChatCompletionRequest{
		Model:       req.Model,
		Messages:    msgs,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		User:        req.User,
		Tools:       tools,
		ToolChoice:  req.ToolChoice,
	}

	var out openaiwire.ChatCompletionResponse
	if err := p.doJSON(ctx, http.MethodPost, p.baseURL+"/chat/completions", body, &out); err != nil {
		return llm.ChatCompletionResponse{}, err
	}

	choices := make([]llm.ChatCompletionChoice, 0, len(out.Choices))
	for _, c := range out.Choices {
		msg := llm.ChatMessage{
			Role:    c.Message.Role,
			Content: c.Message.Content,
			Name:    c.Message.Name,
		}
		
		// Convert tool calls from response
		if len(c.Message.ToolCalls) > 0 {
			msg.ToolCalls = make([]llm.ToolCall, 0, len(c.Message.ToolCalls))
			for _, tc := range c.Message.ToolCalls {
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
		
		choices = append(choices, llm.ChatCompletionChoice{
			Index:        c.Index,
			Message:      msg,
			FinishReason: c.FinishReason,
		})
	}

	return llm.ChatCompletionResponse{
		ID:      out.ID,
		Created: out.Created,
		Model:   out.Model,
		Choices: choices,
		Usage: llm.TokenUsage{
			PromptTokens:     out.Usage.PromptTokens,
			CompletionTokens: out.Usage.CompletionTokens,
			TotalTokens:      out.Usage.TotalTokens,
		},
	}, nil
}

func (p *Provider) StreamChatCompletion(ctx context.Context, req llm.ChatCompletionRequest) (llm.ChatCompletionStream, error) {
	msgs := openaiwire.ConvertDomainMessages(req.Messages)

	// Convert tools
	var tools []openaiwire.Tool
	if len(req.Tools) > 0 {
		tools = make([]openaiwire.Tool, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, openaiwire.Tool{
				Type: t.Type,
				Function: openaiwire.ToolFunction{
					Name:        t.Function.Name,
					Description: t.Function.Description,
					Parameters:  t.Function.Parameters,
				},
			})
		}
	}

	body := openaiwire.ChatCompletionRequest{
		Model:       req.Model,
		Messages:    msgs,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		User:        req.User,
		Tools:       tools,
		ToolChoice:  req.ToolChoice,
	}

	return p.doStreamingRequest(ctx, p.baseURL+"/chat/completions", body)
}

func (p *Provider) CreateEmbeddings(ctx context.Context, req llm.EmbeddingsRequest) (llm.EmbeddingsResponse, error) {
	var out openaiwire.EmbeddingResponse
	if err := p.doJSON(ctx, http.MethodPost, p.baseURL+"/embeddings", openaiwire.EmbeddingRequest{Model: req.Model, Input: req.Input, User: req.User}, &out); err != nil {
		return llm.EmbeddingsResponse{}, err
	}

	data := make([]llm.Embedding, 0, len(out.Data))
	for _, d := range out.Data {
		data = append(data, llm.Embedding{Index: d.Index, Vector: d.Embedding})
	}
	return llm.EmbeddingsResponse{
		ID:    out.ID,
		Model: out.Model,
		Data:  data,
		Usage: llm.EmbeddingsUsage{
			PromptTokens: out.Usage.PromptTokens,
			TotalTokens:  out.Usage.TotalTokens,
		},
	}, nil
}

func (p *Provider) doJSON(ctx context.Context, method, url string, in any, out any) error {
	if p.apiKey == "" {
		return fmt.Errorf("openrouter api key is empty")
	}
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}

	r, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			msg = resp.Status
		}
		if resp.StatusCode == http.StatusBadRequest {
			return llm.InvalidArgument(msg)
		}
		return fmt.Errorf("openrouter http %d: %s", resp.StatusCode, msg)
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (p *Provider) doStreamingRequest(ctx context.Context, url string, body openaiwire.ChatCompletionRequest) (llm.ChatCompletionStream, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("openrouter api key is empty")
	}
	
	// Set stream to true
	bodyMap := map[string]any{
		"model":       body.Model,
		"messages":    body.Messages,
		"stream":      true,
		"temperature": body.Temperature,
	}
	if body.MaxTokens > 0 {
		bodyMap["max_tokens"] = body.MaxTokens
	}
	if body.User != "" {
		bodyMap["user"] = body.User
	}
	if len(body.Tools) > 0 {
		bodyMap["tools"] = body.Tools
	}
	if body.ToolChoice != nil {
		bodyMap["tool_choice"] = body.ToolChoice
	}

	b, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, err
	}

	r, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+p.apiKey)
	r.Header.Set("Accept", "text/event-stream")

	resp, err := p.streamingHTTPClient.Do(r)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			msg = resp.Status
		}
		if resp.StatusCode == http.StatusBadRequest {
			return nil, llm.InvalidArgument(msg)
		}
		return nil, fmt.Errorf("openrouter http %d: %s", resp.StatusCode, msg)
	}

	return openaiwire.NewSSEStream(resp), nil
}
