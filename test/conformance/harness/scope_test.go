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

// TestScopeLinesKeepTheAskedRowsAndWhatTheyRead: the scoped manifest cuts a
// line only when a surface it is scored on runs and none of its cases is
// asked; it keeps what a kept line reads (the instance of a cook-write row,
// the message batch of a write-refusal-message row, the block or cook a
// forgery is made from, by case or by root), and every line of a surface that
// does not run. A scope case no row carries, and a forgery whose subject no
// line answers to, are refused by name, never sent as nothing.
func TestScopeLinesKeepTheAskedRowsAndWhatTheyRead(t *testing.T) {
	text := strings.Join([]string{
		"# derived",
		"unit U a.schema",
		"connection C U h ann.bin",
		"instance inst1 U Root a.bin",
		"instance inst2 U Root b.bin",
		"cook-write inst1 in1 - -",
		"cook-write inst2 in2 - -",
		"write-refusal wr1 U Root",
		"message msg1 C Root m1.bin m1.batch",
		"message msg2 C Root m2.bin m2.batch",
		"write-refusal-message wrm1 C Root m1.bin field kind text",
		"block x U x.blk",
		"block y U y.blk",
		"cook ck U Root ck.cook",
		"cook cj U Root cj.cook",
		"cook cv U Scene cv.cook",
		"forgery forged1 block x f1.blk 64 0",
		"forgery forged2 block y f2.blk 64 0",
		"forgery forged3 cook ck f3.cook 64 null",
		"forgery forged4 cook Scene f4.cook 64 null",
		"forgery forged5 block nowhere f5.blk 64 0",
	}, "\n")
	cut := func(scope map[caseKey]bool) string {
		t.Helper()
		kept, err := scopeLines(text, scope)
		if err != nil {
			t.Fatal(err)
		}
		return kept
	}
	has := func(what, kept string, want, wantNot []string) {
		t.Helper()
		for _, line := range want {
			if !strings.Contains(kept, line) {
				t.Errorf("%s dropped %q:\n%s", what, line, kept)
			}
		}
		for _, line := range wantNot {
			if strings.Contains(kept, line) {
				t.Errorf("%s kept %q:\n%s", what, line, kept)
			}
		}
	}
	// the asked cook-write row, the instance it reads, every line of a
	// surface that does not run; the other cook-write row is cut
	has("the scope of cook-write inst1", cut(map[caseKey]bool{{"cook-write", "inst1"}: true}),
		[]string{"cook-write inst1 ", "instance inst1 ", "instance inst2 ", "write-refusal wr1 ", "message msg1 ", "block y ", "forgery forged2 "},
		[]string{"cook-write inst2 "})
	// the asked rows on json-read, cook-write and message, and the cut of
	// each surface's unasked rows (inst1 is read by no kept row); a
	// write-refusal-message row on a surface that does not run keeps its
	// batch, msg1, there too
	has("the scope of msg2 and inst2", cut(map[caseKey]bool{{"message", "msg2"}: true, {"json-read", "inst2"}: true, {"cook-write", "inst2"}: true}),
		[]string{"message msg2 ", "instance inst2 ", "cook-write inst2 ", "connection C ", "message msg1 ", "write-refusal-message wrm1 "},
		[]string{"instance inst1 ", "cook-write inst1 "})
	// a write-refusal-message row asked keeps the message row whose batch it
	// writes against, even when the message surface runs without it
	has("the scope of wrm1 and msg2", cut(map[caseKey]bool{{"write-refusal-message", "wrm1"}: true, {"message", "msg2"}: true}),
		[]string{"write-refusal-message wrm1 ", "message msg1 ", "message msg2 "}, nil)
	// a forgery goes with the image it forges, by case or by cooked root,
	// and the forgery surface's other rows are cut
	has("the scope of forged1, forged3 and forged4", cut(map[caseKey]bool{{"forgery", "forged1"}: true, {"cook-forgery", "forged3"}: true, {"cook-forgery", "forged4"}: true}),
		[]string{"forgery forged1 ", "block x U", "forgery forged3 ", "cook ck U", "forgery forged4 ", "cook cv U Scene"},
		[]string{"forgery forged2 ", "forgery forged5 "})
	if _, err := scopeLines(text, map[caseKey]bool{{"wire", "gone"}: true}); err == nil || !strings.Contains(err.Error(), "wire/gone") {
		t.Errorf("a scope case no row carries: err %v, want a refusal naming wire/gone", err)
	}
	if _, err := scopeLines(text, map[caseKey]bool{{"forgery", "forged5"}: true}); err == nil || !strings.Contains(err.Error(), "block nowhere") || !strings.Contains(err.Error(), "forged5") {
		t.Errorf("a forgery made from no line: err %v, want a refusal naming block nowhere and forged5", err)
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
