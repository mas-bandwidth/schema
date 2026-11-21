package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateOptInCorpus = flag.Bool("update-optin-corpus", false, "rewrite testdata/conformance/optin/leg-*.txt from the harness's own filter")

// optInCorpusLeg derives the committed opt-in corpus
// (testdata/conformance/optin/MANIFEST.txt) and filters it for a leg whose
// `list` is listed, as `harness run` does: the manifest the leg is handed, as
// its rows alone, and the cases it is not asked.
func optInCorpusLeg(t *testing.T, listed map[string]bool) (string, notAsked) {
	t.Helper()
	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, "testdata", "conformance", "optin", "MANIFEST.txt"), defaultJSONDir)
	if err != nil {
		t.Fatal(err)
	}
	derived := filepath.Join(t.TempDir(), "manifest.txt")
	if err := deriveManifest(m, derived); err != nil {
		t.Fatal(err)
	}
	path, cases, _, err := legManifest(m, derived, "leg", listed)
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows strings.Builder
	for line := range strings.SplitSeq(string(text), "\n") {
		if s := strings.TrimSpace(line); s != "" && !strings.HasPrefix(s, "#") {
			rows.WriteString(line + "\n")
		}
	}
	return rows.String(), cases
}

// pinOptInCorpus holds the filtered manifest to the committed one the Java
// driver answers (internal/codegen/javatable/conformance_optin_test.go).
func pinOptInCorpus(t *testing.T, name, got string) {
	t.Helper()
	pin := filepath.Join(conformanceRoot(t), "testdata", "conformance", "optin", name)
	if *updateOptInCorpus {
		if err := os.WriteFile(pin, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(pin)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("%s differs from the harness's filter; the leg is handed:\n%s", name, got)
	}
}

// handed says whether the leg's manifest carries a row of this kind and name.
func handed(rows, kind, name string) bool {
	for line := range strings.SplitSeq(rows, "\n") {
		if f := strings.Fields(line); len(f) > 1 && f[0] == kind && f[1] == name {
			return true
		}
	}
	return false
}

// TestOptInCorpusOmittingLegIsHandedNoRowOfTheUnit (F-186, F-096, F-097): a
// leg whose `list` does not name `unit backenddemo` is handed none of that
// unit's rows: not its connection, and not the write-refusal-message row that
// reads it through that connection, which a driver could answer only by
// failing the whole surface. Each such case is not asked. The control rows on
// the required units are handed and asked.
func TestOptInCorpusOmittingLegIsHandedNoRowOfTheUnit(t *testing.T) {
	t.Parallel()
	rows, cases := optInCorpusLeg(t, map[string]bool{})
	for _, c := range []struct {
		kind, name string
		want       bool
	}{
		{"connection", "backend_conn", false},
		{"write-refusal-message", "mref_range_high_ok", false},
		{"instance", "score.json", false},
		{"write-refusal", "shared", false},
		{"unit", "backenddemo", false},
		// the controls
		{"connection", "varevo3_conn", true},
		{"write-refusal-message", "mref_unionstr_ok", true},
		{"instance", "score", true},
		{"instance", "shared", true},
	} {
		if got := handed(rows, c.kind, c.name); got != c.want {
			t.Errorf("leg handed %s %s = %v, want %v", c.kind, c.name, got, c.want)
		}
	}
	checkAsked(t, cases, []askedCase{
		{"write-refusal-message", "mref_range_high_ok", true},
		{"write-refusal-message", "mref_unionstr_ok", false},
		// F-096: one suffix, the row's own
		{"json-write", "score.json", false},
		{"json-write", "score.json.json", true},
		{"wire", "score.json", true},
		// F-097: keyed by surface and name
		{"wire", "shared", false},
		{"json-write", "shared.json", false},
		{"write-refusal", "shared", true},
	})
	pinOptInCorpus(t, "leg-omits.txt", rows)
}

// TestOptInCorpusOptedInLegIsHandedEveryRow: a leg that names the unit is
// handed the derived manifest whole and is asked every case.
func TestOptInCorpusOptedInLegIsHandedEveryRow(t *testing.T) {
	t.Parallel()
	rows, cases := optInCorpusLeg(t, map[string]bool{"unit backenddemo": true})
	if len(cases) != 0 {
		t.Errorf("an opted-in leg is not asked %v", cases)
	}
	for _, name := range []string{"mref_range_high_ok", "mref_unionstr_ok"} {
		if !handed(rows, "write-refusal-message", name) {
			t.Errorf("an opted-in leg is not handed %s", name)
		}
	}
	pinOptInCorpus(t, "leg-opts-in.txt", rows)
}

// TestLegIsNeverHandedARowItIsNotAsked: whatever row kind reads an omitted
// unit, directly or through a connection, the filter either drops it or
// refuses to hand the leg a manifest at all. A connection row whose unit is
// omitted takes every row on that connection with it.
func TestLegIsNeverHandedARowItIsNotAsked(t *testing.T) {
	t.Parallel()
	m := &Manifest{
		Units: []Unit{{Key: "demo", Paths: []string{"a"}}, {Key: "extra", Paths: []string{"b"}}},
		OptIn: map[string]bool{"extra": true},
		// the connection keyed `extra` is on the required unit: its key spells
		// the omitted unit's name and says nothing about it
		Connections: []Connection{{Key: "c_extra", Unit: "extra", Wire: "c.bin"}, {Key: "c_demo", Unit: "demo", Wire: "d.bin"}, {Key: "extra", Unit: "demo", Wire: "e.bin"}},
		Messages:    []Message{{Name: "msg", Connection: "c_extra", Unit: "extra", Root: "R", FileWire: "f.bin", MessageWire: "m.bin"}},
		WriteRefusals: []WriteRefusal{
			{Form: "message", Name: "wm", Unit: "c_extra", Root: "R", Wire: "f.bin", Path: "n", Kind: "int", Value: "1", Verdict: "wrote"},
			{Form: "message", Name: "wm_ok", Unit: "c_demo", Root: "R", Wire: "f.bin", Path: "n", Kind: "int", Value: "1", Verdict: "wrote"},
			{Form: "message", Name: "wm_named", Unit: "extra", Root: "R", Wire: "f.bin", Path: "n", Kind: "int", Value: "1", Verdict: "wrote"},
		},
	}
	derived := filepath.Join(t.TempDir(), "manifest.txt")
	if err := deriveManifest(m, derived); err != nil {
		t.Fatal(err)
	}
	path, cases, _, err := legManifest(m, derived, "leg", map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rows := string(text)
	for _, c := range []struct{ kind, name string }{{"message", "msg"}, {"write-refusal-message", "wm"}, {"connection", "c_extra"}} {
		if handed(rows, c.kind, c.name) {
			t.Errorf("the leg is handed %s %s, which it is not asked", c.kind, c.name)
		}
	}
	for _, c := range []struct{ kind, name string }{
		{"write-refusal-message", "wm_ok"}, {"connection", "c_demo"},
		{"write-refusal-message", "wm_named"}, {"connection", "extra"},
	} {
		if !handed(rows, c.kind, c.name) {
			t.Errorf("the leg is not handed %s %s, which it is asked:\n%s", c.kind, c.name, rows)
		}
	}
	checkAsked(t, cases, []askedCase{
		{"message", "msg", true},
		{"write-refusal-message", "wm", true},
		{"write-refusal-message", "wm_ok", false},
		{"write-refusal-message", "wm_named", false},
	})

	// a row the filter keeps for a case the leg is not asked is refused, never
	// handed to a driver: here a line the filter did not drop reads the
	// omitted unit's connection
	if err := checkLegRows(rows+"\nwrite-refusal-message wm c_extra R f.bin n int 1\n", cases); err == nil {
		t.Error("checkLegRows accepted a row of a case the leg is not asked")
	} else if !strings.Contains(err.Error(), "wm") || !strings.Contains(err.Error(), "c_extra") {
		t.Errorf("the refusal %q does not name the row and its connection", err)
	}
}
