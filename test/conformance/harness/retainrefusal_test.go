package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
			scoreRetainRefusal(answer, e, r)
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
