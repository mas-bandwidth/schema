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
// TWO ENTRY POINTS, ONE FORM EACH, AND EACH REFUSES THE OTHER'S BYTE (§15):
// <T>Load is the VARIABLE form's entry point and READS form 1; <T>FixedLoad is
// the form-3 reader and names a form-1 file `previous_form`. The `fixed table`
// KEYWORD ([ir.Struct.FixedDeclared], #823) is the SELECTION POINT for what a
// WRITER emits — a bounded body a variable table declares is the variable wire
// and gets no form-3 surface at all, nothing is derived in either direction
// (§2.2, [ir.TableFixedRoots]) — and it does NOT move the refusal into <T>Load.
// The shared corpus pins `v1_cfg_as_v2` at `read` over a form-1 file of a
// DECLARED fixed root and the C++ reference reads it there through <T>Load, so
// a Go <T>Load that refused would be the one leg disagreeing with the byte
// authority. FixedLoad answers by the form REGISTRY (§3): previous_form for 1,
// message_form_as_file for 2, newer_form for every byte §3 has not assigned.
// The single entry point that would answer both by DISPATCH is §15's follow-on.
package gotable

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/goname"
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
					continue
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

// fixedDeprecatedDefault is the value a DEPRECATED FIELD's slot carries
// (docs/SPEC-TABLES.md §3.4, DEPRECATED SLOT): its declared default, or the
// type's zero when none is declared. Only a scalar leaf whose default rides in
// the const op's eight bytes is answered; every other shape keeps its slot and
// is not a default the const op can land.
func fixedDeprecatedDefault(f *ir.Field) (raw uint64, size int64, ok bool) {
	if !f.Deprecated || f.Type.Optional || f.Array != ir.ArrayNone || f.KeyEnum != "" {
		return 0, 0, false
	}
	size = fixedElementBytes(f)
	if size <= 0 || size > 8 {
		return 0, 0, false
	}
	switch f.Type.Kind {
	case ir.TBool:
		if f.HasDefault && f.DefBool {
			return 1, size, true
		}
		return 0, size, true
	case ir.TFloat32:
		if f.HasDefault {
			return uint64(math.Float32bits(float32(f.DefFloat))), size, true
		}
		return 0, size, true
	case ir.TFloat64:
		if f.HasDefault {
			return math.Float64bits(f.DefFloat), size, true
		}
		return 0, size, true
	case ir.TInt, ir.TBits, ir.TFixed:
		if f.HasDefault && f.DefInt != nil {
			return fixedUint64(f.DefInt), size, true
		}
		return 0, size, true
	case ir.TNamed:
		switch r := f.Type.Ref.(type) {
		case *ir.Enum:
			if f.HasDefault && f.DefVariant != "" {
				for i, name := range r.Variants {
					if name == f.DefVariant {
						return uint64(i + 1), size, true
					}
				}
			}
			return 0, size, true
		case *ir.Flags:
			if f.HasDefault && f.DefInt != nil {
				return fixedUint64(f.DefInt), size, true
			}
			return 0, size, true
		}
	}
	return 0, 0, false
}

// fixedUint64 is v's two's-complement value in the record's 64-bit lane.
func fixedUint64(v *big.Int) uint64 {
	mod := new(big.Int).Lsh(big.NewInt(1), 64)
	return new(big.Int).Mod(v, mod).Uint64()
}

// THE CLOSURE TEST IS ir.TableFixedSupported AND THERE IS ONE OF IT. This file
// used to carry a private copy — the same function byte for byte, with its own
// `16` where ir holds `ir.TableFixedMaxDepth` — and the private copy is what
// SELECTED THE EMITTED FORM. The red-team read of 2026-09-11 found what that
// costs: a fixed table nested 17 to 64 deep rode form 3 out of C++, C, C# and
// Rust and form 1 out of this leg, the same declaration on two incompatible
// wires. A bound a port can hold for itself is a bound the ports can disagree
// about, so the copy is gone and the question is asked where the answer lives.

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

// offGo is a destination over the storage the unit actually holds: in a
// regional unit a plain type nested in a fixed table is its <Name>Row, whose
// layout is not the packet struct's (docs/SPEC-TABLES.md §3.4, A VARIABLE TABLE
// IN THE UNIT CHANGES NOTHING FOR ITS FIXED TABLES); in a plain unit
// fixedStorage is the name itself.
func (g *tableGen) offGo(owner, field string) string {
	return fmt.Sprintf("uint32(unsafe.Offsetof(%s{}.%s))", g.fixedStorage(owner), field)
}

// fixedStorage is the Go type a fixed-form destination names: the <Name>Row in
// a regional unit (storageName), and the name itself in a plain unit.
func (g *tableGen) fixedStorage(n string) string {
	if !g.regional {
		return n
	}
	return g.storageName(n)
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
			dstGo("0", "", g.offGo(owner, member(f)+"Present"), 0, fixedDstArg(0, f.Deprecated)))
		g.fixedWalkPayload(w, owner, f, id, size-fixedPresentBytes)
		return
	}
	g.fixedWalkPayload(w, owner, f, id, size)
}

func (g *tableGen) fixedWalkPayload(w *fixedWalk, owner string, f *ir.Field, id uint64, size int64) {
	switch {
	case f.KeyEnum != "":
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindKeyed, size: size, children: 2, note: f.Name},
			dstGo(g.offGo(owner, member(f)), g.sizeofGo(f), "", 0, fixedDstArg(0, f.Deprecated)))
		g.fixedWalkEnum(w, f.KeyEnumRef, ir.TableWireId(f.KeyEnum), f.KeyEnum, "0", false)
		g.fixedWalkElement(w, f, 0, "element", "0", false)
	case f.Array != ir.ArrayNone:
		counted, aux := 0, ""
		if f.Array == ir.ArrayCounted {
			counted, aux = 1, g.offGo(owner, member(f)+"Count")
		}
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindArray, size: size, children: 1, note: f.Name},
			dstGo(g.offGo(owner, member(f)), g.sizeofGo(f), aux, counted, fixedDstArg(0, f.Deprecated)))
		g.fixedWalkElement(w, f, 0, "element", "0", false)
	case f.Type.Kind == ir.TBytes:
		// `bytes(N)` is an ARRAY of u8 on this wire (ir/fixedform.go, the
		// TBytes row of tableFixedWalkPayload). Dst is the buffer, Aux is the
		// live length, Counted is 1, stride is 1: the same columns a counted
		// array uses. The identity plan still lands the field with the TEXT op
		// and reads neither column; only the compiled path reads these.
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindArray, size: size, children: 1, note: f.Name},
			dstGo(g.offGo(owner, member(f)), "1", g.offGo(owner, member(f)+"Length"), 1, fixedDstArg(3, f.Deprecated)))
		w.push(fixedLayoutEntry{id: 0, kind: ir.TableKindU8, size: 1, children: 0, note: "u8"}, dstGo("0", "", "", 0, 0))
	case f.Type.Kind == ir.TString:
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindString, size: size, children: 0, note: f.Name},
			dstGo(g.offGo(owner, member(f)+"Length"), "", g.offGo(owner, member(f)), 0, fixedDstArg(1, f.Deprecated)))
	default:
		g.fixedWalkElement(w, f, id, f.Name, g.offGo(owner, member(f)), f.Deprecated)
	}
}

func fixedDstArg(arg int, deprecated bool) int {
	if deprecated {
		return arg | 0x80
	}
	return arg
}

func (g *tableGen) fixedWalkElement(w *fixedWalk, f *ir.Field, id uint64, note, dst string, deprecated bool) {
	size := fixedElementBytes(f)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			w.push(fixedLayoutEntry{id: id, kind: ir.TableKindTable, size: size, children: len(r.Fields), note: note}, dstGo(dst, "", "", 0, fixedDstArg(0, deprecated)))
			for _, sub := range r.Fields {
				g.fixedWalkField(w, r.Name, sub)
			}
			return
		case *ir.Union:
			w.push(fixedLayoutEntry{id: id, kind: ir.TableKindUnion, size: size, children: len(r.Variants), note: note},
				dstGo(dst, "", dst+" + "+g.offGo(f.Type.Name, "Type"), 0, fixedDstArg(0, deprecated)))
			for _, v := range r.Variants {
				armID := ir.TableWireId(v.WireName())
				if v.F == nil {
					w.push(fixedLayoutEntry{id: armID, kind: ir.TableKindNoPayload, size: 0, children: 0, note: v.Name},
						dstGo("0", "", "", 0, 0))
					continue
				}
				// AN ARM IS A FIELD LINE (§2.6), so its fixed-form layout is the
				// same field walk with the union as owner and the arm as member:
				// an array arm lands as TableKindArray with its live count as the
				// companion, a text/bytes arm as its kind, a scalar or named arm
				// through the element walk. The arm's storage is the flat Go
				// member, not a nested struct, so the destination terms are
				// member(f) and member(f)+"Count"/"Length".
				g.fixedWalkPayload(w, f.Type.Name, v.F, armID, fixedFieldBytes(v.F))
			}
			return
		case *ir.Enum:
			g.fixedWalkEnum(w, r, id, note, dst, deprecated)
			return
		}
	}
	w.push(fixedLayoutEntry{id: id, kind: ir.TableFixedLeafKind(f), size: size, children: 0, note: note}, dstGo(dst, "", "", 0, fixedDstArg(0, deprecated)))
}

