package usagecallback

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Sender struct {
	client  *http.Client
	timeout time.Duration
}

func New(client *http.Client, timeout time.Duration) *Sender {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	return &Sender{client: client, timeout: timeout}
}

type Payload struct {
	Event            string `json:"event"`
	Subject          string `json:"subject"`
	RequestID        string `json:"request_id,omitempty"`
	Operation        string `json:"operation"`
	GenerationID     string `json:"generation_id"`
	Model            string `json:"model"`
	CreatedUnix      int64  `json:"created_unix,omitempty"`
	PromptTokens     uint32 `json:"prompt_tokens"`
	CompletionTokens uint32 `json:"completion_tokens"`
	TotalTokens      uint32 `json:"total_tokens"`
	OccurredAtUnix   int64  `json:"occurred_at_unix"`
}

func (s *Sender) Send(ctx context.Context, url string, payload Payload) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("usage callback sender not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("usage callback non-2xx: %s", resp.Status)
	}
	return nil
}
