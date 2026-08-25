// Package js emits the JavaScript target: one .js ES module per schema file —
// Constants.schema -> Constants.js — deterministic to the byte (SPEC §6.1).
// There is no external formatter in the build path: the emitter writes clean
// 2-space-indented JavaScript directly (the runtime's own style; goldens pin
// it), and the parse refuser is node importing every generated module in the
// test chain.
//
// serialize.js is the runtime (mas-bandwidth/serialize.js): ESM, zero
// dependencies, WriteStream/ReadStream/MeasureStream with bool-returning
// serialize methods and sticky latched errors, checked/production selected
// once at module load on NODE_ENV. Generated code inherits both modes from
// the stream object it is handed and never reads NODE_ENV itself. Generated
// modules import nothing from the runtime — every wire call is a method on
// the stream parameter — so the only imports are cross-file references
// within the unit, derived per file.
//
// Storage is the §6.1 storage principle in JavaScript's value domain:
// classes with PUBLIC FIELDS, every field initialized in the constructor in
// declaration order (hidden-class stability), member names via
// ir.GoExportName — the SAME PascalCase mapping as the Go and C# targets, so
// the checker's existing collision detection covers JS without a second
// registry. The value-domain rule that governs everything: Number for
// storage widths of 32 bits or fewer, BigInt for 64 and 128 — the runtime's
// own seam (streams.js: serializeBits takes a Number, serializeBits64 a
// BigInt). Flags-typed fields store uint64 in every target (SPEC §6.1), so
// they are BigInt here even at narrow wire widths, with flat mask constants
// in the Go spelling exactly.
//
// JavaScript has no ref parameters, so serialize methods take {value} holder
// objects. Generated functions reuse three module-scoped scratch holders —
// one Number, one BigInt, one boolean — which is safe for the same reason
// the runtime shares FLOAT_SCRATCH: JavaScript is single threaded per realm
// and a holder is always consumed in the same call that fills it; generated
// code never holds one across a nested Write/Read call.
//
// Functions follow the §6.3 checked-runtime row: bool-returning Write*/Read*
// with the C++-style early-out on every call. A schema validation failure (a
// wrong wire constant, nonzero reserved bits, an out-of-contract write)
// returns false WITHOUT latching; stream failures latch on the runtime's
// sticky stream.error. The write side carries the generated fold-guard set
// of the Go/C# targets — the guards ride in BOTH runtime modes, keeping
// write-refusal behavior mode-independent for folded paths. Where JS storage
// is untyped where the siblings' is typed, the Number-domain guards also
// refuse non-integers (Number.isInteger), the fold's translation of the
// runtime's own checked-write refusal; BigInt-domain paths reinterpret
// through BigInt.asIntN/asUintN exactly where C# storage types would wrap.
package js

import (
	"fmt"
	"maps"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/internal/ast"
	"github.com/mas-bandwidth/schema/ir"
)

// Generate returns basename.js -> file contents for every file of the unit.
// ES modules are order-free for functions and classes used at call time, but
// top-level const initializers evaluate top to bottom, so declarations emit
// in ir.EmissionOrder per file (the C/C++ discipline — a symbolic const
// initializer referencing a later const would hit the temporal dead zone).
func Generate(u *ir.Unit) (map[string][]byte, error) {
	out := map[string][]byte{}
	home := protocolIdHome(u)
	msgOwner := ir.MessageOwner(u)
	objOwner := ir.ObjectOwner(u)
	bases := map[string]bool{}
	for _, f := range u.Files {
		bases[f.Base] = true
	}
	for _, f := range u.Files {
		if bases[f.Base+"Table"] {
			return nil, fmt.Errorf("schema files %s and %sTable collide — the JS emitter writes %sTable.js as %s's table codec; rename one file", f.Base, f.Base, f.Base, f.Base)
		}
		if bases[f.Base+"Flat"] {
			return nil, fmt.Errorf("schema files %s and %sFlat collide — the JS emitter writes %sFlat.js as %s's flat-tier codec; rename one file", f.Base, f.Base, f.Base, f.Base)
		}
		if f.Base == "serialize" {
			// the build wiring drops a serialize.js runtime re-export shim
			// beside the generated modules (the Makefile's twin of the go.mod
			// replace); a schema file of that basename would collide with it
			return nil, fmt.Errorf("schema file serialize.schema collides with the serialize.js runtime shim the build wiring writes beside the generated modules; rename the file")
		}
	}
	for _, f := range u.Files {
		g := &gen{unit: u, file: f, msgOwner: msgOwner, objOwner: objOwner, imports: map[string]map[string]bool{}}
		g.emitFile(f.Base == home)
		out[f.Base+".js"] = g.assemble()
	}
	// the flat tier: BaseFlat.js beside Base.js wherever the file carries a
	// wire surface (flat.go) — always emitted, never flag-gated: the ruling
	// makes flat THE JavaScript path, and the runtime tier above is emitted
	// regardless because it is the flat tier's CI oracle
	maps.Copy(out, generateFlat(u, msgOwner))
	return out, nil
}

