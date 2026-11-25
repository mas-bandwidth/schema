package goosetable

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// R14 ON THE GOOSE LEG — A GROWN UNION TAG READS THE WRITER'S TAG WIDTH AND
// COMPILES WHOLE (docs/FIXED-FORM-ALGORITHM.md §5.2 EMIT kind 15: their_tag :=
// te.size - widest arm, my_tag := me.size - widest arm; FIRST, UNGUARDED, a
// `const 0` of my_tag bytes — None — whose read of the writer's raw tag is at
// THE WRITER'S tag width, so its source reach is src + their_tag).
//
// The Goose leg compiles each lineage plan IN THE GO GENERATOR
// (fixedevolve.go compileEvolvePlan, evolveCompiler.union) and emits it as
// Goose source over a reader-layout image (emitNodes). The None tag is a
// nodeSet of curTag bytes that reads no record byte; the forged-tag check and
// every arm guard read the writer's tag at oldTag; the union's reach is
// checkSrc(oldOff, te.size), the writer's union inside the writer's record.
//
// THE VECTOR (every leg's R14): THEIRS, a record of 5 — a union of tag 1 over
// one u32: [root,13,5,1], [u,15,5,1], [a,8,4,0]; MINE(tag), the same ids with
// the tag grown: [root,13,tag+4,1], [u,15,tag+4,1], [a,8,4,0].
//
//  1. THEIRS vs MINE(8), MINE(2) and MINE(1) compile WHOLE through the leg's
//     own compiler (compileEvolvePlan), no refusal; the None nodeSet lands
//     MY tag bytes at 0, and the forged-tag read and the arm guard read the
//     WRITER's tag at 1 byte, the u32 copied from 1 to `tag`.
//  2. The MINE(8) plan RUNS: its emitted Goose (emitNodes, the leg's own
//     emitter) over a 12-byte reader image reads [1, 44 33 22 11] as an
//     8-byte tag of exactly 1 and the u32 0x11223344 past it, [0,..] as None,
//     and refuses a forged tag 2 out_of_range.
//  3. The generator's lineage lane: a unit generated with THEIRS in its
//     lineage reads it through <Root>FixedEvolveLoad — MINE(1), the writer's
//     own schema, and MINE(2), the union grown to 256 arms (a tag of 8 needs
//     more arms than any layout holds) — and MINE(2) also reads a writer tag
//     of 2 bytes over ONE arm at 2 bytes (0x0101 = 257 refuses out_of_range),
//     and refuses a writer tag of 3 bytes, a width no union lays,
//     layout_malformed.
func TestGooseFixedR14GrownTagReadsWritersTagWidth(t *testing.T) {
	t.Parallel()
	const writer = "package probe\nunion U { a uint32 }\nfixed table VR { u U }\n"
	var arms strings.Builder
	for i := 1; i < 256; i++ {
		fmt.Fprintf(&arms, "  p%d\n", i)
	}
	mine2 := "package probe\nunion U {\n  a uint32\n" + arms.String() + "}\nfixed table VR { u U }\n"

	wU := parseCheckUnit(t, writer)
	theirs, ok := FixedEvolutionOf(wU, "VR")
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
	ids := gooseR14IDs(t, theirs.Layout)
	if want := gooseR14Layout(ids, 5); string(theirs.Layout) != string(want) {
		t.Fatalf("R14: the writer's layout is not the vector:\n got %x\nwant %x", theirs.Layout, want)
	}

	// 1. THE COMPILE, through the leg's own compiler, for every MINE(tag).
	plans := map[int64]evolvePlan{}
	for _, tag := range []int64{1, 2, 8} {
		mine := gooseR14Layout(ids, uint32(tag+4))
		plan := compileEvolvePlan(wSt, theirs.Layout, mine)
		if plan.refuse != 0 || !plan.supported {
			t.Errorf("R14 MINE(%d): the grown tag refuses %d (supported %t), want a whole plan", tag, plan.refuse, plan.supported)
			continue
		}
		plans[tag] = plan
		var none, forged, guard *evolveNode
		for i := range plan.nodes {
			n := &plan.nodes[i]
			switch {
			case n.op == nodeSet && n.arg == 0 && none == nil:
				none = n
			case n.op == nodeForgedTag:
				forged = n
			case n.op == nodeIf && n.arg == 1:
				guard = n
			}
		}
		if none == nil || none.curOff != 0 || none.size != tag {
			t.Errorf("R14 MINE(%d): the None const does not land MY %d-byte tag at 0: %+v", tag, tag, none)
		}
		if forged == nil || forged.oldOff != 0 || forged.tagWidth != 1 || forged.count != 1 {
			t.Errorf("R14 MINE(%d): the None's read of the writer's tag is not at its 1-byte width, against its 1 arm: %+v", tag, forged)
		} else if forged.oldOff+forged.tagWidth > 5 {
			t.Errorf("R14 MINE(%d): the None's reach %d passes the writer's record of 5", tag, forged.oldOff+forged.tagWidth)
		}
		if guard == nil || guard.tagWidth != 1 {
			t.Errorf("R14 MINE(%d): the arm's guard does not read the writer's tag at 1 byte: %+v", tag, guard)
			continue
		}
		var copied bool
		for _, b := range guard.body {
			if b.op == nodeCopy && b.oldOff == 1 && b.curOff == tag && b.size == 4 {
				copied = true
			}
			if b.op == nodeSet && (b.arg != 1 || b.size != tag) {
				t.Errorf("R14 MINE(%d): the arm's tag const lands %d in %d bytes, want 1 in %d", tag, b.arg, b.size, tag)
			}
		}
		if !copied {
			t.Errorf("R14 MINE(%d): the u32 is not copied from the writer's 1 to my %d: %+v", tag, tag, guard.body)
		}
	}

	// 2. THE MINE(8) PLAN RUNS, emitted by the leg's own emitter.
	var planSrc string
	if p, ok := plans[8]; ok {
		g := goose.NewGen(wU, &ir.File{Base: "R14"}, map[string]string{})
		e := &fixedEvolveEmit{g: g}
		g.Pf("//R14-PLAN\n")
		seq := 0
		e.emitNodes(p.nodes, "body", "", "VR", &seq)
		_, planSrc, _ = strings.Cut(string(g.Assemble()), "//R14-PLAN\n")
	}

	// 3. THE GENERATOR'S LINEAGE LANE.
	forgedEntry := func(size uint32) FixedEvolutionEntry {
		l := gooseR14Layout(ids, size)
		return FixedEvolutionEntry{Wire: ir.TableFixedLayoutHash(l, wSt), Layout: l, Record: 8 + int64(size)}
	}
	wide, tag3 := forgedEntry(6), forgedEntry(7)
	type read struct {
		name  string
		entry FixedEvolutionEntry
		body  []byte
		want  string
	}
	vector := []read{
		{"arm1", theirs, []byte{1, 0x44, 0x33, 0x22, 0x11}, "1 ok 1 287454020"},
		{"none", theirs, []byte{0, 0, 0, 0, 0}, "1 ok 0 0"},
		{"forged2", theirs, []byte{2, 0, 0, 0, 0}, "-1 out_of_range"},
	}
	readers := []struct {
		name   string
		src    string
		reads  []read
		handed []FixedEvolutionEntry
		plan   bool
	}{
		{"MINE1", writer, vector, []FixedEvolutionEntry{theirs}, true},
		{"MINE2", mine2, append(append([]read(nil), vector...),
			read{"wide_arm1", wide, []byte{1, 0, 0x44, 0x33, 0x22, 0x11}, "1 ok 1 287454020"},
			read{"wide_none", wide, []byte{0, 0, 0, 0, 0, 0}, "1 ok 0 0"},
			read{"wide_forged2", wide, []byte{2, 0, 0, 0, 0, 0}, "-1 out_of_range"},
			read{"wide_forged257", wide, []byte{1, 1, 0x44, 0x33, 0x22, 0x11}, "-1 out_of_range"},
			read{"tag3_malformed", tag3, []byte{1, 0, 0, 0x44, 0x33, 0x22, 0x11}, "-1 layout_malformed"},
		), []FixedEvolutionEntry{theirs, wide, tag3}, false},
	}
	// THE WRITER'S TAG WIDTH OVER A STRUCT ARM: the widest arm is found over
	// the union's CHILDREN, each a subtree, so a struct arm's own fields never
	// stand in for the next arm. The writer's union is tag 1 + the u64 (9
	// bytes); stepping one entry at a time took `s.x` for the second arm,
	// found a widest of 2 and a tag of 7.
	t.Run("struct_arm", func(t *testing.T) {
		t.Parallel()
		const old = "package probe\ntype Inner { x uint8\n y uint8 }\nunion U { s Inner\n w uint64 }\nfixed table VR { u U }\n"
		const cur = "package probe\ntype Inner { x uint8\n y uint8 }\nunion U { s Inner\n w uint64\n z uint8 }\nfixed table VR { u U }\n"
		e, ok := FixedEvolutionOf(parseCheckUnit(t, old), "VR")
		if !ok {
			t.Fatal("R14 struct_arm: the writer builds no fixed lineage entry")
		}
		files := map[string][]byte{
			"w.bin": makeFixedFile(e.Wire, e.Layout, []byte{2, 8, 7, 6, 5, 4, 3, 2, 1}),
			"s.bin": makeFixedFile(e.Wire, e.Layout, []byte{1, 0x11, 0x22, 0, 0, 0, 0, 0, 0}),
		}
		out := runCustomFixedGoose(t, parseCheckUnit(t, cur), map[string][]FixedEvolutionEntry{"VR": {e}}, `
fn r14_read(name: const u8[:]) {
    var bytes: u8[>..] = [];
    if !read_file(name, bytes) { print("read-failed"); return; }
    var values: probe::VR[1] = [probe::VR { .. }; 1];
    var rep = schema_rt::FixedEvolveReport { .. };
    let n = probe::VRFixedEvolveLoad(values, bytes, rep);
    print("R14 ", name, " ", n, " ", schema_rt::fixed_evolve_refusal_name(rep.reason), " ", values[0].U.Type, " ", values[0].U.W, " ", values[0].U.S.X, " ", values[0].U.S.Y);
}

fn main() {
    r14_read("w.bin");
    r14_read("s.bin");
}
`, files)
		want := "R14 w.bin 1 ok 2 72623859790382856 0 0\nR14 s.bin 1 ok 1 0 17 34"
		if out != want {
			t.Errorf("R14 struct_arm: read\n%s\nwant\n%s", out, want)
		}
	})

	for _, rd := range readers {
		t.Run(rd.name, func(t *testing.T) {
			t.Parallel()
			files := map[string][]byte{}
			var main strings.Builder
			main.WriteString(`
fn r14_read(name: const u8[:]) {
    var bytes: u8[>..] = [];
    if !read_file(name, bytes) { print("read-failed"); return; }
    var values: probe::VR[1] = [probe::VR { .. }; 1];
    var rep = schema_rt::FixedEvolveReport { .. };
    let n = probe::VRFixedEvolveLoad(values, bytes, rep);
    if n < 0 {
        print("R14 ", name, " ", n, " ", schema_rt::fixed_evolve_refusal_name(rep.reason));
        return;
    }
    var a: u64 = 0;
    if values[0].U.Type == 1 { a = values[0].U.A as u64; }
    print("R14 ", name, " ", n, " ", schema_rt::fixed_evolve_refusal_name(rep.reason), " ", values[0].U.Type, " ", a);
}
`)
			if rd.plan && planSrc != "" {
				fmt.Fprintf(&main, `
fn r14_plan(data: const u8[:], img: u8[>..]&, report: schema_rt::FixedEvolveReport&) -> i64 {
    let body: i64 = 0;
    var forged: i64 = 0;
%s
    if forged != 0 { return schema_rt::fixed_evolve_refuse(report, schema_rt::FixedRefuseOutOfRange, false); }
    return 0;
}

fn r14_run(name: const u8[:]) {
    var data: u8[>..] = [];
    if !read_file(name, data) { print("read-failed"); return; }
    var img: u8[>..] = [];
    var z: i64 = 0;
    while z < 12 { img.push(0); z += 1; }
    var rep = schema_rt::FixedEvolveReport { .. };
    let r = r14_plan(data, img, rep);
    print("PLAN ", name, " ", r, " ", schema_rt::fixed_evolve_refusal_name(rep.reason));
    hex(img);
}
`, planSrc)
			}
			main.WriteString("\nfn main() {\n")
			for _, r := range rd.reads {
				files[r.name+".bin"] = makeFixedFile(r.entry.Wire, r.entry.Layout, r.body)
				fmt.Fprintf(&main, "    r14_read(%q);\n", r.name+".bin")
			}
			if rd.plan && planSrc != "" {
				for _, r := range vector {
					files["plan_"+r.name+".rec"] = r.body
					fmt.Fprintf(&main, "    r14_run(%q);\n", "plan_"+r.name+".rec")
				}
			}
			main.WriteString("}\n")
			out := runCustomFixedGoose(t, parseCheckUnit(t, rd.src), map[string][]FixedEvolutionEntry{"VR": rd.handed}, main.String(), files)
			lines := strings.Split(out, "\n")
			got := map[string]string{}
			for i, l := range lines {
				if rest, ok := strings.CutPrefix(l, "R14 "); ok {
					name, val, _ := strings.Cut(rest, " ")
					got[strings.TrimSuffix(name, ".bin")] = val
				}
				if rest, ok := strings.CutPrefix(l, "PLAN "); ok && i+1 < len(lines) {
					name, val, _ := strings.Cut(rest, " ")
					got[strings.TrimSuffix(name, ".rec")] = val + " " + lines[i+1]
				}
			}
			for _, r := range rd.reads {
				if got[r.name] != r.want {
					t.Errorf("R14 %s/%s: read %q, want %q", rd.name, r.name, got[r.name], r.want)
				}
			}
			if rd.plan {
				// THE MINE(8) PLAN: my 8-byte tag lands EXACTLY 1, its high
				// bytes zero, and the u32 whole past it.
				for name, want := range map[string]string{
					"plan_arm1":    "0 ok 010000000000000044332211",
					"plan_none":    "0 ok 000000000000000000000000",
					"plan_forged2": "-1 out_of_range " + strings.Repeat("0", 24),
				} {
					if got[name] != want {
						t.Errorf("R14 MINE(8) plan %s: ran %q, want %q", name, got[name], want)
					}
				}
			}
			if t.Failed() {
				t.Logf("R14 %s probe output:\n%s", rd.name, out)
			}
		})
	}
}

// gooseR14IDs reads the root, union and arm ids out of the writer's own
// three-entry layout, so the vector names this build's own ids.
func gooseR14IDs(t *testing.T, layout []byte) [3]uint64 {
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

// gooseR14Layout lays the vector's three entries: the root and the union of
// `size` bytes (the tag is size - 4) and the u32 arm (kind 8).
func gooseR14Layout(ids [3]uint64, size uint32) []byte {
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
