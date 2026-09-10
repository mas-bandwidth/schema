// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3: the Java writer,
// the Java reader and the LAYOUT.
//
// A fixed-table record is an eight-byte hash of the writer's LAYOUT and then
// the values in declared order, every field at its DECLARED STORAGE WIDTH,
// nothing padded between fields. So:
//
//	THE WRITER is the type's constant bytes laid down — the hash, then zeros —
//	and then value stores at constant offsets. The body's size is a CONSTANT
//	of the type, so there is no measuring pass, no id interning, no trailer
//	and no second walk.
//
//	THE READER IS ONE PLAN-DRIVEN PATH. A read is a prefill and one loop over
//	one plan: the IDENTITY PLAN when the record's hash is this build's own,
//	and a plan compiled ONCE from the writer's own layout otherwise. There is
//	no second reader and no fast/slow cliff — the owner's own ruling, which
//	§3.4 quotes him on.
//
// THIS IS THE PACKET CODEC'S SHAPE AND NOT THE VARIABLE FORM'S. Java's packet
// wire is a value class of public fields beside static `write`/`read`/`measure`
// functions over a caller-owned byte[], with every word moved through a
// VarHandle at LITTLE_ENDIAN. This form is that, with byte-width stores in
// place of the bitpacker's shifts. Nothing here reads a field reference, a
// kind byte, a length or a trailer: form 1's accelerators keep those and this
// form has none of them.
//
// THE PLAN'S DESTINATION IS THE RECORD IMAGE. C++ points a plan at a struct's
// own bytes because there its storage image IS its wire image; Java cannot —
// a `string(N)` is a byte[] beside an int and an array of records is an array
// of references — so the plan lands into a byte[] laid out exactly as THIS
// reader's own body, and one generated straight line (`scatter`) lands that
// image into the value. The identity plan is then ONE copy of the whole body,
// which is exactly what §3.4's coalescing promises, and the same loop and the
// same scatter run for every writer.
package javatable

import (
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixedKindOptional is the ONE kind §3.4's layout adds to §3's closed set: the
// OPTIONAL WRAPPER, one child, whose size is one present byte plus its
// child's. It is ir's, beside the rest of the kind vocabulary, because it is
// wire law and not this backend's.
const fixedKindOptional = ir.TableKindOptional

// THE CONSTANT SIZE, KIND BY KIND, IS ir's (docs/SPEC-TABLES.md §3.4). It is
// wire law and not this backend's arithmetic: the C++ reference emits its
// layout and its template from the same functions and the compiler warns and
// refuses from them, so the reference's bytes and this port's cannot drift.
const (
	fixedCountBytes   = ir.TableFixedCountBytes
	fixedPresentBytes = ir.TableFixedPresentBytes
)

func fixedStorageBytes(t ir.FieldType) int64 { return ir.TableFixedStorageBytes(t) }
func fixedTypeBytes(st *ir.Struct) int64     { return ir.TableFixedTypeBytes(st) }
func fixedFieldBytes(f *ir.Field) int64      { return ir.TableFixedFieldBytes(f) }
func fixedElementBytes(f *ir.Field) int64    { return ir.TableFixedElementBytes(f) }

// fixedSupported reports whether a type's whole closure is one this port
// carries, and it is the C++ REFERENCE's own answer so that the two emit the
// form for the same roots. A pointer, a map and an unbounded array make their
// holder VARIABLE and never reach here; what this adds is the two shapes the
// reference does not lay out — a union arm carrying text or an array, and a
// guarded branch, which §3.4 refuses outright.
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
// THE LAYOUT
// ---------------------------------------------------------------------------

// fixedLayoutEntry is one seventeen-byte entry: an id, a kind, a constant size
// and a child count, in the writer's declared order (docs/SPEC-TABLES.md §3.4).
type fixedLayoutEntry struct {
	id       uint64
	kind     int
	size     int64
	children int
	counted  bool   // MY side: does a live count ride in front of this array
	note     string // a comment on the emitted array; never a wire byte
}

type fixedWalk struct{ entries []fixedLayoutEntry }

func (w *fixedWalk) push(e fixedLayoutEntry) { w.entries = append(w.entries, e) }

func fixedWalkRoot(st *ir.Struct) *fixedWalk {
	w := &fixedWalk{}
	w.push(fixedLayoutEntry{id: ir.TableWireId(st.WireName()), kind: ir.TableKindTable,
		size: fixedTypeBytes(st), children: len(st.Fields), note: st.Name})
	for _, f := range st.Fields {
		fixedWalkField(w, f)
	}
	return w
}

func fixedWalkField(w *fixedWalk, f *ir.Field) {
	id := ir.TableFieldWireId(f)
	size := fixedFieldBytes(f)
	if f.Type.Optional {
		w.push(fixedLayoutEntry{id: id, kind: fixedKindOptional, size: size, children: 1, note: f.Name + " ?"})
		fixedWalkPayload(w, f, id, size-fixedPresentBytes)
		return
	}
	fixedWalkPayload(w, f, id, size)
}

func fixedWalkPayload(w *fixedWalk, f *ir.Field, id uint64, size int64) {
	switch {
	case f.KeyEnum != "":
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindKeyed, size: size, children: 2, note: f.Name})
		fixedWalkEnum(w, f.KeyEnumRef, ir.TableWireId(f.KeyEnum), f.KeyEnum)
		fixedWalkElement(w, f, 0, "element")
	case f.Array != ir.ArrayNone:
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindArray, size: size, children: 1,
			counted: f.Array == ir.ArrayCounted, note: f.Name})
		fixedWalkElement(w, f, 0, "element")
	case f.Type.Kind == ir.TBytes:
		// `bytes(N)` is an ARRAY of u8 on this wire, as it is in §3, and a
		// LIVE COUNT rides in front of it: the used length.
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindArray, size: size, children: 1, counted: true, note: f.Name})
		w.push(fixedLayoutEntry{id: 0, kind: ir.TableKindU8, size: 1, children: 0, note: "u8"})
	case f.Type.Kind == ir.TString:
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindString, size: size, children: 0, note: f.Name})
	case f.Type.Kind == ir.TWString:
		w.push(fixedLayoutEntry{id: id, kind: ir.TableKindWstring, size: size, children: 0, note: f.Name})
	default:
		fixedWalkElement(w, f, id, f.Name)
	}
}

func fixedWalkElement(w *fixedWalk, f *ir.Field, id uint64, note string) {
	size := fixedElementBytes(f)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			w.push(fixedLayoutEntry{id: id, kind: ir.TableKindTable, size: size, children: len(r.Fields), note: note})
			for _, sub := range r.Fields {
				fixedWalkField(w, sub)
			}
			return
		case *ir.Union:
			w.push(fixedLayoutEntry{id: id, kind: ir.TableKindUnion, size: size, children: len(r.Variants), note: note})
			for _, v := range r.Variants {
				fixedWalkElement(w, v.F, ir.TableWireId(v.WireName()), v.Name)
			}
			return
		case *ir.Enum:
			fixedWalkEnum(w, r, id, note)
			return
		}
	}
	w.push(fixedLayoutEntry{id: id, kind: ir.TableWireScalarKind(f), size: size, children: 0, note: note})
}

func fixedWalkEnum(w *fixedWalk, e *ir.Enum, id uint64, note string) {
	w.push(fixedLayoutEntry{id: id, kind: ir.TableKindEnum, size: int64(e.StorageBits / 8),
		children: len(e.Variants), note: note})
	for i := range e.Variants {
		w.push(fixedLayoutEntry{id: ir.TableWireId(e.VariantWireName(i)), kind: ir.TableKindNoPayload,
			size: 0, children: 0, note: e.Variants[i]})
	}
}

