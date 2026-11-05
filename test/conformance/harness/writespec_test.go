package main

import (
	"path/filepath"
	"testing"
)

// THE REWRITE SURFACE COVERS EVERY READABLE ROW (docs/SPEC-TABLES.md §22.7):
// every corpus input whose verdict is read — an evolution (report) row that
// reads, and a framing row that reads — is written back and held to the
// canonical bytes, so each must be named by a rewrite row. And no rewrite row
// names a refused input: a rewrite promise on a row every leg refuses is empty.
func TestRewriteSurfaceCoversEveryReadableRow(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	m, err := ReadManifest(filepath.Join(root, "testdata/conformance/tables/MANIFEST.txt"), filepath.Join(root, "testdata/conformance/tables/json"))
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := readReports(filepath.Join(root, "testdata/conformance/tables/reports.txt"))
	if err != nil {
		t.Fatal(err)
	}

	rewrites := map[string]RewriteCase{}
	for _, rw := range m.Rewrites {
		rewrites[rw.Name] = rw
	}

	need := func(name, unit, root, wire string) {
		rw, ok := rewrites[name]
		if !ok {
			t.Errorf("%s reads and has no rewrite row: add `rewrite %s %s %s %s`", name, name, unit, root, wire)
			return
		}
		if rw.Unit != unit || rw.Root != root || rw.Wire != wire {
			t.Errorf("rewrite %s reads %s %s %s, and its source row reads %s %s %s", name, rw.Unit, rw.Root, rw.Wire, unit, root, wire)
		}
	}

	reports := map[string]ReportCase{}
	for _, r := range m.Reports {
		reports[r.Name] = r
	}
	framings := map[string]FramingCase{}
	for _, fc := range m.Framings {
		framings[fc.Name] = fc
	}

	for _, r := range reports {
		if c := pinned[r.Name]; c.Malformed || c.Refused {
			continue
		}
		need(r.Name, r.Unit, r.Root, r.Wire)
	}
	for _, fc := range framings {
		if fc.Refused || fc.Damaged {
			continue
		}
		need(fc.Name, fc.Unit, fc.Root, fc.Wire)
	}

	for _, rw := range m.Rewrites {
		if r, ok := reports[rw.Name]; ok {
			if c := pinned[r.Name]; c.Refused {
				t.Errorf("rewrite %s names a report row that refuses", rw.Name)
			}
			continue
		}
		if fc, ok := framings[rw.Name]; ok {
			if fc.Refused || fc.Damaged {
				t.Errorf("rewrite %s names a framing row that refuses or is damaged", rw.Name)
			}
		}
	}
}
