// Package cpp emits the C++ target: TWO headers per schema file, everything in
// namespace <package>, #pragma once, deterministic to the byte (SPEC §6.1).
//
// <Base>.h is the DATA header — constants, enums, flags, type/message structs,
// the per-object view families, MaxBits/MaxBytes bounds and the Message
// storage surface — and depends on serialize.h only when a storage type
// requires it (int128/fixed). <Base>Wire.h is the WIRE header — the
// Write/Read functions, Quantize/Unquantize and the tag/dispatch wire — and
// includes <Base>.h plus serialize.h. The split exists so data consumers (a
// game basing its math types on generated structs) never inherit the
// serialize runtime or its macro namespace, which collides with vendored
// older serialize copies (the space integration finding, 2026-08-11).
//
// Every member zero-initializes unless the schema specifies a default
// (Glenn, 2026-08-05).
package cpp

import (
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/internal/ast"
	"github.com/mas-bandwidth/schema/ir"
)

// Options selects caller-facing generation choices (SPEC §4.8).
type Options struct {
	// MessageRepr is the C++ message dispatch representation: "union" (a
	// tagged struct over an anonymous union, the classic game idiom — the
	// DEFAULT) or "variant" (std::variant, index == wire tag, opt-in).
	// Caller's choice per Glenn 2026-08-05; the default is union on his
	// compile-time measurement the same hour: 0.17s -> 0.27s "is not trivial
	// for me. it's almost 2X" / "I'm very negative on modern C++ for this
	// reason... you almost always pay through the nose for it at compile
	// time."
	MessageRepr string
}

// Generate returns basename.h -> file contents for every file of the unit.
// It fails loudly on a cross-file include cycle: schema references are
// order-free (SPEC §4.2), but a by-value C++ member needs a complete type, so
// mutually-including headers cannot compile and the fix is moving a
// declaration, which only the author can choose.
func Generate(u *ir.Unit, opts Options) (map[string][]byte, error) {
	switch opts.MessageRepr {
	case "":
		opts.MessageRepr = "union"
	case "variant", "union":
	default:
		return nil, fmt.Errorf("unknown --cpp-message %q — the choices are union (default) and variant", opts.MessageRepr)
	}
	if opts.MessageRepr == "union" {
		if err := checkUnionMemberNames(u); err != nil {
			return nil, err
		}
	}
	if cycle := includeCycle(u); cycle != "" {
		return nil, fmt.Errorf(
			"generated C++ headers would include each other in a cycle (%s) — types compose across files order-free in schema, but C++ headers cannot include mutually; move a declaration so the file graph is acyclic", cycle)
	}
	bases := map[string]bool{}
	for _, f := range u.Files {
		bases[f.Base] = true
	}
	for _, f := range u.Files {
		if bases[f.Base+"Wire"] {
			return nil, fmt.Errorf("schema files %s and %sWire collide — the C++ emitter writes %sWire.h as %s's wire header; rename one file (SPEC §6.1)", f.Base, f.Base, f.Base, f.Base)
		}
	}
	out := map[string][]byte{}
	protocolIdHome := protocolIdHome(u)
	msgOwner := ir.MessageOwner(u)
	objOwner := ir.ObjectOwner(u)
	for _, f := range u.Files {
		g := &gen{unit: u, file: f, opts: opts, msgOwner: msgOwner, objOwner: objOwner}
		g.emitDataFile(f.Base == protocolIdHome)
		out[f.Base+".h"] = g.assemble()

		w := &gen{unit: u, file: f, opts: opts, msgOwner: msgOwner, objOwner: objOwner, wire: true}
		w.emitWireFile()
		out[f.Base+"Wire.h"] = w.assemble()
	}
	return out, nil
}

