// THE FIXED FORM'S EMISSION for Dart (docs/SPEC-TABLES.md §3.4): the storage a
// table's values live in, the write template's straight line of stores, and
// the straight-line projection between the canonical body image and the
// language's own objects.
//
// THE SHAPE IS THE PACKET CODEC'S. Glenn's rule for every leg of this form is
// that the port looks at the equivalent PACKET codec and takes its shape,
// because the fixed form is far closer to that than to the id-table wire — and
// in Dart that is internal/codegen/dart: `final class` storage with every
// field initialized at its declaration, buffers and nested instances
// ALLOCATED AT CONSTRUCTION AND NEVER REPLACED, a caller-owned `ByteData` with
// `Endian.little` passed explicitly at every call, constant offsets and
// constant widths inlined at each field, a `zero`/`init` pair that restores in
// place, a read that FILLS the value it was handed, and a verdict rather than
// an exception. `string(N)` is a `Uint8List(N)` with an `int` used length
// beside it, `wstring(N)` a `Uint16List(N)` with its length in CODE UNITS, an
// enum an `int` off the packet emitter's constant namespace, a union an `int
// type` beside its arms, and a 128-bit value the packet emitter's own
// `Int128`/`UInt128` pair, `hi` and `lo`.
//
// WHAT IT DOES NOT TAKE from the packet codec is the bit packer: there is no
// bit cursor on this form, so the staging word and the window loads are gone
// and what is left is the store itself, at a constant byte offset.
package darttable

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---------------------------------------------------------------------------
// THE VALUE DOMAIN
// ---------------------------------------------------------------------------

// fixedSigned reports whether a leaf's storage image is two's complement.
func fixedSigned(t ir.FieldType) bool {
	switch t.Kind {
	case ir.TInt, ir.TFixed:
		return t.Signed
	}
	return false
}

// fixedIs128 reports a leaf carried by the packet emitter's Int128/UInt128
// pair: sixteen bytes, the LOW 64-bit half first, which is this wire's order
// for the family everywhere else (docs/SPEC-TABLES.md §3).
func fixedIs128(t ir.FieldType) bool { return fixedStorageBytes(t) == 16 }

// fixedScalarType is the Dart storage a leaf holds, in the packet emitter's
// own value domain.
func fixedScalarType(t ir.FieldType) string {
	switch t.Kind {
	case ir.TBool:
		return "bool"
	case ir.TFloat32, ir.TFloat64:
		return "double"
	case ir.TNamed:
		switch t.Ref.(type) {
		case *ir.Enum, *ir.Flags:
			return "int" // an integer-backed namespace, as the packet classes hold it
		}
	}
	if fixedIs128(t) {
		if fixedSigned(t) {
			return "Int128"
		}
		return "UInt128"
	}
	return "int"
}

// fixedElementClass is the Dart class a named type's field holds.
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

// fixedTypedList is the typed list a scalar element rides in, which is the
// packet emitter's own choice at every width. `zero` is the element's
// construction value: a List<T> takes it in the constructor, and a typed list
// is born all-zero and takes a fillRange behind it only when the DECLARED
// DEFAULT is not the zero — which the caller decides, because a list that
// silently dropped a declared default would disagree with the prefill.
func fixedTypedList(t ir.FieldType, count int64, zero string) string {
	switch t.Kind {
	case ir.TBool:
		return fmt.Sprintf("List<bool>.filled(%d, %s)", count, zero)
	case ir.TFloat32:
		return fmt.Sprintf("Float32List(%d)", count)
	case ir.TFloat64:
		return fmt.Sprintf("Float64List(%d)", count)
	}
	if fixedIs128(t) {
		if fixedSigned(t) {
			return fmt.Sprintf("List<Int128>.filled(%d, %s)", count, zero)
		}
		return fmt.Sprintf("List<UInt128>.filled(%d, %s)", count, zero)
	}
	bits := fixedStorageBytes(t) * 8
	if fixedSigned(t) {
		return fmt.Sprintf("Int%dList(%d)", bits, count)
	}
	return fmt.Sprintf("Uint%dList(%d)", bits, count)
}

// fixedElementAllZero reports an element whose construction image is all
// zeros, which is what a typed list is born as. It is decided by laying the
// element's PREFILL down and looking, so the storage and the prefill can never
// disagree about a declared default.
func fixedElementAllZero(f *ir.Field) bool {
	out := make([]byte, fixedElementBytes(f))
	fixedPrefillElement(out, 0, f)
	for _, b := range out {
		if b != 0 {
			return false
		}
	}
	return true
}

// fixedIsByteList reports a scalar element list whose backing store is a
// Uint8List, which is the one case a whole run moves with setRange.
func fixedIsByteList(t ir.FieldType) bool {
	return t.Kind == ir.TInt && !t.Signed && fixedStorageBytes(t) == 1
}

// ---------------------------------------------------------------------------
// THE GENERATOR
// ---------------------------------------------------------------------------

type fixedGen struct {
	unit *ir.Unit
	body strings.Builder
	need func(cls string, extra ...string) // an import for another emitter's class
}

func (g *fixedGen) pf(format string, args ...any) { fmt.Fprintf(&g.body, format, args...) }

