// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3: the reference
// writer, the reference reader and the vocabulary block, for C++.
//
// A fixed-table record is an eight-byte hash of the writer's vocabulary block
// and then the values in declared order, every field at its DECLARED STORAGE
// WIDTH, nothing padded between fields. So:
//
//	THE WRITER is the type's constant bytes memcpy'd — the hash and zeros —
//	and then value stores at constant offsets. MeasureBody is a constexpr.
//
//	THE READER IS ONE PLAN-DRIVEN PATH. A plan is a flat array of entries and
//	a read is one loop over it. For a record whose hash equals the reader's
//	own the plan is the IDENTITY PLAN, built at COMPILE TIME by a constexpr
//	walk of this type's leaves with adjacent runs coalesced; for any other
//	hash the SAME LOOP runs over a plan compiled once from the writer's block.
//	There is no second reader and no fast/slow cliff — the owner's own ruling,
//	which §3.4 quotes him on.
//
// Nothing here touches form 1: a fixed-table type's reader accepts both, by
// the form byte.
package cpptable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixedKindOptional is the ONE kind §3.4's block adds to §3's closed set: the
// OPTIONAL WRAPPER, one child, whose size is one present byte plus the child's.
// Nothing rides under it in a record — it is a block kind and not a wire kind.
const fixedKindOptional = 35

// ---------------------------------------------------------------------------
// THE LAYOUT: a constant size per field
// ---------------------------------------------------------------------------

// fixedStorageBytes is a LEAF's declared storage width — the width SPEC.md
// gives the declaration, identical in every port, which is what makes the
// identity plan a copy rather than a conversion (§3.4).
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
			return 8 // the raw mask, as §3 carries it
		}
	}
	return 0
}

const (
	fixedCountBytes   = int64(4) // a count and a length are the int32 their storage already is
	fixedPresentBytes = int64(1)
)

// fixedTypeBytes is C(T). Every one is a compile-time constant, which is why
// MeasureBody is a constexpr and not a function that reads the value.
func fixedTypeBytes(st *ir.Struct) int64 {
	var n int64
	for _, f := range st.Fields {
		n += fixedFieldBytes(f)
	}
	return n
}

// fixedFieldBytes is C(f) — §3.4's constant-size table in one function.
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

// fixedElementBytes is the constant of ONE element of a field.
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

// fixedSupported reports whether a type's whole closure is one this reference
// carries. A pointer, a map and an unbounded array make their holder VARIABLE
// and never reach here; what this adds is the two shapes the reference does
// not yet lay out — a union arm carrying text or an array, and a guarded
// branch, which §3.4 refuses outright.
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
						return false // a void arm has no storage this walk can name
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

// ---------------------------------------------------------------------------
// THE VOCABULARY BLOCK, and MY side of it
// ---------------------------------------------------------------------------

// fixedBlockEntry is one seventeen-byte entry: an id, a kind, a constant size
// and a child count, in the writer's declared order (§3.4).
type fixedBlockEntry struct {
	id       uint64
	kind     int
	size     int64
	children int
	note     string // a comment on the emitted array; never a wire byte
}

// fixedWalk builds the block and, beside it, the DESTINATION row per entry:
// the storage facts a block entry cannot carry, which is what a plan compiled
// from another writer's block lands values through.
type fixedWalk struct {
	entries []fixedBlockEntry
	dst     []string
}

func (w *fixedWalk) push(e fixedBlockEntry, dst string) {
	w.entries = append(w.entries, e)
	w.dst = append(w.dst, dst)
}

func dstRow(dst, stride, aux string, counted, arg int) string {
	if dst == "" {
		dst = "0"
	}
	if stride == "" {
		stride = "0"
	}
	if aux == "" {
		aux = "0"
	}
	return fmt.Sprintf("{ %s, %s, %s, %d, %d }", dst, stride, aux, counted, arg)
}

func offOf(owner, member string) string {
	return fmt.Sprintf("(uint32_t) __builtin_offsetof( %s, %s )", owner, member)
}

