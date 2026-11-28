package migrations

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"researchpilot/internal/persistence"
)

func TestMigrationChecksumCrossPlatform(t *testing.T) {
	lf := []byte("CREATE TABLE example(id INT);\n")
	crlf := []byte("CREATE TABLE example(id INT);\r\n")
	canonical, accepted := Checksums(lf)
	other, _ := Checksums(crlf)
	if canonical != other || !accepted[fmt.Sprintf("%x", sha256.Sum256(crlf))] {
		t.Fatal("checkout newline rejected")
	}
	changed, _ := Checksums([]byte("CREATE TABLE example(id BIGINT);\n"))
	if accepted[changed] {
		t.Fatal("changed SQL accepted")
	}
}

func TestMigrationsIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN not configured")
	}
	ctx := t.Context()
	db, err := persistence.OpenMySQL(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	dir := os.Getenv("MIGRATIONS_DIR")
	if dir == "" {
		dir = filepath.Join("..", "..", "..", "migrations")
	}
	const tenant = "00000000000000000000000000000001"
	const task = "00000000000000000000000000000002"
	// Reproduce a populated M1 volume with no migration ledger.
	var exists int
	if err := db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='users'").Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists == 0 {
		raw, err := os.ReadFile(filepath.Join(dir, "001_m1.sql"))
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range strings.Split(string(raw), ";") {
			if strings.TrimSpace(statement) != "" {
				if _, err = db.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
		}
		if _, err = db.Exec("INSERT INTO users VALUES(?,?,?)", tenant, tenant+"@example.test", "unused"); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec("INSERT INTO research_tasks(id,tenant_id,goal,status,event_seq,deadline_at) VALUES(?,?,?,'SUCCEEDED',1,UTC_TIMESTAMP(6))", task, tenant, "legacy"); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec("INSERT INTO research_reports VALUES(?,?,?)", tenant, task, "legacy report"); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec("UPDATE research_tasks SET status='SUCCEEDED' WHERE id=?", task); err != nil {
			t.Fatal(err)
		}
	}
	// Tests use a dedicated empty database. Re-running must preserve data and ledger.
	priorDir := t.TempDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".sql" || entry.Name() == "022_research_conversations.sql" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(priorDir, entry.Name()), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := Migrate(ctx, db, priorDir); err != nil {
		t.Fatal(err)
	}
	var conversationMigration int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version='022_research_conversations.sql'").Scan(&conversationMigration); err != nil {
		t.Fatal(err)
	}
	if conversationMigration == 0 {
		for _, row := range []struct{ id, parent string }{
			{"00000000000000000000000000000003", task},
			{"00000000000000000000000000000004", "00000000000000000000000000000003"},
			{"00000000000000000000000000000005", task},
		} {
			if _, err := db.Exec("INSERT INTO research_tasks(id,tenant_id,goal,parent_task_id,status,event_seq,deadline_at) VALUES(?,?,?,?,'SUCCEEDED',1,UTC_TIMESTAMP(6))", row.id, tenant, "legacy follow-up", row.parent); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := Migrate(ctx, db, dir); err != nil {
		t.Fatal(err)
	}
	var legacy string
	if err := db.QueryRow("SELECT markdown FROM research_reports WHERE tenant_id=? AND task_id=? AND kind='demo'", tenant, task).Scan(&legacy); err != nil || legacy != "legacy report" {
		t.Fatalf("legacy report lost: %v", err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE dirty=false").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 22 {
		t.Fatalf("ledger count=%d", count)
	}
	var conversation string
	if err := db.QueryRow("SELECT conversation_id FROM research_tasks WHERE tenant_id=? AND id=?", tenant, task).Scan(&conversation); err != nil || conversation != task {
		t.Fatalf("legacy root mapping: %s %v", conversation, err)
	}
	if conversationMigration == 0 {
		var turns int
		if err := db.QueryRow("SELECT COUNT(*) FROM research_tasks WHERE tenant_id=? AND conversation_id=?", tenant, task).Scan(&turns); err != nil || turns != 4 {
			t.Fatalf("legacy follow-ups were not grouped: %d %v", turns, err)
		}
	}
	if err := Migrate(ctx, db, dir); err != nil {
		t.Fatal(err)
	}
}