// protocolIdHome picks the file that carries ProtocolId: the constants aspect
// file if the unit has one, else the first — the same rule as the other
// targets.
func protocolIdHome(u *ir.Unit) string {
	for _, f := range u.Files {
		if f.Base == "Constants" {
			return f.Base
		}
	}
	if len(u.Files) > 0 {
		return u.Files[0].Base
	}
	return ""
}

type gen struct {
	unit     *ir.Unit
	file     *ir.File
	msgOwner string // the one file that carries the message dispatch surface
	objOwner string // the one file that carries the object tag surface

	body strings.Builder

	// imports collects cross-file references as file base -> exported symbol
	// set; ESM is file-scoped, so unlike Go/C# nothing is ambient and every
	// cross-file name must be imported (derived per reference, the same edges
	// ir.FileDeps tracks).
	imports map[string]map[string]bool

	// bulkBytes marks the statically byte-aligned [N]uint8 fields of the
	// struct whose functions are being emitted (ir.AlignedFixedByteArrays) —
	// the serializeBytes bulk path; nil inside view functions, whose field
	// lists embed at unknown alignment (a per-struct proof never transfers).
	bulkBytes map[*ir.Field]bool

	needNumScratch  bool
	needBigScratch  bool
	needBoolScratch bool
}

func (g *gen) pf(format string, args ...any) {
	fmt.Fprintf(&g.body, format, args...)
}

// addRef records that the current file references the declaration decl (by
// the exported symbols listed), importing it when it lives in another file.
func (g *gen) addRef(decl string, symbols ...string) {
	base, ok := g.unit.DeclFile[decl]
	if !ok {
		return
	}
	g.addRefBase(base, symbols...)
}

// addRefBase imports symbols from a known file base — for the synthesized
// unit-level surfaces (MessageType/ObjectType live in their owner file, not
// in DeclFile).
func (g *gen) addRefBase(base string, symbols ...string) {
	if base == g.file.Base {
		return
	}
	set := g.imports[base]
	if set == nil {
		set = map[string]bool{}
		g.imports[base] = set
	}
	for _, s := range symbols {
		set[s] = true
	}
}

func (g *gen) assemble() []byte {
	var h strings.Builder
	// the basename, not the invocation-relative path: output is deterministic
	// to the byte wherever the compiler runs (SPEC §6.1)
	fmt.Fprintf(&h, "// Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", g.file.Base)
	fmt.Fprintf(&h, "// package %s — protocol id 0x%016x\n", g.unit.Package, g.unit.ProtocolId)
	h.WriteString("//\n")
	h.WriteString("// Storage members are PascalCase via the same mapping as the Go target, so\n")
	h.WriteString("// the checker's collision registry covers JS for free. Wire functions return\n")
	h.WriteString("// bool — the C++-style early-out. A schema validation failure (a wrong wire\n")
	h.WriteString("// constant, nonzero reserved bits, an out-of-contract write) returns false\n")
	h.WriteString("// WITHOUT latching; stream failures latch on stream.error — the runtime's own\n")
	h.WriteString("// sticky latch. Callers get bool always; error tells the two apart.\n")
	h.WriteString("//\n")
	h.WriteString("// Number storage for widths of 32 bits or fewer, BigInt for 64 and 128 —\n")
	h.WriteString("// the serialize.js value-domain seam. Checked/production comes from the\n")
	h.WriteString("// stream object; generated code never reads NODE_ENV.\n\n")
	if len(g.imports) > 0 {
		bases := make([]string, 0, len(g.imports))
		for b := range g.imports {
			bases = append(bases, b)
		}
		sort.Strings(bases)
		for _, b := range bases {
			syms := make([]string, 0, len(g.imports[b]))
			for s := range g.imports[b] {
				syms = append(syms, s)
			}
			sort.Strings(syms)
			fmt.Fprintf(&h, "import { %s } from \"./%s.js\";\n", strings.Join(syms, ", "), b)
		}
		h.WriteString("\n")
	}
	if g.needNumScratch || g.needBigScratch || g.needBoolScratch {
		h.WriteString("// Scratch holders for the runtime's {value} refs (streams.js has no ref\n")
		h.WriteString("// parameters). Module scope is safe — JavaScript is single threaded per\n")
		h.WriteString("// realm and a holder is consumed in the same call that fills it, the\n")
		h.WriteString("// runtime's own FLOAT_SCRATCH argument; generated code never holds one\n")
		h.WriteString("// across a nested Write/Read call.\n")
		if g.needNumScratch {
			h.WriteString("const NUMBER_SCRATCH = { value: 0 };\n")
		}
		if g.needBigScratch {
			h.WriteString("const BIGINT_SCRATCH = { value: 0n };\n")
		}
		if g.needBoolScratch {
			h.WriteString("const BOOL_SCRATCH = { value: false };\n")
		}
		h.WriteString("\n")
	}
	h.WriteString(g.body.String())
	return []byte(h.String())
}

