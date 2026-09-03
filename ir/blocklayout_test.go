package ir_test

// The BLOCK FORM's layout model, pinned against docs/SPEC-TABLES.md §19.1's worked
// nine-array table and against the hand-written scatter this form replaces.
//
// The agreement is with the ARITHMETIC, not with one frame: the rule stated on
// the page and the hand layout are the same walk over the same pitches, so
// they land every array at the same offset for ANY counts. The frame below is
// the page's, chosen to be legible.

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func loadRender(t *testing.T) *ir.Unit {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join("..", "tables", "block")})
	if err != nil {
		t.Fatalf("gather the block corpus: %v", err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load the block corpus: %v", err)
	}
	return u
}

func TestBlockLayoutMatchesTheWorkedTable(t *testing.T) {
	u := loadRender(t)
	b := ir.Blocks(u)
	if b == nil {
		t.Fatal("the block corpus marks a table; ir.Blocks returned nil")
	}
	bl := b.Block("RenderFrame")
	if bl == nil {
		t.Fatal("RenderFrame is not a block-form table")
	}

	// the projection: the 24-byte prologue, a uint64, and nine triples. §19.1
	// works the shape with a 16-byte prologue and 168 bytes; the byte-order
	// word makes it 176, and the nine starts and the extent are UNCHANGED
	// because 168 and 176 both round to 192 — which is the page's own point
	// about the prologue being free in this shape.
	if bl.Projection.Size != 176 || bl.Projection.Align != 8 {
		t.Errorf("projection sizeof/alignof = %d/%d, want 176/8", bl.Projection.Size, bl.Projection.Align)
	}

	wantRows := []struct {
		field  string
		elem   string
		stride int64
		max    int64
	}{
		{"cameras", "RenderCamera", 72, 1},
		{"ships", "RenderShip", 88, 4096},
		{"turrets", "RenderTurret", 64, 1024},
		{"missiles", "RenderMissile", 72, 4096},
		{"dynamic_props", "RenderDynamicProp", 72, 4096},
		{"static_props", "RenderStaticProp", 80, 20000},
		{"cosmetic_props", "RenderCosmeticProp", 80, 8192},
		{"lasers", "RenderLaser", 64, 32000},
		{"explosions", "RenderExplosion", 80, 32000},
	}
	if len(bl.Arrays) != len(wantRows) {
		t.Fatalf("out-of-line arrays = %d, want %d", len(bl.Arrays), len(wantRows))
	}
	for i, want := range wantRows {
		got := bl.Arrays[i]
		if got.Field.Name != want.field || got.ElemName != want.elem || got.Stride != want.stride || got.Max != want.max {
			t.Errorf("array %d = %s/%s stride %d max %d, want %s/%s stride %d max %d",
				i, got.Field.Name, got.ElemName, got.Stride, got.Max, want.field, want.elem, want.stride, want.max)
		}
		// the triple is sixteen bytes with no interior padding, at the field's
		// own position (§2.7)
		if got.CountOffset != got.OffsetOfOffset+8 || got.StrideOffset != got.OffsetOfOffset+12 {
			t.Errorf("array %s triple members at %d/%d/%d — the triple has no interior padding",
				want.field, got.OffsetOfOffset, got.CountOffset, got.StrideOffset)
		}
	}

	// §19.1's frame, and its starts and extent to the digit
	counts := []int64{1, 300, 900, 120, 40, 5000, 800, 200, 60}
	aligns := make([]int64, len(bl.Arrays))
	for i, a := range bl.Arrays {
		aligns[i] = a.ElemAlign()
	}
	starts, used := ir.BlockExtent(bl, aligns, counts)
	wantStarts := []int64{192, 320, 26752, 84352, 92992, 95872, 495872, 559872, 572672}
	for i := range starts {
		if starts[i] != wantStarts[i] {
			t.Errorf("%s starts at %d, want %d", bl.Arrays[i].Field.Name, starts[i], wantStarts[i])
		}
	}
	if used != 577472 {
		t.Errorf("used extent = %d, want 577472", used)
	}

	// the allocate-max law: one extent, sized from the declared maxima
	if bl.MaxBytes != 7879488 {
		t.Errorf("BlockMaxBytes = %d, want 7879488 (§19.1)", bl.MaxBytes)
	}
}

