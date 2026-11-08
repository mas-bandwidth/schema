package luautable_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// countCapSchema is the measure unit of
// internal/codegen/goosetable/testdata/load_refusal_reasons.tsv.
const countCapSchema = "package probe\n table Child { values []int32 }\n table Root { values []int32\n entries map[int32]int32\n child Child\n next *Root }\n"

// countCapProbe reads every input through every builder read and prints one
// line per (input, path): whether the answer is nil, the reason, the refused
// count, the malformed flag and the sum of the evolution counters.
const countCapProbe = `local inputs = require("./inputs")
local T = require("./ProbeTable")
local load = require("./schema_load")
local R = require("./schema_refusal")
local Ret = require("./schema_retain")

local function report(): any
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0, retained = 0, retainLost = 0 }
end
local paths = {
	tolerant = function(b, rep) return load.loadWalk("Root", b, rep) end,
	load = function(b, rep) return load.load("Root", b, rep) end,
	strict = function(b, rep) return R.ReadStrict(T, "Root", b, rep) end,
	retain = function(b, rep) return (Ret.LoadRetain("Root", b, rep)) end,
	retainTolerant = function(b, rep) return (Ret.loadRetainWalk("Root", b, rep)) end,
}
local names = {}
for name in inputs do names[#names + 1] = name end
table.sort(names)
local order = { "tolerant", "load", "strict", "retain", "retainTolerant" }
for _, name in names do
	for _, path in order do
		local rep = report()
		local v = paths[path](inputs[name], rep)
		local moved = (rep.unknown or 0) + (rep.kindMismatch or 0) + (rep.clamped or 0) + (rep.widened or 0) + (rep.duplicate or 0) + (rep.missingRequired or 0) + (rep.blobShared or 0)
		print(string.format("%s.%s nil=%s reason=%s refused=%d malformed=%s moved=%d",
			name, path, tostring(v == nil), tostring(rep.reason), rep.refused or 0, tostring(rep.malformed == true), moved))
	end
end
`

var countCapPaths = []string{"tolerant", "load", "strict", "retain", "retainTolerant"}

// TestLuauCountCapRefusal holds every builder read to docs/SPEC-TABLES.md
// §2.9: a `[]` or map count above the int32 storage cap answers nil, refuses
// by the name count_over_extent_cap with refused set, leaves malformed unset
// and moves no counter. The inputs are the pinned cap rows of the shared
// load_refusal_reasons.tsv, values_widened_over_cap among them: its element
// kind widens, so a widened count before the cap would show here. The negative controls are the pin's good row,
// which reads, and each cap row with its count lowered to the cap itself
// (2^31-1), which is not this refusal.
func TestLuauCountCapRefusal(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter")
	t.Parallel()
	data, err := os.ReadFile("../goosetable/testdata/load_refusal_reasons.tsv")
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	var capRows []string
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 6 || f[1] != "measure" || (!strings.Contains(f[0], "_cap3") && f[0] != "values_widened_over_cap" && f[0] != "good") {
			continue
		}
		wire, err := hex.DecodeString(f[2])
		if err != nil {
			t.Fatal(err)
		}
		inputs[f[0]] = wire
		if f[0] == "good" {
			continue
		}
		if f[3] != "count_over_extent_cap" {
			t.Fatalf("%s: the pin's strict column is %s, want count_over_extent_cap", f[0], f[3])
		}
		capRows = append(capRows, f[0])
		// the at-cap twin: the same five-byte LEB128 count spelling 2^31-1
		at := bytes.Index(wire, []byte{0x80, 0x80, 0x80, 0x80})
		if at < 0 {
			t.Fatalf("%s: no five-byte count in the wire", f[0])
		}
		twin := bytes.Clone(wire)
		copy(twin[at:at+5], []byte{0xff, 0xff, 0xff, 0xff, 0x07})
		inputs[f[0]+"_atcap"] = twin
	}
	sort.Strings(capRows)
	if len(capRows) != 5 || inputs["good"] == nil {
		t.Fatalf("%d cap rows and good=%v, want 5 and the good row", len(capRows), inputs["good"] != nil)
	}

	got := runLuauCountCap(t, countCapSchema, inputs, nil)

	refusal := "nil=true reason=count_over_extent_cap refused=1 malformed=false moved=0"
	for _, row := range capRows {
		for _, path := range countCapPaths {
			key := row + "." + path
			if got[key] != refusal {
				t.Errorf("%s: %s, want %s", key, got[key], refusal)
			}
			twin := got[row+"_atcap."+path]
			if twin == "" || strings.Contains(twin, "count_over_extent_cap") || strings.Contains(twin, "refused=1") {
				t.Errorf("%s_atcap.%s: %s; a count AT the cap is not the storage refusal", row, path, twin)
			}
		}
	}
	for _, path := range countCapPaths {
		key := "good." + path
		if want := "nil=false reason=nil refused=0 malformed=false moved=0"; got[key] != want {
			t.Errorf("%s: %s, want %s", key, got[key], want)
		}
	}
	if t.Failed() {
		var all []string
		for k, v := range got {
			all = append(all, fmt.Sprintf("%s %s", k, v))
		}
		sort.Strings(all)
		t.Logf("probe:\n%s", strings.Join(all, "\n"))
	}
}

