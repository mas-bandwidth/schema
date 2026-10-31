package rusttable

// THE COOK WRITE SIDE FROM THE STORAGE VALUE (docs/SPEC-TABLES.md §7.6): the
// Rust spelling of the region writer `schema cook` runs
// (internal/tablecook/region.go), over the variable wire's storage structs.
//
// A cook is written from the VALUE a load produced, the storage struct the
// variable codec reads into — the same value Go's <T>CookFrom and C++'s Cook
// take. The writer is GENERATED per record rather than descriptor-driven,
// because a Rust storage struct is not a C ABI record (a string is a String, an
// array a Vec, an edge an Rc) and so has no offsets a walker could read. What
// it writes is laid out by ir.RecordLayout, ir.FieldPieces and ir.UnionLayout —
// the compiler's one C ABI model — so the bytes are the cook's by construction.
//
// The region is the nodes of one pointer graph in the numbering's DEPTH-FIRST
// PRE-ORDER (§3.1, §6.3): the root at the base, every node at its own type's
// alignment, a blob at eight behind its u32 length and four zero bytes (a
// *string blob carries its terminator), and the data part rounded to the
// region's alignment. The numbering is the wire save's own walk
// (<T>_number), so a wire and a cook agree on which node is which.
//
// The fixed-point kind (fixed(I, F) / ufixed(I, F), §4.3) is laid down as its
// raw storage integer at the I+F width, exactly as region.go's element does
// for TInt and TFixed alike, a 128-bit width through the wide put. A float is
// FINITE (§3, §3.3): a NaN or an infinity in the value refuses the cook, as
// tablewire.RefuseNonFinite refuses it in the engine.
//
// A construct this writer does not reach — an unbounded list or a map (the
// node extent of §2.9 and §2.8), unbounded text and text as an array element —
// leaves the root WITHOUT a <T>_cook_write, so a caller meets a missing
// function and never a wrong byte. A root the variable class does not carry
// has no storage struct and so no writer either.

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// cookWriteSet is the unit's records this writer carries: every emitted body
// whose own fields it reaches and whose every reached record it carries too.
func cookWriteSet(u *ir.Unit, vc *variableClass) map[string]bool {
	ok := map[string]bool{}
	for n := range vc.emitted {
		ok[n] = cookWriteFieldsCarried(vStruct(u, n))
	}
	for changed := true; changed; {
		changed = false
		for n := range vc.emitted {
			if !ok[n] {
				continue
			}
			for _, ref := range cookWriteReached(vStruct(u, n)) {
				if !vc.emitted[ref] || !ok[ref] {
					ok[n] = false
					changed = true
					break
				}
			}
		}
	}
	return ok
}

// cookWriteFieldsCarried reports whether every field of one record (and every
// arm of every union it holds) is a construct this writer lays down.
func cookWriteFieldsCarried(st *ir.Struct) bool {
	if st == nil {
		return false
	}
	for _, f := range st.Fields {
		if !cookWriteFieldCarried(f, map[*ir.Union]bool{}) {
			return false
		}
	}
	return true
}

func cookWriteFieldCarried(f *ir.Field, seen map[*ir.Union]bool) bool {
	switch {
	case f.Type.TextUnbounded():
		return false
	case f.IsMap():
		return f.MapEntry != nil && cookWriteFieldsCarried(f.MapEntry)
	case (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString) && !f.Type.Pointer &&
		(f.Array != ir.ArrayNone || f.KeyEnum != ""):
		return false
	case f.KeyEnum != "" && (f.KeyEnumRef == nil || f.ArrayBound <= 0):
		return false
	}
	if un := unionOf(f); un != nil && !seen[un] {
		seen[un] = true
		for _, v := range un.Variants {
			if v.F != nil && !cookWriteFieldCarried(v.F, seen) {
				return false
			}
		}
	}
	return true
}

// cookWriteReached is every record one record's storage names: by value, by
// element, by pointer and through a union arm.
func cookWriteReached(st *ir.Struct) []string {
	var out []string
	seen := map[*ir.Union]bool{}
	var walk func(f *ir.Field)
	walk = func(f *ir.Field) {
		if f == nil {
			return
		}
		if f.IsMap() && f.MapEntry != nil {
			out = append(out, f.MapEntry.Name)
		}
		if ref, ok := f.Type.Ref.(*ir.Struct); ok && !f.Type.Blob() {
			out = append(out, ref.Name)
		}
		if un := unionOf(f); un != nil && !seen[un] {
			seen[un] = true
			for _, v := range un.Variants {
				walk(v.F)
			}
		}
	}
	for _, f := range st.Fields {
		walk(f)
	}
	return out
}