func (g *tableGen) fixedWalkRoot(st *ir.Struct) *fixedWalk {
	w := &fixedWalk{}
	w.push(fixedBlockEntry{id: ir.TableWireId(st.WireName()), kind: ir.TableKindTable, size: fixedTypeBytes(st), children: len(st.Fields), note: st.Name}, dstRow("0", "", "", 0, 0))
	for _, f := range st.Fields {
		g.fixedWalkField(w, st.Name, f)
	}
	return w
}

func (g *tableGen) fixedWalkField(w *fixedWalk, owner string, f *ir.Field) {
	id := ir.TableFieldWireId(f)
	size := fixedFieldBytes(f)
	if f.Type.Optional {
		w.push(fixedBlockEntry{id: id, kind: fixedKindOptional, size: size, children: 1, note: f.Name + " ?"},
			dstRow("0", "", offOf(owner, f.Name+"_present"), 0, 0))
		g.fixedWalkPayload(w, owner, f, id, size-fixedPresentBytes)
		return
	}
	g.fixedWalkPayload(w, owner, f, id, size)
}

func (g *tableGen) fixedWalkPayload(w *fixedWalk, owner string, f *ir.Field, id uint64, size int64) {
	switch {
	case f.KeyEnum != "":
		// a keyed array's slots are the storage's first member, so the field's
		// own offset is the slot base (§2.4's TableKeyed)
		w.push(fixedBlockEntry{id: id, kind: ir.TableKindKeyed, size: size, children: 2, note: f.Name},
			dstRow(offOf(owner, f.Name), "(uint32_t) sizeof( "+g.fixedElemStorage(f)+" )", "", 0, 0))
		g.fixedWalkEnum(w, f.KeyEnumRef, ir.TableWireId(f.KeyEnum), f.KeyEnum, "0")
		g.fixedWalkElement(w, f, 0, "element", "0")
	case f.Array != ir.ArrayNone:
		counted, aux := 0, ""
		if f.Array == ir.ArrayCounted {
			counted, aux = 1, offOf(owner, f.Name+"_count")
		}
		w.push(fixedBlockEntry{id: id, kind: ir.TableKindArray, size: size, children: 1, note: f.Name},
			dstRow(offOf(owner, f.Name), "(uint32_t) sizeof( "+g.fixedElemStorage(f)+" )", aux, counted, 0))
		g.fixedWalkElement(w, f, 0, "element", "0")
	case f.Type.Kind == ir.TBytes:
		// `bytes(N)` is an ARRAY of u8 on this wire, as it is in §3, and the
		// text flavour on the row is what makes the reader land it in one move
		w.push(fixedBlockEntry{id: id, kind: ir.TableKindArray, size: size, children: 1, note: f.Name},
			dstRow(offOf(owner, f.Name+"_length"), "1", offOf(owner, f.Name), 1, 3))
		w.push(fixedBlockEntry{id: 0, kind: ir.TableKindU8, size: 1, children: 0, note: "u8"}, dstRow("0", "", "", 0, 0))
	case f.Type.Kind == ir.TString:
		w.push(fixedBlockEntry{id: id, kind: ir.TableKindString, size: size, children: 0, note: f.Name},
			dstRow(offOf(owner, f.Name+"_length"), "", offOf(owner, f.Name), 0, 1))
	case f.Type.Kind == ir.TWString:
		w.push(fixedBlockEntry{id: id, kind: ir.TableKindWstring, size: size, children: 0, note: f.Name},
			dstRow(offOf(owner, f.Name+"_length"), "", offOf(owner, f.Name), 0, 2))
	default:
		g.fixedWalkElement(w, f, id, f.Name, offOf(owner, f.Name))
	}
}

