package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"researchpilot/internal/common/identity"
	"researchpilot/internal/repository"
)

var ErrInvalidCredentials = errors.New("invalid_credentials")
var ErrRegistrationConflict = errors.New("registration_conflict")

type UserWriter interface {
	CreateUser(context.Context, string, string, []byte) error
}

// ValidateCredentials normalizes and checks registration input.
func ValidateCredentials(email, password string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" || !strings.Contains(domain, ".") ||
		strings.ContainsAny(email, " \t\r\n") || strings.Contains(domain, "@") ||
		len(email) > 254 || len(password) < 10 || len(password) > 72 {
		return "", ErrInvalidCredentials
	}
	return email, nil
}

func Register(ctx context.Context, users UserWriter, email, password string) error {
	email, err := ValidateCredentials(email, password)
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	err = users.CreateUser(ctx, identity.New(16), email, hash)
	if repository.IsDuplicate(err) {
		return ErrRegistrationConflict
	}
	return err
}

const SessionAgeSeconds = 8 * 3600

type AuthStore interface {
	UserWriter
	SessionReader
	UserByEmail(context.Context, string) (string, []byte, error)
	CreateSession(context.Context, []byte, string) error
	DeleteSession(context.Context, []byte) error
}

var dummyPassword, _ = bcrypt.GenerateFromPassword([]byte("unused-comparison-value"), bcrypt.DefaultCost)

func AccountSubject(email string) string { return strings.ToLower(strings.TrimSpace(email)) }
func Login(ctx context.Context, store AuthStore, email, password string) (string, error) {
	user, hash, err := store.UserByEmail(ctx, AccountSubject(email))
	if errors.Is(err, sql.ErrNoRows) {
		hash = dummyPassword
	} else if err != nil {
		return "", err
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(password)) != nil || user == "" {
		return "", ErrInvalidCredentials
	}
	token := identity.New(32)
	digest := sha256.Sum256([]byte(token))
	if err := store.CreateSession(ctx, digest[:], user); err != nil {
		return "", err
	}
	return token, nil
}
func Logout(ctx context.Context, store AuthStore, token string) error {
	digest := sha256.Sum256([]byte(token))
	return store.DeleteSession(ctx, digest[:])
}
