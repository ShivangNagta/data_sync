package server

import (
	"crypto/rand"
	"encoding/hex"
)

// newID returns a random hex identifier (used for file/version/device ids).
func newID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// newToken returns a random hex bearer token (32 random bytes -> 64 chars).
func newToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
