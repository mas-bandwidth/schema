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

// F-068, the waited pid: "wait $p" whose status nothing reads loses the job's
// failure as surely as a bare wait.
func TestWaitStatusUnreadIsRed(t *testing.T) {
	fs, _ := fragment(t, "background-wait-unread.mk")
	got := withRule(fs, RuleBackground)
	if len(got) != 1 {
		t.Fatalf("want 1 background finding, got %d: %v", len(got), fs)
	}
	if got[0].Line != 5 || !strings.Contains(got[0].Text, "nothing reads the status of this wait") {
		t.Errorf("want the unread wait on line 5, got %v", got[0])
	}
}

// PR #4588 review: an early-exit reader in a pipeline read as a condition is red, one
// finding per planted shape (lines 5 to 12); the read-all forms are green.
func TestEarlyExitReaderInConditionIsRed(t *testing.T) {
	fs, _ := fragment(t, "early-exit-red.mk")
	got := withRule(fs, RuleEarlyExit)
	lines := map[int]bool{}
	for _, f := range got {
		lines[f.Line] = true
	}
	for l := 5; l <= 12; l++ {
		if !lines[l] {
			t.Errorf("want an early-exit finding on line %d, got %v", l, got)
		}
	}
	if len(got) != 8 {
		t.Errorf("want 8 early-exit findings, got %d: %v", len(got), got)
	}
}

func TestEarlyExitReadAllIsGreen(t *testing.T) {
	fs, _ := fragment(t, "early-exit-green.mk")
	if len(fs) != 0 {
		t.Errorf("want no findings, got %v", fs)
	}
}

// Without pipefail the pipeline's status is its last stage's, so a writer
// killed by SIGPIPE changes nothing: the rule holds only where pipefail does.
func TestEarlyExitNeedsPipefail(t *testing.T) {
	src := "#!/bin/sh\nset -e\nif cat log | grep -q x; then exit 1; fi\n"
	var st Stats
	if got := withRule(ScanScript("a.sh", src, &st), RuleEarlyExit); len(got) != 0 {
		t.Errorf("want no early-exit finding without pipefail, got %v", got)
	}
	src = "#!/bin/sh\nset -e -o pipefail\nif cat log | grep -q x; then exit 1; fi\n"
	if got := withRule(ScanScript("a.sh", src, &st), RuleEarlyExit); len(got) != 1 {
		t.Errorf("want 1 early-exit finding under pipefail, got %v", got)
	}
}

// gatedFinding reports whether the tree may not hold f: a lost background
// status, a stamp that skips its work (F-046, F-068), an early-exit reader in
// a condition (PR #4588 review), and any makefile not run under "bash -o pipefail"
// (the root losing it, an included makefile setting SHELL, or .SHELLFLAGS
// anywhere; F-060). The other rules' findings are not gated (Glenn's ruling).
func gatedFinding(f Finding) bool {
	switch f.Rule {
	case RuleBackground, RuleStamp, RuleEarlyExit:
		return true
	case RuleNoStrictShell:
		return f.File == "Makefile" || f.File == "GNUmakefile" || strings.HasSuffix(f.File, ".mk") ||
			strings.HasSuffix(f.File, "/Makefile")
	}
	return false
}

// treeFindings scans the repository, with edits (path -> new contents) read
// in place of those files, and returns its gated findings. Nothing is written.
func treeFindings(t *testing.T, edits map[string]string) []Finding {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	fs, st, err := scanTree(root, RootMakefiles, edits)
	if err != nil {
		t.Fatal(err)
	}
	if st.Recipes == 0 || st.Scripts == 0 {
		t.Fatalf("read %d recipe lines and %d scripts: a scan must read both", st.Recipes, st.Scripts)
	}
	var out []Finding
	for _, f := range fs {
		if gatedFinding(f) {
			out = append(out, f)
		}
	}
	return out
}

// The repository itself holds no gated finding, and the scan must have read
// recipes and scripts, so a scan of nothing is never green. The Makefile's
// SHELL line names this test.
func TestTreeHasNoGatedFinding(t *testing.T) {
	for _, f := range treeFindings(t, nil) {
		t.Errorf("%v", f)
	}
}

// Each way of losing pipefail goes red on the tree: the root makefile without
// its SHELL line, an included makefile setting SHELL := sh, and a .SHELLFLAGS
// line added to the root.
func TestTreeLosingPipefailIsRed(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	mk := read("Makefile")
	const shellLine = "SHELL := bash -o pipefail\n"
	if strings.Count(mk, shellLine) != 1 {
		t.Fatalf("the root Makefile must hold %q exactly once", shellLine)
	}
	frag := "make/go.mk"
	for name, edits := range map[string]map[string]string{
		"root SHELL line removed":   {"Makefile": strings.Replace(mk, shellLine, "", 1)},
		"fragment sets SHELL := sh": {frag: "SHELL := sh\n" + read(frag)},
		".SHELLFLAGS added to root": {"Makefile": strings.Replace(mk, shellLine, shellLine+".SHELLFLAGS := -e -c\n", 1)},
		// every form make obeys as an assignment (cold review of bda906475)
		"fragment: override SHELL := sh":         {frag: "override SHELL := sh\n" + read(frag)},
		"root: override SHELL := sh appended":    {"Makefile": mk + "override SHELL := sh\n"},
		"fragment: export SHELL := sh":           {frag: "export SHELL := sh\n" + read(frag)},
		"fragment: target-specific SHELL":        {frag: "cards-conformance-all: SHELL := sh\n" + read(frag)},
		"root: target-specific SHELL":            {"Makefile": mk + "cards-conformance-all: SHELL := sh\n"},
		"fragment: private target SHELL":         {frag: "cards-conformance-all: private SHELL = sh\n" + read(frag)},
		"fragment: SHELL space-indented in ifeq": {frag: "ifeq (1,1)\n  SHELL := sh\nendif\n" + read(frag)},
		"fragment: SHELL ::= sh":                 {frag: "SHELL ::= sh\n" + read(frag)},
		"fragment: SHELL ?= sh":                  {frag: "SHELL ?= sh\n" + read(frag)},
		"fragment: SHELL != echo sh":             {frag: "SHELL != echo sh\n" + read(frag)},
		"root: SHELL += -x":                      {"Makefile": mk + "SHELL += -x\n"},
		"fragment: .SHELLFLAGS += -x":            {frag: ".SHELLFLAGS += -x\n" + read(frag)},
		"root: .SHELLFLAGS += -x":                {"Makefile": mk + ".SHELLFLAGS += -x\n"},
		"root: override .SHELLFLAGS := -e -c":    {"Makefile": mk + "override .SHELLFLAGS := -e -c\n"},
		"root: target .SHELLFLAGS = -c":          {"Makefile": mk + "all: .SHELLFLAGS = -c\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := withRule(treeFindings(t, edits), RuleNoStrictShell); len(got) == 0 {
				t.Errorf("%s: want a no-strict-shell finding, got none", name)
			}
		})
	}
}
