package squirreltable_test

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
)

// countCapSchema is the measure unit of
// internal/codegen/goosetable/testdata/load_refusal_reasons.tsv, and
// countCapFloatSchema its float unit.
const (
	countCapSchema      = "package probe\n table Child { values []int32 }\n table Root { values []int32\n entries map[int32]int32\n child Child\n next *Root }\n"
	countCapFloatSchema = "package probe\n table Root { f float32\n values []int32 }\n"
)

// countCapProbe reads each input through every builder read: the tolerant
// load, the plain load, ReadStrict and the retaining load. Each line is
// `<key> <path> <value> <refused> <malformed> <counters> <reason>`, where
// counters is the sum of the report's counters.
const countCapProbe = `local inputs = dofile("inputs.nut", true);
local schema = dofile("probe/schema_module.nut", true);
local T = schema.require("__MOD__");
local L = schema.require("schema_table_load");
local R = schema.require("schema_table_refusal");
local RT = schema.require("schema_table_retain");
local newReport = function() {
  return { unknown = 0, mismatch = 0, widened = 0, oor = 0,
    repeated = 0, bodyDamaged = false, refused = false };
};
local show = function(key, path, v, r) {
  local n = r.unknown + r.mismatch + r.widened + r.oor + r.repeated;
  local why = ("reason" in r) && r.reason != null ? r.reason : "-";
  print(key + " " + path + " " + (v == null ? "nil" : "some") + " " + r.refused + " " +
        r.bodyDamaged + " " + n + " " + why + "\n");
};
foreach (key in __KEYS__) {
  local r = newReport();
  show(key, "tolerant", L.loadWalk(T, "Root", inputs[key], r), r);
  r = newReport();
  show(key, "plain", L.load(T, "Root", inputs[key], r), r);
  r = {};
  show(key, "strict", R.ReadStrict(T, "Root", inputs[key], r), r);
  r = newReport();
  local store = { capacity = 1048576, idCapacity = 1024 };
  local v = RT.LoadRetain(T, "Root", inputs[key], store, r);
  show(key, "retain", v, r);
  if (v == null && store.recs.len() != 0) print(key + " retain kept " + store.recs.len() + " records\n");
}
`

// TestSquirrelCountCapRefusal holds every builder read to docs/SPEC-TABLES.md
// §2.9: a `[]` or map count above the int32 storage cap answers null, refused
// by the name count_over_extent_cap, with malformed unset and no counter
// moved, on the tolerant load, the plain load, ReadStrict and the retaining
// load. The inputs are the cap rows of the shared pin table, whose strict
// column this read must also answer; values_widened_over_cap carries an int8
// element kind that widens to int32, and its refusal moves no widened counter
// either. The negative controls are the pin's good
// row, which reads, and each cap row's count lowered to exactly 2^31-1, which
// is not this refusal (its body cannot cover the count, so it is damage).
func TestSquirrelCountCapRefusal(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "goosetable", "testdata", "load_refusal_reasons.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	pinned := map[string]string{}
	var keys []string
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 6 || f[1] != "measure" || (!strings.Contains(f[0], "_cap3") && f[0] != "good" && f[0] != "values_widened_over_cap") {
			continue
		}
		wire, err := hex.DecodeString(f[2])
		if err != nil {
			t.Fatal(err)
		}
		inputs[f[0]] = wire
		pinned[f[0]] = f[3]
		keys = append(keys, f[0])
		if f[0] != "good" {
			// the count's LEB128 starts at byte 5: 2^31 or 2^32 there, and
			// the control spells 2^31-1 in the same five bytes
			at := append([]byte(nil), wire...)
			copy(at[5:10], []byte{0xff, 0xff, 0xff, 0xff, 0x07})
			inputs[f[0]+"_atcap"] = at
			keys = append(keys, f[0]+"_atcap")
		}
	}
	if len(pinned) != 6 {
		t.Fatalf("%d cap rows and the good row, want 6", len(pinned))
	}
	got, out := runCountCapProbe(t, countCapSchema, keys, inputs, nil)
	for _, k := range keys {
		for _, p := range countCapPaths {
			a, ok := got[k+" "+p]
			if !ok {
				t.Errorf("%s %s: no answer", k, p)
				continue
			}
			value, refused, malformed, counters, reason := a[0], a[1], a[2], a[3], a[4]
			switch {
			case strings.HasSuffix(k, "_atcap"):
				if reason == "count_over_extent_cap" || refused == "true" || value != "nil" || malformed != "true" {
					t.Errorf("%s %s: %v, want damage (null, malformed) and not the count-cap refusal", k, p, a)
				}
			case k == "good":
				if value != "some" || refused != "false" || malformed != "false" {
					t.Errorf("%s %s: %v, want the value read", k, p, a)
				}
			default:
				if value != "nil" || refused != "true" || malformed != "false" || counters != "0" || reason != "count_over_extent_cap" {
					t.Errorf("%s %s: %v, want nil refused=true malformed=false counters=0 count_over_extent_cap", k, p, a)
				}
				if p == "strict" && reason != pinned[k] {
					t.Errorf("%s strict: %s, the pin says %s", k, reason, pinned[k])
				}
			}
		}
	}
	if strings.Contains(out, "retain kept") {
		t.Errorf("a refused retaining load kept captured records:\n%s", out)
	}
}

