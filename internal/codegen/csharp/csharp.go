// Package csharp emits the C# target: one .cs file per schema file —
// Constants.schema -> Constants.cs — everything in namespace <Package>
// (file-scoped, C# 10+), deterministic to the byte (SPEC §6.1). There is no
// external formatter in the build path: the emitter writes clean
// 4-space-indented C# directly (goldens pin it), and the refuser is
// `dotnet build` with TreatWarningsAsErrors in the test chain.
//
// Storage follows the §6.1 C# column: sealed classes with PUBLIC FIELDS (the
// family's C-flavored idiom — SPEC §4.8's standing principle), member names
// via ir.GoExportName — the SAME PascalCase mapping as the Go target, so the
// checker's existing collision detection covers C# members without a second
// registry. Integer families name their storage directly (sbyte..ulong),
// enums are native C# enums with explicit unsigned backing ([max = ...]
// headroom values are representable natively — C# enums are open over their
// backing type, so no newtype is needed), flags-typed fields are plain ulong
// with flat mask constants on Schema (the Go spelling exactly, so the
// registry's existing claims cover them), string(N)/bytes(N) are a
// pre-allocated byte[N] plus an int used length, arrays are pre-allocated
// T[N] with an int used count beside the counted form. Element classes are
// allocated once, at construction — nothing here heap-allocates per message.
//
// C# has no namespace-level functions or constants, so every generated
// function and constant lives on `public static partial class Schema`
// (SPEC §6.1 naming: static class Schema members in namespace <Package>) —
// partial, each file contributing its own declarations' members.
//
// Functions follow the §6.3 C# row: static bool Write*/Read* against the
// sealed WriteStream/ReadStream, C++-style bool early-out on every call,
// counts and lengths checked before the loops and slices they guard. A schema
// validation failure (a wrong wire constant, nonzero reserved bits, an
// interior null) returns false WITHOUT latching; stream failures latch on the
// runtime's sticky stream.Error. The write side leans on the runtime's own
// refusals — serialize.cs refuses out-of-range values on write exactly like
// serialize.go (WriteStream.SerializeInt/SerializeInt64 latch
// ValueOutOfRange), and its raw bit writer masks silently exactly like
// serialize.go's — so the generated write-guard set is the Go target's leaner
// one: only the flags wire-width guard and the full-range-unsigned raw path,
// where the generated code bypasses the ranged calls.
package csharp

import (
	"fmt"
	"maps"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/internal/ast"
	"github.com/mas-bandwidth/schema/ir"
)

// Generate returns basename.cs -> file contents for every file of the unit.
// C# compilations are order-free across files, so like Go there is no topo
// sort and no cross-file include graph to refuse.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	// fixed(I, F), int128 and uint128: serialize.cs carries the full surface
	// on every TFM since the Int128Value/UInt128Value pair landed (2026-08-12,
	// option b) — storage maps to the pair, wire calls mirror the C++ macros.
	out := map[string][]byte{}
	home := protocolIdHome(u)
	msgOwner := ir.MessageOwner(u)
	objOwner := ir.ObjectOwner(u)
	batched, needCore := batchPlan(u)
	bases := map[string]bool{}
	for _, f := range u.Files {
		bases[f.Base] = true
	}
	for _, f := range u.Files {
		if bases[f.Base+"Table"] {
			return nil, fmt.Errorf("schema files %s and %sTable collide — the C# emitter writes %sTable.cs as %s's table codec; rename one file", f.Base, f.Base, f.Base, f.Base)
		}
	}
	for _, f := range u.Files {
		g := &gen{unit: u, file: f, msgOwner: msgOwner, objOwner: objOwner, batched: batched, needCore: needCore}
		g.emitFile(f.Base == home)
		out[f.Base+".cs"] = g.assemble()
	}
	tables, err := GenerateTable(u)
	if err != nil {
		return nil, err
	}
	maps.Copy(out, tables)
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
	owner    string // the class whose members are being emitted (CS0542 escape)

	batched   map[string]bool    // Write/Read pair names whose entry runs a batch
	needCore  map[string]bool    // pair names that get a *Batch core (batched or composed under one)
	inBatch   bool               // emitting a batch-form core: receiver is `batch`, composition by ref
	bulkBytes map[*ir.Field]bool // statically byte-aligned [N]uint8 fields — bulk path (ir.AlignedFixedByteArrays)

	types          strings.Builder // namespace-level declarations (enums, classes)
	schema         strings.Builder // members of the partial static Schema class
	needsSerialize bool            // the file emits wire functions -> using Serialize;
	needsSystem    bool            // the file references System (Array, Math, AsSpan)
	needsCompiler  bool            // the file emits [MethodImpl] -> using System.Runtime.CompilerServices;
}