// fn writes a function signature the way the Dart formatter would: one line
// when it fits the eighty-column bound, and the tall form with a trailing
// comma when it does not. The formatter is this leg's gate, so it measures.
func (g *fixedGen) fn(ret, name string, params []string) {
	one := fmt.Sprintf("%s %s(%s) {", ret, name, strings.Join(params, ", "))
	if len(one) <= 80 {
		g.pf("%s\n", one)
		return
	}
	g.pf("%s %s(\n", ret, name)
	for _, p := range params {
		g.pf("  %s,\n", p)
	}
	g.pf(") {\n")
}

// call writes one call statement the way the Dart formatter would: one line
// when it fits the eighty-column bound, and the tall form with a trailing
// comma when it does not. Every emitted statement of this file goes through
// it, so a long field name wraps the way `dart format` wraps it rather than
// drifting off the gate.
func (g *fixedGen) call(ind, lead, fn string, args []string, tail string) {
	one := ind + lead + fn + "(" + strings.Join(args, ", ") + ")" + tail
	if len(one) <= 80 {
		g.pf("%s\n", one)
		return
	}
	g.pf("%s%s%s(\n", ind, lead, fn)
	for _, a := range args {
		g.pf("%s  %s,\n", ind, a)
	}
	g.pf("%s)%s\n", ind, tail)
}

// off renders a constant byte offset expression: a literal when the base is
// the record's own start, and base + literal inside a nested walk.
func fixedOff(base string, at int64) string {
	if base == "" {
		return fmt.Sprintf("%d", at)
	}
	if at == 0 {
		return base
	}
	return fmt.Sprintf("%s + %d", base, at)
}

// fixedSetter and fixedGetter are the ByteData calls for one leaf, at its
// declared storage width, with the little-endian flag passed EXPLICITLY at
// every call — the packet codec's rule and the block form's rule alike.
func fixedSetter(t ir.FieldType) (call string, endian bool) {
	switch t.Kind {
	case ir.TBool:
		return "setUint8", false
	case ir.TFloat32:
		return "setFloat32", true
	case ir.TFloat64:
		return "setFloat64", true
	}
	if _, ok := t.Ref.(*ir.Flags); ok && t.Kind == ir.TNamed {
		return "setUint64", true
	}
	bits := fixedStorageBytes(t) * 8
	if fixedSigned(t) {
		if bits == 8 {
			return "setInt8", false
		}
		return fmt.Sprintf("setInt%d", bits), true
	}
	if bits == 8 {
		return "setUint8", false
	}
	return fmt.Sprintf("setUint%d", bits), true
}

func fixedGetter(t ir.FieldType) (string, bool) {
	call, endian := fixedSetter(t)
	return "g" + strings.TrimPrefix(call, "s"), endian
}

// store emits one ByteData call at a constant offset.
func (g *fixedGen) store(ind, at string, t ir.FieldType, expr string) {
	if fixedIs128(t) {
		// SIXTEEN BYTES, THE LOW 64-BIT HALF FIRST (docs/SPEC-TABLES.md §3).
		// Dart holds a 128-bit value as the packet emitter's pair, so the two
		// halves are stored as they are held.
		g.call(ind, "", "view.setUint64", []string{at, expr + ".lo", "Endian.little"}, ";")
		g.call(ind, "", "view.setUint64", []string{fixedOff(at, 8), expr + ".hi", "Endian.little"}, ";")
		return
	}
	name, endian := fixedSetter(t)
	if t.Kind == ir.TBool {
		g.call(ind, "", "view."+name, []string{at, expr + " ? 1 : 0"}, ";")
		return
	}
	if endian {
		g.call(ind, "", "view."+name, []string{at, expr, "Endian.little"}, ";")
		return
	}
	g.call(ind, "", "view."+name, []string{at, expr}, ";")
}

func (g *fixedGen) load(ind, lhs, at string, t ir.FieldType) {
	if fixedIs128(t) {
		cls := "UInt128"
		if fixedSigned(t) {
			cls = "Int128"
		}
		g.need(cls)
		g.pf("%s%s = %s(\n", ind, lhs, cls)
		g.pf("%s  view.getUint64(%s, Endian.little),\n", ind, fixedOff(at, 8))
		g.pf("%s  view.getUint64(%s, Endian.little),\n", ind, at)
		g.pf("%s);\n", ind)
		return
	}
	name, endian := fixedGetter(t)
	if t.Kind == ir.TBool {
		// A BOOL LANDS AS `byte != 0` (docs/SPEC-TABLES.md §3.4): a peer that
		// wrote 2 wrote true, and this reader says so rather than refusing.
		g.call(ind, lhs+" = ", "view.getUint8", []string{at}, " != 0;")
		return
	}
	if endian {
		g.call(ind, lhs+" = ", "view."+name, []string{at, "Endian.little"}, ";")
		return
	}
	g.call(ind, lhs+" = ", "view."+name, []string{at}, ";")
}

// storeTag stores an ordinal at the width the layout fixed, which is one, two
// or four bytes and always unsigned.
func (g *fixedGen) storeTag(ind, at string, width int64, expr string) {
	if width == 1 {
		g.call(ind, "", "view.setUint8", []string{at, expr}, ";")
		return
	}
	g.call(ind, "", fmt.Sprintf("view.setUint%d", width*8), []string{at, expr, "Endian.little"}, ";")
}

func (g *fixedGen) loadTag(ind, lhs, at string, width int64) {
	if width == 1 {
		g.call(ind, lhs+" = ", "view.getUint8", []string{at}, ";")
		return
	}
	g.call(ind, lhs+" = ", fmt.Sprintf("view.getUint%d", width*8), []string{at, "Endian.little"}, ";")
}

