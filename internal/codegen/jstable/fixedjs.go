// THE FIXED FORM'S EMISSION for JavaScript (docs/SPEC-TABLES.md §3.4): the
// write template, the identity plan, and the straight-line projection between
// the canonical body image and the language's own objects.
//
// THE SHAPE IS THE PACKET WIRE'S, not the variable table's. The owner's rule
// for every language leg is that the port looks at the equivalent PACKET codec
// and takes its shape, because the fixed form is far closer to that than to the
// id-table wire — and in JavaScript that is the FLAT TIER of
// internal/codegen/js: a caller-owned DataView, `true` passed as the
// little-endian flag at every single call, constant offsets and constant
// widths inlined at each field, a destination object the reader FILLS rather
// than returns, storage preallocated in the class constructor and never
// replaced, and a verdict — a byte count or `-1`, a count or `-1` — instead of
// an exception. Nothing here throws and nothing here allocates per record.
//
// WHAT IT DOES NOT TAKE FROM THE FLAT TIER is the bitpacker: the flat tier
// stages a 32-bit word and a bit cursor because the packet wire is bit-packed.
// The fixed form has no bit cursor at all — every field rides at its DECLARED
// STORAGE WIDTH at a constant byte offset — so the staging word, the window
// loads and the whole `br`-lag discipline are gone, and what is left is the
// store itself.
package jstable

