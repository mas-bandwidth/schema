package compiler

import (
	"bytes"
	"strings"
	"testing"
)

// TestGoRuntimeOption pins the Go generator option `runtime`
// (docs/SPEC-TABLES.md §8.4): absent, empty and "standalone" are the same
// bytes, "shared" imports the shared package, and any other value is refused
// by name.
func TestGoRuntimeOption(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, "package probe\nfixed table A { n int32 }\n")
	c := New()
	base, err := c.Generate(u, "go", Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"", "standalone"} {
		got, err := c.Generate(u, "go", Options{"runtime": v})
		if err != nil {
			t.Fatalf("runtime=%q: %v", v, err)
		}
		if len(got) != len(base) {
			t.Fatalf("runtime=%q emits %d files, default %d", v, len(got), len(base))
		}
		for name, want := range base {
			if !bytes.Equal(got[name], want) {
				t.Fatalf("runtime=%q: %s differs from the default", v, name)
			}
		}
	}
	shared, err := c.Generate(u, "go", Options{"runtime": "shared"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(shared["ProbeTable.go"], []byte("schema/v2/tables/goruntime")) {
		t.Fatalf("runtime=shared does not import the shared runtime")
	}
	if _, err := c.Generate(u, "go", Options{"runtime": "bogus"}); err == nil || !strings.Contains(err.Error(), `"bogus"`) {
		t.Fatalf("an unknown runtime is refused by name: %v", err)
	}
}