// ---------------------------------------------------------------------------
// THE STORAGE a table's values live in
// ---------------------------------------------------------------------------
//
// internal/codegen/dart emits a class for every `type` a unit declares and
// NONE for a `table` — a table is a table-wire concept and the packet
// backends' traversals never meet one. The C++ reference's table header emits
// the `struct` for exactly this reason, and this is its twin.
//
// AND IT IS WHERE THE PRESENT FLAG LIVES. §3.4 departs from §2.3 by one byte:
// `?T` and a plain `T` are wire-identical on form 1 and ONE BYTE APART here,
// so an optional field needs a presence member. A TABLE's class is this
// emitter's own, so the flag lands in a member this file writes.

func (g *fixedGen) emitStorageClass(st *ir.Struct) {
	g.pf("/// %s's storage. Allocated at construction and never replaced: a read\n", st.Name)
	g.pf("/// FILLS one of these, exactly as the packet reader fills a packet class.\n")
	g.pf("final class %s {\n", st.Name)
	for i, f := range st.Fields {
		if i > 0 && f.Type.Optional {
			// the formatter wants a blank line in front of a doc comment
			g.pf("\n")
		}
		g.emitFieldStorage(f, "  ")
	}
	g.pf("}\n\n")
	g.pf("/// init%s restores construction values in place, preserving storage.\n", capitalize(st.Name))
	g.pf("void init%s(%s value) {\n", capitalize(st.Name), st.Name)
	for _, f := range st.Fields {
		g.emitFieldReset(f, "  ", "value")
	}
	g.pf("}\n\n")
}

// fixedZeroFor is a leaf's construction value, with a declared default folded
// in where one overrides SPEC §5's zero.
func (g *fixedGen) fixedZeroFor(f *ir.Field) string {
	t := f.Type
	if t.Kind == ir.TBool {
		if f.HasDefault && f.DefBool {
			return "true"
		}
		return "false"
	}
	if t.Kind == ir.TNamed {
		if e, ok := t.Ref.(*ir.Enum); ok {
			g.need(t.Name)
			if f.HasDefault && f.DefVariant != "" {
				return t.Name + "." + dartName(e.VariantWireNameOf(f.DefVariant))
			}
			return t.Name + ".none"
		}
		if _, ok := t.Ref.(*ir.Flags); ok {
			if f.HasDefault && f.DefInt != nil {
				return fixedIntLit(f.DefInt, 8)
			}
			return "0"
		}
	}
	switch t.Kind {
	case ir.TFloat32, ir.TFloat64:
		if f.HasDefault {
			return fixedFloatLit(f.DefFloat)
		}
		return "0.0"
	}
	if fixedIs128(t) {
		cls := "UInt128"
		if fixedSigned(t) {
			cls = "Int128"
		}
		g.need(cls)
		if f.HasDefault && f.DefInt != nil {
			lo, hi := fixed128Halves(f.DefInt)
			return fmt.Sprintf("%s(%s, %s)", cls, fixedHexLit(hi), fixedHexLit(lo))
		}
		return cls + ".zero"
	}
	if f.HasDefault && f.DefInt != nil {
		return fixedIntLit(f.DefInt, fixedStorageBytes(t))
	}
	return "0"
}

// fixedElementZero is the construction value of ONE element of a field.
func (g *fixedGen) fixedElementZero(f *ir.Field) string {
	if cls := fixedElementClass(f.Type); cls != "" {
		g.need(cls)
		return cls + "()"
	}
	return g.fixedZeroFor(f)
}

func (g *fixedGen) emitFieldStorage(f *ir.Field, ind string) {
	name := dartName(f.Name)
	if f.Type.Optional {
		// the PRESENT FLAG, §3.4's one departure from §2.3
		g.pf("%s/// the present flag: the payload rides WHOLE either way (§3.4)\n", ind)
		g.pf("%sbool %sPresent = false;\n", ind, name)
	}
	switch {
	case f.KeyEnum != "":
		g.emitListStorage(f, ind, name, f.KeyEnumRef.Max)
	case f.Array == ir.ArrayFixed:
		g.emitListStorage(f, ind, name, f.ArrayBound)
	case f.Array == ir.ArrayCounted:
		g.emitListStorage(f, ind, name, f.ArrayBound)
		g.pf("%sint %sCount = %d;\n", ind, name, f.ArrayMin)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		g.pf("%sfinal Uint8List %s = Uint8List(%d);\n", ind, name, f.Type.Size)
		g.pf("%sint %sLength = %d;\n", ind, name, fixedDefaultTextLength(f))
	case f.Type.Kind == ir.TWString:
		g.pf("%sfinal Uint16List %s = Uint16List(%d);\n", ind, name, f.Type.Size)
		g.pf("%sint %sLength = 0;\n", ind, name)
	default:
		if cls := fixedElementClass(f.Type); cls != "" {
			g.need(cls)
			g.pf("%sfinal %s %s = %s();\n", ind, cls, name, cls)
			return
		}
		g.pf("%s%s %s = %s;\n", ind, fixedScalarType(f.Type), name, g.fixedZeroFor(f))
	}
}

