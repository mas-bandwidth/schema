// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3: the Go writer, the
// Go reader and the layout, following the C++ reference (cpptable/fixedform.go)
// for BYTES and the Go packet codec (internal/codegen/golang) for SHAPE.
//
// A record is an eight-byte hash of the writer's layout and then the values
// in declared order, every field at its DECLARED STORAGE WIDTH. The writer is
// the packet codec with byte-width stores instead of shifts. The reader is
// ONE plan-driven path. There is no version byte inside the layout; the form
// byte versions it. The word is LAYOUT, not block.
//
// FILE HEADER (docs/SPEC-TABLES.md §3.4, WHAT A FILE CARRIES): form byte 3,
// the layout's length as a u32 LE, the layout, then the records back to back
// to the end of the file. Five bytes before the layout, and THE HASH IS PER
// RECORD and not in the header — a record is its hash and then its values.
// This is the reference's header (cpptable) and the committed corpus's.
//
// Form-1 Load of a DECLARED fixed table is a named refusal (Glenn
// 2026-09-09), never a slow read — and the word that matters is DECLARED.
// The refusal is keyed on the `fixed table` KEYWORD ([ir.Struct.FixedDeclared],
// #823), not on the shape: a table the compiler merely derived into the fixed
// mode asked for nothing and keeps the form-1 Load it never lost, even though
// this backend also emits form 3 for it. FixedLoad itself is the form-3 reader
// either way, and it answers by the form REGISTRY (§3): previous_form for 1,
// message_form_as_file for 2, newer_form for every byte §3 has not assigned.
// The C++ reference still accepts form 1 by the form byte; this port does not
// copy that for a declared fixed table.
package gotable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const fixedKindOptional = 35
const fixedLeafCap = 4096
const (
	fixedCountBytes   = int64(4)
	fixedPresentBytes = int64(1)
)

func fixedStorageBytes(t ir.FieldType) int64 {
	switch t.Kind {
	case ir.TBool:
		return 1
	case ir.TInt, ir.TFixed:
		return int64(t.Width / 8)
	case ir.TBits:
		if t.Width <= 32 {
			return 4
		}
		return 8
	case ir.TFloat32:
		return 4
	case ir.TFloat64:
		return 8
	case ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Enum:
			return int64(r.StorageBits / 8)
		case *ir.Flags:
			return 8
		}
	}
	return 0
}

func fixedTypeBytes(st *ir.Struct) int64 {
	var n int64
	for _, f := range st.Fields {
		n += fixedFieldBytes(f)
	}
	return n
}

func fixedFieldBytes(f *ir.Field) int64 {
	var payload int64
	switch {
	case f.KeyEnum != "":
		payload = f.KeyEnumRef.Max * fixedElementBytes(f)
	case f.Array == ir.ArrayFixed:
		payload = f.ArrayBound * fixedElementBytes(f)
	case f.Array == ir.ArrayCounted:
		payload = fixedCountBytes + f.ArrayBound*fixedElementBytes(f)
	case f.Type.Kind == ir.TString:
		payload = fixedCountBytes + f.Type.Size
	case f.Type.Kind == ir.TWString:
		payload = fixedCountBytes + 2*f.Type.Size
	case f.Type.Kind == ir.TBytes:
		payload = fixedCountBytes + f.Type.Size
	default:
		payload = fixedElementBytes(f)
	}
	if f.Type.Optional {
		return fixedPresentBytes + payload
	}
	return payload
}

func fixedElementBytes(f *ir.Field) int64 {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return fixedTypeBytes(r)
		case *ir.Union:
			widest := int64(0)
			for _, v := range r.Variants {
				if n := fixedFieldBytes(v.F); n > widest {
					widest = n
				}
			}
			return int64(ir.StorageBitsFor(r.Max)/8) + widest
		}
	}
	return fixedStorageBytes(f.Type)
}

func fixedSupported(st *ir.Struct, depth int) bool {
	if depth > 16 {
		return false
	}
	for _, f := range st.Fields {
		if f.Guard != "" || f.Type.Pointer || f.IsMap() || f.IsList() || f.Type.Blob() {
			return false
		}
		if f.Type.Kind == ir.TNamed {
			switch r := f.Type.Ref.(type) {
			case *ir.Struct:
				if !fixedSupported(r, depth+1) {
					return false
				}
			case *ir.Union:
				for _, v := range r.Variants {
					if v.F == nil {
						return false
					}
					a := v.F
					if a.Type.Pointer || a.IsMap() || a.IsList() || a.Array != ir.ArrayNone || a.KeyEnum != "" ||
						a.Type.Kind == ir.TString || a.Type.Kind == ir.TWString || a.Type.Kind == ir.TBytes || a.Type.Optional {
						return false
					}
					if s, ok := a.Type.Ref.(*ir.Struct); ok && a.Type.Kind == ir.TNamed && !fixedSupported(s, depth+1) {
						return false
					}
				}
			}
		}
	}
	return true
}

