package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture is a two-leg run over two surfaces: every case passes, both legs
// have a conformance negative control, and nothing is absent.
func fixture() Inputs {
	ok := func(names ...string) SurfaceResult {
		c := map[string]string{}
		for _, n := range names {
			c[n] = "pass"
		}
		return SurfaceResult{Cases: c}
	}
	leg := func() map[string]SurfaceResult {
		return map[string]SurfaceResult{
			"wire":       ok("alpha", "beta"),
			"json-write": ok("alpha.json", "beta.json"),
		}
	}
	return Inputs{
		Results:   Results{Langs: []string{"aa", "bb"}, Results: map[string]map[string]SurfaceResult{"aa": leg(), "bb": leg()}},
		CaseUnits: map[string]string{"alpha": "u1", "beta": "u2"},
		Units:     []string{"u1", "u2", "writer-only"},
		Porting:   map[string]map[string]string{},
		Controls: Controls{
			"conformance-negative-control-aa": "CI full / negative controls (g)",
			"conformance-negative-control-bb": "CI full / negative controls (g)",
		},
	}
}

func mustCell(t *testing.T, p Proof, row, lang string) Cell {
	t.Helper()
	c, ok := p.Cell(row, lang)
	if !ok {
		t.Fatalf("no cell %s / %s", row, lang)
	}
	return c
}

// TestProofProvesAPassingLegWithItsGateJobAndControl: a proven cell names the
// passing gate, the CI job that runs it and the leg's negative control.
func TestProofProvesAPassingLegWithItsGateJobAndControl(t *testing.T) {
	p := Compute(fixture())
	for _, row := range []string{"surface wire", "surface json-write", "cases u1", "cases u2"} {
		for _, lang := range []string{"aa", "bb"} {
			c := mustCell(t, p, row, lang)
			if c.Status != StatusProven || c.Gate == "" || c.Job == "" || c.Control == "" {
				t.Errorf("%s / %s = %+v, want proven with gate, job and control", row, lang, c)
			}
		}
	}
	if _, ok := p.Cell("cases writer-only", "aa"); ok {
		t.Error("a unit no case names became a row")
	}
}

// TestProofNegativeControlDeletedAnswer is the gate's own negative control:
// delete one language's answer for one case, and the cells holding that case
// go red NAMING it, while the other leg and the other case stay proven.
func TestProofNegativeControlDeletedAnswer(t *testing.T) {
	in := fixture()
	delete(in.Results.Results["bb"]["wire"].Cases, "beta")
	p := Compute(in)
	for _, row := range []string{"surface wire", "cases u2"} {
		c := mustCell(t, p, row, "bb")
		if c.Status != StatusRed || !strings.Contains(c.Why, "wire/beta") {
			t.Errorf("%s / bb = %+v, want red naming wire/beta", row, c)
		}
	}
	for _, rc := range [][2]string{{"surface wire", "aa"}, {"cases u1", "bb"}, {"surface json-write", "bb"}} {
		if c := mustCell(t, p, rc[0], rc[1]); c.Status != StatusProven {
			t.Errorf("%s / %s went %s with only bb's wire/beta deleted", rc[0], rc[1], c.Status)
		}
	}
	if !strings.Contains(RedList(p), "surface wire / bb") {
		t.Errorf("the red list does not name the cell:\n%s", RedList(p))
	}
}

