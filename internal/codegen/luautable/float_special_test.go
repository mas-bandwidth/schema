package luautable_test

import (
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestLuauPlainFloatSpecialValues pins docs/SPEC-TABLES.md §3.3, "A PLAIN
// FLOAT'S SPECIAL VALUES TAKE ONE RULE", for the plain float32 and float64
// fields of a variable table: -0 rides (elision compares BIT PATTERNS), +0
// elides, and a NaN (quiet and signalling) or an infinity rides bit for bit.
// The tolerant read names it non_finite without moving a counter and the
// strict read refuses it after kind_mismatch.
//
// The expected bytes and verdicts come from schema-go's own engine over the
// same unit (internal/tablewire and internal/tabletext), never typed by hand:
// the Go writer's bytes are the wire the Luau save must write, and the Go
// reader's landed cells and report are the bits and counts the Luau load must
// answer.
const floatSpecialSchema = `package fs

table Var
{
    a float32
    b float64
}
`

// floatProbe loads the wire through the tolerant load and the strict read,
// prints the report's counters, the loaded bits and the save's bytes.
const floatProbe = `local inputs = require("./inputs")
local load = require("./load")
local save = require("./save")
local R = require("./refusal")
local St = require("./schema_tables")
local s = buffer.create(8)
local function hex(b: buffer?): string
	if b == nil then return "nil" end
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
-- the float32 a float64 cell rides as, by the same bit surgery save.luau uses
local function narrow32(x: number): number
	buffer.writef64(s, 0, x)
	local hi = buffer.readu32(s, 4)
	local lo = buffer.readu32(s, 0)
	if bit32.band(hi, 0x7FF00000) == 0x7FF00000 and (bit32.band(hi, 0xFFFFF) ~= 0 or lo ~= 0) then
		local payload = bit32.band(bit32.bor(bit32.lshift(bit32.band(hi, 0xFFFFF), 3), bit32.rshift(lo, 29)), 0x7FFFFF)
		if payload == 0 then payload = 0x400000 end
		return bit32.bor(bit32.band(hi, 0x80000000), 0x7F800000, payload)
	end
	buffer.writef32(s, 0, x)
	return buffer.readu32(s, 0)
end
local function hi64(x: number): number
	buffer.writef64(s, 0, x)
	return buffer.readu32(s, 4)
end
local function lo64(x: number): number
	buffer.writef64(s, 0, x)
	return buffer.readu32(s, 0)
end
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0, nonFinite = 0 }
local v = load.load("Var", inputs.wire, report)
if v == nil then print("LOADNIL"); return end
print(string.format("tol=%d,%d,%d,%d,%d,%d,%d,%d",
	report.malformed and 1 or 0, report.refused, report.unknown, report.kindMismatch,
	report.clamped, report.widened, report.duplicate, report.nonFinite))
print(string.format("bits=%08x,%08x,%08x", narrow32(v.A), hi64(v.B), lo64(v.B)))
local d = St.lookup("Var")
local out = save.save(d, v)
local measure = save.measure(d, v)
print("save=" .. hex(out) .. " measure=" .. tostring(measure))
local r2: any = {}
R.ReadStrict(nil, "Var", inputs.wire, r2)
print("strict=" .. (r2.reason or "ok") .. " field=" .. (r2.field or "-"))
`

type floatCase struct {
	name string
	b32  uint32
	b64  uint64
}

var floatCases = []floatCase{
	{"plus_zero", 0x00000000, 0x0000000000000000},
	{"minus_zero", 0x80000000, 0x8000000000000000},
	{"nan_quiet", 0x7FC00000, 0x7FF8000000000000},
	{"nan_signalling", 0x7F800001, 0x7FF0000000000001},
	{"nan_payload", 0xFFA5A5A5, 0x7FF00DEFACED0001},
	{"plus_inf", 0x7F800000, 0x7FF0000000000000},
	{"minus_inf", 0xFF800000, 0xFFF0000000000000},
}

// goFloatField finds a field of the Go reference's decoded instance by name.
func goFloatField(t *testing.T, inst *tabletext.Instance, name string) *tabletext.Field {
	t.Helper()
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return &inst.Fields[i]
		}
	}
	t.Fatalf("the Go reference instance declares no field %q", name)
	return nil
}

