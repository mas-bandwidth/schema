package goosetable_test

// STRING IDENTITY for Goose (docs/SPEC-TABLES.md §2.5, §3.1): a caller's equal
// strings in two blob slots save BY VALUE, two node records; only one node
// named from two slots is blob_shared. A *string slot's identity in the Goose
// value is the NODE it names (runtime/wire_save.goose, visit), never the bytes
// the node views, so the gate drives every way a value can hold equal text:
//
//	decoded   a file the reference engine writes (internal/tablewire Encode)
//	          from equal and empty strings at every site a *string can sit
//	          (two root slots, a pointed-at child, a union arm, map values)
//	          loads plain and retaining, and saves back to the reference's
//	          bytes, twice
//	view      the caller points the second slot's node at the FIRST node's
//	          bytes: two nodes over one view still save by value
//	copy      the caller gives a slot that shared a node its own node over the
//	          same bytes: the save writes two records, the reference's bytes
//	shared    one node named from two slots is refused by name, plain and
//	          retaining, after a load that reads it
//
// TestGooseStringIdentityNegativeControl plants identity-by-view in the save
// (a blob whose bytes view equals an earlier blob's is refused as shared) and
// drops the blob_shared refusal, and requires each to turn its own rows red.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const stringIdentitySchema = `package probe
table Child { caption *string }
union Choice { text *string }
table Root { a *string
 b *string
 child *Child
 choice Choice
 tags map[string(8)]*string }
`

const stringIdentityMain = `
fn saves(name: const u8[:], data: const u8[:], vals: schema_rt::LoadVal[>..]&, nodes: schema_rt::LoadNode[>..]&, root: i64) {
    var out: u8[>..<] = [];
    let r1 = schema_rt::save_table(conformance_unit(), "Root", data, vals, nodes, root, out);
    var again: u8[>..<] = [];
    let r2 = schema_rt::save_table(conformance_unit(), "Root", data, vals, nodes, root, again);
    write_stdout(str(name, " ", schema_rt::save_refusal_name(r1), "_ ", hex(out), " ", schema_rt::save_refusal_name(r2), "_ ", hex(again), "\n"));
}

// a slot of the root record: a = 0, b = 1
fn slot(vals: schema_rt::LoadVal[>..]&, root: i64, i: i64) -> i64 { return vals[root].first + i; }

fn identity(name: const u8[:], edit: const u8[:]) {
    let data = input(str(name, ".bin"));
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var report = schema_rt::LoadReport { .. };
    let root = schema_rt::load_table(conformance_unit(), "Root", data, vals, nodes, report);
    if root < 0 || report.body_damaged {
        write_stdout(str(name, "/", edit, " refused\n"));
        if edit == "plain" {
            var rv: schema_rt::LoadVal[>..] = [];
            var rn: schema_rt::LoadNode[>..] = [];
            var rr = schema_rt::LoadReport { .. };
            var rbytes: u8[>..<] = [];
            var rrecs: schema_rt::RetainRec[>..<] = [];
            var rids: u64[>..<] = [];
            var store = schema_rt::RetainStore { capacity: -1, id_capacity: -1, used: 0, bytes: rbytes, recs: rrecs, ids: rids };
            let rroot = schema_rt::load_table_retain(conformance_unit(), "Root", data, rv, rn, rr, store);
            if rroot < 0 || rr.body_damaged { write_stdout(str(name, "/retain refused\n")); }
            else { write_stdout(str(name, "/retain loaded\n")); }
        }
        return;
    }
    let a = vals[slot(vals, root, 0)].bits.lo as! i64;
    let b = vals[slot(vals, root, 1)].bits.lo as! i64;
    if edit == "view" {
        // the second node views the first node's bytes
        nodes[b - 2].off = nodes[a - 2].off;
        nodes[b - 2].len = nodes[a - 2].len;
    } else if edit == "copy" {
        // the second slot shares the first slot's node, then gets a node of its own over the same bytes
        vals[slot(vals, root, 1)].bits.lo = a as! u64;
        let n = nodes[a - 2];
        nodes.push(n);
        vals[slot(vals, root, 1)].bits.lo = (nodes.len + 1) as! u64;
    } else if edit == "one-node" {
        // the caller names the first slot's node from the second
        vals[slot(vals, root, 1)].bits.lo = a as! u64;
    }
    saves(str(name, "/", edit), data, vals, nodes, root);
    if edit != "plain" { return; }
    write_stdout(str(name, "/blob_shared ", report.blob_shared, "\n"));
    var rv: schema_rt::LoadVal[>..] = [];
    var rn: schema_rt::LoadNode[>..] = [];
    var rr = schema_rt::LoadReport { .. };
    var rbytes: u8[>..<] = [];
    var rrecs: schema_rt::RetainRec[>..<] = [];
    var rids: u64[>..<] = [];
    var store = schema_rt::RetainStore { capacity: -1, id_capacity: -1, used: 0, bytes: rbytes, recs: rrecs, ids: rids };
    let rroot = schema_rt::load_table_retain(conformance_unit(), "Root", data, rv, rn, rr, store);
    if rroot < 0 || rr.body_damaged { write_stdout(str(name, "/retain refused\n")); return; }
    var out: u8[>..<] = [];
    let r = schema_rt::save_table_retain(conformance_unit(), "Root", data, rv, rn, rroot, store, rr, out);
    write_stdout(str(name, "/retain ", schema_rt::save_refusal_name(r), "_ ", hex(out), " ", schema_rt::save_refusal_name(r), "_ ", hex(out), "\n"));
}

