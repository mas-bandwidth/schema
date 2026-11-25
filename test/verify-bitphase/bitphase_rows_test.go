package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// TestTheRegistryHoldsTheBitPhaseRows: the registry's bit-phase tests are
// bitphase_rows.go's rows exactly, line for line: a roster row added with no
// registry test, a registry test with no row behind it, and a test whose
// inputs, type or words moved are each red, by key.
func TestTheRegistryHoldsTheBitPhaseRows(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../" + testreg.DefaultPath)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var head struct{ Rec, Key, Family string }
		if json.Unmarshal(sc.Bytes(), &head) == nil && head.Rec == "test" && head.Family == BitPhaseFamily {
			got[head.Key] = sc.Text()
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := writeBitPhaseRows(&b); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for line := range strings.SplitSeq(strings.TrimSpace(b.String()), "\n") {
		var head struct{ Key string }
		if err := json.Unmarshal([]byte(line), &head); err != nil {
			t.Fatal(err)
		}
		want[head.Key] = true
		switch g, ok := got[head.Key]; {
		case !ok:
			t.Errorf("%s: a bit-phase row the registry does not hold: run `build/verify-bitphase-bin --bit-phase-rows` and put its line in %s", head.Key, testreg.DefaultPath)
		case g != line:
			t.Errorf("%s: the registry row is not bitphase_rows.go's:\n  registry %s\n  rows     %s", head.Key, g, line)
		}
	}
	for k := range got {
		if !want[k] {
			t.Errorf("%s: a registry test with no bit-phase row behind it", k)
		}
	}
	if len(want) != len(requiredRoster())+2 {
		t.Errorf("%d rows for a roster of %d and two steps", len(want), len(requiredRoster()))
	}
}

// TestABitPhaseRequestIsHeldToItsRow: a request whose inputs are its row's
// passes; one with other inputs, another family, another language, a case no
// row has, or a case asked twice is refused before anything runs.
func TestABitPhaseRequestIsHeldToItsRow(t *testing.T) {
	t.Parallel()
	rows := bitPhaseRows()
	r := rows[3]
	ok, err := json.Marshal(r.Inputs)
	if err != nil {
		t.Fatal(err)
	}
	other := r.Inputs
	other.Kind = "refuse"
	bad, _ := json.Marshal(other)
	req := func(lang, family, c string, in []byte) testreg.Request {
		return testreg.Request{Instance: lang + ".functional." + r.Type + "." + family + "." + c, Lang: lang, Family: family, Case: c, Inputs: in}
	}
	if got, err := bitPhaseRequests("go", []testreg.Request{req("go", BitPhaseFamily, r.Case, ok)}); err != nil || len(got) != 1 || got[0].row.Key != r.Key {
		t.Fatalf("a request that is its row: %v %+v", err, got)
	}
	for name, reqs := range map[string][]testreg.Request{
		"other inputs":   {req("go", BitPhaseFamily, r.Case, bad)},
		"other family":   {req("go", AlignArraysFamily, r.Case, ok)},
		"other language": {req("c", BitPhaseFamily, r.Case, ok)},
		"no such case":   {req("go", BitPhaseFamily, "no-such-case", ok)},
		"asked twice":    {req("go", BitPhaseFamily, r.Case, ok), req("go", BitPhaseFamily, r.Case, ok)},
	} {
		if _, err := bitPhaseRequests("go", reqs); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// TestTheBitPhaseRowsAreTheRosterAndTheSteps: one row per roster row, a
// valid row a roundtrip and a refuse row a load-refusal, each reading the
// main schema through its language's driver; then the two main-only steps.
func TestTheBitPhaseRowsAreTheRosterAndTheSteps(t *testing.T) {
	t.Parallel()
	rows := bitPhaseRows()
	roster := requiredRoster()
	cases := map[string]bool{}
	for i, c := range roster {
		r := rows[i]
		if r.Inputs.File != c.File || r.Inputs.Kind != c.Kind || r.Inputs.Root != c.Type ||
			(c.Kind == "valid") != (r.Type == "roundtrip") || (c.Kind == "refuse") != (r.Type == "load-refusal") ||
			r.Inputs.Schema != BitPhaseSchema || len(r.Inputs.Files) != 1 || r.Inputs.Files[0] != BitPhaseSchema || r.Inputs.Driver != bitPhaseDriver {
			t.Errorf("row %d %+v is not the roster's %+v", i, r, c)
		}
		if cases[r.Case] {
			t.Errorf("two rows are case %s", r.Case)
		}
		cases[r.Case] = true
	}
	steps := rows[len(roster):]
	if len(steps) != 2 || steps[0].Inputs.Step != bitPhaseCfDecodeCase || steps[1].Inputs.Step != bitPhaseF32LitCase {
		t.Errorf("the steps: %+v", steps)
	}
}