func (g *fixedGen) emitListStorage(f *ir.Field, ind, name string, count int64) {
	if cls := fixedElementClass(f.Type); cls != "" {
		g.need(cls)
		g.pf("%sfinal List<%s> %s = List.generate(%d, (_) => %s());\n", ind, cls, name, count, cls)
		return
	}
	zero := g.fixedElementZero(f)
	lit := fixedTypedList(f.Type, count, zero)
	if !strings.HasPrefix(lit, "List<") && !fixedElementAllZero(f) {
		// A TYPED LIST IS BORN ALL-ZERO, so a declared default that is not the
		// zero has to be laid in — the same default the prefill lays into every
		// slot of the image, so the two agree by construction.
		lit += fmt.Sprintf("\n    ..fillRange(0, %d, %s)", count, zero)
	}
	g.pf("%sfinal %s %s = %s;\n", ind, fixedListType(f.Type, count), name, lit)
}

// fixedListType is the declared type of a scalar element list.
func fixedListType(t ir.FieldType, count int64) string {
	lit := fixedTypedList(t, count, "0")
	if i := strings.IndexByte(lit, '('); i >= 0 && !strings.HasPrefix(lit, "List<") {
		return lit[:i]
	}
	switch {
	case t.Kind == ir.TBool:
		return "List<bool>"
	case fixedIs128(t) && fixedSigned(t):
		return "List<Int128>"
	case fixedIs128(t):
		return "List<UInt128>"
	}
	return "List<int>"
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

// emitFieldReset restores one field in place, never replacing its storage.
func (g *fixedGen) emitFieldReset(f *ir.Field, ind, target string) {
	name := dartName(f.Name)
	if f.Type.Optional {
		g.pf("%s%s.%sPresent = false;\n", ind, target, name)
	}
	switch {
	case f.KeyEnum != "", f.Array != ir.ArrayNone:
		count := f.ArrayBound
		if f.KeyEnum != "" {
			count = f.KeyEnumRef.Max
		}
		if cls := fixedElementClass(f.Type); cls != "" {
			restore := g.restoreFn(f.Type, cls)
			g.pf("%sfor (var i = 0; i < %d; i++) {\n", ind, count)
			g.pf("%s  %s(%s.%s[i]);\n", ind, restore, target, name)
			g.pf("%s}\n", ind)
		} else {
			g.call(ind, "", target+"."+name+".fillRange",
				[]string{"0", fmt.Sprintf("%d", count), g.fixedElementZero(f)}, ";")
		}
		if f.Array == ir.ArrayCounted {
			g.pf("%s%s.%sCount = %d;\n", ind, target, name, f.ArrayMin)
		}
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TWString:
		g.call(ind, "", target+"."+name+".fillRange",
			[]string{"0", fmt.Sprintf("%d", f.Type.Size), "0"}, ";")
		g.pf("%s%s.%sLength = %d;\n", ind, target, name, fixedDefaultTextLength(f))
		g.emitTextDefaultBytes(f, ind, target)
	default:
		if cls := fixedElementClass(f.Type); cls != "" {
			g.pf("%s%s(%s.%s);\n", ind, g.restoreFn(f.Type, cls), target, name)
			return
		}
		g.pf("%s%s.%s = %s;\n", ind, target, name, g.fixedZeroFor(f))
	}
}

// restoreFn names the in-place restore of a nested value, and it is NOT one
// name: this emitter writes `init<Table>` for a table of its own, the packet
// emitter writes `init<Type>` for a `type`, and a UNION gets `zero<Union>`
// there and nothing else — construction of a union IS the empty union, so the
// zero form is the restore (SPEC §4.8). Naming the wrong one is a link error,
// which is why the choice is made here and once.
func (g *fixedGen) restoreFn(t ir.FieldType, cls string) string {
	if _, isUnion := t.Ref.(*ir.Union); isUnion && t.Kind == ir.TNamed {
		g.need(cls, "zero"+capitalize(cls))
		return "zero" + capitalize(cls)
	}
	g.need(cls, "init"+capitalize(cls))
	return "init" + capitalize(cls)
}

// emitTextDefaultBytes lays a declared string/bytes default back in.
func (g *fixedGen) emitTextDefaultBytes(f *ir.Field, ind, target string) {
	n := fixedDefaultTextLength(f)
	if n == 0 {
		return
	}
	name := dartName(f.Name)
	for i := int64(0); i < n; i++ {
		g.pf("%s%s.%s[%d] = 0x%02x;\n", ind, target, name, i, f.DefBytes[i])
	}
}

// ---------------------------------------------------------------------------
// THE WRITE: a template, then stores at constant offsets
// ---------------------------------------------------------------------------

// emitWriteBody emits <T>FixedWriteBody: the straight line of stores for one
// type. The template — the hash, then zeros — is laid down by the caller,
// which is also what zero-fills every byte of declared slack without this
// function touching it.
//
// A BOUND IS WRITTEN WHOLE. A string's N bytes and an array's MAX elements
// ride however much of them is used, which is what makes a read-then-save
// reproduce a record BYTE FOR BYTE: the read landed the peer's slack in this
// storage, so writing the storage back writes the peer's slack back.
//
// AN ABSENT OPTIONAL'S PAYLOAD IS THE ONE EXCEPTION, and it is §3.4's own:
// under a present flag of `0` the payload is SLACK, and slack is ZERO ON
// WRITE. So the payload's stores stand under the flag and the template's
// zeros ride when it is clear. Byte identity is unmoved over CONFORMING
// input — a conforming peer wrote zeros there, and this reader read them —
// and over input that put meaning under a clear flag it was never owed.
func (g *fixedGen) emitWriteBody(st *ir.Struct) {
	g.pf("/// %s's stores. Every offset is a constant of the type and nothing is\n", st.Name)
	g.pf("/// measured: on this form MeasureBody is a constant, not a walk.\n")
	g.fn("void", lowerFirst(st.Name)+"FixedWriteBody",
		[]string{"Uint8List bytes", "ByteData view", "int at", st.Name + " value"})
	var cur int64
	for _, f := range st.Fields {
		g.emitWriteField(f, "at", cur, "value", "  ")
		cur += fixedFieldBytes(f)
	}
	g.pf("}\n\n")
}

func (g *fixedGen) emitWriteField(f *ir.Field, base string, at int64, val, ind string) {
	name := dartName(f.Name)
	if f.Type.Optional {
		g.call(ind, "", "view.setUint8",
			[]string{fixedOff(base, at), val + "." + name + "Present ? 1 : 0"}, ";")
		at += fixedPresentBytes
		// AN ABSENT OPTIONAL'S PAYLOAD IS SLACK, AND SLACK IS ZERO ON WRITE
		// (docs/SPEC-TABLES.md §3.4): the payload rides WHOLE whether or not
		// it is present, and when the flag is 0 what rides is zeros. The
		// template already laid them down, so the write is the one this
		// branch does NOT do — it costs the writer nothing, which is the
		// spec's own reason for the rule.
		g.pf("%sif (%s.%sPresent) {\n", ind, val, name)
		g.emitWritePayload(f, base, at, val, name, ind+"  ")
		g.pf("%s}\n", ind)
		return
	}
	g.emitWritePayload(f, base, at, val, name, ind)
}

// emitWritePayload is the field's own stores, past any present flag.
func (g *fixedGen) emitWritePayload(f *ir.Field, base string, at int64, val, name, ind string) {
	switch {
	case f.KeyEnum != "":
		g.emitWriteLoop(f, base, at, f.KeyEnumRef.Max, val+"."+name, ind)
	case f.Array == ir.ArrayFixed:
		g.emitWriteLoop(f, base, at, f.ArrayBound, val+"."+name, ind)
	case f.Array == ir.ArrayCounted:
		// the count, then MAX elements, the slack behind it as the value holds it
		g.pf("%sassert(%s.%sCount >= 0);\n", ind, val, name)
		g.pf("%sassert(%s.%sCount <= %d);\n", ind, val, name, f.ArrayBound)
		g.call(ind, "", "view.setInt32",
			[]string{fixedOff(base, at), val + "." + name + "Count", "Endian.little"}, ";")
		g.emitWriteLoop(f, base, at+fixedCountBytes, f.ArrayBound, val+"."+name, ind)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		g.pf("%sassert(%s.%sLength >= 0);\n", ind, val, name)
		g.pf("%sassert(%s.%sLength <= %d);\n", ind, val, name, f.Type.Size)
		g.call(ind, "", "view.setInt32",
			[]string{fixedOff(base, at), val + "." + name + "Length", "Endian.little"}, ";")
		g.call(ind, "", "bytes.setRange", []string{
			fixedOff(base, at+fixedCountBytes),
			fixedOff(base, at+fixedCountBytes+f.Type.Size),
			val + "." + name,
		}, ";")
	case f.Type.Kind == ir.TWString:
		// the length in CODE UNITS, then 2N bytes
		g.pf("%sassert(%s.%sLength >= 0);\n", ind, val, name)
		g.pf("%sassert(%s.%sLength <= %d);\n", ind, val, name, f.Type.Size)
		g.call(ind, "", "view.setInt32",
			[]string{fixedOff(base, at), val + "." + name + "Length", "Endian.little"}, ";")
		g.pf("%sfor (var i = 0; i < %d; i++) {\n", ind, f.Type.Size)
		g.call(ind+"  ", "", "view.setUint16",
			[]string{fixedOff(base, at+fixedCountBytes) + " + i * 2", val + "." + name + "[i]", "Endian.little"}, ";")
		g.pf("%s}\n", ind)
	default:
		g.emitWriteElement(f, base, at, val+"."+name, ind)
	}
}

func (g *fixedGen) emitWriteLoop(f *ir.Field, base string, at, count int64, expr, ind string) {
	elem := fixedElementBytes(f)
	if fixedElementClass(f.Type) == "" && fixedIsByteList(f.Type) {
		g.call(ind, "", "bytes.setRange",
			[]string{fixedOff(base, at), fixedOff(base, at+count), expr}, ";")
		return
	}
	g.pf("%sfor (var i = 0; i < %d; i++) {\n", ind, count)
	g.emitWriteElement(f, fmt.Sprintf("%s + i * %d", fixedOff(base, at), elem), 0, expr+"[i]", ind+"  ")
	g.pf("%s}\n", ind)
}

func (g *fixedGen) emitWriteElement(f *ir.Field, base string, at int64, expr, ind string) {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			g.call(ind, "", lowerFirst(r.Name)+"FixedWriteBody",
				[]string{"bytes", "view", fixedOff(base, at), expr}, ";")
			return
		case *ir.Union:
			tag := fixedUnionTagBytes(r)
			g.storeTag(ind, fixedOff(base, at), tag, expr+".type")
			g.pf("%sswitch (%s.type) {\n", ind, expr)
			for i, v := range r.Variants {
				g.pf("%s  case %d:\n", ind, i+1)
				g.emitWriteElement(v.F, fixedOff(base, at+tag), 0, expr+"."+dartName(v.Name), ind+"    ")
				g.pf("%s    break;\n", ind)
			}
			g.pf("%s  default:\n%s    break;\n%s}\n", ind, ind, ind)
			return
		case *ir.Enum:
			// the ORDINAL is the variant's POSITION IN THE LAYOUT, from 1, and
			// 0 is None — which is the value the declaration already carries
			g.storeTag(ind, fixedOff(base, at), int64(r.StorageBits/8), expr)
			return
		}
	}
	g.store(ind, fixedOff(base, at), f.Type, expr)
}

