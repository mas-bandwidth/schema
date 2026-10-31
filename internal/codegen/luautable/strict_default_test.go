package luautable_test

import (
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
// reason, while the tolerant walk lives under its own L.loadTolerant name and
// keeps the recovered value with the counters moved. The wires and the
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

	clampedInst := model.New(st)
	setInt(t, clampedInst, "n", 100)
	clamped := encodeWire(t, model, clampedInst)

	widerInst := widerModel.New(widerSt)
	setInt(t, widerInst, "n", 1)
	kindMismatch := encodeWire(t, widerModel, widerInst)

	// A TRUNCATED WIRE keeps the id table readable and cuts the root body's
	// terminator: the framing opens, the walk stops, and the tolerant read
	// hands back the recovered (defaulted) root with malformed counted.
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
	type oracle struct {
		want     string
		counters string
		n        int64
	}
	oracles := map[string]oracle{}
	for name, wire := range wires {
		inst := model.New(st)
		var report tabletext.Report
		ok, err := tablewire.Decode(model, inst, wire, &report)
		if err != nil {
			t.Fatalf("%s: the Go reference refused the wire: %v", name, err)
		}
		oracles[name] = oracle{
			want:     referenceReason(report, ok),
			counters: fmt.Sprintf("%d,%d,%d,%d,%d,%s", report.Unknown, report.KindMismatch, report.Widened, report.Clamped, report.Duplicate, boolText(report.Malformed)),
			n:        fieldInt(t, inst, "n"),
		}
	}
	if got := oracles["clean"]; got.want != "" || got.counters != "0,0,0,0,0,false" || got.n != 3 {
		t.Fatalf("the Go reference does not read the clean wire silently: %+v", got)
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
	local tolerant = load.loadTolerant("Var", b, tolerantRep)
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

		// THE TOLERANT WALK IS THE CONTROL: it keeps the recovered value with
		// the counters the Go engine counted, under the explicit name only.
		if tolerantValue == "nil" {
			t.Fatalf("%s: L.loadTolerant handed back no recovered value", name)
		}
		if tolerantCounters != oracle.counters {
			t.Errorf("%s: L.loadTolerant counters are %s, the Go reference says %s", name, tolerantCounters, oracle.counters)
		}
		if tolerantValue != fmt.Sprint(oracle.n) {
			t.Errorf("%s: L.loadTolerant recovered n=%s, the Go reference says %d", name, tolerantValue, oracle.n)
		}

		// R.ReadStrict IS THE NAME §6.5 GIVES: L.load must answer the same one.
		if oracle.want == "" {
			if readValue == "nil" {
				t.Errorf("%s: R.ReadStrict refused a clean wire: %s", name, readReason)
			}
		} else if readValue != "nil" || readReason != oracle.want {
			t.Errorf("%s: R.ReadStrict answered value=%s reason=%s, the Go reference says %q", name, readValue, readReason, oracle.want)
		}

		if name == "clean" {
			if strictValue == "nil" || strictReason != "nil" {
				t.Fatalf("clean: the plain L.load refused a clean wire: value=%s reason=%s", strictValue, strictReason)
			}
			if strictValue != tolerantValue {
				t.Fatalf("clean: L.load gave %s and L.loadTolerant gave %s", strictValue, tolerantValue)
			}
			if strictCounters != oracle.counters {
				t.Errorf("clean: the plain L.load counters are %s, the Go reference says %s", strictCounters, oracle.counters)
			}
			continue
		}
		if strictValue != "nil" {
			t.Errorf("%s: the plain L.load handed back the recovered value %s; §6.5 says it refuses", name, strictValue)
		}
		if strictReason != oracle.want {
			t.Errorf("%s: the plain L.load named %s, the Go reference says %s", name, strictReason, oracle.want)
		}
		if strictReason != readReason {
			t.Errorf("%s: the plain L.load named %s, R.ReadStrict named %s", name, strictReason, readReason)
		}
		if strictCounters != tolerantCounters {
			t.Errorf("%s: the plain L.load counters are %s, not this read's tolerant counters %s", name, strictCounters, tolerantCounters)
		}
	}
}

// referenceReason is the §6.5 name the Go report owes under the strict read's
// order: malformed (or unframed damage) first, then clamped, then
// kind_mismatch. The wires in this test move one counter each.
func referenceReason(report tabletext.Report, ok bool) string {
	switch {
	case report.Malformed, !ok:
		return "malformed"
	case report.Clamped > 0:
		return "clamped"
	case report.KindMismatch > 0:
		return "kind_mismatch"
	}
	return ""
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
