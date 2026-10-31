package rusttable

// THE NODE EXTENT ON THE COOK WRITE SIDE (docs/SPEC-TABLES.md §2.8, §2.9,
// §7.6): where a map's generated `{ key, value }` entries and a list's
// elements ride. Neither is a node of its own. Both are by-value records
// inside the HOLDER'S node, after the record's own storage rounded to the
// region's alignment floor, and the holder's sixteen-byte slot holds an int64
// SELF-RELATIVE delta to the array and an int32 count (an empty container is
// the null reference and a zero count).
//
// THE ORDER IS PRE-ORDER, the C++ reference's (internal/codegen/cpptable/
// extent.go, ExtentAt and ExtentPack) and the tool's (internal/tablecook/
// extent.go): the record's fields in declaration order, each container at its
// own position, a container's WHOLE ARRAY first at its element's alignment,
// then, element by element in index order (entry by entry in ASCENDING KEY
// order for a map), the arrays any element holds by value. A pointer and a
// byte buffer are nodes, not this extent, so the walk descends only by-value
// edges: a nested record, an array element, a union's set arm, a map entry.
//
// ONE WALK, TWO HALVES: `<t>_cook_carve` with no writer MEASURES (the running
// offset only, which is what the layout reserves for the node), and with one
// it WRITES the arrays and points each slot at its array. The writer checks
// that the bytes it carved are the bytes the layout reserved.
//
// AN UNREACHED NON-EMPTY SLOT (§7.6) cannot arise here: a counted array is a
// `Vec` whose length IS its live count, so there is no slot past the count to
// hold a container the walk does not reach.

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// cookHasExtent reports a record with any list or map reachable BY VALUE: the
// records whose node is its storage, rounded to the region's alignment floor,
// PLUS an extent (internal/tablecook's recordHasExtent).
func cookHasExtent(st *ir.Struct) bool {
	return cookHasExtentSeen(st, map[*ir.Struct]bool{}, map[*ir.Union]bool{})
}

func cookHasExtentSeen(st *ir.Struct, seenS map[*ir.Struct]bool, seenU map[*ir.Union]bool) bool {
	if st == nil || seenS[st] {
		return false
	}
	seenS[st] = true
	for _, f := range st.Fields {
		if cookFieldHasExtent(f, seenS, seenU) {
			return true
		}
	}
	return false
}

// cookFieldHasExtent reports a field that is a container, or whose by-value
// storage can hold one.
func cookFieldHasExtent(f *ir.Field, seenS map[*ir.Struct]bool, seenU map[*ir.Union]bool) bool {
	if f.IsMap() || f.IsList() {
		return true
	}
	if f.Type.Pointer || f.Type.Blob() || f.Type.Kind != ir.TNamed {
		return false
	}
	switch ref := f.Type.Ref.(type) {
	case *ir.Struct:
		return cookHasExtentSeen(ref, seenS, seenU)
	case *ir.Union:
		if seenU[ref] {
			return false
		}
		seenU[ref] = true
		for _, v := range ref.Variants {
			if v.F != nil && cookFieldHasExtent(v.F, seenS, seenU) {
				return true
			}
		}
	}
	return false
}

func cookFieldExtent(f *ir.Field) bool {
	return cookFieldHasExtent(f, map[*ir.Struct]bool{}, map[*ir.Union]bool{})
}

// cookExtentAlign is the greatest alignment of any element or entry the
// record's extent can hold, at any depth: a list's element, a map's
// generated entry, and those their elements hold in turn. 1 for a record
// with no extent.
func cookExtentAlign(u *ir.Unit, st *ir.Struct) int64 {
	return cookExtentAlignSeen(u, st, map[*ir.Struct]bool{}, map[*ir.Union]bool{})
}

func cookExtentAlignSeen(u *ir.Unit, st *ir.Struct, seenS map[*ir.Struct]bool, seenU map[*ir.Union]bool) int64 {
	if st == nil || seenS[st] {
		return 1
	}
	seenS[st] = true
	align := int64(1)
	for _, f := range st.Fields {
		align = max(align, cookFieldExtentAlign(u, f, seenS, seenU))
	}
	return align
}