func (g *tableGen) fixedWalkElement(w *fixedWalk, f *ir.Field, id uint64, note, dst string) {
	size := fixedElementBytes(f)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			w.push(fixedBlockEntry{id: id, kind: ir.TableKindTable, size: size, children: len(r.Fields), note: note}, dstRow(dst, "", "", 0, 0))
			for _, sub := range r.Fields {
				g.fixedWalkField(w, r.Name, sub)
			}
			return
		case *ir.Union:
			tagType := f.Type.Name + "Type"
			w.push(fixedBlockEntry{id: id, kind: ir.TableKindUnion, size: size, children: len(r.Variants), note: note},
				dstRow(dst, "", dst+" + "+offOf(f.Type.Name, "type"), 0, 0))
			_ = tagType
			for _, v := range r.Variants {
				armID := ir.TableWireId(v.WireName())
				g.fixedWalkElement(w, v.F, armID, v.Name, offOf(f.Type.Name, v.Name))
			}
			return
		case *ir.Enum:
			g.fixedWalkEnum(w, r, id, note, dst)
			return
		}
	}
	w.push(fixedBlockEntry{id: id, kind: ir.TableWireScalarKind(f), size: size, children: 0, note: note}, dstRow(dst, "", "", 0, 0))
}

func (g *tableGen) fixedWalkEnum(w *fixedWalk, e *ir.Enum, id uint64, note, dst string) {
	w.push(fixedBlockEntry{id: id, kind: ir.TableKindEnum, size: int64(e.StorageBits / 8), children: len(e.Variants), note: note}, dstRow(dst, "", "", 0, 0))
	for i := range e.Variants {
		w.push(fixedBlockEntry{id: ir.TableWireId(e.VariantWireName(i)), kind: ir.TableKindNoPayload, size: 0, children: 0, note: e.Variants[i]}, dstRow("0", "", "", 0, 0))
	}
}

// fixedBlockBytes is the block exactly as it rides: a u32 entry count and a
// run of seventeen-byte entries, every number little-endian.
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
	for i := 0; i < 8; i++ {
		b = append(b, byte(v>>(8*i)))
	}
	return b
}

// fixedBlockHash is fnv1a64 over the block's bytes exactly as written, and it
// is the eight bytes every record carries (§3.4).
func fixedBlockHash(block []byte) uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, b := range block {
		h ^= uint64(b)
		h *= 0x100000001b3
	}
	return h
}

// ---------------------------------------------------------------------------
// THE EMISSION
// ---------------------------------------------------------------------------

// fixedRoots is every table of this file the fixed form is emitted for.
func (g *tableGen) fixedRoots(members []*ir.Struct) []*ir.Struct {
	var out []*ir.Struct
	for _, st := range members {
		if !st.IsTable || st.IsMapEntry() || g.isVar(st.Name) || !fixedSupported(st, 0) {
			continue
		}
		out = append(out, st)
	}
	return out
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
		g.pf("constexpr int %sFixedLeaves( TableFixedEntry * out, uint32_t src, uint32_t dst );\n", st.Name)
	}
	g.pf("\n")
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

// fixedLeafCount bounds the entries a type's leaf walk writes, which sizes the
// constexpr plan array. It counts leaves BEFORE coalescing.
func fixedLeafCount(st *ir.Struct) int {
	n := 0
	for _, f := range st.Fields {
		n += fixedFieldLeafCount(f)
	}
	return n
}

func fixedFieldLeafCount(f *ir.Field) int {
	per := fixedElementLeafCount(f)
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

func fixedElementLeafCount(f *ir.Field) int {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return fixedLeafCount(r)
		case *ir.Union:
			n := 1 // the tag
			for _, v := range r.Variants {
				n += fixedFieldLeafCount(v.F)
			}
			return n
		}
	}
	return 1
}

