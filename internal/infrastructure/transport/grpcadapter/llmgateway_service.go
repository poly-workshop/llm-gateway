package grpcadapter

import (
	"context"
	"errors"
	"log/slog"
	"time"

	llmgatewayv1 "github.com/poly-workshop/llm-gateway/gen/go/llmgateway/v1"
	"github.com/poly-workshop/llm-gateway/internal/application/llmgateway"
	"github.com/poly-workshop/llm-gateway/internal/domain/llm"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/auth"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/usagecallback"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

type LLMGatewayService struct {
	llmgatewayv1.UnimplementedLLMGatewayServiceServer

	app      *llmgateway.Service
	authMgr  *auth.Manager
	cbSender *usagecallback.Sender
}

func NewLLMGatewayService(app *llmgateway.Service, authMgr *auth.Manager) *LLMGatewayService {
	return &LLMGatewayService{
		app:      app,
		authMgr:  authMgr,
		cbSender: usagecallback.New(nil, 3*time.Second),
	}
}

func (s *LLMGatewayService) IssueTemporaryCredentials(ctx context.Context, _ *llmgatewayv1.IssueTemporaryCredentialsRequest) (*llmgatewayv1.IssueTemporaryCredentialsResponse, error) {
	if s.authMgr == nil {
		return nil, status.Error(codes.FailedPrecondition, "auth not configured")
	}

	md, _ := metadata.FromIncomingContext(ctx)
	var serviceToken string
	if v := md.Get("x-service-token"); len(v) > 0 {
		serviceToken = v[0]
	}

	creds, err := s.authMgr.IssueTemporaryCredentials(ctx, serviceToken)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthenticated) {
			return nil, status.Error(codes.Unauthenticated, "invalid service token")
		}
		if errors.Is(err, auth.ErrForbidden) {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &llmgatewayv1.IssueTemporaryCredentialsResponse{
		Credentials: &llmgatewayv1.TemporaryCredentials{
			AccessKeyId:     creds.AccessKeyID,
			AccessKeySecret: creds.AccessKeySecret,
			ExpiresAtUnix:   creds.ExpiresAt.Unix(),
		},
	}, nil
}

func (s *LLMGatewayService) SetUsageCallback(ctx context.Context, req *llmgatewayv1.SetUsageCallbackRequest) (*llmgatewayv1.SetUsageCallbackResponse, error) {
	if s.authMgr == nil {
		return nil, status.Error(codes.FailedPrecondition, "auth not configured")
	}
	// Requirement: ServiceToken only (not signature).
	if auth.MethodFromContext(ctx) != auth.MethodServiceToken {
		return nil, status.Error(codes.PermissionDenied, "service token required")
	}
	subject := auth.SubjectFromContext(ctx)
	if subject == "" {
		return nil, status.Error(codes.PermissionDenied, "missing subject")
	}
	if err := s.authMgr.SetUsageCallbackAllowlist(subject, req.GetUrls()); err != nil {
		if errors.Is(err, auth.ErrForbidden) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &llmgatewayv1.SetUsageCallbackResponse{Urls: s.authMgr.UsageCallbackAllowlist(subject)}, nil
}

func (s *LLMGatewayService) GetUsageCallback(ctx context.Context, _ *llmgatewayv1.GetUsageCallbackRequest) (*llmgatewayv1.GetUsageCallbackResponse, error) {
	if s.authMgr == nil {
		return nil, status.Error(codes.FailedPrecondition, "auth not configured")
	}
	// Requirement: ServiceToken only (not signature).
	if auth.MethodFromContext(ctx) != auth.MethodServiceToken {
		return nil, status.Error(codes.PermissionDenied, "service token required")
	}
	subject := auth.SubjectFromContext(ctx)
	if subject == "" {
		return nil, status.Error(codes.PermissionDenied, "missing subject")
	}
	return &llmgatewayv1.GetUsageCallbackResponse{Urls: s.authMgr.UsageCallbackAllowlist(subject)}, nil
}

func (s *LLMGatewayService) ListModels(ctx context.Context, _ *llmgatewayv1.ListModelsRequest) (*llmgatewayv1.ListModelsResponse, error) {
	models, err := s.app.ListModels(ctx)
	if err != nil {
		return nil, toStatusErr(err)
	}

	out := make([]*llmgatewayv1.Model, 0, len(models))
	for _, m := range models {
		m := m
		out = append(out, &llmgatewayv1.Model{
			Id:           m.ID,
			Name:         m.Name,
			Provider:     m.Provider,
			Capabilities: m.Capabilities,
		})
	}
	return &llmgatewayv1.ListModelsResponse{Data: out}, nil
}

func (s *LLMGatewayService) GetModel(ctx context.Context, req *llmgatewayv1.GetModelRequest) (*llmgatewayv1.GetModelResponse, error) {
	m, err := s.app.GetModel(ctx, req.GetId())
	if err != nil {
		return nil, toStatusErr(err)
	}
	return &llmgatewayv1.GetModelResponse{
		Model: &llmgatewayv1.Model{
			Id:           m.ID,
			Name:         m.Name,
			Provider:     m.Provider,
			Capabilities: m.Capabilities,
		},
	}, nil
}

func (s *LLMGatewayService) CreateChatCompletion(ctx context.Context, req *llmgatewayv1.CreateChatCompletionRequest) (*llmgatewayv1.CreateChatCompletionResponse, error) {
	msgs := make([]llm.ChatMessage, 0, len(req.GetMessages()))
	for _, m := range req.GetMessages() {
		msg := llm.ChatMessage{
			Role: m.GetRole(),
			Name: m.GetName(),
		}
		// Parse content field: can be string or array of content parts.
		if err := parseMessageContent(m.GetContent(), &msg); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid message content: %v", err)
		}
		msgs = append(msgs, msg)
	}

	res, err := s.app.CreateChatCompletion(ctx, llm.ChatCompletionRequest{
		Model:       req.GetModel(),
		Messages:    msgs,
		Temperature: req.GetTemperature(),
		MaxTokens:   req.GetMaxTokens(),
		User:        req.GetUser(),
	})
	if err != nil {
		return nil, toStatusErr(err)
	}

	s.maybeSendUsageCallback(ctx, "chat.completions", llm.Generation{
		ID:      res.ID,
		Model:   res.Model,
		Created: res.Created,
		Usage:   res.Usage,
	})

	choices := make([]*llmgatewayv1.ChatCompletionChoice, 0, len(res.Choices))
	for _, c := range res.Choices {
		c := c
		choices = append(choices, &llmgatewayv1.ChatCompletionChoice{
			Index: c.Index,
			Message: &llmgatewayv1.ChatMessage{
				Role:    c.Message.Role,
				Content: structpb.NewStringValue(c.Message.Content),
				Name:    c.Message.Name,
			},
			FinishReason: c.FinishReason,
		})
	}

	return &llmgatewayv1.CreateChatCompletionResponse{
		Id:      res.ID,
		Created: res.Created,
		Model:   res.Model,
		Choices: choices,
		Usage: &llmgatewayv1.TokenUsage{
			PromptTokens:     res.Usage.PromptTokens,
			CompletionTokens: res.Usage.CompletionTokens,
			TotalTokens:      res.Usage.TotalTokens,
		},
	}, nil
}

func (s *LLMGatewayService) CreateChatCompletionStream(*llmgatewayv1.CreateChatCompletionStreamRequest, grpc.ServerStreamingServer[llmgatewayv1.CreateChatCompletionStreamResponse]) error {
	return status.Error(codes.Unimplemented, "not implemented yet")
}

func (s *LLMGatewayService) CreateEmbeddings(ctx context.Context, req *llmgatewayv1.CreateEmbeddingsRequest) (*llmgatewayv1.CreateEmbeddingsResponse, error) {
	res, err := s.app.CreateEmbeddings(ctx, llm.EmbeddingsRequest{
		Model: req.GetModel(),
		Input: req.GetInput(),
		User:  req.GetUser(),
	})
	if err != nil {
		return nil, toStatusErr(err)
	}

	s.maybeSendUsageCallback(ctx, "embeddings", llm.Generation{
		ID:      res.ID,
		Model:   res.Model,
		Created: 0,
		Usage: llm.TokenUsage{
			PromptTokens:     res.Usage.PromptTokens,
			CompletionTokens: 0,
			TotalTokens:      res.Usage.TotalTokens,
		},
	})

	data := make([]*llmgatewayv1.Embedding, 0, len(res.Data))
	for _, e := range res.Data {
		e := e
		data = append(data, &llmgatewayv1.Embedding{
			Index:     e.Index,
			Embedding: e.Vector,
		})
	}

	return &llmgatewayv1.CreateEmbeddingsResponse{
		Id:    res.ID,
		Model: res.Model,
		Data:  data,
		Usage: &llmgatewayv1.EmbeddingsUsage{
			PromptTokens: res.Usage.PromptTokens,
			TotalTokens:  res.Usage.TotalTokens,
		},
	}, nil
}

func (s *LLMGatewayService) GetGeneration(ctx context.Context, req *llmgatewayv1.GetGenerationRequest) (*llmgatewayv1.GetGenerationResponse, error) {
	gen, err := s.app.GetGeneration(ctx, req.GetId())
	if err != nil {
		return nil, toStatusErr(err)
	}

	return &llmgatewayv1.GetGenerationResponse{
		Generation: &llmgatewayv1.Generation{
			Id:      gen.ID,
			Model:   gen.Model,
			Created: gen.Created,
			Usage: &llmgatewayv1.TokenUsage{
				PromptTokens:     gen.Usage.PromptTokens,
				CompletionTokens: gen.Usage.CompletionTokens,
				TotalTokens:      gen.Usage.TotalTokens,
			},
		},
	}, nil
}

func toStatusErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, llm.ErrInvalidArgument) {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	return status.Error(codes.Internal, err.Error())
}

