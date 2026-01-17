package httpgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/poly-workshop/llm-gateway/internal/application/llmgateway"
	"github.com/poly-workshop/llm-gateway/internal/domain/llm"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/auth"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/config"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/health"
)

type Server struct {
	httpListen string

	app     *llmgateway.Service
	authMgr *auth.Manager

	cors config.CORSConfig
}

func New(httpListen string, appSvc *llmgateway.Service, authMgr *auth.Manager, corsCfg config.CORSConfig) (*Server, error) {
	if httpListen == "" {
		return nil, fmt.Errorf("http listen address is empty")
	}
	if appSvc == nil {
		return nil, fmt.Errorf("app service is nil")
	}
	return &Server{
		httpListen: httpListen,
		app:        appSvc,
		authMgr:    authMgr,
		cors:       corsCfg,
	}, nil
}

func (s *Server) Start(ctx context.Context) error {
	r := chi.NewRouter()

	r.Get("/livez", func(w http.ResponseWriter, r *http.Request) { health.Livez(w, r) })
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) { health.Readyz(nil)(w, r) })

	// Data-plane routes (OpenAI-ish).
	r.Route("/v1", func(r chi.Router) {
		r.Use(s.corsMiddleware)
		r.Use(s.authMiddleware)

		// Do not expose /v1/auth/* on the public HTTP gateway (moved to admin service).
		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/v1/auth/") {
				http.NotFound(w, r)
				return
			}
			http.NotFound(w, r)
		})

		r.Get("/models", s.handleListModels)
		r.Get("/models/{id}", func(w http.ResponseWriter, r *http.Request) {
			s.handleGetModel(w, r, chi.URLParam(r, "id"))
		})
		r.Post("/chat/completions", s.handleCreateChatCompletion)
		r.Post("/embeddings", s.handleCreateEmbeddings)
		r.Get("/generation/{id}", func(w http.ResponseWriter, r *http.Request) {
			s.handleGetGeneration(w, r, chi.URLParam(r, "id"))
		})
	})

	srv := &http.Server{
		Addr:              s.httpListen,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("http listening", "addr", s.httpListen)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if s == nil || s.authMgr == nil {
			next.ServeHTTP(w, r)
			return
		}

		token, provided, errMsg := readJWTFromRequest(r)
		if errMsg != "" {
			writeJSONError(w, http.StatusUnauthorized, errMsg)
			return
		}
		if !provided {
			if s.authMgr.Required() {
				writeJSONError(w, http.StatusUnauthorized, "missing authorization")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if token == "" {
			writeJSONError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		subject, jti, allowedModels, ok := s.authMgr.AuthenticateJWT(ctx, token, time.Now())
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "invalid jwt")
			return
		}
		ctx = auth.WithSubject(ctx, subject)
		ctx = auth.WithJTI(ctx, jti)
		ctx = auth.WithAllowedModels(ctx, allowedModels)
		ctx = auth.WithMethod(ctx, auth.MethodJWT)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

const jwtCookieName = "llmgw_access_token"

func readJWTFromRequest(r *http.Request) (token string, provided bool, errMsg string) {
	if r == nil {
		return "", false, ""
	}

	// 1) Standard Authorization header.
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	headerProvided := authz != ""
	headerErr := ""
	if headerProvided {
		tok, ok, _ := parseAuthValue(authz)
		if ok {
			return tok, true, ""
		}
		headerErr = "authorization must be bearer token"
	}

	// 2) Cookie (browser-friendly) - fixed name.
	c, err := r.Cookie(jwtCookieName)
	if err == nil && c != nil {
		val := strings.TrimSpace(c.Value)
		if val != "" {
			tok, ok, _ := parseAuthValue(val)
			if ok {
				return tok, true, ""
			}
			if looksLikeJWT(val) {
				return val, true, ""
			}
			// Cookie exists but value isn't usable.
			return "", true, "invalid jwt"
		}
	}

	// If any auth was provided but not valid, surface the header error (if any)
	// otherwise report missing/invalid JWT.
	if headerProvided {
		return "", true, headerErr
	}
	return "", false, ""
}

func parseAuthValue(v string) (token string, ok bool, errMsg string) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false, ""
	}
	l := strings.ToLower(v)
	if strings.HasPrefix(l, "bearer ") {
		return strings.TrimSpace(v[len("bearer "):]), true, ""
	}
	return "", false, ""
}

func looksLikeJWT(s string) bool {
	// JWT is typically 3 dot-separated base64url segments.
	// We keep this intentionally loose: just check for two dots.
	if s == "" {
		return false
	}
	return strings.Count(s, ".") >= 2
}

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.app.ListModels(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}

	allowed := make(map[string]struct{})
	if ids := auth.AllowedModelsFromContext(r.Context()); len(ids) > 0 {
		allowed = make(map[string]struct{}, len(ids))
		for _, id := range ids {
			if id == "" {
				continue
			}
			allowed[id] = struct{}{}
		}
	}
	out := make([]Model, 0, len(models))
	for _, m := range models {
		if len(allowed) > 0 {
			if _, ok := allowed[m.ID]; !ok {
				continue
			}
		}
		out = append(out, Model{
			ID:           m.ID,
			Object:       "model",
			Name:         m.Name,
			Provider:     m.Provider,
			Capabilities: m.Capabilities,
		})
	}
	writeJSON(w, http.StatusOK, ListModelsResponse{Object: "list", Data: out})
}

