// TestLuaPointerTargetTypeChecked holds docs/SPEC-TABLES.md §3.1's rule for a
// pointer's target type (docs/rulings.json pointer-target-type-checked) on the
// Lua leg: in the variable form, a node record whose type this reader DECLARES
// but which is not the type its pointer slot requires is damage, and the whole
// load refuses. A record whose type this reader cannot name at all — a newer
// writer's type — stays lawful: it is counted `unknown` once and every pointer
// naming it reads null.
//
// Every row of testdata/wire/pointer-target-type/MANIFEST.tsv runs through
// every read of the variable form: the walk Load wraps, the plain load, the
// strict read and the retaining load. The verdict alone is compared
// (docs/rulings.json refusal-verdict-only), and the unknown-type row also holds
// that its pointer reads null. LoadMeasure frames and does not judge, so it
// answers the engine's region measure (internal/tablewire.FileRegionMeasure).
package luatable

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

type luaPointerTargetRow struct {
	name, unit, root, file, verdict string
	unknown                         int
}

func luaPointerTargetRows(t *testing.T, manifest string) []luaPointerTargetRow {
	t.Helper()
	var rows []luaPointerTargetRow
	for i, line := range strings.Split(manifest, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		row := luaPointerTargetRow{name: f[0], unit: f[1], root: f[2], file: f[3], verdict: f[4], unknown: -1}
		if row.verdict != "read" && row.verdict != "refused" {
			t.Fatalf("MANIFEST.tsv:%d: verdict %q, want read or refused", i+1, row.verdict)
		}
		if f[5] != "-" {
			n, err := strconv.Atoi(f[5])
			if err != nil {
				t.Fatalf("MANIFEST.tsv:%d: unknown %q: %v", i+1, f[5], err)
			}
			row.unknown = n
		}
		rows = append(rows, row)
	}
	return rows
}

const luaPointerTargetProbe = `local inputs = require("inputs")
local T = require(inputs.module)
local load = require("load")
local refusal = require("refusal")
local retain = require("retain")
local root, wire = inputs.root, inputs.wire

local function fresh()
  return { damaged = false, refused = 0, unknown = 0, kindChanged = 0,
    outOfRange = 0, widened = 0, repeatedKey = 0, sharedBlob = 0,
    missingRequired = 0, wireDamaged = false, reason = "",
    retained = 0, retainLost = 0 }
end
local function ptr(v)
  if v == nil then return "nil" end
  return v.next == nil and "null" or "node"
end

local wr = fresh()
local wv = load.loadWalk(T, root, wire, wr)
local lr = fresh()
local lv = load.Load(T, root, wire, lr)
local sok, sv = pcall(refusal.ReadStrict, T, root, wire)
local rr = fresh()
local store = { list = {}, used = 0 }
local rv = retain.LoadRetain(T, root, wire, store, rr)
local m = load.LoadMeasure(T, root, wire)

print(table.concat({
  tostring(wv ~= nil), wr.unknown, ptr(wv),
  tostring(lv ~= nil), lr.unknown, ptr(lv),
  tostring(sok and sv ~= nil), ptr(sv),
  tostring(rv ~= nil), rr.unknown, ptr(rv),
  m == nil and "nil" or tostring(m),
}, "|"))
`

func TestLuaPointerTargetTypeChecked(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Lua 5.4 functional pointer-target-type probe")

	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	vectors := filepath.Join(repo, "testdata", "wire", "pointer-target-type")
	manifest, err := os.ReadFile(filepath.Join(vectors, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("vectors manifest: %v", err)
	}
	rows := luaPointerTargetRows(t, string(manifest))
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}
	u := luarun.LoadUnit(t, filepath.Join(repo, rows[0].unit))
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	for _, row := range rows {
		if row.unit != rows[0].unit {
			t.Fatalf("%s reads %s; this probe reads %s", row.name, row.unit, rows[0].unit)
		}
		st := u.Tables[row.root]
		if st == nil {
			t.Fatalf("%s: the unit declares no root %s", row.name, row.root)
		}
		wire, err := os.ReadFile(filepath.Join(vectors, row.file))
		if err != nil {
			t.Fatalf("%s: %v", row.name, err)
		}
		measure, whole, refused := tablewire.FileRegionMeasure(u, st, wire)
		wantMeasure := "nil"
		if whole && !refused {
			wantMeasure = strconv.FormatInt(measure, 10)
		}

		out, stderr, err := try(t, files, luaPointerTargetProbe, map[string][]byte{
			"module": []byte(ModuleName(u)),
			"root":   []byte(row.root),
			"wire":   wire,
		})
		if err != nil {
			t.Fatalf("%s: probe: %v: %s", row.name, err, stderr)
		}
		f := strings.Split(strings.TrimSpace(out), "|")
		if len(f) != 12 {
			t.Fatalf("%s: probe printed %q, want 12 fields", row.name, strings.TrimSpace(out))
		}
		type answer struct {
			entry   string
			read    bool
			unknown int
			ptr     string
		}
		answers := []answer{
			{"walk", f[0] == "true", atoi(t, row.name, "walk unknown", f[1]), f[2]},
			{"load", f[3] == "true", atoi(t, row.name, "load unknown", f[4]), f[5]},
			{"strict", f[6] == "true", -1, f[7]},
			{"retain", f[8] == "true", atoi(t, row.name, "retain unknown", f[9]), f[10]},
		}
		wantRead := row.verdict == "read"
		for _, a := range answers {
			if a.read != wantRead {
				t.Errorf("%s (%s): the read %s, the manifest says %s", row.name, a.entry, verdict(a.read), row.verdict)
				continue
			}
			if !wantRead {
				continue
			}
			if row.unknown >= 0 && a.unknown >= 0 && a.unknown != row.unknown {
				t.Errorf("%s (%s): unknown %d, want %d", row.name, a.entry, a.unknown, row.unknown)
			}
			if row.unknown > 0 && a.ptr != "null" {
				t.Errorf("%s (%s): the pointer reads %s, want null", row.name, a.entry, a.ptr)
			}
		}
		if got := f[11]; got != wantMeasure {
			t.Errorf("%s: LoadMeasure %s, the engine measures %s", row.name, got, wantMeasure)
		}
	}
}

func atoi(t *testing.T, name, what, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatalf("%s: %s %q: %v", name, what, s, err)
	}
	return n
}

func verdict(read bool) string {
	if read {
		return "accepted it"
	}
	return "refused it"
}
