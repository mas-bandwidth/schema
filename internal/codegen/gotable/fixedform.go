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
// THE PLAN'S DESTINATION IS THE RECORD IMAGE, not the public struct. Go's ABI
// is not the wire (FixedTable is 1384 bytes where the body is 1236): there is
// no packed struct, counts sit after arrays, and a union is tag-plus-arms.
// So the identity plan is ONE Copy of the whole body, defaults prefill only
// the holes a compiled plan will not write (identity's list is empty), and a
// generated scatter lands the image in the value. No Reset on the load, and
// no double write of the body.
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
	"math"
	"math/big"
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

func packedOff(n int64) string { return fmt.Sprintf("%d", n) }

func (g *tableGen) fixedWalkRoot(st *ir.Struct) *fixedWalk {
	w := &fixedWalk{}
	w.push(fixedLayoutEntry{id: ir.TableWireId(st.WireName()), kind: ir.TableKindTable, size: fixedTypeBytes(st), children: len(st.Fields), note: st.Name}, dstGo("0", "", "", 0, 0))
	off := int64(0)
	for _, f := range st.Fields {
		g.fixedWalkField(w, f, off)
		off += fixedFieldBytes(f)
	}
	return w
}

func (g *tableGen) fixedWalkField(w *fixedWalk, f *ir.Field, off int64) {
	id := ir.TableFieldWireId(f)
	size := fixedFieldBytes(f)
	if f.Type.Optional {
		w.push(fixedLayoutEntry{id: id, kind: fixedKindOptional, size: size, children: 1, note: f.Name + " ?"},
			dstGo("0", "", packedOff(off), 0, 0))
		g.fixedWalkPayload(w, f, id, size-fixedPresentBytes, off+fixedPresentBytes)
		return
	}
	g.fixedWalkPayload(w, f, id, size, off)
}

func (g *tableGen) fixedWalkPayload(w *fixedWalk, f *ir.Field, id uint64, size, off int64) {
	elem := packedOff(fixedElementBytes(f))
	switch {
	case f.KeyEnum != "":
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindKeyed, size: size, children: 2, note: f.Name},
			dstGo(packedOff(off), elem, "", 0, 0))
		g.fixedWalkEnum(w, f.KeyEnumRef, ir.TableWireId(f.KeyEnum), f.KeyEnum, "0")
		g.fixedWalkElement(w, f, 0, "element", 0)
	case f.Array != ir.ArrayNone:
		counted, aux, payload := 0, "", off
		if f.Array == ir.ArrayCounted {
			counted, aux, payload = 1, packedOff(off), off+fixedCountBytes
		}
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindArray, size: size, children: 1, note: f.Name},
			dstGo(packedOff(payload), elem, aux, counted, 0))
		g.fixedWalkElement(w, f, 0, "element", 0)
	case f.Type.Kind == ir.TBytes:
		// `bytes(N)` is an ARRAY of u8 on this wire (ir/fixedform.go, the
		// TBytes row of tableFixedWalkPayload). Dst is the buffer, Aux is the
		// live length, Counted is 1, stride is 1: the same columns a counted
		// array uses. Identity is one Copy of the body and reads neither
		// column; only the compiled path reads these.
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindArray, size: size, children: 1, note: f.Name},
			dstGo(packedOff(off+fixedCountBytes), "1", packedOff(off), 1, 3))
		w.push(fixedLayoutEntry{id: 0, kind: ir.TableKindU8, size: 1, children: 0, note: "u8"}, dstGo("0", "", "", 0, 0))
	case f.Type.Kind == ir.TString:
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindString, size: size, children: 0, note: f.Name},
			dstGo(packedOff(off), "", packedOff(off+fixedCountBytes), 0, 1))
	case f.Type.Kind == ir.TWString:
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindWstring, size: size, children: 0, note: f.Name},
			dstGo(packedOff(off), "", packedOff(off+fixedCountBytes), 0, 2))
	default:
		g.fixedWalkElement(w, f, id, f.Name, off)
	}
}

