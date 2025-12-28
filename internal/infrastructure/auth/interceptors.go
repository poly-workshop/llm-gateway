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
	mdAuthorization = "authorization"
)

func UnaryServerInterceptor(mgr *Manager) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		subject, allowedModels, method, err := authenticate(ctx, mgr)
		if err != nil {
			return nil, err
		}
		ctx = WithSubject(ctx, subject)
		ctx = WithAllowedModels(ctx, allowedModels)
		ctx = WithMethod(ctx, method)
		return handler(ctx, req)
	}
}

func StreamServerInterceptor(mgr *Manager) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		subject, allowedModels, method, err := authenticate(ss.Context(), mgr)
		if err != nil {
			return err
		}
		wrapped := &serverStreamWithContext{
			ServerStream: ss,
			ctx:          WithMethod(WithAllowedModels(WithSubject(ss.Context(), subject), allowedModels), method),
		}
		return handler(srv, wrapped)
	}
}

type serverStreamWithContext struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *serverStreamWithContext) Context() context.Context { return s.ctx }

func authenticate(ctx context.Context, mgr *Manager) (subject string, allowedModels []string, method Method, err error) {
	md, _ := metadata.FromIncomingContext(ctx)

	if mgr == nil {
		return "", nil, "", nil
	}

	authz := first(md.Get(mdAuthorization))
	if authz == "" {
		if mgr.Required() {
			return "", nil, "", status.Error(codes.Unauthenticated, "missing authorization")
		}
		return "", nil, "", nil
	}

	// Expect: "Bearer <token>"
	l := strings.ToLower(authz)
	if !strings.HasPrefix(l, "bearer ") {
		return "", nil, "", status.Error(codes.Unauthenticated, "authorization must be bearer token")
	}
	token := strings.TrimSpace(authz[len("bearer "):])
	if token == "" {
		return "", nil, "", status.Error(codes.Unauthenticated, "missing bearer token")
	}
	if subject, allowedModels, ok := mgr.AuthenticateJWT(ctx, token, time.Now()); ok {
		return subject, allowedModels, MethodJWT, nil
	}
	return "", nil, "", status.Error(codes.Unauthenticated, "invalid jwt")
}

func first(v []string) string {
	if len(v) == 0 {
		return ""
	}
	return v[0]
}
