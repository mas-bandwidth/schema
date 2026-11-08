package goosetable_test

// WIDENING ON READ, AND THE REFUSAL REASONS, for Goose (docs/SPEC-TABLES.md
// §3, §3.3, §4), the gates the Go leg's cell names (gotable wire_test.go,
// maps_test.go), each held to the Go leg's pinned answers and to the reference
// engine's read (internal/tablewire Decode), which shares no code with the
// Goose runtime:
//
//	TestGooseWireSignalingNaNWideningAndLEBOverflow  a float32 payload under a
//	    float64 field: a NaN (signaling or quiet, any payload) or an infinity
//	    refuses the read as non_finite_float and lands nothing; a finite one
//	    (a subnormal and a negative zero included) widens EXACTLY and counts
//	    one widened. A LEB128 whose tenth byte carries a bit past 2^64 is
//	    refused at the place it starts
//	TestGooseWideningLadder  every wire scalar kind under every declared
//	    scalar kind (the 12 x 10 grid): the load's widened, kind_mismatch,
//	    clamped and verdict are the reference's, and what it landed saves to
//	    the reference's bytes
//	TestGooseMapReadReports  a map's widened key counts once per map, a
//	    repeated key counts duplicate, a descending key is damage that
//	    discards the whole load, and a key of another kind empties the map
//	    for one kind_mismatch
//
// The Go leg's TestMeasureRefusalReasons is LoadMeasure's (count_over_length,
// count_over_extent_cap, wire_damaged, unknown_form on the measure); Goose
// has no LoadMeasure (make/goose-fuzz.mk), and the load's own reason for each
// of those files is refusalreasons_test.go's.
//
// TestGooseWideningNegativeControl breaks the float rung of the ladder, the
// LEB's tenth-byte clause and the map's once-per-map widened count, and
// requires each to turn its own gate red.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// wideningResult is one load the probe printed.
type wideningResult struct {
	loaded                                        bool
	malformed                                     bool
	widened, mismatch, duplicate, clamped, length int64
	reason                                        string
	first                                         uint64 // the bits of the root's field 0
	saved                                         []byte
}

const wideningProbeMain = `
fn probe(name: const u8[:]) {
    let data = input(str(name, ".bin"));
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var report = schema_rt::LoadReport { .. };
    let root = schema_rt::load_table(conformance_unit(), "Root", data, vals, nodes, report);
    var first: u64 = 0;
    var length: i64 = 0;
    var out: u8[>..<] = [];
    if root >= 0 {
        let s = vals[root].first;
        first = vals[s].bits.lo;
        length = vals[s].len;
        if schema_rt::save_table(conformance_unit(), "Root", data, vals, nodes, root, out) != 0 { out.clear(); }
    }
    write_stdout(str(name, " ", (if root >= 0 { 1 } else { 0 }), " ", (if report.body_damaged { 1 } else { 0 }), " ",
        report.widened, " ", report.mismatch, " ", report.repeated, " ", report.oor, " ", length, " ",
        schema_rt::load_refusal_name(report.reason), "_ ", first, " ", hex(out), "\n"));
}

fn leb_tenth() {
    var bad: i64 = 0;
    for last in 128 {
        var data: u8[>..] = [];
        for i in 9 { data.push(0x80); }
        data.push(last as! u8);
        let v, next, ok = schema_rt::leb_at(data, 0, data.len);
        // one spelling only: a tenth byte of 1 is 2^63, 0 is non-minimal, and
        // every byte above 1 carries a bit past 2^64
        let want = last == 1;
        if ok != want || (ok && (v != 0x8000000000000000 || next != 10)) { bad += 1; write_stdout(str("tenth", last, " ", ok, " ", v, "\n")); }
    }
    write_stdout(str("leb-checked ", bad, "\n"));
}

fn main() {
`