func (s *Server) handleGetModel(w http.ResponseWriter, r *http.Request, id string) {
	if ids := auth.AllowedModelsFromContext(r.Context()); len(ids) > 0 {
		allowed := make(map[string]struct{}, len(ids))
		for _, mid := range ids {
			if mid == "" {
				continue
			}
			allowed[mid] = struct{}{}
		}
		if _, ok := allowed[id]; !ok {
			writeJSONError(w, http.StatusForbidden, "model not allowed")
			return
		}
	}
	m, err := s.app.GetModel(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, GetModelResponse{Model: Model{
		ID:           m.ID,
		Object:       "model",
		Name:         m.Name,
		Provider:     m.Provider,
		Capabilities: m.Capabilities,
	}})
}

func (s *Server) handleCreateChatCompletion(w http.ResponseWriter, r *http.Request) {
	var req CreateChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}

	// Check if streaming is requested
	if req.Stream {
		s.handleStreamChatCompletion(w, r, req)
		return
	}

	if ids := auth.AllowedModelsFromContext(r.Context()); len(ids) > 0 {
		allowed := make(map[string]struct{}, len(ids))
		for _, mid := range ids {
			if mid == "" {
				continue
			}
			allowed[mid] = struct{}{}
		}
		if _, ok := allowed[req.Model]; !ok {
			writeJSONError(w, http.StatusForbidden, "model not allowed")
			return
		}
	}
	msgs, err := req.toDomainMessages()
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Convert tools
	var tools []llm.Tool
	if len(req.Tools) > 0 {
		tools = make([]llm.Tool, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, llm.Tool{
				Type: t.Type,
				Function: llm.ToolFunction{
					Name:        t.Function.Name,
					Description: t.Function.Description,
					Parameters:  t.Function.Parameters,
				},
			})
		}
	}

	res, err := s.app.CreateChatCompletion(r.Context(), llm.ChatCompletionRequest{
		Model:       req.Model,
		Messages:    msgs,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		User:        req.User,
		Tools:       tools,
		ToolChoice:  req.ToolChoice,
	})
	if err != nil {
		writeErr(w, err)
		return
	}

	choices := make([]ChatCompletionChoice, 0, len(res.Choices))
	for _, c := range res.Choices {
		msgOut := ChatMessageOut{
			Role:    c.Message.Role,
			Content: c.Message.Content,
			Name:    c.Message.Name,
		}
		
		// Convert tool calls to response format
		if len(c.Message.ToolCalls) > 0 {
			msgOut.ToolCalls = make([]ToolCall, 0, len(c.Message.ToolCalls))
			for _, tc := range c.Message.ToolCalls {
				msgOut.ToolCalls = append(msgOut.ToolCalls, ToolCall{
					ID:   tc.ID,
					Type: tc.Type,
					Function: FunctionCall{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				})
			}
		}
		
		choices = append(choices, ChatCompletionChoice{
			Index:        c.Index,
			Message:      msgOut,
			FinishReason: c.FinishReason,
		})
	}
	writeJSON(w, http.StatusOK, CreateChatCompletionResponse{
		ID:      res.ID,
		Created: res.Created,
		Model:   res.Model,
		Choices: choices,
		Usage: TokenUsage{
			PromptTokens:     res.Usage.PromptTokens,
			CompletionTokens: res.Usage.CompletionTokens,
			TotalTokens:      res.Usage.TotalTokens,
		},
	})
}

func (s *Server) handleCreateEmbeddings(w http.ResponseWriter, r *http.Request) {
	var req CreateEmbeddingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if ids := auth.AllowedModelsFromContext(r.Context()); len(ids) > 0 {
		allowed := make(map[string]struct{}, len(ids))
		for _, mid := range ids {
			if mid == "" {
				continue
			}
			allowed[mid] = struct{}{}
		}
		if _, ok := allowed[req.Model]; !ok {
			writeJSONError(w, http.StatusForbidden, "model not allowed")
			return
		}
	}

	res, err := s.app.CreateEmbeddings(r.Context(), llm.EmbeddingsRequest{
		Model: req.Model,
		Input: req.Input,
		User:  req.User,
	})
	if err != nil {
		writeErr(w, err)
		return
	}

	data := make([]Embedding, 0, len(res.Data))
	for _, e := range res.Data {
		data = append(data, Embedding{Index: e.Index, Embedding: e.Vector})
	}

	writeJSON(w, http.StatusOK, CreateEmbeddingsResponse{
		ID:    res.ID,
		Model: res.Model,
		Data:  data,
		Usage: EmbeddingsUsage{PromptTokens: res.Usage.PromptTokens, TotalTokens: res.Usage.TotalTokens},
	})
}

