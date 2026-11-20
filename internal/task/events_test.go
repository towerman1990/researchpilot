package task

import (
	"testing"
)

func TestExpiredCursorBoundaries(t *testing.T) {
	if CursorExpired(0, 1) || CursorExpired(10, 11) || !CursorExpired(9, 11) {
		t.Fatal("invalid replay floor")
	}
}
