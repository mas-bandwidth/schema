package lobstertable_test

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// The final single-read contract accepts complete evolution and refuses every
// damaged or incompatible value. Diagnostic spellings and counter order are
// intentionally not assertions. Every malformed fixture is checked through
// ordinary, diagnostic, retaining and region-measure entry points.
func TestLobsterSingleReadVariable(t *testing.T) {
	t.Parallel()
	schema := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(schema, []byte(singleReadVariableSchema), 0644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{schema})
	if err != nil {
		t.Fatal(err)
	}
	fixtures := map[string]string{
		"known_bool":            "0101010200170b8c0879d0f2d50100000000000000",
		"range":                 "0101040a00000000f524f6e7fb630ec10100000000000000",
		"long_text":             "01010c05313233343500861b638ebaadbcc40100000000000000",
		"count_over_bound":      "01010e08030301000200030000fb674d1acf7b27210100000000000000",
		"header_crosses_length": "01010e010300fb674d1acf7b27210100000000000000",
		"array_trailing":        "01010e0303000000fb674d1acf7b27210100000000000000",
		"kind_mismatch":         "0101020100170b8c0879d0f2d50100000000000000",
		"duplicate_known":       "0101040100000001040100000000f524f6e7fb630ec10100000000000000",
		"duplicate_unknown":     "01010101010101005630b3ead46180d80100000000000000",
		"unknown_bool":          "01010102005630b3ead46180d80100000000000000",
		"unknown_surrogate":     "0101210200d8005630b3ead46180d80100000000000000",
		"unknown_utf8":          "01010c02c0af005630b3ead46180d80100000000000000",
		"unknown_null":          "0101200178005630b3ead46180d80100000000000000",
		"unknown_kind":          "010122005630b3ead46180d80100000000000000",
		"unknown_nested_repeat": "01010d0702010102010100005630b3ead46180d80a0a53078bf0c5ef0200000000000000",
		"unknown_array_bool":    "01010e03010102005630b3ead46180d80100000000000000",
		"unknown_keyed_repeat":  "010110080102020101020100005630b3ead46180d8ec105b36194ac93d0200000000000000",
		"unknown_union_bool":    "01010f02010102005630b3ead46180d8b5cc700519c056e70200000000000000",
		"unknown_nonfinite":     "01010a0000807f005630b3ead46180d80100000000000000",
		"pointer_past_nodes":    "01011102009d480025763424620100000000000000",
		"node_unreachable":      "01010c040102010000ffffffffffffffffb9160fa87e3092c50200000000000000",
		"node_cycle":            "01011102020c0701030401110200005630b3ead46180d8ffffffffffffffffb9160fa87e3092c50300000000000000",
		"unknown_node_bool":     "01011102020c0701030401010200005630b3ead46180d8ffffffffffffffffb9160fa87e3092c50300000000000000",
		"blob_shared":           "01011102021102030c06010403616263008cec01864cdc63afa5f101864cdf63afffffffffffffffff58fcaffad8e04b700400000000000000",
		"node_count":            "01010c010100ffffffffffffffff0100000000000000",
		"known_node_range":      "01011102020c0a01030704040a00000000009d48002576342462fffffffffffffffffde71397dc1be321ea0ce83094fde47c0400000000000000",
		"wide_bound":            "0101210661006200630000ca5c7155f6f133a60100000000000000",
		"keyed_duplicate":       "0101100a03020202010002020200005df15095f21f5570ec2202864cfc63af0200000000000000",
		"map_repeated_key":      "01010e1a0d020b020c0161030401000000000b020c0161030402000000000038810af11f06a7a3ec105b36194ac93dea0ce83094fde47c0300000000000000",
		"map_descending_key":    "01010e1a0d020b020c0162030401000000000b020c0161030402000000000038810af11f06a7a3ec105b36194ac93dea0ce83094fde47c0300000000000000",
		"map_long_key":          "01010e120d010f020c056c6f6e6721030401000000000038810af11f06a7a3ec105b36194ac93dea0ce83094fde47c0300000000000000",
		"map_missing_key":       "01010e0a0d0107030401000000000038810af11f06a7a3ec105b36194ac93dea0ce83094fde47c0300000000000000",
		"text_total":            "01010e0a0c0203616263036465660017d69b78483fac6b0100000000000000",
		"text_element":          "01010e080c010561626364650017d69b78483fac6b0100000000000000",
		"widened":               "0101020700f524f6e7fb630ec10100000000000000",
		"unknown_valid":         "01010101005630b3ead46180d80100000000000000",
		"unknown_node_valid":    "01011102020c0401030100005630b3ead46180d8ffffffffffffffffb9160fa87e3092c50300000000000000",
		"empty":                 "01000000000000000000",
	}
	inputs := map[string][]byte{}
	for name, encoded := range fixtures {
		data, err := hex.DecodeString(encoded)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		inputs[name] = data
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(singleReadVariableProgram), 0644); err != nil {
		t.Fatal(err)
	}
	if out := strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs))); out != "one-read native passed" {
		t.Fatalf("%s", out)
	}
	// An unknown invalid bool is detected by the generic payload pass rather
	// than by declared-field decoding. Removing that pass must expose acceptance.
	runtime := filepath.Join(g.dir, "load.lobster")
	data, err := os.ReadFile(runtime)
	if err != nil {
		t.Fatal(err)
	}
	guard := "    if not wire_check(st, first):"
	if strings.Count(string(data), guard) != 1 {
		t.Fatal("wire check guard not unique")
	}
	patched := strings.Replace(string(data), guard, "    if false:", 1)
	if err := os.WriteFile(runtime, []byte(patched), 0644); err != nil {
		t.Fatal(err)
	}
	const control = `import .ProbeTable
import .load
let wire = read_file("unknown_bool")
assert wire
let rep = schema_rt.LoadReport {}
let rec = schema_rt.load("Probe", wire, rep)
print(if rec: "accepted" else: "refused")
schema_rt.release(rec)
`
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(control), 0644); err != nil {
		t.Fatal(err)
	}
	if out := strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs))); out != "accepted" {
		t.Fatalf("generic validation mutation did not expose the defect: %s", out)
	}
}

