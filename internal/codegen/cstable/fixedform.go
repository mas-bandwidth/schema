package cstable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const (
	fixedKindOptional = 35
	kFixedNoGuard     = int64(0xFFFFFFFF)
	fixedCountBytes   = int64(4)
	fixedPresentBytes = int64(1)
	fixedLeafCap      = 4096
	kFixedOpFlat      = byte(7)
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

// fixedFlatType reports a type whose STORAGE IMAGE IS ITS WIRE IMAGE: the
// declared order is the storage order, there is no padding anywhere in it, and
// no field of it reorders against the wire. An array of such a type is ONE run
// however many elements it has, which is what keeps a plan small and what
// makes the whole of a big array one move.
func (g *tableGen) fixedFlatType(st *ir.Struct) bool {
	// In C#, all schema structs are generated as classes (reference types), so
	// an array of structs is an array of object references, never flat memory.
	return false
}

// fixedFlatElem is the same question about ONE element of a field. In C#,
// only primitives, enums, and flags have value-type array storage matching the wire.
func (g *tableGen) fixedFlatElem(f *ir.Field) bool {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return g.fixedFlatType(r)
		case *ir.Union:
			return false
		case *ir.Enum, *ir.Flags:
			return true
		}
	}
	switch f.Type.Kind {
	case ir.TString, ir.TWString, ir.TBytes, ir.TMap:
		return false
	}
	return true
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
	flat := g.fixedFlatElem(f)
	n := per
	switch {
	case f.KeyEnum != "":
		n = int(f.KeyEnumRef.Max) * per
		if flat {
			n = 1
		}
	case f.Array == ir.ArrayFixed:
		n = int(f.ArrayBound) * per
		if flat {
			n = 1
		}
	case f.Array == ir.ArrayCounted:
		n = 1 + int(f.ArrayBound)*per
		if flat {
			n = 2
		}
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

type fixedBlockEntry struct {
	id       uint64
	kind     int
	size     int64
	children int
	note     string
}

func fixedBlockBytes(entries []fixedBlockEntry) []byte {
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

func fixedBlockHash(block []byte) uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, b := range block {
		h ^= uint64(b)
		h *= 0x100000001b3
	}
	return h
}

func (g *tableGen) fixedRoots(members []*ir.Struct) []*ir.Struct {
	var out []*ir.Struct
	for _, st := range members {
		if !ir.TableFixedEmitted(g.unit, st) {
			continue
		}
		out = append(out, st)
	}
	return out
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

type fixedSlot struct {
	setRaw       string
	setDouble    string
	setWide      string
	setBytes     string
	setChars     string
	setRawReport string
}

type fixedPlanEntry struct {
	src   int64
	dst   int
	size  int64
	aux   int
	guard int64
	op    byte
	arg   byte
	meta  byte
}

type fixedRootBuild struct {
	g       *tableGen
	entries []fixedBlockEntry
	dstRows []string
	slots   []fixedSlot
	plan    []fixedPlanEntry
}

func (b *fixedRootBuild) fixedFlatElem(f *ir.Field) bool {
	if b.g == nil {
		return false
	}
	return b.g.fixedFlatElem(f)
}

func (b *fixedRootBuild) flatSpanSetter(fieldExpr, elemType string, elemBytes int64) string {
	if elemType == "byte" {
		return fmt.Sprintf("(t, b, l) => { if (%s != null) b.Slice(0, Math.Min(b.Length, %s.Length)).CopyTo(%s); }", fieldExpr, fieldExpr, fieldExpr)
	}
	return fmt.Sprintf("(t, b, l) => { if (%s != null) MemoryMarshal.Cast<byte, %s>(b.Slice(0, Math.Min(b.Length, %s.Length * %d))).CopyTo(%s); }", fieldExpr, elemType, fieldExpr, elemBytes, fieldExpr)
}

func (b *fixedRootBuild) countElementSlots(f *ir.Field) int {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			n := 0
			for _, sub := range r.Fields {
				n += b.countFieldSlots(sub)
			}
			return n
		case *ir.Union:
			n := 1
			for _, v := range r.Variants {
				n += b.countFieldSlots(v.F)
			}
			return n
		}
	}
	return 1
}

func (b *fixedRootBuild) countFieldSlots(f *ir.Field) int {
	n := 0
	if f.Type.Optional {
		n++
	}
	flat := b.fixedFlatElem(f)
	switch {
	case f.KeyEnum != "":
		n += int(f.KeyEnumRef.Max) * b.countElementSlots(f)
	case f.Array == ir.ArrayCounted:
		if flat {
			n += 2
		} else {
			n += 1 + int(f.ArrayBound)*b.countElementSlots(f)
		}
	case f.Array == ir.ArrayFixed:
		if flat {
			n += 1
		} else {
			n += int(f.ArrayBound) * b.countElementSlots(f)
		}
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString || f.Type.Kind == ir.TBytes:
		n += 2
	default:
		n += b.countElementSlots(f)
	}
	return n
}

func (b *fixedRootBuild) addScalarSlot(f *ir.Field, expr string) {
	switch f.Type.Kind {
	case ir.TBool:
		b.slots = append(b.slots, fixedSlot{
			setRaw: fmt.Sprintf("(t, v) => %s = v != 0", expr),
		})
	case ir.TFloat32:
		b.slots = append(b.slots, fixedSlot{
			setRaw:    fmt.Sprintf("(t, v) => %s = BitConverter.UInt32BitsToSingle((uint)v)", expr),
			setDouble: fmt.Sprintf("(t, d) => %s = (float)d", expr),
		})
	case ir.TFloat64:
		b.slots = append(b.slots, fixedSlot{
			setRaw:    fmt.Sprintf("(t, v) => %s = BitConverter.UInt64BitsToDouble(v)", expr),
			setDouble: fmt.Sprintf("(t, d) => %s = d", expr),
		})
	case ir.TInt:
		typ := csFieldType(f.Type)
		if f.Type.Width == 128 {
			b.slots = append(b.slots, fixedSlot{
				setWide: fmt.Sprintf("(t, w) => %s = unchecked((%s)w)", expr, typ),
				setRaw:  fmt.Sprintf("(t, v) => %s = unchecked((%s)v)", expr, typ),
			})
		} else {
			b.slots = append(b.slots, fixedSlot{
				setRaw: fmt.Sprintf("(t, v) => %s = unchecked((%s)v)", expr, typ),
			})
		}
	default:
		typ := csFieldType(f.Type)
		if f.Type.Width == 128 {
			b.slots = append(b.slots, fixedSlot{
				setWide: fmt.Sprintf("(t, w) => %s = unchecked((%s)w)", expr, typ),
				setRaw:  fmt.Sprintf("(t, v) => %s = unchecked((%s)v)", expr, typ),
			})
		} else {
			b.slots = append(b.slots, fixedSlot{
				setRaw: fmt.Sprintf("(t, v) => %s = (%s)v", expr, typ),
			})
		}
	}
}

func (b *fixedRootBuild) pushElementBlockOnly(f *ir.Field, id uint64, note string) {
	elemSize := fixedElementBytes(f)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			b.entries = append(b.entries, fixedBlockEntry{
				id:       id,
				kind:     ir.TableKindTable,
				size:     elemSize,
				children: len(r.Fields),
				note:     note,
			})
			b.dstRows = append(b.dstRows, "new TableFixedDst(0, 0, 0, 0, 0)")
			for _, sub := range r.Fields {
				b.pushFieldBlockOnly(sub, r.Name)
			}
			return
		case *ir.Union:
			b.entries = append(b.entries, fixedBlockEntry{
				id:       id,
				kind:     ir.TableKindUnion,
				size:     elemSize,
				children: len(r.Variants),
				note:     note,
			})
			b.dstRows = append(b.dstRows, "new TableFixedDst(0, 0, 0, 0, 0)")
			for _, v := range r.Variants {
				b.pushElementBlockOnly(v.F, ir.TableWireId(v.WireName()), v.Name)
			}
			return
		case *ir.Enum:
			b.pushEnumBlockOnly(r, id, note)
			return
		}
	}
	b.entries = append(b.entries, fixedBlockEntry{
		id:       id,
		kind:     ir.TableWireScalarKind(f),
		size:     elemSize,
		children: 0,
		note:     note,
	})
	b.dstRows = append(b.dstRows, "new TableFixedDst(0, 0, 0, 0, 0)")
}