// TestProofAbsentIsRedUnlessRuled: an absent case is red with no ruling, and
// not applicable under one; a ruling never turns a FAILING answer green.
func TestProofAbsentIsRedUnlessRuled(t *testing.T) {
	in := fixture()
	in.Results.Results["bb"]["wire"].Cases["beta"] = "absent"
	if c := mustCell(t, Compute(in), "surface wire", "bb"); c.Status != StatusRed || !strings.Contains(c.Why, "absent") {
		t.Fatalf("an unruled absence reads %+v", c)
	}
	in.Rulings = Rulings{Rulings: []Ruling{{ID: "r1", Date: "2026-10-31", Quotes: []string{"q"}, Source: "s",
		Applies: []Selector{{Surface: "wire", Case: "beta"}}}}}
	c := mustCell(t, Compute(in), "surface wire", "bb")
	if c.Status != StatusProven || c.Ruling != "r1" {
		t.Fatalf("a ruled absence beside a passing case reads %+v, want proven citing r1", c)
	}
	if c := mustCell(t, Compute(in), "cases u2", "bb"); c.Status == StatusRed {
		t.Fatalf("u2's ruled absence is red: %+v", c)
	}
	in.Results.Results["bb"]["wire"].Cases["beta"] = "fail"
	if c := mustCell(t, Compute(in), "surface wire", "bb"); c.Status != StatusRed {
		t.Fatalf("a ruling covered a failing answer: %+v", c)
	}
}

// TestProofWholeSurfaceAbsentAndRuledIsNotApplicable: every case absent under
// a ruling is n/a, citing it.
func TestProofWholeSurfaceAbsentAndRuledIsNotApplicable(t *testing.T) {
	in := fixture()
	for n := range in.Results.Results["bb"]["wire"].Cases {
		in.Results.Results["bb"]["wire"].Cases[n] = "absent"
	}
	in.Rulings = Rulings{Rulings: []Ruling{{ID: "r1", Date: "2026-10-31", Quotes: []string{"q"}, Source: "s",
		Applies: []Selector{{Surface: "wire", Case: "*", Langs: []string{"bb"}}}}}}
	if c := mustCell(t, Compute(in), "surface wire", "bb"); c.Status != StatusNA || c.Ruling != "r1" {
		t.Fatalf("got %+v, want n/a under r1", c)
	}
}

// TestProofGateWithNoNegativeControlIsRed: a leg whose conformance gate has no
// control is red on every passing cell, by name.
func TestProofGateWithNoNegativeControlIsRed(t *testing.T) {
	in := fixture()
	delete(in.Controls, "conformance-negative-control-bb")
	c := mustCell(t, Compute(in), "surface wire", "bb")
	if c.Status != StatusRed || !strings.Contains(c.Why, "no negative control") {
		t.Fatalf("got %+v", c)
	}
}

// TestProofPortingCells: ✅ needs a named gate and a negative control; ❌ is
// red with its issue; — is red unless a ruling names the row.
func TestProofPortingCells(t *testing.T) {
	in := fixture()
	in.Controls["tables-aa-thing-negative-control"] = "CI full / negative controls (g)"
	in.Porting = map[string]map[string]string{"M5": {
		"aa": "✅ `tables-aa-thing` `tables-aa-thing-negative-control`",
		"bb": "✅ `TestBbThing` (`internal/x.go:1`)",
	}, "M6": {
		"aa": "❌ #123 not yet",
		"bb": "— the platform cannot",
	}, "M10": {
		"aa": "✅ `internal/codegen/x.go:12`",
		"bb": "✅ `TestBbThing` `TestBbThingNegativeControl`",
	}}
	in.Rulings = Rulings{Rulings: []Ruling{{ID: "r2", Date: "2026-10-31", Quotes: []string{"q"}, Source: "s",
		Applies: []Selector{{Row: "M6", Langs: []string{"bb"}}}}}}
	p := Compute(in)
	want := map[[2]string]string{
		{"porting M5", "aa"}:  StatusProven,
		{"porting M5", "bb"}:  StatusRed, // a gate with no control
		{"porting M6", "aa"}:  StatusRed,
		{"porting M6", "bb"}:  StatusNA,
		{"porting M10", "aa"}: StatusRed, // paths only
		{"porting M10", "bb"}: StatusProven,
		{"porting M11", "aa"}: StatusRed, // no section data
	}
	for k, w := range want {
		if c := mustCell(t, p, k[0], k[1]); c.Status != w {
			t.Errorf("%s / %s = %+v, want %s", k[0], k[1], c, w)
		}
	}
	if c := mustCell(t, p, "porting M5", "aa"); !strings.Contains(c.Control, "tables-aa-thing-negative-control") || !strings.Contains(c.Control, "negative controls (g)") {
		t.Errorf("M5 / aa control = %q", c.Control)
	}
}

