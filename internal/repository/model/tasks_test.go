package model

import (
	"encoding/json"
	"testing"
)

func TestSnapshotSequencePreservesPrecision(t *testing.T) {
	raw, err := json.Marshal(TaskSnapshot{Seq: 9007199254740993})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["event_seq"] != "9007199254740993" {
		t.Fatalf("event_seq = %#v", fields["event_seq"])
	}
}