func (b *fixedRootBuild) pushEnumBlockOnly(e *ir.Enum, id uint64, note string) {
	b.entries = append(b.entries, fixedBlockEntry{
		id:       id,
		kind:     ir.TableKindEnum,
		size:     int64(e.StorageBits / 8),
		children: len(e.Variants),
		note:     note,
	})
	b.dstRows = append(b.dstRows, "new TableFixedDst(0, 0, 0, 0, 0)")
	for vi := range e.Variants {
		b.entries = append(b.entries, fixedBlockEntry{
			id:       ir.TableWireId(e.VariantWireName(vi)),
			kind:     ir.TableKindNoPayload,
			size:     0,
			children: 0,
			note:     e.Variants[vi],
		})
		b.dstRows = append(b.dstRows, "new TableFixedDst(0, 0, 0, 0, 0)")
	}
}

func (b *fixedRootBuild) pushFieldBlockOnly(f *ir.Field, owner string) {
	id := ir.TableFieldWireId(f)
	size := fixedFieldBytes(f)
	if f.Type.Optional {
		b.entries = append(b.entries, fixedBlockEntry{
			id:       id,
			kind:     fixedKindOptional,
			size:     size,
			children: 1,
			note:     f.Name + " ?",
		})
		b.dstRows = append(b.dstRows, "new TableFixedDst(0, 0, 0, 0, 0)")
		b.pushPayloadBlockOnly(f, owner, id, size-fixedPresentBytes)
		return
	}
	b.pushPayloadBlockOnly(f, owner, id, size)
}

func (b *fixedRootBuild) pushPayloadBlockOnly(f *ir.Field, owner string, id uint64, size int64) {
	switch {
	case f.KeyEnum != "":
		b.entries = append(b.entries, fixedBlockEntry{id: id, kind: ir.TableKindKeyed, size: size, children: 2, note: f.Name})
		b.dstRows = append(b.dstRows, "new TableFixedDst(0, 0, 0, 0, 0)")
		b.pushEnumBlockOnly(f.KeyEnumRef, ir.TableWireId(f.KeyEnum), f.KeyEnum)
		b.pushElementBlockOnly(f, 0, "element")
	case f.Array != ir.ArrayNone:
		b.entries = append(b.entries, fixedBlockEntry{id: id, kind: ir.TableKindArray, size: size, children: 1, note: f.Name})
		b.dstRows = append(b.dstRows, "new TableFixedDst(0, 0, 0, 0, 0)")
		b.pushElementBlockOnly(f, 0, "element")
	case f.Type.Kind == ir.TBytes:
		b.entries = append(b.entries, fixedBlockEntry{id: id, kind: ir.TableKindArray, size: size, children: 1, note: f.Name})
		b.dstRows = append(b.dstRows, "new TableFixedDst(0, 0, 0, 0, 0)")
		b.entries = append(b.entries, fixedBlockEntry{id: 0, kind: ir.TableKindU8, size: 1, children: 0, note: "u8"})
		b.dstRows = append(b.dstRows, "new TableFixedDst(0, 0, 0, 0, 0)")
	case f.Type.Kind == ir.TString:
		b.entries = append(b.entries, fixedBlockEntry{id: id, kind: ir.TableKindString, size: size, children: 0, note: f.Name})
		b.dstRows = append(b.dstRows, "new TableFixedDst(0, 0, 0, 0, 0)")
	case f.Type.Kind == ir.TWString:
		b.entries = append(b.entries, fixedBlockEntry{id: id, kind: ir.TableKindWstring, size: size, children: 0, note: f.Name})
		b.dstRows = append(b.dstRows, "new TableFixedDst(0, 0, 0, 0, 0)")
	default:
		b.pushElementBlockOnly(f, id, f.Name)
	}
}

func (b *fixedRootBuild) buildField(owner *ir.Struct, f *ir.Field, expr string, baseSlot int, wireOff *int64, guard int64, arg byte) {
	prop := member(f)
	fieldExpr := expr + "." + prop
	id := ir.TableFieldWireId(f)
	size := fixedFieldBytes(f)
	fieldWireBase := *wireOff

	if f.Type.Optional {
		presentSlot := len(b.slots)
		b.slots = append(b.slots, fixedSlot{
			setRaw: fmt.Sprintf("(t, v) => %sPresent = v != 0", fieldExpr),
		})
		b.entries = append(b.entries, fixedBlockEntry{
			id:       id,
			kind:     fixedKindOptional,
			size:     size,
			children: 1,
			note:     f.Name + " ?",
		})
		b.dstRows = append(b.dstRows, fmt.Sprintf("new TableFixedDst(0, 0, %d, 0, 0)", presentSlot-baseSlot))
		b.plan = append(b.plan, fixedPlanEntry{
			src:   fieldWireBase,
			dst:   presentSlot,
			size:  1,
			guard: guard,
			op:    0,
			arg:   arg,
		})
		*wireOff += fixedPresentBytes
		b.buildPayload(owner, f, expr, baseSlot, wireOff, id, size-fixedPresentBytes, guard, arg)
		return
	}

	b.buildPayload(owner, f, expr, baseSlot, wireOff, id, size, guard, arg)
}

