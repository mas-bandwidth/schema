package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// mainOnlyName matches the main schema's own case types as any driver spells
// them (CfDecode, cf_decode, F32Lit, f32_lit, F32Const, F32_CONST, f32Const)
// and the case's output tags and tokens.
var mainOnlyName = regexp.MustCompile(`(?i)cf_?decode|f32_?lit|f32_?const`)

// TestEveryMainOnlyStepIsMarked is the reason a case family can run every
// driver: each line of a driver source that names a type only the main
// BitPhase.schema declares sits between the main-only markers, so a family
// run drops it. A driver step added outside them breaks every family run on
// that leg (#4997 broke `make verify-squirrel-bench` with "the index
// 'CfDecode' does not exist"); this names the line instead.
func TestEveryMainOnlyStepIsMarked(t *testing.T) {
	t.Parallel()
	for _, file := range []string{"drivers_compiled.go", "drivers_vm.go"} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		in, blocks := false, 0
		for n, line := range strings.Split(string(b), "\n") {
			switch {
			case strings.HasSuffix(line, mainOnlyBegin) && !strings.HasSuffix(line, mainOnlyEnd):
				in = true
				blocks++
				continue
			case strings.TrimSpace(line) == mainOnlyEnd:
				in = false
				continue
			}
			if in || strings.Contains(line, "withCfDecodeSteps(") {
				continue
			}
			if mainOnlyName.MatchString(line) {
				t.Errorf("%s:%d names a main-only case outside the markers: %s", file, n+1, strings.TrimSpace(line))
			}
		}
		if blocks == 0 {
			t.Errorf("%s has no main-only block", file)
		}
	}
}

// TestMainOnlyStepsKeepOrDrop: a main run keeps the steps and drops only the
// markers, a family run drops the block whole, and a malformed block panics.
func TestMainOnlyStepsKeepOrDrop(t *testing.T) {
	t.Parallel()
	src := "a\n  @MAIN_ONLY@\nb\nc\n@END_MAIN_ONLY@\nd"
	if got := mainOnlySteps(src, true); got != "a\nb\nc\nd" {
		t.Errorf("main run: %q", got)
	}
	if got := mainOnlySteps(src, false); got != "a\nd" {
		t.Errorf("family run: %q", got)
	}
	for _, bad := range []string{
		"@MAIN_ONLY@\na",
		"a\n@END_MAIN_ONLY@",
		"@MAIN_ONLY@\n@MAIN_ONLY@\n@END_MAIN_ONLY@",
		"x @MAIN_ONLY@ y",
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%q: want a panic", bad)
				}
			}()
			mainOnlySteps(bad, true)
		}()
	}
}

// TestFamilyRunRefusesMainOnlyOutput: a step that ran in a family run fails
// by name.
func TestFamilyRunRefusesMainOnlyOutput(t *testing.T) {
	t.Parallel()
	if err := checkNoMainOnlyOutput("squirrel", "BITPHASE_OK squirrel 36\n"); err != nil {
		t.Fatal(err)
	}
	if err := checkNoMainOnlyOutput("squirrel", "CFDECODE squirrel 0 c00a6666\n"); err == nil || !strings.Contains(err.Error(), "main-corpus step") {
		t.Fatalf("want a refusal, got %v", err)
	}
}

// TestFamilyTokenAloneIsAPass: a family run's driver prints only its token,
// and requireToken passes it; on a main run the same output is red, since the
// main corpus owes the decode and literal cases. Without the family guard in
// requireToken every family run failed on the missing CFDECODE lines (#4997).
func TestFamilyTokenAloneIsAPass(t *testing.T) {
	savedRoster, savedFamily := activeRoster, familyRun
	t.Cleanup(func() { activeRoster, familyRun = savedRoster, savedFamily })
	activeRoster, familyRun = alignArraysRoster, true
	out := fmt.Sprintf("BITPHASE_OK squirrel %d\n", len(alignArraysRoster()))
	if err := requireToken(out, nil, "squirrel"); err != nil {
		t.Fatalf("a family run's token alone: %v", err)
	}
	familyRun = false
	if err := requireToken(out, nil, "squirrel"); err == nil {
		t.Fatal("a main run's token alone passed: the decode and literal cases went unchecked")
	}
}