// fixedLayoutBytes is the LAYOUT exactly as it rides: a u32 entry count and a
// run of seventeen-byte entries, every number little-endian. THERE IS NO
// VERSION BYTE IN FRONT OF THE COUNT — the FORM BYTE versions the layout's
// format, so a layout format change is a new form byte (§3.4).
func fixedLayoutBytes(entries []fixedLayoutEntry) []byte {
	out := make([]byte, 0, ir.TableFixedLayoutHeaderBytes+ir.TableFixedEntryBytes*len(entries))
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

// fixedLayoutHash is fnv1a64 over the layout's bytes exactly as written, and
// it is the eight bytes every record carries (§3.4).
func fixedLayoutHash(layout []byte) uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, b := range layout {
		h ^= uint64(b)
		h *= 0x100000001b3
	}
	return h
}

// ---------------------------------------------------------------------------
// THE PREFILL: the declared defaults, as a record image
// ---------------------------------------------------------------------------
//
// C++ prefills by calling the type's own Reset. Java's plan lands into a
// record image, so the prefill is that image: the declared defaults laid out
// exactly as the body is, baked as a constant of the type. It is the ONE place
// a declared default reaches this form, and it is what answers "absent field"
// — a field this record does not carry has no plan entry at all, so the
// default the prefill put there is what the scatter reads.

func fixedDefaultImage(st *ir.Struct) []byte {
	out := make([]byte, fixedTypeBytes(st))
	fixedDefaultType(out, st)
	return out
}

func fixedDefaultType(out []byte, st *ir.Struct) {
	at := int64(0)
	for _, f := range st.Fields {
		fixedDefaultField(out[at:at+fixedFieldBytes(f)], f)
		at += fixedFieldBytes(f)
	}
}

func fixedDefaultField(out []byte, f *ir.Field) {
	if f.Type.Optional {
		// a fresh optional is ABSENT, and its payload still rides whole
		out = out[fixedPresentBytes:]
	}
	switch {
	case f.KeyEnum != "":
		fixedDefaultSlots(out, f, f.KeyEnumRef.Max)
	case f.Array == ir.ArrayFixed:
		fixedDefaultSlots(out, f, f.ArrayBound)
	case f.Array == ir.ArrayCounted:
		// the born count is the declared minimum (ir.Field.BornCount)
		fixedPutU32(out, uint32(f.BornCount()))
		fixedDefaultSlots(out[fixedCountBytes:], f, f.ArrayBound)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		fixedPutU32(out, uint32(len(f.DefBytes)))
		copy(out[fixedCountBytes:], f.DefBytes)
	case f.Type.Kind == ir.TWString:
		// a wstring's declared default is code units, which no declaration
		// spells today; the length rides and the units stay zero
		fixedPutU32(out, uint32(len(f.DefBytes)))
	default:
		fixedDefaultElement(out, f)
	}
}