// wideningProbe runs every named wire through one program for u.
func wideningProbe(t *testing.T, u *ir.Unit, wires map[string][]byte, leb bool, override map[string][]byte) (map[string]wideningResult, []string) {
	t.Helper()
	files := probeFiles(t, u, override)
	var main strings.Builder
	main.WriteString(probePrelude)
	main.WriteString(wideningProbeMain)
	for name, w := range wires {
		files[name+".bin"] = w
		fmt.Fprintf(&main, "    probe(%q);\n", name)
	}
	if leb {
		main.WriteString("    leb_tenth();\n")
	}
	main.WriteString("}\n")
	files["main.goose"] = []byte(main.String())
	got := map[string]wideningResult{}
	var lebRed []string
	for name, f := range probeLines(gooserun.Run(t, t.TempDir(), "main.goose", files)) {
		if last, ok := strings.CutPrefix(name, "tenth"); ok {
			lebRed = append(lebRed, last+" "+strings.Join(f, " "))
			continue
		}
		if name == "leb-checked" {
			continue
		}
		if len(f) != 10 {
			t.Fatalf("%s: the probe printed %q", name, f)
		}
		n := make([]int64, 7)
		for i := range 7 {
			v, err := strconv.ParseInt(f[i], 10, 64)
			if err != nil {
				t.Fatalf("%s: %q is not a count", name, f[i])
			}
			n[i] = v
		}
		first, err := strconv.ParseUint(f[8], 10, 64)
		if err != nil {
			t.Fatalf("%s: %q is not bits", name, f[8])
		}
		got[name] = wideningResult{
			loaded: n[0] == 1, malformed: n[1] == 1, widened: n[2], mismatch: n[3], duplicate: n[4], clamped: n[5], length: n[6],
			reason: strings.TrimSuffix(f[7], "_"), first: first, saved: probeHex(t, f[9]),
		}
	}
	return got, lebRed
}

// oneFieldWire is a file of one root field: the form byte, the field's
// reference, kind and payload, the terminator, and the id table.
func oneFieldWire(name string, kind byte, payload []byte) []byte {
	wire := append([]byte{1, 1, kind}, payload...)
	wire = append(wire, 0)
	wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId(name))
	return binary.LittleEndian.AppendUint64(wire, 1)
}

// signalingNaN is the float gate's red list.
func signalingNaN(t *testing.T, override map[string][]byte) []string {
	t.Helper()
	u := probeUnit(t, "package probe\ntable Root { value float64 }\n")
	refused := []uint32{0x7f800001, 0xff812345, 0x7fc12345, 0x7f800000, 0xff800000}
	widened := []uint32{0x3fc00000, 0x80000000, 0x007fffff, 0x80000001}
	wires := map[string][]byte{}
	for _, bits := range append(append([]uint32{}, refused...), widened...) {
		wires[fmt.Sprintf("f%08x", bits)] = oneFieldWire("value", 10, binary.LittleEndian.AppendUint32(nil, bits))
	}
	got, lebRed := wideningProbe(t, u, wires, true, override)
	var bad []string
	for _, b := range lebRed {
		bad = append(bad, "leb: tenth byte "+b)
	}
	for _, bits := range refused {
		g := got[fmt.Sprintf("f%08x", bits)]
		if g.loaded || g.reason != "non_finite_float" {
			bad = append(bad, fmt.Sprintf("float %#x: loaded %v, reason %q; want refused as non_finite_float", bits, g.loaded, g.reason))
		}
	}
	m := tabletext.NewModel(u)
	for _, bits := range widened {
		g := got[fmt.Sprintf("f%08x", bits)]
		want := math.Float64bits(float64(math.Float32frombits(bits)))
		if !g.loaded || g.malformed || g.widened != 1 || g.first != want {
			bad = append(bad, fmt.Sprintf("float %#x: loaded %v, widened %d, bits %#x; want widened once to %#x", bits, g.loaded, g.widened, g.first, want))
		}
		var r tabletext.Report
		if ok, err := tablewire.Decode(m, m.New(m.Lookup("Root")), wires[fmt.Sprintf("f%08x", bits)], &r); !ok || err != nil || r.Widened != 1 {
			t.Fatalf("the reference reads %#x as %+v %v", bits, r, err)
		}
	}
	return bad
}

func TestGooseWireSignalingNaNWideningAndLEBOverflow(t *testing.T) {
	t.Parallel()
	for _, b := range signalingNaN(t, nil) {
		t.Error(b)
	}
}

