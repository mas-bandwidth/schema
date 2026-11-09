package goosetable_test

// THE GOOSE VARIABLE-TABLE CELLS UNDER THE STRICT READ (docs/SPEC-TABLES.md
// §3, §3.1, §3.3, §4, §6.5, §7, §7.6, §16.1, §16.7): one read answers the
// whole value or a refusal. Every wire, text, cook and message vector is
// checked against the Go reference (internal/tablewire, internal/tabletext,
// internal/tablecook) before the generated Goose unit and runtime are asked.
//
// This is a functional test: it invokes the real Goose interpreter
// (gooserun.Run) over the generated unit and runtime.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const gooseVtCellsSchema = `package probe

enum Slot {
    alpha
    beta
    gamma | was = "third"
}

fixed table Spot {
    x   float32
    y   float32 = 1.5
    tag uint8 | min = 0, max = 100
}

fixed table Config {
    scale   float32 = 1.0
    name    string(16) = "hi"
    spot    Spot
    keyed   [Slot]int32
}

table Sealed | integrity = "crc32c" {
    tag int32 = 5
}

table Leaf | was = "Node" {
    id   uint32
    name string(8)
    next *Leaf
}

union Choice {
    xs      [..4]int32
    leaf    *Leaf
    spot    Spot
    caption string(8)
}