// A raised MAXIMUM grows the storage. What it does to the number a block
// carries is the BUILD VERSION's question now, not a per-table digest's
// (docs/SPEC-TABLES.md §20): a bound is a layout fact of the by-value struct, so it
// moves the build version and BlockOpen refuses until both sides are
// regenerated. What is pinned here is the STORAGE half, which is what the
// allocate-max law rests on.
func TestRaisingAMaximumGrowsTheStorage(t *testing.T) {
	u := loadRender(t)
	baselineMax := ir.Blocks(u).Block("RenderFrame").MaxBytes

	for _, f := range u.Tables["RenderFrame"].Fields {
		if f.Name == "ships" {
			f.ArrayBound *= 2
		}
	}
	after := ir.Blocks(u).Block("RenderFrame")
	if after.MaxBytes <= baselineMax {
		t.Errorf("raising a maximum did not grow the storage: %d -> %d", baselineMax, after.MaxBytes)
	}
	if got := after.ArrayByName("ships").Max; got != 8192 {
		t.Errorf("ships max = %d, want 8192", got)
	}
}

// THE BUILD VERSION SEES LAYOUT (docs/SPEC-TABLES.md §20.1 group 2): a moved
// offset, a changed size, a different pitch. Its negative control is the
// sabotage in the Makefile's block-digest-negative-control target; this is the
// positive half.
func TestBuildVersionMovesOnALayoutEdit(t *testing.T) {
	u := loadRender(t)
	baseline := ir.BuildVersion(u)

	// append a field at the end of a ROW: the row grows and so does its
	// derived pitch, so every consumer that indexes at the old pitch is wrong
	ship := u.Tables["RenderShip"]
	ship.Fields = append(ship.Fields, &ir.Field{Name: "extra", Type: ir.FieldType{Kind: ir.TInt, Signed: false, Width: 32}})
	if got := ir.BuildVersion(u); got == baseline {
		t.Error("appending a field to a row did not move the build version — BlockOpen would accept a block whose rows moved")
	}
}

// THE BUILD VERSION SEES MEANING (docs/SPEC-TABLES.md §20.1 group 3): a fact that
// changes what a load PUTS in a slot while moving no offset at all. Nothing in
// the layout half could see this one.
func TestBuildVersionMovesOnAMeaningEdit(t *testing.T) {
	u := loadRender(t)
	baseline := ir.BuildVersion(u)
	for _, f := range u.Structs["RenderQuaternion"].Fields {
		if f.Name == "w" {
			f.DefFloat = 2.0
		}
	}
	if got := ir.BuildVersion(u); got == baseline {
		t.Error("changing a specified default did not move the build version — group 3 is exactly the class that moves no offset")
	}
}

// AND IT DOES NOT SEE what no byte depends on (docs/SPEC-TABLES.md §20.4): a `was`
// rename moves no wire id, so it moves no line of either projection.
func TestBuildVersionSurvivesAWasRename(t *testing.T) {
	u := loadRender(t)
	baseline := ir.BuildVersion(u)
	for _, f := range u.Tables["RenderShip"].Fields {
		if f.Name == "thrust" {
			f.WasName = "thrust"
			f.Name = "throttle"
		}
	}
	if got := ir.BuildVersion(u); got != baseline {
		t.Errorf("a `was` rename moved the build version (0x%016x -> 0x%016x) — the projections key on the WIRE ID for exactly this reason",
			baseline, got)
	}
}