// camelToSnake maps an UpperCamelCase declaration name to the
// lower_snake_case union member name: ShipCreate -> ship_create,
// ABTest -> ab_test.
func camelToSnake(name string) string {
	var b strings.Builder
	runes := []rune(name)
	for i, r := range runes {
		if r >= 'A' && r <= 'Z' {
			prevLower := i > 0 && runes[i-1] >= 'a' && runes[i-1] <= 'z'
			nextLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'
			if i > 0 && (prevLower || nextLower) {
				b.WriteByte('_')
			}
			b.WriteRune(r - 'A' + 'a')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// checkUnionMemberNames guards the union representation's derived member
// names: distinct after snake-casing, and never the tag field's own name.
func checkUnionMemberNames(u *ir.Unit) error {
	seen := map[string]string{}
	for _, m := range u.Messages {
		s := camelToSnake(m)
		if s == "type" {
			return fmt.Errorf("message %s: its union member name would be %q, which is the tag field — rename the message (SPEC §4.8)", m, s)
		}
		if prev, dup := seen[s]; dup {
			return fmt.Errorf("messages %s and %s share the union member name %q after snake-casing — rename one (SPEC §4.8)", prev, m, s)
		}
		seen[s] = m
	}
	return nil
}

// includeCycle returns a printable cycle among generated headers, or "".
func includeCycle(u *ir.Unit) string {
	deps := ir.FileDeps(u)
	const (
		white = 0
		grey  = 1
		black = 2
	)
	color := map[string]int{}
	var path []string
	var found string
	var visit func(base string) bool
	visit = func(base string) bool {
		switch color[base] {
		case grey:
			found = strings.Join(append(path, base), " -> ")
			return false
		case black:
			return true
		}
		color[base] = grey
		path = append(path, base)
		targets := make([]string, 0, len(deps[base]))
		for t := range deps[base] {
			targets = append(targets, t)
		}
		sort.Strings(targets)
		for _, t := range targets {
			if !visit(t) {
				return false
			}
		}
		path = path[:len(path)-1]
		color[base] = black
		return true
	}
	for _, f := range u.Files {
		if !visit(f.Base) {
			return found
		}
	}
	return ""
}

// protocolIdHome picks the file that carries the unit-level ProtocolId
// constant: the constants aspect file if the unit has one, else the first.
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
	opts     Options
	msgOwner string // the one file that carries the message dispatch surface
	objOwner string // the one file that carries the object tag surface
	wire     bool   // emitting the wire half (<Base>Wire.h): functions over serialize streams

	body           strings.Builder
	includes       map[string]bool
	nativeIncludes map[string]bool    // cpp_include headers of referenced native-mapped types
	emitted        map[string]bool    // consts of this file emitted so far (symbolic-reference safety)
	bulkBytes      map[*ir.Field]bool // fixed [N]uint8 arrays at statically byte-aligned positions (ir.AlignedFixedByteArrays)
	needsSerialize bool               // the file emits wire functions -> include "serialize.h"
	needsCstring   bool               // the file emits memset -> include <cstring>
	needsCmath     bool               // the file emits floor() -> include <cmath>
	needsVariant   bool               // the file emits the Message dispatch -> include <variant>
}

func (g *gen) pf(format string, args ...any) {
	fmt.Fprintf(&g.body, format, args...)
}

func (g *gen) assemble() []byte {
	var h strings.Builder
	// the basename, not the invocation-relative path: output is deterministic
	// to the byte wherever the compiler runs (SPEC §6.1)
	fmt.Fprintf(&h, "// Generated by the schema compiler from %s.schema. DO NOT EDIT.\n// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n// your choice. See the LICENSE exception in the schema compiler; the compiler is\n// AGPL-3.0, its output is not.\n", g.file.Base)
	fmt.Fprintf(&h, "// package %s — protocol id 0x%016x\n\n", g.unit.Package, g.unit.ProtocolId)
	h.WriteString("#pragma once\n\n")
	h.WriteString("#include <cstdint>\n")
	if g.needsCmath {
		h.WriteString("#include <cmath>\n")
	}
	if g.needsCstring {
		h.WriteString("#include <cstring>\n")
	}
	if g.needsVariant {
		h.WriteString("#include <variant>\n")
	}
	if g.needsSerialize {
		h.WriteString("\n#include \"serialize.h\"\n")
	}
	if g.wire {
		// the wire half sits on its own data half; cross-file wire calls ride
		// the dep's wire header (whose data header arrives transitively)
		fmt.Fprintf(&h, "\n#include \"%s.h\"\n", g.file.Base)
	}
	if len(g.includes) > 0 {
		names := make([]string, 0, len(g.includes))
		for n := range g.includes {
			names = append(names, n)
		}
		sort.Strings(names)
		if !g.wire {
			h.WriteString("\n")
		}
		for _, n := range names {
			if g.wire {
				fmt.Fprintf(&h, "#include \"%sWire.h\"\n", n)
			} else {
				fmt.Fprintf(&h, "#include \"%s.h\"\n", n)
			}
		}
	}
	if len(g.nativeIncludes) > 0 {
		names := make([]string, 0, len(g.nativeIncludes))
		for n := range g.nativeIncludes {
			names = append(names, n)
		}
		sort.Strings(names)
		h.WriteString("\n// native type mapping (cpp_native, SPEC §4.2): the hand types storage speaks\n")
		for _, n := range names {
			fmt.Fprintf(&h, "#include \"%s\"\n", n)
		}
	}
	fmt.Fprintf(&h, "\nnamespace %s {\n\n", g.unit.Package)
	body := g.body.String()
	h.WriteString(body)
	if !strings.HasSuffix(body, "\n\n") {
		h.WriteString("\n")
	}
	fmt.Fprintf(&h, "} // namespace %s\n", g.unit.Package)
	return []byte(h.String())
}

func (g *gen) emitDataFile(carriesProtocolId bool) {
	g.includes = map[string]bool{}
	g.nativeIncludes = map[string]bool{}
	g.emitted = map[string]bool{}

	if carriesProtocolId {
		g.pf("// The unit's protocol id — the hash of its schema files (SPEC §3.1). Two\n")
		g.pf("// sides at the same id speak identical bits; there is no other versioning.\n")
		g.pf("inline constexpr uint64_t ProtocolId = 0x%016xull;\n\n", g.unit.ProtocolId)
	}

	// MessageType / ObjectType lead their OWNER file — the unit-level surface
	// is emitted exactly once, in the topologically last carrying file, so
	// declarations spread across files never redeclare it (SPEC §2 keeps the
	// aspect layout non-enforced; ir.MessageOwner picks the file).
	if g.file.Base == g.msgOwner && len(g.unit.Messages) > 0 {
		g.emitTagEnum("MessageType", g.unit.Messages,
			"the message set, extracted by the compiler — None = 0, then each message sorted by name (SPEC §4.8)")
	}
	if g.file.Base == g.objOwner && len(g.unit.ObjNames) > 0 {
		g.emitTagEnum("ObjectType", g.unit.ObjNames,
			"the object set, extracted by the compiler — None = 0, then each object sorted by name (SPEC §4.8)")
	}

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
			g.pf("// context-scoped | local fields, its State struct is generated once per\n")
			g.pf("// context (ClientShipState, ServerShipState, ...), each holding the `all`\n")
			g.pf("// fields plus its own context's. No preprocessor in any target.\n\n")
		case *ir.Struct:
			g.emitStruct(d)
			g.emitStructMaxBits(d)
		case *ir.Union:
			g.emitUnion(d)
			g.emitUnionMaxBits(d)
		case *ir.Object:
			g.emitObject(d)
			g.emitObjectQuantize(d)
			g.emitObjectMaxBits(d)
		}
	}

	if g.file.Base == g.msgOwner && len(g.unit.Messages) > 0 {
		g.emitMessageData()
	}
}