func (g *tableGen) fixedWalkEnum(w *fixedWalk, e *ir.Enum, id uint64, note, dst string, deprecated bool) {
	w.push(fixedLayoutEntry{id: id, kind: ir.TableKindEnum, size: int64(e.StorageBits / 8), children: len(e.Variants), note: note}, dstGo(dst, "", "", 0, fixedDstArg(0, deprecated)))
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

func (g *tableGen) isVarTable(name string) bool {
	return ir.VariableTables(g.unit)[name]
}

// hasFixedForm is whether this table emits FixedLoad/FixedSave. It is a
// question about SHAPE — the closure this form can lay out — and NOT about what
// else the unit holds: a variable table in the unit changes nothing for its
// fixed tables (docs/SPEC-TABLES.md §3.4, A VARIABLE TABLE IN THE UNIT CHANGES
// NOTHING FOR ITS FIXED TABLES). A regional unit's storage is the <Name>Row,
// and every destination below is unsafe.Offsetof/Sizeof over that storage, so
// a string(N)'s N+1 buffer and the Row's padding land where the Row puts them.
// The one shape the Row cannot carry yet is a union (fixedRowHoldsUnion).
func (g *tableGen) hasFixedForm(st *ir.Struct) bool {
	if g.regional && fixedRowHoldsUnion(st, map[*ir.Struct]bool{}) {
		return false
	}
	if !st.IsTable || st.IsMapEntry() || g.isVarTable(st.Name) || !ir.TableFixedSupported(st, 0) {
		return false
	}
	// AND THE RECORD CEILING, the same 65536 TableFixedFormRoots applies
	// (docs/SPEC-TABLES.md §3.4, docs/FIXED-FORM-ALGORITHM.md §5.2): a body past
	// it is one no conforming reader decodes, so emitting a form-3 writer here
	// would produce bytes nobody can read. This leg does not build its roots
	// from ir.TableFixedFormRoots, so the bound is named here too, or a bounded
	// `blob bytes(70000)` — one leaf, inside the leaf cap — rides form 3 out of
	// this leg alone. ir.TableFixedRecordBounds names the table; it keeps form 1.
	if ir.TableFixedTypeBytes(st) > ir.TableFixedRecordMaxBytes {
		return false
	}
	// AND THE WALK'S DEPTH BOUND, asked where the answer lives
	// (ir.TableFixedWithinDepth). This leg does not build its roots from
	// ir.TableFixedFormRoots, so the bound has to be named here or it is not
	// applied at all — which is what the red team found: a table nested 17 deep
	// rode form 3 out of C++ and form 1 out of here, silently.
	if !ir.TableFixedWithinDepth(st) {
		return false
	}
	return g.fixedLeafCount(st) <= fixedLeafCap
}

// fixedRowHoldsUnion is THE NAMED FOLLOW-ON of docs/SPEC-TABLES.md §3.4 (A
// UNION IN A REGIONAL UNIT'S FIXED TABLE): in a regional unit a union is its
// overlay Row (a tag and one payload extent, arms behind accessors), so the
// per-arm destinations this walk names (unsafe.Offsetof(U{}.Arm)) do not exist
// there. A fixed table whose closure reaches a union, directly, through a
// nested table or through an arm, keeps form 1 in a regional unit, decided
// here at compile time and never at run time.
func fixedRowHoldsUnion(st *ir.Struct, seen map[*ir.Struct]bool) bool {
	if seen[st] {
		return false
	}
	seen[st] = true
	for _, f := range st.Fields {
		if f.Type.Kind != ir.TNamed {
			continue
		}
		switch r := f.Type.Ref.(type) {
		case *ir.Union:
			return true
		case *ir.Struct:
			if fixedRowHoldsUnion(r, seen) {
				return true
			}
		}
	}
	return false
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

// emitFixedForm emits the form-3 surface for every qualifying fixed table of
// the file, in a plain unit and a regional one alike (docs/SPEC-TABLES.md §3.4,
// A VARIABLE TABLE IN THE UNIT CHANGES NOTHING FOR ITS FIXED TABLES).
func (g *tableGen) emitFixedForm(members []*ir.Struct) {
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
		g.emitFixedWriteBody(st, false)
		if fixedHasDeprecatedField(st) {
			g.emitFixedWriteBody(st, true)
		}
	}
	for _, st := range order {
		if fixedHasDeprecatedField(st) {
			g.emitFixedDefaultImage(st)
		}
	}
	// THE READ-SIDE BOUNDS, in the same closure order the write bodies take —
	// post-order, so a nested type's body stands before the call to it.
	for _, st := range order {
		g.emitFixedClampBodyFn(st)
	}
	// THE READ-SIDE FINITE PASS, in the same order (docs/SPEC-TABLES.md §3.3):
	// it runs before the bounds pass, and a NaN or an infinity refuses.
	for _, st := range order {
		g.emitFixedFiniteBodyFn(st)
	}
	// THE WRITE-SIDE CONTRACT, in the same order: the checks the read side
	// would answer with a clamp or a refusal, answered here with a refusal.
	for _, st := range order {
		g.emitFixedCheckBodyFn(st)
	}
	for _, st := range roots {
		g.emitFixedClamp(st)
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
				if v.F == nil {
					continue
				}
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
	// Deprecated destinations stay uncovered so the reset image yields their
	// declared defaults for every shape (docs/SPEC-TABLES.md §3.4).
	if f.Deprecated {
		return 0
	}
	per := g.fixedElementLeafCount(f)
	n := per
	switch {
	case f.KeyEnum != "":
		n = int(f.KeyEnumRef.Max) * per
	case f.Array == ir.ArrayFixed:
		n = int(f.ArrayBound) * per
	case f.Array == ir.ArrayCounted:
		n = 1 + int(f.ArrayBound)*per
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
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
				if v.F != nil {
					n += g.fixedFieldLeafCount(v.F)
				}
			}
			return n
		}
	}
	return 1
}

func (g *tableGen) emitFixedLeafWalk(st *ir.Struct) {
	// links is the walk's record of each entry's GUARD CHAIN, innermost first,
	// beside out and indexed alike; tableFixedBuildPlan turns it outermost first
	// and lays it into the pool (docs/FIXED-FORM-ALGORITHM.md §4.1).
	g.pf("func %sFixedLeaves(out []TableFixedEntry, links [][]TableFixedGuardLink, src, dst uint32) int {\n", st.Name)
	g.pf("\tn := 0\n")
	off := int64(0)
	for _, f := range st.Fields {
		// A deprecated field has no source-reading plan entries. The read's
		// prefill supplies the field's reset image (docs/SPEC-TABLES.md §3.4).
		if !f.Deprecated {
			g.emitFixedFieldLeaves(st, f, off)
		}
		off += fixedFieldBytes(f)
	}
	g.pf("\treturn n\n}\n\n")
}

func (g *tableGen) emitFixedFieldLeaves(st *ir.Struct, f *ir.Field, off int64) {
	base := off
	if f.Type.Optional {
		// the PRESENT byte lands in a Go bool, so it is normalised, not copied
		g.pf("\tout[n] = TableFixedEntry{Src: src + %d, Dst: dst + %s, Size: 1, Guard: tableFixedNoGuard, Op: tableFixedBool} // %s present\n",
			base, g.offGo(st.Name, member(f)+"Present"), f.Name)
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
			base, g.offGo(st.Name, member(f)+"Count"), f.ArrayBound, f.Name)
		g.pf("\tn++\n")
		g.emitFixedElementLoop(st, f, base+fixedCountBytes, f.ArrayBound)
	case f.Type.Kind == ir.TString:
		g.emitFixedTextLeaf(st, f, base, f.Type.Size, 1)
	case f.Type.Kind == ir.TBytes:
		g.emitFixedTextLeaf(st, f, base, f.Type.Size, 3)
	default:
		g.pf("\t{\n\t\tes := src + %d\n\t\ted := dst + %s\n", base, g.offGo(st.Name, member(f)))
		g.emitFixedElementLeavesAt(f, "es", "ed", 2)
		g.pf("\t}\n")
	}
}

func (g *tableGen) emitFixedTextLeaf(st *ir.Struct, f *ir.Field, base, units int64, flavour int) {
	g.pf("\tout[n] = TableFixedEntry{Src: src + %d, Dst: dst + %s, Size: %d, Aux: dst + %s, Guard: tableFixedNoGuard, Op: tableFixedText, Meta: %d} // %s\n",
		base, g.offGo(st.Name, member(f)+"Length"), units, g.offGo(st.Name, member(f)), flavour, f.Name)
	g.pf("\tn++\n")
}

func (g *tableGen) emitFixedElementLoop(st *ir.Struct, f *ir.Field, base, count int64) {
	elem := fixedElementBytes(f)
	g.pf("\tfor i := range uint32(%d) {\n", count)
	g.pf("\t\tes := src + %d + i*%d\n", base, elem)
	g.pf("\t\ted := dst + %s + i*%s\n", g.offGo(st.Name, member(f)), g.sizeofGo(f))
	g.emitFixedElementLeavesAt(f, "es", "ed", 2)
	g.pf("\t}\n")
}