table Root {
    spot    Spot
    cfg     Config
    renamed int32 | was = "former"
    wide    int128 | min = -100000000000000000000, max = 100000000000000000000
    text    string(16)
    caption string(16)
    mood    Slot = beta
    keyed   [Slot]int32
    xs      []int32
    raw     bytes(4)
    bounded [..3]Spot
    ptrs    []*Leaf
    choice  Choice
    choices []Choice
    opt     ?[..4]int32
    lookup  map[int32]int32
    head    *Leaf
    blob    *bytes
    note_a  *string
    note_b  *string
}
`

type gooseVtWireVec struct {
	name     string
	wire     []byte
	want     bool
	wantWire []byte
}

func buildGooseVtWireVectors(t *testing.T, u *ir.Unit) ([]gooseVtWireVec, []byte, []byte, []byte, []byte) {
	t.Helper()
	m := tabletext.NewModel(u)
	encodeJSON := func(root, text string) []byte {
		inst := m.New(m.Lookup(root))
		var rep tabletext.Report
		if !m.Read(inst, []byte(text), &rep) || rep.Refused {
			t.Fatalf("reference rejected valid JSON %s: %+v", text, rep)
		}
		w, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("reference encode %s: %v", text, err)
		}
		return w
	}

	makeWire := func(build func(ref func(uint64) []byte) []byte) []byte {
		var ids []uint64
		ref := func(id uint64) []byte {
			for i, v := range ids {
				if v == id {
					return binary.AppendUvarint(nil, uint64(i+1))
				}
			}
			ids = append(ids, id)
			return binary.AppendUvarint(nil, uint64(len(ids)))
		}
		body := append(build(ref), 0)
		wire := append([]byte{1}, body...)
		for _, id := range ids {
			wire = binary.LittleEndian.AppendUint64(wire, id)
		}
		return binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
	}
	framed := func(ref func(uint64) []byte, name string, kind byte, payload []byte) []byte {
		out := append(ref(ir.TableWireId(name)), kind)
		out = binary.AppendUvarint(out, uint64(len(payload)))
		return append(out, payload...)
	}
	ptrField := func(ref func(uint64) []byte, name string, index uint64) []byte {
		out := append(ref(ir.TableWireId(name)), byte(ir.TableKindPointer))
		return binary.AppendUvarint(out, index)
	}
	type rec struct {
		tid  uint64
		body []byte
	}
	nodeTable := func(ref func(uint64) []byte, recs ...rec) []byte {
		nt := binary.AppendUvarint(nil, uint64(len(recs)))
		for _, r := range recs {
			nt = append(nt, ref(r.tid)...)
			nt = append(nt, binary.AppendUvarint(nil, uint64(len(r.body)))...)
			nt = append(nt, r.body...)
		}
		out := append(ref(math.MaxUint64), byte(ir.TableKindString))
		out = binary.AppendUvarint(out, uint64(len(nt)))
		return append(out, nt...)
	}
	leafTID := ir.TableWireId(u.Tables["Leaf"].WireName())

	validRichWire := encodeJSON("Root", `{"spot":{"x":1.5,"y":2.5,"tag":7},"cfg":{"scale":2,"name":"cfg","spot":{"x":3,"y":4,"tag":8},"keyed":{"alpha":10,"gamma":30}},"renamed":42,"wide":-12345678901234567890,"text":"hello","caption":"мир","mood":"gamma","keyed":{"alpha":1,"beta":2,"gamma":3},"xs":[5,6],"raw":"AAECAw==","bounded":[{"x":1,"y":2,"tag":1}],"ptrs":[{"id":1,"name":"a","next":null},null],"choice":{"spot":{"x":9,"y":10,"tag":3}},"choices":[{"xs":[1,2]},{"caption":"hi"}],"opt":[7,8],"lookup":{"3":30,"5":50},"head":{"id":9,"name":"h","next":{"id":10,"name":"n","next":null}},"blob":"AAEC","note_a":"same","note_b":"same"}`)

	vecs := []gooseVtWireVec{
		{
			name: "valid-rich",
			want: true,
			wire: validRichWire,
		},
		{
			name: "valid-defaults",
			want: true,
			wire: encodeJSON("Root", `{}`),
		},
		{
			name: "widened-i8",
			want: true,
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				return append(append(ref(ir.TableWireId("former")), byte(ir.TableKindI8)), 7)
			}),
			wantWire: encodeJSON("Root", `{"renamed":7}`),
		},
		{
			name: "kind-mismatch",
			want: false,
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				// former (renamed) is int32 (kind 4), wire carries string (kind 12)
				return framed(ref, "former", byte(ir.TableKindString), []byte("bad"))
			}),
		},
		{
			name: "repeated-field-id",
			want: false,
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				f1 := append(append(ref(ir.TableWireId("former")), byte(ir.TableKindI32)), 1, 0, 0, 0)
				f2 := append(append(ref(ir.TableWireId("former")), byte(ir.TableKindI32)), 2, 0, 0, 0)
				return append(f1, f2...)
			}),
		},
		{
			name: "inert-body-repeated-id",
			want: false,
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				f1 := append(append(ref(ir.TableWireId("inner_x")), byte(ir.TableKindI32)), 1, 0, 0, 0)
				f2 := append(append(ref(ir.TableWireId("inner_x")), byte(ir.TableKindI32)), 2, 0, 0, 0)
				inner := append(append(f1, f2...), 0)
				return framed(ref, "unknown_table", byte(ir.TableKindTable), inner)
			}),
		},
		{
			name: "inert-body-short-array",
			want: false,
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				// unknown array field with 1-byte payload (missing element count in 2-byte header)
				return framed(ref, "unknown_arr", byte(ir.TableKindArray), []byte{byte(ir.TableKindI32)})
			}),
		},
		{
			name: "scalar-oor",
			want: false,
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				// spot.tag has max = 100; 200 is out of range
				tag := append(append(ref(ir.TableWireId("tag")), byte(ir.TableKindU8)), 200, 0)
				return framed(ref, "spot", byte(ir.TableKindTable), tag)
			}),
		},
		{
			name: "blob-shared",
			want: false,
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				// both note_a and note_b point at node 2 (one *string record)
				var b []byte
				b = append(b, ptrField(ref, "note_a", 2)...)
				b = append(b, ptrField(ref, "note_b", 2)...)
				b = append(b, nodeTable(ref, rec{ir.StringWireTypeId, []byte("shared")})...)
				return b
			}),
		},
		{
			name: "unreachable-node",
			want: false,
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				return nodeTable(ref, rec{leafTID, []byte{0}})
			}),
		},
		{
			name: "pointer-cycle",
			want: false,
			wire: makeWire(func(ref func(uint64) []byte) []byte {
				var b []byte
				b = append(b, ptrField(ref, "head", 2)...)
				selfNext := append(ptrField(ref, "next", 2), 0)
				b = append(b, nodeTable(ref, rec{leafTID, selfNext})...)
				return b
			}),
		},
	}

	for _, v := range vecs {
		var rep tabletext.Report
		ok, err := tablewire.Decode(m, m.New(u.Tables["Root"]), v.wire, &rep)
		if ok != v.want || rep.Refused == v.want {
			t.Fatalf("oracle mismatch on %s: ok=%v rep=%+v err=%v, want %v", v.name, ok, rep, err, v.want)
		}
		if !v.want && !tablewire.ReadRefusal(err) {
			t.Fatalf("oracle refusal on %s is not ReadRefusal: %v", v.name, err)
		}
	}

	sealedOK := encodeJSON("Sealed", `{"tag":9}`)
	sealedBad := bytes.Clone(sealedOK)
	sealedBad[len(sealedBad)-1] ^= 0xff

	inst := m.New(u.Tables["Root"])
	var rrep tabletext.Report
	if ok, err := tablewire.Decode(m, inst, validRichWire, &rrep); !ok || err != nil || rrep.Refused {
		t.Fatalf("decode validRichWire: %v %+v", err, rrep)
	}
	wantCook, err := tablecook.Cook(m, inst, tablecook.Options{})
	if err != nil {
		t.Fatalf("cook validRichWire: %v", err)
	}
	wantSharedJSONWire := encodeJSON("Root", `{"head":{"&node":1,"id":9,"name":"h","next":null},"ptrs":[{"&node":1}]}`)
	return vecs, sealedOK, sealedBad, wantCook, wantSharedJSONWire
}

func buildGooseVtMessageVectors(t *testing.T, u *ir.Unit) (announce []byte, validBatch, dupFieldBatch, dupKeyBatch, damagedUnknownBatch []byte) {
	t.Helper()
	announce = ir.TableAnnouncement(u)
	var vocab tablewire.Vocabulary
	var arep tabletext.Report
	if err := vocab.AnnounceRead(announce, &arep); err != nil {
		t.Fatalf("announce: %v", err)
	}
	m := tabletext.NewModel(u)

	// Build bit-packed batches against vocab's refBits.
	refBits := vocab.RefBits()
	slotOf := func(id uint64) uint64 {
		for i, e := range vocab.Entries() {
			if e.Id == id {
				return uint64(i + 1)
			}
		}
		t.Fatalf("id %#x not in vocab", id)
		return 0
	}
	packBits := func(write func(put func(v uint64, n int))) []byte {
		var out []byte
		var cur uint64
		var used int
		put := func(v uint64, n int) {
			left := n
			x := v
			for left > 0 {
				space := 8 - used
				take := min(left, space)
				mask := (uint64(1) << uint(take)) - 1
				cur |= (x & mask) << uint(used)
				used += take
				left -= take
				x >>= uint(take)
				if used == 8 {
					out = append(out, byte(cur))
					cur, used = 0, 0
				}
			}
		}
		write(put)
		if used > 0 {
			put(0, 8-used)
		}
		return out
	}

	scaleSlot := slotOf(ir.TableWireId("scale"))
	keyedSlot := slotOf(ir.TableWireId("keyed"))
	alphaSlot := slotOf(ir.TableWireId("alpha"))
	unknownSlot := slotOf(ir.TableWireId("former")) // int32 field from Root, unknown to Config

	// validBatch: Config body naming `scale` once.
	validBatch = packBits(func(put func(uint64, int)) {
		put(2, 8)
		put(0, 8)
		put(scaleSlot, refBits)
		put(uint64(math.Float32bits(2.5)), 32)
		put(0, refBits)
	})
	var vrep tabletext.Report
	if n, ok, err := tablewire.DecodeMessages(m, []*tabletext.Instance{m.New(m.Lookup("Config"))}, validBatch, &vocab, &vrep); !ok || n != 1 || vrep.Refused || err != nil {
		t.Fatalf("oracle on validBatch: n=%d ok=%v rep=%+v err=%v", n, ok, vrep, err)
	}

	// dupFieldBatch: Config body naming `scale` twice.
	dupFieldBatch = packBits(func(put func(uint64, int)) {
		put(2, 8) // form 2
		put(0, 8) // 1 body (count - 1 = 0)
		put(scaleSlot, refBits)
		put(uint64(math.Float32bits(2.0)), 32)
		put(scaleSlot, refBits)
		put(uint64(math.Float32bits(3.0)), 32)
		put(0, refBits)
	})

	// dupKeyBatch: Config body with `keyed` having 2 slots both naming `alpha`.
	dupKeyBatch = packBits(func(put func(uint64, int)) {
		put(2, 8)
		put(0, 8)
		put(keyedSlot, refBits)
		put(2, 2) // 2 present slots (bits_required(0, 3) == 2)
		put(alphaSlot, refBits)
		put(10, 32)
		put(alphaSlot, refBits)
		put(20, 32)
		put(0, refBits)
	})

	// damagedUnknownBatch: Config body naming an unknown field (`former`), then truncated before terminator.
	damagedUnknownBatch = packBits(func(put func(uint64, int)) {
		put(2, 8)
		put(0, 8)
		put(unknownSlot, refBits)
		put(42, 32)
		// non-zero pad / missing terminator -> truncated when we strip or corrupt
		put(scaleSlot, refBits)
	})

	for name, b := range map[string][]byte{
		"dupFieldBatch":       dupFieldBatch,
		"dupKeyBatch":         dupKeyBatch,
		"damagedUnknownBatch": damagedUnknownBatch,
	} {
		var rep tabletext.Report
		n, ok, err := tablewire.DecodeMessages(m, []*tabletext.Instance{m.New(m.Lookup("Config"))}, b, &vocab, &rep)
		if ok || n != 0 || !rep.Refused || rep.Unknown != 0 {
			t.Fatalf("oracle on %s: n=%d ok=%v rep=%+v err=%v", name, n, ok, rep, err)
		}
	}
	return announce, validBatch, dupFieldBatch, dupKeyBatch, damagedUnknownBatch
}

func runGooseVtCellsStrictGate(t *testing.T, override map[string][]byte) []string {
	t.Helper()
	u := probeUnit(t, gooseVtCellsSchema)
	wireVecs, sealedOK, sealedBad, wantCook, wantSharedJSONWire := buildGooseVtWireVectors(t, u)
	announce, validBatch, dupFieldBatch, dupKeyBatch, damagedUnknownBatch := buildGooseVtMessageVectors(t, u)

	files := probeFiles(t, u, override)
	var main strings.Builder
	main.WriteString(probePrelude)
	for name := range files {
		if m, ok := strings.CutSuffix(name, ".goose"); ok && m != "conformance_unit" && m != "wire_load" && m != "wire_retain" && m != "wire_save" {
			fmt.Fprintf(&main, "import .%s;\n", m)
		}
	}
	files["announce.bin"] = announce
	files["msg_valid.bin"] = validBatch
	files["msg_dup_field.bin"] = dupFieldBatch
	files["msg_dup_key.bin"] = dupKeyBatch
	files["msg_dmg_unknown.bin"] = damagedUnknownBatch
	files["sealed_ok.bin"] = sealedOK
	files["sealed_bad.bin"] = sealedBad
	for _, v := range wireVecs {
		files[v.name+".bin"] = v.wire
	}
	main.WriteString(`