// emitWireFile emits the wire half: every serialize-stream function for the
// file's declarations. Same-file constants are pre-marked emitted — they live
// in the data header this file includes, so symbolic references stay
// renderable rather than folding to literals.
func (g *gen) emitWireFile() {
	g.includes = map[string]bool{}
	g.nativeIncludes = map[string]bool{}
	g.emitted = map[string]bool{}
	for _, d := range g.file.Decls {
		if c, ok := d.(*ir.Const); ok {
			g.emitted[c.Name] = true
		}
	}

	if g.fileEmitsWire() {
		g.emitWriteInlineMacro()
		g.emitReadInlineMacro()
	}

	if fileHasStrings(g.file) {
		g.emitUtf8Validator()
		g.emitInteriorNullScan()
	}

	for _, d := range ir.EmissionOrder(g.file) {
		var fields []*ir.Field
		switch d := d.(type) {
		case *ir.Struct:
			fields = d.Fields
		case *ir.Object:
			fields = d.Fields
		case *ir.Union:
			// cross-file payloads mean cross-file wire calls exactly like
			// named field types
			for _, v := range d.Variants {
				g.noteRef(v.Type)
			}
		}
		// cross-file field types mean cross-file wire calls (WriteVector3 from
		// another file's wire header) — record the dep before emitting
		for _, f := range fields {
			if f.Type.Kind == ir.TNamed {
				g.noteRef(f.Type.Name)
			}
		}
		switch d := d.(type) {
		case *ir.Struct:
			g.emitStructWire(d)
		case *ir.Union:
			g.emitUnionWire(d)
		case *ir.Object:
			g.emitObjectWire(d)
		}
	}

	if g.file.Base == g.msgOwner && len(g.unit.Messages) > 0 {
		g.emitMessageWire()
	}
	if g.file.Base == g.objOwner && len(g.unit.ObjNames) > 0 {
		g.emitObjectTagWire()
	}
}

func (g *gen) emitTagEnum(name string, members []string, comment string) {
	g.pf("// %s: %s\n", name, comment)
	storage := cppUint(ir.StorageBitsFor(int64(len(members))))
	g.pf("enum class %s : %s {\n", name, storage)
	g.pf("    None = 0,\n")
	for i, m := range members {
		g.pf("    %s = %d,\n", m, i+1)
	}
	g.pf("    Max = %d, // the exported extent (SPEC §4.2)\n", len(members))
	g.pf("};\n\n")
}

// emitUnion emits a first-class one-of (SPEC §4.8): the generated <Name>Type
// tag enum, then the message union shape exactly — a struct holding the tag
// over an anonymous union of the arms, constructed as None, trivially
// copyable. An arm's storage is established ZEROED when the arm is selected
// (by Read<Name> before it decodes, or by a writer assigning the arm).
func (g *gen) emitUnion(d *ir.Union) {
	members := make([]string, len(d.Variants))
	for i, v := range d.Variants {
		members[i] = ir.GoExportName(v.Name)
	}
	g.emitTagEnum(d.Name+"Type",
		members,
		fmt.Sprintf("union %s's tag — None = 0, then each variant in declared order (SPEC §4.8)", d.Name))

	g.pf("// union %s — at most one of the arms; the tag says which. Construction is\n", d.Name)
	g.pf("// None: the tag alone is initialized; an arm's storage is established ZEROED\n")
	g.pf("// when the arm is selected — by Read%s before it decodes (SPEC §5), or by\n", d.Name)
	g.pf("// assigning it: value.%s = %s{}. Bytes of unselected arms are indeterminate.\n",
		unionExampleArm(d), unionExampleType(d))
	g.pf("struct %s\n{\n", d.Name)
	g.pf("    %sType type;\n", d.Name)
	if len(d.Variants) > 0 {
		g.pf("\n    union\n    {\n")
		for _, v := range d.Variants {
			g.noteRef(v.Type)
			g.pf("        %s %s;\n", v.Type, v.Name)
		}
		g.pf("    };\n")
	}
	g.pf("\n    %s() : type( %sType::None ) {} // the tag only — arms are zero-established at selection\n", d.Name, d.Name)
	g.pf("};\n\n")
}