import (
	"fmt"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---------------------------------------------------------------------------
// WHAT THIS BACKEND REFUSES, BY NAME
// ---------------------------------------------------------------------------

// fixedRefusal answers why a table cannot carry the fixed form in JavaScript,
// or "" when it can. NAMED, NEVER SILENT is the property: a table this backend
// will not emit the form for gets a line in the generated module saying which
// table, which field and why, so a consumer reaching for Save gets a missing
// name from its own tooling beside a file that says why.
func fixedRefusal(st *ir.Struct) string {
	return ir.TableFixedJSRefusal(st)
}

// ---------------------------------------------------------------------------
// THE VALUE DOMAIN: Number or BigInt
// ---------------------------------------------------------------------------

// fixedIsBig reports whether a leaf's JavaScript storage is a BigInt. The rule
// is internal/codegen/js's own (js.go: "Number for wire widths <= 32, BigInt
// for 64 and 128"), read through the DECLARED STORAGE WIDTH this form rides
// at: eight bytes or more of INTEGER is a BigInt, and a float never is however
// wide it is. `flags` is a BigInt at every declared width because flags store
// uint64 in every target.
func fixedIsBig(t ir.FieldType) bool {
	switch t.Kind {
	case ir.TFloat32, ir.TFloat64, ir.TBool:
		return false
	case ir.TNamed:
		switch t.Ref.(type) {
		case *ir.Flags:
			return true
		case *ir.Enum:
			return false
		}
		return false
	}
	return fixedStorageBytes(t) >= 8
}

// fixedSigned reports whether a leaf's storage image is two's complement.
func fixedSigned(t ir.FieldType) bool {
	switch t.Kind {
	case ir.TInt, ir.TFixed:
		return t.Signed
	}
	return false
}

// fixedSetter and fixedGetter are the DataView calls for one leaf, at its
// declared storage width, with the little-endian flag passed explicitly at
// every call — the flat tier's rule and the block form's rule alike.
func fixedSetter(t ir.FieldType) (string, bool) {
	w := fixedStorageBytes(t)
	switch t.Kind {
	case ir.TBool:
		return "setUint8", false
	case ir.TFloat32:
		// a table float is finite (docs/SPEC-TABLES.md §3.3): the content pass
		// refuses a NaN or an infinity on its raw bits before a load, and every
		// finite value rides the ordinary conversion bit for bit, -0 and the
		// subnormals included
		return "setFloat32", true
	case ir.TFloat64:
		return "setFloat64", true
	}
	if _, ok := t.Ref.(*ir.Flags); ok && t.Kind == ir.TNamed {
		return "setBigUint64", true
	}
	signed := fixedSigned(t)
	switch w {
	case 1:
		if signed {
			return "setInt8", false
		}
		return "setUint8", false
	case 2:
		if signed {
			return "setInt16", true
		}
		return "setUint16", true
	case 4:
		if signed {
			return "setInt32", true
		}
		return "setUint32", true
	case 8:
		if signed {
			return "setBigInt64", true
		}
		return "setBigUint64", true
	}
	return "", false
}

func fixedGetter(t ir.FieldType) (string, bool) {
	s, le := fixedSetter(t)
	return "g" + strings.TrimPrefix(s, "s"), le
}

// ---------------------------------------------------------------------------
// THE GENERATOR
// ---------------------------------------------------------------------------

type fixedGen struct {
	unit *ir.Unit
	body strings.Builder
	// need registers the class and Init helper an emitted line reaches for, so
	// a helper declared in another module of the unit is imported rather than
	// emitted twice. The DECODE side needs it too, not just the class emitter:
	// an absent optional lands its payload's declared default, and for a
	// nested type that default is that type's own Init.
	need func(cls, typeName string)
	// loops is how deep the decode's emitted loops nest. A nested struct is
	// INLINED into its parent's projection (one straight function, the way the
	// flat packet reader is), so a loop inside an inlined element must not
	// shadow the loop variable its base offset is written in.
	loops int
	// inl numbers the locals an inlined struct element binds.
	inl int
}

// loopVar names the loop variable at the current decode nesting depth: `i` at
// the top, so a flat type's emission is unchanged, then i1, i2, ...
func (g *fixedGen) loopVar() string {
	if g.loops == 0 {
		return "i"
	}
	return fmt.Sprintf("i%d", g.loops)
}

func (g *fixedGen) require(cls, typeName string) {
	if cls == "" || g.need == nil {
		return
	}
	g.need(cls, typeName)
}

func (g *fixedGen) pf(format string, args ...any) { fmt.Fprintf(&g.body, format, args...) }

// call emits one DataView call at a constant offset.
func (g *fixedGen) store(ind, view, at string, t ir.FieldType, expr string) {
	if fixedStorageBytes(t) == 16 {
		// SIXTEEN BYTES, THE LOW 64-BIT HALF FIRST, which is this wire's order
		// for the family everywhere else (docs/SPEC-TABLES.md §3). JavaScript
		// holds a 128-bit value as one BigInt, so the halves are split here.
		g.pf("%s%s.setBigUint64(%s, BigInt.asUintN(64, %s), true);\n", ind, view, at, expr)
		g.pf("%s%s.setBigUint64(%s, BigInt.asUintN(64, %s >> 64n), true);\n", ind, view, off(at, 8), expr)
		return
	}
	call, le := fixedSetter(t)
	if call == "" {
		g.pf("%s// no storage image for this leaf\n", ind)
		return
	}
	if t.Kind == ir.TBool {
		g.pf("%s%s.%s(%s, %s ? 1 : 0);\n", ind, view, call, at, expr)
		return
	}
	if le {
		g.pf("%s%s.%s(%s, %s, true);\n", ind, view, call, at, expr)
		return
	}
	g.pf("%s%s.%s(%s, %s);\n", ind, view, call, at, expr)
}

func (g *fixedGen) load(ind, lhs, view, at string, t ir.FieldType) {
	if fixedStorageBytes(t) == 16 {
		// A ZERO HIGH HALF IS THE LOW HALF, with no shift and no OR: two BigInts
		// fewer per field. Otherwise a signed value takes its high half SIGNED,
		// which is the same number BigInt.asIntN(128, ...) made, one BigInt sooner.
		hi := "getBigUint64"
		if fixedSigned(t) {
			hi = "getBigInt64"
		}
		g.pf("%s%s = (%s.getUint32(%s, true) | %s.getUint32(%s, true)) === 0\n", ind, lhs, view, off(at, 8), view, off(at, 12))
		g.pf("%s  ? %s.getBigUint64(%s, true)\n", ind, view, at)
		g.pf("%s  : (%s.%s(%s, true) << 64n) | %s.getBigUint64(%s, true);\n", ind, view, hi, off(at, 8), view, at)
		return
	}
	call, le := fixedGetter(t)
	if call == "" {
		g.pf("%s// no storage image for this leaf\n", ind)
		return
	}
	if t.Kind == ir.TBool {
		g.pf("%s%s = %s.getUint8(%s) !== 0;\n", ind, lhs, view, at)
		return
	}
	if le {
		g.pf("%s%s = %s.%s(%s, true);\n", ind, lhs, view, call, at)
		return
	}
	g.pf("%s%s = %s.%s(%s);\n", ind, lhs, view, call, at)
}

// off renders a constant byte offset expression: a literal when the base is
// the record's own start, and base + literal inside a nested walk.
func off(base string, at int64) string {
	if base == "" {
		return fmt.Sprintf("%d", at)
	}
	if at == 0 {
		return base
	}
	return fmt.Sprintf("%s + %d", base, at)
}

// ---------------------------------------------------------------------------
// THE WRITE: a template, then stores at constant offsets
// ---------------------------------------------------------------------------

// emitWriteBody emits <T>FixedWriteBody( view, at, value ): the straight line
// of stores for one type. The template — the hash, then zeros — is written
// first by the caller, which is also what zero-fills every byte of declared
// slack without this function touching it.
func (g *fixedGen) emitWriteBody(st *ir.Struct) {
	g.pf("// %s's stores. Every offset is a constant of the type and nothing is\n", st.Name)
	g.pf("// measured: on this form MeasureBody is a constant, not a walk.\n")
	g.pf("export function %sFixedWriteBody(view, at, value) {\n", st.Name)
	var cur int64
	for _, f := range st.Fields {
		g.emitWriteField(f, "at", cur, "value", "  ")
		cur += fixedFieldBytes(f)
	}
	g.pf("}\n\n")
}

// emitWriteField writes one FIELD — the optional wrapper's present byte first
// where one rides, then the payload.
//
// THE OPTIONAL WRAPPER (§3.4's kind 35) is the one place this form and §2.3
// part company: the present byte rides at the field's own offset and the
// payload WHOLE behind it, so `?T` and a plain `T` are ONE BYTE APART here
// where the packet wire cannot tell them apart at all.
//
// AN ABSENT OPTIONAL'S PAYLOAD IS THE TEMPLATE'S ZEROS (docs/SPEC-TABLES.md
// §3.4). The payload rides WHOLE whether or not it is present, and when the
// flag is 0 what rides is zero. It is ONE `if` here and a rule nowhere else,
// because the template already put the zeros there — so an absent optional
// costs the writer the branch and not one store, and a caller's untouched
// payload storage never reaches the wire.
//
// THIS BRANCH WAS NOT ALWAYS HERE, and what it settles is worth stating. The
// payload used to be written from storage either way, matching the reference's
// line at the time, and that made the bytes a function of the writer's STORAGE
// rather than of its VALUE: a value that had its optional set and then cleared
// wrote different bytes from one that never set it. The reference's own union
// never had the property — an untaken arm keeps the template's zeros, because
// only the taken arm is written — so the two constructs disagreed inside one
// form. The reference settled it in favour of the union's rule, and this
// follows it.
func (g *fixedGen) emitWriteField(f *ir.Field, base string, at int64, val, ind string) {
	if f.Type.Optional {
		name := js.MemberName(f.Name)
		g.pf("%sview.setUint8(%s, %s.%sPresent ? 1 : 0);\n", ind, off(base, at), val, name)
		g.pf("%sif (%s.%sPresent) {\n", ind, val, name)
		g.emitWritePayload(f, base, at+fixedPresentBytes, val, ind+"  ")
		g.pf("%s}\n", ind)
		return
	}
	g.emitWritePayload(f, base, at, val, ind)
}

func (g *fixedGen) emitWritePayload(f *ir.Field, base string, at int64, val, ind string) {
	name := js.MemberName(f.Name)
	switch {
	case f.KeyEnum != "":
		// EVERY SLOT OF A KEYED ARRAY IS LIVE (§2.4): there is no count and no
		// slack, so the declared bound IS the loop.
		g.emitWriteLoop(f, base, at, strconv.FormatInt(f.KeyEnumRef.Max, 10), val+"."+name, ind)
	case f.Array == ir.ArrayFixed:
		// AND EVERY ELEMENT OF `[N]T` IS LIVE: min equals max, so no count rides.
		g.emitWriteLoop(f, base, at, strconv.FormatInt(f.ArrayBound, 10), val+"."+name, ind)
	case f.Array == ir.ArrayCounted:
		// THE COUNT IS THE LOOP AND THE SLACK STAYS THE TEMPLATE'S ZEROS
		// (docs/SPEC-TABLES.md §3.4). Writing all Max elements put the unused
		// slots' STORAGE on the wire, which for an array of a type with declared
		// defaults is the element's default image and not zero — a value nobody
		// wrote, riding as if somebody had.
		g.pf("%sview.setInt32(%s, %s.%sCount, true);\n", ind, off(base, at), val, name)
		g.emitWriteLoop(f, base, at+fixedCountBytes, fmt.Sprintf("%s.%sCount", val, name), val+"."+name, ind)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		// THE USED LENGTH, THEN THAT MANY UNITS, and the slack stays zero.
		// Copying the whole declared span instead put whatever the caller's
		// storage held past the used length on the wire, so a `string(N)`
		// written twice with two different values did not compare equal on the
		// second write.
		g.pf("%sview.setInt32(%s, %s.%sLength, true);\n", ind, off(base, at), val, name)
		g.pf("%sfor (let i = 0; i < %s.%sLength; i++) { view.setUint8(%s + i, %s.%s[i]); }\n",
			ind, val, name, off(base, at+fixedCountBytes), val, name)
	case f.Type.Kind == ir.TWString:
		// the length in CODE UNITS, then that many two-byte units
		g.pf("%sview.setInt32(%s, %s.%sLength, true);\n", ind, off(base, at), val, name)
		g.pf("%sfor (let i = 0; i < %s.%sLength; i++) { view.setUint16(%s + i * 2, %s.%s[i], true); }\n",
			ind, val, name, off(base, at+fixedCountBytes), val, name)
	default:
		g.emitWriteElement(f, base, at, val+"."+name, ind)
	}
}

func (g *fixedGen) emitWriteLoop(f *ir.Field, base string, at int64, count string, expr, ind string) {
	elem := fixedElementBytes(f)
	g.pf("%sfor (let i = 0; i < %s; i++) {\n", ind, count)
	g.emitWriteElement(f, fmt.Sprintf("%s + i * %d", off(base, at), elem), 0, expr+"[i]", ind+"  ")
	g.pf("%s}\n", ind)
}

func (g *fixedGen) emitWriteElement(f *ir.Field, base string, at int64, expr, ind string) {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			g.pf("%s%sFixedWriteBody(view, %s, %s);\n", ind, r.Name, off(base, at), expr)
			return
		case *ir.Union:
			tag := fixedUnionTagBytes(r)
			g.storeTag(ind, off(base, at), tag, expr+".type")
			g.pf("%sswitch (%s.type) {\n", ind, expr)
			for i, v := range r.Variants {
				g.pf("%s  case %d: {\n", ind, i+1)
				if v.F != nil {
					// A DIRECT ARM IS A FIELD LINE (docs/SPEC-TABLES.md §2.6):
					// a text, bytes or array arm writes through the SAME payload
					// emitters a field of its type does, with the arm's own name
					// as the member and the union object as the owner.
					g.emitWritePayload(v.F, base, at+tag, expr, ind+"    ")
				}
				g.pf("%s    break;\n%s  }\n", ind, ind)
			}
			g.pf("%s  default: break;\n%s}\n", ind, ind)
			return
		case *ir.Enum:
			// the ORDINAL is the variant's POSITION IN THE BLOCK, from 1, and
			// 0 is None — which is the value the declaration already carries
			g.storeTag(ind, off(base, at), int64(r.StorageBits/8), expr)
			return
		}
	}
	g.store(ind, "view", off(base, at), f.Type, expr)
}

