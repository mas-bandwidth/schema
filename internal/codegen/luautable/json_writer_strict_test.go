package luautable_test

// The Luau text writer is STRICT about the values it is handed
// (docs/SPEC-TABLES.md §16.3): a value the strict text reader would refuse —
// an integer off its declared range or width, a non-finite float, a string
// that is not well-formed UTF-8 or holds U+0000, a string past its bound, a
// count past its bound — must be refused rather than written as a text that
// reads back differently. The expected verdict and name are computed in this
// test from schema-go's own text engine (internal/tabletext), never typed by
// hand: the offending text is read by the engine and its report must name a
// refusal, or (for a value no JSON text can spell, a non-finite float) the
// engine's writer must refuse the instance.
//
// Every clean value still writes the engine's bytes.

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const strictWriterSchema = `package probe

table Leaf
{
    note string(8)
}

table Root
{
    i int32 | min = -5, max = 5
    u uint8
    b bits(3)
    f float32 | min = 0, max = 1, resolution = 0.5
    g float32
    s string(8)
    w wstring(8)
    y bytes(4)
    xs [..3]int32
    fs [2]int32
}
`

// strictWriterUnit compiles the one unit, emits the Luau surfaces into a
// fresh directory, and returns the same checked unit the Go engine binds.
func strictWriterUnit(t *testing.T) (*ir.Unit, *generatedUnit) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Root.schema")
	if err := os.WriteFile(path, []byte(strictWriterSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the unit does not load: %v", err)
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range tables {
		files[name] = data
	}
	g := &generatedUnit{dir: t.TempDir(), files: files, loaded: true}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(g.dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return u, g
}

// strictGoName is the name the engine's report gives a refusal, so the test
// records the Go answer rather than inventing one.
func strictGoName(r tabletext.Report) string {
	if r.Reason != "" {
		return r.Reason
	}
	switch {
	case r.Malformed:
		return "malformed"
	case r.Clamped > 0:
		return "clamped"
	case r.KindMismatch > 0:
		return "kind_mismatch"
	case r.Unknown > 0:
		return "unknown"
	case r.Duplicate > 0:
		return "duplicate"
	case r.Widened > 0:
		return "widened"
	}
	return ""
}

func strictFindField(inst *tabletext.Instance, name string) *tabletext.Field {
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return &inst.Fields[i]
		}
	}
	return nil
}

// strictGoTextVerdict reads the offending text with schema-go's engine and
// returns the refusal name when the strict read would refuse it.
func strictGoTextVerdict(t *testing.T, m *tabletext.Model, text []byte) (string, bool) {
	t.Helper()
	inst := m.New(m.Lookup("Root"))
	var report tabletext.Report
	ok := m.Read(inst, text, &report)
	if !ok || !report.Silent() {
		name := strictGoName(report)
		if name == "" {
			t.Fatalf("the Go engine refused %q but named no event: %+v", text, report)
		}
		return name, true
	}
	return "", false
}

// strictGoWriteVerdict is the oracle for a value no JSON text can spell: build
// it in the engine's own instance and let the engine's writer answer.
func strictGoWriteVerdict(t *testing.T, m *tabletext.Model, mutate func(*tabletext.Instance)) string {
	t.Helper()
	inst := m.New(m.Lookup("Root"))
	_, err := m.Write(inst)
	if err != nil {
		t.Fatalf("the engine refused its own fresh instance: %v", err)
	}
	mutate(inst)
	if _, err := m.Write(inst); err != nil {
		return err.Error()
	}
	return ""
}

// strictRefusalProbe builds the descriptor's value in Luau storage, applies
// one mutation, and prints whether ToJson refused it.
func strictRefusalProbe(g *generatedUnit, mutation string) string {
	return jsonModules(g) + `
local json = require("./json")
local St = require("./schema_tables")
local v = St.lookup("Root").new()
` + mutation + `
local text = json.ToJson("Root", v)
if text == nil then print("refused") else print("wrote") end
`
}

func strictCleanProbe(g *generatedUnit) string {
	return jsonModules(g) + `local json = require("./json")
local inputs = require("./inputs")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = json.FromJson("Root", inputs.text, report)
if v == nil then print("refused"); return end
local text = json.ToJson("Root", v)
if text == nil then print("writer-refused"); return end
local parts = table.create(buffer.len(text))
for i = 0, buffer.len(text) - 1 do
	parts[i + 1] = string.format("%02x", buffer.readu8(text, i))
end
print(table.concat(parts))
`
}

