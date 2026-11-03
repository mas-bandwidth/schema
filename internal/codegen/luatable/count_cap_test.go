package luatable

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// countCapSchema is the measure unit of
// internal/codegen/goosetable/testdata/load_refusal_reasons.tsv.
const countCapSchema = "package probe\n table Child { values []int32 }\n table Root { values []int32\n entries map[int32]int32\n child Child\n next *Root }\n"

// countCapProbe reads every input through each builder read: the tolerant
// load, the plain load, ReadStrict and the retaining load. Each line is
// "<input> <path> <answer> <reason> <refused> <malformed> <counters> <kept>",
// where answer is "value" or "nil", counters is the sum of the report's
// counters and kept is the number of records the retaining store holds.
const countCapProbe = `local T = require(%q)
local load = require("load")
local refusal = require("refusal")
local retain = require("retain")
local inputs = require("inputs")
local function report()
  return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0,
    clamped = 0, widened = 0, duplicate = 0, blobShared = 0,
    missingRequired = 0, wireDamaged = false, reason = "",
    retained = 0, retainLost = 0 }
end
local function counters(r)
  return r.unknown + r.kindMismatch + r.clamped + r.widened + r.duplicate
    + (r.blobShared or 0) + (r.missingRequired or 0)
end
local function line(name, path, v, r, kept)
  local reason = r.reason
  if reason == nil or reason == "" then reason = "-" end
  print(string.format("%%s %%s %%s %%s %%d %%s %%d %%d", name, path,
    v == nil and "nil" or "value", reason, r.refused, tostring(r.malformed),
    counters(r), kept))
end
for _, name in ipairs({ %s }) do
  local wire = inputs[name]
  local r = report()
  line(name, "tolerant", load.LoadTolerant(T, "Root", wire, r), r, 0)
  r = report()
  line(name, "load", load.Load(T, "Root", wire, r), r, 0)
  local ok, got = pcall(refusal.ReadStrict, T, "Root", wire)
  if ok then
    print(name .. " strict value")
  else
    print(name .. " strict " .. tostring(got))
  end
  r = report()
  local store = { list = {}, used = 0 }
  local v = retain.LoadRetain(T, "Root", wire, store, r)
  line(name, "retain", v, r, #store.list)
end
`

// countCapRow is one input of the count-cap test, with the strict answer the
// shared pin table holds for it.
type countCapRow struct {
	name   string
	wire   []byte
	strict string
}

// countCapRows reads the cap rows and the good row of the shared pin table,
// and adds an AT-CAP twin of each cap row: the same wire with its count
// spelled 2^31-1, which is past what the body covers and so is damage, never
// the count-cap refusal.
func countCapRows(t *testing.T) []countCapRow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "goosetable", "testdata", "load_refusal_reasons.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []countCapRow
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 6 || f[1] != "measure" || (!strings.Contains(f[0], "_cap3") && f[0] != "good") {
			continue
		}
		wire, err := hex.DecodeString(f[2])
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, countCapRow{name: f[0], wire: wire, strict: f[3]})
		if f[0] == "good" {
			continue
		}
		at := bytes.Clone(wire)
		copy(at[5:10], []byte{0xff, 0xff, 0xff, 0xff, 0x07})
		rows = append(rows, countCapRow{name: f[0] + "_at_cap", wire: at})
	}
	if len(rows) != 9 {
		t.Fatalf("%d rows, want the four cap rows, their at-cap twins and the good row", len(rows))
	}
	for _, row := range rows {
		if strings.HasSuffix(row.name, "_cap31") || strings.HasSuffix(row.name, "_cap32") {
			if row.strict != "count_over_extent_cap" {
				t.Fatalf("%s: the pin table's strict answer is %s", row.name, row.strict)
			}
		}
	}
	return rows
}

// runCountCap generates the probe unit, lets edit change the emitted files,
// and answers the probe's lines keyed by "<input> <path>".
func runCountCap(t *testing.T, rows []countCapRow, edit func(map[string][]byte)) map[string]string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "probe.schema")
	if err := os.WriteFile(src, []byte(countCapSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	inputs := map[string][]byte{}
	var names []string
	for _, row := range rows {
		inputs[row.name] = row.wire
		names = append(names, fmt.Sprintf("%q", row.name))
	}
	probe := fmt.Sprintf(countCapProbe, ModuleName(u), strings.Join(names, ", "))
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(luarun.Run(t, dir, "main.lua", inputs))), "\n") {
		f := strings.SplitN(line, " ", 3)
		if len(f) != 3 {
			t.Fatalf("probe line %q", line)
		}
		got[f[0]+" "+f[1]] = f[2]
	}
	return got
}

