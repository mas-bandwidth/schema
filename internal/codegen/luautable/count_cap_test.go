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
	tolerant = function(b, rep) return load.loadTolerant("Root", b, rep) end,
	load = function(b, rep) return load.load("Root", b, rep) end,
	strict = function(b, rep) return R.ReadStrict(T, "Root", b, rep) end,
	retain = function(b, rep) return (Ret.LoadRetain("Root", b, rep)) end,
	retainTolerant = function(b, rep) return (Ret.LoadRetainTolerant("Root", b, rep)) end,
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
// load_refusal_reasons.tsv. The negative controls are the pin's good row,
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
		if len(f) != 6 || f[1] != "measure" || (!strings.Contains(f[0], "_cap3") && f[0] != "good") {
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
	if len(capRows) != 4 || inputs["good"] == nil {
		t.Fatalf("%d cap rows and good=%v, want 4 and the good row", len(capRows), inputs["good"] != nil)
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(src, []byte(countCapSchema), 0o644); err != nil {
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
