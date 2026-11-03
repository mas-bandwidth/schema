package mootable_test

import (
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// countCapProbe prints, per input and per builder read path, the answer a
// count above the int32 storage cap must give (docs/SPEC-TABLES.md §2.9,
// §6.5): whether a value came back, report.reason, report.refused,
// report.malformed, the sum of the report's counters, and the store's use
// for the retaining load with a store.
const countCapProbe = `local function line(name: string, path: string, v: any, rep: any, used: number)
	local moved = rep.unknown + rep.kindMismatch + rep.clamped + rep.widened + rep.duplicate
		+ rep.missingRequired + rep.nonFinite + rep.blobShared + rep.retained + rep.retainLost
	print(string.format("%s\t%s\t%s\t%s\t%d\t%s\t%d\t%d", name, path, tostring(v ~= nil),
		tostring(rep.reason), rep.refused, tostring(rep.malformed), moved, used))
end
local function probe(name: string)
	local wire = inputs[name]
	local rep = L.newReport()
	line(name, "tolerant", L.loadTolerant("Root", wire, rep), rep, 0)
	rep = L.newReport()
	line(name, "plain", L.load("Root", wire, rep), rep, 0)
	rep = L.newReport()
	line(name, "strict", L.ReadStrict("Root", wire, rep), rep, 0)
	rep = L.newReport()
	line(name, "retain", R.LoadRetain("Root", wire, rep, nil), rep, 0)
	rep = L.newReport()
	local store = R.newStore(1024)
	line(name, "retain_store", R.LoadRetain("Root", wire, rep, store), rep, R.used(store))
end
`

var countCapPaths = []string{"tolerant", "plain", "strict", "retain", "retain_store"}

// countCapRetained is a file of the pin's measure unit written by a wider
// Root that also carries `extra int32` ahead of `values`: the reader retains
// extra, then meets the values count. retained_good counts one element;
// retained_cap spells 2^31 there instead, so the store holds a captured record
// when the refusal arrives.
const (
	countCapRetainedHead = "0101040700000002"
	countCapRetainedTail = "0069cb79a912ee29fdfb674d1acf7b27210200000000000000"
	countCapRetainedGood = countCapRetainedHead + "0e06040109000000" + countCapRetainedTail
	countCapRetainedCap  = countCapRetainedHead + "0e06048080808008" + countCapRetainedTail
)

// countCapInputs is the shared pin's five cap rows
// (internal/codegen/goosetable/testdata/load_refusal_reasons.tsv), the widened
// one among them (an int8 element kind over the cap, which moves no widened
// counter), its good
// row, the values field at a count AT the cap (2^31-1), and the two retained
// files.
func countCapInputs(t *testing.T) (map[string][]byte, []string) {
	t.Helper()
	in := map[string][]byte{}
	var names []string
	add := func(name, wire string) {
		b, err := hex.DecodeString(wire)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		in[name] = b
		names = append(names, name)
	}
	for _, r := range reasonRows(t) {
		if r.schema != "measure" || (!strings.Contains(r.name, "_cap3") && r.name != "good" && r.name != "values_widened_over_cap") {
			continue
		}
		if r.name != "good" && r.strict != "count_over_extent_cap" {
			t.Fatalf("%s: the pin answers %s, want count_over_extent_cap", r.name, r.strict)
		}
		add(r.name, r.wire)
		if r.name == "values_cap31" {
			at := strings.Replace(r.wire, "8080808008", "ffffffff07", 1)
			if at == r.wire {
				t.Fatal("values_cap31 does not spell its count as 80 80 80 80 08")
			}
			add("at_cap", at)
		}
	}
	add("retained_good", countCapRetainedGood)
	add("retained_cap", countCapRetainedCap)
	if len(names) != 9 {
		t.Fatalf("%d inputs, want the five cap rows, good, at_cap and the two retained files", len(names))
	}
	return in, names
}

// countCapFloatInputs is the shared pin's float unit: float_good, and a NaN and
// an over-cap count in each read order. When a read meets two refusals, the
// first in read order names it (docs/SPEC-TABLES.md §2.9), so
// nan_then_over_cap answers non_finite_float and over_cap_then_nan answers
// count_over_extent_cap.
func countCapFloatInputs(t *testing.T) (map[string][]byte, []string) {
	t.Helper()
	in := map[string][]byte{}
	var names []string
	for _, r := range reasonRows(t) {
		if r.schema != "float" {
			continue
		}
		b, err := hex.DecodeString(r.wire)
		if err != nil {
			t.Fatalf("%s: %v", r.name, err)
		}
		in[r.name] = b
		names = append(names, r.name)
	}
	if !slices.Equal(names, []string{"float_good", "nan_then_over_cap", "over_cap_then_nan"}) {
		t.Fatalf("the pin's float rows are %v", names)
	}
	return in, names
}

// countCapRed is the gate's red list, one line per input and path whose
// answer is not §2.9's, over the pin's measure and float units. mutate, when
// set, edits the emitted runtime file `file` first.
func countCapRed(t *testing.T, file string, mutate func(string) string) []string {
	t.Helper()
	var bad []string
	for _, u := range []struct {
		unit   string
		inputs func(*testing.T) (map[string][]byte, []string)
	}{{"measure", countCapInputs}, {"float", countCapFloatInputs}} {
		bad = append(bad, countCapUnitRed(t, u.unit, u.inputs, file, mutate)...)
	}
	return bad
}

// countCapUnitRed runs one pin unit's inputs through every builder read path.
func countCapUnitRed(t *testing.T, unit string, inputs func(*testing.T) (map[string][]byte, []string), file string, mutate func(string) string) []string {
	t.Helper()
	in, names := inputs(t)
	g := generateFiles(t, map[string]string{"Probe.schema": reasonSchemas[unit]})
	if mutate != nil {
		files := maps.Clone(g.files)
		files[file] = []byte(mutate(string(files[file])))
		g = writeUnit(t, g.unit, files)
	}
	var main strings.Builder
	main.WriteString("--!strict\nlocal inputs = require(\"./inputs\")\nlocal L = require(\"./table_load\")\nlocal R = require(\"./table_retain\")\n" + g.requireAll() + countCapProbe)
	for _, name := range names {
		fmt.Fprintf(&main, "probe(%q)\n", name)
	}
	got := map[string][]string{}
	for line := range strings.SplitSeq(g.run(t, main.String(), in), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 8 {
			t.Fatalf("the probe printed %q", line)
		}
		got[f[0]+"/"+f[1]] = f[2:]
	}
	var bad []string
	for _, name := range names {
		for _, path := range countCapPaths {
			a, ok := got[name+"/"+path]
			if !ok {
				bad = append(bad, fmt.Sprintf("%s/%s: no answer", name, path))
				continue
			}
			value, reason, refused, malformed, moved, used := a[0], a[1], a[2], a[3], a[4], a[5]
			switch name {
			case "nan_then_over_cap":
				if value != "false" || !strings.HasPrefix(reason, "non_finite_float") || refused != "0" || malformed != "false" || used != "0" {
					bad = append(bad, fmt.Sprintf("%s/%s: value %s reason %s refused %s malformed %s store %s; want no value, non_finite_float, refused 0, malformed false, 0",
						name, path, value, reason, refused, malformed, used))
				}
			case "good", "retained_good", "float_good":
				if value != "true" || malformed != "false" || refused != "0" {
					bad = append(bad, fmt.Sprintf("%s/%s: value %s refused %s malformed %s reason %s, want a value", name, path, value, refused, malformed, reason))
				}
				if name == "retained_good" && path == "retain_store" && used == "0" {
					bad = append(bad, fmt.Sprintf("%s/%s: the store retained nothing, so retained_cap proves nothing", name, path))
				}
			case "at_cap":
				if reason == "count_over_extent_cap" || value != "false" || malformed != "true" {
					bad = append(bad, fmt.Sprintf("%s/%s: value %s reason %s malformed %s, want damage, never the cap refusal", name, path, value, reason, malformed))
				}
			default:
				if value != "false" || reason != "count_over_extent_cap" || refused != "1" || malformed != "false" || moved != "0" || used != "0" {
					bad = append(bad, fmt.Sprintf("%s/%s: value %s reason %s refused %s malformed %s counters %s store %s; want no value, count_over_extent_cap, refused 1, malformed false, 0, 0",
						name, path, value, reason, refused, malformed, moved, used))
				}
			}
		}
	}
	return bad
}

// TestMooCountCapRefusal holds every builder read path (loadTolerant, the
// plain load, ReadStrict, LoadRetain with and without a store) to §2.9's
// answer for a `[]` or map count above the int32 storage cap: no value, the
// reason count_over_extent_cap, refused set, malformed not set, no counter
// moved and nothing left in a retaining store. The good rows read, and a count
// AT the cap that the body cannot cover is damage, never the cap refusal. A
// NaN met before the count is the first refusal in read order and answers
// non_finite_float; a NaN met after it leaves the count's answer.
func TestMooCountCapRefusal(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_load.moo)")
	t.Parallel()
	for _, b := range countCapRed(t, "", nil) {
		t.Error(b)
	}
}