func strictHex(data []byte) string {
	var b strings.Builder
	for _, c := range data {
		fmt.Fprintf(&b, "%02x", c)
	}
	return b.String()
}

func TestLuauJsonWriterIsStrict(t *testing.T) {
	t.Parallel()
	u, g := strictWriterUnit(t)
	m := tabletext.NewModel(u)

	const cleanText = `{
  "i": 5,
  "u": 255,
  "b": 7,
  "f": 0.5,
  "g": 1.5,
  "s": "abc",
  "w": "hi",
  "y": "AQID",
  "xs": [
    1,
    2,
    3
  ],
  "fs": [
    4,
    5
  ]
}
`

	t.Run("clean-writes-the-engine-bytes", func(t *testing.T) {
		inst := m.New(m.Lookup("Root"))
		var report tabletext.Report
		if ok := m.Read(inst, []byte(cleanText), &report); !ok || !report.Silent() {
			t.Fatalf("the Go engine refused the clean row: ok=%v report=%+v", ok, report)
		}
		want, err := m.Write(inst)
		if err != nil {
			t.Fatalf("the Go engine refused to write the clean row: %v", err)
		}
		// §16.1: the canonical text ends with exactly one newline, which the
		// engine's writer leaves to the caller.
		want = append(want, '\n')
		if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(strictCleanProbe(g)), 0o644); err != nil {
			t.Fatal(err)
		}
		out := strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"text": []byte(cleanText)})))
		if out != strictHex(want) {
			t.Fatalf("Luau ToJson wrote\n%s\nwant the engine's\n%s", out, strictHex(want))
		}
	})

	t.Run("refused", func(t *testing.T) {
		rows := []struct {
			name    string
			text    string
			mutate  string
			goWrite func(*tabletext.Instance)
		}{
			{"int-over-max", `{"i": 9}`, "v.I = 9", nil},
			{"uint8-over-width", `{"u": 256}`, "v.U = 256", nil},
			{"bits-over-width", `{"b": 8}`, "v.B = 8", nil},
			{"float-over-range", `{"f": 2}`, "v.F = 2", nil},
			{
				"float-non-finite",
				`{"g": 1}`,
				"v.G = math.huge",
				func(inst *tabletext.Instance) { strictFindField(inst, "g").Cell.F = math.Inf(1) },
			},
			{
				"string-ill-formed-utf8",
				"{\"s\": \"\xff\"}",
				"v.S = buffer.fromstring(string.char(0xff)); v.SLength = 1",
				nil,
			},
			{
				"string-holds-zero",
				"{\"s\": \"a\\u0000b\"}",
				"v.S = buffer.fromstring(\"a\" .. string.char(0) .. \"b\"); v.SLength = 3",
				nil,
			},
			{
				"string-past-bound",
				`{"s": "123456789"}`,
				"v.S = buffer.fromstring(\"123456789\"); v.SLength = 9",
				nil,
			},
			{
				"wstring-holds-zero",
				`{"w": "\u0000"}`,
				"v.W = buffer.fromstring(string.char(0, 0)); v.WLength = 1",
				nil,
			},
			{
				"bytes-past-bound",
				`{"y": "AQIDBAU="}`,
				"v.Y = buffer.fromstring(\"12345\"); v.YLength = 5",
				nil,
			},
			{
				"count-past-bound",
				`{"xs": [1, 2, 3, 4]}`,
				"v.Xs = { 1, 2, 3, 4 }; v.XsCount = 4",
				nil,
			},
		}
		for _, row := range rows {
			t.Run(row.name, func(t *testing.T) {
				var name string
				var refused bool
				if row.goWrite != nil {
					name = strictGoWriteVerdict(t, m, row.goWrite)
					refused = name != ""
				} else {
					name, refused = strictGoTextVerdict(t, m, []byte(row.text))
				}
				if !refused {
					t.Fatalf("the Go engine accepted the row; it is not a strict-reader refusal")
				}
				if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(strictRefusalProbe(g, row.mutate)), 0o644); err != nil {
					t.Fatal(err)
				}
				out := strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", nil)))
				if out != "refused" {
					t.Fatalf("Luau ToJson %s the value; Go refuses it by name %q", out, name)
				}
			})
		}
	})
}
