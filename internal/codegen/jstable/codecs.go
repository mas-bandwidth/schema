// TABLE-wire storage, codec and descriptor emission for JavaScript
// (docs/SPEC-TABLES.md), mirroring internal/codegen/cpptable — the reference —
// and internal/codegen/cstable, the worked second port. Readers restore
// declared defaults then overlay, skip unknown ids, skip kind mismatches,
// clamp out-of-range values, and count every event.
package jstable

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- literals ----

// formatFloat64 renders a float64 literal at the storage type's own precision.
func formatFloat64(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// formatFloat32 renders a float32 default as the EXACT double a float32 holds.
//
// This is where JavaScript's one numeric type shows. C# spells `0.2f` and its
// storage IS a float32, so a comparison against the default and a read of the
// wire meet at the same value. JavaScript has only doubles, so the literal a
// port writes has to be the double that float32 widens to — `0.2` would be a
// different number from the one a read of `0.2f`'s four bytes produces, and
// the elision comparison would then never hold. The elision comparison itself
// rounds the storage through Math.fround for the same reason, so a caller that
// assigns a plain double into a float32 field elides exactly where a C# caller
// assigning the same literal would.
func formatFloat32(v float64) string {
	return formatFloat64(float64(float32(v)))
}

// jsIntLit renders an integer literal in the domain a field's storage lives
// in: a Number below 2^53 at widths of 32 bits or fewer, a BigInt literal at
// 64 (the packet emitter's value-domain seam, SPEC §6.1).
func jsIntLit(v *big.Int, widthBytes int) string {
	if widthBytes >= 8 {
		return v.String() + "n"
	}
	return v.String()
}

// tableStorageRange is the inclusive range an integer storage of the given
// width can hold.
func tableStorageRange(signed bool, bits int) (*big.Int, *big.Int) {
	one := big.NewInt(1)
	if signed {
		hi := new(big.Int).Lsh(one, uint(bits-1))
		return new(big.Int).Neg(hi), new(big.Int).Sub(hi, one)
	}
	return big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(one, uint(bits)), one)
}

// tableClampEnds answers which ends of a declared min/max range a read can
// actually clamp at. The decode local is the wire kind's own width, so a bound
// sitting ON that width's limit is a comparison no decoded value can satisfy
// and the emitter drops it — the same "this check cannot fire" test the
// bits(N) width clamp already applies when N is the storage width
// (docs/SPEC-TABLES.md §4, issue #342). The emitted shape mirrors C++'s,
// because one table codec in three languages is the point.
func tableClampEnds(f *ir.Field, widthBytes int) (low, high bool) {
	signed := f.Type.Kind == ir.TInt && f.Type.Signed
	lo, hi := tableStorageRange(signed, widthBytes*8)
	return f.IntMin.Cmp(lo) > 0, f.IntMax.Cmp(hi) < 0
}

// fieldDefaultExpr renders the JavaScript expression a field's default
// compares against on the write side (elision) — identical values to the
// storage initializers, so measure, save and the reader's prefill agree.
func (g *tableGen) fieldDefaultExpr(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		if f.HasDefault && f.DefBool {
			return "true"
		}
		return "false"
	case ir.TFloat32:
		if f.HasDefault {
			return formatFloat32(f.DefFloat)
		}
		return "0.0"
	case ir.TFloat64:
		if f.HasDefault {
			return formatFloat64(f.DefFloat)
		}
		return "0.0"
	case ir.TInt, ir.TBits:
		width := 4
		if f.Type.Width > 32 {
			width = 8
		}
		if f.HasDefault && f.DefInt != nil {
			return jsIntLit(f.DefInt, width)
		}
		if width >= 8 {
			return "0n"
		}
		return "0"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			g.needDecl(f.Type.Name, f.Type.Name)
			if f.HasDefault && f.DefVariant != "" {
				return f.Type.Name + "." + f.DefVariant
			}
			return f.Type.Name + ".None"
		case *ir.Flags:
			return "0n"
		}
	}
	return "0"
}

// elisionRead is the expression an elision test compares against the default.
// It is the storage itself everywhere but at float32, where the storage is a
// double and the wire carries four bytes: rounding the read through
// Math.fround makes the comparison the one C#'s typed float storage makes for
// free, and makes it agree with the bytes putF32 would have written.
func elisionRead(expr string, t ir.FieldType) string {
	if t.Kind == ir.TFloat32 {
		return "Math.fround(" + expr + ")"
	}
	return expr
}

// float32DefaultNote explains a float32 default whose literal is not the one
// the schema spells. JavaScript has only doubles, so a float32's default is
// written as the double a float32 WIDENS TO — `0.2` and the value four bytes of
// wire produce are different numbers, and an elision comparing against the
// former would never hold. A reader meeting 0.20000000298023224 in generated
// source is owed the reason at the line.
func float32DefaultNote(f *ir.Field, literal string) string {
	if f.Type.Kind != ir.TFloat32 || !f.HasDefault {
		return ""
	}
	spelled := formatFloat64(f.DefFloat)
	if spelled == literal {
		return ""
	}
	return fmt.Sprintf(" // float32 %s exactly: the double four bytes of wire read back as", spelled)
}

// member is a field's JavaScript storage member name — the PascalCase mapping
// the packet emitter uses (ir.GoExportName), so one field is spelled one way
// across a unit.
func member(f *ir.Field) string { return ir.GoExportName(f.Name) }

// ---- storage (table declarations only; closure types come from <Base>.js) ----

func (g *tableGen) emitTableClass(st *ir.Struct) {
	g.pf("%s", ir.DocComment(st.Doc, "", "//"))
	g.pf("// table %s — TABLE-wire storage: public fields, every buffer allocated at\n", st.Name)
	g.pf("// construction, declared defaults in the constructor (docs/SPEC-TABLES.md)\n")
	g.pf("export class %s {\n  constructor() {\n", st.Name)
	prevGuard := ""
	for _, f := range st.Fields {
		if f.Guard != prevGuard {
			if f.Guard != "" {
				g.pf("\n    // %s — guarded fields stay off the wire when the guard says so;\n", f.Guard)
				g.pf("    // a read's restored defaults stand in for the untaken side\n")
			} else {
				g.pf("\n")
			}
			prevGuard = f.Guard
		}
		g.pf("%s", ir.DocComment(f.Doc, "    ", "//"))
		g.emitTableStorageField(f)
	}
	g.pf("  }\n}\n\n")
}

// jsElementFactory is the expression that builds one element of a class-typed
// array — a nested table, a nested type, or a union.
func (g *tableGen) jsElementFactory(f *ir.Field) string {
	g.needClass(f.Type)
	return "() => new " + f.Type.Name + "()"
}

// needClass imports the storage class of a named reference: a table's class
// lives in that file's TABLE module, a `type`'s and a union's in its PACKET
// module.
func (g *tableGen) needClass(t ir.FieldType) {
	if t.Kind != ir.TNamed {
		return
	}
	if st, ok := t.Ref.(*ir.Struct); ok && st.IsTable {
		g.needTable(t.Name, t.Name)
		return
	}
	g.needDecl(t.Name, t.Name)
}

func (g *tableGen) emitTableStorageField(f *ir.Field) {
	name := member(f)
	switch {
	case f.Type.Kind == ir.TString:
		g.pf("    this.%s = new Uint8Array(%d); // string(%s): max length, used length beside it\n",
			name, f.Type.Size, ir.RenderExpr(f.Type.SizeExpr))
		g.pf("    this.%sLength = 0;\n", name)
	case f.Type.Kind == ir.TBytes:
		g.pf("    this.%s = new Uint8Array(%d); // bytes(%s): fixed buffer, used length beside it\n",
			name, f.Type.Size, ir.RenderExpr(f.Type.SizeExpr))
		g.pf("    this.%sLength = 0;\n", name)
	case f.KeyEnum != "":
		// ONE SLOT PER NAMED VARIANT, the key k at index k-1: nothing is
		// stored for None, and the accessor is the only place the shift
		// appears. Every named slot exists, so there is no count companion,
		// and the extent comes from the enum's own Max — nothing outside the
		// array names its size (docs/SPEC-TABLES.md §2.4).
		g.needRuntime("TableKeyed")
		g.needDecl(f.KeyEnum, f.KeyEnum)
		factory := "null"
		if isClassRef(f.Type) {
			factory = g.jsElementFactory(f)
		}
		g.pf("    this.%s = new TableKeyed(%s.Max, %s); // [%s]: one slot per named variant, keyed by the value\n",
			name, f.KeyEnum, factory, f.KeyEnum)
	case f.Array != ir.ArrayNone:
		switch {
		case isClassRef(f.Type):
			g.pf("    this.%s = Array.from({ length: %d }, %s);\n", name, f.ArrayBound, g.jsElementFactory(f))
		case isByteElem(f.Type):
			// uint8 arrays store as Uint8Array — the packet emitter's own
			// spelling, so one field is one type across a unit
			g.pf("    this.%s = new Uint8Array(%d);\n", name, f.ArrayBound)
		default:
			g.pf("    this.%s = new Array(%d).fill(%s);\n", name, f.ArrayBound, g.zeroValue(f.Type))
		}
		if f.Array == ir.ArrayCounted {
			g.pf("    this.%sCount = 0; // used count beside it; count in [0, %d]\n", name, f.ArrayBound)
		}
	default:
		init := ""
		note := ""
		if isClassRef(f.Type) {
			// preallocated at construction — the storage principle: nothing
			// heap-allocates per value after it exists
			g.needClass(f.Type)
			init = "new " + f.Type.Name + "()"
		} else {
			init = g.fieldDefaultExpr(f)
			note = float32DefaultNote(f, init)
		}
		g.pf("    this.%s = %s;%s\n", name, init, note)
	}
	if f.Type.Optional {
		// `?T` — the value plus its presence bool, and nothing else: the
		// holder stays a fixed-size record (docs/SPEC-TABLES.md §2.3). PRESENCE,
		// not content, decides whether the field rides.
		g.pf("    this.%sPresent = false; // ?%s: absent until set\n", name, tableFieldTypeName(f))
	}
}

// zeroValue is the §5 zero form of one array slot — the packet emitter's own
// (internal/codegen/js), because a closure `type`'s array is that emitter's
// storage and a table's must read as the same thing.
func (g *tableGen) zeroValue(t ir.FieldType) string {
	switch t.Kind {
	case ir.TBool:
		return "false"
	case ir.TFloat32, ir.TFloat64:
		return "0.0"
	case ir.TNamed:
		switch t.Ref.(type) {
		case *ir.Enum:
			g.needDecl(t.Name, t.Name)
			return t.Name + ".None"
		case *ir.Flags:
			return "0n"
		}
	}
	if isBigStorage(t) {
		return "0n"
	}
	return "0"
}