// TestMooCountCapRefusalNegativeControl drops the retaining load's store reset
// on the refusal: retained_cap's store keeps the record captured before the
// count, and that row and path go red, and only it.
func TestMooCountCapRefusalNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_retain.moo)")
	t.Parallel()
	const from = "\tif v == nil and report.reason == \"count_over_extent_cap\" then\n\t\treset_store(store)\n"
	bad := countCapRed(t, "table_retain.moo", func(src string) string {
		if strings.Count(src, from) != 1 {
			t.Fatalf("table_retain.moo carries the anchor %d times, want 1: the control would be vacuous", strings.Count(src, from))
		}
		return strings.Replace(src, from, "\tif false then\n", 1)
	})
	var red []string
	for _, b := range bad {
		red = append(red, b[:strings.Index(b, ":")])
	}
	if !slices.Equal(red, []string{"retained_cap/retain_store"}) {
		t.Fatalf("red %v, want exactly [retained_cap/retain_store]", red)
	}
	t.Logf("red: %s", bad[0])
}

// TestMooCountCapReadOrderNegativeControl drops the read-order rule: the load
// answers the count whatever the walk met first, so nan_then_over_cap goes red
// on every builder read path, and only it.
func TestMooCountCapReadOrderNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_load.moo)")
	t.Parallel()
	const from = "\t\tif _countNonFinite > 0 then\n"
	bad := countCapRed(t, "table_load.moo", func(src string) string {
		if strings.Count(src, from) != 1 {
			t.Fatalf("table_load.moo carries the anchor %d times, want 1: the control would be vacuous", strings.Count(src, from))
		}
		return strings.Replace(src, from, "\t\tif false then\n", 1)
	})
	var red []string
	for _, b := range bad {
		red = append(red, b[:strings.Index(b, ":")])
	}
	var want []string
	for _, path := range countCapPaths {
		want = append(want, "nan_then_over_cap/"+path)
	}
	if !slices.Equal(red, want) {
		t.Fatalf("red %v, want exactly %v", red, want)
	}
	t.Logf("red: %s", bad[0])
}
