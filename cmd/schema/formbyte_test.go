// WHAT BYTE 0 SAYS, FIRST (docs/SPEC-TABLES.md §3, "THE FIRST BYTE").
//
// Every schema file begins with the form byte, and the registry is the only
// place a value is assigned. So `schema check` handed a saved file answers its
// FORM before it says anything else about it, on one line — and byte `0`, and
// any value no form defines, are refused BY NAME rather than guessed at.
//
// The CLI is driven as a user drives it, a process and an argument list, for
// the reason main_test.go gives: the policy under test is the CLI's.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// a saved file of one byte, which is all a form line reads.
func formFile(t *testing.T, name string, b ...byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// THE THREE LIVE FORMS, THE TWO PLANNED ONES, AND THE REFUSALS — one line each,
// and the line is the FIRST thing the command prints.
func TestCheckReportsTheFormByteFirst(t *testing.T) {
	bin := buildCLI(t)
	for _, row := range []struct {
		byte byte
		want string
	}{
		{1, "FORM 1 the variable form"},
		{2, "FORM 2 the message form"},
		{3, "FORM 3 the fixed form"},
		{4, "FORM 4 the cook — PLANNED"},
		{5, "FORM 5 the block form — PLANNED"},
		{0, "REFUSED: form 0 is never assigned"},
		{6, "REFUSED: form 6 is assigned by no form"},
		{0xFF, "REFUSED: form 255 is assigned by no form"},
	} {
		p := formFile(t, "saved.bin", row.byte, 0, 0, 0)
		out := run(t, bin, "check", p)
		first, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
		if !strings.Contains(first, row.want) {
			t.Errorf("check over a form-%d file printed %q first, want a line carrying %q", row.byte, first, row.want)
		}
		if !strings.HasPrefix(first, p+": ") {
			t.Errorf("the form line must name the file it is about: %q", first)
		}
	}
}

// AN EMPTY FILE HAS NO FIRST BYTE, and it is refused for that rather than for a
// form it does not carry.
func TestCheckRefusesAFileWithNoFormByte(t *testing.T) {
	bin := buildCLI(t)
	out := run(t, bin, "check", formFile(t, "empty.bin"))
	if !strings.Contains(out, "REFUSED: an empty file carries no form byte") {
		t.Errorf("an empty file must be refused by name: %q", out)
	}
}

// AND THE FORM LINE COMES BEFORE THE UNIT'S OWN ANSWER when both are asked
// for, because it is the fact the rest of the read depends on.
func TestCheckPrintsTheFormLineBeforeTheUnit(t *testing.T) {
	bin := buildCLI(t)
	dir, _ := writeUnit(t, filepath.Join(t.TempDir(), "unit"))
	out := run(t, bin, "check", "--verbose", formFile(t, "saved.bin", 3), dir)
	form := strings.Index(out, "FORM 3 the fixed form")
	ok := strings.Index(out, "ok: package")
	if form < 0 || ok < 0 {
		t.Fatalf("both lines must be printed: %q", out)
	}
	if form > ok {
		t.Errorf("the form line must come FIRST: %q", out)
	}
}
