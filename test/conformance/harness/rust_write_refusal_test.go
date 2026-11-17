package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestRustDriverAnswersWriteRefusal runs the Rust conformance driver over the
// four write-refusal surfaces and holds every answer to the verdict the
// manifest names (docs/SPEC.md §5.1). The Rust writers are an assert leg, so
// the driver's debug build is the one that answers.
func TestRustDriverAnswersWriteRefusal(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	build := exec.Command("make", "build/conformance-rust")
	build.Dir = root
	// This gate checks the debug writer's refusal, so optimization and debug
	// symbols add build work without changing its assertion coverage. Keep the
	// assertions explicit; the row must also fit with a cold target.
	build.Env = append(os.Environ(),
		"CARGO_PROFILE_DEV_OPT_LEVEL=0",
		"CARGO_PROFILE_DEV_DEBUG=0",
		"CARGO_PROFILE_DEV_INCREMENTAL=false",
		"CARGO_PROFILE_DEV_DEBUG_ASSERTIONS=true",
	)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("make build/conformance-rust: %v\n%s", err, out)
	}
	manifest := filepath.Join(root, defaultManifest)
	m, err := ReadManifest(manifest, filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	driver := filepath.Join(root, "test", "conformance", "rust", "driver")
	for _, surface := range []string{"write-refusal", "write-refusal-fixed", "write-refusal-message", "write-refusal-json"} {
		t.Run(surface, func(t *testing.T) {
			var rows []WriteRefusal
			for _, wr := range m.WriteRefusals {
				if wr.Surface() == surface {
					rows = append(rows, wr)
				}
			}
			if len(rows) == 0 {
				t.Fatalf("the manifest carries no %s rows", surface)
			}
			out := t.TempDir()
			run := exec.Command(driver, manifest, surface, out)
			run.Dir = root
			if got, err := run.CombinedOutput(); err != nil {
				t.Fatalf("the driver refused the %s surface: %v\n%s", surface, err, got)
			}
			for _, wr := range rows {
				answer, err := os.ReadFile(filepath.Join(out, wr.Name))
				if err != nil {
					t.Errorf("%s: the driver wrote no answer: %v", wr.Name, err)
					continue
				}
				if want := wr.Verdict + "\n"; string(answer) != want {
					t.Errorf("%s: the driver answered %q, the manifest owes %q", wr.Name, answer, want)
				}
			}
		})
	}
}
