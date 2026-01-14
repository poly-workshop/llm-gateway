package openrouter

import (
	"bufio"
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

	httpClient *http.Client
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
	}
}

func (p *Provider) CreateChatCompletion(ctx context.Context, req llm.ChatCompletionRequest) (llm.ChatCompletionResponse, error) {
	msgs := make([]openaiwire.Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		var content any
		if len(m.ContentParts) > 0 {
			// Multimodal message with content parts (for vision models).
			parts := make([]openaiwire.ContentPart, 0, len(m.ContentParts))
			for _, cp := range m.ContentParts {
				part := openaiwire.ContentPart{Type: cp.Type, Text: cp.Text}
				if cp.ImageURL != nil {
					part.ImageURL = &openaiwire.ImageURL{URL: cp.ImageURL.URL, Detail: cp.ImageURL.Detail}
				}
				parts = append(parts, part)
			}
			content = parts
		} else {
			// Simple text message.
			content = m.Content
		}
		msgs = append(msgs, openaiwire.Message{Role: m.Role, Content: content, Name: m.Name})
	}

	body := openaiwire.ChatCompletionRequest{
		Model:       req.Model,
		Messages:    msgs,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		User:        req.User,
	}

	var out openaiwire.ChatCompletionResponse
	if err := p.doJSON(ctx, http.MethodPost, p.baseURL+"/chat/completions", body, &out); err != nil {
		return llm.ChatCompletionResponse{}, err
	}

	choices := make([]llm.ChatCompletionChoice, 0, len(out.Choices))
	for _, c := range out.Choices {
		choices = append(choices, llm.ChatCompletionChoice{
			Index: c.Index,
			Message: llm.ChatMessage{
				Role:    c.Message.Role,
				Content: c.Message.Content,
				Name:    c.Message.Name,
			},
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
	msgs := make([]openaiwire.Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		var content any
		if len(m.ContentParts) > 0 {
			// Multimodal message with content parts (for vision models).
			parts := make([]openaiwire.ContentPart, 0, len(m.ContentParts))
			for _, cp := range m.ContentParts {
				part := openaiwire.ContentPart{Type: cp.Type, Text: cp.Text}
				if cp.ImageURL != nil {
					part.ImageURL = &openaiwire.ImageURL{URL: cp.ImageURL.URL, Detail: cp.ImageURL.Detail}
				}
				parts = append(parts, part)
			}
			content = parts
		} else {
			// Simple text message.
			content = m.Content
		}
		msgs = append(msgs, openaiwire.Message{Role: m.Role, Content: content, Name: m.Name})
	}

	body := openaiwire.ChatCompletionRequest{
		Model:       req.Model,
		Messages:    msgs,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		User:        req.User,
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

	resp, err := p.httpClient.Do(r)
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

	return &sseStream{
		resp:   resp,
		reader: bufio.NewReader(resp.Body),
	}, nil
}

type sseStream struct {
	resp   *http.Response
	reader *bufio.Reader
}

func (s *sseStream) Recv() (llm.ChatCompletionChunk, error) {
	for {
		line, err := s.reader.ReadString('\n')
		if err != nil {
			return llm.ChatCompletionChunk{}, err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			return llm.ChatCompletionChunk{}, io.EOF
		}

		var chunk openaiwire.ChatCompletionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // Skip malformed chunks
		}

		choices := make([]llm.ChatCompletionChunkChoice, 0, len(chunk.Choices))
		for _, c := range chunk.Choices {
			choices = append(choices, llm.ChatCompletionChunkChoice{
				Index: c.Index,
				Delta: llm.ChatMessageDelta{
					Role:    c.Delta.Role,
					Content: c.Delta.Content,
				},
				FinishReason: c.FinishReason,
			})
		}

		return llm.ChatCompletionChunk{
			ID:      chunk.ID,
			Object:  chunk.Object,
			Created: chunk.Created,
			Model:   chunk.Model,
			Choices: choices,
		}, nil
	}
}

func (s *sseStream) Close() error {
	return s.resp.Body.Close()
}
