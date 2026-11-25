package slowtest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// verdictTB records how a helper ended a test instead of ending this one.
type verdictTB struct {
	testing.TB
	failed string
}

func (v *verdictTB) Helper() {}

func (v *verdictTB) Fatalf(format string, args ...any) {
	v.failed = fmt.Sprintf(format, args...)
	panic(v)
}

func verdictOf(f func(testing.TB)) (v *verdictTB) {
	v = &verdictTB{}
	defer func() { _ = recover() }()
	f(v)
	return v
}

const noSuchTool = "schema-no-such-toolchain-on-any-path"

func TestAMissingToolFailsNamingIt(t *testing.T) {
	v := verdictOf(func(tb testing.TB) { Tool(tb, noSuchTool) })
	if !strings.Contains(v.failed, "no "+noSuchTool+" on PATH") {
		t.Errorf("a missing tool: failed %q; want a failure that names it", v.failed)
	}
}

func TestAToolThatIsThereIsReturned(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "schema-probe-tool"), nil, 0o755); err != nil { // an executable stand-in, never run
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	var path string
	v := verdictOf(func(tb testing.TB) { path = Tool(tb, "schema-probe-tool") })
	if path != filepath.Join(dir, "schema-probe-tool") || v.failed != "" {
		t.Errorf("a tool on PATH: path %q, failed %q", path, v.failed)
	}
}