// isBigStorage reports a field type whose JavaScript storage is a BigInt — the
// packet emitter's value-domain seam: widths past 32 bits, and flags.
func isBigStorage(t ir.FieldType) bool {
	switch t.Kind {
	case ir.TInt, ir.TFixed, ir.TBits:
		return t.Width > 32
	case ir.TNamed:
		_, isFlags := t.Ref.(*ir.Flags)
		return isFlags
	}
	return false
}

// isByteElem reports a uint8 element type — the arrays the packet emitter
// stores as Uint8Array.
func isByteElem(t ir.FieldType) bool {
	return t.Kind == ir.TInt && !t.Signed && t.Width == 8
}

// isClassRef reports a named reference whose JavaScript storage is a class
// instance: a generated struct/table class or a union.
func isClassRef(t ir.FieldType) bool {
	if t.Kind != ir.TNamed {
		return false
	}
	switch t.Ref.(type) {
	case *ir.Struct, *ir.Union:
		return true
	}
	return false
}

// keyedSlots renders a keyed array's RAW slot storage, the form the codecs
// index by slot number rather than by variant.
//
// A TABLE's keyed field is a TableKeyed, whose slots sit behind .Slots; a
// closure `type`'s field is its PACKET storage — a plain array — because a
// type's class is emitted by the packet backend and nothing on this wire
// changes that (docs/SPEC-TABLES.md §2.4). Both are E.Max elements with the key
// k at index k-1, so only the spelling differs.
func (g *tableGen) keyedSlots(access string, f *ir.Field) string {
	name := access + member(f)
	if f.KeyEnum != "" && g.owner != nil && g.owner.IsTable {
		return name + ".Slots"
	}
	return name
}

// ---- enum identity on the table wire (docs/SPEC-TABLES.md §5) ----

func enumRef(f *ir.Field) *ir.Enum {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	e, _ := f.Type.Ref.(*ir.Enum)
	return e
}

// enumIdFn / enumValueFn name one enum's identity pair. C++ and C# overload on
// the enum's own type; JavaScript has no overloading and an enum is a frozen
// object of plain Numbers, so the pair takes the enum's name in its own — and
// the checker claims both spellings for every enum of a unit that declares a
// table (§11).
func enumIdFn(name string) string    { return "TableEnumId" + name }
func enumValueFn(name string) string { return "TableEnumValue" + name }

// emitEnumIdentity emits one enum's value <-> table-wire id pair. Emitted by
// the module of the file that DECLARES the enum, once per unit.
//
// Both halves answer `undefined` for "no identity", where C++ and C# return
// false through an out parameter: it is the language's own spelling of "no
// value" — what Map.get and Array.find answer — and a Smi-or-undefined result
// crosses a call without boxing anything.
func (g *tableGen) emitEnumIdentity(e *ir.Enum) {
	g.needDecl(e.Name, e.Name)
	g.pf("// %s on the TABLE wire: a value rides as the u16 hash of its VARIANT\n", e.Name)
	g.pf("// NAME, so a variant may be added anywhere, removed, or reordered and old\n")
	g.pf("// data still reads (docs/SPEC-TABLES.md §5). None is the one reserved id, 0.\n")
	g.pf("// undefined is \"no wire identity\": a value no variant names, or an id this\n")
	g.pf("// build cannot name.\n")
	g.pf("export function %s(value) {\n", enumIdFn(e.Name))
	g.pf("  switch (value) {\n")
	g.pf("    case %s.None: return 0;\n", e.Name)
	for _, v := range e.Variants {
		g.pf("    case %s.%s: return 0x%04x;\n", e.Name, v, ir.VariantId(v))
	}
	g.pf("    default: return undefined; // no variant names this value: no wire identity\n")
	g.pf("  }\n}\n\n")
	g.pf("export function %s(id) {\n", enumValueFn(e.Name))
	g.pf("  switch (id) {\n")
	g.pf("    case 0: return %s.None;\n", e.Name)
	for _, v := range e.Variants {
		g.pf("    case 0x%04x: return %s.%s;\n", ir.VariantId(v), e.Name, v)
	}
	g.pf("    default: return undefined; // an id this build cannot name\n")
	g.pf("  }\n}\n\n")
}

// refusalExpr renders the throw a codec makes when a value violates a storage
// invariant — a count or length past its bound, an enum value or union tag no
// variant names — with the offending value interpolated into the message. C++
// and C# answer -1 for these; the JavaScript shape is an exception, because
// the value is the CALLER's and a value the wire cannot carry is the caller's
// error — the report is for what the DATA does, never for what the caller
// did. The message names the table and the field.
func (g *tableGen) refusalExpr(f *ir.Field, what, expr string) string {
	owner := ""
	if g.owner != nil {
		owner = g.owner.Name + "."
	}
	return fmt.Sprintf("throw new RangeError(%q + %s + %q);", owner+f.Name+": ", expr, " "+what)
}

// needEnumIdentity imports one enum's identity pair from the module that
// declares the enum.
func (g *tableGen) needEnumIdentity(name string) {
	if base, ok := g.unit.DeclFile[name]; ok {
		g.need(base+"Table", enumIdFn(name), enumValueFn(name))
	}
}

// ---- guards ----

// tableGuardExprs composes each guarded field's branch condition against the
// JavaScript storage members ("value.Active && !value.HasTarget").
func tableGuardExprs(st *ir.Struct) map[string]string { return guardWalk(st, true) }

// tableGuardStrings is the schema-facing twin for the reflection descriptors
// ("at_rest", "!at_rest", "active && has_target").
func tableGuardStrings(st *ir.Struct) map[string]string { return guardWalk(st, false) }

func guardWalk(st *ir.Struct, js bool) map[string]string {
	name := func(cond string) string {
		if js {
			return "value." + ir.GoExportName(cond)
		}
		return cond
	}
	guards := map[string]string{}
	var walk func(items []ir.Item, cond string)
	walk = func(items []ir.Item, cond string) {
		for _, item := range items {
			switch item := item.(type) {
			case *ir.FieldItem:
				if cond != "" {
					guards[item.F.Name] = cond
				}
			case *ir.Branch:
				pos, neg := name(item.Cond), "!"+name(item.Cond)
				if item.Neg {
					pos, neg = neg, pos
				}
				and := func(a, b string) string {
					if a == "" {
						return b
					}
					return a + " && " + b
				}
				walk(item.Then, and(cond, pos))
				walk(item.Else, and(cond, neg))
			}
		}
	}
	walk(st.Items, "")
	return guards
}

// ---- reset: the reader's prefill ----

// emitTableReset restores a value's declared defaults IN PLACE — the twin of
// the C++ reader's placement-new prefill, in place on purpose: reusing the
// caller's buffers is what keeps the read path free of allocation.
//
// The name is the name-first `<Name>Reset` §11 already claims for every
// closure member, which is C++'s spelling; C# spells a verb-first overload set
// because it has overloading and JavaScript does not.
func (g *tableGen) emitTableReset(st *ir.Struct) {
	g.pf("// %sReset restores %s's declared defaults in place, reusing every buffer\n", st.Name, st.Name)
	g.pf("// the value already owns. The reader calls it before overlaying.\n")
	g.pf("export function %sReset(value) {\n", st.Name)
	if len(st.Fields) == 0 {
		g.pf("  // empty type: nothing to restore\n")
	}
	for _, f := range st.Fields {
		g.emitTableResetField(f)
		if f.Type.Optional {
			g.pf("  value.%sPresent = false;\n", member(f))
		}
	}
	g.pf("}\n\n")
}

func (g *tableGen) emitTableResetField(f *ir.Field) {
	name := member(f)
	switch {
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		g.pf("  value.%s.fill(0);\n", name)
		g.pf("  value.%sLength = 0;\n", name)
	case f.Array != ir.ArrayNone && isClassRef(f.Type):
		base := g.keyedSlots("value.", f)
		g.pf("  for (let i = 0; i < %s.length; i++) {\n", base)
		if un, isUnion := f.Type.Ref.(*ir.Union); isUnion {
			g.needDecl(un.Name, un.Name+"Type")
			g.pf("    %s[i].Type = %sType.None;\n", base, f.Type.Name)
		} else {
			g.needTable(f.Type.Name, f.Type.Name+"Reset")
			g.pf("    %sReset(%s[i]);\n", f.Type.Name, base)
		}
		g.pf("  }\n")
		if f.Array == ir.ArrayCounted {
			g.pf("  value.%sCount = 0;\n", name)
		}
	case f.Array != ir.ArrayNone:
		base := g.keyedSlots("value.", f)
		g.pf("  %s.fill(%s);\n", base, g.zeroValue(f.Type))
		if f.Array == ir.ArrayCounted {
			g.pf("  value.%sCount = 0;\n", name)
		}
	default:
		if un, isUnion := f.Type.Ref.(*ir.Union); isUnion && f.Type.Kind == ir.TNamed {
			// the tag is the whole reset: an arm zero-establishes when the
			// reader selects it, exactly as the packet reader does
			g.needDecl(un.Name, un.Name+"Type")
			g.pf("  value.%s.Type = %sType.None;\n", name, f.Type.Name)
			return
		}
		if isClassRef(f.Type) {
			g.needTable(f.Type.Name, f.Type.Name+"Reset")
			g.pf("  %sReset(value.%s);\n", f.Type.Name, name)
			return
		}
		g.pf("  value.%s = %s;\n", name, g.fieldDefaultExpr(f))
	}
}

// ---- measure ----

// emitTableMeasure emits <X>Measure: the EXACT encoded size of a value,
// writing nothing — the parallel-generation lever. Mirrors <X>SaveBody's
// elision decisions branch for branch: for any value, Save writes exactly this
// many bytes into a buffer of exactly this size. A value violating its storage
// invariants throws a RangeError naming the field, exactly where the write
// side refuses it.
func (g *tableGen) emitTableMeasure(st *ir.Struct) {
	g.pf("export function %sMeasure(value) {\n", st.Name)
	g.pf("  let bytes = 2; // terminator\n")
	guards := tableGuardExprs(st)
	for _, f := range st.Fields {
		if cond, guarded := guards[f.Name]; guarded {
			g.pf("  if (%s) {\n", cond)
			g.indent = "  "
			g.emitTableMeasureField(f)
			g.indent = ""
			g.pf("  }\n")
			continue
		}
		g.emitTableMeasureField(f)
	}
	g.pf("  return bytes;\n}\n\n")
}