// unionExampleArm/Type name the first arm for the assignment example in the
// generated comment; an empty union gets a placeholder that names the rule.
func unionExampleArm(d *ir.Union) string {
	if len(d.Variants) == 0 {
		return "arm"
	}
	return d.Variants[0].Name
}

func unionExampleType(d *ir.Union) string {
	if len(d.Variants) == 0 {
		return "Arm"
	}
	return d.Variants[0].Type
}

func (g *gen) emitConst(d *ir.Const) {
	typ := cppConstType(d.Storage)
	if d.IsFloat {
		g.pf("inline constexpr %s %s = %s;%s\n", typ, d.Name, formatFloat(d.Float, d.Storage == "float32"), g.foldComment(d.Expr))
		g.emitted[d.Name] = true
		return
	}
	if !d.Int.IsInt64() {
		// above INT64_MAX a bare decimal literal is ill-formed C++ — only a
		// uint64-typed constant can hold such a value, so the suffix is right
		g.pf("inline constexpr %s %s = %sull; // = %s\n", typ, d.Name, d.Int.String(), ir.RenderExpr(d.Expr))
		g.emitted[d.Name] = true
		return
	}
	g.pf("inline constexpr %s %s = %s;%s\n", typ, d.Name, g.renderInt(d.Expr, d.Int), g.foldComment(d.Expr))
	g.emitted[d.Name] = true
}

// foldComment returns a trailing comment carrying the schema expression when
// the rendered C++ had to fold it (an E.Max reference has no C++ twin).
func (g *gen) foldComment(e ast.Expr) string {
	if ir.ExprHasEnumMax(e) {
		return fmt.Sprintf(" // = %s", ir.RenderExpr(e))
	}
	return ""
}

func (g *gen) emitEnum(d *ir.Enum) {
	g.pf("// enum %s — None = 0 implicit, variants dense from 1, wire range [0, %d] (SPEC §4.2)\n", d.Name, d.Max)
	g.pf("enum class %s : %s {\n", d.Name, cppUint(d.StorageBits))
	g.pf("    None = 0,\n")
	for i, v := range d.Variants {
		g.pf("    %s = %d,\n", v, i+1)
	}
	g.pf("    Max = %d, // the exported extent (SPEC §4.2)\n", d.Max)
	g.pf("};\n\n")
	g.pf("// EnumName: debug/log name for any %s value, out-of-set included\n", d.Name)
	g.pf("inline const char * EnumName( %s value )\n{\n", d.Name)
	g.pf("    switch ( value )\n    {\n")
	g.pf("        case %s::None: return \"None\";\n", d.Name)
	for _, v := range d.Variants {
		g.pf("        case %s::%s: return \"%s\";\n", d.Name, v, v)
	}
	g.pf("        default: return \"???\";\n    }\n}\n\n")
}

func (g *gen) emitFlags(d *ir.Flags) {
	g.pf("// flags %s — one bit per variant, consumed as masks; storage uint64 in every\n", d.Name)
	g.pf("// target, wire %d bits (SPEC §4.2)\n", d.WireBits)
	g.pf("using %s = uint64_t;\n", d.Name)
	for i, v := range d.Variants {
		g.pf("inline constexpr %s %s_%s = 1ull << %d;\n", d.Name, d.Name, v, i)
	}
	g.pf("inline constexpr int64_t %sCount = %d; // the declared variant count (SPEC §4.2)\n", d.Name, len(d.Variants))
	g.pf("\n")
}

func (g *gen) emitStruct(d *ir.Struct) {
	kind := "type"
	if d.IsMessage {
		kind = "message"
	}
	if len(d.Tags) > 0 {
		g.pf("// %s %s [%s] — the tag is user-chosen and inert in v1; the delta pass\n", kind, d.Name, strings.Join(d.Tags, ", "))
		g.pf("// claims tags and assigns actions (SPEC §4.2, Type tags)\n")
	} else {
		g.pf("// %s %s\n", kind, d.Name)
	}
	g.pf("struct %s {\n", d.Name)
	g.emitFields(d.Fields, storageDeep)
	g.pf("};\n\n")
}

// view selects which storage a field emission derives (SPEC §4.8).
type view int

const (
	storageDeep    view = iota // declared storage — State and Data_Deep
	storageShallow             // quantized wire storage
	storageInterp              // interpolate storage: projected fields wire-int, composites continuous
)