func (b *fixedRootBuild) buildPayload(owner *ir.Struct, f *ir.Field, expr string, baseSlot int, wireOff *int64, id uint64, size int64, guard int64, arg byte) {
	prop := member(f)
	fieldExpr := expr + "." + prop
	fieldWireBase := *wireOff

	switch {
	case f.KeyEnum != "":
		elemSlots := b.countElementSlots(f)
		elemBaseSlot := len(b.slots)
		b.entries = append(b.entries, fixedBlockEntry{
			id:       id,
			kind:     ir.TableKindKeyed,
			size:     size,
			children: 2,
			note:     f.Name,
		})
		b.dstRows = append(b.dstRows, fmt.Sprintf("new TableFixedDst(%d, %d, 0, 0, 0)", elemBaseSlot-baseSlot, elemSlots))
		b.pushEnumBlockOnly(f.KeyEnumRef, ir.TableWireId(f.KeyEnum), f.KeyEnum)
		b.pushElementBlockOnly(f, 0, "element")
		elemWireSize := fixedElementBytes(f)
		for k := 0; k < int(f.KeyEnumRef.Max); k++ {
			slotExpr := fmt.Sprintf("%s[%d]", fieldExpr, k)
			if owner != nil && owner.IsTable {
				slotExpr = fmt.Sprintf("%s.Slots[%d]", fieldExpr, k)
			}
			elemWire := fieldWireBase + int64(k)*elemWireSize
			*wireOff = elemWire
			b.buildElementSlotsAndPlan(f, slotExpr, elemBaseSlot+k*elemSlots, guard, arg, wireOff)
		}
		*wireOff = fieldWireBase + size

	case f.Array == ir.ArrayCounted:
		countSlot := len(b.slots)
		b.slots = append(b.slots, fixedSlot{
			setRaw: fmt.Sprintf("(t, v) => %sCount = (int)v", fieldExpr),
		})
		b.plan = append(b.plan, fixedPlanEntry{
			src:   fieldWireBase,
			dst:   countSlot,
			size:  f.ArrayBound,
			guard: guard,
			op:    1,
			arg:   arg,
		})
		elemWireSize := fixedElementBytes(f)
		totalElemBytes := f.ArrayBound * elemWireSize
		elemBaseSlot := len(b.slots)
		b.entries = append(b.entries, fixedBlockEntry{
			id:       id,
			kind:     ir.TableKindArray,
			size:     size,
			children: 1,
			note:     f.Name,
		})
		if b.fixedFlatElem(f) {
			b.dstRows = append(b.dstRows, fmt.Sprintf("new TableFixedDst(%d, 0, %d, 1, 0)", elemBaseSlot-baseSlot, countSlot-baseSlot))
			b.pushElementBlockOnly(f, 0, "element")
			elemType := csFieldType(f.Type)
			b.slots = append(b.slots, fixedSlot{
				setBytes: b.flatSpanSetter(fieldExpr, elemType, elemWireSize),
			})
			b.plan = append(b.plan, fixedPlanEntry{
				src:   fieldWireBase + fixedCountBytes,
				dst:   elemBaseSlot,
				size:  totalElemBytes,
				guard: guard,
				op:    kFixedOpFlat,
				arg:   arg,
			})
		} else {
			elemSlots := b.countElementSlots(f)
			b.dstRows = append(b.dstRows, fmt.Sprintf("new TableFixedDst(%d, %d, %d, 1, 0)", elemBaseSlot-baseSlot, elemSlots, countSlot-baseSlot))
			b.pushElementBlockOnly(f, 0, "element")
			for i := 0; i < int(f.ArrayBound); i++ {
				elemExpr := fmt.Sprintf("%s[%d]", fieldExpr, i)
				elemWire := fieldWireBase + fixedCountBytes + int64(i)*elemWireSize
				*wireOff = elemWire
				b.buildElementSlotsAndPlan(f, elemExpr, elemBaseSlot+i*elemSlots, guard, arg, wireOff)
			}
		}
		*wireOff = fieldWireBase + size

	case f.Array == ir.ArrayFixed:
		elemWireSize := fixedElementBytes(f)
		totalElemBytes := f.ArrayBound * elemWireSize
		elemBaseSlot := len(b.slots)
		b.entries = append(b.entries, fixedBlockEntry{
			id:       id,
			kind:     ir.TableKindArray,
			size:     size,
			children: 1,
			note:     f.Name,
		})
		if b.fixedFlatElem(f) {
			b.dstRows = append(b.dstRows, fmt.Sprintf("new TableFixedDst(%d, 0, 0, 0, 0)", elemBaseSlot-baseSlot))
			b.pushElementBlockOnly(f, 0, "element")
			elemType := csFieldType(f.Type)
			b.slots = append(b.slots, fixedSlot{
				setBytes: b.flatSpanSetter(fieldExpr, elemType, elemWireSize),
			})
			b.plan = append(b.plan, fixedPlanEntry{
				src:   fieldWireBase,
				dst:   elemBaseSlot,
				size:  totalElemBytes,
				guard: guard,
				op:    kFixedOpFlat,
				arg:   arg,
			})
		} else {
			elemSlots := b.countElementSlots(f)
			b.dstRows = append(b.dstRows, fmt.Sprintf("new TableFixedDst(%d, %d, 0, 0, 0)", elemBaseSlot-baseSlot, elemSlots))
			b.pushElementBlockOnly(f, 0, "element")
			for i := 0; i < int(f.ArrayBound); i++ {
				elemExpr := fmt.Sprintf("%s[%d]", fieldExpr, i)
				elemWire := fieldWireBase + int64(i)*elemWireSize
				*wireOff = elemWire
				b.buildElementSlotsAndPlan(f, elemExpr, elemBaseSlot+i*elemSlots, guard, arg, wireOff)
			}
		}
		*wireOff = fieldWireBase + size

	case f.Type.Kind == ir.TBytes:
		lenSlot := len(b.slots)
		b.slots = append(b.slots, fixedSlot{
			setRaw: fmt.Sprintf("(t, v) => %sLength = (int)v", fieldExpr),
		})
		bufSlot := len(b.slots)
		b.slots = append(b.slots, fixedSlot{
			setBytes: fmt.Sprintf("(t, b, l) => { if (%s != null) b.Slice(0, Math.Min(b.Length, %s.Length)).CopyTo(%s); }", fieldExpr, fieldExpr, fieldExpr),
		})
		b.entries = append(b.entries, fixedBlockEntry{
			id:       id,
			kind:     ir.TableKindArray,
			size:     size,
			children: 1,
			note:     f.Name,
		})
		b.dstRows = append(b.dstRows, fmt.Sprintf("new TableFixedDst(%d, 1, %d, 1, 3)", bufSlot-baseSlot, lenSlot-baseSlot))
		b.entries = append(b.entries, fixedBlockEntry{id: 0, kind: ir.TableKindU8, size: 1, children: 0, note: "u8"})
		b.dstRows = append(b.dstRows, "new TableFixedDst(0, 0, 0, 0, 0)")
		b.plan = append(b.plan, fixedPlanEntry{
			src:   fieldWireBase,
			dst:   lenSlot,
			size:  f.Type.Size,
			aux:   bufSlot,
			guard: guard,
			op:    2,
			arg:   arg,
			meta:  3,
		})
		*wireOff += size

	case f.Type.Kind == ir.TString:
		lenSlot := len(b.slots)
		b.slots = append(b.slots, fixedSlot{
			setRaw: fmt.Sprintf("(t, v) => %sLength = (int)v", fieldExpr),
		})
		bufSlot := len(b.slots)
		b.slots = append(b.slots, fixedSlot{
			setBytes: fmt.Sprintf("(t, b, l) => { if (%s != null) b.Slice(0, Math.Min(b.Length, %s.Length)).CopyTo(%s); }", fieldExpr, fieldExpr, fieldExpr),
		})
		b.entries = append(b.entries, fixedBlockEntry{
			id:       id,
			kind:     ir.TableKindString,
			size:     size,
			children: 0,
			note:     f.Name,
		})
		b.dstRows = append(b.dstRows, fmt.Sprintf("new TableFixedDst(%d, 0, %d, 0, 1)", lenSlot-baseSlot, bufSlot-baseSlot))
		b.plan = append(b.plan, fixedPlanEntry{
			src:   fieldWireBase,
			dst:   lenSlot,
			size:  f.Type.Size,
			aux:   bufSlot,
			guard: guard,
			op:    2,
			arg:   arg,
			meta:  1,
		})
		*wireOff += size

	case f.Type.Kind == ir.TWString:
		lenSlot := len(b.slots)
		b.slots = append(b.slots, fixedSlot{
			setRaw: fmt.Sprintf("(t, v) => %sLength = (int)v", fieldExpr),
		})
		bufSlot := len(b.slots)
		b.slots = append(b.slots, fixedSlot{
			setChars: fmt.Sprintf("(t, b, l) => { ReadOnlySpan<char> c = MemoryMarshal.Cast<byte, char>(b); if (%s != null) c.Slice(0, Math.Min(c.Length, %s.Length)).CopyTo(%s); }", fieldExpr, fieldExpr, fieldExpr),
		})
		b.entries = append(b.entries, fixedBlockEntry{
			id:       id,
			kind:     ir.TableKindWstring,
			size:     size,
			children: 0,
			note:     f.Name,
		})
		b.dstRows = append(b.dstRows, fmt.Sprintf("new TableFixedDst(%d, 0, %d, 0, 2)", lenSlot-baseSlot, bufSlot-baseSlot))
		b.plan = append(b.plan, fixedPlanEntry{
			src:   fieldWireBase,
			dst:   lenSlot,
			size:  2 * f.Type.Size,
			aux:   bufSlot,
			guard: guard,
			op:    2,
			arg:   arg,
			meta:  2,
		})
		*wireOff += size

	default:
		b.buildElement(f, fieldExpr, baseSlot, id, f.Name, guard, arg, wireOff)
	}
}

