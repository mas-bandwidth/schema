package main

import (
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/inputfamilies"
)

// The retain-refusal ledger is an EXACT SET (retainrefusal.go): a failing
// ledgered case is owed and uncredited, a passing ledgered case is red, and a
// line the manifest cannot place is refused when the ledger is read.

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

func TestRetainRefusalLedgerIsExact(t *testing.T) {
	ledger := retainRefusalLedger{"lobster row": "a cause"}

	answer, e := retainRefusalCase(t, retainRefusalSaved, "retained", "retained")
	r := &result{}
	var out strings.Builder
	scoreOwedRetainRefusal(&out, "lobster", answer, e, ledger["lobster row"], r)
	if len(r.failures) != 0 || r.missing != 1 || r.outcome["row"] != outcomeOwed {
		t.Fatalf("a failing ledgered case: failures %q, missing %d, outcome %q", r.failures, r.missing, r.outcome["row"])
	}
	if !strings.Contains(out.String(), "conformance OWED lobster / retain-refusal: row owed: a cause") {
		t.Fatalf("the owed case was not said: %q", out.String())
	}

	answer, e = retainRefusalCase(t, retainRefusalSaved, "retained", "plain")
	r = &result{}
	scoreOwedRetainRefusal(io.Discard, "lobster", answer, e, ledger["lobster row"], r)
	if len(r.failures) != 1 || !strings.Contains(r.failures[0], "answers it correctly: remove the line") || r.pass != 0 {
		t.Fatalf("a passing ledgered case: failures %q, pass %d", r.failures, r.pass)
	}

	if _, owed := ledger.owes("lobster", "retain", "row"); owed {
		t.Fatal("the ledger owes a case on another surface")
	}
	if _, owed := ledger.owes("goose", "retain-refusal", "row"); owed {
		t.Fatal("the ledger owes another leg's case")
	}
}

func TestRetainRefusalLedgerRefusesWhatItCannotPlace(t *testing.T) {
	m := &Manifest{RetainRefusals: []RetainRefusal{{Name: "row"}}}
	for _, c := range []struct{ name, text, want string }{
		{"no cause", "lobster row owed:\n", "want `<leg> <case> owed: <cause>`"},
		{"no owed word", "lobster row because\n", "want `<leg> <case> owed: <cause>`"},
		{"an unknown case", "lobster other owed: a cause\n", "names no retain-refusal row"},
		{"an unknown leg", "cobol row owed: planted, no such leg\n", "cobol names no leg"},
		{"a repeated line", "lobster row owed: a\nlobster row owed: b\n", "ledgered twice"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, RetainRefusalOwed), []byte(c.text), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := loadRetainRefusalLedger(filepath.Join(dir, "MANIFEST.txt"), m)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err %v, want %q", err, c.want)
			}
		})
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, RetainRefusalOwed), []byte("# a comment\n\nlobster row owed: a cause\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ledger, err := loadRetainRefusalLedger(filepath.Join(dir, "MANIFEST.txt"), m)
	if err != nil || ledger["lobster row"] != "a cause" || len(ledger) != 1 {
		t.Fatalf("ledger %v, err %v", ledger, err)
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