fn check_wire(name: const u8[:]) {
    let data = input(str(name, ".bin"));
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var report = schema_rt::LoadReport { .. };
    let root = schema_rt::load_table(conformance_unit(), "Root", data, vals, nodes, report);
    var rv: schema_rt::LoadVal[>..] = [];
    var rn: schema_rt::LoadNode[>..] = [];
    var rr = schema_rt::LoadReport { .. };
    var rbytes: u8[>..<] = [];
    var rrecs: schema_rt::RetainRec[>..<] = [];
    var rids: u64[>..<] = [];
    var store = schema_rt::RetainStore { capacity: -1, id_capacity: -1, used: 0, bytes: rbytes, recs: rrecs, ids: rids };
    let rroot = schema_rt::load_table_retain(conformance_unit(), "Root", data, rv, rn, rr, store);
    if root < 0 || report.refused || report.body_damaged {
        let r_refused = rroot < 0 || rr.refused || rr.body_damaged;
        write_stdout(str("wire/", name, " refused ", r_refused, "\n"));
        return;
    }
    var out: u8[>..<] = [];
    let sr = schema_rt::save_table(conformance_unit(), "Root", data, vals, nodes, root, out);
    var rout: u8[>..<] = [];
    let rsr = schema_rt::save_table_retain(conformance_unit(), "Root", data, rv, rn, rroot, store, rr, rout);
    write_stdout(str("wire/", name, " ok ", sr == 0 && rsr == 0 && hex(out) == hex(rout), " ", hex(out), "\n"));
}

