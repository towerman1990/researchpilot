package model

import "time"

// User and Session reflect the authentication tables in migrations/001_m1.sql.
type User struct {
	ID           string `gorm:"column:id;primaryKey"`
	Email        string `gorm:"column:email"`
	PasswordHash []byte `gorm:"column:password_hash"`
}

func (User) TableName() string { return "users" }

type Session struct {
	TokenHash []byte    `gorm:"column:token_hash;primaryKey"`
	UserID    string    `gorm:"column:user_id"`
	ExpiresAt time.Time `gorm:"column:expires_at"`
}

func (Session) TableName() string { return "sessions" }