// cookElementBytes is the stride of one array slot: the element's own storage
// size, which is what ir.FieldPieces sized the whole array by
// (internal/tablecook/region.go's elementBytes).
func cookElementBytes(u *ir.Unit, f *ir.Field) int64 {
	single := *f
	single.Array = ir.ArrayNone
	single.ArrayBound = 0
	single.KeyEnum = ""
	single.KeyEnumRef = nil
	single.Type.Optional = false
	var end int64
	for _, p := range ir.FieldPieces(u, &single, 0) {
		if p.Offset+p.Size > end {
			end = p.Offset + p.Size
		}
	}
	return end
}

// cookRef is a borrow of a place expression: `(*x)` is the reference `x`
// already, and anything else is borrowed.
func cookRef(x string) string {
	if strings.HasPrefix(x, "(*") && strings.HasSuffix(x, ")") && !strings.ContainsAny(x[2:len(x)-1], "()") {
		return x[2 : len(x)-1]
	}
	return "&" + x
}

// cookVal is a place expression read as a value: `(*x)` is `*x`.
func cookVal(x string) string {
	if strings.HasPrefix(x, "(*") && strings.HasSuffix(x, ")") && !strings.ContainsAny(x[2:len(x)-1], "()") {
		return x[1 : len(x)-1]
	}
	return x
}

// cookCast is a value converted from its storage type to want, with no cast
// when the two are one type.
func cookCast(x, have, want string) string {
	if have == want {
		return cookVal(x)
	}
	return cookVal(x) + " as " + want
}

// at spells a region offset: the record's base plus a constant.
func cookAt(base string, off int64) string {
	if off == 0 {
		return base
	}
	return fmt.Sprintf("%s + %d", base, off)
}