// ---------------------------------------------------------------------------
// THE DECODE: the canonical image into the language's own objects
// ---------------------------------------------------------------------------
//
// THIS IS THE STEP C++ GETS FOR FREE. There a struct IS its bytes, so the plan
// lands values in the value itself and there is nothing after the loop; here a
// value is an object with named fields and the image is a Uint8List, so the
// projection is a pass of constant-offset loads — the same straight line the
// packet reader is, minus the bit cursor. Every count and every length it
// reads was already clamped to this reader's own bound by the plan, so nothing
// it indexes with can be out of range and nothing here throws.

func (g *fixedGen) emitDecodeBody(st *ir.Struct) {
	g.pf("/// %s's loads, out of the reader's own image and into the value the\n", st.Name)
	g.pf("/// caller handed in — filled, never returned, as the packet reader is.\n")
	g.pf("///\n")
	g.pf("/// THE DECLARED BOUNDS ARE CHECKED HERE and nowhere else: a range clamps\n")
	g.pf("/// and counts (docs/SPEC-TABLES.md §4), a union tag past the last arm and\n")
	g.pf("/// an enum ordinal past the last variant land None and count. They are\n")
	g.pf("/// straight line rather than plan entries, so the identity path and a\n")
	g.pf("/// plan compiled from a stranger's layout hold the SAME bounds.\n")
	g.fn("void", lowerFirst(st.Name)+"FixedDecode",
		[]string{st.Name + " value", "Uint8List image", "ByteData view", "int at",
			"TableFixedReport report"})
	var cur int64
	for _, f := range st.Fields {
		g.emitDecodeField(f, "at", cur, "value", "  ")
		cur += fixedFieldBytes(f)
	}
	g.pf("}\n\n")
}