type fixedLayoutEntry struct {
	id       uint64
	kind     int
	size     int64
	children int
	note     string
}

type fixedWalk struct {
	entries []fixedLayoutEntry
	dst     []string
}

func (w *fixedWalk) push(e fixedLayoutEntry, dst string) {
	w.entries = append(w.entries, e)
	w.dst = append(w.dst, dst)
}

func dstGo(dst, stride, aux string, counted, arg int) string {
	if dst == "" {
		dst = "0"
	}
	if stride == "" {
		stride = "0"
	}
	if aux == "" {
		aux = "0"
	}
	return fmt.Sprintf("{%s, %s, %s, %d, %d, %d}", dst, stride, aux, counted, arg, arg)
}

func offGo(owner, field string) string {
	return fmt.Sprintf("uint32(unsafe.Offsetof(%s{}.%s))", owner, field)
}

func (g *tableGen) fixedWalkRoot(st *ir.Struct) *fixedWalk {
	w := &fixedWalk{}
	w.push(fixedLayoutEntry{id: ir.TableWireId(st.WireName()), kind: ir.TableKindTable, size: fixedTypeBytes(st), children: len(st.Fields), note: st.Name}, dstGo("0", "", "", 0, 0))
	for _, f := range st.Fields {
		g.fixedWalkField(w, st.Name, f)
	}
	return w
}

func (g *tableGen) fixedWalkField(w *fixedWalk, owner string, f *ir.Field) {
	id := ir.TableFieldWireId(f)
	size := fixedFieldBytes(f)
	if f.Type.Optional {
		w.push(fixedLayoutEntry{id: id, kind: fixedKindOptional, size: size, children: 1, note: f.Name + " ?"},
			dstGo("0", "", offGo(owner, member(f)+"Present"), 0, 0))
		g.fixedWalkPayload(w, owner, f, id, size-fixedPresentBytes)
		return
	}
	g.fixedWalkPayload(w, owner, f, id, size)
}

func (g *tableGen) fixedWalkPayload(w *fixedWalk, owner string, f *ir.Field, id uint64, size int64) {
	switch {
	case f.KeyEnum != "":
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindKeyed, size: size, children: 2, note: f.Name},
			dstGo(offGo(owner, member(f)), g.sizeofGo(f), "", 0, 0))
		g.fixedWalkEnum(w, f.KeyEnumRef, ir.TableWireId(f.KeyEnum), f.KeyEnum, "0")
		g.fixedWalkElement(w, f, 0, "element", "0")
	case f.Array != ir.ArrayNone:
		counted, aux := 0, ""
		if f.Array == ir.ArrayCounted {
			counted, aux = 1, offGo(owner, member(f)+"Count")
		}
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindArray, size: size, children: 1, note: f.Name},
			dstGo(offGo(owner, member(f)), g.sizeofGo(f), aux, counted, 0))
		g.fixedWalkElement(w, f, 0, "element", "0")
	case f.Type.Kind == ir.TBytes:
		// THE LANES ARE SWAPPED AGAINST THE ARRAY ROW ABOVE, deliberately and
		// not happily: a counted array puts the ARRAY in dst and the count in
		// aux, and `bytes(N)` — the same kind 14 — puts the LENGTH in dst and
		// the payload in aux, because the reader lands it with the text op,
		// whose dst is a count. This port mirrors the reference exactly
		// (ir/fixedform.go, the TBytes row of fixedWalkPayload); the lane
		// convention is a REFERENCE-SIDE question and its fix belongs on
		// fixed-table-form, where it had not landed at b7fdb475.
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindArray, size: size, children: 1, note: f.Name},
			dstGo(offGo(owner, member(f)+"Length"), "1", offGo(owner, member(f)), 1, 3))
		w.push(fixedLayoutEntry{id: 0, kind: ir.TableKindU8, size: 1, children: 0, note: "u8"}, dstGo("0", "", "", 0, 0))
	case f.Type.Kind == ir.TString:
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindString, size: size, children: 0, note: f.Name},
			dstGo(offGo(owner, member(f)+"Length"), "", offGo(owner, member(f)), 0, 1))
	case f.Type.Kind == ir.TWString:
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindWstring, size: size, children: 0, note: f.Name},
			dstGo(offGo(owner, member(f)+"Length"), "", offGo(owner, member(f)), 0, 2))
	default:
		g.fixedWalkElement(w, f, id, f.Name, offGo(owner, member(f)))
	}
}