// THE DIGEST'S NEGATIVE CONTROL (the block form's second, and the one a layout
// test cannot supply for itself). A digest that shares its model with the code
// it checks proves nothing until it is shown CAPABLE of missing a break — so
// this reorders two fields of one row and asserts, twice:
//
//   - the real build version MOVES, because §20.2's cook projection carries
//     each field's OFFSET beside its wire id; and
//   - a WEAKENED projection with the offsets stripped out — the
//     positional-versus-keyed sabotage, in this form's own terms — does NOT
//     move, so the break is silently accepted.
//
// The second half is what makes the first mean something: the offset token is
// load-bearing, and the test says so by removing it.
func TestBuildVersionOffsetTermIsLoadBearing(t *testing.T) {
	// The sabotage is the POSITIONAL-VERSUS-KEYED one, in this form's terms: a
	// digest that treated the projection as a SET of facts keyed by wire id
	// rather than as an ordered text with each field's offset on its line. Drop
	// the offsets and sort, and a field reorder becomes invisible.
	strip := func(projection string) string {
		var out []string
		for line := range strings.SplitSeq(projection, "\n") {
			var kept []string
			for token := range strings.FieldsSeq(line) {
				if strings.HasPrefix(token, "offset=") {
					continue
				}
				kept = append(kept, token)
			}
			out = append(out, strings.Join(kept, " "))
		}
		sort.Strings(out)
		return strings.Join(out, "\n")
	}

	before := loadRender(t)
	baselineVersion := ir.BuildVersion(before)
	baselineWeak := strip(ir.CookProjection(before))

	// reorder two SAME-SIZE, same-alignment fields of one row: object_id and
	// target_object_id are both uint32, so nothing about the row's size or
	// alignment moves — only two offsets do, which is exactly the break a
	// pointed-at row cannot survive and cannot report.
	after := loadRender(t)
	ship := after.Tables["RenderShip"]
	for i := range ship.Fields {
		if ship.Fields[i].Name == "object_id" {
			ship.Fields[i], ship.Fields[i+1] = ship.Fields[i+1], ship.Fields[i]
			break
		}
	}

	if ir.BuildVersion(after) == baselineVersion {
		t.Error("reordering two same-size row fields did not move the build version — a consumer would read every row at the wrong offsets and BlockOpen would accept it")
	}
	if got := strip(ir.CookProjection(after)); got != baselineWeak {
		t.Errorf("the sabotaged projection was expected to be BLIND to the reorder, and was not:\n%s", got)
	}
}

// A RAISED MAXIMUM moves the build version, and this test exists to make that
// visible rather than to bless it.
//
// §19.3 excluded a declared maximum from the block form's OWN digest so that
// raising one stayed absorbable: a maximum moves the offset_ofs written into
// an instance and moves no offset a consumer reads AT, because a consumer
// takes every offset_of from the instance. The owner's ruling since replaced
// that per-table digest with the unit's BUILD VERSION, and a bound is a layout
// fact of the by-value struct (§20.1 group 2: "an array's bound changed"), so
// it moves. The consequence is stated plainly here: under one build version a
// raised maximum is a refusal at Open like any other edit, and both sides are
// regenerated. It is pinned so the day that answer changes, this test says so.
func TestRaisingAMaximumMovesTheBuildVersion(t *testing.T) {
	u := loadRender(t)
	baseline := ir.BuildVersion(u)
	for _, f := range u.Tables["RenderFrame"].Fields {
		if f.Name == "ships" {
			f.ArrayBound *= 2
		}
	}
	if ir.BuildVersion(u) == baseline {
		t.Error("raising a maximum left the build version where it was — the layout projection carries the bound, and this test is the record of that")
	}
}

