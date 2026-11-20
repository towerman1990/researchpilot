package service

import (
	"context"
	"crypto/rand"
	"fmt"

	"researchpilot/internal/common/modelkey"
	"researchpilot/internal/repository/model"
)

type KeyRotationStore interface {
	RotateCredentials(context.Context, uint32, func(model.RotationCredential) ([]byte, error)) error
}

type KeyRotation struct {
	Store KeyRotationStore
}

// Run re-encrypts every saved version in one transaction. Keep old key files available
// until all API and Worker instances use the new active version.
func (s KeyRotation) Run(ctx context.Context) error {
	target := modelkey.Version()
	next, err := modelkey.Cipher(target)
	if err != nil {
		return err
	}
	return s.Store.RotateCredentials(ctx, target, func(credential model.RotationCredential) ([]byte, error) {
		old, err := modelkey.Cipher(credential.KeyVersion)
		if err != nil {
			return nil, err
		}
		if len(credential.KeyCipher) < old.NonceSize() {
			return nil, fmt.Errorf("invalid_key_cipher")
		}
		aad := []byte(fmt.Sprintf("%s:%d", credential.TenantID, credential.Version))
		plain, err := old.Open(nil, credential.KeyCipher[:old.NonceSize()], credential.KeyCipher[old.NonceSize():], aad)
		if err != nil {
			return nil, fmt.Errorf("model_key_decryption_failed")
		}
		defer clear(plain)
		nonce := make([]byte, next.NonceSize())
		if _, err = rand.Read(nonce); err != nil {
			return nil, err
		}
		return next.Seal(nonce, nonce, plain, aad), nil
	})
}