func cookFieldExtentAlign(u *ir.Unit, f *ir.Field, seenS map[*ir.Struct]bool, seenU map[*ir.Union]bool) int64 {
	switch {
	case f.IsMap():
		return max(ir.RecordLayout(u, f.MapEntry).Align, cookExtentAlignSeen(u, f.MapEntry, seenS, seenU))
	case f.IsList():
		_, align := ir.ListElementLayout(u, f)
		elem := scalarOnly(f)
		elem.Type.Pointer = f.Type.Pointer
		return max(align, cookFieldExtentAlign(u, elem, seenS, seenU))
	}
	if f.Type.Pointer || f.Type.Blob() || f.Type.Kind != ir.TNamed {
		return 1
	}
	switch ref := f.Type.Ref.(type) {
	case *ir.Struct:
		return cookExtentAlignSeen(u, ref, seenS, seenU)
	case *ir.Union:
		if seenU[ref] {
			return 1
		}
		seenU[ref] = true
		align := int64(1)
		for _, v := range ref.Variants {
			if v.F != nil {
				align = max(align, cookFieldExtentAlign(u, v.F, seenS, seenU))
			}
		}
		return align
	}
	return 1
}

// cookNodeAlign is a node's alignment in the region: the greater of its
// record's and every element's its extent can hold, so an array aligned
// relative to the extent is aligned in the region too.
func cookNodeAlign(u *ir.Unit, st *ir.Struct) int64 {
	return max(ir.RecordLayout(u, st).Align, cookExtentAlign(u, st))
}

// cookRecordBytes is one node's RECORD half: its sizeof, and for a record
// that can hold an extent that rounded to the region's alignment floor and to
// the greatest element alignment its extent can hold, which is where the
// extent begins (§7.2).
func cookRecordBytes(u *ir.Unit, st *ir.Struct) int64 {
	ml := ir.RecordLayout(u, st)
	if !cookHasExtent(st) {
		return ml.Size
	}
	a := max(ir.RegionAlignFloor, cookExtentAlign(u, st))
	return (ml.Size + a - 1) / a * a
}

// emitCookCarver emits `<t>_cook_carve` for one carried record that can hold
// an extent.
func (g *gen) emitCookCarver(b *strings.Builder, st *ir.Struct) {
	ml := ir.RecordLayout(g.unit, st)
	fmt.Fprintf(b, "/// %s — the node extent %s's lists and maps take, PRE-ORDER\n", fn(st.Name, "cook_carve"), st.Name)
	b.WriteString("/// (docs/SPEC-TABLES.md §2.8, §2.9, §7.6): with no writer it measures, with one\n")
	b.WriteString("/// it lays every array and points every slot at its array. `at` is the record's\n")
	b.WriteString("/// own region offset, which a measure does not read.\n")
	b.WriteString("#[cfg(feature = \"cook\")]\n#[allow(unused_variables, unused_mut, clippy::identity_op, clippy::needless_option_as_deref, clippy::single_match)]\n")
	fmt.Fprintf(b, "pub fn %s(v: &%s, c: &mut TableCarve, mut w: Option<&mut TableRegionWriter>, at: i64) {\n", fn(st.Name, "cook_carve"), st.Name)
	for i := range ml.Fields {
		fl := &ml.Fields[i]
		if !cookFieldExtent(fl.Field) {
			continue
		}
		g.emitCarveField(b, fl.Field, "v."+ir.RustSnake(fl.Field.Name), "at", fl.Offset, "    ")
	}
	b.WriteString("}\n\n")
}

