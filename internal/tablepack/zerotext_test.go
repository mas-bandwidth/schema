package tablepack_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func loadUnit(t *testing.T, src string) (*compiler.Compiler, *ir.Unit) {
	t.Helper()
	dir := t.TempDir()
	sf := filepath.Join(dir, "probe.schema")
	if err := os.WriteFile(sf, []byte("package probe\n"+src), 0o644); err != nil {
		t.Fatal(err)
	}
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{sf})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return c, u
}

// The engine's pack refuses, by name, a U+0000 in any narrow text it would
// place — a string(N), a *string, a map's string key, or a text-list element —
// matching the Go leg's writer and docs/SPEC-TABLES.md §3 / §4.2. It writes
// nothing (docs/SPEC-TABLES.md §17).
func TestEnginePackRefusesZeroByteInText(t *testing.T) {
	t.Parallel()
	packCase := func(name, schema, rootJSON string) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c, u := loadUnit(t, schema+"\n")
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "Root.json"), []byte(rootJSON), 0o644); err != nil {
				t.Fatal(err)
			}
			wire, _, _, err := c.Pack(u, "Root", dir)
			if err == nil {
				t.Fatalf("pack accepted %s", rootJSON)
			}
			if len(wire) != 0 {
				t.Fatalf("pack wrote bytes before refusing: %d", len(wire))
			}
			if !strings.Contains(err.Error(), "text_zero_byte") {
				t.Fatalf("the refusal does not name text_zero_byte: %v", err)
			}
		})
	}

	packCase("string field holds aNULb",
		"table Root { t string(8) }",
		`{"t":"a\u0000b"}`)
	packCase("string field holds lone NUL",
		"table Root { t string(8) }",
		`{"t":"\u0000"}`)
	packCase("pointer to string",
		"table Root { sp *string }",
		`{"sp":"a\u0000b"}`)
	packCase("map string key",
		"table Root { m map[string(16)]uint32 }",
		`{"m":{"a\u0000b":1}}`)
	packCase("text list element",
		"table Root { lst [..4]string(8) }",
		`{"lst":["a","b\u0000c"]}`)
	packCase("string in a union arm",
		"union U {\n    A string(8)\n    B uint32\n}\ntable Root {\n    u U\n}",
		`{"u":{"A":"a\u0000b"}}`)

	t.Run("bytes holding a zero byte packs and unpacks silent", func(t *testing.T) {
		t.Parallel()
		c, u := loadUnit(t, "table Root { b *bytes }\n")
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "Root.json"), []byte(`{"b":"AA=="}`), 0o644); err != nil {
			t.Fatal(err)
		}
		wire, _, report, err := c.Pack(u, "Root", dir)
		if err != nil {
			t.Fatalf("*bytes holding a zero byte was refused: %v", err)
		}
		if !report.Silent() {
			t.Fatalf("clean pack did not read silent: %+v", report)
		}
		out := t.TempDir()
		r, err := c.Unpack(u, "Root", wire, out)
		if err != nil {
			t.Fatalf("unpack failed: %v", err)
		}
		if !r.Silent() {
			t.Fatalf("clean unpack did not read silent: %+v", r)
		}
	})

	t.Run("clean text packs and unpacks silent", func(t *testing.T) {
		t.Parallel()
		c, u := loadUnit(t, "table Root { t string(8) }\n")
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "Root.json"), []byte(`{"t":"ab"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		wire, _, report, err := c.Pack(u, "Root", dir)
		if err != nil {
			t.Fatalf("clean text was refused: %v", err)
		}
		if !report.Silent() {
			t.Fatalf("clean pack did not read silent: %+v", report)
		}
		out := t.TempDir()
		r, err := c.Unpack(u, "Root", wire, out)
		if err != nil {
			t.Fatalf("unpack failed: %v", err)
		}
		if !r.Silent() {
			t.Fatalf("clean unpack did not read silent: %+v", r)
		}
	})
}
