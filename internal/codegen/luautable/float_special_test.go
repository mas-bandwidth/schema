package luautable_test

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
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
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestLuauPlainFloatSpecialValues pins docs/SPEC-TABLES.md §3.3, "A PLAIN
// FLOAT'S SPECIAL VALUES TAKE ONE RULE", for the plain float32 and float64
// fields of a variable table, under the finite rule of 2026-10-27 (Glenn:
// "tables go finite-only too, same rule as packets"): -0 rides (elision
// compares BIT PATTERNS), +0 elides, a subnormal rides as its bits, and a NaN
// (quiet, signalling, with a payload) or an infinity is refused by the save
// and the measure and by every read, tolerant and strict, as non_finite_float
// naming Var.a.
//
// The expected bytes and verdicts come from schema-go's own engine over the
// same unit (internal/tablewire and internal/tabletext), never typed by hand:
// the Go writer's bytes, or its refusal, are what the Luau save must answer,
// and the Go reader's landed cells and report, or its refusal, are what the
// Luau load must answer. A wire no conforming writer emits is the Go writer's
// bytes for a finite twin with the float bytes replaced by the pattern.
const floatSpecialSchema = `package fs

table Var
{
    a float32
    b float64
}
`

// floatProbe loads the wire through the tolerant load and the strict read, and
// saves and measures a value built from the case's two patterns.
const floatProbe = `local inputs = require("./inputs")
local load = require("./schema_load")
local save = require("./schema_save")
local R = require("./schema_refusal")
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
local function bits32(x: number): number
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
local report: any = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local got = load.loadTolerant("Var", inputs.wire, report)
if got == nil then
	print("tol=refused reason=" .. tostring(report.reason))
else
	print(string.format("tol=%d,%d,%d,%d,%d,%d,%d", report.malformed and 1 or 0, report.refused, report.unknown,
		report.kindMismatch, report.clamped, report.widened, report.duplicate))
	print(string.format("bits=%08x,%08x,%08x", bits32(got.A), hi64(got.B), lo64(got.B)))
end
local d = St.lookup("Var")
local v = d.new()
v.A = buffer.readf32(inputs.a32, 0)
v.B = buffer.readf64(inputs.b64, 0)
local out, why = save.save(d, v)
local measure, mwhy = save.measure(d, v)
if out == nil then
	print("save=refused " .. tostring(why and why.name) .. " " .. tostring(why and why.field) ..
		" measure=" .. tostring(measure) .. " " .. tostring(mwhy and mwhy.name))
else
	print("save=" .. hex(out) .. " measure=" .. tostring(measure))
end
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
	{"min_subnormal", 0x00000001, 0x0000000000000001},
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

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// The finite twin whose bytes a non-finite wire is cut from: neither pattern
// occurs anywhere else in the file.
const (
	twin32 = 0x3FC00000         // 1.5
	twin64 = 0x4004000000000000 // 2.5
)

// patchPattern replaces the one occurrence of old's little-endian bytes in
// wire with new's, failing when old is not there exactly once.
func patchPattern(t *testing.T, wire, old, new []byte) []byte {
	t.Helper()
	if n := bytes.Count(wire, old); n != 1 {
		t.Fatalf("the twin's pattern %x occurs %d times in %x, want once", old, n, wire)
	}
	return bytes.Replace(wire, old, new, 1)
}

func TestLuauPlainFloatSpecialValues(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	g := generateFiles(t, map[string]string{"Var.schema": floatSpecialSchema})

	// the same source through schema-go's engine: the unit, its instance model
	// and the wire walks the emitted code mirrors (§3)
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
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(probe.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	encode := func(b32 uint32, b64 uint64) ([]byte, error) {
		inst := model.New(st)
		goFloatField(t, inst, "a").Cell.F = float64(math.Float32frombits(b32))
		goFloatField(t, inst, "b").Cell.F = math.Float64frombits(b64)
		return tablewire.Encode(model, inst)
	}

	refusals := 0
	for _, c := range floatCases {
		a32 := binary.LittleEndian.AppendUint32(nil, c.b32)
		b64 := binary.LittleEndian.AppendUint64(nil, c.b64)
		var want []string
		goWire, encErr := encode(c.b32, c.b64)
		wire := goWire
		if encErr != nil {
			// the Go writer refuses: the Luau save and measure must too, and the
			// read is handed the finite twin's bytes with these patterns in
			var nf *tablewire.NonFiniteRefusal
			if !errors.As(encErr, &nf) {
				t.Fatalf("%s: the Go writer refused with %v, want a NonFiniteRefusal", c.name, encErr)
			}
			twin, err := encode(twin32, twin64)
			if err != nil {
				t.Fatalf("%s: the Go writer refused the finite twin: %v", c.name, err)
			}
			wire = patchPattern(t, twin, binary.LittleEndian.AppendUint32(nil, twin32), a32)
			wire = patchPattern(t, wire, binary.LittleEndian.AppendUint64(nil, twin64), b64)
			decoded := model.New(st)
			var rep tabletext.Report
			_, derr := tablewire.Decode(model, decoded, wire, &rep)
			var rf *tablewire.NonFiniteRefusal
			if !errors.As(derr, &rf) {
				t.Fatalf("%s: the Go reader answered %v over the patched wire, want a NonFiniteRefusal", c.name, derr)
			}
			want = []string{
				"tol=refused reason=" + rep.Reason,
				"save=refused non_finite_float " + nf.Field + " measure=nil non_finite_float",
				"strict=non_finite_float field=" + rf.Field,
			}
			refusals++
		} else {
			decoded := model.New(st)
			var rep tabletext.Report
			ok, derr := tablewire.Decode(model, decoded, wire, &rep)
			if derr != nil || !ok {
				t.Fatalf("%s: the Go reader refused the writer's own bytes: ok %v err %v report %+v", c.name, ok, derr, rep)
			}
			a := math.Float32bits(float32(goFloatField(t, decoded, "a").Cell.F))
			b := math.Float64bits(goFloatField(t, decoded, "b").Cell.F)
			if a != c.b32 || b != c.b64 {
				t.Fatalf("%s: the Go reader landed 0x%08X/0x%016X, want 0x%08X/0x%016X", c.name, a, b, c.b32, c.b64)
			}
			want = []string{
				fmt.Sprintf("tol=%d,%d,%d,%d,%d,%d,%d", boolInt(rep.Malformed), boolInt(rep.Refused), rep.Unknown, rep.KindMismatch, rep.Clamped, rep.Widened, rep.Duplicate),
				fmt.Sprintf("bits=%08x,%08x,%08x", a, uint32(b>>32), uint32(b)),
				"save=" + hex.EncodeToString(wire) + " measure=" + strconv.Itoa(len(wire)),
				"strict=ok field=-",
			}
		}

		out := string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"wire": wire, "a32": a32, "b64": b64}))
		got := strings.Split(strings.TrimRight(out, "\n"), "\n")
		if len(got) != len(want) {
			t.Fatalf("%s: the probe wrote %d lines, want %d:\n%s\nwant:\n%s", c.name, len(got), len(want), out, strings.Join(want, "\n"))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s: line %d is %q, want %q", c.name, i+1, got[i], want[i])
			}
		}
	}
	if refusals != 5 {
		t.Fatalf("%d cases were refused by the Go engine, want the 5 non-finite ones", refusals)
	}
}
