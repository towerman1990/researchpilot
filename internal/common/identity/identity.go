// Package identity contains shared identifier generation utilities.
package identity

import (
	"crypto/rand"
	"encoding/hex"
)

func New(n int) string {
	raw := make([]byte, n)
	_, _ = rand.Read(raw)
	return hex.EncodeToString(raw)
}