// emitCookWriteFromStorage emits the file's cook writer: one body writer per
// carried record, one writer per carried union, the node dispatch and one
// <T>_cook_write per carried root. Everything is behind the `cook` feature,
// as every other piece of the cooked form is.
func (g *gen) emitCookWriteFromStorage(b *strings.Builder, names []string) {
	cw := cookWriteSet(g.unit, g.vc)
	any := false
	for _, n := range names {
		if cw[n] {
			any = true
			break
		}
	}
	if !any {
		return
	}
	b.WriteString("// ---- the COOK WRITE side from the storage value (docs/SPEC-TABLES.md §7.6) ----\n\n")
	for _, n := range names {
		if cw[n] {
			g.emitCookBodyWriter(b, vStruct(g.unit, n))
		}
	}
	for _, un := range g.collectVariableUnions(names) {
		if g.cookUnionCarried(un, cw) {
			g.emitCookUnionWriter(b, un)
		}
	}
	for _, n := range names {
		if st := vStruct(g.unit, n); cw[n] && cookHasExtent(st) {
			g.emitCookCarver(b, st)
		}
	}
	var targets []*ir.Struct
	for _, st := range g.pointerTargets(names) {
		if cw[st.Name] {
			targets = append(targets, st)
		}
	}
	rootsAny := false
	for _, n := range names {
		if st := vStruct(g.unit, n); cw[n] && g.isVariableRoot(st) {
			rootsAny = true
		}
	}
	if !rootsAny {
		return
	}
	b.WriteString("/// One table node's cooked record size and alignment, by its type id (§7.2).\n")
	b.WriteString("#[cfg(feature = \"cook\")]\n#[allow(dead_code)]\n")
	if len(targets) == 0 {
		// a graph of the root alone: no table node to place
		b.WriteString("fn cook_record_layout(_type_id: u64) -> Option<(i64, i64)> {\n    None\n}\n\n")
	} else {
		b.WriteString("fn cook_record_layout(type_id: u64) -> Option<(i64, i64)> {\n    match type_id {\n")
		for _, st := range targets {
			ml := ir.RecordLayout(g.unit, st)
			fmt.Fprintf(b, "        0x%016x => Some((%d, %d)),\n", ir.TableWireId(st.WireName()), cookRecordBytes(g.unit, st), ml.Align)
		}
		b.WriteString("        _ => None,\n    }\n}\n\n")
	}
	b.WriteString("/// One table node's EXTENT, the bytes its lists and maps take behind its\n")
	b.WriteString("/// record (§2.8, §2.9), by its type id: what the layout reserves.\n")
	b.WriteString("#[cfg(feature = \"cook\")]\n#[allow(dead_code, unused_variables)]\n")
	b.WriteString("fn cook_record_extent(type_id: u64, node: &Rc<dyn Any>) -> i64 {\n    match type_id {\n")
	for _, st := range targets {
		if !cookHasExtent(st) {
			continue
		}
		fmt.Fprintf(b, "        0x%016x => match node.downcast_ref::<%s>() {\n", ir.TableWireId(st.WireName()), st.Name)
		fmt.Fprintf(b, "            Some(v) => {\n                let mut c = TableCarve { extent_at: 0, at: 0 };\n                %s(v, &mut c, None, 0);\n                c.at\n            }\n", fn(st.Name, "cook_carve"))
		b.WriteString("            None => 0,\n        },\n")
	}
	b.WriteString("        _ => 0,\n    }\n}\n\n")
	b.WriteString("/// One table node's cooked record, by its type id (§7.2).\n")
	b.WriteString("#[cfg(feature = \"cook\")]\n#[allow(dead_code, unused_variables)]\n")
	if len(targets) == 0 {
		b.WriteString("fn cook_record_write(_type_id: u64, _node: &Rc<dyn Any>, w: &mut TableRegionWriter, _at: i64) {\n    w.fail();\n}\n\n")
	} else {
		b.WriteString("fn cook_record_write(type_id: u64, node: &Rc<dyn Any>, w: &mut TableRegionWriter, at: i64) {\n    match type_id {\n")
		for _, st := range targets {
			fmt.Fprintf(b, "        0x%016x => match node.downcast_ref::<%s>() {\n", ir.TableWireId(st.WireName()), st.Name)
			if cookHasExtent(st) {
				fmt.Fprintf(b, "            Some(v) => {\n                %s(v, w, at);\n", fn(st.Name, "cook_body"))
				b.WriteString("                // THE EXTENT WRITTEN IS THE EXTENT MEASURED (§2.8, §2.9)\n")
				fmt.Fprintf(b, "                let mut m = TableCarve { extent_at: 0, at: 0 };\n                %s(v, &mut m, None, 0);\n", fn(st.Name, "cook_carve"))
				fmt.Fprintf(b, "                let mut c = TableCarve { extent_at: at + %d, at: 0 };\n                %s(v, &mut c, Some(&mut *w), at);\n", cookRecordBytes(g.unit, st), fn(st.Name, "cook_carve"))
				b.WriteString("                if c.at != m.at {\n                    w.fail();\n                }\n            }\n")
			} else {
				fmt.Fprintf(b, "            Some(v) => %s(v, w, at),\n", fn(st.Name, "cook_body"))
			}
			b.WriteString("            None => w.fail(),\n        },\n")
		}
		b.WriteString("        _ => w.fail(),\n    }\n}\n\n")
	}
	for _, n := range names {
		st := vStruct(g.unit, n)
		if cw[n] && g.isVariableRoot(st) {
			g.emitCookRootWriter(b, st)
		}
	}
}

// cookUnionCarried reports whether every arm of a union names records the
// writer carries.
func (g *gen) cookUnionCarried(un *ir.Union, cw map[string]bool) bool {
	for _, v := range un.Variants {
		if v.F == nil {
			continue
		}
		if !cookWriteFieldCarried(v.F, map[*ir.Union]bool{}) {
			return false
		}
		if ref, ok := v.F.Type.Ref.(*ir.Struct); ok && !v.F.Type.Blob() && !cw[ref.Name] {
			return false
		}
		if inner := unionOf(v.F); inner != nil && inner != un && !g.cookUnionCarried(inner, cw) {
			return false
		}
	}
	return true
}

func (g *gen) emitCookBodyWriter(b *strings.Builder, st *ir.Struct) {
	ml := ir.RecordLayout(g.unit, st)
	fmt.Fprintf(b, "/// %s — one %s record (%d bytes, aligned %d) at `at` in the region\n", fn(st.Name, "cook_body"), st.Name, ml.Size, ml.Align)
	b.WriteString("/// (docs/SPEC-TABLES.md §7.2, §7.6): every piece through the cook's byte\n")
	b.WriteString("/// order, every byte the fields do not cover left zero.\n")
	b.WriteString("#[cfg(feature = \"cook\")]\n#[allow(unused_variables, clippy::identity_op)]\n")
	fmt.Fprintf(b, "pub fn %s(v: &%s, w: &mut TableRegionWriter, at: i64) {\n", fn(st.Name, "cook_body"), st.Name)
	for i := range ml.Fields {
		fl := &ml.Fields[i]
		g.emitCookField(b, fl.Field, "v."+ir.RustSnake(fl.Field.Name), "at", fl.Offset, "    ")
	}
	b.WriteString("}\n\n")
}