// fixedFieldHasFloat is true for a field that holds a float32 or a float64,
// here or anywhere below it (a struct by value, a union arm, an array).
func fixedFieldHasFloat(f *ir.Field) bool {
	if f == nil {
		return false
	}
	if fixedIsFloat(f.Type) {
		return true
	}
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return slices.ContainsFunc(r.Fields, fixedFieldHasFloat)
		case *ir.Union:
			for _, v := range r.Variants {
				if fixedFieldHasFloat(v.F) {
					return true
				}
			}
		}
	}
	return false
}

// emitCheckBody emits <T>FixedCheckBody( value ), THE WRITER'S FINITE CHECK
// (docs/SPEC-TABLES.md §3.3; Glenn, 2026-10-27, "tables go finite-only too,
// same rule as packets"): false when any float the record would carry is a
// NaN or an infinity, a float32 one past the float32 range included, so the
// save refuses (-1) before it writes a byte. Only a type that holds a float
// gets one; the save of a root without one has nothing to check.
func (g *fixedGen) emitCheckBody(st *ir.Struct) {
	if !slices.ContainsFunc(st.Fields, fixedFieldHasFloat) {
		return
	}
	g.pf("// %s's floats, every one finite or the save refuses (docs/SPEC-TABLES.md §3.3).\n", st.Name)
	g.pf("export function %sFixedCheckBody(value) {\n", st.Name)
	for _, f := range st.Fields {
		if !fixedFieldHasFloat(f) {
			continue
		}
		if f.Type.Optional {
			name := js.MemberName(f.Name)
			g.pf("  if (value.%sPresent) {\n", name)
			g.emitCheckValuePayload(f, "value", "    ")
			g.pf("  }\n")
			continue
		}
		g.emitCheckValuePayload(f, "value", "  ")
	}
	g.pf("  return true;\n}\n\n")
}

func (g *fixedGen) emitCheckValuePayload(f *ir.Field, val, ind string) {
	name := js.MemberName(f.Name)
	switch {
	case f.KeyEnum != "":
		g.emitCheckValueLoop(f, strconv.FormatInt(f.KeyEnumRef.Max, 10), val+"."+name, ind)
	case f.Array == ir.ArrayFixed:
		g.emitCheckValueLoop(f, strconv.FormatInt(f.ArrayBound, 10), val+"."+name, ind)
	case f.Array == ir.ArrayCounted:
		g.emitCheckValueLoop(f, fmt.Sprintf("%s.%sCount", val, name), val+"."+name, ind)
	default:
		g.emitCheckValueElement(f, val+"."+name, ind)
	}
}

func (g *fixedGen) emitCheckValueLoop(f *ir.Field, count, expr, ind string) {
	iv := fmt.Sprintf("i%d", strings.Count(ind, "  "))
	g.pf("%sfor (let %s = 0; %s < %s; %s++) {\n", ind, iv, iv, count, iv)
	g.emitCheckValueElement(f, expr+"["+iv+"]", ind+"  ")
	g.pf("%s}\n", ind)
}

func (g *fixedGen) emitCheckValueElement(f *ir.Field, expr, ind string) {
	switch f.Type.Kind {
	case ir.TFloat32:
		g.pf("%sif (!Number.isFinite(Math.fround(%s))) { return false; }\n", ind, expr)
		return
	case ir.TFloat64:
		g.pf("%sif (!Number.isFinite(%s)) { return false; }\n", ind, expr)
		return
	}
	if f.Type.Kind != ir.TNamed {
		return
	}
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		g.pf("%sif (!%sFixedCheckBody(%s)) { return false; }\n", ind, r.Name, expr)
	case *ir.Union:
		g.pf("%sswitch (%s.type) {\n", ind, expr)
		for i, v := range r.Variants {
			if !fixedFieldHasFloat(v.F) {
				continue
			}
			g.pf("%s  case %d: {\n", ind, i+1)
			g.emitCheckValuePayload(v.F, expr, ind+"    ")
			g.pf("%s    break;\n%s  }\n", ind, ind)
		}
		g.pf("%s  default: break;\n%s}\n", ind, ind)
	}
}

// storeTag stores an ordinal at a width the block fixed: one, two, four — or
// EIGHT, which this language carries as TWO u32 LANES and says so (§5.9 #34).
// A single `setUint32` above width 2 wrote four bytes where the layout declares
// eight and left the high four holding whatever the template put there; a
// `<< (8*k)` loop would have been worse, because JavaScript shifts MOD 32 and
// byte 4 would land back on byte 0.
func (g *fixedGen) storeTag(ind, at string, width int64, expr string) {
	switch width {
	case 1:
		g.pf("%sview.setUint8(%s, %s);\n", ind, at, expr)
	case 2:
		g.pf("%sview.setUint16(%s, %s, true);\n", ind, at, expr)
	case 8:
		g.pf("%s{\n", ind)
		g.pf("%s  const t = Number(%s);\n", ind, expr)
		g.pf("%s  view.setUint32(%s, t >>> 0, true);\n", ind, at)
		g.pf("%s  view.setUint32((%s) + 4, Math.floor(t / 4294967296) >>> 0, true);\n", ind, at)
		g.pf("%s}\n", ind)
	default:
		g.pf("%sview.setUint32(%s, %s, true);\n", ind, at, expr)
	}
}

// loadTag reads an ordinal back out of the image. AT WIDTH 8 IT IS TWO LANES:
// a remap table is never longer than 65535 entries and an enum's or a union's
// declared extent never reaches 2^32, so a NONZERO HIGH LANE is an ordinal no
// declaration can hold. It lands as 2^32 — above every extent the clamps below
// compare against — so the ordinal is forced through §4.6's clamp, comes back
// None and counts one `clamped`, which is the one answer a forged eight-byte
// tag is owed (§5.4).
func (g *fixedGen) loadTag(ind, lhs, at string, width int64) {
	switch width {
	case 1:
		g.pf("%s%s = view.getUint8(%s);\n", ind, lhs, at)
	case 2:
		g.pf("%s%s = view.getUint16(%s, true);\n", ind, lhs, at)
	case 8:
		g.pf("%s{\n", ind)
		g.pf("%s  const lo = view.getUint32(%s, true);\n", ind, at)
		g.pf("%s  const hi = view.getUint32((%s) + 4, true);\n", ind, at)
		g.pf("%s  %s = hi !== 0 ? 4294967296 : lo;\n", ind, lhs)
		g.pf("%s}\n", ind)
	default:
		g.pf("%s%s = view.getUint32(%s, true);\n", ind, lhs, at)
	}
}

// ---------------------------------------------------------------------------
// THE DECODE: the canonical image into the language's own objects
// ---------------------------------------------------------------------------

// emitDecodeBody emits <T>FixedDecode( value, view, at ): the straight line
// that projects the reader's own image into the language's objects.
//
// THIS IS THE STEP C++ GETS FOR FREE. There a struct IS its bytes, so the plan
// lands values in the value itself and the bounds pass walks that storage; here
// a value is an object with named properties and the image is a Uint8Array, so
// the projection is a pass of constant-offset loads — the same straight line
// the flat packet reader is, minus the bit cursor — AND it is §4.6's bounds
// pass. The plan never clamps: identity is one OP_COPY of the whole body, a
// compiled plan copies a same-size ranged integer, and an ordinal op remaps by
// name. Hash chooses the plan. This decode is the one path both land in.
// A COUNTED ARRAY WALKS ITS LIVE COUNT, never the bound: slack is never
// projected and never clamped.
func (g *fixedGen) emitDecodeBody(st *ir.Struct) {
	g.pf("// %s's loads, out of the reader's own image and into the value the\n", st.Name)
	g.pf("// caller handed in — filled, never returned, exactly as Read<Name>Flat is.\n")
	// The body is emitted first so the signature can name `report` only where
	// the body reads it: a type with no text and no nested decode never
	// refuses, and an unread parameter is eslint no-unused-vars. A caller's
	// report argument is accepted either way.
	outer := g.body.String()
	g.body.Reset()
	// THE CONTENT RULES APPLY TO THE USED UNITS BEFORE ANY DESTINATION WRITE
	// (docs/FIXED-FORM-ALGORITHM.md §4.5 fix 11, §5.3): REFUSE is total, so a
	// text violation in the image, or a non-finite float (docs/SPEC-TABLES.md
	// §3.3), refuses by name before a single property of value is touched.
	g.emitCheckStruct(st, "at", 0, "  ")
	g.inl, g.loops = 0, 0
	var cur int64
	for _, f := range st.Fields {
		g.emitDecodeField(f, "at", cur, "value", "  ")
		cur += fixedFieldBytes(f)
	}
	inner := g.body.String()
	g.body.Reset()
	g.body.WriteString(outer)
	if jsIdentifiers(inner)["report"] {
		g.pf("export function %sFixedDecode(value, view, at, report) {\n", st.Name)
	} else {
		g.pf("export function %sFixedDecode(value, view, at) {\n", st.Name)
	}
	g.body.WriteString(inner)
	g.pf("}\n\n")
}

