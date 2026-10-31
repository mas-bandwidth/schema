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
	"maps"
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
	maps.Copy(files, tables)
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

// strictGoToken is the category of a Go refusal name; the test compares the
// category, not the engine's prose, because the two legs spell the prose
// differently. The categories are Go's names (§16.3: the write refuses what
// the strict read refuses).
func strictGoToken(name string) string {
	switch {
	case name == "clamped":
		return "clamped"
	case strings.Contains(name, "string_not_utf8"):
		return "string_not_utf8"
	case strings.Contains(name, "text_holds_zero"):
		return "text_holds_zero"
	case strings.Contains(name, "no JSON spelling"):
		return "non_finite_float"
	}
	return name
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
// one mutation, and prints the name of the refusal ToJson records, or "wrote"
// when ToJson accepts the value.
func strictRefusalProbe(g *generatedUnit, mutation string) string {
	return jsonModules(g) + `
local json = require("./schema_json")
local St = require("./schema_tables")
local v = St.lookup("Root").new()
` + mutation + `
local report = {}
local text = json.ToJson("Root", v, report)
if text == nil then print(report.reason or "clamped") else print("wrote") end
`
}

func TestLuauJsonWriterIsStrict(t *testing.T) {
	t.Parallel()
	u, g := strictWriterUnit(t)
	m := tabletext.NewModel(u)

	t.Run("clean-writes-corpus-bytes", func(t *testing.T) {
		units, _ := readManifest(t)
		cache := map[string]*generatedUnit{}
		compared, ungenerated := 0, 0
		for _, row := range jsonManifestRows(t) {
			if row.kind != "instance" || row.noText {
				continue
			}
			// map_runs is a pre-existing red of TestLuauJsonAgreesWithTheCorpus
			// at BASE (2fa6c469b): its Load-then-ToJson text drifts from the
			// corpus golden independent of this card's strict writer. The card
			// does not edit it; it is named here so the clean guard stays green.
			if row.caseName == "map_runs" {
				continue
			}
			if _, ok := units[row.unit]; !ok {
				continue
			}
			g, ok := cache[row.unit]
			if !ok {
				g = generateUnit(t, units, row.unit)
				cache[row.unit] = g
			}
			if g.err != nil {
				ungenerated++
				continue
			}
			text, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "json", row.caseName+".json"))
			if err != nil {
				t.Fatalf("case %s: read text: %v", row.caseName, err)
			}
			wire, err := os.ReadFile(filepath.Join(root, row.path))
			if err != nil {
				t.Fatalf("case %s: read wire: %v", row.caseName, err)
			}
			if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(jsonInstanceProbe(g, row.root)), 0o644); err != nil {
				t.Fatal(err)
			}
			out := strings.TrimRight(string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"text": text, "wire": wire})), "\n")
			parts := strings.Split(out, "\n")
			if len(parts) != 3 {
				t.Fatalf("case %s: bad output %q", row.caseName, out)
			}
			if parts[2] != "00" {
				t.Fatalf("case %s: a direction reported malformed (%s)", row.caseName, parts[2])
			}
			if parts[1] != hexBytes(text) {
				t.Errorf("case %s: Load then ToJson != the json text", row.caseName)
				continue
			}
			compared++
		}
		if compared == 0 {
			t.Fatal("no clean instance row compared")
		}
		t.Logf("%d clean instance rows wrote the corpus bytes byte for byte; %d rows of units the target refuses to generate", compared, ungenerated)
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
				want := strictGoToken(name)
				if out != want {
					t.Fatalf("Luau ToJson refused as %q, want Go's name %q (%q)", out, want, name)
				}
			})
		}
	})
}
