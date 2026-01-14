package dashscope

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

// Provider implements application.llmgateway.Provider for DashScope OpenAI-compatible mode.
type Provider struct {
	baseURL string
	apiKey  string

	httpClient *http.Client
}

func NewProvider(baseURL, apiKey string, timeout time.Duration) *Provider {
	baseURL = strings.TrimRight(baseURL, "/")
	if timeout <= 0 {
		timeout = 20 * time.Second
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
		return fmt.Errorf("dashscope api key is empty")
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
		return fmt.Errorf("dashscope http %d: %s", resp.StatusCode, msg)
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

