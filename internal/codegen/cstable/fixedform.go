package cstable

import (
	"encoding/binary"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const (
	fixedKindOptional = 35
	kFixedNoGuard     = int64(0xFFFFFFFF)
	fixedCountBytes   = int64(4)
	fixedPresentBytes = int64(1)
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
				if v.F == nil {
					continue // a payload-free arm names no type (§2.6)
				}
				if n := fixedFieldBytes(v.F); n > widest {
					widest = n
				}
			}
			return int64(ir.StorageBitsFor(r.Max)/8) + widest
		}
	}
	return fixedStorageBytes(f.Type)
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
				if v.F == nil {
					continue // a payload-free arm names no type (§2.6)
				}
				if s, ok := v.F.Type.Ref.(*ir.Struct); ok && v.F.Type.Kind == ir.TNamed {
					fixedCollectTypes(s, seen, order)
				}
			}
		}
	}
	*order = append(*order, st)
}

// unitFixedOrder is every type in the UNIT that needs a fixed write body, in
// dependency order: each fixed-emitted table and, post-order before it, every
// type it holds by value. The set is the unit's and not a file's because
// `Schema` is ONE partial class across a unit's files — a second definition of
// <T>FixedWriteBody is CS0111, not C++'s harmless re-inclusion behind a guard.
func (g *tableGen) unitFixedOrder() []*ir.Struct {
	closure := ir.TableClosure(g.unit)
	seen := map[string]bool{}
	var order []*ir.Struct
	add := func(st *ir.Struct) {
		if !ir.TableFixedEmitted(g.unit, st) {
			return
		}
		fixedCollectTypes(st, seen, &order)
	}
	for _, f := range g.unit.Files {
		for _, st := range f.Tables {
			add(st)
		}
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok && closure[st.Name] {
				add(st)
			}
		}
	}
	return order
}

// fileFixedBodies is the part of the unit's order THIS FILE declares, the one
// file that emits each body — the rule the enum identities already follow. A
// type a fixed table in another file reaches is that other file's business.
func (g *tableGen) fileFixedBodies() []*ir.Struct {
	if g.file == nil {
		return nil
	}
	here := map[string]bool{}
	for _, st := range g.file.Tables {
		here[st.Name] = true
	}
	for _, d := range g.file.Decls {
		if st, ok := d.(*ir.Struct); ok {
			here[st.Name] = true
		}
	}
	order := g.unitFixedOrder()
	out := make([]*ir.Struct, 0, len(order))
	for _, st := range order {
		if here[st.Name] {
			out = append(out, st)
		}
	}
	return out
}

type fixedSlot struct {
	setRaw       string
	setDouble    string
	setWide      string
	setBytes     string
	setChars     string
	setRawReport string
	reset        string
}

type fixedFillRange struct {
	off, size uint32
}

func landSlot(cover []byte, slot int) {
	if slot >= 0 && slot < len(cover) {
		cover[slot] = 1
	}
}

// identitySlotCover is every slot the identity plan lands, merged. THE PREFILL
// IS THIS SET MINUS WHAT A PLAN LANDS, so against the identity plan it is empty.
func identitySlotCover(plan []fixedPlanEntry, slotN int) []fixedFillRange {
	if slotN <= 0 {
		return nil
	}
	landed := make([]byte, slotN)
	for _, p := range plan {
		landSlot(landed, p.dst)
		if p.op == 2 {
			landSlot(landed, p.aux)
		}
	}
	var out []fixedFillRange
	i := 0
	for i < slotN {
		if landed[i] == 0 {
			i++
			continue
		}
		start := i
		for i < slotN && landed[i] != 0 {
			i++
		}
		out = append(out, fixedFillRange{uint32(start), uint32(i - start)})
	}
	return out
}

type fixedPlanEntry struct {
	src    int64
	dst    int
	size   int64
	aux    int
	guard  int64
	op     byte
	arg    uint64
	meta   byte
	argw   byte // THE GUARD'S WIDTH IN BYTES. Zero is read as one at emit.
	guard2 int64
	arg2   uint64
	argw2  byte
	// links is THE GUARD CHAIN (docs/FIXED-FORM-ALGORITHM.md §4.1), one link per
	// enclosing union, recorded INNERMOST FIRST as stampGuard runs inner unions
	// before the outer one; fixedGuardPool turns it OUTERMOST FIRST and lays a
	// chain past the two lanes into the pool as offset+count (guardIndex, a byte
	// offset into the pool, and guardCount).
	links      []fixedGuardLink
	guardIndex int
	guardCount int
}

// fixedGuardLink is ONE LINK of a guard chain: the tag's byte offset, the
// ordinal it must hold and the width it is compared at (§4.1).
type fixedGuardLink struct {
	guard int64
	arg   uint64
	argw  byte
}

// fixedGuardPool is THE GUARD CHAIN laid down (docs/FIXED-FORM-ALGORITHM.md
// §4.1): every entry whose chain runs past the two lanes gets offset+count into
// one pool of links, OUTERMOST FIRST, deduped by content so two entries under
// one chain share one slice. A chain of one or two is the two-lane special case
// and carries nothing else. The only bound is the layout's 64, which the build's
// own depth bound already holds the type to.
func fixedGuardPool(plan []fixedPlanEntry) []byte {
	var pool []byte
	index := map[string]int{}
	for i := range plan {
		links := plan[i].links
		if len(links) <= 2 {
			continue
		}
		var key strings.Builder
		var laid []byte
		for _, l := range slices.Backward(links) {
			fmt.Fprintf(&key, "%d:%d:%d;", l.guard, l.arg, l.argw)
			laid = binary.LittleEndian.AppendUint32(laid, uint32(l.guard))
			laid = binary.LittleEndian.AppendUint64(laid, l.arg)
			laid = append(laid, l.argw)
		}
		at, ok := index[key.String()]
		if !ok {
			at = len(pool)
			index[key.String()] = at
			pool = append(pool, laid...)
		}
		plan[i].guardIndex = at
		plan[i].guardCount = len(links)
	}
	return pool
}

// fixedPartitionPlan stable-partitions the built plan into every UNGUARDED
// entry first and then every guarded one, and coalesces adjacent COPY entries
// inside each half, never across the split (docs/FIXED-FORM-ALGORITHM.md §5.9
// #42). It returns the partition and the split: the number of unguarded
// entries, the index the guarded half starts at — a plan with no guarded entry
// has its split at the END, not at zero. The run loop still tests the guard per
// entry, which #42 allows outright; what a leg may not do is skip the
// partition. A field past a union otherwise lands its unguarded entry behind
// the union's guarded arms, and the coalescer would then face the two halves
// interleaved.
//
// The destination is a SLOT index, not a byte offset, so two adjacent scalar
// copies never merge: each scalar slot has its own setter and one entry cannot
// write two slots. The coalesce condition therefore demands the SAME slot and a
// contiguous source, which only a span-setter run satisfies — and a span-setter
// run is already one Flat or Text entry. The pass still exists, and its
// guard-equality and never-across-the-split checks are what §5.9 #42 pins.
func fixedPartitionPlan(plan []fixedPlanEntry) ([]fixedPlanEntry, int) {
	out := make([]fixedPlanEntry, 0, len(plan))
	split := 0
	for pass := range 2 {
		for _, e := range plan {
			if (e.guard != kFixedNoGuard) != (pass == 1) {
				continue
			}
			if len(out) > split {
				last := &out[len(out)-1]
				if last.op == e.op && e.op == 0 &&
					last.guard == e.guard && last.arg == e.arg &&
					last.argw == e.argw && last.meta == e.meta &&
					last.guard2 == e.guard2 && last.arg2 == e.arg2 && last.argw2 == e.argw2 &&
					len(last.links) == len(e.links) &&
					last.src+last.size == e.src && last.dst == e.dst {
					last.size += e.size
					continue
				}
			}
			out = append(out, e)
		}
		if pass == 0 {
			split = len(out)
		}
	}
	return out, split
}