// rv is the serialize receiver of the function being emitted: the stream, or
// the register-resident batch inside a *Batch core.
func (g *gen) rv() string {
	if g.inBatch {
		return "batch"
	}
	return "stream"
}

// m maps an exported member name into the class currently being emitted. A
// member equal to its enclosing class's name (CS0542) cannot reach here: the
// checker refuses it at the source — no escaping machinery (SPEC §4.6).
func (g *gen) m(exported string) string {
	return exported
}

// fieldBase is a field's C# member name inside the current class.
func (g *gen) fieldBase(f *ir.Field) string {
	return g.m(ir.GoExportName(f.Name))
}

// tf prints into the namespace-level region.
func (g *gen) tf(format string, args ...any) {
	fmt.Fprintf(&g.types, format, args...)
}

// sf prints into the Schema class region (indented at assemble time).
func (g *gen) sf(format string, args ...any) {
	fmt.Fprintf(&g.schema, format, args...)
}

func (g *gen) assemble() []byte {
	var h strings.Builder
	// the basename, not the invocation-relative path: output is deterministic
	// to the byte wherever the compiler runs (SPEC §6.1)
	fmt.Fprintf(&h, "// Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", g.file.Base)
	fmt.Fprintf(&h, "// package %s — protocol id 0x%016x\n", g.unit.Package, g.unit.ProtocolId)
	if g.needsSerialize {
		h.WriteString("//\n")
		h.WriteString("// Storage members are PascalCase via the same mapping as the Go target, so\n")
		h.WriteString("// the checker's collision registry covers C# for free. Wire functions return\n")
		h.WriteString("// bool — the C++-style early-out. A schema validation failure (a wrong wire\n")
		h.WriteString("// constant, nonzero reserved bits, an interior null) returns false WITHOUT\n")
		h.WriteString("// latching; stream failures latch on stream.Error — the runtime's own sticky\n")
		h.WriteString("// latch. Callers get bool always; Error tells the two apart.\n")
	}
	h.WriteString("\n")
	if g.needsSystem {
		h.WriteString("using System;\n")
	}
	if g.needsCompiler {
		h.WriteString("using System.Runtime.CompilerServices;\n")
	}
	if g.needsSerialize {
		h.WriteString("using Serialize;\n")
	}
	if g.needsSystem || g.needsCompiler || g.needsSerialize {
		h.WriteString("\n")
	}
	// block namespace, not file-scoped: Unity's compiler is C# 9 and
	// file-scoped namespaces are C# 10 — the generated code must compile
	// everywhere the game does
	fmt.Fprintf(&h, "namespace %s\n{\n\n", capitalize(g.unit.Package))
	var body strings.Builder
	body.WriteString(g.types.String())
	if g.schema.Len() > 0 {
		body.WriteString("// Schema carries every generated function and constant of the unit — C# has\n")
		body.WriteString("// no namespace-level functions or constants, so the static class is their\n")
		body.WriteString("// home (SPEC §6.1 naming); partial, one slice per generated file.\n")
		body.WriteString("public static partial class Schema\n{\n")
		body.WriteString(indent4(g.schema.String()))
		body.WriteString("}\n")
	}
	h.WriteString(indent4(body.String()))
	h.WriteString("\n}\n")
	return []byte(h.String())
}

