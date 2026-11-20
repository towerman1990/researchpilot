package repository

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"

	"gorm.io/gorm"

	"researchpilot/internal/repository/model"
)

func (s *Store) CreateUser(ctx context.Context, id, email string, hash []byte) error {
	return s.q.User.WithContext(ctx).Create(&model.User{ID: id, Email: email, PasswordHash: hash})
}

func (s *Store) UserByEmail(ctx context.Context, email string) (string, []byte, error) {
	user := s.q.User
	record, err := user.WithContext(ctx).Where(user.Email.Eq(email)).First()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil, sql.ErrNoRows
	}
	if err != nil {
		return "", nil, err
	}
	return record.ID, record.PasswordHash, nil
}

// SessionUser uses the database clock so session expiry matches MySQL writes.
func (s *Store) SessionUser(ctx context.Context, digest []byte) (string, error) {
	rows, err := s.q.Session.WithContext(ctx).FindSessionUser(hex.EncodeToString(digest))
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", sql.ErrNoRows
	}
	return rows[0], nil
}
func (s *Store) DeleteSession(ctx context.Context, digest []byte) error {
	return s.q.Session.WithContext(ctx).RemoveSession(hex.EncodeToString(digest))
}
func (s *Store) CreateSession(ctx context.Context, digest []byte, user string) error {
	return s.q.Session.WithContext(ctx).InsertSession(hex.EncodeToString(digest), user)
}
