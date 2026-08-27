package client

import (
	"context"

	"google.golang.org/grpc/metadata"
)

// WithToken returns a context with the bearer token attached to outgoing
// gRPC metadata. The server's auth interceptor reads this to authenticate.
func WithToken(ctx context.Context, token string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}
