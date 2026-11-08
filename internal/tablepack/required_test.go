package tablepack_test

import (
	"github.com/mas-bandwidth/schema/v2/compiler"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackRequiredRefusalPublishesNothing(t *testing.T) {
	dir := t.TempDir()
	schema := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(schema, []byte("package probe\ntable Root { value int32 | required }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c := compiler.New()
	u, err := c.Load([]string{schema})
	if err != nil {
		t.Fatal(err)
	}
	tree := t.TempDir()
	if err := os.WriteFile(filepath.Join(tree, "Root.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	wire, _, report, err := c.Pack(u, "Root", tree)
	if err == nil || !strings.Contains(err.Error(), "missing_required: value") || len(wire) != 0 || report.Silent() || !report.Refused || report.Reason != "missing_required: value" {
		t.Fatalf("pack: wire=%x report=%+v err=%v", wire, report, err)
	}
	if err := os.WriteFile(filepath.Join(tree, "Root.json"), []byte(`{"value":0}`), 0600); err != nil {
		t.Fatal(err)
	}
	wire, _, report, err = c.Pack(u, "Root", tree)
	if err != nil || len(wire) == 0 || !report.Silent() {
		t.Fatalf("present zero: wire=%x report=%+v err=%v", wire, report, err)
	}
}