func (g *gen) emitFile(carriesProtocolId bool) {
	if carriesProtocolId {
		g.pf("// The unit's protocol id — the hash of its schema files (SPEC §3.1). Two\n")
		g.pf("// sides at the same id speak identical bits; there is no other versioning.\n")
		g.pf("export const ProtocolId = 0x%016xn;\n\n", g.unit.ProtocolId)
	}

	// MessageType / ObjectType lead their OWNER file — the unit-level surface
	// is emitted exactly once, in the topologically last carrying file, so
	// declarations spread across files never redeclare it (SPEC §2 keeps the
	// aspect layout non-enforced; ir.MessageOwner picks).
	if g.file.Base == g.msgOwner && len(g.unit.Messages) > 0 {
		g.emitTagEnum("MessageType", g.unit.Messages,
			"the message set, extracted by the compiler — None = 0, then each message sorted by name (SPEC §4.8)")
	}
	if g.file.Base == g.objOwner && len(g.unit.ObjNames) > 0 {
		g.emitTagEnum("ObjectType", g.unit.ObjNames,
			"the object set, extracted by the compiler — None = 0, then each object sorted by name (SPEC §4.8)")
	}

	// EmissionOrder, not declaration order: top-level const initializers
	// evaluate top to bottom in an ES module, so a symbolic reference to a
	// later const would be a temporal-dead-zone error
	for _, d := range ir.EmissionOrder(g.file) {
		switch d := d.(type) {
		case *ir.Const:
			g.emitConst(d)
		case *ir.Enum:
			g.emitEnum(d)
		case *ir.Flags:
			g.emitFlags(d)
		case *ir.ContextsMarker:
			g.pf("// contexts declared for this unit: %s (SPEC §4.2).\n", strings.Join(d.Names, ", "))
			g.pf("// Contexts generate no standalone artifacts — where an object carries\n")
			g.pf("// context-scoped [local] fields, its State class is generated once per\n")
			g.pf("// context (ClientShipState, ServerShipState, ...), each holding the `all`\n")
			g.pf("// fields plus its own context's.\n\n")
		case *ir.Struct:
			g.emitClass(d)
			g.emitStructFunctions(d)
		case *ir.Union:
			g.emitUnion(d)
			g.emitUnionFunctions(d)
		case *ir.Object:
			g.emitObject(d)
			g.emitObjectFunctions(d)
		}
	}

	// the dispatch surfaces close their owner files, after every class they
	// reference — the C# target's layout exactly
	if g.file.Base == g.msgOwner && len(g.unit.Messages) > 0 {
		g.emitMessageStorage()
		g.emitMessageTagFunctions()
	}
	if g.file.Base == g.objOwner && len(g.unit.ObjNames) > 0 {
		g.emitObjectTagFunctions()
	}
}

// emitTagEnum emits a tag enum as a frozen plain object — the JS translation
// of the family's integer-backed enums; values are Numbers.
func (g *gen) emitTagEnum(name string, members []string, comment string) {
	g.pf("// %s: %s\n", name, comment)
	g.pf("export const %s = Object.freeze({\n", name)
	g.pf("  None: 0,\n")
	for i, m := range members {
		g.pf("  %s: %d,\n", m, i+1)
	}
	g.pf("  Max: %d, // the exported extent (SPEC §4.2)\n", len(members))
	g.pf("});\n\n")
}