func fixedDefaultSlots(out []byte, f *ir.Field, count int64) {
	elem := fixedElementBytes(f)
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
			// a defaulted enum field rides its variant's ORDINAL, from 1; 0 is
			// None, which is also the zero image
			if f.HasDefault && f.DefVariant != "" {
				for i, v := range r.Variants {
					if v == f.DefVariant {
						fixedPutUint(out, uint64(i+1), fixedStorageBytes(f.Type))
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
		fixedPutBig(out, f.DefInt, fixedStorageBytes(f.Type))
	}
}

func fixedPutU32(out []byte, v uint32) { fixedPutUint(out, uint64(v), 4) }

func fixedPutUint(out []byte, v uint64, width int64) {
	for i := int64(0); i < width && i < 8; i++ {
		out[i] = byte(v >> (8 * i))
	}
}

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
// THE CLOSURE
// ---------------------------------------------------------------------------

// fixedRoots is every table of the unit the fixed form is emitted for, in
// declaration order. ir answers the size ceiling — a record past 65536 bytes
// is one no conforming reader decodes, so the table keeps form 1 and the
// compiler names it — and fixedSupported answers the shapes the reference does
// not lay out.
func fixedRoots(u *ir.Unit) []*ir.Struct {
	var out []*ir.Struct
	for _, st := range ir.TableFixedFormRoots(u) {
		if !fixedSupported(st, 0) {
			continue
		}
		out = append(out, st)
	}
	return out
}

// fixedCollectTypes orders a root's closure so a nested type is emitted before
// the type that holds it.
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

// fixedUnions is every union the closure reaches, in a stable order: each gets
// a value class of its own, because a union's arms are storage this port has
// to name.
func fixedUnions(order []*ir.Struct) []*ir.Union {
	seen := map[string]bool{}
	var out []*ir.Union
	for _, st := range order {
		for _, f := range st.Fields {
			if f.Type.Kind != ir.TNamed {
				continue
			}
			if r, ok := f.Type.Ref.(*ir.Union); ok && !seen[r.Name] {
				seen[r.Name] = true
				out = append(out, r)
			}
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// THE JAVA SURFACE
// ---------------------------------------------------------------------------

// fixedClass is the class one type's fixed-form surface lands in: `<Type>Fixed`
// in a file of its own name, which is what every other Java table surface does
// (`<Table>Row`, `<Table>Block`, `<Table>Cook`). One public class per file is
// Java's own rule, and a package-scope name needs no qualification anywhere in
// the unit, so a nested type is emitted ONCE, beside the type it names, and
// referred to by that name from every holder.
func fixedClass(name string) string { return ir.GoExportName(name) + "Fixed" }

// fixedValue is the spelling of one type's VALUE: a nested class, so a field
// named `hash` and the type's own `hash` constant never meet.
func fixedValue(name string) string { return fixedClass(name) + ".Value" }

type fixedGen struct {
	unit *ir.Unit
	b    strings.Builder
}

func (g *fixedGen) pf(format string, args ...any) { fmt.Fprintf(&g.b, format, args...) }

// generateFixedFiles emits `<Type>Fixed.java` for every type of every fixed
// root's closure, and the shared runtime beside them. A unit with no fixed
// root gets neither: nothing would be in them but a refusal nobody calls.
func generateFixedFiles(u *ir.Unit) (map[string][]byte, error) {
	roots := fixedRoots(u)
	if len(roots) == 0 {
		return map[string][]byte{}, nil
	}
	seen := map[string]bool{}
	var order []*ir.Struct
	for _, st := range roots {
		fixedCollectTypes(st, seen, &order)
	}
	isRoot := map[string]bool{}
	for _, st := range roots {
		isRoot[st.Name] = true
	}
	out := map[string][]byte{
		"TableFixed.java": javaFile(u, "the FIXED FORM's runtime (docs/SPEC-TABLES.md §3.4), form byte 3", tableFixedRuntime),
	}
	for _, un := range fixedUnions(order) {
		g := &fixedGen{unit: u}
		g.emitUnion(un)
		out[fixedClass(un.Name)+".java"] = javaFile(u,
			fmt.Sprintf("union %s under the fixed form (docs/SPEC-TABLES.md §3.4)", un.Name), g.b.String())
	}
	for _, st := range order {
		g := &fixedGen{unit: u}
		g.emitType(st, isRoot[st.Name])
		out[fixedClass(st.Name)+".java"] = javaFile(u,
			fmt.Sprintf("%s under the fixed form (docs/SPEC-TABLES.md §3.4)", st.Name), g.b.String())
	}
	return out, nil
}

// fixedNames is every class name this form claims for a unit, which is what
// the file-collision refusal is checked against.
func fixedNames(u *ir.Unit) []string {
	roots := fixedRoots(u)
	if len(roots) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var order []*ir.Struct
	for _, st := range roots {
		fixedCollectTypes(st, seen, &order)
	}
	out := []string{"TableFixed"}
	for _, un := range fixedUnions(order) {
		out = append(out, fixedClass(un.Name))
	}
	for _, st := range order {
		out = append(out, fixedClass(st.Name))
	}
	sort.Strings(out)
	return out
}

// ---- the storage spellings, which are the packet codec's own --------------

// fixedScalarType is a leaf's Java storage type: the same-width SIGNED type,
// bit-transparent, which is the packet emitter's rule and the reason an
// unsigned value is masked rather than widened.
func fixedScalarType(t ir.FieldType) string {
	switch t.Kind {
	case ir.TBool:
		return "boolean"
	case ir.TFloat32:
		return "float"
	case ir.TFloat64:
		return "double"
	case ir.TBits:
		if t.Width <= 32 {
			return "int"
		}
		return "long"
	case ir.TInt, ir.TFixed:
		if t.Width == 128 {
			if t.Signed {
				return "Int128"
			}
			return "UInt128"
		}
		return fixedIntType(t.Width)
	case ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Enum:
			return fixedIntType(r.StorageBits)
		case *ir.Flags:
			return "long" // the raw mask, as §3 carries it
		case *ir.Struct:
			return fixedValue(t.Name)
		case *ir.Union:
			return fixedValue(t.Name)
		}
	}
	return "long"
}

func fixedIntType(width int) string {
	switch {
	case width <= 8:
		return "byte"
	case width <= 16:
		return "short"
	case width <= 32:
		return "int"
	}
	return "long"
}

// fixedIsObject reports a storage type Java allocates rather than stores flat,
// which is what a constructor has to fill: an array of them starts null.
func fixedIsObject(t ir.FieldType) bool {
	switch fixedScalarType(t) {
	case "Int128", "UInt128":
		return true
	}
	if t.Kind == ir.TNamed {
		switch t.Ref.(type) {
		case *ir.Struct, *ir.Union:
			return true
		}
	}
	return false
}

// emitType writes one type's whole fixed-form surface.
func (g *fixedGen) emitType(st *ir.Struct, root bool) {
	cls := fixedClass(st.Name)
	body := fixedTypeBytes(st)

	g.pf("// %s under the FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3.\n", st.Name)
	g.pf("//\n")
	if root {
		g.pf("// A ROOT: a file is the form byte, the layout, then the records back to\n")
		g.pf("// back to the end of it, each an eight-byte hash and a body of values in\n")
		g.pf("// declared order at their declared storage widths.\n")
	} else {
		g.pf("// A NESTED type: it rides INLINE inside its holder's body, at its own\n")
		g.pf("// constant size, with no length and no terminator.\n")
	}
	g.pf("public final class %s {\n", cls)
	g.pf("    private %s() {}\n\n", cls)

	g.emitValueClass(st)
	g.emitBodyConstant(st, body)
	g.emitWriteBody(st)
	g.emitScatter(st)
	if root {
		g.emitRoot(st, body)
	}
	g.pf("}\n")
}

// emitValueClass is the storage, and it is the packet codec's own storage:
// public fields, `final` pre-allocated arrays and objects, a `<name>Length`
// beside every text buffer and a `<name>Count` beside every counted array.
func (g *fixedGen) emitValueClass(st *ir.Struct) {
	g.pf("    /** %s's storage: public fields, every buffer allocated at construction,\n", st.Name)
	g.pf("     *  which is the packet codec's own shape. Declared defaults are here and\n")
	g.pf("     *  nowhere else. */\n")
	g.pf("    public static final class Value {\n")
	var ctor []string
	for _, f := range st.Fields {
		ctor = append(ctor, g.emitStorageField(f)...)
	}
	if len(ctor) > 0 {
		g.pf("\n        /** pre-allocated element instances — Java object arrays start null. */\n")
		g.pf("        public Value() {\n")
		for _, line := range ctor {
			g.pf("%s", line)
		}
		g.pf("        }\n")
	}
	g.pf("    }\n\n")
}

func (g *fixedGen) emitStorageField(f *ir.Field) []string {
	name := javaName(f.Name)
	var ctor []string
	if f.Type.Optional {
		g.pf("        /** `?%s`: the present flag; the payload rides WHOLE either way (§3.4). */\n", f.Name)
		g.pf("        public boolean %sPresent;\n", name)
	}
	switch {
	case f.Type.Kind == ir.TWString && f.Array == ir.ArrayNone && f.KeyEnum == "":
		g.pf("        /** wstring(%d): UTF-16 code units, used length beside them. */\n", f.Type.Size)
		g.pf("        public final char[] %s = new char[%d];\n", name, f.Type.Size)
		g.pf("        public int %sLength;\n", name)
		return nil
	case (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes) && f.Array == ir.ArrayNone && f.KeyEnum == "":
		g.pf("        /** %s(%d): a fixed buffer, used length beside it. */\n", ir.TableTypeSpelling(f), f.Type.Size)
		g.pf("        public final byte[] %s = new byte[%d];\n", name, f.Type.Size)
		if len(f.DefBytes) > 0 {
			g.pf("        public int %sLength = %d;\n", name, len(f.DefBytes))
			ctor = append(ctor, fmt.Sprintf("            %s;\n", fixedByteDefaultExpr(name, f.DefBytes)))
		} else {
			g.pf("        public int %sLength;\n", name)
		}
		return ctor
	}
	slots, _ := g.fixedSlots(f)
	if slots > 0 {
		typ := fixedScalarType(f.Type)
		g.pf("        /** %s */\n", ir.FieldTypeSpelling(f))
		g.pf("        public final %s[] %s = new %s[%d];\n", typ, name, typ, slots)
		if fixedIsObject(f.Type) {
			if typ == "Int128" || typ == "UInt128" {
				ctor = append(ctor, fmt.Sprintf("            java.util.Arrays.fill(%s, %s.zero);\n", name, typ))
			} else {
				ctor = append(ctor,
					fmt.Sprintf("            for (int i = 0; i < %s.length; i++) {\n", name),
					fmt.Sprintf("                %s[i] = new %s();\n", name, typ),
					"            }\n")
			}
		} else if init := fixedScalarDefault(f); init != "" {
			// EVERY SLOT TAKES THE DECLARED DEFAULT, keyed or positional: the
			// prefill image says the same thing, and the two have to agree or a
			// fresh value and a defaulted read would differ.
			ctor = append(ctor, fmt.Sprintf("            java.util.Arrays.fill(%s, %s);\n", name, init))
		}
		if f.Array == ir.ArrayCounted {
			if n := f.BornCount(); n > 0 {
				g.pf("        public int %sCount = %d;\n", name, n)
			} else {
				g.pf("        public int %sCount;\n", name)
			}
		}
		return ctor
	}
	typ := fixedScalarType(f.Type)
	g.pf("        /** %s */\n", ir.FieldTypeSpelling(f))
	if fixedIsObject(f.Type) {
		if typ == "Int128" || typ == "UInt128" {
			g.pf("        public %s %s = %s.zero;\n", typ, name, typ)
		} else {
			g.pf("        public final %s %s = new %s();\n", typ, name, typ)
		}
		return nil
	}
	if init := fixedScalarDefault(f); init != "" {
		g.pf("        public %s %s = %s;\n", typ, name, init)
	} else {
		g.pf("        public %s %s;\n", typ, name)
	}
	return nil
}

// fixedSlots answers how many elements a field's storage holds, and whether
// the field is enum-keyed.
func (g *fixedGen) fixedSlots(f *ir.Field) (int64, bool) {
	switch {
	case f.KeyEnum != "":
		return f.KeyEnumRef.Max, true
	case f.Array != ir.ArrayNone:
		return f.ArrayBound, false
	}
	return 0, false
}

// fixedScalarDefault renders a leaf's declared default as a Java literal.
func fixedScalarDefault(f *ir.Field) string {
	if f.Type.Kind == ir.TNamed {
		if r, ok := f.Type.Ref.(*ir.Enum); ok {
			if f.HasDefault && f.DefVariant != "" {
				for i, v := range r.Variants {
					if v == f.DefVariant {
						return fixedNarrowLit(fixedIntType(r.StorageBits), big.NewInt(int64(i+1)))
					}
				}
			}
			return ""
		}
		if _, ok := f.Type.Ref.(*ir.Flags); ok {
			if f.HasDefault && f.DefInt != nil {
				return fixedNarrowLit("long", f.DefInt)
			}
			return ""
		}
	}
	if !f.HasDefault {
		return ""
	}
	switch f.Type.Kind {
	case ir.TBool:
		if f.DefBool {
			return "true"
		}
		return ""
	case ir.TFloat32:
		return fixedFloatLit(float32(f.DefFloat))
	case ir.TFloat64:
		return fixedDoubleLit(f.DefFloat)
	}
	if f.DefInt == nil {
		return ""
	}
	return fixedNarrowLit(fixedScalarType(f.Type), f.DefInt)
}

func fixedNarrowLit(typ string, v *big.Int) string {
	switch typ {
	case "byte":
		return fmt.Sprintf("(byte) %d", int8(v.Int64()))
	case "short":
		return fmt.Sprintf("(short) %d", int16(v.Int64()))
	case "int":
		return fmt.Sprintf("%d", int32(v.Int64()))
	case "long":
		return fmt.Sprintf("%dL", v.Int64())
	case "Int128", "UInt128":
		lo, hi := fixed128Halves(v)
		return fmt.Sprintf("new %s(%sL, %sL)", typ, fixedHexLong(hi), fixedHexLong(lo))
	}
	return v.String()
}

func fixed128Halves(v *big.Int) (lo, hi uint64) {
	n := new(big.Int).Set(v)
	if n.Sign() < 0 {
		n.Add(n, new(big.Int).Lsh(big.NewInt(1), 128))
	}
	mask := new(big.Int).SetUint64(^uint64(0))
	lo = new(big.Int).And(n, mask).Uint64()
	hi = new(big.Int).And(new(big.Int).Rsh(n, 64), mask).Uint64()
	return lo, hi
}

func fixedHexLong(v uint64) string {
	if v == 0 {
		return "0"
	}
	return fmt.Sprintf("0x%x", v)
}

func fixedFloatLit(v float32) string {
	switch {
	case math.IsNaN(float64(v)):
		return "Float.NaN"
	case math.IsInf(float64(v), 1):
		return "Float.POSITIVE_INFINITY"
	case math.IsInf(float64(v), -1):
		return "Float.NEGATIVE_INFINITY"
	}
	return fmt.Sprintf("Float.intBitsToFloat(%s)", fixedHexInt(math.Float32bits(v)))
}

func fixedDoubleLit(v float64) string {
	switch {
	case math.IsNaN(v):
		return "Double.NaN"
	case math.IsInf(v, 1):
		return "Double.POSITIVE_INFINITY"
	case math.IsInf(v, -1):
		return "Double.NEGATIVE_INFINITY"
	}
	return fmt.Sprintf("Double.longBitsToDouble(%sL)", fixedHexLong(math.Float64bits(v)))
}

func fixedHexInt(v uint32) string {
	if v == 0 {
		return "0"
	}
	return fmt.Sprintf("0x%x", v)
}

func fixedByteDefaultExpr(name string, b []byte) string {
	var sb strings.Builder
	for i, v := range b {
		if i > 0 {
			sb.WriteString("; ")
		}
		fmt.Fprintf(&sb, "%s[%d] = (byte) %d", name, i, int8(v))
	}
	return sb.String()
}

// ---- the constants --------------------------------------------------------

func (g *fixedGen) emitBodyConstant(st *ir.Struct, body int64) {
	g.pf("    /** THE BODY IS A CONSTANT SIZE, for every value this type can hold: the\n")
	g.pf("     *  measure is a constant and not a function that reads the value (§3.4). */\n")
	g.pf("    public static final int bodyBytes = %d;\n\n", body)
}

// ---- the writer -----------------------------------------------------------

func (g *fixedGen) emitWriteBody(st *ir.Struct) {
	cls := fixedClass(st.Name)
	g.pf("    /** %s's stores. The caller lays the template down first — the hash, then\n", st.Name)
	g.pf("     *  zeros — which is also what zero-fills every byte of declared slack, so\n")
	g.pf("     *  this is a straight line of byte-width stores at constant offsets and\n")
	g.pf("     *  nothing else. */\n")
	g.pf("    public static void writeBody(byte[] b, int at, Value v) {\n")
	g.pf("        assert checkWrite(v);\n")
	off := int64(0)
	for _, f := range st.Fields {
		g.emitWriteField(f, off, "b", "at", "v", 8)
		off += fixedFieldBytes(f)
	}
	g.pf("    }\n\n")
	g.emitCheckWrite(st, cls)
}

// emitCheckWrite is the writer's contract walk, called once through `assert` —
// the packet codec's own predicate-extraction form. THE WRITE-SIDE CHECKS ARE
// DEBUG ONLY: a release build compiles them out and the writer trusts, which
// is the family rule everywhere.
func (g *fixedGen) emitCheckWrite(st *ir.Struct, cls string) {
	var lines []string
	for _, f := range st.Fields {
		name := javaName(f.Name)
		switch {
		case (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString) &&
			f.Array == ir.ArrayNone && f.KeyEnum == "":
			lines = append(lines, fmt.Sprintf("        assert v.%sLength >= 0 && v.%sLength <= %d;\n", name, name, f.Type.Size))
		case f.Array == ir.ArrayCounted:
			lines = append(lines, fmt.Sprintf("        assert v.%sCount >= %d && v.%sCount <= %d;\n",
				name, f.ArrayMin, name, f.ArrayBound))
		}
	}
	g.pf("    // checkWrite is writeBody's contract walk, called once through `assert`.\n")
	g.pf("    // THE WRITE-SIDE CHECKS ARE DEBUG ONLY: without -ea the writer trusts,\n")
	g.pf("    // which is what makes a save one template and a straight line of stores.\n")
	g.pf("    private static boolean checkWrite(Value v) {\n")
	if len(lines) == 0 {
		g.pf("        assert v != null;\n")
	}
	for _, l := range lines {
		g.pf("%s", l)
	}
	g.pf("        return true;\n    }\n\n")
	_ = cls
}

func (g *fixedGen) emitWriteField(f *ir.Field, off int64, buf, at, val string, indent int) {
	if f.Type.Optional {
		// AN ABSENT OPTIONAL'S PAYLOAD IS THE TEMPLATE'S ZEROS
		// (docs/SPEC-TABLES.md §3.4): the payload rides WHOLE whether or not it
		// is present, and when the flag is 0 what rides is zero. It is ONE `if`
		// and no rule anywhere else, because the template already put the zeros
		// there — so an absent optional costs the writer a branch and not one
		// store, and a caller's untouched payload storage never reaches the
		// wire. The C++ reference makes the same branch in the same place.
		ind := strings.Repeat(" ", indent)
		g.pf("%sTableFixed.put8(%s, %s + %d, %s.%sPresent ? 1 : 0);\n", ind, buf, at, off, val, javaName(f.Name))
		g.pf("%sif (%s.%sPresent) {\n", ind, val, javaName(f.Name))
		g.emitWritePayload(f, off+fixedPresentBytes, buf, at, val, indent+4)
		g.pf("%s}\n", ind)
		return
	}
	g.emitWritePayload(f, off, buf, at, val, indent)
}

// emitWritePayload is the field's payload stores at the base given — the whole
// of emitWriteField for a plain field, and the guarded half for an optional.
func (g *fixedGen) emitWritePayload(f *ir.Field, base int64, buf, at, val string, indent int) {
	ind := strings.Repeat(" ", indent)
	name := javaName(f.Name)
	switch {
	case f.KeyEnum != "":
		g.emitWriteLoop(f, base, f.KeyEnumRef.Max, buf, at, val+"."+name, indent)
	case f.Array == ir.ArrayFixed:
		g.emitWriteLoop(f, base, f.ArrayBound, buf, at, val+"."+name, indent)
	case f.Array == ir.ArrayCounted:
		g.pf("%sTableFixed.put32(%s, %s + %d, %s.%sCount);\n", ind, buf, at, base, val, name)
		g.emitWriteLoop(f, base+fixedCountBytes, f.ArrayBound, buf, at, val+"."+name, indent)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		// TEXT WRITES ITS LENGTH UNITS onto the zeroed template: the slack past
		// the used bytes is already zero and the writer never touches it.
		g.pf("%sTableFixed.put32(%s, %s + %d, %s.%sLength);\n", ind, buf, at, base, val, name)
		g.pf("%sSystem.arraycopy(%s.%s, 0, %s, %s + %d, %s.%sLength);\n", ind, val, name, buf, at, base+4, val, name)
	case f.Type.Kind == ir.TWString:
		g.pf("%sTableFixed.put32(%s, %s + %d, %s.%sLength);\n", ind, buf, at, base, val, name)
		g.pf("%sfor (int i = 0; i < %s.%sLength; i++) {\n", ind, val, name)
		g.pf("%s    TableFixed.put16(%s, %s + %d + i * 2, %s.%s[i]);\n", ind, buf, at, base+4, val, name)
		g.pf("%s}\n", ind)
	default:
		g.emitWriteElement(f, base, buf, at, val+"."+name, indent)
	}
}

func (g *fixedGen) emitWriteLoop(f *ir.Field, base, count int64, buf, at, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	elem := fixedElementBytes(f)
	g.pf("%sfor (int i = 0; i < %d; i++) {\n", ind, count)
	g.emitWriteElement(f, 0, buf, fmt.Sprintf("%s + %d + i * %d", at, base, elem), expr+"[i]", indent+4)
	g.pf("%s}\n", ind)
}

func (g *fixedGen) emitWriteElement(f *ir.Field, off int64, buf, at, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			g.pf("%s%s.writeBody(%s, %s + %d, %s);\n", ind, fixedClass(r.Name), buf, at, off, expr)
			return
		case *ir.Union:
			g.pf("%s%s.writeBody(%s, %s + %d, %s);\n", ind, fixedClass(r.Name), buf, at, off, expr)
			return
		case *ir.Enum:
			g.pf("%sTableFixed.putUint(%s, %s + %d, %s, %d);\n", ind, buf, at, off, fixedWiden(expr, fixedIntType(r.StorageBits)), r.StorageBits/8)
			return
		case *ir.Flags:
			g.pf("%sTableFixed.put64(%s, %s + %d, %s);\n", ind, buf, at, off, expr)
			return
		}
	}
	w := fixedStorageBytes(f.Type)
	switch f.Type.Kind {
	case ir.TBool:
		g.pf("%sTableFixed.put8(%s, %s + %d, %s ? 1 : 0);\n", ind, buf, at, off, expr)
	case ir.TFloat32:
		g.pf("%sTableFixed.put32(%s, %s + %d, Float.floatToRawIntBits(%s));\n", ind, buf, at, off, expr)
	case ir.TFloat64:
		g.pf("%sTableFixed.put64(%s, %s + %d, Double.doubleToRawLongBits(%s));\n", ind, buf, at, off, expr)
	default:
		if w == 16 {
			// the LOW 64-bit half then the HIGH, the type wire's own order (§3)
			g.pf("%sTableFixed.put64(%s, %s + %d, %s.lo);\n", ind, buf, at, off, expr)
			g.pf("%sTableFixed.put64(%s, %s + %d, %s.hi);\n", ind, buf, at, off+8, expr)
			return
		}
		g.pf("%sTableFixed.putUint(%s, %s + %d, %s, %d);\n", ind, buf, at, off, fixedWiden(expr, fixedScalarType(f.Type)), w)
	}
}

// fixedWiden masks a narrow signed storage type up to the `long` the store
// takes, which is the packet codec's rule: unsigned values ride bit-transparent
// in the same-width signed type, so the mask goes on after the widening.
func fixedWiden(expr, typ string) string {
	switch typ {
	case "byte":
		return "(" + expr + ") & 0xffL"
	case "short":
		return "(" + expr + ") & 0xffffL"
	case "int":
		return "(" + expr + ") & 0xffffffffL"
	}
	return expr
}

// ---- the reader's straight line -------------------------------------------

// emitScatter lands ONE record image into the value. It is the ONLY place this
// port's storage spelling meets the wire's, which is what keeps the plan free
// of storage offsets — and it runs identically whether the plan was the
// identity plan or one compiled from a stranger's layout.
func (g *fixedGen) emitScatter(st *ir.Struct) {
	g.pf("    /** land one record image into a value. The image is this reader's own\n")
	g.pf("     *  body layout, so every offset here is a constant. */\n")
	g.pf("    public static void scatter(byte[] b, int at, Value v, TableFixed.Report r) {\n")
	off := int64(0)
	for _, f := range st.Fields {
		g.emitScatterField(f, off, "b", "at", "v", "r", 8)
		off += fixedFieldBytes(f)
	}
	if len(st.Fields) == 0 {
		g.pf("        // an empty body: presence is the payload\n")
	}
	g.pf("    }\n\n")
}

func (g *fixedGen) emitScatterField(f *ir.Field, off int64, buf, at, val, rep string, indent int) {
	ind := strings.Repeat(" ", indent)
	name := javaName(f.Name)
	base := off
	if f.Type.Optional {
		// BOOLS LAND AS `byte != 0`, whatever the byte holds
		g.pf("%s%s.%sPresent = %s[%s + %d] != 0;\n", ind, val, name, buf, at, base)
		base += fixedPresentBytes
	}
	switch {
	case f.KeyEnum != "":
		g.emitScatterLoop(f, base, f.KeyEnumRef.Max, buf, at, val+"."+name, rep, indent)
	case f.Array == ir.ArrayFixed:
		g.emitScatterLoop(f, base, f.ArrayBound, buf, at, val+"."+name, rep, indent)
	case f.Array == ir.ArrayCounted:
		g.emitScatterCount(fmt.Sprintf("%s.%sCount", val, name), buf, at, base, f.ArrayBound, rep, indent)
		g.emitScatterLoop(f, base+fixedCountBytes, f.ArrayBound, buf, at, val+"."+name, rep, indent)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		g.emitScatterCount(fmt.Sprintf("%s.%sLength", val, name), buf, at, base, f.Type.Size, rep, indent)
		g.pf("%sSystem.arraycopy(%s, %s + %d, %s.%s, 0, %s.%sLength);\n", ind, buf, at, base+4, val, name, val, name)
	case f.Type.Kind == ir.TWString:
		g.emitScatterCount(fmt.Sprintf("%s.%sLength", val, name), buf, at, base, f.Type.Size, rep, indent)
		g.pf("%sfor (int i = 0; i < %s.%sLength; i++) {\n", ind, val, name)
		g.pf("%s    %s.%s[i] = (char) TableFixed.get16(%s, %s + %d + i * 2);\n", ind, val, name, buf, at, base+4)
		g.pf("%s}\n", ind)
	default:
		g.emitScatterElement(f, base, buf, at, val+"."+name, rep, indent, f)
	}
}

// emitScatterCount is §3.4's `count` op, inline: a count or a length is read
// and CLAMPED to this reader's own bound, counting one `clamped` if it fired.
// The plan's own count op has already done it for a compiled plan; this is
// what holds the IDENTITY path to the same rule, because a record under this
// reader's own hash can still carry a count this build does not admit.
func (g *fixedGen) emitScatterCount(dst, buf, at string, off, bound int64, rep string, indent int) {
	ind := strings.Repeat(" ", indent)
	g.pf("%s{\n", ind)
	g.pf("%s    int n = TableFixed.get32(%s, %s + %d);\n", ind, buf, at, off)
	g.pf("%s    if (n < 0) { n = 0; %s.clamped++; } else if (n > %d) { n = %d; %s.clamped++; }\n",
		ind, rep, bound, bound, rep)
	g.pf("%s    %s = n;\n", ind, dst)
	g.pf("%s}\n", ind)
}

func (g *fixedGen) emitScatterLoop(f *ir.Field, base, count int64, buf, at, expr, rep string, indent int) {
	ind := strings.Repeat(" ", indent)
	elem := fixedElementBytes(f)
	g.pf("%sfor (int i = 0; i < %d; i++) {\n", ind, count)
	g.emitScatterElement(f, 0, buf, fmt.Sprintf("%s + %d + i * %d", at, base, elem), expr+"[i]", rep, indent+4, f)
	g.pf("%s}\n", ind)
}

func (g *fixedGen) emitScatterElement(f *ir.Field, off int64, buf, at, expr, rep string, indent int, owner *ir.Field) {
	ind := strings.Repeat(" ", indent)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			g.pf("%s%s.scatter(%s, %s + %d, %s, %s);\n", ind, fixedClass(r.Name), buf, at, off, expr, rep)
			return
		case *ir.Union:
			g.pf("%s%s.scatter(%s, %s + %d, %s, %s);\n", ind, fixedClass(r.Name), buf, at, off, expr, rep)
			return
		case *ir.Enum:
			// AN ORDINAL PAST THE LAST VARIANT LANDS None (0) AND COUNTS ONE
			// CLAMPED, which is the same answer the compiled plan's ordinal
			// op lands (§3.4's one read): the ordinal is the variant's
			// POSITION, so a value past the last one names nothing here.
			g.pf("%s{\n", ind)
			g.pf("%s    long q = TableFixed.getUint(%s, %s + %d, %d);\n", ind, buf, at, off, r.StorageBits/8)
			g.pf("%s    if (q > %dL) { q = 0L; %s.clamped++; }\n", ind, len(r.Variants), rep)
			g.pf("%s    %s = %s;\n", ind, expr, fixedNarrow(fixedIntType(r.StorageBits), "q"))
			g.pf("%s}\n", ind)
			return
		case *ir.Flags:
			g.pf("%s%s = TableFixed.get64(%s, %s + %d);\n", ind, expr, buf, at, off)
			return
		}
	}
	w := fixedStorageBytes(f.Type)
	switch f.Type.Kind {
	case ir.TBool:
		// A BOOL LANDS AS `byte != 0`: a peer's 2 is true and never damage.
		g.pf("%s%s = %s[%s + %d] != 0;\n", ind, expr, buf, at, off)
		return
	case ir.TFloat32:
		g.pf("%s%s = Float.intBitsToFloat(TableFixed.get32(%s, %s + %d));\n", ind, expr, buf, at, off)
		return
	case ir.TFloat64:
		g.pf("%s%s = Double.longBitsToDouble(TableFixed.get64(%s, %s + %d));\n", ind, expr, buf, at, off)
		return
	}
	if w == 16 {
		typ := "UInt128"
		if f.Type.Signed {
			typ = "Int128"
		}
		g.pf("%s%s = new %s(TableFixed.get64(%s, %s + %d), TableFixed.get64(%s, %s + %d));\n",
			ind, expr, typ, buf, at, off+8, buf, at, off)
		return
	}
	typ := fixedScalarType(f.Type)
	raw := fmt.Sprintf("TableFixed.getUint(%s, %s + %d, %d)", buf, at, off, w)
	if f.Type.Signed || f.Type.Kind == ir.TFixed && f.Type.Signed {
		raw = fmt.Sprintf("TableFixed.getInt(%s, %s + %d, %d)", buf, at, off, w)
	}
	// A RANGED INTEGER CLAMPS ON LOAD AND COUNTS (docs/SPEC-TABLES.md §3.4's
	// constant-size table, §4): the bounds do not ride, so a peer that widened
	// them lands a value this reader does not admit and the reader cuts it to
	// its own.
	if lo, hi, ok := fixedIntRange(owner); ok && typ != "Int128" && typ != "UInt128" {
		g.pf("%s{\n", ind)
		g.pf("%s    long q = %s;\n", ind, raw)
		g.pf("%s    if (q < %sL) { q = %sL; %s.clamped++; } else if (q > %sL) { q = %sL; %s.clamped++; }\n",
			ind, lo, lo, rep, hi, hi, rep)
		g.pf("%s    %s = %s;\n", ind, expr, fixedNarrow(typ, "q"))
		g.pf("%s}\n", ind)
		return
	}
	g.pf("%s%s = %s;\n", ind, expr, fixedNarrow(typ, raw))
}

