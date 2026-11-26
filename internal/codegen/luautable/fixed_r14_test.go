package luautable_test

// R14 ON THE LUAU LEG — A UNION TAG GROWN BETWEEN WRITER AND READER READS THE
// WRITER'S TAG AT THE WRITER'S TAG WIDTH, and the grown pair is lawful whole.
//
// THE LAW: docs/FIXED-FORM-ALGORITHM.md §5.2 EMIT kind 15 — their_tag :=
// te.size - widest arm; my_tag := me.size - widest arm. The None const LANDS
// my_tag bytes, but a leg that reads the writer's raw tag reads it at
// THEIR_TAG bytes, never my_tag, so its source reach is src + their_tag,
// inside the writer's record: a tag grown 1 -> 8 compiles whole and the tag
// never swallows the arm's payload (the ruling of 2026-11-25; Rust
// 306e734396, test/conformance/rust/rows/R14.rs; JavaScript
// test/conformance/js/rows/R14.mjs). A tag width that is not 1, 2, 4 or 8
// (§1's union row) is no tag a lock records: its lane refuses
// layout_malformed and nothing clamps it (§5.9 #36).
//
// THE PRODUCTION PATH ON THIS LEG. Luau lays no op plan. Its compile of a
// lineage entry is planFor (runtime/schema_fixed.luau), run once per entry
// on the first load under its hash: compile parses the writer's layout into
// an offset tree (parseNode — a union's arms sit past the WRITER's tag,
// te.size - widest arm, and every child must lie inside its parent) and
// lawfulTable holds it against the reader's own tree. Its run is readUnion,
// which reads the writer's raw tag at tagWidth(w) and the arm past it. Both
// are driven here through the generated unit's own lineage
// (<Name>FixedLineage, laid by GenerateLineage) and the plain F.read.
//
// THE VECTOR (the brief every leg's R14 uses). A layout block is a u32LE
// entry count, then 17-byte entries: id u64LE, kind u8, size u32LE, children
// u32LE. Kind 13 a table, 15 a union (size = tag + widest arm), 8 a u32.
//
//   THEIRS (writer): [1,13,5,1], [7,15,5,1], [9,8,4,0] — a record of 5, a
//     union whose tag is 1 byte over one u32 arm.
//   MINE(tag) (reader): [1,13,tag+4,1], [7,15,tag+4,1], [9,8,4,0].
//
// The ids 1, 7 and 9 are the unit's own wire ids of the root, the union field
// and its arm: this leg finds a writer's child for a reader field BY WIRE ID
// (readTable's byId, lawfulTable), so a forged id no descriptor carries would
// read nothing at all. Every kind, size and child count is the vector's, and
// THEIRS with those ids is byte for byte the unit's own lock layout
// (asserted). The reader's own layout MINE(tag) is handed to F.read as the
// fixed table's layout, beside the generated lineage.
//
// What the reader's 8-byte tag lands on this leg is the union value's Type:
// a tag that swallowed the arm's payload would be 0x11223344.. or would
// refuse, so Type == 1 exactly is the 8-byte tag with its high bytes zero.

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
)

const r14LuauSchema = `package probe

union R14Pick
{
    a uint32
}

fixed table R14
{
    u R14Pick
}
`

// r14LuauLayout is the vector's layout block at the given ids, root and
// union size (tag + 4), the arm a u32.
func r14LuauLayout(ids [3]uint64, tag uint32) []byte {
	out := binary.LittleEndian.AppendUint32(nil, 3)
	put := func(id uint64, kind byte, size, kids uint32) {
		out = binary.LittleEndian.AppendUint64(out, id)
		out = append(out, kind)
		out = binary.LittleEndian.AppendUint32(out, size)
		out = binary.LittleEndian.AppendUint32(out, kids)
	}
	put(ids[0], 13, tag+4, 1)
	put(ids[1], 15, tag+4, 1)
	put(ids[2], 8, 4, 0)
	return out
}

// r14LuauMain reads each input file through the plain F.read under a fixed
// table whose own layout is MINE(myTag) and whose lineage is the generated
// one (THEIRS, then the unit's own).
const r14LuauMain = `local mine = F.fixed_layout({
	{ id = %[1]s, kind = 13, size = %[4]d, children = 1 },
	{ id = %[2]s, kind = 15, size = %[4]d, children = 1 },
	{ id = %[3]s, kind = 8, size = 4, children = 0 },
})
local fixed = { hash = T.R14FixedHash, recordBytes = %[4]d, layout = mine,
	lineage = T.R14FixedLineage, floor = T.R14FixedFloor }
for _, name in { "arm", "none", "forged", "high" } do
	local rep: any = {}
	local rows = F.read(T.R14Descriptor, fixed, (I :: any)[name], rep)
	if rows == nil then
		print(name .. " refused " .. tostring(rep.reason))
	else
		local u = rows[1].U
		local a = u.A
		print(name .. " ok " .. string.format("%%.17g", u.Type) .. " " .. (if a == nil then "nil" else string.format("%%d", a)))
	end
end
`

type r14LuauCase struct {
	name     string
	theirTag uint32 // the writer's tag width
	myTag    uint32 // the reader's tag width
	want     string
}