func (g *tableGen) fixedWalkElement(w *fixedWalk, f *ir.Field, id uint64, note, dst string) {
	size := fixedElementBytes(f)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			w.push(fixedLayoutEntry{id: id, kind: ir.TableKindTable, size: size, children: len(r.Fields), note: note}, dstGo(dst, "", "", 0, 0))
			for _, sub := range r.Fields {
				g.fixedWalkField(w, r.Name, sub)
			}
			return
		case *ir.Union:
			w.push(fixedLayoutEntry{id: id, kind: ir.TableKindUnion, size: size, children: len(r.Variants), note: note},
				dstGo(dst, "", dst+" + "+offGo(f.Type.Name, "Type"), 0, 0))
			for _, v := range r.Variants {
				armID := ir.TableWireId(v.WireName())
				g.fixedWalkElement(w, v.F, armID, v.Name, offGo(f.Type.Name, ir.GoExportName(v.Name)))
			}
			return
		case *ir.Enum:
			g.fixedWalkEnum(w, r, id, note, dst)
			return
		}
	}
	w.push(fixedLayoutEntry{id: id, kind: ir.TableWireScalarKind(f), size: size, children: 0, note: note}, dstGo(dst, "", "", 0, 0))
}

func (g *tableGen) fixedWalkEnum(w *fixedWalk, e *ir.Enum, id uint64, note, dst string) {
	w.push(fixedLayoutEntry{id: id, kind: ir.TableKindEnum, size: int64(e.StorageBits / 8), children: len(e.Variants), note: note}, dstGo(dst, "", "", 0, 0))
	for i := range e.Variants {
		w.push(fixedLayoutEntry{id: ir.TableWireId(e.VariantWireName(i)), kind: ir.TableKindNoPayload, size: 0, children: 0, note: e.Variants[i]}, dstGo("0", "", "", 0, 0))
	}
}

func fixedLayoutBytes(entries []fixedLayoutEntry) []byte {
	out := make([]byte, 0, 4+17*len(entries))
	out = appendFixedU32(out, uint32(len(entries)))
	for _, e := range entries {
		out = appendFixedU64(out, e.id)
		out = append(out, byte(e.kind))
		out = appendFixedU32(out, uint32(e.size))
		out = appendFixedU32(out, uint32(e.children))
	}
	return out
}

func appendFixedU32(b []byte, v uint32) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

func appendFixedU64(b []byte, v uint64) []byte {
	for i := range 8 {
		b = append(b, byte(v>>(8*i)))
	}
	return b
}

func fixedLayoutHash(layout []byte) uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, b := range layout {
		h ^= uint64(b)
		h *= 0x100000001b3
	}
	return h
}

func (g *tableGen) isVarTable(name string) bool {
	return ir.VariableTables(g.unit)[name]
}

// hasFixedForm is whether this table emits FixedLoad/FixedSave. It is a
// question about SHAPE — the closure this form can lay out — and a regional
// unit skips the form entirely, so a mixed unit's fixed-size tables keep
// form-1 Load.
func (g *tableGen) hasFixedForm(st *ir.Struct) bool {
	if g.regional || !st.IsTable || st.IsMapEntry() || g.isVarTable(st.Name) || !fixedSupported(st, 0) {
		return false
	}
	return g.fixedLeafCount(st) <= fixedLeafCap
}

// refusesForm1 is the OTHER question, and Glenn 2026-09-09 is that they are
// two: a form-1 file handed to <T>Load is a named refusal when the AUTHOR
// DECLARED the table fixed, never merely because the compiler could lay it
// out fixed. hasFixedForm above is shape; this is the keyword. Nothing sets
// [ir.Struct.FixedDeclared] until #823's `fixed table` lands, so today every
// table in this tree keeps its form-1 Load and the shared wire oracle reads
// what it always read.
func (g *tableGen) refusesForm1(st *ir.Struct) bool {
	return g.hasFixedForm(st) && st.FixedDeclared
}

func (g *tableGen) fixedRoots(members []*ir.Struct) []*ir.Struct {
	var out []*ir.Struct
	for _, st := range members {
		if g.hasFixedForm(st) {
			out = append(out, st)
		}
	}
	return out
}

func (g *tableGen) emitFixedForm(members []*ir.Struct) {
	if g.regional {
		return
	}
	roots := g.fixedRoots(members)
	if len(roots) == 0 {
		return
	}
	g.needsUnsafe()
	g.pf("// ---- THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4) ----\n")
	g.pf("//\n")
	g.pf("// A record is an eight-byte hash of the writer's layout and then the values\n")
	g.pf("// in declared order, every field at its declared storage width. The writer is\n")
	g.pf("// constant-offset stores; the reader is ONE loop over ONE plan.\n\n")
	seen := map[string]bool{}
	var order []*ir.Struct
	for _, st := range roots {
		fixedCollectTypes(st, seen, &order)
	}
	for _, st := range order {
		g.emitFixedLeafWalk(st)
		g.emitFixedWriteBody(st)
	}
	for _, st := range roots {
		g.emitFixedRoot(st)
	}
}

