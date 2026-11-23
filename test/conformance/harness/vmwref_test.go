package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// vmWrefRows are the write-refusal rows a VM driver's own setter used to get
// wrong before the writer saw the value: a count that grows an EMPTY array
// (graph_tree's layers; deepCopy of a missing first element grew nothing, so
// the writer saw the old count and wrote) and a 64-bit token a double cannot
// hold (18446744073709551615 rounded to 2^64, so the writer refused a value
// the declaration admits). Each refused row's lawful twin rides beside it, so
// a setter that refused everything fails too.
var vmWrefRows = []string{
	"wref_var_layers_count", "wref_var_layers_count_ok",
	"wref_var_u64_wide", "wref_var_u64_ok",
}

// TestVMDriversSetWriteRefusalValuesExactly runs the Lua, Luau and MOO drivers'
// write-refusal surface over the committed manifest and holds vmWrefRows to
// the verdicts the manifest names, the Go engine's (docs/SPEC.md §5.1).
func TestVMDriversSetWriteRefusalValuesExactly(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "make and the pinned Lua, Luau and MOO interpreters (it runs their conformance drivers)")
	root := conformanceRoot(t)
	manifest := filepath.Join(root, defaultManifest)
	m, err := ReadManifest(manifest, filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for _, wr := range m.WriteRefusals {
		want[wr.Name] = wr.Verdict
	}
	for _, name := range vmWrefRows {
		if want[name] == "" {
			t.Fatalf("the manifest carries no write-refusal row %s", name)
		}
	}
	for _, leg := range []string{"lua", "luau", "moo"} {
		t.Run(leg, func(t *testing.T) {
			build := exec.Command("make", "build/conformance-"+leg)
			build.Dir = root
			if out, err := build.CombinedOutput(); err != nil {
				t.Fatalf("make build/conformance-%s: %v\n%s", leg, err, out)
			}
			driverPath := filepath.Join(root, "test", "conformance", leg, "driver")
			list := exec.Command(driverPath, manifest, "list")
			list.Dir = root
			listOut, err := list.Output()
			if err != nil {
				t.Fatalf("the driver's list: %v", err)
			}
			listed := map[string]bool{}
			for line := range strings.SplitSeq(string(listOut), "\n") {
				if s := strings.TrimSpace(line); s != "" {
					listed[s] = true
				}
			}
			raw, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatal(err)
			}
			copied := filepath.Join(t.TempDir(), "manifest.txt")
			if err := os.WriteFile(copied, raw, 0o644); err != nil {
				t.Fatal(err)
			}
			legManifestPath, _, _, err := legManifest(m, copied, leg, listed)
			if err != nil {
				t.Fatal(err)
			}
			out := t.TempDir()
			run := exec.Command(driverPath, legManifestPath, "write-refusal", out)
			run.Dir = root
			if got, err := run.CombinedOutput(); err != nil {
				t.Fatalf("the driver refused the write-refusal surface: %v\n%s", err, got)
			}
			for _, name := range vmWrefRows {
				answer, err := os.ReadFile(filepath.Join(out, name))
				if err != nil {
					t.Errorf("%s: the driver wrote no answer: %v", name, err)
					continue
				}
				verdict := want[name]
				if string(answer) != verdict+"\n" && (verdict != "refused" || string(answer) != "unrepresentable\n") {
					t.Errorf("%s: the driver answered %q, the manifest owes %q", name, answer, verdict+"\n")
				}
			}
		})
	}
}