// THE ROW-WALK COLUMNS (docs/SPEC-TABLES.md §8.1): what a generic walker needs
// about a field after it knows where the field starts. PaddedRow is the case
// that has one of everything the block form leaves INLINE — a string, a fixed
// array, an enum-keyed array, an optional — and PaddedFrame adds a `bytes` and
// the out-of-line triple beside them.
//
// It is written against the numbers rather than against a re-derivation,
// because a test that re-derived the layout would agree with a wrong model as
// happily as with a right one.
func TestBlockFieldRowWalkColumns(t *testing.T) {
	u := loadRender(t)
	b := ir.Blocks(u)
	bl := b.Block("PaddedFrame")
	if bl == nil {
		t.Fatal("PaddedFrame is not a block-form table")
	}
	row := b.Layout("PaddedRow")
	if row == nil {
		t.Fatal("PaddedRow has no layout in the block closure")
	}

	type want struct {
		field                                string
		isArray, counted, optional           bool
		bound, elem, countOffset, presOffset int64
	}
	// the ROW, at its own offsets
	rowWants := []want{
		{"tag", false, false, false, 0, 1, -1, -1},
		{"value", false, false, false, 0, 8, -1, -1},
		{"flag", false, false, false, 0, 1, -1, -1},
		{"id", false, false, false, 0, 4, -1, -1},
		// string(15): a char[16] buffer at 24 and the int32 used length at 40.
		// A string is COUNTED and not an ARRAY, and the bound is the declared
		// maximum rather than the buffer.
		{"label", false, true, false, 15, 1, 40, -1},
		{"slots", true, false, false, 4, 2, -1, -1},   // [4]uint16
		{"teams", true, false, false, 4, 1, -1, -1},   // [Team]uint8: one slot per named variant
		{"counter", false, false, true, 0, 4, -1, 60}, // ?int32: the value, then the presence bool
	}
	for _, w := range rowWants {
		fl := row.FieldByName(w.field)
		if fl == nil {
			t.Errorf("PaddedRow declares no field %s", w.field)
			continue
		}
		got := ir.BlockFieldOf(u, fl.Field, fl.Offset, false)
		if got.IsArray != w.isArray || got.Counted != w.counted || got.Optional != w.optional ||
			got.ArrayBound != w.bound || got.ElemSize != w.elem ||
			got.CountOffset != w.countOffset || got.PresentOffset != w.presOffset {
			t.Errorf("PaddedRow.%s = %+v, want array=%t counted=%t optional=%t bound=%d elem=%d count@%d present@%d",
				w.field, got, w.isArray, w.counted, w.optional, w.bound, w.elem, w.countOffset, w.presOffset)
		}
	}

	// the PROJECTION, whose out-of-line array is its triple: the count
	// companion is the triple's own `count` member, eight bytes in, which is
	// the same column doing the same job as a string's used length.
	rows := bl.Projection.FieldByName("rows")
	got := ir.BlockFieldOf(u, rows.Field, rows.Offset, true)
	if !got.IsArray || !got.Counted || got.CountOffset != rows.Offset+8 || got.ArrayBound != 64 {
		t.Errorf("PaddedFrame.rows = %+v, want an array of bound 64 counted at %d", got, rows.Offset+8)
	}
	// bytes(12): twelve bytes then the int32 length, an ARRAY and COUNTED
	blob := bl.Projection.FieldByName("blob")
	got = ir.BlockFieldOf(u, blob.Field, blob.Offset, true)
	if !got.IsArray || !got.Counted || got.ArrayBound != 12 || got.ElemSize != 1 ||
		got.CountOffset != blob.Offset+12 {
		t.Errorf("PaddedFrame.blob = %+v, want twelve u8 slots counted at %d", got, blob.Offset+12)
	}
}

// The NEGATIVE CONTROL for the columns above, and it is the one a sweep cannot
// give: a walker that took `size` for the element count would read a
// `string(15)` as twenty one-byte slots and an optional int32 as five. The
// assertion is that the two numbers DIFFER — the columns carry something the
// offset/size pair does not.
func TestRowWalkColumnsAreNotDerivableFromSize(t *testing.T) {
	u := loadRender(t)
	row := ir.Blocks(u).Layout("PaddedRow")
	for _, name := range []string{"label", "counter"} {
		fl := row.FieldByName(name)
		got := ir.BlockFieldOf(u, fl.Field, fl.Offset, false)
		span := got.ArrayBound * got.ElemSize
		if got.ArrayBound == 0 {
			span = got.ElemSize
		}
		if span == fl.Size {
			t.Errorf("PaddedRow.%s spans %d of %d bytes — the companion is inside the field, "+
				"and a walker that took the field's size would read it too", name, span, fl.Size)
		}
	}
}
