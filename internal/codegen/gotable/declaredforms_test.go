// A table that declares `forms = "..."` (docs/SPEC-TABLES.md §2.12) gets only
// the Go surfaces of the forms it names; a form no table in the unit carries
// emits no surface file at all. The default — a table with no `forms`
// declaration — is every form, today's behaviour.
package gotable

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
)

func keysOf(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for k := range files {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func joinedSource(files map[string][]byte) string {
	var b strings.Builder
	for _, k := range keysOf(files) {
		b.Write(files[k])
		b.WriteByte('\n')
	}
	return b.String()
}

func TestGoTableEmitsOnlyItsDeclaredForms(t *testing.T) {
	t.Parallel()
	defaultSchema := `package probe
table Pong { x int32 }
fixed table Fox { x int32 }
`
	def := generate(t, defaultSchema)
	if !hasFormFile(def, "Cook.go") || !hasFormFile(def, "TableJson.go") || !hasFormFile(def, "Block.go") {
		t.Fatalf("an undeclared unit emits every form — got %v", keysOf(def))
	}

	// Single-table unit with a map field (synthesizing PongMEntry) declaring only wire.
	decl := generate(t, "package probe\ntable Pong | forms = \"wire\"\n{\n    x int32\n    m map[string(8)]int32\n}\n")
	for _, name := range keysOf(decl) {
		if strings.HasSuffix(name, "Block.go") ||
			strings.HasSuffix(name, "Cook.go") ||
			strings.HasSuffix(name, "TableJson.go") {
			t.Errorf("%s emitted although Pong declares only wire", name)
		}
	}
	if !hasFormFile(decl, "Table.go") {
		t.Fatalf("the wire form is missing — got %v", keysOf(decl))
	}
	wireOnlySrc := joinedSource(decl)
	for _, forbidden := range []string{
		"type PongCook", "func PongOpen", "func PongAt",
		"func PongCookFrom", "func PongCookMeasure",
		"func PongFromJson", "func PongToJson",
		"type PongBlock", "func PongBlockOpen",
	} {
		if strings.Contains(wireOnlySrc, forbidden) {
			t.Errorf("wire-only unit leaked omitted form member %q", forbidden)
		}
	}

	// Multi-table unit: Pong and FoxWire declare only wire; Ping and FoxAll are undeclared (all forms).
	mixed := generate(t, `package probe
table Pong | forms = "wire"
{
    x int32
}
table Ping
{
    x int32
}
fixed table FoxWire | forms = "wire"
{
    x int32
}
fixed table FoxAll
{
    x int32
}
`)
	mixedSrc := joinedSource(mixed)
	for _, want := range []string{
		"type PingCook struct", "func PingOpen(", "func PingAt(",
		"func PingCookFrom(", "func PingCookMeasure(",
		"func PingFromJson(", "func PingToJson(",
		"type FoxAllBlock struct", "func FoxAllBlockOpen(",
		"type FoxAllCook struct", "func FoxAllOpen(",
		"func FoxAllFromJson(", "func FoxAllToJson(",
	} {
		if !strings.Contains(mixedSrc, want) {
			t.Errorf("multi-table unit missing expected member %q for undeclared table", want)
		}
	}
	for _, forbidden := range []string{
		"type PongCook", "func PongOpen(", "func PongAt(",
		"func PongCookFrom(", "func PongCookMeasure(",
		"func PongFromJson(", "func PongToJson(",
		"type PongBlock", "func PongBlockOpen(",
		"type FoxWireBlock", "func FoxWireBlockOpen(",
		"type FoxWireCook", "func FoxWireOpen(", "func FoxWireAt(",
		"func FoxWireCookFrom(", "func FoxWireCookMeasure(",
		"func FoxWireFromJson(", "func FoxWireToJson(",
	} {
		if strings.Contains(mixedSrc, forbidden) {
			t.Errorf("multi-table unit leaked omitted form member %q for wire-only table", forbidden)
		}
	}
}

func hasFormFile(files map[string][]byte, suffix string) bool {
	for name := range files {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func compileDeclaredFormUnit(t *testing.T, schema, testSource string) {
	t.Helper()
	u := unitFrom(t, schema)
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	files["go.mod"] = []byte("module probe\n\ngo 1.26\n")
	files["forms_test.go"] = []byte(testSource)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated runtime: %v\n%s", err, out)
	}
}

// TestDeclaredFormUnitsCompile proves a unit whose tables declare forms still
// compiles: only the declared forms' surfaces are emitted, and the wire table
// file and view neither reference an omitted form's symbols nor drop a core
// type.
func TestDeclaredFormUnitsCompile(t *testing.T) {
	t.Parallel()
	const probe = `package probe
import "testing"
func TestNoop(t *testing.T) {}
`
	for _, dec := range []string{
		"wire",
		"wire,json",
		"wire,cook",
		"wire,block",
		"wire,json,cook",
		"wire,json,block",
		"wire,cook,block",
		"wire,json,cook,block",
	} {
		dec := dec
		t.Run("variable/"+dec, func(t *testing.T) {
			t.Parallel()
			compileDeclaredFormUnit(t, `package probe
table Pong | forms = "`+dec+`"
{
    x int32
    m map[string(8)]int32
}
`, probe)
		})
		t.Run("fixed/"+dec, func(t *testing.T) {
			t.Parallel()
			compileDeclaredFormUnit(t, `package probe
fixed table Fox | forms = "`+dec+`"
{
    x int32
}
`, probe)
		})
	}
	t.Run("mixed", func(t *testing.T) {
		t.Parallel()
		compileDeclaredFormUnit(t, `package probe
table Pong | forms = "wire"
{
    x int32
    m map[string(8)]int32
}
table Ping
{
    x int32
}
fixed table FoxWire | forms = "wire"
{
    x int32
}
fixed table FoxAll
{
    x int32
}
`, probe)
	})
}

// TestSurfaceIntersectsDeclaredForms pins how a --surface request composes with
// a table's `forms` declaration (docs/SPEC-TABLES.md §2.12). Ping is a fixed
// table so the block form exists for it at all (§19 blocks are fixed-only). A table carries
// json, cook or block only when its declaration allows it AND the surface set
// asks for it; wire is always carried. The default (full) surface set refuses
// nothing; an explicit list naming a form a declaring table does not carry is
// refused by name, naming the table, the surface and the table's forms value.
func TestSurfaceIntersectsDeclaredForms(t *testing.T) {
	t.Parallel()
	const schema = `package probe
table Pong | forms = "wire,json"
{
    x int32
}
fixed table Ping
{
    x int32
}
`
	type carries struct{ json, cook, block bool }
	marks := func(name string) (json, cook, block string) {
		return "func " + name + "FromJson(", "func " + name + "CookFrom(", "type " + name + "Block"
	}
	for _, tc := range []struct {
		name    string
		surface string
		pong    carries
		ping    carries
		refuse  []string // substrings the refusal must name; nil means no refusal
	}{
		{name: "default", surface: "", pong: carries{json: true}, ping: carries{json: true, cook: true, block: true}},
		{name: "wire,json", surface: "wire,json", pong: carries{json: true}, ping: carries{json: true}},
		{name: "wire,cook", surface: "wire,cook", refuse: []string{"Pong", "cook", `"wire,json"`, "§2.12"}},
		{name: "wire", surface: "wire"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, err := ParseSurfaces(tc.surface)
			if err != nil {
				t.Fatal(err)
			}
			out, err := GenerateLineageSurface(unitFrom(t, schema), nil, s)
			if tc.refuse != nil {
				if err == nil {
					t.Fatalf("--surface %s was not refused although Pong declares forms = \"wire,json\"", tc.surface)
				}
				for _, want := range tc.refuse {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("refusal %q does not name %q", err, want)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("--surface %q refused: %v", tc.surface, err)
			}
			src := joinedSource(out)
			if !strings.Contains(src, "func PongSave(") || !strings.Contains(src, "func PingSave(") {
				t.Fatalf("the wire form is missing for --surface %q", tc.surface)
			}
			for _, row := range []struct {
				table string
				want  carries
			}{{"Pong", tc.pong}, {"Ping", tc.ping}} {
				j, c, b := marks(row.table)
				for _, m := range []struct {
					mark string
					want bool
				}{{j, row.want.json}, {c, row.want.cook}, {b, row.want.block}} {
					if got := strings.Contains(src, m.mark); got != m.want {
						t.Errorf("--surface %q: %s present=%v, want %v", tc.surface, m.mark, got, m.want)
					}
				}
			}
		})
	}
}