func (b *fixedRootBuild) buildElement(f *ir.Field, expr string, baseSlot int, id uint64, note string, guard int64, arg byte, wireOff *int64) {
	wireBase := *wireOff
	elemSize := fixedElementBytes(f)

	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			structBase := len(b.slots)
			b.entries = append(b.entries, fixedBlockEntry{
				id:       id,
				kind:     ir.TableKindTable,
				size:     elemSize,
				children: len(r.Fields),
				note:     note,
			})
			b.dstRows = append(b.dstRows, fmt.Sprintf("new TableFixedDst(%d, 0, 0, 0, 0)", structBase-baseSlot))
			for _, sub := range r.Fields {
				b.buildField(r, sub, expr, structBase, wireOff, guard, arg)
			}
			return

		case *ir.Union:
			tagBytes := int64(ir.StorageBitsFor(r.Max) / 8)
			tagSlot := len(b.slots)
			tagType := f.Type.Name + "Type"
			b.slots = append(b.slots, fixedSlot{
				setRawReport: fmt.Sprintf("(t, v, rep) => { if (v > %d) { %s.Type = (%s)0; if (rep != null) rep.Clamped++; } else { %s.Type = (%s)v; } }", len(r.Variants), expr, tagType, expr, tagType),
			})
			armBase := len(b.slots)
			b.entries = append(b.entries, fixedBlockEntry{
				id:       id,
				kind:     ir.TableKindUnion,
				size:     elemSize,
				children: len(r.Variants),
				note:     note,
			})
			b.dstRows = append(b.dstRows, fmt.Sprintf("new TableFixedDst(%d, 0, %d, 0, 0)", armBase-baseSlot, tagSlot-baseSlot))
			b.plan = append(b.plan, fixedPlanEntry{
				src:   wireBase,
				dst:   tagSlot,
				size:  tagBytes,
				guard: guard,
				op:    0,
				arg:   arg,
			})
			armWireOffset := wireBase + tagBytes
			for j, v := range r.Variants {
				armID := ir.TableWireId(v.WireName())
				armProp := ir.GoExportName(v.Name)
				armExpr := fmt.Sprintf("%s.%s", expr, armProp)
				*wireOff = armWireOffset
				b.buildElement(v.F, armExpr, armBase, armID, v.Name, wireBase, byte(j+1), wireOff)
			}
			*wireOff = wireBase + elemSize
			return

		case *ir.Enum:
			slot := len(b.slots)
			w := int64(r.StorageBits / 8)
			b.slots = append(b.slots, fixedSlot{
				setRawReport: fmt.Sprintf("(t, v, rep) => { if (v > %d) { %s = 0; if (rep != null) rep.Clamped++; } else { %s = (%s)v; } }", len(r.Variants), expr, expr, f.Type.Name),
			})
			b.entries = append(b.entries, fixedBlockEntry{
				id:       id,
				kind:     ir.TableKindEnum,
				size:     w,
				children: len(r.Variants),
				note:     note,
			})
			b.dstRows = append(b.dstRows, fmt.Sprintf("new TableFixedDst(%d, 0, 0, 0, 0)", slot-baseSlot))
			for vi := range r.Variants {
				b.entries = append(b.entries, fixedBlockEntry{
					id:       ir.TableWireId(r.VariantWireName(vi)),
					kind:     ir.TableKindNoPayload,
					size:     0,
					children: 0,
					note:     r.Variants[vi],
				})
				b.dstRows = append(b.dstRows, "new TableFixedDst(0, 0, 0, 0, 0)")
			}
			b.plan = append(b.plan, fixedPlanEntry{
				src:   wireBase,
				dst:   slot,
				size:  w,
				guard: guard,
				op:    0,
				arg:   arg,
			})
			*wireOff += w
			return

		case *ir.Flags:
			slot := len(b.slots)
			b.slots = append(b.slots, fixedSlot{
				setRaw: fmt.Sprintf("(t, v) => %s = v", expr),
			})
			b.entries = append(b.entries, fixedBlockEntry{
				id:       id,
				kind:     ir.TableWireScalarKind(f),
				size:     8,
				children: 0,
				note:     note,
			})
			b.dstRows = append(b.dstRows, fmt.Sprintf("new TableFixedDst(%d, 0, 0, 0, 0)", slot-baseSlot))
			b.plan = append(b.plan, fixedPlanEntry{
				src:   wireBase,
				dst:   slot,
				size:  8,
				guard: guard,
				op:    0,
				arg:   arg,
			})
			*wireOff += 8
			return
		}
	}

	slot := len(b.slots)
	w := fixedStorageBytes(f.Type)
	b.entries = append(b.entries, fixedBlockEntry{
		id:       id,
		kind:     ir.TableWireScalarKind(f),
		size:     w,
		children: 0,
		note:     note,
	})
	b.dstRows = append(b.dstRows, fmt.Sprintf("new TableFixedDst(%d, 0, 0, 0, 0)", slot-baseSlot))
	b.addScalarSlot(f, expr)
	b.plan = append(b.plan, fixedPlanEntry{
		src:   wireBase,
		dst:   slot,
		size:  w,
		guard: guard,
		op:    0,
		arg:   arg,
	})
	*wireOff += w
}

func (b *fixedRootBuild) buildElementSlotsAndPlan(f *ir.Field, expr string, baseSlot int, guard int64, arg byte, wireOff *int64) {
	wireBase := *wireOff
	elemSize := fixedElementBytes(f)

	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			for _, sub := range r.Fields {
				b.buildFieldSlotsAndPlan(r, sub, expr, baseSlot, wireOff, guard, arg)
			}
			return
		case *ir.Union:
			tagBytes := int64(ir.StorageBitsFor(r.Max) / 8)
			tagSlot := len(b.slots)
			tagType := f.Type.Name + "Type"
			b.slots = append(b.slots, fixedSlot{
				setRawReport: fmt.Sprintf("(t, v, rep) => { if (v > %d) { %s.Type = (%s)0; if (rep != null) rep.Clamped++; } else { %s.Type = (%s)v; } }", len(r.Variants), expr, tagType, expr, tagType),
			})
			b.plan = append(b.plan, fixedPlanEntry{
				src:   wireBase,
				dst:   tagSlot,
				size:  tagBytes,
				guard: guard,
				op:    0,
				arg:   arg,
			})
			armWireOffset := wireBase + tagBytes
			for j, v := range r.Variants {
				armProp := ir.GoExportName(v.Name)
				armExpr := fmt.Sprintf("%s.%s", expr, armProp)
				*wireOff = armWireOffset
				b.buildElementSlotsAndPlan(v.F, armExpr, baseSlot, wireBase, byte(j+1), wireOff)
			}
			*wireOff = wireBase + elemSize
			return
		case *ir.Enum:
			slot := len(b.slots)
			w := int64(r.StorageBits / 8)
			b.slots = append(b.slots, fixedSlot{
				setRawReport: fmt.Sprintf("(t, v, rep) => { if (v > %d) { %s = 0; if (rep != null) rep.Clamped++; } else { %s = (%s)v; } }", len(r.Variants), expr, expr, f.Type.Name),
			})
			b.plan = append(b.plan, fixedPlanEntry{
				src:   wireBase,
				dst:   slot,
				size:  w,
				guard: guard,
				op:    0,
				arg:   arg,
			})
			*wireOff += w
			return
		case *ir.Flags:
			slot := len(b.slots)
			b.slots = append(b.slots, fixedSlot{
				setRaw: fmt.Sprintf("(t, v) => %s = v", expr),
			})
			b.plan = append(b.plan, fixedPlanEntry{
				src:   wireBase,
				dst:   slot,
				size:  8,
				guard: guard,
				op:    0,
				arg:   arg,
			})
			*wireOff += 8
			return
		}
	}

	slot := len(b.slots)
	w := fixedStorageBytes(f.Type)
	b.addScalarSlot(f, expr)
	b.plan = append(b.plan, fixedPlanEntry{
		src:   wireBase,
		dst:   slot,
		size:  w,
		guard: guard,
		op:    0,
		arg:   arg,
	})
	*wireOff += w
}

func (b *fixedRootBuild) buildFieldSlotsAndPlan(owner *ir.Struct, f *ir.Field, expr string, baseSlot int, wireOff *int64, guard int64, arg byte) {
	prop := member(f)
	fieldExpr := expr + "." + prop
	fieldWireBase := *wireOff
	size := fixedFieldBytes(f)

	if f.Type.Optional {
		presentSlot := len(b.slots)
		b.slots = append(b.slots, fixedSlot{
			setRaw: fmt.Sprintf("(t, v) => %sPresent = v != 0", fieldExpr),
		})
		b.plan = append(b.plan, fixedPlanEntry{
			src:   fieldWireBase,
			dst:   presentSlot,
			size:  1,
			guard: guard,
			op:    0,
			arg:   arg,
		})
		*wireOff += fixedPresentBytes
		b.buildPayloadSlotsAndPlan(owner, f, expr, baseSlot, wireOff, size-fixedPresentBytes, guard, arg)
		return
	}

	b.buildPayloadSlotsAndPlan(owner, f, expr, baseSlot, wireOff, size, guard, arg)
}