func (g *gen) emitCookUnionWriter(b *strings.Builder, un *ir.Union) {
	_, _, tag, armOffset := ir.UnionLayout(g.unit, un)
	fmt.Fprintf(b, "/// %s — the union's tag at offset 0 and the SET arm at offset %d\n", fn(un.Name, "cook_union"), armOffset)
	b.WriteString("/// (docs/SPEC-TABLES.md §2.6, §7.2); every other byte of its extent stays zero.\n")
	b.WriteString("#[cfg(feature = \"cook\")]\n#[allow(unused_variables, clippy::identity_op)]\n")
	fmt.Fprintf(b, "pub fn %s(v: &%s, w: &mut TableRegionWriter, at: i64) {\n", fn(un.Name, "cook_union"), un.Name)
	b.WriteString("    match v {\n")
	fmt.Fprintf(b, "        %s::None => {}\n", un.Name)
	for i, arm := range un.Variants {
		name := rustVariant(arm.Name)
		if arm.Void() {
			fmt.Fprintf(b, "        %s::%s => w.put(at, %d, %d),\n", un.Name, name, i+1, tag)
			continue
		}
		fmt.Fprintf(b, "        %s::%s(x) => {\n", un.Name, name)
		fmt.Fprintf(b, "            w.put(at, %d, %d);\n", i+1, tag)
		g.emitCookField(b, arm.F, "(*x)", "at", armOffset, "            ")
		b.WriteString("        }\n")
	}
	b.WriteString("    }\n}\n\n")
}

// emitCookField writes one field's whole storage, PIECE BY PIECE, as
// internal/tablecook/region.go's field does: `x` is a place expression of the
// field's storage type and the field begins at base + off.
func (g *gen) emitCookField(b *strings.Builder, f *ir.Field, x, base string, off int64, ind string) {
	pieces := ir.FieldPieces(g.unit, f, off)
	if len(pieces) == 0 {
		return
	}
	if f.Type.Optional {
		present := pieces[len(pieces)-1]
		inner := *f
		inner.Type.Optional = false
		fmt.Fprintf(b, "%sw.put(%s, if %s.is_some() { 1 } else { 0 }, 1);\n", ind, cookAt(base, present.Offset), x)
		fmt.Fprintf(b, "%smatch %s {\n", ind, cookRef(x))
		fmt.Fprintf(b, "%s    Some(o) => {\n", ind)
		g.emitCookFieldValue(b, &inner, "(*o)", base, off, ind+"        ")
		fmt.Fprintf(b, "%s    }\n", ind)
		// an ABSENT optional holds its declared default, which is what the
		// reference's instance carries and what `T x = {}` leaves (§2.3)
		fmt.Fprintf(b, "%s    None => {\n", ind)
		fmt.Fprintf(b, "%s        let d: %s = %s;\n", ind, variableFieldType(&inner), fieldDefault(&inner))
		g.emitCookFieldValue(b, &inner, "d", base, off, ind+"        ")
		fmt.Fprintf(b, "%s    }\n", ind)
		fmt.Fprintf(b, "%s}\n", ind)
		return
	}
	g.emitCookFieldValue(b, f, x, base, off, ind)
}

