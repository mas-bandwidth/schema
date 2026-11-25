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
	got := closeScope(map[caseKey]bool{{"report", "rep_bad"}: true}, legWants, disc, nil, nil)
	want := map[caseKey]bool{{"report", "rep_bad"}: true, {"wire", "w_ok"}: true, {"wire", "w_bad"}: true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scope %v, want %v", got, want)
	}
	// asking the lawful half alone brings the refusal paired with it
	got = closeScope(map[caseKey]bool{{"wire", "w_ok"}: true}, legWants, disc, nil, nil)
	if want := map[caseKey]bool{{"wire", "w_ok"}: true, {"wire", "w_bad"}: true}; !reflect.DeepEqual(got, want) {
		t.Errorf("scope of the lawful half %v, want %v", got, want)
	}
	// a framed rewrite row brings the framing row of its name, whose
	// write-back is its answer (rewrite.go); an unframed one, and a framing
	// row asked alone, bring nothing
	framed := map[string]FramingCase{"rw_f": {Name: "rw_f"}}
	got = closeScope(map[caseKey]bool{{"rewrite", "rw_f"}: true, {"rewrite", "rw_plain"}: true, {"framing", "fr_alone"}: true}, legWants, disc, nil, framed)
	if want := map[caseKey]bool{{"rewrite", "rw_f"}: true, {"framing", "rw_f"}: true, {"rewrite", "rw_plain"}: true, {"framing", "fr_alone"}: true}; !reflect.DeepEqual(got, want) {
		t.Errorf("scope of the rewrite rows %v, want %v", got, want)
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
		"message msg3 C Root m3.bin m3.batch",
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
		kept, err := scopeLines(text, scope, nil)
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
	// with the wire surface running too, inst1 is a running surface's
	// unasked row, and stays only because the asked cook-write row reads it
	has("the scope of cook-write inst1 and wire inst2", cut(map[caseKey]bool{{"cook-write", "inst1"}: true, {"wire", "inst2"}: true}),
		[]string{"cook-write inst1 ", "instance inst1 ", "instance inst2 "},
		[]string{"cook-write inst2 "})
	// the asked rows on json-read, cook-write and message, and the cut of
	// each surface's unasked rows (inst1 is read by no kept row); the
	// write-refusal-message surface does not run, so its row reads no batch
	has("the scope of msg2 and inst2", cut(map[caseKey]bool{{"message", "msg2"}: true, {"json-read", "inst2"}: true, {"cook-write", "inst2"}: true}),
		[]string{"message msg2 ", "instance inst2 ", "cook-write inst2 ", "connection C ", "write-refusal-message wrm1 "},
		[]string{"instance inst1 ", "cook-write inst1 ", "message msg1 "})
	// a write-refusal-message row asked keeps the message row whose batch it
	// writes against, even when the message surface runs without it
	has("the scope of wrm1 and msg2", cut(map[caseKey]bool{{"write-refusal-message", "wrm1"}: true, {"message", "msg2"}: true}),
		[]string{"write-refusal-message wrm1 ", "message msg1 ", "message msg2 "},
		[]string{"message msg3 "})
	// with no write-refusal-message row asked, no batch is read: the message
	// surface's unasked rows are cut, msg1 with them
	has("the scope of msg2", cut(map[caseKey]bool{{"message", "msg2"}: true}),
		[]string{"message msg2 ", "write-refusal-message wrm1 "},
		[]string{"message msg1 ", "message msg3 "})
	// a forgery goes with the image it forges, by case or by cooked root,
	// and the forgery surface's other rows are cut
	// with the block and cook surfaces running on other rows, the subjects
	// stay only because the asked forgeries are made from them
	has("the scope of forged1, forged3, forged4, block y and cook cj", cut(map[caseKey]bool{
		{"forgery", "forged1"}: true, {"cook-forgery", "forged3"}: true, {"cook-forgery", "forged4"}: true,
		{"block", "y"}: true, {"cook", "cj"}: true,
	}),
		[]string{"forgery forged1 ", "block x U", "forgery forged3 ", "cook ck U", "forgery forged4 ", "cook cv U Scene", "block y U", "cook cj U"},
		[]string{"forgery forged2 ", "forgery forged5 "})
	if _, err := scopeLines(text, map[caseKey]bool{{"wire", "gone"}: true}, nil); err == nil || !strings.Contains(err.Error(), "wire/gone") {
		t.Errorf("a scope case no row carries: err %v, want a refusal naming wire/gone", err)
	}
	if _, err := scopeLines(text, map[caseKey]bool{{"forgery", "forged5"}: true}, nil); err == nil || !strings.Contains(err.Error(), "block nowhere") || !strings.Contains(err.Error(), "forged5") {
		t.Errorf("a forgery made from no line: err %v, want a refusal naming block nowhere and forged5", err)
	}
}