func fixedCollectTypes(st *ir.Struct, seen map[string]bool, order *[]*ir.Struct) {
	if seen[st.Name] {
		return
	}
	seen[st.Name] = true
	for _, f := range st.Fields {
		if f.Type.Kind != ir.TNamed {
			continue
		}
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			fixedCollectTypes(r, seen, order)
		case *ir.Union:
			for _, v := range r.Variants {
				if s, ok := v.F.Type.Ref.(*ir.Struct); ok && v.F.Type.Kind == ir.TNamed {
					fixedCollectTypes(s, seen, order)
				}
			}
		}
	}
	*order = append(*order, st)
}

func (g *tableGen) fixedLeafCount(st *ir.Struct) int {
	n := 0
	for _, f := range st.Fields {
		n += g.fixedFieldLeafCount(f)
	}
	return n
}

func (g *tableGen) fixedFieldLeafCount(f *ir.Field) int {
	per := g.fixedElementLeafCount(f)
	n := per
	switch {
	case f.KeyEnum != "":
		n = int(f.KeyEnumRef.Max) * per
	case f.Array == ir.ArrayFixed:
		n = int(f.ArrayBound) * per
	case f.Array == ir.ArrayCounted:
		n = 1 + int(f.ArrayBound)*per
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TWString, f.Type.Kind == ir.TBytes:
		n = 1
	}
	if f.Type.Optional {
		n++
	}
	return n
}

func (g *tableGen) fixedElementLeafCount(f *ir.Field) int {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return g.fixedLeafCount(r)
		case *ir.Union:
			n := 1
			for _, v := range r.Variants {
				n += g.fixedFieldLeafCount(v.F)
			}
			return n
		}
	}
	return 1
}

func (g *tableGen) emitFixedLeafWalk(st *ir.Struct) {
	g.pf("func %sFixedLeaves(out []TableFixedEntry, src, dst uint32) int {\n", st.Name)
	g.pf("\tn := 0\n")
	off := int64(0)
	for _, f := range st.Fields {
		g.emitFixedFieldLeaves(st, f, off)
		off += fixedFieldBytes(f)
	}
	g.pf("\treturn n\n}\n\n")
}

func (g *tableGen) emitFixedFieldLeaves(st *ir.Struct, f *ir.Field, off int64) {
	base := off
	if f.Type.Optional {
		// the PRESENT byte lands in a Go bool, so it is normalised, not copied
		g.pf("\tout[n] = TableFixedEntry{Src: src + %d, Dst: dst + %s, Size: 1, Guard: tableFixedNoGuard, Op: tableFixedBool} // %s present\n",
			base, offGo(st.Name, member(f)+"Present"), f.Name)
		g.pf("\tn++\n")
		base += fixedPresentBytes
	}
	switch {
	case f.KeyEnum != "":
		g.emitFixedElementLoop(st, f, base, f.KeyEnumRef.Max)
	case f.Array == ir.ArrayFixed:
		g.emitFixedElementLoop(st, f, base, f.ArrayBound)
	case f.Array == ir.ArrayCounted:
		g.pf("\tout[n] = TableFixedEntry{Src: src + %d, Dst: dst + %s, Size: %d, Guard: tableFixedNoGuard, Op: tableFixedCount} // %s count\n",
			base, offGo(st.Name, member(f)+"Count"), f.ArrayBound, f.Name)
		g.pf("\tn++\n")
		g.emitFixedElementLoop(st, f, base+fixedCountBytes, f.ArrayBound)
	case f.Type.Kind == ir.TString:
		g.emitFixedTextLeaf(st, f, base, f.Type.Size, 1)
	case f.Type.Kind == ir.TWString:
		g.emitFixedTextLeaf(st, f, base, 2*f.Type.Size, 2)
	case f.Type.Kind == ir.TBytes:
		g.emitFixedTextLeaf(st, f, base, f.Type.Size, 3)
	default:
		g.pf("\t{\n\t\tes := src + %d\n\t\ted := dst + %s\n", base, offGo(st.Name, member(f)))
		g.emitFixedElementLeavesAt(f, "es", "ed", 2)
		g.pf("\t}\n")
	}
}

func (g *tableGen) emitFixedTextLeaf(st *ir.Struct, f *ir.Field, base, units int64, flavour int) {
	g.pf("\tout[n] = TableFixedEntry{Src: src + %d, Dst: dst + %s, Size: %d, Aux: dst + %s, Guard: tableFixedNoGuard, Op: tableFixedText, Meta: %d} // %s\n",
		base, offGo(st.Name, member(f)+"Length"), units, offGo(st.Name, member(f)), flavour, f.Name)
	g.pf("\tn++\n")
}

func (g *tableGen) emitFixedElementLoop(st *ir.Struct, f *ir.Field, base, count int64) {
	elem := fixedElementBytes(f)
	g.pf("\tfor i := uint32(0); i < %d; i++ {\n", count)
	g.pf("\t\tes := src + %d + i*%d\n", base, elem)
	g.pf("\t\ted := dst + %s + i*%s\n", offGo(st.Name, member(f)), g.sizeofGo(f))
	g.emitFixedElementLeavesAt(f, "es", "ed", 2)
	g.pf("\t}\n")
}

