package auth

import (
	"context"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	mdServiceToken = "x-service-token"

	mdAccessKeyID = "x-access-key-id"
	mdSignature   = "x-signature"
	mdTimestamp   = "x-timestamp"
	mdNonce       = "x-nonce"
	mdCallbackURL = "x-usage-callback"

	// Filled by HTTP gateway for HTTP-signing verification.
	mdHTTPMethod = "x-llmgw-http-method"
	mdHTTPPath   = "x-llmgw-http-path"
	mdHTTPQuery  = "x-llmgw-http-query"
	mdBodySHA256 = "x-llmgw-body-sha256"
)

func UnaryServerInterceptor(mgr *Manager) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if mgr == nil || !mgr.Enabled() {
			return handler(ctx, req)
		}
		subject, method, err := authenticate(ctx, mgr, info.FullMethod)
		if err != nil {
			return nil, err
		}
		ctx = WithSubject(ctx, subject)
		ctx = WithMethod(ctx, method)
		return handler(ctx, req)
	}
}

func StreamServerInterceptor(mgr *Manager) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if mgr == nil || !mgr.Enabled() {
			return handler(srv, ss)
		}
		subject, method, err := authenticate(ss.Context(), mgr, info.FullMethod)
		if err != nil {
			return err
		}
		wrapped := &serverStreamWithContext{
			ServerStream: ss,
			ctx:          WithMethod(WithSubject(ss.Context(), subject), method),
		}
		return handler(srv, wrapped)
	}
}

type serverStreamWithContext struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *serverStreamWithContext) Context() context.Context { return s.ctx }

func authenticate(ctx context.Context, mgr *Manager, fullMethod string) (subject string, method Method, err error) {
	md, _ := metadata.FromIncomingContext(ctx)

	// 1) ServiceToken direct access
	if tok := first(md.Get(mdServiceToken)); tok != "" {
		if subject, ok := mgr.AuthenticateServiceToken(ctx, tok); ok {
			return subject, MethodServiceToken, nil
		}
		return "", "", status.Error(codes.Unauthenticated, "invalid service token")
	}

	// For issuing temp credentials, ServiceToken is required.
	if strings.HasSuffix(fullMethod, "/IssueTemporaryCredentials") {
		return "", "", status.Error(codes.Unauthenticated, "service token required")
	}

	// 2) Temporary credentials signature access
	in := SignatureInput{
		AccessKeyID:    first(md.Get(mdAccessKeyID)),
		Signature:      first(md.Get(mdSignature)),
		Timestamp:      parseInt64(first(md.Get(mdTimestamp))),
		Nonce:          first(md.Get(mdNonce)),
		CallbackURL:    first(md.Get(mdCallbackURL)),
		HTTPMethod:     first(md.Get(mdHTTPMethod)),
		HTTPPath:       first(md.Get(mdHTTPPath)),
		HTTPQuery:      first(md.Get(mdHTTPQuery)),
		BodySHA256:     first(md.Get(mdBodySHA256)),
		GRPCFullMethod: fullMethod,
	}
	if subject, ok := mgr.AuthenticateSignature(ctx, in, time.Now()); ok {
		return subject, MethodSignature, nil
	}
	return "", "", status.Error(codes.Unauthenticated, "invalid signature")
}

func first(v []string) string {
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

func parseInt64(s string) int64 {
	var n int64
	sign := int64(1)
	for i, r := range s {
		if i == 0 && r == '-' {
			sign = -1
			continue
		}
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int64(r-'0')
	}
	return n * sign
}
