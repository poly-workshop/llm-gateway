package grpcadapter

import (
	"context"
	"fmt"
	"strings"
	"time"

	adminv1 "github.com/poly-workshop/llm-gateway/gen/go/llmgateway/admin/v1"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/auth"
	"github.com/poly-workshop/llm-gateway/internal/infrastructure/llmconfigstore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type LLMGatewayAdminService struct {
	adminv1.UnimplementedLLMGatewayAdminServiceServer

	signer *auth.JWTSigner
	llm    llmconfigstore.Store
}

func NewLLMGatewayAdminService(signer *auth.JWTSigner, llmStore llmconfigstore.Store) *LLMGatewayAdminService {
	return &LLMGatewayAdminService{signer: signer, llm: llmStore}
}

func (s *LLMGatewayAdminService) IssueToken(ctx context.Context, req *adminv1.IssueTokenRequest) (*adminv1.IssueTokenResponse, error) {
	_ = ctx
	if s == nil || s.signer == nil {
		return nil, status.Error(codes.FailedPrecondition, "jwt signer not configured")
	}
	subject := req.GetSubject()
	if subject == "" {
		return nil, status.Error(codes.InvalidArgument, "missing subject")
	}

	ttl := time.Duration(req.GetTtlSeconds()) * time.Second
	token, exp, err := s.signer.Sign(subject, ttl, req.GetAllowedModelIds(), time.Now())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &adminv1.IssueTokenResponse{
		AccessToken:   token,
		ExpiresAtUnix: exp.Unix(),
	}, nil
}

func (s *LLMGatewayAdminService) UpsertProviderConfig(ctx context.Context, req *adminv1.UpsertProviderConfigRequest) (*adminv1.UpsertProviderConfigResponse, error) {
	if s == nil || s.llm == nil {
		return nil, status.Error(codes.FailedPrecondition, "llm config store not configured")
	}
	c := req.GetConfig()
	if c == nil {
		return nil, status.Error(codes.InvalidArgument, "missing config")
	}
	provider, err := providerEnumToString(c.GetProvider())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	in := llmconfigstore.ProviderConfig{
		Provider:       provider,
		BaseURL:        c.GetBaseUrl(),
		APIKey:         c.GetApiKey(),
		TimeoutSeconds: c.GetTimeoutSeconds(),
	}
	if err := s.llm.UpsertProviderConfig(ctx, in); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &adminv1.UpsertProviderConfigResponse{}, nil
}

func (s *LLMGatewayAdminService) DeleteProviderConfig(ctx context.Context, req *adminv1.DeleteProviderConfigRequest) (*adminv1.DeleteProviderConfigResponse, error) {
	if s == nil || s.llm == nil {
		return nil, status.Error(codes.FailedPrecondition, "llm config store not configured")
	}
	provider, err := providerEnumToString(req.GetProvider())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := s.llm.DeleteProviderConfig(ctx, provider); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &adminv1.DeleteProviderConfigResponse{}, nil
}

func (s *LLMGatewayAdminService) ListProviderConfigs(ctx context.Context, _ *adminv1.ListProviderConfigsRequest) (*adminv1.ListProviderConfigsResponse, error) {
	if s == nil || s.llm == nil {
		return nil, status.Error(codes.FailedPrecondition, "llm config store not configured")
	}
	cfgs, err := s.llm.ListProviderConfigs(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	out := make([]*adminv1.ProviderConfigView, 0, len(cfgs))
	for _, c := range cfgs {
		p, err := providerStringToEnum(c.Provider)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		out = append(out, &adminv1.ProviderConfigView{
			Provider:       p,
			BaseUrl:        c.BaseURL,
			TimeoutSeconds: c.TimeoutSeconds,
			ApiKeyPresent:  c.APIKeyPresent,
		})
	}
	return &adminv1.ListProviderConfigsResponse{Configs: out}, nil
}

func (s *LLMGatewayAdminService) UpsertModel(ctx context.Context, req *adminv1.UpsertModelRequest) (*adminv1.UpsertModelResponse, error) {
	if s == nil || s.llm == nil {
		return nil, status.Error(codes.FailedPrecondition, "llm config store not configured")
	}
	m := req.GetModel()
	if m == nil {
		return nil, status.Error(codes.InvalidArgument, "missing model")
	}
	provider, err := providerEnumToString(m.GetProvider())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	capabilities, err := capabilityEnumsToStrings(m.GetCapabilities())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	upstream := strings.TrimSpace(m.GetUpstreamModel())
	upstream = strings.Trim(upstream, "/")
	if upstream == "" {
		return nil, status.Error(codes.InvalidArgument, "missing model upstream_model")
	}
	modelID := provider + "/" + upstream
	in := llmconfigstore.ModelSpec{
		ID:              modelID,
		Name:            upstream,
		Provider:        provider,
		Capabilities:    capabilities,
		UpstreamModel:   upstream,
		MaxOutputTokens: m.GetMaxOutputTokens(),
	}
	if err := s.llm.UpsertModel(ctx, in); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &adminv1.UpsertModelResponse{Id: modelID}, nil
}

func (s *LLMGatewayAdminService) DeleteModel(ctx context.Context, req *adminv1.DeleteModelRequest) (*adminv1.DeleteModelResponse, error) {
	if s == nil || s.llm == nil {
		return nil, status.Error(codes.FailedPrecondition, "llm config store not configured")
	}
	id := req.GetId()
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "missing model id")
	}
	if err := s.llm.DeleteModel(ctx, id); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &adminv1.DeleteModelResponse{}, nil
}

