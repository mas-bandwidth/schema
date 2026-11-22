// THE GO TARGET NEVER EMITS A FILE NAME GO READS AS A BUILD CONSTRAINT: the
// packet source is <base>.go, and the go tool gives a file name meaning — a
// `_test` suffix makes it test-only, a `_<goos>`, `_<goarch>` or
// `_<goos>_<goarch>` suffix limits it to that platform, and a leading `_` or
// `.` drops it from the package. Such a basename is refused at check for
// every target (SPEC §4.6, File-name safety: a unit the checker accepts
// builds in all 18 targets), naming Go; the Go target keeps its own refusal
// behind the checker's, held here on a unit renamed after the check.
package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/irtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func loadBase(t *testing.T, base, src string) *ir.Unit {
	t.Helper()
	u, err := loadBaseErr(t, base, src)
	if err != nil {
		t.Fatalf("%s.schema: load: %v", base, err)
	}
	return u
}

func loadBaseErr(t *testing.T, base, src string) (*ir.Unit, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, base+".schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return New().Load([]string{path})
}

func TestGoRefusesABasenameGoReadsAsABuildConstraint(t *testing.T) {
	t.Parallel()
	const src = "package p\ntype Ship { hp int32 }\ntable Fleet { size int32 }\n"
	for _, base := range []string{"Player_test", "Player_windows", "Player_amd64", "Player_linux_arm64", "Player_js_test", "_Player"} {
		// the checker refuses it for every target, naming Go
		if _, err := loadBaseErr(t, base, src); err == nil || !strings.Contains(err.Error(), "(Go)") {
			t.Errorf("%s.schema: want the checker's refusal naming Go, got %v", base, err)
		}
		// the Go target's own guard, behind the checker's
		u := loadBase(t, "Player", src)
		irtest.Rebase(t, u, "Player", base)
		if _, err := New().Generate(u, "go", Options{}); err == nil || !strings.Contains(err.Error(), "build constraint") {
			t.Errorf("%s.schema: want the Go file-name refusal, got %v", base, err)
		}
	}
	// `_test.go` is read off the WHOLE name, past a dot: test-only. A dot is
	// not an identifier character, so the checker refuses these basenames
	// first (SPEC §4.6 rule 1); the Go guard still reads the whole name.
	for _, base := range []string{"Player_linux.v1", "Player.v1_test"} {
		if _, err := loadBaseErr(t, base, src); err == nil || !strings.Contains(err.Error(), "is not an identifier") {
			t.Errorf("%s.schema: want the checker's identifier refusal, got %v", base, err)
		}
		u := loadBase(t, "Player", src)
		irtest.Rebase(t, u, "Player", base)
		if _, err := New().Generate(u, "go", Options{}); err == nil || !strings.Contains(err.Error(), "build constraint") {
			t.Errorf("%s.schema: want the Go file-name refusal, got %v", base, err)
		}
	}
	// a name that only looks like one is accepted and generated: the go tool
	// reads the GOOS/GOARCH parts after the first `_` exactly in lower case
	for _, base := range []string{"Player", "linux", "Player_Windows", "Player_tests", "Player_x"} {
		files, err := New().Generate(loadBase(t, base, src), "go", Options{})
		if err != nil {
			t.Errorf("%s.schema: %v", base, err)
			continue
		}
		if _, ok := files[base+".go"]; !ok {
			t.Errorf("%s.schema: no %s.go", base, base)
		}
	}
}