const singleReadVariableSchema = `package probe
table Probe {
 flag bool
 score int32 | min=0, max=9
 name string(4)
 values [2]int16
 child *Child
 wide wstring(2)
 rows map[string(4)]int32
 texts [..2]string(4) | total=5
 keyed [Key]int16
}
table Child {
 value int32 | min=0, max=9
}

fixed table Fixed { number int32 }
table ValueOnly { score int32 }

enum Key { A, B }
`

const singleReadVariableProgram = `import .ProbeTable
import .schema_save
import .strict_and_refusals
import .retain_unknown
import .region_measure

// A legal read returns every value and writes canonical bytes unchanged.
let original = schema_rt.new_record(schema_rt.table_of("Probe"))
original.fields[0].cell.b = true
original.fields[1].cell.i = 7
original.fields[2].cell.s = "four"
original.fields[2].count = 4
original.fields[3].elems[0].i = 3
original.fields[3].elems[1].i = 4
let child = schema_rt.new_record(schema_rt.table_of("Child"))
child.fields[0].cell.i = 9
original.fields[4].cell.node = child
original.fields[5].cell.units = [97, 98]
original.fields[5].count = 2
let entry = schema_rt.new_record(schema_rt.table_of("ProbeRowsEntry"))
entry.fields[0].cell.s = "key"
entry.fields[0].count = 3
entry.fields[1].cell.i = 8
push(original.fields[6].entries, schema_rt.Value { tab: entry })
let text0 = original.fields[7].elems[0].tab
let text1 = original.fields[7].elems[1].tab
assert text0 and text1
text0.fields[0].cell.s = "abc"
text0.fields[0].count = 3
text1.fields[0].cell.s = "de"
text1.fields[0].count = 2
original.fields[7].count = 2
original.fields[8].elems[0].i = 4
let canonical = schema_rt.save(original)
let report = schema_rt.LoadReport {}
let loaded = schema_rt.load("Probe", canonical, report)
assert loaded
assert loaded.fields[0].cell.b
assert loaded.fields[1].cell.i == 7
assert loaded.fields[2].cell.s == "four"
assert loaded.fields[3].elems[0].i == 3 and loaded.fields[3].elems[1].i == 4
let loaded_child = loaded.fields[4].cell.node
assert loaded_child and loaded_child.fields[0].cell.i == 9
assert length(loaded.fields[5].cell.units) == 2 and loaded.fields[5].cell.units[0] == 97 and loaded.fields[5].cell.units[1] == 98
assert length(loaded.fields[6].entries) == 1
let loaded_entry = loaded.fields[6].entries[0].tab
assert loaded_entry and loaded_entry.fields[0].cell.s == "key" and loaded_entry.fields[1].cell.i == 8
assert loaded.fields[7].count == 2
let loaded_text = loaded.fields[7].elems[1].tab
assert loaded_text and loaded_text.fields[0].cell.s == "de"
assert loaded.fields[8].elems[0].i == 4
assert schema_rt.save(loaded) == canonical
schema_rt.release(loaded)
schema_rt.release(original)

def refused(file:string):
    let data = read_file(file)
    assert data
    let rep = schema_rt.LoadReport {}
    let rec = schema_rt.load("Probe", data, rep)
    if rec:
        print("accepted damaged fixture: " + file)
    assert not rec
    let sr = schema_rt.LoadReport {}
    let strict, reason = schema_rt.load_strict("Probe", data, sr)
    assert not strict and reason != ""
    let rr = schema_rt.LoadReport {}
    let retained = schema_rt.Retain { capacity: 4096, id_capacity: 256, used: 0, ids: [], known: [] }
    let rrec = schema_rt.load_retain("Probe", data, retained, rr)
    assert not rrec
    assert retained.used == 0 and length(retained.ids) == 0 and length(retained.holders) == 0
    let n, whole, bad, why = schema_rt.load_measure("Probe", data)
    assert n == -1 and not whole and bad and why != ""
refused("known_bool")
refused("range")
refused("long_text")
refused("count_over_bound")
refused("header_crosses_length")
refused("array_trailing")
refused("kind_mismatch")
refused("duplicate_known")
refused("duplicate_unknown")
refused("unknown_bool")
refused("unknown_surrogate")
refused("unknown_utf8")
refused("unknown_null")
refused("unknown_kind")
refused("unknown_nested_repeat")
refused("unknown_array_bool")
refused("unknown_keyed_repeat")
refused("unknown_union_bool")
refused("unknown_nonfinite")
refused("pointer_past_nodes")
refused("node_unreachable")
refused("node_cycle")
refused("unknown_node_bool")
refused("blob_shared")
refused("node_count")
refused("known_node_range")

refused("wide_bound")
refused("keyed_duplicate")
refused("map_repeated_key")
refused("map_descending_key")
refused("map_long_key")
refused("map_missing_key")
refused("text_total")
refused("text_element")
for(["widened", "unknown_valid", "unknown_node_valid", "empty"]) name:
    let data = read_file(name)
    assert data
    let evolution_report = schema_rt.LoadReport {}
    let evolution_loaded = schema_rt.load("Probe", data, evolution_report)
    assert evolution_loaded
    if name == "widened":
        assert evolution_loaded.fields[1].cell.i == 7
    schema_rt.release(evolution_loaded)
let empty = read_file("empty")
assert empty
let fixed_evolution_report = schema_rt.LoadReport {}
assert not schema_rt.load("Fixed", empty, fixed_evolution_report)
let evolving = read_file("unknown_node_valid")
assert evolving
let retaining_report = schema_rt.LoadReport {}
let retaining = schema_rt.Retain { capacity: 4096, id_capacity: 256, used: 0, ids: [], known: [] }
let value_only = schema_rt.load_retain("ValueOnly", evolving, retaining, retaining_report)
assert value_only and length(value_only.retained) == 0
assert retaining.used == 0
schema_rt.release(value_only)
let reused_report = schema_rt.LoadReport {}
let damaged = read_file("unknown_bool")
assert damaged
assert not schema_rt.load("Probe", damaged, reused_report)
let retry = schema_rt.load("Probe", empty, reused_report)
assert retry
schema_rt.release(retry)
print("one-read native passed")
`
