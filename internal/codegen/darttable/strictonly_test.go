package darttable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStrictOnlyDartNoTolerantEntry is the strict-only layer-4 grep gate for
// the Dart leg (docs/plans/strict-only.md): the generator's output for the
// leg's example schema and internal/tablenames/dart.go hold none of the
// retired tolerant names — no Tolerant entry point, and none of the retired
// counters or verdict flags (clamped, kindMismatch, duplicate, malformed,
// blobShared). The one read returns the whole value or refuses the whole
// load; it never returns a partial value beside a counter.
func TestStrictOnlyDartNoTolerantEntry(t *testing.T) {
	t.Parallel()
	retired := []string{"Tolerant", "clamped", "kindMismatch", "duplicate", "malformed", "blobShared"}

	u := unitFrom(t, `package probe
table Root { a int32
 b string(16)
 c []int32
 d *string }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("generator emitted no files for the example schema")
	}
	for name, body := range files {
		for _, word := range retired {
			if strings.Contains(string(body), word) {
				t.Errorf("generated %s retains retired tolerant name %q", name, word)
			}
		}
	}

	names, err := os.ReadFile(filepath.Join("..", "..", "tablenames", "dart.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range retired {
		if strings.Contains(string(names), word) {
			t.Errorf("internal/tablenames/dart.go retains retired tolerant name %q", word)
		}
	}
}