func fixedStructChecked(st *ir.Struct) bool {
	if st == nil {
		return false
	}
	return slices.ContainsFunc(st.Fields, fixedFieldChecked)
}

// fixedFieldChecked is true for a field the content pass reads before the
// projection: a text field (its used units, §4.5 fix 11) and a float (its raw
// bits, docs/SPEC-TABLES.md §3.3), here or anywhere below it.
func fixedFieldChecked(f *ir.Field) bool {
	if f == nil {
		return false
	}
	if f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString || fixedIsFloat(f.Type) {
		return true
	}
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return fixedStructChecked(r)
		case *ir.Union:
			for _, v := range r.Variants {
				if v.F != nil && fixedFieldChecked(v.F) {
					return true
				}
			}
		}
	}
	return false
}

// fixedIsFloat is true for a float32 or a float64 leaf.
func fixedIsFloat(t ir.FieldType) bool { return t.Kind == ir.TFloat32 || t.Kind == ir.TFloat64 }

// emitCheckStruct walks st's text and float fields in view before
// emitDecodeField writes anything into value (docs/FIXED-FORM-ALGORITHM.md
// §4.5 fix 11, §5.3): an ill-formed text refuses as text_ill_formed, and a NaN
// or an infinity on its raw bits as non_finite_float (docs/SPEC-TABLES.md
// §3.3). Both refusals are total: nothing of the record lands.
func (g *fixedGen) emitCheckStruct(st *ir.Struct, base string, at int64, ind string) {
	if !fixedStructChecked(st) {
		return
	}
	cur := at
	for _, f := range st.Fields {
		if fixedFieldChecked(f) {
			g.emitCheckField(f, base, cur, ind)
		}
		cur += fixedFieldBytes(f)
	}
}

func (g *fixedGen) emitCheckField(f *ir.Field, base string, at int64, ind string) {
	if f.Type.Optional {
		g.pf("%sif (view.getUint8(%s) !== 0) {\n", ind, off(base, at))
		g.emitCheckPayload(f, base, at+fixedPresentBytes, ind+"  ")
		g.pf("%s}\n", ind)
		return
	}
	g.emitCheckPayload(f, base, at, ind)
}

func (g *fixedGen) emitCheckPayload(f *ir.Field, base string, at int64, ind string) {
	switch {
	case f.KeyEnum != "":
		g.emitCheckLoop(f, base, at, strconv.FormatInt(f.KeyEnumRef.Max, 10), ind)
	case f.Array == ir.ArrayFixed:
		g.emitCheckLoop(f, base, at, strconv.FormatInt(f.ArrayBound, 10), ind)
	case f.Array == ir.ArrayCounted:
		g.pf("%s{\n", ind)
		g.pf("%s  let n = view.getInt32(%s, true);\n", ind, off(base, at))
		g.pf("%s  if (n < 0) { n = 0; } else if (n > %d) { n = %d; }\n", ind, f.ArrayBound, f.ArrayBound)
		g.emitCheckLoop(f, base, at+fixedCountBytes, "n", ind+"  ")
		g.pf("%s}\n", ind)
	case f.Type.Kind == ir.TString:
		g.pf("%s{\n", ind)
		g.pf("%s  let n = view.getInt32(%s, true);\n", ind, off(base, at))
		g.pf("%s  if (n < 0) { n = 0; } else if (n > %d) { n = %d; }\n", ind, f.Type.Size, f.Type.Size)
		g.pf("%s  if (!TableFixedTextOk(TableFixedTextUtf8, view, %s, n)) { report.refused = TableFixedRefusal.TextIllFormed; return; }\n",
			ind, off(base, at+fixedCountBytes))
		g.pf("%s}\n", ind)
	case f.Type.Kind == ir.TWString:
		g.pf("%s{\n", ind)
		g.pf("%s  let n = view.getInt32(%s, true);\n", ind, off(base, at))
		g.pf("%s  if (n < 0) { n = 0; } else if (n > %d) { n = %d; }\n", ind, f.Type.Size, f.Type.Size)
		g.pf("%s  if (!TableFixedTextOk(TableFixedTextWide, view, %s, n * 2)) { report.refused = TableFixedRefusal.TextIllFormed; return; }\n",
			ind, off(base, at+fixedCountBytes))
		g.pf("%s}\n", ind)
	default:
		g.emitCheckElement(f, base, at, ind)
	}
}

func (g *fixedGen) emitCheckLoop(f *ir.Field, base string, at int64, count, ind string) {
	elem := fixedElementBytes(f)
	g.pf("%sfor (let i = 0; i < %s; i++) {\n", ind, count)
	g.emitCheckElement(f, fmt.Sprintf("%s + i * %d", off(base, at), elem), 0, ind+"  ")
	g.pf("%s}\n", ind)
}

func (g *fixedGen) emitCheckElement(f *ir.Field, base string, at int64, ind string) {
	switch f.Type.Kind {
	case ir.TFloat32:
		g.pf("%sif ((view.getUint32(%s, true) & 0x7f800000) === 0x7f800000) { report.refused = TableFixedRefusal.NonFiniteFloat; return; }\n", ind, off(base, at))
		return
	case ir.TFloat64:
		// the exponent is the high word's bits 20..30
		g.pf("%sif ((view.getUint32(%s, true) & 0x7ff00000) === 0x7ff00000) { report.refused = TableFixedRefusal.NonFiniteFloat; return; }\n", ind, off(base, at+4))
		return
	}
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			g.emitCheckStruct(r, base, at, ind)
		case *ir.Union:
			tag := fixedUnionTagBytes(r)
			g.pf("%s{\n", ind)
			g.pf("%s  let tag = 0;\n", ind)
			g.loadTag(ind+"  ", "tag", off(base, at), tag)
			g.pf("%s  switch (tag) {\n", ind)
			for i, v := range r.Variants {
				if v.F != nil && fixedFieldChecked(v.F) {
					g.pf("%s    case %d: {\n", ind, i+1)
					g.emitCheckPayload(v.F, base, at+tag, ind+"      ")
					g.pf("%s      break;\n%s    }\n", ind, ind)
				}
			}
			g.pf("%s    default: break;\n%s  }\n", ind, ind)
			g.pf("%s}\n", ind)
		}
	}
}

// emitDecodeField projects one FIELD out of the image, through the optional
// wrapper where one rides.
//
// A PRESENT BYTE IS A FLAG AND NOT A NUMBER: anything non-zero is present.
// That is the same rule `bool` reads under two lines below — an endpoint's `true`
// is allowed to be any non-zero byte, and a reader that only accepted 1 would
// be reading a number where the wire states a flag.
//
// AN ABSENT FIELD'S PAYLOAD IS NEVER PROJECTED. The bytes behind a zero flag
// are whatever the writer's storage happened to hold; they are not the value,
// and a reader that landed them would be handing the consumer a value the
// writer said was not there. The field takes its DECLARED DEFAULT instead,
// which is what the prefill already says an absent field reads as.
func (g *fixedGen) emitDecodeField(f *ir.Field, base string, at int64, val, ind string) {
	if f.Type.Optional {
		name := js.MemberName(f.Name)
		g.pf("%s%s.%sPresent = view.getUint8(%s) !== 0;\n", ind, val, name, off(base, at))
		g.pf("%sif (%s.%sPresent) {\n", ind, val, name)
		g.emitDecodePayload(f, base, at+fixedPresentBytes, val, ind+"  ")
		g.pf("%s} else {\n", ind)
		g.emitPayloadReset(f, ind+"  ", val)
		g.pf("%s}\n", ind)
		return
	}
	g.emitDecodePayload(f, base, at, val, ind)
}

