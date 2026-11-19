package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

func main() {
	if len(os.Args) != 2 || (os.Args[1] != "api" && os.Args[1] != "worker" && os.Args[1] != "migrate" && os.Args[1] != "rotate-model-keys" && os.Args[1] != "evaluate") {
		log.Fatal("usage: researchpilot api|worker|migrate|rotate-model-keys|evaluate")
	}
	if err := run(os.Args[1]); err != nil {
		log.Fatal(err)
	}
}

func run(mode string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		return errors.New("MYSQL_DSN is required")
	}
	databaseDSN, err := mysqlConfig(dsn)
	if err != nil {
		return err
	}
	db, err := sql.Open("mysql", databaseDSN)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(24)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(3 * time.Minute)
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		return err
	}
	if mode == "migrate" {
		return migrate(ctx, db, env("MIGRATIONS_DIR", "migrations"))
	}
	var schemaReady bool
	if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version='007_trial.sql' AND dirty=false)").Scan(&schemaReady); err != nil || !schemaReady {
		return errors.New("M2 schema missing; run researchpilot migrate before starting API or worker")
	}
	if mode == "evaluate" {
		return evaluate(ctx, db)
	}
	if mode == "rotate-model-keys" {
		return rotateModelKeys(ctx, db)
	}
	if mode == "worker" {
		return runWorker(ctx, db, env("REDIS_ADDR", "127.0.0.1:6379"))
	}

	limits := redis.NewClient(&redis.Options{Addr: env("REDIS_ADDR", "127.0.0.1:6379"), Password: os.Getenv("REDIS_PASSWORD")})
	defer limits.Close()
	api := &apiServer{db: db, origin: env("PUBLIC_ORIGIN", "http://localhost:8080"), limiter: redisLimiter{limits}}
	server := &http.Server{
		Addr: env("HTTP_ADDR", ":8080"), Handler: api.routes(),
		ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second,
	}
	shutdown := make(chan struct{})
	go func() {
		defer close(shutdown)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if server.Shutdown(shutdownCtx) != nil {
			_ = server.Close()
		}
	}()
	log.Printf("API listening on %s", server.Addr)
	err = server.ListenAndServe()
	stop()
	<-shutdown
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func mysqlConfig(dsn string) (string, error) {
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		return "", errors.New("invalid MYSQL_DSN")
	}
	if config.Params == nil {
		config.Params = make(map[string]string)
	}
	config.Params["time_zone"] = "'" + "+00:00" + "'"
	config.Timeout = 5 * time.Second
	config.ReadTimeout = 10 * time.Second
	config.WriteTimeout = 10 * time.Second
	return config.FormatDSN(), nil
}

func identifier(n int) string {
	raw := make([]byte, n)
	_, _ = rand.Read(raw)
	return hex.EncodeToString(raw)
}

func terminal(status string) bool {
	return status == "SUCCEEDED" || status == "FAILED" || status == "CANCELLED" || status == "PARTIAL"
}