func (g *fixedGen) emitDecodeField(f *ir.Field, base string, at int64, val, ind string) {
	name := dartName(f.Name)
	if f.Type.Optional {
		g.call(ind, val+"."+name+"Present = ", "view.getUint8", []string{fixedOff(base, at)}, " != 0;")
		at += fixedPresentBytes
	}
	switch {
	case f.KeyEnum != "":
		g.emitDecodeLoop(f, base, at, f.KeyEnumRef.Max, val+"."+name, ind)
	case f.Array == ir.ArrayFixed:
		g.emitDecodeLoop(f, base, at, f.ArrayBound, val+"."+name, ind)
	case f.Array == ir.ArrayCounted:
		g.call(ind, val+"."+name+"Count = ", "view.getInt32",
			[]string{fixedOff(base, at), "Endian.little"}, ";")
		g.emitDecodeLoop(f, base, at+fixedCountBytes, f.ArrayBound, val+"."+name, ind)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		g.call(ind, val+"."+name+"Length = ", "view.getInt32",
			[]string{fixedOff(base, at), "Endian.little"}, ";")
		g.call(ind, "", val+"."+name+".setRange",
			[]string{"0", fmt.Sprintf("%d", f.Type.Size), "image", fixedOff(base, at+fixedCountBytes)}, ";")
	case f.Type.Kind == ir.TWString:
		g.call(ind, val+"."+name+"Length = ", "view.getInt32",
			[]string{fixedOff(base, at), "Endian.little"}, ";")
		g.pf("%sfor (var i = 0; i < %d; i++) {\n", ind, f.Type.Size)
		g.call(ind+"  ", val+"."+name+"[i] = ", "view.getUint16",
			[]string{fixedOff(base, at+fixedCountBytes) + " + i * 2", "Endian.little"}, ";")
		g.pf("%s}\n", ind)
	default:
		g.emitDecodeElement(f, base, at, val+"."+name, ind)
	}
}

func (g *fixedGen) emitDecodeLoop(f *ir.Field, base string, at, count int64, expr, ind string) {
	elem := fixedElementBytes(f)
	// The run move is only available to an element the decode does nothing
	// else to: a BOUNDED byte whose range clamps takes the loop instead.
	if fixedElementClass(f.Type) == "" && fixedIsByteList(f.Type) && !fixedHasClamp(f) {
		g.call(ind, "", expr+".setRange",
			[]string{"0", fmt.Sprintf("%d", count), "image", fixedOff(base, at)}, ";")
		return
	}
	g.pf("%sfor (var i = 0; i < %d; i++) {\n", ind, count)
	g.emitDecodeElement(f, fmt.Sprintf("%s + i * %d", fixedOff(base, at), elem), 0, expr+"[i]", ind+"  ")
	g.pf("%s}\n", ind)
}

