package main

import (
	"strings"
	"testing"
)

const ledgerJSON = `[
  ["c", "SKIP: fixed_a — retired by §5.6", [["internal/codegen/ctable", "TestA"]]],
  ["c", "SKIP: fixed_b — retired by §5.6", [["internal/codegen/ctable", "TestB"]]],
  ["rust", "SKIP fixed_a: §5.6 retired it", [["internal/codegen/rusttable", "TestA"]]]
]`

func TestParseLedgerTakesOneLeg(t *testing.T) {
	got, err := parseLedger([]byte(ledgerJSON), "c")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got["SKIP: fixed_a — retired by §5.6"] {
		t.Fatalf("c ledger = %v", got)
	}
	if _, err := parseLedger([]byte(`[["c","x",[]],["c","x",[]]]`), "c"); err == nil {
		t.Fatal("a duplicate line parsed")
	}
	if _, err := parseLedger([]byte(`[["c","x"]]`), "c"); err == nil {
		t.Fatal("an entry without needs parsed")
	}
}

func TestSkipLineIsTheReadinessReading(t *testing.T) {
	for raw, want := range map[string]string{
		"SKIP: fixed_a — x":                   "SKIP: fixed_a — x",
		"  SKIP name — RETIRED by y":          "SKIP name — RETIRED by y",
		"main_test.go:12: SKIPPED: TestX — z": "SKIPPED: TestX — z",
		"RETIRED by §5.6: fn — w  ":           "RETIRED by §5.6: fn — w",
	} {
		got, ok := skipLine(raw)
		if !ok || got != want {
			t.Errorf("skipLine(%q) = %q, %v; want %q", raw, got, ok, want)
		}
	}
	for _, raw := range []string{"ok 3 checks", "cs fixed form: 3 test(s) skipped by name, §5.6", "SKIPPING"} {
		if _, ok := skipLine(raw); ok {
			t.Errorf("%q read as a skip", raw)
		}
	}
}

func TestCheckIsAnExactSet(t *testing.T) {
	ledger, err := parseLedger([]byte(ledgerJSON), "c")
	if err != nil {
		t.Fatal(err)
	}
	a, b := "SKIP: fixed_a — retired by §5.6", "SKIP: fixed_b — retired by §5.6"
	if err := check([]string{a, b, a}, ledger, "c", "l.json"); err != nil {
		t.Fatalf("the exact set is red: %v", err)
	}
	// An unledgered skip is red.
	err = check([]string{a, b, "SKIP: fixed_c — new"}, ledger, "c", "l.json")
	if err == nil || !strings.Contains(err.Error(), "SKIP: fixed_c — new") {
		t.Fatalf("an unledgered skip passed: %v", err)
	}
	// A ledgered retirement the run did not print is red.
	err = check([]string{a}, ledger, "c", "l.json")
	if err == nil || !strings.Contains(err.Error(), "were not printed") || !strings.Contains(err.Error(), b) {
		t.Fatalf("a quiet retirement passed: %v", err)
	}
	// Another leg's line is not this leg's.
	err = check([]string{a, b, "SKIP fixed_a: §5.6 retired it"}, ledger, "c", "l.json")
	if err == nil {
		t.Fatal("the rust line passed for c")
	}
}
