package goosetable_test

// THE SAVE'S SIGNED BOUNDS over a caller's edited values: the save converts a
// stored union tag, a pointer slot's node index and a retained record's copied
// length to signed integers before it indexes with them, so a value with its
// high bit set reads as negative. Each conversion is bounded below as well as
// above:
//
//	union   the root's union slot is loaded, then its tag is set to
//	        0xFFFFFFFFFFFFFFFF (signed -1): save_table and save_table_retain
//	        refuse union_no_arm, and the caller's output keeps its one
//	        poison byte (runtime/wire_save.goose, edges_arm)
//	pointer the root's *string slot is set to 0x8000000000000002, to all
//	        ones, and to the node count plus two (one past the node table's
//	        last index): save_table and save_table_retain refuse
//	        invalid_value, and the output keeps its poison byte
//	        (runtime/wire_save.goose, edge_of)
//	retain  a retained by-value table's string length in the caller's
//	        RetainStore is rewritten as a LEB128 of 2^64-1000 (signed -1000):
//	        the record is dropped as one retain_lost and the save writes the
//	        bytes a save without retention writes (runtime/wire_retain.goose,
//	        em_copy)
//
// TestGooseSaveSignedBoundsNegativeControl takes each bound out and requires
// the probe to stop on an out-of-range index, or, for the pointer's signed
// reading, to save the high-bit slots as null.
//
// Unit tests here use no real time, no socket and no network: each run is
// `goose run` of a generated program in t.TempDir().

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const saveBoundsSchema = `package probe
table Leaf { s string(32) }
union Choice { leaf Leaf
 text *string }
table Root { choice Choice
 note *string }
`

// saveBoundsWriter is the writer's newer Root: a by-value table field this
// reader cannot name, which the retaining load keeps.
const saveBoundsWriter = `package probe
table Leaf { s string(32) }
table Extra { s string(32) }
union Choice { leaf Leaf
 text *string }
table Root { choice Choice
 note *string
 extra Extra }
`

// saveBoundsPoison is the LEB128 of 2^64-1000, whose signed reading is -1000.
func saveBoundsPoison() []byte {
	x := ^uint64(0) - 999
	var b []byte
	for {
		low := byte(x & 0x7F)
		x >>= 7
		if x == 0 {
			return append(b, low)
		}
		b = append(b, low|0x80)
	}
}

