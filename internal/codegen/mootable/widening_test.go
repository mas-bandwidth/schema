package mootable_test

// WIDENING ON READ (docs/SPEC-TABLES.md §3, §3.3, §4) for moo, the gates the
// Go leg's cell names (gotable wire_test.go, maps_test.go), each held to the
// Go leg's pinned answers and to the reference engine's read
// (internal/tablewire Decode), which shares no code with the moo runtime:
//
//	TestMooWireSignalingNaNWideningAndLEBOverflow  a float32 payload under a
//	    float64 field: a NaN (signaling or quiet, any payload) or an infinity
//	    refuses the read as non_finite_float and lands nothing; a finite one
//	    (a subnormal and a negative zero included) widens EXACTLY, counts one
//	    widened, and saves to the reference's bytes. A LEB128 whose tenth byte
//	    carries a bit past 2^64 is refused (table_values.moo V.leb)
//	TestMooWideningLadder  every wire scalar kind under every declared scalar
//	    kind (the 12 x 10 grid): the load's widened, kind_mismatch and clamped
//	    are the reference's, and what it landed saves to the reference's bytes
//	TestMooMapReadReports  a map's widened key counts once per map, a
//	    repeated key counts duplicate, a descending key is damage that discards
//	    the whole load, and a key of another kind empties the map for one
//	    kind_mismatch
//
// TestMooFloat32WidensBitForBit (float_widen_test.go) holds the float rung's
// bit surgery to the hardware over 40000 patterns; the refusal reasons are
// refusalreasons_test.go's. TestMooWideningNegativeControl breaks the float
// rung, the LEB's tenth-byte clause and the map's once-per-map widened count,
// and requires each to turn its own gate red.

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// wideningResult is one load the probe printed.
type wideningResult struct {
	loaded, malformed                   bool
	widened, mismatch, duplicate, clamp int64
	reason                              string
	saved                               []byte
}

// wideningProbe loads each input at Root through loadTolerant and saves what
// landed; leb adds the tenth-byte sweep of table_values.moo's V.leb.
const wideningProbe = `local inputs = require("./inputs")
local load = require("./table_load")
local save = require("./table_save")
local V = require("./table_values")
local St = require("./schema_tables")
REQUIRES
local d = St.lookup("Root")
local function probe(name: string)
	local rep = load.newReport()
	local v = load.loadTolerant("Root", inputs[name], rep)
	local saved = "-"
	if v ~= nil then
		local out = save.save(d, v)
		if out ~= nil then
			local parts = table.create(buffer.len(out))
			for i = 0, buffer.len(out) - 1 do
				parts[i + 1] = string.format("%02x", buffer.readu8(out, i))
			end
			saved = "x" .. table.concat(parts)
		end
	end
	local reason = if rep.reason == nil then "-" else string.split(tostring(rep.reason), ":")[1]
	print(string.format("%s %d %d %d %d %d %d %s %s", name, if v ~= nil then 1 else 0, if rep.malformed then 1 else 0,
		rep.widened, rep.kindMismatch, rep.duplicate, rep.clamped, reason, saved))
end
local function lebTenth()
	-- one spelling only: a tenth byte of 1 is 2^63, 0 is non-minimal, and
	-- every byte above 1 carries a bit past 2^64
	for last = 0, 127 do
		local b = buffer.create(10)
		buffer.fill(b, 0, 0x80, 9)
		buffer.writeu8(b, 9, last)
		local v, nxt = V.leb(b, 0, 10)
		local ok = v ~= nil
		if ok ~= (last == 1) or (ok and nxt ~= 10) then
			print("tenth" .. last .. " " .. tostring(ok) .. " " .. tostring(nxt))
		end
	end
end
`

