// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3, for Rust.
//
// A fixed-table record is an eight-byte hash of the writer's vocabulary block
// and then the values in declared order, every field at its DECLARED STORAGE
// WIDTH, nothing padded between fields. The BYTES are the C++ reference's
// (internal/codegen/cpptable/fixedform.go) exactly; what is this port's own is
// where those bytes land, and that is stated here rather than left to be found:
//
//	THE MODEL IS THE PACKET CODEC'S, NOT FORM 1'S. The Rust type wire's
//	generated writer is a straight line of stores by field NAME into a buffer
//	the caller owns, and its reader is the mirror of it; there is no
//	per-field reference, no kind byte, no length and no probe anywhere in it.
//	The fixed form's writer and reader are exactly that shape with byte-width
//	stores in place of bit windows.
//
//	THE PLAN WORKS IN THE RECORD IMAGE AND NOT IN THE STORAGE. C++ lands a
//	plan entry's bytes straight into the storage struct at an offsetof, which
//	Rust cannot do for the whole of a type: a Rust union is a real enum with
//	no committed payload layout (the same reason the cooked form gives at
//	cook.go's `cookable`), and a `string(N)` is `[u8; N + 1]` here where the
//	wire is N. So the plan's DESTINATION is THIS BUILD'S OWN RECORD IMAGE —
//	the same declared-order, unpadded bytes the writer produces — and a
//	generated straight-line SCATTER lands that image in the value. The
//	consequences are all in this port's favour:
//
//	  * the identity plan is ONE entry, the whole body in one move, which is
//	    §3.4's "adjacent runs coalesced" taken to its end: in the image domain
//	    the record's declared order IS the destination's order;
//	  * the plan compiler needs no side table of storage offsets, because MY
//	    offset is MY declared offset — the same walk that built the block;
//	  * there is no `unsafe` and no `offset_of!` anywhere on this path, where
//	    the two accelerators need both.
//
//	ONE READER PATH, and it is one for the same reason §3.4 gives: prefill the
//	image with the declared defaults, run ONE loop over ONE plan, scatter. The
//	identity hash and a stranger's hash differ only in which plan the loop was
//	handed, and they cost the same.
//
// Nothing here touches form 1, which this port does not carry at all: Rust's
// table surface is the two accelerators (§7, §19), and this is the first WIRE
// form it emits.
package rusttable