// runLuauCountCap generates the probe unit from schema, lets edit change the
// emitted files, runs countCapProbe over inputs and answers its lines keyed by
// "<input>.<path>".
func runLuauCountCap(t *testing.T, schema string, inputs map[string][]byte, edit func(map[string][]byte)) map[string]string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(src, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{src})
	if err != nil {
		t.Fatalf("the probe unit does not load: %v", err)
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
	if edit != nil {
		edit(files)
	}
	files["main.luau"] = []byte(countCapProbe)
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(luaurun.Run(t, dir, "main.luau", inputs)), "\n"), "\n") {
		key, answer, ok := strings.Cut(line, " ")
		if !ok {
			t.Fatalf("the probe printed %q", line)
		}
		got[key] = answer
	}
	return got
}

// countCapFloatSchema is the float unit of the shared pin table: a variable
// root whose wire can carry a NaN and an over-cap count in either read order.
const countCapFloatSchema = "package probe\n table Root { f float32\n values []int32 }\n"

// readOrderFaults runs the float rows of the shared pin table and answers the
// "<input>.<path>" reads that differ from the two-refusal rule
// (docs/SPEC-TABLES.md §2.9): the first refusal in read order names the load,
// so nan_then_over_cap refuses as non_finite_float and over_cap_then_nan as
// count_over_extent_cap, each with malformed unset, no counter moved and a nil
// answer; float_good reads.
func readOrderFaults(t *testing.T, edit func(map[string][]byte)) map[string]string {
	t.Helper()
	data, err := os.ReadFile("../goosetable/testdata/load_refusal_reasons.tsv")
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	strict := map[string]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 6 || f[1] != "float" {
			continue
		}
		wire, err := hex.DecodeString(f[2])
		if err != nil {
			t.Fatal(err)
		}
		inputs[f[0]] = wire
		strict[f[0]] = f[3]
	}
	if len(inputs) != 3 {
		t.Fatalf("%d float rows, want float_good, nan_then_over_cap and over_cap_then_nan", len(inputs))
	}
	got := runLuauCountCap(t, countCapFloatSchema, inputs, edit)
	bad := map[string]string{}
	for name, reason := range strict {
		for _, path := range countCapPaths {
			key := name + "." + path
			line := got[key]
			ok := false
			switch reason {
			case "ok":
				ok = line == "nil=false reason=nil refused=0 malformed=false moved=0"
			case "count_over_extent_cap":
				ok = line == "nil=true reason=count_over_extent_cap refused=1 malformed=false moved=0"
			case "non_finite_float":
				ok = strings.HasPrefix(line, "nil=true reason=non_finite_float") && strings.HasSuffix(line, " malformed=false moved=0")
			}
			if !ok {
				bad[key] = line
			}
		}
	}
	return bad
}

// TestLuauCountCapReadOrder holds every builder read to the two-refusal rule
// on the float rows of the shared pin table.
func TestLuauCountCapReadOrder(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter")
	t.Parallel()
	for key, line := range readOrderFaults(t, nil) {
		t.Errorf("%s: %s", key, line)
	}
}

// TestLuauCountCapReadOrderNegativeControl drops the NaN the count refusal
// kept, so the count wins whatever came first, and requires exactly
// nan_then_over_cap to go red on every builder read.
func TestLuauCountCapReadOrderNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter")
	t.Parallel()
	bad := readOrderFaults(t, func(files map[string][]byte) {
		const from, to = "\t_nonFinite = _countNonFinite\n", "\t_nonFinite = nil\n"
		src := files["schema_load.luau"]
		if bytes.Count(src, []byte(from)) != 1 {
			t.Fatalf("the emitted schema_load.luau no longer holds %q once", from)
		}
		files["schema_load.luau"] = bytes.ReplaceAll(src, []byte(from), []byte(to))
	})
	for _, path := range countCapPaths {
		if _, ok := bad["nan_then_over_cap."+path]; !ok {
			t.Errorf("nan_then_over_cap.%s: the probe did not see the read-order control", path)
		}
	}
	if len(bad) != len(countCapPaths) {
		t.Errorf("the control turned %d reads red, want the %d of nan_then_over_cap: %v", len(bad), len(countCapPaths), bad)
	}
}
