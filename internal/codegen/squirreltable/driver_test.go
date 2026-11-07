package squirreltable_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// knownSurfaces is every surface test/conformance/README.md names. A driver
// may list a subset, but nothing else.
var knownSurfaces = map[string]bool{
	"wire": true, "message": true, "report": true, "framing": true,
	"retain": true, "retain-save": true,
	"json-read": true, "json-write": true, "json-hostile": true,
	"cook": true, "cook-write": true, "cook-foreign": true,
	"block": true, "block-foreign": true, "block-dump": true,
	"forgery": true, "cook-forgery": true,
	"cook-reason": true, "block-reason": true,
}

// TestSquirrelDriverListsItsSurfaces runs the conformance driver's `list` from
// the repository root and requires every surface test/conformance/README.md
// names, and nothing else.
func TestSquirrelDriverListsItsSurfaces(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	sq := sqrun.Require(t)
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
	for line := range strings.SplitSeq(stdout.String(), "\n") {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		if !knownSurfaces[s] {
			t.Errorf("the driver lists %q, which test/conformance/README.md does not name", s)
		}
		seen[s] = true
	}
	// PARITY: the nine main legs answer every surface (test/conformance/
	// README.md), so the Squirrel leg lists every one; a case it cannot reach
	// is a per-case absence, never a surface left out.
	for s := range knownSurfaces {
		if !seen[s] {
			t.Errorf("the driver's `list` does not name the %s surface:\n%s", s, stdout.String())
		}
	}
}
