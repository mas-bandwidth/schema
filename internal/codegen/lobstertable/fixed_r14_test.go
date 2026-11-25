package lobstertable

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// R14 ON THE LOBSTER LEG — A GROWN UNION TAG READS THE WRITER'S TAG WIDTH AND
// COMPILES WHOLE (docs/FIXED-FORM-ALGORITHM.md §5.2 EMIT kind 15: their_tag :=
// te.size - widest arm, my_tag := me.size - widest arm; FIRST, UNGUARDED, a
// `const 0` of my_tag bytes — None — whose read of the writer's raw tag is at
// THE WRITER'S tag width, so its source reach is src + their_tag).
//
// The Lobster leg compiles each lineage entry IN THE RUNTIME, at load
// (runtime/fixedevolve.lobster fixed_compile), against this reader's own
// layout and dst rows. The None const carries the writer's tag width in
// `meta` (fixed_compile_entry, case 15), its reach is `src + meta`
// (fixed_src_end), and the run's bounds pass reads the writer's tag at
// `meta` bytes (fixed_value_refusal).
//
// THE VECTOR (every leg's R14): THEIRS, a record of 5 — a union of tag 1 over
// one u32: [root,13,5,1], [u,15,5,1], [a,8,4,0]; MINE(tag), the same ids with
// the tag grown: [root,13,tag+4,1], [u,15,tag+4,1], [a,8,4,0], the reader's
// storage the union's tag at image 0 and the u32 at image `tag`.
//
//  1. THE RUNTIME'S OWN COMPILE: THEIRS vs MINE(8), MINE(2) and MINE(1)
//     compile WHOLE (fixed_compile answers entries, no refusal); the None
//     const is at src 0, lands `tag` bytes, reads the writer's tag at meta 1,
//     against the writer's 1 arm (dst_size), its reach fixed_src_end 1 of 5.
//  2. THE RUN (fixed_run) of the MINE(8) plan: [1, 44 33 22 11] lands an
//     8-byte tag of exactly 1 and the u32 0x11223344 past it, [0,..] None,
//     and a forged tag 2 refuses out_of_range.
//  3. THE LINEAGE LANE: a unit generated with THEIRS in its lineage reads it
//     through <Root>FixedLoad — MINE(1), the writer's own schema, and
//     MINE(2), the union grown to 256 arms (a tag of 8 needs more arms than
//     any layout holds) — and MINE(2) also reads a writer tag of 2 bytes over
//     ONE arm at 2 bytes (0x0101 = 257 refuses out_of_range), and refuses a
//     writer tag of 3 bytes, a width no union lays, layout_malformed.
func TestLobsterFixedR14GrownTagReadsWritersTagWidth(t *testing.T) {
	t.Parallel()
	const writer = "package probe\nunion U { a uint32 }\nfixed table VR { u U }\n"
	var arms strings.Builder
	for i := 1; i < 256; i++ {
		fmt.Fprintf(&arms, "  p%d\n", i)
	}
	mine2 := "package probe\nunion U {\n  a uint32\n" + arms.String() + "}\nfixed table VR { u U }\n"

	wU := lobsterUnitOf(t, writer)
	theirs, ok := FixedLineageOf(wU, "VR")
	if !ok {
		t.Fatal("R14: the writer builds no fixed lineage entry")
	}
	var wSt *ir.Struct
	for _, st := range ir.TableFixedFormRoots(wU) {
		if st.Name == "VR" {
			wSt = st
		}
	}
	if wSt == nil {
		t.Fatal("R14: the writer has no fixed root VR")
	}
	ids := lobsterR14IDs(t, theirs.Layout)
	if want := lobsterR14Layout(ids, 5); string(theirs.Layout) != string(want) {
		t.Fatalf("R14: the writer's layout is not the vector:\n got %x\nwant %x", theirs.Layout, want)
	}
	forged := func(size uint32) FixedLineageEntry {
		l := lobsterR14Layout(ids, size)
		return FixedLineageEntry{Wire: ir.TableFixedLayoutHash(l, wSt), Layout: l, Record: 8 + int64(size)}
	}
	wide, tag3 := forged(6), forged(7)

	type read struct {
		name  string
		entry FixedLineageEntry
		body  []byte
		want  string
	}
	vector := []read{
		{"arm1", theirs, []byte{1, 0x44, 0x33, 0x22, 0x11}, "1 - probe.UTypeA 287454020"},
		{"none", theirs, []byte{0, 0, 0, 0, 0}, "1 - probe.UTypeNone 0"},
		{"forged2", theirs, []byte{2, 0, 0, 0, 0}, "-1 out_of_range"},
	}
	readers := []struct {
		name   string
		src    string
		reads  []read
		handed []FixedLineageEntry
		direct bool
	}{
		{"MINE1", writer, vector, []FixedLineageEntry{theirs}, true},
		{"MINE2", mine2, append(append([]read(nil), vector...),
			read{"wide_arm1", wide, []byte{1, 0, 0x44, 0x33, 0x22, 0x11}, "1 - probe.UTypeA 287454020"},
			read{"wide_none", wide, []byte{0, 0, 0, 0, 0, 0}, "1 - probe.UTypeNone 0"},
			read{"wide_forged2", wide, []byte{2, 0, 0, 0, 0, 0}, "-1 out_of_range"},
			read{"wide_forged257", wide, []byte{1, 1, 0x44, 0x33, 0x22, 0x11}, "-1 out_of_range"},
			read{"tag3_malformed", tag3, []byte{1, 0, 0, 0x44, 0x33, 0x22, 0x11}, "-1 layout_malformed"},
		), []FixedLineageEntry{theirs, wide, tag3}, false},
	}
	for _, rd := range readers {
		t.Run(rd.name, func(t *testing.T) {
			t.Parallel()
			inputs := map[string][]byte{}
			var body strings.Builder
			for _, r := range rd.reads {
				inputs[r.name+".bin"] = lobsterR14File(r.entry.Wire, r.entry.Layout, r.body)
				fmt.Fprintf(&body, "    r14_read(%q)\n", r.name+".bin")
			}
			if rd.direct {
				inputs["theirs.lay"] = theirs.Layout
				inputs["zero12.img"] = make([]byte, 12)
				for _, tag := range []uint32{1, 2, 8} {
					inputs[fmt.Sprintf("mine%d.lay", tag)] = lobsterR14Layout(ids, tag+4)
					fmt.Fprintf(&body, "    r14_compile(%d)\n", tag)
				}
				for _, r := range vector {
					inputs[r.name+".rec"] = r.body
					fmt.Fprintf(&body, "    r14_run(%q)\n", r.name+".rec")
				}
			}
			out := runLobsterR14(t, lobsterUnitOf(t, rd.src), map[string][]FixedLineageEntry{"VR": rd.handed}, body.String(), inputs)
			got := map[string]string{}
			for l := range strings.SplitSeq(out, "\n") {
				if rest, ok := strings.CutPrefix(l, "R14 "); ok {
					name, val, _ := strings.Cut(rest, " ")
					got[name] = strings.TrimSpace(val)
				}
			}
			for _, r := range rd.reads {
				if got[r.name+".bin"] != r.want {
					t.Errorf("R14 %s/%s: read %q, want %q", rd.name, r.name, got[r.name+".bin"], r.want)
				}
			}
			if rd.direct {
				// made, the None const's src, size, meta (the WRITER's tag
				// width), dst_size (the writer's arm count) and its reach
				for _, tag := range []int{1, 2, 8} {
					name := fmt.Sprintf("compile%d", tag)
					if want := fmt.Sprintf("whole 0 %d 1 1 1", tag); got[name] != want {
						t.Errorf("R14 MINE(%d): the runtime's compile answered %q, want %q (made>0, None src 0, size %d, meta 1, arms 1, reach 1)", tag, got[name], want, tag)
					}
				}
				for name, want := range map[string]string{
					"arm1.rec":    "- 010000000000000044332211",
					"none.rec":    "- 000000000000000000000000",
					"forged2.rec": "out_of_range 000000000000000000000000",
				} {
					if got[name] != want {
						t.Errorf("R14 MINE(8) run %s: %q, want %q", name, got[name], want)
					}
				}
			}
			if t.Failed() {
				t.Logf("R14 %s probe output:\n%s", rd.name, out)
			}
		})
	}
}