func (g *fixedGen) emitDecodeElement(f *ir.Field, base string, at int64, expr, ind string) {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			g.call(ind, "", lowerFirst(r.Name)+"FixedDecode",
				[]string{expr, "image", "view", fixedOff(base, at), "report"}, ";")
			return
		case *ir.Union:
			tag := fixedUnionTagBytes(r)
			g.loadTag(ind, expr+".type", fixedOff(base, at), tag)
			g.pf("%sswitch (%s.type) {\n", ind, expr)
			g.pf("%s  case 0:\n%s    break; // None, which is a tag this reader holds\n", ind, ind)
			for i, v := range r.Variants {
				g.pf("%s  case %d:\n", ind, i+1)
				g.emitDecodeElement(v.F, fixedOff(base, at+tag), 0, expr+"."+dartName(v.Name), ind+"    ")
				g.pf("%s    break;\n", ind)
			}
			// A TAG PAST THE LAST ARM IS None (§3.4): the arms run from 1 and
			// tag 0 is None, so a tag beyond them names storage NO DECLARATION
			// DESCRIBES. It lands None and counts one clamp — the same landing
			// the compiled plan gives it through its own remap, so the identity
			// path and a stranger's plan agree about a hostile tag.
			g.pf("%s  default:\n", ind)
			g.pf("%s    %s.type = 0;\n", ind, expr)
			g.pf("%s    report.clamped++;\n", ind)
			g.pf("%s    break;\n%s}\n", ind, ind)
			return
		case *ir.Enum:
			g.loadTag(ind, expr, fixedOff(base, at), int64(r.StorageBits/8))
			// AN ORDINAL PAST THE LAST VARIANT IS None (§3.4): the ordinal is
			// the variant's POSITION IN THE LAYOUT, from 1, and the layout
			// lists this reader's variants and no more. Past them it lands 0
			// and counts one clamp, which is where the plan's own remap lands
			// it too.
			if n := len(r.Variants); n > 0 {
				g.pf("%sif (%s > %d) {\n", ind, expr, n)
				g.pf("%s  %s = 0;\n", ind, expr)
				g.pf("%s  report.clamped++;\n", ind)
				g.pf("%s}\n", ind)
			}
			return
		}
	}
	g.load(ind, expr, fixedOff(base, at), f.Type)
	g.emitClamp(f, expr, ind)
}

// ---------------------------------------------------------------------------
// THE DECLARED RANGE, CLAMPED ON LOAD AND COUNTED (docs/SPEC-TABLES.md §4)
// ---------------------------------------------------------------------------
//
// IT IS STRAIGHT LINE IN THE DECODE AND NOT A PLAN ENTRY, which is the whole
// of the design here. The plan moves BYTES between two layouts; a declared
// range is a fact about THIS READER's field and about no layout at all, so a
// plan entry for it would have to be compiled twice — once into the identity
// plan and once into every stranger's — and the two could drift. One clamp,
// after the copy, in the projection every path runs, holds on both.
//
// A FIXED-POINT VALUE RIDES RAW AT ITS STORAGE WIDTH on this form: the storage
// is the scaled integer and NOTHING HERE DIVIDES BY 2^F. So the bound compared
// against is the declared whole-unit bound SHIFTED BY F onto that same raw
// scale, which is what ir.TableRawRange answers.
//
// AN END THAT CANNOT FIRE IS NOT WRITTEN. A bound sitting ON the storage
// type's own limit describes a value the load cannot hold, so the comparison
// has no false case; the C++ table emitter elides it for the same reason and
// under the same test.

// fixedStorageRange is a leaf's own storage limits, on the raw scale.
func fixedStorageRange(t ir.FieldType) (lo, hi *big.Int) {
	bits := uint(fixedStorageBytes(t) * 8)
	if fixedSigned(t) {
		half := new(big.Int).Lsh(big.NewInt(1), bits-1)
		return new(big.Int).Neg(half), new(big.Int).Sub(half, big.NewInt(1))
	}
	return big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), bits), big.NewInt(1))
}

// fixedClampEnds answers the declared range on the raw scale and which of its
// two ends the decode writes a comparison for.
func fixedClampEnds(f *ir.Field) (lo, hi *big.Int, low, high bool) {
	rlo, rhi, ok := ir.TableRawRange(f)
	if !ok {
		return nil, nil, false, false
	}
	slo, shi := fixedStorageRange(f.Type)
	return rlo, rhi, rlo.Cmp(slo) > 0, rhi.Cmp(shi) < 0
}

// fixedBitsClamp answers a `bits(N)` width clamp: the older half of the same
// rule, written only where N is narrower than the storage the form gives it.
func fixedBitsClamp(t ir.FieldType) (*big.Int, bool) {
	if t.Kind != ir.TBits || int64(t.Width) >= fixedStorageBytes(t)*8 {
		return nil, false
	}
	one := big.NewInt(1)
	return new(big.Int).Sub(new(big.Int).Lsh(one, uint(t.Width)), one), true
}

// fixedHasClamp reports a leaf whose decode writes any comparison at all.
func fixedHasClamp(f *ir.Field) bool {
	switch f.Type.Kind {
	case ir.TInt, ir.TFixed, ir.TBits:
	default:
		return false
	}
	if _, _, low, high := fixedClampEnds(f); low || high {
		return true
	}
	_, ok := fixedBitsClamp(f.Type)
	return ok
}

