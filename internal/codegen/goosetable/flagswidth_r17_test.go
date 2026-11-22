package goosetable

// A TABLE LOAD HOLDS A MASK TO ITS WIDTH (docs/SPEC-TABLES.md §4; R17, Glenn
// 2026-11-22: "The newer reader can read old data, but never the other way").
// W is the flags declaration wire width (the variant count, or K under
// `| max = K`). Every table read refuses a mask with a bit at or above W as
// out_of_range: the variable load, the fixed load, and the message read of a
// newer writer that announces a wider mask. Every table writer refuses to
// write such a mask, in every build: Goose has no assert. Unit tests here use
// no real time, no socket and no network: each run is `goose run` of a
// generated program in t.TempDir().

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const gooseR17Schema = `package probe

flags Perms { PERMS }
flags Wide | max = 40 { A }

table Doc
{
    lead  uint32
    perms Perms
    mask  Wide
}

fixed table Rec
{
    lead  uint32
    perms Perms
    mask  Wide
    trail uint32
}
`

const gooseR17Hex = `fn hexw(b: const u8[:]) -> u8[>..] {
    let digits = "0123456789abcdef";
    var line: u8[>..] = [];
    for x in b {
        line.push(digits[(x >> 4) as! i64]);
        line.push(digits[(x & 15) as! i64]);
    }
    return line;
}

`

// gooseR17Sentinel is a lawful mask (37 bits, under W = 40) the probe finds
// in a saved file and patches.
const gooseR17Sentinel = 0x123456789a