// r14LuauLawful is the answer every lawful case owes: arm 1 read as Type 1
// with the u32 whole past the writer's tag, tag 0 as None (the arm slot
// holds its declared default, 0, unread), and a forged tag 2
// — and a tag of every byte 0xff — past the writer's one arm refused
// out_of_range (§4.6; the verdict, read by its name on this leg).
const r14LuauLawful = "arm ok 1 287454020\n" +
	"none ok 0 0\n" +
	"forged refused out_of_range\n" +
	"high refused out_of_range"

// r14LuauMalformed is a writer tag width no lock records (not 1, 2, 4 or 8): the
// lane refuses, and every file under it refuses layout_malformed before a
// record byte is read.
const r14LuauMalformed = "arm refused layout_malformed\n" +
	"none refused layout_malformed\n" +
	"forged refused layout_malformed\n" +
	"high refused layout_malformed"

// r14LuauCases: the cell (THEIRS vs MINE(8)), its twins (MINE(1), MINE(2)),
// and the same law from the writer's side: a writer whose tag is 8 bytes over
// one arm (MINE(8) as THEIRS) is read at 8, and a writer tag of 0, 3, 5, 6, 7
// or 9 bytes is no tag at all (an ordinal width is exactly 1, 2, 4 or 8, never
// "1..8": docs/FIXED-FORM-ALGORITHM.md, THE GUARD'S WIDTH).
var r14LuauCases = []r14LuauCase{
	{name: "theirs_vs_mine8", theirTag: 1, myTag: 8, want: r14LuauLawful},
	{name: "theirs_vs_mine1", theirTag: 1, myTag: 1, want: r14LuauLawful},
	{name: "theirs_vs_mine2", theirTag: 1, myTag: 2, want: r14LuauLawful},
	{name: "writer_tag8_vs_mine8", theirTag: 8, myTag: 8, want: r14LuauLawful},
	{name: "writer_tag0_refused", theirTag: 0, myTag: 1, want: r14LuauMalformed},
	{name: "writer_tag3_refused", theirTag: 3, myTag: 1, want: r14LuauMalformed},
	{name: "writer_tag5_refused", theirTag: 5, myTag: 1, want: r14LuauMalformed},
	{name: "writer_tag6_refused", theirTag: 6, myTag: 1, want: r14LuauMalformed},
	{name: "writer_tag7_refused", theirTag: 7, myTag: 1, want: r14LuauMalformed},
	{name: "writer_tag9_refused", theirTag: 9, myTag: 1, want: r14LuauMalformed},
}

func r14LuauRun(t *testing.T, c r14LuauCase) string {
	t.Helper()
	u := luauCutSource(t, r14LuauSchema)
	own := luauCutEntry(t, u, "R14")
	if len(own.Layout) != 4+3*17 {
		t.Fatalf("the unit's own layout is not three entries: %d bytes", len(own.Layout))
	}
	var ids [3]uint64
	for i := range ids {
		ids[i] = binary.LittleEndian.Uint64(own.Layout[4+17*i:])
	}
	if got := r14LuauLayout(ids, 1); string(got) != string(own.Layout) {
		t.Fatalf("THEIRS at the unit's ids is not the unit's own lock layout:\n got %x\nwant %x", got, own.Layout)
	}
	theirs := r14LuauLayout(ids, c.theirTag)
	const theirHash = 0x4444444433333333
	g := luauCutGen(t, u, map[string][]luautable.FixedLineageEntry{"R14": {{
		Wire: theirHash, Layout: theirs, Record: int64(8 + c.theirTag + 4),
	}}})
	record := func(tag byte) []byte {
		r := make([]byte, c.theirTag+4)
		r[0] = tag
		if tag == 1 {
			binary.LittleEndian.PutUint32(r[c.theirTag:], 0x11223344)
		}
		return r
	}
	// every byte of the writer's tag set: past the one arm at any width
	high := record(0)
	for i := range c.theirTag {
		high[i] = 0xff
	}
	main := luauCutPrelude + fmt.Sprintf(r14LuauMain, r14LuauId(ids[0]), r14LuauId(ids[1]), r14LuauId(ids[2]), c.myTag+4)
	return g.run(t, main, map[string][]byte{
		"arm":    luauCutFile(theirHash, theirs, record(1)),
		"none":   luauCutFile(theirHash, theirs, record(0)),
		"forged": luauCutFile(theirHash, theirs, record(2)),
		"high":   luauCutFile(theirHash, theirs, high),
	})
}

// TestLuauFixedR14GrownTagReadsTheWritersTagWidth is the Luau leg's R14.
func TestLuauFixedR14GrownTagReadsTheWritersTagWidth(t *testing.T) {
	t.Parallel()
	for _, c := range r14LuauCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimSpace(r14LuauRun(t, c)); got != c.want {
				t.Errorf("R14 %s:\n got %q\nwant %q", c.name, got, c.want)
			}
		})
	}
}

// r14LuauId is a wire id as the generator spells a Luau integer literal.
func r14LuauId(id uint64) string { return luau.IntegerLit(new(big.Int).SetUint64(id)) }