// TestEveryCorpusCaseIsCarriedByAManifestRow holds the committed corpus to
// the scoped run: every case of every surface the harness scores is carried
// by a line of the derived manifest (derivedRows says which), so asking it
// alone sends the leg that row, and the row's references survive the cut. A
// case sent on a manifest of its own (ownManifestCases) is held to that
// manifest instead: a rewrite row to the rewrite manifest's instance line,
// or, framed, to the framing row of its name the scope brings with it; an
// authored text to the json-read manifest's instance line.
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
	framed, err := framedRewrites(newUnits(m), m)
	if err != nil {
		t.Fatal(err)
	}
	rewritePath := filepath.Join(t.TempDir(), "rewrite.txt")
	if err := deriveRewriteManifest(m, rewritePath, nil, framed); err != nil {
		t.Fatal(err)
	}
	rewriteText, err := os.ReadFile(rewritePath)
	if err != nil {
		t.Fatal(err)
	}
	ownLines := map[caseKey]bool{}
	for surface, text := range map[string]string{"rewrite": string(rewriteText), "json-read": jsonReadManifest(string(data), m, nil)} {
		for line := range strings.SplitSeq(text, "\n") {
			if f := strings.Fields(line); len(f) > 1 && f[0] == "instance" {
				ownLines[caseKey{surface, f[1]}] = true
			}
		}
	}
	own := ownManifestCases(m)
	if len(own) == 0 {
		t.Error("the corpus has no case on a manifest of its own: the check of them below read none")
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
		first := ""
		for _, name := range names {
			cases++
			key := caseKey{s, name}
			switch _, isFramed := framed[name]; {
			case own[key] && s == "rewrite" && isFramed:
				if !carried[caseKey{"framing", name}] {
					t.Errorf("%s/%s: framed, but no derived manifest line carries the framing row whose write-back answers it", s, name)
				}
			case own[key]:
				if !ownLines[key] {
					t.Errorf("%s/%s: its own manifest has no line for it, so a scoped run would never send it", s, name)
				}
			default:
				all[key] = true
				if first == "" {
					first = name
				}
				if !carried[key] {
					t.Errorf("%s/%s: no derived manifest line carries it, so a scoped run would never send it", s, name)
				}
			}
		}
		// the first case the derived manifest carries, asked alone, cuts a
		// manifest whose references hold
		if first == "" {
			continue
		}
		if _, err := scopeLines(string(data), map[caseKey]bool{{s, first}: true}, nil); err != nil {
			t.Errorf("%s/%s asked alone: %v", s, first, err)
		}
	}
	if cases == 0 {
		t.Fatal("the corpus gave no case to check")
	}
	// every case asked at once: every forgery's subject answers to a line
	if _, err := scopeLines(string(data), all, nil); err != nil {
		t.Errorf("the whole corpus asked at once: %v", err)
	}
}

// TestTheCutLeavesCasesOnTheirOwnManifestAlone: a rewrite row and an authored
// json-read text are sent on manifests of their own (ownManifestCases), so a
// scope that asks them cuts the derived manifest without asking it for their
// rows; without that, the cut refuses them as cases no line carries.
func TestTheCutLeavesCasesOnTheirOwnManifestAlone(t *testing.T) {
	dir := t.TempDir()
	derived := filepath.Join(dir, "manifest-go.txt")
	text := "unit U a.schema\ninstance inst1 U Root a.bin\n"
	if err := os.WriteFile(derived, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	legWants := map[string][]expectation{
		"wire":      {{name: "inst1"}},
		"rewrite":   {{name: "rw1"}},
		"json-read": {{name: "inst1"}, {name: "authored1"}},
	}
	scope := map[caseKey]bool{{"wire", "inst1"}: true, {"rewrite", "rw1"}: true, {"json-read", "authored1"}: true}
	own := map[caseKey]bool{{"rewrite", "rw1"}: true, {"json-read", "authored1"}: true}
	path, err := scopeManifest(derived, "go", scope, nil, legWants, nil, own)
	if err != nil {
		t.Fatalf("a scope asking cases on their own manifests: %v", err)
	}
	kept, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(kept), "instance inst1 U Root a.bin") {
		t.Errorf("the asked wire row was cut:\n%s", kept)
	}
	if _, err := scopeManifest(derived, "go", scope, nil, legWants, nil, nil); err == nil || !strings.Contains(err.Error(), "rewrite/rw1") || !strings.Contains(err.Error(), "json-read/authored1") {
		t.Errorf("with no own-manifest cases named, the cut must refuse rewrite/rw1 and json-read/authored1 by name, got %v", err)
	}
}

// TestTheFailFastProbeLeavesFramedRewritesToTheirWriteBack: with
// CONFORMANCE_FAIL_FAST=1 the probe scores a surface as soon as it returns,
// before the framing write-backs are copied in as the framed rewrite rows'
// answers (answerFramedRewrites). It scores the asked rows the leg was
// handed, and never a framed rewrite row; a framing row of the same name, on
// its own surface, it still scores.
func TestTheFailFastProbeLeavesFramedRewritesToTheirWriteBack(t *testing.T) {
	framed := map[string]FramingCase{"rw_f": {Name: "rw_f"}}
	rewrite := []expectation{{name: "rw_f"}, {name: "rw_plain"}, {name: "rw_unasked"}, {name: "rw_unsent"}}
	scope := map[caseKey]bool{{"rewrite", "rw_f"}: true, {"rewrite", "rw_plain"}: true, {"rewrite", "rw_unsent"}: true, {"framing", "rw_f"}: true}
	unsent := notAsked{{"rewrite", "rw_unsent"}: "opt-in"}
	names := func(es []expectation) []string {
		var out []string
		for _, e := range es {
			out = append(out, e.name)
		}
		return out
	}
	if got := names(probeCases("rewrite", rewrite, unsent, scope, framed)); !reflect.DeepEqual(got, []string{"rw_plain"}) {
		t.Errorf("the probe scores rewrite rows %v, want [rw_plain]: a framed row has no answer yet, and the rest were not asked or not sent", got)
	}
	if got := names(probeCases("rewrite", rewrite, nil, nil, framed)); !reflect.DeepEqual(got, []string{"rw_plain", "rw_unasked", "rw_unsent"}) {
		t.Errorf("unscoped, the probe scores rewrite rows %v, want every row but the framed one", got)
	}
	if got := names(probeCases("framing", []expectation{{name: "rw_f"}}, nil, scope, framed)); !reflect.DeepEqual(got, []string{"rw_f"}) {
		t.Errorf("the probe scores framing rows %v, want [rw_f]: a framing row is answered on its own surface", got)
	}
}
