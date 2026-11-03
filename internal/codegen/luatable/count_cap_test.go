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

// countCapRow is one input of the count-cap test: its class says what every
// builder read must answer for it, and strict is the strict answer the shared
// pin table holds for a pinned row.
type countCapRow struct {
	name   string
	wire   []byte
	class  string
	strict string
}

// The row classes. A CAP row refuses as count_over_extent_cap on every builder
// read with the report clean. GOOD reads clean. DAMAGE is a wire the load
// discards as malformed, never the count-cap refusal; COUNTED damage also
// moves a counter, which shows the bytes that follow a refusal in
// damage_after_cap would reach the report if the load did not put it back.
const (
	capRow     = "cap"
	goodRow    = "good"
	damageRow  = "damage"
	countedRow = "counted"
)

// The damage-after-refusal pair, in the measure unit. damage_after_cap is
// values_cap31's root body with two fields after the over-cap values: entries
// with element kind 4 where a map needs 13 (one kind_mismatch), then a field
// reference past the id table (malformed). The walk reads both after the
// refusal. damage_alone is the same body with the values count 0, so the
// kind_mismatch and the malformed are this wire's own answer.
const (
	damageAfterCapHex = "01010e0604808080800802" + "0e020400" + "09" + "fb674d1acf7b2721" + "53a245082ca7b2c5" + "0200000000000000"
	damageAloneHex    = "01010e020400" + "020e020400" + "09" + "fb674d1acf7b2721" + "53a245082ca7b2c5" + "0200000000000000"
)

// countCapRows reads the cap rows and the good row of the shared pin table,
// and adds an AT-CAP twin of each cap row: the same wire with its count
// spelled 2^31-1, which is past what the body covers and so is damage, never
// the count-cap refusal. values_widened_over_cap is the cap row whose element
// kind widens, so a widened count would show in the report. Then the
// damage-after-refusal pair.
func countCapRows(t *testing.T) []countCapRow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "goosetable", "testdata", "load_refusal_reasons.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []countCapRow
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 6 || f[1] != "measure" {
			continue
		}
		if !strings.Contains(f[0], "_cap3") && f[0] != "values_widened_over_cap" && f[0] != "good" {
			continue
		}
		wire, err := hex.DecodeString(f[2])
		if err != nil {
			t.Fatal(err)
		}
		if f[0] == "good" {
			rows = append(rows, countCapRow{name: f[0], wire: wire, class: goodRow, strict: f[3]})
			continue
		}
		if f[3] != "count_over_extent_cap" {
			t.Fatalf("%s: the pin table's strict answer is %s", f[0], f[3])
		}
		rows = append(rows, countCapRow{name: f[0], wire: wire, class: capRow, strict: f[3]})
		at := bytes.Clone(wire)
		copy(at[5:10], []byte{0xff, 0xff, 0xff, 0xff, 0x07})
		rows = append(rows, countCapRow{name: f[0] + "_at_cap", wire: at, class: damageRow})
	}
	if len(rows) != 11 {
		t.Fatalf("%d rows, want the five cap rows, their at-cap twins and the good row", len(rows))
	}
	for _, h := range []struct{ name, hex, class, strict string }{
		{"damage_after_cap", damageAfterCapHex, capRow, "count_over_extent_cap"},
		{"damage_alone", damageAloneHex, countedRow, ""},
	} {
		wire, err := hex.DecodeString(h.hex)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, countCapRow{name: h.name, wire: wire, class: h.class, strict: h.strict})
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
// probe's lines, one per failing read, each "<input> <path>: ...". A cap row
// must answer nil on every builder read with the reason count_over_extent_cap,
// refused set, malformed unset, no counter moved and nothing retained; the
// good row reads clean; a damage row is malformed and never the refusal, and a
// counted one also moves a counter.
func countCapFaults(rows []countCapRow, got map[string]string) []string {
	var bad []string
	for _, row := range rows {
		for _, path := range []string{"tolerant", "load", "retain"} {
			line := got[row.name+" "+path]
			switch row.class {
			case goodRow:
				if want := "value - 0 false 0 0"; line != want {
					bad = append(bad, fmt.Sprintf("%s %s: %q, want %q", row.name, path, line, want))
				}
			case capRow:
				if want := "nil count_over_extent_cap 1 false 0 0"; line != want {
					bad = append(bad, fmt.Sprintf("%s %s: %q, want %q", row.name, path, line, want))
				}
			default:
				f := strings.Fields(line)
				if len(f) != 6 || f[0] != "nil" || f[1] == "count_over_extent_cap" || f[3] != "true" || (row.class == countedRow && f[4] == "0") {
					bad = append(bad, fmt.Sprintf("%s %s: %q, want damage and not the count-cap refusal", row.name, path, line))
				}
			}
		}
		strict := got[row.name+" strict"]
		switch row.class {
		case goodRow:
			if strict != "value" {
				bad = append(bad, fmt.Sprintf("%s strict: %q, want value", row.name, strict))
			}
		case capRow:
			if strict != row.strict {
				bad = append(bad, fmt.Sprintf("%s strict: %q, want %s", row.name, strict, row.strict))
			}
		default:
			if strict == "value" || strings.Contains(strict, "count_over_extent_cap") {
				bad = append(bad, fmt.Sprintf("%s strict: %q, want damage and not the count-cap refusal", row.name, strict))
			}
		}
	}
	return bad
}

