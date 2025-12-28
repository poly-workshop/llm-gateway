package auth

import "context"

type ctxKey int

const (
	ctxKeySubject ctxKey = iota
	ctxKeyMethod
)

type Method string

const (
	MethodServiceToken Method = "service_token"
	MethodSignature    Method = "signature"
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