// fixedIntRange answers a plain integer field's declared bounds as Java `long`
// literals. Only the integer kinds: a fixed-point field's bounds are whole
// units against a raw scaled wire, and a float's are a different rule, so
// neither clamps here.
func fixedIntRange(f *ir.Field) (string, string, bool) {
	if f == nil || !f.HasIntRange || f.IntMin == nil || f.IntMax == nil {
		return "", "", false
	}
	switch f.Type.Kind {
	case ir.TInt, ir.TBits:
	default:
		return "", "", false
	}
	if f.Type.Width > 64 || !f.IntMin.IsInt64() || !f.IntMax.IsInt64() {
		return "", "", false
	}
	return f.IntMin.String(), f.IntMax.String(), true
}

func fixedNarrow(typ, expr string) string {
	switch typ {
	case "byte", "short", "int":
		return "(" + typ + ") (" + expr + ")"
	}
	return expr
}

// ---- the union's own class ------------------------------------------------

func (g *fixedGen) emitUnion(un *ir.Union) {
	cls := fixedClass(un.Name)
	tag := int64(ir.StorageBitsFor(un.Max) / 8)
	widest := int64(0)
	for _, v := range un.Variants {
		if v.F == nil {
			continue
		}
		if n := fixedFieldBytes(v.F); n > widest {
			widest = n
		}
	}
	g.pf("// union %s under the FIXED FORM (docs/SPEC-TABLES.md §3.4): the TAG at its\n", un.Name)
	g.pf("// own storage width, then the WIDEST ARM. Tag 0 is None, as an enum's\n")
	g.pf("// ordinal 0 is; the slack behind a narrower arm is zero on write and\n")
	g.pf("// IGNORED on read, never a refusal.\n")
	g.pf("public final class %s {\n", cls)
	g.pf("    private %s() {}\n\n", cls)
	g.pf("    /** the tag: None, then each arm in declared order from 1. */\n")
	g.pf("    public static final int none = 0;\n")
	for i, v := range un.Variants {
		g.pf("    public static final int %s = %d;\n", javaName(v.Name), i+1)
	}
	g.pf("\n    /** the tag's own storage width, in bytes. */\n")
	g.pf("    public static final int tagBytes = %d;\n", tag)
	g.pf("    /** the tag, then the widest arm. */\n")
	g.pf("    public static final int bodyBytes = %d;\n\n", tag+widest)

	g.pf("    /** %s's storage: the tag beside one pre-allocated arm per variant,\n", un.Name)
	g.pf("     *  which is the packet codec's own union shape. */\n")
	g.pf("    public static final class Value {\n")
	g.pf("        public int type = none;\n")
	var ctor []string
	for _, v := range un.Variants {
		if v.F == nil {
			continue
		}
		ctor = append(ctor, g.emitStorageField(v.F)...)
	}
	if len(ctor) > 0 {
		g.pf("\n        /** pre-allocated arms — Java object fields start null. */\n")
		g.pf("        public Value() {\n")
		for _, line := range ctor {
			g.pf("%s", line)
		}
		g.pf("        }\n")
	}
	g.pf("    }\n\n")

	g.pf("    /** the tag, then the SELECTED arm; the template's zeros are the slack. */\n")
	g.pf("    public static void writeBody(byte[] b, int at, Value v) {\n")
	g.pf("        TableFixed.putUint(b, at, v.type, tagBytes);\n")
	g.pf("        switch (v.type) {\n")
	for i, v := range un.Variants {
		if v.F == nil {
			continue
		}
		g.pf("            case %d: {\n", i+1)
		g.emitWriteElement(v.F, 0, "b", fmt.Sprintf("at + %d", tag), "v."+javaName(v.F.Name), 16)
		g.pf("                break;\n            }\n")
	}
	g.pf("            default: break;\n")
	g.pf("        }\n    }\n\n")

	g.pf("    /** the tag, then the selected arm; an unselected arm keeps what it held. */\n")
	g.pf("    public static void scatter(byte[] b, int at, Value v, TableFixed.Report r) {\n")
	g.pf("        v.type = (int) TableFixed.getUint(b, at, tagBytes);\n")
	g.pf("        // A TAG PAST THE LAST ARM LANDS None AND COUNTS ONE CLAMPED: it\n")
	g.pf("        // is a value this reader's own storage does not admit, and\n")
	g.pf("        // landing it raw would hand a caller a `type` that names no arm\n")
	g.pf("        // and that its own switch cannot answer.\n")
	g.pf("        if (v.type > %d) { v.type = none; r.clamped++; }\n", len(un.Variants))
	g.pf("        switch (v.type) {\n")
	for i, v := range un.Variants {
		if v.F == nil {
			continue
		}
		g.pf("            case %d: {\n", i+1)
		g.emitScatterElement(v.F, 0, "b", fmt.Sprintf("at + %d", tag), "v."+javaName(v.F.Name), "r", 16, v.F)
		g.pf("                break;\n            }\n")
	}
	g.pf("            default: break;\n")
	g.pf("        }\n    }\n")
	g.pf("}\n")
}