func (s *LLMGatewayService) maybeSendUsageCallback(ctx context.Context, op string, gen llm.Generation) {
	if s == nil || s.authMgr == nil || s.cbSender == nil {
		return
	}
	subject := auth.SubjectFromContext(ctx)
	if subject == "" {
		return
	}

	// Caller provides callback URL per request. Only allow if it is in the trusted allowlist.
	md, _ := metadata.FromIncomingContext(ctx)
	var cbURL string
	if v := md.Get("x-usage-callback"); len(v) > 0 {
		cbURL = v[0]
	}
	if cbURL == "" {
		return
	}
	if !s.authMgr.IsUsageCallbackAllowed(subject, cbURL) {
		slog.Warn("usage callback url not allowed", "url", cbURL, "subject", subject, "op", op)
		return
	}

	var requestID string
	if v := md.Get("x-request-id"); len(v) > 0 {
		requestID = v[0]
	}

	payload := usagecallback.Payload{
		Event:            "llm.usage",
		Subject:          subject,
		RequestID:        requestID,
		Operation:        op,
		GenerationID:     gen.ID,
		Model:            gen.Model,
		CreatedUnix:      gen.Created,
		PromptTokens:     gen.Usage.PromptTokens,
		CompletionTokens: gen.Usage.CompletionTokens,
		TotalTokens:      gen.Usage.TotalTokens,
		OccurredAtUnix:   time.Now().Unix(),
	}

	go func() {
		// Avoid tying callback to request cancellation.
		if err := s.cbSender.Send(context.Background(), cbURL, payload); err != nil {
			slog.Warn("usage callback failed", "url", cbURL, "subject", subject, "op", op, "generation_id", gen.ID, "error", err)
		}
	}()
}

