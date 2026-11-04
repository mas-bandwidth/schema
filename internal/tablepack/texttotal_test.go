package tablepack_test

import (
	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"os"
	"path/filepath"
	"testing"
)

func TestTextTotalDirectoryInputClampsPrefix(t *testing.T) {
	src := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(src, []byte("package probe\ntable Root { names [..4]string(5) | total=5 }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c := compiler.New()
	u, err := c.Load([]string{src})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "names"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"0.json": `"abcde"`, "1.json": `"xyz"`, "2.json": `"toolong"`} {
		if err := os.WriteFile(filepath.Join(dir, "names", name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	wire, _, r, err := tablepack.Pack(tabletext.NewModel(u), "Root", dir)
	if err != nil || len(wire) == 0 || r.Clamped != 1 || r.Reason != "clamped: Root.names" || r.Refused {
		t.Fatalf("pack: %x %+v %v", wire, r, err)
	}
	out := t.TempDir()
	_, err = c.Unpack(u, "Root", wire, out)
	if err != nil {
		t.Fatal(err)
	}
}
