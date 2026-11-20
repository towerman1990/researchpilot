package modelkey

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func Version() uint32 {
	raw := os.Getenv("MODEL_MASTER_KEY_VERSION")
	if raw == "" {
		raw = "1"
	}
	n, err := strconv.ParseUint(raw, 10, 32)
	if err != nil || n == 0 {
		return 0
	}
	return uint32(n)
}

func Cipher(version uint32) (cipher.AEAD, error) {
	if version == 0 {
		return nil, errors.New("invalid_model_key_version")
	}
	path := os.Getenv(fmt.Sprintf("MODEL_MASTER_KEY_FILE_V%d", version))
	if path == "" && version == Version() {
		path = os.Getenv("MODEL_MASTER_KEY_FILE")
	}
	if path == "" {
		return nil, errors.New("model_encryption_not_configured")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("model_encryption_not_configured")
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(key) != 32 {
		return nil, errors.New("invalid_model_master_key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func Open(aead cipher.AEAD, encrypted []byte, tenant string, version uint32) (string, error) {
	if len(encrypted) < aead.NonceSize() {
		return "", errors.New("invalid_key_cipher")
	}
	nonce, ciphertext := encrypted[:aead.NonceSize()], encrypted[aead.NonceSize():]
	key, err := aead.Open(nil, nonce, ciphertext, []byte(fmt.Sprintf("%s:%d", tenant, version)))
	if err != nil {
		return "", errors.New("model_key_decryption_failed")
	}
	return string(key), nil
}
