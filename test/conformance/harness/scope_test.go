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
// reads, the block or cook a kept forgery is made from (a driver looks the
// subject up and fails the surface without it), and every row that carries no
// case; every other row is dropped. A scope case no row carries is refused by
// name, never sent as nothing.
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
		"block x U x.blk",
		"block y U y.blk",
		"cook ck U Root ck.cook",
		"cook cj U Root cj.cook",
		"forgery forged1 block x f1.blk 64 0",
		"forgery forged2 block y f2.blk 64 0",
		"forgery forged3 cook ck f3.cook 64 null",
		"cook cv U Scene cv.cook",
		"forgery forged4 cook Scene f4.cook 64 null",
		"forgery forged5 block nowhere f5.blk 64 0",
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
	}, "\n")
	if kept != wantKept {
		t.Errorf("kept\n%s\nwant\n%s", kept, wantKept)
	}
	// a forgery goes with the image it forges, and no other forgery or image
	kept, err = scopeLines(text, map[caseKey]bool{{"forgery", "forged1"}: true, {"cook-forgery", "forged3"}: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"forgery forged1 ", "block x U", "forgery forged3 ", "cook ck U"} {
		if !strings.Contains(kept, line) {
			t.Errorf("the scope of forged1 and forged3 dropped %q:\n%s", line, kept)
		}
	}
	for _, line := range []string{"forgery forged2 ", "block y U", "cook cj U", "cook cv U"} {
		if strings.Contains(kept, line) {
			t.Errorf("the scope of forged1 and forged3 kept %q:\n%s", line, kept)
		}
	}
	// a cooked forgery's subject may be a root: the cook line of that root
	// goes with it (squirrel, luau, lobster, moo and elixir look it up so)
	kept, err = scopeLines(text, map[caseKey]bool{{"cook-forgery", "forged4"}: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(kept, "cook cv U Scene cv.cook") {
		t.Errorf("the scope of forged4, made from root Scene, dropped the cook of that root:\n%s", kept)
	}
	// a forgery whose subject no line answers to is refused, by name
	if _, err := scopeLines(text, map[caseKey]bool{{"forgery", "forged5"}: true}); err == nil || !strings.Contains(err.Error(), "block nowhere") || !strings.Contains(err.Error(), "forged5") {
		t.Errorf("a forgery made from no line: err %v, want a refusal naming block nowhere and forged5", err)
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
	all := map[caseKey]bool{}
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
			all[caseKey{s, name}] = true
			if !carried[caseKey{s, name}] {
				t.Errorf("%s/%s: no derived manifest line carries it, so a scoped run would never send it", s, name)
			}
		}
		// the first case asked alone cuts a manifest whose references hold
		if _, err := scopeLines(string(data), map[caseKey]bool{{s, names[0]}: true}); err != nil {
			t.Errorf("%s/%s asked alone: %v", s, names[0], err)
		}
	}
	if cases == 0 {
		t.Fatal("the corpus gave no case to check")
	}
	// every case asked at once: every forgery's subject answers to a line
	if _, err := scopeLines(string(data), all); err != nil {
		t.Errorf("the whole corpus asked at once: %v", err)
	}
}
