package testreg

import (
	"context"
	"strings"
	"testing"
)

const owedLine = `{"rec":"owed","test":"roundtrip.fam.one","lang":"go","reason":"go answers it absent today"}` + "\n"

// TestAnOwedInstanceIsMissing: a test the language owes is an instance, never
// a pass: named in a run it is MISSING and red, coverage shows it MISS, and
// the ready query, which the pull-request group runs, leaves it out.
func TestAnOwedInstanceIsMissing(t *testing.T) {
	t.Parallel()
	reg := load(t, fixture+owedLine)
	if reason, ok := reg.OwedBy("roundtrip.fam.one", "go"); !ok || reason != "go answers it absent today" {
		t.Fatalf("OwedBy = %q, %v", reason, ok)
	}
	inst, err := reg.Resolve("go.functional.roundtrip.fam.one")
	if err != nil {
		t.Fatalf("an owed instance is an instance: %v", err)
	}
	rep := runner(reg, fake{}).Run(context.Background(), []Instance{inst})
	wantRed(t, rep, "MISSING go.functional.roundtrip.fam.one: go owes roundtrip.fam.one: go answers it absent today", "missing 1")
	for _, n := range selectQ(t, reg, Query{Ready: true, Tiers: []string{"unit", "functional"}}) {
		if n.Full == "go.functional.roundtrip.fam.one" {
			t.Fatal("the ready query holds an owed instance")
		}
	}
	if _, err := reg.Select(Query{Names: []string{"go.functional.roundtrip.fam.one"}, Ready: true}); err == nil || !strings.Contains(err.Error(), "the filters exclude the full names") {
		t.Fatalf("a full name the ready filter excludes was dropped in silence: %v", err)
	}
	c := Cover(reg, map[string]Adapter{"fake": fake{}})
	if c.Cells[0][0] != CellMissing {
		t.Fatalf("an owed cell reads %s, want %s", c.Cells[0][0], CellMissing)
	}
	// the nightly query holds it, so the nightly is red on it
	found := false
	for _, n := range selectQ(t, reg, Query{Tiers: []string{"unit", "functional"}}) {
		found = found || n.Full == "go.functional.roundtrip.fam.one"
	}
	if !found {
		t.Fatal("the every-tier query lost an owed instance")
	}
}

func TestLoadRefusesOwedForATestThatDoesNotExist(t *testing.T) {
	t.Parallel()
	bad := `{"rec":"owed","test":"roundtrip.fam.nine","lang":"go","reason":"r"}` + "\n"
	refused(t, fixture+bad, "fixture.jsonl:15:", `owed: test "roundtrip.fam.nine" does not exist`)
}

func TestLoadRefusesOwedForALanguageThatDoesNotExist(t *testing.T) {
	t.Parallel()
	bad := `{"rec":"owed","test":"roundtrip.fam.one","lang":"cobol","reason":"r"}` + "\n"
	refused(t, fixture+bad, "fixture.jsonl:15:", `owed: language "cobol" does not exist`)
}

func TestLoadRefusesOwedWithNoReason(t *testing.T) {
	t.Parallel()
	bad := `{"rec":"owed","test":"roundtrip.fam.one","lang":"go","reason":" "}` + "\n"
	refused(t, fixture+bad, "fixture.jsonl:15:", "gives no reason")
}

func TestLoadRefusesOwedTwice(t *testing.T) {
	t.Parallel()
	refused(t, fixture+owedLine+owedLine, "fixture.jsonl:16:", "already declared on line 15")
}

func TestLoadRefusesOwedForAnUnrepresentablePair(t *testing.T) {
	t.Parallel()
	bad := `{"rec":"owed","test":"roundtrip.fam.two","lang":"lua","reason":"r"}` + "\n"
	refused(t, fixture+bad, "fixture.jsonl:15:", "declares it unrepresentable")
}

func TestLoadRefusesOwedForALanguageFreeTest(t *testing.T) {
	t.Parallel()
	bad := `{"rec":"owed","test":"tooling.self.check","lang":"go","reason":"r"}` + "\n"
	refused(t, fixture+bad, "fixture.jsonl:15:", "is language-free")
}