// countCapFaults answers what differs from docs/SPEC-TABLES.md §2.9 in the
// probe's lines, one per failing read, each "<input> <path>: ...": a cap row must answer nil on every builder read with the
// reason count_over_extent_cap, refused set, malformed unset, no counter moved
// and nothing retained; the good row reads clean; an at-cap twin is damage and
// never the refusal.
func countCapFaults(rows []countCapRow, got map[string]string) []string {
	var bad []string
	for _, row := range rows {
		for _, path := range []string{"tolerant", "load", "retain"} {
			line := got[row.name+" "+path]
			var want string
			switch {
			case row.name == "good":
				want = "value - 0 false 0 0"
			case strings.HasSuffix(row.name, "_at_cap"):
				if strings.Contains(line, "count_over_extent_cap") || !strings.HasPrefix(line, "nil ") || !strings.Contains(line, " true ") {
					bad = append(bad, fmt.Sprintf("%s %s: %q, want damage and not the count-cap refusal", row.name, path, line))
				}
				continue
			default:
				want = "nil count_over_extent_cap 1 false 0 0"
			}
			if line != want {
				bad = append(bad, fmt.Sprintf("%s %s: %q, want %q", row.name, path, line, want))
			}
		}
		strict := got[row.name+" strict"]
		if strings.HasSuffix(row.name, "_at_cap") {
			if strict == "value" || strings.Contains(strict, "count_over_extent_cap") {
				bad = append(bad, fmt.Sprintf("%s strict: %q, want damage and not the count-cap refusal", row.name, strict))
			}
			continue
		}
		want := row.strict
		if want == "ok" {
			want = "value"
		}
		if strict != want {
			bad = append(bad, fmt.Sprintf("%s strict: %q, want %s", row.name, strict, want))
		}
	}
	return bad
}

// TestLuaCountCapRefusal holds every builder read to the pinned answer for a
// `[]` or map count above the int32 storage cap (docs/SPEC-TABLES.md §2.9,
// §6.5): the four cap rows of the shared pin table refuse as
// count_over_extent_cap on the tolerant load, the plain load, ReadStrict and
// the retaining load, with refused set, malformed unset, no counter moved and
// a nil answer.
func TestLuaCountCapRefusal(t *testing.T) {
	t.Parallel()
	rows := countCapRows(t)
	for _, fault := range countCapFaults(rows, runCountCap(t, rows, nil)) {
		t.Error(fault)
	}
}

// TestLuaCountCapRefusalNegativeControl removes the refusal from the emitted
// runtime and requires the cap rows to go red on every builder read, so the
// test above cannot pass on a runtime that does not refuse.
func TestLuaCountCapRefusalNegativeControl(t *testing.T) {
	t.Parallel()
	rows := countCapRows(t)
	got := runCountCap(t, rows, func(files map[string][]byte) {
		src := files["load.lua"]
		if bytes.Count(src, []byte("      refuse_count(ctx)\n")) != 2 {
			t.Fatal("the emitted load.lua no longer calls refuse_count at the two cap sites")
		}
		files["load.lua"] = bytes.ReplaceAll(src, []byte("      refuse_count(ctx)\n"), nil)
	})
	failed := map[string]bool{}
	for _, fault := range countCapFaults(rows, got) {
		f := strings.SplitN(fault, ":", 2)
		failed[f[0]] = true
	}
	for _, row := range rows {
		if !strings.Contains(row.name, "_cap3") || strings.HasSuffix(row.name, "_at_cap") {
			continue
		}
		for _, path := range []string{"tolerant", "load", "strict", "retain"} {
			if !failed[row.name+" "+path] {
				t.Errorf("%s %s: the probe did not see the refusal removed", row.name, path)
			}
		}
	}
	if len(failed) != 16 {
		t.Errorf("the control failed %d cases, want the 16 cap-row reads alone: %v", len(failed), failed)
	}
}