func (g *fixedGen) emitDecodePayload(f *ir.Field, base string, at int64, val, ind string) {
	name := js.MemberName(f.Name)
	switch {
	case f.KeyEnum != "":
		g.emitDecodeLoop(f, base, at, strconv.FormatInt(f.KeyEnumRef.Max, 10), val+"."+name, ind)
	case f.Array == ir.ArrayFixed:
		g.emitDecodeLoop(f, base, at, strconv.FormatInt(f.ArrayBound, 10), val+"."+name, ind)
	case f.Array == ir.ArrayCounted:
		g.emitDecodeCount(ind, val+"."+name+"Count", off(base, at), f.ArrayBound)
		// LIVE ELEMENTS ONLY: clamp and ordinal counters count the writer's
		// live count, never the slack behind it. Identity copies the whole
		// array into the image; this loop is the pass both paths share.
		g.emitDecodeLoop(f, base, at+fixedCountBytes, val+"."+name+"Count", val+"."+name, ind)
	case f.Type.Kind == ir.TString:
		// Content validity over the used units was checked before any write to
		// value in emitCheckStruct (§4.5 fix 11, §5.3).
		g.emitDecodeCount(ind, val+"."+name+"Length", off(base, at), f.Type.Size)
		iv := g.loopVar()
		g.pf("%sfor (let %s = 0; %s < %d; %s++) { %s.%s[%s] = view.getUint8(%s + %s); }\n",
			ind, iv, iv, f.Type.Size, iv, val, name, iv, off(base, at+fixedCountBytes), iv)
	case f.Type.Kind == ir.TBytes:
		g.emitDecodeCount(ind, val+"."+name+"Length", off(base, at), f.Type.Size)
		iv := g.loopVar()
		g.pf("%sfor (let %s = 0; %s < %d; %s++) { %s.%s[%s] = view.getUint8(%s + %s); }\n",
			ind, iv, iv, f.Type.Size, iv, val, name, iv, off(base, at+fixedCountBytes), iv)
	case f.Type.Kind == ir.TWString:
		// Content validity over the used units was checked before any write to
		// value in emitCheckStruct (§4.5 fix 11, §5.3).
		g.emitDecodeCount(ind, val+"."+name+"Length", off(base, at), f.Type.Size)
		iv := g.loopVar()
		g.pf("%sfor (let %s = 0; %s < %d; %s++) { %s.%s[%s] = view.getUint16(%s + %s * 2, true); }\n",
			ind, iv, iv, f.Type.Size, iv, val, name, iv, off(base, at+fixedCountBytes), iv)
	default:
		g.emitDecodeElement(f, base, at, val+"."+name, ind)
	}
}

// emitDecodeCount is §3.4's `count` op, inline: a count or a length is read out
// of the image and CLAMPED to THIS reader's own bound, counting one `clamped`
// if it fired.
//
// THE COMPILED PLAN'S OWN count op has already done this — OP_COUNT and
// OP_TEXT clamp on the way into the image — so on that path this is a
// no-op and no counter moves twice. It is here for the IDENTITY path, which
// is ONE OP_COPY of the whole body: a record under this build's own hash
// still arrives from a writer this reader cannot vouch for, and a count or a
// length it carries is a number the consumer would otherwise index the
// reader's own storage by. The bound is the reader's, never the record's.
// This is the twin of javatable/fixedform.go's emitScatterCount and of
// rusttable/fixedform.go's emitFixedScatterCount.
func (g *fixedGen) emitDecodeCount(ind, dst, at string, bound int64) {
	g.pf("%s{\n", ind)
	g.pf("%s  let n = view.getInt32(%s, true);\n", ind, at)
	g.pf("%s  if (n < 0) { n = 0; report.clamped++; } else if (n > %d) { n = %d; report.clamped++; }\n",
		ind, bound, bound)
	g.pf("%s  %s = n;\n", ind, dst)
	g.pf("%s}\n", ind)
}

func (g *fixedGen) emitDecodeLoop(f *ir.Field, base string, at int64, count, expr, ind string) {
	elem := fixedElementBytes(f)
	iv := g.loopVar()
	if _, ok := f.Type.Ref.(*ir.Struct); ok && f.Type.Kind == ir.TNamed {
		// AN ARRAY OF STRUCTS IS WALKED WITH ITS ARRAY AND ITS COUNT IN LOCALS
		// and a RUNNING offset, so the inlined element body reads the image at
		// o + k: no property load per element for the array or the bound, and
		// no multiply per field.
		g.pf("%s{\n", ind)
		g.pf("%s  const a%s = %s, n%s = %s;\n", ind, iv, expr, iv, count)
		g.pf("%s  for (let %s = 0, o%s = %s; %s < n%s; %s++, o%s += %d) {\n", ind, iv, iv, off(base, at), iv, iv, iv, iv, elem)
		g.loops++
		g.emitDecodeElement(f, "o"+iv, 0, "a"+iv+"["+iv+"]", ind+"    ")
		g.loops--
		g.pf("%s  }\n", ind)
		g.pf("%s}\n", ind)
		return
	}
	g.pf("%sfor (let %s = 0; %s < %s; %s++) {\n", ind, iv, iv, count, iv)
	g.loops++
	g.emitDecodeElement(f, fmt.Sprintf("%s + %s * %d", off(base, at), iv, elem), 0, expr+"["+iv+"]", ind+"  ")
	g.loops--
	g.pf("%s}\n", ind)
}

func (g *fixedGen) emitDecodeElement(f *ir.Field, base string, at int64, expr, ind string) {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			// A NESTED STRUCT IS INLINED, not called: the projection is ONE
			// straight function, the way the flat packet reader is written. The
			// content check already ran over this element's text at the top of
			// the outermost decode (emitCheckStruct recurses), so the inlined
			// fields are the loads alone and nothing is validated twice.
			// <T>FixedDecode is still emitted per struct for any other caller.
			g.inl++
			v := fmt.Sprintf("e%d", g.inl)
			g.pf("%s{\n", ind)
			g.pf("%s  const %s = %s;\n", ind, v, expr)
			cur := at
			for _, sf := range r.Fields {
				g.emitDecodeField(sf, base, cur, v, ind+"  ")
				cur += fixedFieldBytes(sf)
			}
			g.pf("%s}\n", ind)
			return
		case *ir.Union:
			tag := fixedUnionTagBytes(r)
			g.loadTag(ind, expr+".type", off(base, at), tag)
			// A TAG BEYOND THE ARM COUNT LANDS AS None (0) AND COUNTS ONE
			// clamped. It is the twin of the count and length clamps two
			// functions down: on the IDENTITY path the whole body is ONE
			// OP_COPY, so no per-field op runs and a forged tag would reach the
			// consumer as a number naming an arm that does not exist.
			//
			// §5.4 OWES THIS COUNT ON BOTH PLANS, and the two ordinals reach it
			// differently. AN ENUM ORDINAL does: the compiled plan's OP_ORDINAL
			// remaps it and counts the forgery itself (fixedruntime.go's ordinal
			// op), so `clamped` is 1 on the compiled plan and 1 on the identity
			// plan, never 2 — the op counts where this projection cannot see,
			// and this projection counts where no op ran.
			//
			// A UNION TAG ON THE COMPILED PLAN IS THIS LEG'S ONE OWED COUNT. The
			// arms are const entries under their own tag's guard, so a forged tag
			// matches no guard, leaves the prefill's None standing, and arrives
			// here already 0 with nothing to compare — the VALUE is right and the
			// counter is short. Paying it means emitting the tag as an OP_ORDINAL
			// over the writer's arm ordinals beside the guarded consts, which is
			// a plan-shape change and not a projection one; it is named here
			// rather than left silent (§5.8's habit, §5.4's rule).
			g.pf("%sif (%s.type > %d) { %s.type = 0; report.clamped++; }\n",
				ind, expr, len(r.Variants), expr)
			g.pf("%sswitch (%s.type) {\n", ind, expr)
			for i, v := range r.Variants {
				g.pf("%s  case %d: {\n", ind, i+1)
				if v.F != nil {
					// A DIRECT ARM IS A FIELD LINE (docs/SPEC-TABLES.md §2.6):
					// a text, bytes or array arm projects through the SAME
					// payload emitters a field of its type does, with the arm's
					// own name as the member and the union object as the owner.
					g.emitDecodePayload(v.F, base, at+tag, expr, ind+"    ")
				}
				g.pf("%s    break;\n%s  }\n", ind, ind)
			}
			g.pf("%s  default: break;\n%s}\n", ind, ind)
			return
		case *ir.Enum:
			g.loadTag(ind, expr, off(base, at), int64(r.StorageBits/8))
			// AN ORDINAL PAST THE ENUM'S TOP VALUE LANDS None (0) AND COUNTS
			// ONE clamped. Identity is one OP_COPY of the whole body, so the
			// plan never holds the set; this projection is the bounds pass
			// both paths share. A value inside an `| max = K` headroom is in
			// the set and stands (r.Max, not the named-variant count).
			if max, ok := fixedEnumClampMax(r); ok {
				g.pf("%sif (%s > %d) { %s = 0; report.clamped++; }\n",
					ind, expr, max, expr)
			}
			return
		}
	}
	if g.emitBigClampLanes(ind, expr, off(base, at), f) {
		return
	}
	g.load(ind, expr, "view", off(base, at), f.Type)
	g.emitDecodeBounds(ind, expr, f)
}