fn main() {
`

// identityRow is one saved row's expectation: the refusal name ("" for
// none) and, when it saves, the bytes.
type identityRow struct {
	refusal     string
	wire        []byte
	loadRefusal bool
}

// stringIdentityGate runs the gate and answers its red list.
func stringIdentityGate(t *testing.T, override map[string][]byte) []string {
	t.Helper()
	u := probeUnit(t, stringIdentitySchema)
	m := tabletext.NewModel(u)
	files := probeFiles(t, u, override)
	var main strings.Builder
	main.WriteString(probePrelude)
	main.WriteString(stringIdentityMain)
	want := map[string]identityRow{}
	inputs := []string{
		`{"a":"","b":""}`,
		`{"a":"same","b":"same"}`,
		`{"a":"","b":"","child":{"caption":""},"choice":{"text":""}}`,
		`{"a":"","b":null}`,
		`{"a":"","tags":{"one":"","two":""},"choice":{"text":""}}`,
		`{"a":"same","b":"same","child":{"caption":"same"},"choice":{"text":"same"},"tags":{"one":"same","two":"same"}}`,
	}
	for i, text := range inputs {
		v := m.New(u.Tables["Root"])
		var r tabletext.Report
		if !m.Read(v, []byte(text), &r) || r.Refused {
			t.Fatalf("input %d: %+v", i, r)
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatal(err)
		}
		name := fmt.Sprintf("decoded%d", i)
		files[name+".bin"] = wire
		fmt.Fprintf(&main, "    identity(%q, \"plain\");\n", name)
		want[name+"/plain"] = identityRow{wire: wire}
		want[name+"/retain"] = identityRow{wire: wire}
		want[name+"/blob_shared"] = identityRow{refusal: "0"}
		if text == `{"a":"same","b":"same"}` {
			fmt.Fprintf(&main, "    identity(%q, \"view\");\n", name)
			want[name+"/view"] = identityRow{wire: wire}
			fmt.Fprintf(&main, "    identity(%q, \"copy\");\n", name)
			want[name+"/copy"] = identityRow{wire: wire}
			fmt.Fprintf(&main, "    identity(%q, \"one-node\");\n", name)
			want[name+"/one-node"] = identityRow{refusal: "blob_shared"}
		}
	}
	// ONE empty-string node named from both root slots (the Dart leg's
	// committed shape): the reference reads it and counts one blob_shared,
	// and refuses to write it
	shared := []byte{1, 1, 17, 2, 2, 17, 2, 3, 12, 3, 1, 4, 0, 0}
	for _, id := range []uint64{ir.TableWireId("a"), ir.TableWireId("b"), ir.TableNodeWireId, ir.StringWireTypeId, 4} {
		shared = binary.LittleEndian.AppendUint64(shared, id)
	}
	v := m.New(u.Tables["Root"])
	var r tabletext.Report
	if ok, err := tablewire.Decode(m, v, shared, &r); ok || !tablewire.ReadRefusal(err) {
		t.Fatalf("the reference reads the shared string as %+v %v", r, err)
	}
	files["shared.bin"] = shared
	main.WriteString("    identity(\"shared\", \"plain\");\n    identity(\"shared\", \"copy\");\n")
	want["shared/plain"] = identityRow{loadRefusal: true}
	want["shared/copy"] = identityRow{loadRefusal: true}
	want["shared/retain"] = identityRow{loadRefusal: true}
	main.WriteString("}\n")
	files["main.goose"] = []byte(main.String())
	got := probeLines(gooserun.Run(t, t.TempDir(), "main.goose", files))
	var bad []string
	for name, w := range want {
		f, ok := got[name]
		switch {
		case !ok:
			bad = append(bad, name+": no answer")
		case w.loadRefusal:
			if len(f) != 1 || f[0] != "refused" {
				bad = append(bad, fmt.Sprintf("%s: the load did not refuse (%q)", name, f))
			}
		case len(f) == 1 && f[0] == "refused":
			bad = append(bad, name+": the load refused")
		case strings.HasSuffix(name, "/blob_shared"):
			if len(f) != 1 || f[0] != w.refusal {
				bad = append(bad, fmt.Sprintf("%s: the load counted %q, want %s", name, f, w.refusal))
			}
		case len(f) != 4:
			bad = append(bad, fmt.Sprintf("%s: the probe printed %q", name, f))
		default:
			for i := 0; i < 4; i += 2 {
				refusal := strings.TrimSuffix(f[i], "_")
				if refusal != w.refusal {
					bad = append(bad, fmt.Sprintf("%s: save %d answered %q, want %q", name, i/2+1, refusal, w.refusal))
				} else if refusal == "" && !bytes.Equal(probeHex(t, f[i+1]), w.wire) {
					bad = append(bad, fmt.Sprintf("%s: save %d is not the reference's bytes (first difference at %d)", name, i/2+1, firstDifference(probeHex(t, f[i+1]), w.wire)))
				}
			}
		}
	}
	return bad
}

func TestGooseStringIdentity(t *testing.T) {
	t.Parallel()
	for _, b := range stringIdentityGate(t, nil) {
		t.Error(b)
	}
}

// TestGooseStringIdentityNegativeControl breaks the save's identity twice:
//
//   - identity BY VIEW: a blob whose bytes view equals an earlier blob's is
//     refused as shared, so two nodes over equal text read as one: the view
//     row must go red;
//   - the blob_shared refusal is dropped, so one node named twice is written
//     once and referenced twice: the shared rows must go red.
func TestGooseStringIdentityNegativeControl(t *testing.T) {
	t.Parallel()
	for _, ctl := range []struct {
		name, from, to, red string
	}{
		{"by-view",
			"    c.order.push(wire);\n    c.index[wire] = c.order.len + 1;",
			"    if blob { for o in c.order { if o >= 2 && c.nodes[o - 2].blob != 0 && c.nodes[o - 2].off == c.nodes[wire - 2].off && c.nodes[o - 2].len == c.nodes[wire - 2].len { refuse(c, SaveRefuseBlobShared); return; } } }\n    c.order.push(wire);\n    c.index[wire] = c.order.len + 1;",
			"/view"},
		{"no-shared-refusal",
			"        if blob { refuse(c, SaveRefuseBlobShared); }\n",
			"",
			"/one-node"},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			bad := stringIdentityGate(t, sabotage(t, "wire_save.goose", ctl.from, ctl.to))
			owned := 0
			for _, b := range bad {
				if strings.Contains(b, ctl.red) {
					owned++
				}
			}
			if owned == 0 {
				t.Fatalf("%s: no %s row went red (%d findings: %q)", ctl.name, ctl.red, len(bad), bad)
			}
			t.Logf("%s: %d findings, %d on %s rows; first %s", ctl.name, len(bad), owned, ctl.red, bad[0])
		})
	}
}
