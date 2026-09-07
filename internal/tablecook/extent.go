// THE NODE EXTENT (docs/SPEC-TABLES.md §2.9, §7.2, §7.6): the bytes a node's
// UNBOUNDED ARRAYS take BEHIND the record's own storage, and the walk that
// carves them.
//
// A `[]T` slot is sixteen bytes in the record — an int64 SELF-RELATIVE delta
// and an int32 count — and the elements themselves are not a node of their
// own: they ride inside the HOLDER'S node, after the record, so a deref is one
// add and an index is one multiply, in place, with nothing to parse. A node's
// SIZE therefore depends on its VALUE, which is the one thing the fixed
// classes never made true, and the layout takes the instance the numbering
// walked for exactly that reason.
//
// THE ORDER IS PRE-ORDER, and it is the C++ reference's own
// (`internal/codegen/cpptable/extent.go`): the record's fields in declaration
// order, each container at its own position, a list's WHOLE ARRAY first and
// then, element by element in INDEX order, the arrays of any list an element
// holds by value. Two walks take it — one measuring, one writing — and they
// have to agree exactly, so they are one walk here with the writing half
// switched off, rather than two that could drift.
//
// A POINTER IS NOT AN EDGE HERE. A pointee is its own node with its own
// extent, and so is a byte buffer, which is why this walk descends only
// by-value edges: a nested record, an array element, a union's set arm.
package tablecook

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// recordHasExtent reports a record with any list reachable BY VALUE — the
// records whose node is its storage PLUS an extent, and whose record storage
// is therefore rounded to the region's alignment floor before the extent
// begins, exactly as the reference rounds it.
//
// A MAP counts too, because the reference sizes a map-bearing node the same
// way. The tool's cook surface refuses a map-bearing unit by name before this
// walk runs (compiler/tablesmaps.go), so the carve below never meets one; this
// predicate agrees with the reference regardless, so the two never disagree
// about a node's SIZE for a unit only one of them can write.
func recordHasExtent(u *ir.Unit, st *ir.Struct) bool {
	if st == nil {
		return false
	}
	for _, f := range st.Fields {
		if f.IsMap() || f.IsList() {
			return true
		}
		if f.Type.Pointer || f.Type.Blob() || f.Type.Kind != ir.TNamed {
			continue
		}
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			if recordHasExtent(u, ref) {
				return true
			}
		case *ir.Union:
			if unionHasExtent(u, ref, map[*ir.Union]bool{}) {
				return true
			}
		}
	}
	return false
}

// unionHasExtent reports a union with a container below an ARM held by value
// (§2.6). A pointer arm and a byte buffer arm reach nodes, not this node's
// extent, so neither is one.
func unionHasExtent(u *ir.Unit, un *ir.Union, seen map[*ir.Union]bool) bool {
	if un == nil || seen[un] {
		return false
	}
	seen[un] = true
	for _, v := range un.Variants {
		if v.F == nil {
			continue
		}
		if v.F.IsMap() || v.F.IsList() {
			return true
		}
		if v.F.Type.Pointer || v.F.Type.Blob() || v.F.Type.Kind != ir.TNamed {
			continue
		}
		switch ref := v.F.Type.Ref.(type) {
		case *ir.Struct:
			if recordHasExtent(u, ref) {
				return true
			}
		case *ir.Union:
			if unionHasExtent(u, ref, seen) {
				return true
			}
		}
	}
	return false
}

// recordBytes is one node's RECORD half: its `sizeof` for a record with no
// extent, and that rounded to the region's alignment floor for one with an
// extent, which is where the extent begins (§7.2).
func recordBytes(u *ir.Unit, st *ir.Struct) int64 {
	ml := ir.RecordLayout(u, st)
	if !recordHasExtent(u, st) {
		return ml.Size
	}
	return alignUp(ml.Size, ir.RegionAlignFloor)
}

// carver is the one extent walk, in both its halves. With `w` nil it MEASURES
// — the running offset only — and with `w` set it WRITES: the elements copied
// into the extent and the holder's slot pointed at them. `extentAt` is where
// the node's extent begins in the region; it is unused by a measure.
type carver struct {
	m        *tabletext.Model
	w        *regionWriter
	extentAt int64
	at       int64 // the running offset inside the extent
}

// extentOf is the whole extent one node's value takes, from a fresh offset:
// what the layout reserves for it beside the record's own storage.
func extentOf(m *tabletext.Model, st *ir.Struct, inst *tabletext.Instance) (int64, error) {
	if !recordHasExtent(m.Unit, st) {
		return 0, nil
	}
	c := &carver{m: m}
	if err := c.record(0, st, inst); err != nil {
		return 0, err
	}
	return c.at, nil
}

// record walks one record's fields in declaration order. `recordAt` is the
// record's own byte address in the region, which the WRITING half needs so a
// container's slot is filled where the field sits; a measure ignores it.
func (c *carver) record(recordAt int64, st *ir.Struct, inst *tabletext.Instance) error {
	if inst == nil {
		return nil
	}
	ml := ir.RecordLayout(c.m.Unit, st)
	for i := range ml.Fields {
		fl := &ml.Fields[i]
		fv := instField(inst, fl.Field)
		if fv == nil {
			continue
		}
		if err := c.field(recordAt+fl.Offset, fl.Field, fv); err != nil {
			return fmt.Errorf("%s.%s: %w", st.Name, fl.Field.Name, err)
		}
	}
	return nil
}

