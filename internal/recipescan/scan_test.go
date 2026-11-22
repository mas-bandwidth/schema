package recipescan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The rules F-046, F-060, F-068 and F-069 rest on, each held by a committed
// fixture under testdata/ that goes red, beside the fixed form that stays
// green. Parse only: no process, clock, socket or network.

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fragment scans a fixture as an included makefile, the way make/*.mk are
// read, under a root whose shell sets pipefail without -e (the repository's).
func fragment(t *testing.T, name string) ([]Finding, Stats) {
	t.Helper()
	var st Stats
	fs := ScanMakefile(name, readFixture(t, name), false, false, &st)
	if st.Recipes == 0 {
		t.Fatalf("%s: read no recipe line", name)
	}
	return fs, st
}

func withRule(fs []Finding, rule string) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.Rule == rule {
			out = append(out, f)
		}
	}
	return out
}

func TestBareWaitIsRed(t *testing.T) {
	fs, _ := fragment(t, "background-bare-wait.mk")
	got := withRule(fs, RuleBackground)
	if len(got) != 1 {
		t.Fatalf("want 1 background finding, got %d: %v", len(got), fs)
	}
	if got[0].Line != 7 || !strings.Contains(got[0].Text, "bare wait") {
		t.Errorf("want the bare wait on line 7, got %v", got[0])
	}
}

func TestWaitByPidIsGreen(t *testing.T) {
	fs, _ := fragment(t, "background-wait-pid.mk")
	if len(fs) != 0 {
		t.Errorf("want no findings, got %v", fs)
	}
}

func TestStampSelfGuardIsRed(t *testing.T) {
	fs, _ := fragment(t, "stamp-self-guard.mk")
	got := withRule(fs, RuleStamp)
	if len(got) != 1 {
		t.Fatalf("want 1 stamp finding, got %d: %v", len(got), fs)
	}
	if got[0].Line != 4 || !strings.Contains(got[0].Text, "skips its work when its own target exists") {
		t.Errorf("want the self guard on line 4, got %v", got[0])
	}
}

func TestStampTouchStatusLostIsRed(t *testing.T) {
	fs, _ := fragment(t, "stamp-touch-lost.mk")
	got := withRule(fs, RuleStamp)
	if len(got) != 1 {
		t.Fatalf("want 1 stamp finding, got %d: %v", len(got), fs)
	}
	if got[0].Line != 6 || !strings.Contains(got[0].Text, `guards "touch $@" is lost`) {
		t.Errorf("want the lost touch status on line 6, got %v", got[0])
	}
}

func TestStampCorrectIsGreen(t *testing.T) {
	fs, _ := fragment(t, "stamp-correct.mk")
	if len(fs) != 0 {
		t.Errorf("want no findings, got %v", fs)
	}
}

func TestShellFlagsRootIsRed(t *testing.T) {
	var st Stats
	fs := ScanMakefile("shellflags-root.mk", readFixture(t, "shellflags-root.mk"), true, false, &st)
	got := withRule(fs, RuleNoStrictShell)
	if len(got) != 1 || !strings.Contains(got[0].Text, ".SHELLFLAGS") {
		t.Fatalf("want 1 no-strict-shell finding naming .SHELLFLAGS, got %v", fs)
	}
	// An included fragment may set neither.
	st = Stats{}
	fs = ScanMakefile("shellflags-root.mk", readFixture(t, "shellflags-root.mk"), false, false, &st)
	if got := withRule(fs, RuleNoStrictShell); len(got) != 1 {
		t.Errorf("want 1 no-strict-shell finding for a fragment, got %v", fs)
	}
}

func TestCleanRootIsGreen(t *testing.T) {
	var st Stats
	fs := ScanMakefile("clean-root.mk", readFixture(t, "clean-root.mk"), true, false, &st)
	if len(fs) != 0 {
		t.Errorf("want no findings, got %v", fs)
	}
	if st.Recipes != 2 || st.Pipes != 1 {
		t.Errorf("want 2 recipe lines and 1 pipe read, got %+v", st)
	}
}

// F-069: an extensionless script is found by its shebang anywhere in the
// tree, and a file whose shebang names another interpreter is not read.
func TestExtensionlessScriptFoundByShebang(t *testing.T) {
	fs, st, err := ScanTree(filepath.Join("testdata", "tree"), []string{"Makefile"})
	if err != nil {
		t.Fatal(err)
	}
	if st.Scripts != 1 {
		t.Errorf("want 1 script read (the driver, not tools/notes), got %d", st.Scripts)
	}
	if len(fs) != 1 {
		t.Fatalf("want 1 finding, got %v", fs)
	}
	f := fs[0]
	if f.File != "test/conformance/fixture/driver" || f.Line != 3 || f.Rule != RuleSwallow {
		t.Errorf("want the driver's swallow on line 3, got %v", f)
	}
}

func TestShellShebang(t *testing.T) {
	for line, want := range map[string]bool{
		"#!/bin/sh":                 true,
		"#!/bin/bash -e":            true,
		"#!/usr/bin/env bash":       true,
		"#!/usr/bin/env -S bash -e": true,
		"#!/usr/bin/env python3":    false,
		"#!/usr/bin/perl":           false,
		"# not a shebang":           false,
	} {
		if got := ShellShebang(line); got != want {
			t.Errorf("ShellShebang(%q) = %v, want %v", line, got, want)
		}
	}
}

// The repository itself: no recipe or script may lose a background job's
// status or a stamp generator's status (F-046, F-068), and the scan must have
// read recipes and scripts, so a scan of nothing is never green.
func TestTreeHasNoBackgroundOrStampFinding(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	fs, st, err := ScanTree(root, RootMakefiles)
	if err != nil {
		t.Fatal(err)
	}
	if st.Recipes == 0 || st.Scripts == 0 {
		t.Fatalf("read %d recipe lines and %d scripts: a scan must read both", st.Recipes, st.Scripts)
	}
	for _, f := range fs {
		if f.Rule == RuleBackground || f.Rule == RuleStamp {
			t.Errorf("%v", f)
		}
	}
}
