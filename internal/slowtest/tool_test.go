package slowtest

import (
	"strings"
	"testing"
)

// verdictTB records how a test ended instead of ending this one.
type verdictTB struct {
	testing.TB
	skipped, failed string
}

func (v *verdictTB) Helper() {}

func (v *verdictTB) Skip(args ...any) {
	v.skipped = args[0].(string)
	panic(v)
}

func (v *verdictTB) Fatalf(format string, args ...any) {
	v.failed = format + " " + args[0].(string)
	panic(v)
}

func verdictOf(f func(testing.TB)) (v *verdictTB) {
	v = &verdictTB{}
	defer func() { _ = recover() }()
	f(v)
	return v
}

const noSuchTool = "schema-no-such-toolchain-on-any-path"

func TestAMissingToolIsASkipWithAReason(t *testing.T) {
	t.Setenv("SCHEMA_REQUIRE_CORPUS", "")
	v := verdictOf(func(tb testing.TB) { Tool(tb, noSuchTool) })
	if v.skipped != "no "+noSuchTool+" on PATH" || v.failed != "" {
		t.Errorf("a missing tool: skipped %q, failed %q; want a skip that names it", v.skipped, v.failed)
	}
}

func TestAMissingToolFailsWhereTheLaneIsMeantToHaveIt(t *testing.T) {
	t.Setenv("SCHEMA_REQUIRE_CORPUS", "1")
	v := verdictOf(func(tb testing.TB) { Tool(tb, noSuchTool) })
	if v.skipped != "" || !strings.Contains(v.failed, "no "+noSuchTool+" on PATH") {
		t.Errorf("a missing tool under SCHEMA_REQUIRE_CORPUS: skipped %q, failed %q; want a failure that names it", v.skipped, v.failed)
	}
}

func TestAToolThatIsThereIsReturned(t *testing.T) {
	t.Setenv("SCHEMA_REQUIRE_CORPUS", "1")
	var path string
	v := verdictOf(func(tb testing.TB) { path = Tool(tb, "go") })
	if path == "" || v.skipped != "" || v.failed != "" {
		t.Errorf("go is on the PATH of its own test: path %q, skipped %q, failed %q", path, v.skipped, v.failed)
	}
}