// emitBigClampLanes is §4.6's ranged clamp for a 64- or 128-bit integer,
// decided on the image's u32 LANES AS A NUMBER before any BigInt exists: a
// clamped value lands its bound's literal and an in-range one is loaded once,
// so no BigInt is made only to be compared and no BigInt comparison runs.
//
// THE DECISION IS EXACT. The Number is hi * 2^32 + lo (for 128 bits, the high
// 64-bit half), which rounds MONOTONICALLY, and it is only used when every
// bound it is compared against is an integer below 2^53 in magnitude: a value
// past such a bound rounds to a Number at or past it, never back across it,
// and a value at the bound is exact. A 128-bit bound must also be a whole
// multiple of 2^64 so the low half decides only the one tie it can. Anything
// else answers false and takes the BigInt path unchanged.
func (g *fixedGen) emitBigClampLanes(ind, expr, at string, f *ir.Field) bool {
	if !fixedIsBig(f.Type) {
		return false
	}
	if _, _, ok := fixedFloatClampEnds(f); ok {
		return false
	}
	w := fixedStorageBytes(f.Type)
	rlo, rhi, ok := ir.TableRawRange(f)
	var low, high bool
	switch {
	case f.Type.Kind == ir.TBits:
		// a bits(N) that does not fill its storage clamps at 2^N - 1 and only
		// there (emitDecodeBounds' last case); one with a declared range too is
		// left to the BigInt path
		if ok || int64(f.Type.Width) >= 8*w {
			return false
		}
		rlo = big.NewInt(0)
		rhi = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
		low, high = false, true
	case ok:
		low, high = fixedClampEnds(f)
	default:
		return false
	}
	if !low && !high {
		return false
	}
	safe := new(big.Int).Lsh(big.NewInt(1), 53)
	inSafe := func(n *big.Int) bool { return new(big.Int).Abs(n).Cmp(safe) < 0 }
	hiGet := "getUint32"
	if fixedSigned(f.Type) {
		hiGet = "getInt32"
	}
	var nExpr, loCond, hiCond string
	switch w {
	case 8:
		if (low && !inSafe(rlo)) || (high && !inSafe(rhi)) {
			return false
		}
		nExpr = fmt.Sprintf("view.%s(%s, true) * 4294967296 + view.getUint32(%s, true)", hiGet, off(at, 4), at)
		loCond = fmt.Sprintf("n < %s", rlo.String())
		hiCond = fmt.Sprintf("n > %s", rhi.String())
	case 16:
		two64 := new(big.Int).Lsh(big.NewInt(1), 64)
		ql, rl := new(big.Int), new(big.Int)
		qh, rh := new(big.Int), new(big.Int)
		ql.DivMod(rlo, two64, rl) // Euclidean: the remainder is in [0, 2^64)
		qh.DivMod(rhi, two64, rh)
		if (low && (rl.Sign() != 0 || !inSafe(ql))) || (high && (rh.Sign() != 0 || !inSafe(qh))) {
			return false
		}
		nExpr = fmt.Sprintf("view.%s(%s, true) * 4294967296 + view.getUint32(%s, true)", hiGet, off(at, 12), off(at, 8))
		// below L = qL * 2^64 exactly when the high half is below qL; above
		// H = qH * 2^64 when the high half is past qH, or equal with a low half
		// that is not zero
		loCond = fmt.Sprintf("n < %s", ql.String())
		hiCond = fmt.Sprintf("n > %s || (n === %s && (view.getUint32(%s, true) | view.getUint32(%s, true)) !== 0)",
			qh.String(), qh.String(), at, off(at, 4))
	default:
		return false
	}
	g.pf("%s{\n", ind)
	g.pf("%s  const n = %s;\n", ind, nExpr)
	first := true
	if low {
		g.pf("%s  if (%s) { %s = %s; report.clamped++; }\n", ind, loCond, expr, jsIntLit(rlo, true))
		first = false
	}
	if high {
		kw := "if"
		if !first {
			kw = "else if"
		}
		g.pf("%s  %s (%s) { %s = %s; report.clamped++; }\n", ind, kw, hiCond, expr, jsIntLit(rhi, true))
	}
	g.pf("%s  else {\n", ind)
	g.load(ind+"    ", expr, "view", at, f.Type)
	g.pf("%s  }\n", ind)
	g.pf("%s}\n", ind)
	return true
}

// emitDecodeBounds is §4.6's ranged-scalar clamp, inline: a declared min/max
// (and a bits(N) width that does not fill storage) is held AFTER the load, on
// the value the consumer will see. The plan never does this. Vacuous ends —
// a bound sitting on the storage width's own limit — are dropped, because no
// loaded value can fail them.
func (g *fixedGen) emitDecodeBounds(ind, expr string, f *ir.Field) {
	// A RANGED FLOAT CLAMPS ON LOAD AND COUNTS, exactly as the ranged integer
	// below does, and against THIS READER'S own min and max (schema#1164,
	// ruled statement B): in the fixed form a compressed float rides AS THE
	// FLOAT (SPEC §3.4), its bounds are DEFINITIONS in the digest, and the
	// compiled plan carries no range at all, so the pass can only be the
	// reader's. A NaN or an infinity never reaches it: the content pass refused
	// it on its raw bits first (docs/SPEC-TABLES.md §3.3). `<` and `>` are IEEE
	// comparisons on purpose, so a negative zero is not below a min of +0.
	if lo, hi, ok := fixedFloatClampEnds(f); ok {
		g.pf("%sif (%s < %s) { %s = %s; report.clamped++; } else if (%s > %s) { %s = %s; report.clamped++; }\n",
			ind, expr, lo, expr, lo, expr, hi, expr, hi)
		return
	}
	if rlo, rhi, ok := ir.TableRawRange(f); ok {
		asBig := fixedIsBig(f.Type)
		lo, hi := jsIntLit(rlo, asBig), jsIntLit(rhi, asBig)
		low, high := fixedClampEnds(f)
		switch {
		case low && high:
			g.pf("%sif (%s < %s) { %s = %s; report.clamped++; } else if (%s > %s) { %s = %s; report.clamped++; }\n",
				ind, expr, lo, expr, lo, expr, hi, expr, hi)
		case low:
			g.pf("%sif (%s < %s) { %s = %s; report.clamped++; }\n", ind, expr, lo, expr, lo)
		case high:
			g.pf("%sif (%s > %s) { %s = %s; report.clamped++; }\n", ind, expr, hi, expr, hi)
		}
	}
	w := fixedStorageBytes(f.Type)
	if f.Type.Kind == ir.TBits && int64(f.Type.Width) < 8*w {
		maxv := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
		lit := jsIntLit(maxv, fixedIsBig(f.Type))
		g.pf("%sif (%s > %s) { %s = %s; report.clamped++; }\n", ind, expr, lit, expr, lit)
	}
}