func (g *tableGen) sizeofGo(f *ir.Field) string {
	if f.Type.Kind == ir.TNamed {
		switch f.Type.Ref.(type) {
		case *ir.Enum, *ir.Flags:
			return fmt.Sprintf("uint32(unsafe.Sizeof(%s(0)))", f.Type.Name)
		default:
			return fmt.Sprintf("uint32(unsafe.Sizeof(%s{}))", f.Type.Name)
		}
	}
	t := goFieldType(f.Type)
	switch t {
	case "bool":
		return "uint32(unsafe.Sizeof(false))"
	case "float32":
		return "uint32(unsafe.Sizeof(float32(0)))"
	case "float64":
		return "uint32(unsafe.Sizeof(float64(0)))"
	}
	if strings.HasPrefix(t, "int") || strings.HasPrefix(t, "uint") {
		return fmt.Sprintf("uint32(unsafe.Sizeof(%s(0)))", t)
	}
	return fmt.Sprintf("uint32(unsafe.Sizeof(%s{}))", t)
}

func (g *tableGen) emitFixedElementLeavesAt(f *ir.Field, src, dst string, tabs int) {
	ind := strings.Repeat("\t", tabs)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			g.pf("%sn += %sFixedLeaves(out[n:], %s, %s)\n", ind, r.Name, src, dst)
			return
		case *ir.Union:
			tag := int64(ir.StorageBitsFor(r.Max) / 8)
			// THE TAG IS NOT A COPY: a tag naming no arm of this reader lands
			// as None and counts `unknown`, the way form 1 answers an arm id
			// it does not know (docs/SPEC-TABLES.md §4). Aux carries this
			// reader's own arm count.
			g.pf("%sout[n] = TableFixedEntry{Src: %s, Dst: %s + %s, Size: %d, Aux: %d, Guard: tableFixedNoGuard, Op: tableFixedTag} // the tag\n",
				ind, src, dst, offGo(f.Type.Name, "Type"), tag, len(r.Variants))
			g.pf("%sn++\n", ind)
			for i, v := range r.Variants {
				g.pf("%s{ // arm %s, ordinal %d\n%s\tguardAt := n\n", ind, v.Name, i+1, ind)
				g.emitFixedElementLeavesAt(v.F, fmt.Sprintf("%s+%d", src, tag),
					fmt.Sprintf("%s+%s", dst, offGo(f.Type.Name, ir.GoExportName(v.Name))), tabs+1)
				g.pf("%s\tfor q := guardAt; q < n; q++ {\n%s\t\tout[q].Guard = %s\n%s\t\tout[q].Arg = %d\n%s\t}\n", ind, ind, src, ind, i+1, ind)
				g.pf("%s}\n", ind)
			}
			return
		}
	}
	// A GO bool IS NOT A BYTE. A Go true is the byte 1 (`== true`, array
	// equality); `if v` is not portable (TESTB on amd64, TBZ bit 0 on arm64).
	// The wire and the reference both say nonzero is true; tableFixedBool
	// normalises (byte != 0 → 1) instead of copying, and being its own op it
	// never coalesces into a neighbouring copy run.
	op := "tableFixedCopy"
	if f.Type.Kind == ir.TBool {
		op = "tableFixedBool"
	}
	g.pf("%sout[n] = TableFixedEntry{Src: %s, Dst: %s, Size: %d, Guard: tableFixedNoGuard, Op: %s}\n",
		ind, src, dst, fixedElementBytes(f), op)
	g.pf("%sn++\n", ind)
}

func (g *tableGen) emitFixedWriteBody(st *ir.Struct) {
	g.pf("func %sFixedWriteBody(b []byte, value *%s) {\n", st.Name, st.Name)
	off := int64(0)
	if len(st.Fields) == 0 {
		g.pf("\t_, _ = b, value\n")
	}
	for _, f := range st.Fields {
		g.emitFixedWriteField(f, off, "b", "value", 1)
		off += fixedFieldBytes(f)
	}
	g.pf("}\n\n")
}