// emitFixedLeafWalk emits the constexpr walk that writes this type's LEAVES at
// a base the caller gives. It is a constexpr function rather than a table of
// literals so that an array of eighty elements is a loop in the source and not
// eighty lines of it, and so that every destination is an offsetof the C++
// compiler folds.
func (g *tableGen) emitFixedLeafWalk(st *ir.Struct) {
	g.pf("// %s's LEAVES: one entry per run of bytes that lands, at a base the caller\n", st.Name)
	g.pf("// gives. TableFixedBuildPlan coalesces the adjacent ones, at compile time.\n")
	g.pf("constexpr int %sFixedLeaves( TableFixedEntry * out, uint32_t src, uint32_t dst )\n{\n", st.Name)
	g.pf("    int n = 0;\n    (void) out; (void) src; (void) dst;\n")
	off := int64(0)
	for _, f := range st.Fields {
		g.emitFixedFieldLeaves(st, f, off)
		off += fixedFieldBytes(f)
	}
	g.pf("    return n;\n}\n\n")
}

func (g *tableGen) emitFixedFieldLeaves(st *ir.Struct, f *ir.Field, off int64) {
	base := off
	if f.Type.Optional {
		g.pf("    out[n++] = TableFixedEntry{ src + %du, dst + %s, 1u, 0u, kTableFixedNoGuard, kTableFixedCopy, 0, 0 }; // %s present\n",
			base, offOf(st.Name, f.Name+"_present"), f.Name)
		base += fixedPresentBytes
	}
	switch {
	case f.KeyEnum != "":
		g.emitFixedElementLoop(st, f, base, f.KeyEnumRef.Max)
	case f.Array == ir.ArrayFixed:
		g.emitFixedElementLoop(st, f, base, f.ArrayBound)
	case f.Array == ir.ArrayCounted:
		g.pf("    out[n++] = TableFixedEntry{ src + %du, dst + %s, %du, 0u, kTableFixedNoGuard, kTableFixedCount, 0, 0 }; // %s count\n",
			base, offOf(st.Name, f.Name+"_count"), f.ArrayBound, f.Name)
		g.emitFixedElementLoop(st, f, base+fixedCountBytes, f.ArrayBound)
	case f.Type.Kind == ir.TString:
		g.emitFixedTextLeaf(st, f, base, f.Type.Size, 1)
	case f.Type.Kind == ir.TWString:
		g.emitFixedTextLeaf(st, f, base, 2*f.Type.Size, 2)
	case f.Type.Kind == ir.TBytes:
		g.emitFixedTextLeaf(st, f, base, f.Type.Size, 3)
	default:
		g.pf("    {\n        const uint32_t es = src + %du;\n        const uint32_t ed = dst + %s;\n", base, offOf(st.Name, f.Name))
		g.emitFixedElementLeavesAt(f, "es", "ed", 8)
		g.pf("    }\n")
	}
}

func (g *tableGen) emitFixedTextLeaf(st *ir.Struct, f *ir.Field, base, units int64, flavour int) {
	g.pf("    out[n++] = TableFixedEntry{ src + %du, dst + %s, %du, dst + %s, kTableFixedNoGuard, kTableFixedText, %d, 0 }; // %s\n",
		base, offOf(st.Name, f.Name+"_length"), units, offOf(st.Name, f.Name), flavour, f.Name)
}

func (g *tableGen) emitFixedElementLoop(st *ir.Struct, f *ir.Field, base, count int64) {
	elem := fixedElementBytes(f)
	g.pf("    for ( uint32_t i = 0; i < %du; ++i )\n    {\n", count)
	g.pf("        const uint32_t es = src + %du + i * %du;\n", base, elem)
	g.pf("        const uint32_t ed = dst + %s + i * (uint32_t) sizeof( %s );\n", offOf(st.Name, f.Name), g.fixedElemStorage(f))
	g.emitFixedElementLeavesAt(f, "es", "ed", 8)
	g.pf("    }\n")
}