// emitCarveField carves one field whose storage is, or can hold, a container.
// `x` is a place expression of the field's storage type, at base + off.
func (g *gen) emitCarveField(b *strings.Builder, f *ir.Field, x, base string, off int64, ind string) {
	pieces := ir.FieldPieces(g.unit, f, off)
	if len(pieces) == 0 {
		return
	}
	slot := cookAt(base, pieces[0].Offset)
	switch {
	case f.IsMap():
		g.emitCarveMap(b, f, x, slot, ind)
		return
	case f.IsList():
		if f.Type.Optional {
			fmt.Fprintf(b, "%sif let Some(xs) = %s {\n", ind, cookRef(x))
			g.emitCarveList(b, f, "(*xs)", slot, ind+"    ")
			fmt.Fprintf(b, "%s} else {\n", ind)
			size, align := ir.ListElementLayout(g.unit, f)
			fmt.Fprintf(b, "%s    let _ = c.place(0, %d, %d); // an absent list is empty, and its array is still aligned\n", ind, size, align)
			fmt.Fprintf(b, "%s}\n", ind)
			return
		}
		g.emitCarveList(b, f, x, slot, ind)
		return
	}
	if f.Type.Optional {
		// an absent optional holds its declared default, which carries no
		// container: nothing to carve
		inner := *f
		inner.Type.Optional = false
		fmt.Fprintf(b, "%sif let Some(o) = %s {\n", ind, cookRef(x))
		g.emitCarveField(b, &inner, "(*o)", base, off, ind+"    ")
		fmt.Fprintf(b, "%s}\n", ind)
		return
	}
	if f.KeyEnum != "" || f.Array != ir.ArrayNone {
		// every slot of a fixed or keyed array, and every LIVE slot of a
		// counted one (a Vec past its length holds nothing); a slot the Vec
		// does not hold is the element's default, which carries no container
		step := cookElementBytes(g.unit, f)
		elem := scalarOnly(f)
		fmt.Fprintf(b, "%sfor (i, e) in %s.iter().enumerate().take(%d) {\n", ind, x, f.ArrayBound)
		g.emitCarveValue(b, elem, "(*e)", fmt.Sprintf("%s + i as i64 * %d", slot, step), ind+"    ")
		fmt.Fprintf(b, "%s}\n", ind)
		return
	}
	g.emitCarveValue(b, f, x, slot, ind)
}

// emitCarveValue descends ONE by-value value at `at`: a record through its own
// carve, a union through its set arm.
func (g *gen) emitCarveValue(b *strings.Builder, f *ir.Field, x, at, ind string) {
	if f.Type.Pointer || f.Type.Blob() || f.Type.Kind != ir.TNamed {
		return
	}
	switch ref := f.Type.Ref.(type) {
	case *ir.Struct:
		if cookHasExtent(ref) {
			fmt.Fprintf(b, "%s%s(%s, c, w.as_deref_mut(), %s);\n", ind, fn(ref.Name, "cook_carve"), cookRef(x), at)
		}
	case *ir.Union:
		if !cookFieldExtent(f) {
			return
		}
		_, _, _, armOffset := ir.UnionLayout(g.unit, ref)
		fmt.Fprintf(b, "%smatch %s {\n", ind, cookRef(x))
		for _, arm := range ref.Variants {
			if arm.Void() || arm.F == nil || !cookFieldExtent(arm.F) {
				continue
			}
			fmt.Fprintf(b, "%s    %s::%s(a) => {\n", ind, ref.Name, rustVariant(arm.Name))
			armAt := fmt.Sprintf("%s + %d", at, armOffset)
			g.emitCarveField(b, arm.F, "(*a)", armAt, 0, ind+"        ")
			fmt.Fprintf(b, "%s    }\n", ind)
		}
		fmt.Fprintf(b, "%s    _ => {}\n", ind)
		fmt.Fprintf(b, "%s}\n", ind)
	}
}

// emitCarveList carves ONE unbounded array: the whole element array first at
// the element's alignment, the slot pointed at it, then each element's own
// containers in index order.
func (g *gen) emitCarveList(b *strings.Builder, f *ir.Field, x, slot, ind string) {
	size, align := ir.ListElementLayout(g.unit, f)
	elem := scalarOnly(f)
	elem.Type.Pointer = f.Type.Pointer
	fmt.Fprintf(b, "%s{\n", ind)
	in := ind + "    "
	fmt.Fprintf(b, "%slet xs = %s;\n", in, cookRef(x))
	fmt.Fprintf(b, "%slet start = c.place(xs.len(), %d, %d);\n", in, size, align)
	fmt.Fprintf(b, "%sif let Some(w) = w.as_deref_mut() {\n", in)
	fmt.Fprintf(b, "%s    let array = c.extent_at + start;\n", in)
	fmt.Fprintf(b, "%s    w.put_slot(%s, array, xs.len());\n", in, slot)
	fmt.Fprintf(b, "%s    for (i, e) in xs.iter().enumerate() {\n", in)
	fmt.Fprintf(b, "%s        let ea = array + i as i64 * %d;\n", in, size)
	g.emitCookElement(b, elem, "(*e)", "ea", in+"        ")
	fmt.Fprintf(b, "%s    }\n", in)
	fmt.Fprintf(b, "%s}\n", in)
	if cookFieldExtent(elem) {
		fmt.Fprintf(b, "%sfor (i, e) in xs.iter().enumerate() {\n", in)
		g.emitCarveValue(b, elem, "(*e)", fmt.Sprintf("c.extent_at + start + i as i64 * %d", size), in+"    ")
		fmt.Fprintf(b, "%s}\n", in)
	}
	fmt.Fprintf(b, "%s}\n", ind)
}

