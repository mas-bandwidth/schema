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
	"strings"

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
	if !fixedSupported(st, 0) {
		return "its closure carries a construct the fixed class refuses — a pointer, a map, an unbounded array or a guarded branch (docs/SPEC-TABLES.md §3.4)"
	}
	if f := fixedFirstOptional(st, 0); f != "" {
		// §3.4's ONE departure from §2.3: on this form `?T` and a plain `T`
		// nesting are ONE BYTE APART, because the present flag rides. The
		// JavaScript packet classes have no presence member to land that byte
		// in — on the packet wire the two spellings are WIRE-IDENTICAL, so
		// internal/codegen/js emits no such member — and inventing one here
		// would be this backend inventing storage for a class another emitter
		// owns. Refused by name until the packet classes carry presence.
		return "field " + f + " is OPTIONAL, and the JavaScript packet classes carry no presence member for its present flag (docs/SPEC-TABLES.md §3.4, §2.3)"
	}
	return ""
}

// fixedFirstOptional names the first optional field anywhere in a closure.
func fixedFirstOptional(st *ir.Struct, depth int) string {
	if depth > 16 {
		return ""
	}
	for _, f := range st.Fields {
		if f.Type.Optional {
			return st.Name + "." + f.Name
		}
		if f.Type.Kind != ir.TNamed {
			continue
		}
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			if n := fixedFirstOptional(r, depth+1); n != "" {
				return n
			}
		case *ir.Union:
			for _, v := range r.Variants {
				if v.F == nil {
					continue
				}
				if s, ok := v.F.Type.Ref.(*ir.Struct); ok && v.F.Type.Kind == ir.TNamed {
					if n := fixedFirstOptional(s, depth+1); n != "" {
						return n
					}
				}
			}
		}
	}
	return ""
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
		g.pf("%s%s = %s.getBigUint64(%s, true) | (%s.getBigUint64(%s, true) << 64n);\n",
			ind, lhs, view, at, view, off(at, 8))
		if fixedSigned(t) {
			g.pf("%s%s = BigInt.asIntN(128, %s);\n", ind, lhs, lhs)
		}
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

func (g *fixedGen) emitWriteField(f *ir.Field, base string, at int64, val, ind string) {
	name := ir.GoExportName(f.Name)
	switch {
	case f.KeyEnum != "":
		g.emitWriteLoop(f, base, at, f.KeyEnumRef.Max, val+"."+name, ind)
	case f.Array == ir.ArrayFixed:
		g.emitWriteLoop(f, base, at, f.ArrayBound, val+"."+name, ind)
	case f.Array == ir.ArrayCounted:
		// the count, then MAX elements, the slack zero-filled
		g.pf("%sview.setInt32(%s, %s.%sCount, true);\n", ind, off(base, at), val, name)
		g.emitWriteLoop(f, base, at+fixedCountBytes, f.ArrayBound, val+"."+name, ind)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		g.pf("%sview.setInt32(%s, %s.%sLength, true);\n", ind, off(base, at), val, name)
		g.pf("%sfor (let i = 0; i < %d; i++) { view.setUint8(%s + i, %s.%s[i]); }\n",
			ind, f.Type.Size, off(base, at+fixedCountBytes), val, name)
	case f.Type.Kind == ir.TWString:
		// the length in CODE UNITS, then 2N bytes
		g.pf("%sview.setInt32(%s, %s.%sLength, true);\n", ind, off(base, at), val, name)
		g.pf("%sfor (let i = 0; i < %d; i++) { view.setUint16(%s + i * 2, %s.%s[i], true); }\n",
			ind, f.Type.Size, off(base, at+fixedCountBytes), val, name)
	default:
		g.emitWriteElement(f, base, at, val+"."+name, ind)
	}
}