func (g *tableGen) fixedWalkElement(w *fixedWalk, f *ir.Field, id uint64, note string, off int64) {
	size := fixedElementBytes(f)
	dst := packedOff(off)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			w.push(fixedLayoutEntry{id: id, kind: ir.TableKindTable, size: size, children: len(r.Fields), note: note}, dstGo(dst, "", "", 0, 0))
			nested := int64(0)
			for _, sub := range r.Fields {
				g.fixedWalkField(w, sub, nested)
				nested += fixedFieldBytes(sub)
			}
			return
		case *ir.Union:
			// Aux is the tag. On the packed image the tag leads the union, so
			// Aux and Dst are the same offset; every arm overlays at tag width.
			w.push(fixedLayoutEntry{id: id, kind: ir.TableKindUnion, size: size, children: len(r.Variants), note: note},
				dstGo(dst, "", dst, 0, 0))
			tag := int64(ir.StorageBitsFor(r.Max) / 8)
			for _, v := range r.Variants {
				armID := ir.TableWireId(v.WireName())
				g.fixedWalkElement(w, v.F, armID, v.Name, tag)
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
	g.pf("// ---- THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4) ----\n")
	g.pf("//\n")
	g.pf("// A record is an eight-byte hash of the writer's layout and then the values\n")
	g.pf("// in declared order, every field at its declared storage width. The writer is\n")
	g.pf("// constant-offset stores; the reader is ONE loop over ONE plan into the\n")
	g.pf("// packed record image, then a scatter into the public struct.\n\n")
	seen := map[string]bool{}
	var order []*ir.Struct
	for _, st := range roots {
		fixedCollectTypes(st, seen, &order)
	}
	for _, st := range order {
		g.emitFixedWriteBody(st)
		g.emitFixedScatter(st)
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

// emitFixedScatter lands ONE packed record image in the public struct. It is
// the only place this port's storage spelling meets the wire's, which is what
// lets the identity plan be one Copy of the body.
func (g *tableGen) emitFixedScatter(st *ir.Struct) {
	g.pf("func %sFixedScatter(b []byte, value *%s, report *TableReport) {\n", st.Name, st.Name)
	g.pf("\t_ = report\n")
	if len(st.Fields) == 0 {
		g.pf("\t_, _ = b, value\n")
	}
	off := int64(0)
	for _, f := range st.Fields {
		g.emitFixedScatterField(f, off, "b", "value", 1)
		off += fixedFieldBytes(f)
	}
	g.pf("}\n\n")
}

func (g *tableGen) emitFixedScatterField(f *ir.Field, off int64, buf, val string, tabs int) {
	ind := strings.Repeat("\t", tabs)
	base := off
	if f.Type.Optional {
		g.pf("%s%s.%sPresent = %s[%d] != 0\n", ind, val, member(f), buf, base)
		base += fixedPresentBytes
	}
	switch {
	case f.KeyEnum != "":
		g.emitFixedScatterLoop(f, buf, packedOff(base), f.KeyEnumRef.Max, val+"."+member(f), tabs)
	case f.Array == ir.ArrayFixed:
		g.emitFixedScatterLoop(f, buf, packedOff(base), f.ArrayBound, val+"."+member(f), tabs)
	case f.Array == ir.ArrayCounted:
		g.emitFixedScatterCount(ind, fmt.Sprintf("%s.%sCount", val, member(f)), buf, packedOff(base), f.ArrayBound)
		g.emitFixedScatterLoop(f, buf, packedOff(base+fixedCountBytes), f.ArrayBound, val+"."+member(f), tabs)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		g.emitFixedScatterCount(ind, fmt.Sprintf("%s.%sLength", val, member(f)), buf, packedOff(base), f.Type.Size)
		g.pf("%scopy(%s.%s[:], %s[%d:%d])\n", ind, val, member(f), buf, base+4, base+4+f.Type.Size)
	case f.Type.Kind == ir.TWString:
		g.emitFixedScatterCount(ind, fmt.Sprintf("%s.%sLength", val, member(f)), buf, packedOff(base), f.Type.Size)
		g.pf("%sfor i := 0; i < %d; i++ {\n%s\t%s.%s[i] = tableFixedGet16(%s[%d+i*2:])\n%s}\n",
			ind, f.Type.Size, ind, val, member(f), buf, base+4, ind)
	default:
		g.emitFixedScatterElement(f, buf, packedOff(base), val+"."+member(f), tabs)
	}
}

func (g *tableGen) emitFixedScatterCount(ind, dst, buf, at string, bound int64) {
	g.pf("%s{\n", ind)
	g.pf("%s\tn := int32(tableFixedGet32(%s[%s:]))\n", ind, buf, at)
	g.pf("%s\tif n < 0 {\n%s\t\tn = 0\n%s\t\treport.Clamped++\n%s\t} else if n > %d {\n%s\t\tn = %d\n%s\t\treport.Clamped++\n%s\t}\n",
		ind, ind, ind, ind, bound, ind, bound, ind, ind)
	g.pf("%s\t%s = n\n", ind, dst)
	g.pf("%s}\n", ind)
}

func (g *tableGen) emitFixedScatterLoop(f *ir.Field, buf, base string, count int64, expr string, tabs int) {
	ind := strings.Repeat("\t", tabs)
	elem := fixedElementBytes(f)
	g.pf("%sfor i := int64(0); i < %d; i++ {\n", ind, count)
	g.emitFixedScatterElement(f, buf, fmt.Sprintf("%s+i*%d", base, elem), expr+"[i]", tabs+1)
	g.pf("%s}\n", ind)
}

func (g *tableGen) emitFixedScatterElement(f *ir.Field, buf, at, expr string, tabs int) {
	ind := strings.Repeat("\t", tabs)
	slice := fmt.Sprintf("%s[%s:]", buf, at)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			g.pf("%s%sFixedScatter(%s, &%s, report)\n", ind, r.Name, slice, expr)
			return
		case *ir.Union:
			tag := int64(ir.StorageBitsFor(r.Max) / 8)
			g.pf("%s{\n", ind)
			g.pf("%s\tvar tag uint64\n", ind)
			g.pf("%s\tfor k := int64(0); k < %d; k++ {\n%s\t\ttag |= uint64(%s[%s+k]) << (8 * uint64(k))\n%s\t}\n",
				ind, tag, ind, buf, at, ind)
			g.pf("%s\tif tag > %d {\n%s\t\ttag = 0\n%s\t\treport.Unknown++\n%s\t}\n",
				ind, len(r.Variants), ind, ind, ind)
			g.pf("%s\t%s.Type = %sType(tag)\n", ind, expr, f.Type.Name)
			g.pf("%s\tswitch %s.Type {\n", ind, expr)
			for _, v := range r.Variants {
				g.pf("%s\tcase %sType%s:\n", ind, f.Type.Name, ir.GoExportName(v.Name))
				g.emitFixedScatterElement(v.F, buf, fmt.Sprintf("%s+%d", at, tag), expr+"."+ir.GoExportName(v.Name), tabs+2)
			}
			g.pf("%s\t}\n%s}\n", ind, ind)
			return
		case *ir.Enum:
			w := int64(r.StorageBits / 8)
			g.pf("%s{\n", ind)
			g.pf("%s\tq := %s\n", ind, fixedGetUint(slice, w))
			g.pf("%s\tif q > %d {\n%s\t\tq = 0\n%s\t}\n", ind, len(r.Variants), ind, ind)
			g.pf("%s\t%s = %s(q)\n", ind, expr, f.Type.Name)
			g.pf("%s}\n", ind)
			return
		case *ir.Flags:
			g.pf("%s%s = %s(tableFixedGet64(%s))\n", ind, expr, f.Type.Name, slice)
			return
		}
	}
	w := fixedStorageBytes(f.Type)
	switch f.Type.Kind {
	case ir.TBool:
		g.pf("%s%s = %s[%s] != 0\n", ind, expr, buf, at)
		return
	case ir.TFloat32:
		g.pf("%s%s = tableFixedGetF32(%s)\n", ind, expr, slice)
		return
	case ir.TFloat64:
		g.pf("%s%s = tableFixedGetF64(%s)\n", ind, expr, slice)
		return
	}
	if w == 16 {
		g.pf("%s%s.Lo = tableFixedGet64(%s)\n", ind, expr, slice)
		g.pf("%s%s.Hi = tableFixedGet64(%s[8:])\n", ind, expr, slice)
		return
	}
	typ := goFieldType(f.Type)
	g.pf("%s%s = %s(%s)\n", ind, expr, typ, fixedGetUint(slice, w))
}

func fixedGetUint(at string, width int64) string {
	switch width {
	case 1:
		return "uint64(tableFixedGet8(" + at + "))"
	case 2:
		return "uint64(tableFixedGet16(" + at + "))"
	case 4:
		return "uint64(tableFixedGet32(" + at + "))"
	}
	return "tableFixedGet64(" + at + ")"
}

func (g *tableGen) emitFixedRoot(st *ir.Struct) {
	w := g.fixedWalkRoot(st)
	layout := fixedLayoutBytes(w.entries)
	hash := fixedLayoutHash(layout)
	body := fixedTypeBytes(st)
	defaults := fixedDefaultImage(st)

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

	g.pf("// THE PREFILL: declared defaults as a packed body. A field a compiled plan\n")
	g.pf("// does not land has no dest write, so what the scatter reads there is this.\n")
	g.pf("var %sFixedDefaults = []byte{\n", st.Name)
	g.emitByteArray(defaults)
	g.pf("}\n\n")

	g.pf("// Identity is one Copy of the packed body. The public struct is not the\n")
	g.pf("// wire, so dest is the record image and scatter lands the value.\n")
	g.pf("var %sFixedPlan = tableFixedPlan{Entries: []TableFixedEntry{{\n", st.Name)
	g.pf("\tSrc: 0, Dst: 0, Size: %sFixedBodyBytes, Guard: tableFixedNoGuard, Op: tableFixedCopy,\n", st.Name)
	g.pf("}}, Count: 1}\n\n")

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
	g.pf("\tidentity := hash == %sFixedHash\n", st.Name)
	g.pf("\tif !identity {\n")
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
	// Prefill is the dest ranges the plan does not land, copied from the
	// packed defaults image. Identity's plan is one Copy of the whole body,
	// so the hole list is empty and the copy below is a no-op. Scatter lands
	// the image in the public struct. There is no Reset.
	g.pf("\tvar image [%sFixedBodyBytes]byte\n", st.Name)
	g.pf("\tholes := tableFixedHoles(entries, entryCount, %sFixedBodyBytes)\n", st.Name)
	g.pf("\tfor k := int64(0); k < n; k++ {\n")
	g.pf("\t\tfor i := range holes {\n")
	g.pf("\t\t\th := holes[i]\n")
	g.pf("\t\t\tcopy(image[h.Off:h.Off+h.Size], %sFixedDefaults[h.Off:h.Off+h.Size])\n", st.Name)
	g.pf("\t\t}\n")
	g.pf("\t\tif tableFixedGet64(at) != hash {\n\t\t\treturn tableFixedRefuse(report, \"no_layout\")\n\t\t}\n")
	g.pf("\t\ttableFixedRun(entries, entryCount, at[8:], image[:], report)\n")
	g.pf("\t\t%sFixedScatter(image[:], &values[k], report)\n", st.Name)
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
	if len(b) == 0 {
		g.pf("\t// empty body\n")
		return
	}
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

func fixedDefaultImage(st *ir.Struct) []byte {
	out := make([]byte, fixedTypeBytes(st))
	fixedDefaultType(out, st)
	return out
}

func fixedDefaultType(out []byte, st *ir.Struct) {
	at := int64(0)
	for _, f := range st.Fields {
		n := fixedFieldBytes(f)
		fixedDefaultField(out[at:at+n], f)
		at += n
	}
}

func fixedDefaultField(out []byte, f *ir.Field) {
	if f.Type.Optional {
		out = out[fixedPresentBytes:]
	}
	switch {
	case f.KeyEnum != "":
		fixedDefaultSlots(out, f, f.KeyEnumRef.Max)
	case f.Array == ir.ArrayFixed:
		fixedDefaultSlots(out, f, f.ArrayBound)
	case f.Array == ir.ArrayCounted:
		fixedPutU32(out, uint32(f.BornCount()))
		fixedDefaultSlots(out[fixedCountBytes:], f, f.ArrayBound)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		fixedPutU32(out, uint32(len(f.DefBytes)))
		copy(out[fixedCountBytes:], f.DefBytes)
	case f.Type.Kind == ir.TWString:
		fixedPutU32(out, uint32(len(f.DefBytes)))
	default:
		fixedDefaultElement(out, f)
	}
}

func fixedDefaultSlots(out []byte, f *ir.Field, count int64) {
	elem := fixedElementBytes(f)
	for i := int64(0); i < count; i++ {
		fixedDefaultElement(out[i*elem:(i+1)*elem], f)
	}
}

func fixedDefaultElement(out []byte, f *ir.Field) {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			fixedDefaultType(out, r)
			return
		case *ir.Enum:
			if f.HasDefault && f.DefVariant != "" {
				for i, v := range r.Variants {
					if v == f.DefVariant {
						fixedPutUint(out, uint64(i+1), fixedStorageBytes(f.Type))
						break
					}
				}
			}
			return
		case *ir.Union:
			return
		}
	}
	if !f.HasDefault {
		return
	}
	switch f.Type.Kind {
	case ir.TBool:
		if f.DefBool {
			out[0] = 1
		}
	case ir.TFloat32:
		fixedPutUint(out, uint64(math.Float32bits(float32(f.DefFloat))), 4)
	case ir.TFloat64:
		fixedPutUint(out, math.Float64bits(f.DefFloat), 8)
	default:
		if f.DefInt == nil {
			return
		}
		fixedPutBig(out, f.DefInt, fixedStorageBytes(f.Type))
	}
}

func fixedPutU32(out []byte, v uint32) { fixedPutUint(out, uint64(v), 4) }

func fixedPutUint(out []byte, v uint64, width int64) {
	for i := int64(0); i < width && i < 8; i++ {
		out[i] = byte(v >> (8 * i))
	}
}

func fixedPutBig(out []byte, v *big.Int, width int64) {
	if width <= 0 {
		return
	}
	n := new(big.Int).Set(v)
	if n.Sign() < 0 {
		mod := new(big.Int).Lsh(big.NewInt(1), uint(width*8))
		n.Add(n, mod)
	}
	raw := n.Bytes()
	for i := 0; i < len(raw) && int64(i) < width; i++ {
		out[i] = raw[len(raw)-1-i]
	}
}