// ---- the root's surface ---------------------------------------------------

func (g *fixedGen) emitRoot(st *ir.Struct, body int64) {
	w := fixedWalkRoot(st)
	layout := fixedLayoutBytes(w.entries)
	hash := fixedLayoutHash(layout)
	defaults := fixedDefaultImage(st)

	g.pf("    /** the hash and the body: what one record occupies. */\n")
	g.pf("    public static final int recordBytes = 8 + bodyBytes;\n\n")

	g.pf("    /** fnv1a64 over the layout's bytes exactly as written, and the eight\n")
	g.pf("     *  bytes every record carries. A WIRE IDENTITY and not a security claim. */\n")
	g.pf("    public static final long hash = 0x%016xL;\n\n", hash)

	g.pf("    /** THE LAYOUT (form 1 called this the vocabulary block): %d entries, a\n", len(w.entries))
	g.pf("     *  PRE-ORDER walk of this type's closure in declared order — a u32 entry\n")
	g.pf("     *  count and a run of seventeen-byte entries, every number little-endian.\n")
	g.pf("     *  Every byte is settled by the compiler and it is sent ONCE. */\n")
	g.emitByteConstant("layout", layout, w.entries)

	g.pf("    /** THE PREFILL: this type's declared defaults, laid out as a body. A\n")
	g.pf("     *  field a record does not carry has no plan entry at all, so what the\n")
	g.pf("     *  scatter reads there is what this put there. */\n")
	g.emitByteConstant("defaults", defaults, nil)

	g.pf("    /** MY side of the layout, one byte per entry: does a LIVE COUNT ride in\n")
	g.pf("     *  front of this array. It is the one storage fact a layout entry cannot\n")
	g.pf("     *  state and the plan compiler needs — a bare `[N]T` and a counted\n")
	g.pf("     *  `[..N]T` are otherwise the same shape. */\n")
	g.pf("    public static final byte[] counted = {\n")
	g.emitCountedArray(w.entries)
	g.pf("    };\n\n")

	g.pf("    /** everything a FILE carries once, in front of the records: §3's own\n")
	g.pf("     *  sixteen-byte header, then the u32 layout length, then the layout. */\n")
	g.pf("    public static final int headerBytes = TableFixed.fileHeaderBytes + 4 + %d;\n\n", len(layout))

	g.pf("    // THE FILE HEADER LIVES BEHIND THIS FUNCTION AND TableFixed.readHeader AND\n")
	g.pf("    // NOWHERE ELSE, so the day §3 moves a byte of it there is one place on\n")
	g.pf("    // each side of this port to move.\n")
	g.pf("    //\n")
	g.pf("    //     offset  0        the form byte, 3\n")
	g.pf("    //     offsets 1 .. 7   reserved, zero\n")
	g.pf("    //     offsets 8 .. 15  the LAYOUT HASH (u64 LE)\n")
	g.pf("    //     offset  16       the layout length (u32 LE), then the layout\n")
	g.pf("    /** write the header at the front of buffer; answers where the records start. */\n")
	g.pf("    public static int writeHeader(byte[] buffer) {\n")
	g.pf("        java.util.Arrays.fill(buffer, 0, TableFixed.fileHeaderBytes, (byte) 0); // the seven reserved bytes, and the rest\n")
	g.pf("        buffer[0] = TableFixed.form;\n")
	g.pf("        TableFixed.put64(buffer, TableFixed.hashAt, hash);\n")
	g.pf("        TableFixed.put32(buffer, TableFixed.fileHeaderBytes, layout.length);\n")
	g.pf("        System.arraycopy(layout, 0, buffer, TableFixed.fileHeaderBytes + 4, layout.length);\n")
	g.pf("        return headerBytes;\n")
	g.pf("    }\n\n")

	g.pf("    /** A FILE: the form byte, the layout, then the records to the end of it. */\n")
	g.pf("    public static int measure(int count) {\n")
	g.pf("        return headerBytes + count * recordBytes;\n")
	g.pf("    }\n\n")

	g.pf("    /** THE WRITE: the header once, then per record the hash, the template's\n")
	g.pf("     *  zeros and a straight line of stores. There is no measuring pass, no id\n")
	g.pf("     *  interning, no trailer and no second walk. Answers the bytes written,\n")
	g.pf("     *  or -1 when the caller's buffer cannot hold them. */\n")
	g.pf("    public static int save(Value[] values, int count, byte[] buffer) {\n")
	g.pf("        if (values == null || buffer == null || count < 0 || count > values.length) { return -1; }\n")
	g.pf("        final int need = measure(count);\n")
	g.pf("        if (buffer.length < need) { return -1; }\n")
	g.pf("        int at = writeHeader(buffer);\n")
	g.pf("        for (int k = 0; k < count; k++) {\n")
	g.pf("            TableFixed.put64(buffer, at, hash);\n")
	g.pf("            java.util.Arrays.fill(buffer, at + 8, at + recordBytes, (byte) 0); // the template's zeros\n")
	g.pf("            writeBody(buffer, at + 8, values[k]);\n")
	g.pf("            at += recordBytes;\n")
	g.pf("        }\n")
	g.pf("        return need;\n")
	g.pf("    }\n\n")

	g.pf("    // THE IDENTITY PLAN, and it is a CONSTANT of this reader: in the image\n")
	g.pf("    // domain the declared order IS the destination's, so §3.4's coalescing\n")
	g.pf("    // takes the whole body to ONE move. Published by class initialization.\n")
	g.pf("    private static final class Identity {\n")
	g.pf("        static final TableFixed.Entry[] plan = build();\n\n")
	g.pf("        private static TableFixed.Entry[] build() {\n")
	g.pf("            TableFixed.Entry[] p = TableFixed.plan(1);\n")
	g.pf("            p[0].size = bodyBytes;\n")
	g.pf("            return p;\n")
	g.pf("        }\n")
	g.pf("    }\n\n")

	g.pf("    /** the plan a record under THIS build's own hash is read through. */\n")
	g.pf("    public static TableFixed.Entry[] identityPlan() { return Identity.plan; }\n\n")

	g.pf("    /** a scratch image of one record body, allocated ONCE by the caller. */\n")
	g.pf("    public static byte[] image() { return new byte[bodyBytes]; }\n\n")

	g.pf("    /** THE READ: a prefill and ONE loop over ONE plan — the identity plan\n")
	g.pf("     *  when the layout's hash is this build's own, and a plan compiled once\n")
	g.pf("     *  from the writer's own layout otherwise. Same loop, same scatter, same\n")
	g.pf("     *  cost either way (§3.4). Answers the records read, or -1.\n")
	g.pf("     *\n")
	g.pf("     *  NOTHING PER RECORD ALLOCATES, which is the claim the batch loop\n")
	g.pf("     *  rests on: EVERY BUFFER IS THE CALLER'S — the plan, the ordinal\n")
	g.pf("     *  remap scratch and the record image — and the loop below only\n")
	g.pf("     *  fills them. What this DOES make is a fixed handful of cursors,\n")
	g.pf("     *  once per call: the header below, and on the compiled path the two\n")
	g.pf("     *  Layout views (a byte[] reference and two ints each, over the\n")
	g.pf("     *  caller's own bytes) that TableFixed.parse returns. None escapes\n")
	g.pf("     *  this method, and none is per record. */\n")
	g.pf("    public static int load(Value[] values, int count, byte[] data,\n")
	g.pf("                           TableFixed.Entry[] plan, short[] remap, byte[] image,\n")
	g.pf("                           TableFixed.Report report) {\n")
	g.pf("        final TableFixed.Header head = new TableFixed.Header();\n")
	g.pf("        if (!TableFixed.readHeader(data, head, report)) { return -1; }\n")
	g.pf("        TableFixed.Entry[] entries;\n")
	g.pf("        int entryCount;\n")
	g.pf("        long recordSize = recordBytes;\n")
	g.pf("        if (head.hash == hash) {\n")
	g.pf("            entries = identityPlan();\n")
	g.pf("            entryCount = 1;\n")
	g.pf("        } else {\n")
	g.pf("            // ANOTHER WRITER: the same loop, over a plan compiled from its\n")
	g.pf("            // layout. THE LAYOUT IS VALIDATED BEFORE A SINGLE RECORD BYTE IS\n")
	g.pf("            // TOUCHED, and every rule it fails refuses under ITS OWN NAME.\n")
	g.pf("            final TableFixed.Layout theirs = TableFixed.parse(data, head.layoutAt, head.layoutLength, report);\n")
	g.pf("            if (theirs == null) { return -1; }\n")
	g.pf("            final TableFixed.Layout mine = TableFixed.parse(layout, 0, layout.length, report);\n")
	g.pf("            if (mine == null) { return -1; }\n")
	g.pf("            final int made = TableFixed.compile(theirs, mine, counted, plan, remap, report);\n")
	g.pf("            if (made < 0) { report.refuse(TableFixed.Reason.planTooLarge); return -1; }\n")
	g.pf("            entries = plan;\n")
	g.pf("            entryCount = made;\n")
	g.pf("            recordSize = 8 + theirs.size(0);\n")
	g.pf("        }\n")
	g.pf("        // THE HEADER NAMES THE LAYOUT ONCE, and it is checked LAST of the\n")
	g.pf("        // three: the layout's own rules each refuse under their own name\n")
	g.pf("        // first, so a broken layout is never reported as a lying header (§3).\n")
	g.pf("        if (head.declaredHash != head.hash) { report.refuse(TableFixed.Reason.layoutMalformed); return -1; }\n")
	g.pf("        final long rest = data.length - head.recordsAt;\n")
	g.pf("        if (recordSize <= 8 || rest %% recordSize != 0) { report.malformed = true; return -1; }\n")
	g.pf("        final long n = rest / recordSize;\n")
	g.pf("        if (n > count || n > values.length) { report.refuse(TableFixed.Reason.batchTooLarge); return -1; }\n")
	g.pf("        int at = head.recordsAt;\n")
	g.pf("        final int bodySize = (int) (recordSize - 8);\n")
	g.pf("        for (int k = 0; k < (int) n; k++) {\n")
	g.pf("            // A RECORD WHOSE HASH NAMES NO LAYOUT THIS READER HOLDS IS A\n")
	g.pf("            // REFUSAL BY NAME, never a guess and never damage.\n")
	g.pf("            if (TableFixed.get64(data, at) != head.hash) { report.refuse(TableFixed.Reason.noLayout); return -1; }\n")
	g.pf("            System.arraycopy(defaults, 0, image, 0, bodyBytes); // the declared defaults, one prefill\n")
	g.pf("            TableFixed.run(entries, entryCount, remap, data, at + 8, bodySize, image, report);\n")
	g.pf("            scatter(image, 0, values[k], report);\n")
	g.pf("            at += bodySize + 8;\n")
	g.pf("        }\n")
	g.pf("        return (int) n;\n")
	g.pf("    }\n")
}

