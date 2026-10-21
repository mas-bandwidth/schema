// `schema cook` is not silent about a shared blob (docs/SPEC-TABLES.md §2.5,
// §4, §17.1). The tolerant read counts blob_shared, and the printed counter
// line has to carry it, so a pipeline can see the wire and fail on it unless
// --tolerate says the report is expected.
//
// Reverting any one of the three edits turns a named test red:
//   - cmd/schema/main.go's printed counter → this test (the line loses
//     `blob_shared 1`).
//   - compiler/pack.go's TableReport.BlobShared/publicReport/Silent → this
//     test (the report reads silent and cook exits 0).
//   - internal/tabletext/value.go's Report.Silent() → Cook's TableReport
//     (tested in compiler/blobsharedreport_test.go).
package main

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runExit is run's variant for a command that is EXPECTED to fail: it returns
// the exit code and the combined output instead of failing the test.
func runExit(t *testing.T, bin string, args ...string) (int, string) {
	t.Helper()
	out, err := exec.Command(bin, args...).CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("schema %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	return code, string(out)
}

func TestCookBlobSharedIsNotSilent(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	work := t.TempDir()
	// the repo root, reached from cmd/schema by walking up two directories.
	repo := filepath.Join("..", "..")
	wire := filepath.Join(repo, "testdata", "wire", "tables", "blob_shared.bin")
	unit := filepath.Join(repo, "tables", "blobs")

	code, out := runExit(t, bin, "cook", "--root", "Catalog", "--in", wire,
		"--out", filepath.Join(work, "x.cook"), unit)
	if code == 0 {
		t.Fatalf("cook of the shared-blob wire exited 0; want nonzero\n%s", out)
	}
	if !strings.Contains(out, "the report is not silent") {
		t.Errorf("stderr does not name the non-silent report:\n%s", out)
	}
	if !strings.Contains(out, "blob_shared 1") {
		t.Errorf("the printed counter line does not carry blob_shared 1:\n%s", out)
	}

	// --tolerate accepts the report but still prints the count.
	code, out = runExit(t, bin, "cook", "--root", "Catalog", "--in", wire,
		"--out", filepath.Join(work, "tolerated.cook"), "--tolerate", unit)
	if code != 0 {
		t.Fatalf("cook --tolerate exited %d; want 0\n%s", code, out)
	}
	if !strings.Contains(out, "blob_shared 1") {
		t.Errorf("cook --tolerate did not print blob_shared 1:\n%s", out)
	}
}
