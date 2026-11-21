// THE GO TARGET NEVER EMITS A FILE NAME GO READS AS A BUILD CONSTRAINT: the
// packet source is <base>.go, and the go tool gives a file name meaning — a
// `_test` suffix makes it test-only, a `_<goos>`, `_<goarch>` or
// `_<goos>_<goarch>` suffix limits it to that platform, and a leading `_` or
// `.` drops it from the package. Such a basename is refused by the Go target
// by name; every other target still generates the unit.
package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func loadBase(t *testing.T, base, src string) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, base+".schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := New().Load([]string{path})
	if err != nil {
		t.Fatalf("%s.schema: load: %v", base, err)
	}
	return u
}

func TestGoRefusesABasenameGoReadsAsABuildConstraint(t *testing.T) {
	t.Parallel()
	const src = "package p\ntype Ship { hp int32 }\ntable Fleet { size int32 }\n"
	for _, base := range []string{"Player_test", "Player_windows", "Player_amd64", "Player_linux_arm64", "Player_js_test", "_Player"} {
		_, err := New().Generate(loadBase(t, base, src), "go", Options{})
		if err == nil || !strings.Contains(err.Error(), "build constraint") {
			t.Errorf("%s.schema: want the Go file-name refusal, got %v", base, err)
		}
		if _, err := New().Generate(loadBase(t, base, src), "cpp", Options{}); err != nil {
			t.Errorf("%s.schema: cpp refused a basename only Go cannot carry: %v", base, err)
		}
	}
	// a name that only looks like one is generated: the go tool reads the
	// parts after the first `_`, exactly in lower case
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