type fixedRootBuild struct {
	g       *tableGen
	argw    byte // the CURRENT union's tag width, stamped onto every guarded push
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
				if v.F == nil {
					continue // a payload-free arm names no type (§2.6)
				}
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

func fixedGuardWidth(tagBytes int64) byte {
	if tagBytes < 1 || tagBytes > 8 {
		return 1
	}
	return byte(tagBytes)
}

func (b *fixedRootBuild) addPlan(e fixedPlanEntry) {
	if e.argw == 0 {
		w := b.argw
		if w == 0 {
			w = 1
		}
		e.argw = w
	}
	// NO SECOND CONDITION IS NoGuard, and a plan entry never sets guard2 itself:
	// stampGuard stamps it after the entry is in the plan. Its zero value would
	// otherwise read as a real outer guard at offset 0 (§4.1).
	if e.guard2 == 0 {
		e.guard2 = kFixedNoGuard
	}
	b.plan = append(b.plan, e)
}

// stampGuard stamps an arm's tag over everything the arm added since at: an
// entry with no guard yet takes the tag on the first lane, and an entry an
// inner union already guarded takes it on the second lane, so BOTH tags must
// hold (§4.1 "the entry runs when BOTH tags hold their ordinal").
func (b *fixedRootBuild) stampGuard(at int, guard int64, arg uint64, argw byte) {
	for q := at; q < len(b.plan); q++ {
		// ONE MORE LINK OF THE GUARD CHAIN (§4.1): an arm three unions deep keeps
		// all three, and the two lanes hold the innermost two, never a lane
		// written over another.
		b.plan[q].links = append(b.plan[q].links, fixedGuardLink{guard: guard, arg: arg, argw: argw})
		if b.plan[q].guard == kFixedNoGuard {
			b.plan[q].guard = guard
			b.plan[q].arg = arg
			b.plan[q].argw = argw
			continue
		}
		if b.plan[q].guard2 == kFixedNoGuard {
			b.plan[q].guard2 = guard
			b.plan[q].arg2 = arg
			b.plan[q].argw2 = argw
		}
	}
}

func (b *fixedRootBuild) scalarReset(f *ir.Field, expr string) string {
	def := fieldDefaultExpr(f)
	if enumRef(f) != nil {
		def = b.qualified(def)
	} else if _, flags := f.Type.Ref.(*ir.Flags); flags && f.Type.Kind == ir.TNamed && def != "0" {
		def = "(" + b.qualified(f.Type.Name) + ")" + f.DefInt.String() + "ul"
	}
	return fmt.Sprintf("(t) => { %s = %s; }", expr, def)
}

// qualified names a declaration of the unit's own namespace from the fixed
// form's nested one.
func (b *fixedRootBuild) qualified(name string) string {
	if b.g == nil {
		return name
	}
	return "global::" + capitalize(b.g.unit.Package) + "." + name
}

func presentReset(fieldExpr string) string {
	return fmt.Sprintf("(t) => { %sPresent = false; }", fieldExpr)
}

func countReset(fieldExpr string) string {
	return fmt.Sprintf("(t) => { %sCount = 0; }", fieldExpr)
}

func flatReset(fieldExpr string) string {
	return fmt.Sprintf("(t) => { if (%s != null) Array.Clear(%s, 0, %s.Length); }", fieldExpr, fieldExpr, fieldExpr)
}

func textLenReset(f *ir.Field, fieldExpr string) string {
	return fmt.Sprintf("(t) => { %sLength = %d; }", fieldExpr, len(f.DefBytes))
}

func bufferReset(f *ir.Field, fieldExpr string) string {
	var sb strings.Builder
	sb.WriteString("(t) => { if (")
	sb.WriteString(fieldExpr)
	sb.WriteString(" != null) { Array.Clear(")
	sb.WriteString(fieldExpr)
	sb.WriteString(", 0, ")
	sb.WriteString(fieldExpr)
	sb.WriteString(".Length);")
	for i, by := range f.DefBytes {
		fmt.Fprintf(&sb, " %s[%d] = 0x%02x;", fieldExpr, i, by)
	}
	sb.WriteString(" } }")
	return sb.String()
}

func unionTagReset(expr, tagType string) string {
	return fmt.Sprintf("(t) => { %s.Type = %s.None; }", expr, tagType)
}

func (b *fixedRootBuild) addScalarSlot(f *ir.Field, expr string) {
	reset := b.scalarReset(f, expr)
	switch f.Type.Kind {
	case ir.TBool:
		b.slots = append(b.slots, fixedSlot{
			setRaw: fmt.Sprintf("(t, v) => %s = v != 0", expr),
			reset:  reset,
		})
	case ir.TFloat32:
		b.slots = append(b.slots, fixedSlot{
			setRaw:    fmt.Sprintf("(t, v) => %s = BitConverter.UInt32BitsToSingle((uint)v)", expr),
			setDouble: fmt.Sprintf("(t, d) => %s = (float)d", expr),
			reset:     reset,
		})
	case ir.TFloat64:
		b.slots = append(b.slots, fixedSlot{
			setRaw:    fmt.Sprintf("(t, v) => %s = BitConverter.UInt64BitsToDouble(v)", expr),
			setDouble: fmt.Sprintf("(t, d) => %s = d", expr),
			reset:     reset,
		})
	case ir.TInt:
		typ := csFieldType(f.Type)
		if f.Type.Width == 128 {
			b.slots = append(b.slots, fixedSlot{
				setWide: fmt.Sprintf("(t, w) => %s = unchecked((%s)w)", expr, typ),
				setRaw:  fmt.Sprintf("(t, v) => %s = unchecked((%s)v)", expr, typ),
				reset:   reset,
			})
		} else {
			b.slots = append(b.slots, fixedSlot{
				setRaw: fmt.Sprintf("(t, v) => %s = unchecked((%s)v)", expr, typ),
				reset:  reset,
			})
		}
	default:
		typ := csFieldType(f.Type)
		if f.Type.Width == 128 {
			b.slots = append(b.slots, fixedSlot{
				setWide: fmt.Sprintf("(t, w) => %s = unchecked((%s)w)", expr, typ),
				setRaw:  fmt.Sprintf("(t, v) => %s = unchecked((%s)v)", expr, typ),
				reset:   reset,
			})
		} else {
			b.slots = append(b.slots, fixedSlot{
				setRaw: fmt.Sprintf("(t, v) => %s = (%s)v", expr, typ),
				reset:  reset,
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
				if v.F == nil {
					b.pushNoPayloadArm(v)
					continue
				}
				// AN ARM IS A FIELD LINE (§2.6): an array, string or bytes arm
				// is laid out the way the same line in a struct is, under the
				// arm's id (ir.tableFixedWalkArm, the reference's layout).
				b.pushPayloadBlockOnly(v.F, r.Name, ir.TableWireId(v.WireName()), fixedFieldBytes(v.F))
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

// pushNoPayloadArm is a payload-free arm's layout entry: an entry and no bytes
// (kind 32), the tag alone is its whole storage (§2.6). It is an entry rather
// than nothing because the layout is what the arm list is compared by, so an
// arm appended after a payload-free one must move the hash
// (ir.tableFixedWalkArm).
func (b *fixedRootBuild) pushNoPayloadArm(v ir.UnionVariant) {
	b.entries = append(b.entries, fixedBlockEntry{
		id:       ir.TableWireId(v.WireName()),
		kind:     ir.TableKindNoPayload,
		size:     0,
		children: 0,
		note:     v.Name,
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

func (b *fixedRootBuild) buildField(owner *ir.Struct, f *ir.Field, expr string, baseSlot int, wireOff *int64, guard int64, arg uint64) {
	prop := member(f)
	fieldExpr := expr + "." + prop
	id := ir.TableFieldWireId(f)
	size := fixedFieldBytes(f)
	fieldWireBase := *wireOff

	if f.Type.Optional {
		presentSlot := len(b.slots)
		b.slots = append(b.slots, fixedSlot{
			setRaw: fmt.Sprintf("(t, v) => %sPresent = v != 0", fieldExpr),
			reset:  presentReset(fieldExpr),
		})
		b.entries = append(b.entries, fixedBlockEntry{
			id:       id,
			kind:     fixedKindOptional,
			size:     size,
			children: 1,
			note:     f.Name + " ?",
		})
		b.dstRows = append(b.dstRows, fmt.Sprintf("new TableFixedDst(0, 0, %d, 0, 0)", presentSlot-baseSlot))
		b.addPlan(fixedPlanEntry{
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

func (b *fixedRootBuild) buildPayload(owner *ir.Struct, f *ir.Field, expr string, baseSlot int, wireOff *int64, id uint64, size int64, guard int64, arg uint64) {
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
		elemWireSize := fixedElementBytes(f)
		for k := 0; k < int(f.KeyEnumRef.Max); k++ {
			slotExpr := fmt.Sprintf("%s[%d]", fieldExpr, k)
			if owner != nil && owner.IsTable {
				slotExpr = fmt.Sprintf("%s.Slots[%d]", fieldExpr, k)
			}
			elemWire := fieldWireBase + int64(k)*elemWireSize
			*wireOff = elemWire
			if k == 0 {
				b.buildElement(f, slotExpr, elemBaseSlot, 0, "element", guard, arg, wireOff)
			} else {
				b.buildElementSlotsAndPlan(f, slotExpr, elemBaseSlot+k*elemSlots, guard, arg, wireOff)
			}
		}
		*wireOff = fieldWireBase + size

	case f.Array == ir.ArrayCounted:
		countSlot := len(b.slots)
		b.slots = append(b.slots, fixedSlot{
			setRaw: fmt.Sprintf("(t, v) => %sCount = (int)v", fieldExpr),
			reset:  countReset(fieldExpr),
		})
		b.addPlan(fixedPlanEntry{
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
				reset:    flatReset(fieldExpr),
			})
			b.addPlan(fixedPlanEntry{
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
			for i := 0; i < int(f.ArrayBound); i++ {
				elemExpr := fmt.Sprintf("%s[%d]", fieldExpr, i)
				elemWire := fieldWireBase + fixedCountBytes + int64(i)*elemWireSize
				*wireOff = elemWire
				if i == 0 {
					b.buildElement(f, elemExpr, elemBaseSlot, 0, "element", guard, arg, wireOff)
				} else {
					b.buildElementSlotsAndPlan(f, elemExpr, elemBaseSlot+i*elemSlots, guard, arg, wireOff)
				}
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
				reset:    flatReset(fieldExpr),
			})
			b.addPlan(fixedPlanEntry{
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
			for i := 0; i < int(f.ArrayBound); i++ {
				elemExpr := fmt.Sprintf("%s[%d]", fieldExpr, i)
				elemWire := fieldWireBase + int64(i)*elemWireSize
				*wireOff = elemWire
				if i == 0 {
					b.buildElement(f, elemExpr, elemBaseSlot, 0, "element", guard, arg, wireOff)
				} else {
					b.buildElementSlotsAndPlan(f, elemExpr, elemBaseSlot+i*elemSlots, guard, arg, wireOff)
				}
			}
		}
		*wireOff = fieldWireBase + size

	case f.Type.Kind == ir.TBytes:
		lenSlot := len(b.slots)
		b.slots = append(b.slots, fixedSlot{
			setRaw: fmt.Sprintf("(t, v) => %sLength = (int)v", fieldExpr),
			reset:  textLenReset(f, fieldExpr),
		})
		bufSlot := len(b.slots)
		b.slots = append(b.slots, fixedSlot{
			setBytes: fmt.Sprintf("(t, b, l) => { if (%s != null) b.Slice(0, Math.Min(b.Length, %s.Length)).CopyTo(%s); }", fieldExpr, fieldExpr, fieldExpr),
			reset:    bufferReset(f, fieldExpr),
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
		b.addPlan(fixedPlanEntry{
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
			reset:  textLenReset(f, fieldExpr),
		})
		bufSlot := len(b.slots)
		b.slots = append(b.slots, fixedSlot{
			setBytes: fmt.Sprintf("(t, b, l) => { if (%s != null) b.Slice(0, Math.Min(b.Length, %s.Length)).CopyTo(%s); }", fieldExpr, fieldExpr, fieldExpr),
			reset:    bufferReset(f, fieldExpr),
		})
		b.entries = append(b.entries, fixedBlockEntry{
			id:       id,
			kind:     ir.TableKindString,
			size:     size,
			children: 0,
			note:     f.Name,
		})
		b.dstRows = append(b.dstRows, fmt.Sprintf("new TableFixedDst(%d, 0, %d, 0, 1)", lenSlot-baseSlot, bufSlot-baseSlot))
		b.addPlan(fixedPlanEntry{
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
			reset:  textLenReset(f, fieldExpr),
		})
		bufSlot := len(b.slots)
		b.slots = append(b.slots, fixedSlot{
			setChars: fmt.Sprintf("(t, b, l) => { ReadOnlySpan<char> c = MemoryMarshal.Cast<byte, char>(b); if (%s != null) c.Slice(0, Math.Min(c.Length, %s.Length)).CopyTo(%s); }", fieldExpr, fieldExpr, fieldExpr),
			reset:    bufferReset(f, fieldExpr),
		})
		b.entries = append(b.entries, fixedBlockEntry{
			id:       id,
			kind:     ir.TableKindWstring,
			size:     size,
			children: 0,
			note:     f.Name,
		})
		b.dstRows = append(b.dstRows, fmt.Sprintf("new TableFixedDst(%d, 0, %d, 0, 2)", lenSlot-baseSlot, bufSlot-baseSlot))
		b.addPlan(fixedPlanEntry{
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

func (b *fixedRootBuild) buildElement(f *ir.Field, expr string, baseSlot int, id uint64, note string, guard int64, arg uint64, wireOff *int64) {
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
				// THE TAG LANDS RAW AND MOVES NOTHING (§5.4: `copy`/`const`
				// land their value and move nothing). The clamp past the arm
				// count — and its count — are the BOUNDS PASS's, which runs over
				// storage after the run (§5.4, §4.6).
				setRawReport: fmt.Sprintf("(t, v, rep) => { %s.Type = (%s)v; }", expr, tagType),
				reset:        unionTagReset(expr, tagType),
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
			b.addPlan(fixedPlanEntry{
				src:   wireBase,
				dst:   tagSlot,
				size:  tagBytes,
				guard: guard,
				op:    0,
				arg:   arg,
			})
			savedArgw := b.argw
			b.argw = fixedGuardWidth(tagBytes)
			armWireOffset := wireBase + tagBytes
			owner := unionOwner(r)
			for j, v := range r.Variants {
				*wireOff = armWireOffset
				if v.F == nil {
					b.pushNoPayloadArm(v)
					continue
				}
				at := len(b.plan)
				// AN ARM IS A FIELD LINE (§2.6): its value and companion
				// (Length, Count) are members of the union object, so the arm
				// walks as a payload, never as a bare element.
				b.buildPayload(owner, v.F, expr, armBase, wireOff, ir.TableWireId(v.WireName()), fixedFieldBytes(v.F), kFixedNoGuard, 0)
				b.stampGuard(at, wireBase, uint64(j+1), b.argw)
			}
			b.argw = savedArgw
			*wireOff = wireBase + elemSize
			return

		case *ir.Enum:
			slot := len(b.slots)
			w := int64(r.StorageBits / 8)
			b.slots = append(b.slots, fixedSlot{
				// THE ORDINAL LANDS RAW AND MOVES NOTHING (§5.4: `copy`/
				// `ordinal` land their value and move nothing). A forged ordinal
				// past the top variant is the BOUNDS PASS's to clamp and count,
				// on BOTH plans (§5.4, §4.6).
				setRawReport: fmt.Sprintf("(t, v, rep) => { %s = (%s)v; }", expr, f.Type.Name),
				reset:        b.scalarReset(f, expr),
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
			b.addPlan(fixedPlanEntry{
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
				setRaw: fmt.Sprintf("(t, v) => %s = (%s)v", expr, b.qualified(f.Type.Name)),
				reset:  b.scalarReset(f, expr),
			})
			b.entries = append(b.entries, fixedBlockEntry{
				id:       id,
				kind:     ir.TableWireScalarKind(f),
				size:     8,
				children: 0,
				note:     note,
			})
			b.dstRows = append(b.dstRows, fmt.Sprintf("new TableFixedDst(%d, 0, 0, 0, 0)", slot-baseSlot))
			b.addPlan(fixedPlanEntry{
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
	b.addPlan(fixedPlanEntry{
		src:   wireBase,
		dst:   slot,
		size:  w,
		guard: guard,
		op:    0,
		arg:   arg,
	})
	*wireOff += w
}

func (b *fixedRootBuild) buildElementSlotsAndPlan(f *ir.Field, expr string, baseSlot int, guard int64, arg uint64, wireOff *int64) {
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
				// THE TAG LANDS RAW AND MOVES NOTHING (§5.4: `copy`/`const`
				// land their value and move nothing). The clamp past the arm
				// count — and its count — are the BOUNDS PASS's, which runs over
				// storage after the run (§5.4, §4.6).
				setRawReport: fmt.Sprintf("(t, v, rep) => { %s.Type = (%s)v; }", expr, tagType),
				reset:        unionTagReset(expr, tagType),
			})
			b.addPlan(fixedPlanEntry{
				src:   wireBase,
				dst:   tagSlot,
				size:  tagBytes,
				guard: guard,
				op:    0,
				arg:   arg,
			})
			savedArgw := b.argw
			b.argw = fixedGuardWidth(tagBytes)
			armWireOffset := wireBase + tagBytes
			owner := unionOwner(r)
			for j, v := range r.Variants {
				if v.F == nil {
					continue // a payload-free arm: no slot, no plan entry (§2.6)
				}
				*wireOff = armWireOffset
				at := len(b.plan)
				b.buildPayloadSlotsAndPlan(owner, v.F, expr, baseSlot, wireOff, fixedFieldBytes(v.F), kFixedNoGuard, 0)
				b.stampGuard(at, wireBase, uint64(j+1), b.argw)
			}
			b.argw = savedArgw
			*wireOff = wireBase + elemSize
			return
		case *ir.Enum:
			slot := len(b.slots)
			w := int64(r.StorageBits / 8)
			b.slots = append(b.slots, fixedSlot{
				// THE ORDINAL LANDS RAW AND MOVES NOTHING (§5.4: `copy`/
				// `ordinal` land their value and move nothing). A forged ordinal
				// past the top variant is the BOUNDS PASS's to clamp and count,
				// on BOTH plans (§5.4, §4.6).
				setRawReport: fmt.Sprintf("(t, v, rep) => { %s = (%s)v; }", expr, f.Type.Name),
				reset:        b.scalarReset(f, expr),
			})
			b.addPlan(fixedPlanEntry{
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
				setRaw: fmt.Sprintf("(t, v) => %s = (%s)v", expr, b.qualified(f.Type.Name)),
				reset:  b.scalarReset(f, expr),
			})
			b.addPlan(fixedPlanEntry{
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
	b.addPlan(fixedPlanEntry{
		src:   wireBase,
		dst:   slot,
		size:  w,
		guard: guard,
		op:    0,
		arg:   arg,
	})
	*wireOff += w
}

func (b *fixedRootBuild) buildFieldSlotsAndPlan(owner *ir.Struct, f *ir.Field, expr string, baseSlot int, wireOff *int64, guard int64, arg uint64) {
	prop := member(f)
	fieldExpr := expr + "." + prop
	fieldWireBase := *wireOff
	size := fixedFieldBytes(f)

	if f.Type.Optional {
		presentSlot := len(b.slots)
		b.slots = append(b.slots, fixedSlot{
			setRaw: fmt.Sprintf("(t, v) => %sPresent = v != 0", fieldExpr),
			reset:  presentReset(fieldExpr),
		})
		b.addPlan(fixedPlanEntry{
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

func (b *fixedRootBuild) buildPayloadSlotsAndPlan(owner *ir.Struct, f *ir.Field, expr string, baseSlot int, wireOff *int64, size int64, guard int64, arg uint64) {
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
			reset:  countReset(fieldExpr),
		})
		b.addPlan(fixedPlanEntry{
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
				reset:    flatReset(fieldExpr),
			})
			b.addPlan(fixedPlanEntry{
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
				reset:    flatReset(fieldExpr),
			})
			b.addPlan(fixedPlanEntry{
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
			reset:  textLenReset(f, fieldExpr),
		})
		bufSlot := len(b.slots)
		b.slots = append(b.slots, fixedSlot{
			setBytes: fmt.Sprintf("(t, b, l) => { if (%s != null) b.Slice(0, Math.Min(b.Length, %s.Length)).CopyTo(%s); }", fieldExpr, fieldExpr, fieldExpr),
			reset:    bufferReset(f, fieldExpr),
		})
		b.addPlan(fixedPlanEntry{
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
			reset:  textLenReset(f, fieldExpr),
		})
		bufSlot := len(b.slots)
		b.slots = append(b.slots, fixedSlot{
			setBytes: fmt.Sprintf("(t, b, l) => { if (%s != null) b.Slice(0, Math.Min(b.Length, %s.Length)).CopyTo(%s); }", fieldExpr, fieldExpr, fieldExpr),
			reset:    bufferReset(f, fieldExpr),
		})
		b.addPlan(fixedPlanEntry{
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
			reset:  textLenReset(f, fieldExpr),
		})
		bufSlot := len(b.slots)
		b.slots = append(b.slots, fixedSlot{
			setChars: fmt.Sprintf("(t, b, l) => { ReadOnlySpan<char> c = MemoryMarshal.Cast<byte, char>(b); if (%s != null) c.Slice(0, Math.Min(c.Length, %s.Length)).CopyTo(%s); }", fieldExpr, fieldExpr, fieldExpr),
			reset:    bufferReset(f, fieldExpr),
		})
		b.addPlan(fixedPlanEntry{
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
	bodies := g.fileFixedBodies()
	if len(roots) == 0 && len(bodies) == 0 {
		return
	}
	g.pf("// ---- THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4) ----\n")
	g.pf("//\n")
	g.pf("// A record is an eight-byte hash of the writer's vocabulary block and then\n")
	g.pf("// the values in declared order, every field at its declared storage width.\n")
	g.pf("// The writer is the constant bytes memcpy'd and then stores; the reader is\n")
	g.pf("// ONE loop over ONE plan, the identity plan here and a plan compiled from\n")
	g.pf("// the writer's own block for anybody else.\n\n")

	for _, st := range bodies {
		g.emitFixedWriteBody(st)
	}
	// THE BOUNDS PASS, one body per type and the root's entry point beside its
	// load (§4.6, §5.3 step 11). The body set is the unit's order, so a type
	// reached twice in a closure is spelled once.
	for _, st := range bodies {
		g.emitFixedClampBodyFn(st)
	}
	// THE FINITE PASS, in the same order (docs/SPEC-TABLES.md §3.3): the read
	// runs it before the bounds pass and the writer before its first byte.
	for _, st := range bodies {
		g.emitFixedFiniteBodyFn(st)
	}
	for _, st := range roots {
		g.emitFixedClamp(st)
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
	prop := member(f)
	if f.Type.Optional {
		// AN ABSENT OPTIONAL'S PAYLOAD IS THE TEMPLATE'S ZEROS
		// (docs/SPEC-TABLES.md §3.4): the payload rides WHOLE whether or not it
		// is present, and when the flag is 0 what rides is zero. It is ONE `if`
		// here rather than a rule anywhere else, because the template already
		// put the zeros there — so an absent optional costs the writer the
		// branch and not one store, and a caller's untouched payload storage
		// never reaches the wire.
		g.pf("%s%s[%d] = (byte)(%s.%sPresent ? 1 : 0);\n", ind, buf, off, val, prop)
		g.pf("%sif (%s.%sPresent)\n%s{\n", ind, val, prop, ind)
		g.emitFixedWritePayload(owner, f, off+fixedPresentBytes, buf, val, indent+4)
		g.pf("%s}\n", ind)
		return
	}
	g.emitFixedWritePayload(owner, f, off, buf, val, indent)
}

func (g *tableGen) emitFixedWritePayload(owner *ir.Struct, f *ir.Field, base int64, buf, val string, indent int) {
	g.emitFixedWritePayloadAs(owner, f, base, buf, val, f.Name, indent)
}

// emitFixedWritePayloadAs is [emitFixedWritePayload] with the stem its locals
// (count_<stem>, len_<stem>) take. A struct's own fields use the field name; a
// union arm, written inline inside an enclosing body, uses <Union>_<arm>, so an
// arm's local never collides with a field of the same name in the body it rides
// in (C# refuses a local that shadows one in an enclosing scope, CS0136).
func (g *tableGen) emitFixedWritePayloadAs(owner *ir.Struct, f *ir.Field, base int64, buf, val, stem string, indent int) {
	ind := strings.Repeat(" ", indent)
	prop := member(f)
	switch {
	case f.KeyEnum != "":
		g.emitFixedWriteKeyedLoop(owner, f, base, f.KeyEnumRef.Max, buf, val+"."+prop, indent)
	case f.Array == ir.ArrayFixed:
		g.emitFixedWriteLoop(f, base, f.ArrayBound, "", buf, val+"."+prop, indent)
	case f.Array == ir.ArrayCounted:
		// THE COUNT IS THE LOOP AND THE SLACK STAYS THE TEMPLATE'S ZEROS
		// (docs/SPEC-TABLES.md §3.4). Writing all Max elements put the unused
		// slots' STORAGE on the wire.
		g.pf("%sint count_%s = %s.%sCount;\n", ind, stem, val, prop)
		g.pf("%sSystem.Diagnostics.Debug.Assert(count_%s >= 0 && count_%s <= %d); // the declared count is the bound (§3.4)\n", ind, stem, stem, f.ArrayBound)
		g.pf("%sBinaryPrimitives.WriteInt32LittleEndian(%s.Slice(%d), count_%s);\n", ind, buf, base, stem)
		g.emitFixedWriteLoop(f, base+fixedCountBytes, f.ArrayBound, "count_"+stem, buf, val+"."+prop, indent)
	case f.Type.Kind == ir.TString:
		g.pf("%sint len_%s = %s.%s != null ? %s.%sLength : 0;\n", ind, stem, val, prop, val, prop)
		g.pf("%sSystem.Diagnostics.Debug.Assert(len_%s <= %d);\n", ind, stem, f.Type.Size)
		g.pf("%sBinaryPrimitives.WriteInt32LittleEndian(%s.Slice(%d), len_%s);\n", ind, buf, base, stem)
		g.pf("%sif (len_%s > 0) { %s.%s.AsSpan(0, len_%s).CopyTo(%s.Slice(%d)); }\n", ind, stem, val, prop, stem, buf, base+4)
	case f.Type.Kind == ir.TWString:
		g.pf("%sint len_%s = %s.%s != null ? %s.%sLength : 0;\n", ind, stem, val, prop, val, prop)
		g.pf("%sSystem.Diagnostics.Debug.Assert(len_%s <= %d);\n", ind, stem, f.Type.Size)
		g.pf("%sBinaryPrimitives.WriteInt32LittleEndian(%s.Slice(%d), len_%s);\n", ind, buf, base, stem)
		g.pf("%sif (len_%s > 0) { MemoryMarshal.AsBytes(%s.%s.AsSpan(0, len_%s)).CopyTo(%s.Slice(%d)); }\n", ind, stem, val, prop, stem, buf, base+4)
	case f.Type.Kind == ir.TBytes:
		g.pf("%sint len_%s = %s.%s != null ? %s.%sLength : 0;\n", ind, stem, val, prop, val, prop)
		g.pf("%sSystem.Diagnostics.Debug.Assert(len_%s <= %d);\n", ind, stem, f.Type.Size)
		g.pf("%sBinaryPrimitives.WriteInt32LittleEndian(%s.Slice(%d), len_%s);\n", ind, buf, base, stem)
		g.pf("%sif (len_%s > 0) { %s.%s.AsSpan(0, len_%s).CopyTo(%s.Slice(%d)); }\n", ind, stem, val, prop, stem, buf, base+4)
	default:
		g.emitFixedWriteElement(f, base, buf, val+"."+prop, indent)
	}
}

// fixedLoopVar names a fixed-form element loop's counter at the current
// nesting, and enterLoop/leaveLoop bracket the loop's body. A loop nests when an
// array arm rides inside an array of unions, and reusing `i` would shadow the
// outer counter, which C# refuses (CS0136). The outermost loop keeps `i`, so a
// body with no nesting is spelled as it always was.
func (g *tableGen) fixedLoopVar() string {
	if g.loopDepth == 0 {
		return "i"
	}
	return fmt.Sprintf("i%d", g.loopDepth)
}

func (g *tableGen) enterLoop() string {
	v := g.fixedLoopVar()
	g.loopDepth++
	return v
}

func (g *tableGen) leaveLoop() { g.loopDepth-- }

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
	i := g.enterLoop()
	g.pf("%sif (%s != null)\n%s{\n", ind, slotsExpr, ind)
	g.pf("%s    for (int %s = 0; %s < %d && %s < %s.Length; ++%s)\n%s    {\n", ind, i, i, count, i, slotsExpr, i, ind)
	g.emitFixedWriteElement(f, 0, fmt.Sprintf("%s.Slice(%d + %s * %d)", buf, base, i, elem), fmt.Sprintf("%s[%s]", slotsExpr, i), indent+8)
	g.pf("%s    }\n%s}\n", ind, ind)
	g.leaveLoop()
}

func (g *tableGen) emitFixedWriteLoop(f *ir.Field, base, bound int64, live, buf, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	elem := fixedElementBytes(f)
	nExpr := fmt.Sprintf("%d", bound)
	if live != "" {
		nExpr = live
	}
	if g.fixedFlatElem(f) {
		elemType := csFieldType(f.Type)
		slice := fmt.Sprintf("%s.Slice(%d)", buf, base)
		if base == 0 {
			slice = buf
		}
		if elemType == "byte" {
			g.pf("%sif (%s != null) { %s.AsSpan(0, Math.Min(%s.Length, %s)).CopyTo(%s); }\n", ind, expr, expr, expr, nExpr, slice)
		} else {
			g.pf("%sif (%s != null) { MemoryMarshal.AsBytes(%s.AsSpan(0, Math.Min(%s.Length, %s))).CopyTo(%s); }\n", ind, expr, expr, expr, nExpr, slice)
		}
		return
	}
	i := g.enterLoop()
	g.pf("%sif (%s != null)\n%s{\n", ind, expr, ind)
	g.pf("%s    for (int %s = 0; %s < %s && %s < %s.Length; ++%s)\n%s    {\n", ind, i, i, nExpr, i, expr, i, ind)
	g.emitFixedWriteElement(f, 0, fmt.Sprintf("%s.Slice(%d + %s * %d)", buf, base, i, elem), fmt.Sprintf("%s[%s]", expr, i), indent+8)
	g.pf("%s    }\n%s}\n", ind, ind)
	g.leaveLoop()
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
			owner := unionOwner(r)
			for _, v := range r.Variants {
				armProp := ir.GoExportName(v.Name)
				g.pf("%s        case %s.%s:\n", ind, tagType, armProp)
				if v.F != nil {
					// AN ARM IS A FIELD LINE (§2.6): an array arm writes its
					// count and its elements, a text or bytes arm its used
					// length and its units, from the union object's members.
					// A payload-free arm's tag is its whole storage: the case
					// carries no store, only the break.
					g.emitFixedWritePayloadAs(owner, v.F, tagBytes, slice, expr, f.Type.Name+"_"+v.F.Name, indent+12)
				}
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
	b := &fixedRootBuild{g: g, argw: 1}
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
	// THE PLAN IS PARTITIONED, unguarded entries first (§5.9 #42): the walk
	// lays entries down in field order, so a field past a union would land its
	// unguarded entry behind the union's guarded arms. The split is the number
	// of unguarded entries, carried beside the plan as metadata and never
	// consulted by the run loop, which tests the guard per entry (allowed).
	plan, planSplit := fixedPartitionPlan(b.plan)
	guardPool := fixedGuardPool(plan)
	b.plan = plan

	block := fixedBlockBytes(b.entries)
	hash := ir.TableFixedLayoutHash(block, st)
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
		if s.reset != "" {
			parts = append(parts, "reset: "+s.reset)
		}
		g.pf("    new TableFixedSlot<%s>(%s),\n", name, strings.Join(parts, ", "))
	}
	g.pf("};\n\n")
	g.emitFixedRunHelpers(name, b.slots)

	cover := identitySlotCover(b.plan, len(b.slots))
	g.pf("// THE TYPE'S VALUE SLOTS: every slot the identity plan lands, sorted and\n")
	g.pf("// merged. THE PREFILL IS THIS SET MINUS WHAT A PLAN LANDS, so against the\n")
	g.pf("// identity plan it is empty and the identity read writes no slot twice.\n")
	g.pf("public static readonly TableFixedFill[] %sFixedCover = new TableFixedFill[] {\n", name)
	for _, r := range cover {
		g.pf("    new TableFixedFill(%du, %du),\n", r.off, r.size)
	}
	g.pf("};\n\n")

	// THE SPLIT (§5.9 #42): the number of unguarded entries, the index the
	// guarded half starts at. It is metadata beside the plan, exactly the shape
	// the C++ reference carries as FixedPlanGuarded: the run loop does not read
	// it, but the partition it names is what a plan-execution test holds this
	// leg to.
	g.pf("public const int %sFixedPlanSplit = %d;\n\n", name, planSplit)
	// THE GUARD CHAIN'S POOL (docs/FIXED-FORM-ALGORITHM.md §4.1): the links of
	// every chain past the two lanes, OUTERMOST FIRST, thirteen bytes a link. An
	// entry carries offset+count into it; the identity read hands it to the run
	// loop as the plan's bytes.
	g.pf("public static readonly byte[] %sFixedGuardPool = new byte[] {", name)
	for i, by := range guardPool {
		if i%16 == 0 {
			g.pf("\n   ")
		}
		g.pf(" 0x%02x,", by)
	}
	g.pf("\n};\n\n")
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
		argw := p.argw
		if argw == 0 {
			argw = 1
		}
		if p.guard2 == kFixedNoGuard && p.guardCount == 0 {
			g.pf("    new TableFixedEntry(%du, %du, %du, %du, %s, %s, %d, 0, 0, %d, %d),\n",
				p.src, p.dst, p.size, p.aux, guardStr, opName, p.arg, p.meta, argw)
			continue
		}
		// AN ARM INSIDE AN ARM ANSWERS TO THE OUTER TAG TOO (§4.1): the entry
		// carries the second condition on the Guard2/Arg2/ArgW2 lanes, full
		// width and in their own lanes, never over Arg.
		argw2 := p.argw2
		if argw2 == 0 {
			argw2 = 1
		}
		if p.guardCount == 0 {
			g.pf("    new TableFixedEntry(%du, %du, %du, %du, %s, %s, %d, 0, 0, %d, %d, %du, %d, %d),\n",
				p.src, p.dst, p.size, p.aux, guardStr, opName, p.arg, p.meta, argw, p.guard2, p.arg2, argw2)
			continue
		}
		// AND A CHAIN PAST THE TWO LANES (§4.1): offset+count into the pool.
		g.pf("    new TableFixedEntry(%du, %du, %du, %du, %s, %s, %d, 0, 0, %d, %d, %du, %d, %d, %du, %du),\n",
			p.src, p.dst, p.size, p.aux, guardStr, opName, p.arg, p.meta, argw, p.guard2, p.arg2, argw2, p.guardIndex, p.guardCount)
	}
	g.pf("});\n\n")

	// THE LINEAGE, OLDEST FIRST, THE CURRENT LAYOUT LAST (§5.2). It is the
	// build's data and never the wire's: a file is matched on its hash against
	// these entries, and the layout it carries is COMPARED with the bytes the
	// lock recorded, never walked. The floor is 1 + the highest retired index.
	entries, floor := g.fixedLineage(st, FixedLineageEntry{
		Wire: hash, Layout: block, Digest: ir.TableFixedDefinitionsDigest(st), Record: 8 + body,
	})
	for i, e := range entries {
		if e.Retired {
			g.pf("// RETIRED: %s\n", e.Reason)
		}
		g.pf("private static readonly byte[] %sFixedLayout%d = new byte[] {\n", name, i)
		g.emitCsByteArray(e.Layout)
		g.pf("};\n")
	}
	g.pf("public static readonly TableFixedKnownLayout[] %sFixedKnown = new TableFixedKnownLayout[] {\n", name)
	for i, e := range entries {
		g.pf("    new TableFixedKnownLayout(0x%016xul, %sFixedLayout%d, %d),\n", e.Wire, name, i, e.Record)
	}
	g.pf("};\n\n")
	g.pf("// THE FLOOR: below it a layout this build once served is RETIRED, and the\n")
	g.pf("// answer is layout_unsupported — upgrade the client — rather than\n")
	g.pf("// layout_newer, which is ship the reader (§5.2).\n")
	g.pf("public const int %sFixedFloor = %d;\n\n", name, floor)
	g.pf("// ONE PLAN PER LINEAGE ENTRY, laid down from THE LOCK'S bytes in this\n")
	g.pf("// type's static initializer: nothing compiles on the load path, and there\n")
	g.pf("// is no cache to miss (§5.2, §5.8 row 3, §5.9 #3).\n")
	g.pf("//\n")
	g.pf("// A THROW HERE WOULD POISON THIS TYPE. This field initializer runs in the\n")
	g.pf("// static constructor, and an exception out of a static constructor is\n")
	g.pf("// wrapped in a TypeInitializationException that every later touch of ANY\n")
	g.pf("// member of this class rethrows for the life of the process — the refusal\n")
	g.pf("// paths included, and a read of a file carrying this build's own layout\n")
	g.pf("// included. TableFixedWire.LineagePlans therefore does not throw: EVERY\n")
	g.pf("// ENTRY IS A LANE WITH ITS OWN REFUSAL, stored before anything can fail on\n")
	g.pf("// it, and a lock bug costs the one version it broke rather than the table.\n")
	g.pf("public static readonly TableFixedLineagePlan[] %sFixedLineagePlans =\n", name)
	g.pf("    TableFixedWire.LineagePlans(%sFixedKnown, %sFixedLayout, %sFixedDst, %sFixedHash);\n\n",
		name, name, name, name)

	// THE WRITER'S OWN BOUNDED LEAVES, per entry (ALGORITHM §4.6, bill §12.5):
	// the compiled lane's bounds pass holds the ENDPOINT's declaration and never
	// this reader's wider own, so each entry's ranged leaves ride as one clamp
	// function over this reader's value. Only entries that carry them; the
	// identity entry (last) has none, its writer being this reader.
	entryBounds := g.fixedWriterBounds(st, entries, hash)
	hasWriterBounds := entryBounds != nil
	if hasWriterBounds {
		for i, rows := range entryBounds {
			if len(rows) == 0 {
				continue
			}
			g.emitFixedWriterClamp(name, i, rows)
		}
		g.pf("// THE WRITER'S OWN BOUNDS per lineage entry, as one delegate array the\n")
		g.pf("// load indexes by pick: null for the identity entry and for entries that\n")
		g.pf("// bound nothing.\n")
		g.pf("public static readonly System.Action<%s, TableReport>[] %sFixedClampKnown = new System.Action<%s, TableReport>[] {\n", name, name, name)
		for i, rows := range entryBounds {
			if len(rows) == 0 {
				g.pf("    null,\n")
			} else {
				g.pf("    %sFixedClampKnown%d,\n", name, i)
			}
		}
		g.pf("};\n\n")
	}

	g.pf("public static long %sFixedMeasure(long count)\n{\n", name)
	g.pf("    return TableFixedWire.HeaderBytes + 4 + %sFixedLayoutBytes + count * %sFixedRecordBytes;\n}\n\n", name, name)

	g.pf("public static long %sFixedSave(ReadOnlySpan<%s> values, Span<byte> buffer)\n{\n", name, name)
	g.pf("    long need = %sFixedMeasure(values.Length);\n", name)
	g.pf("    if (values.Length < 0 || buffer.Length < need) { return -1; }\n")
	if fixedFiniteNeededStruct(st, map[string]bool{}) {
		// a NaN or an infinity is refused by every writer (docs/SPEC-TABLES.md
		// §3.3), before the first output byte
		g.pf("    for (int k = 0; k < values.Length; ++k) { if (%sFixedFiniteBody(values[k]) != null) { return -1; } }\n", name)
	}
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
	// STEP 1 (§5.3): only whether there IS a first byte. THE FORM BYTE IS READ
	// BEFORE THE FILE'S LENGTH — a committed form-1 file is TEN bytes and a
	// form-2 batch THREE. And the DIRECTIONS are §5.3's, the same eight other
	// legs answer: 1 previous_form, 2 message_form_as_file, OTHERWISE newer_form
	// — never "below this form" for a byte, which named 0 previous_form.
	g.pf("    if (data.Length < 1)\n    {\n")
	g.pf("        if (report != null) { report.Malformed = true; report.Verdict = TableWire.Verdict.Damaged; }\n")
	g.pf("        return -1;\n    }\n")
	g.pf("    if (data[0] != TableFixedWire.Form)\n    {\n")
	g.pf("        if (report != null)\n        {\n")
	g.pf("            report.Refused = true;\n")
	g.pf("            report.Reason = data[0] == 1 ? \"previous_form\"\n")
	g.pf("                          : data[0] == 2 ? \"message_form_as_file\"\n")
	g.pf("                          : \"newer_form\";\n")
	g.pf("            report.Verdict = TableWire.Verdict.Refused;\n        }\n")
	g.pf("        return -1;\n    }\n")
	g.pf("    if (data.Length < TableFixedWire.HeaderBytes + 4)\n    {\n")
	g.pf("        if (report != null) { report.Malformed = true; report.Verdict = TableWire.Verdict.Damaged; }\n")
	g.pf("        return -1;\n    }\n")
	// STEP 2's second half: THE SEVEN RESERVED BYTES ARE REFUSED, NOT IGNORED.
	g.pf("    for (int reserved = 1; reserved < TableFixedWire.HashAt; reserved++)\n    {\n")
	g.pf("        if (data[reserved] != 0)\n        {\n")
	g.pf("            if (report != null) { report.Malformed = true; report.Verdict = TableWire.Verdict.Damaged; }\n")
	g.pf("            return -1;\n        }\n    }\n")
	g.pf("    uint layout_bytes = BinaryPrimitives.ReadUInt32LittleEndian(data.Slice(TableFixedWire.HeaderBytes));\n")
	g.pf("    if ((long)layout_bytes + TableFixedWire.HeaderBytes + 4 > data.Length)\n    {\n")
	g.pf("        if (report != null) { report.Refused = true; report.Reason = \"layout_malformed\"; report.Verdict = TableWire.Verdict.Refused; }\n")
	g.pf("        return -1;\n    }\n")
	g.pf("    ReadOnlySpan<byte> layout = data.Slice(TableFixedWire.HeaderBytes + 4, (int)layout_bytes);\n")
	// STEP 4: THE HEADER'S HASH, TAKEN AS GIVEN. Nothing is recomputed from the
	// wire: the definitions digest is not on the wire, so the hash cannot be
	// re-derived from a file at all (§5.3).
	g.pf("    ulong hash = BinaryPrimitives.ReadUInt64LittleEndian(data.Slice(TableFixedWire.HashAt));\n")
	// STEP 5 and STEP 6: SELECT BY HASH, then the floor. Outside the lineage is
	// layout_newer — ship the reader; below the floor is layout_unsupported —
	// upgrade the client. BOTH report THE FILE'S hash (§5.9 #7), and what
	// belongs to layout_newer alone is "and nothing else".
	g.pf("    int pick = TableFixedWire.Select(%sFixedKnown, hash);\n", name)
	g.pf("    if (pick < 0)\n    {\n")
	g.pf("        if (report != null) { report.Refused = true; report.Reason = \"layout_newer\"; report.LayoutHash = hash; report.Verdict = TableWire.Verdict.Refused; }\n")
	g.pf("        return -1;\n    }\n")
	g.pf("    if (pick < %sFixedFloor)\n    {\n", name)
	g.pf("        if (report != null) { report.Refused = true; report.Reason = \"layout_unsupported\"; report.LayoutHash = hash; report.Verdict = TableWire.Verdict.Refused; }\n")
	g.pf("        return -1;\n    }\n")
	// STEP 7: a known hash is read under THE LOCK'S layout bytes. A difference
	// is ONE name — a lie about a known version — and §1.1's seven rules do not
	// run at read time at all: no stranger's layout is ever walked.
	g.pf("    TableFixedKnownLayout known = %sFixedKnown[pick];\n", name)
	g.pf("    if (layout_bytes != (uint)known.Layout.Length || !layout.SequenceEqual(known.Layout))\n    {\n")
	g.pf("        if (report != null) { report.Refused = true; report.Reason = \"layout_malformed\"; report.Verdict = TableWire.Verdict.Refused; }\n")
	g.pf("        return -1;\n    }\n")
	// STEP 8: the plan this endpoint resolves to, and the record size FROM THE LOCK.
	g.pf("    ReadOnlySpan<byte> at = data.Slice(TableFixedWire.HeaderBytes + 4 + (int)layout_bytes);\n")
	g.pf("    int rest = data.Length - TableFixedWire.HeaderBytes - 4 - (int)layout_bytes;\n")
	g.pf("    ReadOnlySpan<TableFixedEntry> entries = %sFixedPlan;\n", name)
	g.pf("    long record_bytes = known.RecordBytes;\n")
	// THE IDENTITY PLAN'S BYTES ARE ITS GUARD POOL (§4.1): it carries no remap
	// table, and a chain past the two lanes is read out of the pool here.
	g.pf("    ReadOnlySpan<byte> planBytes = %sFixedGuardPool;\n", name)
	g.pf("    TableFixedFill[] fillBuf = Array.Empty<TableFixedFill>();\n")
	g.pf("    int fillCount = 0;\n")
	g.pf("    int census_unknown = 0;\n")
	g.pf("    int census_kind = 0;\n")
	if hasWriterBounds {
		g.pf("    // THE WRITER'S OWN BOUNDS for the endpoint this file selected (ALGORITHM\n")
		g.pf("    // §4.6, bill §12.5): held after the plan run, before this reader's own\n")
		g.pf("    // pass, so a value forged past what the writer could have written lands\n")
		g.pf("    // the writer's bound and counts, never this reader's wider own.\n")
		g.pf("    System.Action<%s, TableReport> writerClamp = null;\n", name)
	}
	// THE IDENTITY LANE IS AN INDEX COMPARISON (§5.3): the reader's own entry is
	// the LAST lineage entry (§5.2 appends the current layout last), so the lane
	// is chosen by comparing the resolved lineage index with the reader's own
	// index — never by recomputing or re-comparing the header's hash (§5.3, R7).
	g.pf("    if (pick != %d)\n    {\n", len(entries)-1)
	g.pf("        TableFixedLineagePlan lane = %sFixedLineagePlans[pick];\n", name)
	g.pf("        if (lane.Why != null)\n        {\n")
	g.pf("            if (report != null) { report.Refused = true; report.Reason = lane.Why; report.Verdict = TableWire.Verdict.Refused; }\n")
	g.pf("            return -1;\n        }\n")
	// §5.9 #5: the caller's plan slice stays a CAPACITY DECLARATION and is no
	// longer written through. The selected plan's entry count is checked
	// against it and refuses plan_too_large BY NAME.
	g.pf("        if (lane.Count > plan.Length)\n        {\n")
	g.pf("            if (report != null) { report.Refused = true; report.Reason = \"plan_too_large\"; report.Verdict = TableWire.Verdict.Refused; }\n")
	g.pf("            return -1;\n        }\n")
	g.pf("        entries = new ReadOnlySpan<TableFixedEntry>(lane.Entries, 0, lane.Count);\n")
	g.pf("        planBytes = MemoryMarshal.AsBytes<TableFixedEntry>(lane.Entries);\n")
	g.pf("        // THE PREFILL IS THE SLOTS THE PLAN DOES NOT LAND, derived from\n")
	g.pf("        // the plan this endpoint selected. It reads no layout and compiles\n")
	g.pf("        // nothing, so it is not what §5.8 row 3 retires from the load\n")
	g.pf("        // path; the PLAN is, and the plan is the build's.\n")
	g.pf("        int slotN = %sFixedSlots.Length;\n", name)
	g.pf("        if (slotN > 0)\n        {\n")
	g.pf("            byte[] landed = new byte[slotN];\n")
	g.pf("            fillBuf = new TableFixedFill[slotN];\n")
	g.pf("            fillCount = TableFixedWire.Fills(entries, %sFixedCover, landed, fillBuf);\n", name)
	g.pf("        }\n")
	g.pf("        // THE CENSUS IS ONCE PER ENDPOINT and never per record (§5.4), and\n")
	g.pf("        // it lands only on a read that RETURNS: REFUSE moves no counter.\n")
	g.pf("        census_unknown = lane.Unknown;\n")
	g.pf("        census_kind = lane.KindMismatch;\n")
	if hasWriterBounds {
		g.pf("        writerClamp = %sFixedClampKnown[pick];\n", name)
	}
	g.pf("    }\n")
	g.pf("    if (record_bytes <= 8 || rest %% record_bytes != 0)\n    {\n")
	g.pf("        if (report != null) { report.Malformed = true; report.Verdict = TableWire.Verdict.Damaged; }\n")
	g.pf("        return -1;\n    }\n")
	g.pf("    long n = rest / record_bytes;\n")
	g.pf("    if (n > values.Length)\n    {\n")
	g.pf("        if (report != null) { report.Refused = true; report.Reason = \"batch_too_large\"; report.Verdict = TableWire.Verdict.Refused; }\n")
	g.pf("        return -1;\n    }\n")
	// THE WIDEN SCRATCH IS HOISTED OUT OF THE RECORD LOOP. A widened run needs
	// a buffer wider than the bytes it reads, and taking that buffer inside the
	// loop charged one allocation per widened run PER RECORD. One buffer serves
	// the whole batch: TableFixedWire.Run grows it only when a run needs more
	// than it holds, and writes every byte it hands on.
	g.pf("    byte[] widenScratch = Array.Empty<byte>();\n")
	g.pf("    // ONE RECORD LOOP. Prefill the holes, walk the plan. Empty list is the\n")
	g.pf("    // skip: identity's fill is empty, so this read writes no slot twice.\n")
	g.pf("    for (int k = 0; k < n; ++k)\n    {\n")
	// THE PER-RECORD HASH CHECK IS FIRST — before the prefill, so a refusal has
	// written not one destination byte (§5.3, §5.8 row 9).
	g.pf("        if (BinaryPrimitives.ReadUInt64LittleEndian(at) != hash)\n        {\n")
	g.pf("            if (report != null) { report.Refused = true; report.Reason = \"no_layout\"; report.Verdict = TableWire.Verdict.Refused; }\n")
	g.pf("            return -1;\n        }\n")
	g.pf("        if (values[k] == null) { values[k] = new %s(); }\n", name)
	g.pf("        if (fillCount < 0) { TableFixedWire.FillRun(fillBuf.AsSpan(0, fillCount), %sFixedSlots, values[k]); }\n", name)
	g.pf("        %sFixedFillRun(fillBuf.AsSpan(0, fillCount), values[k]);\n", name)
	// THE RUN REPORTS ITS OWN REFUSAL (§4.5, §5.8 row 8): a content violation
	// in the used units comes back as the name text_ill_formed, and the load
	// refuses BY NAME — not malformed, and nothing decoded past it.
	g.pf("        int forgedOrdinal = 0;\n")
	g.pf("        // Specialized per-table twin of TableFixedWire.Run(entries, %sFixedSlots, ...): one compiled loop per plan, no delegate call per plan entry.\n", name)
	g.pf("        string run_why = %sFixedRun(entries, at.Slice(8), values[k], report, planBytes, ref widenScratch, ref forgedOrdinal);\n", name)
	g.pf("        if (run_why != null)\n        {\n")
	g.pf("            if (report != null) { report.Refused = true; report.Reason = run_why; report.Verdict = TableWire.Verdict.Refused; }\n")
	g.pf("            return -1;\n        }\n")
	if fixedFiniteNeededStruct(st, map[string]bool{}) {
		// A FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3): the finite pass runs
		// FIRST, before the bounds pass, over the storage the run just wrote,
		// and a NaN or an infinity refuses the whole batch as non_finite_float,
		// the tolerant read included. Nothing of the batch is kept: every record
		// this call wrote goes back to its defaults.
		g.pf("        string finite_why = %sFixedFiniteBody(values[k]);\n", name)
		g.pf("        if (finite_why != null)\n        {\n")
		g.pf("            for (int j = 0; j <= k; ++j) { TableReset(values[j]); }\n")
		g.pf("            if (report != null) { report.Refused = true; report.NonFiniteFloat = finite_why; report.Reason = \"non_finite_float: \" + finite_why; report.Verdict = TableWire.Verdict.Refused; }\n")
		g.pf("            return -1;\n        }\n")
	}
	// STEP 11's BOUNDS(out[rec]): straight-line code after the plan run, over
	// STORAGE, so ONE pass covers BOTH plans (§4.6). A type that bounds nothing
	// emits no pass and this line is not written at all.
	if hasWriterBounds {
		g.pf("        writerClamp?.Invoke(values[k], report);\n")
	}
	if ir.TableFixedClampNeeded(st) {
		g.pf("        %sFixedClamp(values[k], report);\n", name)
	}
	// THE FORGED ORDINAL'S CLAMP IS THE BOUNDS STEP'S TOO (§5.4): the ordinal
	// op lands `None` and moves nothing, recording the forge in the per-record
	// lane; the count reaches the report here, beside the storage pass's own.
	g.pf("        if (report != null) { report.Clamped += forgedOrdinal; }\n")
	g.pf("        at = at.Slice((int)record_bytes);\n    }\n")
	// THE COMPILE CENSUS LANDS ONCE, AFTER THE RECORD LOOP, on a read that
	// returns n (§5.9 #6). Not inside the loop — §5.4 says once per endpoint — and
	// not before it, because a refusal that came after would have moved a
	// counter and REFUSE IS TOTAL.
	g.pf("    if (report != null)\n    {\n")
	g.pf("        report.Unknown += census_unknown;\n")
	g.pf("        report.KindMismatch += census_kind;\n")
	g.pf("        report.Verdict = TableWire.Verdict.Ok;\n    }\n    return n;\n}\n\n")

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
	g.pf("    byte[] widenScratch = Array.Empty<byte>();\n")
	g.pf("    int forgedOrdinal = 0;\n")
	g.pf("    string run_why = %sFixedRun(plan, src, dst, report, planBytes, ref widenScratch, ref forgedOrdinal);\n", name)
	g.pf("    if (report != null) { report.Clamped += forgedOrdinal; }\n")
	g.pf("    if (run_why != null && report != null) { report.Refused = true; report.Reason = run_why; report.Verdict = TableWire.Verdict.Refused; }\n}\n\n")
}

func fixedLambdaStmt(lambda string) string {
	_, body, ok := strings.Cut(lambda, "=>")
	if !ok {
		return ""
	}
	body = strings.TrimSpace(body)
	if strings.HasPrefix(body, "{") && strings.HasSuffix(body, "}") {
		return body
	}
	return body + ";"
}

func (g *tableGen) emitFixedRunHelpers(name string, slots []fixedSlot) {
	g.pf("private static void %sFixedSetRaw(%s t, uint slot, ulong v, TableReport rep)\n{\n", name, name)
	g.pf("    switch (slot)\n    {\n")
	for i, s := range slots {
		if s.setRaw != "" {
			g.pf("        case %d: %s break;\n", i, fixedLambdaStmt(s.setRaw))
		} else if s.setRawReport != "" {
			g.pf("        case %d: %s break;\n", i, fixedLambdaStmt(s.setRawReport))
		}
	}
	g.pf("        default: break;\n    }\n}\n\n")

	g.pf("private static void %sFixedSetDouble(%s t, uint slot, double d)\n{\n", name, name)
	g.pf("    switch (slot)\n    {\n")
	for i, s := range slots {
		if s.setDouble != "" {
			g.pf("        case %d: %s break;\n", i, fixedLambdaStmt(s.setDouble))
		}
	}
	g.pf("        default: break;\n    }\n}\n\n")

	g.pf("private static void %sFixedSetWide(%s t, uint slot, UInt128 w)\n{\n", name, name)
	g.pf("    switch (slot)\n    {\n")
	for i, s := range slots {
		if s.setWide != "" {
			g.pf("        case %d: %s break;\n", i, fixedLambdaStmt(s.setWide))
		}
	}
	g.pf("        default: break;\n    }\n}\n\n")

	g.pf("private static void %sFixedSetBytes(%s t, uint slot, ReadOnlySpan<byte> b, int l)\n{\n", name, name)
	g.pf("    switch (slot)\n    {\n")
	for i, s := range slots {
		if s.setBytes != "" {
			g.pf("        case %d: %s break;\n", i, fixedLambdaStmt(s.setBytes))
		}
	}
	g.pf("        default: break;\n    }\n}\n\n")

	g.pf("private static void %sFixedSetChars(%s t, uint slot, ReadOnlySpan<byte> b, int l)\n{\n", name, name)
	g.pf("    switch (slot)\n    {\n")
	for i, s := range slots {
		if s.setChars != "" {
			g.pf("        case %d: %s break;\n", i, fixedLambdaStmt(s.setChars))
		}
	}
	g.pf("        default: break;\n    }\n}\n\n")

	g.pf("private static void %sFixedResetSlot(%s t, uint slot)\n{\n", name, name)
	g.pf("    switch (slot)\n    {\n")
	for i, s := range slots {
		if s.reset != "" {
			g.pf("        case %d: %s break;\n", i, fixedLambdaStmt(s.reset))
		}
	}
	g.pf("        default: break;\n    }\n}\n\n")

	g.pf("public static void %sFixedFillRun(ReadOnlySpan<TableFixedFill> fill, %s dst)\n{\n", name, name)
	g.pf("    for (int i = 0; i < fill.Length; ++i)\n    {\n")
	g.pf("        uint off = fill[i].Dst;\n")
	g.pf("        uint n = fill[i].Size;\n")
	g.pf("        for (uint s = 0; s < n; ++s)\n        {\n")
	g.pf("            %sFixedResetSlot(dst, off + s);\n", name)
	g.pf("        }\n    }\n}\n\n")

	g.pf("public static string %sFixedRun(\n", name)
	g.pf("    ReadOnlySpan<TableFixedEntry> plan,\n")
	g.pf("    ReadOnlySpan<byte> src,\n")
	g.pf("    %s dst,\n", name)
	g.pf("    TableReport report,\n")
	g.pf("    ReadOnlySpan<byte> planBytes,\n")
	g.pf("    ref byte[] widenScratch,\n")
	g.pf("    ref int forgedOrdinal)\n{\n")
	g.pf("    for (int i = 0; i < plan.Length; ++i)\n    {\n")
	g.pf("        ref readonly TableFixedEntry p = ref plan[i];\n")
	g.pf("        if (p.Guard != TableFixedWire.NoGuard && TableFixedWire.TagAt(src, p.Guard, p.ArgW) != p.Arg) { continue; }\n")
	g.pf("        if (p.Guard2 != TableFixedWire.NoGuard && TableFixedWire.TagAt(src, p.Guard2, p.ArgW2) != p.Arg2) { continue; }\n")
	g.pf("        if (p.GuardCount > 2 && !TableFixedWire.ChainHolds(src, p, planBytes)) { continue; }\n")
	g.pf("        switch (p.Op)\n        {\n")
	g.pf("            case TableFixedWire.Copy:\n                {\n")
	g.pf("                    if (p.Size == 1) { %sFixedSetRaw(dst, p.Dst, src[(int)p.Src], report); }\n", name)
	g.pf("                    else if (p.Size == 2) { %sFixedSetRaw(dst, p.Dst, BinaryPrimitives.ReadUInt16LittleEndian(src.Slice((int)p.Src)), report); }\n", name)
	g.pf("                    else if (p.Size == 4) { %sFixedSetRaw(dst, p.Dst, BinaryPrimitives.ReadUInt32LittleEndian(src.Slice((int)p.Src)), report); }\n", name)
	g.pf("                    else if (p.Size == 8) { %sFixedSetRaw(dst, p.Dst, BinaryPrimitives.ReadUInt64LittleEndian(src.Slice((int)p.Src)), report); }\n", name)
	g.pf("                    else if (p.Size == 16)\n                    {\n")
	g.pf("                        ulong lo = BinaryPrimitives.ReadUInt64LittleEndian(src.Slice((int)p.Src));\n")
	g.pf("                        ulong hi = BinaryPrimitives.ReadUInt64LittleEndian(src.Slice((int)p.Src + 8));\n")
	g.pf("                        %sFixedSetWide(dst, p.Dst, new UInt128(hi, lo));\n", name)
	g.pf("                    }\n")
	g.pf("                    else { %sFixedSetBytes(dst, p.Dst, src.Slice((int)p.Src, (int)p.Size), (int)p.Size); }\n", name)
	g.pf("                    break;\n                }\n")
	g.pf("            case TableFixedWire.Flat:\n                {\n")
	g.pf("                    %sFixedSetBytes(dst, p.Dst, src.Slice((int)p.Src, (int)p.Size), (int)p.Size);\n", name)
	g.pf("                    break;\n                }\n")
	g.pf("            case TableFixedWire.FlatWiden:\n                {\n")
	g.pf("                    int sw = p.Meta == 0 ? 1 : p.Meta;\n")
	g.pf("                    int dw = p.DstSize == 0 ? 1 : p.DstSize;\n")
	g.pf("                    int runs = (int)p.Size / sw;\n")
	g.pf("                    int need = runs * dw;\n")
	g.pf("                    if (widenScratch == null || widenScratch.Length < need)\n                    {\n")
	g.pf("                        widenScratch = new byte[need];\n                    }\n")
	g.pf("                    Span<byte> wide = widenScratch.AsSpan(0, need);\n")
	g.pf("                    for (int e = 0; e < runs; ++e)\n                    {\n")
	g.pf("                        int at = (int)p.Src + e * sw;\n")
	g.pf("                        if (p.Sign == 2)\n                        {\n")
	g.pf("                            double d2 = (double)BinaryPrimitives.ReadSingleLittleEndian(src.Slice(at));\n")
	g.pf("                            BinaryPrimitives.WriteDoubleLittleEndian(wide.Slice(e * dw), d2);\n")
	g.pf("                            continue;\n                        }\n")
	g.pf("                        ulong raw = 0;\n")
	g.pf("                        if (sw == 1) raw = src[at];\n")
	g.pf("                        else if (sw == 2) raw = BinaryPrimitives.ReadUInt16LittleEndian(src.Slice(at));\n")
	g.pf("                        else if (sw == 4) raw = BinaryPrimitives.ReadUInt32LittleEndian(src.Slice(at));\n")
	g.pf("                        else if (sw == 8) raw = BinaryPrimitives.ReadUInt64LittleEndian(src.Slice(at));\n")
	g.pf("                        if (p.Sign == 1)\n                        {\n")
	g.pf("                            int bits = sw * 8;\n")
	g.pf("                            ulong top = 1UL << (bits - 1);\n")
	g.pf("                            if ((raw & top) != 0) { raw |= ~((top << 1) - 1UL); }\n                        }\n")
	g.pf("                        Span<byte> cell = wide.Slice(e * dw, dw);\n")
	g.pf("                        if (dw == 1) cell[0] = (byte)raw;\n")
	g.pf("                        else if (dw == 2) BinaryPrimitives.WriteUInt16LittleEndian(cell, (ushort)raw);\n")
	g.pf("                        else if (dw == 4) BinaryPrimitives.WriteUInt32LittleEndian(cell, (uint)raw);\n")
	g.pf("                        else if (dw == 8) BinaryPrimitives.WriteUInt64LittleEndian(cell, raw);\n                    }\n")
	g.pf("                    %sFixedSetBytes(dst, p.Dst, wide, need);\n", name)
	g.pf("                    if (report != null) { report.Widened += runs; }\n")
	g.pf("                    break;\n                }\n")
	g.pf("            case TableFixedWire.Count:\n                {\n")
	g.pf("                    int v = BinaryPrimitives.ReadInt32LittleEndian(src.Slice((int)p.Src));\n")
	g.pf("                    if (v < 0) { v = 0; if (report != null) report.Clamped++; }\n")
	g.pf("                    else if ((uint)v > p.Size) { v = (int)p.Size; if (report != null) report.Clamped++; }\n")
	g.pf("                    %sFixedSetRaw(dst, p.Dst, (ulong)(uint)v, report);\n", name)
	g.pf("                    break;\n                }\n")
	g.pf("            case TableFixedWire.Text:\n                {\n")
	g.pf("                    uint unit = (p.Meta == TableFixedWire.TextWide) ? 2u : 1u;\n")
	g.pf("                    uint cap = p.Size / unit;\n")
	g.pf("                    int v = BinaryPrimitives.ReadInt32LittleEndian(src.Slice((int)p.Src));\n")
	g.pf("                    if (v < 0) { v = 0; if (report != null) report.Clamped++; }\n")
	g.pf("                    else if ((uint)v > cap) { v = (int)cap; if (report != null) report.Clamped++; }\n")
	g.pf("                    ReadOnlySpan<byte> textBytes = src.Slice((int)p.Src + 4, (int)p.Size);\n")
	g.pf("                    if (TableFixedWire.TextIllFormed(p.Meta, (uint)v, textBytes))\n                    {\n")
	g.pf("                        return \"text_ill_formed\";\n                    }\n")
	g.pf("                    %sFixedSetRaw(dst, p.Dst, (ulong)(uint)v, report);\n", name)
	g.pf("                    if (p.Meta == TableFixedWire.TextWide)\n                    {\n")
	g.pf("                        %sFixedSetChars(dst, p.Aux, textBytes, v);\n", name)
	g.pf("                    }\n")
	g.pf("                    else\n                    {\n")
	g.pf("                        %sFixedSetBytes(dst, p.Aux, textBytes, v);\n", name)
	g.pf("                    }\n")
	g.pf("                    break;\n                }\n")
	g.pf("            case TableFixedWire.Ordinal:\n                {\n")
	g.pf("                    ulong raw = 0;\n")
	g.pf("                    if (p.Size == 1) raw = src[(int)p.Src];\n")
	g.pf("                    else if (p.Size == 2) raw = BinaryPrimitives.ReadUInt16LittleEndian(src.Slice((int)p.Src));\n")
	g.pf("                    else if (p.Size == 4) raw = BinaryPrimitives.ReadUInt32LittleEndian(src.Slice((int)p.Src));\n")
	g.pf("                    else if (p.Size == 8) raw = BinaryPrimitives.ReadUInt64LittleEndian(src.Slice((int)p.Src));\n")
	g.pf("                    uint v = 0;\n")
	g.pf("                    if (!planBytes.IsEmpty && p.Aux < (uint)planBytes.Length)\n                    {\n")
	g.pf("                        ReadOnlySpan<byte> tableBytes = planBytes.Slice((int)p.Aux);\n")
	g.pf("                        ushort count = BinaryPrimitives.ReadUInt16LittleEndian(tableBytes);\n")
	g.pf("                        if (raw != 0 && raw <= count)\n                        {\n")
	g.pf("                            v = BinaryPrimitives.ReadUInt16LittleEndian(tableBytes.Slice(2 * (int)raw));\n                        }\n")
	g.pf("                        if (raw > count) { forgedOrdinal++; }\n                    }\n")
	g.pf("                    %sFixedSetRaw(dst, p.Dst, v, report);\n", name)
	g.pf("                    break;\n                }\n")
	g.pf("            case TableFixedWire.Widen:\n                {\n")
	g.pf("                    ulong raw = 0;\n")
	g.pf("                    if (p.Size == 1) raw = src[(int)p.Src];\n")
	g.pf("                    else if (p.Size == 2) raw = BinaryPrimitives.ReadUInt16LittleEndian(src.Slice((int)p.Src));\n")
	g.pf("                    else if (p.Size == 4) raw = BinaryPrimitives.ReadUInt32LittleEndian(src.Slice((int)p.Src));\n")
	g.pf("                    else if (p.Size == 8) raw = BinaryPrimitives.ReadUInt64LittleEndian(src.Slice((int)p.Src));\n")
	g.pf("                    if (p.Sign != 0)\n                    {\n")
	g.pf("                        int bits = (int)p.Size * 8;\n")
	g.pf("                        ulong top = 1UL << (bits - 1);\n")
	g.pf("                        if ((raw & top) != 0)\n                        {\n")
	g.pf("                            raw |= ~((top << 1) - 1UL);\n                        }\n                    }\n")
	g.pf("                    if (p.DstSize == 16)\n                    {\n")
	g.pf("                        UInt128 w = (p.Sign != 0 && (long)raw < 0) ? unchecked((UInt128)(Int128)(long)raw) : (UInt128)raw;\n")
	g.pf("                        %sFixedSetWide(dst, p.Dst, w);\n", name)
	g.pf("                    }\n")
	g.pf("                    else\n                    {\n")
	g.pf("                        %sFixedSetRaw(dst, p.Dst, raw, report);\n", name)
	g.pf("                    }\n")
	g.pf("                    if (report != null) { report.Widened++; }\n")
	g.pf("                    break;\n                }\n")
	g.pf("            case TableFixedWire.WidenF:\n                {\n")
	g.pf("                    float f = BinaryPrimitives.ReadSingleLittleEndian(src.Slice((int)p.Src));\n")
	g.pf("                    %sFixedSetDouble(dst, p.Dst, (double)f);\n", name)
	g.pf("                    if (report != null) { report.Widened++; }\n")
	g.pf("                    break;\n                }\n")
	g.pf("            case TableFixedWire.Const:\n                {\n")
	g.pf("                    %sFixedSetRaw(dst, p.Dst, p.Aux, report);\n", name)
	g.pf("                    break;\n                }\n")
	g.pf("            default: break;\n        }\n    }\n    return null;\n}\n\n")
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