// ladderKinds are the scalar declarations and their wire kinds.
var ladderKinds = []struct {
	decl string
	kind byte
}{
	{"int8", 2}, {"int16", 3}, {"int32", 4}, {"int64", 5},
	{"uint8", 6}, {"uint16", 7}, {"uint32", 8}, {"uint64", 9},
	{"float32", 10}, {"float64", 11},
	{"int128 | min = -1267650600228229401496703205376, max = 1267650600228229401496703205376", 18}, {"uint128", 19},
}

// ladderPayload is a payload of a wire kind whose top bit is set, so a
// widening that extends by the wrong signedness is seen.
func ladderPayload(kind byte) []byte {
	switch kind {
	case 10:
		return binary.LittleEndian.AppendUint32(nil, math.Float32bits(-1.5))
	case 11:
		return binary.LittleEndian.AppendUint64(nil, math.Float64bits(-2.25))
	}
	width := map[byte]int{2: 1, 3: 2, 4: 4, 5: 8, 6: 1, 7: 2, 8: 4, 9: 8}[kind]
	p := bytes.Repeat([]byte{0xFF}, width)
	p[0] = 0xFD
	return p
}

// wideningLadder is the grid gate's red list.
func wideningLadder(t *testing.T, override map[string][]byte) []string {
	t.Helper()
	var bad []string
	widened := 0
	for _, decl := range ladderKinds {
		u := probeUnit(t, fmt.Sprintf("package probe\ntable Root { value %s }\n", decl.decl))
		m := tabletext.NewModel(u)
		wires := map[string][]byte{}
		for _, w := range ladderKinds[:10] {
			wires[fmt.Sprintf("k%d", w.kind)] = oneFieldWire("value", w.kind, ladderPayload(w.kind))
		}
		got, _ := wideningProbe(t, u, wires, false, override)
		for _, w := range ladderKinds[:10] {
			name := fmt.Sprintf("k%d", w.kind)
			g := got[name]
			v := m.New(m.Lookup("Root"))
			var r tabletext.Report
			ok, err := tablewire.Decode(m, v, wires[name], &r)
			if !ok || err != nil || r.Refused {
				t.Fatalf("the reference refuses %s under %s: %+v %v", name, decl.decl, r, err)
			}
			want, err := tablewire.Encode(m, v)
			if err != nil {
				t.Fatal(err)
			}
			at := fmt.Sprintf("kind %d under %s", w.kind, decl.decl)
			if !g.loaded || g.malformed || g.widened != int64(r.Widened) || g.mismatch != 0 || g.clamped != 0 {
				bad = append(bad, fmt.Sprintf("%s: loaded %v, widened %d, kind_mismatch %d, clamped %d; the reference %d, %d, %d",
					at, g.loaded, g.widened, g.mismatch, g.clamped, r.Widened, 0, 0))
				continue
			}
			if !bytes.Equal(g.saved, want) {
				bad = append(bad, fmt.Sprintf("%s: what landed saves to %x, the reference's value to %x", at, g.saved, want))
			}
			widened += r.Widened
		}
	}
	if widened == 0 {
		t.Fatalf("the grid exercised no widening; it must exercise one")
	}
	t.Logf("12 declarations x 10 wire kinds: %d widened", widened)
	return bad
}

func TestGooseWideningLadder(t *testing.T) {
	t.Skip("layer 4 (goose): tolerant-read test, re-pin as a verdict: docs/plans/strict-only.md")
	t.Parallel()
	for _, b := range wideningLadder(t, nil) {
		t.Error(b)
	}
}

// mapWire is the Go leg's map file (gotable maps_test.go, makeWire): the
// map's entries with these key bytes and kinds, each value int8 9, then
// `past` as int32 42.
func mapWire(keys, kinds []byte) []byte {
	ids := []string{"entries", "key", "value", "past"}
	b := []byte{1, 1, 14}
	b = binary.AppendUvarint(b, uint64(2+len(keys)*8))
	b = append(b, 13)
	b = binary.AppendUvarint(b, uint64(len(keys)))
	for i, k := range keys {
		b = append(b, 7, 2, kinds[i], k, 3, 2, 9, 0)
	}
	b = append(b, 4, 4)
	b = binary.LittleEndian.AppendUint32(b, 42)
	b = append(b, 0)
	for _, id := range ids {
		b = binary.LittleEndian.AppendUint64(b, ir.TableWireId(id))
	}
	return binary.LittleEndian.AppendUint64(b, uint64(len(ids)))
}