// emitCookFieldValue is a non-optional field's pieces.
func (g *gen) emitCookFieldValue(b *strings.Builder, f *ir.Field, x, base string, off int64, ind string) {
	pieces := ir.FieldPieces(g.unit, f, off)
	value := pieces[0]
	switch {
	case f.IsMap(), f.IsList():
		// THE SLOT IS THE CARVE'S (docs/SPEC-TABLES.md §2.8, §2.9): its
		// sixteen bytes point into the holder's node extent, which only the
		// carve knows; an empty container leaves them zero, the null reference
		// and the zero count
		return
	case f.Type.Blob():
		fmt.Fprintf(b, "%sw.ref_blob(%s, %s.as_ref().map(|b| Rc::as_ptr(b) as *const u8 as usize));\n", ind, cookAt(base, value.Offset), x)
		return
	case f.Type.Pointer && f.Array == ir.ArrayNone && f.KeyEnum == "":
		fmt.Fprintf(b, "%sw.ref_node(%s, %s);\n", ind, cookAt(base, value.Offset), cookRef(x))
		return
	case f.Type.Kind == ir.TString:
		fmt.Fprintf(b, "%sw.put_text(%s, %s.as_bytes(), %d, %s);\n", ind, cookAt(base, value.Offset), x, f.Type.Size, cookAt(base, pieces[1].Offset))
		return
	case f.Type.Kind == ir.TBytes:
		fmt.Fprintf(b, "%sw.put_text(%s, %s, %d, %s);\n", ind, cookAt(base, value.Offset), cookRef(x), f.Type.Size, cookAt(base, pieces[1].Offset))
		return
	case f.Type.Kind == ir.TWString:
		fmt.Fprintf(b, "%sw.put_units(%s, %s, %d, %s);\n", ind, cookAt(base, value.Offset), cookRef(x), f.Type.Size, cookAt(base, pieces[1].Offset))
		return
	case f.KeyEnum != "" || f.Array != ir.ArrayNone:
		bound := f.ArrayBound
		step := cookElementBytes(g.unit, f)
		elem := scalarOnly(f)
		elem.HasDefault = false
		elem.Type.Pointer = f.Type.Pointer
		if f.Array == ir.ArrayCounted {
			fmt.Fprintf(b, "%sif %s.len() > %d { w.fail(); }\n", ind, x, bound)
			fmt.Fprintf(b, "%sw.put(%s, %s.len() as u64, 4);\n", ind, cookAt(base, pieces[1].Offset), x)
		}
		fmt.Fprintf(b, "%sfor i in 0..%d_usize {\n", ind, bound)
		fmt.Fprintf(b, "%s    let ea = %s + i as i64 * %d;\n", ind, cookAt(base, value.Offset), step)
		// a slot past the live count holds the VALUE-INITIALIZED element:
		// zero for a scalar, the element type's own defaults for a record
		// (internal/tabletext/value.go's elementZero)
		if ref, ok := f.Type.Ref.(*ir.Struct); ok && !f.Type.Pointer && f.Type.Kind == ir.TNamed {
			fmt.Fprintf(b, "%s    match %s.get(i) {\n", ind, x)
			fmt.Fprintf(b, "%s        Some(e) => %s(e, w, ea),\n", ind, fn(ref.Name, "cook_body"))
			fmt.Fprintf(b, "%s        None => %s(&%s::default(), w, ea),\n", ind, fn(ref.Name, "cook_body"), ref.Name)
			fmt.Fprintf(b, "%s    }\n", ind)
		} else {
			fmt.Fprintf(b, "%s    if let Some(e) = %s.get(i) {\n", ind, x)
			g.emitCookElement(b, elem, "(*e)", "ea", ind+"        ")
			fmt.Fprintf(b, "%s    }\n", ind)
		}
		fmt.Fprintf(b, "%s}\n", ind)
		return
	}
	g.emitCookElement(b, f, x, cookAt(base, value.Offset), ind)
}

// emitCookElement writes ONE value of the field's declared type at `at`: an
// array element, or the scalar itself (region.go's element).
func (g *gen) emitCookElement(b *strings.Builder, f *ir.Field, x, at, ind string) {
	t := f.Type
	if t.Blob() {
		fmt.Fprintf(b, "%sw.ref_blob(%s, %s.as_ref().map(|b| Rc::as_ptr(b) as *const u8 as usize));\n", ind, at, x)
		return
	}
	if t.Pointer {
		fmt.Fprintf(b, "%sw.ref_node(%s, %s);\n", ind, at, cookRef(x))
		return
	}
	switch t.Kind {
	case ir.TBool:
		fmt.Fprintf(b, "%sw.put(%s, if %s { 1 } else { 0 }, 1);\n", ind, at, cookVal(x))
	case ir.TFloat32:
		// a float rides as its bit pattern, with no canonicalisation, and it
		// is FINITE (§3, §3.3): a NaN or an infinity refuses the cook
		fmt.Fprintf(b, "%sw.put_f32(%s, %s);\n", ind, at, cookVal(x))
	case ir.TFloat64:
		fmt.Fprintf(b, "%sw.put_f64(%s, %s);\n", ind, at, cookVal(x))
	case ir.TInt, ir.TFixed:
		// a fixed-point value is its raw storage integer at the I+F width
		// (§4.3, §7.2), which is region.go's one case for both kinds
		have := cookScalarRust(f)
		if t.Width > 64 {
			fmt.Fprintf(b, "%sw.put_wide(%s, %s);\n", ind, at, cookCast(x, have, "u128"))
			return
		}
		fmt.Fprintf(b, "%sw.put(%s, %s, %d);\n", ind, at, cookCast(x, have, "u64"), t.Width/8)
	case ir.TBits:
		width := 8
		if t.Width <= 32 {
			width = 4
		}
		fmt.Fprintf(b, "%sw.put(%s, %s, %d);\n", ind, at, cookCast(x, scalarRust(f), "u64"), width)
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			// the slot stores the ORDINAL, not the wire's name hash (§7.2)
			fmt.Fprintf(b, "%sw.put(%s, %s, %d);\n", ind, at, cookCast(x, ref.Name, "u64"), ir.StorageBitsFor(ref.Max)/8)
		case *ir.Flags:
			fmt.Fprintf(b, "%sw.put(%s, %s, 8);\n", ind, at, cookVal(x))
		case *ir.Struct:
			fmt.Fprintf(b, "%s%s(%s, w, %s);\n", ind, fn(ref.Name, "cook_body"), cookRef(x), at)
		case *ir.Union:
			fmt.Fprintf(b, "%s%s(%s, w, %s);\n", ind, fn(ref.Name, "cook_union"), cookRef(x), at)
		}
	}
}

