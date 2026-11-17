package mootable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const zeroByteWriterSchema = `package probe

table Root
{
    text string(8)
    words []string
    names map[string(8)]int32
}
`

// TestMooJsonWriterRefusesZeroByte holds the JSON writer to row 8: text
// storage containing NUL is not text and must be refused before writing.
func TestMooJsonWriterRefusesZeroByte(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (it runs the generated text form)")

	schemaDir := t.TempDir()
	schemaPath := filepath.Join(schemaDir, "Probe.schema")
	if err := os.WriteFile(schemaPath, []byte(zeroByteWriterSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{schemaPath})
	if err != nil {
		t.Fatalf("load schema: %v", err)
	}
	m := tabletext.NewModel(u)
	clean := func(t *testing.T) *tabletext.Instance {
		t.Helper()
		v := m.New(m.Lookup("Root"))
		var report tabletext.Report
		if !m.Read(v, []byte(`{"text":"ab","words":["a","b"],"names":{"key":1}}`), &report) || !report.Silent() {
			t.Fatalf("clean fixture did not read: %+v", report)
		}
		return v
	}
	setZero := func(v *tabletext.Instance, name string) {
		for i := range v.Fields {
			f := &v.Fields[i]
			if f.Def.Name != name {
				continue
			}
			switch name {
			case "text":
				f.Cell.Str = []byte{'a', 0, 'b'}
			case "words":
				f.Elems[1].Tab.Fields[0].Cell.Str = []byte{'a', 0, 'b'}
			case "names":
				f.Entries[0].Tab.Fields[0].Cell.Str = []byte{'a', 0, 'b'}
			}
			return
		}
		t.Fatalf("fixture has no %s field", name)
	}

	// Each probe builds the value directly, independent of the text reader.
	run := func(t *testing.T, v *tabletext.Instance) string {
		t.Helper()
		lit := newLiteral(u)
		value := lit.instance(v)
		var main strings.Builder
		main.WriteString(probePrelude)
		main.WriteString(literalPrelude)
		for _, mod := range lit.modules() {
			fmt.Fprintf(&main, "MOD[%q] = require(\"./%s\")\n", mod, mod)
		}
		main.WriteString(lit.b.String())
		fmt.Fprintf(&main, `local M = MOD[%q]
local size = M.RootToJsonMeasure(%s)
	local out = buffer.create(4096)
	local written = M.RootToJson(%s, out)
	local text = if written >= 0 then string.sub(buffer.tostring(out), 1, written) else ""
	hexs(string.format("%%d,%%d,%%d,%%s", size, written, buffer.readu8(out, 0), text))
`, tableModule(u, "Root"), value, value)
		dir := generateUnitDir(t, u, nil)
		if err := os.WriteFile(filepath.Join(dir, "main.moo"), []byte(main.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return decodeHexLines(t, moorun.Run(t, dir, "main.moo", nil))[0]
	}

	for _, c := range []struct{ name, field string }{
		{"string field", "text"},
		{"text-list element", "words"},
		{"map key", "names"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			v := clean(t)
			setZero(v, c.field)
			if got := run(t, v); got != "-1,-1,0," {
				t.Fatalf("writer accepted zero byte as %q", got)
			}
		})
	}

	t.Run("clean control", func(t *testing.T) {
		t.Parallel()
		want := "{\n  \"text\": \"ab\",\n  \"words\": [\n    \"a\",\n    \"b\"\n  ],\n  \"names\": {\n    \"key\": 1\n  }\n}\n"
		got := run(t, clean(t))
		parts := strings.SplitN(got, ",", 4)
		if len(parts) != 4 || parts[0] != parts[1] || parts[2] != "123" || parts[3] != want {
			t.Fatalf("clean writer output %q, want positive matching measure/write and %q", got, want)
		}
	})
}
