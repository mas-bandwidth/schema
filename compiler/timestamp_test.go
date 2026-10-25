package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// timestampUnit declares the `timestamp` kind in a variable table and in a
// fixed one (SPEC.md §4.13, docs/SPEC-TABLES.md §2.11).
const timestampUnit = `package stamp

table Event
{
    id        uint32
    opened_at timestamp
}

fixed table Session
{
    started   timestamp
}
`

// TestTimestampIsCarriedByGoAndRefusedByName pins the central refusal: Go
// carries the kind first, and generating a unit that uses it for any target
// that has not lifted the refusal is an error naming the construct, the target
// and "Go carries it first" — raised once, in Generate, with no other emitter
// edited. A target that has registered carryTimestamp (Go, C++, and squirrel
// under its tag) is not held to the refusal (docs/SPEC-TABLES.md §2.11, §16.2).
func TestTimestampIsCarriedByGoAndRefusedByName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "Stamp.schema")
	if err := os.WriteFile(path, []byte(timestampUnit), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New()
	u, err := c.Load([]string{path})
	if err != nil {
		t.Fatalf("Load refused a bare timestamp field of a table: %v", err)
	}
	if files, err := c.Generate(u, "go", Options{}); err != nil || len(files) == 0 {
		t.Fatalf("the Go target must carry the kind: %v", err)
	}
	targets := c.Targets()
	if len(targets) < 2 {
		t.Fatalf("expected the other targets to be registered: %v", targets)
	}
	for _, target := range targets {
		if carries(target, carryTimestamp) {
			continue
		}
		files, err := c.Generate(u, target, Options{})
		if err == nil {
			t.Errorf("target %s generated a unit that uses timestamp (%d files)", target, len(files))
			continue
		}
		for _, want := range []string{target + " target", "timestamp", "Event.opened_at", "Session.started", "Go carries it first"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("target %s: the refusal lacks %q: %v", target, want, err)
			}
		}
	}
	// and a unit that does not use it is untouched by the rule
	plain := filepath.Join(dir, "Plain.schema")
	if err := os.WriteFile(plain, []byte("package plain\n\nfixed table Row\n{\n    at int64\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pu, err := c.Load([]string{plain})
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if _, err := c.Generate(pu, target, Options{}); err != nil && strings.Contains(err.Error(), "timestamp") {
			t.Errorf("target %s: a unit without a timestamp was refused as one: %v", target, err)
		}
	}
}