// cookScalarRust is the storage type a scalar's value is held in: the
// variable codec's own spelling, and for the fixed-point kind its raw integer
// at the I+F storage width, signed for fixed and unsigned for ufixed (§4.3).
func cookScalarRust(f *ir.Field) string {
	if f.Type.Kind == ir.TFixed {
		if f.Type.Signed {
			return rustInt(f.Type.Width)
		}
		return rustUint(f.Type.Width)
	}
	return scalarRust(f)
}

// emitCookRootWriter emits <T>_cook_write: number the graph, lay the region
// out, write every node, and frame it — header, data, directory (§7.1).
func (g *gen) emitCookRootWriter(b *strings.Builder, st *ir.Struct) {
	ml := ir.RecordLayout(g.unit, st)
	name := fn(st.Name, "cook_write")
	fmt.Fprintf(b, "/// %s — the whole COOK of one %s value in the byte order `order`\n", name, st.Name)
	b.WriteString("/// (1 little, 2 big; docs/SPEC-TABLES.md §7.1, §7.6): the bytes `schema cook`\n")
	b.WriteString("/// writes from the same wire. Err names the refusal (§7.6) and nothing is\n")
	b.WriteString("/// produced: a bad byte order, a count or a length over its declared bound.\n")
	b.WriteString("#[cfg(feature = \"cook\")]\n")
	fmt.Fprintf(b, "pub fn %s(v: &%s, order: u64) -> Result<Vec<u8>, String> {\n", name, st.Name)
	b.WriteString("    if order != 1 && order != 2 {\n        return Err(String::from(\"invalid_value\"));\n    }\n")
	b.WriteString("    let mut g = TableNodeGraph::new();\n")
	fmt.Fprintf(b, "    %s(v, &mut g);\n", fn(st.Name, "number"))
	rb := cookRecordBytes(g.unit, st)
	if cookHasExtent(st) {
		fmt.Fprintf(b, "    let mut m = TableCarve { extent_at: 0, at: 0 };\n    %s(v, &mut m, None, 0);\n", fn(st.Name, "cook_carve"))
		fmt.Fprintf(b, "    let mut w = table_region_layout(&g, %d + m.at, %d, cook_record_layout, cook_record_extent, order)?;\n", rb, ml.Align)
		fmt.Fprintf(b, "    %s(v, &mut w, 0);\n", fn(st.Name, "cook_body"))
		b.WriteString("    // THE ROOT'S EXTENT, and the extent written is the extent measured (§2.8, §2.9)\n")
		fmt.Fprintf(b, "    let mut c = TableCarve { extent_at: %d, at: 0 };\n    %s(v, &mut c, Some(&mut w), 0);\n", rb, fn(st.Name, "cook_carve"))
		b.WriteString("    if c.at != m.at {\n        w.fail();\n    }\n")
	} else {
		fmt.Fprintf(b, "    let mut w = table_region_layout(&g, %d, %d, cook_record_layout, cook_record_extent, order)?;\n", rb, ml.Align)
		fmt.Fprintf(b, "    %s(v, &mut w, 0);\n", fn(st.Name, "cook_body"))
	}
	b.WriteString("    for k in 0..g.records.len() {\n")
	b.WriteString("        let at = w.offsets[k + 1];\n")
	b.WriteString("        match &g.records[k] {\n")
	b.WriteString("            TableNodeRecord::Blob(_, blob) => w.put_blob(at, blob),\n")
	b.WriteString("            TableNodeRecord::Table(type_id, node) => cook_record_write(*type_id, node, &mut w, at),\n")
	b.WriteString("        }\n    }\n")
	fmt.Fprintf(b, "    w.finish(&g, 0x%016x, super::BUILD_VERSION)\n", ir.TableWireId(st.WireName()))
	b.WriteString("}\n\n")
}