// emitConst emits a schema const. Bare integers and explicit types through
// uint32 are Numbers; explicit int64/uint64 (and any bare value past the
// 2^53 - 1 safe-integer bound) are BigInt — the value-domain seam applied to
// constants, since JavaScript has no 64-bit Number integers.
func (g *gen) emitConst(d *ir.Const) {
	if d.IsFloat {
		if d.Storage == "float32" {
			g.pf("export const %s = %s;%s\n\n", d.Name, formatFloat32(d.Float), g.foldComment(d.Expr))
			return
		}
		g.pf("export const %s = %s;%s\n\n", d.Name, formatFloat(d.Float), g.foldComment(d.Expr))
		return
	}
	if constIsBig(d) {
		g.pf("export const %s = %s;%s\n\n", d.Name, bigLit(d.Int), g.foldComment(d.Expr))
		return
	}
	g.pf("export const %s = %s;%s\n\n", d.Name, g.renderNum(d.Expr, d.Int), g.foldComment(d.Expr))
}

// constIsBig reports whether a schema integer const stores as BigInt in JS:
// explicitly 64-bit typed, or a value past Number's safe-integer bound.
func constIsBig(d *ir.Const) bool {
	if d.Explicit && (d.Storage == "int64" || d.Storage == "uint64") {
		return true
	}
	return d.Int != nil && !fitsSafeInteger(d.Int)
}

var maxSafeInteger = big.NewInt(1<<53 - 1)
var minSafeInteger = big.NewInt(-(1<<53 - 1))

func fitsSafeInteger(v *big.Int) bool {
	return v.Cmp(minSafeInteger) >= 0 && v.Cmp(maxSafeInteger) <= 0
}

// foldComment returns a trailing comment carrying the schema expression when
// the rendered JS had to fold it (an E.Max reference has no JS twin).
func (g *gen) foldComment(e ast.Expr) string {
	if e != nil && ir.ExprHasEnumMax(e) {
		return fmt.Sprintf(" // = %s", ir.RenderExpr(e))
	}
	return ""
}

func (g *gen) emitEnum(d *ir.Enum) {
	g.pf("// %s — None = 0 implicit, variants dense from 1, wire range [0, %d] (SPEC §4.2);\n", d.Name, d.Max)
	g.pf("// a frozen object of Number values — the JS translation of the family's\n")
	g.pf("// integer-backed enums; [max = ...] headroom values are plain Numbers\n")
	g.pf("export const %s = Object.freeze({\n", d.Name)
	g.pf("  None: 0,\n")
	for i, v := range d.Variants {
		g.pf("  %s: %d,\n", v, i+1)
	}
	g.pf("  Max: %d, // the exported extent (SPEC §4.2)\n", d.Max)
	g.pf("});\n\n")
	g.pf("// EnumName%s: debug/log/tooling name for any %s wire value —\n", d.Name, d.Name)
	g.pf("// out-of-set values (wire-legal up to the declared max) name as \"???\"\n")
	g.pf("export function EnumName%s(value) {\n", d.Name)
	g.pf("  switch (value) {\n")
	g.pf("    case %s.None:\n      return \"None\";\n", d.Name)
	for _, v := range d.Variants {
		g.pf("    case %s.%s:\n      return %q;\n", d.Name, v, v)
	}
	g.pf("    default:\n      return \"???\";\n  }\n}\n\n")
}

func (g *gen) emitFlags(d *ir.Flags) {
	g.pf("// %s — one bit per variant, consumed as masks; flags-typed fields store\n", d.Name)
	g.pf("// uint64 in every target, so BigInt here, wire %d bits (SPEC §4.2). Masks\n", d.WireBits)
	g.pf("// are flat PascalCase — the Go target's spelling exactly, so the checker's\n")
	g.pf("// existing claims cover them.\n")
	for i, v := range d.Variants {
		g.pf("export const %s%s = 1n << %dn;\n", d.Name, v, i)
	}
	g.pf("export const %sCount = %d; // the declared variant count (SPEC §4.2)\n", d.Name, len(d.Variants))
	g.pf("\n")
}