func (b *fixedRootBuild) buildPayloadSlotsAndPlan(owner *ir.Struct, f *ir.Field, expr string, baseSlot int, wireOff *int64, size int64, guard int64, arg byte) {
	prop := member(f)
	fieldExpr := expr + "." + prop
	fieldWireBase := *wireOff

	switch {
	case f.KeyEnum != "":
		elemSlots := b.countElementSlots(f)
		elemBaseSlot := len(b.slots)
		elemWireSize := fixedElementBytes(f)
		for k := 0; k < int(f.KeyEnumRef.Max); k++ {
			slotExpr := fmt.Sprintf("%s[%d]", fieldExpr, k)
			if owner != nil && owner.IsTable {
				slotExpr = fmt.Sprintf("%s.Slots[%d]", fieldExpr, k)
			}
			elemWire := fieldWireBase + int64(k)*elemWireSize
			*wireOff = elemWire
			b.buildElementSlotsAndPlan(f, slotExpr, elemBaseSlot+k*elemSlots, guard, arg, wireOff)
		}
		*wireOff = fieldWireBase + size

	case f.Array == ir.ArrayCounted:
		countSlot := len(b.slots)
		b.slots = append(b.slots, fixedSlot{
			setRaw: fmt.Sprintf("(t, v) => %sCount = (int)v", fieldExpr),
		})
		b.plan = append(b.plan, fixedPlanEntry{
			src:   fieldWireBase,
			dst:   countSlot,
			size:  f.ArrayBound,
			guard: guard,
			op:    1,
			arg:   arg,
		})
		elemWireSize := fixedElementBytes(f)
		totalElemBytes := f.ArrayBound * elemWireSize
		elemBaseSlot := len(b.slots)
		if b.fixedFlatElem(f) {
			elemType := csFieldType(f.Type)
			b.slots = append(b.slots, fixedSlot{
				setBytes: b.flatSpanSetter(fieldExpr, elemType, elemWireSize),
			})
			b.plan = append(b.plan, fixedPlanEntry{
				src:   fieldWireBase + fixedCountBytes,
				dst:   elemBaseSlot,
				size:  totalElemBytes,
				guard: guard,
				op:    kFixedOpFlat,
				arg:   arg,
			})
		} else {
			elemSlots := b.countElementSlots(f)
			for i := 0; i < int(f.ArrayBound); i++ {
				elemExpr := fmt.Sprintf("%s[%d]", fieldExpr, i)
				elemWire := fieldWireBase + fixedCountBytes + int64(i)*elemWireSize
				*wireOff = elemWire
				b.buildElementSlotsAndPlan(f, elemExpr, elemBaseSlot+i*elemSlots, guard, arg, wireOff)
			}
		}
		*wireOff = fieldWireBase + size

	case f.Array == ir.ArrayFixed:
		elemWireSize := fixedElementBytes(f)
		totalElemBytes := f.ArrayBound * elemWireSize
		elemBaseSlot := len(b.slots)
		if b.fixedFlatElem(f) {
			elemType := csFieldType(f.Type)
			b.slots = append(b.slots, fixedSlot{
				setBytes: b.flatSpanSetter(fieldExpr, elemType, elemWireSize),
			})
			b.plan = append(b.plan, fixedPlanEntry{
				src:   fieldWireBase,
				dst:   elemBaseSlot,
				size:  totalElemBytes,
				guard: guard,
				op:    kFixedOpFlat,
				arg:   arg,
			})
		} else {
			elemSlots := b.countElementSlots(f)
			for i := 0; i < int(f.ArrayBound); i++ {
				elemExpr := fmt.Sprintf("%s[%d]", fieldExpr, i)
				elemWire := fieldWireBase + int64(i)*elemWireSize
				*wireOff = elemWire
				b.buildElementSlotsAndPlan(f, elemExpr, elemBaseSlot+i*elemSlots, guard, arg, wireOff)
			}
		}
		*wireOff = fieldWireBase + size

	case f.Type.Kind == ir.TBytes:
		lenSlot := len(b.slots)
		b.slots = append(b.slots, fixedSlot{
			setRaw: fmt.Sprintf("(t, v) => %sLength = (int)v", fieldExpr),
		})
		bufSlot := len(b.slots)
		b.slots = append(b.slots, fixedSlot{
			setBytes: fmt.Sprintf("(t, b, l) => { if (%s != null) b.Slice(0, Math.Min(b.Length, %s.Length)).CopyTo(%s); }", fieldExpr, fieldExpr, fieldExpr),
		})
		b.plan = append(b.plan, fixedPlanEntry{
			src:   fieldWireBase,
			dst:   lenSlot,
			size:  f.Type.Size,
			aux:   bufSlot,
			guard: guard,
			op:    2,
			arg:   arg,
			meta:  3,
		})
		*wireOff += size

	case f.Type.Kind == ir.TString:
		lenSlot := len(b.slots)
		b.slots = append(b.slots, fixedSlot{
			setRaw: fmt.Sprintf("(t, v) => %sLength = (int)v", fieldExpr),
		})
		bufSlot := len(b.slots)
		b.slots = append(b.slots, fixedSlot{
			setBytes: fmt.Sprintf("(t, b, l) => { if (%s != null) b.Slice(0, Math.Min(b.Length, %s.Length)).CopyTo(%s); }", fieldExpr, fieldExpr, fieldExpr),
		})
		b.plan = append(b.plan, fixedPlanEntry{
			src:   fieldWireBase,
			dst:   lenSlot,
			size:  f.Type.Size,
			aux:   bufSlot,
			guard: guard,
			op:    2,
			arg:   arg,
			meta:  1,
		})
		*wireOff += size

	case f.Type.Kind == ir.TWString:
		lenSlot := len(b.slots)
		b.slots = append(b.slots, fixedSlot{
			setRaw: fmt.Sprintf("(t, v) => %sLength = (int)v", fieldExpr),
		})
		bufSlot := len(b.slots)
		b.slots = append(b.slots, fixedSlot{
			setChars: fmt.Sprintf("(t, b, l) => { ReadOnlySpan<char> c = MemoryMarshal.Cast<byte, char>(b); if (%s != null) c.Slice(0, Math.Min(c.Length, %s.Length)).CopyTo(%s); }", fieldExpr, fieldExpr, fieldExpr),
		})
		b.plan = append(b.plan, fixedPlanEntry{
			src:   fieldWireBase,
			dst:   lenSlot,
			size:  2 * f.Type.Size,
			aux:   bufSlot,
			guard: guard,
			op:    2,
			arg:   arg,
			meta:  2,
		})
		*wireOff += size

	default:
		b.buildElementSlotsAndPlan(f, fieldExpr, baseSlot, guard, arg, wireOff)
	}
}

func (g *tableGen) emitFixedForm(members []*ir.Struct) {
	roots := g.fixedRoots(members)
	if len(roots) == 0 {
		return
	}
	g.pf("// ---- THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4) ----\n")
	g.pf("//\n")
	g.pf("// A record is an eight-byte hash of the writer's vocabulary block and then\n")
	g.pf("// the values in declared order, every field at its declared storage width.\n")
	g.pf("// The writer is the constant bytes memcpy'd and then stores; the reader is\n")
	g.pf("// ONE loop over ONE plan, the identity plan here and a plan compiled from\n")
	g.pf("// the writer's own block for anybody else.\n\n")

	seen := map[string]bool{}
	var order []*ir.Struct
	for _, st := range roots {
		fixedCollectTypes(st, seen, &order)
	}
	for _, st := range order {
		g.emitFixedWriteBody(st)
	}
	for _, st := range roots {
		g.emitFixedRoot(st)
	}
}

func (g *tableGen) emitFixedWriteBody(st *ir.Struct) {
	name := st.Name
	g.pf("// %s's stores. The template — the hash, then zeros — is memcpy'd first,\n", name)
	g.pf("// which is also what zero-fills every byte of declared slack.\n")
	g.pf("public static void %sFixedWriteBody(Span<byte> b, %s value)\n{\n", name, name)
	g.pf("    if (value == null) return;\n")
	off := int64(0)
	for _, f := range st.Fields {
		g.emitFixedWriteField(st, f, off, "b", "value", 4)
		off += fixedFieldBytes(f)
	}
	g.pf("}\n\n")
}