// cookRegionWriterRust is the region writer's runtime, emitted into the
// variable runtime behind the `cook` feature: the byte-order stores, the
// reference deltas the numbering resolves, the layout pass and the framing.
const cookRegionWriterRust = `
// ---- the COOK WRITE side's region writer (docs/SPEC-TABLES.md §6.3, §7) ----

/// One region being written: the DATA part, the byte order (1 little, 2 big),
/// the numbering it lays out and each node's region offset, by index - 1 (the
/// root is offset 0). A refusal clears ok and the cook writes nothing.
#[cfg(feature = "cook")]
pub struct TableRegionWriter<'a> {
    pub data: Vec<u8>,
    pub order: u64,
    pub align: i64,
    pub graph: &'a TableNodeGraph,
    pub offsets: Vec<i64>,
    pub ok: bool,
}

#[cfg(feature = "cook")]
fn table_region_up(v: i64, a: i64) -> i64 {
    (v + a - 1) / a * a
}

/// THE LAYOUT (internal/tablecook/region.go's Layout): the root at the base
/// (root_size is its record and its extent), then every node of the numbering
/// in index order at its own alignment — a table node its record and its
/// extent, a
/// blob at eight, its u32 length, four zero bytes, its bytes and a *string
/// blob's terminator — and the data part rounded to the region's alignment,
/// the greatest record alignment, never below eight.
#[cfg(feature = "cook")]
pub fn table_region_layout(
    g: &TableNodeGraph,
    root_size: i64,
    root_align: i64,
    layout: fn(u64) -> Option<(i64, i64)>,
    extent: fn(u64, &Rc<dyn Any>) -> i64,
    order: u64,
) -> Result<TableRegionWriter<'_>, String> {
    let mut align = root_align.max(8);
    let mut offset = root_size;
    let mut offsets = Vec::with_capacity(g.records.len() + 1);
    offsets.push(0);
    for rec in g.records.iter() {
        match rec {
            TableNodeRecord::Blob(type_id, blob) => {
                if blob.len() as u64 > u32::MAX as u64 {
                    return Err(String::from("invalid_value"));
                }
                offset = table_region_up(offset, 8);
                offsets.push(offset);
                offset += 8 + blob.len() as i64 + if *type_id == STRING_TYPE_ID { 1 } else { 0 };
            }
            TableNodeRecord::Table(type_id, node) => {
                let Some((size, a)) = layout(*type_id) else {
                    return Err(String::from("invalid_value"));
                };
                align = align.max(a);
                offset = table_region_up(offset, a);
                offsets.push(offset);
                // a NODE'S SIZE DEPENDS ON ITS VALUE where a list or a map
                // rides in its extent (§2.8, §2.9): the record, then the extent
                offset += size + extent(*type_id, node);
            }
        }
    }
    let bytes = table_region_up(offset, align);
    Ok(TableRegionWriter { data: vec![0u8; bytes as usize], order, align, graph: g, offsets, ok: true })
}

#[cfg(feature = "cook")]
impl TableRegionWriter<'_> {
    pub fn fail(&mut self) {
        self.ok = false;
    }

    /// One scalar of width bytes at a region offset, in the cook's byte order.
    pub fn put(&mut self, at: i64, value: u64, width: u32) {
        let at = at as usize;
        for i in 0..width as usize {
            let shift = if self.order == 2 { width as usize - 1 - i } else { i };
            self.data[at + i] = (value >> (shift * 8)) as u8;
        }
    }

    /// A float32 as its bit pattern; a NaN or an infinity refuses (§3.3).
    pub fn put_f32(&mut self, at: i64, value: f32) {
        if !value.is_finite() {
            self.ok = false;
            return;
        }
        self.put(at, value.to_bits() as u64, 4);
    }

    /// A float64 as its bit pattern; a NaN or an infinity refuses (§3.3).
    pub fn put_f64(&mut self, at: i64, value: f64) {
        if !value.is_finite() {
            self.ok = false;
            return;
        }
        self.put(at, value.to_bits(), 8);
    }

    /// A 128-bit integer: a big-endian region holds its high half first, as
    /// it holds a u64 with its high byte first (§7.2).
    pub fn put_wide(&mut self, at: i64, value: u128) {
        let bytes = if self.order == 2 { value.to_be_bytes() } else { value.to_le_bytes() };
        self.data[at as usize..at as usize + 16].copy_from_slice(&bytes);
    }

    /// string(N) or bytes(N): the used bytes, a zero tail, and the int32 used
    /// length at its own piece; a value over its declared bound refuses.
    pub fn put_text(&mut self, at: i64, bytes: &[u8], bound: usize, length_at: i64) {
        if bytes.len() > bound {
            self.ok = false;
            return;
        }
        self.data[at as usize..at as usize + bytes.len()].copy_from_slice(bytes);
        self.put(length_at, bytes.len() as u64, 4);
    }

    /// wstring(N): the used CODE UNITS, each a two-byte scalar in the cook's
    /// byte order, and the int32 used length in units (§7.2).
    pub fn put_units(&mut self, at: i64, units: &[u16], bound: usize, length_at: i64) {
        if units.len() > bound {
            self.ok = false;
            return;
        }
        for (i, u) in units.iter().enumerate() {
            self.put(at + 2 * i as i64, *u as u64, 2);
        }
        self.put(length_at, units.len() as u64, 4);
    }

    /// A reference slot to a table node: the SELF-RELATIVE delta from the
    /// slot's own address to the node's start, and zero for null (§6.3).
    pub fn ref_node<T>(&mut self, at: i64, node: &Option<Rc<T>>) {
        match node {
            None => self.put(at, 0, 8),
            Some(n) => {
                let index = self.graph.node_index(n) as usize;
                self.ref_index(at, index);
            }
        }
    }

    /// A reference slot to a blob node, by the blob's identity (§2.5).
    pub fn ref_blob(&mut self, at: i64, key: Option<usize>) {
        match key {
            None => self.put(at, 0, 8),
            Some(k) => {
                let index = self.graph.blob_index(k) as usize;
                self.ref_index(at, index);
            }
        }
    }

    fn ref_index(&mut self, at: i64, index: usize) {
        if index < 2 || index > self.offsets.len() {
            self.ok = false; // a node the numbering does not carry
            return;
        }
        let delta = self.offsets[index - 1] - at;
        if delta == 0 {
            self.ok = false;
            return;
        }
        self.put(at, delta as u64, 8);
    }

    /// A blob node: its u32 length in the cook's order, then its bytes; the
    /// pad word and a *string blob's terminator are the buffer's zeros.
    pub fn put_blob(&mut self, at: i64, blob: &[u8]) {
        self.put(at, blob.len() as u64, 4);
        let from = at as usize + 8;
        self.data[from..from + blob.len()].copy_from_slice(blob);
    }

    /// Frame the region (§7.1): the 64-byte header, the data part at
    /// align_up(64, alignment), and the node directory — each node's offset
    /// and type id — as the attribution part.
    pub fn finish(self, g: &TableNodeGraph, root_type: u64, build: u64) -> Result<Vec<u8>, String> {
        if !self.ok {
            return Err(String::from("invalid_value"));
        }
        let data_offset = table_region_up(64, self.align) as usize;
        let nodes = g.records.len() + 1;
        let total = data_offset + self.data.len() + nodes * 16;
        let mut out = TableRegionWriter { data: vec![0u8; total], order: self.order, align: self.align, graph: g, offsets: Vec::new(), ok: true };
        out.put(0, 0x4b4f4f434d484353, 8);
        out.put(8, build, 8);
        out.put(16, self.order, 8);
        out.put(24, self.data.len() as u64, 8);
        out.put(32, (nodes * 16) as u64, 8);
        out.put(40, self.align as u64, 8);
        out.data[data_offset..data_offset + self.data.len()].copy_from_slice(&self.data);
        let mut at = (data_offset + self.data.len()) as i64;
        for k in 0..nodes {
            let id = if k == 0 {
                root_type
            } else {
                match &g.records[k - 1] {
                    TableNodeRecord::Blob(type_id, _) => *type_id,
                    TableNodeRecord::Table(type_id, _) => *type_id,
                }
            };
            out.put(at, self.offsets[k] as u64, 8);
            out.put(at + 8, id, 8);
            at += 16;
        }
        Ok(out.data)
    }
}
`

// cookWriteImports is the body writers another file's codec emits, which a
// body of this file may call: a record nested by value or pointed at across
// two schema files of one unit.
func cookWriteImports(u *ir.Unit, vc *variableClass, otherNames []string) []string {
	cw := cookWriteSet(u, vc)
	var out []string
	for _, n := range otherNames {
		if cw[n] {
			out = append(out, fn(n, "cook_body"))
		}
	}
	return out
}
