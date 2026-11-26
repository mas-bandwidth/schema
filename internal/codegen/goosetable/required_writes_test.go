package goosetable_test

// A `| required` FIELD NEVER ELIDES, AND A PRESENT OPTIONAL MAP RIDES EMPTY
// (docs/SPEC-TABLES.md §2.3, The `| required` marker; §2.8; tablewire's
// encodeField and encodeMap): the writer emits a required field even at its
// default, in every shape (a scalar, a string, a text list, a nested table, a
// counted array born at its Min, an unbounded array, a fixed array, a map, an
// enum-keyed array, a union at None), and an optional map that is present
// rides with its zero-entry body. The Goose save's bytes are held to the
// engine's two ways: the engine's own wire loaded and saved again (Root; the
// conformance rewrite edge_by_name is this case in the corpus), and a fresh
// value through its typed writer, where nothing rode and every field is
// absent (Typed: the shapes the typed value carries, value_bridge.go).

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const requiredWritesSchema = `package req

enum Color { red, green }

table Sub { v int32 }

union Pick
{
    a int32
    b string(8)
}

table Root
{
    n     int32 | required
    s     string(8) | required
    words []string | required
    sub   Sub | required
    few   [2..4]int32 | required
    list  []int32 | required
    trio  [3]int32 | required
    m     map[int32]int32 | required
    slots [Color]int32 | required
    pick  Pick | required
    opt   ?map[int32]int32
    plain map[int32]int32
}

table Typed
{
    n     int32 | required
    s     string(8) | required
    sub   Sub | required
    few   [2..4]int32 | required
    trio  [3]int32 | required
    slots [Color]int32 | required
    pick  Pick | required
}
`

// requiredWritesText is the engine's value: every required field at its
// default (the text form must name each one), the optional map present and
// empty, the plain map empty.
const requiredWritesText = `{"n":0,"s":"","words":[],"sub":{},"few":[0,0],"list":[],"trio":[0,0,0],"m":{},"slots":{},"pick":{},"opt":{},"plain":{}}`

const requiredWritesMain = `
fn main() {
    let wire = input("root.bin");
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var report = schema_rt::LoadReport { .. };
    let at = schema_rt::load_table(conformance_unit(), "Root", wire, vals, nodes, report);
    if at < 0 { print("rewrite refused"); } else {
        var out: u8[>..<] = [];
        let refusal = schema_rt::save_table(conformance_unit(), "Root", wire, vals, nodes, at, out);
        if refusal != 0 { print("rewrite refused ", refusal); } else { print("rewrite ", hex(out)); }
    }
    var v = req::NewTyped();
    var data: u8[>..] = [];
    var fvals: schema_rt::LoadVal[>..] = [];
    var fnodes: schema_rt::LoadNode[>..] = [];
    var fresh: u8[>..<] = [];
    let fat = req::TypedToLoaded(v, conformance_unit(), data, fvals);
    if fat < 0 { print("fresh refused"); } else {
        let refusal = schema_rt::save_table(conformance_unit(), "Typed", data, fvals, fnodes, fat, fresh);
        if refusal != 0 { print("fresh refused ", refusal); } else { print("fresh ", hex(fresh)); }
    }
}
`

func TestGooseRequiredFieldsNeverElide(t *testing.T) {
	t.Parallel()
	unit := probeUnit(t, requiredWritesSchema)
	model := tabletext.NewModel(unit)
	inst := model.New(model.Lookup("Root"))
	var rep tabletext.Report
	if !model.Read(inst, []byte(requiredWritesText), &rep) {
		t.Fatalf("the engine refused the value: %+v", rep)
	}
	wire, err := tablewire.Encode(model, inst)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := tablewire.Encode(model, model.New(model.Lookup("Typed")))
	if err != nil {
		t.Fatal(err)
	}
	files := probeFiles(t, unit, nil)
	files["root.bin"] = wire
	files["main.goose"] = []byte(probePrelude + requiredWritesMain)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := string(gooserun.Run(t, dir, "main.goose", nil))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		name, rest, _ := strings.Cut(line, " ")
		got[name] = strings.TrimSpace(rest)
	}
	want := map[string]string{
		"rewrite": hex.EncodeToString(wire),
		"fresh":   hex.EncodeToString(fresh),
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: goose wrote %q, the engine wrote %q\n%s", k, got[k], w, out)
		}
	}
}
