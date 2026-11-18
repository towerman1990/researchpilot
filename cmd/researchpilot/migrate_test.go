package main

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestMigrationChecksumCrossPlatform(t *testing.T) {
	lf := []byte("CREATE TABLE example(id INT);\n")
	crlf := []byte("CREATE TABLE example(id INT);\r\n")
	canonical, accepted := migrationChecksums(lf)
	other, _ := migrationChecksums(crlf)
	if canonical != other || !accepted[fmt.Sprintf("%x", sha256.Sum256(crlf))] {
		t.Fatal("checkout newline rejected")
	}
	changed, _ := migrationChecksums([]byte("CREATE TABLE example(id BIGINT);\n"))
	if accepted[changed] {
		t.Fatal("changed SQL accepted")
	}
}