func (g *tableGen) emitTableMeasureField(f *ir.Field) {
	name := member(f)
	kind := tableScalarKind(f)
	width := tableKindWidth(kind)
	switch {
	case f.Type.Optional:
		// an optional's PRESENCE is the Payload: it rides even when the value
		// is entirely default, exactly as a pointer's pointee does — otherwise
		// absent and present-at-default would be one value on the wire
		g.pf("  if (value.%sPresent) { // ?%s: presence decides, not content\n", name, tableFieldTypeName(f))
		switch {
		case kind == tkTable:
			g.needTable(f.Type.Name, f.Type.Name+"Measure")
			g.pf("    bytes += 3 + 4 + %sMeasure(value.%s); // %s\n", f.Type.Name, name, f.Name)
		case enumRef(f) != nil:
			g.needEnumIdentity(f.Type.Name)
			g.pf("    if (%s(value.%s) === undefined) { %s }\n", enumIdFn(f.Type.Name), name,
				g.refusalExpr(f, "is a value no variant names, so it has no wire identity", "value."+name))
			g.pf("    bytes += 3 + 2; // %s: the variant's name hash\n", f.Name)
		default:
			g.pf("    bytes += 3 + %d; // %s\n", width, f.Name)
		}
		g.pf("  }\n")
	case f.KeyEnum != "":
		// enum-keyed: the body carries (variant id, length-prefixed element)
		// pairs, so a slot lands by NAME however the enum moved. A slot at its
		// default elides like any default, and an empty array elides whole.
		g.pf("  {\n")
		g.pf("    let pairs = 0;\n    let keyedBytes = 0;\n")
		g.pf("    for (let i = 0; i < %d; i++) { // [%s]: every stored slot is a named variant's\n", f.ArrayBound, f.KeyEnum)
		g.emitKeyedSlotRides(f, kind, "      ")
		if kind == tkTable {
			g.pf("      pairs++; keyedBytes += 2 + 4 + elemBytes; // key, length, body\n")
		} else {
			g.pf("      pairs++; keyedBytes += 2 + 4 + %d; // key, length, element\n", width)
		}
		g.pf("    }\n")
		g.pf("    if (pairs > 0) { bytes += 3 + 4 + 5 + keyedBytes; } // %s\n", f.Name)
		g.pf("  }\n")
	case f.Type.Kind == ir.TString:
		g.emitLengthCheck(f)
		g.pf("  if (value.%sLength > 0) { bytes += 3 + 4 + value.%sLength; } // %s\n", name, name, f.Name)
	case f.Type.Kind == ir.TBytes:
		g.emitLengthCheck(f)
		g.pf("  if (value.%sLength > 0) { bytes += 3 + 4 + 5 + value.%sLength; } // %s\n", name, name, f.Name)
	case f.Array == ir.ArrayCounted && kind == tkTable:
		g.needTable(f.Type.Name, f.Type.Name+"Measure")
		g.emitCountCheck(f)
		g.pf("  if (value.%sCount > 0) {\n", name)
		g.pf("    bytes += 3 + 4 + 5; // %s\n", f.Name)
		g.pf("    for (let i = 0; i < value.%sCount; i++) {\n", name)
		g.pf("      bytes += 4 + %sMeasure(value.%s[i]);\n", f.Type.Name, name)
		g.pf("    }\n  }\n")
	case f.Array == ir.ArrayCounted:
		g.emitCountCheck(f)
		g.pf("  if (value.%sCount > 0) {\n", name)
		g.emitEnumElementCheck(f, fmt.Sprintf("value.%s[i]", name), fmt.Sprintf("value.%sCount", name), "    ")
		g.pf("    bytes += 3 + 4 + 5 + value.%sCount * %d; // %s\n", name, width, f.Name)
		g.pf("  }\n")
	case f.Array == ir.ArrayFixed && kind == tkTable:
		g.needTable(f.Type.Name, f.Type.Name+"Measure")
		g.pf("  {\n")
		g.pf("    bytes += 3 + 4 + 5; // %s (fixed [%d])\n", f.Name, f.ArrayBound)
		g.pf("    for (let i = 0; i < %d; i++) {\n", f.ArrayBound)
		g.pf("      bytes += 4 + %sMeasure(value.%s[i]);\n", f.Type.Name, name)
		g.pf("    }\n  }\n")
	case f.Array == ir.ArrayFixed:
		def := g.fieldDefaultExpr(f)
		g.pf("  {\n")
		g.pf("    let allDefault = true;\n")
		g.pf("    for (let i = 0; i < %d; i++) { if (%s !== %s) { allDefault = false; break; } }\n",
			f.ArrayBound, elisionRead(fmt.Sprintf("value.%s[i]", name), f.Type), def)
		g.pf("    if (!allDefault) {\n")
		g.emitEnumElementCheck(f, fmt.Sprintf("value.%s[i]", name), fmt.Sprintf("%d", f.ArrayBound), "      ")
		g.pf("      bytes += 3 + 4 + 5 + %d; // %s\n", f.ArrayBound*int64(width), f.Name)
		g.pf("    }\n  }\n")
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		g.needDecl(un.Name, un.Name+"Type")
		g.pf("  switch (value.%s.Type) { // %s\n", name, f.Name)
		g.pf("    case %sType.None: break; // None elides — TLV absence is the None\n", un.Name)
		for _, v := range un.Variants {
			g.needTable(v.Type, v.Type+"Measure")
			g.pf("    case %sType.%s:\n", un.Name, ir.GoExportName(v.Name))
			g.pf("      bytes += 3 + 2 + 4 + %sMeasure(value.%s.%s); // the u16 ARM ID, then the arm length-prefixed\n      break;\n",
				v.Type, name, ir.GoExportName(v.Name))
		}
		g.pf("    default: %s // the write side refuses it too\n", g.refusalExpr(f, "is a union tag no arm names", "value."+name+".Type"))
		g.pf("  }\n")
	case kind == tkTable:
		g.needTable(f.Type.Name, f.Type.Name+"Measure")
		g.pf("  {\n")
		g.pf("    const body = %sMeasure(value.%s);\n", f.Type.Name, name)
		g.pf("    if (body > 2) { bytes += 3 + 4 + body; } // %s: all-default nested elides\n", f.Name)
		g.pf("  }\n")
	case enumRef(f) != nil:
		g.needEnumIdentity(f.Type.Name)
		g.pf("  if (value.%s !== %s) {\n", name, g.fieldDefaultExpr(f))
		g.pf("    if (%s(value.%s) === undefined) { %s }\n", enumIdFn(f.Type.Name), name,
			g.refusalExpr(f, "is a value no variant names, so it has no wire identity", "value."+name))
		g.pf("    bytes += 3 + 2; // %s: the variant's name hash\n  }\n", f.Name)
	default:
		g.pf("  if (%s !== %s) { bytes += 3 + %d; } // %s\n",
			elisionRead("value."+name, f.Type), g.fieldDefaultExpr(f), width, f.Name)
	}
}

// emitKeyedSlotRides emits the head of an enum-keyed array's per-slot loop
// (docs/SPEC-TABLES.md §2.4, §3.2). It elides a slot holding its default, refuses
// a slot whose value or whose KEY no variant names — a value with no wire
// identity is refused rather than silently renamed, the enum rule applied to
// slots — and leaves `keyId` holding the slot's wire id. For a table element
// `elemBytes` holds the measured body, so measure and save decide elision on
// the same number.
func (g *tableGen) emitKeyedSlotRides(f *ir.Field, kind int, ind string) {
	expr := g.keyedSlots("value.", f) + "[i]"
	switch {
	case kind == tkTable:
		g.needTable(f.Type.Name, f.Type.Name+"Measure")
		g.pf("%sconst elemBytes = %sMeasure(%s);\n", ind, f.Type.Name, expr)
		g.pf("%sif (elemBytes <= 2) { continue; } // an all-default slot elides\n", ind)
	case enumRef(f) != nil:
		g.needEnumIdentity(f.Type.Name)
		g.pf("%sif (%s === %s) { continue; } // a default slot elides\n", ind, expr, g.fieldDefaultExpr(f))
		g.pf("%sif (%s(%s) === undefined) { %s }\n", ind, enumIdFn(f.Type.Name), expr,
			g.refusalExpr(f, "is a value no variant names, so it has no wire identity", expr))
	default:
		g.pf("%sif (%s === %s) { continue; } // a default slot elides\n",
			ind, elisionRead(expr, f.Type), g.fieldDefaultExpr(f))
	}
	g.needEnumIdentity(f.KeyEnum)
	g.pf("%sconst keyId = %s(i + 1); // i is the STORAGE index; the key it holds is i + 1\n", ind, enumIdFn(f.KeyEnum))
	g.pf("%sif (keyId === undefined) { %s }\n", ind, g.refusalExpr(f, "keys a slot no variant names", "(i + 1)"))
}

// emitEnumElementCheck validates an enum ARRAY's elements before they ride: a
// value no variant names has no wire identity, so the value is refused rather
// than silently written as None (the union tag's rule, applied to enums).
func (g *tableGen) emitEnumElementCheck(f *ir.Field, expr, count, ind string) {
	if enumRef(f) == nil {
		return
	}
	g.needEnumIdentity(f.Type.Name)
	g.pf("%sfor (let i = 0; i < %s; i++) { // %s: every element must be nameable\n", ind, count, f.Name)
	g.pf("%s  if (%s(%s) === undefined) { %s }\n%s}\n", ind, enumIdFn(f.Type.Name), expr,
		g.refusalExpr(f, "is a value no variant names, so it has no wire identity", expr), ind)
}

// emitLengthCheck refuses a string's or a bytes' used length outside its
// declared bound — the storage invariant, held where C++ answers -1.
func (g *tableGen) emitLengthCheck(f *ir.Field) {
	name := member(f)
	g.pf("  if (!(value.%sLength >= 0) || value.%sLength > %d) { %s }\n", name, name, f.Type.Size,
		g.refusalExpr(f, fmt.Sprintf("is a used length past the declared %d", f.Type.Size), "value."+name+"Length"))
}

// emitCountCheck refuses a counted array's used count outside its bound.
func (g *tableGen) emitCountCheck(f *ir.Field) {
	name := member(f)
	g.pf("  if (!(value.%sCount >= 0) || value.%sCount > %d) { %s }\n", name, name, f.ArrayBound,
		g.refusalExpr(f, fmt.Sprintf("is a used count past the declared %d", f.ArrayBound), "value."+name+"Count"))
}

// ---- write / save ----

func (g *tableGen) emitTableWrite(st *ir.Struct) {
	g.pf("export function %sSaveBody(w, value) {\n", st.Name)
	guards := tableGuardExprs(st)
	for _, f := range st.Fields {
		if cond, guarded := guards[f.Name]; guarded {
			g.pf("  if (%s) {\n", cond)
			g.indent = "  "
			g.emitTableWriteField(f)
			g.indent = ""
			g.pf("  }\n")
			continue
		}
		g.emitTableWriteField(f)
	}
	g.pf("  w.put16(0); // terminator\n")
	g.pf("  return !w.Overflow;\n}\n\n")
}

