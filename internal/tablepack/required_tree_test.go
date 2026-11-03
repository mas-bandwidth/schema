package tablepack_test

import (
	"github.com/mas-bandwidth/schema/v2/compiler"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequiredTreePresence(t *testing.T) {
	schema := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(schema, []byte(`package probe
 enum Key { A, B }
 table Kid { x int32 | required }
 table Root { value int32 | required }
 table Parent { child Kid }
 table ListRoot { items [2]Kid }
 table KeyedRoot { items [Key]Kid }
 table Guarded { enabled bool
 if enabled { value int32 | required }
 }
 `), 0600); err != nil {
		t.Fatal(err)
	}
	c := compiler.New()
	u, err := c.Load([]string{schema})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, root, reason string
		files              map[string]string
	}{
		{"missing_tree", "Root", "missing_required: value", nil},
		{"present_tree", "Root", "", map[string]string{"value.json": "0"}},
		{"nested_file", "Parent", "missing_required: x", map[string]string{"child.json": "{}"}},
		{"nested_directory", "Parent", "missing_required: x", map[string]string{"child/.keep": ""}},
		{"array_element", "ListRoot", "missing_required: x", map[string]string{"items/0.json": "{}"}},
		{"keyed_element", "KeyedRoot", "missing_required: x", map[string]string{"items/A.json": "{}"}},
		{"guard_off", "Guarded", "", nil},
		{"guard_on", "Guarded", "missing_required: value", map[string]string{"enabled.json": "true"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, value := range tc.files {
				p := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(value), 0600); err != nil {
					t.Fatal(err)
				}
			}
			wire, _, r, err := c.Pack(u, tc.root, dir)
			if tc.reason == "" {
				if err != nil || len(wire) == 0 || !r.Silent() {
					t.Fatalf("accepted: wire=%x r=%+v err=%v", wire, r, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.reason) || len(wire) != 0 || r.MissingRequired != 1 || r.Malformed {
				t.Fatalf("refused: wire=%x r=%+v err=%v", wire, r, err)
			}
		})
	}
}