func (g *gen) emitClass(d *ir.Struct) {
	kind := "type"
	if d.IsMessage {
		kind = "message"
	}
	if d.IsTable {
		kind = "table"
	}
	if len(d.Tags) > 0 {
		g.pf("// %s %s [%s] — the tag is user-chosen and inert in v1; the delta pass\n", kind, d.Name, strings.Join(d.Tags, ", "))
		g.pf("// claims tags and assigns actions (SPEC §4.2, Type tags)\n")
	} else {
		g.pf("// %s %s\n", kind, d.Name)
	}
	g.pf("export class %s {\n", d.Name)
	if d.IsMessage {
		g.addRefBase(g.msgOwner, "MessageType")
		g.pf("  // the message's tag — dispatch pins it per class, as the C# target's\n")
		g.pf("  // Type property does\n")
		g.pf("  get Type() {\n    return MessageType.%s;\n  }\n\n", d.Name)
	}
	g.pf("  constructor() {\n")
	if len(d.Fields) == 0 {
		g.pf("    // empty body — presence is the payload (SPEC §4.6)\n")
	}
	g.emitClassFields(d.Fields)
	g.pf("  }\n}\n\n")
}

// The MessageType reference above needs the owner file's export; message
// classes outside the owner import it. ESM import cycles are legal here:
// the getter runs at call time, after every module in the cycle evaluated.

func (g *gen) emitClassFields(fields []*ir.Field) {
	prevGuard := ""
	for _, f := range fields {
		if f.Guard != prevGuard {
			if f.Guard != "" {
				g.pf("\n    // %s — wire branch; storage holds both sides, a read zeroes the\n", f.Guard)
				g.pf("    // untaken side (SPEC §5)\n")
			} else {
				g.pf("\n") // leaving a branch group — separate so membership stays visible
			}
			prevGuard = f.Guard
		}
		g.emitStorageField(f)
	}
}

func (g *gen) emitStorageField(f *ir.Field) {
	name := ir.GoExportName(f.Name)
	switch {
	case f.Type.Kind == ir.TString:
		g.pf("    this.%s = new Uint8Array(%s); // string(%s): max length, used length beside it (SPEC §4.7)\n",
			name, g.renderNum(f.Type.SizeExpr, big.NewInt(f.Type.Size)), ir.RenderExpr(f.Type.SizeExpr))
		g.pf("    this.%sLength = 0;\n", name)
	case f.Type.Kind == ir.TBytes:
		g.pf("    this.%s = new Uint8Array(%s); // bytes(%s): fixed buffer, used length beside it (SPEC §4.7)\n",
			name, g.renderNum(f.Type.SizeExpr, big.NewInt(f.Type.Size)), ir.RenderExpr(f.Type.SizeExpr))
		g.pf("    this.%sLength = 0;\n", name)
	case f.Array != ir.ArrayNone:
		bound := g.renderNum(f.ArrayExpr, big.NewInt(f.ArrayBound))
		switch {
		case f.Type.Kind == ir.TNamed && isClassRef(f.Type.Ref):
			// pre-allocated element instances — the storage principle
			// (SPEC §6.1): every buffer exists at construction; unions are
			// classes here exactly like structs
			g.addRef(f.Type.Name, f.Type.Name)
			g.pf("    this.%s = Array.from({ length: %s }, () => new %s());%s\n", name, bound, f.Type.Name, g.fieldComment(f))
		case isByteElem(f.Type):
			// uint8 arrays store as Uint8Array — the runtime's own byte-buffer
			// type (serializeBytes' contract), so the aligned bulk path can
			// hand the buffer over whole; element access is Number as in any
			// array, and the wire is identical either way
			g.pf("    this.%s = new Uint8Array(%s);%s\n", name, bound, g.fieldComment(f))
		default:
			g.pf("    this.%s = new Array(%s).fill(%s);%s\n", name, bound, g.zeroValue(f.Type), g.fieldComment(f))
		}
		if f.Array == ir.ArrayCounted {
			g.pf("    this.%sCount = 0;\n", name)
		}
	default:
		init := ""
		switch {
		case f.HasDefault:
			init = g.defaultValue(f)
		case f.Type.Kind == ir.TNamed && isClassRef(f.Type.Ref):
			// pre-allocated at construction — the storage principle (SPEC §6.1)
			g.addRef(f.Type.Name, f.Type.Name)
			init = "new " + f.Type.Name + "()"
		default:
			init = g.zeroValue(f.Type)
		}
		g.pf("    this.%s = %s;%s\n", name, init, g.fieldComment(f))
	}
}