// fixedLiteralCap is where a readable byte-array literal stops being one. A
// Java array initializer is CODE, and a class initializer may hold 64KiB of
// it, so a big constant rides as string chunks instead (TableFixed.bytes).
// Past this the literal is not readable anyway.
const fixedLiteralCap = 1024

// fixedChunkBytes is how many bytes ride in one string literal. Every byte is
// an octal escape, and a literal's own limit is 65535 bytes of modified UTF-8,
// which a byte outside ASCII spends two of.
const fixedChunkBytes = 8000

// emitByteConstant writes one constant byte run: a readable array literal
// while it is readable, and string chunks once it is not.
func (g *fixedGen) emitByteConstant(name string, b []byte, entries []fixedLayoutEntry) {
	if len(b) > fixedLiteralCap {
		g.pf("    public static final byte[] %s = TableFixed.bytes(\n", name)
		g.emitByteChunks(b)
		g.pf("    );\n\n")
		return
	}
	g.pf("    public static final byte[] %s = {\n", name)
	g.emitByteArray(b, entries)
	g.pf("    };\n\n")
}

// emitByteChunks writes a constant byte run as octal-escaped string literals,
// one per chunk: every byte is a \\ooo escape, so nothing in the text can be
// read as anything but the byte it is.
func (g *fixedGen) emitByteChunks(b []byte) {
	for at := 0; at < len(b); at += fixedChunkBytes {
		end := min(at+fixedChunkBytes, len(b))
		var sb strings.Builder
		for _, v := range b[at:end] {
			fmt.Fprintf(&sb, "\\%03o", v)
		}
		sep := ","
		if end == len(b) {
			sep = ""
		}
		g.pf("        \"%s\"%s\n", sb.String(), sep)
	}
}