func (g *tableGen) emitFixedWriteField(owner *ir.Struct, f *ir.Field, off int64, buf, val string, indent int) {
	ind := strings.Repeat(" ", indent)
	base := off
	prop := member(f)
	if f.Type.Optional {
		g.pf("%s%s[%d] = (byte)(%s.%sPresent ? 1 : 0);\n", ind, buf, base, val, prop)
		base += fixedPresentBytes
	}
	switch {
	case f.KeyEnum != "":
		g.emitFixedWriteKeyedLoop(owner, f, base, f.KeyEnumRef.Max, buf, val+"."+prop, indent)
	case f.Array == ir.ArrayFixed:
		g.emitFixedWriteLoop(f, base, f.ArrayBound, buf, val+"."+prop, indent)
	case f.Array == ir.ArrayCounted:
		g.pf("%sBinaryPrimitives.WriteInt32LittleEndian(%s.Slice(%d), %s.%sCount);\n", ind, buf, base, val, prop)
		g.emitFixedWriteLoop(f, base+fixedCountBytes, f.ArrayBound, buf, val+"."+prop, indent)
	case f.Type.Kind == ir.TString:
		g.pf("%sint len_%s = %s.%s != null ? %s.%sLength : 0;\n", ind, f.Name, val, prop, val, prop)
		g.pf("%sSystem.Diagnostics.Debug.Assert(len_%s <= %d);\n", ind, f.Name, f.Type.Size)
		g.pf("%sBinaryPrimitives.WriteInt32LittleEndian(%s.Slice(%d), len_%s);\n", ind, buf, base, f.Name)
		g.pf("%sif (len_%s > 0) { %s.%s.AsSpan(0, len_%s).CopyTo(%s.Slice(%d)); }\n", ind, f.Name, val, prop, f.Name, buf, base+4)
	case f.Type.Kind == ir.TWString:
		g.pf("%sint len_%s = %s.%s != null ? %s.%sLength : 0;\n", ind, f.Name, val, prop, val, prop)
		g.pf("%sSystem.Diagnostics.Debug.Assert(len_%s <= %d);\n", ind, f.Name, f.Type.Size)
		g.pf("%sBinaryPrimitives.WriteInt32LittleEndian(%s.Slice(%d), len_%s);\n", ind, buf, base, f.Name)
		g.pf("%sif (len_%s > 0) { MemoryMarshal.AsBytes(%s.%s.AsSpan(0, len_%s)).CopyTo(%s.Slice(%d)); }\n", ind, f.Name, val, prop, f.Name, buf, base+4)
	case f.Type.Kind == ir.TBytes:
		g.pf("%sint len_%s = %s.%s != null ? %s.%sLength : 0;\n", ind, f.Name, val, prop, val, prop)
		g.pf("%sSystem.Diagnostics.Debug.Assert(len_%s <= %d);\n", ind, f.Name, f.Type.Size)
		g.pf("%sBinaryPrimitives.WriteInt32LittleEndian(%s.Slice(%d), len_%s);\n", ind, buf, base, f.Name)
		g.pf("%sif (len_%s > 0) { %s.%s.AsSpan(0, len_%s).CopyTo(%s.Slice(%d)); }\n", ind, f.Name, val, prop, f.Name, buf, base+4)
	default:
		g.emitFixedWriteElement(f, base, buf, val+"."+prop, indent)
	}
}

func (g *tableGen) emitFixedWriteKeyedLoop(owner *ir.Struct, f *ir.Field, base, count int64, buf, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	elem := fixedElementBytes(f)
	slotsExpr := expr
	if owner != nil && owner.IsTable {
		slotsExpr = expr + ".Slots"
	}
	if g.fixedFlatElem(f) {
		elemType := csFieldType(f.Type)
		slice := fmt.Sprintf("%s.Slice(%d)", buf, base)
		if base == 0 {
			slice = buf
		}
		if elemType == "byte" {
			g.pf("%sif (%s != null) { %s.AsSpan(0, Math.Min(%s.Length, %d)).CopyTo(%s); }\n", ind, slotsExpr, slotsExpr, slotsExpr, count, slice)
		} else {
			g.pf("%sif (%s != null) { MemoryMarshal.AsBytes(%s.AsSpan(0, Math.Min(%s.Length, %d))).CopyTo(%s); }\n", ind, slotsExpr, slotsExpr, slotsExpr, count, slice)
		}
		return
	}
	g.pf("%sif (%s != null)\n%s{\n", ind, slotsExpr, ind)
	g.pf("%s    for (int i = 0; i < %d && i < %s.Length; ++i)\n%s    {\n", ind, count, slotsExpr, ind)
	g.emitFixedWriteElement(f, 0, fmt.Sprintf("%s.Slice(%d + i * %d)", buf, base, elem), fmt.Sprintf("%s[i]", slotsExpr), indent+8)
	g.pf("%s    }\n%s}\n", ind, ind)
}

func (g *tableGen) emitFixedWriteLoop(f *ir.Field, base, count int64, buf, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	elem := fixedElementBytes(f)
	if g.fixedFlatElem(f) {
		elemType := csFieldType(f.Type)
		slice := fmt.Sprintf("%s.Slice(%d)", buf, base)
		if base == 0 {
			slice = buf
		}
		if elemType == "byte" {
			g.pf("%sif (%s != null) { %s.AsSpan(0, Math.Min(%s.Length, %d)).CopyTo(%s); }\n", ind, expr, expr, expr, count, slice)
		} else {
			g.pf("%sif (%s != null) { MemoryMarshal.AsBytes(%s.AsSpan(0, Math.Min(%s.Length, %d))).CopyTo(%s); }\n", ind, expr, expr, expr, count, slice)
		}
		return
	}
	g.pf("%sif (%s != null)\n%s{\n", ind, expr, ind)
	g.pf("%s    for (int i = 0; i < %d && i < %s.Length; ++i)\n%s    {\n", ind, count, expr, ind)
	g.emitFixedWriteElement(f, 0, fmt.Sprintf("%s.Slice(%d + i * %d)", buf, base, elem), fmt.Sprintf("%s[i]", expr), indent+8)
	g.pf("%s    }\n%s}\n", ind, ind)
}

