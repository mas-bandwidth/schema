package ir_test

// The BLOCK FORM's layout model, pinned against SPEC-TABLES.md §19.1's worked
// nine-array table and against the hand-written scatter this form replaces.
//
// The agreement is with the ARITHMETIC, not with one frame: the rule stated on
// the page and the hand layout are the same walk over the same pitches, so
// they land every array at the same offset for ANY counts. The frame below is
// the page's, chosen to be legible.

import (
	"path/filepath"
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

	// the projection: the 16-byte prologue, a uint64, and nine triples
	if bl.Projection.Size != 168 || bl.Projection.Align != 8 {
		t.Errorf("projection sizeof/alignof = %d/%d, want 168/8", bl.Projection.Size, bl.Projection.Align)
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

// A declared MAXIMUM is deliberately NOT a digest fact (§19.3): it moves the
// offset_ofs written into an instance, and a consumer takes every offset_of
// FROM the instance, so raising one is absorbed on the DEFAULT entry point.
// A port that folded the maximum in would break that absorption with nothing
// to catch it — so the exclusion is pinned here rather than trusted.
func TestBlockDigestExcludesTheMaximum(t *testing.T) {
	u := loadRender(t)
	before := ir.Blocks(u).Block("RenderFrame")
	baseline := before.LayoutId
	baselineMax := before.MaxBytes

	for _, f := range u.Tables["RenderFrame"].Fields {
		if f.Name == "ships" {
			f.ArrayBound *= 2
		}
	}
	after := ir.Blocks(u).Block("RenderFrame")
	if after.LayoutId != baseline {
		t.Errorf("raising a maximum moved the layout id (0x%016x -> 0x%016x) — §19.4's edit 3 is absorbed on the DEFAULT path and a moved id refuses it",
			baseline, after.LayoutId)
	}
	if after.MaxBytes <= baselineMax {
		t.Errorf("raising a maximum did not grow the storage: %d -> %d", baselineMax, after.MaxBytes)
	}
}

// The digest SEES layout: a moved offset, a changed size, a different pitch
// (§19.3). Its negative control is the sabotage in the Makefile's
// block-digest-negative-control target; this is the positive half.
func TestBlockDigestMovesOnALayoutEdit(t *testing.T) {
	u := loadRender(t)
	baseline := ir.Blocks(u).Block("RenderFrame").LayoutId

	// append a field at the end of a ROW: the row grows and so does its
	// derived pitch, which is exactly what §19.4's edit 2 says moves the id
	ship := u.Tables["RenderShip"]
	ship.Fields = append(ship.Fields, &ir.Field{Name: "extra", Type: ir.FieldType{Kind: ir.TInt, Signed: false, Width: 32}})
	if got := ir.Blocks(u).Block("RenderFrame").LayoutId; got == baseline {
		t.Error("appending a field to a row did not move the layout id — §19.4's edit 2 must refuse under BlockOpen")
	}
}
