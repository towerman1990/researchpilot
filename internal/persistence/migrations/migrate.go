package migrations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MySQL DDL implicitly commits. An unfinished migration is deliberately left
// dirty so a later invocation cannot accidentally repeat partially applied DDL.
func Migrate(ctx context.Context, db *sql.DB, dir string) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var locked int
	if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK('researchpilot_schema',10)").Scan(&locked); err != nil {
		return err
	}
	if locked != 1 {
		return fmt.Errorf("migration lock unavailable")
	}
	defer conn.ExecContext(context.Background(), "SELECT RELEASE_LOCK('researchpilot_schema')")
	_, err = conn.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations(version VARCHAR(128) PRIMARY KEY,checksum CHAR(64) NOT NULL,dirty BOOLEAN NOT NULL,applied_at DATETIME(6) NULL) ENGINE=InnoDB")
	if err != nil {
		return err
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	if len(files) == 0 {
		return fmt.Errorf("no SQL migrations in %s", dir)
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		version := filepath.Base(file)
		checksum, compatibleChecksums := Checksums(raw)
		var stored string
		var dirty bool
		err = conn.QueryRowContext(ctx, "SELECT checksum,dirty FROM schema_migrations WHERE version=?", version).Scan(&stored, &dirty)
		if err == nil {
			if dirty || !compatibleChecksums[stored] {
				return fmt.Errorf("migration %s dirty or changed; inspect schema before retrying", version)
			}
			continue
		}
		if err != sql.ErrNoRows {
			return err
		}
		// M1 databases were initialized by the container image before this
		// migration ledger existed. Baseline only the known M1 schema.
		if version == "001_m1.sql" {
			var count int
			err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name IN ('users','sessions','research_tasks','request_keys','task_events','task_outbox','research_reports')").Scan(&count)
			if err != nil {
				return err
			}
			if count != 0 && count != 7 {
				return fmt.Errorf("incomplete M1 schema; refusing to baseline")
			}
			if count == 7 {
				if err = validateM1(ctx, conn); err != nil {
					return err
				}
				_, err = conn.ExecContext(ctx, "INSERT INTO schema_migrations VALUES(?,?,false,UTC_TIMESTAMP(6))", version, checksum)
				if err != nil {
					return err
				}
				continue
			}
		}
		_, err = conn.ExecContext(ctx, "INSERT INTO schema_migrations(version,checksum,dirty) VALUES(?,?,true)", version, checksum)
		if err != nil {
			return err
		}
		for _, statement := range strings.Split(string(raw), ";") {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if _, err = conn.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("%s left dirty: %w", version, err)
			}
		}
		if _, err = conn.ExecContext(ctx, "UPDATE schema_migrations SET dirty=false,applied_at=UTC_TIMESTAMP(6) WHERE version=?", version); err != nil {
			return err
		}
	}
	return nil
}

func validateM1(ctx context.Context, conn *sql.Conn) error {
	// Zero-row reads validate every column used by M1 before adopting its DDL.
	queries := []string{
		"SELECT id,email,password_hash FROM users LIMIT 0",
		"SELECT token_hash,user_id,expires_at FROM sessions LIMIT 0",
		"SELECT id,tenant_id,goal,status,phase,generation,lease_epoch,lease_until,deadline_at,event_seq,error_code,created_at FROM research_tasks LIMIT 0",
		"SELECT tenant_id,key_digest,request_digest,task_id FROM request_keys LIMIT 0",
		"SELECT tenant_id,task_id,seq,event_type,payload,created_at FROM task_events LIMIT 0",
		"SELECT tenant_id,task_id,generation,available_at,claim_epoch,claim_until,sent_at FROM task_outbox LIMIT 0",
		"SELECT tenant_id,task_id,markdown FROM research_reports LIMIT 0",
	}
	for _, q := range queries {
		rows, err := conn.QueryContext(ctx, q)
		if err != nil {
			return fmt.Errorf("M1 baseline validation: %w", err)
		}
		rows.Close()
	}
	return nil
}

// Git may check out the same SQL with LF or CRLF. Canonicalize new ledger
// entries while accepting legacy raw checksums for either checkout convention.
func Checksums(raw []byte) (string, map[string]bool) {
	lf := bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	crlf := bytes.ReplaceAll(lf, []byte("\n"), []byte("\r\n"))
	canonical := fmt.Sprintf("%x", sha256.Sum256(lf))
	return canonical, map[string]bool{canonical: true, fmt.Sprintf("%x", sha256.Sum256(raw)): true, fmt.Sprintf("%x", sha256.Sum256(crlf)): true}
}

// CheckReady verifies that the schema required by application commands is ready.
func CheckReady(ctx context.Context, db *sql.DB) error {
	var ready bool
	if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version='022_research_conversations.sql' AND dirty=false)").Scan(&ready); err != nil || !ready {
		return errors.New("required schema missing; run researchpilot migrate before starting API or worker")
	}
	return nil
}