func TestGooseFlagsMaskHeldToItsWidth(t *testing.T) {
	slowtest.Gate(t, "goose (it runs the generated table readers and writers)")
	t.Parallel()
	narrow := reviewUnit(t, strings.Replace(gooseR17Schema, "PERMS", "Read, Write", 1))
	wide := reviewUnit(t, strings.Replace(gooseR17Schema, "PERMS", "Read, Write, Exec", 1))
	ns := goose.Namespace(narrow)
	unit := ns + "::" + UnitName

	m := tabletext.NewModel(narrow)
	inst := m.New(narrow.Tables["Doc"])
	var rep tabletext.Report
	if !m.Read(inst, []byte(`{"lead":7,"perms":["Read","Write"],"mask":["A"]}`), &rep) || rep.Refused {
		t.Fatalf("the probe text: %+v", rep)
	}
	docIn, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}

	// The narrow build writes lawful files carrying the sentinel in the mask
	// slot, and each of its writers answers a mask past W.
	writes := []struct{ name, field, value string }{
		{"lawful", "mask", "0x123456789A"},
		{"perms4", "perms", "4"},
		{"bit40", "mask", "0x10000000000"},
		{"bit63", "mask", "0x8000000000000000"},
	}
	slot := map[string]int{"perms": 1, "mask": 2}
	var b strings.Builder
	b.WriteString(gooseR17Hex + "fn main() {\n    var data: u8[>..] = [];\n    if !read_file(\"in.bin\", data) { print(\"read-failed\"); return; }\n")
	for k, w := range writes {
		fmt.Fprintf(&b, "    var vals%d: schema_rt::LoadVal[>..] = [];\n    var nodes%d: schema_rt::LoadNode[>..] = [];\n    var report%d = schema_rt::LoadReport { .. };\n", k, k, k)
		fmt.Fprintf(&b, "    let root%d = schema_rt::load_table(%s, \"Doc\", data, vals%d, nodes%d, report%d);\n", k, unit, k, k, k)
		fmt.Fprintf(&b, "    if root%d < 0 || schema_rt::load_error(report%d, root%d) != 0 { print(\"load-refused\"); return; }\n", k, k, k)
		fmt.Fprintf(&b, "    vals%d[vals%d[root%d].first + %d].bits = schema_rt::Int128 { lo: %s, hi: 0 };\n", k, k, k, slot[w.field], w.value)
		fmt.Fprintf(&b, "    var out%d: u8[>..<] = [];\n", k)
		fmt.Fprintf(&b, "    let r%d = schema_rt::save_table(%s, \"Doc\", data, vals%d, nodes%d, root%d, out%d);\n", k, unit, k, k, k, k)
		fmt.Fprintf(&b, "    print(\"save_%s _\", schema_rt::save_refusal_name(r%d), \" \", hexw(out%d));\n", w.name, k, k)
		fmt.Fprintf(&b, "    var rec%d: %s::Rec[1] = [%s::Rec { .. }; 1];\n", k, ns, ns)
		fmt.Fprintf(&b, "    rec%[1]d[0].Lead = 7;\n    rec%[1]d[0].Perms = 3;\n    rec%[1]d[0].Mask = 0x123456789A;\n    rec%[1]d[0].Trail = 9;\n", k)
		fmt.Fprintf(&b, "    rec%d[0].%s = %s;\n", k, map[string]string{"perms": "Perms", "mask": "Mask"}[w.field], w.value)
		fmt.Fprintf(&b, "    var fb%d: u8[>..] = [];\n", k)
		fmt.Fprintf(&b, "    let fw%d = %s::RecFixedSave(rec%d[0..1], fb%d);\n", k, ns, k, k)
		fmt.Fprintf(&b, "    print(\"fixedsave_%s \", fw%d, \" \", fb%d.len, \" _\", hexw(fb%d));\n", w.name, k, k, k)
		fmt.Fprintf(&b, "    var mb%d: u8[>..] = [];\n    var mrep%d = schema_rt::MsgReport { .. };\n", k, k)
		fmt.Fprintf(&b, "    let mw%d = %s::RecSaveMessages(rec%d[0..1], mb%d, mrep%d);\n", k, ns, k, k, k)
		fmt.Fprintf(&b, "    print(\"msgsave_%s \", if mw%d < 0 { -1 } else { 1 }, \" _\", schema_rt::msg_reason_name(mrep%d.reason));\n", w.name, k, k)
	}
	b.WriteString("}\n")
	lines := reviewLines(runCustomFixedGoose(t, narrow, nil, b.String(), map[string][]byte{"in.bin": docIn}))
	for _, w := range writes {
		save, fixed, msg := lines["save_"+w.name], lines["fixedsave_"+w.name], lines["msgsave_"+w.name]
		if len(save) < 1 || len(fixed) < 3 || len(msg) < 2 {
			t.Fatalf("%s: the write probe printed save %q fixed %q msg %q", w.name, save, fixed, msg)
		}
		if w.name == "lawful" {
			if save[0] != "_" || fixed[0] == "-1" || msg[0] != "1" {
				t.Fatalf("the lawful write was refused: save %q fixed %q msg %q", save, fixed, msg)
			}
			continue
		}
		if save[0] != "_invalid_value" {
			t.Errorf("%s: the variable save answered %q, want invalid_value (a writer writes no mask its reader refuses)", w.name, save)
		}
		if fixed[0] != "-1" || fixed[1] != "0" {
			t.Errorf("%s: the fixed save answered %q, want -1 with nothing written", w.name, fixed)
		}
		if msg[0] != "-1" || msg[1] != "_invalid_value" {
			t.Errorf("%s: the message save answered %q, want -1 invalid_value", w.name, msg)
		}
	}
	files := map[string][]byte{}
	for key, line := range map[string][]string{"doc": lines["save_lawful"], "rec": lines["fixedsave_lawful"]} {
		h, err := hex.DecodeString(strings.TrimPrefix(line[len(line)-1], "_"))
		if err == nil && len(h) == 0 {
			err = fmt.Errorf("the lawful save printed %q", line)
		}
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		files[key] = h
	}

	// The newer build (Perms has a third flag) writes a message batch with the
	// new bit set and one without it.
	wns := goose.Namespace(wide)
	newer := reviewLines(runCustomFixedGoose(t, wide, nil, gooseR17Hex+strings.ReplaceAll(`fn main() {
    var v: NS::Rec[2] = [NS::Rec { .. }; 2];
    v[0].Perms = 3;
    v[1].Perms = 5;
    var a: u8[>..] = [];
    var ar = schema_rt::MsgReport { .. };
    if NS::RecSaveMessages(v[0..1], a, ar) < 0 { print("inside-refused"); return; }
    var p: u8[>..] = [];
    var pr = schema_rt::MsgReport { .. };
    if NS::RecSaveMessages(v[1..2], p, pr) < 0 { print("past-refused"); return; }
    print("msg_inside ", hexw(a));
    print("msg_past ", hexw(p));
}
`, "NS", wns), nil))
	for _, k := range []string{"msg_inside", "msg_past"} {
		if len(newer[k]) != 1 {
			t.Fatalf("the newer build printed %q", newer)
		}
		h, err := hex.DecodeString(newer[k][0])
		if err != nil {
			t.Fatal(err)
		}
		files[k] = h
	}

	var sentinel [8]byte
	binary.LittleEndian.PutUint64(sentinel[:], gooseR17Sentinel)
	inputs := map[string][]byte{"announce.bin": tablewire.Announce(wide), "msg_inside.bin": files["msg_inside"], "msg_past.bin": files["msg_past"]}
	values := []struct {
		name  string
		value uint64
	}{{"inside", gooseR17Sentinel}, {"past40", gooseR17Sentinel | 1<<40}, {"bit63", 1 << 63}}
	for _, name := range []string{"doc", "rec"} {
		f := files[name]
		if n := bytes.Count(f, sentinel[:]); n != 1 {
			t.Fatalf("%s: the mask slot appears %d times, want 1", name, n)
		}
		at := bytes.Index(f, sentinel[:])
		for _, v := range values {
			out := append([]byte(nil), f...)
			binary.LittleEndian.PutUint64(out[at:at+8], v.value)
			inputs[name+"_"+v.name+".bin"] = out
		}
	}

	// The narrow build reads each: a mask past its W refuses the whole load.
	b.Reset()
	b.WriteString("fn main() {\n")
	for k, v := range values {
		fmt.Fprintf(&b, "    var d%d: u8[>..] = [];\n    if !read_file(\"doc_%s.bin\", d%d) { print(\"read-failed\"); return; }\n", k, v.name, k)
		fmt.Fprintf(&b, "    var vals%d: schema_rt::LoadVal[>..] = [];\n    var nodes%d: schema_rt::LoadNode[>..] = [];\n    var report%d = schema_rt::LoadReport { .. };\n", k, k, k)
		fmt.Fprintf(&b, "    let root%d = schema_rt::load_table(%s, \"Doc\", d%d, vals%d, nodes%d, report%d);\n", k, unit, k, k, k, k)
		fmt.Fprintf(&b, "    if root%d < 0 || schema_rt::load_error(report%d, root%d) != 0 { print(\"load_%s refused \", schema_rt::load_refusal_name(schema_rt::load_error(report%d, root%d))); } else { print(\"load_%s read \", vals%d[vals%d[root%d].first + 2].bits.lo); }\n", k, k, k, v.name, k, k, v.name, k, k, k)
		fmt.Fprintf(&b, "    var f%d: u8[>..] = [];\n    if !read_file(\"rec_%s.bin\", f%d) { print(\"read-failed\"); return; }\n", k, v.name, k)
		fmt.Fprintf(&b, "    var rv%d: %s::Rec[1] = [%s::Rec { .. }; 1];\n    var frep%d = schema_rt::FixedReport { .. };\n", k, ns, ns, k)
		fmt.Fprintf(&b, "    let n%d = %s::RecFixedLoad(rv%d[0..1], f%d, frep%d);\n", k, ns, k, k, k)
		fmt.Fprintf(&b, "    if n%d == 1 { print(\"fixedload_%s read \", rv%d[0].Mask); } else { print(\"fixedload_%s refused \", schema_rt::fixed_refusal_name(frep%d.reason)); }\n", k, v.name, k, v.name, k)
	}
	b.WriteString("    var peer: u8[>..] = [];\n    if !read_file(\"announce.bin\", peer) { print(\"read-failed\"); return; }\n")
	b.WriteString("    var voc = schema_rt::MsgVocabulary { .. };\n    var vrep = schema_rt::MsgReport { .. };\n")
	b.WriteString("    if !schema_rt::announce_read(voc, peer, vrep) { print(\"announce-refused \", schema_rt::msg_reason_name(vrep.reason)); return; }\n")
	for k, name := range []string{"msg_inside", "msg_past"} {
		fmt.Fprintf(&b, "    var md%d: u8[>..] = [];\n    if !read_file(\"%s.bin\", md%d) { print(\"read-failed\"); return; }\n", k, name, k)
		fmt.Fprintf(&b, "    var back%d: %s::Rec[1] = [%s::Rec { .. }; 1];\n    var mrep%d = schema_rt::MsgReport { .. };\n", k, ns, ns, k)
		fmt.Fprintf(&b, "    let mn%d = %s::RecLoadMessages(back%d[0..1], voc, md%d, mrep%d);\n", k, ns, k, k, k)
		fmt.Fprintf(&b, "    if mn%d == 1 && mrep%d.reason == 0 { print(\"%s read \", back%d[0].Perms); } else { print(\"%s refused \", schema_rt::msg_reason_name(mrep%d.reason)); }\n", k, k, name, k, name, k)
	}
	b.WriteString("}\n")
	got := reviewLines(runCustomFixedGoose(t, narrow, nil, b.String(), inputs))
	want := map[string]string{
		"load_inside":      fmt.Sprintf("read %d", gooseR17Sentinel),
		"load_past40":      "refused out_of_range",
		"load_bit63":       "refused out_of_range",
		"fixedload_inside": fmt.Sprintf("read %d", gooseR17Sentinel),
		"fixedload_past40": "refused out_of_range",
		"fixedload_bit63":  "refused out_of_range",
		"msg_inside":       "read 3",
		"msg_past":         "refused out_of_range",
	}
	for k, w := range want {
		if g := strings.Join(got[k], " "); g != w {
			t.Errorf("%s: the narrow build answered %q, want %q", k, g, w)
		}
	}
}