import (
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// FixedRuntimeModule is the fixed form's shared runtime, emitted once per unit
// that carries a fixed root.
const FixedRuntimeModule = "fixed_runtime"

// ---------------------------------------------------------------------------
// THE WIRE LAW IS ir's, AND THIS FILE ONLY RENDERS IT
//
// The constant size of every field, the pre-order walk that becomes the LAYOUT
// and the hash over it live in ir/fixedform.go, because they produce BYTES and
// every port has to produce the same ones. This port USED to carry a private
// copy of all three — fixedStorageBytes, fixedTypeBytes, fixedFieldBytes,
// fixedElementBytes, fixedUnionBytes, the fixedWalk* family, fixedBlockBytes
// and fixedBlockHash — which is a SECOND ANSWER waiting to happen: it could
// drift off the wire law and say nothing, and the only thing watching was a
// test that compared this port's emitted text against ANOTHER BACKEND'S
// emitted text, so a change to the law that both ports missed would pass.
//
// They are gone. What is left here is this port's own side of the form: where
// a value lands, how a store is spelled, and the prefill image Rust needs
// because it has no by-value Reset.
// ---------------------------------------------------------------------------

// tagMax is the greatest value a tag of `width` bytes holds.
func tagMax(width int64) int64 {
	if width >= 8 {
		return math.MaxInt64
	}
	return int64(1)<<(8*width) - 1
}

// fixedCounted is MY side of one layout entry, in the one byte the plan
// compiler needs: whether the entry is an array carrying a live COUNT in front
// of it. ir's walk already answers it (TableFixedDstSpec.Counted), so this
// reads the answer rather than deciding it again.
func fixedCounted(e ir.TableFixedLayoutEntry) bool { return e.Dst.Counted != 0 }

// ---------------------------------------------------------------------------
// THE PREFILL IMAGE: the declared defaults, as record bytes
//
// §3.4's answer to an absent field is a PREFILL, and this is what this port
// prefills WITH. C++ calls the type's own Reset; Rust's table surface has no
// by-value reset (a `<Name>Row` is `core::mem::zeroed`), so the defaults are
// laid down HERE, as the record image a fresh value would write. It is a
// compile-time constant like the block and the template, and it is the one
// place a declared default reaches this form.
// ---------------------------------------------------------------------------

func fixedDefaultImage(st *ir.Struct) []byte {
	out := make([]byte, ir.TableFixedTypeBytes(st))
	fixedDefaultType(out, st)
	return out
}

func fixedDefaultType(out []byte, st *ir.Struct) {
	at := int64(0)
	for _, f := range st.Fields {
		fixedDefaultField(out[at:at+ir.TableFixedFieldBytes(f)], f)
		at += ir.TableFixedFieldBytes(f)
	}
}

func fixedDefaultField(out []byte, f *ir.Field) {
	if f.Type.Optional {
		// a fresh optional is ABSENT, and its payload still rides whole
		out = out[ir.TableFixedPresentBytes:]
	}
	switch {
	case f.KeyEnum != "":
		fixedDefaultSlots(out, f, f.KeyEnumRef.Max)
	case f.Array == ir.ArrayFixed:
		fixedDefaultSlots(out, f, f.ArrayBound)
	case f.Array == ir.ArrayCounted:
		// the born count is the declared minimum (ir.Field.BornCount)
		fixedPutU32(out, uint32(f.BornCount()))
		fixedDefaultSlots(out[ir.TableFixedCountBytes:], f, f.ArrayBound)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		fixedPutU32(out, uint32(len(f.DefBytes)))
		copy(out[ir.TableFixedCountBytes:], f.DefBytes)
	case f.Type.Kind == ir.TWString:
		fixedPutU32(out, uint32(len(f.DefBytes)))
	default:
		fixedDefaultElement(out, f)
	}
}

func fixedDefaultSlots(out []byte, f *ir.Field, count int64) {
	elem := ir.TableFixedElementBytes(f)
	for i := range count {
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
			// a defaulted enum field rides its variant's ORDINAL, from 1;
			// 0 is None, which is also the zero image
			if f.HasDefault && f.DefVariant != "" {
				for i, v := range r.Variants {
					if v == f.DefVariant {
						fixedPutUint(out, uint64(i+1), ir.TableFixedStorageBytes(f.Type))
						break
					}
				}
			}
			return
		case *ir.Union:
			return // a fresh union is None, tag 0, which is the zero image
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
		fixedPutBig(out, f.DefInt, ir.TableFixedStorageBytes(f.Type))
	}
}

func fixedPutU32(out []byte, v uint32) { fixedPutUint(out, uint64(v), 4) }

func fixedPutUint(out []byte, v uint64, width int64) {
	for i := int64(0); i < width && i < 8; i++ {
		out[i] = byte(v >> (8 * i))
	}
}

// fixedPutBig lays a declared integer default down two's complement
// little-endian at its storage width, which is what the 128-bit family needs
// and what every narrower one gets for free.
func fixedPutBig(out []byte, v *big.Int, width int64) {
	if width <= 0 {
		return
	}
	n := new(big.Int).Set(v)
	if n.Sign() < 0 {
		mod := new(big.Int).Lsh(big.NewInt(1), uint(width*8))
		n.Add(n, mod)
	}
	raw := n.Bytes() // big-endian
	for i := 0; i < len(raw) && int64(i) < width; i++ {
		out[i] = raw[len(raw)-1-i]
	}
}

// ---------------------------------------------------------------------------
// THE EMISSION
// ---------------------------------------------------------------------------

// fixedRoots is every table of this file the fixed form is emitted for. A
// table that reaches a POINTER, a MAP or an unbounded array is VARIABLE and
// keeps §3 entirely; one that reaches a union carrying text or an array is
// §3.4's own refusal, and it is the same shape this port has no Row for
// (cook.go's `rowable`), so the two questions have one answer.
func (g *gen) fixedRoots() []*ir.Struct {
	var out []*ir.Struct
	for _, st := range orderTables(g.file.Tables) {
		if !ir.TableFixedEmitted(g.unit, st) || !g.rowable(st.Name) {
			continue
		}
		out = append(out, st)
	}
	return out
}

// unitFixedClosure is the closure walk: every type and every UNION reachable
// BY VALUE from a fixed root anywhere in the unit — the ones whose writer and
// scatter have to exist. A nested type is emitted by the file that DECLARES
// it, exactly as its Row is, so two roots sharing one nested type do not
// define it twice.
func unitFixedClosure(u *ir.Unit, closure map[string]bool) (types, unions map[string]bool) {
	types, unions = map[string]bool{}, map[string]bool{}
	for _, f := range u.Files {
		g := &gen{unit: u, file: f, closure: closure}
		for _, st := range g.fixedRoots() {
			fixedCollectTypes(st, types, unions)
		}
	}
	return types, unions
}

// anyFixedRoot reports whether the unit carries a fixed root anywhere, which
// is what says the shared runtime module is emitted.
func anyFixedRoot(u *ir.Unit, closure map[string]bool) bool {
	for _, f := range u.Files {
		g := &gen{unit: u, file: f, closure: closure}
		if len(g.fixedRoots()) > 0 {
			return true
		}
	}
	return false
}

// fixedModule emits <base>_fixed.rs: the writer and the scatter for every type
// of the unit's fixed closure this file declares, and the block, the identity
// plan and the one plan-driven load for every fixed root it declares.
func (g *gen) fixedModule() []byte {
	roots := g.fixedRoots()
	reach, reachUnions := unitFixedClosure(g.unit, g.closure)
	var members []*ir.Struct
	for _, st := range g.cookRoots() {
		if reach[st.Name] {
			members = append(members, st)
		}
	}
	var unions []*ir.Union
	for _, un := range g.fileRowUnions() {
		if reachUnions[un.Name] {
			unions = append(unions, un)
		}
	}
	if len(members) == 0 && len(roots) == 0 && len(unions) == 0 {
		return nil
	}
	g.body.Reset()
	g.pf("%s", header(g.file.Base, g.unit.Package, "THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4)"))
	g.pf(`//
// A record is an eight-byte hash of the writer's vocabulary block and then the
// values in declared order, every field at its declared storage width, nothing
// padded between them. The writer is a straight line of stores into a zeroed
// template — which is also what zero-fills every byte of declared slack — and
// the reader is ONE loop over ONE plan and then a straight line of loads.
//
// THE PLAN'S DESTINATION IS THIS BUILD'S OWN RECORD IMAGE and not its storage,
// because a Rust ` + "`string(N)`" + ` is [u8; N + 1] where the wire is N and a Rust
// union has no committed layout at all. So the identity plan is ONE entry —
// the whole body in one move, which is what §3.4's coalescing rule comes to
// when the destination's order IS the declared order — and the same loop runs
// over a plan compiled from another writer's block for anybody else.
//
// The VALUE is the unit's blittable ` + "`<Name>Row`" + `: a fixed record is plain data,
// which is exactly what a Row is (docs/SPEC-TABLES.md §7.2, §19.3).

// THE BLOCK, THE PREFILL AND THE PLAN ARE COMPILE-TIME CONSTANTS OF THE TYPE
// (§3.4), which is what clippy's large_const_arrays is about and why it is
// allowed here: a static would move them off the type they belong to and out
// of the const measure that reads their length.
#![allow(unused_imports)]
#![allow(clippy::large_const_arrays)]
#![allow(clippy::needless_range_loop)]
#![allow(clippy::too_many_arguments)]

use crate::*;

`)
	for _, un := range unions {
		g.emitFixedWriteUnion(un)
		g.emitFixedScatterUnion(un)
	}
	for _, st := range members {
		g.emitFixedWriteBody(st)
		g.emitFixedScatter(st)
	}
	for _, st := range roots {
		g.emitFixedRoot(st)
	}
	return []byte(g.body.String())
}

func fixedCollectTypes(st *ir.Struct, seen, unions map[string]bool) {
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
			fixedCollectTypes(r, seen, unions)
		case *ir.Union:
			fixedCollectUnion(r, seen, unions)
		}
	}
}