func (g *fixedGen) emitWriteLoop(f *ir.Field, base string, at, count int64, expr, ind string) {
	elem := fixedElementBytes(f)
	g.pf("%sfor (let i = 0; i < %d; i++) {\n", ind, count)
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
			g.storeTag(ind, off(base, at), tag, expr+".Type")
			g.pf("%sswitch (%s.Type) {\n", ind, expr)
			for i, v := range r.Variants {
				g.pf("%s  case %d: {\n", ind, i+1)
				g.emitWriteElement(v.F, off(base, at+tag), 0, expr+"."+ir.GoExportName(v.Name), ind+"    ")
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

// storeTag stores an ordinal at a width the block fixed, which is always one,
// two or four bytes and always unsigned.
func (g *fixedGen) storeTag(ind, at string, width int64, expr string) {
	switch width {
	case 1:
		g.pf("%sview.setUint8(%s, %s);\n", ind, at, expr)
	case 2:
		g.pf("%sview.setUint16(%s, %s, true);\n", ind, at, expr)
	default:
		g.pf("%sview.setUint32(%s, %s, true);\n", ind, at, expr)
	}
}

func (g *fixedGen) loadTag(ind, lhs, at string, width int64) {
	switch width {
	case 1:
		g.pf("%s%s = view.getUint8(%s);\n", ind, lhs, at)
	case 2:
		g.pf("%s%s = view.getUint16(%s, true);\n", ind, lhs, at)
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
// lands values in the value itself and there is nothing after the loop; here a
// value is an object with named properties and the image is a Uint8Array, so
// the projection is a pass of constant-offset loads — the same straight line
// the flat packet reader is, minus the bit cursor.
func (g *fixedGen) emitDecodeBody(st *ir.Struct) {
	g.pf("// %s's loads, out of the reader's own image and into the value the\n", st.Name)
	g.pf("// caller handed in — filled, never returned, exactly as Read<Name>Flat is.\n")
	g.pf("export function %sFixedDecode(value, view, at) {\n", st.Name)
	var cur int64
	for _, f := range st.Fields {
		g.emitDecodeField(f, "at", cur, "value", "  ")
		cur += fixedFieldBytes(f)
	}
	g.pf("}\n\n")
}

func (g *fixedGen) emitDecodeField(f *ir.Field, base string, at int64, val, ind string) {
	name := ir.GoExportName(f.Name)
	switch {
	case f.KeyEnum != "":
		g.emitDecodeLoop(f, base, at, f.KeyEnumRef.Max, val+"."+name, ind)
	case f.Array == ir.ArrayFixed:
		g.emitDecodeLoop(f, base, at, f.ArrayBound, val+"."+name, ind)
	case f.Array == ir.ArrayCounted:
		g.pf("%s%s.%sCount = view.getInt32(%s, true);\n", ind, val, name, off(base, at))
		g.emitDecodeLoop(f, base, at+fixedCountBytes, f.ArrayBound, val+"."+name, ind)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		g.pf("%s%s.%sLength = view.getInt32(%s, true);\n", ind, val, name, off(base, at))
		g.pf("%sfor (let i = 0; i < %d; i++) { %s.%s[i] = view.getUint8(%s + i); }\n",
			ind, f.Type.Size, val, name, off(base, at+fixedCountBytes))
	case f.Type.Kind == ir.TWString:
		g.pf("%s%s.%sLength = view.getInt32(%s, true);\n", ind, val, name, off(base, at))
		g.pf("%sfor (let i = 0; i < %d; i++) { %s.%s[i] = view.getUint16(%s + i * 2, true); }\n",
			ind, f.Type.Size, val, name, off(base, at+fixedCountBytes))
	default:
		g.emitDecodeElement(f, base, at, val+"."+name, ind)
	}
}

func (g *fixedGen) emitDecodeLoop(f *ir.Field, base string, at, count int64, expr, ind string) {
	elem := fixedElementBytes(f)
	g.pf("%sfor (let i = 0; i < %d; i++) {\n", ind, count)
	g.emitDecodeElement(f, fmt.Sprintf("%s + i * %d", off(base, at), elem), 0, expr+"[i]", ind+"  ")
	g.pf("%s}\n", ind)
}

func (g *fixedGen) emitDecodeElement(f *ir.Field, base string, at int64, expr, ind string) {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			g.pf("%s%sFixedDecode(%s, view, %s);\n", ind, r.Name, expr, off(base, at))
			return
		case *ir.Union:
			tag := fixedUnionTagBytes(r)
			g.loadTag(ind, expr+".Type", off(base, at), tag)
			g.pf("%sswitch (%s.Type) {\n", ind, expr)
			for i, v := range r.Variants {
				g.pf("%s  case %d: {\n", ind, i+1)
				g.emitDecodeElement(v.F, off(base, at+tag), 0, expr+"."+ir.GoExportName(v.Name), ind+"    ")
				g.pf("%s    break;\n%s  }\n", ind, ind)
			}
			g.pf("%s  default: break;\n%s}\n", ind, ind)
			return
		case *ir.Enum:
			g.loadTag(ind, expr, off(base, at), int64(r.StorageBits/8))
			return
		}
	}
	g.load(ind, expr, "view", off(base, at), f.Type)
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

// fixedDeclaringFile answers the module basename that declares a non-table
// named type, so its class can be imported rather than emitted twice.
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
	}
	return ""
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
func (g *fixedGen) emitTableClass(st *ir.Struct, need func(string, string)) {
	g.pf("// %s's storage. Preallocated in the constructor and never replaced: a\n", st.Name)
	g.pf("// read FILLS one of these, exactly as Read<Name>Flat fills a packet class.\n")
	g.pf("export class %s {\n  constructor() {\n", st.Name)
	for _, f := range st.Fields {
		g.emitFieldStorage(f, "    ", "this", need)
	}
	g.pf("  }\n}\n\n")
	g.pf("// Init%s restores fresh construction values in place, preserving storage.\n", st.Name)
	g.pf("export function Init%s(value) {\n", st.Name)
	for _, f := range st.Fields {
		g.emitFieldReset(f, "  ", "value", need)
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

func (g *fixedGen) emitFieldStorage(f *ir.Field, ind, target string, need func(string, string)) {
	name := ir.GoExportName(f.Name)
	if cls := fixedElementClass(f.Type); cls != "" {
		need(cls, f.Type.Name)
	}
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
		g.pf("%s%s.%sLength = %d;\n", ind, target, name, fixedDefaultTextLength(f))
	case f.Type.Kind == ir.TWString:
		g.pf("%s%s.%s = new Uint16Array(%d);\n", ind, target, name, f.Type.Size)
		g.pf("%s%s.%sLength = 0;\n", ind, target, name)
	default:
		if cls := fixedElementClass(f.Type); cls != "" {
			g.pf("%s%s.%s = new %s();\n", ind, target, name, cls)
			return
		}
		g.pf("%s%s.%s = %s;\n", ind, target, name, fixedZeroFor(f))
	}
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
func (g *fixedGen) emitFieldReset(f *ir.Field, ind, target string, need func(string, string)) {
	name := ir.GoExportName(f.Name)
	switch {
	case f.KeyEnum != "", f.Array != ir.ArrayNone:
		count := f.ArrayBound
		if f.KeyEnum != "" {
			count = f.KeyEnumRef.Max
		}
		if cls := fixedElementClass(f.Type); cls != "" {
			need(cls, f.Type.Name)
			g.pf("%sfor (let i = 0; i < %d; i++) { Init%s(%s.%s[i]); }\n", ind, count, cls, target, name)
		} else {
			g.pf("%s%s.%s.fill(%s);\n", ind, target, name, fixedElementZero(f))
		}
		if f.Array == ir.ArrayCounted {
			g.pf("%s%s.%sCount = %d;\n", ind, target, name, f.ArrayMin)
		}
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TWString:
		g.pf("%s%s.%s.fill(0);\n", ind, target, name)
		g.pf("%s%s.%sLength = %d;\n", ind, target, name, fixedDefaultTextLength(f))
	default:
		if cls := fixedElementClass(f.Type); cls != "" {
			need(cls, f.Type.Name)
			g.pf("%sInit%s(%s.%s);\n", ind, cls, target, name)
			return
		}
		g.pf("%s%s.%s = %s;\n", ind, target, name, fixedZeroFor(f))
	}
}
