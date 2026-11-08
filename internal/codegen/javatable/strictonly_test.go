package javatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStrictOnlyJavaNoTolerantEntry is the strict-only layer-4 grep gate for
// the Java leg (docs/plans/strict-only.md): the generator's output for the
// leg's example schema and internal/tablenames/java.go hold none of the
// retired tolerant names — no Tolerant entry point, and none of the retired
// counters or verdict flags (clamped, kindMismatch, duplicate, malformed,
// blobShared). The one read returns the whole value or refuses the whole
// load; it never returns a partial value beside a counter.
func TestStrictOnlyJavaNoTolerantEntry(t *testing.T) {
	t.Parallel()
	retired := []string{
		"Tolerant",
		"tolerant",
		"kindMismatch",
		"clamped",
		"duplicate",
		"malformed",
		"blobShared",
	}

	u := unitFrom(t, `package probe
fixed table Fixed {
	a int32
	b string(16)
}
table Root {
	a int32
	b string(16)
	c []int32
	d *string
}
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

	names, err := os.ReadFile(filepath.Join("..", "..", "tablenames", "java.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range retired {
		if strings.Contains(string(names), word) {
			t.Errorf("internal/tablenames/java.go retains retired tolerant name %q", word)
		}
	}
}