// mapReports is the map gate's red list.
func mapReports(t *testing.T, override map[string][]byte) []string {
	t.Helper()
	u := probeUnit(t, "package probe\ntable Root { entries map[int32]int32\n past int32 }\n")
	cases := []struct {
		name                        string
		keys, kinds                 []byte
		count, widen, dup, mismatch int64
		damaged                     bool
	}{
		{"ascending_widened", []byte{253, 2}, []byte{2, 2}, 2, 3, 0, 0, false},
		{"descending", []byte{2, 253}, []byte{2, 2}, 1, 2, 0, 0, true},
		{"repeated", []byte{2, 2}, []byte{2, 2}, 1, 3, 1, 0, false},
		{"key_kind_mismatch", []byte{253, 2}, []byte{2, 6}, 0, 2, 0, 1, false},
	}
	wires := map[string][]byte{}
	for _, c := range cases {
		wires[c.name] = mapWire(c.keys, c.kinds)
	}
	got, _ := wideningProbe(t, u, wires, false, override)
	m := tabletext.NewModel(u)
	var bad []string
	for _, c := range cases {
		g := got[c.name]
		var r tabletext.Report
		ok, err := tablewire.Decode(m, m.New(m.Lookup("Root")), wires[c.name], &r)
		if err != nil || r.Refused != c.damaged || int64(r.Widened) != c.widen {
			t.Fatalf("%s: the reference reads %v %+v %v, which is not the Go leg's pin", c.name, ok, r, err)
		}
		if c.damaged {
			if g.loaded || !g.malformed || g.widened != c.widen {
				bad = append(bad, fmt.Sprintf("%s: loaded %v, malformed %v, widened %d; damage discards the whole load, widened %d", c.name, g.loaded, g.malformed, g.widened, c.widen))
			}
			continue
		}
		if !g.loaded || g.malformed || g.length != c.count || g.widened != c.widen || g.duplicate != c.dup || g.mismatch != c.mismatch {
			bad = append(bad, fmt.Sprintf("%s: loaded %v, entries %d, widened %d, duplicate %d, kind_mismatch %d; want %d, %d, %d, %d",
				c.name, g.loaded, g.length, g.widened, g.duplicate, g.mismatch, c.count, c.widen, c.dup, c.mismatch))
		}
	}
	return bad
}

func TestGooseMapReadReports(t *testing.T) {
	t.Skip("layer 4 (goose): tolerant-read test, re-pin as a verdict: docs/plans/strict-only.md")
	t.Parallel()
	for _, b := range mapReports(t, nil) {
		t.Error(b)
	}
}

// TestGooseWideningNegativeControl:
//
//   - the ladder loses its float rung (f32 no longer widens into f64): the
//     float gate's finite rows and the grid go red;
//   - the LEB loses its tenth-byte clause: the float gate's LEB rows go red;
//   - a map counts a widened key per entry, not once per map: the map gate
//     goes red.
func TestGooseWideningNegativeControl(t *testing.T) {
	t.Parallel()
	for _, ctl := range []struct {
		name, from, to string
		gates          []func(*testing.T, map[string][]byte) []string
	}{
		{"float-rung", "        11 => kind == 10,\n", "", []func(*testing.T, map[string][]byte) []string{signalingNaN, wideningLadder}},
		{"leb-tenth", "        if i == 9 && b > 1 { return 0, o, false; }\n", "", []func(*testing.T, map[string][]byte) []string{signalingNaN}},
		{"map-key-once", "            if entry_widened && !widened { widened = true; c.report.widened += 1; }\n",
			"            if entry_widened { widened = true; c.report.widened += 1; }\n", []func(*testing.T, map[string][]byte) []string{mapReports}},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			if ctl.name != "leb-tenth" {
				t.Skip("layer 4 (goose): tolerant-read test, re-pin as a verdict: docs/plans/strict-only.md")
			}
			t.Parallel()
			o := sabotage(t, "wire_load.goose", ctl.from, ctl.to)
			for i, gate := range ctl.gates {
				bad := gate(t, o)
				if len(bad) == 0 {
					t.Fatalf("%s: gate %d stayed green", ctl.name, i)
				}
				t.Logf("%s: gate %d red, %d findings, first %s", ctl.name, i, len(bad), bad[0])
			}
		})
	}
}
