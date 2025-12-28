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
	// OpenAI-compatible request/response shapes (minimal subset).
	// For vision models, content can be an array of content parts.
	type imageURL struct {
		URL    string `json:"url"`
		Detail string `json:"detail,omitempty"`
	}
	type contentPart struct {
		Type     string    `json:"type"`
		Text     string    `json:"text,omitempty"`
		ImageURL *imageURL `json:"image_url,omitempty"`
	}
	// message supports both simple text content and multimodal content.
	// Content field is used for text-only, ContentParts for multimodal.
	type message struct {
		Role         string        `json:"role"`
		Content      any           `json:"content"` // string or []contentPart
		Name         string        `json:"name,omitempty"`
	}
	type responseMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
		Name    string `json:"name,omitempty"`
	}
	type chatReq struct {
		Model       string    `json:"model"`
		Messages    []message `json:"messages"`
		Temperature float64   `json:"temperature,omitempty"`
		MaxTokens   uint32    `json:"max_tokens,omitempty"`
		User        string    `json:"user,omitempty"`
	}
	type usage struct {
		PromptTokens     uint32 `json:"prompt_tokens"`
		CompletionTokens uint32 `json:"completion_tokens"`
		TotalTokens      uint32 `json:"total_tokens"`
	}
	type choice struct {
		Index        uint32          `json:"index"`
		Message      responseMessage `json:"message"`
		FinishReason string          `json:"finish_reason"`
	}
	type chatResp struct {
		ID      string   `json:"id"`
		Created int64    `json:"created"`
		Model   string   `json:"model"`
		Choices []choice `json:"choices"`
		Usage   usage    `json:"usage"`
	}

	msgs := make([]message, 0, len(req.Messages))
	for _, m := range req.Messages {
		var content any
		if len(m.ContentParts) > 0 {
			// Multimodal message with content parts (for vision models).
			parts := make([]contentPart, 0, len(m.ContentParts))
			for _, cp := range m.ContentParts {
				part := contentPart{Type: cp.Type, Text: cp.Text}
				if cp.ImageURL != nil {
					part.ImageURL = &imageURL{URL: cp.ImageURL.URL, Detail: cp.ImageURL.Detail}
				}
				parts = append(parts, part)
			}
			content = parts
		} else {
			// Simple text message.
			content = m.Content
		}
		msgs = append(msgs, message{Role: m.Role, Content: content, Name: m.Name})
	}

	body := chatReq{
		Model:       req.Model,
		Messages:    msgs,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		User:        req.User,
	}

	var out chatResp
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
	type embReq struct {
		Model string   `json:"model"`
		Input []string `json:"input"`
		User  string   `json:"user,omitempty"`
	}
	type embDatum struct {
		Index     uint32    `json:"index"`
		Embedding []float32 `json:"embedding"`
	}
	type embUsage struct {
		PromptTokens uint32 `json:"prompt_tokens"`
		TotalTokens  uint32 `json:"total_tokens"`
	}
	type embResp struct {
		ID    string     `json:"id"`
		Model string     `json:"model"`
		Data  []embDatum `json:"data"`
		Usage embUsage   `json:"usage"`
	}

	var out embResp
	if err := p.doJSON(ctx, http.MethodPost, p.baseURL+"/embeddings", embReq{Model: req.Model, Input: req.Input, User: req.User}, &out); err != nil {
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