func plainSpecial(f *tabletext.Field) bool {
	return !f.Def.HasFloatRange && (math.IsNaN(f.Cell.F) || math.IsInf(f.Cell.F, 0))
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestLuauPlainFloatSpecialValues(t *testing.T) {
	t.Parallel()
	g := generateFiles(t, map[string]string{"Var.schema": floatSpecialSchema})

	// the same source through schema-go's engine: the unit, its instance model
	// and the wire/text walks the emitted code mirrors (§3, §16.5)
	srcDir := t.TempDir()
	schemaPath := filepath.Join(srcDir, "Var.schema")
	if err := os.WriteFile(schemaPath, []byte(floatSpecialSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{schemaPath})
	if err != nil {
		t.Fatalf("the Go reference unit does not load: %v", err)
	}
	model := tabletext.NewModel(u)
	st := model.Lookup("Var")
	if st == nil {
		t.Fatal("the Go reference unit has no Var table")
	}

	var mods []string
	for name := range g.files {
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	var probe strings.Builder
	probe.WriteString("local inputs = require(\"./inputs\")\n")
	for _, m := range mods {
		fmt.Fprintf(&probe, "require(\"./%s\")\n", m)
	}
	probe.WriteString(floatProbe)

	for _, c := range floatCases {
		// the Go writer's value and bytes
		inst := model.New(st)
		goFloatField(t, inst, "a").Cell.F = tablewire.WidenF32(c.b32)
		goFloatField(t, inst, "b").Cell.F = math.Float64frombits(c.b64)
		wantWire, err := tablewire.Encode(model, inst)
		if err != nil {
			t.Fatalf("%s: the Go writer refused: %v", c.name, err)
		}

		// the Go reader over the same bytes: the landed cells and the report
		decoded := model.New(st)
		var rep tabletext.Report
		ok, err := tablewire.Decode(model, decoded, wantWire, &rep)
		if err != nil || !ok {
			t.Fatalf("%s: the Go reader refused the writer's own bytes: ok %v err %v report %+v", c.name, ok, err, rep)
		}
		a := goFloatField(t, decoded, "a")
		b := goFloatField(t, decoded, "b")
		want32 := tablewire.NarrowF32(a.Cell.F)
		want64 := math.Float64bits(b.Cell.F)
		if want32 != c.b32 || want64 != c.b64 {
			t.Fatalf("%s: the Go reader landed 0x%08X/0x%016X, want 0x%08X/0x%016X", c.name, want32, want64, c.b32, c.b64)
		}
		nonFinite, field := 0, ""
		for i := range decoded.Fields {
			f := &decoded.Fields[i]
			if plainSpecial(f) {
				nonFinite++
				if field == "" {
					field = f.Def.Name
				}
			}
		}
		wantReason := "ok"
		if nonFinite > 0 {
			wantReason = "non_finite"
		}
		if field == "" {
			field = "-"
		}
		want := []string{
			fmt.Sprintf("tol=%d,%d,%d,%d,%d,%d,%d,%d", boolInt(rep.Malformed), boolInt(rep.Refused), rep.Unknown, rep.KindMismatch, rep.Clamped, rep.Widened, rep.Duplicate, nonFinite),
			fmt.Sprintf("bits=%08x,%08x,%08x", want32, uint32(want64>>32), uint32(want64)),
			"save=" + hex.EncodeToString(wantWire) + " measure=" + strconv.Itoa(len(wantWire)),
			"strict=" + wantReason + " field=" + field,
		}

		if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(probe.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		out := string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"wire": wantWire}))
		got := strings.Split(strings.TrimRight(out, "\n"), "\n")
		if len(got) != len(want) {
			t.Fatalf("%s: the probe wrote %d lines, want %d:\n%s", c.name, len(got), len(want), out)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s: line %d is %q, want %q", c.name, i+1, got[i], want[i])
			}
		}
	}
}