// runLobsterR14 generates u with the handed lineage and runs body inside the
// probe's run(), beside r14_read (one file through VRFixedLoad), r14_compile
// (THEIRS against MINE(tag) through the runtime's own compile) and r14_run
// (the MINE(8) plan over one writer record).
func runLobsterR14(t *testing.T, u *ir.Unit, lineage map[string][]FixedLineageEntry, body string, inputs map[string][]byte) string {
	t.Helper()
	files, err := lobster.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	main := fmt.Sprintf(`import .ProbeTable
import .schema_runtime

namespace main_ns

def read_in(name:string) -> string:
    let f = read_file(name)
    if f: return f
    return ""

def hexs(s:string) -> string:
    var out = ""
    for(length(s)) i:
        let b = s[i] & 0xff
        out += "0123456789abcdef".substring(b >> 4, 1) + "0123456789abcdef".substring(b & 15, 1)
    return out

def r14_read(name:string):
    let data = read_in(name)
    let back:[%[1]s.VR] = [%[1]s.NewVR()]
    let r = schema_rt.TableReport {}
    let plan:[schema_rt.TableFixedEntry] = []
    for(4096): push(plan, schema_rt.TableFixedEntry {})
    let n = %[1]s.VRFixedLoad(back, data, plan, r)
    if n < 0:
        print "R14 " + name + " " + string(n) + " " + r.reason
        return
    var a = 0
    if back[0].U.Type == 1:
        a = back[0].U.A
    print "R14 " + name + " " + string(n) + " " + (if r.reason == "": "-" else: r.reason) + " " + string(back[0].U.Type) + " " + string(a)

def r14_dst(tag:int) -> [schema_rt.TableFixedDst]:
    return [schema_rt.TableFixedDst {}, schema_rt.TableFixedDst { dst: 0, aux: 0 }, schema_rt.TableFixedDst { dst: tag }]

def r14_plan(tag:int) -> [schema_rt.TableFixedEntry], int:
    let theirs, why = schema_rt.fixed_parse_layout(read_in("theirs.lay"))
    if why != "":
        return [], -100
    return schema_rt.fixed_compile(theirs, read_in("mine" + string(tag) + ".lay"), r14_dst(tag), 256)

def r14_compile(tag:int):
    let plan, made = r14_plan(tag)
    if made <= 0:
        print "R14 compile" + string(tag) + " refused " + string(made)
        return
    var found = false
    for(made) i:
        let e = plan[i]
        if not found and e.op == schema_rt.tableFixedConst and e.aux == 0 and e.dst_size != 0:
            found = true
            print "R14 compile" + string(tag) + " whole " + string(e.src) + " " + string(e.size) + " " + string(e.meta) + " " + string(e.dst_size) + " " + string(schema_rt.fixed_src_end(e))
    if not found:
        print "R14 compile" + string(tag) + " no-none-const"

def r14_run(name:string):
    let plan, made = r14_plan(8)
    if made <= 0:
        print "R14 " + name + " refused " + string(made)
        return
    let r = schema_rt.TableReport {}
    let img, why = schema_rt.fixed_run(plan, made, read_in(name), 0, read_in("zero12.img"), r)
    print "R14 " + name + " " + (if why == "": "-" else: why) + " " + hexs(img)

def run():
%[2]s

run()
`, u.Package, body)
	if err := os.WriteFile(filepath.Join(dir, "main.lobster"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	return string(lobsterrun.Run(t, dir, "main.lobster", inputs))
}

// lobsterR14IDs reads the root, union and arm ids out of the writer's own
// three-entry layout, so the vector names this build's own ids.
func lobsterR14IDs(t *testing.T, layout []byte) [3]uint64 {
	t.Helper()
	if len(layout) != 4+3*17 || binary.LittleEndian.Uint32(layout) != 3 {
		t.Fatalf("R14: the writer's layout is not three entries: %x", layout)
	}
	var ids [3]uint64
	for i := range ids {
		ids[i] = binary.LittleEndian.Uint64(layout[4+17*i:])
	}
	return ids
}

// lobsterR14Layout lays the vector's three entries: the root and the union of
// `size` bytes (the tag is size - 4) and the u32 arm (kind 8).
func lobsterR14Layout(ids [3]uint64, size uint32) []byte {
	out := binary.LittleEndian.AppendUint32(nil, 3)
	put := func(id uint64, kind uint8, size, children uint32) {
		out = binary.LittleEndian.AppendUint64(out, id)
		out = append(out, kind)
		out = binary.LittleEndian.AppendUint32(out, size)
		out = binary.LittleEndian.AppendUint32(out, children)
	}
	put(ids[0], 13, size, 1)
	put(ids[1], 15, size, 1)
	put(ids[2], 8, 4, 0)
	return out
}

// lobsterR14File frames one record under `hash` (§5.3): form byte 3, seven
// reserved zeros, the hash, the layout's length and bytes, the record's hash
// and body.
func lobsterR14File(hash uint64, layout, body []byte) []byte {
	out := []byte{3, 0, 0, 0, 0, 0, 0, 0}
	out = binary.LittleEndian.AppendUint64(out, hash)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(layout)))
	out = append(out, layout...)
	out = binary.LittleEndian.AppendUint64(out, hash)
	return append(out, body...)
}
