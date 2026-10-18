package squirreltable_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
)

// knownSurfaces is every surface test/conformance/README.md names. A driver
// may list a subset, but nothing else.
var knownSurfaces = map[string]bool{
	"wire": true, "message": true, "report": true,
	"retain": true, "retain-save": true,
	"json-read": true, "json-write": true, "json-hostile": true,
	"cook": true, "cook-write": true, "cook-foreign": true,
	"block": true, "block-foreign": true, "block-dump": true,
	"forgery": true, "cook-forgery": true,
	"cook-reason": true, "block-reason": true,
}

// TestSquirrelDriverListsItsSurfaces runs the conformance driver's `list` from
// the repository root and requires wire and report among its lines, and every
// listed surface to be one test/conformance/README.md names.
func TestSquirrelDriverListsItsSurfaces(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	sq, err := sqrun.Interpreter()
	if err != nil {
		t.Fatalf("the pinned Squirrel interpreter does not resolve (%v); run make dist-squirrel, or point SQ at sq64 built with -DSQUSEDOUBLE", err)
	}
	manifest := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")
	driver := filepath.Join(root, "test", "conformance", "squirrel", "driver")
	cmd := exec.Command(driver, manifest, "list")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "SQ="+sq)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("`%s list` failed: %v\n%s", driver, err, stderr.String())
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(stdout.String(), "\n") {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		if !knownSurfaces[s] {
			t.Errorf("the driver lists %q, which test/conformance/README.md does not name", s)
		}
		seen[s] = true
	}
	if !seen["wire"] {
		t.Errorf("the driver's `list` does not name the wire surface:\n%s", stdout.String())
	}
	if !seen["report"] {
		t.Errorf("the driver's `list` does not name the report surface:\n%s", stdout.String())
	}
}
