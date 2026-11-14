package goosetable_test

// A MAP IS A SORTED ENTRY ARRAY (docs/SPEC-TABLES.md §2.8, docs/PORTING.md
// M19), the Goose leg's gate. The WRITER holds the sort and the READER trusts
// nothing:
//
//   - every writer DERIVES the ascending order from the value, whatever order
//     the value holds its entries in: the probe loads a wire, REVERSES the
//     entries of every map in the flat value table, and the wire save, the
//     text write and the cook (both byte orders, the entry arrays in the
//     holder's node extent) must still be the reference's bytes — the tool's
//     encoder, its text writer and `schema cook` (internal/tablecook);
//   - the text reader places entries ascending whatever order the object
//     names its keys in, and the wire it saves is the reference's;
//   - the wire reader holds §2.8's rules: a widened key ascending loads and
//     counts once, a descending key, a repeated key and a foreign key kind
//     refuse the strict read whole (mapReports, widening_test.go).
//
// The schema holds a map of records that hold a map, a text-keyed map and a
// map of pointers, so the order also decides where a node is numbered (§3.1).

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const mapsProbeSchema = `package probe
table Leaf { v int32 }
table Inner { tags map[int32]uint16
 n int32 }
table Root { scores map[int32]int64
 deep map[uint16]Inner
 names map[string]int32
 links map[uint8]*Leaf
 tail int32 }
`

// mapsProbeText names every map's keys out of order, and points two link
// entries at one shared leaf, so the numbering follows the entry order.
const mapsProbeText = `{"scores":{"5":50,"-3":-30,"1":10,"-40":4},` +
	`"deep":{"9":{"tags":{"7":1,"-2":2,"3":3},"n":4},"3":{"n":1},"6":{"tags":{"1":5}}},` +
	`"names":{"zed":1,"al":2,"b":3,"alpha":4},` +
	`"links":{"8":{"&node":1,"v":80},"2":{"v":20},"5":{"&node":1}},` +
	`"tail":7}`

// mapsProbeMain prints one line per row, `<row> <hex>`, or `<row> !<why>`.
const mapsProbeMain = `
// reverse_maps reverses the entries of every map in the value table in place
// and answers how many maps it reversed: the storage order is the caller's,
// and no writer may rest on it.
fn reverse_maps(vals: schema_rt::LoadVal[>..]&) -> i64 {
    var n: i64 = 0;
    for i in vals.len {
        if vals[i].shape == schema_rt::ShapeMap && vals[i].made && vals[i].len > 1 {
            var a = vals[i].first;
            var b = vals[i].first + vals[i].len - 1;
            while a < b {
                let t = vals[a];
                vals[a] = vals[b];
                vals[b] = t;
                a += 1;
                b -= 1;
            }
            n += 1;
        }
    }
    return n;
}

// unsorted answers how many maps of the value table hold their entries in
// any order but ascending: map_entry_order is the order, and a reader must
// already have placed them so (§2.8).
fn unsorted(data: const u8[:], vals: schema_rt::LoadVal[>..]&) -> i64 {
    var n: i64 = 0;
    var order: i64[>..<] = [];
    for i in vals.len {
        let v = vals[i];
        if v.shape == schema_rt::ShapeMap && v.made && v.len > 1 {
            let kd = probe::TableUnit.tables[vals[v.first].tab].fields[0];
            order.resize(0);
            schema_rt::map_entry_order(data, vals, kd, v.first, v.len, order);
            var out = false;
            for e, k in order { if e != v.first + k { out = true; } }
            if out { n += 1; }
        }
    }
    return n;
}

fn row(name: const u8[:], ok: bool, b: const u8[:]) {
    if ok { write_stdout(str(name, " ")); hex(b); } else { print(name, " !refused"); }
}

fn writers(prefix: const u8[:], data: const u8[:], vals: schema_rt::LoadVal[>..]&, nodes: schema_rt::LoadNode[>..]&, at: i64) {
    var wire: u8[>..<] = [];
    let saved = schema_rt::save_table(probe::TableUnit, "Root", data, vals, nodes, at, wire) == 0;
    row(str(prefix, "-wire"), saved, wire);
    var text: u8[>..] = [];
    let wrote = schema_rt::json_save_table(probe::TableUnit, probe::TextUnit, "Root", data, vals, nodes, at, text) == text.len;
    row(str(prefix, "-text"), wrote, text);
    var big = false;
    while true {
        let need = probe::RootCookMeasure(data, vals, nodes, at);
        var out: u8[>..] = [];
        for _ in (if need > 0 { need } else { 0 }) { out.push(0); }
        let name = str(prefix, if big { "-cook-big" } else { "-cook-little" });
        let cooked = need > 0 && probe::RootCook(data, vals, nodes, at, out, big);
        row(name, cooked, out);
        if big { break; }
        big = true;
    }
}

fn main() {
    var text: u8[>..] = [];
    var file: u8[>..] = [];
    if !read_file("maps.json", text) || !read_file("maps.bin", file) { print("unreadable"); exit(1); }

    // the text reader: keys out of order, entries placed ascending
    var tdata: u8[>..] = [];
    var tvals: schema_rt::LoadVal[>..] = [];
    var tnodes: schema_rt::LoadNode[>..] = [];
    var tr = schema_rt::json_report();
    let tat = schema_rt::json_load_table(probe::TableUnit, probe::TextUnit, "Root", text, tdata, tvals, tnodes, tr);
    if tat < 0 || tr.damaged { print("read-text !refused"); } else {
        print("unsorted-text ", unsorted(tdata, tvals));
        var wire: u8[>..<] = [];
        let saved = schema_rt::save_table(probe::TableUnit, "Root", tdata, tvals, tnodes, tat, wire) == 0;
        row("read-text", saved, wire);
    }

    // the wire, as loaded, then with every map's entries reversed in storage
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var rep = schema_rt::LoadReport { .. };
    let at = schema_rt::load_table(probe::TableUnit, "Root", file, vals, nodes, rep);
    if at < 0 || rep.refused || rep.body_damaged { print("load !refused"); exit(1); }
    print("unsorted-wire ", unsorted(file, vals));
    writers("loaded", file, vals, nodes, at);
    print("reversed ", reverse_maps(vals));
    writers("reversed", file, vals, nodes, at);
}
`