func (g *tableGen) emitFixedWriteElement(f *ir.Field, off int64, buf, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	slice := fmt.Sprintf("%s.Slice(%d)", buf, off)
	if off == 0 {
		slice = buf
	}
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			g.pf("%s%sFixedWriteBody(%s, %s);\n", ind, r.Name, slice, expr)
			return
		case *ir.Union:
			tagBytes := int64(ir.StorageBitsFor(r.Max) / 8)
			g.pf("%sif (%s != null)\n%s{\n", ind, expr, ind)
			tagType := f.Type.Name + "Type"
			switch tagBytes {
			case 1:
				g.pf("%s    %s[0] = (byte)%s.Type;\n", ind, slice, expr)
			case 2:
				g.pf("%s    BinaryPrimitives.WriteUInt16LittleEndian(%s, (ushort)%s.Type);\n", ind, slice, expr)
			case 4:
				g.pf("%s    BinaryPrimitives.WriteUInt32LittleEndian(%s, (uint)%s.Type);\n", ind, slice, expr)
			}
			g.pf("%s    switch (%s.Type)\n%s    {\n", ind, expr, ind)
			for _, v := range r.Variants {
				armProp := ir.GoExportName(v.Name)
				g.pf("%s        case %s.%s:\n", ind, tagType, armProp)
				armSlice := fmt.Sprintf("%s.Slice(%d)", slice, tagBytes)
				g.emitFixedWriteElement(v.F, 0, armSlice, fmt.Sprintf("%s.%s", expr, armProp), indent+12)
				g.pf("%s            break;\n", ind)
			}
			g.pf("%s        default: break;\n%s    }\n%s}\n", ind, ind, ind)
			return
		case *ir.Enum:
			w := int64(r.StorageBits / 8)
			switch w {
			case 1:
				g.pf("%s%s[0] = (byte)%s;\n", ind, slice, expr)
			case 2:
				g.pf("%sBinaryPrimitives.WriteUInt16LittleEndian(%s, (ushort)%s);\n", ind, slice, expr)
			case 4:
				g.pf("%sBinaryPrimitives.WriteUInt32LittleEndian(%s, (uint)%s);\n", ind, slice, expr)
			}
			return
		case *ir.Flags:
			g.pf("%sBinaryPrimitives.WriteUInt64LittleEndian(%s, (ulong)%s);\n", ind, slice, expr)
			return
		}
	}
	w := fixedStorageBytes(f.Type)
	switch f.Type.Kind {
	case ir.TBool:
		g.pf("%s%s[0] = (byte)(%s ? 1 : 0);\n", ind, slice, expr)
	case ir.TFloat32:
		g.pf("%sBinaryPrimitives.WriteSingleLittleEndian(%s, %s);\n", ind, slice, expr)
	case ir.TFloat64:
		g.pf("%sBinaryPrimitives.WriteDoubleLittleEndian(%s, %s);\n", ind, slice, expr)
	case ir.TInt:
		switch w {
		case 1:
			g.pf("%s%s[0] = unchecked((byte)%s);\n", ind, slice, expr)
		case 2:
			g.pf("%sBinaryPrimitives.WriteInt16LittleEndian(%s, (short)%s);\n", ind, slice, expr)
		case 4:
			g.pf("%sBinaryPrimitives.WriteInt32LittleEndian(%s, (int)%s);\n", ind, slice, expr)
		case 8:
			g.pf("%sBinaryPrimitives.WriteInt64LittleEndian(%s, (long)%s);\n", ind, slice, expr)
		case 16:
			if f.Type.Signed {
				g.pf("%sBinaryPrimitives.WriteUInt64LittleEndian(%s, (ulong)(unchecked((UInt128)(Int128)%s)));\n", ind, slice, expr)
				g.pf("%sBinaryPrimitives.WriteUInt64LittleEndian(%s.Slice(8), (ulong)(unchecked((UInt128)(Int128)%s) >> 64));\n", ind, slice, expr)
			} else {
				g.pf("%sBinaryPrimitives.WriteUInt64LittleEndian(%s, (ulong)(unchecked((UInt128)%s)));\n", ind, slice, expr)
				g.pf("%sBinaryPrimitives.WriteUInt64LittleEndian(%s.Slice(8), (ulong)(unchecked((UInt128)%s) >> 64));\n", ind, slice, expr)
			}
		}
	default:
		switch w {
		case 1:
			g.pf("%s%s[0] = (byte)%s;\n", ind, slice, expr)
		case 2:
			g.pf("%sBinaryPrimitives.WriteUInt16LittleEndian(%s, (ushort)%s);\n", ind, slice, expr)
		case 4:
			g.pf("%sBinaryPrimitives.WriteUInt32LittleEndian(%s, (uint)%s);\n", ind, slice, expr)
		case 8:
			g.pf("%sBinaryPrimitives.WriteUInt64LittleEndian(%s, (ulong)%s);\n", ind, slice, expr)
		case 16:
			if f.Type.Signed {
				g.pf("%sBinaryPrimitives.WriteUInt64LittleEndian(%s, (ulong)(unchecked((UInt128)(Int128)%s)));\n", ind, slice, expr)
				g.pf("%sBinaryPrimitives.WriteUInt64LittleEndian(%s.Slice(8), (ulong)(unchecked((UInt128)(Int128)%s) >> 64));\n", ind, slice, expr)
			} else {
				g.pf("%sBinaryPrimitives.WriteUInt64LittleEndian(%s, (ulong)(unchecked((UInt128)%s)));\n", ind, slice, expr)
				g.pf("%sBinaryPrimitives.WriteUInt64LittleEndian(%s.Slice(8), (ulong)(unchecked((UInt128)%s) >> 64));\n", ind, slice, expr)
			}
		}
	}
}