// indent4 indents every nonempty line by one level, for the Schema class body.
func indent4(s string) string {
	s = strings.TrimRight(s, "\n")
	var b strings.Builder
	for line := range strings.SplitSeq(s, "\n") {
		if line == "" {
			b.WriteString("\n")
			continue
		}
		b.WriteString("    ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func (g *gen) emitFile(carriesProtocolId bool) {
	if carriesProtocolId {
		g.sf("// The unit's protocol id — the hash of its schema files (SPEC §3.1). Two\n")
		g.sf("// sides at the same id speak identical bits; there is no other versioning.\n")
		g.sf("public const ulong ProtocolId = 0x%016x;\n\n", g.unit.ProtocolId)
	}

	// MessageType / ObjectType lead their OWNER file — the unit-level surface
	// is emitted exactly once, in the topologically last carrying file, so
	// declarations spread across files never redeclare it in the compilation
	// (SPEC §2 keeps the aspect layout non-enforced; ir.MessageOwner picks).
	if g.file.Base == g.msgOwner && len(g.unit.Messages) > 0 {
		g.emitTagEnum("MessageType", g.unit.Messages,
			"the message set, extracted by the compiler — None = 0, then each message sorted by name (SPEC §4.8)")
		g.tf("// Message is the dispatch surface: the abstract base every generated message\n")
		g.tf("// extends. null is None — the stream terminator (SPEC §4.8). Each sealed\n")
		g.tf("// override pins its message's tag; dispatch is a type-pattern switch.\n")
		g.tf("public abstract class Message\n{\n    public abstract MessageType Type { get; }\n}\n\n")
	}
	if g.file.Base == g.objOwner && len(g.unit.ObjNames) > 0 {
		g.emitTagEnum("ObjectType", g.unit.ObjNames,
			"the object set, extracted by the compiler — None = 0, then each object sorted by name (SPEC §4.8)")
	}

	// declaration order — schema references are order-free and so is C#
	for _, d := range g.file.Decls {
		switch d := d.(type) {
		case *ir.Const:
			g.emitConst(d)
		case *ir.Enum:
			g.emitEnum(d)
		case *ir.Flags:
			g.emitFlags(d)
		case *ir.ContextsMarker:
			g.tf("// contexts declared for this unit: %s (SPEC §4.2).\n", strings.Join(d.Names, ", "))
			g.tf("// Contexts generate no standalone artifacts — where an object carries\n")
			g.tf("// context-scoped [local] fields, its State class is generated once per\n")
			g.tf("// context (ClientShipState, ServerShipState, ...), each holding the `all`\n")
			g.tf("// fields plus its own context's. No preprocessor symbols in this target.\n\n")
		case *ir.Struct:
			g.emitClass(d)
			g.emitStructFunctions(d)
		case *ir.Object:
			g.emitObject(d)
			g.emitObjectFunctions(d)
		}
	}

	if g.file.Base == g.msgOwner && len(g.unit.Messages) > 0 {
		g.emitMessageStorage()
		g.emitMessageTagFunctions()
	}
	if g.file.Base == g.objOwner && len(g.unit.ObjNames) > 0 {
		g.emitObjectTagFunctions()
	}
}

// emitTagEnum emits a tag enum with unsigned backing sized to the set.
func (g *gen) emitTagEnum(name string, members []string, comment string) {
	g.tf("// %s: %s\n", name, comment)
	g.tf("public enum %s : %s\n{\n", name, csUint(ir.StorageBitsFor(int64(len(members)))))
	g.tf("    None = 0,\n")
	for i, m := range members {
		g.tf("    %s = %d,\n", m, i+1)
	}
	g.tf("}\n\n")
}

// emitConst emits a schema const on Schema: bare integers are long, bare
// floats double, and an explicitly-typed schema const carries its declared C#
// type (the declared type pins the exported type, SPEC §4.2). Consumers cast
// at the use site where a narrower type is required — C# constants are typed.
func (g *gen) emitConst(d *ir.Const) {
	if d.IsFloat {
		if d.Storage == "float32" {
			g.sf("public const float %s = %s;%s\n\n", d.Name, formatFloat32(d.Float), g.foldComment(d.Expr))
			return
		}
		g.sf("public const double %s = %s;%s\n\n", d.Name, formatFloat(d.Float), g.foldComment(d.Expr))
		return
	}
	typ := csStorage(d.Storage)
	g.sf("public const %s %s = %s;%s\n\n", typ, d.Name, g.renderArg(d.Expr, d.Int, typ, false), g.foldComment(d.Expr))
}

// foldComment returns a trailing comment carrying the schema expression when
// the rendered C# had to fold it (an E.Max reference has no C# twin).
func (g *gen) foldComment(e ast.Expr) string {
	if e != nil && containsMax(e) {
		return fmt.Sprintf(" // = %s", schemaExpr(e))
	}
	return ""
}

func (g *gen) emitEnum(d *ir.Enum) {
	g.tf("// %s — None = 0 implicit, variants dense from 1, wire range [0, %d] (SPEC §4.2);\n", d.Name, d.Max)
	g.tf("// a native enum with unsigned backing — [max = ...] headroom values are\n")
	g.tf("// representable because C# enums are open over their backing type (SPEC §6.1)\n")
	g.tf("public enum %s : %s\n{\n", d.Name, csUint(d.StorageBits))
	g.tf("    None = 0,\n")
	for i, v := range d.Variants {
		g.tf("    %s = %d,\n", v, i+1)
	}
	g.tf("}\n\n")
	// the ulong parameter (not the enum type) keeps out-of-set values exact:
	// a cast through a narrower backing would truncate 256 -> 0 -> "None"
	// for an 8-bit enum
	g.sf("// EnumName%s: debug/log/tooling name for any %s wire value —\n", d.Name, d.Name)
	g.sf("// out-of-set values (wire-legal up to the declared max) name as \"???\"\n")
	g.sf("public static string EnumName%s(ulong value)\n{\n", d.Name)
	g.sf("    switch (value)\n    {\n")
	g.sf("        case (ulong)%s.None:\n            return \"None\";\n", d.Name)
	for _, v := range d.Variants {
		g.sf("        case (ulong)%s.%s:\n            return %q;\n", d.Name, v, v)
	}
	g.sf("        default:\n            return \"???\";\n    }\n}\n\n")
}

func (g *gen) emitFlags(d *ir.Flags) {
	g.sf("// %s — one bit per variant, consumed as masks; flags-typed fields store a\n", d.Name)
	g.sf("// plain ulong, wire %d bits (SPEC §4.2). Masks are flat PascalCase — the Go\n", d.WireBits)
	g.sf("// target's spelling exactly, so the checker's existing claims cover them.\n")
	for i, v := range d.Variants {
		g.sf("public const ulong %s%s = 1ul << %d;\n", d.Name, v, i)
	}
	g.sf("\n")
}

func (g *gen) emitClass(d *ir.Struct) {
	kind := "type"
	if d.IsMessage {
		kind = "message"
	}
	if len(d.Tags) > 0 {
		g.tf("// %s %s [%s] — the tag is user-chosen and inert in v1; the delta pass\n", kind, d.Name, strings.Join(d.Tags, ", "))
		g.tf("// claims tags and assigns actions (SPEC §4.2, Type tags)\n")
	} else {
		g.tf("// %s %s\n", kind, d.Name)
	}
	base := ""
	if d.IsMessage {
		base = " : Message"
	}
	g.owner = d.Name
	g.tf("public sealed class %s%s\n{\n", d.Name, base)
	if d.IsMessage {
		g.tf("    public override MessageType Type => MessageType.%s;\n", d.Name)
		if len(d.Fields) > 0 {
			g.tf("\n")
		}
	}
	g.emitClassFields(d.Fields, storageDeep)
	g.emitElementConstructor(d.Name, d.Fields, storageDeep)
	g.tf("}\n\n")
}

// emitElementConstructor pre-allocates the element classes of struct-typed
// arrays — the storage principle (SPEC §6.1): every buffer exists at
// construction, nothing allocates per message. Scalar arrays and plain
// class-typed members are pre-allocated by their field initializers.
func (g *gen) emitElementConstructor(className string, fields []*ir.Field, v view) {
	var elems []*ir.Field
	for _, f := range fields {
		if g.viewKeepsStorage(f, v) && f.Array != ir.ArrayNone {
			if _, ok := f.Type.Ref.(*ir.Struct); ok && f.Type.Kind == ir.TNamed {
				elems = append(elems, f)
			}
		}
	}
	if len(elems) == 0 {
		return
	}
	g.tf("\n    public %s()\n    {\n", className)
	for _, f := range elems {
		name := g.fieldBase(f)
		g.tf("        for (int i = 0; i < %s.Length; i++)\n        {\n", name)
		g.tf("            %s[i] = new %s();\n        }\n", name, f.Type.Name)
	}
	g.tf("    }\n")
}

// viewKeepsStorage reports whether a field keeps its declared storage under
// the view (quantized composites and projected floats replace theirs).
func (g *gen) viewKeepsStorage(f *ir.Field, v view) bool {
	if v == storageShallow && f.HasQuantize {
		return false
	}
	if (v == storageShallow || v == storageInterp) && f.HasFloatRange {
		return false
	}
	return true
}

// view selects which storage a field emission derives (SPEC §4.8).
type view int

const (
	storageDeep    view = iota // declared storage — State and Data_Deep
	storageShallow             // quantized wire storage
	storageInterp              // interpolate storage: projected fields wire-int, composites continuous
)

func (g *gen) emitObject(d *ir.Object) {
	g.tf("// ---- object %s — one definition, a generated family per target (SPEC §4.8) ----\n\n", d.Name)

	if hasContextFields(d) {
		for _, ctx := range g.unit.Contexts {
			var fields []*ir.Field
			for _, f := range d.Fields {
				if f.Context == "" || f.Context == ctx {
					fields = append(fields, f)
				}
			}
			name := capitalize(ctx) + d.Name + "State"
			g.tf("// %s — the full simulation class for the %s context: every `all`\n", name, ctx)
			g.tf("// field plus the fields scoped [local, context = %s]\n", ctx)
			g.emitViewClass(name, fields, storageDeep)
		}
	} else {
		g.tf("// %sState — the full simulation class: every field\n", d.Name)
		g.emitViewClass(d.Name+"State", d.Fields, storageDeep)
	}

	deep, interp := splitObjectFields(d)

	g.tf("// %sData_Deep — every non-[local] field, deep encodings: full state for\n", d.Name)
	g.tf("// client-side prediction\n")
	g.emitViewClass(d.Name+"Data_Deep", deep, storageDeep)

	g.tf("// %sData_Shallow — the [interpolate] fields on the quantized wire: the\n", d.Name)
	g.tf("// implementation detail on the way to interpolation on the client\n")
	g.emitViewClass(d.Name+"Data_Shallow", interp, storageShallow)

	g.tf("// %sData_Interpolate — the same fields in interpolate storage: projected\n", d.Name)
	g.tf("// fields stay in the wire integer domain and snap-interpolate; quantized\n")
	g.tf("// composites store continuous (SPEC §4.8 rule 5)\n")
	g.emitViewClass(d.Name+"Data_Interpolate", interp, storageInterp)
}

func (g *gen) emitViewClass(name string, fields []*ir.Field, v view) {
	g.owner = name
	g.tf("public sealed class %s\n{\n", name)
	g.emitClassFields(fields, v)
	g.emitElementConstructor(name, fields, v)
	g.tf("}\n\n")
}

func splitObjectFields(d *ir.Object) (deep, interp []*ir.Field) {
	for _, f := range d.Fields {
		if !f.Local {
			deep = append(deep, f)
		}
		if f.Interpolate {
			interp = append(interp, f)
		}
	}
	return
}

func hasContextFields(d *ir.Object) bool {
	for _, f := range d.Fields {
		if f.Context != "" {
			return true
		}
	}
	return false
}

func (g *gen) emitClassFields(fields []*ir.Field, v view) {
	prevGuard := ""
	for _, f := range fields {
		if f.Guard != prevGuard {
			if f.Guard != "" {
				g.tf("\n    // %s — wire branch; storage holds both sides, a read zeroes the\n", f.Guard)
				g.tf("    // untaken side (SPEC §5)\n")
			} else {
				g.tf("\n") // leaving a branch group — separate so membership stays visible
			}
			prevGuard = f.Guard
		}
		g.emitField(f, v)
	}
}

func (g *gen) emitField(f *ir.Field, v view) {
	name := g.fieldBase(f)
	if v == storageShallow && f.HasQuantize {
		st := f.Type.Ref.(*ir.Struct)
		if f.FixedShallow {
			g.tf("    // %s: %s narrowed to %d fractional bits (quantize = %s) — per-component\n",
				f.Name, f.Type.Name, f.QuantShift, schemaExpr(f.QuantScaleExpr))
			g.tf("    // quantized units; bounds are the component's whole-unit [min, max] scaled\n")
			for _, comp := range st.Fields {
				lo, hi, _, typ, _ := fixedShallowComp(f, comp)
				g.tf("    public %s %s; // in [%s, %s]\n", typ, g.m(ir.GoExportName(f.Name)+ir.GoExportName(comp.Name)), lo, hi)
			}
			return
		}
		g.tf("    // %s: %s quantized by %s, max %s — per-component int in [-%d, %d]\n",
			f.Name, f.Type.Name, schemaExpr(f.QuantScaleExpr), schemaExpr(f.QuantMaxExpr), f.QuantBound, f.QuantBound)
		typ := csInt(smallestSigned(f.QuantBound))
		for _, comp := range st.Fields {
			g.tf("    public %s %s;\n", typ, g.m(ir.GoExportName(f.Name)+ir.GoExportName(comp.Name)))
		}
		return
	}
	if (v == storageShallow || v == storageInterp) && f.HasFloatRange {
		typ := csUint(ir.StorageBitsFor(f.Steps))
		note := ""
		if f.Round != "nearest" {
			note = ", round " + f.Round
		}
		tail := ""
		if v == storageInterp {
			tail = " — wire-int domain, snap-interpolated (SPEC §4.8 rule 5)"
		}
		g.tf("    public %s %s; // float [%s, %s] @ resolution %s -> wire int [0, %d]%s%s\n",
			typ, name, formatFloat(f.FMin), formatFloat(f.FMax),
			formatFloat(f.Resolution), f.Steps, note, tail)
		return
	}
	g.emitStorageField(f)
}

func (g *gen) emitStorageField(f *ir.Field) {
	name := g.fieldBase(f)
	typ := g.csFieldType(f.Type)

	switch {
	case f.Type.Kind == ir.TString:
		g.tf("    public byte[] %s = new byte[%s]; // string(%s): max length, used length beside it (SPEC §4.7)\n",
			name, g.renderArg(f.Type.SizeExpr, big.NewInt(f.Type.Size), "", true), schemaExpr(f.Type.SizeExpr))
		g.tf("    public int %s;\n", g.m(name+"Length"))
	case f.Type.Kind == ir.TBytes:
		g.tf("    public byte[] %s = new byte[%s]; // bytes(%s): fixed buffer, used length beside it (SPEC §4.7)\n",
			name, g.renderArg(f.Type.SizeExpr, big.NewInt(f.Type.Size), "", true), schemaExpr(f.Type.SizeExpr))
		g.tf("    public int %s;\n", g.m(name+"Length"))
	case f.Array == ir.ArrayFixed:
		g.tf("    public %s[] %s = new %s[%s];%s\n",
			typ, name, typ, g.renderArg(f.ArrayExpr, big.NewInt(f.ArrayBound), "", true), g.fieldComment(f))
	case f.Array == ir.ArrayCounted:
		g.tf("    public %s[] %s = new %s[%s]; // used count beside it; wire count in [%d, %s]\n",
			typ, name, typ, g.renderArg(f.ArrayExpr, big.NewInt(f.ArrayBound), "", true), f.ArrayMin, schemaExpr(f.ArrayExpr))
		g.tf("    public int %s;\n", g.m(name+"Count"))
	default:
		init := ""
		if f.HasDefault {
			init = " = " + g.defaultValue(f, true)
		} else if _, isStruct := f.Type.Ref.(*ir.Struct); isStruct && f.Type.Kind == ir.TNamed {
			// pre-allocated at construction — the storage principle (SPEC §6.1)
			init = " = new " + f.Type.Name + "()"
		}
		g.tf("    public %s %s%s;%s\n", typ, name, init, g.fieldComment(f))
	}
}

func (g *gen) fieldComment(f *ir.Field) string {
	var parts []string
	if f.Type.Kind == ir.TNamed {
		if _, isFlags := f.Type.Ref.(*ir.Flags); isFlags {
			parts = append(parts, fmt.Sprintf("%s — consumed as masks, ulong storage (SPEC §4.2)", f.Type.Name))
		}
	}
	if f.HasDefault {
		parts = append(parts, fmt.Sprintf("= %s at construction; Zero* gives the §5 zero form", g.defaultValue(f, false)))
	}
	if f.HasIntRange {
		parts = append(parts, fmt.Sprintf("wire [%s, %s]", f.IntMin, f.IntMax))
	}
	if f.HasFloatRange {
		note := ""
		if f.Round != "nearest" {
			note = ", round " + f.Round
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

func (g *gen) defaultValue(f *ir.Field, qualified bool) string {
	switch {
	case f.Type.Kind == ir.TBool:
		return fmt.Sprintf("%v", f.DefBool)
	case f.DefVariant != "":
		return f.Type.Name + "." + f.DefVariant
	case f.Type.Kind == ir.TFloat32:
		return formatFloat32(f.DefFloat)
	case f.Type.Kind == ir.TFloat64:
		return formatFloat(f.DefFloat)
	case f.Type.Kind == ir.TInt && f.Type.Width == 128:
		// 128-bit defaults compose through the pair — a bare decimal literal
		// past 64 bits is not a legal C# constant
		if f.Type.Signed {
			return csRender128(f.DefInt)
		}
		return csRenderU128(f.DefInt)
	default:
		// the initializer target is the field's own storage type, so a
		// symbolic long const must cast down to it
		return g.renderArg(f.DefExpr, f.DefInt, g.csFieldType(f.Type), qualified)
	}
}

// csFieldType maps a field type to its C# storage (SPEC §6.1).
func (g *gen) csFieldType(t ir.FieldType) string {
	switch t.Kind {
	case ir.TInt:
		if t.Width == 128 {
			// the emulated pair (serialize.cs Int128Pair.cs): full surface on
			// every TFM, implicit System.Int128 conversions on .NET 7+
			if t.Signed {
				return "Int128Value"
			}
			return "UInt128Value"
		}
		if t.Signed {
			return csInt(t.Width)
		}
		return csUint(t.Width)
	case ir.TFixed:
		// raw scaled integer in storage of exactly I+F bits, the type's own
		// signedness — serialize's fixed storage convention (STANDARD.md,
		// fixed); SerializeFixed overload resolution picks the unsigned
		// codec from the unsigned storage type
		if t.Width == 128 {
			if t.Signed {
				return "Int128Value"
			}
			return "UInt128Value"
		}
		if t.Signed {
			return csInt(t.Width)
		}
		return csUint(t.Width)
	case ir.TBits:
		if t.Width <= 32 {
			return "uint"
		}
		return "ulong"
	case ir.TBool:
		return "bool"
	case ir.TFloat32:
		return "float"
	case ir.TFloat64:
		return "double"
	case ir.TNamed:
		if _, isFlags := t.Ref.(*ir.Flags); isFlags {
			return "ulong" // §6.1: flags-typed fields store plain ulong
		}
		return t.Name
	}
	return "/* ? */"
}

// csStorage maps a schema storage name (explicit const types) to C#.
func csStorage(s string) string {
	switch s {
	case "int8":
		return "sbyte"
	case "int16":
		return "short"
	case "int32":
		return "int"
	case "int64":
		return "long"
	case "uint8":
		return "byte"
	case "uint16":
		return "ushort"
	case "uint32":
		return "uint"
	case "uint64":
		return "ulong"
	case "float32":
		return "float"
	case "float64":
		return "double"
	}
	return "/* ? */"
}

// renderArg renders an integer expression for a Schema-interior context
// requiring the C# type typ ("" = a context with no required type, e.g. an
// array size — no cast). Folded values and literal-only expressions are
// emitted as bare literals (C# constant conversions apply when the value
// fits); a symbolic form referencing BARE (untyped-in-schema, long-in-C#)
// constants casts to the required type — the same renderability rule as the
// Go and Rust targets, so all three fold identically.
func (g *gen) renderArg(e ast.Expr, folded *big.Int, typ string, qualified bool) string {
	if e == nil || containsMax(e) || !g.renderable(e) || !g.overflowSafe(e) {
		return folded.String()
	}
	if !containsIdent(e) {
		return renderExpr(e, qualified)
	}
	s := renderExpr(e, qualified)
	if typ == "" || typ == "long" {
		return s // bare consts are long already
	}
	if _, ok := e.(*ast.IdentExpr); ok {
		return "(" + typ + ")" + s
	}
	return "(" + typ + ")(" + s + ")"
}

// renderScaleF64 renders a quantization scale in double arithmetic: symbolic
// consts cast ((double)PositionUnits), folded values become double literals.
func (g *gen) renderScaleF64(e ast.Expr, folded int64) string {
	if e == nil || containsMax(e) || !g.renderable(e) || !containsIdent(e) {
		return strconv.FormatInt(folded, 10) + ".0"
	}
	s := renderExpr(e, false)
	if _, ok := e.(*ast.IdentExpr); ok {
		return "(double)" + s
	}
	return "(double)(" + s + ")"
}

// renderScaleF32 is renderScaleF64's float twin, for float component division.
func (g *gen) renderScaleF32(e ast.Expr, folded int64) string {
	if e == nil || containsMax(e) || !g.renderable(e) || !containsIdent(e) {
		return strconv.FormatInt(folded, 10) + ".0f"
	}
	s := renderExpr(e, false)
	if _, ok := e.(*ast.IdentExpr); ok {
		return "(float)" + s
	}
	return "(float)(" + s + ")"
}

// overflowSafe reports whether e can render symbolically without C#'s
// CHECKED constant arithmetic rejecting it. Schema folding is
// arbitrary-precision; C# literal subtrees evaluate in int, so
// `7 * 700000000` is a CS0220 compile error even though the product fits the
// long bound it feeds (found by FuzzGeneratedCompiles, issue #22 — the C++
// twin of this gate). A subtree referencing a constant evaluates in long.
// Anything unprovable folds — folding is always correct.
func (g *gen) overflowSafe(e ast.Expr) bool {
	_, _, ok := g.carrierEval(e)
	return ok
}

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
			v.Quo(x, y) // truncation toward zero, as C# divides
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

// fitsCarrier: a subtree with a constant reference evaluates in long; a
// literal-only subtree evaluates in int, conservatively taken as 32-bit.
func fitsCarrier(v *big.Int, wide bool) bool {
	if wide {
		return v.IsInt64()
	}
	return v.IsInt64() && v.Int64() >= math.MinInt32 && v.Int64() <= math.MaxInt32
}

func (g *gen) renderable(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.IdentExpr:
		c, ok := g.unit.Consts[e.Name]
		return ok && !c.IsFloat && !c.Explicit
	case *ast.UnaryExpr:
		return g.renderable(e.X)
	case *ast.BinaryExpr:
		return g.renderable(e.X) && g.renderable(e.Y)
	case *ast.ParenExpr:
		return g.renderable(e.X)
	}
	return true
}

// renderExpr renders an expression in C# form. qualified prefixes constant
// references with "Schema." for render sites outside the Schema class (class
// field initializers) — inside Schema the bare name resolves.
func renderExpr(e ast.Expr, qualified bool) string {
	switch e := e.(type) {
	case *ast.IntLit:
		return e.Text
	case *ast.FloatLit:
		return e.Text
	case *ast.IdentExpr:
		if qualified {
			return "Schema." + e.Name
		}
		return e.Name
	case *ast.UnaryExpr:
		inner := renderExpr(e.X, qualified)
		if strings.HasPrefix(inner, "-") {
			// "--x" is decrement in C#, not double negation (issue #22)
			return "-(" + inner + ")"
		}
		return "-" + inner
	case *ast.BinaryExpr:
		return fmt.Sprintf("%s %s %s", renderExpr(e.X, qualified), e.Op, renderExpr(e.Y, qualified))
	case *ast.ParenExpr:
		return "(" + renderExpr(e.X, qualified) + ")"
	}
	return "?"
}

// schemaExpr renders an expression in schema source form, for comments.
func schemaExpr(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.IntLit:
		return e.Text
	case *ast.FloatLit:
		return e.Text
	case *ast.IdentExpr:
		return e.Name
	case *ast.MaxExpr:
		return e.Enum + ".Max"
	case *ast.UnaryExpr:
		return "-" + schemaExpr(e.X)
	case *ast.BinaryExpr:
		return fmt.Sprintf("%s %s %s", schemaExpr(e.X), e.Op, schemaExpr(e.Y))
	case *ast.ParenExpr:
		return "(" + schemaExpr(e.X) + ")"
	}
	return "?"
}

func containsMax(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.MaxExpr:
		return true
	case *ast.UnaryExpr:
		return containsMax(e.X)
	case *ast.BinaryExpr:
		return containsMax(e.X) || containsMax(e.Y)
	case *ast.ParenExpr:
		return containsMax(e.X)
	}
	return false
}

func containsIdent(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.IdentExpr:
		return true
	case *ast.UnaryExpr:
		return containsIdent(e.X)
	case *ast.BinaryExpr:
		return containsIdent(e.X) || containsIdent(e.Y)
	case *ast.ParenExpr:
		return containsIdent(e.X)
	}
	return false
}

// csRender128 renders a 128-bit bound as C# source. Values inside long ride
// the pair's implicit conversion; wider values compose the two's-complement
// 64-bit lanes through the (hi, lo) constructor.
func csRender128(v *big.Int) string {
	i64lo := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 63))
	i64hi := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 63), big.NewInt(1))
	if v.Cmp(i64lo) >= 0 && v.Cmp(i64hi) <= 0 {
		if v.Cmp(big.NewInt(-2147483648)) >= 0 && v.Cmp(big.NewInt(2147483647)) <= 0 {
			return v.String()
		}
		return v.String() + "L"
	}
	u := new(big.Int).Set(v)
	if u.Sign() < 0 {
		u.Add(u, new(big.Int).Lsh(big.NewInt(1), 128))
	}
	hi := new(big.Int).Rsh(u, 64)
	lo := new(big.Int).And(u, maxUint64)
	return fmt.Sprintf("new Int128Value(0x%xul, 0x%xul)", hi, lo)
}

