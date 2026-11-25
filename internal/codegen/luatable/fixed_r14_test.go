package luatable

// R14 ON THE LUA LEG — A UNION TAG GROWN BETWEEN WRITER AND READER READS THE
// WRITER'S TAG AT THE WRITER'S TAG WIDTH, and the grown pair is lawful whole.
//
// THE LAW: docs/FIXED-FORM-ALGORITHM.md §5.2 EMIT kind 15 — their_tag :=
// te.size - widest arm; my_tag := me.size - widest arm. The None const LANDS
// my_tag bytes, but a leg that reads the writer's raw tag reads it at
// THEIR_TAG bytes, never my_tag, so its source reach is src + their_tag,
// inside the writer's record: a tag grown 1 -> 8 compiles whole and the tag
// never swallows the arm's payload (the ruling of 2026-11-25; Rust
// 306e734396, test/conformance/rust/rows/R14.rs; JavaScript
// test/conformance/js/rows/R14.mjs).
//
// THE PRODUCTION PATH ON THIS LEG. Lua lays no op plan. Its "compile" of a
// foreign layout is F.prepare at module load (runtime/fixed.lua): each known
// lineage entry gets its layout tree (layout_tree, which bounds every size
// against the writer's record) and its lawfulness against the reader's own
// layout (lawful_table). Its run is decode_old_value, which walks the
// WRITER's tree: the union case reads the writer's raw tag and, past it, the
// taken arm. Both are driven here on a generated unit through the generated
// module's ReadFixed.
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
// (decode_old_table's byId, lawful_table's children_named), so a forged id no
// descriptor carries would read nothing at all. Every kind, size and child
// count is the vector's, and THEIRS with those ids is byte for byte the
// unit's own lock layout (asserted).
//
// What the reader's 8-byte tag "lands" on this leg is the union value's tag,
// a Lua integer: a tag that swallowed the arm's payload would be 0x11223344..
// or would refuse, so tag == 1 exactly (math.type integer) is the 8-byte tag
// with its high bytes zero.

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

const r14LuaSchema = `package probe

union R14Pick
{
    a uint32
}

fixed table R14
{
    u R14Pick
}
`

// r14LuaLayout is the vector's layout block at the given ids, root and union
// size (tag + 4), the arm a u32.
func r14LuaLayout(ids [3]uint64, tag uint32) []byte {
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

// r14LuaFile frames one form-3 file: header, layout, and each record behind
// the hash.
func r14LuaFile(hash uint64, layout []byte, records ...[]byte) []byte {
	out := []byte{3, 0, 0, 0, 0, 0, 0, 0}
	out = binary.LittleEndian.AppendUint64(out, hash)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(layout)))
	out = append(out, layout...)
	for _, r := range records {
		out = binary.LittleEndian.AppendUint64(out, hash)
		out = append(out, r...)
	}
	return out
}

// r14LuaMain loads the module (F.prepare runs here: the lineage lane is laid
// at load), reports the lane, then reads each input file.
const r14LuaMain = `local T = require(%q)
local I = require("inputs")
local e = T.Fixed["R14"].known[1]
print("lane " .. tostring(e.tree ~= false and e.tree.refusal == nil) .. " " .. tostring(e.unlawful))
for _, name in ipairs({ "arm", "none", "forged", "high" }) do
  local ok, v = pcall(T.ReadFixed, "R14", I[name])
  if ok then
    local tag = v.u.tag
    print(name .. " ok " .. tostring(math.type(tag)) .. " " .. string.format("%%d", tag) .. " " .. tostring(v.u.a))
  else
    print(name .. " refused " .. tostring(v))
  end
end
`

type r14LuaCase struct {
	name     string
	theirTag uint32 // the writer's tag width
	myTag    uint32 // the reader's tag width
}