// fixedElemStorage is the C++ storage type of ONE element, which is what the
// loop above strides by.
func (g *tableGen) fixedElemStorage(f *ir.Field) string {
	if f.Type.Kind == ir.TNamed {
		return f.Type.Name
	}
	switch f.Type.Kind {
	case ir.TBool:
		return "bool"
	case ir.TFloat32:
		return "float"
	case ir.TFloat64:
		return "double"
	case ir.TBits:
		if f.Type.Width <= 32 {
			return "uint32_t"
		}
		return "uint64_t"
	}
	if f.Type.Width == 128 {
		if f.Type.Signed {
			return "serialize::int128_t"
		}
		return "serialize::uint128_t"
	}
	if f.Type.Signed {
		return fmt.Sprintf("int%d_t", f.Type.Width)
	}
	return fmt.Sprintf("uint%d_t", f.Type.Width)
}

// emitFixedElementLeavesAt emits ONE element's leaves at runtime bases.
func (g *tableGen) emitFixedElementLeavesAt(f *ir.Field, src, dst string, indent int) {
	ind := strings.Repeat(" ", indent)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			g.pf("%sn += %sFixedLeaves( out + n, %s, %s );\n", ind, r.Name, src, dst)
			return
		case *ir.Union:
			tag := int64(ir.StorageBitsFor(r.Max) / 8)
			g.pf("%sout[n++] = TableFixedEntry{ %s, %s + %s, %du, 0u, kTableFixedNoGuard, kTableFixedCopy, 0, 0 }; // the tag\n",
				ind, src, dst, offOf(f.Type.Name, "type"), tag)
			for i, v := range r.Variants {
				g.pf("%s{ // arm %s, ordinal %d — guarded on the tag\n%s    const int guard_at = n;\n", ind, v.Name, i+1, ind)
				g.emitFixedElementLeavesAt(v.F, fmt.Sprintf("( %s + %du )", src, tag),
					fmt.Sprintf("( %s + %s )", dst, offOf(f.Type.Name, v.Name)), indent+4)
				g.pf("%s    for ( int q = guard_at; q < n; ++q ) { out[q].guard = %s; out[q].arg = %d; }\n", ind, src, i+1)
				g.pf("%s}\n", ind)
			}
			return
		}
	}
	g.pf("%sout[n++] = TableFixedEntry{ %s, %s, %du, 0u, kTableFixedNoGuard, kTableFixedCopy, 0, 0 };\n",
		ind, src, dst, fixedElementBytes(f))
}

// ---- the template writer ---------------------------------------------------

func (g *tableGen) emitFixedWriteBody(st *ir.Struct) {
	g.pf("// %s's stores. The template — the hash, then zeros — is memcpy'd first,\n", st.Name)
	g.pf("// which is also what zero-fills every byte of declared slack.\n")
	g.pf("inline void %sFixedWriteBody( uint8_t * b, const %s & value )\n{\n", st.Name, st.Name)
	g.pf("    (void) b; (void) value;\n")
	off := int64(0)
	for _, f := range st.Fields {
		g.emitFixedWriteField(f, off, "b", "value", 4)
		off += fixedFieldBytes(f)
	}
	g.pf("}\n\n")
}