// isClassRef reports a named reference whose JS storage is a pre-allocated
// class instance: a generated struct class or a union (SPEC §4.8).
func isClassRef(ref ir.Decl) bool {
	switch ref.(type) {
	case *ir.Struct, *ir.Union:
		return true
	}
	return false
}

// emitUnion emits a first-class one-of (SPEC §4.8): the <Name>Type tag
// object, then the class — the tag beside one pre-allocated arm per variant
// (the MessageStorage stand-in; nothing allocates per value after
// construction).
func (g *gen) emitUnion(d *ir.Union) {
	members := make([]string, len(d.Variants))
	for i, v := range d.Variants {
		members[i] = ir.GoExportName(v.Name)
	}
	g.emitTagEnum(d.Name+"Type", members,
		fmt.Sprintf("union %s's tag — None = 0, then each variant in declared order (SPEC §4.8)", d.Name))

	g.pf("// %s — at most one of the arms; Type says which. Construction is the empty\n", d.Name)
	g.pf("// union (None). A read zero-establishes exactly the selected arm before\n")
	g.pf("// decoding it (SPEC §5); unselected arms keep what they last held — the\n")
	g.pf("// MessageStorage reuse discipline. Consumers read the selected arm only.\n")
	g.pf("export class %s {\n", d.Name)
	g.pf("  constructor() {\n")
	g.pf("    this.Type = %sType.None;\n", d.Name)
	for _, v := range d.Variants {
		g.addRef(v.Type, v.Type)
		g.pf("    this.%s = new %s();\n", ir.GoExportName(v.Name), v.Type)
	}
	g.pf("  }\n}\n\n")
}

// zeroValue is the §5 zero form of a scalar storage slot.
func (g *gen) zeroValue(t ir.FieldType) string {
	switch t.Kind {
	case ir.TBool:
		return "false"
	case ir.TNamed:
		switch t.Ref.(type) {
		case *ir.Enum:
			g.addRef(t.Name, t.Name)
			return t.Name + ".None"
		case *ir.Flags:
			return "0n"
		}
		return "null" // unreachable: struct-typed slots pre-allocate instances
	}
	if isBigStorage(t) {
		return "0n"
	}
	return "0"
}

// isByteElem reports a uint8 element type — the arrays that store as
// Uint8Array (the runtime's byte-buffer type).
func isByteElem(t ir.FieldType) bool {
	return t.Kind == ir.TInt && !t.Signed && t.Width == 8
}

// isBigStorage reports whether a field type stores as BigInt (the seam:
// widths past 32 bits, and flags — uint64 storage in every target).
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

