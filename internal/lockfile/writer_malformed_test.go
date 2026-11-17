package lockfile_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
)

func TestLockWriterMalformedSnapshotRefusesBeforeLayoutWalk(t *testing.T) {
	t.Parallel()
	const src = `package lockdemo
 enum Key { Red, Blue }
 type Child { ratio float32 | min = 0, max = 1, resolution = 0.1 }
 fixed table Row { n int32
 child Child
 text string(8)
 items [2..4]int16
 keyed [Key]int32 }
 `
	dir, paths := fixture(t, src)
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, lockfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	field := func(s map[string]any, record, name string) map[string]any {
		for _, raw := range s["records"].([]any) {
			r := raw.(map[string]any)
			if r["name"] == record {
				for _, f := range r["fields"].([]any) {
					f := f.(map[string]any)
					if f["name"] == name {
						return f
					}
				}
			}
		}
		panic("fixture field missing")
	}
	for _, tc := range []struct {
		name, want string
		change     func(map[string]any)
	}{
		{"unknown-kind", "unknown kind", func(s map[string]any) { field(s, "Row", "n")["kind"] = "opaque" }},
		{"negative-width", "scalar width", func(s map[string]any) { field(s, "Row", "n")["width"] = json.Number("-8") }},
		{"negative-text", "extent", func(s map[string]any) { field(s, "Row", "text")["size"] = json.Number("-1") }},
		{"oversized-text", "extent", func(s map[string]any) { field(s, "Row", "text")["size"] = json.Number("9223372036854775807") }},
		{"negative-array", "bound", func(s map[string]any) { field(s, "Row", "items")["bound"] = json.Number("-1") }},
		{"missing-type", "missing type", func(s map[string]any) { field(s, "Row", "child")["type"] = "Missing" }},
		{"wrong-key-ref", "missing key enum", func(s map[string]any) { field(s, "Row", "keyed")["key"] = "Row" }},
		{"invalid-enum", "enum", func(s map[string]any) { s["enums"].([]any)[0].(map[string]any)["max"] = json.Number("-1") }},
		{"cyclic-closure", "cyclic", func(s map[string]any) { f := field(s, "Row", "child"); f["type"] = "Row" }},
		{"wrong-round", "compressed-float", func(s map[string]any) { field(s, "Child", "ratio")["round"] = "down" }},
		{"wrong-steps", "compressed-float", func(s map[string]any) { field(s, "Child", "ratio")["steps"] = json.Number("999") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			words := strings.Fields(string(data))
			var token string
			for _, w := range words {
				if strings.HasPrefix(w, "writer=") {
					token = w
					break
				}
			}
			raw, err := hex.DecodeString(strings.TrimPrefix(token, "writer="))
			if err != nil {
				t.Fatal(err)
			}
			var snap map[string]any
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.UseNumber()
			if err := dec.Decode(&snap); err != nil {
				t.Fatal(err)
			}
			tc.change(snap)
			changed, err := json.Marshal(snap)
			if err != nil {
				t.Fatal(err)
			}
			mutant := strings.Replace(string(data), token, "writer="+hex.EncodeToString(changed), 1)
			if _, err := lockfile.Parse("malformed.lock", []byte(mutant)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q refusal before a layout walk: %v", tc.want, err)
			}
		})
	}
}