const saveBoundsMain = `
fn store_of(bytes: u8[>..<]&, recs: schema_rt::RetainRec[>..<]&, ids: u64[>..<]&) -> schema_rt::RetainStore {
    return schema_rt::RetainStore { capacity: -1, id_capacity: -1, used: 0, bytes: bytes, recs: recs, ids: ids };
}

// union_case loads the union wire, sets the root union's tag to all ones and
// saves it plain and retaining into outputs that hold one poison byte each.
fn union_case() {
    let data = input("union.bin");
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var report = schema_rt::LoadReport { .. };
    let root = schema_rt::load_table(conformance_unit(), "Root", data, vals, nodes, report);
    if root < 0 || report.malformed { write_stdout("union refused\n"); return; }
    let s = vals[root].first;
    if vals[s].shape != schema_rt::ShapeUnion || vals[s].state != schema_rt::ValPresent { write_stdout("union unset\n"); return; }
    vals[s].bits = schema_rt::Int128 { lo: 0xFFFFFFFFFFFFFFFF, hi: 0 };
    var out: u8[>..<] = [];
    out.push(0xEE);
    let r = schema_rt::save_table(conformance_unit(), "Root", data, vals, nodes, root, out);
    write_stdout(str("union ", schema_rt::save_refusal_name(r), "_ ", hex(out), "\n"));
    var bytes: u8[>..<] = [];
    var recs: schema_rt::RetainRec[>..<] = [];
    var ids: u64[>..<] = [];
    var store = store_of(bytes, recs, ids);
    var rr = schema_rt::LoadReport { .. };
    var again: u8[>..<] = [];
    again.push(0xEE);
    let r2 = schema_rt::save_table_retain(conformance_unit(), "Root", data, vals, nodes, root, store, rr, again);
    write_stdout(str("union-retain ", schema_rt::save_refusal_name(r2), "_ ", hex(again), "\n"));
}

// pointer_case loads the union wire, sets the root's *string slot to lo
// (the node count plus two when past), and saves it plain and retaining
// into outputs that hold one poison byte each.
fn pointer_case(name: const u8[:], lo: u64, past: bool) {
    let data = input("union.bin");
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var report = schema_rt::LoadReport { .. };
    let root = schema_rt::load_table(conformance_unit(), "Root", data, vals, nodes, report);
    if root < 0 || report.malformed { write_stdout(str(name, " refused\n")); return; }
    let s = vals[root].first + 1;
    if vals[s].state != schema_rt::ValPresent { write_stdout(str(name, " unset\n")); return; }
    vals[s].bits = schema_rt::Int128 { lo: if past { (nodes.len + 2) as! u64 } else { lo }, hi: 0 };
    var out: u8[>..<] = [];
    out.push(0xEE);
    let r = schema_rt::save_table(conformance_unit(), "Root", data, vals, nodes, root, out);
    write_stdout(str(name, " ", schema_rt::save_refusal_name(r), "_ ", hex(out), "\n"));
    var bytes: u8[>..<] = [];
    var recs: schema_rt::RetainRec[>..<] = [];
    var ids: u64[>..<] = [];
    var store = store_of(bytes, recs, ids);
    var rr = schema_rt::LoadReport { .. };
    var again: u8[>..<] = [];
    again.push(0xEE);
    let r2 = schema_rt::save_table_retain(conformance_unit(), "Root", data, vals, nodes, root, store, rr, again);
    write_stdout(str(name, "-retain ", schema_rt::save_refusal_name(r2), "_ ", hex(again), "\n"));
}

// retain_case loads the newer writer's wire retaining, rewrites the retained
// string's length in the caller's store, and saves it retaining; the plain
// save is the bytes it must equal.
fn retain_case(poison: const u8[:]) {
    let data = input("retain.bin");
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var report = schema_rt::LoadReport { .. };
    var bytes: u8[>..<] = [];
    var recs: schema_rt::RetainRec[>..<] = [];
    var ids: u64[>..<] = [];
    var store = store_of(bytes, recs, ids);
    let root = schema_rt::load_table_retain(conformance_unit(), "Root", data, vals, nodes, report, store);
    if root < 0 || report.malformed || store.recs.len != 1 { write_stdout("retain refused\n"); return; }
    var plain: u8[>..<] = [];
    let p = schema_rt::save_table(conformance_unit(), "Root", data, vals, nodes, root, plain);
    // the retained string's LEB128 length (32) is the byte before its first 'q'
    var at: i64 = -1;
    let r0 = store.recs[0];
    for i in r0.len - 1 {
        if at < 0 && store.bytes[r0.off + i] == 32 && store.bytes[r0.off + i + 1] == 'q' { at = r0.off + i; }
    }
    if at < 0 { write_stdout("retain unfound\n"); return; }
    for b, i in poison { store.bytes[at + i] = b; }
    let lost = report.retain_lost;
    var out: u8[>..<] = [];
    let r = schema_rt::save_table_retain(conformance_unit(), "Root", data, vals, nodes, root, store, report, out);
    write_stdout(str("retain ", schema_rt::save_refusal_name(r), "_ ", report.retain_lost - lost, " ", hex(out), " ", schema_rt::save_refusal_name(p), "_ ", hex(plain), "\n"));
}

fn main() {
    union_case();
    pointer_case("ptr-high", 0x8000000000000002, false);
    pointer_case("ptr-ones", 0xFFFFFFFFFFFFFFFF, false);
    pointer_case("ptr-past", 0, true);
`

