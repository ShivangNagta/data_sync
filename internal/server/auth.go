package server

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type deviceIDKey struct{}

// AuthInterceptor authenticates incoming RPCs. It runs before each handler,
// resolving the bearer token to a device and injecting its ID into context.
// RegisterDevice is excluded since no token exists yet on first contact.
type AuthInterceptor struct {
	devices *DeviceRepository
}

func NewAuthInterceptor(devices *DeviceRepository) *AuthInterceptor {
	return &AuthInterceptor{devices: devices}
}

// Unary returns a unary server interceptor used for non-streaming RPCs.
func (a *AuthInterceptor) Unary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if info.FullMethod == "/sync.SyncService/RegisterDevice" {
			return handler(ctx, req)
		}

		ctx, err := a.authenticate(ctx)
		if err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// Stream returns a stream server interceptor used for streaming RPCs.
func (a *AuthInterceptor) Stream() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if info.FullMethod == "/sync.SyncService/RegisterDevice" {
			return handler(srv, ss)
		}

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

	deviceID, found, err := a.devices.GetDeviceByToken(ctx, token)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "auth lookup: %v", err)
	}
	if !found {
		return nil, status.Error(codes.Unauthenticated, "invalid token")
	}

	return context.WithValue(ctx, deviceIDKey{}, deviceID), nil
}

// bearerToken extracts the token from an "authorization: bearer <token>" header.
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

// UploaderFrom reads the authenticated device ID from the context.
func UploaderFrom(ctx context.Context) (Uploader, bool) {
	id, ok := ctx.Value(deviceIDKey{}).(string)
	if !ok || id == "" {
		return Uploader{}, false
	}
	return Uploader{DeviceID: id}, true
}

// wrappedStream overrides Context to pass the authenticated context
// into stream handlers.
type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context {
	return w.ctx
}