func (c *carver) field(at int64, f *ir.Field, fv *tabletext.Field) error {
	if f.IsMap() {
		// the tool's cook surface refuses a map-bearing unit by name before
		// this walk runs; reaching here would mean that refusal moved
		return fmt.Errorf("internal: a map slot reached the extent walk, and the tool's cook refuses a map-bearing unit at its surface (docs/SPEC-TABLES.md §2.8)")
	}
	pieces := ir.FieldPieces(c.m.Unit, f, at)
	if len(pieces) == 0 {
		return nil
	}
	if f.IsList() {
		return c.list(pieces[0].Offset, f, fv)
	}
	if f.Type.Pointer || f.Type.Blob() {
		return nil // a pointee and a byte buffer are nodes, not this extent
	}
	if !fieldHasExtent(c.m.Unit, f) {
		return nil
	}
	value := pieces[0]
	step := elementBytes(c.m.Unit, f)
	switch f.Array {
	case ir.ArrayNone:
		return c.element(value.Offset, f, &fv.Cell)
	case ir.ArrayCounted:
		live := min(int64(fv.Count), int64(len(fv.Elems)))
		for i := range live {
			if err := c.element(value.Offset+i*step, f, &fv.Elems[i]); err != nil {
				return err
			}
		}
		// AN UNREACHED NON-EMPTY EXTENT IS REFUSED (§7.6): a counted array's
		// slots past its live count are storage the walk does not reach, so a
		// list with elements in one names bytes the region will not hold. It
		// is the refusal §7.6 gives a pointer in the same place.
		for i := live; i < int64(len(fv.Elems)); i++ {
			probe := &carver{m: c.m}
			if err := probe.element(0, f, &fv.Elems[i]); err != nil {
				return err
			}
			if probe.at != 0 {
				return fmt.Errorf("slot %d is past the live count %d and holds %d bytes of extent: a cook cannot carry storage the walk does not reach (docs/SPEC-TABLES.md §7.6)", i, fv.Count, probe.at)
			}
		}
		return nil
	default:
		for i := range int64(len(fv.Elems)) {
			if err := c.element(value.Offset+i*step, f, &fv.Elems[i]); err != nil {
				return err
			}
		}
		return nil
	}
}

// list carves ONE unbounded array: the array's own bytes first, at the
// element's alignment, then the slot's sixteen bytes, then — element by
// element in INDEX order — the arrays of any list an element holds by value.
func (c *carver) list(at int64, f *ir.Field, fv *tabletext.Field) error {
	size, align := ir.ListElementLayout(c.m.Unit, f)
	count := min(int64(fv.Count), int64(len(fv.Elems)))
	c.at = alignUp(c.at, align)
	start := c.at
	c.at += count * size

	if c.w != nil {
		array := c.extentAt + start
		// THE SIXTEEN BYTES OF THE SLOT: the self-relative delta from the
		// slot's own address to the array, and zero for an empty list, whose
		// reference is null in every encoding (§2.9)
		if count > 0 {
			c.w.putI64(at, array-at)
		} else {
			c.w.putI64(at, RefNull)
		}
		c.w.putI32(at+8, int32(count))
		for i := range count {
			if err := c.w.element(array+i*size, f, &fv.Elems[i]); err != nil {
				return err
			}
		}
	}
	if !listElementHasExtent(c.m.Unit, f) {
		return nil
	}
	// THEN, ELEMENT BY ELEMENT IN INDEX ORDER, the arrays each element holds
	// by value — after the whole array, which is what makes the order
	// PRE-ORDER and what a reader indexing the array depends on. A measure
	// runs with `extentAt` zero, and the addresses it computes are never read.
	for i := range count {
		if err := c.element(c.extentAt+start+i*size, f, &fv.Elems[i]); err != nil {
			return err
		}
	}
	return nil
}

// element descends ONE by-value element or nested value.
func (c *carver) element(at int64, f *ir.Field, cell *tabletext.Cell) error {
	if f.Type.Pointer || f.Type.Kind != ir.TNamed {
		return nil
	}
	switch ref := f.Type.Ref.(type) {
	case *ir.Struct:
		sub := cell.Tab
		if sub == nil {
			sub = c.m.New(ref)
		}
		return c.record(at, ref, sub)
	case *ir.Union:
		if cell.U == 0 || int(cell.U) > len(ref.Variants) {
			return nil
		}
		arm := ref.Variants[cell.U-1]
		if arm.Void() {
			return nil
		}
		_, _, _, armOffset := ir.UnionLayout(c.m.Unit, ref)
		if arm.Body() {
			sub := cell.Tab
			if sub == nil {
				sub = c.m.New(arm.Ref)
			}
			return c.record(at+armOffset, arm.Ref, sub)
		}
		fv := cell.Arm
		if fv == nil {
			fv = c.m.NewArm(arm)
		}
		return c.field(at+armOffset, arm.F, fv)
	}
	return nil
}

// fieldHasExtent reports a field whose by-value storage can hold a container.
func fieldHasExtent(u *ir.Unit, f *ir.Field) bool {
	if f.Type.Pointer || f.Type.Blob() || f.Type.Kind != ir.TNamed {
		return false
	}
	switch ref := f.Type.Ref.(type) {
	case *ir.Struct:
		return recordHasExtent(u, ref)
	case *ir.Union:
		return unionHasExtent(u, ref, map[*ir.Union]bool{})
	}
	return false
}

// listElementHasExtent reports a list whose ELEMENT can hold a container of
// its own — a list of tables that hold lists, which is the depth §2.9's
// measure sums at.
func listElementHasExtent(u *ir.Unit, f *ir.Field) bool {
	return fieldHasExtent(u, f)
}