// saveBounds runs the probe over the runtime (with `override` replacing
// runtime files) and answers its stdout, its stderr, and whether it ran to
// its end.
func saveBounds(t *testing.T, override map[string][]byte) (string, string, bool) {
	t.Helper()
	u := probeUnit(t, saveBoundsSchema)
	m := tabletext.NewModel(u)
	v := m.New(u.Tables["Root"])
	var r tabletext.Report
	if !m.Read(v, []byte(`{"choice":{"leaf":{"s":"arm"}},"note":"n"}`), &r) || r.Malformed {
		t.Fatalf("union text: %+v", r)
	}
	unionWire, err := tablewire.Encode(m, v)
	if err != nil {
		t.Fatal(err)
	}
	wu := probeUnit(t, saveBoundsWriter)
	wm := tabletext.NewModel(wu)
	wv := wm.New(wu.Tables["Root"])
	text := fmt.Sprintf(`{"note":"n","extra":{"s":%q}}`, strings.Repeat("q", 32))
	if !wm.Read(wv, []byte(text), &r) || r.Malformed {
		t.Fatalf("retain text: %+v", r)
	}
	retainWire, err := tablewire.Encode(wm, wv)
	if err != nil {
		t.Fatal(err)
	}
	files := probeFiles(t, u, override)
	files["union.bin"] = unionWire
	files["retain.bin"] = retainWire
	var main strings.Builder
	main.WriteString(probePrelude)
	main.WriteString(saveBoundsMain)
	fmt.Fprintf(&main, "    let poison: u8[%d] = [", len(saveBoundsPoison()))
	for i, b := range saveBoundsPoison() {
		if i > 0 {
			main.WriteString(", ")
		}
		fmt.Fprintf(&main, "%d", b)
	}
	main.WriteString("];\n    retain_case(poison);\n}\n")
	files["main.goose"] = []byte(main.String())
	goose, why := gooserun.Compiler()
	if goose == "" {
		t.Fatalf("the Goose compiler does not resolve: %s", why)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(goose, "run", "main.goose")
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	return stdout.String(), stderr.String(), err == nil
}

// saveBoundsRefusals is each refused row's refusal name.
var saveBoundsRefusals = map[string]string{
	"union": "union_no_arm", "union-retain": "union_no_arm",
	"ptr-high": "invalid_value", "ptr-high-retain": "invalid_value",
	"ptr-ones": "invalid_value", "ptr-ones-retain": "invalid_value",
	"ptr-past": "invalid_value", "ptr-past-retain": "invalid_value",
}

func TestGooseSaveSignedBounds(t *testing.T) {
	t.Parallel()
	out, stderr, ok := saveBounds(t, nil)
	if !ok {
		t.Fatalf("the probe stopped: %q\nstderr:\n%s", out, stderr)
	}
	lines := probeLines([]byte(out))
	for name, want := range saveBoundsRefusals {
		if f := lines[name]; len(f) != 2 || f[0] != want+"_" || f[1] != "ee" {
			t.Errorf("%s: the save answered %q, want %s_ with the output's poison byte untouched", name, f, want)
		}
	}
	f := lines["retain"]
	switch {
	case len(f) != 5:
		t.Errorf("retain: the probe printed %q", f)
	case f[0] != "_" || f[3] != "_":
		t.Errorf("retain: the saves refused %q and %q, want neither", f[0], f[3])
	case f[1] != "1":
		t.Errorf("retain: the save counted %s retain_lost, want 1", f[1])
	case !bytes.Equal(probeHex(t, f[2]), probeHex(t, f[4])):
		t.Errorf("retain: the save wrote %s, want the plain save's %s", f[2], f[4])
	}
}

// TestGooseSaveSignedBoundsNegativeControl takes out one bound at a time:
//
//   - edges_arm without `tag < 0`: the all-ones tag indexes the arms at -2;
//   - edge_of without its upper bound: the pointer one past the node table
//     indexes the save's open marks past their end;
//   - edge_of bounding the signed reading from above only: the high-bit
//     pointers read as negative and save as null, unrefused;
//   - em_copy without the copied length's bound: the cursor steps back past
//     the record and the next read indexes the store below zero.
func TestGooseSaveSignedBoundsNegativeControl(t *testing.T) {
	t.Parallel()
	const bound = "    if c.vals[s].bits.hi != 0 || wire > (c.nodes.len + 1) as! u64 { refuse(c, SaveRefuseInvalidValue); return 0; }\n"
	for _, ctl := range []struct {
		name, file, from, to string
		null                 bool // the probe runs to its end and the high-bit rows save
	}{
		{"union-tag", "wire_save.goose",
			"    if u < 0 || tag < 0 || tag > c.unit.unions[u].arms.len { return; }\n",
			"    if u < 0 || tag > c.unit.unions[u].arms.len { return; }\n", false},
		{"pointer-upper", "wire_save.goose", bound,
			"    if c.vals[s].bits.hi != 0 { refuse(c, SaveRefuseInvalidValue); return 0; }\n", false},
		{"pointer-signed", "wire_save.goose", bound,
			"    if c.vals[s].bits.hi != 0 || (wire as! i64) > c.nodes.len + 1 { refuse(c, SaveRefuseInvalidValue); return 0; }\n", true},
		{"retain-length", "wire_retain.goose",
			"        if x > (k.end - k.off - used) as! u64 { k.bad = true; return; }\n",
			"", false},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			out, stderr, ok := saveBounds(t, sabotage(t, ctl.file, ctl.from, ctl.to))
			if ctl.null {
				lines := probeLines([]byte(out))
				if !ok {
					t.Fatalf("%s: the probe stopped: %q\nstderr:\n%s", ctl.name, out, stderr)
				}
				for _, name := range []string{"ptr-high", "ptr-high-retain", "ptr-ones", "ptr-ones-retain"} {
					if f := lines[name]; len(f) != 2 || f[0] != "_" {
						t.Errorf("%s: %s answered %q, want an unrefused save", ctl.name, name, f)
					}
				}
				t.Logf("%s: ptr-high saved %s", ctl.name, lines["ptr-high"])
				return
			}
			if ok || !strings.Contains(stderr, "out of bounds") || !strings.Contains(stderr, ctl.file) {
				t.Fatalf("%s: without the bound the probe ran to its end (%t) or stopped elsewhere: %q\nstderr:\n%s", ctl.name, ok, out, stderr)
			}
			lines := strings.Split(strings.TrimSpace(stderr), "\n")
			t.Logf("%s: %s", ctl.name, lines[len(lines)-1])
		})
	}
}
