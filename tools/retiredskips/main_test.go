package main

import (
	"strings"
	"testing"
)

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

func TestEverySkipIsRed(t *testing.T) {
	if err := check(nil, "c"); err != nil {
		t.Fatalf("a run that printed no skip is red: %v", err)
	}
	a, b := "SKIP: fixed_a — retired by §5.6", "RETIRED by §5.6: fn — w"
	err := check([]string{a, b, a}, "c")
	if err == nil {
		t.Fatal("a run that printed skips passed")
	}
	for _, want := range []string{a, b, "printed 2 skips"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}
