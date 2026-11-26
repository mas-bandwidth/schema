package mootable_test

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/mootable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// R14 ON THE MOO LEG — A GROWN UNION TAG READS THE WRITER'S TAG WIDTH AND
// COMPILES WHOLE (docs/FIXED-FORM-ALGORITHM.md §5.2 EMIT kind 15: their_tag :=
// te.size - widest arm; the union's None reads the writer's raw tag at THE
// WRITER'S tag width, so its source reach is src + their_tag, inside the
// writer's record).
//
// The moo leg compiles each lineage entry IN THE RUNTIME, at load
// (runtime/table_fixed.moo parseLayout + compileValue), from the layout bytes
// the Go generator laid down beside the descriptor (fixedform.go,
// <Root>Fixed.lineage). parseLayout takes the writer's tag width off the
// layout — `tagw = e.size - widest` (table_fixed.moo:197), held to 1, 2, 4 or
// 8 — and bounds every child's reach, `c.off + c.size`, by the WRITER's
// record (`inside`, :132); the read takes the tag at `n.tagw`
// (`ordinalAt(rec, off, n.tagw)`, :791). A moo union holds its tag as a
// number, so the reader has no tag width of its own.
//
// THE VECTOR (every leg's R14): THEIRS, a record of 5 — a union of tag 1 over
// one u32: [root,13,5,1], [u,15,5,1], [a,8,4,0]. The readers: MINE(1), the
// writer's own schema, and MINE(2), the union grown to 256 arms so its tag is
// 2 bytes (the real growth a schema makes; a tag of 8 needs more arms than any
// layout holds). Both read
//
//	[1, 0x44,0x33,0x22,0x11]  tag 1, the u32 0x11223344 (the payload is never the tag)
//	[0, 0,0,0,0]              None
//	[2, 0,0,0,0]              a forged tag past the writer's one arm: out_of_range (§4.6)
//
// and MINE(2) also reads a writer whose tag is 2 bytes over ONE arm
// ([root,13,6,1], [u,15,6,1], [a,8,4,0]) at the layout's 2 bytes, the u32 at
// offset 2 — a tag of 0x0101 is 257, past the one arm, and refuses
// out_of_range where a 1-byte read would land arm 1 — and refuses a 3-byte
// tag layout_malformed. The moo leg answered every read at the writer's
// width before this row (2026-11-25): no fix, the row holds it.
func TestMooFixedR14GrownTagReadsWritersTagWidth(t *testing.T) {
	t.Parallel()
	const writer = "package probe\nunion U { a uint32 }\nfixed table VR { u U }\n"
	var arms strings.Builder
	for i := 1; i < 256; i++ {
		fmt.Fprintf(&arms, "  p%d\n", i)
	}
	mine2 := "package probe\nunion U {\n  a uint32\n" + arms.String() + "}\nfixed table VR { u U }\n"

	wU := unitFromSource(t, writer)
	theirs, ok := mootable.FixedLineageOf(wU, "VR")
	if !ok {
		t.Fatal("R14: the writer builds no fixed lineage entry")
	}
	ids := mooR14IDs(t, theirs.Layout)
	if want := mooR14Layout(ids, 5); string(theirs.Layout) != string(want) {
		t.Fatalf("R14: the writer's layout is not the vector:\n got %x\nwant %x", theirs.Layout, want)
	}
	forged := func(size uint32) mootable.FixedLineageEntry {
		l := mooR14Layout(ids, size)
		return mootable.FixedLineageEntry{Wire: ir.TableFixedLayoutHash(l, theirs.Writer), Layout: l, Record: 8 + int64(size), Writer: theirs.Writer}
	}
	wide, tag3 := forged(6), forged(7)

	type read struct {
		name  string
		entry mootable.FixedLineageEntry
		body  []byte
		want  string // the head line, then the union's type and arm when it reads
	}
	vector := []read{
		{"arm1", theirs, []byte{1, 0x44, 0x33, 0x22, 0x11}, "ok 1|1|287454020"},
		{"none", theirs, []byte{0, 0, 0, 0, 0}, "ok 1|0|"},
		{"forged2", theirs, []byte{2, 0, 0, 0, 0}, "refused out_of_range"},
	}
	readers := []struct {
		name   string
		src    string
		reads  []read
		handed []mootable.FixedLineageEntry
	}{
		{"MINE1", writer, vector, []mootable.FixedLineageEntry{theirs}},
		{"MINE2", mine2, append(append([]read(nil), vector...),
			read{"wide_arm1", wide, []byte{1, 0, 0x44, 0x33, 0x22, 0x11}, "ok 1|1|287454020"},
			read{"wide_none", wide, []byte{0, 0, 0, 0, 0, 0}, "ok 1|0|"},
			read{"wide_forged2", wide, []byte{2, 0, 0, 0, 0, 0}, "refused out_of_range"},
			// 0x0101 at the writer's 2 bytes is 257, past its one arm; read at
			// 1 byte it would be arm 1
			read{"wide_forged257", wide, []byte{1, 1, 0x44, 0x33, 0x22, 0x11}, "refused out_of_range"},
			read{"tag3_malformed", tag3, []byte{1, 0, 0, 0x44, 0x33, 0x22, 0x11}, "refused layout_malformed"},
		), []mootable.FixedLineageEntry{theirs, wide, tag3}},
	}
	for _, rd := range readers {
		t.Run(rd.name, func(t *testing.T) {
			t.Parallel()
			g := fixedUnit(t, unitFromSource(t, rd.src), map[string][]mootable.FixedLineageEntry{"VR": rd.handed})
			for _, r := range rd.reads {
				run := runFixedLoad(t, g, "ProbeTable", "VR", mooR14File(r.entry.Wire, r.entry.Layout, r.body))
				got := run.head
				if strings.HasPrefix(got, "ok ") {
					got = strings.Join(strings.Fields(got)[:2], " ") + "|" + run.dump["r0.u.type"] + "|" + run.dump["r0.u.a"]
				} else {
					got = strings.Join(strings.Fields(got)[:min(2, len(strings.Fields(got)))], " ")
				}
				if got != r.want {
					t.Errorf("R14 %s/%s: read %q, want %q (head %q, dump %v)", rd.name, r.name, got, r.want, run.head, run.dump)
				}
			}
		})
	}
}

// mooR14IDs reads the root, union and arm ids out of the writer's own
// three-entry layout, so the vector names this build's own ids.
func mooR14IDs(t *testing.T, layout []byte) [3]uint64 {
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

// mooR14Layout lays the vector's three entries: the root and the union of
// `size` bytes (the tag is size - 4) and the u32 arm (kind 8).
func mooR14Layout(ids [3]uint64, size uint32) []byte {
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

// mooR14File frames one record under `hash` (§5.3): form byte 3, seven
// reserved zeros, the hash, the layout's length and bytes, the record's hash
// and body.
func mooR14File(hash uint64, layout, body []byte) []byte {
	out := []byte{3, 0, 0, 0, 0, 0, 0, 0}
	out = binary.LittleEndian.AppendUint64(out, hash)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(layout)))
	out = append(out, layout...)
	out = binary.LittleEndian.AppendUint64(out, hash)
	return append(out, body...)
}