// fixedCollectUnion descends an ARM exactly as a field is descended (§2.6): an
// arm's payload is a record of the closure like any other.
func fixedCollectUnion(un *ir.Union, seen, unions map[string]bool) {
	if unions[un.Name] {
		return
	}
	unions[un.Name] = true
	for _, v := range un.Variants {
		if v.F == nil || v.F.Type.Kind != ir.TNamed {
			continue
		}
		switch r := v.F.Type.Ref.(type) {
		case *ir.Struct:
			fixedCollectTypes(r, seen, unions)
		case *ir.Union:
			fixedCollectUnion(r, seen, unions)
		}
	}
}

// ---- the template writer ---------------------------------------------------

// emitFixedWriteBody is the type's stores. It is the packet codec's writer
// with byte-width stores in place of bit windows: one straight line, by field
// NAME, at offsets the compiler settled.
func (g *gen) emitFixedWriteBody(st *ir.Struct) {
	g.pf("/// %s's stores, at constant offsets into a body the caller zeroed.\n", st.Name)
	g.pf("/// The zeros are the template, and they are also what fills every byte of\n")
	g.pf("/// declared slack without this function touching it (§3.4).\n")
	g.pf("pub fn %s(b: &mut [u8], value: &%sRow) {\n", fn(st.Name, "fixed_write_body"), st.Name)
	if len(st.Fields) == 0 {
		g.pf("    let _ = (b, value);\n")
	}
	off := int64(0)
	for _, f := range st.Fields {
		g.emitFixedWriteField(f, off)
		off += ir.TableFixedFieldBytes(f)
	}
	g.pf("}\n\n")
}

func (g *gen) emitFixedWriteField(f *ir.Field, off int64) {
	if f.Type.Optional {
		// AN ABSENT OPTIONAL'S PAYLOAD IS THE TEMPLATE'S ZEROS
		// (docs/SPEC-TABLES.md §3.4): the payload rides WHOLE whether or not it
		// is present, and when the flag is 0 what rides is ZERO. It is ONE `if`
		// here rather than a rule anywhere else, because the template already
		// put the zeros there — so an absent optional costs the writer the
		// branch and not one store, and a caller's untouched payload storage
		// never reaches the wire.
		g.pf("    b[%d] = u8::from(value.%s_present);\n", off, f.Name)
		g.pf("    if value.%s_present {\n", f.Name)
		g.emitFixedWritePayload(f, off+ir.TableFixedPresentBytes, 8)
		g.pf("    }\n")
		return
	}
	g.emitFixedWritePayload(f, off, 4)
}

func (g *gen) emitFixedWritePayload(f *ir.Field, base int64, indent int) {
	ind := strings.Repeat(" ", indent)
	switch {
	case f.KeyEnum != "":
		// EVERY SLOT OF A KEYED ARRAY IS LIVE (§2.4): there is no count and no
		// slack, so the bound is the loop.
		g.emitFixedWriteSlots(f, base, fmt.Sprintf("%d", f.KeyEnumRef.Max), indent)
	case f.Array == ir.ArrayFixed:
		// AND EVERY ELEMENT OF `[N]T` IS LIVE: min equals max, so no count rides.
		g.emitFixedWriteSlots(f, base, fmt.Sprintf("%d", f.ArrayBound), indent)
	case f.Array == ir.ArrayCounted:
		// THE COUNT IS THE LOOP AND THE SLACK STAYS THE TEMPLATE'S ZEROS
		// (docs/SPEC-TABLES.md §3.4). Writing all Max elements put the unused
		// slots' STORAGE on the wire, which for an array of a type with
		// declared defaults is the element's default image and not zero — a
		// value nobody wrote, riding as if somebody had.
		g.fixedWriteBound(f.Name+"_count", f.ArrayBound, "count", ind)
		g.pf("%sb[%d..%d].copy_from_slice(&(value.%s_count as u32).to_le_bytes());\n", ind, base, base+4, f.Name)
		g.emitFixedWriteSlots(f, base+ir.TableFixedCountBytes,
			fmt.Sprintf("value.%s_count.clamp(0, %d) as usize", f.Name, f.ArrayBound), indent)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		g.emitFixedWriteText(f, base, 1, indent)
	case f.Type.Kind == ir.TWString:
		g.emitFixedWriteText(f, base, 2, indent)
	default:
		g.emitFixedWriteElementAt(f, fmt.Sprintf("%d", base), "value."+f.Name, indent)
	}
}

// emitFixedWriteText is a text field's store: the used LENGTH, then that many
// UNITS onto the template's zeros (docs/SPEC-TABLES.md §3.4, "the slack is
// zero"). Copying the whole span instead put whatever the storage held past
// the used length on the wire — stale bytes, and a `string(N)` written twice
// with two different values would not compare equal on the second write.
//
// A `wstring(N)` is the same store with a TWO-BYTE unit, and it is here rather
// than in a default case that emitted a zero-width slice: wide text's buffer is
// [u16; N + 1] and its length counts CODE UNITS, so the span is 2 * length.
func (g *gen) emitFixedWriteText(f *ir.Field, base, unit int64, indent int) {
	ind := strings.Repeat(" ", indent)
	g.fixedWriteBound(f.Name+"_length", f.Type.Size, "length", ind)
	g.pf("%sb[%d..%d].copy_from_slice(&(value.%s_length as u32).to_le_bytes());\n", ind, base, base+4, f.Name)
	g.pf("%s{\n", ind)
	g.pf("%s    let n = value.%s_length.clamp(0, %d) as usize;\n", ind, f.Name, f.Type.Size)
	if unit == 1 {
		g.pf("%s    b[%d..%d + n].copy_from_slice(&value.%s[..n]);\n", ind, base+4, base+4, f.Name)
	} else {
		g.pf("%s    for i in 0..n {\n", ind)
		g.pf("%s        let at = %d + %d * i;\n", ind, base+4, unit)
		g.pf("%s        b[at..at + %d].copy_from_slice(&value.%s[i].to_le_bytes());\n", ind, unit, f.Name)
		g.pf("%s    }\n", ind)
	}
	g.pf("%s}\n", ind)
}