fn check_shared_save() {
    let data = input("valid-rich.bin");
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var report = schema_rt::LoadReport { .. };
    let root = schema_rt::load_table(conformance_unit(), "Root", data, vals, nodes, report);
    if root < 0 { write_stdout("save/shared load-refused\n"); return; }
    // Root fields note_a and note_b are the last two slots (17 and 18).
    let base = vals[root].first;
    vals[base + 18].bits.lo = vals[base + 17].bits.lo;
    var out: u8[>..<] = [];
    let sr = schema_rt::save_table(conformance_unit(), "Root", data, vals, nodes, root, out);
    write_stdout(str("save/shared ", schema_rt::save_refusal_name(sr), "_\n"));
}

fn check_crc() {
    let ok_data = input("sealed_ok.bin");
    var v1: schema_rt::LoadVal[>..] = [];
    var n1: schema_rt::LoadNode[>..] = [];
    var r1 = schema_rt::LoadReport { .. };
    let root1 = schema_rt::load_table(conformance_unit(), "Sealed", ok_data, v1, n1, r1);
    write_stdout(str("crc/ok ", root1 >= 0 && !r1.refused && !r1.body_damaged, "\n"));

    let bad_data = input("sealed_bad.bin");
    var v2: schema_rt::LoadVal[>..] = [];
    var n2: schema_rt::LoadNode[>..] = [];
    var r2 = schema_rt::LoadReport { .. };
    let root2 = schema_rt::load_table(conformance_unit(), "Sealed", bad_data, v2, n2, r2);
    write_stdout(str("crc/bad ", root2 < 0 && r2.body_damaged && r2.reason == schema_rt::LoadRefuseIntegrityMismatch, "\n"));
}

