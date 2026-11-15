package luautable_test

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A COMPRESSED FLOAT'S -0 LANDS AS +0 ON THE VARIABLE READ
// (docs/SPEC-TABLES.md §3.3, A COMPRESSED FLOAT'S SPECIAL VALUES): the
// variable-form strict load lands a ranged float32 field's -0 as +0, as the
// fixed form does, while a plain float32 keeps its sign. The wire is a lawful
// save of a finite twin whose two float fields are replaced by the -0 bits;
// schema-go's own engine over that wire is the expected answer.
const loadRangedFloatZeroSchema = `package lz

table Var
{
    r float32 | min = -10, max = 10, resolution = 0.001
    p float32
}
`

const loadRangedFloatZeroProbe = `local inputs = require("./inputs")
local T = require("./VarTable")
local R = require("./schema_refusal")
local s = buffer.create(4)
local function bits(x: number): number
	buffer.writef32(s, 0, x)
	return buffer.readu32(s, 0)
end
local report: any = {}
local v = R.ReadStrict(T, "Var", inputs.wire, report)
if v == nil then
	print("strict=refused " .. tostring(report.reason))
else
	print(string.format("bits=%08x,%08x", bits(v.R), bits(v.P)))
end
`

func TestLuauLoadRangedFloatZeroUnsigned(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")

	g := generateFiles(t, map[string]string{"Var.schema": loadRangedFloatZeroSchema})

	srcDir := t.TempDir()
	schemaPath := filepath.Join(srcDir, "Var.schema")
	if err := os.WriteFile(schemaPath, []byte(loadRangedFloatZeroSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{schemaPath})
	if err != nil {
		t.Fatalf("the ranged-float schema does not load: %v", err)
	}
	model := tabletext.NewModel(u)
	st := model.Lookup("Var")
	if st == nil {
		t.Fatal("the Go reference unit has no Var table")
	}

	// a lawful save of a finite twin, then both float fields set to -0 bits
	inst := model.New(st)
	goFloatField(t, inst, "r").Cell.F = 1.5
	goFloatField(t, inst, "p").Cell.F = 2.5
	wire, err := tablewire.Encode(model, inst)
	if err != nil {
		t.Fatalf("the Go writer refused the finite twin: %v", err)
	}
	wire = patchPattern(t, wire, le32(math.Float32bits(1.5)), le32(0x80000000))
	wire = patchPattern(t, wire, le32(math.Float32bits(2.5)), le32(0x80000000))

	// schema-go's engine over the same wire is the expected answer
	back := model.New(st)
	var report tabletext.Report
	ok, derr := tablewire.Decode(model, back, wire, &report)
	if derr != nil || !ok {
		t.Fatalf("the Go reader refused the patched wire: ok %v err %v report %+v", ok, derr, report)
	}
	wantR := math.Float32bits(float32(goFloatField(t, back, "r").Cell.F))
	wantP := math.Float32bits(float32(goFloatField(t, back, "p").Cell.F))
	if wantR != 0 {
		t.Fatalf("the Go engine landed the ranged float 0x%08X, want its -0 made +0", wantR)
	}
	if wantP != 0x80000000 {
		t.Fatalf("the Go engine landed the plain float 0x%08X, want its -0 sign kept", wantP)
	}

	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(loadRangedFloatZeroProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"wire": wire})))
	want := fmt.Sprintf("bits=%08x,%08x", wantR, wantP)
	if out != want {
		t.Fatalf("the variable strict load answered %q, want %q (ranged +0, plain -0)", out, want)
	}
}