func (g *gen) fieldComment(f *ir.Field) string {
	var parts []string
	if f.Type.Kind == ir.TNamed {
		if _, isFlags := f.Type.Ref.(*ir.Flags); isFlags {
			parts = append(parts, fmt.Sprintf("%s — consumed as masks, BigInt (uint64) storage (SPEC §4.2)", f.Type.Name))
		}
	}
	if f.HasDefault {
		parts = append(parts, fmt.Sprintf("= %s at construction; Zero* gives the §5 zero form", g.defaultValue(f)))
	}
	if f.HasIntRange {
		parts = append(parts, fmt.Sprintf("wire [%s, %s]", f.IntMin, f.IntMax))
	}
	if f.HasFloatRange {
		note := ""
		if f.Round != "nearest" {
			note = ", round " + f.Round + " (advisory: SPEC §4.8 rule 4 — projection, not this wire conversion)"
		}
		if f.Interpolate {
			parts = append(parts, fmt.Sprintf("shallow wire: [%s, %s] @ %s -> int [0, %d]%s",
				formatFloat(f.FMin), formatFloat(f.FMax), formatFloat(f.Resolution), f.Steps, note))
		} else {
			parts = append(parts, fmt.Sprintf("compressed float [%s, %s] @ %s%s",
				formatFloat(f.FMin), formatFloat(f.FMax), formatFloat(f.Resolution), note))
		}
	}
	if f.Local {
		if f.Context != "" {
			parts = append(parts, fmt.Sprintf("[local, context = %s]", f.Context))
		} else {
			parts = append(parts, "[local] — no wire")
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " // " + strings.Join(parts, "; ")
}

func (g *gen) defaultValue(f *ir.Field) string {
	switch {
	case f.Type.Kind == ir.TBool:
		return fmt.Sprintf("%v", f.DefBool)
	case f.DefVariant != "":
		g.addRef(f.Type.Name, f.Type.Name)
		return f.Type.Name + "." + f.DefVariant
	case f.Type.Kind == ir.TFloat32:
		return formatFloat32(f.DefFloat)
	case f.Type.Kind == ir.TFloat64:
		return formatFloat(f.DefFloat)
	case isBigStorage(f.Type):
		// 64/128-bit storage: a BigInt literal is exact at any width — no
		// lane composition needed, unlike the Go/C# pair constructors
		return bigLit(f.DefInt)
	default:
		return g.renderNum(f.DefExpr, f.DefInt)
	}
}

// bigLit renders an integer as a BigInt literal — exact at any width, which
// collapses the other targets' 128-bit lane composition to a plain literal.
func bigLit(v *big.Int) string {
	return v.String() + "n"
}

// renderNum renders an integer expression for a Number-domain context:
// symbolically where every referenced constant is a bare (untyped) schema
// const with Number storage and every subtree folds exactly in the double
// domain (integral, inside ±(2^53 - 1) — JS `/` is float division, so a
// non-exact division subtree must fold); the computed literal otherwise.
// Folding is always correct.
func (g *gen) renderNum(e ast.Expr, folded *big.Int) string {
	if e == nil || ir.ExprHasEnumMax(e) || !g.renderable(e) || !g.numExact(e) {
		return folded.String()
	}
	return g.renderExpr(e)
}

// numExact reports whether every subtree of e evaluates exactly in JS Number
// arithmetic: integral values inside the safe-integer bound, divisions with
// zero remainder (JS `/` is float division; schema folding truncates).
func (g *gen) numExact(e ast.Expr) bool {
	_, ok := g.numEval(e)
	return ok
}

func (g *gen) numEval(e ast.Expr) (*big.Int, bool) {
	switch e := e.(type) {
	case *ast.IntLit:
		return e.Value, fitsSafeInteger(e.Value)
	case *ast.IdentExpr:
		c, ok := g.unit.Consts[e.Name]
		if !ok || c.IsFloat || c.Int == nil || constIsBig(c) {
			return nil, false
		}
		return c.Int, true
	case *ast.ParenExpr:
		return g.numEval(e.X)
	case *ast.UnaryExpr:
		v, ok := g.numEval(e.X)
		if !ok {
			return nil, false
		}
		nv := new(big.Int).Neg(v)
		return nv, fitsSafeInteger(nv)
	case *ast.BinaryExpr:
		x, ok := g.numEval(e.X)
		if !ok {
			return nil, false
		}
		y, ok := g.numEval(e.Y)
		if !ok {
			return nil, false
		}
		v := new(big.Int)
		switch e.Op {
		case "+":
			v.Add(x, y)
		case "-":
			v.Sub(x, y)
		case "*":
			v.Mul(x, y)
		case "/":
			if y.Sign() == 0 {
				return nil, false
			}
			rem := new(big.Int)
			v.QuoRem(x, y, rem)
			if rem.Sign() != 0 {
				return nil, false // JS float division would not truncate
			}
		case "%":
			if y.Sign() == 0 {
				return nil, false
			}
			v.Rem(x, y)
		default:
			return nil, false
		}
		return v, fitsSafeInteger(v)
	}
	return nil, false
}

// renderable: every referenced constant must be a BARE (untyped) integer
// schema const — an explicitly 64-bit const stores as BigInt here
// (constIsBig), which cannot mix with Number arithmetic.
func (g *gen) renderable(e ast.Expr) bool {
	for _, name := range ir.ExprConsts(e) {
		c, ok := g.unit.Consts[name]
		if !ok || c.IsFloat || c.Explicit {
			return false
		}
	}
	return true
}

// renderExpr renders an expression in JS form: constants keep the schema
// spelling, imported where they live in another file.
func (g *gen) renderExpr(e ast.Expr) string {
	return ir.RenderExprIdent(e, func(name string) string {
		g.addRef(name, name)
		return name
	})
}

func formatFloat(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// formatFloat32 renders a float32-precision literal: the shortest form that
// parses to exactly float32(v) — as a JS double it holds that float32 value
// exactly (f32 ⊂ f64), and the runtime frounds its parameters, so the wire
// widths agree with ir.CompressedFloatBits in every target.
func formatFloat32(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 32)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}