func (s *Server) handleGetGeneration(w http.ResponseWriter, r *http.Request, id string) {
	gen, err := s.app.GetGeneration(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, GetGenerationResponse{
		Generation: Generation{
			ID:      gen.ID,
			Model:   gen.Model,
			Created: gen.Created,
			Usage: TokenUsage{
				PromptTokens:     gen.Usage.PromptTokens,
				CompletionTokens: gen.Usage.CompletionTokens,
				TotalTokens:      gen.Usage.TotalTokens,
			},
		},
	})
}

func writeErr(w http.ResponseWriter, err error) {
	if errors.Is(err, llm.ErrInvalidArgument) {
		// Unwrap the error message to provide clean API responses
		msg := strings.TrimPrefix(err.Error(), "invalid argument: ")
		writeJSONError(w, http.StatusBadRequest, msg)
		return
	}
	writeJSONError(w, http.StatusInternalServerError, err.Error())
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	errorType := "internal_error"
	switch status {
	case http.StatusBadRequest:
		errorType = "invalid_request_error"
	case http.StatusUnauthorized:
		errorType = "authentication_error"
	case http.StatusForbidden:
		errorType = "permission_error"
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": msg,
			"type":    errorType,
		},
	})
}

func (s *Server) handleStreamChatCompletion(w http.ResponseWriter, r *http.Request, req CreateChatCompletionRequest) {
	// Check model access
	if ids := auth.AllowedModelsFromContext(r.Context()); len(ids) > 0 {
		allowed := make(map[string]struct{}, len(ids))
		for _, mid := range ids {
			if mid == "" {
				continue
			}
			allowed[mid] = struct{}{}
		}
		if _, ok := allowed[req.Model]; !ok {
			writeJSONError(w, http.StatusForbidden, "model not allowed")
			return
		}
	}

	msgs, err := req.toDomainMessages()
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Convert tools
	var tools []llm.Tool
	if len(req.Tools) > 0 {
		tools = make([]llm.Tool, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, llm.Tool{
				Type: t.Type,
				Function: llm.ToolFunction{
					Name:        t.Function.Name,
					Description: t.Function.Description,
					Parameters:  t.Function.Parameters,
				},
			})
		}
	}

	stream, err := s.app.StreamChatCompletion(r.Context(), llm.ChatCompletionRequest{
		Model:       req.Model,
		Messages:    msgs,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		User:        req.User,
		Tools:       tools,
		ToolChoice:  req.ToolChoice,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	defer stream.Close()

	flusher, ok := w.(http.Flusher)
	if !ok {
		slog.Error("response writer does not implement http.Flusher; streaming not supported")
		writeJSONError(w, http.StatusInternalServerError, "streaming not supported by server")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	// Stream chunks
	for {
		select {
		case <-r.Context().Done():
			// Client disconnected, stop streaming
			slog.Debug("client disconnected during streaming")
			return
		default:
		}

		chunk, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				// Send [DONE] message
				fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				return
			}
			// Log error and send SSE error event, then close stream
			slog.Error("streaming error", "error", err)
			if errMsg, marshalErr := json.Marshal(map[string]string{"error": "streaming error"}); marshalErr == nil {
				fmt.Fprintf(w, "event: error\ndata: %s\n\n", errMsg)
				flusher.Flush()
			}
			return
		}

		choices := make([]ChatCompletionChunkChoiceOut, 0, len(chunk.Choices))
		for _, c := range chunk.Choices {
			delta := ChatCompletionChunkDelta{
				Role:    c.Delta.Role,
				Content: c.Delta.Content,
			}
			
			// Convert tool call deltas
			if len(c.Delta.ToolCalls) > 0 {
				delta.ToolCalls = make([]ToolCallDelta, 0, len(c.Delta.ToolCalls))
				for _, tc := range c.Delta.ToolCalls {
					toolCallDelta := ToolCallDelta{
						Index: tc.Index,
						ID:    tc.ID,
						Type:  tc.Type,
					}
					if tc.Function != nil {
						toolCallDelta.Function = &FunctionCallDelta{
							Name:      tc.Function.Name,
							Arguments: tc.Function.Arguments,
						}
					}
					delta.ToolCalls = append(delta.ToolCalls, toolCallDelta)
				}
			}
			
			choices = append(choices, ChatCompletionChunkChoiceOut{
				Index:        c.Index,
				Delta:        delta,
				FinishReason: c.FinishReason,
			})
		}

		var usage *TokenUsage
		if chunk.Usage != nil {
			usage = &TokenUsage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
		}

		chunkResp := ChatCompletionChunkResponse{
			ID:      chunk.ID,
			Object:  chunk.Object,
			Created: chunk.Created,
			Model:   chunk.Model,
			Choices: choices,
			Usage:   usage,
		}

		data, err := json.Marshal(chunkResp)
		if err != nil {
			slog.Error("failed to marshal chunk", "error", err)
			// Send error event to client before terminating
			if errMsg, marshalErr := json.Marshal(map[string]string{"error": "internal error marshaling chunk"}); marshalErr == nil {
				fmt.Fprintf(w, "event: error\ndata: %s\n\n", errMsg)
				flusher.Flush()
			}
			return
		}

		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
}
