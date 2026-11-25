package main

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/inputfamilies"
)


func retainRefusalCase(t *testing.T, saved, before, wire string) (string, expectation) {
	t.Helper()
	dir := t.TempDir()
	answer := filepath.Join(dir, "row")
	write := func(path, text string) {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(answer, saved)
	write(answer+".before", before)
	if wire != "" {
		write(answer+".wire", wire)
	}
	return answer, expectation{name: "row", want: []byte("retained"), save: []byte("plain")}
}

func TestRetainRefusalScoresTheSecondSave(t *testing.T) {
	for _, c := range []struct {
		name, wire string
		pass       bool
	}{
		{"the plain save", "plain", true},
		{"the first save again", "retained", false},
		{"other bytes", "other", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			answer, e := retainRefusalCase(t, retainRefusalSaved, "retained", c.wire)
			r := &result{}
			scoreRetainRefusal("js", answer, e, r)
			if (r.pass == 1) != c.pass || (len(r.failures) == 0) != c.pass {
				t.Fatalf("pass %d, failures %q; want pass %v", r.pass, r.failures, c.pass)
			}
		})
	}
}

// save-refused skips the second save's byte check, so only a leg whose store
// is bound to the root it loaded may give it (retainRefusalUnboundLegs). From
// every other leg on the roster it is red.
func TestRetainRefusalSaveRefusedOnlyFromBoundLegs(t *testing.T) {
	for lang := range retainRefusalUnboundLegs {
		if !slices.Contains(inputfamilies.Legs, lang) {
			t.Fatalf("retainRefusalUnboundLegs names %s, which is not on the roster", lang)
		}
	}
	for _, lang := range inputfamilies.Legs {
		t.Run(lang, func(t *testing.T) {
			answer, e := retainRefusalCase(t, retainRefusalUnbound, "retained", "")
			r := &result{}
			scoreRetainRefusal(lang, answer, e, r)
			if retainRefusalUnboundLegs[lang] {
				if r.pass != 1 || len(r.failures) != 0 {
					t.Fatalf("a bound leg's save-refused: pass %d, failures %q", r.pass, r.failures)
				}
				return
			}
			if r.pass != 0 || len(r.failures) != 1 || !strings.Contains(r.failures[0], "owes the second save's bytes") {
				t.Fatalf("an unbound leg's save-refused: pass %d, failures %q", r.pass, r.failures)
			}
		})
	}
}

// generateRetainRefusals writes and prunes only under the root it is given.
// It once wrote the fixed RetainRefusalDir, so a generate over a test's
// temporary root (TestGenerateReportRowsRetainDamageVerdict, whose manifest
// has no retain-refusal rows) pruned every committed golden.
func TestGenerateRetainRefusalsWritesOnlyUnderItsRoot(t *testing.T) {
	m, _, u := corpus(t)
	if len(m.RetainRefusals) == 0 {
		t.Fatal("the corpus has no retain-refusal rows")
	}
	snapshot := func() map[string]string {
		entries, err := os.ReadDir(RetainRefusalDir)
		if err != nil {
			t.Fatal(err)
		}
		files := map[string]string{}
		for _, e := range entries {
			data, err := os.ReadFile(filepath.Join(RetainRefusalDir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			files[e.Name()] = string(data)
		}
		return files
	}
	committed := snapshot()
	if len(committed) != 2*len(m.RetainRefusals) {
		t.Fatalf("%s holds %d files, want %d", RetainRefusalDir, len(committed), 2*len(m.RetainRefusals))
	}
	unchanged := func(when string) {
		t.Helper()
		if now := snapshot(); !maps.Equal(now, committed) {
			t.Fatalf("%s: the committed goldens in %s changed (%d files, was %d)", when, RetainRefusalDir, len(now), len(committed))
		}
	}

	root := t.TempDir()
	jsonDir := filepath.Join(root, "json")
	tmp := filepath.Join(root, "retain-refusal")

	// no rows: nothing to keep, and the prune runs on the temporary root
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "stray.bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := generateRetainRefusals(u, &Manifest{Units: m.Units}, jsonDir); err != nil {
		t.Fatal(err)
	}
	unchanged("a manifest with no rows")
	if _, err := os.Stat(filepath.Join(tmp, "stray.bin")); !os.IsNotExist(err) {
		t.Fatalf("the prune did not run on the given root: %v", err)
	}

	// every row: the goldens land under the temporary root, byte for byte
	if err := generateRetainRefusals(u, m, jsonDir); err != nil {
		t.Fatal(err)
	}
	unchanged("the corpus's rows")
	for name, want := range committed {
		got, err := os.ReadFile(filepath.Join(tmp, name))
		if err != nil {
			t.Fatalf("the generator wrote no %s under its root: %v", name, err)
		}
		if string(got) != want {
			t.Fatalf("%s under the temporary root differs from the committed golden", name)
		}
	}
}

// The retired retain-refusal ledger (retainrefusal.go) held named cases back
// from red. A file by its name beside the manifest stops the run and the
// registry rows, and the committed corpus carries none.
func TestTheRetiredRetainRefusalLedgerIsRefused(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "MANIFEST.txt")
	if err := refuseRetiredRetainRefusalLedger(manifest); err != nil {
		t.Fatalf("no ledger beside the manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, RetiredRetainRefusalLedger), []byte("lobster row owed: a cause\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := refuseRetiredRetainRefusalLedger(manifest)
	if err == nil || !strings.Contains(err.Error(), "retired") {
		t.Fatalf("a planted ledger was not refused: %v", err)
	}
	chdirRepoRoot(t)
	if err := refuseRetiredRetainRefusalLedger(defaultManifest); err != nil {
		t.Fatal(err)
	}
}