// fixedWriteBound is the WRITE-SIDE CHECK, and it is DEBUG-ONLY BY RULE:
// `debug_assert!` is gone in release exactly as the reference's schema_assert
// is under NDEBUG, so a release build pays nothing for it. The READ side checks
// the same number in every build and counts the clamp — a write-side check
// catches the caller's own bug at the call site, and it is never the thing that
// keeps the wire safe. The store beside it CLAMPS rather than trusting the
// number, because a Rust slice out of range is a panic and not a memcpy past
// the end.
func (g *gen) fixedWriteBound(member string, bound int64, what, ind string) {
	g.pf("%sdebug_assert!(\n", ind)
	g.pf("%s    value.%s >= 0 && value.%s <= %d,\n", ind, member, member, bound)
	g.pf("%s    \"the declared %s is the bound (docs/SPEC-TABLES.md §3.4)\"\n", ind, what)
	g.pf("%s);\n", ind)
}

func (g *gen) emitFixedWriteSlots(f *ir.Field, base int64, count string, indent int) {
	elem := ir.TableFixedElementBytes(f)
	if count == "0" {
		return
	}
	g.pf("%sfor i in 0..%s {\n", strings.Repeat(" ", indent), count)
	g.emitFixedWriteElementAt(f, fixedSlotOffset(base, elem), fmt.Sprintf("value.%s[i]", f.Name), indent+4)
	g.pf("%s}\n", strings.Repeat(" ", indent))
}

func (g *gen) emitFixedWriteElementAt(f *ir.Field, at, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	if name, n, nested := fixedNestedBody(f); nested {
		g.pf("%slet at = %s;\n", ind, at)
		g.pf("%s%s(&mut b[at..at + %d], &%s);\n", ind, fn(name, "fixed_write_body"), n, expr)
		return
	}
	width := ir.TableFixedElementBytes(f)
	g.pf("%slet at = %s;\n", ind, at)
	switch f.Type.Kind {
	case ir.TBool:
		g.pf("%sb[at] = u8::from(%s);\n", ind, expr)
	case ir.TFloat32:
		g.pf("%sb[at..at + 4].copy_from_slice(&%s.to_le_bytes());\n", ind, expr)
	case ir.TFloat64:
		g.pf("%sb[at..at + 8].copy_from_slice(&%s.to_le_bytes());\n", ind, expr)
	default:
		raw := fixedRawUint(width)
		if _, wide := fixedWide128(f); wide {
			// THE 128-BIT SLOT IS THE ALIGNED WRAPPER (fixed_runtime's
			// TableU128 / TableI128) and `.0` is the integer; its little-endian
			// image is the same sixteen bytes either signedness.
			g.pf("%sb[at..at + 16].copy_from_slice(&%s.0.to_le_bytes());\n", ind, expr)
			return
		}
		if cookElemType(f) == raw {
			// already the raw storage image; a cast to its own type is noise
			g.pf("%sb[at..at + %d].copy_from_slice(&%s.to_le_bytes());\n", ind, width, expr)
			return
		}
		g.pf("%sb[at..at + %d].copy_from_slice(&(%s as %s).to_le_bytes());\n", ind, width, expr, raw)
	}
}

// fixedWide128 names the aligned wrapper a 128-bit row slot is, and says
// whether the field is one at all. See TableU128 in the fixed runtime for why
// a bare u128 will not do.
func fixedWide128(f *ir.Field) (string, bool) {
	if f == nil || f.Type.Kind != ir.TInt && f.Type.Kind != ir.TFixed || f.Type.Width <= 64 {
		return "", false
	}
	if f.Type.Signed {
		return "TableI128", true
	}
	return "TableU128", true
}

// fixedSlotOffset is one slot's offset expression inside a loop, with the
// no-op addition a base of zero would spell left out.
func fixedSlotOffset(base, elem int64) string {
	step := fmt.Sprintf("i * %d", elem)
	if elem == 1 {
		step = "i"
	}
	if base == 0 {
		return step
	}
	return fmt.Sprintf("%d + %s", base, step)
}

// fixedRawUint is the unsigned Rust type a store of `width` bytes goes
// through: the raw storage image, which is what the wire carries.
func fixedRawUint(width int64) string {
	switch width {
	case 1:
		return "u8"
	case 2:
		return "u16"
	case 4:
		return "u32"
	case 16:
		return "u128"
	}
	return "u64"
}

// ---- the scatter -----------------------------------------------------------

// emitFixedScatter lands ONE record image in the value. It is the packet
// codec's reader in shape — a straight line of loads by field NAME — and it is
// the ONLY place this port's storage spelling meets the wire's, which is what
// keeps the plan free of storage offsets entirely.
func (g *gen) emitFixedScatter(st *ir.Struct) {
	g.pf("/// %s: one record image landed in the value, field by field.\n", st.Name)
	g.pf("///\n")
	g.pf("/// Every field is written, so nothing of the caller's previous value survives:\n")
	g.pf("/// a field the record did not carry took its declared default in the PREFILL,\n")
	g.pf("/// which is §3.4's whole answer to an absent field.\n")
	g.pf("pub fn %s(b: &[u8], value: &mut %sRow, report: &mut TableFixedReport) {\n",
		fn(st.Name, "fixed_scatter"), st.Name)
	if len(st.Fields) == 0 {
		g.pf("    let _ = (b, value, report);\n")
	}
	g.pf("    let _ = &report;\n")
	off := int64(0)
	for _, f := range st.Fields {
		g.emitFixedScatterField(f, off)
		off += ir.TableFixedFieldBytes(f)
	}
	g.pf("}\n\n")
}