// fixedEnumClampMax is the enum's top wire value when a loaded ordinal can
// actually sit past it. loadTag reads 1, 2 or 4 unsigned bytes; a Max that
// fills that load has no out-of-range ordinal to close.
func fixedEnumClampMax(r *ir.Enum) (int64, bool) {
	if r == nil || r.StorageBits <= 0 {
		return 0, false
	}
	var loaded int64
	switch {
	case r.StorageBits <= 8:
		loaded = 255
	case r.StorageBits <= 16:
		loaded = 65535
	case r.StorageBits <= 32:
		loaded = 4294967295
	default:
		// AN EIGHT-BYTE ORDINAL ARRIVES THROUGH TWO u32 LANES (loadTag): a set
		// high lane lands as 2^32, which no declared Max reaches, so the clamp
		// is never vacuous at this width and is never elided.
		loaded = math.MaxInt64
	}
	if r.Max >= loaded {
		return 0, false
	}
	return r.Max, true
}

// fixedClampEnds answers which ends of a declared min/max a loaded value can
// actually fail. A bound on the storage width's own limit is a comparison no
// DataView load can lose.
func fixedClampEnds(f *ir.Field) (low, high bool) {
	rlo, rhi, ok := ir.TableRawRange(f)
	if !ok {
		return false, false
	}
	bits := uint(8 * fixedStorageBytes(f.Type))
	if bits == 0 {
		return false, false
	}
	one := big.NewInt(1)
	var lo, hi *big.Int
	if ir.TableKindSigned(ir.TableScalarKind(f)) {
		top := new(big.Int).Lsh(one, bits-1)
		lo, hi = new(big.Int).Neg(top), new(big.Int).Sub(top, one)
	} else {
		lo, hi = big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(one, bits), one)
	}
	return rlo.Cmp(lo) > 0, rhi.Cmp(hi) < 0
}

func jsIntLit(n *big.Int, asBig bool) string {
	if n == nil {
		return "0"
	}
	if asBig {
		return n.String() + "n"
	}
	return n.String()
}

// ---------------------------------------------------------------------------
// THE STORAGE CLASS a table's values live in
// ---------------------------------------------------------------------------
//
// internal/codegen/js emits a class for every `type` a unit declares and NONE
// for a `table` — a table is a table-wire concept and the packet backends'
// traversals never meet one (ir.File.Tables). The C++ reference's table header
// emits the `struct` for exactly this reason, and this is its twin: the class
// a table's values live in, storage PREALLOCATED IN THE CONSTRUCTOR and never
// replaced afterwards, every field assigned in declaration order for
// hidden-class stability, which is internal/codegen/js's own rule for its
// packet classes and the reason a read can fill a value rather than return one.

// fixedDeclaredUnion answers whether a unit declares this name as a UNION.
// A table-closure union (docs/SPEC-TABLES.md §2.6) assembles beside the decl
// stream in File.TableUnions, so both lists are searched.
func fixedDeclaredUnion(u *ir.Unit, name string) bool {
	for _, f := range u.Files {
		for _, d := range f.Decls {
			if r, ok := d.(*ir.Union); ok && r.Name == name {
				return true
			}
		}
		for _, r := range f.TableUnions {
			if r.Name == name {
				return true
			}
		}
	}
	return false
}

// fixedDeclaringFile answers the module basename that declares a non-table
// named type, so its class can be imported rather than emitted twice. A
// table-closure union lives in File.TableUnions, so it is searched too.
func fixedDeclaringFile(u *ir.Unit, name string) string {
	for _, f := range u.Files {
		for _, d := range f.Decls {
			switch r := d.(type) {
			case *ir.Struct:
				if r.Name == name {
					return f.Base
				}
			case *ir.Union:
				if r.Name == name {
					return f.Base
				}
			}
		}
		for _, r := range f.TableUnions {
			if r.Name == name {
				return f.Base
			}
		}
	}
	return ""
}

// fixedIsUnionType reports whether a named type is a UNION, which resets
// differently from a struct: internal/codegen/js emits no `Init<Union>` at all
// — a union's reset is its TAG going back to None, with the dormant arms
// keeping their storage (its emitInitComposite says so) — so a helper that
// reached for one imported a name nothing exports and the whole module failed
// to load at its import line.
func fixedIsUnionType(t ir.FieldType) bool {
	if t.Kind != ir.TNamed {
		return false
	}
	_, ok := t.Ref.(*ir.Union)
	return ok
}

// compositeReset is the ONE statement that restores a nested value in place: a
// union's tag back to None, a struct's through its own Init. It answers the
// statement rather than printing it so a loop body can stay on one line.
func (g *fixedGen) compositeReset(t ir.FieldType, path string) string {
	if fixedIsUnionType(t) {
		return path + ".type = 0; /* None; dormant arms keep their storage */"
	}
	g.require(fixedElementClass(t), t.Name)
	return fmt.Sprintf("Init%s(%s);", fixedElementClass(t), path)
}

// fixedClassName is the JavaScript storage a named type's field holds.
func fixedElementClass(t ir.FieldType) string {
	if t.Kind != ir.TNamed {
		return ""
	}
	switch r := t.Ref.(type) {
	case *ir.Struct:
		return r.Name
	case *ir.Union:
		return r.Name
	}
	return ""
}

// emitTableClass emits `class T` and `InitT( value )` for one table.
func (g *fixedGen) emitTableClass(st *ir.Struct) {
	g.pf("// %s's storage. Preallocated in the constructor and never replaced: a\n", st.Name)
	g.pf("// read FILLS one of these, exactly as Read<Name>Flat fills a packet class.\n")
	g.pf("export class %s {\n  constructor() {\n", st.Name)
	for _, f := range st.Fields {
		g.emitFieldStorage(f, "    ", "this")
	}
	g.pf("  }\n")
	for _, f := range st.Fields {
		if f.KeyEnum != "" {
			emitKeyedStatics(g.pf, st.Name, js.MemberName(f.Name), f)
		}
	}
	g.pf("}\n\n")
	g.pf("// Init%s restores fresh construction values in place, preserving storage.\n", st.Name)
	g.pf("export function Init%s(value) {\n", st.Name)
	for _, f := range st.Fields {
		g.emitFieldReset(f, "  ", "value")
	}
	g.pf("}\n\n")
}

// zeroFor is a leaf's construction value: a Number, a BigInt or a boolean, with
// a declared default folded in where one overrides SPEC §5's zero.
func fixedZeroFor(f *ir.Field) string {
	t := f.Type
	if t.Kind == ir.TBool {
		if f.HasDefault && f.DefBool {
			return "true"
		}
		return "false"
	}
	if t.Kind == ir.TNamed {
		if e, ok := t.Ref.(*ir.Enum); ok {
			if f.HasDefault && f.DefVariant != "" {
				return fmt.Sprintf("%d", fixedVariantOrdinal(e, f.DefVariant))
			}
			return "0"
		}
		if _, ok := t.Ref.(*ir.Flags); ok {
			if f.HasDefault && f.DefInt != nil {
				return f.DefInt.String() + "n"
			}
			return "0n"
		}
	}
	big := fixedIsBig(t)
	switch t.Kind {
	case ir.TFloat32, ir.TFloat64:
		if f.HasDefault {
			return fmt.Sprintf("%v", f.DefFloat)
		}
		return "0"
	}
	if f.HasDefault && f.DefInt != nil {
		if big {
			return f.DefInt.String() + "n"
		}
		return f.DefInt.String()
	}
	if big {
		return "0n"
	}
	return "0"
}