func (g *tableGen) emitFixedWriteField(f *ir.Field, off int64, buf, val string, tabs int) {
	ind := strings.Repeat("\t", tabs)
	base := off
	if f.Type.Optional {
		g.pf("%s{\n%s\tp := uint8(0)\n%s\tif %s.%sPresent {\n%s\t\tp = 1\n%s\t}\n%s\ttableFixedPut8(%s[%d:], p)\n%s}\n",
			ind, ind, ind, val, member(f), ind, ind, ind, buf, base, ind)
		base += fixedPresentBytes
	}
	dest := fmt.Sprintf("%s[%d:]", buf, base)
	switch {
	case f.KeyEnum != "":
		g.emitFixedWriteLoop(f, dest, f.KeyEnumRef.Max, val+"."+member(f), tabs)
	case f.Array == ir.ArrayFixed:
		g.emitFixedWriteLoop(f, dest, f.ArrayBound, val+"."+member(f), tabs)
	case f.Array == ir.ArrayCounted:
		g.pf("%stableFixedPut32(%s, uint32(%s.%sCount))\n", ind, dest, val, member(f))
		g.emitFixedWriteLoop(f, fmt.Sprintf("%s[%d:]", buf, base+fixedCountBytes), f.ArrayBound, val+"."+member(f), tabs)
	case f.Type.Kind == ir.TString:
		g.pf("%stableFixedPut32(%s, uint32(%s.%sLength))\n", ind, dest, val, member(f))
		g.pf("%scopy(%s[4:], %s.%s[:])\n", ind, dest, val, member(f))
	case f.Type.Kind == ir.TWString:
		g.pf("%stableFixedPut32(%s, uint32(%s.%sLength))\n", ind, dest, val, member(f))
		g.pf("%sfor i := 0; i < %d; i++ {\n%s\ttableFixedPut16(%s[4+i*2:], %s.%s[i])\n%s}\n",
			ind, f.Type.Size, ind, dest, val, member(f), ind)
	case f.Type.Kind == ir.TBytes:
		g.pf("%stableFixedPut32(%s, uint32(%s.%sLength))\n", ind, dest, val, member(f))
		g.pf("%scopy(%s[4:], %s.%s[:])\n", ind, dest, val, member(f))
	default:
		g.emitFixedWriteElement(f, dest, val+"."+member(f), tabs)
	}
}

func (g *tableGen) emitFixedWriteLoop(f *ir.Field, dest string, count int64, expr string, tabs int) {
	ind := strings.Repeat("\t", tabs)
	elem := fixedElementBytes(f)
	g.pf("%sfor i := int64(0); i < %d; i++ {\n", ind, count)
	g.emitFixedWriteElement(f, fmt.Sprintf("%s[i*%d:]", dest, elem), expr+"[i]", tabs+1)
	g.pf("%s}\n", ind)
}

func (g *tableGen) emitFixedWriteElement(f *ir.Field, dest, expr string, tabs int) {
	ind := strings.Repeat("\t", tabs)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			g.pf("%s%sFixedWriteBody(%s, &%s)\n", ind, r.Name, dest, expr)
			return
		case *ir.Union:
			tag := int64(ir.StorageBitsFor(r.Max) / 8)
			g.emitFixedPutInt(ind, dest, tag, expr+".Type")
			g.pf("%sswitch %s.Type {\n", ind, expr)
			for _, v := range r.Variants {
				g.pf("%scase %sType%s:\n", ind, f.Type.Name, ir.GoExportName(v.Name))
				g.emitFixedWriteElement(v.F, fmt.Sprintf("%s[%d:]", dest, tag), expr+"."+ir.GoExportName(v.Name), tabs+1)
			}
			g.pf("%s}\n", ind)
			return
		case *ir.Enum:
			w := int64(r.StorageBits / 8)
			g.emitFixedPutInt(ind, dest, w, expr)
			return
		case *ir.Flags:
			g.pf("%stableFixedPut64(%s, uint64(%s))\n", ind, dest, expr)
			return
		}
	}
	w := fixedStorageBytes(f.Type)
	switch f.Type.Kind {
	case ir.TBool:
		g.pf("%s{\n%s\tp := uint8(0)\n%s\tif %s {\n%s\t\tp = 1\n%s\t}\n%s\ttableFixedPut8(%s, p)\n%s}\n",
			ind, ind, ind, expr, ind, ind, ind, dest, ind)
	case ir.TFloat32:
		g.pf("%stableFixedPutF32(%s, %s)\n", ind, dest, expr)
	case ir.TFloat64:
		g.pf("%stableFixedPutF64(%s, %s)\n", ind, dest, expr)
	default:
		if w == 16 {
			g.pf("%stableFixedPut128(%s, %s.Lo, %s.Hi)\n", ind, dest, expr, expr)
			return
		}
		g.emitFixedPutInt(ind, dest, w, expr)
	}
}

func (g *tableGen) emitFixedPutInt(ind, dest string, width int64, expr string) {
	switch width {
	case 1:
		g.pf("%stableFixedPut8(%s, uint8(%s))\n", ind, dest, expr)
	case 2:
		g.pf("%stableFixedPut16(%s, uint16(%s))\n", ind, dest, expr)
	case 4:
		g.pf("%stableFixedPut32(%s, uint32(%s))\n", ind, dest, expr)
	case 8:
		g.pf("%stableFixedPut64(%s, uint64(%s))\n", ind, dest, expr)
	}
}

