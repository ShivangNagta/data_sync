// This file contains the client gRPC connection and device registration.

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/shivangnagta/data_sync/proto/sync"
)

// SyncClient wraps the gRPC connection and the authenticated device token.
type SyncClient struct {
	conn     *grpc.ClientConn
	api      sync.SyncServiceClient
	DeviceID string
	Token    string
}

// ClientConfig holds the settings needed to connect and register.
type ClientConfig struct {
	Addr      string // server gRPC address e.g. "localhost:54321"
	Name      string // device name
	TokenFile string // where to persist the device token
}

func NewSyncClient(cfg ClientConfig) (*SyncClient, error) {
	conn, err := grpc.NewClient(cfg.Addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}

	sc := &SyncClient{conn: conn, api: sync.NewSyncServiceClient(conn)}
	if err := sc.ensureRegistered(context.Background(), cfg); err != nil {
		conn.Close()
		return nil, err
	}
	return sc, nil
}

func (c *SyncClient) Close() error { return c.conn.Close() }

// ensureRegistered reuses a persisted token if present, otherwise registers
// a new device and saves the token locally.
func (c *SyncClient) ensureRegistered(ctx context.Context, cfg ClientConfig) error {
	// Try an already authenticated call; if the saved token is valid we're done.
	if saved, err := loadToken(cfg.TokenFile); err == nil && saved.DeviceID != "" {
		c.DeviceID = saved.DeviceID
		c.Token = saved.Token
		return nil
	}

	reg, err := c.api.RegisterDevice(ctx, &sync.RegisterDeviceRequest{Name: cfg.Name})
	if err != nil {
		return fmt.Errorf("register device: %w", err)
	}
	c.DeviceID = reg.DeviceId
	c.Token = reg.Token

	if err := saveToken(cfg.TokenFile, reg.DeviceId, reg.Token); err != nil {
		return fmt.Errorf("save token: %w", err)
	}
	return nil
}

// AuthContext returns a context carrying the bearer token for authenticated calls.
func (c *SyncClient) AuthContext(ctx context.Context) context.Context {
	return WithToken(ctx, c.Token)
}

func (c *SyncClient) API() sync.SyncServiceClient { return c.api }

var errUnauthenticated = status.Error(codes.Unauthenticated, "unauthenticated")

type savedIdentity struct {
	DeviceID string `json:"device_id"`
	Token    string `json:"token"`
}

func loadToken(path string) (savedIdentity, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return savedIdentity{}, err
	}
	var id savedIdentity
	if err := json.Unmarshal(b, &id); err != nil {
		return savedIdentity{}, err
	}
	return id, nil
}

func saveToken(path, deviceID, token string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, _ := json.Marshal(savedIdentity{DeviceID: deviceID, Token: token})
	return os.WriteFile(path, b, 0o600)
}
