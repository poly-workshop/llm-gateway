package auth

import "context"

type ctxKey int

const (
	ctxKeySubject ctxKey = iota
	ctxKeyMethod
	ctxKeyAllowedModels
	ctxKeyJTI
)

type Method string

const (
	MethodJWT Method = "jwt"
)

func WithSubject(ctx context.Context, subject string) context.Context {
	if subject == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKeySubject, subject)
}

func SubjectFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(ctxKeySubject).(string)
	return v
}

func WithMethod(ctx context.Context, m Method) context.Context {
	if m == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyMethod, m)
}

func MethodFromContext(ctx context.Context) Method {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(ctxKeyMethod).(Method)
	return v
}

func WithAllowedModels(ctx context.Context, modelIDs []string) context.Context {
	if ctx == nil {
		return ctx
	}
	if len(modelIDs) == 0 {
		return ctx
	}
	// Copy to avoid retaining caller slice.
	out := make([]string, 0, len(modelIDs))
	for _, id := range modelIDs {
		if id == "" {
			continue
		}
		out = append(out, id)
	}
	if len(out) == 0 {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyAllowedModels, out)
}

// AllowedModelsFromContext returns the model allowlist embedded in the auth token.
// If nil/empty, the request is not restricted.
func AllowedModelsFromContext(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	v, _ := ctx.Value(ctxKeyAllowedModels).([]string)
	return v
}

func WithJTI(ctx context.Context, jti string) context.Context {
	if jti == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyJTI, jti)
}

func JTIFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(ctxKeyJTI).(string)
	return v
}
