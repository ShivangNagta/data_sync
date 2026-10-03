package client

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/shivangnagta/data_sync/proto/sync"
)

type SyncClient struct {
	conn  *grpc.ClientConn
	api   sync.SyncServiceClient
	Token string
}

type ClientConfig struct {
	Addr  string
	Token string
}

func NewSyncClient(cfg ClientConfig) (*SyncClient, error) {
	conn, err := grpc.NewClient(cfg.Addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}

	return &SyncClient{conn: conn, api: sync.NewSyncServiceClient(conn), Token: cfg.Token}, nil
}

func (c *SyncClient) Close() error { return c.conn.Close() }

func (c *SyncClient) AuthContext(ctx context.Context) context.Context {
	return WithToken(ctx, c.Token)
}

func (c *SyncClient) API() sync.SyncServiceClient { return c.api }

func (c *SyncClient) Backend() SyncBackend { return grpcBackend{client: c} }