// countCapPaths are the builder reads the probe answers for each input.
var countCapPaths = []string{"tolerant", "plain", "strict", "retain"}

// runCountCapProbe emits the unit, lets mutate edit the emitted directory,
// reads each input through every builder read, and answers the probe's fields
// keyed by "<key> <path>" with the raw output.
func runCountCapProbe(t *testing.T, schema string, keys []string, inputs map[string][]byte, mutate func(dir string)) (map[string][]string, string) {
	t.Helper()
	dir := t.TempDir()
	mod := emitUnit(t, dir, "probe", loadSource(t, schema))
	if mutate != nil {
		mutate(filepath.Join(dir, "probe"))
	}
	var list strings.Builder
	list.WriteString("[")
	for i, k := range keys {
		if i > 0 {
			list.WriteString(", ")
		}
		fmt.Fprintf(&list, "%q", k)
	}
	list.WriteString("]")
	probe := strings.NewReplacer("__MOD__", mod, "__KEYS__", list.String()).Replace(countCapProbe)
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(probe))
	out := string(sqrun.Run(t, dir, "main.nut", inputs))
	got := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) < 7 {
			t.Fatalf("probe line %q", line)
		}
		// a reason may carry ": <field>"; the name is its first word
		got[f[0]+" "+f[1]] = f[2:7]
	}
	return got, out
}

// readOrderFaults answers, for the pin's float unit, every "<key> <path>"
// read that differs from the rule that when a read meets two refusals, the
// first in read order names it (docs/SPEC-TABLES.md §2.9): nan_then_over_cap
// answers non_finite_float on every builder read, null and malformed unset,
// over_cap_then_nan answers count_over_extent_cap with refused set and no
// counter moved, and float_good reads.
func readOrderFaults(t *testing.T, mutate func(dir string)) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "goosetable", "testdata", "load_refusal_reasons.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	pinned := map[string]string{}
	var keys []string
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
		pinned[f[0]] = f[3]
		keys = append(keys, f[0])
	}
	if strings.Join(keys, " ") != "float_good nan_then_over_cap over_cap_then_nan" {
		t.Fatalf("the pin's float rows are %v", keys)
	}
	got, out := runCountCapProbe(t, countCapFloatSchema, keys, inputs, mutate)
	bad := map[string]string{}
	for _, k := range keys {
		for _, p := range countCapPaths {
			a, ok := got[k+" "+p]
			if !ok {
				bad[k+" "+p] = "no answer"
				continue
			}
			value, refused, malformed, counters, reason := a[0], a[1], a[2], a[3], strings.TrimSuffix(a[4], ":")
			switch k {
			case "float_good":
				if value != "some" || refused != "false" || malformed != "false" {
					bad[k+" "+p] = fmt.Sprintf("%v, want the value read", a)
				}
			case "nan_then_over_cap":
				if value != "nil" || malformed != "false" || reason != "non_finite_float" {
					bad[k+" "+p] = fmt.Sprintf("%v, want nil malformed=false non_finite_float", a)
				}
			default:
				if value != "nil" || refused != "true" || malformed != "false" || counters != "0" || reason != "count_over_extent_cap" {
					bad[k+" "+p] = fmt.Sprintf("%v, want nil refused=true malformed=false counters=0 count_over_extent_cap", a)
				}
			}
			if p == "strict" && k != "float_good" && reason != pinned[k] {
				bad[k+" "+p] = fmt.Sprintf("%s, the pin says %s", reason, pinned[k])
			}
		}
	}
	if strings.Contains(out, "retain kept") {
		bad["retain"] = "a refused retaining load kept captured records:\n" + out
	}
	return bad
}

// TestSquirrelCountCapReadOrder holds the pin's float unit to the first
// refusal in read order, on every builder read.
func TestSquirrelCountCapReadOrder(t *testing.T) {
	t.Parallel()
	for read, fault := range readOrderFaults(t, nil) {
		t.Errorf("%s: %s", read, fault)
	}
}

// TestSquirrelCountCapReadOrderNegativeControl lets the count refusal win
// whatever the walk met first: nan_then_over_cap goes red on the reads that
// answer the load's own reason, and no other row moves.
func TestSquirrelCountCapReadOrderNegativeControl(t *testing.T) {
	t.Parallel()
	const from = "if (report.countRefusal && st.nonFinite == null) {"
	bad := readOrderFaults(t, func(dir string) {
		path := filepath.Join(dir, "schema_table_load.nut")
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(src), from) != 1 {
			t.Fatalf("schema_table_load.nut carries the anchor %d times, want 1: the control would be vacuous", strings.Count(string(src), from))
		}
		writeFile(t, path, []byte(strings.Replace(string(src), from, "if (report.countRefusal) {", 1)))
	})
	for _, p := range []string{"tolerant", "plain", "retain"} {
		if bad["nan_then_over_cap "+p] == "" {
			t.Errorf("nan_then_over_cap %s: the control did not turn it red", p)
		}
	}
	for read := range bad {
		if !strings.HasPrefix(read, "nan_then_over_cap ") {
			t.Errorf("%s: red under the control, which should not reach it: %s", read, bad[read])
		}
	}
}