fn check_cook() {
    let data = input("valid-rich.bin");
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var report = schema_rt::LoadReport { .. };
    let root = schema_rt::load_table(conformance_unit(), "Root", data, vals, nodes, report);
    let need = probe::RootCookMeasure(data, vals, nodes, root);
    if need <= 0 {
        write_stdout("cook/valid false false false -\n");
        return;
    }
    var cooked: u8[>..] = [];
    for _ in need { cooked.push(0); }
    let ok = probe::RootCook(data, vals, nodes, root, cooked, false);
    let open0 = probe::RootCookOpen(cooked, 0);
    let open1 = probe::RootCookOpen(cooked, 1);
    write_stdout(str("cook/valid ", ok, " ", open0, " ", open1, " ", hex(cooked), "\n"));
}

fn check_json(name: const u8[:], text: const u8[:]) {
    var data: u8[>..] = [];
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var rep = schema_rt::json_report();
    let root = schema_rt::json_load_table(conformance_unit(), probe::TextUnit, "Root", text, data, vals, nodes, rep);
    var cfg = probe::NewConfig();
    var crep = schema_rt::json_report();
    let cok = probe::ConfigFromJson(cfg, text, crep);
    write_stdout(str("json/", name, " ", root >= 0 && !rep.damaged, " ", cok && !crep.damaged, "\n"));
}

