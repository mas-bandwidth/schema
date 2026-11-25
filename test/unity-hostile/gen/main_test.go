package main

import (
	"strings"
	"testing"
)

// UNITY-2: a carrier whose surface detection stops matching turns every case
// it carries into a row with no reader. Any such row fails generation by
// name; no list allows one.
func TestARowWithNoReaderFailsGeneration(t *testing.T) {
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
	if err := checkSkips(skipped); err == nil || !strings.Contains(err.Error(), "tables/examples PackConfig wire") {
		t.Fatalf("a row with no reader did not fail the gate by name: %v", err)
	}
	if err := checkSkips(nil); err != nil {
		t.Fatalf("CONTROL: a dispatch with a reader for every row failed: %v", err)
	}
}