// emitTableSave emits the buffer-level entries of the measure/save pair, in
// the language's own two spellings: <X>Save hands back a fresh Uint8Array of
// exactly <X>Measure's size — the shape a JavaScript caller expects from a
// serializer — and <X>SaveInto writes into a Uint8Array the caller owns and
// answers the bytes written, which is the zero-allocation half. A buffer too
// small is the caller's error and throws; C++ answers -1 there.
func (g *tableGen) emitTableSave(st *ir.Struct) {
	g.needRuntime("TableWriter")
	g.pf("export function %sSave(value) {\n", st.Name)
	g.pf("  const buffer = new Uint8Array(%sMeasure(value));\n", st.Name)
	g.pf("  %sSaveBody(new TableWriter(buffer), value); // cannot overflow: the buffer is measure's answer\n", st.Name)
	g.pf("  return buffer;\n}\n\n")
	g.pf("export function %sSaveInto(value, buffer) {\n", st.Name)
	g.pf("  const w = new TableWriter(buffer);\n")
	g.pf("  if (!%sSaveBody(w, value)) {\n", st.Name)
	g.pf("    throw new RangeError(\"%sSaveInto: a buffer of \" + buffer.length + \" bytes is short of the \" + %sMeasure(value) + \" the value measures\");\n", st.Name, st.Name)
	g.pf("  }\n")
	g.pf("  return w.Offset; // == %sMeasure(value)\n}\n\n", st.Name)
}

func (g *tableGen) emitTableWriteField(f *ir.Field) {
	name := member(f)
	id := ir.TableFieldId(f)
	kind := tableScalarKind(f)
	switch {
	case f.Type.Optional:
		// present: the payload ALWAYS rides, all-default included — the
		// pointer's rule, and what makes ?T, *T and a plain nesting
		// wire-identical (docs/SPEC-TABLES.md §2.3, §3.1)
		g.pf("  if (value.%sPresent) { // ?%s\n", name, tableFieldTypeName(f))
		switch {
		case kind == tkTable:
			g.needTable(f.Type.Name, f.Type.Name+"Measure", f.Type.Name+"SaveBody")
			g.pf("    w.field(0x%04x, %d); // %s\n", id, tkTable, f.Name)
			g.pf("    w.put32(%sMeasure(value.%s));\n", f.Type.Name, name)
			g.pf("    if (!%sSaveBody(w, value.%s)) { return false; }\n", f.Type.Name, name)
		case enumRef(f) != nil:
			g.needEnumIdentity(f.Type.Name)
			g.pf("    const variantId = %s(value.%s);\n", enumIdFn(f.Type.Name), name)
			g.pf("    if (variantId === undefined) { %s }\n",
				g.refusalExpr(f, "is a value no variant names, so it has no wire identity", "value."+name))
			g.pf("    w.field(0x%04x, %d); // %s\n", id, kind, f.Name)
			g.pf("    w.put16(variantId);\n")
		default:
			g.pf("    w.field(0x%04x, %d); // %s\n", id, kind, f.Name)
			g.emitTableWriteElement(f, kind, "value."+name, "    ")
		}
		g.pf("  }\n")
	case f.KeyEnum != "":
		// the slots ride KEYED: (variant id, length-prefixed element) pairs,
		// counted like any array's elements. Two passes so the count is known
		// before the header rides, and so measure and save agree byte for byte.
		g.pf("  {\n")
		g.pf("    let pairs = 0;\n")
		g.pf("    for (let i = 0; i < %d; i++) { // [%s]: every stored slot is a named variant's\n", f.ArrayBound, f.KeyEnum)
		g.emitKeyedSlotRides(f, kind, "      ")
		g.pf("      pairs++;\n")
		g.pf("    }\n")
		g.pf("    if (pairs > 0) {\n")
		g.pf("      // KIND 16, not 14: a keyed body and a positional one are\n")
		g.pf("      // incompatible, so a reader of the other kind must see a kind\n")
		g.pf("      // mismatch and skip, never misdecode (docs/SPEC-TABLES.md §3.2)\n")
		g.pf("      w.field(0x%04x, %d); // %s (keyed by %s)\n", id, tkKeyed, f.Name, f.KeyEnum)
		g.pf("      const lenAt = w.open32();\n")
		g.pf("      w.put8(%d); w.put32(pairs);\n", kind)
		g.pf("      // ASCENDING BY VARIANT ORDINAL, which is slot order — this\n")
		g.pf("      // writer's choice, and a reader must not rely on it: every\n")
		g.pf("      // slot is found by its key (docs/SPEC-TABLES.md §3.2)\n")
		g.pf("      for (let i = 0; i < %d; i++) {\n", f.ArrayBound)
		g.emitKeyedSlotRides(f, kind, "        ")
		g.pf("        w.put16(keyId); // the slot's VARIANT id, not its position\n")
		g.pf("        const elemLenAt = w.open32();\n")
		if kind == tkTable {
			g.needTable(f.Type.Name, f.Type.Name+"SaveBody")
			g.pf("        if (!%sSaveBody(w, %s[i])) { return false; }\n", f.Type.Name, g.keyedSlots("value.", f))
		} else {
			g.emitTableWriteElement(f, kind, g.keyedSlots("value.", f)+"[i]", "        ")
		}
		g.pf("        w.close32(elemLenAt);\n")
		g.pf("      }\n")
		g.pf("      w.close32(lenAt);\n")
		g.pf("    }\n  }\n")
	case f.Type.Kind == ir.TString:
		g.emitLengthCheck(f)
		g.pf("  if (value.%sLength > 0) {\n", name)
		g.pf("    w.field(0x%04x, %d); // %s\n", id, tkString, f.Name)
		g.pf("    w.put32(value.%sLength);\n", name)
		g.pf("    w.raw(value.%s, 0, value.%sLength);\n  }\n", name, name)
	case f.Type.Kind == ir.TBytes:
		g.emitLengthCheck(f)
		g.pf("  if (value.%sLength > 0) {\n", name)
		g.pf("    w.field(0x%04x, %d); // %s\n", id, tkArray, f.Name)
		g.pf("    w.put32(5 + value.%sLength);\n", name)
		g.pf("    w.put8(%d); w.put32(value.%sLength);\n", tkU8, name)
		g.pf("    w.raw(value.%s, 0, value.%sLength);\n  }\n", name, name)
	case f.Array == ir.ArrayCounted:
		g.emitCountCheck(f)
		g.pf("  if (value.%sCount > 0) {\n", name)
		g.pf("    w.field(0x%04x, %d); // %s\n", id, tkArray, f.Name)
		g.pf("    const lenAt = w.open32();\n")
		g.pf("    w.put8(%d); w.put32(value.%sCount);\n", kind, name)
		g.pf("    for (let i = 0; i < value.%sCount; i++) {\n", name)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", name), "      ")
		g.pf("    }\n")
		g.pf("    w.close32(lenAt);\n  }\n")
	case f.Array == ir.ArrayFixed && kind == tkTable:
		// fixed arrays of tables always ride — position is identity there
		g.pf("  {\n")
		g.pf("    w.field(0x%04x, %d); // %s (fixed [%d])\n", id, tkArray, f.Name, f.ArrayBound)
		g.pf("    const lenAt = w.open32();\n")
		g.pf("    w.put8(%d); w.put32(%d);\n", kind, f.ArrayBound)
		g.pf("    for (let i = 0; i < %d; i++) {\n", f.ArrayBound)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", name), "      ")
		g.pf("    }\n")
		g.pf("    w.close32(lenAt);\n  }\n")
	case f.Array == ir.ArrayFixed:
		// fixed arrays are positional; an all-default array elides entirely
		// (parity with the reader's restored defaults)
		def := g.fieldDefaultExpr(f)
		g.pf("  {\n")
		g.pf("    let allDefault = true;\n")
		g.pf("    for (let i = 0; i < %d; i++) { if (%s !== %s) { allDefault = false; break; } }\n",
			f.ArrayBound, elisionRead(fmt.Sprintf("value.%s[i]", name), f.Type), def)
		g.pf("    if (!allDefault) {\n")
		g.pf("      w.field(0x%04x, %d); // %s (fixed [%d])\n", id, tkArray, f.Name, f.ArrayBound)
		g.pf("      const lenAt = w.open32();\n")
		g.pf("      w.put8(%d); w.put32(%d);\n", kind, f.ArrayBound)
		g.pf("      for (let i = 0; i < %d; i++) {\n", f.ArrayBound)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", name), "        ")
		g.pf("      }\n")
		g.pf("      w.close32(lenAt);\n    }\n  }\n")
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		g.needDecl(un.Name, un.Name+"Type")
		g.pf("  if (value.%s.Type !== %sType.None) {\n", name, un.Name)
		g.pf("    w.field(0x%04x, %d); // %s\n", id, tkUnion, f.Name)
		g.pf("    // the ARM ID is the hash of the arm's NAME (docs/SPEC-TABLES.md §5), so\n")
		g.pf("    // arms may be added anywhere, removed and reordered\n")
		g.pf("    switch (value.%s.Type) {\n", name)
		for _, v := range un.Variants {
			g.pf("      case %sType.%s: w.put16(0x%04x); break;\n", un.Name, ir.GoExportName(v.Name), ir.VariantId(v.Name))
		}
		g.pf("      default: %s // write validates the tag before it rides\n", g.refusalExpr(f, "is a union tag no arm names", "value."+name+".Type"))
		g.pf("    }\n")
		g.pf("    const lenAt = w.open32();\n")
		g.pf("    switch (value.%s.Type) {\n", name)
		for _, v := range un.Variants {
			g.needTable(v.Type, v.Type+"SaveBody")
			g.pf("      case %sType.%s: if (!%sSaveBody(w, value.%s.%s)) { return false; } break;\n",
				un.Name, ir.GoExportName(v.Name), v.Type, name, ir.GoExportName(v.Name))
		}
		g.pf("      default: %s // write validates the tag before it rides\n", g.refusalExpr(f, "is a union tag no arm names", "value."+name+".Type"))
		g.pf("    }\n")
		g.pf("    w.close32(lenAt);\n  }\n")
	case kind == tkTable:
		// elision is decided BEFORE any byte is emitted: measuring first keeps
		// an all-default nested field from touching the buffer at all, so
		// saving into a buffer of exactly Measure's size never trips overflow
		// on transient header bytes
		g.needTable(f.Type.Name, f.Type.Name+"Measure", f.Type.Name+"SaveBody")
		g.pf("  {\n")
		g.pf("    const body = %sMeasure(value.%s);\n", f.Type.Name, name)
		g.pf("    if (body > 2) { // all-default nested elides\n")
		g.pf("      w.field(0x%04x, %d); // %s\n", id, tkTable, f.Name)
		g.pf("      w.put32(body);\n")
		g.pf("      if (!%sSaveBody(w, value.%s)) { return false; }\n", f.Type.Name, name)
		g.pf("    }\n  }\n")
	case enumRef(f) != nil:
		// the id is resolved BEFORE the header rides: a value no variant names
		// has no wire identity, and the write refuses it rather than writing None
		g.needEnumIdentity(f.Type.Name)
		g.pf("  if (value.%s !== %s) {\n", name, g.fieldDefaultExpr(f))
		g.pf("    const variantId = %s(value.%s);\n", enumIdFn(f.Type.Name), name)
		g.pf("    if (variantId === undefined) { %s }\n", g.refusalExpr(f, "is a value no variant names, so it has no wire identity", "value."+name))
		g.pf("    w.field(0x%04x, %d); // %s\n", id, kind, f.Name)
		g.pf("    w.put16(variantId);\n  }\n")
	default:
		g.pf("  if (%s !== %s) {\n", elisionRead("value."+name, f.Type), g.fieldDefaultExpr(f))
		g.pf("    w.field(0x%04x, %d); // %s\n", id, kind, f.Name)
		g.emitTableWriteElement(f, kind, "value."+name, "    ")
		g.pf("  }\n")
	}
}