// csRenderU128 is csRender128 for the unsigned domain: values inside ulong
// ride the pair's implicit conversion; wider values compose the lanes.
func csRenderU128(v *big.Int) string {
	if v.Cmp(maxUint64) <= 0 {
		if v.Cmp(big.NewInt(2147483647)) <= 0 {
			return v.String()
		}
		return v.String() + "ul"
	}
	hi := new(big.Int).Rsh(v, 64)
	lo := new(big.Int).And(v, maxUint64)
	return fmt.Sprintf("new UInt128Value(0x%xul, 0x%xul)", hi, lo)
}

func csInt(width int) string {
	switch width {
	case 8:
		return "sbyte"
	case 16:
		return "short"
	case 32:
		return "int"
	}
	return "long"
}

func csUint(width int) string {
	switch width {
	case 8:
		return "byte"
	case 16:
		return "ushort"
	case 32:
		return "uint"
	}
	return "ulong"
}

// fixedShallowComp resolves one component of a narrowed fixed composite
// (SPEC §4.8 rule 2b) to its C# shallow shape: wire bounds, the int/long
// serialize switch, and the storage type. The bounds mirror
// ir.FixedShallowBounds so all five backends agree on the wire.
func fixedShallowComp(f, cf *ir.Field) (lo, hi *big.Int, wide bool, typ string, width int) {
	lo, hi = ir.FixedShallowBounds(f, cf)
	wide = hi.Cmp(big.NewInt(2147483647)) > 0 || lo.Cmp(big.NewInt(-2147483648)) < 0
	abs := new(big.Int).Neg(lo)
	if abs.Cmp(hi) < 0 {
		abs = hi
	}
	bound := int64(9223372036854775807)
	if abs.IsInt64() {
		bound = abs.Int64()
	}
	width = smallestSigned(bound)
	typ = csInt(width)
	return
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

func formatFloat(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// formatFloat32 renders a float literal for a float argument position with
// the explicit suffix, so the type never rests on inference.
func formatFloat32(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 32)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s + "f"
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