func r14LuaRun(t *testing.T, c r14LuaCase) string {
	t.Helper()
	u := luarun.LoadUnit(t, writeR14Schema(t))
	own, ok := FixedLineageOf(u, "R14")
	if !ok {
		t.Fatal("no fixed root R14")
	}
	if len(own.Layout) != 4+3*17 {
		t.Fatalf("the unit's own layout is not three entries: %d bytes", len(own.Layout))
	}
	var ids [3]uint64
	for i := range ids {
		ids[i] = binary.LittleEndian.Uint64(own.Layout[4+17*i:])
	}
	if got := r14LuaLayout(ids, 1); string(got) != string(own.Layout) {
		t.Fatalf("THEIRS at the unit's ids is not the unit's own lock layout:\n got %x\nwant %x", got, own.Layout)
	}
	theirs := r14LuaLayout(ids, c.theirTag)
	const theirHash = 0x4444444433333333
	lineage := map[string][]FixedLineageEntry{"R14": {{
		Wire: theirHash, Layout: theirs, Record: int64(8 + c.theirTag + 4),
	}}}
	files, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatalf("generate with the lineage: %v", err)
	}
	// THE READER'S OWN LAYOUT IS MINE(myTag): F.prepare holds the writer's
	// tree lawful against it.
	mod := ModuleName(u) + ".lua"
	body := string(files[mod])
	from := fmt.Sprintf("_M.Fixed[%q].layout = ", "R14")
	to := fmt.Sprintf("_M.Fixed[%q].hash = ", "R14")
	at, end := strings.Index(body, from), strings.Index(body, to)
	if strings.Count(body, from) != 1 || strings.Count(body, to) != 1 || at < 0 || end < at {
		t.Fatalf("the own layout line is not in %s exactly once", mod)
	}
	files[mod] = []byte(body[:at] + from + chunkedLuaByteString(r14LuaLayout(ids, c.myTag)) + "\n" + body[end:])
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	writeFile(t, filepath.Join(dir, "main.lua"), fmt.Sprintf(r14LuaMain, ModuleName(u)))
	record := func(tag byte) []byte {
		r := make([]byte, c.theirTag+4)
		r[0] = tag
		if tag == 1 {
			binary.LittleEndian.PutUint32(r[c.theirTag:], 0x11223344)
		}
		return r
	}
	// every byte of the writer's tag set: past the one arm at any width, and
	// at 8 bytes an integer whose top bit is set
	high := record(0)
	for i := range c.theirTag {
		high[i] = 0xff
	}
	stdout, stderr, err := luarun.Try(t, dir, "main.lua", map[string][]byte{
		"arm":    r14LuaFile(theirHash, theirs, record(1)),
		"none":   r14LuaFile(theirHash, theirs, record(0)),
		"forged": r14LuaFile(theirHash, theirs, record(2)),
		"high":   r14LuaFile(theirHash, theirs, high),
	})
	if err != nil {
		return strings.TrimSpace(stdout) + "\nerror: " + err.Error() + "\n" + stderr
	}
	return strings.TrimSpace(stdout)
}

func writeR14Schema(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "R14Probe.schema")
	if err := os.WriteFile(p, []byte(r14LuaSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// r14LuaLawful is the answer every lawful case owes: the lane laid lawful at
// load, arm 1 read as tag 1 (an integer, exactly 1) with the u32 whole past
// the writer's tag, tag 0 as None, and a forged tag 2 — and a tag of every
// byte 0xff — past the writer's one arm refused out_of_range (§4.6; the
// verdict, read by its name on this leg).
const r14LuaLawful = "lane true false\n" +
	"arm ok integer 1 287454020\n" +
	"none ok integer 0 nil\n" +
	"forged refused out_of_range\n" +
	"high refused out_of_range"

// r14LuaMalformed is a writer tag width no lock records (not 1, 2, 4 or 8:
// §1's union row, §5.9 #36): the lane refuses at load and every file under it refuses
// layout_malformed before a record byte is read.
const r14LuaMalformed = "lane false false\n" +
	"arm refused layout_malformed\n" +
	"none refused layout_malformed\n" +
	"forged refused layout_malformed\n" +
	"high refused layout_malformed"

// r14LuaCases: the cell (THEIRS vs MINE(8)), its twins (MINE(1), MINE(2)),
// and the same law from the writer's side: a writer whose tag is 8 bytes over
// one arm (MINE(8) as THEIRS) holds its tag in 8 bytes, which the arm COUNT
// alone would call 1 — the writer's tag width is te.size - widest arm, and a
// width of 0, 3, 5, 6, 7 or 9 is no tag at all (an ordinal width is exactly
// 1, 2, 4 or 8, never "1..8": docs/FIXED-FORM-ALGORITHM.md, THE GUARD'S WIDTH).
var r14LuaCases = []struct {
	c    r14LuaCase
	want string
}{
	{r14LuaCase{name: "theirs_vs_mine8", theirTag: 1, myTag: 8}, r14LuaLawful},
	{r14LuaCase{name: "theirs_vs_mine1", theirTag: 1, myTag: 1}, r14LuaLawful},
	{r14LuaCase{name: "theirs_vs_mine2", theirTag: 1, myTag: 2}, r14LuaLawful},
	{r14LuaCase{name: "writer_tag8_vs_mine8", theirTag: 8, myTag: 8}, r14LuaLawful},
	{r14LuaCase{name: "writer_tag0_refused", theirTag: 0, myTag: 1}, r14LuaMalformed},
	{r14LuaCase{name: "writer_tag3_refused", theirTag: 3, myTag: 1}, r14LuaMalformed},
	{r14LuaCase{name: "writer_tag5_refused", theirTag: 5, myTag: 1}, r14LuaMalformed},
	{r14LuaCase{name: "writer_tag6_refused", theirTag: 6, myTag: 1}, r14LuaMalformed},
	{r14LuaCase{name: "writer_tag7_refused", theirTag: 7, myTag: 1}, r14LuaMalformed},
	{r14LuaCase{name: "writer_tag9_refused", theirTag: 9, myTag: 1}, r14LuaMalformed},
}

// TestLuaFixedR14GrownTagReadsTheWritersTagWidth is the Lua leg's R14.
func TestLuaFixedR14GrownTagReadsTheWritersTagWidth(t *testing.T) {
	t.Parallel()
	for _, row := range r14LuaCases {
		t.Run(row.c.name, func(t *testing.T) {
			t.Parallel()
			if got := r14LuaRun(t, row.c); got != row.want {
				t.Errorf("R14 %s:\n got %q\nwant %q", row.c.name, got, row.want)
			}
		})
	}
}
