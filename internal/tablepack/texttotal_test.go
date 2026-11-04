package tablepack_test

import (
	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A text list's `| total` bounds the bytes its elements hold together
// (docs/SPEC-TABLES.md §2.13): a directory tree whose element files pass the
// total refuses length_over_bound, as does an element past its own string(5),
// and a tree inside both bounds packs and unpacks.
func TestTextTotalDirectoryInputRefuses(t *testing.T) {
	src := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(src, []byte("package probe\ntable Root { names [..4]string(5) | total=5 }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c := compiler.New()
	u, err := c.Load([]string{src})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		files map[string]string
		ok    bool
	}{
		{map[string]string{"0.json": `"abcde"`, "1.json": `"x"`}, false},
		{map[string]string{"0.json": `"ab"`, "1.json": `"toolong"`}, false},
		{map[string]string{"0.json": `"ab"`, "1.json": `"xyz"`}, true},
	} {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "names"), 0700); err != nil {
			t.Fatal(err)
		}
		for name, value := range tc.files {
			if err := os.WriteFile(filepath.Join(dir, "names", name), []byte(value), 0600); err != nil {
				t.Fatal(err)
			}
		}
		wire, _, r, err := tablepack.Pack(tabletext.NewModel(u), "Root", dir)
		if !tc.ok {
			if err == nil || len(wire) != 0 || !strings.Contains(err.Error(), "length_over_bound") {
				t.Fatalf("%v: want a length_over_bound refusal: %x %+v %v", tc.files, wire, r, err)
			}
			continue
		}
		if err != nil || len(wire) == 0 || !r.Silent() {
			t.Fatalf("%v: pack: %x %+v %v", tc.files, wire, r, err)
		}
		if _, err := c.Unpack(u, "Root", wire, t.TempDir()); err != nil {
			t.Fatal(err)
		}
	}
}