// wideningRun runs every named wire through one program for the schema; it
// answers the loads and the LEB sweep's red lines. mutate, when set, edits
// the emitted runtime first.
func wideningRun(t *testing.T, schema string, wires map[string][]byte, leb bool, mutate func(*testing.T, map[string][]byte)) (map[string]wideningResult, []string) {
	t.Helper()
	g := generateFiles(t, map[string]string{"Probe.schema": schema})
	if mutate != nil {
		files := maps.Clone(g.files)
		mutate(t, files)
		g = writeUnit(t, g.unit, files)
	}
	var main strings.Builder
	main.WriteString("--!strict\n" + strings.Replace(wideningProbe, "REQUIRES\n", g.requireAll(), 1))
	for _, name := range sortedKeys(wires) {
		fmt.Fprintf(&main, "probe(%q)\n", name)
	}
	if leb {
		main.WriteString("lebTenth()\n")
	}
	got := map[string]wideningResult{}
	var lebRed []string
	for line := range strings.SplitSeq(g.run(t, main.String(), wires), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if strings.HasPrefix(f[0], "tenth") {
			lebRed = append(lebRed, line)
			continue
		}
		if len(f) != 9 {
			t.Fatalf("the probe printed %q", line)
		}
		n := make([]int64, 6)
		for i := range 6 {
			v, err := strconv.ParseInt(f[i+1], 10, 64)
			if err != nil {
				t.Fatalf("%s: %q is not a count", f[0], f[i+1])
			}
			n[i] = v
		}
		r := wideningResult{loaded: n[0] == 1, malformed: n[1] == 1, widened: n[2], mismatch: n[3], duplicate: n[4], clamp: n[5], reason: f[7]}
		if f[8] != "-" {
			saved, err := hex.DecodeString(strings.TrimPrefix(f[8], "x"))
			if err != nil {
				t.Fatalf("%s: %q is not hex", f[0], f[8])
			}
			r.saved = saved
		}
		got[f[0]] = r
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

// referenceRead is the reference engine's read of wire at Root, and the
// bytes it writes back for what it read.
func referenceRead(t *testing.T, schema string, wire []byte) (tabletext.Report, []byte) {
	t.Helper()
	u := unitFromSource(t, schema)
	m := tabletext.NewModel(u)
	v := m.New(m.Lookup("Root"))
	var r tabletext.Report
	ok, err := tablewire.Decode(m, v, wire, &r)
	if err != nil || !ok || r.Malformed {
		return r, nil
	}
	saved, err := tablewire.Encode(m, v)
	if err != nil {
		t.Fatalf("the reference does not write what it read: %v", err)
	}
	return r, saved
}

// signalingNaN is the float gate's red list.
func signalingNaN(t *testing.T, mutate func(*testing.T, map[string][]byte)) []string {
	t.Helper()
	const schema = "package probe\nfixed table Root { value float64 }\n"
	refused := []uint32{0x7f800001, 0xff812345, 0x7fc12345, 0x7f800000, 0xff800000}
	widened := []uint32{0x3fc00000, 0x80000000, 0x007fffff, 0x80000001}
	wires := map[string][]byte{}
	for _, bits := range append(append([]uint32{}, refused...), widened...) {
		wires[fmt.Sprintf("f%08x", bits)] = oneFieldWire("value", 10, binary.LittleEndian.AppendUint32(nil, bits))
	}
	got, lebRed := wideningRun(t, schema, wires, true, mutate)
	var bad []string
	for _, l := range lebRed {
		bad = append(bad, "leb: tenth byte "+l)
	}
	for _, bits := range refused {
		g := got[fmt.Sprintf("f%08x", bits)]
		if g.loaded || g.reason != "non_finite_float" {
			bad = append(bad, fmt.Sprintf("float %#x: loaded %v, reason %q; want refused as non_finite_float", bits, g.loaded, g.reason))
		}
	}
	for _, bits := range widened {
		name := fmt.Sprintf("f%08x", bits)
		g := got[name]
		r, want := referenceRead(t, schema, wires[name])
		if r.Widened != 1 || want == nil {
			t.Fatalf("the reference reads %#x as %+v", bits, r)
		}
		if exact := binary.LittleEndian.AppendUint64(nil, math.Float64bits(float64(math.Float32frombits(bits)))); !bytes.Contains(want, exact) {
			t.Fatalf("the reference's save of %#x does not carry its exact widening", bits)
		}
		if !g.loaded || g.malformed || g.widened != 1 || !bytes.Equal(g.saved, want) {
			bad = append(bad, fmt.Sprintf("float %#x: loaded %v, widened %d, saved %x; want widened once, saved %x", bits, g.loaded, g.widened, g.saved, want))
		}
	}
	return bad
}

func TestMooWireSignalingNaNWideningAndLEBOverflow(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table runtime)")
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
func wideningLadder(t *testing.T, mutate func(*testing.T, map[string][]byte)) []string {
	t.Helper()
	var bad []string
	widened, mismatched := 0, 0
	for _, decl := range ladderKinds {
		schema := fmt.Sprintf("package probe\ntable Root { value %s }\n", decl.decl)
		wires := map[string][]byte{}
		for _, w := range ladderKinds[:10] {
			wires[fmt.Sprintf("k%d", w.kind)] = oneFieldWire("value", w.kind, ladderPayload(w.kind))
		}
		got, _ := wideningRun(t, schema, wires, false, mutate)
		for _, w := range ladderKinds[:10] {
			name := fmt.Sprintf("k%d", w.kind)
			g := got[name]
			r, want := referenceRead(t, schema, wires[name])
			if want == nil {
				t.Fatalf("the reference refuses %s under %s: %+v", name, decl.decl, r)
			}
			at := fmt.Sprintf("kind %d under %s", w.kind, decl.decl)
			if !g.loaded || g.malformed || g.widened != int64(r.Widened) || g.mismatch != int64(r.KindMismatch) || g.clamp != int64(r.Clamped) {
				bad = append(bad, fmt.Sprintf("%s: loaded %v, widened %d, kind_mismatch %d, clamped %d; the reference %d, %d, %d",
					at, g.loaded, g.widened, g.mismatch, g.clamp, r.Widened, r.KindMismatch, r.Clamped))
				continue
			}
			if !bytes.Equal(g.saved, want) {
				bad = append(bad, fmt.Sprintf("%s: what landed saves to %x, the reference's value to %x", at, g.saved, want))
			}
			widened += r.Widened
			mismatched += r.KindMismatch
		}
	}
	if widened == 0 || mismatched == 0 {
		t.Fatalf("the grid exercised %d widenings and %d mismatches; it must exercise both", widened, mismatched)
	}
	t.Logf("12 declarations x 10 wire kinds: %d widened, %d kind mismatches", widened, mismatched)
	return bad
}

func TestMooWideningLadder(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table runtime)")
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
func mapReports(t *testing.T, mutate func(*testing.T, map[string][]byte)) []string {
	t.Helper()
	const schema = "package probe\ntable Root { entries map[int32]int32\n past int32 }\n"
	cases := []struct {
		name                 string
		keys, kinds          []byte
		widen, dup, mismatch int64
		damaged              bool
	}{
		{"ascending_widened", []byte{253, 2}, []byte{2, 2}, 3, 0, 0, false},
		{"descending", []byte{2, 253}, []byte{2, 2}, 2, 0, 0, true},
		{"repeated", []byte{2, 2}, []byte{2, 2}, 3, 1, 0, false},
		{"key_kind_mismatch", []byte{253, 2}, []byte{2, 6}, 2, 0, 1, false},
	}
	wires := map[string][]byte{}
	for _, c := range cases {
		wires[c.name] = mapWire(c.keys, c.kinds)
	}
	got, _ := wideningRun(t, schema, wires, false, mutate)
	var bad []string
	for _, c := range cases {
		g := got[c.name]
		r, want := referenceRead(t, schema, wires[c.name])
		if r.Malformed != c.damaged || int64(r.Widened) != c.widen || int64(r.Duplicate) != c.dup || int64(r.KindMismatch) != c.mismatch {
			t.Fatalf("%s: the reference reads %+v, which is not the Go leg's pin", c.name, r)
		}
		if c.damaged {
			if g.loaded || !g.malformed || g.widened != c.widen {
				bad = append(bad, fmt.Sprintf("%s: loaded %v, malformed %v, widened %d; damage discards the whole load, widened %d", c.name, g.loaded, g.malformed, g.widened, c.widen))
			}
			continue
		}
		if !g.loaded || g.malformed || g.widened != c.widen || g.duplicate != c.dup || g.mismatch != c.mismatch || !bytes.Equal(g.saved, want) {
			bad = append(bad, fmt.Sprintf("%s: loaded %v, widened %d, duplicate %d, kind_mismatch %d, saved %x; want %d, %d, %d, saved %x",
				c.name, g.loaded, g.widened, g.duplicate, g.mismatch, g.saved, c.widen, c.dup, c.mismatch, want))
		}
	}
	return bad
}

func TestMooMapReadReports(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table runtime)")
	t.Parallel()
	for _, b := range mapReports(t, nil) {
		t.Error(b)
	}
}

// TestMooWideningNegativeControl:
//
//   - the ladder loses its float rung (f32 no longer widens into f64): the
//     float gate's finite rows and the grid go red;
//   - the LEB loses its tenth-byte clause: the float gate's LEB rows go red;
//   - a map counts a widened key per entry, not once per map: the map gate
//     goes red.
func TestMooWideningNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table runtime)")
	t.Parallel()
	plant := func(file, from, to string) func(*testing.T, map[string][]byte) {
		return func(t *testing.T, files map[string][]byte) {
			src := string(files[file])
			if strings.Count(src, from) != 1 {
				t.Fatalf("%s carries %q %d times, want 1: the control would be vacuous", file, from, strings.Count(src, from))
			}
			files[file] = []byte(strings.Replace(src, from, to, 1))
		}
	}
	for _, ctl := range []struct {
		name   string
		mutate func(*testing.T, map[string][]byte)
		gates  []func(*testing.T, func(*testing.T, map[string][]byte)) []string
	}{
		{"float-rung", plant("table_values.moo", "\tif declared == 11 then\n\t\treturn kind == 10\n\tend\n", ""),
			[]func(*testing.T, func(*testing.T, map[string][]byte)) []string{signalingNaN, wideningLadder}},
		{"leb-tenth", plant("table_values.moo", "\t\tif i == 9 and x > 1 then\n\t\t\treturn nil, at\n\t\tend\n", ""),
			[]func(*testing.T, func(*testing.T, map[string][]byte)) []string{signalingNaN}},
		{"map-key-once", plant("table_load.moo", "\t\t\tif entryWidened and not widened then\n", "\t\t\tif entryWidened then\n"),
			[]func(*testing.T, func(*testing.T, map[string][]byte)) []string{mapReports}},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			for i, gate := range ctl.gates {
				bad := gate(t, ctl.mutate)
				if len(bad) == 0 {
					t.Fatalf("%s: gate %d stayed green", ctl.name, i)
				}
				t.Logf("%s: gate %d red, %d findings, first %s", ctl.name, i, len(bad), bad[0])
			}
		})
	}
}