func (g *tableGen) emitTableWriteElement(f *ir.Field, kind int, expr, ind string) {
	if enumRef(f) != nil {
		// writeElementId, not elementId: an enum-keyed array's slot loop is
		// already holding a `const keyId` in the enclosing block, and a
		// redeclaration in a nested block would shadow rather than read.
		g.needEnumIdentity(f.Type.Name)
		g.pf("%s{\n%s  const writeElementId = %s(%s);\n", ind, ind, enumIdFn(f.Type.Name), expr)
		g.pf("%s  if (writeElementId === undefined) { %s }\n", ind, g.refusalExpr(f, "is a value no variant names, so it has no wire identity", expr))
		g.pf("%s  w.put16(writeElementId);\n%s}\n", ind, ind)
		return
	}
	switch kind {
	case tkBool:
		g.pf("%sw.put8(%s ? 1 : 0);\n", ind, expr)
	case tkF32:
		// THE VIEW STORE IS INLINE, and this is the one place this backend
		// does not simply call the runtime. A double that crosses a JS call
		// boundary is a HEAP NUMBER unless the callee is inlined: sixteen
		// bytes, allocated per call, on a path this port claims allocates
		// nothing. Whether V8 inlines `w.putF32(x)` depends on the size of
		// the body around it — which is the estate's own law that a
		// generated codec must not depend on the compiler's inlining
		// budget, in a JIT. So the float write is the store itself.
		g.pf("%sif (w.Offset + 4 <= w.Bytes.length) { w.View.setFloat32(w.Offset, %s, true); w.Offset += 4; } else { w.Overflow = true; }\n", ind, expr)
	case tkF64:
		g.pf("%sif (w.Offset + 8 <= w.Bytes.length) { w.View.setFloat64(w.Offset, %s, true); w.Offset += 8; } else { w.Overflow = true; }\n", ind, expr)
	case tkTable:
		g.needTable(f.Type.Name, f.Type.Name+"SaveBody")
		g.pf("%s{\n%s  const elemLenAt = w.open32();\n", ind, ind)
		g.pf("%s  if (!%sSaveBody(w, %s)) { return false; }\n", ind, f.Type.Name, expr)
		g.pf("%s  w.close32(elemLenAt);\n%s}\n", ind, ind)
	default:
		switch tableKindWidth(kind) {
		case 1:
			g.pf("%sw.put8(%s);\n", ind, expr)
		case 2:
			g.pf("%sw.put16(%s);\n", ind, expr)
		case 4:
			g.pf("%sw.put32(%s);\n", ind, expr)
		default:
			g.pf("%sw.put64(%s);\n", ind, expr)
		}
	}
}

// ---- read ----

func (g *tableGen) emitTableRead(st *ir.Struct) {
	g.pf("export function %sLoadBody(r, value) {\n", st.Name)
	g.pf("  %sReset(value); // restore declared defaults in place, then overlay\n", st.Name)
	g.pf("  for (;;) {\n")
	g.pf("    if (!r.has(2)) { r.Report.Malformed = true; return false; }\n")
	g.pf("    const fieldId = r.get16();\n")
	g.pf("    if (fieldId === 0) { return true; }\n")
	g.pf("    if (!r.has(1)) { r.Report.Malformed = true; return false; }\n")
	g.pf("    const kind = r.get8();\n")
	if len(st.Fields) > 0 {
		g.pf("    switch (fieldId) {\n")
		for _, f := range st.Fields {
			id := ir.TableFieldId(f)
			kind := tableScalarKind(f)
			wireKind := kind
			if f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes {
				wireKind = tkArray
			}
			if f.KeyEnum != "" {
				// a KEYED body is its own kind, so the positional-to-keyed
				// edit (and its reverse) reads as a kind mismatch and is
				// counted, never decoded as the other body (§3.2)
				wireKind = tkKeyed
			}
			if f.Type.Kind == ir.TBytes {
				kind = tkU8 // bytes travel as an array of u8 elements
			}
			g.pf("      case 0x%04x: { // %s\n", id, f.Name)
			g.pf("        if (kind !== %d) { if (!r.mismatch(kind)) { return false; } break; }\n", wireKind)
			g.emitTableReadField(f, kind)
			if f.Type.Optional {
				// the field rode, so it is PRESENT — content decides nothing
				// here either (docs/SPEC-TABLES.md §2.3)
				g.pf("        value.%sPresent = true;\n", member(f))
			}
			g.pf("        break;\n      }\n")
		}
		g.pf("      default: { if (!r.unknown(kind)) { return false; } break; }\n")
		g.pf("    }\n  }\n}\n\n")
	} else {
		g.pf("    if (!r.unknown(kind)) { return false; }\n")
		g.pf("  }\n}\n\n")
	}

	// THE ENTRY POINT HANDS THE VALUE BACK, which is the shape a JavaScript
	// caller expects of a decoder: `const cfg = XLoad(bytes, report)`. The
	// value is a fresh one unless the caller passes its own, and a caller on a
	// per-frame path does — that is the zero-allocation half, and it is the same
	// value the C++ reference takes by reference. What the DATA did is the
	// report's answer, never a return: framing damage sets report.Malformed and
	// keeps the prefix, exactly as C++'s false does.
	g.needRuntime("TableReader", "TableReport")
	g.pf("export function %sLoad(bytes, report, value) {\n", st.Name)
	g.pf("  if (value === undefined || value === null) { value = new %s(); }\n", st.Name)
	g.pf("  const r = new TableReader(bytes, report !== null && report !== undefined ? report : new TableReport());\n")
	g.pf("  %sLoadBody(r, value);\n", st.Name)
	g.pf("  return value;\n}\n\n")
}

