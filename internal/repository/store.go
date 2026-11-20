// Package repository contains database access used by application services.
package repository

import (
	"database/sql"
	"errors"

	driver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"researchpilot/internal/repository/query"
)

// Store shares the application's existing database pool; it does not own it.
type Store struct {
	db *gorm.DB
	q  *query.Query
}

func New(db *sql.DB) (*Store, error) {
	gdb, err := gorm.Open(mysql.New(mysql.Config{Conn: db, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	return &Store{db: gdb, q: query.Use(gdb)}, nil
}

func IsDuplicate(err error) bool {
	var dbErr *driver.MySQLError
	return errors.As(err, &dbErr) && dbErr.Number == 1062
}
