package jstable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMapsAndUnionArmsOnTheTableWireJavaScript is the card's carry step
// (docs/SPEC-TABLES.md §2.3, §2.4, §2.6, §2.8, §3, §3.1): the JavaScript
// variable form carries maps, union arms of any field type, optional ?T,
// enum-keyed arrays [E]T, optional arrays and 128-bit scalars on the table wire,
// byte for byte against the Go oracle (internal/codegen/gotable,
// internal/tablewire), for the tables/maps, tables/arms, tables/messages,
// tables/scalars and examples-wide corpora.
//
// RED FIRST: the refusal layer still names every one of these constructs, so a
// single-table unit made of them is refused before a byte is round-tripped. The
// byte-for-byte corpus body lands with the codec.
func TestMapsAndUnionArmsOnTheTableWireJavaScript(t *testing.T) {
	t.Parallel()

	field := func(name string, typ ir.FieldType) *ir.Field { return &ir.Field{Name: name, Type: typ} }

	for _, tc := range []struct {
		name  string
		field *ir.Field
	}{
		{"map", &ir.Field{Name: "m", Type: ir.FieldType{Kind: ir.TMap}, MapEntry: &ir.Struct{Name: "m_entry"}}},
		{"optional", field("opt", ir.FieldType{Kind: ir.TInt, Signed: true, Width: 32, Optional: true})},
		{"enumkeyed", &ir.Field{Name: "ships", Type: ir.FieldType{Kind: ir.TInt, Signed: true, Width: 8}, KeyEnum: "ShipType", KeyEnumRef: &ir.Enum{Name: "ShipType", Max: 4}}},
		{"unionarm", &ir.Field{Name: "arm", Type: ir.FieldType{Kind: ir.TNamed, Ref: &ir.Union{Name: "Pick"}}}},
		{"uint128", field("wide", ir.FieldType{Kind: ir.TInt, Signed: false, Width: 128})},
	} {
		st := &ir.Struct{Name: "Probe", IsTable: true, Fields: []*ir.Field{tc.field}}
		if got := tableVariableRefusals(st); len(got) != 0 {
			t.Errorf("%s: the JavaScript variable wire still refuses a construct this card carries: %q", tc.name, got)
			continue
		}
		if _, err := generateVariableFiles(unitWith(st), nil); err != nil && strings.Contains(err.Error(), "Probe."+tc.field.Name) {
			t.Errorf("%s: generateVariableFiles still refuses Probe.%s: %v", tc.name, tc.field.Name, err)
		}
	}
}

// TestUint128VariableWireJavaScript is the wide-scalar step of this card on the
// table wire (docs/SPEC-TABLES.md §3): a uint128 field rides as a sixteen-byte
// little-endian value and round-trips through the generated JavaScript Save and
// Load, byte for byte. A uint128 is a BigInt in this language (internal/tablewire
// retains the raw integer), so the probe holds a value past 2^64.
func TestUint128VariableWireJavaScript(t *testing.T) {
	t.Parallel()
	node := jsNode(t)

	srcDir := t.TempDir()
	schemaPath := filepath.Join(srcDir, "Wide.schema")
	if err := os.WriteFile(schemaPath, []byte("package probe\n\ntable Wide {\n  n uint128\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unit := loadUnit(t, schemaPath)
	out, err := generateVariableFiles(unit, nil)
	if err != nil {
		t.Fatalf("generateVariableFiles refused a uint128 field: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("generateVariableFiles emitted %d modules, want one", len(out))
	}
	dir := t.TempDir()
	var module string
	for name, content := range out {
		module = filepath.Base(name)
		if err := os.WriteFile(filepath.Join(dir, module), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	script := filepath.Join(dir, "probe.mjs")
	probe := `
import { Wide, WideSave, WideLoad, TableVariableReport } from "./` + module + `";
const w = new Wide();
w.n = (1n << 100n) + 5n;
const saved = WideSave(w);
if (saved === null) { console.log("save refused"); process.exit(1); }
const back = WideLoad(saved, new TableVariableReport());
if (back === null || back.n !== (1n << 100n) + 5n) { console.log("round-trip", back && String(back.n)); process.exit(1); }
const again = WideSave(back);
if (again.length !== saved.length) { console.log("len", again.length, saved.length); process.exit(1); }
for (let i = 0; i < saved.length; i++) { if (again[i] !== saved[i]) { console.log("byte", i, again[i], saved[i]); process.exit(1); } }
console.log("uint128 round-trip", saved.length, "bytes, byte for byte");
`
	if err := os.WriteFile(script, []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, script)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the generated JavaScript did not round-trip a uint128: %v\n%s", err, out)
	}
}