func (g *gen) emitFixedScatterField(f *ir.Field, off int64) {
	base := off
	if f.Type.Optional {
		g.pf("    value.%s_present = b[%d] != 0;\n", f.Name, base)
		base += ir.TableFixedPresentBytes
	}
	switch {
	case f.KeyEnum != "":
		g.emitFixedScatterSlots(f, base, f.KeyEnumRef.Max)
	case f.Array == ir.ArrayFixed:
		g.emitFixedScatterSlots(f, base, f.ArrayBound)
	case f.Array == ir.ArrayCounted:
		g.emitFixedScatterCount(f.Name+"_count", base, f.ArrayBound)
		g.emitFixedScatterSlots(f, base+ir.TableFixedCountBytes, f.ArrayBound)
	case f.Type.Kind == ir.TString:
		g.emitFixedScatterCount(f.Name+"_length", base, f.Type.Size)
		g.pf("    value.%s = [0u8; %d];\n", f.Name, f.Type.Size+1)
		g.pf("    value.%s[..%d].copy_from_slice(&b[%d..%d]);\n", f.Name, f.Type.Size, base+4, base+4+f.Type.Size)
		g.pf("    value.%s[value.%s_length as usize] = 0; // the used length terminates the buffer\n", f.Name, f.Name)
	case f.Type.Kind == ir.TWString:
		// WIDE TEXT lands unit by unit: the buffer is [u16; N + 1] and the wire
		// is 2 * N little-endian code units, so there is no one-move copy here
		// the way the narrow twin has.
		g.emitFixedScatterCount(f.Name+"_length", base, f.Type.Size)
		g.pf("    value.%s = [0u16; %d];\n", f.Name, f.Type.Size+1)
		g.pf("    for i in 0..%d {\n", f.Type.Size)
		g.pf("        let at = %d + 2 * i;\n", base+4)
		g.pf("        value.%s[i] = u16::from_le_bytes(b[at..at + 2].try_into().expect(\"two bytes\"));\n", f.Name)
		g.pf("    }\n")
		g.pf("    value.%s[value.%s_length as usize] = 0; // the used length terminates the buffer\n", f.Name, f.Name)
	case f.Type.Kind == ir.TBytes:
		g.emitFixedScatterCount(f.Name+"_length", base, f.Type.Size)
		g.pf("    value.%s.copy_from_slice(&b[%d..%d]);\n", f.Name, base+4, base+4+f.Type.Size)
	default:
		g.emitFixedScatterElement(f, fmt.Sprintf("%d", base), "value."+f.Name, 4)
	}
}

// emitFixedScatterCount is §3.4's `count` op inline: read it, clamp it to THIS
// reader's own bound, and count one clamped if it fired.
func (g *gen) emitFixedScatterCount(member string, at, bound int64) {
	g.pf("    {\n")
	g.pf("        let raw = i32::from_le_bytes(b[%d..%d].try_into().expect(\"four bytes\"));\n", at, at+4)
	g.pf("        let held = raw.clamp(0, %d);\n", bound)
	g.pf("        if held != raw {\n            report.clamped += 1;\n        }\n")
	g.pf("        value.%s = held;\n", member)
	g.pf("    }\n")
}

func (g *gen) emitFixedScatterSlots(f *ir.Field, base, count int64) {
	elem := ir.TableFixedElementBytes(f)
	if count == 0 {
		return
	}
	g.pf("    for i in 0..%d {\n", count)
	g.emitFixedScatterElement(f, fixedSlotOffset(base, elem), fmt.Sprintf("value.%s[i]", f.Name), 8)
	g.pf("    }\n")
}

func (g *gen) emitFixedScatterElement(f *ir.Field, at, dst string, indent int) {
	ind := strings.Repeat(" ", indent)
	if name, n, nested := fixedNestedBody(f); nested {
		g.pf("%s{\n%s    let at = %s;\n", ind, ind, at)
		g.pf("%s    %s(&b[at..at + %d], &mut %s, report);\n", ind, fn(name, "fixed_scatter"), n, dst)
		g.pf("%s}\n", ind)
		return
	}
	width := ir.TableFixedElementBytes(f)
	g.pf("%s{\n%s    let at = %s;\n", ind, ind, at)
	switch f.Type.Kind {
	case ir.TBool:
		g.pf("%s    %s = b[at] != 0;\n", ind, dst)
	case ir.TFloat32:
		g.pf("%s    %s = f32::from_le_bytes(b[at..at + 4].try_into().expect(\"four bytes\"));\n", ind, dst)
	case ir.TFloat64:
		g.pf("%s    %s = f64::from_le_bytes(b[at..at + 8].try_into().expect(\"eight bytes\"));\n", ind, dst)
	default:
		raw := fixedRawUint(width)
		g.pf("%s    let raw = %s::from_le_bytes(b[at..at + %d].try_into().expect(\"the declared width\"));\n",
			ind, raw, width)
		slot := g.fixedSlotType(f)
		held := "raw"
		if wrap, wide := fixedWide128(f); wide {
			// the aligned wrapper takes the integer, and a signed one takes it
			// two's complement at the same sixteen bytes
			held = wrap + "(raw)"
			if f.Type.Signed {
				held = wrap + "(raw as i128)"
			}
		} else if slot != raw {
			held = "raw as " + slot
		}
		if extent, ranged := fixedEnumExtent(f, width); ranged {
			// AN ORDINAL PAST THE DECLARED EXTENT IS OUT OF RANGE, exactly as a
			// union tag past the arm count is (see the union's scatter below):
			// it lands as None and counts one clamped rather than arriving as a
			// number this build has no variant for. The identity plan copies
			// the ordinal verbatim out of a stranger's record, so the scatter is
			// where the range is closed — the same place a count's bound is.
			g.pf("%s    if raw > %d {\n", ind, extent)
			g.pf("%s        report.clamped += 1;\n", ind)
			g.pf("%s        %s = 0;\n", ind, dst)
			g.pf("%s    } else {\n", ind)
			g.pf("%s        %s = %s;\n", ind, dst, held)
			g.pf("%s    }\n", ind)
		} else {
			g.pf("%s    %s = %s;\n", ind, dst, held)
		}
	}
	g.pf("%s}\n", ind)
}

// fixedEnumExtent answers an ENUM leaf's top wire value and whether a range
// check on it is worth emitting at all: an enum whose extent is every value its
// storage width holds has no out-of-range ordinal to close, and a comparison
// there would be one the type's own limits make useless — which rustc says out
// loud.
func fixedEnumExtent(f *ir.Field, width int64) (int64, bool) {
	if f == nil || f.Type.Kind != ir.TNamed {
		return 0, false
	}
	en, ok := f.Type.Ref.(*ir.Enum)
	if !ok {
		return 0, false
	}
	if en.Max >= tagMax(width) {
		return 0, false
	}
	return en.Max, true
}