fn check_json_var() {
    let shared_text = "{\"head\":{\"&node\":1,\"id\":9,\"name\":\"h\",\"next\":null},\"ptrs\":[{\"&node\":1}]}";
    var data: u8[>..] = [];
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var rep = schema_rt::json_report();
    let root = schema_rt::json_load_table(conformance_unit(), probe::TextUnit, "Root", shared_text, data, vals, nodes, rep);
    if root < 0 || rep.damaged {
        write_stdout("json_var/share refused -\n");
    } else {
        var jout: u8[>..] = [];
        let jw = schema_rt::json_save_table(conformance_unit(), probe::TextUnit, "Root", data, vals, nodes, root, jout);
        var data2: u8[>..] = [];
        var vals2: schema_rt::LoadVal[>..] = [];
        var nodes2: schema_rt::LoadNode[>..] = [];
        var rep2 = schema_rt::json_report();
        let root2 = schema_rt::json_load_table(conformance_unit(), probe::TextUnit, "Root", jout, data2, vals2, nodes2, rep2);
        var wout: u8[>..<] = [];
        var sr: i64 = -1;
        if jw >= 0 && root2 >= 0 && !rep2.damaged {
            sr = schema_rt::save_table(conformance_unit(), "Root", data2, vals2, nodes2, root2, wout);
        }
        write_stdout(str("json_var/share ", sr == 0, " ", hex(wout), "\n"));
    }
    let dup_label = "{\"head\":{\"&node\":1,\"id\":9,\"name\":\"h\",\"next\":{\"&node\":1,\"id\":2,\"name\":\"b\",\"next\":null}}}";
    var ddata: u8[>..] = [];
    var dvals: schema_rt::LoadVal[>..] = [];
    var dnodes: schema_rt::LoadNode[>..] = [];
    var drep = schema_rt::json_report();
    let droot = schema_rt::json_load_table(conformance_unit(), probe::TextUnit, "Root", dup_label, ddata, dvals, dnodes, drep);
    write_stdout(str("json_var/dup_label ", droot < 0 && drep.damaged, "\n"));
}

fn check_msg(name: const u8[:]) {
    let peer = input("announce.bin");
    var voc = schema_rt::MsgVocabulary { .. };
    var vrep = schema_rt::MsgReport { .. };
    if !schema_rt::announce_read(voc, peer, vrep) {
        write_stdout(str("msg/", name, " announce-failed\n"));
        return;
    }
    let data = input(str("msg_", name, ".bin"));
    var values: probe::Config[2] = [probe::Config { .. }; 2];
    var rep = schema_rt::MsgReport { .. };
    let n = probe::ConfigLoadMessages(values[0..2], voc, data, rep);
    write_stdout(str("msg/", name, " ", n, " ", schema_rt::msg_reason_name(rep.reason), " ", rep.damaged, " ", rep.unknown, "\n"));
}