func (g *gen) emitObject(d *ir.Object) {
	g.pf("// ---- object %s — one definition, a generated family per target (SPEC §4.8) ----\n\n", d.Name)

	// State: the full simulation struct — every field; one per context where
	// context-scoped fields exist, else once, unprefixed.
	if hasContextFields(d) {
		for _, ctx := range g.unit.Contexts {
			var fields []*ir.Field
			for _, f := range d.Fields {
				if f.Context == "" || f.Context == ctx {
					fields = append(fields, f)
				}
			}
			g.pf("// %s%sState — the full simulation struct for the %s context: every `all`\n", capitalize(ctx), d.Name, ctx)
			g.pf("// field plus the fields scoped | local, context = %s\n", ctx)
			g.pf("struct %s%sState {\n", capitalize(ctx), d.Name)
			g.emitFields(fields, storageDeep)
			g.pf("};\n\n")
		}
	} else {
		g.pf("// %sState — the full simulation struct: every field\n", d.Name)
		g.pf("struct %sState {\n", d.Name)
		g.emitFields(d.Fields, storageDeep)
		g.pf("};\n\n")
	}

	var deep, interp []*ir.Field
	for _, f := range d.Fields {
		if !f.Local {
			deep = append(deep, f)
		}
		if f.Interpolate {
			interp = append(interp, f)
		}
	}

	g.pf("// %sData_Deep — every non- | local field, deep encodings: full state for\n", d.Name)
	g.pf("// client-side prediction\n")
	g.pf("struct %sData_Deep {\n", d.Name)
	g.emitFields(deep, storageDeep)
	g.pf("};\n\n")

	g.pf("// %sData_Shallow — the | interpolate fields on the quantized wire: the\n", d.Name)
	g.pf("// implementation detail on the way to interpolation on the client\n")
	g.pf("struct %sData_Shallow {\n", d.Name)
	g.emitFields(interp, storageShallow)
	g.pf("};\n\n")

	g.pf("// %sData_Interpolate — the same fields in interpolate storage: projected\n", d.Name)
	g.pf("// fields stay in the wire integer domain and snap-interpolate; quantized\n")
	g.pf("// composites store continuous (SPEC §4.8 rule 5)\n")
	g.pf("struct %sData_Interpolate {\n", d.Name)
	g.emitFields(interp, storageInterp)
	g.pf("};\n\n")
}

func hasContextFields(d *ir.Object) bool {
	for _, f := range d.Fields {
		if f.Context != "" {
			return true
		}
	}
	return false
}

func (g *gen) emitFields(fields []*ir.Field, v view) {
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
		g.emitField(f, v)
	}
}

func (g *gen) emitField(f *ir.Field, v view) {
	if v == storageShallow && f.HasQuantize {
		st := f.Type.Ref.(*ir.Struct)
		if f.FixedShallow {
			g.pf("    // %s: %s narrowed to %d fractional bits (quantize = %s) — per-component\n",
				f.Name, f.Type.Name, f.QuantShift, ir.RenderExpr(f.QuantScaleExpr))
			g.pf("    // quantized units; bounds are the component's whole-unit | min, max scaled\n")
			for _, comp := range st.Fields {
				lo, hi, _, _, typ := fixedShallowComp(f, comp)
				g.pf("    %s %s_%s = 0; // in [%s, %s]\n", typ, f.Name, comp.Name, lo, hi)
			}
			return
		}
		g.pf("    // %s: %s quantized by %s, max %s — per-component int in [-%d, %d]\n",
			f.Name, f.Type.Name, ir.RenderExpr(f.QuantScaleExpr), ir.RenderExpr(f.QuantMaxExpr), f.QuantBound, f.QuantBound)
		typ := cppInt(smallestSigned(f.QuantBound))
		for _, comp := range st.Fields {
			g.pf("    %s %s_%s = 0;\n", typ, f.Name, comp.Name)
		}
		return
	}
	if (v == storageShallow || v == storageInterp) && f.HasFloatRange {
		typ := cppUint(smallestUnsigned(f.Steps))
		note := ""
		if f.Round != "nearest" {
			note = ", round " + f.Round + " (advisory: SPEC §4.8 rule 4 — projection, not this wire conversion)"
		}
		tail := ""
		if v == storageInterp {
			tail = " — wire-int domain, snap-interpolated (SPEC §4.8 rule 5)"
		}
		g.pf("    %s %s = 0; // float [%s, %s] @ resolution %s -> wire int [0, %d]%s%s\n",
			typ, f.Name, formatFloatShort(f.FMin), formatFloatShort(f.FMax),
			formatFloatShort(f.Resolution), f.Steps, note, tail)
		return
	}

	// declared storage (deep view), and shallow/interp passthrough for
	// discrete fields (enums, flags, plain scalars)
	g.emitStorageField(f)
}

func (g *gen) emitStorageField(f *ir.Field) {
	typ, isStructType := g.cppFieldType(f.Type)

	switch {
	case f.Type.Kind == ir.TString:
		g.pf("    char %s[%s + 1] = {}; // string(%s): max length, used length beside it (SPEC §4.7)\n",
			f.Name, g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size)), ir.RenderExpr(f.Type.SizeExpr))
		g.pf("    int32_t %s_length = 0;\n", f.Name)
	case f.Type.Kind == ir.TBytes:
		g.pf("    uint8_t %s[%s] = {}; // bytes(%s): fixed buffer, used length beside it (SPEC §4.7)\n",
			f.Name, g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size)), ir.RenderExpr(f.Type.SizeExpr))
		g.pf("    int32_t %s_length = 0;\n", f.Name)
	case f.Array == ir.ArrayFixed:
		g.pf("    %s %s[%s] = {};%s\n", typ, f.Name, g.renderInt(f.ArrayExpr, big.NewInt(f.ArrayBound)), g.fieldComment(f))
	case f.Array == ir.ArrayCounted:
		g.pf("    %s %s[%s] = {}; // used count beside it; wire count in [%d, %s]\n",
			typ, f.Name, g.renderInt(f.ArrayExpr, big.NewInt(f.ArrayBound)), f.ArrayMin, g.renderInt(f.ArrayExpr, big.NewInt(f.ArrayBound)))
		g.pf("    int32_t %s_count = 0;\n", f.Name)
	default:
		g.pf("    %s %s%s;%s\n", typ, f.Name, g.initializer(f, typ, isStructType), g.fieldComment(f))
	}
}

