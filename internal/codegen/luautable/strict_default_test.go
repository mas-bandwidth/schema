package luautable_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestLuauDefaultReadIsStrict holds docs/SPEC-TABLES.md §6.5's rule that THE
// PLAIN NAME IS THE STRICT READ on the Luau leg: L.load refuses what
// R.ReadStrict refuses and hands back no recovered value, naming the same
// reason. Strict only (docs/rulings.json strict-only-read): what the Go engine
// refuses, every Luau read refuses, L.loadWalk included, compared by verdict
// alone (refusal-verdict-only); what the engine reads, every Luau read keeps
// with the engine's value and counters. The wires and the
// counters are schema-go's own engine (internal/tablewire and
// internal/tabletext) over the same unit, never typed by hand.
func TestLuauDefaultReadIsStrict(t *testing.T) {
	t.Parallel()

	const schemaSrc = `package probe

table Var
{
    n int32 = 1 | min = 0, max = 10
}
`
	g := generateFiles(t, map[string]string{"Var.schema": schemaSrc})

	model, st := goReferenceUnit(t, "Var.schema", schemaSrc)
	// A WIDER WRITER of the same field name: the wire says int64 where the
	// reader declares int32, the one kind mismatch the reference counts.
	widerModel, widerSt := goReferenceUnit(t, "Var.schema", `package probe

table Var
{
    n int64
}
`)

	cleanInst := model.New(st)
	setInt(t, cleanInst, "n", 3)
	clean := encodeWire(t, model, cleanInst)

	// A VALUE PAST ITS RANGE: the engine's writer refuses n = 100 (invalid_value),
	// so the off-contract wire comes from a RANGE-FREE TWIN with the same name
	// and kind. The twin's wire for an in-range value is the ranged schema's own
	// bytes, so the two wires differ only in the value.
	rangeFreeModel, rangeFreeSt := goReferenceUnit(t, "Var.schema", `package probe

table Var
{
    n int32 = 1
}
`)
	twinInst := rangeFreeModel.New(rangeFreeSt)
	setInt(t, twinInst, "n", 3)
	if twin := encodeWire(t, rangeFreeModel, twinInst); !bytes.Equal(twin, clean) {
		t.Fatalf("the range-free twin does not write the ranged schema's bytes:\ntwin   %x\nranged %x", twin, clean)
	}
	clampedInst := rangeFreeModel.New(rangeFreeSt)
	setInt(t, clampedInst, "n", 100)
	clamped := encodeWire(t, rangeFreeModel, clampedInst)

	widerInst := widerModel.New(widerSt)
	setInt(t, widerInst, "n", 1)
	kindMismatch := encodeWire(t, widerModel, widerInst)

	// A TRUNCATED WIRE keeps the id table readable and cuts the root body's
	// terminator: the framing opens, the walk stops, and the tolerant read
	// discards the whole value with malformed counted.
	empty := encodeWire(t, model, model.New(st))
	if len(empty) < 2 {
		t.Fatalf("the empty wire is too short to truncate: %d bytes", len(empty))
	}
	truncated := append([]byte(nil), empty[:len(empty)-1]...)

	wires := map[string][]byte{
		"clean":         clean,
		"clamped":       clamped,
		"kind_mismatch": kindMismatch,
		"truncated":     truncated,
	}

	// THE REFERENCE'S VERDICT for every wire, counted by schema-go's decoder.
	// Strict only (docs/rulings.json strict-only-read): a wire the engine
	// refuses is a verdict, compared by verdict alone (refusal-verdict-only).
	type oracle struct {
		refused  bool
		counters string
		n        int64
	}
	oracles := map[string]oracle{}
	for name, wire := range wires {
		inst := model.New(st)
		var report tabletext.Report
		ok, err := tablewire.Decode(model, inst, wire, &report)
		refused := !ok || err != nil || report.Refused
		o := oracle{refused: refused}
		if !refused {
			o.counters = fmt.Sprintf("%d,%d,%d,%d,%d,%s", report.Unknown, 0, report.Widened, 0, 0, boolText(report.Refused))
			o.n = fieldInt(t, inst, "n")
		}
		oracles[name] = o
	}
	if got := oracles["clean"]; got.refused || got.counters != "0,0,0,0,0,false" || got.n != 3 {
		t.Fatalf("the Go reference does not read the clean wire silently: %+v", got)
	}
	for _, name := range []string{"clamped", "truncated"} {
		if !oracles[name].refused {
			t.Fatalf("%s: the Go reference read the wire; strict-only refuses it", name)
		}
	}

	main := `local inputs = require("./inputs")
local load = require("./schema_load")
local R = require("./schema_refusal")
require("./VarTable")

local function fresh()
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
local function counters(rep)
	return string.format("%d,%d,%d,%d,%d,%s", rep.unknown, rep.kindMismatch, rep.widened, rep.clamped, rep.duplicate, tostring(rep.malformed))
end
local function n(v)
	if v == nil then return "nil" end
	return tostring(v.N)
end
for _, name in { "clean", "clamped", "kind_mismatch", "truncated" } do
	local b = inputs[name]
	local strictRep = fresh()
	local strict = load.load("Var", b, strictRep)
	local tolerantRep = fresh()
	local tolerant = load.loadWalk("Var", b, tolerantRep)
	local readRep = fresh()
	local read = R.ReadStrict(nil, "Var", b, readRep)
	print(name .. "|" .. n(strict) .. "|" .. tostring(strictRep.reason) .. "|" .. counters(strictRep) ..
		"|" .. n(tolerant) .. "|" .. counters(tolerantRep) ..
		"|" .. n(read) .. "|" .. tostring(readRep.reason))
end
`
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", wires)))
	lines := strings.Split(out, "\n")
	if len(lines) != len(wires) {
		t.Fatalf("the probe wrote %d lines, want %d:\n%s", len(lines), len(wires), out)
	}

	for _, line := range lines {
		fields := strings.Split(line, "|")
		if len(fields) != 8 {
			t.Fatalf("probe line is not name|strict|reason|counters|tolerant|counters|read|reason: %q", line)
		}
		name := fields[0]
		oracle, ok := oracles[name]
		if !ok {
			t.Fatalf("the probe named an unknown wire %q", name)
		}
		strictValue, strictReason, strictCounters := fields[1], fields[2], fields[3]
		tolerantValue, tolerantCounters := fields[4], fields[5]
		readValue, readReason := fields[6], fields[7]

		if oracle.refused {
			// EVERY LUAU READ REFUSES what the engine refuses (§6.5, no
			// tolerant read under any name): the plain load, the walk it is
			// built on, and R.ReadStrict, by verdict alone.
			if strictValue != "nil" {
				t.Errorf("%s: the plain L.load handed back %s; the Go reference refuses this wire", name, strictValue)
			}
			if tolerantValue != "nil" {
				t.Errorf("%s: L.loadWalk handed back %s; the Go reference refuses this wire", name, tolerantValue)
			}
			if readValue != "nil" {
				t.Errorf("%s: R.ReadStrict handed back %s; the Go reference refuses this wire", name, readValue)
			}
			// Damage discards the whole load with malformed counted (§4).
			if name == "truncated" && !strings.HasSuffix(tolerantCounters, ",true") {
				t.Errorf("%s: the damaged walk counts %s, and damage owes malformed", name, tolerantCounters)
			}
			// R.ReadStrict IS THE NAME §6.5 GIVES: L.load answers the same one,
			// with this read's own counters.
			if strictReason == "nil" || strictReason != readReason {
				t.Errorf("%s: the plain L.load named %s, R.ReadStrict named %s", name, strictReason, readReason)
			}
			if strictCounters != tolerantCounters {
				t.Errorf("%s: the plain L.load counters are %s, not this read's walk counters %s", name, strictCounters, tolerantCounters)
			}
			continue
		}

		// A wire the engine reads: every Luau read keeps the engine's value
		// and counters.
		if tolerantValue == "nil" {
			t.Fatalf("%s: L.loadWalk handed back no value; the Go reference reads this wire", name)
		}
		if tolerantCounters != oracle.counters {
			t.Errorf("%s: L.loadWalk counters are %s, the Go reference says %s", name, tolerantCounters, oracle.counters)
		}
		if tolerantValue != fmt.Sprint(oracle.n) {
			t.Errorf("%s: L.loadWalk recovered n=%s, the Go reference says %d", name, tolerantValue, oracle.n)
		}
		if readValue == "nil" {
			t.Errorf("%s: R.ReadStrict refused a wire the Go reference reads: %s", name, readReason)
		}
		if strictValue == "nil" || strictReason != "nil" {
			t.Fatalf("%s: the plain L.load refused a wire the Go reference reads: value=%s reason=%s", name, strictValue, strictReason)
		}
		if strictValue != tolerantValue {
			t.Fatalf("%s: L.load gave %s and L.loadWalk gave %s", name, strictValue, tolerantValue)
		}
		if strictCounters != oracle.counters {
			t.Errorf("%s: the plain L.load counters are %s, the Go reference says %s", name, strictCounters, oracle.counters)
		}
	}
}

func boolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// goReferenceUnit loads a schema source into the Go reference model.
func goReferenceUnit(t *testing.T, name, src string) (*tabletext.Model, *ir.Struct) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the Go reference unit does not load: %v", err)
	}
	model := tabletext.NewModel(u)
	st := model.Lookup("Var")
	if st == nil {
		t.Fatal("the Go reference unit has no Var table")
	}
	return model, st
}

func encodeWire(t *testing.T, model *tabletext.Model, inst *tabletext.Instance) []byte {
	t.Helper()
	wire, err := tablewire.Encode(model, inst)
	if err != nil {
		t.Fatalf("tablewire.Encode: %v", err)
	}
	return wire
}

func fieldInt(t *testing.T, inst *tabletext.Instance, name string) int64 {
	t.Helper()
	f, ok := inst.FieldByKey(name)
	if !ok {
		t.Fatalf("the instance has no field %s", name)
	}
	return f.Cell.I
}

func setInt(t *testing.T, inst *tabletext.Instance, name string, v int64) {
	t.Helper()
	f, ok := inst.FieldByKey(name)
	if !ok {
		t.Fatalf("the instance has no field %s", name)
	}
	f.Cell.I = v
	f.Cell.U = uint64(v)
}
