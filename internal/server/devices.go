package server

import (
	"context"
	"database/sql"
	"fmt"
)

// DevRepository provides data access to the devices table.
type DeviceRepository struct {
	db *sql.DB
}

func NewDeviceRepository(db *sql.DB) *DeviceRepository {
	return &DeviceRepository{db: db}
}

// RegisterDevice persists a new device row with the given token.
// The caller generates the token and passes it in.
func (r *DeviceRepository) RegisterDevice(ctx context.Context, deviceID, name, token string) error {
	_, err := r.db.ExecContext(ctx,
		"INSERT INTO devices (device_id, name, token) VALUES (?, ?, ?)",
		deviceID, name, token,
	)
	if err != nil {
		return fmt.Errorf("register device: %w", err)
	}
	return nil
}

// GetDeviceByToken looks up a device by its auth token.
// Returns (false, nil) if no device matches.
func (r *DeviceRepository) GetDeviceByToken(ctx context.Context, token string) (string, bool, error) {
	var deviceID string
	err := r.db.QueryRowContext(ctx,
		"SELECT device_id FROM devices WHERE token = ?",
		token,
	).Scan(&deviceID)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get device by token: %w", err)
	}
	return deviceID, true, nil
}