// initializer returns the member initializer: the specified default if one is
// declared, else the zero value (Go's rule, in every generated language).
func (g *gen) initializer(f *ir.Field, typ string, isStructType bool) string {
	if f.HasDefault {
		switch {
		case f.Type.Kind == ir.TBool:
			return fmt.Sprintf(" = %v", f.DefBool)
		case f.DefVariant != "":
			return fmt.Sprintf(" = %s::%s", f.Type.Name, f.DefVariant)
		case f.Type.Kind == ir.TFloat32:
			return " = " + formatFloat(f.DefFloat, true)
		case f.Type.Kind == ir.TFloat64:
			return " = " + formatFloat(f.DefFloat, false)
		default:
			if f.Type.Kind == ir.TInt && f.Type.Width == 128 {
				return " = " + g.render128(f.DefExpr, f.DefInt, f.Type.Signed)
			}
			if f.Type.Kind == ir.TFixed && f.Type.Width == 128 && !f.Type.Signed {
				// the default expression is in WHOLE UNITS; DefInt is the raw
				// scaled integer, which for wide ufixed can exceed int64
				return " = " + g.render128(nil, f.DefInt, false)
			}
			return " = " + g.renderInt(f.DefExpr, f.DefInt)
		}
	}
	if isStructType {
		return "" // a generated struct type zero-initializes itself, member by member
	}
	switch f.Type.Kind {
	case ir.TBool:
		return " = false"
	case ir.TFloat32:
		return " = 0.0f"
	case ir.TFloat64:
		return " = 0.0"
	case ir.TNamed:
		if _, isEnum := f.Type.Ref.(*ir.Enum); isEnum {
			return fmt.Sprintf(" = %s::None", f.Type.Name)
		}
		return " = 0" // flags alias
	default:
		return " = 0"
	}
}

