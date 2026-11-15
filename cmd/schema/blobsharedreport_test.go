// `schema cook` refuses a shared blob (docs/SPEC-TABLES.md §2.5, §6.5): the
// read refuses the whole wire `blob_shared`, cook exits nonzero naming it and
// writes nothing. The accepted twin is the corpus's blob_small, which cooks
// silently.
package main

import (
	"errors"
	"os"
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

func TestCookBlobSharedRefuses(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	work := t.TempDir()
	// the repo root, reached from cmd/schema by walking up two directories.
	repo := filepath.Join("..", "..")
	wire := filepath.Join(repo, "testdata", "wire", "tables", "blob_shared.bin")
	unit := filepath.Join(repo, "tables", "blobs")

	out := filepath.Join(work, "x.cook")
	code, text := runExit(t, bin, "cook", "--root", "Catalog", "--in", wire, "--out", out, unit)
	if code == 0 {
		t.Fatalf("cook of the shared-blob wire exited 0; want nonzero\n%s", text)
	}
	if !strings.Contains(text, "blob_shared") {
		t.Errorf("cook does not name blob_shared:\n%s", text)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("cook of a refused wire wrote %s", out)
	}

	small := filepath.Join(repo, "testdata", "wire", "tables", "blob_small.bin")
	if code, text := runExit(t, bin, "cook", "--root", "Catalog", "--in", small,
		"--out", filepath.Join(work, "small.cook"), unit); code != 0 || text != "" {
		t.Fatalf("cook of blob_small exited %d with %q; want 0 and silence", code, text)
	}
}