// emitCarveMap carves ONE map: the entry array first at alignof( Entry ), the
// entries in ASCENDING KEY ORDER, the slot pointed at it, then each entry's own
// containers in key order.
func (g *gen) emitCarveMap(b *strings.Builder, f *ir.Field, x, slot, ind string) {
	entry := f.MapEntry
	ml := ir.RecordLayout(g.unit, entry)
	key := ir.RustSnake(ir.MapKeyFieldName)
	fmt.Fprintf(b, "%s{\n", ind)
	in := ind + "    "
	fmt.Fprintf(b, "%slet xs = %s;\n", in, cookRef(x))
	fmt.Fprintf(b, "%slet mut order: Vec<usize> = (0..xs.len()).collect();\n", in)
	fmt.Fprintf(b, "%sorder.sort_by(|&a, &b| %s);\n", in, mapKeyOrd(f, "xs[a]."+key, "xs[b]."+key))
	fmt.Fprintf(b, "%slet start = c.place(xs.len(), %d, %d);\n", in, ml.Size, max(ml.Align, 1))
	fmt.Fprintf(b, "%sif let Some(w) = w.as_deref_mut() {\n", in)
	fmt.Fprintf(b, "%s    let array = c.extent_at + start;\n", in)
	fmt.Fprintf(b, "%s    w.put_slot(%s, array, xs.len());\n", in, slot)
	fmt.Fprintf(b, "%s    for (i, &k) in order.iter().enumerate() {\n", in)
	fmt.Fprintf(b, "%s        %s(&xs[k], w, array + i as i64 * %d);\n", in, fn(entry.Name, "cook_body"), ml.Size)
	fmt.Fprintf(b, "%s    }\n", in)
	fmt.Fprintf(b, "%s}\n", in)
	if cookHasExtent(entry) {
		fmt.Fprintf(b, "%sfor (i, &k) in order.iter().enumerate() {\n", in)
		fmt.Fprintf(b, "%s    %s(&xs[k], c, w.as_deref_mut(), c.extent_at + start + i as i64 * %d);\n", in, fn(entry.Name, "cook_carve"), ml.Size)
		fmt.Fprintf(b, "%s}\n", in)
	}
	fmt.Fprintf(b, "%s}\n", ind)
}

// cookCarveRust is the carve's runtime, beside the region writer: the running
// offset and where the node's extent begins.
const cookCarveRust = `
/// One node's extent being carved (docs/SPEC-TABLES.md §2.8, §2.9): where the
/// extent begins in the region and the running offset inside it.
#[cfg(feature = "cook")]
pub struct TableCarve {
    pub extent_at: i64,
    pub at: i64,
}

#[cfg(feature = "cook")]
impl TableCarve {
    /// Reserve one container's whole array, count x size at align, and answer
    /// where it starts inside the extent.
    pub fn place(&mut self, count: usize, size: i64, align: i64) -> i64 {
        self.at = (self.at + align - 1) / align * align;
        let start = self.at;
        self.at += count as i64 * size;
        start
    }
}

#[cfg(feature = "cook")]
impl TableRegionWriter<'_> {
    /// A container's sixteen-byte slot: the SELF-RELATIVE delta from the slot
    /// to its array and the int32 count, or the null reference and zero for
    /// an empty one (§2.8, §2.9).
    pub fn put_slot(&mut self, at: i64, array: i64, count: usize) {
        if count == 0 {
            self.put(at, 0, 8);
            self.put(at + 8, 0, 4);
            return;
        }
        if count > i32::MAX as usize {
            self.ok = false;
            return;
        }
        self.put(at, (array - at) as u64, 8);
        self.put(at + 8, count as u64, 4);
    }
}
`