func (g *gen) fieldComment(f *ir.Field) string {
	var parts []string
	if f.Type.Kind == ir.TFixed {
		spelling, q := "fixed", "Q"
		if !f.Type.Signed {
			spelling, q = "ufixed", "UQ"
		}
		parts = append(parts, fmt.Sprintf("%s(%d, %d) — %s%d.%d, raw value scaled by 2^%d; bounds in whole units",
			spelling, f.Type.IntBits, f.Type.FracBits, q, f.Type.IntBits, f.Type.FracBits, f.Type.FracBits))
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
			// the triple describes the SHALLOW wire only; the deep wire is the
			// bare storage type's encoding (SPEC §4.8)
			parts = append(parts, fmt.Sprintf("shallow wire: [%s, %s] @ %s -> int [0, %d]%s",
				formatFloatShort(f.FMin), formatFloatShort(f.FMax), formatFloatShort(f.Resolution), f.Steps, note))
		} else {
			parts = append(parts, fmt.Sprintf("compressed float [%s, %s] @ %s%s",
				formatFloatShort(f.FMin), formatFloatShort(f.FMax), formatFloatShort(f.Resolution), note))
		}
	}
	if f.Local {
		if f.Context != "" {
			parts = append(parts, fmt.Sprintf(" | local, context = %s", f.Context))
		} else {
			parts = append(parts, " | local — no wire")
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " // " + strings.Join(parts, "; ")
}

// cppFieldType maps a field type to its C++ storage (SPEC §6.1) and reports
// whether it is a generated struct type (which self-initializes).
func (g *gen) cppFieldType(t ir.FieldType) (string, bool) {
	switch t.Kind {
	case ir.TInt:
		if t.Width == 128 {
			// serialize::int128_t / serialize::uint128_t — native __int128
			// where the compiler provides it, the emulated pair where it
			// doesn't; byte-identical wire either way (STANDARD.md, uint128)
			g.needsSerialize = true
			if t.Signed {
				return "serialize::int128_t", false
			}
			return "serialize::uint128_t", false
		}
		return cppInt2(t.Signed, t.Width), false
	case ir.TFixed:
		// the raw scaled integer, in storage of exactly I+F bits whose
		// signedness is the type's own — serialize_fixed's storage
		// convention (STANDARD.md, fixed; the runtime's codec is storage
		// generic, so ufixed rides the same macro with unsigned storage)
		if t.Width == 128 {
			g.needsSerialize = true
			if t.Signed {
				return "serialize::int128_t", false
			}
			return "serialize::uint128_t", false
		}
		return cppInt2(t.Signed, t.Width), false
	case ir.TBits:
		if t.Width <= 32 {
			return "uint32_t", false
		}
		return "uint64_t", false
	case ir.TBool:
		return "bool", false
	case ir.TFloat32:
		return "float", false
	case ir.TFloat64:
		return "double", false
	case ir.TNamed:
		g.noteRef(t.Name)
		if _, isUnion := t.Ref.(*ir.Union); isUnion {
			// the generated union struct self-initializes (its constructor
			// is the None value), the struct rule exactly
			return t.Name, true
		}
		st, isStruct := t.Ref.(*ir.Struct)
		if isStruct && st.CppNative != "" && g.unit.DeclFile[t.Name] != g.file.Base {
			// native type mapping (SPEC §4.2): storage speaks the hand math
			// type deriving from the generated basis — same layout (the
			// relocatability asserts still hold), plus its operations. Inside
			// the basis's own header the mapping is off: the native header
			// includes that header, so a mapped reference would be circular.
			g.nativeIncludes[st.CppInclude] = true
			return "::" + st.CppNative, true
		}
		return t.Name, isStruct
	}
	return "/* ? */", false
}

// noteRef records a cross-file include for a referenced declaration.
func (g *gen) noteRef(name string) {
	if base, ok := g.unit.DeclFile[name]; ok && base != g.file.Base {
		g.includes[base] = true
	}
}

// renderInt renders an integer expression symbolically where its constant
// references are safe to name in C++ — declared earlier in this file or in an
// included file — and folds to the computed value otherwise (an E.Max
// reference always folds; the schema source rides in a comment). The two
// extremes with no direct decimal spelling fold guarded, the same guards
// emitConst applies to constants of the same values: INT64_MIN's literal
// half overflows long long before the unary minus applies, and a value past
// INT64_MAX deduces unsigned as a bare decimal — -Wimplicitly-unsigned-
// literal, a -Werror build break in the consumer's tree — so only the ull
// spelling can hold it (issue #100; the member initializers were the one
// fold site reaching here unguarded).
func (g *gen) renderInt(e ast.Expr, folded *big.Int) string {
	if folded != nil && folded.IsInt64() && folded.Int64() == math.MinInt64 {
		return "( -9223372036854775807ll - 1 )" // INT64_MIN has no literal spelling in C++
	}
	if e == nil || ir.ExprHasEnumMax(e) || !g.renderable(e) || !g.overflowSafe(e) {
		if !folded.IsInt64() && folded.Sign() > 0 {
			return folded.String() + "ull"
		}
		return folded.String()
	}
	return g.renderExpr(e)
}

// overflowSafe reports whether e can render symbolically without the TARGET's
// arithmetic overflowing on the way to the folded value. Schema folding is
// arbitrary-precision; C++ is not: a literal-only subtree evaluates in int
// (small decimal literals deduce int), and `7 * 700000000` is a
// -Werror=-Winteger-overflow build break even though the product fits the
// int64 bound it feeds (found by FuzzGeneratedCompiles, issue #22). A subtree
// referencing a constant evaluates in int64 (constants emit as typed
// constexpr int64_t). Anything unprovable folds — folding is always correct,
// symbolic rendering is the luxury.
func (g *gen) overflowSafe(e ast.Expr) bool {
	_, _, ok := g.carrierEval(e)
	return ok
}

// carrierEval returns e's folded value, whether the subtree references a
// constant (and so carries 64-bit arithmetic), and whether every
// subexpression's value fits the arithmetic type it evaluates in.
func (g *gen) carrierEval(e ast.Expr) (*big.Int, bool, bool) {
	switch e := e.(type) {
	case *ast.IntLit:
		// a literal past INT64_MAX has no signed spelling in the target —
		// it deduces unsigned, a -Werror warning — so it cannot ride
		// symbolically even where the folded result is small
		return e.Value, false, e.Value.IsInt64()
	case *ast.IdentExpr:
		c, ok := g.unit.Consts[e.Name]
		if !ok || c.IsFloat || c.Int == nil {
			return nil, true, false
		}
		return c.Int, true, true
	case *ast.ParenExpr:
		return g.carrierEval(e.X)
	case *ast.UnaryExpr:
		v, wide, ok := g.carrierEval(e.X)
		if !ok {
			return nil, wide, false
		}
		nv := new(big.Int).Neg(v)
		return nv, wide, fitsCarrier(nv, wide)
	case *ast.BinaryExpr:
		x, xw, ok := g.carrierEval(e.X)
		if !ok {
			return nil, xw, false
		}
		y, yw, ok := g.carrierEval(e.Y)
		if !ok {
			return nil, yw, false
		}
		wide := xw || yw
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
				return nil, wide, false
			}
			v.Quo(x, y) // truncation toward zero, as C++ divides
		case "%":
			if y.Sign() == 0 {
				return nil, wide, false
			}
			v.Rem(x, y)
		default:
			return nil, wide, false
		}
		return v, wide, fitsCarrier(v, wide)
	}
	return nil, false, false
}

// fitsCarrier: a subtree with a constant reference evaluates in int64; a
// literal-only subtree evaluates in int, conservatively taken as 32-bit.
func fitsCarrier(v *big.Int, wide bool) bool {
	if wide {
		return v.IsInt64()
	}
	return v.IsInt64() && v.Int64() >= math.MinInt32 && v.Int64() <= math.MaxInt32
}

