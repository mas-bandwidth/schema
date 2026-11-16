package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFixedLineageCarriesWriterSnapshot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	paths := []string{path}
	if err := os.WriteFile(path, []byte("package probe\nfixed table Row { n int32 | min = 0, max = 10 }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := UpdateSchemaLock(first, paths); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package probe\nfixed table Row { n int32 | min = 0, max = 20 }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := UpdateSchemaLock(second, paths); err != nil {
		t.Fatal(err)
	}
	lineage, err := openFixedLineage(second)
	if err != nil {
		t.Fatal(err)
	}
	entries := lineage.Entries("Row")
	if len(entries) != 2 || entries[0].Writer == nil || entries[0].Writer.Fields[0].IntMax.Int64() != 10 || entries[1].Writer.Fields[0].IntMax.Int64() != 20 {
		t.Fatal("compiler lost the older writer constraints")
	}
	if entries[0].Record != 12 || entries[1].Record != 12 {
		t.Fatal("writer snapshot changed the body/whole-record conversion")
	}
}

func TestWriterSnapshotFixedClosureRefusesGuards(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"package probe\nfixed table Row {\n gate bool\n if gate {\n n int32\n }\n}\n",
		"package probe\ntype Body {\n gate bool\n if gate {\n n int32\n }\n}\nfixed table Row { value Body }\n",
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "Guard.schema")
		if err := os.WriteFile(path, []byte(src), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := New().Load([]string{path}); err == nil || !strings.Contains(err.Error(), "sits in an `if` branch") {
			t.Fatalf("guarded fixed writer must be refused before snapshot capture: %v", err)
		}
	}
}

// These spellings remain frontend follow-ons at this revision. They must be
// checked as source before an explicit historical import, never fabricated as
// evidence of a checked writer. The snapshot codec itself adds no such ban.
func TestWriterSnapshotHistoricalSourceChecksFollowOns(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"a ?string(8)", "a ?bytes(8)", "a ?[Key]int32"} {
		dir := t.TempDir()
		path := filepath.Join(dir, "Optional.schema")
		src := "package probe\nenum Key { Red, Blue }\nfixed table Row { " + field + " }\n"
		if err := os.WriteFile(path, []byte(src), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := New().Load([]string{path}); err == nil || !strings.Contains(err.Error(), "named follow-on") {
			t.Fatalf("historical source must pass the current frontend admission rules: %s: %v", field, err)
		}
	}
}