// parseMessageContent parses the content field which can be a string or an array of content parts.
func parseMessageContent(content *structpb.Value, msg *llm.ChatMessage) error {
	if content == nil {
		return nil
	}

	switch v := content.Kind.(type) {
	case *structpb.Value_StringValue:
		// Simple text content.
		msg.Content = v.StringValue
	case *structpb.Value_ListValue:
		// Array of content parts (for vision models).
		if v.ListValue == nil {
			return nil
		}
		msg.ContentParts = make([]llm.ContentPart, 0, len(v.ListValue.Values))
		for _, item := range v.ListValue.Values {
			part, err := parseContentPart(item)
			if err != nil {
				return err
			}
			msg.ContentParts = append(msg.ContentParts, part)
		}
	case *structpb.Value_NullValue:
		// Null content is valid (empty message).
	default:
		return errors.New("content must be a string or array")
	}
	return nil
}

// parseContentPart parses a single content part from a structpb.Value.
func parseContentPart(v *structpb.Value) (llm.ContentPart, error) {
	obj, ok := v.Kind.(*structpb.Value_StructValue)
	if !ok || obj.StructValue == nil {
		return llm.ContentPart{}, errors.New("content part must be an object")
	}

	fields := obj.StructValue.Fields
	part := llm.ContentPart{}

	// Parse type field.
	if typeVal, ok := fields["type"]; ok {
		if s, ok := typeVal.Kind.(*structpb.Value_StringValue); ok {
			part.Type = s.StringValue
		}
	}

	// Parse text field (for type="text").
	if textVal, ok := fields["text"]; ok {
		if s, ok := textVal.Kind.(*structpb.Value_StringValue); ok {
			part.Text = s.StringValue
		}
	}

	// Parse image_url field (for type="image_url").
	if imgVal, ok := fields["image_url"]; ok {
		if imgObj, ok := imgVal.Kind.(*structpb.Value_StructValue); ok && imgObj.StructValue != nil {
			imgFields := imgObj.StructValue.Fields
			imgURL := &llm.ImageURL{}
			if urlVal, ok := imgFields["url"]; ok {
				if s, ok := urlVal.Kind.(*structpb.Value_StringValue); ok {
					imgURL.URL = s.StringValue
				}
			}
			if detailVal, ok := imgFields["detail"]; ok {
				if s, ok := detailVal.Kind.(*structpb.Value_StringValue); ok {
					imgURL.Detail = s.StringValue
				}
			}
			part.ImageURL = imgURL
		}
	}

	return part, nil
}
