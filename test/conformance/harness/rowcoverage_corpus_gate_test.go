// The row-coverage gate (gates/row-coverage/corpus): every row the manifest
// names has actual corpus bytes for every native leg that reads it. The data
// is the party's ledger (testdata/conformance/tables/MANIFEST.txt, FORMAT.md
// there) and the nine legs are what the harness discovers under
// test/conformance/<lang>/driver -- the same registry the matrix reads.
//
// Every row kind maps to file paths a leg will read, and the gate enumerates
// both sets from the tree: the rows from the committed manifest and the legs
// from the discovered driver registry, never a fixed list. A row that lacks
// corpus bytes is the corpus losing its own expectation for every leg, and
// the test names them by row and leg so a removed ledger line or corpus file
// localises immediately.
//
// The control is what makes the gate a gate: removing one entry -- a ledger
// line or a corpus file -- turns it RED with the missing row and leg named,
// and the entries that hold it back (a stale path, an unknown row kind, a
// duplicate name, a wrongly-exempt no-text instance) are errors in their own
// right.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// legNames discovers the nine native legs from test/conformance/<lang>/driver,
// excluding `harness` (this package's directory) and `canary` (the
// sprint-canary end-to-end positive control, which is its own single C file
// rather than a conformance driver -- test/conformance/canary/).
func legNames(t *testing.T, driversDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(driversDir)
	if err != nil {
		t.Fatalf("%s: %v", driversDir, err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "harness" || name == "canary" {
			continue
		}
		if _, err := os.Stat(filepath.Join(driversDir, name, "driver")); err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(driversDir, name, "ci.json")); err != nil {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// joinRoot joins the repository root onto a manifest-relative path.
func joinRoot(root, path string) string {
	return filepath.Join(root, path)
}

// exists reports whether a future file path exists on disk.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// missing appends one RED line per (row, leg) pair, so a single missing
// corpus file proves the gate fires for every leg that reads the row.
func missing(list *[]string, row, leg, what, path string) {
	*list = append(*list, fmt.Sprintf("row %q, leg %q: missing %s %s", row, leg, what, path))
}

// single appends a one-off ledger-integrity failure that does not multiply
// across legs (duplicate, stale, unknown, wrongly-exempt).
func single(list *[]string, format string, args ...any) {
	*list = append(*list, fmt.Sprintf(format, args...))
}

// TestRowCoverageCorpusGate is the gate. The legs and the rows are both read
// from the tree, so no list here can drift from what the harness discovers or
// what the manifest names.
func TestRowCoverageCorpusGate(t *testing.T) {
	// the manifest's paths are repository-relative and the test runs in its
	// own directory, so the root is joined in rather than chdir'd: a chdir
	// would be a chdir for every other test in the package, exactly as
	// lock_test.go agrees.
	const root = "../../../"

	driversDir := joinRoot(root, "test/conformance")
	legs := legNames(t, driversDir)
	if len(legs) == 0 {
		t.Fatal("the driver registry names no leg -- a gate that ran green tested nothing")
	}

	manifestPath := joinRoot(root, defaultManifest)
	jsonDir := joinRoot(root, defaultJSONDir)
	m, err := ReadManifest(manifestPath, jsonDir)
	if err != nil {
		t.Fatalf("%s: %v", manifestPath, err)
	}

	// every row survives a single round of forward and cross checks; the
	// gets collect failures so one run reports every defect rather than the
	// first one it met.
	var fails []string

	// unit names must be unique: an instance that names a unit the manifest
	// has not loaded is the loader reading nothing.
	unitKeys := map[string]bool{}
	for _, u := range m.Units {
		if unitKeys[u.Key] {
			single(&fails, "duplicate unit key %q in the ledger", u.Key)
		}
		unitKeys[u.Key] = true
	}

	// index instance and block names so cook-write, retention, forgery and
	// refusal rows can be cross-checked against them.
	instanceByName := map[string]Instance{}
	for _, inst := range m.Instances {
		if _, dup := instanceByName[inst.Name]; dup {
			single(&fails, "duplicate instance row %q in the ledger", inst.Name)
		}
		instanceByName[inst.Name] = inst
	}
	blockByName := map[string]bool{}
	for _, b := range m.Blocks {
		if blockByName[b.Name] {
			single(&fails, "duplicate block row %q in the ledger", b.Name)
		}
		blockByName[b.Name] = true
	}
	cookByName := map[string]bool{}
	for _, c := range m.Cooks {
		if cookByName[c.Case] {
			single(&fails, "duplicate cook row %q in the ledger", c.Case)
		}
		cookByName[c.Case] = true
	}
	forgeryByName := map[string]bool{}
	for _, f := range m.Forgeries {
		if forgeryByName[f.Name] {
			single(&fails, "duplicate forgery row %q in the ledger", f.Name)
		}
		forgeryByName[f.Name] = true
	}

	// instances: WIRE bytes for every leg; TEXT bytes for every leg unless
	// the row carries the no-text marker, which is the corpus saying so on
	// the row's own line -- a json/<name>.json beside it is wrongly exempt,
	// exactly the form-§16.7 refusal the marker exists to mark.
	for _, inst := range m.Instances {
		wire := joinRoot(root, inst.Wire)
		jsonPath := joinRoot(root, defaultJSONDir) + "/" + inst.Name + ".json"
		for _, leg := range legs {
			if !exists(wire) {
				missing(&fails, inst.Name, leg, "instance wire", inst.Wire)
			}
			if inst.NoText {
				if exists(jsonPath) {
					single(&fails, "row %q, leg %q: a no-text instance carries a json text (§16.7)",
						inst.Name, leg)
				}
			} else {
				if !exists(jsonPath) {
					missing(&fails, inst.Name, leg, "instance json text", jsonPath)
				}
			}
		}
	}

	// reports: WIRE bytes for every leg alongside the count row the
	// generated reports.txt owes it.
	reportNames := readReportsNames(joinRoot(root, defaultReports))
	for _, r := range m.Reports {
		wire := joinRoot(root, r.Wire)
		for _, leg := range legs {
			if !exists(wire) {
				missing(&fails, r.Name, leg, "report wire", r.Wire)
			}
		}
		if _, ok := reportNames[r.Name]; !ok {
			single(&fails, "row %q: reports.txt has no count for it (run `make conformance-generate`)", r.Name)
		}
	}

	// connections: the announcement wire bytes the connection surfaces read.
	for _, c := range m.Connections {
		wire := joinRoot(root, c.Wire)
		for _, leg := range legs {
			if !exists(wire) {
				missing(&fails, c.Key, leg, "connection wire", c.Wire)
			}
		}
	}

	// messages: BOTH forms the resolved-form pair pins.
	for _, msg := range m.Messages {
		file := joinRoot(root, msg.FileWire)
		message := joinRoot(root, msg.MessageWire)
		for _, leg := range legs {
			if !exists(file) {
				missing(&fails, msg.Name, leg, "message file-form wire", msg.FileWire)
			}
			if !exists(message) {
				missing(&fails, msg.Name, leg, "message message-form wire", msg.MessageWire)
			}
		}
	}

	// retain (file form) and retain-message (form 2): the wire the loader
	// reads and every save the pair writes back. A save's path is the
	// land-mark of its body, so a missing file is the row losing one body's
	// bytes, and a comma-separated list of them is one CASE the message
	// surface carries.
	for _, rc := range m.Retains {
		wire := joinRoot(root, rc.Wire)
		for _, leg := range legs {
			if !exists(wire) {
				missing(&fails, rc.Name, leg, "retain wire", rc.Wire)
			}
			for _, save := range rc.Saves {
				savePath := joinRoot(root, save)
				if !exists(savePath) {
					missing(&fails, rc.Name, leg, "retain save", save)
				}
			}
		}
	}

	// json-hostile: each case is a tree pack itself reads, so the tree
	// must exist with the root's own file.
	for _, h := range m.Hostiles {
		treePath := joinRoot(root, h.Tree)
		jsonPath := filepath.Join(treePath, h.Root+".json")
		for _, leg := range legs {
			if !exists(treePath) {
				missing(&fails, h.Name, leg, "json-hostile tree", h.Tree)
			}
			if !exists(jsonPath) {
				missing(&fails, h.Name, leg, "json-hostile tree root", jsonPath)
			}
		}
	}

	// cook: the canonical node dump every reader must produce. The cook
	// file itself is materialised by test/cookgen at run time; the dump is
	// pinned, so the dump's absence is the row losing its expectation.
	for _, c := range m.Cooks {
		dump := joinRoot(root, c.Dump)
		for _, leg := range legs {
			if !exists(dump) {
				missing(&fails, c.Case, leg, "cook dump", c.Dump)
			}
		}
	}

	// cook-write: the pair generated from the instance wire. Both byte
	// orders live here, and a runtime that does not carry one is a cook
	// written wrong for everyone the corpus hands it to.
	for _, cw := range m.CookWrites {
		little := joinRoot(root, cw.Little)
		big := joinRoot(root, cw.Big)
		for _, leg := range legs {
			if !exists(little) {
				missing(&fails, cw.Instance, leg, "cook-write little-endian", cw.Little)
			}
			if !exists(big) {
				missing(&fails, cw.Instance, leg, "cook-write big-endian", cw.Big)
			}
		}
		// a cook-write REACHES an instance by name, and a name that reaches
		// nothing is a row the run would silently expect nothing from.
		if _, ok := instanceByName[cw.Instance]; !ok {
			single(&fails, "row %q: cook-write names no instance", cw.Instance)
		}
	}

	// block: the binary image an Open must accept, and the canonical row
	// dump its reader produces out of it. The two are pin / pin, and a
	// block-dump surface whose dump is gone is the gate's own field.
	for _, b := range m.Blocks {
		file := joinRoot(root, b.File)
		dump := joinRoot(root, b.Dump)
		for _, leg := range legs {
			if !exists(file) {
				missing(&fails, b.Name, leg, "block file", b.File)
			}
			if !exists(dump) {
				missing(&fails, b.Name, leg, "block dump", b.Dump)
			}
		}
	}

	// forgery: patches over a base fixture, and the base must exist for the
	// harness to materialise it. A cook base is generated by test/cookgen
	// at run time; a block base is the binary file the row already names.
	for _, f := range m.Forgeries {
		hasBlockBase := blockByName[f.Base]
		hasCookBase := cookByName[f.Base]
		if !hasBlockBase && !hasCookBase {
			single(&fails, "row %q: forgery base %q names no block or cook row", f.Name, f.Base)
		}
	}

	// refusal: names a forgery, and a name that reaches nothing is a
	// reason nobody would be asked for.
	for _, r := range m.Refusals {
		if !forgeryByName[r.Forgery] {
			single(&fails, "row %q: refusal names no forgery %q", r.Forgery, r.Forgery)
		}
	}

	for _, f := range fails {
		t.Error(f)
	}
	if len(fails) == 0 {
		t.Logf("the gate is green for %d rows across %d legs",
			len(m.Instances)+len(m.Reports)+len(m.Connections)+len(m.Messages)+
				len(m.Retains)+len(m.Hostiles)+len(m.Cooks)+len(m.CookWrites)+
				len(m.Blocks)+len(m.Forgeries)+len(m.Refusals), len(legs))
	}
}

// readReportsNames reads reports.txt and returns the set of case names the
// generation step named a count for, so a report row whose case the file
// never generated is a row whose expectation was never written.
func readReportsNames(path string) map[string]bool {
	out := map[string]bool{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		out[f[0]] = true
	}
	return out
}
