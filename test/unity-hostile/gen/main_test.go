package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// UNITY-2: a carrier whose surface detection stops matching turns every case
// it carries into a skip. The gate holds a floor on skips: a skipped row that
// known-skips.tsv does not name fails generation, and so does a named row that
// no longer skips.
func TestSkipsAreHeldToKnownSkips(t *testing.T) {
	t.Parallel()
	rows := []row{{name: "wire-valid", carrier: "wire", schema: "tables/examples", table: "PackConfig"}}
	// a unit whose generated source no longer spells the Load surface the
	// detection looks for, as when a generator rename moves it
	u := &unit{schema: "tables/examples", dir: "u00_tables_examples", namespace: "Examples", source: "// renamed surfaces"}
	_, skipped, err := renderDispatch(rows, map[string]*unit{"tables/examples": u}, validateRow)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 1 {
		t.Fatalf("a row with no detected surface was not reported as skipped: %q", skipped)
	}
	if err := checkSkips(skipped, map[string]bool{}); err == nil || !strings.Contains(err.Error(), "tables/examples PackConfig wire") {
		t.Fatalf("an unlisted skip did not fail the gate by name: %v", err)
	}
	if err := checkSkips(skipped, map[string]bool{skipped[0]: true}); err != nil {
		t.Fatalf("a listed skip failed the gate: %v", err)
	}
	if err := checkSkips(nil, map[string]bool{skipped[0]: true}); err == nil || !strings.Contains(err.Error(), "no longer skipped") {
		t.Fatalf("a listed row that no longer skips did not fail the gate: %v", err)
	}
}

// The committed list parses, and a missing file allows no skip at all.
func TestKnownSkipsFileReads(t *testing.T) {
	t.Parallel()
	if _, err := readKnownSkips(filepath.Join("..", "known-skips.tsv")); err != nil {
		t.Fatal(err)
	}
	known, err := readKnownSkips(filepath.Join(t.TempDir(), "absent.tsv"))
	if err != nil || len(known) != 0 {
		t.Fatalf("a missing list allowed %d skips (err %v)", len(known), err)
	}
	bad := filepath.Join(t.TempDir(), "bad.tsv")
	if err := os.WriteFile(bad, []byte("tables/examples PackConfig wire\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readKnownSkips(bad); err == nil {
		t.Fatal("a line that is not three tab-separated fields was accepted")
	}
}
