package server

import (
	"context"
	"os"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type deviceIDKey struct{}

type AuthInterceptor struct {
	token string
}

func NewAuthInterceptor() *AuthInterceptor {
	return &AuthInterceptor{token: os.Getenv("SYNC_TOKEN")}
}

func (a *AuthInterceptor) Unary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		ctx, err := a.authenticate(ctx)
		if err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

func (a *AuthInterceptor) Stream() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx, err := a.authenticate(ss.Context())
		if err != nil {
			return err
		}
		return handler(srv, &wrappedStream{ss, ctx})
	}
}

func (a *AuthInterceptor) authenticate(ctx context.Context) (context.Context, error) {
	token, ok := bearerToken(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing bearer token")
	}
	if a.token == "" || token != a.token {
		return nil, status.Error(codes.Unauthenticated, "invalid token")
	}
	return context.WithValue(ctx, deviceIDKey{}, "device"), nil
}

func bearerToken(ctx context.Context) (string, bool) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", false
	}
	vals := md.Get("authorization")
	if len(vals) == 0 {
		return "", false
	}
	parts := strings.SplitN(vals[0], " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return "", false
	}
	return strings.TrimSpace(parts[1]), true
}

func UploaderFrom(ctx context.Context) (Uploader, bool) {
	id, ok := ctx.Value(deviceIDKey{}).(string)
	if !ok || id == "" {
		return Uploader{}, false
	}
	return Uploader{DeviceID: id}, true
}

type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context {
	return w.ctx
}