func (g *tableGen) sizeofGo(f *ir.Field) string {
	if f.Type.Kind == ir.TNamed {
		switch f.Type.Ref.(type) {
		case *ir.Enum, *ir.Flags:
			return fmt.Sprintf("uint32(unsafe.Sizeof(%s(0)))", f.Type.Name)
		default:
			return fmt.Sprintf("uint32(unsafe.Sizeof(%s{}))", g.fixedStorage(f.Type.Name))
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
			g.pf("%sn += %sFixedLeaves(out[n:], links[n:], %s, %s)\n", ind, r.Name, src, dst)
			return
		case *ir.Union:
			tag := int64(ir.StorageBitsFor(r.Max) / 8)
			// THE TAG IS A COPY. A tag past the arm count is a bound the
			// storage pass holds after the run, over live elements only —
			// never a plan entry, which would cover slack slots of a counted
			// array of unions and count them (docs/SPEC-TABLES.md §3.4).
			g.pf("%sout[n] = TableFixedEntry{Src: %s, Dst: %s + %s, Size: %d, Guard: tableFixedNoGuard, Op: tableFixedCopy} // the tag\n",
				ind, src, dst, g.offGo(f.Type.Name, "Type"), tag)
			g.pf("%sn++\n", ind)
			for i, v := range r.Variants {
				g.pf("%s{ // arm %s, ordinal %d\n%s\tguardAt := n\n", ind, v.Name, i+1, ind)
				if v.F != nil {
					g.emitFixedArmLeaves(v, f.Type.Name, fmt.Sprintf("%s+%d", src, tag), dst, tabs+1)
				}
				// THE GUARD'S WIDTH IS THE TAG'S (§5.2): stamped here, the read
				// loop compares the tag WHOLE and a foreign 0x0101 is not arm 1.
				// AN ARM INSIDE AN ARM ANSWERS TO THE OUTER TAG (§4.1, the guard
				// chain): every entry this arm produced gets ONE MORE LINK, never
				// a lane written over another, so an arm three unions deep keeps
				// all three. The walk records links innermost first (an inner
				// union stamps before this one); tableFixedBuildPlan turns them
				// outermost first, lays the innermost two on the Guard and Guard2
				// lanes, and the whole chain into the pool.
				g.pf("%s\tfor q := guardAt; q < n; q++ {\n", ind)
				g.pf("%s\t\tlinks[q] = append(links[q], TableFixedGuardLink{Guard: %s, Arg: %d, ArgW: %d})\n", ind, src, i+1, tag)
				g.pf("%s\t}\n", ind)
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

// emitFixedArmLeaves is one union arm's plan leaves, the twin of the field walk
// (emitFixedFieldLeaves) with the arm's flat Go storage spellings: an array arm
// puts its live count in member+"Count", a text/bytes arm its used length in
// member+"Length". src is the arm's payload start (past the tag), dst the union
// member's base; the value member is g.offGo(unionName, arm).
func (g *tableGen) emitFixedArmLeaves(v ir.UnionVariant, unionName, src, dst string, tabs int) {
	f := v.F
	if f.Deprecated {
		return
	}
	ind := strings.Repeat("\t", tabs)
	valueDst := dst + " + " + g.offGo(unionName, goname.Member(v.Name))
	switch {
	case f.Array == ir.ArrayFixed:
		g.emitFixedArmLoop(f, src, valueDst, strconv.FormatInt(f.ArrayBound, 10), tabs)
	case f.Array == ir.ArrayCounted:
		g.pf("%sout[n] = TableFixedEntry{Src: %s, Dst: %s, Size: %d, Guard: tableFixedNoGuard, Op: tableFixedCount} // %s count\n",
			ind, src, dst+" + "+g.offGo(unionName, goname.Member(v.Name)+"Count"), f.ArrayBound, v.Name)
		g.pf("%sn++\n", ind)
		g.emitFixedArmLoop(f, src+" + "+strconv.FormatInt(fixedCountBytes, 10), valueDst, strconv.FormatInt(f.ArrayBound, 10), tabs)
	case f.Type.Kind == ir.TString:
		g.emitFixedArmTextLeaf(ind, src, dst, valueDst, unionName, goname.Member(v.Name), f.Type.Size, 1)
	case f.Type.Kind == ir.TBytes:
		g.emitFixedArmTextLeaf(ind, src, dst, valueDst, unionName, goname.Member(v.Name), f.Type.Size, 3)
	default:
		g.emitFixedElementLeavesAt(f, src, valueDst, tabs)
	}
}

// emitFixedArmTextLeaf is a text/bytes arm's single plan entry, the arm's
// member+"Length" companion as Dst and the buffer as Aux (mirror of
// emitFixedTextLeaf).
func (g *tableGen) emitFixedArmTextLeaf(ind, src, dst, valueDst, unionName, arm string, units int64, flavour int) {
	g.pf("%sout[n] = TableFixedEntry{Src: %s, Dst: %s, Size: %d, Aux: %s, Guard: tableFixedNoGuard, Op: tableFixedText, Meta: %d} // %s\n",
		ind, src, dst+" + "+g.offGo(unionName, arm+"Length"), units, valueDst, flavour, arm)
	g.pf("%sn++\n", ind)
}

// emitFixedArmLoop is an array arm's element loop, mirroring
// emitFixedElementLoop with the arm's already-relative src and dst strings.
func (g *tableGen) emitFixedArmLoop(f *ir.Field, src, dst, count string, tabs int) {
	ind := strings.Repeat("\t", tabs)
	elem := fixedElementBytes(f)
	g.pf("%sfor i := range uint32(%s) {\n", ind, count)
	g.pf("%s\tes := %s + i*%d\n", ind, src, elem)
	g.pf("%s\ted := %s + i*%s\n", ind, dst, g.sizeofGo(f))
	g.emitFixedElementLeavesAt(f, "es", "ed", tabs+2)
	g.pf("%s}\n", ind)
}

func fixedHasDeprecatedField(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if f.Deprecated {
			return true
		}
	}
	return false
}

func (g *tableGen) emitFixedDefaultImage(st *ir.Struct) {
	g.pf("var %sFixedDefaultBody = func() []byte {\n", st.Name)
	g.pf("\tvar value %s\n\t%sReset(&value)\n", g.fixedStorage(st.Name), st.Name)
	g.pf("\tbody := make([]byte, %d)\n", fixedTypeBytes(st))
	g.pf("\t%sFixedWriteDefaultBody(body, &value)\n\treturn body\n}()\n\n", st.Name)
}

func (g *tableGen) emitFixedWriteBody(st *ir.Struct, defaultMode bool) {
	fn := st.Name + "FixedWriteBody"
	if defaultMode {
		fn = st.Name + "FixedWriteDefaultBody"
	}
	g.pf("func %s(b []byte, value *%s) {\n", fn, g.fixedStorage(st.Name))
	off := int64(0)
	if len(st.Fields) == 0 {
		g.pf("\t_, _ = b, value\n")
	}
	for _, f := range st.Fields {
		if f.Deprecated && !defaultMode {
			end := off + fixedFieldBytes(f)
			g.pf("\tcopy(b[%d:%d], %sFixedDefaultBody[%d:%d])\n", off, end, st.Name, off, end)
		} else {
			g.emitFixedWriteField(f, off, "b", "value", 0, 1, defaultMode)
		}
		off += fixedFieldBytes(f)
	}
	g.pf("}\n\n")
}

func (g *tableGen) emitFixedWriteField(f *ir.Field, off int64, buf, val string, depth, tabs int, defaultMode bool) {
	ind := strings.Repeat("\t", tabs)
	if f.Type.Optional {
		g.pf("%s{\n%s\tp := uint8(0)\n%s\tif %s.%sPresent {\n%s\t\tp = 1\n%s\t}\n%s\ttableFixedPut8(%s[%d:], p)\n%s}\n",
			ind, ind, ind, val, member(f), ind, ind, ind, buf, off, ind)
		// AN ABSENT OPTIONAL'S PAYLOAD IS THE TEMPLATE'S ZEROS
		// (docs/FIXED-FORM-ALGORITHM.md §3.1, fix 13): the payload rides WHOLE
		// whether or not it is present, and when the flag is 0 what rides is
		// zero. It is ONE `if` here rather than a rule anywhere else, because
		// the template already put the zeros there — so an absent optional
		// costs the writer the branch and not one store, and a caller's
		// untouched payload storage never reaches the wire.
		g.pf("%sif %s.%sPresent {\n", ind, val, member(f))
		g.emitFixedWritePayload(f, off+fixedPresentBytes, buf, val, depth, tabs+1, defaultMode)
		g.pf("%s}\n", ind)
		return
	}
	g.emitFixedWritePayload(f, off, buf, val, depth, tabs, defaultMode)
}

func (g *tableGen) emitFixedWritePayload(f *ir.Field, off int64, buf, val string, depth, tabs int, defaultMode bool) {
	ind := strings.Repeat("\t", tabs)
	base := off
	dest := fmt.Sprintf("%s[%d:]", buf, base)
	// A DEPRECATED FIELD'S SLOT CARRIES ITS DECLARED DEFAULT whatever the value
	// holds (docs/SPEC-TABLES.md §3.4, DEPRECATED SLOT): nothing writes a
	// retired field, so the value is never the source of those bytes.
	if raw, size, ok := fixedDeprecatedDefault(f); ok {
		g.emitFixedWriteConst(ind, dest, size, raw)
		return
	}
	switch {
	case f.KeyEnum != "":
		g.emitFixedWriteLoop(f, dest, f.KeyEnumRef.Max, val+"."+member(f), depth, tabs, defaultMode)
	case f.Array == ir.ArrayFixed:
		g.emitFixedWriteLoop(f, dest, f.ArrayBound, val+"."+member(f), depth, tabs, defaultMode)
	case f.Array == ir.ArrayCounted:
		g.pf("%stableFixedPut32(%s, uint32(%s.%sCount))\n", ind, dest, val, member(f))
		g.emitFixedWriteLive(f, fmt.Sprintf("%s[%d:]", buf, base+fixedCountBytes), fmt.Sprintf("%s.%sCount", val, member(f)), val+"."+member(f), depth, tabs, defaultMode)
	case f.Type.Kind == ir.TString:
		g.pf("%stableFixedPut32(%s, uint32(%s.%sLength))\n", ind, dest, val, member(f))
		g.pf("%scopy(%s[4:], %s.%s[:min(uint32(%s.%sLength), %d)])\n", ind, dest, val, member(f), val, member(f), f.Type.Size)
	case f.Type.Kind == ir.TBytes:
		g.pf("%stableFixedPut32(%s, uint32(%s.%sLength))\n", ind, dest, val, member(f))
		g.pf("%scopy(%s[4:], %s.%s[:min(uint32(%s.%sLength), %d)])\n", ind, dest, val, member(f), val, member(f), f.Type.Size)
	default:
		g.emitFixedWriteElement(f, dest, val+"."+member(f), depth, tabs, defaultMode)
	}
}

func (g *tableGen) emitFixedWriteLoop(f *ir.Field, dest string, count int64, expr string, depth, tabs int, defaultMode bool) {
	ind := strings.Repeat("\t", tabs)
	elem := fixedElementBytes(f)
	loop := fixedLoopVar(depth)
	g.pf("%sfor %s := range int64(%d) {\n", ind, loop, count)
	g.emitFixedWriteElement(f, fmt.Sprintf("%s[%s*%d:]", dest, loop, elem), expr+"["+loop+"]", depth+1, tabs+1, defaultMode)
	g.pf("%s}\n", ind)
}

// fixedOwnsSlots is whether a counted array's write zeroes its own slots: an
// array of structs or unions, whose element slots carry slack of their own. Its
// slots are zeroed one at a time just before each element's stores, so the
// slot is hot in cache when they land (the speed gate measured the difference
// against one template-wide zero), and the dead slots past the count in one
// clear. An array of scalars is stored whole by its live puts and keeps its
// dead slots in the template.
func fixedOwnsSlots(f *ir.Field) bool {
	if f.Array != ir.ArrayCounted || f.Type.Kind != ir.TNamed {
		return false
	}
	switch f.Type.Ref.(type) {
	case *ir.Struct, *ir.Union:
		return true
	}
	return false
}

// emitFixedWriteLive stores a counted array's LIVE extent
// (docs/FIXED-FORM-ALGORITHM.md §3.1: "Arrays write `count` elements ...
// never all `Max`"); an array that owns its slots (fixedOwnsSlots) zeroes
// them too. The count is clamped to 0..Max here only so a caller of the write
// body itself can never index past the array; <T>FixedSave has already
// refused a count outside it.
func (g *tableGen) emitFixedWriteLive(f *ir.Field, dest, count, expr string, depth, tabs int, defaultMode bool) {
	ind := strings.Repeat("\t", tabs)
	elem := fixedElementBytes(f)
	loop := fixedLoopVar(depth)
	live := "live" + loop
	slot := fixedOwnsSlots(f)
	g.pf("%s{\n", ind)
	g.pf("%s\t%s := min(max(int64(%s), 0), %d)\n", ind, live, count, f.ArrayBound)
	g.pf("%s\tfor %s := range %s {\n", ind, loop, live)
	if slot {
		g.pf("%s\t\tclear(%s[%s*%d : %s*%d+%d])\n", ind, dest, loop, elem, loop, elem, elem)
	}
	g.emitFixedWriteElement(f, fmt.Sprintf("%s[%s*%d:]", dest, loop, elem), expr+"["+loop+"]", depth+1, tabs+2, defaultMode)
	g.pf("%s\t}\n", ind)
	if slot {
		g.pf("%s\tclear(%s[%s*%d : %d])\n", ind, dest, live, elem, f.ArrayBound*elem)
	}
	g.pf("%s}\n", ind)
}

// fixedTemplateRanges is the record body's template-zero ranges for the root
// save: every byte except the root's own counted arrays that own their slots
// (fixedOwnsSlots), which emitFixedWriteLive zeroes as it stores them, so the
// save does not zero them twice. One under an optional stays inside the
// template, because an absent payload is never stored.
func fixedTemplateRanges(st *ir.Struct) [][2]int64 {
	var out [][2]int64
	at, from := int64(0), int64(0)
	for _, f := range st.Fields {
		n := fixedFieldBytes(f)
		if fixedOwnsSlots(f) && !f.Type.Optional {
			if at > from {
				out = append(out, [2]int64{from, at})
			}
			from = at + n
		}
		at += n
	}
	if at > from {
		out = append(out, [2]int64{from, at})
	}
	return out
}

func (g *tableGen) emitFixedWriteElement(f *ir.Field, dest, expr string, depth, tabs int, defaultMode bool) {
	ind := strings.Repeat("\t", tabs)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			// Default images start from the type's Reset value. A nested
			// normal writer therefore emits its live defaults and substitutes
			// any deprecated slots from its own default image.
			g.pf("%s%sFixedWriteBody(%s, &%s)\n", ind, r.Name, dest, expr)
			return
		case *ir.Union:
			tag := int64(ir.StorageBitsFor(r.Max) / 8)
			g.emitFixedPutInt(ind, dest, tag, expr+".Type")
			g.pf("%sswitch %s.Type {\n", ind, expr)
			for _, v := range r.Variants {
				if v.F == nil {
					continue
				}
				g.pf("%scase %sType%s:\n", ind, f.Type.Name, ir.GoExportName(v.Name))
				g.emitFixedWriteArm(v, fmt.Sprintf("%s[%d:]", dest, tag), expr+"."+goname.Member(v.Name), depth, tabs+1, defaultMode)
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

// emitFixedWriteArm is one union arm's fixed-form store, the twin of
// emitFixedWritePayload with the arm's flat Go storage spellings: the arm's
// value is the member expr, its live count in arm+"Count" and its used length
// in arm+"Length". A payload-free arm writes nothing under the tag (§2.6).
func (g *tableGen) emitFixedWriteArm(v ir.UnionVariant, dest, arm string, depth, tabs int, defaultMode bool) {
	f := v.F
	ind := strings.Repeat("\t", tabs)
	if f.Deprecated && !defaultMode {
		if raw, size, ok := fixedDeprecatedDefault(f); ok {
			g.emitFixedWriteConst(ind, dest, size, raw)
		}
		return
	}
	switch {
	case f.Array == ir.ArrayFixed:
		g.emitFixedWriteLoop(f, dest, f.ArrayBound, arm, depth, tabs, defaultMode)
	case f.Array == ir.ArrayCounted:
		g.pf("%stableFixedPut32(%s, uint32(%sCount))\n", ind, dest, arm)
		g.emitFixedWriteLive(f, fmt.Sprintf("%s[%d:]", dest, fixedCountBytes), arm+"Count", arm, depth, tabs, defaultMode)
	case f.Type.Kind == ir.TString:
		g.pf("%stableFixedPut32(%s, uint32(%sLength))\n", ind, dest, arm)
		g.pf("%scopy(%s[4:], %s[:min(uint32(%sLength), %d)])\n", ind, dest, arm, arm, f.Type.Size)
	case f.Type.Kind == ir.TBytes:
		g.pf("%stableFixedPut32(%s, uint32(%sLength))\n", ind, dest, arm)
		g.pf("%scopy(%s[4:], %s[:min(uint32(%sLength), %d)])\n", ind, dest, arm, arm, f.Type.Size)
	default:
		g.emitFixedWriteElement(f, dest, arm, depth, tabs, defaultMode)
	}
}

func fixedLoopVar(depth int) string {
	if depth == 0 {
		return "i"
	}
	return fmt.Sprintf("i%d", depth)
}

// emitFixedWriteConst stores a DEPRECATED SLOT's declared default
// (docs/SPEC-TABLES.md §3.4, DEPRECATED SLOT) at the field's own width.
func (g *tableGen) emitFixedWriteConst(ind, dest string, width int64, raw uint64) {
	switch width {
	case 1:
		g.pf("%stableFixedPut8(%s, %d)\n", ind, dest, uint8(raw))
	case 2:
		g.pf("%stableFixedPut16(%s, %d)\n", ind, dest, uint16(raw))
	case 4:
		g.pf("%stableFixedPut32(%s, %d)\n", ind, dest, uint32(raw))
	case 8:
		g.pf("%stableFixedPut64(%s, %d)\n", ind, dest, raw)
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
	hash := ir.TableFixedLayoutHash(layout, st)
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

	// THE LINEAGE, OLDEST FIRST, THE CURRENT LAYOUT LAST (§5.2). It is the
	// build's data and never the wire's: a file is matched on its hash against
	// these entries, and the layout it carries is COMPARED with the bytes the
	// lock recorded, never walked. The floor is 1 + the highest retired index.
	entries, floor := g.fixedLineage(st, FixedLineageEntry{Wire: hash, Layout: layout, Record: 8 + body})
	g.pf("var %sFixedKnown = []TableFixedKnownLayout{\n", st.Name)
	for _, e := range entries {
		g.pf("\t{\n\t\tHash:   0x%016x,\n", e.Wire)
		if e.Retired {
			g.pf("\t\t// RETIRED: %s\n", e.Reason)
		}
		g.pf("\t\tLayoutBytes: %d,\n\t\tRecord: %d,\n\t\tLayout: []byte{\n", len(e.Layout), e.Record)
		g.emitByteArray(e.Layout)
		g.pf("\t\t},\n\t},\n")
	}
	g.pf("}\n\n")
	g.pf("// The floor: below it a layout this build once served is retired, and\n")
	g.pf("// the answer is layout_unsupported rather than layout_unknown (§5.2).\n")
	g.pf("const %sFixedFloor = %d\n\n", st.Name, floor)
	g.pf("// One plan per lineage entry, laid down from THE LOCK's bytes: nothing\n")
	g.pf("// compiles on the load path (§5.8 row 3).\n")
	g.pf("var %sFixedLineagePlans = tableFixedLineagePlans(%sFixedKnown, %sFixedLayout, %sFixedDst, %sFixedHash)\n\n",
		st.Name, st.Name, st.Name, st.Name, st.Name)

	// THE PREFILL IS LAID ONCE (§4.3): the hole list and the default image are
	// per plan and computed once, at init, never per call. A load path that
	// recomputed either would allocate on every call with records.
	g.pf("var %sFixedResetImage = func() []byte {\n", st.Name)
	g.pf("\tvar def %s\n", st.Name)
	g.pf("\t%sReset(&def)\n", st.Name)
	g.pf("\treturn tableFixedOverlay(unsafe.Pointer(&def), unsafe.Sizeof(def))\n")
	g.pf("}()\n\n")
	g.pf("var %sFixedHoles = tableFixedHoles(%sFixedPlan.Entries, %sFixedPlan.Count, uint32(len(%sFixedResetImage)))\n\n",
		st.Name, st.Name, st.Name, st.Name)
	g.pf("var %sFixedLineageHoles = func() [][]tableFixedHole {\n", st.Name)
	g.pf("\tout := make([][]tableFixedHole, len(%sFixedLineagePlans))\n", st.Name)
	g.pf("\tfor i := range %sFixedLineagePlans {\n", st.Name)
	g.pf("\t\tlane := &%sFixedLineagePlans[i]\n", st.Name)
	g.pf("\t\tif lane.Why == \"\" {\n")
	g.pf("\t\t\tout[i] = tableFixedHoles(lane.Entries, lane.Count, uint32(len(%sFixedResetImage)))\n", st.Name)
	g.pf("\t\t}\n")
	g.pf("\t}\n")
	g.pf("\treturn out\n")
	g.pf("}()\n\n")

	// THE CAPACITY A CALLER WOULD GUESS IS EXPORTED INSTEAD (§5.2, "AND THE
	// CAPACITY A CALLER WOULD GUESS IS EXPORTED INSTEAD"): the largest entry
	// count over the identity plan and every lineage plan, so a plan slice of
	// this length never refuses plan_too_large for a layout the lineage holds
	// — and one entry fewer refuses the largest plan by that same name. It is
	// the maximum over the BUILT plans and not a generate-time literal, because
	// the identity plan's coalesced count depends on the Go struct's own
	// offsets (unsafe.Offsetof, padding): the exact number exists first at
	// package initialization, and any larger guess is the guessing this name
	// removes. A retired entry's plan is never built, so its zero Count
	// contributes nothing to the maximum — §5.2's plan-cap cut.
	g.pf("// %sFixedPlanMax is the plan capacity every layout this build reads needs:\n", st.Name)
	g.pf("// the largest entry count over the identity plan and every lineage plan\n")
	g.pf("// (docs/FIXED-FORM-ALGORITHM.md §5.2). A plan slice of this length never\n")
	g.pf("// refuses plan_too_large for a layout the lineage holds.\n")
	g.pf("var %sFixedPlanMax = func() int32 {\n", st.Name)
	g.pf("\tm := %sFixedPlan.Count\n", st.Name)
	g.pf("\tfor _, lane := range %sFixedLineagePlans {\n", st.Name)
	g.pf("\t\tif lane.Count > m {\n\t\t\tm = lane.Count\n\t\t}\n")
	g.pf("\t}\n")
	g.pf("\treturn m\n")
	g.pf("}()\n\n")

	g.pf("// A FILE: form byte, seven reserved zeros, the LAYOUT HASH at 8, body at 16,\n")
	g.pf("// then the layout behind its u32 length, then the records to the end of it.\n")
	g.pf("func %sFixedMeasure(count int64) int64 {\n", st.Name)
	// THE INTEGRITY TRAILER is counted (docs/SPEC-TABLES.md §2.14, §3.4 THE
	// FRAMING): four bytes after the last record, only when the table declares it.
	trailer := ""
	if st.Integrity != "" {
		trailer = " + 4"
	}
	g.pf("\treturn TableFixedHeaderBytes + 4 + int64(len(%sFixedLayout)) + count*%sFixedRecordBytes%s\n}\n\n", st.Name, st.Name, trailer)

	// THE WRITE-SIDE CONTRACT (docs/FIXED-FORM-ALGORITHM.md §3.1, docs/USAGE.md
	// "Writes are the caller's responsibility"): Go has no debug-only idiom, so
	// FixedSave REFUSES in every build — a count past Max, a length past N, a
	// value off its min/max, an ordinal past its set, ill-formed text — with -1,
	// and FixedSaveReason names it TableRefuseInvalidValue. The whole batch is
	// checked before the first byte is stored, so a refused batch leaves the
	// buffer untouched; nothing is clamped.
	g.pf("// %sFixedSave is the fixed form's writer: it returns the bytes written, or -1 for a\n", st.Name)
	g.pf("// buffer too short or a value outside its declared contract (§3.1); see FixedSaveReason.\n")
	g.pf("func %sFixedSave(values []%s, buffer []byte) int64 {\n", st.Name, st.Name)
	g.pf("\tneed := %sFixedMeasure(int64(len(values)))\n", st.Name)
	g.pf("\tif int64(len(buffer)) < need {\n\t\treturn -1\n\t}\n")
	if fixedCheckNeededStruct(st, map[string]bool{}) {
		g.pf("\tfor k := range values {\n\t\tif !%sFixedCheckBody(&values[k]) {\n\t\t\treturn -1\n\t\t}\n\t}\n", st.Name)
	}
	g.pf("\tclear(buffer[:TableFixedHeaderBytes])\n")
	g.pf("\tbuffer[0] = TableFixedForm\n")
	g.pf("\ttableFixedPut64(buffer[TableFixedHashAt:], %sFixedHash)\n", st.Name)
	g.pf("\ttableFixedPut32(buffer[TableFixedHeaderBytes:], uint32(len(%sFixedLayout)))\n", st.Name)
	g.pf("\tcopy(buffer[TableFixedHeaderBytes+4:], %sFixedLayout)\n", st.Name)
	g.pf("\tat := buffer[TableFixedHeaderBytes+4+len(%sFixedLayout):]\n", st.Name)
	g.pf("\tfor k := range values {\n")
	g.pf("\t\ttableFixedPut64(at, %sFixedHash)\n", st.Name)
	if ranges := fixedTemplateRanges(st); len(ranges) == 1 && ranges[0] == [2]int64{0, fixedTypeBytes(st)} {
		g.pf("\t\tclear(at[8 : 8+%sFixedBodyBytes])\n", st.Name)
	} else {
		for _, r := range ranges {
			g.pf("\t\tclear(at[%d:%d])\n", 8+r[0], 8+r[1])
		}
	}
	g.pf("\t\t%sFixedWriteBody(at[8:], &values[k])\n", st.Name)
	g.pf("\t\tat = at[%sFixedRecordBytes:]\n", st.Name)
	g.pf("\t}\n")
	if st.Integrity != "" {
		// THE SEAL (docs/SPEC-TABLES.md §2.14, §3.4 THE FRAMING): the CRC-32C of
		// every byte before it, through the variable form's tableIntegritySeal.
		g.pf("\treturn tableIntegritySeal(buffer, need-4)\n}\n\n")
	} else {
		g.pf("\treturn need\n}\n\n")
	}

	// THE NAMED TWIN: -1 is two refusals, and the reason says which. A buffer
	// short of the measure is TableRefuseTruncated (the caller's sizing), and a
	// value outside its contract is TableRefuseInvalidValue (the caller's data).
	g.pf("func %sFixedSaveReason(values []%s, buffer []byte) (int64, error) {\n", st.Name, st.Name)
	g.pf("\tif n := %sFixedSave(values, buffer); n >= 0 {\n\t\treturn n, nil\n\t}\n", st.Name)
	g.pf("\tif int64(len(buffer)) < %sFixedMeasure(int64(len(values))) {\n\t\treturn -1, TableRefuseTruncated\n\t}\n", st.Name)
	g.pf("\treturn -1, TableRefuseInvalidValue\n}\n\n")

	// THE PLAIN NAME IS THE STRICT READ, THE TOLERANT ONE IS NAMED (docs/SPEC-TABLES.md §6.5).
	g.pf("// %sFixedLoad is the plain fixed-form read, and it is STRICT (docs/SPEC-TABLES.md §6.5): -1 for\n", st.Name)
	g.pf("// every refusal of %sFixedLoadTolerant and for a read whose report.Err() is not nil, with\n", st.Name)
	g.pf("// every value it had written reset. A supplied report receives THIS read's report.\n")
	g.pf("func %sFixedLoad(values []%s, data []byte, plan []TableFixedEntry, report *TableReport) int64 {\n", st.Name, st.Name)
	g.pf("\tread := report\n\tif read == nil {\n\t\tread = &TableReport{}\n\t} else {\n\t\t*read = TableReport{}\n\t}\n\tn := %sFixedLoadTolerant(values, data, plan, read)\n", st.Name)
	g.pf("\tif n < 0 {\n\t\treturn -1\n\t}\n\tif read.Err() != nil {\n\t\tfor k := range n {\n\t\t\t%sReset(&values[k])\n\t\t}\n\t\treturn -1\n\t}\n\treturn n\n}\n\n", st.Name)
	g.pf("// %sFixedLoadTolerant is the TOLERANT fixed-form read (docs/SPEC-TABLES.md §6.5): it keeps\n", st.Name)
	g.pf("// every recovered record, with the counters counted onto the report passed.\n")
	g.pf("func %sFixedLoadTolerant(values []%s, data []byte, plan []TableFixedEntry, report *TableReport) int64 {\n", st.Name, st.Name)
	g.pf("\tif report == nil {\n\t\tvar local TableReport\n\t\treport = &local\n\t}\n")
	// A LOAD'S REPORT IS ITS OWN (§5.3's joint table): a report a caller reuses
	// must not carry a previous read's counters or a previous refusal's name.
	g.pf("\t*report = TableReport{}\n")
	// EVERY REFUSAL OF THE LOAD HAS A NAME, THE FRAMING-DAMAGE RETURNS INCLUDED
	// (docs/FIXED-FORM-ALGORITHM.md §5.3, docs/SPEC-TABLES.md §3.4): each leaves
	// report.Reason set to its exported constant — TableRefuseTruncated.Error() or
	// TableRefuseReservedNotZero.Error() for the four damage returns below, which
	// keep report.Malformed set beside the name.
	// STEP 1: only whether there IS a first byte. THE FORM BYTE IS READ BEFORE
	// THE FILE'S LENGTH — a committed form-1 file is TEN bytes and a form-2
	// batch THREE, so a reader that measured first would answer `malformed` for
	// every real file of the two forms it is supposed to name (§5.3).
	g.pf("\tif len(data) < 1 {\n\t\treport.Malformed = true\n\t\treport.Reason = TableRefuseTruncated.Error()\n\t\treturn -1\n\t}\n")
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
	g.pf("\t\tcase 1: // the VARIABLE form, and the only form this one is newer than\n\t\t\treturn tableFixedRefuse(report, TableRefusePreviousForm.Error())\n")
	g.pf("\t\tcase 2: // the MESSAGE form: a form this build carries, through another surface\n\t\t\treturn tableFixedRefuse(report, TableRefuseMessageFormAsFile.Error())\n")
	g.pf("\t\t}\n")
	g.pf("\t\treturn tableFixedRefuse(report, TableRefuseNewerForm.Error())\n\t}\n")
	// THE INTEGRITY TRAILER, AFTER THE FORM BYTE AND BEFORE ANYTHING ELSE
	// (docs/SPEC-TABLES.md §2.14, §3.4 THE FRAMING): a mismatch is whole-file
	// damage named integrity_mismatch, the variable form's report from
	// tableIntegrityOpen, with no layout compared and no record touched; then
	// every rule below reads the file without its trailer.
	if st.Integrity != "" {
		g.pf("\tif !tableIntegrityHolds(data) {\n\t\treport.Verdict = TableOpenDamaged\n\t\treport.Malformed = true\n\t\treport.Reason = \"integrity_mismatch\"\n\t\treturn -1\n\t}\n")
		g.pf("\tdata = data[:len(data)-4]\n")
	}
	// STEP 2: the twenty-byte minimum, and then the SEVEN RESERVED BYTES, which
	// are REFUSED and not ignored so they stay spendable later (§3.4, §5.3).
	g.pf("\tif len(data) < TableFixedHeaderBytes+4 {\n\t\treport.Malformed = true\n\t\treport.Reason = TableRefuseTruncated.Error()\n\t\treturn -1\n\t}\n")
	g.pf("\tfor _, reserved := range data[1:TableFixedHashAt] {\n\t\tif reserved != 0 {\n\t\t\treport.Malformed = true\n\t\t\treport.Reason = TableRefuseReservedNotZero.Error()\n\t\t\treturn -1\n\t\t}\n\t}\n")
	// STEP 3: the layout's length, and the bytes behind it.
	g.pf("\tlayoutBytes := tableFixedGet32(data[TableFixedHeaderBytes:])\n")
	g.pf("\tif int64(layoutBytes)+TableFixedHeaderBytes+4 > int64(len(data)) {\n\t\treturn tableFixedRefuse(report, TableRefuseLayoutMalformed.Error())\n\t}\n")
	g.pf("\tlayout := data[TableFixedHeaderBytes+4 : TableFixedHeaderBytes+4+layoutBytes]\n")
	// STEP 4: THE HEADER'S HASH, TAKEN AS GIVEN. Nothing is recomputed from the
	// wire: the definitions digest is not on the wire, so the hash cannot be
	// re-derived from a file at all (§5.3).
	g.pf("\thash := tableFixedGet64(data[TableFixedHashAt:])\n")
	// STEP 5 and STEP 6: SELECT BY HASH, then the floor. Outside the lineage is
	// layout_unknown (§5.3 step 5, SPEC-TABLES §21.3): the lineage is oldest
	// first with this build's own layout last, a retired mark means older and the
	// wire carries a digest with no ordinal, so nothing here PROVES a hash
	// newer and layout_newer is reserved, never emitted. Below the floor is
	// layout_unsupported — upgrade the client. Both report THE FILE'S hash and
	// nothing else.
	g.pf("\tpick := tableFixedSelect(%sFixedKnown, hash)\n", st.Name)
	g.pf("\tif pick < 0 {\n\t\treturn tableFixedRefuseHash(report, TableRefuseLayoutUnknown.Error(), hash)\n\t}\n")
	g.pf("\tif pick < %sFixedFloor {\n\t\treturn tableFixedRefuseHash(report, TableRefuseLayoutUnsupported.Error(), hash)\n\t}\n", st.Name)
	// STEP 7: a known hash is read under THE LOCK'S layout bytes. A difference
	// is one name — a lie about a known version — and the seven §1.1 rules do
	// not run at read time at all: no stranger's layout is ever walked.
	g.pf("\tknown := &%sFixedKnown[pick]\n", st.Name)
	g.pf("\tif int(layoutBytes) != len(known.Layout) {\n\t\treturn tableFixedRefuse(report, TableRefuseLayoutMalformed.Error())\n\t}\n")
	g.pf("\tfor i := range layout {\n\t\tif layout[i] != known.Layout[i] {\n\t\t\treturn tableFixedRefuse(report, TableRefuseLayoutMalformed.Error())\n\t\t}\n\t}\n")
	// STEP 8: the plan this endpoint resolves to, and the record size FROM THE LOCK.
	g.pf("\tentries := %sFixedPlan.Entries\n", st.Name)
	g.pf("\tentryCount := %sFixedPlan.Count\n", st.Name)
	g.pf("\t// THE PREFILL'S HOLES ARE LAID ONCE (§4.3): the identity plan's list, then\n")
	g.pf("\t// a lineage plan's when an endpoint selects one. Nothing computes them per call.\n")
	g.pf("\tholes := %sFixedHoles\n", st.Name)
	g.pf("\trecordBytes := known.Record\n")
	g.pf("\tcensusUnknown, censusKind := int32(0), int32(0)\n")
	g.pf("\tif hash != %sFixedHash {\n", st.Name)
	g.pf("\t\tlane := &%sFixedLineagePlans[pick]\n", st.Name)
	g.pf("\t\tif lane.Why != \"\" {\n\t\t\treturn tableFixedRefuse(report, lane.Why)\n\t\t}\n")
	g.pf("\t\tentries, entryCount = lane.Entries, lane.Count\n")
	g.pf("\t\tholes = %sFixedLineageHoles[pick]\n", st.Name)
	g.pf("\t\t// THE CENSUS IS ONCE PER ENDPOINT and never per record (§5.4), and it\n")
	g.pf("\t\t// lands only on a read that returns: REFUSE moves no counter.\n")
	g.pf("\t\tcensusUnknown, censusKind = lane.Unknown, lane.KindMismatch\n")
	g.pf("\t}\n")
	// THE SELECTED PLAN'S ENTRY COUNT IS CHECKED EITHER WAY (§5.9 #5, #45):
	// the identity lane is a plan too, and a caller's short plan slice owes the
	// same name there as it does for a lineage entry.
	g.pf("\tif int64(entryCount) > int64(len(plan)) {\n\t\treturn tableFixedRefuse(report, TableRefusePlanTooLarge.Error())\n\t}\n")
	// STEP 9 and STEP 10.
	g.pf("\tat := data[TableFixedHeaderBytes+4+layoutBytes:]\n")
	g.pf("\trest := int64(len(data)) - TableFixedHeaderBytes - 4 - int64(layoutBytes)\n")
	g.pf("\tif recordBytes <= 8 || rest%%recordBytes != 0 {\n\t\treport.Malformed = true\n\t\treport.Reason = TableRefuseTruncated.Error()\n\t\treturn -1\n\t}\n")
	g.pf("\tn := rest / recordBytes\n")
	g.pf("\tif n > int64(len(values)) {\n\t\treturn tableFixedRefuse(report, TableRefuseBatchTooLarge.Error())\n\t}\n")
	// Prefill is the dest ranges the winning plan does not land, copied from
	// one Reset image laid once at init (§4.3). Identity's list on Go is the
	// ABI padding and unselected arms. There is no identity flag in the loop.
	g.pf("\tdefBytes := %sFixedResetImage\n", st.Name)
	// STEP 11: EVERY RECORD'S HASH AND TEXT FIRST, in a pass of its own ahead
	// of the first prefill. REFUSE is total over the batch too: a refusal at
	// record k leaves out[0..k-1] as untouched as out[k], so a hostile image is
	// never partly landed, clamped or zero-filled
	// (docs/FIXED-FORM-ALGORITHM.md §5.3, "TOTAL OVER THE BATCH, TOO"; §5.8
	// row 9). The text content is the §4.5 fix 11 refusal, checked here for
	// the same reason.
	g.pf("\tfor k, scan := int64(0), at; k < n; k, scan = k+1, scan[recordBytes:] {\n")
	g.pf("\t\tif tableFixedGet64(scan) != hash {\n\t\t\treturn tableFixedRefuse(report, TableRefuseNoLayout.Error())\n\t\t}\n")
	g.pf("\t\tif why := tableFixedTextRefusal(entries, entryCount, scan[8:recordBytes]); why != \"\" {\n")
	g.pf("\t\t\treturn tableFixedRefuse(report, why)\n")
	g.pf("\t\t}\n")
	g.pf("\t}\n")
	g.pf("\tfor k := range n {\n")
	g.pf("\t\tdst := tableFixedOverlay(unsafe.Pointer(&values[k]), unsafe.Sizeof(values[k]))\n")
	g.pf("\t\tfor i := range holes {\n")
	g.pf("\t\t\th := holes[i]\n")
	g.pf("\t\t\tcopy(dst[h.Off:h.Off+h.Size], defBytes[h.Off:h.Off+h.Size])\n")
	g.pf("\t\t}\n")
	// THE RUN SEES ONE RECORD (§5.2). Handed `at[8:]` it saw every byte to the
	// END OF THE FILE, so an entry past the writer's record read the NEXT
	// record's bytes instead of being caught.
	g.pf("\t\tif why := tableFixedRun(entries, entryCount, at[8:recordBytes], dst, report); why != \"\" {\n")
	g.pf("\t\t\treturn tableFixedRefuse(report, why)\n")
	g.pf("\t\t}\n")
	// AND THE FORGED ORDINAL, in the BOUNDS pass and not in the `ordinal`/`const`
	// op (§5.4, fix 27): the op lands None and moves nothing, and this pass reads
	// the writer's count the plan carries — the remap table's length word, or an
	// unguarded const's DstSize — which the storage clamp cannot see. Silent for
	// the identity plan, whose entries are `copy`.
	if fixedFiniteNeededStruct(st, map[string]bool{}) {
		// A FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3): the finite pass runs
		// FIRST, before the bounds pass, over the storage the run just wrote,
		// and a NaN or an infinity refuses the whole batch as
		// non_finite_float, the tolerant read included. Nothing of the batch
		// is kept: every record this call wrote goes back to its defaults,
		// as the strict read's refusals leave them.
		g.pf("\t\tif why := %sFixedFiniteBody(&values[k]); why != \"\" {\n", st.Name)
		g.pf("\t\t\tfor j := range k + 1 {\n\t\t\t\t%sReset(&values[j])\n\t\t\t}\n", st.Name)
		g.pf("\t\t\t*report = TableReport{NonFiniteFloat: why}\n\t\t\ttableNonFiniteRefused(report)\n\t\t\treturn -1\n\t\t}\n")
	}
	g.pf("\t\ttableFixedForgedBounds(entries, entryCount, at[8:recordBytes], report)\n")
	if ir.TableFixedClampNeeded(st) {
		g.pf("\t\t// AND THE BOUNDS THE LOOP DOES NOT HOLD, straight-line over the\n")
		g.pf("\t\t// storage it just wrote: the same pass for either plan (§3.4).\n")
		g.pf("\t\t%sFixedClamp(&values[k], report)\n", st.Name)
	}
	g.pf("\t\tat = at[recordBytes:]\n")
	g.pf("\t}\n")
	g.pf("\treport.Unknown += censusUnknown\n")
	g.pf("\treport.KindMismatch += censusKind\n")
	g.pf("\treturn n\n}\n\n")
}

/* ---- the read-side bounds --------------------------------------------------

A fixed record is a positional image, so the ONE read loop moves bytes and asks
nothing about what they mean. What a declaration bounds is held HERE, as
straight-line code in the generated decode, after the copy — never as plan
entries, which would be a test per bounded field on every read of every record
and is the cost the identity plan exists to avoid.

THE PASS RUNS OVER STORAGE, so ONE pass covers both plans. It walks only what a
read can have written: a counted array's LIVE elements and never its slack, an
optional's payload only when the present byte says so. */

func (g *tableGen) emitFixedClampBodyFn(st *ir.Struct) {
	if !ir.TableFixedClampNeeded(st) {
		return
	}
	g.pf("// %s's read-side bounds.\n", st.Name)
	g.pf("func %sFixedClampBody(value *%s, clamped *int32) {\n", st.Name, g.fixedStorage(st.Name))
	g.pf("\t_, _ = value, clamped\n")
	for _, f := range st.Fields {
		g.emitFixedClampField(f, "value", 1, fixedPassClamp)
	}
	g.pf("}\n\n")
}

// emitFixedCheckBodyFn is the WRITE-SIDE twin of emitFixedClampBodyFn
// (docs/FIXED-FORM-ALGORITHM.md §3.1, docs/USAGE.md "Writes are the caller's
// responsibility"): the same walk over the same live extent, with every bound
// the read side would clamp (and every count, length and text rule it would
// refuse) answered by `return false` instead. Go has no debug-only idiom, so
// the fixed writer holds the contract in every build; <T>FixedSave calls it
// before the first byte is stored. A type that bounds nothing gets no function
// (§2.2's zero-cost rule).
func (g *tableGen) emitFixedCheckBodyFn(st *ir.Struct) {
	if !fixedCheckNeededStruct(st, map[string]bool{}) {
		return
	}
	g.pf("// %s's write-side contract (docs/FIXED-FORM-ALGORITHM.md §3.1): false is TableRefuseInvalidValue.\n", st.Name)
	g.pf("func %sFixedCheckBody(value *%s) bool {\n", st.Name, g.fixedStorage(st.Name))
	text := fixedDirectText(st)
	if text {
		// z and hi are the whole body's zero-byte and high-bit tests
		// (emitFixedTextCheck), gathered across every string and asked once.
		g.pf("\tvar z, hi uint64\n")
	} else {
		g.pf("\t_ = value\n")
	}
	for _, f := range st.Fields {
		g.emitFixedClampField(f, "value", 1, fixedPassCheck)
	}
	if !text {
		g.pf("\treturn true\n}\n\n")
		return
	}
	g.pf("\tif z&0x8080808080808080 != 0 {\n\t\treturn false\n\t}\n")
	g.pf("\tif hi&0x8080808080808080 != 0 {\n\t\treturn %sFixedCheckBodyExact(value)\n\t}\n", st.Name)
	g.pf("\treturn true\n}\n\n")
	g.pf("// %sFixedCheckBodyExact is %sFixedCheckBody with each string asked by\n", st.Name, st.Name)
	g.pf("// tableFixedUTF8Ok: the answer for text with a byte at or past 0x80 (§3.1).\n")
	g.pf("func %sFixedCheckBodyExact(value *%s) bool {\n", st.Name, g.fixedStorage(st.Name))
	g.pf("\t_ = value\n")
	for _, f := range st.Fields {
		g.emitFixedClampField(f, "value", 1, fixedPassExact)
	}
	g.pf("\treturn true\n}\n\n")
}

// fixedDirectText is whether the check body of st asks a string's text rule
// itself: a string field, under an optional or in a union arm (at any depth of
// unions, whose arms are emitted in line), but not inside a nested struct,
// which has a check body of its own.
func fixedDirectText(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if fixedFieldText(f, map[*ir.Union]bool{}) {
			return true
		}
	}
	return false
}

func fixedFieldText(f *ir.Field, seen map[*ir.Union]bool) bool {
	if f.Type.Kind == ir.TString {
		return true
	}
	if f.Type.Kind != ir.TNamed {
		return false
	}
	u, ok := f.Type.Ref.(*ir.Union)
	if !ok || seen[u] {
		return false
	}
	seen[u] = true
	for _, v := range u.Variants {
		if v.F != nil && fixedFieldText(v.F, seen) {
			return true
		}
	}
	return false
}

// fixedCheckNeededStruct is TableFixedClampNeeded widened to what only a
// writer holds: a counted array's count and a text or bytes length are bounds
// the read side clamps but a write side must refuse.
func fixedCheckNeededStruct(st *ir.Struct, seen map[string]bool) bool {
	if seen[st.Name] {
		return false
	}
	seen[st.Name] = true
	for _, f := range st.Fields {
		if fixedCheckNeededField(f, seen) {
			return true
		}
	}
	return false
}

func fixedCheckNeededField(f *ir.Field, seen map[string]bool) bool {
	// Deprecated fixed slots are sourced from the declared default image, so
	// live values cannot make the write fail. Keep this writer-only skip here;
	// the read-side clamp pass still applies the endpoint-layout bounds.
	if f.Deprecated {
		return false
	}
	// A PLAIN float has no bound to clamp, and the writer still refuses a NaN
	// or an infinity (docs/SPEC-TABLES.md §3.3, plainFloatFinite).
	if _, ok := plainFloatFinite(f, ""); ok {
		return true
	}
	// A counted array must check its own count even when its element has no
	// bounded fields. In particular, an unbounded named struct still has Max.
	if f.Array == ir.ArrayCounted {
		return true
	}
	if f.Type.Kind == ir.TNamed {
		if r, ok := f.Type.Ref.(*ir.Struct); ok {
			return fixedCheckNeededStruct(r, seen)
		}
		return ir.TableFixedClampNeededField(f)
	}
	switch f.Type.Kind {
	case ir.TString, ir.TBytes:
		return true
	}
	return ir.TableFixedClampNeededField(f)
}

// fixedBoundNeeded answers the one question both passes ask of a field: is
// there anything for this pass to do. The check pass (chk) asks the wider one.
func fixedBoundNeeded(f *ir.Field, chk bool) bool {
	if chk {
		return fixedCheckNeededField(f, map[string]bool{})
	}
	return ir.TableFixedClampNeededField(f)
}

func fixedBoundNeededStruct(r *ir.Struct, chk bool) bool {
	if chk {
		return fixedCheckNeededStruct(r, map[string]bool{})
	}
	return ir.TableFixedClampNeeded(r)
}

func (g *tableGen) emitFixedClamp(st *ir.Struct) {
	if !ir.TableFixedClampNeeded(st) {
		return
	}
	g.pf("// THE READ-SIDE BOUNDS (docs/SPEC-TABLES.md §3.4): a ranged scalar's\n")
	g.pf("// declared min and max, and an ORDINAL's set — a union tag past the arm\n")
	g.pf("// count, an enum ordinal past the enum's top value. Straight-line, after\n")
	g.pf("// the copy, over STORAGE, so the identity plan and a plan compiled from a\n")
	g.pf("// stranger's layout are held to the same numbers by the same pass. Every\n")
	g.pf("// clamp COUNTS. The pass walks LIVE elements only: a counted array's live\n")
	g.pf("// count, not its slack; an optional's payload only when present; a union's\n")
	g.pf("// set arm, not the others.\n")
	g.pf("func %sFixedClamp(value *%s, report *TableReport) {\n", st.Name, st.Name)
	g.pf("\tclamped := int32(0)\n")
	g.pf("\t%sFixedClampBody(value, &clamped)\n", st.Name)
	g.pf("\treport.Clamped += clamped\n}\n\n")
}

// compressedFloatClamp is the ONE bounds rule a compressed float takes wherever
// a read holds it to its declared range (docs/SPEC-TABLES.md §3.3, A COMPRESSED
// FLOAT'S SPECIAL VALUES): a value below `min` clamps to `min` and COUNTS, one
// above `max` clamps to `max` and counts, and a -0 lands as +0, the value 0
// itself, with no sign bit and no count, because it is inside the range and
// only its spelling differs. A NaN or an infinity never reaches it: every read
// refuses one on its raw bits first, as non_finite_float, before this bounds
// pass runs (2026-10-27).
func compressedFloatClamp(expr, lo, hi, count string) string {
	return fmt.Sprintf("if %[1]s < %[2]s { %[1]s = %[2]s; %[4]s } else if %[1]s > %[3]s { %[1]s = %[3]s; %[4]s } else if %[1]s == 0 { %[1]s = 0 }",
		expr, lo, hi, count)
}

// fixedPass is which of the passes below is being emitted.
type fixedPass int

const (
	// fixedPassClamp is the READ side's bounds pass: every violation is
	// repaired and counted.
	fixedPassClamp fixedPass = iota
	// fixedPassCheck is the WRITE side's check (<T>FixedCheckBody): every
	// violation is a `return false`, and a string's text rule is the in-line
	// word test of emitFixedTextCheck.
	fixedPassCheck
	// fixedPassExact is the same check with each string asked exactly, one
	// tableFixedUTF8Ok per string (<T>FixedCheckBodyExact): the fast pass
	// hands a body to it only when some used byte is at or past 0x80.
	fixedPassExact
)

// The pass below is written once and emitted for each fixedPass. The clamp
// and the checks walk the same extent and hold the same bounds, because both
// come from scalarBounds.
func (g *tableGen) emitFixedClampField(f *ir.Field, val string, tabs int, pass fixedPass) {
	chk := pass != fixedPassClamp
	// A deprecated fixed slot writes its declared default, so its live value
	// must not refuse the write. Read-side clamping still applies.
	if chk && f.Deprecated {
		return
	}
	if !fixedBoundNeeded(f, chk) {
		return
	}
	ind := strings.Repeat("\t", tabs)
	if f.Type.Optional {
		g.pf("%sif %s.%sPresent {\n", ind, val, member(f))
		g.emitFixedClampPayload(f, val, tabs+1, pass)
		g.pf("%s}\n", ind)
		return
	}
	g.emitFixedClampPayload(f, val, tabs, pass)
}

func (g *tableGen) emitFixedClampPayload(f *ir.Field, val string, tabs int, pass fixedPass) {
	chk := pass != fixedPassClamp
	ind := strings.Repeat("\t", tabs)
	elem := *f
	elem.Array, elem.KeyEnum = ir.ArrayNone, ""
	loop := func(count string, down bool) {
		if chk && !fixedCheckNeededField(&elem, map[string]bool{}) {
			return
		}
		// An element with nothing to clamp emits no loop at all: an empty
		// loop over the count is a no-op, and its index would be unused.
		var body strings.Builder
		g.into(&body, func() {
			g.emitFixedClampElement(f, fmt.Sprintf("%s.%s[i]", val, member(f)), tabs+1, pass)
		})
		if body.Len() == 0 {
			return
		}
		if down {
			// THE CHECK WALKS A COUNTED ARRAY DOWN (docs/FIXED-FORM-ALGORITHM.md
			// §3.1): every live element is checked either way, and ending on
			// the FIRST elements leaves them in cache for the store, which
			// walks up (measured by the paired bench, bench/paired).
			g.pf("%sfor i := %s - 1; i >= 0; i-- {\n", ind, count)
		} else {
			g.pf("%sfor i := range %s {\n", ind, count)
		}
		g.raw(body.String())
		g.pf("%s}\n", ind)
	}
	switch {
	case f.KeyEnum != "":
		loop(fmt.Sprint(f.KeyEnumRef.Max), false)
	case f.Array == ir.ArrayFixed:
		loop(fmt.Sprint(f.ArrayBound), false)
	case f.Array == ir.ArrayCounted:
		if chk {
			g.pf("%sif %s.%sCount < 0 || %s.%sCount > %d { return false }\n", ind, val, member(f), val, member(f), f.ArrayBound)
		}
		loop(fmt.Sprintf("int64(%s.%sCount)", val, member(f)), chk)
	default:
		g.emitFixedClampElement(f, val+"."+member(f), tabs, pass)
	}
}

func (g *tableGen) emitFixedClampElement(f *ir.Field, expr string, tabs int, pass fixedPass) {
	chk := pass != fixedPassClamp
	ind := strings.Repeat("\t", tabs)
	// bound repairs one violated bound in the read pass and refuses it in the
	// write pass; both is the same condition.
	bound := func(conds []scalarBound) {
		if len(conds) == 0 {
			return
		}
		if chk {
			var parts []string
			for _, c := range conds {
				parts = append(parts, c.cond)
			}
			g.pf("%sif %s { return false }\n", ind, strings.Join(parts, " || "))
			return
		}
		for i, c := range conds {
			if i == 0 {
				g.pf("%sif %s { %s = %s; (*clamped)++ }", ind, c.cond, expr, c.set)
			} else {
				g.pf(" else if %s { %s = %s; (*clamped)++ }", c.cond, expr, c.set)
			}
		}
		g.pf("\n")
	}
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			if fixedBoundNeededStruct(r, chk) {
				if chk {
					g.pf("%sif !%sFixedCheckBody(&%s) { return false }\n", ind, r.Name, expr)
				} else {
					g.pf("%s%sFixedClampBody(&%s, clamped)\n", ind, r.Name, expr)
				}
			}
			return
		case *ir.Union:
			if chk {
				g.pf("%sif uint32(%s.Type) > %d { return false }\n", ind, expr, r.Max)
			} else {
				g.pf("%sif uint32(%s.Type) > %d { %s.Type = %sTypeNone; (*clamped)++ }\n",
					ind, expr, r.Max, expr, f.Type.Name)
			}
			if !g.fixedArmNeeded(r, chk) {
				return
			}
			g.pf("%sswitch %s.Type {\n", ind, expr)
			for _, v := range r.Variants {
				if v.F == nil || !fixedBoundNeeded(v.F, chk) {
					continue
				}
				// An arm is a field line: its payload gets the per-element
				// walk a declared field gets, not a single-element clamp.
				// Name the copy after the arm, as the union's own storage
				// does (unions.go), and hand it to emitFixedClampField: a
				// fixed array walks to its bound, a counted array to its live
				// Count, an optional only when present. §3.4 walks LIVE
				// elements only, so a fixed array never touches slack and a
				// counted array never touches [Count:].
				arm := *v.F
				arm.Name = v.Name
				g.pf("%scase %sType%s:\n", ind, f.Type.Name, ir.GoExportName(v.Name))
				g.emitFixedClampField(&arm, expr, tabs+1, pass)
			}
			g.pf("%s}\n", ind)
			return
		case *ir.Enum:
			if chk {
				if enumHasDeprecated(r) {
					// A STRICT WRITE REFUSES A DEPRECATED VARIANT (docs/SPEC-TABLES.md §2.15).
					g.pf("%sif %s.TableEnumDeprecated() { return false }\n", ind, expr)
				}
				g.pf("%sif uint64(%s) > %d { return false }\n", ind, expr, r.Max)
			} else {
				g.pf("%sif uint64(%s) > %d { %s = %sNone; (*clamped)++ }\n",
					ind, expr, r.Max, expr, f.Type.Name)
				if enumHasDeprecated(r) {
					// A DEPRECATED VARIANT READS AS THE FIELD'S DEFAULT, and as
					// the ELEMENT default, without counting (docs/SPEC-TABLES.md
					// §2.15).
					g.pf("%sif %s.TableEnumDeprecated() { %s = %s }\n", ind, expr, expr, fieldDefaultExpr(f))
				}
			}
			return
		}
		return
	}
	if chk {
		switch f.Type.Kind {
		case ir.TString:
			if pass == fixedPassCheck {
				g.emitFixedTextCheck(expr, f.Type.Size, ind)
			} else {
				g.pf("%sif uint32(%sLength) > %d || !tableFixedUTF8Ok(%s[:%sLength]) { return false }\n",
					ind, expr, f.Type.Size, expr, expr)
			}
			return
		case ir.TBytes:
			g.pf("%sif %sLength < 0 || %sLength > %d { return false }\n", ind, expr, expr, f.Type.Size)
			return
		}
	}
	// Keep the read-side compressed-float special-values rule from §3.3.
	// The writer instead uses the refusal bounds below.
	if !chk && f.HasFloatRange {
		switch f.Type.Kind {
		case ir.TFloat32:
			g.pf("%s%s\n", ind, compressedFloatClamp(expr, formatFloat32(f.FMin), formatFloat32(f.FMax), "(*clamped)++"))
			return
		case ir.TFloat64:
			g.pf("%s%s\n", ind, compressedFloatClamp(expr, formatFloat64(f.FMin), formatFloat64(f.FMax), "(*clamped)++"))
			return
		}
	}
	bounds := scalarRangeBounds(f, expr)
	bound(bounds)
	if b, ok := scalarWidthBound(f, expr); ok {
		bound([]scalarBound{b})
	}
	// THE WRITER'S FINITE RULE for a plain float (docs/SPEC-TABLES.md §3.3):
	// the read side's finite pass refuses what this refuses.
	if chk {
		if cond, ok := plainFloatFinite(f, expr); ok {
			g.pf("%sif %s { return false }\n", ind, cond)
		}
	}
}

// emitFixedTextCheck is the WRITER's string rule for one string(N) field
// (docs/FIXED-FORM-ALGORITHM.md §3.1, docs/USAGE.md "Writes are the caller's
// responsibility"): a length outside 0..N refuses, and the used bytes must be
// well-formed UTF-8 with no NUL. It is emitted in line rather than called,
// because the speed gate measured the call. Eight bytes a step: z (the check
// body's own, asked once at its end) gathers each word's zero-byte test
// (exact: a word has a zero byte if and only if some lane of (x-0x01..)&^x has
// its high bit set), and hi (the body's own too) gathers the high bits.
// The last partial word is read whole from the field's own storage: when
// n >= 8 it overlaps used bytes already tested, and when n < 8 it is masked to
// the used bytes, with 0x01 (neither zero nor high) in the lanes past n. A
// zero anywhere refuses; text with no high bit anywhere is ASCII and passes;
// a body with a byte at or past 0x80 is asked again by its exact twin, one
// tableFixedUTF8Ok per string. The answer is exactly utf8.Valid plus a NUL
// scan over the used bytes of every string.
func (g *tableGen) emitFixedTextCheck(expr string, size int64, ind string) {
	g.pf("%sif n := int(%sLength); uint(n) > %d {\n%s\treturn false\n%s} else {\n", ind, expr, size, ind, ind)
	g.emitTextWords(expr, size, ind+"\t", "tableFixedGet64", true)
	g.pf("%s}\n", ind)
}

// emitTextWords is the word test both writers share (emitFixedTextCheck,
// emitWireValue): over p := expr[:n], with n already inside 0..size, it ORs
// each word's zero-byte test into z and its high bits into hi, both declared by
// the caller. get is the unit's little-endian 64-bit load. masked says the
// last partial word of a string shorter than a word may be read whole from the
// field's own storage (a fixed string(N) of N >= 8 always may); otherwise
// that tail is tested a byte at a time.
func (g *tableGen) emitTextWords(expr string, size int64, in, get string, masked bool) {
	const ones = "0x0101010101010101"
	g.pf("%sp := %s[:n]\n", in, expr)
	if size >= 16 {
		g.pf("%sfor len(p) >= 16 {\n", in)
		g.pf("%s\tx, y := %s(p), %s(p[8:])\n", in, get, get)
		g.pf("%s\thi |= x | y\n", in)
		g.pf("%s\tz |= (x-%s)&^x | (y-%s)&^y\n", in, ones, ones)
		g.pf("%s\tp = p[16:]\n%s}\n", in, in)
	}
	if size < 8 {
		g.pf("%sfor _, c := range p {\n%s\tx := uint64(c)\n%s\thi |= x\n%s\tz |= (x - 1) &^ x\n%s}\n", in, in, in, in, in)
		return
	}
	g.pf("%sif len(p) >= 8 {\n%s\tx := %s(p)\n%s\thi |= x\n%s\tz |= (x - %s) &^ x\n%s\tp = p[8:]\n%s}\n",
		in, in, get, in, in, ones, in, in)
	g.pf("%sif len(p) > 0 {\n", in)
	if masked {
		g.pf("%s\tvar x uint64\n", in)
		g.pf("%s\tif n >= 8 {\n%s\t\tx = %s(%s[n-8:])\n", in, in, get, expr)
		g.pf("%s\t} else {\n%s\t\tm := uint64(1)<<(8*uint(n)) - 1\n%s\t\tx = %s(%s[:])&m | %s&^m\n%s\t}\n", in, in, in, get, expr, ones, in)
		g.pf("%s\thi |= x\n%s\tz |= (x - %s) &^ x\n", in, in, ones)
	} else {
		g.pf("%s\tif n >= 8 {\n%s\t\tx := %s(%s[n-8:])\n%s\t\thi |= x\n%s\t\tz |= (x - %s) &^ x\n", in, in, get, expr, in, in, ones)
		g.pf("%s\t} else {\n%s\t\tfor _, c := range p {\n%s\t\t\tx := uint64(c)\n%s\t\t\thi |= x\n%s\t\t\tz |= (x - 1) &^ x\n%s\t\t}\n%s\t}\n", in, in, in, in, in, in, in)
	}
	g.pf("%s}\n", in)
}

// fixedArmNeeded is whether ANY arm's payload needs this pass: the clamp's
// TableFixedClampNeededUnionArm for the read side, the wider question for the
// write side.
func (g *tableGen) fixedArmNeeded(r *ir.Union, chk bool) bool {
	for _, v := range r.Variants {
		if v.F != nil && fixedBoundNeeded(v.F, chk) {
			return true
		}
	}
	return false
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

// raw writes already-indented text to the current target, as pf would after
// its own indentation: text captured through into goes back out this way.
func (g *tableGen) raw(s string) {
	if g.out != nil {
		g.out.WriteString(s)
		return
	}
	g.body.WriteString(s)
}

/* ---- the read-side finite pass ---------------------------------------------

A FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3; Glenn, 2026-10-27, "tables go
finite-only too, same rule as packets"): every fixed read refuses a NaN or an
infinity as non_finite_float, naming the field, tolerant and strict alike, and
the test runs FIRST, before the bounds pass, so a compressed float's clamp
never sees one. Like the bounds pass it runs over STORAGE, straight-line, after
the record lands, so ONE pass covers both plans: the identity copy and a
lineage plan's widenF, whose ordinary conversion keeps a NaN a NaN and an
infinity an infinity. It walks only what a read can have written — a counted
array's live elements, an optional's payload when present, a union's set arm —
and x-x != 0 is the whole test: NaN for both non-finite classes, zero for
every finite value, -0 and the subnormals included. */

// fixedFiniteNeededStruct is whether a fixed type holds a float anywhere a
// read lands one.
func fixedFiniteNeededStruct(st *ir.Struct, seen map[string]bool) bool {
	if seen[st.Name] {
		return false
	}
	seen[st.Name] = true
	for _, f := range st.Fields {
		if fixedFiniteNeededField(f, seen) {
			return true
		}
	}
	return false
}

func fixedFiniteNeededField(f *ir.Field, seen map[string]bool) bool {
	switch f.Type.Kind {
	case ir.TFloat32, ir.TFloat64:
		return true
	case ir.TNamed:
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return fixedFiniteNeededStruct(r, seen)
		case *ir.Union:
			for _, v := range r.Variants {
				if v.F != nil && fixedFiniteNeededField(v.F, seen) {
					return true
				}
			}
		}
	}
	return false
}

// emitFixedFiniteBodyFn emits <T>FixedFiniteBody: the first non-finite float's
// <Owner>.<field> name, or "" for a record whose floats are all finite. A type
// that holds no float gets no function (§2.2's zero-cost rule).
func (g *tableGen) emitFixedFiniteBodyFn(st *ir.Struct) {
	if !fixedFiniteNeededStruct(st, map[string]bool{}) {
		return
	}
	g.pf("// %sFixedFiniteBody is %s's read-side finite rule (docs/SPEC-TABLES.md §3.3).\n", st.Name, st.Name)
	g.pf("func %sFixedFiniteBody(value *%s) string {\n", st.Name, g.fixedStorage(st.Name))
	for _, f := range st.Fields {
		g.emitFixedFiniteField(f, f, "value", 1)
	}
	g.pf("\treturn \"\"\n}\n\n")
}

// emitFixedFiniteField walks one field line's live extent. named is the
// declaration the refusal names: the field itself, or a union arm's own line
// where the walk holds a copy renamed to the arm's storage member.
func (g *tableGen) emitFixedFiniteField(f, named *ir.Field, val string, tabs int) {
	if !fixedFiniteNeededField(f, map[string]bool{}) {
		return
	}
	ind := strings.Repeat("\t", tabs)
	if f.Type.Optional {
		g.pf("%sif %s.%sPresent {\n", ind, val, member(f))
		g.emitFixedFinitePayload(f, named, val, tabs+1)
		g.pf("%s}\n", ind)
		return
	}
	g.emitFixedFinitePayload(f, named, val, tabs)
}

func (g *tableGen) emitFixedFinitePayload(f, named *ir.Field, val string, tabs int) {
	ind := strings.Repeat("\t", tabs)
	loop := func(count string) {
		g.pf("%sfor i := range %s {\n", ind, count)
		g.emitFixedFiniteElement(f, named, fmt.Sprintf("%s.%s[i]", val, member(f)), tabs+1)
		g.pf("%s}\n", ind)
	}
	switch {
	case f.KeyEnum != "":
		loop(fmt.Sprint(f.KeyEnumRef.Max))
	case f.Array == ir.ArrayFixed:
		loop(fmt.Sprint(f.ArrayBound))
	case f.Array == ir.ArrayCounted:
		// the count was refused past its bound before the record landed
		// (tableFixedTextRefusal); min keeps the walk inside storage anyway
		loop(fmt.Sprintf("min(int64(%s.%sCount), %d)", val, member(f), f.ArrayBound))
	default:
		g.emitFixedFiniteElement(f, named, val+"."+member(f), tabs)
	}
}

func (g *tableGen) emitFixedFiniteElement(f, named *ir.Field, expr string, tabs int) {
	ind := strings.Repeat("\t", tabs)
	switch f.Type.Kind {
	case ir.TFloat32, ir.TFloat64:
		g.pf("%sif %s-%s != 0 {\n%s\treturn %q\n%s}\n", ind, expr, expr, ind, g.floatOwner(named), ind)
		return
	case ir.TNamed:
	default:
		return
	}
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		if fixedFiniteNeededStruct(r, map[string]bool{}) {
			g.pf("%sif why := %sFixedFiniteBody(&%s); why != \"\" {\n%s\treturn why\n%s}\n", ind, r.Name, expr, ind, ind)
		}
	case *ir.Union:
		g.pf("%sswitch %s.Type {\n", ind, expr)
		for _, v := range r.Variants {
			if v.F == nil || !fixedFiniteNeededField(v.F, map[string]bool{}) {
				continue
			}
			arm := *v.F
			arm.Name = v.Name
			g.pf("%scase %sType%s:\n", ind, f.Type.Name, ir.GoExportName(v.Name))
			g.emitFixedFiniteField(&arm, v.F, expr, tabs+1)
		}
		g.pf("%s}\n", ind)
	}
}