func (g *tableGen) emitTableReadField(f *ir.Field, kind int) {
	name := member(f)
	ind := "        "
	switch {
	case f.KeyEnum != "":
		// each pair is placed by its VARIANT id, so a slot lands by name
		// however the enum moved; an id this reader cannot name is skipped by
		// its length and counted unknown, and a slot the writer never sent
		// keeps the prefill's default (docs/SPEC-TABLES.md §3.2)
		g.needEnumIdentity(f.KeyEnum)
		g.pf("%sif (!r.has(4)) { r.Report.Malformed = true; return false; }\n", ind)
		g.pf("%sconst bodyLen = r.get32();\n", ind)
		g.pf("%sif (!r.has(bodyLen)) { r.Report.Malformed = true; return false; }\n", ind)
		g.pf("%sconst bodyEnd = r.Offset + bodyLen;\n", ind)
		g.pf("%sif (bodyLen >= 5) {\n", ind)
		g.pf("%s  const elemKind = r.get8();\n", ind)
		g.pf("%s  const count = r.get32();\n", ind)
		g.pf("%s  if (elemKind !== %d) { r.Report.KindMismatch++; r.Offset = bodyEnd; break; }\n", ind, kind)
		g.pf("%s  const outerLimit = r.Limit;\n", ind)
		g.pf("%s  r.Limit = bodyEnd; // the body's own reach, exactly a slice's\n", ind)
		g.pf("%s  for (let i = 0; i < count; i++) {\n", ind)
		g.pf("%s    if (!r.has(2)) { r.Report.Malformed = true; break; }\n", ind)
		g.pf("%s    const key = r.get16();\n", ind)
		g.pf("%s    if (!r.has(4)) { r.Report.Malformed = true; break; }\n", ind)
		g.pf("%s    const elemLen = r.get32();\n", ind)
		g.pf("%s    if (!r.has(elemLen)) { r.Report.Malformed = true; break; }\n", ind)
		g.pf("%s    if (key === 0) {\n", ind)
		g.pf("%s      // None is the NULL KEY: 0 is the reserved id no declared\n", ind)
		g.pf("%s      // name can fold to, so a body carrying one is DAMAGED, not\n", ind)
		g.pf("%s      // merely foreign. Framing damage stops this body, keeps what\n", ind)
		g.pf("%s      // it decoded, and the parent reads on past the length\n", ind)
		g.pf("%s      // (docs/SPEC-TABLES.md §3.2, §4).\n", ind)
		g.pf("%s      r.Report.Malformed = true;\n%s      break;\n%s    }\n", ind, ind, ind)
		g.pf("%s    const slot = %s(key);\n", ind, enumValueFn(f.KeyEnum))
		g.pf("%s    if (slot === undefined) {\n", ind)
		g.pf("%s      r.Report.Unknown++; // a slot this reader cannot name\n", ind)
		g.pf("%s      r.Offset += elemLen;\n%s      continue;\n%s    }\n", ind, ind, ind)
		g.pf("%s    {\n", ind)
		g.pf("%s      const elemEnd = r.Offset + elemLen;\n", ind)
		g.pf("%s      const pairLimit = r.Limit;\n", ind)
		g.pf("%s      r.Limit = elemEnd;\n", ind)
		// the key k lives at STORAGE INDEX k-1 (docs/SPEC-TABLES.md §2.4)
		slot := g.keyedSlots("value.", f) + "[slot - 1]"
		if kind == tkTable {
			g.needTable(f.Type.Name, f.Type.Name+"LoadBody")
			g.pf("%s      %sLoadBody(r, %s);\n", ind, f.Type.Name, slot)
		} else {
			g.emitTableReadScalarFrom(f, kind, slot, ind+"      ",
				"r.Report.Malformed = true; r.Limit = pairLimit; r.Offset = elemEnd; continue;")
		}
		g.pf("%s      r.Limit = pairLimit;\n", ind)
		g.pf("%s      r.Offset = elemEnd;\n", ind)
		g.pf("%s    }\n", ind)
		g.pf("%s  }\n", ind)
		g.pf("%s  r.Limit = outerLimit;\n", ind)
		g.pf("%s}\n", ind)
		g.pf("%sr.Offset = bodyEnd; // unread pairs and slack skip via the length\n", ind)
	case f.Type.Kind == ir.TString:
		g.pf("%sif (!r.has(4)) { r.Report.Malformed = true; return false; }\n", ind)
		g.pf("%sconst len = r.get32();\n", ind)
		g.pf("%sif (!r.has(len)) { r.Report.Malformed = true; return false; }\n", ind)
		g.pf("%slet keep = len;\n", ind)
		g.pf("%sif (keep > %d) { keep = %d; r.Report.Clamped++; }\n", ind, f.Type.Size, f.Type.Size)
		g.pf("%s// a byte loop, not set(subarray(...)): a subarray is one heap object\n", ind)
		g.pf("%s// per string field, and this read path allocates nothing\n", ind)
		g.pf("%s{\n%s  const from = r.Bytes;\n%s  const to = value.%s;\n", ind, ind, ind, name)
		g.pf("%s  const at = r.Offset;\n", ind)
		g.pf("%s  for (let i = 0; i < keep; i++) { to[i] = from[at + i]; }\n%s}\n", ind, ind)
		g.pf("%svalue.%sLength = keep;\n", ind, name)
		g.pf("%sr.Offset += len;\n", ind)
	case f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		bound := f.ArrayBound
		if f.Type.Kind == ir.TBytes {
			bound = f.Type.Size
		}
		counted := f.Type.Kind == ir.TBytes || f.Array == ir.ArrayCounted
		g.pf("%sif (!r.has(4)) { r.Report.Malformed = true; return false; }\n", ind)
		g.pf("%sconst bodyLen = r.get32();\n", ind)
		g.pf("%sif (!r.has(bodyLen)) { r.Report.Malformed = true; return false; }\n", ind)
		g.pf("%sconst bodyEnd = r.Offset + bodyLen;\n", ind)
		g.pf("%sif (bodyLen >= 5) {\n", ind)
		g.pf("%s  const elemKind = r.get8();\n", ind)
		g.pf("%s  const count = r.get32();\n", ind)
		g.pf("%s  if (elemKind !== %d) { r.Report.KindMismatch++; r.Offset = bodyEnd; break; }\n", ind, kind)
		g.pf("%s  let keep = count;\n", ind)
		g.pf("%s  if (keep > %d) { keep = %d; r.Report.Clamped++; }\n", ind, bound, bound)
		g.pf("%s  // elements are BOUNDED by the field body: a count the length\n", ind)
		g.pf("%s  // cannot cover keeps the decoded prefix, flags malformed, and\n", ind)
		g.pf("%s  // the parent continues at the next field — following fields'\n", ind)
		g.pf("%s  // bytes are never fabricated into elements\n", ind)
		g.pf("%s  const outerLimit = r.Limit;\n", ind)
		g.pf("%s  r.Limit = bodyEnd;\n", ind)
		if counted {
			g.pf("%s  let decoded = 0;\n", ind)
		}
		g.pf("%s  for (let i = 0; i < keep; i++) {\n", ind)
		g.emitTableReadElement(f, kind, ind+"    ")
		if counted {
			g.pf("%s    decoded = i + 1;\n", ind)
		}
		g.pf("%s  }\n", ind)
		g.pf("%s  r.Limit = outerLimit;\n", ind)
		if f.Type.Kind == ir.TBytes {
			g.pf("%s  value.%sLength = decoded;\n", ind, name)
		} else if f.Array == ir.ArrayCounted {
			g.pf("%s  value.%sCount = decoded;\n", ind, name)
		}
		g.pf("%s}\n", ind)
		g.pf("%sr.Offset = bodyEnd; // excess elements and slack skip via the length\n", ind)
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		g.needDecl(un.Name, un.Name+"Type")
		g.pf("%sif (!r.has(2)) { r.Report.Malformed = true; return false; }\n", ind)
		g.pf("%sconst armId = r.get16();\n", ind)
		g.pf("%sif (armId === 0) { value.%s.Type = %sType.None; break; } // empty: the id is the whole payload\n", ind, name, un.Name)
		g.pf("%sif (!r.has(4)) { r.Report.Malformed = true; return false; }\n", ind)
		g.pf("%sconst bodyLen = r.get32();\n", ind)
		g.pf("%sif (!r.has(bodyLen)) { r.Report.Malformed = true; return false; }\n", ind)
		g.pf("%s{\n%s  const bodyEnd = r.Offset + bodyLen;\n", ind, ind)
		g.pf("%s  const outerLimit = r.Limit;\n", ind)
		g.pf("%s  r.Limit = bodyEnd;\n", ind)
		g.pf("%s  switch (armId) { // the arm's NAME hash (docs/SPEC-TABLES.md §5)\n", ind)
		for _, v := range un.Variants {
			g.needTable(v.Type, v.Type+"LoadBody")
			g.pf("%s    case 0x%04x: // %s\n", ind, ir.VariantId(v.Name), v.Name)
			g.pf("%s      value.%s.Type = %sType.%s;\n", ind, name, un.Name, ir.GoExportName(v.Name))
			g.pf("%s      %sLoadBody(r, value.%s.%s);\n", ind, v.Type, name, ir.GoExportName(v.Name))
			g.pf("%s      break;\n", ind)
		}
		g.pf("%s    default:\n", ind)
		g.pf("%s      // an arm this reader cannot name: the value reads EMPTY and\n", ind)
		g.pf("%s      // the body is skipped by its length, never misdecoded. The\n", ind)
		g.pf("%s      // reset is explicit, not the prefill's: a repeated field id\n", ind)
		g.pf("%s      // must not leave an arm decoded by an earlier occurrence\n", ind)
		g.pf("%s      // standing (docs/SPEC-TABLES.md §4).\n", ind)
		g.pf("%s      value.%s.Type = %sType.None;\n", ind, name, un.Name)
		g.pf("%s      r.Report.Unknown++;\n%s      break;\n", ind, ind)
		g.pf("%s  }\n", ind)
		g.pf("%s  r.Limit = outerLimit;\n", ind)
		g.pf("%s  r.Offset = bodyEnd; // the parent advances by the LENGTH, whatever the child read\n", ind)
		g.pf("%s}\n", ind)
	case kind == tkTable:
		g.needTable(f.Type.Name, f.Type.Name+"LoadBody")
		g.pf("%sif (!r.has(4)) { r.Report.Malformed = true; return false; }\n", ind)
		g.pf("%sconst bodyLen = r.get32();\n", ind)
		g.pf("%sif (!r.has(bodyLen)) { r.Report.Malformed = true; return false; }\n", ind)
		g.pf("%s{\n%s  const bodyEnd = r.Offset + bodyLen;\n", ind, ind)
		g.pf("%s  const outerLimit = r.Limit;\n", ind)
		g.pf("%s  r.Limit = bodyEnd;\n", ind)
		g.pf("%s  %sLoadBody(r, value.%s);\n", ind, f.Type.Name, name)
		g.pf("%s  r.Limit = outerLimit;\n", ind)
		g.pf("%s  r.Offset = bodyEnd; // the parent advances by the LENGTH, whatever the child read\n", ind)
		g.pf("%s}\n", ind)
	default:
		g.emitTableReadScalarFrom(f, kind, "value."+name, ind,
			"r.Report.Malformed = true; return false;")
	}
}

// emitTableReadElement decodes one array element from inside the field body;
// truncation keeps the decoded prefix and flags malformed without stopping the
// parent decode.
func (g *tableGen) emitTableReadElement(f *ir.Field, kind int, ind string) {
	name := member(f)
	switch kind {
	case tkTable:
		g.needTable(f.Type.Name, f.Type.Name+"LoadBody")
		g.pf("%sif (!r.has(4)) { r.Report.Malformed = true; break; }\n", ind)
		g.pf("%sconst elemLen = r.get32();\n", ind)
		g.pf("%sif (!r.has(elemLen)) { r.Report.Malformed = true; break; }\n", ind)
		g.pf("%s{\n%s  const elemEnd = r.Offset + elemLen;\n", ind, ind)
		g.pf("%s  const elemLimit = r.Limit;\n", ind)
		g.pf("%s  r.Limit = elemEnd;\n", ind)
		g.pf("%s  %sLoadBody(r, value.%s[i]);\n", ind, f.Type.Name, name)
		g.pf("%s  r.Limit = elemLimit;\n", ind)
		g.pf("%s  r.Offset = elemEnd;\n", ind)
		g.pf("%s}\n", ind)
	default:
		g.emitTableReadScalarFrom(f, kind, fmt.Sprintf("value.%s[i]", name), ind,
			"r.Report.Malformed = true; break;")
	}
}

// jsReadCall is the reader call for one fixed-width wire kind, at the wire's
// own width and signedness — the twin of C++'s intN_t/uintN_t locals, so the
// clamp comparisons happen at the wire's width before they land in storage.
func jsReadCall(kind int) string {
	switch kind {
	case tkI8:
		return "r.getI8()"
	case tkI16:
		return "r.getI16()"
	case tkI32:
		return "r.getI32()"
	case tkI64:
		return "r.getI64()"
	case tkU8:
		return "r.get8()"
	case tkU16:
		return "r.get16()"
	case tkU32:
		return "r.get32()"
	case tkU64:
		return "r.getU64()"
	}
	return "r.get32()"
}

