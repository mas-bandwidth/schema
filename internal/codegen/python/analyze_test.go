package python

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
)

// TestPythonGeneratedCorporaAreStrictClean: mypy in strict mode, at the
// floor's language version, finds nothing in the package generated for each
// packet corpus (examples/, examples128/ and the packet half of
// examples-wide/), the runtime and serialize.py included. Its control is
// TestPythonStrictGateGoesRed.
func TestPythonGeneratedCorporaAreStrictClean(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, pkg, path string }{
		{"examples", "example", repo("examples")},
		{"examples128", "ludicrous", repo("examples128")},
		{"examples-wide", "wide", repo("examples-wide", "WideText.schema")},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			pyrun.WriteTree(t, dir, generated(t, c.path))
			out, err := pyrun.Analyze(t, dir, c.pkg)
			if err != nil || !strings.Contains(out, "Success: no issues found") {
				t.Fatalf("mypy --strict over the generated %s package: %v\n%s", c.name, err, out)
			}
		})
	}
}
