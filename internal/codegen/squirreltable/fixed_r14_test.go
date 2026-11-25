package squirreltable_test

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// R14 ON THE SQUIRREL LEG — A GROWN UNION TAG READS THE WRITER'S TAG WIDTH
// AND COMPILES WHOLE (docs/FIXED-FORM-ALGORITHM.md §5.2 EMIT kind 15: their_tag
// := te.size - widest arm; the union's None const reads the writer's raw tag
// at THE WRITER'S tag width, never the reader's, so its source reach is src +
// their_tag, inside the writer's record).
//
// The Squirrel leg compiles a lineage plan in the GO GENERATOR
// (fixedlineage.go emitFixedPlanDecodeValue): each locked layout becomes
// Squirrel source that reads the writer's record into this reader's values.
// A Squirrel union holds its tag as an integer (`.Type`), so the reader has no
// tag WIDTH of its own: the only width the plan ever reads at is the writer's,
// and the row holds that it is the writer's LAYOUT width — the union entry's
// size less its widest arm — never one derived from anything else.
//
// THE VECTOR (the R14 vector every leg's row uses): THEIRS, the writer, a
// record of 5 — a union of tag 1 over one u32: [root,13,5,1], [u,15,5,1],
// [a,8,4,0]. The readers: MINE(1), the same schema (the identity read), and
// MINE(2), the union grown to 256 arms so its tag is 2 bytes — the real
// growth a schema can make (a tag of 8 needs more arms than any layout holds;
// a Squirrel reader holds its tag as an integer at any width). Both read the
// writer's records:
//
//	[1, 0x44,0x33,0x22,0x11]  tag 1, the u32 0x11223344 (the payload is never the tag)
//	[0, 0,0,0,0]              tag 0, None
//	[2, 0,0,0,0]              a forged tag past the writer's one arm: out_of_range (§4.6)
//
// THE WRITER-WIDTH CASE: a writer layout whose tag is 2 bytes over ONE arm,
// [root,13,6,1], [u,15,6,1], [a,8,4,0], read by MINE(2). Its tag width is the
// layout's (6 - 4 = 2), not the one its arm count would give (1): the plan
// reads the tag at 2 bytes and the u32 at offset 2. Until 2026-11-25 the
// Squirrel plan derived the width from the writer's ARM COUNT, read the tag at
// 1 byte and the u32 at offset 1, and refused the lawful record wire_damaged.
// A tag of 0x0101 there is 257, past the one arm, and refuses out_of_range.
// A writer union whose tag would be 3 bytes is no width a union lays: that
// entry refuses layout_malformed.
func TestSquirrelFixedR14GrownTagReadsWritersTagWidth(t *testing.T) {
	t.Parallel()
	if _, err := sqrun.RepoRoot(); err != nil {
		t.Fatal(err)
	}
	const writer = "package vr\nunion U { a uint32 }\nfixed table VR { u U }\n"
	var arms strings.Builder
	for i := 1; i < 256; i++ {
		fmt.Fprintf(&arms, "  p%d\n", i)
	}
	mine2 := "package vr\nunion U {\n  a uint32\n" + arms.String() + "}\nfixed table VR { u U }\n"

	wU := fixedSynthetic(t, writer)
	theirs, ok := squirreltable.FixedLineageOf(wU, "VR")
	if !ok {
		t.Fatal("R14: the writer builds no fixed lineage entry")
	}
	ids := r14LayoutIDs(t, theirs.Layout)
	// THE VECTOR IS THE WRITER'S OWN LAYOUT, entry for entry: a record of 5,
	// a union of 5 (tag 1 + the u32), the u32 (kind 8, 4 bytes).
	wantTheirs := r14Layout(ids, 5, 5)
	if string(theirs.Layout) != string(wantTheirs) {
		t.Fatalf("R14: the writer's layout is not the vector:\n got %x\nwant %x", theirs.Layout, wantTheirs)
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
	// THE WRITER-WIDTH LAYOUT: a 2-byte tag over the one u32 arm.
	wide := r14Layout(ids, 6, 6)
	wideEntry := squirreltable.FixedLineageEntry{
		Wire:   ir.TableFixedLayoutHash(wide, wSt),
		Layout: wide,
		Record: 8 + 6,
	}
	if wideEntry.Wire == theirs.Wire {
		t.Fatal("R14: the writer-width layout hashes as the vector's")
	}
	// A TAG NO UNION LAYS: 7 - 4 = 3 bytes. The plan has no width to read it
	// at, and the entry refuses layout_malformed (§1.1).
	tag3 := r14Layout(ids, 7, 7)
	tag3Entry := squirreltable.FixedLineageEntry{
		Wire:   ir.TableFixedLayoutHash(tag3, wSt),
		Layout: tag3,
		Record: 8 + 7,
	}

	type read struct {
		name   string
		entry  squirreltable.FixedLineageEntry
		body   []byte
		count  string
		reason string
		tag    string
		a      string
	}
	vector := []read{
		{"arm1", theirs, []byte{1, 0x44, 0x33, 0x22, 0x11}, "1", "", "1", fmt.Sprint(0x11223344)},
		{"none", theirs, []byte{0, 0, 0, 0, 0}, "1", "", "0", "0"},
		{"forged2", theirs, []byte{2, 0, 0, 0, 0}, "-1", "out_of_range", "", ""},
	}
	readers := []struct {
		name   string
		src    string
		reads  []read
		handed []squirreltable.FixedLineageEntry
	}{
		{"MINE1", writer, vector, []squirreltable.FixedLineageEntry{theirs}},
		{"MINE2", mine2, append(append([]read(nil), vector...),
			read{"wide_arm1", wideEntry, []byte{1, 0, 0x44, 0x33, 0x22, 0x11}, "1", "", "1", fmt.Sprint(0x11223344)},
			read{"wide_none", wideEntry, []byte{0, 0, 0, 0, 0, 0}, "1", "", "0", "0"},
			read{"wide_forged2", wideEntry, []byte{2, 0, 0, 0, 0, 0}, "-1", "out_of_range", "", ""},
			// 0x0101 at the writer's 2 bytes is 257, past its one arm; read at
			// 1 byte it would be arm 1
			read{"wide_forged257", wideEntry, []byte{1, 1, 0x44, 0x33, 0x22, 0x11}, "-1", "out_of_range", "", ""},
			read{"tag3_malformed", tag3Entry, []byte{1, 0, 0, 0x44, 0x33, 0x22, 0x11}, "-1", "layout_malformed", "", ""},
		), []squirreltable.FixedLineageEntry{theirs, wideEntry, tag3Entry}},
	}
	for _, rd := range readers {
		t.Run(rd.name, func(t *testing.T) {
			t.Parallel()
			u := fixedSynthetic(t, rd.src)
			files, err := squirrel.Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			tables, err := squirreltable.GenerateLineage(u, map[string][]squirreltable.FixedLineageEntry{"VR": rd.handed})
			if err != nil {
				t.Fatalf("R14: GenerateLineage: %v", err)
			}
			maps.Copy(files, tables)
			// THE WIDTH THE PLAN READS AT, by source: the vector's tag at the
			// writer's 1 byte. MINE2's own tag is 2 bytes, so there the 1-byte
			// read is the compiled lineage plan's and nothing else's.
			src := string(files["VrTables.nut"])
			if src == "" {
				t.Fatalf("R14: no VrTables.nut among %v", keysOf(files))
			}
			if !strings.Contains(src, "_rt_fixed.getu(b, at + 0, 1)") {
				t.Errorf("R14 %s: the plan does not read the writer's tag at its 1-byte width", rd.name)
			}
			dir := t.TempDir()
			for k, v := range files {
				if err := os.WriteFile(filepath.Join(dir, k), v, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			inputs := map[string][]byte{}
			var p strings.Builder
			p.WriteString("local inputs = dofile(\"inputs.nut\", true);\n")
			p.WriteString("local N = dofile(\"schema_module.nut\", true).require(\"VrTables\");\n")
			for _, r := range rd.reads {
				inputs[r.name] = r14File(r.entry.Wire, r.entry.Layout, r.body)
				fmt.Fprintf(&p, "{ local v = N.VR(); local got = N.VRFixedLoad(inputs[%q], [v], null);\n", r.name)
				fmt.Fprintf(&p, "  print(\"R14:%s:\" + got + \":\" + (N.FixedReason == null ? \"\" : N.FixedReason) + \":\" + (got < 0 ? \"\" : v.U.Type + \":\" + (v.U.Type == 1 ? v.U.A : 0)) + \"\\n\"); }\n", r.name)
			}
			if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(p.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			out := string(sqrun.Run(t, dir, "main.nut", inputs))
			got := map[string]string{}
			for l := range strings.SplitSeq(out, "\n") {
				if rest, ok := strings.CutPrefix(l, "R14:"); ok {
					name, val, _ := strings.Cut(rest, ":")
					got[name] = val
				}
			}
			for _, r := range rd.reads {
				want := r.count + ":" + r.reason + ":"
				if r.tag != "" {
					want += r.tag + ":" + r.a
				}
				if got[r.name] != want {
					t.Errorf("R14 %s/%s: read %q, want %q", rd.name, r.name, got[r.name], want)
				}
			}
			if t.Failed() {
				t.Logf("R14 %s probe output:\n%s", rd.name, out)
			}
		})
	}
}

// r14LayoutIDs reads the root, union and arm ids out of the writer's own
// three-entry layout, so the vector names this build's own ids.
func r14LayoutIDs(t *testing.T, layout []byte) [3]uint64 {
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

// r14Layout lays the vector's three entries: the root of `record` bytes, the
// union of `union` bytes (its tag = union - 4), and the u32 arm.
func r14Layout(ids [3]uint64, record, union uint32) []byte {
	out := binary.LittleEndian.AppendUint32(nil, 3)
	put := func(id uint64, kind uint8, size, children uint32) {
		out = binary.LittleEndian.AppendUint64(out, id)
		out = append(out, kind)
		out = binary.LittleEndian.AppendUint32(out, size)
		out = binary.LittleEndian.AppendUint32(out, children)
	}
	put(ids[0], 13, record, 1)
	put(ids[1], 15, union, 1)
	put(ids[2], 8, 4, 0)
	return out
}

// r14File frames one record under `hash` (docs/FIXED-FORM-ALGORITHM.md §5.3):
// the form byte 3, seven reserved zeros, the hash, the layout's length and
// bytes, then the record — its hash, then its body.
func r14File(hash uint64, layout, body []byte) []byte {
	out := []byte{3, 0, 0, 0, 0, 0, 0, 0}
	out = binary.LittleEndian.AppendUint64(out, hash)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(layout)))
	out = append(out, layout...)
	out = binary.LittleEndian.AppendUint64(out, hash)
	return append(out, body...)
}

func keysOf(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