func (g *tableGen) emitFixedRoot(st *ir.Struct) {
	b := &fixedRootBuild{g: g}
	b.entries = append(b.entries, fixedBlockEntry{
		id:       ir.TableWireId(st.WireName()),
		kind:     ir.TableKindTable,
		size:     fixedTypeBytes(st),
		children: len(st.Fields),
		note:     st.Name,
	})
	b.dstRows = append(b.dstRows, "new TableFixedDst(0, 0, 0, 0, 0)")

	wireOff := int64(0)
	for _, f := range st.Fields {
		b.buildField(st, f, "t", 0, &wireOff, kFixedNoGuard, 0)
	}

	block := fixedBlockBytes(b.entries)
	hash := fixedBlockHash(block)
	body := fixedTypeBytes(st)
	name := st.Name

	g.pf("// ---- %s, the fixed form ----\n\n", name)
	g.pf("public const long %sFixedBodyBytes = %d;\n", name, body)
	g.pf("public const long %sFixedRecordBytes = 8 + %sFixedBodyBytes;\n", name, name)
	g.pf("public const ulong %sFixedHash = 0x%016xul;\n\n", name, hash)

	g.pf("public static readonly byte[] %sFixedLayout = new byte[] {\n", name)
	g.emitCsByteArray(block)
	g.pf("};\n")
	g.pf("public const long %sFixedLayoutBytes = %d;\n\n", name, len(block))

	g.pf("public static readonly TableFixedDst[] %sFixedDst = new TableFixedDst[] {\n", name)
	for i, row := range b.dstRows {
		g.pf("    %s, // %s\n", row, b.entries[i].note)
	}
	g.pf("};\n\n")

	g.pf("public static readonly TableFixedSlot<%s>[] %sFixedSlots = new TableFixedSlot<%s>[] {\n", name, name, name)
	for _, s := range b.slots {
		var parts []string
		if s.setRaw != "" {
			parts = append(parts, "setRaw: "+s.setRaw)
		}
		if s.setDouble != "" {
			parts = append(parts, "setDouble: "+s.setDouble)
		}
		if s.setWide != "" {
			parts = append(parts, "setWide: "+s.setWide)
		}
		if s.setBytes != "" {
			parts = append(parts, "setBytes: "+s.setBytes)
		}
		if s.setChars != "" {
			parts = append(parts, "setChars: "+s.setChars)
		}
		if s.setRawReport != "" {
			parts = append(parts, "setRawReport: "+s.setRawReport)
		}
		g.pf("    new TableFixedSlot<%s>(%s),\n", name, strings.Join(parts, ", "))
	}
	g.pf("};\n\n")

	g.pf("public static readonly TableFixedPlan %sFixedPlan = new TableFixedPlan(new TableFixedEntry[] {\n", name)
	for _, p := range b.plan {
		guardStr := "TableFixedWire.NoGuard"
		if p.guard != kFixedNoGuard {
			guardStr = fmt.Sprintf("%du", p.guard)
		}
		opName := "TableFixedWire.Copy"
		switch p.op {
		case 1:
			opName = "TableFixedWire.Count"
		case 2:
			opName = "TableFixedWire.Text"
		case kFixedOpFlat:
			opName = "TableFixedWire.Flat"
		}
		g.pf("    new TableFixedEntry(%du, %du, %du, %du, %s, %s, %d, 0, 0, %d),\n",
			p.src, p.dst, p.size, p.aux, guardStr, opName, p.arg, p.meta)
	}
	g.pf("});\n\n")

	g.pf("public static long %sFixedMeasure(long count)\n{\n", name)
	g.pf("    return TableFixedWire.HeaderBytes + 4 + %sFixedLayoutBytes + count * %sFixedRecordBytes;\n}\n\n", name, name)

	g.pf("public static long %sFixedSave(ReadOnlySpan<%s> values, Span<byte> buffer)\n{\n", name, name)
	g.pf("    long need = %sFixedMeasure(values.Length);\n", name)
	g.pf("    if (values.Length < 0 || buffer.Length < need) { return -1; }\n")
	g.pf("    buffer.Slice(0, TableFixedWire.HeaderBytes).Clear();\n")
	g.pf("    buffer[0] = TableFixedWire.Form;\n")
	g.pf("    BinaryPrimitives.WriteUInt64LittleEndian(buffer.Slice(TableFixedWire.HashAt), %sFixedHash);\n", name)
	g.pf("    BinaryPrimitives.WriteUInt32LittleEndian(buffer.Slice(TableFixedWire.HeaderBytes), (uint)%sFixedLayoutBytes);\n", name)
	g.pf("    %sFixedLayout.CopyTo(buffer.Slice(TableFixedWire.HeaderBytes + 4));\n", name)
	g.pf("    Span<byte> at = buffer.Slice(TableFixedWire.HeaderBytes + 4 + (int)%sFixedLayoutBytes);\n", name)
	g.pf("    for (int k = 0; k < values.Length; ++k)\n    {\n")
	g.pf("        BinaryPrimitives.WriteUInt64LittleEndian(at, %sFixedHash);\n", name)
	g.pf("        at.Slice(8, (int)%sFixedBodyBytes).Clear();\n", name)
	g.pf("        %sFixedWriteBody(at.Slice(8), values[k]);\n", name)
	g.pf("        at = at.Slice((int)%sFixedRecordBytes);\n    }\n    return need;\n}\n\n", name)

	g.pf("public static long %sFixedSave(%s[] values, Span<byte> buffer)\n{\n", name, name)
	g.pf("    return %sFixedSave((ReadOnlySpan<%s>)values, buffer);\n}\n\n", name, name)

	g.pf("public static long %sFixedSave(%s value, Span<byte> buffer)\n{\n", name, name)
	g.pf("    ReadOnlySpan<%s> span = MemoryMarshal.CreateReadOnlySpan(ref value, 1);\n", name)
	g.pf("    return %sFixedSave(span, buffer);\n}\n\n", name)

	g.pf("public static long %sFixedLoad(\n", name)
	g.pf("    Span<%s> values,\n", name)
	g.pf("    ReadOnlySpan<byte> data,\n")
	g.pf("    Span<TableFixedEntry> plan,\n")
	g.pf("    TableReport report = null)\n{\n")
	g.pf("    if (data.Length < TableFixedWire.HeaderBytes + 4)\n    {\n")
	g.pf("        if (report != null) { report.Malformed = true; report.Verdict = TableWire.Verdict.Damaged; }\n")
	g.pf("        return -1;\n    }\n")
	g.pf("    if (data[0] != TableFixedWire.Form)\n    {\n")
	g.pf("        if (report != null)\n        {\n")
	g.pf("            report.Refused = true;\n")
	g.pf("            report.Reason = data[0] == 2 ? \"message_form_as_file\"\n")
	g.pf("                          : data[0] < TableFixedWire.Form ? \"previous_form\"\n")
	g.pf("                          : \"newer_form\";\n")
	g.pf("            report.Verdict = TableWire.Verdict.Refused;\n        }\n")
	g.pf("        return -1;\n    }\n")
	g.pf("    uint layout_bytes = BinaryPrimitives.ReadUInt32LittleEndian(data.Slice(TableFixedWire.HeaderBytes));\n")
	g.pf("    if ((long)layout_bytes + TableFixedWire.HeaderBytes + 4 > data.Length)\n    {\n")
	g.pf("        if (report != null) { report.Refused = true; report.Reason = \"layout_malformed\"; report.Verdict = TableWire.Verdict.Refused; }\n")
	g.pf("        return -1;\n    }\n")
	g.pf("    ReadOnlySpan<byte> layout = data.Slice(TableFixedWire.HeaderBytes + 4, (int)layout_bytes);\n")
	g.pf("    ulong hash = TableFixedWire.HashOf(layout);\n")
	g.pf("    ReadOnlySpan<byte> at = data.Slice(TableFixedWire.HeaderBytes + 4 + (int)layout_bytes);\n")
	g.pf("    int rest = data.Length - TableFixedWire.HeaderBytes - 4 - (int)layout_bytes;\n")
	g.pf("    ReadOnlySpan<TableFixedEntry> entries = %sFixedPlan;\n", name)
	g.pf("    long record_bytes = %sFixedRecordBytes;\n", name)
	g.pf("    ReadOnlySpan<byte> planBytes = ReadOnlySpan<byte>.Empty;\n")
	g.pf("    if (hash != %sFixedHash)\n    {\n", name)
	g.pf("        if (!TableFixedWire.ParseLayout(layout, out TableFixedLayoutView parsed, out string why))\n        {\n            if (report != null) { report.Refused = true; report.Reason = why; report.Verdict = TableWire.Verdict.Refused; }\n            return -1;\n        }\n")
	g.pf("        int made = TableFixedWire.Compile(parsed, %sFixedLayout, %sFixedDst, plan, report);\n", name, name)
	g.pf("        if (made < 0)\n        {\n            if (report != null) { report.Refused = true; report.Reason = \"plan_too_large\"; report.Verdict = TableWire.Verdict.Refused; }\n            return -1;\n        }\n")
	g.pf("        entries = plan.Slice(0, made);\n")
	g.pf("        record_bytes = 8 + (long)TableFixedWire.EntryAt(parsed, 0).Size;\n")
	g.pf("        planBytes = MemoryMarshal.AsBytes(plan);\n    }\n")
	g.pf("    if (BinaryPrimitives.ReadUInt64LittleEndian(data.Slice(TableFixedWire.HashAt)) != hash)\n    {\n")
	g.pf("        if (report != null) { report.Refused = true; report.Reason = \"layout_malformed\"; report.Verdict = TableWire.Verdict.Refused; }\n")
	g.pf("        return -1;\n    }\n")
	g.pf("    if (record_bytes <= 8 || rest %% record_bytes != 0)\n    {\n")
	g.pf("        if (report != null) { report.Malformed = true; report.Verdict = TableWire.Verdict.Damaged; }\n")
	g.pf("        return -1;\n    }\n")
	g.pf("    long n = rest / record_bytes;\n")
	g.pf("    if (n > values.Length)\n    {\n")
	g.pf("        if (report != null) { report.Refused = true; report.Reason = \"batch_too_large\"; report.Verdict = TableWire.Verdict.Refused; }\n")
	g.pf("        return -1;\n    }\n")
	g.pf("    for (int k = 0; k < n; ++k)\n    {\n")
	g.pf("        if (values[k] == null) { values[k] = new %s(); }\n", name)
	g.pf("        TableReset(values[k]);\n")
	g.pf("        if (BinaryPrimitives.ReadUInt64LittleEndian(at) != hash)\n        {\n")
	g.pf("            if (report != null) { report.Refused = true; report.Reason = \"no_layout\"; report.Verdict = TableWire.Verdict.Refused; }\n")
	g.pf("            return -1;\n        }\n")
	g.pf("        TableFixedWire.Run(entries, %sFixedSlots, at.Slice(8), values[k], report, planBytes);\n", name)
	g.pf("        at = at.Slice((int)record_bytes);\n    }\n")
	g.pf("    if (report != null) { report.Verdict = TableWire.Verdict.Ok; }\n    return n;\n}\n\n")

	g.pf("public static long %sFixedLoad(%s[] values, ReadOnlySpan<byte> data, Span<TableFixedEntry> plan, TableReport report = null)\n{\n", name, name)
	g.pf("    return %sFixedLoad((Span<%s>)values, data, plan, report);\n}\n\n", name, name)

	g.pf("public static long %sFixedLoad(%s value, ReadOnlySpan<byte> data, Span<TableFixedEntry> plan, TableReport report = null)\n{\n", name, name)
	g.pf("    Span<%s> span = MemoryMarshal.CreateSpan(ref value, 1);\n", name)
	g.pf("    return %sFixedLoad(span, data, plan, report);\n}\n\n", name)

	g.pf("public static void TableFixedRun(\n")
	g.pf("    ReadOnlySpan<TableFixedEntry> plan,\n")
	g.pf("    ReadOnlySpan<byte> src,\n")
	g.pf("    %s dst,\n", name)
	g.pf("    TableReport report = null,\n")
	g.pf("    ReadOnlySpan<byte> planBytes = default)\n{\n")
	g.pf("    TableFixedWire.Run(plan, %sFixedSlots, src, dst, report, planBytes);\n}\n\n", name)
}

func (g *tableGen) emitCsByteArray(b []byte) {
	for i := 0; i < len(b); i += 16 {
		end := min(i+16, len(b))
		var sb strings.Builder
		sb.WriteString("   ")
		for _, v := range b[i:end] {
			fmt.Fprintf(&sb, " 0x%02x,", v)
		}
		g.pf("%s\n", sb.String())
	}
}
