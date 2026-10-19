package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSkippedLegIsNotAConformancePrerequisite is the skip gate on the
// conformance registry (Makefile, unless_skipped): a leg named in
// SCHEMA_SKIP_LEGS registers NO conformance prerequisite, so `make conformance
// SCHEMA_SKIP_LEGS=c` reaches the Go leg without building C. It reads the same
// registry `make -s registry` prints, after registry_test.go's pattern, twice:
// once with nothing skipped, where build/conformance-c MUST be named (the
// control, so the gate cannot pass on a registry that never listed C), and
// once with c skipped, where it must not be, while build/conformance-go still
// is (a skip removes that one leg and nothing else).
func TestSkippedLegIsNotAConformancePrerequisite(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("make"); err != nil {
		t.Skipf("no make on PATH")
	}

	conformance := func(skip string) []string {
		t.Helper()
		mk := exec.Command("make", "-s", "registry")
		mk.Dir = root
		env := []string{}
		for _, kv := range os.Environ() {
			if !strings.HasPrefix(kv, "SCHEMA_SKIP_LEGS=") && !strings.HasPrefix(kv, "MAKEFLAGS=") && !strings.HasPrefix(kv, "MFLAGS=") {
				env = append(env, kv)
			}
		}
		mk.Env = append(env, "SCHEMA_SKIP_LEGS="+skip)
		out, err := mk.Output()
		if err != nil {
			t.Fatalf("make -s registry SCHEMA_SKIP_LEGS=%q: %v", skip, err)
		}
		for _, line := range strings.Split(string(out), "\n") {
			if rest, ok := strings.CutPrefix(line, "conformance:"); ok {
				return strings.Fields(rest)
			}
		}
		t.Fatalf("make -s registry SCHEMA_SKIP_LEGS=%q printed no conformance line:\n%s", skip, out)
		return nil
	}
	names := func(legs []string, target string) bool {
		for _, l := range legs {
			if l == target {
				return true
			}
		}
		return false
	}

	// CONTROL: nothing skipped, and the C leg IS a conformance prerequisite.
	if all := conformance(""); !names(all, "build/conformance-c") {
		t.Fatalf("CONTROL FAILED: with nothing skipped the conformance line does not name build/conformance-c: %v", all)
	}

	skipped := conformance("c")
	if names(skipped, "build/conformance-c") {
		t.Fatalf("SCHEMA_SKIP_LEGS=c still makes build/conformance-c a conformance prerequisite: %v", skipped)
	}
	if !names(skipped, "build/conformance-go") {
		t.Fatalf("SCHEMA_SKIP_LEGS=c removed more than the C leg; build/conformance-go is gone: %v", skipped)
	}
}
