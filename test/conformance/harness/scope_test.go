package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestTheScopeClosesOverTwins: a scoped run holds every case an asked case's
// verdict depends on (scope.go). An asked report refusal brings its group's
// lawful twin on the wire surface it falls back to; that twin brings the
// refusal it is paired with on its own surface (the closure's fixed point);
// a case nothing depends on stays out.
func TestTheScopeClosesOverTwins(t *testing.T) {
	refused := []byte("refused\n")
	legWants := map[string][]expectation{
		"report": {{name: "rep_bad", want: refused}},
		"wire":   {{name: "w_ok", want: []byte("x")}, {name: "w_bad", want: refused}, {name: "other", want: []byte("y")}},
	}
	disc := &discrimination{
		refusal:  map[caseRef]string{{"report", "rep_bad"}: "report|U|R", {"wire", "w_bad"}: "wire|U|R"},
		lawful:   map[string][]caseRef{"wire|U|R": {{"wire", "w_ok"}}},
		fallback: map[string][]string{"report|U|R": {"wire|U|R"}},
	}
	got := closeScope(map[caseKey]bool{{"report", "rep_bad"}: true}, legWants, disc, nil)
	want := map[caseKey]bool{{"report", "rep_bad"}: true, {"wire", "w_ok"}: true, {"wire", "w_bad"}: true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scope %v, want %v", got, want)
	}
	// asking the lawful half alone brings the refusal paired with it
	got = closeScope(map[caseKey]bool{{"wire", "w_ok"}: true}, legWants, disc, nil)
	if want := map[caseKey]bool{{"wire", "w_ok"}: true, {"wire", "w_bad"}: true}; !reflect.DeepEqual(got, want) {
		t.Errorf("scope of the lawful half %v, want %v", got, want)
	}
}

// TestScopeLinesKeepTheAskedRowsAndWhatTheyRead: the scoped manifest holds
// the rows that carry the scope's cases, the instance a kept cook-write row
// reads, and every row that carries no case; every other row is dropped. A
// scope case no row carries is refused by name, never sent as nothing.
func TestScopeLinesKeepTheAskedRowsAndWhatTheyRead(t *testing.T) {
	text := strings.Join([]string{
		"# derived",
		"unit U a.schema",
		"connection C U",
		"instance inst1 U Root a.bin",
		"instance inst2 U Root b.bin",
		"cook-write inst1 inst1.cook",
		"write-refusal wr1 U Root",
		"message msg1 C",
		"forgery forged1 block x",
	}, "\n")
	kept, err := scopeLines(text, map[caseKey]bool{{"cook-write", "inst1"}: true})
	if err != nil {
		t.Fatal(err)
	}
	wantKept := strings.Join([]string{
		"# derived",
		"unit U a.schema",
		"connection C U",
		"instance inst1 U Root a.bin",
		"cook-write inst1 inst1.cook",
		"forgery forged1 block x",
	}, "\n")
	if kept != wantKept {
		t.Errorf("kept\n%s\nwant\n%s", kept, wantKept)
	}
	kept, err = scopeLines(text, map[caseKey]bool{{"message", "msg1"}: true, {"json-read", "inst2"}: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"message msg1 C", "instance inst2 U Root b.bin", "connection C U"} {
		if !strings.Contains(kept, line) {
			t.Errorf("the scope of msg1 and inst2's text read dropped %q:\n%s", line, kept)
		}
	}
	for _, line := range []string{"instance inst1", "write-refusal wr1", "cook-write inst1"} {
		if strings.Contains(kept, line) {
			t.Errorf("the scope of msg1 and inst2's text read kept %q:\n%s", line, kept)
		}
	}
	if _, err := scopeLines(text, map[caseKey]bool{{"wire", "gone"}: true}); err == nil || !strings.Contains(err.Error(), "wire/gone") {
		t.Errorf("a scope case no row carries: err %v, want a refusal naming wire/gone", err)
	}
}

// TestEveryCorpusCaseIsCarriedByAManifestRow holds the committed corpus to
// the scoped run: every case of every surface the harness scores is carried
// by a line of the derived manifest (derivedRows says which), so asking it
// alone sends the leg that row, and the row's references survive the cut.
// A surface whose rows no line names is handed whole (unscopedSurfaces).
func TestEveryCorpusCaseIsCarriedByAManifestRow(t *testing.T) {
	chdirRepoRoot(t)
	m, err := ReadManifest(defaultManifest, defaultJSONDir)
	if err != nil {
		t.Fatal(err)
	}
	reports, err := readReports(defaultReports)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "derived.txt")
	if err := deriveManifest(m, path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	carried := map[caseKey]bool{}
	for line := range strings.SplitSeq(string(data), "\n") {
		f, row, err := derivedLine(line)
		if err != nil {
			t.Fatal(err)
		}
		if f != nil && row.cases != nil {
			for _, key := range row.cases(f) {
				carried[key] = true
			}
		}
	}
	cases := 0
	for _, s := range surfaces {
		var names []string
		if s == "json-hostile" {
			for _, h := range m.Hostiles {
				names = append(names, h.Name)
			}
		} else {
			want, err := expectations(m, s, reports, defaultJSONDir)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range want {
				names = append(names, e.name)
			}
		}
		if len(names) == 0 {
			t.Errorf("the %s surface has no case: the check below read none of it", s)
			continue
		}
		for _, name := range names {
			cases++
			if !unscopedSurfaces[s] && !carried[caseKey{s, name}] {
				t.Errorf("%s/%s: no derived manifest line carries it, so a scoped run would never send it", s, name)
			}
		}
		// the first case asked alone cuts a manifest whose references hold
		if _, err := scopeLines(string(data), map[caseKey]bool{{s, names[0]}: true}); err != nil && !unscopedSurfaces[s] {
			t.Errorf("%s/%s asked alone: %v", s, names[0], err)
		}
	}
	if cases == 0 {
		t.Fatal("the corpus gave no case to check")
	}
}