func (g *tableGen) emitFixedRoot(st *ir.Struct) {
	w := g.fixedWalkRoot(st)
	layout := fixedLayoutBytes(w.entries)
	hash := fixedLayoutHash(layout)
	body := fixedTypeBytes(st)
	leaves := g.fixedLeafCount(st)

	g.pf("// ---- %s, the fixed form ----\n\n", st.Name)
	g.pf("const %sFixedBodyBytes = %d\n", st.Name, body)
	g.pf("const %sFixedRecordBytes = 8 + %sFixedBodyBytes\n", st.Name, st.Name)
	g.pf("const %sFixedHash = 0x%016x\n\n", st.Name, hash)

	g.pf("var %sFixedLayout = []byte{\n", st.Name)
	g.emitByteArray(layout)
	g.pf("}\n\n")

	g.pf("var %sFixedDst = []TableFixedDst{\n", st.Name)
	for i, row := range w.dst {
		g.pf("\t%s, // %s\n", row, w.entries[i].note)
	}
	g.pf("}\n\n")

	g.pf("var %sFixedPlan = tableFixedBuildPlan(%sFixedLeaves, %d)\n\n", st.Name, st.Name, leaves)

	g.pf("// A FILE: form byte, seven reserved zeros, the LAYOUT HASH at 8, body at 16,\n")
	g.pf("// then the layout behind its u32 length, then the records to the end of it.\n")
	g.pf("func %sFixedMeasure(count int64) int64 {\n", st.Name)
	g.pf("\treturn TableFixedHeaderBytes + 4 + int64(len(%sFixedLayout)) + count*%sFixedRecordBytes\n}\n\n", st.Name, st.Name)

	g.pf("func %sFixedSave(values []%s, buffer []byte) int64 {\n", st.Name, st.Name)
	g.pf("\tneed := %sFixedMeasure(int64(len(values)))\n", st.Name)
	g.pf("\tif int64(len(buffer)) < need {\n\t\treturn -1\n\t}\n")
	g.pf("\tclear(buffer[:TableFixedHeaderBytes])\n")
	g.pf("\tbuffer[0] = TableFixedForm\n")
	g.pf("\ttableFixedPut64(buffer[TableFixedHashAt:], %sFixedHash)\n", st.Name)
	g.pf("\ttableFixedPut32(buffer[TableFixedHeaderBytes:], uint32(len(%sFixedLayout)))\n", st.Name)
	g.pf("\tcopy(buffer[TableFixedHeaderBytes+4:], %sFixedLayout)\n", st.Name)
	g.pf("\tat := buffer[TableFixedHeaderBytes+4+len(%sFixedLayout):]\n", st.Name)
	g.pf("\tfor k := range values {\n")
	g.pf("\t\ttableFixedPut64(at, %sFixedHash)\n", st.Name)
	g.pf("\t\tclear(at[8 : 8+%sFixedBodyBytes])\n", st.Name)
	g.pf("\t\t%sFixedWriteBody(at[8:], &values[k])\n", st.Name)
	g.pf("\t\tat = at[%sFixedRecordBytes:]\n", st.Name)
	g.pf("\t}\n\treturn need\n}\n\n")

	g.pf("func %sFixedLoad(values []%s, data []byte, plan []TableFixedEntry, report *TableReport) int64 {\n", st.Name, st.Name)
	g.pf("\tif report == nil {\n\t\tvar local TableReport\n\t\treport = &local\n\t}\n")
	g.pf("\tif len(data) < TableFixedHeaderBytes+4 {\n\t\treport.Malformed = true\n\t\treturn -1\n\t}\n")
	// THE FORM REGISTRY IS THREE (docs/SPEC-TABLES.md §3, THE FIRST BYTE): 1
	// the variable form, 2 the message form, 3 this one. Each assigned byte
	// gets its OWN name, and `newer_form` — "a form byte this reader does not
	// carry" — is what is left for a byte the registry has not assigned yet,
	// 0 among them. There is no form 0 for a file to be a PREVIOUS form of,
	// and form 2 is a form this build DOES carry through another surface, so
	// §3's own rule for it is `message_form_as_file`: its table is somewhere
	// else (SPEC-TABLES §4, `unknown_form`).
	g.pf("\tif data[0] != TableFixedForm {\n")
	g.pf("\t\tswitch data[0] {\n")
	g.pf("\t\tcase 1: // the VARIABLE form, and the only form this one is newer than\n\t\t\treturn tableFixedRefuse(report, \"previous_form\")\n")
	g.pf("\t\tcase 2: // the MESSAGE form: a form this build carries, through another surface\n\t\t\treturn tableFixedRefuse(report, \"message_form_as_file\")\n")
	g.pf("\t\t}\n")
	g.pf("\t\treturn tableFixedRefuse(report, \"newer_form\")\n\t}\n")
	g.pf("\tlayoutBytes := tableFixedGet32(data[TableFixedHeaderBytes:])\n")
	g.pf("\tif int64(layoutBytes)+TableFixedHeaderBytes+4 > int64(len(data)) {\n\t\treturn tableFixedRefuse(report, \"layout_malformed\")\n\t}\n")
	g.pf("\tlayout := data[TableFixedHeaderBytes+4 : TableFixedHeaderBytes+4+layoutBytes]\n")
	g.pf("\thash := tableFixedHashOf(layout)\n")
	g.pf("\tif tableFixedGet64(data[TableFixedHashAt:]) != hash {\n\t\treturn tableFixedRefuse(report, \"layout_malformed\")\n\t}\n")
	g.pf("\tat := data[TableFixedHeaderBytes+4+layoutBytes:]\n")
	g.pf("\trest := int64(len(data)) - TableFixedHeaderBytes - 4 - int64(layoutBytes)\n")
	g.pf("\tentries := %sFixedPlan.Entries\n", st.Name)
	g.pf("\tentryCount := %sFixedPlan.Count\n", st.Name)
	g.pf("\trecordBytes := int64(%sFixedRecordBytes)\n", st.Name)
	g.pf("\tif hash != %sFixedHash {\n", st.Name)
	g.pf("\t\tparsed, ok := tableFixedParseLayout(layout)\n")
	g.pf("\t\tif !ok {\n\t\t\treturn tableFixedRefuse(report, \"layout_malformed\")\n\t\t}\n")
	g.pf("\t\tmade := tableFixedCompile(parsed, %sFixedLayout, %sFixedDst, plan, report)\n", st.Name, st.Name)
	g.pf("\t\tif made < 0 {\n\t\t\treturn tableFixedRefuse(report, \"plan_too_large\")\n\t\t}\n")
	g.pf("\t\tentries = plan\n\t\tentryCount = made\n")
	g.pf("\t\trecordBytes = 8 + int64(tableFixedEntryAt(parsed, 0).Size)\n")
	g.pf("\t}\n")
	g.pf("\tif recordBytes <= 8 || rest%%recordBytes != 0 {\n\t\treport.Malformed = true\n\t\treturn -1\n\t}\n")
	g.pf("\tn := rest / recordBytes\n")
	g.pf("\tif n > int64(len(values)) {\n\t\treturn tableFixedRefuse(report, \"batch_too_large\")\n\t}\n")
	g.pf("\tfor k := int64(0); k < n; k++ {\n")
	g.pf("\t\t%sReset(&values[k])\n", st.Name)
	g.pf("\t\tif tableFixedGet64(at) != hash {\n\t\t\treturn tableFixedRefuse(report, \"no_layout\")\n\t\t}\n")
	g.pf("\t\tdst := tableFixedOverlay(unsafe.Pointer(&values[k]), unsafe.Sizeof(values[k]))\n")
	g.pf("\t\ttableFixedRun(entries, entryCount, at[8:], dst, report)\n")
	g.pf("\t\tat = at[recordBytes:]\n")
	g.pf("\t}\n\treturn n\n}\n\n")
}