// render128 renders a 128-bit integer constant (a bound or a default). C++
// has no 128-bit literals, so a value that fits int64 renders like any other
// integer (symbolically where safe), a value that fits uint64 renders with
// the ull suffix, and anything wider composes its two's-complement halves in
// the unsigned domain — the runtime's own test spelling
// (`i128( ( serialize::uint128_t( 1 ) << 127 ) )`), exact for native and
// emulated storage alike.
func (g *gen) render128(e ast.Expr, folded *big.Int, signed bool) string {
	i64lo := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 63))
	i64hi := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 63), big.NewInt(1))
	if folded.Cmp(i64lo) >= 0 && folded.Cmp(i64hi) <= 0 {
		return g.renderInt(e, folded)
	}
	if folded.Sign() > 0 && folded.Cmp(maxUint64) <= 0 {
		return folded.String() + "ull"
	}
	// two's complement over 128 bits, split into 64-bit lanes
	u := new(big.Int).Set(folded)
	if u.Sign() < 0 {
		u.Add(u, new(big.Int).Lsh(big.NewInt(1), 128))
	}
	hi := new(big.Int).Rsh(u, 64)
	lo := new(big.Int).And(u, maxUint64)
	composed := fmt.Sprintf("( ( serialize::uint128_t( %sull ) << 64 ) | serialize::uint128_t( %sull ) )", hi, lo)
	if signed {
		return "serialize::int128_t" + composed
	}
	return composed
}

// renderable: C++ needs every referenced constant declared before use in the
// translation unit — same-file references render only after their declaration
// is emitted; a cross-file reference is carried by the include graph.
func (g *gen) renderable(e ast.Expr) bool {
	for _, name := range ir.ExprConsts(e) {
		base, ok := g.unit.DeclFile[name]
		if !ok {
			return false
		}
		if base == g.file.Base && !g.emitted[name] {
			return false
		}
	}
	return true
}

// renderExpr renders an expression in C++ form: constants keep the schema
// spelling, and every reference is noted for the include graph.
func (g *gen) renderExpr(e ast.Expr) string {
	return ir.RenderExprIdent(e, func(name string) string {
		g.noteRef(name)
		return name
	})
}

func cppConstType(storage string) string {
	switch storage {
	case "float32":
		return "float"
	case "float64":
		return "double"
	default:
		signed := strings.HasPrefix(storage, "int")
		width := 64
		_, _ = fmt.Sscanf(strings.TrimPrefix(strings.TrimPrefix(storage, "uint"), "int"), "%d", &width) // no digits: width stays 64
		return cppInt2(signed, width)
	}
}

func cppInt2(signed bool, width int) string {
	if signed {
		return fmt.Sprintf("int%d_t", width)
	}
	return fmt.Sprintf("uint%d_t", width)
}

func cppInt(width int) string  { return fmt.Sprintf("int%d_t", width) }
func cppUint(width int) string { return fmt.Sprintf("uint%d_t", width) }

// fixedShallowComp resolves one component of a narrowed fixed composite
// (SPEC §4.8 rule 2b) to its C++ shallow shape: wire bounds, wire bits,
// the int32/int64 read-write switch, and the storage type.
func fixedShallowComp(f, cf *ir.Field) (lo, hi *big.Int, bits int64, wide bool, typ string) {
	lo, hi = ir.FixedShallowBounds(f, cf)
	bits = bitsRequired(lo, hi)
	wide = hi.Cmp(big.NewInt(2147483647)) > 0 || lo.Cmp(big.NewInt(-2147483648)) < 0
	abs := new(big.Int).Neg(lo)
	if abs.Cmp(hi) < 0 {
		abs = hi
	}
	bound := int64(9223372036854775807)
	if abs.IsInt64() {
		bound = abs.Int64()
	}
	typ = cppInt(smallestSigned(bound))
	return
}

// cppInt64Lit renders a signed 64-bit literal; INT64_MIN has no direct
// literal in C++ (the unary minus applies after the overflowing parse).
func cppInt64Lit(v *big.Int) string {
	if v.IsInt64() && v.Int64() == -9223372036854775808 {
		return "( -9223372036854775807ll - 1 )"
	}
	return v.String() + "ll"
}

func smallestSigned(bound int64) int {
	switch {
	case bound <= 127:
		return 8
	case bound <= 32767:
		return 16
	case bound <= 2147483647:
		return 32
	default:
		return 64
	}
}

func smallestUnsigned(max int64) int { return ir.StorageBitsFor(max) }

func formatFloat(v float64, single bool) string {
	// single-precision literals format at FLOAT32 precision: the shortest
	// string that parses to exactly float32(v) — the same narrowing
	// ir.CompressedFloatBits computes widths with, so all five targets and
	// the advertised MaxBits agree even at f32 rounding ties
	bitSize := 64
	if single {
		bitSize = 32
	}
	s := strconv.FormatFloat(v, 'g', -1, bitSize)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	if single {
		s += "f"
	}
	return s
}

func formatFloatShort(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
