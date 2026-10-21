package goosetable

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
)

// TestGooseRegistryNoFragmentLeavesTheRuntimeAsWritten holds the registry's
// no-op: while runtime/registry holds no fragment, schema_tables.goose is
// emitted byte for byte as it is written (docs/PORT-GOOSE.md, "The registry").
func TestGooseRegistryNoFragmentLeavesTheRuntimeAsWritten(t *testing.T) {
	t.Parallel()
	raw, err := runtimeFS.ReadFile("runtime/" + RuntimeModule + ".goose")
	if err != nil {
		t.Fatal(err)
	}
	frags := registryFragments()
	got := Runtime()[RuntimeModule+".goose"]
	if len(frags) == 0 && !bytes.Equal(got, raw) {
		t.Fatalf("no fragment, and schema_tables.goose is emitted changed (%d bytes, written %d)", len(got), len(raw))
	}
	if len(frags) > 0 && !bytes.HasPrefix(got, raw) {
		t.Fatal("the assembled registry does not follow schema_tables.goose as written")
	}
	if _, ok := Runtime()["registry"]; ok {
		t.Fatal("the registry directory is emitted as a runtime module")
	}
}

// TestGooseRegistryAssemblesByRank holds the order: rank, then fragment
// name, then line, whichever card's fragment declares the registry.
func TestGooseRegistryAssemblesByRank(t *testing.T) {
	t.Parallel()
	frags := map[string][]byte{
		"b_keyed.goosereg":  []byte("// the keyed array\nadd Kind 20 Keyed\nadd kind_rows 10 Row { shape: ShapeArr, array: ArrayKeyed } // narrower first\n"),
		"a_scalar.goosereg": []byte("declare enum Kind\ndeclare array kind_rows Row\n\nadd kind_rows 50 Row { shape: ShapeArr, array: 0 }\nadd Kind 10 Scalar\nadd kind_rows 10 Row { shape: ShapeInt, array: 0 },\n"),
	}
	got, err := assembleRegistry([]byte("namespace schema_rt;\n"), frags)
	if err != nil {
		t.Fatal(err)
	}
	want := `namespace schema_rt;

// THE REGISTRY (docs/PORT-GOOSE.md, THE RUNTIME LAYOUT): assembled by the
// schema compiler from registry/*.goosereg — a_scalar.goosereg, b_keyed.goosereg.

enum Kind {
    Scalar, // a_scalar.goosereg
    Keyed, // b_keyed.goosereg
}

let kind_rows: const Row[3] = [
    Row { shape: ShapeInt, array: 0 }, // a_scalar.goosereg
    Row { shape: ShapeArr, array: ArrayKeyed }, // narrower first (b_keyed.goosereg)
    Row { shape: ShapeArr, array: 0 }, // a_scalar.goosereg
];
`
	if string(got) != want {
		t.Fatalf("assembled:\n%s\nwant:\n%s", got, want)
	}
	again, err := assembleRegistry([]byte("namespace schema_rt;\n"), frags)
	if err != nil || !bytes.Equal(again, got) {
		t.Fatal("assembly is not deterministic")
	}
}

// TestGooseRegistryRefusesByName holds strict write: every malformed
// fragment is refused, naming the file, the line and the defect.
func TestGooseRegistryRefusesByName(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, frag, want string }{
		{"unknown directive", "remove Kind 1 X\n", `f.goosereg:1: unknown directive "remove"`},
		{"bad declare", "declare table Kind\n", "f.goosereg:1: want `declare enum"},
		{"declared twice", "declare enum Kind\ndeclare enum Kind\nadd Kind 1 A\n", "f.goosereg:2: registry Kind is declared twice (first at f.goosereg:1)"},
		{"undeclared", "add Kind 1 A\n", "f.goosereg:1: add to Kind, which no fragment declares"},
		{"no entry", "declare array rows Row\n", "f.goosereg:1: registry rows is declared and has no entry"},
		{"rank", "declare enum Kind\nadd Kind first A\n", `f.goosereg:2: rank "first" is not an integer`},
		{"empty entry", "declare enum Kind\nadd Kind 1\n", "f.goosereg:2: add to Kind has no entry"},
		{"variant twice", "declare enum Kind\nadd Kind 1 A\nadd Kind 2 A\n", "f.goosereg:3: enum Kind variant A is added twice (first at f.goosereg:2)"},
		{"variant ident", "declare enum Kind\nadd Kind 1 A B\n", `f.goosereg:2: enum Kind variant "A B" is not an identifier`},
		{"registry ident", "declare enum 9Kind\n", `f.goosereg:1: registry name "9Kind" is not an identifier`},
	} {
		_, err := assembleRegistry(nil, map[string][]byte{"f.goosereg": []byte(c.frag)})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want an error containing %q", c.name, err, c.want)
		}
	}
}

// TestGooseRegistryCompiles holds the assembled text to the pinned Goose
// compiler: two cards' fragments, an enum and a first-match table over the
// runtime's own shapes, compile with the rest of the runtime and read back
// in rank order.
func TestGooseRegistryCompiles(t *testing.T) {
	t.Parallel()
	rt := Runtime()
	tables, err := assembleRegistry(rt[RuntimeModule+".goose"], map[string][]byte{
		"probe_a.goosereg": []byte("declare enum ProbeKind\ndeclare array probe_rows u8\nadd ProbeKind 1 ProbeScalar\nadd probe_rows 20 ShapeArr\n"),
		"probe_b.goosereg": []byte("add ProbeKind 2 ProbeKeyed\nadd probe_rows 10 ShapeInt // first\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	rt[RuntimeModule+".goose"] = tables
	maps.Copy(rt, goose.Runtime())
	dir := t.TempDir()
	var imports strings.Builder
	for name, data := range rt {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
		imports.WriteString("import ." + strings.TrimSuffix(name, ".goose") + ";\n")
	}
	main := imports.String() + `
fn main() {
    let k = schema_rt::ProbeKind.ProbeKeyed;
    print("rows ", schema_rt::probe_rows.len, " ", schema_rt::probe_rows[0], " ", schema_rt::probe_rows[1]);
}
`
	out := strings.TrimSpace(string(gooserun.Run(t, dir, "main.goose", map[string][]byte{"main.goose": []byte(main)})))
	if want := "rows 2 2 13"; out != want {
		t.Fatalf("main.goose printed %q, want %q (ShapeInt = 2 ranks before ShapeArr = 13)", out, want)
	}
}