func (g *tableGen) emitFixedWriteField(f *ir.Field, off int64, buf, val string, indent int) {
	ind := strings.Repeat(" ", indent)
	base := off
	if f.Type.Optional {
		g.pf("%sTableFixedPut8( %s + %d, %s.%s_present ? 1 : 0 );\n", ind, buf, base, val, f.Name)
		base += fixedPresentBytes
	}
	switch {
	case f.KeyEnum != "":
		g.emitFixedWriteLoop(f, base, f.KeyEnumRef.Max, buf, val+"."+f.Name+".slots", indent)
	case f.Array == ir.ArrayFixed:
		g.emitFixedWriteLoop(f, base, f.ArrayBound, buf, val+"."+f.Name, indent)
	case f.Array == ir.ArrayCounted:
		g.pf("%sTableFixedPut32( %s + %d, (uint32_t) %s.%s_count );\n", ind, buf, base, val, f.Name)
		g.emitFixedWriteLoop(f, base+fixedCountBytes, f.ArrayBound, buf, val+"."+f.Name, indent)
	case f.Type.Kind == ir.TString:
		g.pf("%sTableFixedPut32( %s + %d, (uint32_t) %s.%s_length );\n", ind, buf, base, val, f.Name)
		g.pf("%smemcpy( %s + %d, %s.%s, %d );\n", ind, buf, base+4, val, f.Name, f.Type.Size)
	case f.Type.Kind == ir.TWString:
		g.pf("%sTableFixedPut32( %s + %d, (uint32_t) %s.%s_length );\n", ind, buf, base, val, f.Name)
		g.pf("%smemcpy( %s + %d, %s.%s, %d );\n", ind, buf, base+4, val, f.Name, 2*f.Type.Size)
	case f.Type.Kind == ir.TBytes:
		g.pf("%sTableFixedPut32( %s + %d, (uint32_t) %s.%s_length );\n", ind, buf, base, val, f.Name)
		g.pf("%smemcpy( %s + %d, %s.%s, %d );\n", ind, buf, base+4, val, f.Name, f.Type.Size)
	default:
		g.emitFixedWriteElement(f, base, buf, val+"."+f.Name, indent)
	}
}

func (g *tableGen) emitFixedWriteLoop(f *ir.Field, base, count int64, buf, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	elem := fixedElementBytes(f)
	g.pf("%sfor ( int64_t i = 0; i < %d; ++i )\n%s{\n", ind, count, ind)
	g.emitFixedWriteElement(f, 0, fmt.Sprintf("%s + %d + i * %d", buf, base, elem), expr+"[i]", indent+4)
	g.pf("%s}\n", ind)
}

func (g *tableGen) emitFixedWriteElement(f *ir.Field, off int64, buf, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			g.pf("%s%sFixedWriteBody( %s + %d, %s );\n", ind, r.Name, buf, off, expr)
			return
		case *ir.Union:
			tag := int64(ir.StorageBitsFor(r.Max) / 8)
			g.pf("%sTableFixedPut%d( %s + %d, (uint%d_t) %s.type );\n", ind, tag*8, buf, off, tag*8, expr)
			g.pf("%sswitch ( %s.type )\n%s{\n", ind, expr, ind)
			for _, v := range r.Variants {
				g.pf("%s    case %sType::%s:\n%s    {\n", ind, f.Type.Name, ir.GoExportName(v.Name), ind)
				g.emitFixedWriteElement(v.F, off+tag, buf, fmt.Sprintf("%s.%s", expr, v.Name), indent+8)
				g.pf("%s        break;\n%s    }\n", ind, ind)
			}
			g.pf("%s    default: break;\n%s}\n", ind, ind)
			return
		case *ir.Enum:
			w := int64(r.StorageBits / 8)
			g.pf("%sTableFixedPut%d( %s + %d, (uint%d_t) %s );\n", ind, w*8, buf, off, w*8, expr)
			return
		case *ir.Flags:
			g.pf("%sTableFixedPut64( %s + %d, (uint64_t) %s );\n", ind, buf, off, expr)
			return
		}
	}
	w := fixedStorageBytes(f.Type)
	switch f.Type.Kind {
	case ir.TBool:
		g.pf("%sTableFixedPut8( %s + %d, %s ? 1 : 0 );\n", ind, buf, off, expr)
	case ir.TFloat32:
		g.pf("%sTableFixedPutF32( %s + %d, %s );\n", ind, buf, off, expr)
	case ir.TFloat64:
		g.pf("%sTableFixedPutF64( %s + %d, %s );\n", ind, buf, off, expr)
	default:
		if w == 16 {
			g.pf("%sTableFixedPut128( %s + %d, %s );\n", ind, buf, off, expr)
			return
		}
		g.pf("%sTableFixedPut%d( %s + %d, (uint%d_t) %s );\n", ind, w*8, buf, off, w*8, expr)
	}
}

// ---- the root's surface ----------------------------------------------------