// emitTableReadScalarFrom decodes one fixed-width scalar into a storage member,
// with the range clamps the schema declares. onTrunc is the truncation action:
// a scalar FIELD stops the decode (outer framing damage), an array ELEMENT
// keeps the prefix and breaks.
func (g *tableGen) emitTableReadScalarFrom(f *ir.Field, kind int, lvalue, ind, onTrunc string) {
	width := tableKindWidth(kind)
	g.pf("%sif (!r.has(%d)) { %s }\n", ind, width, onTrunc)
	if enum := enumRef(f); enum != nil {
		// identity is the variant's NAME (docs/SPEC-TABLES.md §5): an id this
		// build cannot name reads as None and counts as unknown, exactly as an
		// unknown FIELD id does — same event, one counter
		g.needEnumIdentity(f.Type.Name)
		g.needDecl(f.Type.Name, f.Type.Name)
		g.pf("%s{\n%s  const decodedEnum = %s(r.get16());\n", ind, ind, enumValueFn(f.Type.Name))
		g.pf("%s  if (decodedEnum === undefined) {\n", ind)
		g.pf("%s    r.Report.Unknown++;\n%s    %s = %s.None;\n%s  } else {\n", ind, ind, lvalue, f.Type.Name, ind)
		g.pf("%s    %s = decodedEnum;\n%s  }\n%s}\n", ind, lvalue, ind, ind)
		return
	}
	switch kind {
	case tkBool:
		g.pf("%s%s = r.get8() !== 0;\n", ind, lvalue)
	case tkF32:
		if f.HasFloatRange {
			g.pf("%s{\n%s  let decodedF = r.View.getFloat32(r.Offset, true); r.Offset += 4;\n", ind, ind)
			g.pf("%s  if (decodedF < %s) { decodedF = %s; r.Report.Clamped++; }\n", ind, formatFloat32(f.FMin), formatFloat32(f.FMin))
			g.pf("%s  else if (decodedF > %s) { decodedF = %s; r.Report.Clamped++; }\n", ind, formatFloat32(f.FMax), formatFloat32(f.FMax))
			g.pf("%s  %s = decodedF;\n%s}\n", ind, lvalue, ind)
			return
		}
		// the read half of the same rule: a double RETURNED from a call is
		// boxed on the way out unless the callee is inlined
		g.pf("%s%s = r.View.getFloat32(r.Offset, true); r.Offset += 4;\n", ind, lvalue)
	case tkF64:
		g.pf("%s%s = r.View.getFloat64(r.Offset, true); r.Offset += 8;\n", ind, lvalue)
	default:
		g.pf("%s{\n%s  let decodedV = %s;\n", ind, ind, jsReadCall(kind))
		if f.HasIntRange {
			low, high := tableClampEnds(f, width)
			if low {
				lo := jsIntLit(f.IntMin, width)
				g.pf("%s  if (decodedV < %s) { decodedV = %s; r.Report.Clamped++; }\n", ind, lo, lo)
			}
			if high {
				hi := jsIntLit(f.IntMax, width)
				lead := "if"
				if low {
					lead = "else if"
				}
				g.pf("%s  %s (decodedV > %s) { decodedV = %s; r.Report.Clamped++; }\n", ind, lead, hi, hi)
			}
		}
		if f.Type.Kind == ir.TBits && f.Type.Width < width*8 {
			maxv := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
			lit := jsIntLit(maxv, width)
			g.pf("%s  if (decodedV > %s) { decodedV = %s; r.Report.Clamped++; } // bits(%d) width clamp\n",
				ind, lit, lit, f.Type.Width)
		}
		g.pf("%s  %s = decodedV;\n%s}\n", ind, lvalue, ind)
	}
}

// ---- reflection descriptors ----

// tableFieldTypeName renders a field's schema-facing type name for the
// descriptor ("float32", "bits(9)", "Grade", "GunnerSettings").
func tableFieldTypeName(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		return "bool"
	case ir.TInt:
		prefix := "int"
		if !f.Type.Signed {
			prefix = "uint"
		}
		return fmt.Sprintf("%s%d", prefix, f.Type.Width)
	case ir.TBits:
		return fmt.Sprintf("bits(%d)", f.Type.Width)
	case ir.TFloat32:
		return "float32"
	case ir.TFloat64:
		return "float64"
	case ir.TString:
		return "string"
	case ir.TBytes:
		return "bytes"
	case ir.TNamed:
		return f.Type.Name
	}
	return "?"
}

// unionArmLambda renders a descriptor function over a union's tag values: 0 is
// the empty arm, [1, N] the declared arms in tag order.
func unionArmLambda(un *ir.Union, arm func(ir.UnionVariant) string, unknown, none string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "(v) => { switch (v) { case 0: return %s;", none)
	for i, v := range un.Variants {
		fmt.Fprintf(&b, " case %d: return %s;", i+1, arm(v))
	}
	fmt.Fprintf(&b, " default: return %s; } }", unknown)
	return b.String()
}

func bigToDouble(v *big.Int) string {
	f, _ := new(big.Float).SetInt(v).Float64()
	return formatFloat64(f)
}

// emitTableDescriptor emits <X>TableType() — the reflection descriptor, built
// once on first use and cached. Descriptors are PLAIN FROZEN OBJECTS: C++
// carries offsets and widths because its storage is one flat struct, and C#
// carries delegates because a C# field has no offsetof; a JavaScript object
// has neither an offset nor a type to name, so the descriptor carries the
// accessor closures the emitter wrote and freezes the record around them.
func (g *tableGen) emitTableDescriptor(st *ir.Struct) {
	guards := tableGuardStrings(st)
	g.pf("let %sTableInfo = null;\n", st.Name)
	g.pf("export function %sTableType() {\n", st.Name)
	g.pf("  if (%sTableInfo !== null) { return %sTableInfo; }\n", st.Name, st.Name)
	g.pf("  const info = {\n")
	g.pf("    Name: %q,\n", st.Name)
	g.pf("    NumFields: %d,\n", len(st.Fields))
	if len(st.Fields) == 0 {
		g.pf("    Fields: [],\n")
	} else {
		g.pf("    Fields: [\n")
		for _, f := range st.Fields {
			g.emitTableFieldDescriptor(f, guards[f.Name])
		}
		g.pf("    ],\n")
	}
	// the RESET hook (docs/SPEC-TABLES.md §8.1): the one column the descriptors
	// cannot express without a function — a generic walker that FILLS a value
	// establishes an absent field's defaults through it, holding no type to
	// spell. It is <Name>Reset, the prefill the wire's read path already calls.
	g.pf("    Reset: %sReset,\n", st.Name)
	// what a PERSON wrote about the declaration (docs/SPEC-TABLES.md §8.1): the
	// /// block above it, verbatim (SPEC §4.1). It is TableDocNone when there
	// is none, never undefined. Its tags (SPEC §4.2) follow in declared order,
	// and an untagged declaration is 0 beside null. Constant data, built once with the cached
	// descriptor and frozen with it.
	g.pf("    %s,\n", g.annotationColumns(st.Doc, st.Tags))
	g.pf("  };\n")
	g.pf("  Object.freeze(info.Fields);\n")
	g.pf("  %sTableInfo = Object.freeze(info);\n", st.Name)
	g.pf("  return %sTableInfo;\n}\n\n", st.Name)
}

// ---- the storage columns: JavaScript's spelling of C++'s offset and elem_size ----

// storageExpr is the JavaScript expression for a field's storage member on an
// instance reached as `o`. A keyed array's slots live behind .Slots on a table
// and are a plain array on a closure `type` (§2.4), and keyedSlots already
// knows which.
func (g *tableGen) storageExpr(f *ir.Field) string { return g.keyedSlots("o.", f) }

// elementExpr is storageExpr indexed where the field is an array, and
// storageExpr itself where it is not — the walker passes 0 for a scalar.
func (g *tableGen) elementExpr(f *ir.Field) string {
	if f.Array != ir.ArrayNone {
		return g.storageExpr(f) + "[i]"
	}
	return g.storageExpr(f)
}

// jsRawGet renders one element as the 64-bit BIT PATTERN the descriptor's
// getRaw hands back, as a BigInt: an integer sign-extended, a bool as 0 or 1,
// an enum or a flags mask as its value, a float as its IEEE-754 bit pattern.
//
// It is the reference's `ulong` exactly, in the one JavaScript type that holds
// sixty-four bits without loss. That is why the generic path allocates: every
// BigInt is an object. The wire path never touches these — it reads and writes
// storage directly, in the Number domain wherever the field fits.
func jsRawGet(expr string, t ir.FieldType) string {
	switch t.Kind {
	case ir.TBool:
		return "(" + expr + " ? 1n : 0n)"
	case ir.TFloat32:
		return "BigInt(TableFloatToBits(" + expr + "))"
	case ir.TFloat64:
		return "TableDoubleToBits(" + expr + ")"
	case ir.TInt:
		if t.Width > 32 {
			return "BigInt.asUintN(64, " + expr + ")"
		}
		if t.Signed {
			// sign-extended into sixty-four bits, exactly as C#'s (ulong)(long)
			return "BigInt.asUintN(64, BigInt(Math.trunc(" + expr + ")))"
		}
		return "BigInt(Math.trunc(" + expr + ") >>> 0)"
	case ir.TBits:
		if t.Width > 32 {
			return "BigInt.asUintN(64, " + expr + ")"
		}
		return "BigInt(Math.trunc(" + expr + ") >>> 0)"
	case ir.TNamed:
		if _, isFlags := t.Ref.(*ir.Flags); isFlags {
			return "BigInt.asUintN(64, " + expr + ")"
		}
		// an enum value: a small non-negative Number
		return "BigInt(Math.trunc(" + expr + ") >>> 0)"
	}
	return "0n"
}

// jsRawSet is its inverse. The narrowing is explicit because the walker has
// already clamped to the field's declared range and to its storage width
// (§16.2), so a value reaching here fits and the mask only restores the
// storage's own domain.
func jsRawSet(expr, src string, t ir.FieldType) string {
	switch t.Kind {
	case ir.TBool:
		return expr + " = " + src + " !== 0n;"
	case ir.TFloat32:
		return expr + " = TableBitsToFloat(Number(BigInt.asUintN(32, " + src + ")));"
	case ir.TFloat64:
		return expr + " = TableBitsToDouble(" + src + ");"
	case ir.TInt:
		if t.Width > 32 {
			if t.Signed {
				return expr + " = BigInt.asIntN(64, " + src + ");"
			}
			return expr + " = BigInt.asUintN(64, " + src + ");"
		}
		if t.Signed {
			return expr + fmt.Sprintf(" = Number(BigInt.asIntN(%d, %s));", t.Width, src)
		}
		return expr + fmt.Sprintf(" = Number(BigInt.asUintN(%d, %s));", t.Width, src)
	case ir.TBits:
		if t.Width > 32 {
			return expr + " = BigInt.asUintN(64, " + src + ");"
		}
		return expr + " = Number(BigInt.asUintN(32, " + src + "));"
	case ir.TNamed:
		if _, isFlags := t.Ref.(*ir.Flags); isFlags {
			return expr + " = BigInt.asUintN(64, " + src + ");"
		}
		return expr + " = Number(BigInt.asUintN(32, " + src + "));"
	}
	return ""
}