// elementZero is the construction value of ONE element of a field.
func fixedElementZero(f *ir.Field) string {
	if cls := fixedElementClass(f.Type); cls != "" {
		return "new " + cls + "()"
	}
	return fixedZeroFor(f)
}

func (g *fixedGen) emitFieldStorage(f *ir.Field, ind, target string) {
	name := js.MemberName(f.Name)
	g.require(fixedElementClass(f.Type), f.Type.Name)
	switch {
	case f.KeyEnum != "":
		g.pf("%s%s.%s = %s;\n", ind, target, name, fixedArrayLiteral(f, f.KeyEnumRef.Max))
	case f.Array == ir.ArrayFixed:
		g.pf("%s%s.%s = %s;\n", ind, target, name, fixedArrayLiteral(f, f.ArrayBound))
	case f.Array == ir.ArrayCounted:
		g.pf("%s%s.%s = %s;\n", ind, target, name, fixedArrayLiteral(f, f.ArrayBound))
		g.pf("%s%s.%sCount = %d;\n", ind, target, name, f.ArrayMin)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		g.pf("%s%s.%s = new Uint8Array(%d);\n", ind, target, name, f.Type.Size)
		g.emitDefaultTextBytes(f, ind, target)
		g.pf("%s%s.%sLength = %d;\n", ind, target, name, fixedDefaultTextLength(f))
	case f.Type.Kind == ir.TWString:
		g.pf("%s%s.%s = new Uint16Array(%d);\n", ind, target, name, f.Type.Size)
		g.pf("%s%s.%sLength = 0;\n", ind, target, name)
	default:
		if cls := fixedElementClass(f.Type); cls != "" {
			g.pf("%s%s.%s = new %s();\n", ind, target, name, cls)
			break
		}
		g.pf("%s%s.%s = %s;\n", ind, target, name, fixedZeroFor(f))
	}
	g.emitPresenceStorage(f, ind, target)
}

// emitPresenceStorage declares a `?T` field's PRESENCE COMPANION, the twin of
// the used length beside a `string(N)` two cases up and of the C++ reference's
// `<name>_present` bool (internal/codegen/cpptable/codecs.go). It lands here,
// on the table's own class, because `?T` is a TABLE construct and this file
// emits every table's class — no `type` can carry an optional, so no packet
// class ever declares one and neither packet tier ever reads one.
func (g *fixedGen) emitPresenceStorage(f *ir.Field, ind, target string) {
	if !f.Type.Optional {
		return
	}
	g.pf("%s%s.%sPresent = false; // ?: absent until set (docs/SPEC-TABLES.md §3.4)\n",
		ind, target, js.MemberName(f.Name))
}

// emitDefaultTextBytes lays a `string(N)` or `bytes(N)` field's DECLARED
// DEFAULT into its storage (SPEC §4.2). Setting only the used LENGTH from the
// default and leaving the buffer zero was a value that claimed N used bytes of
// an empty buffer: the reference's own constructed record reads "fx" where this
// one read two NULs, and since the write side takes the value's own storage,
// the two ports then disagreed on the BYTES for the same logical value.
//
// The prefill lays the same default down on the read side
// (fixedprefill.go), so a constructed value and a record whose writer did not
// carry the field now hold the same thing — which is what a default means.
//
// A wstring carries none: the IR holds a wstring default as BYTES and this form
// counts CODE UNITS, so laying one down would be this backend guessing an
// encoding. fixedDefaultTextLength answers 0 there for the same reason.
func (g *fixedGen) emitDefaultTextBytes(f *ir.Field, ind, target string) {
	if f.Type.Kind == ir.TWString {
		return
	}
	n := fixedDefaultTextLength(f)
	if n <= 0 {
		return
	}
	parts := make([]string, 0, n)
	for _, b := range f.DefBytes[:n] {
		parts = append(parts, strconv.FormatUint(uint64(b), 10))
	}
	g.pf("%s%s.%s.set([%s]); // the declared default (SPEC §4.2)\n",
		ind, target, js.MemberName(f.Name), strings.Join(parts, ", "))
}

func fixedDefaultTextLength(f *ir.Field) int64 {
	if f.HasDefault && len(f.DefBytes) > 0 {
		n := int64(len(f.DefBytes))
		if n > f.Type.Size {
			return f.Type.Size
		}
		return n
	}
	return 0
}

func fixedArrayLiteral(f *ir.Field, count int64) string {
	if cls := fixedElementClass(f.Type); cls != "" {
		return fmt.Sprintf("Array.from({ length: %d }, () => new %s())", count, cls)
	}
	if fixedStorageBytes(f.Type) == 1 && f.Type.Kind == ir.TInt && !f.Type.Signed {
		return fmt.Sprintf("new Uint8Array(%d)", count)
	}
	return fmt.Sprintf("new Array(%d).fill(%s)", count, fixedElementZero(f))
}

// emitFieldReset restores one field in place, never replacing its storage.
func (g *fixedGen) emitFieldReset(f *ir.Field, ind, target string) {
	g.emitPayloadReset(f, ind, target)
	g.emitPresenceStorage(f, ind, target)
}

// emitPayloadReset restores a field's PAYLOAD in place, never replacing its
// storage and never touching its presence. It is both halves of a reset — the
// class's own Init calls it with the presence reset beside it, and the DECODE
// calls it alone, for the payload behind a flag that says absent.
func (g *fixedGen) emitPayloadReset(f *ir.Field, ind, target string) {
	name := js.MemberName(f.Name)
	switch {
	case f.KeyEnum != "", f.Array != ir.ArrayNone:
		count := f.ArrayBound
		if f.KeyEnum != "" {
			count = f.KeyEnumRef.Max
		}
		if cls := fixedElementClass(f.Type); cls != "" {
			g.pf("%sfor (let i = 0; i < %d; i++) { %s }\n",
				ind, count, g.compositeReset(f.Type, fmt.Sprintf("%s.%s[i]", target, name)))
		} else {
			g.pf("%s%s.%s.fill(%s);\n", ind, target, name, fixedElementZero(f))
		}
		if f.Array == ir.ArrayCounted {
			g.pf("%s%s.%sCount = %d;\n", ind, target, name, f.ArrayMin)
		}
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TWString:
		g.pf("%s%s.%s.fill(0);\n", ind, target, name)
		g.emitDefaultTextBytes(f, ind, target)
		g.pf("%s%s.%sLength = %d;\n", ind, target, name, fixedDefaultTextLength(f))
	default:
		if cls := fixedElementClass(f.Type); cls != "" {
			g.pf("%s%s\n", ind, g.compositeReset(f.Type, target+"."+name))
			return
		}
		g.pf("%s%s.%s = %s;\n", ind, target, name, fixedZeroFor(f))
	}
}

// fixedFloatClampEnds answers a ranged float's bounds as JavaScript number
// literals. A float32 field's ends are narrowed to float32 first, so the
// comparison is against exactly the value the reader's own declaration names
// and not a double that is a hair wider. Which bound is the reader's own and
// not the writer's is schema#1164, statement B.
func fixedFloatClampEnds(f *ir.Field) (lo, hi string, ok bool) {
	if f == nil || !f.HasFloatRange {
		return "", "", false
	}
	l, h := f.FMin, f.FMax
	switch f.Type.Kind {
	case ir.TFloat32:
		l, h = float64(float32(l)), float64(float32(h))
	case ir.TFloat64:
	default:
		return "", "", false
	}
	return jsFloatLit(l), jsFloatLit(h), true
}

// jsFloatLit renders a float64 as a literal JavaScript accepts. Go's %v spells
// an infinity `+Inf`, which is a syntax error there.
func jsFloatLit(v float64) string {
	switch {
	case math.IsNaN(v):
		return "NaN"
	case math.IsInf(v, 1):
		return "Infinity"
	case math.IsInf(v, -1):
		return "-Infinity"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}