// fixedSlotType is ONE slot's Rust type in the Row — the cooked spelling,
// because the Row IS the cooked record (docs/SPEC-TABLES.md §7.2).
func (g *gen) fixedSlotType(f *ir.Field) string { return cookElemType(f) }

// fixedNestedBody names the generated write/scatter pair one element goes
// through when the element is a DECLARATION rather than a leaf, and the
// constant size of it. A UNION IS ONE OF THOSE: its pair is emitted beside a
// record's and called exactly the same way, which is what keeps a union inside
// an array, or inside a nested type, from being a case anywhere else in this
// file.
func fixedNestedBody(f *ir.Field) (name string, size int64, ok bool) {
	if f == nil || f.Type.Kind != ir.TNamed {
		return "", 0, false
	}
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		return r.Name, ir.TableFixedTypeBytes(r), true
	case *ir.Union:
		return r.Name, ir.TableFixedUnionBytes(r), true
	}
	return "", 0, false
}

// ---- the union's stores and its scatter (docs/SPEC-TABLES.md §3.4, §15) ----

// emitFixedWriteUnion is a union's stores: the TAG at its own storage width and
// then the LIVE ARM ONLY. Everything past the arm is DECLARED SLACK and it
// rides ZERO — not because this function writes zeros, but because it does not
// touch those bytes and the template already did (§3.4). A None union is
// therefore a tag and nothing else, which is exactly the C reference's switch
// with no matching case.
func (g *gen) emitFixedWriteUnion(un *ir.Union) {
	tagw := ir.TableFixedUnionTagBytes(un)
	g.pf("/// %s's stores: the TAG at its declared storage width, then the LIVE ARM.\n", un.Name)
	g.pf("/// The bytes between a narrower arm and the widest are declared slack and\n")
	g.pf("/// they stay the template's zeros, which this function reaches by not\n")
	g.pf("/// touching them (§3.4).\n")
	g.pf("pub fn %s(b: &mut [u8], value: &%sRow) {\n", fn(un.Name, "fixed_write_body"), un.Name)
	if tagw == 1 {
		g.pf("    b[0] = value.tag;\n")
	} else {
		g.pf("    b[0..%d].copy_from_slice(&value.tag.to_le_bytes());\n", tagw)
	}
	g.pf("    match value.tag {\n")
	for i, v := range un.Variants {
		g.pf("        %d => {\n", i+1)
		g.pf("            // SAFETY: THE TAG NAMES THE LIVE ARM — the twin's whole contract,\n")
		g.pf("            // and the same one the C reference's storage struct carries. The\n")
		g.pf("            // tag was just matched against `%s`, every arm is plain data\n", v.Name)
		g.pf("            // with no niche in it, and the overlay's zero form is a value of\n")
		g.pf("            // every arm — so a Row whose tag was set without its arm reads a\n")
		g.pf("            // zero payload rather than an invalid one.\n")
		g.pf("            let arm = unsafe { value.arms.%s };\n", v.Name)
		g.emitFixedWriteElementAt(v.F, fmt.Sprintf("%d", tagw), "arm", 12)
		g.pf("        }\n")
	}
	g.pf("        _ => {} // None, and any tag no arm of this build names: the tag is all of it\n")
	g.pf("    }\n}\n\n")
}

// emitFixedScatterUnion lands one union's image in the value: the TAG, clamped
// to this build's own arm count, and then the arm the tag selects.
//
// A TAG BEYOND THE ARM COUNT LANDS AS NONE AND COUNTS ONE CLAMPED. It is the
// same rule a count gets and for the same reason — the image is a stranger's
// bytes on the identity path, where the tag rides verbatim, and an ordinal past
// the set is out of range exactly as a count past the bound is. On the compiled
// path the runtime has already written MY ordinal under THEIR tag's guard, so
// the value arriving here is one of mine and the clamp does not fire.
//
// THE ARM IS RECONSTRUCTED AND THEN WRITTEN, which is the packet codec's reader
// shape ("every read reconstructs the selected payload") and also what keeps
// this side free of `unsafe`: a WRITE of a union field is safe in Rust, and
// only a read is not.
func (g *gen) emitFixedScatterUnion(un *ir.Union) {
	tagw := ir.TableFixedUnionTagBytes(un)
	raw := fixedRawUint(tagw)
	g.pf("/// %s: one union image landed in the value — the tag, then the live arm.\n", un.Name)
	g.pf("pub fn %s(b: &[u8], value: &mut %sRow, report: &mut TableFixedReport) {\n",
		fn(un.Name, "fixed_scatter"), un.Name)
	g.pf("    // nothing of the caller's previous value survives: the empty union first,\n")
	g.pf("    // and then the tag and the one arm it names\n")
	g.pf("    *value = %sRow::default();\n", un.Name)
	if tagw == 1 {
		g.pf("    let tag_raw = b[0];\n")
	} else {
		g.pf("    let tag_raw = %s::from_le_bytes(b[0..%d].try_into().expect(\"the tag's width\"));\n", raw, tagw)
	}
	if int64(len(un.Variants)) < tagMax(tagw) {
		g.pf("    // A TAG PAST THE ARM COUNT IS OUT OF RANGE, as a count past its bound is:\n")
		g.pf("    // it lands as None and counts one clamped rather than selecting nothing\n")
		g.pf("    // silently.\n")
		g.pf("    if tag_raw > %d {\n        report.clamped += 1;\n    } else {\n        value.tag = tag_raw;\n    }\n", len(un.Variants))
	} else {
		// EVERY VALUE OF THE TAG'S WIDTH NAMES AN ARM, so there is no
		// out-of-range tag to clamp and a comparison here would be one the
		// type's own limits make useless — which rustc says out loud.
		g.pf("    // every value the tag's width holds names an arm of this build, so there\n")
		g.pf("    // is no out-of-range ordinal here to clamp\n")
		g.pf("    value.tag = tag_raw;\n")
	}
	g.pf("    match value.tag {\n")
	for i, v := range un.Variants {
		g.pf("        %d => {\n", i+1)
		g.pf("            let mut arm = %s::default();\n", cookElemType(v.F))
		g.emitFixedScatterElement(v.F, fmt.Sprintf("%d", tagw), "arm", 12)
		g.pf("            value.arms.%s = arm; // a WRITE of a union field, which is safe\n", v.Name)
		g.pf("        }\n")
	}
	g.pf("        _ => {} // None: the overlay stays the zeros the default laid down\n")
	g.pf("    }\n}\n\n")
}

