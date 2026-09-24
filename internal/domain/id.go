package domain

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

// NewID creates a locally unique, opaque ID without a network dependency.
func NewID(prefix string) (string, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "", fmt.Errorf("ID prefix must not be empty")
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate %s ID: %w", prefix, err)
	}
	return prefix + "_" + hex.EncodeToString(random), nil
}

func MustNewID(prefix string) string {
	id, err := NewID(prefix)
	if err != nil {
		panic(err)
	}
	return id
}