fn main() {
`)
	for _, v := range wireVecs {
		fmt.Fprintf(&main, "    check_wire(%q);\n", v.name)
	}
	main.WriteString(`    check_shared_save();
    check_crc();
    check_cook();
    check_json("valid", "{\"spot\":{\"x\":1,\"y\":2,\"tag\":3}}");
    check_json("mismatch", "{\"spot\":\"not-an-object\"}");
    check_json("oor", "{\"spot\":{\"x\":1,\"y\":2,\"tag\":250}}");
    check_json("duplicate", "{\"spot\":{\"x\":1},\"spot\":{\"x\":2}}");
    check_json_var();
    check_msg("valid");
    check_msg("dup_field");
    check_msg("dup_key");
    check_msg("dmg_unknown");
}
`)
	files["main.goose"] = []byte(main.String())
	got := probeLines(gooserun.Run(t, t.TempDir(), "main.goose", files))

	var bad []string
	for _, v := range wireVecs {
		f := got["wire/"+v.name]
		if !v.want {
			if len(f) != 2 || f[0] != "refused" || f[1] != "true" {
				bad = append(bad, fmt.Sprintf("wire/%s: got %v, want [refused true]", v.name, f))
			}
			continue
		}
		if len(f) != 3 || f[0] != "ok" || f[1] != "true" {
			bad = append(bad, fmt.Sprintf("wire/%s: got %v, want [ok true <hex>]", v.name, f))
			continue
		}
		wantW := v.wire
		if v.wantWire != nil {
			wantW = v.wantWire
		}
		if !bytes.Equal(probeHex(t, f[2]), wantW) {
			bad = append(bad, fmt.Sprintf("wire/%s: saved bytes differ from reference", v.name))
		}
	}
	if f := got["save/shared"]; len(f) != 1 || f[0] != "blob_shared_" {
		bad = append(bad, fmt.Sprintf("save/shared: got %v, want [blob_shared_]", f))
	}
	if f := got["crc/ok"]; len(f) != 1 || f[0] != "true" {
		bad = append(bad, fmt.Sprintf("crc/ok: got %v, want [true]", f))
	}
	if f := got["crc/bad"]; len(f) != 1 || f[0] != "true" {
		bad = append(bad, fmt.Sprintf("crc/bad: got %v, want [true]", f))
	}
	if f := got["cook/valid"]; len(f) != 4 || f[0] != "true" || f[1] != "true" || f[2] != "false" || !bytes.Equal(probeHex(t, f[3]), wantCook) {
		bad = append(bad, fmt.Sprintf("cook/valid: got %v, want [true true false <wantCook>]", f))
	}
	for _, jc := range []struct {
		name string
		want string
	}{
		{"valid", "true true"},
		{"mismatch", "false false"},
		{"oor", "false false"},
		{"duplicate", "false false"},
	} {
		if f := strings.Join(got["json/"+jc.name], " "); f != jc.want {
			bad = append(bad, fmt.Sprintf("json/%s: got %q, want %q", jc.name, f, jc.want))
		}
	}
	if f := got["json_var/share"]; len(f) != 2 || f[0] != "true" || !bytes.Equal(probeHex(t, f[1]), wantSharedJSONWire) {
		bad = append(bad, fmt.Sprintf("json_var/share: got %v, want [true <wantSharedJSONWire>]", f))
	}
	if f := got["json_var/dup_label"]; len(f) != 1 || f[0] != "true" {
		bad = append(bad, fmt.Sprintf("json_var/dup_label: got %v, want [true]", f))
	}
	for _, mc := range []struct {
		name string
		want string
	}{
		{"valid", "1 ok false 0"},
		{"dup_field", "0 ok true 0"},
		{"dup_key", "0 ok true 0"},
		{"dmg_unknown", "0 ok true 0"},
	} {
		if f := strings.Join(got["msg/"+mc.name], " "); f != mc.want {
			bad = append(bad, fmt.Sprintf("msg/%s: got %q, want %q", mc.name, f, mc.want))
		}
	}
	return bad
}

func TestGooseVariableCellsStrict(t *testing.T) {
	slowtest.Gate(t, "goose (functional test: runs the real Goose interpreter over variable-table strict vectors)")
	t.Parallel()
	for _, b := range runGooseVtCellsStrictGate(t, nil) {
		t.Error(b)
	}
}

func TestGooseVariableCellsStrictNegativeControl(t *testing.T) {
	slowtest.Gate(t, "goose (functional test: negative controls for variable-table strict read)")
	t.Parallel()
	u := probeUnit(t, gooseVtCellsSchema)
	tables, err := goosetable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	plantIn := func(t *testing.T, file, from, to string) map[string][]byte {
		t.Helper()
		src, ok := tables[file]
		if !ok {
			src = goosetable.Runtime()[file]
		}
		if strings.Count(string(src), from) != 1 {
			t.Fatalf("%s carries %q %d times, want 1", file, from, strings.Count(string(src), from))
		}
		return map[string][]byte{file: []byte(strings.Replace(string(src), from, to, 1))}
	}

	controls := []struct {
		name              string
		file, from, to    string
		wantRedSubstrings []string
	}{
		{
			name:              "wire-kind-mismatch",
			file:              "wire_load.goose",
			from:              "    if report.mismatch > 0 { return LoadRefuseKindMismatch; }\n",
			to:                "",
			wantRedSubstrings: []string{"wire/kind-mismatch:"},
		},
		{
			name:              "wire-repeated-id",
			file:              "wire_load.goose",
			from:              "    if prev == scope { return false; }\n",
			to:                "",
			wantRedSubstrings: []string{"wire/repeated-field-id:", "wire/inert-body-repeated-id:"},
		},
		{
			name:              "wire-inert-body",
			file:              "wire_load.goose",
			from:              "    let wfail, wnon = wire_check(file, 1, first, first, count as! i64, false);\n",
			to:                "    let wfail, wnon = 0, false;\n",
			wantRedSubstrings: []string{"wire/inert-body-repeated-id:", "wire/inert-body-short-array:", "wire/unreachable-node:", "wire/pointer-cycle:"},
		},
		{
			name:              "wire-blob-shared-save",
			file:              "wire_save.goose",
			from:              "        if blob { refuse(c, SaveRefuseBlobShared); }\n",
			to:                "",
			wantRedSubstrings: []string{"save/shared:"},
		},
		{
			name:              "wire-crc32c",
			file:              "wire_load.goose",
			from:              "        if !integrity_holds(data, 1) {\n",
			to:                "        if false {\n",
			wantRedSubstrings: []string{"crc/bad:"},
		},
		{
			name:              "cook-open-align",
			file:              "cook_form.goose",
			from:              "    if ((lead as u64) + data_offset) % alignment != 0 { return TableRefuseUnalignedBase; }\n",
			to:                "",
			wantRedSubstrings: []string{"cook/valid:"},
		},
		{
			name:              "cook-write-dir",
			file:              "cook_write.goose",
			from:              "        cook_put(out, at + 8, cook_node_id(c, wire), 8, big);\n",
			to:                "",
			wantRedSubstrings: []string{"cook/valid:"},
		},
		{
			name:              "json-strict-refusal",
			file:              "schema_json.goose",
			from:              "    if good && (jr.rep.mismatch > 0 || jr.rep.oor > 0 || jr.rep.repeated > 0) { good = false; }\n",
			to:                "",
			wantRedSubstrings: []string{"json/mismatch:", "json/oor:", "json/duplicate:"},
		},
		{
			name:              "json-var-share",
			file:              "json_value.goose",
			from:              "        jr_point(c, s, l.wire);\n",
			to:                "        jr_null(c, s);\n",
			wantRedSubstrings: []string{"json_var/share:"},
		},
		{
			name:              "msg-repeated-id",
			file:              "message.goose",
			from:              "        if s == id {\n            r.ok = false;\n            return true;\n        }\n",
			to:                "",
			wantRedSubstrings: []string{"msg/dup_field:", "msg/dup_key:"},
		},
	}

	for _, ctl := range controls {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			bad := runGooseVtCellsStrictGate(t, plantIn(t, ctl.file, ctl.from, ctl.to))
			joined := strings.Join(bad, "\n")
			for _, sub := range ctl.wantRedSubstrings {
				if !strings.Contains(joined, sub) {
					t.Errorf("%s: expected finding matching %q, got:\n%s", ctl.name, sub, joined)
				}
			}
		})
	}
}
