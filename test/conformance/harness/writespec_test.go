package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"
)

// THE REWRITE SURFACE COVERS EVERY READABLE ROW (docs/SPEC-TABLES.md §22.7):
// every corpus input whose verdict is read — an evolution (report) row that
// reads, and a framing row that reads — is written back and held to the
// canonical bytes, so each must be named by a rewrite row. And no rewrite row
// names a malformed, damaged, or refused input: a rewrite promise on a row
// every leg refuses under strict whole-load refusal is empty.
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

	for _, problem := range checkRewriteSurface(m, pinned) {
		t.Error(problem)
	}

	withRewrites := func(rewrites []RewriteCase) *Manifest {
		cp := *m
		cp.Rewrites = rewrites
		return &cp
	}
	if len(m.Rewrites) == 0 {
		t.Fatal("the manifest names no rewrite row")
	}
	if errs := checkRewriteSurface(withRewrites(m.Rewrites[1:]), pinned); len(errs) == 0 {
		t.Errorf("negative control: removing readable rewrite %s passed checkRewriteSurface", m.Rewrites[0].Name)
	}

	reportsByName := map[string]ReportCase{}
	for _, r := range m.Reports {
		reportsByName[r.Name] = r
	}
	for _, name := range []string{"a2_arm_length_off_width", "parts_index_out_of_range", "form_zero"} {
		r, ok := reportsByName[name]
		if !ok {
			t.Fatalf("negative control: report row %s is absent from manifest", name)
		}
		c, ok := pinned[name]
		if !ok || (!c.Malformed && !c.Refused) {
			t.Fatalf("negative control: report row %s is neither malformed nor refused (%+v)", name, c)
		}
		extra := append(slices.Clone(m.Rewrites), RewriteCase{
			Name: r.Name,
			Unit: r.Unit,
			Root: r.Root,
			Wire: r.Wire,
		})
		if errs := checkRewriteSurface(withRewrites(extra), pinned); len(errs) == 0 {
			t.Errorf("negative control: adding non-readable report row %s (malformed=%v refused=%v) as a rewrite passed checkRewriteSurface", name, c.Malformed, c.Refused)
		}
	}

	var badFraming *FramingCase
	for i := range m.Framings {
		if m.Framings[i].Refused || m.Framings[i].Damaged {
			badFraming = &m.Framings[i]
			break
		}
	}
	if badFraming == nil {
		t.Fatal("negative control: no refused or damaged framing row in manifest")
	}
	extraFraming := append(slices.Clone(m.Rewrites), RewriteCase{
		Name: badFraming.Name,
		Unit: badFraming.Unit,
		Root: badFraming.Root,
		Wire: badFraming.Wire,
	})
	if errs := checkRewriteSurface(withRewrites(extraFraming), pinned); len(errs) == 0 {
		t.Errorf("negative control: adding non-readable framing row %s as a rewrite passed checkRewriteSurface", badFraming.Name)
	}
}

func checkRewriteSurface(m *Manifest, pinned map[string]Counts) []string {
	var errs []string
	rewrites := map[string]RewriteCase{}
	for _, rw := range m.Rewrites {
		rewrites[rw.Name] = rw
	}

	need := func(name, unit, root, wire string) {
		rw, ok := rewrites[name]
		if !ok {
			errs = append(errs, fmt.Sprintf("%s reads and has no rewrite row: add `rewrite %s %s %s %s`", name, name, unit, root, wire))
			return
		}
		if rw.Unit != unit || rw.Root != root || rw.Wire != wire {
			errs = append(errs, fmt.Sprintf("rewrite %s reads %s %s %s, and its source row reads %s %s %s", name, rw.Unit, rw.Root, rw.Wire, unit, root, wire))
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

	for _, r := range m.Reports {
		c, ok := pinned[r.Name]
		if !ok {
			errs = append(errs, fmt.Sprintf("report %s has no pinned report in reports.txt", r.Name))
			continue
		}
		if c.Malformed || c.Refused {
			continue
		}
		need(r.Name, r.Unit, r.Root, r.Wire)
	}
	for _, fc := range m.Framings {
		if fc.Refused || fc.Damaged {
			continue
		}
		need(fc.Name, fc.Unit, fc.Root, fc.Wire)
	}

	for _, rw := range m.Rewrites {
		if r, ok := reports[rw.Name]; ok {
			if c, ok := pinned[r.Name]; !ok || c.Malformed || c.Refused {
				errs = append(errs, fmt.Sprintf("rewrite %s names a report row that is malformed or refuses", rw.Name))
			}
			continue
		}
		if fc, ok := framings[rw.Name]; ok {
			if fc.Refused || fc.Damaged {
				errs = append(errs, fmt.Sprintf("rewrite %s names a framing row that refuses or is damaged", rw.Name))
			}
		}
	}
	return errs
}
