package goosetable_test

// TWO VERDICTS THE GOOSE LEG OWES THE REFERENCE (docs/SPEC-TABLES.md §2.8,
// §4; docs/PORT-GOOSE.md):
//
//   - a map holding one key twice is REFUSED AT SAVE (invalid_value, as the
//     reference's tableMapCursor refuses it), never written: no reader takes
//     such a file but as damage;
//   - a REFUSED LOAD restores the caller's stores, the way <T>ToLoaded does
//     (value_bridge.go): the wire load and the text load leave the value and
//     node tables (and the text load the data buffer) the length they had.

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const refusedStoresSchema = `package probe
table Root { entries map[int32]int32
 past int32
 name string }
`

const refusedStoresMain = `import os;
import .all;

fn main() {
    var ok: u8[>..] = [];
    var bad: u8[>..] = [];
    var text: u8[>..] = [];
    if !read_file("ok.bin", ok) || !read_file("bad.bin", bad) || !read_file("bad.json", text) { print("unreadable"); exit(1); }
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var rep = schema_rt::LoadReport { .. };
    let at = schema_rt::load_table(probe::TableUnit, "Root", ok, vals, nodes, rep);
    if at < 0 { print("load !refused"); exit(1); }
    var wire: u8[>..<] = [];
    print("save-clean ", schema_rt::save_table(probe::TableUnit, "Root", ok, vals, nodes, at, wire));
    // the second entry's key becomes the first's: a key twice in the value
    let s = vals[at].first;
    let e0 = vals[vals[s].first].first;
    let e1 = vals[vals[s].first + 1].first;
    vals[e1].bits.lo = vals[e0].bits.lo;
    var again: u8[>..<] = [];
    print("save-dup ", schema_rt::save_table(probe::TableUnit, "Root", ok, vals, nodes, at, again));

    // a refused wire load leaves the caller's tables as it found them
    var v2: schema_rt::LoadVal[>..] = [];
    var n2: schema_rt::LoadNode[>..] = [];
    v2.push(schema_rt::LoadVal { .. });
    v2.push(schema_rt::LoadVal { .. });
    n2.push(schema_rt::LoadNode { tab: -1, blob: 0, off: 0, len: 0, val: -1, named: false });
    var r2 = schema_rt::LoadReport { .. };
    print("wire-at ", schema_rt::load_table(probe::TableUnit, "Root", bad, v2, n2, r2));
    print("wire-vals ", v2.len);
    print("wire-nodes ", n2.len);

    // and so does a refused text load, the data buffer too
    var d3: u8[>..] = [];
    var v3: schema_rt::LoadVal[>..] = [];
    var n3: schema_rt::LoadNode[>..] = [];
    d3.push(7);
    v3.push(schema_rt::LoadVal { .. });
    v3.push(schema_rt::LoadVal { .. });
    n3.push(schema_rt::LoadNode { tab: -1, blob: 0, off: 0, len: 0, val: -1, named: false });
    var r3 = schema_rt::json_report();
    print("json-at ", schema_rt::json_load_table(probe::TableUnit, probe::TextUnit, "Root", text, d3, v3, n3, r3));
    print("json-data ", d3.len);
    print("json-vals ", v3.len);
    print("json-nodes ", n3.len);
}
`

// refusedStoresWire is a Root wire holding the int32 keys of `keys`, ascending
// or not, in a map ("entries"), past = 42; it is mapWire for this schema.
func refusedStoresWire(keys []byte) []byte {
	return mapWire(keys, []byte{2, 2, 2, 2}[:len(keys)])
}

func TestGooseRefusedStoresAndDuplicateSave(t *testing.T) {
	slowtest.Gate(t, "the Goose toolchain (it runs the refused-stores probe)")
	t.Parallel()
	_ = binary.LittleEndian
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(path, []byte(refusedStoresSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	_, files := cookUnitFiles(t, []string{path}, nil)
	files["ok.bin"] = refusedStoresWire([]byte{1, 2})
	// a repeated key: the strict load refuses at its end, after the walk
	// has placed the entries
	files["bad.bin"] = refusedStoresWire([]byte{2, 2})
	// a string lands in the data buffer, then the int32 is refused
	files["bad.json"] = []byte(`{"name":"hello","entries":{"1":2},"past":"x"}`)
	files["main.goose"] = []byte(refusedStoresMain)
	out := string(gooserun.Run(t, t.TempDir(), "main.goose", files))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSuffix(out, "\n"), "\n") {
		name, rest, _ := strings.Cut(line, " ")
		got[name] = strings.TrimSpace(rest)
	}
	want := map[string]string{
		"save-clean": "0",
		"save-dup":   "8", // SaveRefuseInvalidValue
		"wire-at":    "-1", "wire-vals": "2", "wire-nodes": "1",
		"json-at": "-1", "json-data": "1", "json-vals": "2", "json-nodes": "1",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q, want %q\n%s", k, got[k], v, out)
		}
	}
}