// TestProofDashCellCitesItsExemption: a `—` cell is n/a when the clause it
// quotes is in the file it names, and red when the file does not carry it (the
// negative control: an invented exemption), when the quote is a word rather
// than a clause, or when the quote is only in the register's own table.
func TestProofDashCellCitesItsExemption(t *testing.T) {
	in := fixture()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	porting := "### M10 — Hooks\n\n**Method.** Who calls the allocator is per form:\na managed backend allocates inside its runtime and says so.\n\n" +
		"| aa | bb |\n|---|---|\n| — \"a cell that only quotes its own row\" (docs/PORTING.md M10) | — |\n"
	spec := "§3.3 Every read refuses a NaN on its raw bits as non_finite_float.\n"
	for p, text := range map[string]string{portingPath: porting, "docs/SPEC-TABLES.md": spec} {
		if err := os.WriteFile(filepath.Join(dir, p), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	docs, err := LoadCitable(dir)
	if err != nil {
		t.Fatal(err)
	}
	in.Docs = docs
	in.Porting = map[string]map[string]string{"M10": {
		"aa": "— the runtime allocates: \"a managed backend allocates\ninside its runtime and says so\" (docs/PORTING.md M10, Method)",
		"bb": "— \"a managed backend never allocates at all, by design\" (docs/PORTING.md M10)",
	}, "M21": {
		"aa": "— retired: \"Every read refuses a NaN on its raw bits\" (docs/SPEC-TABLES.md §3.3)",
		"bb": "— \"refuses\" (docs/SPEC-TABLES.md §3.3)",
	}, "M17": {
		"aa": "— \"a cell that only quotes its own row\" (docs/PORTING.md M10)",
		"bb": "— \"Every read refuses a NaN on its raw bits\" (docs/PORTING.md M10)",
	}}
	p := Compute(in)
	want := map[[2]string]string{
		{"porting M10", "aa"}: StatusNA,
		{"porting M10", "bb"}: StatusRed, // the text is invented
		{"porting M21", "aa"}: StatusNA,
		{"porting M21", "bb"}: StatusRed, // a word, not a clause
		{"porting M17", "aa"}: StatusRed, // only the register's table carries it
		{"porting M17", "bb"}: StatusRed, // the right words, the wrong file
	}
	for k, w := range want {
		if c := mustCell(t, p, k[0], k[1]); c.Status != w {
			t.Errorf("%s / %s = %+v, want %s", k[0], k[1], c, w)
		}
	}
	if c := mustCell(t, p, "porting M10", "aa"); !strings.Contains(c.Cites, "docs/PORTING.md") {
		t.Errorf("M10 / aa cites = %q", c.Cites)
	}
	if c := mustCell(t, p, "porting M10", "bb"); !strings.Contains(c.Why, "does not carry") {
		t.Errorf("M10 / bb why = %q, want the missing clause named", c.Why)
	}
}

// TestReleaseRefusesUnlessTheProofIsGreenAtThatCommit is the release lock.
func TestReleaseRefusesUnlessTheProofIsGreenAtThatCommit(t *testing.T) {
	green := Report{Commit: "abc", Cells: 10}
	if err := VerifyRelease(green, "abc"); err != nil {
		t.Fatalf("a green report at the commit refused: %v", err)
	}
	red := Report{Commit: "abc", Cells: 10, Red: []Cell{{Row: "surface wire", Lang: "goose"}, {Row: "porting M5", Lang: "rust"}}}
	err := VerifyRelease(red, "abc")
	if err == nil || !strings.Contains(err.Error(), "surface wire / goose") || !strings.Contains(err.Error(), "porting M5 / rust") {
		t.Fatalf("a red report: %v, want a refusal naming both cells", err)
	}
	for name, c := range map[string]struct {
		r      Report
		commit string
	}{
		"another commit": {green, "def"},
		"no commit":      {Report{Cells: 10}, "abc"},
		"no cells":       {Report{Commit: "abc"}, "abc"},
		"stale":          {Report{Commit: "abc", Cells: 10, Stale: "docs/proof.json is stale"}, "abc"},
	} {
		if err := VerifyRelease(c.r, c.commit); err == nil {
			t.Errorf("%s: the release went through", name)
		}
	}
}

// TestTheTreeInputsLoad holds the inputs a proof reads from the tree to
// parse: every 3.0.0 register row has a cell table, every ruling quotes Glenn
// with a date, and the conformance manifest maps cases to units.
func TestTheTreeInputsLoad(t *testing.T) {
	root := testRoot(t)
	cells, err := PortingCells(root, PortingRows)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range PortingRows {
		if len(cells[id]) < 9 {
			t.Errorf("porting row %s has %d columns", id, len(cells[id]))
		}
	}
	if _, err := LoadRulings(root); err != nil {
		t.Fatal(err)
	}
	units, order, err := CaseUnits(root)
	if err != nil || len(units) == 0 || len(order) == 0 {
		t.Fatalf("case units: %d cases, %d units, %v", len(units), len(order), err)
	}
	if _, err := LoadControls(root); err != nil {
		t.Fatal(err)
	}
}

// TestProofFindsAGateControlByName: a register cell that names only its gate
// is proven by the control the tree names after it, and the reference column
// by its section's Negative control paragraph; a two-word stem claims nothing.
func TestProofFindsAGateControlByName(t *testing.T) {
	in := fixture()
	in.Controls["tables-aa-keyed-refusal-negative-control"] = "CI full / negative controls (g)"
	in.Controls["tables-aa-negative-control"] = "CI full / negative controls (g)"
	in.Porting = map[string]map[string]string{
		"M5": {"aa": "✅ `tables-aa-keyed-refusal-ndebug`", "bb": "✅ `tables-bb-keyed`",
			referenceColumnKey: "bb", referenceControlKey: "**Negative control.** `tables-bb-keyed-shift-negative-control` in the reference."},
		"M6": {"aa": "✅ `tables-aa-other`"},
	}
	p := Compute(in)
	if c := mustCell(t, p, "porting M5", "aa"); c.Status != StatusProven || !strings.Contains(c.Control, "tables-aa-keyed-refusal-negative-control") {
		t.Errorf("M5 / aa = %+v", c)
	}
	if c := mustCell(t, p, "porting M5", "bb"); c.Status != StatusProven || !strings.Contains(c.Control, "tables-bb-keyed-shift-negative-control") {
		t.Errorf("M5 / bb = %+v", c)
	}
	if c := mustCell(t, p, "porting M6", "aa"); c.Status != StatusRed {
		t.Errorf("M6 / aa = %+v: a two-word stem claimed the gate", c)
	}
}

// TestLoadResultsOrdersLikeTheRegistry: per-leg files (CI) and one combined
// file (make conformance) give the same columns, the reference first.
func TestLoadResultsOrdersLikeTheRegistry(t *testing.T) {
	dir := t.TempDir()
	for _, l := range []string{"go", "c", "cpp"} {
		data := `{"langs":["` + l + `"],"results":{"` + l + `":{"wire":{"cases":{"a":"pass"}}}}}`
		if err := os.WriteFile(filepath.Join(dir, l+".json"), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r, err := LoadResults(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.Langs, ","); got != "cpp,c,go" {
		t.Fatalf("columns %s, want cpp,c,go", got)
	}
}