func (g *tableGen) emitFixedRoot(st *ir.Struct) {
	w := g.fixedWalkRoot(st)
	block := fixedBlockBytes(w.entries)
	hash := fixedBlockHash(block)
	body := fixedTypeBytes(st)

	g.pf("// ---- %s, the fixed form ----\n\n", st.Name)
	g.pf("// MeasureBody IS A CONSTEXPR on this form: the body is the same size for\n")
	g.pf("// every value the type can hold (docs/SPEC-TABLES.md §3.4).\n")
	g.pf("inline constexpr int64_t %sFixedBodyBytes = %d;\n", st.Name, body)
	g.pf("inline constexpr int64_t %sFixedRecordBytes = 8 + %sFixedBodyBytes; // the hash and the body\n", st.Name, st.Name)
	g.pf("inline constexpr uint64_t %sFixedHash = 0x%016xull; // fnv1a64 over the block's bytes\n\n", st.Name, hash)

	g.pf("// THE VOCABULARY BLOCK: %d entries, a PRE-ORDER walk of the closure in the\n", len(w.entries))
	g.pf("// writer's declared order. Every byte is settled by the compiler.\n")
	g.pf("inline constexpr uint8_t %sFixedBlock[] = {\n", st.Name)
	g.emitByteArray(block)
	g.pf("};\n")
	g.pf("inline constexpr int64_t %sFixedBlockBytes = (int64_t) sizeof( %sFixedBlock );\n\n", st.Name, st.Name)

	g.pf("// MY SIDE of the block, one row per entry: the storage facts a block entry\n")
	g.pf("// cannot carry, which is what a plan compiled from another writer's block\n")
	g.pf("// lands values through.\n")
	g.pf("inline constexpr TableFixedDst %sFixedDst[] = {\n", st.Name)
	for i, row := range w.dst {
		g.pf("    %s, // %s\n", row, w.entries[i].note)
	}
	g.pf("};\n\n")

	leaves := fixedLeafCount(st)
	g.pf("// THE IDENTITY PLAN, coalesced at COMPILE TIME out of %d leaves.\n", leaves)
	g.pf("inline constexpr auto %sFixedPlan = TableFixedBuildPlan<%d>( %sFixedLeaves );\n\n", st.Name, leaves, st.Name)

	g.pf("// A FILE: the form byte, the block, then the records to the end of it.\n")
	g.pf("inline constexpr int64_t %sFixedMeasure( int64_t count )\n{\n", st.Name)
	g.pf("    return 1 + 4 + %sFixedBlockBytes + count * %sFixedRecordBytes;\n}\n\n", st.Name, st.Name)

	g.pf("inline int64_t %sFixedSave( const %s * values, int64_t count, uint8_t * buffer, int64_t capacity )\n{\n", st.Name, st.Name)
	g.pf("    const int64_t need = %sFixedMeasure( count );\n", st.Name)
	g.pf("    if ( count < 0 || buffer == NULL || capacity < need ) { return -1; }\n")
	g.pf("    buffer[0] = kTableFixedForm;\n")
	g.pf("    TableFixedPut32( buffer + 1, (uint32_t) %sFixedBlockBytes );\n", st.Name)
	g.pf("    memcpy( buffer + 5, %sFixedBlock, (size_t) %sFixedBlockBytes );\n", st.Name, st.Name)
	g.pf("    uint8_t * at = buffer + 5 + %sFixedBlockBytes;\n", st.Name)
	g.pf("    for ( int64_t k = 0; k < count; ++k )\n    {\n")
	g.pf("        TableFixedPut64( at, %sFixedHash );\n", st.Name)
	g.pf("        memset( at + 8, 0, (size_t) %sFixedBodyBytes ); // the template's zeros\n", st.Name)
	g.pf("        %sFixedWriteBody( at + 8, values[k] );\n", st.Name)
	g.pf("        at += %sFixedRecordBytes;\n    }\n    return need;\n}\n\n", st.Name)

	g.pf("// THE READ: a prefill and ONE loop over ONE plan — the identity plan when\n")
	g.pf("// the block's hash is this build's own, and a plan compiled once from the\n")
	g.pf("// writer's block otherwise. Same loop either way (§3.4).\n")
	g.pf("inline int64_t %sFixedLoad( %s * values, int64_t capacity, const uint8_t * data, int64_t bytes,\n", st.Name, st.Name)
	g.pf("                            TableFixedEntry * plan, int32_t plan_capacity, TableReport * report )\n{\n")
	g.pf("    TableReport local;\n    if ( report == NULL ) { report = &local; }\n")
	g.pf("    if ( data == NULL || bytes < 5 ) { report->malformed = true; return -1; }\n")
	g.pf("    if ( data[0] != kTableFixedForm ) { report->refused = true; report->reason = newer_form; return -1; }\n")
	g.pf("    const uint32_t block_bytes = TableFixedGet32( data + 1 );\n")
	g.pf("    if ( (int64_t) block_bytes + 5 > bytes ) { report->refused = true; report->reason = block_malformed; return -1; }\n")
	g.pf("    const uint8_t * block = data + 5;\n")
	g.pf("    const uint64_t hash = TableFixedHashOf( block, block_bytes );\n")
	g.pf("    const uint8_t * at = block + block_bytes;\n")
	g.pf("    const int64_t rest = bytes - 5 - (int64_t) block_bytes;\n")
	g.pf("    const TableFixedEntry * entries = %sFixedPlan.entries;\n", st.Name)
	g.pf("    int32_t entry_count = %sFixedPlan.count;\n", st.Name)
	g.pf("    int64_t record_bytes = %sFixedRecordBytes;\n", st.Name)
	g.pf("    if ( hash != %sFixedHash )\n    {\n", st.Name)
	g.pf("        // ANOTHER WRITER: the same loop, over a plan compiled from its block.\n")
	g.pf("        TableFixedBlockView parsed;\n")
	g.pf("        if ( !TableFixedParseBlock( block, block_bytes, parsed ) ) { report->refused = true; report->reason = block_malformed; return -1; }\n")
	g.pf("        const int32_t made = TableFixedCompile( parsed, %sFixedBlock, (int32_t) %sFixedBlockBytes, %sFixedDst, plan, plan_capacity, report );\n", st.Name, st.Name, st.Name)
	g.pf("        if ( made < 0 ) { report->refused = true; report->reason = plan_too_large; return -1; }\n")
	g.pf("        entries = plan;\n        entry_count = made;\n")
	g.pf("        record_bytes = 8 + (int64_t) TableFixedEntryAt( parsed, 0 ).size;\n")
	g.pf("    }\n")
	g.pf("    if ( record_bytes <= 8 || rest %% record_bytes != 0 ) { report->malformed = true; return -1; }\n")
	g.pf("    const int64_t n = rest / record_bytes;\n")
	g.pf("    if ( n > capacity ) { report->refused = true; report->reason = batch_too_large; return -1; }\n")
	g.pf("    for ( int64_t k = 0; k < n; ++k )\n    {\n")
	g.pf("        %sReset( values[k] ); // the declared defaults, one prefill\n", st.Name)
	g.pf("        if ( TableFixedGet64( at ) != hash ) { report->refused = true; report->reason = no_block; return -1; }\n")
	g.pf("        TableFixedRun( entries, entry_count, at + 8, (uint8_t *) &values[k], report );\n")
	g.pf("        at += record_bytes;\n    }\n    return n;\n}\n\n")
}

func (g *tableGen) emitByteArray(b []byte) {
	for i := 0; i < len(b); i += 16 {
		end := i + 16
		if end > len(b) {
			end = len(b)
		}
		var sb strings.Builder
		sb.WriteString("   ")
		for _, v := range b[i:end] {
			fmt.Fprintf(&sb, " 0x%02x,", v)
		}
		g.pf("%s\n", sb.String())
	}
}