// fixedClampTerms renders one end of a range as the Dart a comparison and an
// assignment take at this leaf's storage width.
func (g *fixedGen) fixedClampTerms(t ir.FieldType, expr string, v *big.Int) (lhs, cmp, assign string) {
	if fixedIs128(t) {
		cls := "UInt128"
		if fixedSigned(t) {
			cls = "Int128"
		}
		g.need(cls)
		lo, hi := fixed128Halves(v)
		lit := fmt.Sprintf("%s(%s, %s)", cls, fixedHexLit(hi), fixedHexLit(lo))
		return expr, lit, lit
	}
	if !fixedSigned(t) && fixedStorageBytes(t) == 8 {
		// A uint64 RIDES BIT-TRANSPARENTLY in a Dart int, which is signed, so
		// a value past 2^63 reads back negative and the signed comparison
		// would order it below zero. Flipping BOTH sign bits orders the
		// unsigned domain, which is the packet emitter's own _unsignedLessThan
		// spelled inline — one constant, folded, and no import.
		flip := new(big.Int).Xor(new(big.Int).Set(v), new(big.Int).Lsh(big.NewInt(1), 63))
		return "(" + expr + " ^ 0x8000000000000000)", fixedHexLit(flip), fixedHexLit(v)
	}
	return expr, v.String(), v.String()
}

// emitClamp writes the comparisons one leaf's declaration earns, after the
// load that put the raw value in place.
func (g *fixedGen) emitClamp(f *ir.Field, expr, ind string) {
	switch f.Type.Kind {
	case ir.TInt, ir.TFixed, ir.TBits:
	default:
		return
	}
	rlo, rhi, low, high := fixedClampEnds(f)
	lead := "if"
	if low {
		lhs, cmp, assign := g.fixedClampTerms(f.Type, expr, rlo)
		g.pf("%s%s (%s < %s) {\n", ind, lead, lhs, cmp)
		g.pf("%s  %s = %s;\n", ind, expr, assign)
		g.pf("%s  report.clamped++;\n", ind)
		g.pf("%s}", ind)
		lead = " else if"
	}
	if high {
		lhs, cmp, assign := g.fixedClampTerms(f.Type, expr, rhi)
		if lead == "if" {
			g.pf("%s", ind)
		}
		g.pf("%s (%s > %s) {\n", lead, lhs, cmp)
		g.pf("%s  %s = %s;\n", ind, expr, assign)
		g.pf("%s  report.clamped++;\n", ind)
		g.pf("%s}", ind)
	}
	if low || high {
		g.pf("\n")
	}
	if maxv, ok := fixedBitsClamp(f.Type); ok {
		lhs, cmp, assign := g.fixedClampTerms(f.Type, expr, maxv)
		g.pf("%sif (%s > %s) {\n", ind, lhs, cmp)
		g.pf("%s  // the bits(%d) WIDTH clamp\n", ind, f.Type.Width)
		g.pf("%s  %s = %s;\n", ind, expr, assign)
		g.pf("%s  report.clamped++;\n", ind)
		g.pf("%s}\n", ind)
	}
}

// ---------------------------------------------------------------------------
// LITERALS
// ---------------------------------------------------------------------------

// fixedIntLit renders an integer at a declared storage width the way the
// packet emitter does: decimal inside the signed 64-bit range and the HEX BIT
// PATTERN beyond it, because a Dart int is bit-transparent and a decimal past
// the range is not a literal Dart accepts.
func fixedIntLit(v *big.Int, width int64) string {
	n := new(big.Int).Set(v)
	if n.Sign() < 0 && width > 0 && width < 8 {
		return n.String()
	}
	if n.IsInt64() {
		return n.String()
	}
	return fixedHexLit(n)
}

// fixedHexLit renders the low sixty-four bits of a value as a hex literal,
// which Dart reads as the bit pattern.
func fixedHexLit(v *big.Int) string {
	n := new(big.Int).Set(v)
	if n.Sign() < 0 {
		n.Add(n, new(big.Int).Lsh(big.NewInt(1), 64))
	}
	n.And(n, new(big.Int).SetUint64(^uint64(0)))
	if n.IsInt64() {
		return n.String()
	}
	return fmt.Sprintf("0x%016x", n)
}

// fixed128Halves splits a 128-bit value into the low and high 64-bit halves.
func fixed128Halves(v *big.Int) (lo, hi *big.Int) {
	n := new(big.Int).Set(v)
	if n.Sign() < 0 {
		n.Add(n, new(big.Int).Lsh(big.NewInt(1), 128))
	}
	mask := new(big.Int).SetUint64(^uint64(0))
	lo = new(big.Int).And(n, mask)
	hi = new(big.Int).And(new(big.Int).Rsh(n, 64), mask)
	return lo, hi
}

// fixedFloatLit renders a double the way Dart wants one: a shortest round-trip
// form that always carries a decimal point.
func fixedFloatLit(v float64) string {
	s := strings.TrimSpace(fmt.Sprintf("%v", v))
	if !strings.ContainsAny(s, ".eE") && !strings.Contains(s, "Inf") && !strings.Contains(s, "NaN") {
		s += ".0"
	}
	switch {
	case strings.Contains(s, "+Inf"):
		return "double.infinity"
	case strings.Contains(s, "-Inf"):
		return "double.negativeInfinity"
	case strings.Contains(s, "NaN"):
		return "double.nan"
	}
	return s
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
