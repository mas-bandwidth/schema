package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestValueGoldensAreTheOracles holds every committed value golden to the
// oracle that wrote it: a report row's value is internal/tablewire's decode
// written by internal/tabletext, and a hostile row's is internal/tabletext's
// read of the tree written back by the same writer. A golden that drifted
// from the engine, or a row that reads and has no golden, is red here before
// any leg is compared against it.
func TestValueGoldensAreTheOracles(t *testing.T) {
	m, pinned, u := corpus(t)
	for _, rc := range m.Reports {
		c := pinned[rc.Name]
		inst, _ := decodeWire(t, u, rc.Unit, rc.Root, rc.Wire)
		checkGolden(t, rc.Name, rc.Value, !c.Malformed && !c.Refused, u, rc.Unit, inst)
	}
	for _, h := range m.Hostiles {
		unit, err := u.get(h.Unit)
		if err != nil {
			t.Fatal(err)
		}
		text, err := os.ReadFile(filepath.Join(h.Tree, h.Root+".json"))
		if err != nil {
			t.Fatal(err)
		}
		model := tabletext.NewModel(unit)
		inst := model.New(model.Lookup(h.Root))
		var report tabletext.Report
		model.Read(inst, text, &report)
		checkGolden(t, h.Name, h.Value, !h.Refused, u, h.Unit, inst)
	}
}

func checkGolden(t *testing.T, name, path string, reads bool, u *units, unitKey string, inst *tabletext.Instance) {
	t.Helper()
	golden, err := os.ReadFile(path)
	if !reads {
		if err == nil {
			t.Errorf("%s: the row owes no value and %s is a golden nobody owes — run: make conformance-generate", name, path)
		}
		return
	}
	if err != nil {
		t.Errorf("%s: the row reads and owes a value: %v — run: make conformance-generate", name, err)
		return
	}
	unit, err := u.get(unitKey)
	if err != nil {
		t.Fatal(err)
	}
	text, err := tabletext.NewModel(unit).Write(inst)
	if err != nil {
		t.Fatalf("%s: the engine's writer refused the value: %v", name, err)
	}
	if !bytes.Equal(oneText(text), golden) {
		t.Errorf("%s: %s is not the oracle's value — run: make conformance-generate", name, path)
	}
}

// TestValueComparisonIsRedOnAValue is the harness's own negative control for
// the value comparison: a leg answering the right verdict line with one value
// byte changed is red, and its failure names the value rather than the
// verdict; the same answer with the oracle's value is green; and a port on
// the row's uncarried verdict is held to that line alone.
func TestValueComparisonIsRedOnAValue(t *testing.T) {
	dir := t.TempDir()
	value := filepath.Join(dir, "row.value.json")
	golden := []byte("{\n  \"ratings\": [\n    1.4013e-45\n  ]\n}\n")
	if err := os.WriteFile(value, golden, 0o644); err != nil {
		t.Fatal(err)
	}
	row := Hostile{Name: "row", Counts: Counts{}, Value: value}
	e := hostileExpectation(row, "go")
	if e.value != value || e.verdictOnly {
		t.Fatalf("a carried read owes the value: %+v", e)
	}

	score := func(answer []byte, e expectation) *result {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "row"), answer, 0o644); err != nil {
			t.Fatal(err)
		}
		r := &result{}
		scoreAnswer("go", "json-hostile", dir, e, Instance{}, r)
		return r
	}
	verdict := []byte("0,0,0,0,0,false,read\n")
	if r := score(append(append([]byte{}, verdict...), golden...), e); r.pass != 1 {
		t.Fatalf("the oracle's value is red: %v", r.failures)
	}
	drifted := bytes.Replace(golden, []byte("1.4013e-45"), []byte("0"), 1)
	r := score(append(append([]byte{}, verdict...), drifted...), e)
	if r.pass != 0 || len(r.failures) != 1 || !strings.Contains(r.failures[0], "VALUE differs") || !strings.Contains(r.failures[0], "1.4013e-45") {
		t.Fatalf("a drifted value under an agreeing verdict is not named red: pass=%d %v", r.pass, r.failures)
	}
	if r := score(verdict, e); r.pass != 0 {
		t.Fatal("an answer with no value passes a row that owes one")
	}

	// the value ABSENT beside the verdict: the line is still held, and the
	// value counts as missing, never as a pass
	if err := os.WriteFile(filepath.Join(dir, "row.absent"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if r := score(verdict, e); r.pass != 0 || r.missing != 1 || len(r.failures) != 0 {
		t.Fatalf("a value answered absent is not counted missing: pass=%d missing=%d %v", r.pass, r.missing, r.failures)
	}
	if r := score([]byte("1,0,0,0,0,false,read\n"), e); r.missing != 0 || len(r.failures) != 1 {
		t.Fatalf("an absent value excuses a wrong verdict: missing=%d %v", r.missing, r.failures)
	}
	if err := os.Remove(filepath.Join(dir, "row.absent")); err != nil {
		t.Fatal(err)
	}

	// the uncarried port: its verdict line alone, whatever value follows
	row.CarriedBy, row.Uncarried = []string{"go"}, "refused"
	e = hostileExpectation(row, "lua")
	if !e.verdictOnly || e.value != "" {
		t.Fatalf("an uncarried port is held to a value: %+v", e)
	}
	if r := score([]byte("refused\n"), e); r.pass != 1 {
		t.Fatalf("the uncarried verdict is red: %v", r.failures)
	}
	if r := score(verdict, e); r.pass != 0 {
		t.Fatal("the uncarried port passes on the standard's verdict")
	}
}
