package main

import (
	"path/filepath"
	"testing"
)

// THE REWRITE SURFACE COVERS EVERY READABLE ROW (docs/SPEC-TABLES.md §22.7):
// each report row whose pinned verdict reads and each framing row whose hand
// verdict reads is written back by every leg, so a row that reads must have a
// rewrite row. The converse holds too: a row whose verdict refuses is not
// written back, so no rewrite row may name a refused input.
func TestRewriteSurfaceCoversEveryReadableRow(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := readReports(filepath.Join(root, defaultReports))
	if err != nil {
		t.Fatal(err)
	}
	rewrites := map[string]RewriteCase{}
	for _, rw := range m.Rewrites {
		rewrites[rw.Name] = rw
	}
	need := func(kind, name, unit, root, wire string) {
		rw, ok := rewrites[name]
		if !ok {
			t.Errorf("%s %s reads and has no rewrite row: add `rewrite %s %s %s %s`", kind, name, name, unit, root, wire)
			return
		}
		if rw.Unit != unit || rw.Root != root || rw.Wire != wire {
			t.Errorf("rewrite %s reads %s %s %s, and its source %s reads %s %s %s", name, rw.Unit, rw.Root, rw.Wire, kind, unit, root, wire)
		}
	}
	for _, r := range m.Reports {
		if c := pinned[r.Name]; !c.Malformed && !c.Refused {
			need("report", r.Name, r.Unit, r.Root, r.Wire)
		}
	}
	for _, fc := range m.Framings {
		if !fc.Damaged && !fc.Refused {
			need("framing", fc.Name, fc.Unit, fc.Root, fc.Wire)
		}
	}

	refused := map[string]bool{}
	for _, r := range m.Reports {
		if c := pinned[r.Name]; c.Malformed || c.Refused {
			refused[r.Name] = true
		}
	}
	for _, fc := range m.Framings {
		if fc.Damaged || fc.Refused {
			refused[fc.Name] = true
		}
	}
	for _, h := range m.Hostiles {
		if h.Refused {
			refused[h.Name] = true
		}
	}
	for name := range rewrites {
		if refused[name] {
			t.Errorf("rewrite %s names a refused input: remove the rewrite row", name)
		}
	}
}
