package gotable_test

// THE STRICT READ'S RULE FOR THE GO LEG's RETAIN-UNKNOWN AND WIDENING CELLS
// (ROADMAP.md "Variable tables"; docs/SPEC-TABLES.md §3.4, §4, §6.6):
//
//	one read gives either the whole value or a refusal, never a half answer.
//
// This test drives the real Go conformance driver over exactly the two rows the
// cells name (docs/roadmap-evidence/zz-strict-100-go.sexp):
//
//	vt/retain-unknown/go  retain + retain-save on `retain_node_index`: the
//	    retaining read skips an unknown field that holds a nested kind-17 node
//	    pointer, marks the target node reachable, and answers the whole value
//	    with the canonical retained wire written back.
//	vt/widening/go         report on `previous_form_wide_blob`: the fixed root
//	    whose record exceeds §3.4's 65536-byte ceiling has no file form, so the
//	    read refuses the whole value.
//
// Like TestGoDriverReportAndRetain (conformance_report_retain_test.go), this is
// a functional test: it builds and drives the real generated Go driver, so it
// skips by name without SCHEMA_SLOW, and fails by name under
// SCHEMA_REQUIRE_CORPUS=1 when make or the Go toolchain is missing.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// retainWideningManifest writes a manifest holding the two cells' rows and
// returns its path: the report row the widening cell names and the retain row
// the retain-unknown cell names.
func retainWideningManifest(t *testing.T, root string, reports []goReportRow, retains []goRetainRow) string {
	t.Helper()
	var wideBlob goReportRow
	var nodeIndex goRetainRow
	found := map[string]bool{}
	for _, r := range reports {
		if r.name == "previous_form_wide_blob" {
			wideBlob = r
			found["previous_form_wide_blob"] = true
		}
	}
	for _, r := range retains {
		if r.name == "retain_node_index" {
			nodeIndex = r
			found["retain_node_index"] = true
		}
	}
	if !found["previous_form_wide_blob"] || !found["retain_node_index"] {
		t.Fatalf("the strict cells' rows are missing from the manifest: %v", found)
	}
	var derived strings.Builder
	fmt.Fprintf(&derived, "report %s %s %s %s\n", wideBlob.name, wideBlob.unit, wideBlob.root, wideBlob.wire)
	fmt.Fprintf(&derived, "retain %s %s %s %s %s %s %s\n",
		nodeIndex.name, nodeIndex.unit, nodeIndex.root, nodeIndex.wire,
		nodeIndex.capacity, nodeIndex.ids, nodeIndex.saves)
	manifest := filepath.Join(t.TempDir(), "MANIFEST.txt")
	if err := os.WriteFile(manifest, []byte(derived.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return manifest
}

// strictCellsHold runs the two cells' rows through the real driver and returns
// the findings, empty when the strict read holds the rule: the widening cell
// refuses its whole value, and the retain-unknown cell reads the whole value
// and writes back the canonical retained wire.
func strictCellsHold(t *testing.T, root, bin string, reports []goReportRow, retains []goRetainRow) []string {
	t.Helper()
	manifest := retainWideningManifest(t, root, reports, retains)
	var wrong []string

	reportOut := goDriverRunSurface(t, root, bin, manifest, "report")
	ans, err := os.ReadFile(filepath.Join(reportOut, "previous_form_wide_blob"))
	if err != nil {
		return append(wrong, "report/previous_form_wide_blob: missing answer")
	}
	head, body, ok := strings.Cut(string(ans), "\n")
	if !ok || head == "" {
		return append(wrong, fmt.Sprintf("report/previous_form_wide_blob: malformed answer %q", ans))
	}
	gotVerdict := head
	if idx := strings.LastIndexByte(head, ','); idx >= 0 {
		gotVerdict = head[idx+1:]
	}
	// A wrong verdict is the finding. The body and the save wire are what a
	// refusal must not write; checking them after a read would name a refusal
	// that did not happen, and the widening negative control requires the one
	// verdict line (the same cut goDriverCheckAll makes).
	if gotVerdict != "refused" {
		wrong = append(wrong, fmt.Sprintf("report/previous_form_wide_blob: got verdict %s, want refused", gotVerdict))
	} else {
		if body != "" {
			wrong = append(wrong, "report/previous_form_wide_blob: refused read wrote value bytes")
		}
		if _, err := os.Stat(filepath.Join(reportOut, "previous_form_wide_blob.wire")); err == nil {
			wrong = append(wrong, "report/previous_form_wide_blob: refused read wrote save wire")
		}
	}

	retainOut := goDriverRunSurface(t, root, bin, manifest, "retain")
	got, err := os.ReadFile(filepath.Join(retainOut, "retain_node_index"))
	if err != nil {
		wrong = append(wrong, "retain/retain_node_index: missing answer")
	} else {
		wantVal, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "retain-values", "retain_node_index.json"))
		if err != nil {
			t.Fatal(err)
		}
		want := append([]byte("read\n"), wantVal...)
		if !bytes.Equal(got, want) {
			wrong = append(wrong, "retain/retain_node_index: answer differs from read + retain-values/retain_node_index.json")
		}
	}

	saveOut := goDriverRunSurface(t, root, bin, manifest, "retain-save")
	gotSave, err := os.ReadFile(filepath.Join(saveOut, "retain_node_index"))
	if err != nil {
		wrong = append(wrong, "retain-save/retain_node_index: driver wrote nothing")
	} else {
		wantSave, err := os.ReadFile(filepath.Join(root, nodeIndexSaves(retains)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(gotSave, wantSave) {
			wrong = append(wrong, fmt.Sprintf("retain-save/retain_node_index: saved wire (%d bytes) differs from the canonical save (%d bytes)", len(gotSave), len(wantSave)))
		}
	}
	return wrong
}

func nodeIndexSaves(retains []goRetainRow) string {
	for _, r := range retains {
		if r.name == "retain_node_index" {
			return r.saves
		}
	}
	return ""
}

// TestGoRetainAndWideningStrict holds the strict read's rule for the two cells
// (retain-unknown and widening) over the real Go driver.
func TestGoRetainAndWideningStrict(t *testing.T) {
	root := goConformanceRoot(t)
	reports, retains := goReportAndRetainRows(t, root)
	bin := goBuildDriver(t, root, "")
	for _, w := range strictCellsHold(t, root, bin, reports, retains) {
		t.Errorf("%s", w)
	}
}

// TestGoRetainAndWideningStrictNegativeControl plants a fault in the real
// artifact for each cell and requires the strict read to go red:
//
//   - dropping the noFileForm binding on the oversized fixed root WideBlob must
//     turn the widening cell's refusal into a read (report/previous_form_wide_blob);
//   - dropping the markUnknownNodeRefs call must break the retain-unknown cell's
//     whole-value read and save (retain/retain_node_index, retain-save/retain_node_index).
func TestGoRetainAndWideningStrictNegativeControl(t *testing.T) {
	root := goConformanceRoot(t)
	reports, retains := goReportAndRetainRows(t, root)

	t.Run("widening_no_file_form", func(t *testing.T) {
		bin := buildSabotagedGoDriver(
			t,
			root,
			"test/conformance/go/fixed.go",
			`bind("tabledemo", "WideBlob", noFileForm)`,
			`// SABOTAGED: WideBlob left on its variable-form load`,
		)
		wrong := strictCellsHold(t, root, bin, reports, retains)
		want := "report/previous_form_wide_blob: got verdict read, want refused"
		if len(wrong) != 1 || wrong[0] != want {
			t.Fatalf("NEGATIVE CONTROL FAILED: removing WideBlob noFileForm binding produced %v, want [%q]", wrong, want)
		}
	})

	t.Run("retain_unknown_node_refs", func(t *testing.T) {
		bin := buildSabotagedGoDriver(
			t,
			root,
			"test/conformance/go/retain.go",
			`r.Nodes.Mark(index)`,
			`if r.Nested && index > 0 && int(index) <= len(r.Nodes.Entries) { r.Nodes.Entries[index-1].Offset &^= 1 << 63 } // SABOTAGED: unknown nested kind-17 pointers do not mark target nodes`,
		)
		wrong := strictCellsHold(t, root, bin, reports, retains)
		want := []string{
			"retain/retain_node_index: answer differs from read + retain-values/retain_node_index.json",
			"retain-save/retain_node_index: driver wrote nothing",
		}
		if len(wrong) != len(want) || wrong[0] != want[0] || wrong[1] != want[1] {
			t.Fatalf("NEGATIVE CONTROL FAILED: removing markUnknownNodeRefs produced %v, want %v", wrong, want)
		}
	})
}
