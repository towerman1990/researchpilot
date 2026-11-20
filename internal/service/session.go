package service

import (
	"context"
	"crypto/sha256"
)

type SessionReader interface {
	SessionUser(context.Context, []byte) (string, error)
}

func SessionUser(ctx context.Context, sessions SessionReader, token string) (string, error) {
	digest := sha256.Sum256([]byte(token))
	return sessions.SessionUser(ctx, digest[:])
}