// ---- the root's surface ----------------------------------------------------

func (g *gen) emitFixedRoot(st *ir.Struct) {
	entries := ir.TableFixedWalkRoot(st)
	block := ir.TableFixedLayoutBytes(entries)
	hash := ir.TableFixedLayoutHash(block)
	body := ir.TableFixedTypeBytes(st)
	defaults := fixedDefaultImage(st)
	up := ir.RustConstName(st.Name)

	g.pf("// ---- %s, the fixed form ----\n\n", st.Name)
	g.pf("/// The body is the SAME SIZE for every value the type can hold, so a measure\n")
	g.pf("/// is a constant and not a walk (docs/SPEC-TABLES.md §3.4).\n")
	g.pf("pub const %s_FIXED_BODY_BYTES: usize = %d;\n", up, body)
	g.pf("pub const %s_FIXED_RECORD_BYTES: usize = 8 + %s_FIXED_BODY_BYTES; // the hash and the body\n", up, up)
	g.pf("/// fnv1a64 over the layout's bytes, exactly as written.\n")
	g.pf("pub const %s_FIXED_HASH: u64 = 0x%016x;\n\n", up, hash)

	g.pf("/// THE LAYOUT (form 1 calls this the vocabulary block): %d entries, a\n", len(entries))
	g.pf("/// PRE-ORDER walk of the closure in the writer's declared order. Every byte\n")
	g.pf("/// is settled by the compiler, and by ir's walk — the one every port renders.\n")
	g.pf("pub const %s_FIXED_BLOCK: [u8; %d] = [\n", up, len(block))
	g.emitFixedByteArray(block)
	g.pf("];\n\n")

	g.pf("/// THE PREFILL: the declared defaults as a record image. A field a record\n")
	g.pf("/// does not carry has no plan entry, so these bytes are what it keeps.\n")
	g.pf("pub const %s_FIXED_DEFAULTS: [u8; %d] = [\n", up, len(defaults))
	g.emitFixedByteArray(defaults)
	g.pf("];\n\n")

	g.pf("/// MY side of the layout, one byte an entry: whether the entry is an array\n")
	g.pf("/// that carries a live COUNT in front of it, which is the one storage fact\n")
	g.pf("/// a layout entry cannot state and the plan compiler needs.\n")
	g.pf("pub const %s_FIXED_COUNTED: [u8; %d] = [", up, len(entries))
	for i, e := range entries {
		if i%32 == 0 {
			g.pf("\n   ")
		}
		v := 0
		if fixedCounted(e) {
			v = 1
		}
		g.pf(" %d,", v)
	}
	g.pf("\n];\n\n")

	g.pf("/// THE IDENTITY PLAN, and it is ONE ENTRY: in the image domain the record's\n")
	g.pf("/// declared order IS the destination's, so §3.4's coalescing rule takes the\n")
	g.pf("/// whole body to a single move.\n")
	g.pf("pub const %s_FIXED_PLAN: [TableFixedEntry; 1] = [TableFixedEntry {\n", up)
	g.pf("    src: 0,\n    dst: 0,\n    size: %s_FIXED_BODY_BYTES as u32,\n    aux: 0,\n", up)
	g.pf("    guard: TABLE_FIXED_NO_GUARD,\n    op: TableFixedOp::Copy,\n    arg: 0,\n    argw: 1,\n    dstsize: 0,\n    sign: 0,\n}];\n\n")

	name := ir.RustSnake(st.Name)
	g.pf("/// A FILE: THE PINNED HEADER (docs/SPEC-TABLES.md §3, one rule for all five\n")
	g.pf("/// forms) — the form byte, seven RESERVED ZERO bytes, the LAYOUT HASH at 8\n")
	g.pf("/// and the body at 16 — then the layout behind its u32 length, then the\n")
	g.pf("/// records back to back to the end of it (§3.4).\n")
	g.pf("pub const fn %s_fixed_measure(count: usize) -> usize {\n", name)
	g.pf("    TABLE_FIXED_HEADER_BYTES + 4 + %s_FIXED_BLOCK.len() + count * %s_FIXED_RECORD_BYTES\n}\n\n", up, up)

	g.pf("/// Writes `values` as a whole fixed-form FILE. Returns the bytes written, or\n")
	g.pf("/// None when the buffer is smaller than %s_fixed_measure says.\n", name)
	g.pf("pub fn %s_fixed_save(values: &[%sRow], buffer: &mut [u8]) -> Option<usize> {\n", name, st.Name)
	g.pf("    let need = %s_fixed_measure(values.len());\n", name)
	g.pf("    if buffer.len() < need {\n        return None;\n    }\n")
	g.pf("    buffer[..TABLE_FIXED_HEADER_BYTES].fill(0); // the seven reserved bytes, and the rest of the header\n")
	g.pf("    buffer[0] = TABLE_FIXED_FORM;\n")
	g.pf("    buffer[TABLE_FIXED_HASH_AT..TABLE_FIXED_HASH_AT + 8]\n")
	g.pf("        .copy_from_slice(&%s_FIXED_HASH.to_le_bytes()); // the header names the layout ONCE\n", up)
	g.pf("    buffer[TABLE_FIXED_HEADER_BYTES..TABLE_FIXED_HEADER_BYTES + 4]\n")
	g.pf("        .copy_from_slice(&(%s_FIXED_BLOCK.len() as u32).to_le_bytes());\n", up)
	g.pf("    let mut at = TABLE_FIXED_HEADER_BYTES + 4;\n")
	g.pf("    buffer[at..at + %s_FIXED_BLOCK.len()].copy_from_slice(&%s_FIXED_BLOCK);\n", up, up)
	g.pf("    at += %s_FIXED_BLOCK.len();\n", up)
	g.pf("    for value in values {\n")
	g.pf("        buffer[at..at + 8].copy_from_slice(&%s_FIXED_HASH.to_le_bytes());\n", up)
	g.pf("        let body = &mut buffer[at + 8..at + %s_FIXED_RECORD_BYTES];\n", up)
	g.pf("        body.fill(0); // the template's zeros, and every byte of declared slack with them\n")
	g.pf("        %s(body, value);\n", fn(st.Name, "fixed_write_body"))
	g.pf("        at += %s_FIXED_RECORD_BYTES;\n    }\n", up)
	g.pf("    Some(need)\n}\n\n")

	g.pf("/// THE READ: a prefill and ONE loop over ONE plan, then the scatter. The\n")
	g.pf("/// identity plan when the file's layout hashes to this build's own, a plan\n")
	g.pf("/// compiled once from the writer's layout otherwise — SAME LOOP either way,\n")
	g.pf("/// which is the owner's ruling that this form's cost must not move when a\n")
	g.pf("/// peer ships (docs/SPEC-TABLES.md §3.4).\n")
	g.pf("///\n")
	g.pf("/// The plan's storage is the CALLER's, declared by capacity: this codec never\n")
	g.pf("/// allocates, and a layout whose plan does not fit is a refusal by name.\n")
	g.pf("/// Returns the records read, or None on a refusal `report` names.\n")
	g.pf("pub fn %s_fixed_load(\n", name)
	g.pf("    values: &mut [%sRow],\n", st.Name)
	g.pf("    data: &[u8],\n")
	g.pf("    plan: &mut [TableFixedEntry],\n")
	g.pf("    remap: &mut [u16],\n")
	g.pf("    report: &mut TableFixedReport,\n")
	g.pf(") -> Option<usize> {\n")
	g.pf("    if data.len() < TABLE_FIXED_HEADER_BYTES + 4 {\n        report.malformed = true;\n        return None;\n    }\n")
	g.pf("    if data[0] != TABLE_FIXED_FORM {\n        return report.refuse(TableFixedReason::NewerForm);\n    }\n")
	g.pf("    let block_bytes = u32::from_le_bytes(\n")
	g.pf("        data[TABLE_FIXED_HEADER_BYTES..TABLE_FIXED_HEADER_BYTES + 4]\n")
	g.pf("            .try_into()\n            .expect(\"four bytes\"),\n    ) as usize;\n")
	g.pf("    if block_bytes > data.len() - TABLE_FIXED_HEADER_BYTES - 4 {\n")
	g.pf("        return report.refuse(TableFixedReason::BlockMalformed);\n    }\n")
	g.pf("    let block = &data[TABLE_FIXED_HEADER_BYTES + 4..TABLE_FIXED_HEADER_BYTES + 4 + block_bytes];\n")
	g.pf("    let hash = table_fixed_hash(block);\n")
	g.pf("    let rest = &data[TABLE_FIXED_HEADER_BYTES + 4 + block_bytes..];\n\n")
	g.pf("    // WHICH PLAN, and that is the only thing that differs between reading this\n")
	g.pf("    // build's own record and reading anybody else's.\n")
	g.pf("    let mut compiled = 0usize;\n")
	g.pf("    let mut record_bytes = %s_FIXED_RECORD_BYTES;\n", up)
	g.pf("    let identity = hash == %s_FIXED_HASH;\n", up)
	g.pf("    if !identity {\n")
	g.pf("        let theirs = match TableFixedBlock::parse(block) {\n")
	g.pf("            Some(b) => b,\n")
	g.pf("            None => return report.refuse(TableFixedReason::BlockMalformed),\n        };\n")
	g.pf("        let mine = match TableFixedBlock::parse(&%s_FIXED_BLOCK) {\n", up)
	g.pf("            Some(b) => b,\n")
	g.pf("            None => return report.refuse(TableFixedReason::BlockMalformed),\n        };\n")
	g.pf("        compiled = match table_fixed_compile(&theirs, &mine, &%s_FIXED_COUNTED, plan, remap, report) {\n", up)
	g.pf("            Some(n) => n,\n")
	g.pf("            None => return report.refuse(TableFixedReason::PlanTooLarge),\n        };\n")
	g.pf("        record_bytes = 8 + theirs.entry(0).size as usize;\n")
	g.pf("    }\n")
	g.pf("    // THE HEADER NAMES THE LAYOUT ONCE, and it is checked LAST of the three:\n")
	g.pf("    // the layout's own rules each refuse under their own name first, so a\n")
	g.pf("    // broken layout is never reported as a lying header (§3).\n")
	g.pf("    if u64::from_le_bytes(\n")
	g.pf("        data[TABLE_FIXED_HASH_AT..TABLE_FIXED_HASH_AT + 8]\n")
	g.pf("            .try_into()\n            .expect(\"eight bytes\"),\n    ) != hash\n    {\n")
	g.pf("        return report.refuse(TableFixedReason::BlockMalformed);\n    }\n")
	g.pf("    if record_bytes <= 8 || !rest.len().is_multiple_of(record_bytes) {\n")
	g.pf("        report.malformed = true;\n        return None;\n    }\n")
	g.pf("    let n = rest.len() / record_bytes;\n")
	g.pf("    if n > values.len() {\n        return report.refuse(TableFixedReason::BatchTooLarge);\n    }\n")
	g.pf("    let entries: &[TableFixedEntry] = if identity { &%s_FIXED_PLAN } else { &plan[..compiled] };\n", up)
	g.pf("    let mut image = [0u8; %s_FIXED_BODY_BYTES];\n", up)
	g.pf("    for k in 0..n {\n")
	g.pf("        let record = &rest[k * record_bytes..(k + 1) * record_bytes];\n")
	g.pf("        if u64::from_le_bytes(record[..8].try_into().expect(\"eight bytes\")) != hash {\n")
	g.pf("            return report.refuse(TableFixedReason::NoBlock);\n        }\n")
	g.pf("        image.copy_from_slice(&%s_FIXED_DEFAULTS); // the declared defaults, one prefill\n", up)
	g.pf("        table_fixed_run(entries, remap, &record[8..], &mut image, report);\n")
	g.pf("        %s(&image, &mut values[k], report);\n", fn(st.Name, "fixed_scatter"))
	g.pf("    }\n    Some(n)\n}\n\n")
}

func (g *gen) emitFixedByteArray(b []byte) {
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