// mapsRows is the gate's red list: one line per row that is red, each
// starting with the row's name.
func mapsRows(t *testing.T, override map[string][]byte) []string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(path, []byte(mapsProbeSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, files := cookUnitFiles(t, []string{path}, override)
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(inst, []byte(mapsProbeText), &report) || report.Refused {
		t.Fatalf("the reference does not read the probe text: %+v", report)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	text, err := m.Write(inst)
	if err != nil {
		t.Fatal(err)
	}
	// json_save_table writes a text FILE, which ends in a newline, as the
	// conformance json-write rows and `schema` itself do
	text = append(text, '\n')
	want := map[string][]byte{"read-text": wire}
	for _, p := range []string{"loaded", "reversed"} {
		want[p+"-wire"] = wire
		want[p+"-text"] = text
		want[p+"-cook-little"] = toolCook(t, m, "Root", wire, false)
		want[p+"-cook-big"] = toolCook(t, m, "Root", wire, true)
	}
	files["maps.json"] = []byte(mapsProbeText)
	files["maps.bin"] = wire
	files["main.goose"] = []byte("import os;\nimport .all;\n\n" + gooserun.HexProbe + mapsProbeMain)
	out := string(gooserun.Run(t, t.TempDir(), "main.goose", files))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSuffix(out, "\n"), "\n") {
		name, rest, _ := strings.Cut(line, " ")
		got[name] = rest
	}
	// the probe is only a probe of the writers' sort if it unsorted something:
	// the root's four maps and the one inner tag map with more than one entry
	if got["reversed"] != "5" {
		t.Fatalf("the probe reversed %q maps in storage, want 5:\n%s", got["reversed"], out)
	}
	var bad []string
	// THE READERS PLACE: a loaded value holds every map ascending, from the
	// text whatever order its object names the keys in
	for _, name := range []string{"unsorted-text", "unsorted-wire"} {
		if got[name] != "0" {
			bad = append(bad, fmt.Sprintf("%s: %q maps of the loaded value hold their entries out of key order", name, got[name]))
		}
	}
	for _, name := range []string{"read-text", "loaded-wire", "loaded-text", "loaded-cook-little", "loaded-cook-big", "reversed-wire", "reversed-text", "reversed-cook-little", "reversed-cook-big"} {
		g, ok := got[name]
		if !ok {
			bad = append(bad, name+": the probe printed no row")
			continue
		}
		b, err := hex.DecodeString(g)
		if err != nil {
			bad = append(bad, fmt.Sprintf("%s: %s", name, g))
			continue
		}
		if !bytes.Equal(b, want[name]) {
			at := 0
			for at < len(b) && at < len(want[name]) && b[at] == want[name][at] {
				at++
			}
			bad = append(bad, fmt.Sprintf("%s: %d bytes, the reference writes %d; first difference at byte %d", name, len(b), len(want[name]), at))
		}
	}
	return append(bad, mapReports(t, override)...)
}

func TestGooseMapsAreSortedEntryArrays(t *testing.T) {
	slowtest.Gate(t, "the Goose toolchain (it runs the maps probe)")
	t.Parallel()
	for _, b := range mapsRows(t, nil) {
		t.Error(b)
	}
}

// TestGooseMapsNegativeControl takes each rule out of the runtime in turn and
// requires it to turn EXACTLY its own rows red:
//
//   - save-sort: the wire save writes the entries in storage order;
//   - save-numbering: the save numbers nodes walking the map in storage order;
//   - cook-sort: the cook lays the entry array in storage order;
//   - text-order: the text writer writes the entries in storage order;
//   - text-read-sort: the text reader places entries in the object's order
//     (every writer still derives the order, so only the placement row turns);
//   - ascending: the wire reader lands a descending key;
//   - duplicate: the wire reader takes a repeated key without counting it;
//   - key-kind: a key of another kind empties the map without the one
//     kind_mismatch it owes, so the strict read keeps a value.
func TestGooseMapsNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the Goose toolchain (it runs the maps probe)")
	t.Parallel()
	for _, ctl := range []struct {
		name, file, from, to string
		red                  []string
	}{
		{"save-sort", "wire_save.goose", "        body(c, t, c.sorted[mark + k], false);\n", "        body(c, t, c.vals[s].first + k, false); // SABOTAGED\n",
			[]string{"reversed-wire"}},
		{"save-numbering", "wire_save.goose", "            let e = c.sorted[mark + k];\n            if c.vals[e].made { edges(c, c.vals[e].tab, e); }\n", "            let e = c.vals[s].first + k; // SABOTAGED\n            if c.vals[e].made { edges(c, c.vals[e].tab, e); }\n",
			[]string{"reversed-wire"}},
		{"cook-sort", "cook_write.goose", "        ok = cook_record(c, col.tab, c.sorted[mark + k], array + k * col.stride);\n", "        ok = cook_record(c, col.tab, c.vals[s].first + k, array + k * col.stride); // SABOTAGED\n",
			[]string{"reversed-cook-little", "reversed-cook-big"}},
		{"text-order", "json_value.goose", "        while j > mark && jw_key_cmp(c, kd, c.scratch[j - 1], c.scratch[j]) > 0 {\n", "        while false && jw_key_cmp(c, kd, c.scratch[j - 1], c.scratch[j]) > 0 { // SABOTAGED\n",
			[]string{"reversed-text"}},
		{"text-read-sort", "json_value.goose", "                while j > mark && jr_key_cmp(c, kd, c.scratch[j - 1], c.scratch[j]) > 0 {\n", "                while false && jr_key_cmp(c, kd, c.scratch[j - 1], c.scratch[j]) > 0 { // SABOTAGED\n",
			[]string{"unsorted-text"}},
		{"ascending", "wire_load.goose", "            if order > 0 { c.report.body_damaged = true; break; } // descending\n", "            if false { break; } // SABOTAGED\n",
			[]string{"descending"}},
		{"duplicate", "wire_load.goose", "            if order == 0 { c.report.repeated += 1; continue; } // last wins whole\n", "            if order == 0 { continue; } // SABOTAGED\n",
			[]string{"repeated"}},
		{"key-kind", "wire_load.goose", "            if kind_bad { c.report.mismatch += 1; landed = 0; break; }\n", "            if kind_bad { landed = 0; break; } // SABOTAGED: not counted\n",
			[]string{"key_kind_mismatch"}},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			bad := mapsRows(t, sabotage(t, ctl.file, ctl.from, ctl.to))
			var rows []string
			for _, b := range bad {
				rows = append(rows, strings.SplitN(b, ":", 2)[0])
			}
			if strings.Join(rows, " ") != strings.Join(ctl.red, " ") {
				t.Fatalf("%s: the plant turned rows %v red, want exactly %v:\n%s", ctl.name, rows, ctl.red, strings.Join(bad, "\n"))
			}
			t.Logf("%s: red on %v as planted", ctl.name, rows)
		})
	}
}
