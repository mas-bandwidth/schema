package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRefusesMissingFlags(t *testing.T) {
	var out, errs strings.Builder
	for _, args := range [][]string{
		{"-leg", "lua", "-cases", "c", "-owed", "o", "--", "true"},
		{"-name", "x", "-cases", "c", "-owed", "o", "--", "true"},
		{"-name", "x", "-leg", "lua", "-owed", "o", "--", "true"},
		{"-name", "x", "-leg", "lua", "-cases", "c", "--", "true"},
		{"-name", "x", "-leg", "lua", "-cases", "c", "-owed", "o"},
	} {
		if code := run(args, &out, &errs); code != 2 {
			t.Errorf("run(%v) = %d, want 2", args, code)
		}
	}
}

// The reversed witness for the exact-set ledger: the packet-void ledger as
// checked in, plus the one line a cold reader planted (a case owed under the
// misspelt leg golang), is refused before the go driver runs, by name. The
// ledger is read before the command, so the command is never started.
func TestRunRefusesAPlantedUnknownLegLine(t *testing.T) {
	pins := filepath.Join("..", "..", "testdata", "wire", "packet-void")
	owed, err := os.ReadFile(filepath.Join(pins, "owed.txt"))
	if err != nil {
		t.Fatal(err)
	}
	planted := filepath.Join(t.TempDir(), "owed.txt")
	if err := os.WriteFile(planted, append(owed, "golang mixed-ping owed: plant\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errs strings.Builder
	args := []string{"-name", "packet-void-go", "-leg", "go", "-cases", filepath.Join(pins, "cases.txt"), "-owed", planted, "--", "no-such-driver"}
	if code := run(args, &out, &errs); code != 2 || !strings.Contains(errs.String(), "golang is not a leg") {
		t.Fatalf("run with a planted golang line = %d, stderr %q; want 2 naming golang", code, errs.String())
	}
}