// emitFixedForm1Load is the form-1 file reader for a DECLARED fixed table
// (#823's keyword, [ir.Struct.FixedDeclared]) — see [tableGen.refusesForm1].
// Form 1 is previous_form; the slow tableOpen walk never runs. Other bytes
// keep the form-1 Load's existing refusal names. The C++ reference still
// accepts form 1 on this type (cpptable/fixedform.go:19); Glenn says refuse.
func (g *tableGen) emitFixedForm1Load(st *ir.Struct) {
	n := st.Name
	g.pf("func %sLoad(value *%s, data []byte, report *TableReport) bool {\n", n, g.storageName(n))
	g.pf("\tif report == nil { var ignored TableReport; report = &ignored }\n")
	g.pf("\t%sReset(value)\n", n)
	g.pf("\tif len(data) > 0 && data[0] == 1 {\n")
	g.pf("\t\ttableFixedRefuse(report, \"previous_form\")\n")
	g.pf("\t\treturn false\n")
	g.pf("\t}\n")
	g.pf("\t_, verdict := tableOpen(data, report); report.Verdict = verdict; report.Reason = \"\"\n")
	g.pf("\tif verdict == TableOpenRefused { report.Reason = \"unsupported wire form\"; if len(data) > 0 && data[0] == 2 { report.Reason = \"message form requires an announced vocabulary and a message reader\" } }\n")
	g.pf("\tif verdict == TableOpenDamaged { report.Malformed = true }\n")
	g.pf("\treturn false\n}\n\n")
}

func (g *tableGen) emitByteArray(b []byte) {
	for i := 0; i < len(b); i += 16 {
		end := min(i+16, len(b))
		var sb strings.Builder
		sb.WriteString("\t")
		for _, v := range b[i:end] {
			fmt.Fprintf(&sb, "0x%02x, ", v)
		}
		g.pf("%s\n", sb.String())
	}
}