// countCapFailed runs the probe over a runtime edit and answers the set of
// "<input> <path>" reads that differ from §2.9.
func countCapFailed(t *testing.T, rows []countCapRow, edit func(map[string][]byte)) map[string]bool {
	t.Helper()
	failed := map[string]bool{}
	for _, fault := range countCapFaults(rows, runCountCap(t, rows, edit)) {
		f := strings.SplitN(fault, ":", 2)
		failed[f[0]] = true
	}
	return failed
}

// editLoad replaces one text of the emitted load.lua, which must appear
// exactly `times` times.
func editLoad(t *testing.T, files map[string][]byte, from, to string, times int) {
	t.Helper()
	src := files["load.lua"]
	if n := bytes.Count(src, []byte(from)); n != times {
		t.Fatalf("the emitted load.lua holds %q %d times, want %d", from, n, times)
	}
	files["load.lua"] = bytes.ReplaceAll(src, []byte(from), []byte(to))
}

// TestLuaCountCapRefusal holds every builder read to the pinned answer for a
// `[]` or map count above the int32 storage cap (docs/SPEC-TABLES.md §2.9,
// §6.5): the cap rows of the shared pin table refuse as count_over_extent_cap
// on the tolerant load, the plain load, ReadStrict and the retaining load,
// with refused set, malformed unset, no counter moved (a widening element kind
// included) and a nil answer, and damage the walk meets after the refusal does
// not reach the report.
func TestLuaCountCapRefusal(t *testing.T) {
	t.Parallel()
	rows := countCapRows(t)
	for _, fault := range countCapFaults(rows, runCountCap(t, rows, nil)) {
		t.Error(fault)
	}
}

// TestLuaCountCapRefusalNegativeControls breaks the runtime three ways and
// requires each break to turn exactly its own reads red, so the test above
// cannot pass on a runtime that lacks any of the three:
//   - without the refusal, every cap row goes red on every builder read;
//   - counting a widening element kind before the cap turns
//     values_widened_over_cap red on the reads that answer a report;
//   - without putting the report back, damage_after_cap carries the
//     kind_mismatch and the malformed the walk met after the refusal.
func TestLuaCountCapRefusalNegativeControls(t *testing.T) {
	t.Parallel()
	rows := countCapRows(t)
	reads := func(names []string, paths ...string) map[string]bool {
		want := map[string]bool{}
		for _, name := range names {
			for _, path := range paths {
				want[name+" "+path] = true
			}
		}
		return want
	}
	var caps []string
	for _, row := range rows {
		if row.class == capRow {
			caps = append(caps, row.name)
		}
	}
	for _, c := range []struct {
		name string
		edit func(map[string][]byte)
		want map[string]bool
	}{
		{"refusal", func(files map[string][]byte) {
			editLoad(t, files, "      refuse_count(ctx)\n", "", 2)
		}, reads(caps, "tolerant", "load", "strict", "retain")},
		{"widened", func(files map[string][]byte) {
			editLoad(t, files, "if not (fd.list and above(count, 0x7fffffff)) then report.widened = report.widened + 1 end", "report.widened = report.widened + 1", 1)
		}, reads([]string{"values_widened_over_cap"}, "tolerant", "load", "retain")},
		{"restore", func(files map[string][]byte) {
			editLoad(t, files, "      report[k] = held[i]\n", "", 1)
		}, reads([]string{"damage_after_cap"}, "tolerant", "load", "retain")},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := countCapFailed(t, rows, c.edit)
			for read := range c.want {
				if !got[read] {
					t.Errorf("%s: the probe did not see the %s control", read, c.name)
				}
			}
			for read := range got {
				if !c.want[read] {
					t.Errorf("%s: red under the %s control, which should not reach it", read, c.name)
				}
			}
		})
	}
}
