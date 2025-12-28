package auth

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const mdAdminServiceToken = "x-service-token"

func isReflectionFullMethod(fullMethod string) bool {
	// gRPC reflection service method names can vary by version depending on grpc-go.
	// Allow reflection without admin token to preserve dev tooling like grpcurl.
	return strings.HasPrefix(fullMethod, "/grpc.reflection.v1alpha.ServerReflection/") ||
		strings.HasPrefix(fullMethod, "/grpc.reflection.v1.ServerReflection/")
}

func AdminUnaryServerInterceptor(expectedToken string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if info != nil && isReflectionFullMethod(info.FullMethod) {
			return handler(ctx, req)
		}
		if expectedToken == "" {
			return nil, status.Error(codes.FailedPrecondition, "admin service token not configured")
		}
		md, _ := metadata.FromIncomingContext(ctx)
		tok := first(md.Get(mdAdminServiceToken))
		if tok == "" {
			return nil, status.Error(codes.Unauthenticated, "missing x-service-token")
		}
		if tok != expectedToken {
			return nil, status.Error(codes.Unauthenticated, "invalid x-service-token")
		}
		return handler(ctx, req)
	}
}

func AdminStreamServerInterceptor(expectedToken string) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if info != nil && isReflectionFullMethod(info.FullMethod) {
			return handler(srv, ss)
		}
		if expectedToken == "" {
			return status.Error(codes.FailedPrecondition, "admin service token not configured")
		}
		md, _ := metadata.FromIncomingContext(ss.Context())
		tok := first(md.Get(mdAdminServiceToken))
		if tok == "" {
			return status.Error(codes.Unauthenticated, "missing x-service-token")
		}
		if tok != expectedToken {
			return status.Error(codes.Unauthenticated, "invalid x-service-token")
		}
		return handler(srv, ss)
	}
}