// jsElemWidth is the STORAGE width of one element in bytes — C++'s elem_size
// where it has a JavaScript meaning: the last bound a numeric read clamps to
// (§16.2). 0 on every kind whose storage is not a fixed-width number.
func jsElemWidth(t ir.FieldType) int {
	switch t.Kind {
	case ir.TBool:
		return 1
	case ir.TFloat32:
		return 4
	case ir.TFloat64:
		return 8
	case ir.TInt:
		return t.Width / 8
	case ir.TBits:
		if t.Width <= 32 {
			return 4
		}
		return 8
	}
	return 0
}

// tableStorageColumns renders a field's storage columns: the accessor pairs a
// generic walker reaches the value through. Exactly one of getRaw/getChild/
// getBuffer is non-null, and the companions follow the counted and optional
// columns beside them.
func (g *tableGen) tableStorageColumns(f *ir.Field) string {
	var b strings.Builder
	name := member(f)
	switch {
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		fmt.Fprintf(&b, ", GetBuffer: (o) => %s", g.storageExpr(f))
		fmt.Fprintf(&b, ", GetCount: (o) => o.%sLength", name)
		fmt.Fprintf(&b, ", SetCount: (o, n) => { o.%sLength = n; }", name)
	case isClassRef(f.Type):
		fmt.Fprintf(&b, ", GetChild: (o, i) => %s", g.elementExpr(f))
	default:
		g.needRuntime(rawHelpers(f.Type)...)
		fmt.Fprintf(&b, ", GetRaw: (o, i) => %s", jsRawGet(g.elementExpr(f), f.Type))
		fmt.Fprintf(&b, ", SetRaw: (o, i, r) => { %s }", jsRawSet(g.elementExpr(f), "r", f.Type))
	}
	if f.Array == ir.ArrayCounted {
		fmt.Fprintf(&b, ", GetCount: (o) => o.%sCount", name)
		fmt.Fprintf(&b, ", SetCount: (o, n) => { o.%sCount = n; }", name)
	}
	if f.Type.Optional {
		fmt.Fprintf(&b, ", GetPresent: (o) => o.%sPresent", name)
		fmt.Fprintf(&b, ", SetPresent: (o, p) => { o.%sPresent = p; }", name)
	}
	return b.String()
}

// rawHelpers names the bit helpers one field's raw accessors reference, so the
// module imports exactly what it uses.
func rawHelpers(t ir.FieldType) []string {
	switch t.Kind {
	case ir.TFloat32:
		return []string{"TableFloatToBits", "TableBitsToFloat"}
	case ir.TFloat64:
		return []string{"TableDoubleToBits", "TableBitsToDouble"}
	}
	return nil
}

// unionArmsValue renders a union field's arms column: the tag's accessor pair
// and one entry per arm, index 0 being the EMPTY arm, which carries neither
// payload nor descriptor.
func (g *tableGen) unionArmsValue(un *ir.Union) string {
	var b strings.Builder
	g.needDecl(un.Name, un.Name+"Type")
	fmt.Fprintf(&b, "{ GetTag: (o) => o.Type")
	fmt.Fprintf(&b, ", SetTag: (o, t) => { o.Type = Number(t); }")
	b.WriteString(", Arms: [{ TableRef: null, Payload: null }")
	for _, v := range un.Variants {
		g.needTable(v.Type, v.Type+"TableType")
		fmt.Fprintf(&b, ", { TableRef: () => %sTableType(), Payload: (o) => o.%s }",
			v.Type, ir.GoExportName(v.Name))
	}
	b.WriteString("] }")
	return b.String()
}

// annotationColumns renders a row's Doc, NumTags and Tags columns
// (docs/SPEC-TABLES.md §8.1): the shared empty doc and a null list where the
// item carries none, never a per-row empty literal of either.
//
// A tag list rides as a frozen array literal in the row, the way the doc
// itself rides as a string literal. C and C++ hoist theirs to a named static
// because neither language can write an array inside an aggregate
// initializer. JavaScript can, and the descriptor is built once and cached, so
// the literal is evaluated exactly once for the module's lifetime. Hoisting it
// here would put a name derived from a DECLARATION at module or function
// scope, and §11 claims no such spelling, so it would collide with the
// declaration's own binding.
func (g *tableGen) annotationColumns(doc string, tags []string) string {
	docColumn := "TableDocNone"
	if doc != "" {
		docColumn = ir.QuoteDoc(doc)
	} else {
		g.needRuntime("TableDocNone")
	}
	list := "null"
	if len(tags) > 0 {
		list = fmt.Sprintf("Object.freeze([%s])", ir.QuotedTags(tags))
	}
	return fmt.Sprintf("Doc: %s, NumTags: %d, Tags: %s", docColumn, len(tags), list)
}

func (g *tableGen) emitTableFieldDescriptor(f *ir.Field, guard string) {
	id := ir.TableFieldId(f)
	kind := tableScalarKind(f)
	if f.Type.Kind == ir.TBytes {
		kind = tkU8
	}
	isArray := f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes
	counted := f.Array == ir.ArrayCounted || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString

	// the count column, spelled the way the storage spells its own extent: a
	// keyed array DERIVES it from the key enum, so nothing outside the array
	// names its size (docs/SPEC-TABLES.md §2.4, §8.1)
	bound := "0"
	switch {
	case f.KeyEnum != "":
		g.needDecl(f.KeyEnum, f.KeyEnum)
		bound = f.KeyEnum + ".Max"
	case f.Array != ir.ArrayNone:
		bound = strconv.FormatInt(f.ArrayBound, 10)
	case f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TString:
		bound = strconv.FormatInt(f.Type.Size, 10)
	}

	tableRef := "null"
	if _, isStruct := f.Type.Ref.(*ir.Struct); f.Type.Kind == ir.TNamed && isStruct {
		g.needTable(f.Type.Name, f.Type.Name+"TableType")
		tableRef = fmt.Sprintf("() => %sTableType()", f.Type.Name)
	}

	// the KEY's vocabulary on an enum-keyed array (docs/SPEC-TABLES.md §8):
	// functions of the KEY, not of the storage index. keyId(0) is 0 and
	// keyName(0) is "None", the reserved id that says None keys no slot.
	keyTypeName, keyName, keyId := "null", "null", "null"
	if f.KeyEnum != "" {
		g.needEnumIdentity(f.KeyEnum)
		g.needDecl(f.KeyEnum, "EnumName"+f.KeyEnum)
		keyTypeName = fmt.Sprintf("%q", f.KeyEnum)
		keyName = fmt.Sprintf("(v) => EnumName%s(v)", f.KeyEnum)
		keyId = fmt.Sprintf("(v) => { const id = %s(v); return id === undefined ? 0 : id; }", enumIdFn(f.KeyEnum))
	}

	hasRange := "false"
	rangeMin, rangeMax := "0.0", "0.0"
	if f.Type.Kind == ir.TBits && !f.HasIntRange {
		// bits(N) declares its range by its WIDTH: [0, 2^N - 1]. The codec has
		// always clamped a read to it (docs/SPEC-TABLES.md §4); carrying it here
		// is what lets a generic walker apply the same bound without
		// re-deriving it from the type name (§8.1).
		max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
		hasRange = "true"
		rangeMin, rangeMax = "0.0", bigToDouble(max)
	}
	if f.HasIntRange {
		hasRange = "true"
		rangeMin, rangeMax = bigToDouble(f.IntMin), bigToDouble(f.IntMax)
	} else if f.HasFloatRange {
		hasRange = "true"
		rangeMin, rangeMax = formatFloat64(f.FMin), formatFloat64(f.FMax)
	}

	// the VOCABULARY columns: an enum's values, a union's arms and a flags
	// field's BITS are each a named set indexed by [0, enumMax]. An enum's and
	// a union's names carry the table-wire id they ride under; a flags variant
	// has none, and that missing id is what tells the two apart at runtime
	// (docs/SPEC-TABLES.md §4, §5, §8).
	enumMax := "-1"
	enumName := "null"
	variantId := "null"
	arms := "null"
	switch ref := f.Type.Ref.(type) {
	case *ir.Enum:
		if f.Type.Kind == ir.TNamed {
			g.needEnumIdentity(f.Type.Name)
			g.needDecl(f.Type.Name, "EnumName"+f.Type.Name)
			enumMax = fmt.Sprintf("%d", ref.Max)
			enumName = fmt.Sprintf("(v) => EnumName%s(v)", f.Type.Name)
			variantId = fmt.Sprintf("(v) => { const id = %s(v); return id === undefined ? 0 : id; }", enumIdFn(f.Type.Name))
		}
	case *ir.Flags:
		if f.Type.Kind == ir.TNamed {
			// a flags mask is the wire's one POSITIONAL vocabulary
			// (docs/SPEC-TABLES.md §4): its variants are BIT POSITIONS, so the
			// descriptor names bits, and there is no variant id.
			g.needDecl(f.Type.Name, "FlagName"+f.Type.Name)
			enumMax = fmt.Sprintf("%d", len(ref.Variants)-1)
			enumName = fmt.Sprintf("(v) => FlagName%s(v)", f.Type.Name)
		}
	case *ir.Union:
		if f.Type.Kind == ir.TNamed && f.Array == ir.ArrayNone {
			enumMax = fmt.Sprintf("%d", len(ref.Variants))
			enumName = unionArmLambda(ref, func(v ir.UnionVariant) string {
				return fmt.Sprintf("%q", v.Name)
			}, "\"???\"", "\"None\"")
			variantId = unionArmLambda(ref, func(v ir.UnionVariant) string {
				return fmt.Sprintf("0x%04x", ir.VariantId(v.Name))
			}, "0", "0")
			arms = g.unionArmsValue(ref)
		}
	}

	// what a PERSON wrote about the field (docs/SPEC-TABLES.md §8.1): the ///
	// block above it, verbatim (SPEC §4.1). It is TableDocNone when there is
	// none, never undefined. Its tags (SPEC §4.2) follow in declared order, and
	// an untagged field is 0 beside null. Constant data, built once with the cached descriptor
	// and frozen with it.
	g.pf("      { Name: %q, Json: %q, TypeName: %q, Id: 0x%04x, Kind: %d, IsArray: %v, Counted: %v, Optional: %v, ArrayBound: %s, ElemWidth: %d, HasRange: %s, RangeMin: %s, RangeMax: %s, EnumMax: %s, EnumName: %s, VariantId: %s, KeyTypeName: %s, KeyName: %s, KeyId: %s, Guard: %q, TableRef: %s, Arms: %s%s, %s },\n",
		f.Name, ir.TableFieldJsonKey(f), tableFieldTypeName(f), id, kind, isArray, counted, f.Type.Optional, bound,
		jsElemWidth(f.Type), hasRange, rangeMin, rangeMax, enumMax, enumName, variantId,
		keyTypeName, keyName, keyId, guard, tableRef, arms, g.tableStorageColumns(f),
		g.annotationColumns(f.Doc, f.Tags))
}