func (s *LLMGatewayAdminService) ListModels(ctx context.Context, _ *adminv1.ListModelsRequest) (*adminv1.ListModelsResponse, error) {
	if s == nil || s.llm == nil {
		return nil, status.Error(codes.FailedPrecondition, "llm config store not configured")
	}
	models, err := s.llm.ListModels(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	out := make([]*adminv1.ModelSpec, 0, len(models))
	for _, m := range models {
		p, err := providerStringToEnum(m.Provider)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		caps := capabilityStringsToEnums(m.Capabilities)
		out = append(out, &adminv1.ModelSpec{
			Id:              m.ID,
			Provider:        p,
			Capabilities:    caps,
			UpstreamModel:   m.UpstreamModel,
			MaxOutputTokens: m.MaxOutputTokens,
		})
	}
	return &adminv1.ListModelsResponse{Models: out}, nil
}

func providerEnumToString(p adminv1.ProviderType) (string, error) {
	switch p {
	case adminv1.ProviderType_PROVIDER_TYPE_DASHSCOPE:
		return "dashscope", nil
	case adminv1.ProviderType_PROVIDER_TYPE_OPENROUTER:
		return "openrouter", nil
	case adminv1.ProviderType_PROVIDER_TYPE_UNSPECIFIED:
		return "", fmt.Errorf("missing provider")
	default:
		return "", fmt.Errorf("unsupported provider: %v", p)
	}
}

func providerStringToEnum(p string) (adminv1.ProviderType, error) {
	switch p {
	case "dashscope":
		return adminv1.ProviderType_PROVIDER_TYPE_DASHSCOPE, nil
	case "openrouter":
		return adminv1.ProviderType_PROVIDER_TYPE_OPENROUTER, nil
	case "":
		return adminv1.ProviderType_PROVIDER_TYPE_UNSPECIFIED, fmt.Errorf("missing provider")
	default:
		return adminv1.ProviderType_PROVIDER_TYPE_UNSPECIFIED, fmt.Errorf("unsupported provider: %s", p)
	}
}

func capabilityEnumsToStrings(in []adminv1.ModelCapability) ([]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(in))
	for _, c := range in {
		s, err := capabilityEnumToString(c)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func capabilityEnumToString(c adminv1.ModelCapability) (string, error) {
	switch c {
	case adminv1.ModelCapability_MODEL_CAPABILITY_TEXT:
		return "text", nil
	case adminv1.ModelCapability_MODEL_CAPABILITY_IMAGES:
		return "images", nil
	case adminv1.ModelCapability_MODEL_CAPABILITY_AUDIO:
		return "audio", nil
	case adminv1.ModelCapability_MODEL_CAPABILITY_VIDEO:
		return "video", nil
	case adminv1.ModelCapability_MODEL_CAPABILITY_TOOLS:
		return "tools", nil
	case adminv1.ModelCapability_MODEL_CAPABILITY_PROMPT_CACHE:
		return "prompt_cache", nil
	case adminv1.ModelCapability_MODEL_CAPABILITY_STREAMING:
		return "streaming", nil
	case adminv1.ModelCapability_MODEL_CAPABILITY_REASONING:
		return "reasoning", nil
	case adminv1.ModelCapability_MODEL_CAPABILITY_UNSPECIFIED:
		return "", fmt.Errorf("missing capability")
	default:
		return "", fmt.Errorf("unsupported capability: %v", c)
	}
}

func capabilityStringsToEnums(in []string) []adminv1.ModelCapability {
	if len(in) == 0 {
		return nil
	}
	out := make([]adminv1.ModelCapability, 0, len(in))
	for _, s := range in {
		if c, ok := capabilityStringToEnum(s); ok {
			out = append(out, c)
		}
	}
	return out
}

func capabilityStringToEnum(s string) (adminv1.ModelCapability, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.Trim(s, "/")
	s = strings.ReplaceAll(s, "_", ".")
	switch s {
	case "supports.text", "text", "text-only", "text.only":
		return adminv1.ModelCapability_MODEL_CAPABILITY_TEXT, true
	case "supports.images", "supports.image", "images", "image", "vision", "multimodal", "multi.modal", "image.input", "image-input":
		return adminv1.ModelCapability_MODEL_CAPABILITY_IMAGES, true
	case "supports.audio", "audio", "audio.input", "audio-output", "audio.output":
		return adminv1.ModelCapability_MODEL_CAPABILITY_AUDIO, true
	case "supports.video", "video", "video.input":
		return adminv1.ModelCapability_MODEL_CAPABILITY_VIDEO, true
	case "supports.tools", "tools", "tool", "function.calling", "function-calling":
		return adminv1.ModelCapability_MODEL_CAPABILITY_TOOLS, true
	case "supports.prompt.cache", "supports.prompt_cache", "prompt.cache", "prompt-cache", "promptcache":
		return adminv1.ModelCapability_MODEL_CAPABILITY_PROMPT_CACHE, true
	case "supports.streaming", "streaming", "stream":
		return adminv1.ModelCapability_MODEL_CAPABILITY_STREAMING, true
	case "supports.reasoning", "reasoning":
		return adminv1.ModelCapability_MODEL_CAPABILITY_REASONING, true

	// Backward-compat: older stored capability strings (task-like) are treated as supports-text.
	case "chat.completions", "chat.completion", "chat", "chat.completions.stream", "chat.completion.stream", "chat.stream", "embeddings", "embedding":
		return adminv1.ModelCapability_MODEL_CAPABILITY_TEXT, true
	default:
		return adminv1.ModelCapability_MODEL_CAPABILITY_UNSPECIFIED, false
	}
}