func (g *fixedGen) emitByteArray(b []byte, entries []fixedLayoutEntry) {
	if len(entries) > 0 {
		// the layout, entry by entry, so a reader of the generated source can
		// see the walk rather than a wall of hex
		g.pf("        %s, // %d entries\n", fixedBytesLine(b[:4]), len(entries))
		at := 4
		for _, e := range entries {
			g.pf("        %s, // %s: kind %d, size %d, %d %s\n",
				fixedBytesLine(b[at:at+17]), e.note, e.kind, e.size, e.children, fixedChildWord(e.children))
			at += 17
		}
		return
	}
	for i := 0; i < len(b); i += 16 {
		end := min(i+16, len(b))
		g.pf("        %s,\n", fixedBytesLine(b[i:end]))
	}
	if len(b) == 0 {
		g.pf("        // an empty body\n")
	}
}

func fixedChildWord(n int) string {
	if n == 1 {
		return "child"
	}
	return "children"
}

func fixedBytesLine(b []byte) string {
	var sb strings.Builder
	for i, v := range b {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%d", int8(v))
	}
	return sb.String()
}

func (g *fixedGen) emitCountedArray(entries []fixedLayoutEntry) {
	var sb strings.Builder
	for i, e := range entries {
		if i > 0 {
			sb.WriteString(", ")
		}
		if e.counted {
			sb.WriteString("1")
		} else {
			sb.WriteString("0")
		}
		if (i+1)%32 == 0 && i+1 < len(entries) {
			sb.WriteString("\n       ")
		}
	}
	if len(entries) == 0 {
		g.pf("        // no entries\n")
		return
	}
	g.pf("        %s\n", sb.String())
}
