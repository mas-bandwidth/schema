// THE FIXED FORM's Elixir surface (docs/SPEC-TABLES.md §3.4), form byte 3: the
// write, the read, and the value the two carry.
//
// THE MODEL IS THE PACKET CODEC's, NOT FORM 1's — Glenn's rule for this class,
// and it is what every shape below is. The Elixir type wire (internal/codegen/
// elixir) writes a record by destructuring the value once and constructing ONE
// binary out of literal-width segments, and reads it by matching that binary
// and building the struct in one expression; its verdict is a tagged tuple and
// never an exception, its writer TRUSTS the caller up to O(1) contract checks
// that raise ArgumentError, and its floats travel bit-transparently as
// {:nonfinite, bits} where no BEAM float term can hold the pattern. The fixed
// form is exactly that shape with BYTE-WIDTH fields in place of bit windows:
//
//	THE WRITE is one binary construction. Where the reference memcpy's a
//	constant template and stores over it, the construction's own slack
//	segments are literal zeros — the same bytes, one pass, nothing to memset.
//
//	THE READ is a prefill and ONE loop over ONE plan, and then ONE BINARY
//	PATTERN MATCH that projects the record image into terms. For a record whose
//	hash is this build's own the plan is the identity plan the emitter baked
//	in; for any other hash the SAME loop runs over a plan compiled once from
//	the writer's own layout and cached by hash. There is no second reader.
//
// NOTHING HERE IS TAKEN FROM FORM 1, which this backend does not carry at all
// (schema#515): there is no field reference, no kind byte, no length, no
// trailer and no probe anywhere in it.
package elixirtable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// formatWidth is `mix format`'s default line length, which the generated
// output is held to by the leg's own gate.
const formatWidth = 98

// fixedGen carries one schema file's fixed-form emission state.
type fixedGen struct {
	unit *ir.Unit
	ns   string
	file *ir.File
	body strings.Builder
}

func (g *fixedGen) pf(format string, args ...any) {
	fmt.Fprintf(&g.body, format, args...)
}

// FixedModuleSuffix is the module and file suffix a schema file's fixed-form
// surface takes, claimed the way <Base>Block and <Base>Cook are.
const FixedModuleSuffix = "Fixed"

// generateFixed emits the unit's fixed-form surface: the shared runtime, and
// one <Base>Fixed module per file that declares a fixed root or a type one
// reaches.
func generateFixed(u *ir.Unit, ns string) (map[string][]byte, error) {
	roots := fixedRoots(u)
	if len(roots) == 0 {
		return nil, nil
	}
	closure := fixedClosure(roots)
	out := map[string][]byte{}
	out[FixedRuntimeModule+".ex"] = fixedRuntimeModule(u, ns)
	for _, f := range u.Files {
		g := &fixedGen{unit: u, ns: ns, file: f}
		if body := g.module(roots, closure); body != nil {
			out[f.Base+FixedModuleSuffix+".ex"] = body
		}
	}
	return out, nil
}

// mine reports whether a declaration was made in this file.
func (g *fixedGen) mine(name string) bool { return g.unit.DeclFile[name] == g.file.Base }

// moduleOf is the Elixir module a type's fixed-form helpers live in: the one
// named for the schema FILE that declares it, so a nested type shared between
// two files has exactly one definition and both callers reach it by name.
func (g *fixedGen) moduleOf(st *ir.Struct) string { return g.qualifier(st.Name) }

// qualifier is the module prefix a helper of `name` is reached through: EMPTY
// for a declaration of this same file, because a same-module call needs no
// prefix and the short spelling is what keeps a generated line inside the
// formatter's width.
func (g *fixedGen) qualifier(name string) string {
	base := g.unit.DeclFile[name]
	if base == "" || base == g.file.Base {
		return ""
	}
	return fmt.Sprintf("%s.%s%s.", g.ns, moduleBase(base), FixedModuleSuffix)
}

func (g *fixedGen) callWrite(st *ir.Struct, expr string) string {
	return fmt.Sprintf("%s%s_fixed_write_body(%s)", g.moduleOf(st), ir.RustSnake(st.Name), expr)
}

func (g *fixedGen) callDecode(st *ir.Struct, expr string) string {
	return fmt.Sprintf("%s%s_fixed_decode(%s)", g.moduleOf(st), ir.RustSnake(st.Name), expr)
}

func (g *fixedGen) callUnionWrite(u *ir.Union, expr string) string {
	return fmt.Sprintf("%s%s_fixed_write_arm(%s)", g.unionModule(u), ir.RustSnake(u.Name), expr)
}

func (g *fixedGen) callUnionDecode(u *ir.Union, expr string) string {
	return fmt.Sprintf("%s%s_fixed_decode(%s)", g.unionModule(u), ir.RustSnake(u.Name), expr)
}

func (g *fixedGen) unionModule(u *ir.Union) string { return g.qualifier(u.Name) }

// module emits this file's whole fixed-form surface, or nil when the file has
// nothing in it.
func (g *fixedGen) module(roots, closure []*ir.Struct) []byte {
	var here []*ir.Struct
	for _, st := range closure {
		if g.mine(st.Name) {
			here = append(here, st)
		}
	}
	unions := g.unionsIn(closure)
	var myRoots []*ir.Struct
	for _, st := range roots {
		if g.mine(st.Name) {
			myRoots = append(myRoots, st)
		}
	}
	if len(here) == 0 && len(unions) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString(header(g.file.Base, g.unit.Package,
		"the FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4)"))
	b.WriteString("\n")

	// THE VALUE A FIXED RECORD CARRIES. A `type` and a `union` already lower to
	// a struct through the packet emitter; a TABLE does not lower to anything
	// in this port at all, so its value surface is defined here, beside the
	// codec that fills it.
	for _, st := range here {
		if st.IsTable {
			b.WriteString(g.structModule(st))
		}
	}

	fmt.Fprintf(&b, "defmodule %s.%s%s do\n", g.ns, moduleBase(g.file.Base), FixedModuleSuffix)
	g.pf("  @moduledoc \"\"\"\n")
	g.pf("  The FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3, for %s.schema.\n\n", g.file.Base)
	g.pf("  A record is an EIGHT-BYTE HASH of the writer's LAYOUT and then the values\n")
	g.pf("  in declared order, every field at its declared storage width, nothing\n")
	g.pf("  padded between fields. A FILE is the form byte, the layout behind a `u32`\n")
	g.pf("  length, and then the records back to back to the end of it.\n")
	g.pf("  \"\"\"\n\n")
	g.pf("  alias %s.FixedRuntime, as: R\n\n", g.ns)

	for _, u := range unions {
		g.unionHelpers(u)
	}
	for _, st := range here {
		g.typeHelpers(st)
	}
	for _, st := range myRoots {
		g.rootSurface(st)
	}
	b.WriteString(strings.TrimRight(g.body.String(), "\n") + "\n")
	b.WriteString("end\n")
	return []byte(b.String())
}

// unionsIn is every union the closure reaches that this file declares.
func (g *fixedGen) unionsIn(closure []*ir.Struct) []*ir.Union {
	seen := map[string]bool{}
	var out []*ir.Union
	for _, st := range closure {
		for _, f := range st.Fields {
			if f.Type.Kind != ir.TNamed {
				continue
			}
			if u, ok := f.Type.Ref.(*ir.Union); ok && !seen[u.Name] && g.mine(u.Name) {
				seen[u.Name] = true
				out = append(out, u)
			}
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// THE VALUE SURFACE
// ---------------------------------------------------------------------------

// structModule is one TABLE's Elixir value: a struct whose construction carries
// the DECLARED DEFAULTS, which is the packet port's own rule for a type.
//
// AN OPTIONAL FIELD IS TWO FIELDS — `<name>_present` beside `<name>` — and that
// is the wire's shape rather than a convenience. §3.4 says the payload RIDES
// WHOLE whether or not it is present, so a value that threw the payload away
// when the flag was false could not write back the bytes it read; the C++
// reference's storage carries both for the same reason.
func (g *fixedGen) structModule(st *ir.Struct) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# table %s — the value a fixed record of it carries (docs/SPEC-TABLES.md §3.4)\n", st.Name)
	fmt.Fprintf(&b, "defmodule %s.%s do\n", g.ns, st.Name)
	fields := make([]string, 0, len(st.Fields))
	for _, f := range st.Fields {
		if f.Type.Optional {
			fields = append(fields, fmt.Sprintf("%s_present: false", f.Name))
		}
		fields = append(fields, fmt.Sprintf("%s: %s", f.Name, g.fixedElixirDefault(f)))
	}
	if one := "  defstruct " + strings.Join(fields, ", "); len(one) <= formatWidth {
		b.WriteString(one + "\n")
	} else {
		b.WriteString("  defstruct " + fields[0] + ",\n")
		for i := 1; i < len(fields); i++ {
			sep := ","
			if i == len(fields)-1 {
				sep = ""
			}
			fmt.Fprintf(&b, "            %s%s\n", fields[i], sep)
		}
	}
	b.WriteString("end\n\n")
	return b.String()
}

// ---------------------------------------------------------------------------
// THE WRITE: one binary construction
// ---------------------------------------------------------------------------

// wseg is one piece of a type's body. An INLINE piece is a binary segment and
// joins its neighbours in ONE construction; a BLOCK piece is an iodata
// expression of exactly its own constant size and rides beside them, which is
// what keeps a nested array out of an intermediate binary it would only be
// copied out of again.
type wseg struct {
	inline string
	block  node
}

func (g *fixedGen) writeType(st *ir.Struct, val string) []wseg {
	var out []wseg
	for _, f := range st.Fields {
		out = append(out, g.writeField(f, val)...)
	}
	return out
}

func (g *fixedGen) writeField(f *ir.Field, val string) []wseg {
	var out []wseg
	name := val + "." + f.Name
	if f.Type.Optional {
		out = append(out, wseg{inline: fmt.Sprintf("if(%s_present, do: 1, else: 0)::unsigned-8", name)})
	}
	switch {
	case f.KeyEnum != "":
		out = append(out, g.writeElements(f, name, f.KeyEnumRef.Max*fixedElementBytes(f)))
	case f.Array == ir.ArrayFixed:
		out = append(out, g.writeElements(f, name, f.ArrayBound*fixedElementBytes(f)))
	case f.Array == ir.ArrayCounted:
		// THE COUNT, THEN MAX ELEMENTS, the slack zero-filled.
		out = append(out, wseg{inline: fmt.Sprintf("length(%s)::little-signed-32", name)})
		out = append(out, g.writeElements(f, name, f.ArrayBound*fixedElementBytes(f)))
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		// THE LENGTH, THEN THE UNITS ONTO THE ZEROED TEMPLATE, and no clamp: a
		// value past its declared bound is a caller's contract break, which this
		// port raises on for the packet port's own reason — the BEAM has no
		// compile-out assert, so a check here is always on.
		out = append(out, wseg{inline: fmt.Sprintf("byte_size(%s)::little-signed-32", name)})
		out = append(out, wseg{inline: fmt.Sprintf("R.fill(%s, %d)::binary", name, f.Type.Size)})
	case f.Type.Kind == ir.TWString:
		out = append(out, wseg{inline: fmt.Sprintf("div(byte_size(%s), 2)::little-signed-32", name)})
		out = append(out, wseg{inline: fmt.Sprintf("R.fill(%s, %d)::binary", name, 2*f.Type.Size)})
	default:
		out = append(out, g.writeElement(f, name)...)
	}
	return out
}

// writeElements is a field's whole run of elements, padded to its constant
// size — one iodata block, so the elements are never copied into an
// intermediate binary on their way into the record.
func (g *fixedGen) writeElements(f *ir.Field, name string, size int64) wseg {
	elems := call("Enum.map", raw(name), g.elementWriter(f))
	// THE SLACK BEHIND A SHORT ARRAY IS THE ELEMENT'S DEFAULT IMAGE and not
	// zeros, because that is what the C++ reference writes: its writer stores
	// all Max elements out of storage, and storage that has been Reset holds
	// the declared defaults. Where the default IS zeros the two collapse and
	// nothing is spelled twice.
	def := fixedElementDefault(f)
	if fixedAllZero(def) {
		return wseg{block: call("R.pad", elems, rawf("%d", size))}
	}
	return wseg{block: call("R.pad", elems, rawf("%d", size), raw(fixedBinaryLiteral(def, 0)))}
}

// fixedAllZero reports a run of bytes that is entirely zero.
func fixedAllZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

// elementWriter is the function `Enum.map` runs over one element: a CAPTURE
// where the element's writer already is a one-argument function, which is what
// keeps the line short enough for the formatter to leave it alone.
func (g *fixedGen) elementWriter(f *ir.Field) node {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return rawf("&%s%s_fixed_write_body/1", g.moduleOf(r), ir.RustSnake(r.Name))
		case *ir.Union:
			return rawf("&%s%s_fixed_write_arm/1", g.unionModule(r), ir.RustSnake(r.Name))
		}
	}
	return rawf("fn e -> <<%s>> end", g.leafSegment(f, "e"))
}

// writeElement is ONE element: inline where its image is a run of segments,
// a block where it is not.
func (g *fixedGen) writeElement(f *ir.Field, name string) []wseg {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			// A NESTED TYPE WHOSE IMAGE IS A RUN OF BYTES IS INLINED, which is
			// what makes a record of scalars and nested scalars ONE binary
			// construction and not a tree of them.
			if fixedFlatType(r) {
				return g.writeType(r, name)
			}
			return []wseg{{block: raw(g.callWrite(r, name))}}
		case *ir.Union:
			return []wseg{{block: raw(g.callUnionWrite(r, name))}}
		}
	}
	return []wseg{{inline: g.leafSegment(f, name)}}
}

// leafSegment is a LEAF's binary segment: its DECLARED STORAGE IMAGE,
// LITTLE-ENDIAN, at its DECLARED STORAGE WIDTH. The width is the
// DECLARATION's, which SPEC.md fixes identically in every port, which is why a
// bits(12) costs four bytes here where §3 spends two.
func (g *fixedGen) leafSegment(f *ir.Field, name string) string {
	width := ir.FixedStorageBytes(f.Type) * 8
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Enum:
			// THE ORDINAL IS THE VARIANT'S POSITION, from 1, and 0 is None.
			return fmt.Sprintf("%s::little-unsigned-%d", name, r.StorageBits)
		case *ir.Flags:
			return fmt.Sprintf("%s::little-unsigned-64", name)
		}
	}
	switch f.Type.Kind {
	case ir.TBool:
		return fmt.Sprintf("if(%s, do: 1, else: 0)::unsigned-8", name)
	case ir.TFloat32:
		// A COMPRESSED FLOAT RIDES AS THE FLOAT and not as a quantized index:
		// this form is not optimized for bandwidth (§3.4).
		return fmt.Sprintf("R.f32_bits(%s)::little-unsigned-32", name)
	case ir.TFloat64:
		return fmt.Sprintf("R.f64_bits(%s)::little-unsigned-64", name)
	}
	if f.Type.Signed {
		return fmt.Sprintf("%s::little-signed-%d", name, width)
	}
	return fmt.Sprintf("%s::little-unsigned-%d", name, width)
}

// writeBodyNode joins the segments into constructions and the blocks between
// them: one node where there is only one piece, an iodata list otherwise.
func (g *fixedGen) writeBodyNode(segs []wseg) node {
	var parts []node
	var runs []string
	flush := func() {
		if len(runs) == 0 {
			return
		}
		parts = append(parts, binNode{segs: runs})
		runs = nil
	}
	for _, s := range segs {
		if s.inline != "" {
			runs = append(runs, s.inline)
			continue
		}
		flush()
		parts = append(parts, s.block)
	}
	flush()
	switch len(parts) {
	case 0:
		return raw("<<>>")
	case 1:
		return parts[0]
	}
	return listNode{items: parts}
}

// ---------------------------------------------------------------------------
// THE READ: one binary pattern match
// ---------------------------------------------------------------------------

// rseg is one piece of a type's decode: a MATCH segment consuming its own
// constant bytes out of the image, POST statements that turn what it bound into
// a term, and the FIELDS it contributes to the struct.
type rseg struct {
	match []string
	post  []string
	keys  []string
	vals  []node
}

func (r *rseg) field(name string, v node) {
	r.keys = append(r.keys, name)
	r.vals = append(r.vals, v)
}

func (r *rseg) merge(o rseg) {
	r.match = append(r.match, o.match...)
	r.post = append(r.post, o.post...)
	r.keys = append(r.keys, o.keys...)
	r.vals = append(r.vals, o.vals...)
}

func (g *fixedGen) readType(st *ir.Struct, prefix string) rseg {
	var out rseg
	for _, f := range st.Fields {
		out.merge(g.readField(f, prefix))
	}
	return out
}

func (g *fixedGen) readField(f *ir.Field, prefix string) rseg {
	v := prefix + f.Name
	var out rseg
	if f.Type.Optional {
		out.match = append(out.match, "p_"+v+"::unsigned-8")
		out.field(f.Name+"_present", rawf("p_%s != 0", v))
	}
	switch {
	case f.KeyEnum != "":
		g.readElements(&out, f, v, f.KeyEnumRef.Max)
	case f.Array == ir.ArrayFixed:
		g.readElements(&out, f, v, f.ArrayBound)
	case f.Array == ir.ArrayCounted:
		// THE COUNT ALREADY CAME THROUGH THE PLAN'S `count` OP, which clamped
		// it to this reader's own bound, so the projection takes it as read.
		out.match = append(out.match, "n_"+v+"::little-signed-32")
		g.readElements(&out, f, v, f.ArrayBound)
		out.post = append(out.post, fmt.Sprintf("l_%s = Enum.take(l_%s, n_%s)", v, v, v))
		out.field(f.Name, rawf("l_%s", v))
		return out
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		out.match = append(out.match,
			"n_"+v+"::little-signed-32",
			fmt.Sprintf("b_%s::binary-size(%d)", v, f.Type.Size))
		out.field(f.Name, rawf("binary_part(b_%s, 0, min(max(n_%s, 0), %d))", v, v, f.Type.Size))
		return out
	case f.Type.Kind == ir.TWString:
		out.match = append(out.match,
			"n_"+v+"::little-signed-32",
			fmt.Sprintf("b_%s::binary-size(%d)", v, 2*f.Type.Size))
		out.field(f.Name, rawf("binary_part(b_%s, 0, 2 * min(max(n_%s, 0), %d))", v, v, f.Type.Size))
		return out
	default:
		g.readElement(&out, f, v, f.Name)
		return out
	}
	out.field(f.Name, rawf("l_%s", v))
	return out
}

// readElements binds a field's whole run of elements and turns it into a list.
// EVERY SLOT IS READ, in the key enum's declared order, and no key rides.
func (g *fixedGen) readElements(out *rseg, f *ir.Field, v string, count int64) {
	elem := fixedElementBytes(f)
	out.match = append(out.match, fmt.Sprintf("b_%s::binary-size(%d)", v, count*elem))
	out.post = append(out.post, fmt.Sprintf("l_%s = %s", v, g.elementList(f, "b_"+v, elem)))
}

func (g *fixedGen) elementList(f *ir.Field, src string, elem int64) string {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return fmt.Sprintf("for <<e::binary-size(%d) <- %s>>, do: %s", elem, src, g.callDecode(r, "e"))
		case *ir.Union:
			return fmt.Sprintf("for <<e::binary-size(%d) <- %s>>, do: %s", elem, src, g.callUnionDecode(r, "e"))
		}
	}
	spec, wrap := g.leafMatch(f, "e")
	return fmt.Sprintf("for <<%s <- %s>>, do: %s", spec, src, wrap.flat())
}

// readElement binds ONE element, inlining a nested type whose image is a run of
// bytes so the whole projection stays ONE binary pattern match.
func (g *fixedGen) readElement(out *rseg, f *ir.Field, v, field string) {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			if fixedFlatType(r) {
				sub := g.readType(r, v+"_")
				out.match = append(out.match, sub.match...)
				out.post = append(out.post, sub.post...)
				out.field(field, g.structNodeOf(r, sub))
				return
			}
			out.match = append(out.match, fmt.Sprintf("b_%s::binary-size(%d)", v, fixedTypeBytes(r)))
			out.field(field, raw(g.callDecode(r, "b_"+v)))
			return
		case *ir.Union:
			out.match = append(out.match, fmt.Sprintf("b_%s::binary-size(%d)", v, fixedElementBytes(f)))
			out.field(field, raw(g.callUnionDecode(r, "b_"+v)))
			return
		}
	}
	spec, wrap := g.leafMatch(f, v)
	out.match = append(out.match, spec)
	out.field(field, wrap)
}

// leafMatch is a LEAF's match segment and the term it makes.
//
// A RANGED INTEGER IS NOT CLAMPED HERE. The plan's `clamp` op already held it
// to this reader's own bounds and counted the `clamped` if it fired, so the
// projection is a pure function of the image and the report never reaches it.
func (g *fixedGen) leafMatch(f *ir.Field, v string) (string, node) {
	name := "f_" + v
	width := ir.FixedStorageBytes(f.Type) * 8
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Enum:
			return fmt.Sprintf("%s::little-unsigned-%d", name, r.StorageBits), raw(name)
		case *ir.Flags:
			return name + "::little-unsigned-64", raw(name)
		}
	}
	switch f.Type.Kind {
	case ir.TBool:
		// A BOOL LANDS AS `byte != 0`, which is what a form that writes 0 or 1
		// and reads anything owes a hostile writer.
		return name + "::unsigned-8", raw(name + " != 0")
	case ir.TFloat32:
		return name + "::little-unsigned-32", rawf("R.f32_value(%s)", name)
	case ir.TFloat64:
		return name + "::little-unsigned-64", rawf("R.f64_value(%s)", name)
	}
	if f.Type.Signed {
		return fmt.Sprintf("%s::little-signed-%d", name, width), raw(name)
	}
	return fmt.Sprintf("%s::little-unsigned-%d", name, width), raw(name)
}

func (g *fixedGen) structNodeOf(st *ir.Struct, r rseg) node {
	return structNode{mod: g.ns + "." + st.Name, keys: r.keys, vals: r.vals}
}

// ---------------------------------------------------------------------------
// THE HELPERS one TYPE gets
// ---------------------------------------------------------------------------

func (g *fixedGen) typeHelpers(st *ir.Struct) {
	snake := ir.RustSnake(st.Name)
	size := fixedTypeBytes(st)

	g.pf("  # %s's body: %d bytes, the values in DECLARED ORDER, every field at its\n", st.Name, size)
	g.pf("  # declared storage width, nothing padded between fields.\n")
	g.pf("  def %s_fixed_write_body(value) do\n", snake)
	g.pf("    %s\n", g.writeBodyNode(g.writeType(st, "value")).render(4, 4, 0))
	g.pf("  end\n\n")

	g.pf("  # %s's projection: ONE binary pattern match over its %d bytes of record\n", st.Name, size)
	g.pf("  # image, and the struct it makes.\n")
	r := g.readType(st, "")
	g.emitDecodeHead(snake, r.match, 2)
	for _, p := range r.post {
		g.emitAssign(p, 4)
	}
	if len(r.post) > 0 {
		// the blank line the formatter puts after a COMPREHENSION's assignment
		g.pf("\n")
	}
	g.pf("    %s\n", g.structNodeOf(st, r).render(4, 4, 0))
	g.pf("  end\n\n")
}

// emitAssign prints one `name = expr` statement in the formatter's own shape:
// on one line where it fits, and otherwise the name, the `=`, and the value on
// its own line two columns in, with the blank line the formatter puts after a
// multi-line assignment.
func (g *fixedGen) emitAssign(stmt string, col int) {
	ind := indentOf(col)
	if fits(col, 0, stmt) {
		g.pf("%s%s\n", ind, stmt)
		return
	}
	name, value, _ := strings.Cut(stmt, " = ")
	g.pf("%s%s =\n%s%s\n", ind, name, indentOf(col+2), value)
}

// emitDecodeHead prints the decode's function head — the whole image matched at
// once, filled greedily to the format width. Where the one-line head outgrows
// the width the formatter puts the pattern on its own line and the closing
// paren under `do`, and this writes that shape rather than checking it after.
func (g *fixedGen) emitDecodeHead(snake string, match []string, col int) {
	ind := indentOf(col)
	head := fmt.Sprintf("def %s_fixed_decode(", snake)
	pattern := binNode{segs: append(append([]string{}, match...), "_::binary")}
	if one := ind + head + pattern.render(col+len(head), col+len(head), 4) + ") do"; fits(0, 0, one) {
		g.pf("%s\n", one)
		return
	}
	g.pf("%s%s\n", ind, head)
	g.pf("%s%s\n", indentOf(col+6), pattern.render(col+6, col+6, 0))
	g.pf("%s) do\n", indentOf(col+4))
}

// ---------------------------------------------------------------------------
// A UNION: the tag, then the WIDEST ARM
// ---------------------------------------------------------------------------

func (g *fixedGen) unionHelpers(u *ir.Union) {
	snake := ir.RustSnake(u.Name)
	tag := fixedUnionTagBytes(u)
	widest := int64(0)
	for _, v := range u.Variants {
		if v.F == nil {
			continue
		}
		if n := fixedFieldBytes(v.F); n > widest {
			widest = n
		}
	}
	total := tag + widest

	g.pf("  # union %s: the TAG ORDINAL at its own storage width, then the WIDEST ARM,\n", u.Name)
	g.pf("  # the slack behind a narrower arm zero-filled. TAG 0 IS None.\n")
	g.pf("  def %s_fixed_write_arm(value) do\n", snake)
	g.pf("    R.pad(\n")
	g.pf("      [\n")
	g.pf("        <<value.type::little-unsigned-%d>>,\n", tag*8)
	g.pf("        case value.type do\n")
	arms := make([]caseArm, 0, len(u.Variants)+1)
	for i, v := range u.Variants {
		if v.F == nil {
			continue
		}
		arms = append(arms, caseArm{label: fmt.Sprintf("%d", i+1), body: g.armWriter(v.F, "value."+v.Name)})
	}
	arms = append(arms, caseArm{label: "_", body: raw("<<>>")})
	g.emitArms(arms, 10)
	g.pf("        end\n")
	g.pf("      ],\n")
	g.pf("      %d\n", total)
	g.pf("    )\n")
	g.pf("  end\n\n")

	g.pf("  # union %s's projection: the tag, then the arm it names.\n", u.Name)
	g.pf("  def %s_fixed_decode(<<tag::little-unsigned-%d, arm::binary>>) do\n", snake, tag*8)
	g.pf("    case tag do\n")
	darms := make([]caseArm, 0, len(u.Variants)+1)
	for i, v := range u.Variants {
		if v.F == nil {
			continue
		}
		darms = append(darms, caseArm{
			label: fmt.Sprintf("%d", i+1),
			body: structNode{
				mod:  g.ns + "." + u.Name,
				keys: []string{"type", v.Name},
				vals: []node{rawf("%d", i+1), raw(g.armDecode(v.F))},
			},
		})
	}
	darms = append(darms, caseArm{label: "_", body: rawf("%%%s.%s{}", g.ns, u.Name)})
	g.emitArms(darms, 6)
	g.pf("    end\n")
	g.pf("  end\n\n")
}

// caseArm is one arm of a generated `case`: its pattern and its body.
type caseArm struct {
	label string
	body  node
}

// emitArms prints a `case`'s arms in the formatter's own shape. `mix format`
// keeps `label -> body` on one line only while EVERY arm fits; once one does
// not, it expands them all — the pattern, then the body two columns in, with a
// blank line between arms and none before the `end`.
func (g *fixedGen) emitArms(arms []caseArm, col int) {
	ind := indentOf(col)
	flat := true
	for _, a := range arms {
		if !fits(col+len(a.label)+4, 0, a.body.flat()) {
			flat = false
			break
		}
	}
	if flat {
		for _, a := range arms {
			g.pf("%s%s -> %s\n", ind, a.label, a.body.flat())
		}
		return
	}
	for i, a := range arms {
		if i > 0 {
			g.pf("\n")
		}
		g.pf("%s%s ->\n%s%s\n", ind, a.label, indentOf(col+2), a.body.render(col+2, col+2, 0))
	}
}

// armWriter is ONE arm's bytes: the arm's own body, which R.pad then zero-fills
// out to the widest arm's size.
func (g *fixedGen) armWriter(f *ir.Field, name string) node {
	if f.Type.Kind == ir.TNamed {
		if r, ok := f.Type.Ref.(*ir.Struct); ok {
			return raw(g.callWrite(r, name))
		}
	}
	return binNode{segs: []string{g.leafSegment(f, name)}}
}

func (g *fixedGen) armDecode(f *ir.Field) string {
	if f.Type.Kind == ir.TNamed {
		if r, ok := f.Type.Ref.(*ir.Struct); ok {
			return g.callDecode(r, fmt.Sprintf("binary_part(arm, 0, %d)", fixedTypeBytes(r)))
		}
	}
	spec, wrap := g.leafMatch(f, "arm")
	return fmt.Sprintf("(fn <<%s, _::binary>> -> %s end).(arm)", spec, wrap.flat())
}

// ---------------------------------------------------------------------------
// A ROOT's SURFACE: the layout, the hash, the identity plan, save and load
// ---------------------------------------------------------------------------

func (g *fixedGen) rootSurface(st *ir.Struct) {
	snake := ir.RustSnake(st.Name)
	w := fixedWalkRoot(st)
	layout := fixedLayoutBytes(w.entries)
	hash := fixedLayoutHash(layout)
	body := fixedTypeBytes(st)
	plan := fixedIdentityPlan(st)

	g.pf("  # ---- %s, the fixed form ----\n\n", st.Name)
	g.pf("  # MEASURE IS A CONSTANT on this form: the body is the same size for every\n")
	g.pf("  # value the type can hold (docs/SPEC-TABLES.md §3.4).\n")
	g.pf("  @%s_body_bytes %d\n", snake, body)
	g.pf("  @%s_record_bytes %d\n", snake, fixedHashBytes+body)
	g.pf("  @%s_hash 0x%016X\n\n", snake, hash)

	g.pf("  # THE LAYOUT: %d entries, a PRE-ORDER walk of the closure in the writer's\n", len(w.entries))
	g.pf("  # declared order — an id, a kind, a constant size and a child count each,\n")
	g.pf("  # seventeen bytes, every number little-endian. Every byte is settled by the\n")
	g.pf("  # compiler, and its fnv1a64 is the eight bytes every record carries.\n")
	g.pf("  @%s_layout %s\n\n", snake, fixedBinaryLiteral(layout, len("  @"+snake+"_layout ")))

	g.pf("  # MY SIDE of the layout, one row per entry: the storage facts a layout entry\n")
	g.pf("  # cannot carry. Here they are offsets into THIS BUILD's own record image,\n")
	g.pf("  # where the C++ reference's rows carry an offsetof into a struct.\n")
	g.pf("  @%s_dst %s\n\n", snake, g.dstLiteral(w))

	g.pf("  # THE PREFILL: the DECLARED DEFAULTS as record bytes. A field this record\n")
	g.pf("  # does not carry has no plan entry at all, so these bytes are what lands\n")
	g.pf("  # there and the loop never learns the field existed.\n")
	g.pf("  @%s_prefill %s\n\n", snake, fixedBinaryLiteral(fixedDefaultsImage(st), len("  @"+snake+"_prefill ")))

	g.pf("  # THE IDENTITY PLAN, coalesced by the same rule the runtime's compiler uses:\n")
	g.pf("  # two neighbouring copies whose source and destination both advance together\n")
	g.pf("  # are one entry. In the image domain source and destination are the same\n")
	g.pf("  # number, so a record of plain scalars collapses to a SINGLE run.\n")
	g.pf("  @%s_plan %s\n\n", snake, g.planLiteral(plan))

	g.pf("  def %s_fixed_body_bytes, do: @%s_body_bytes\n", snake, snake)
	g.pf("  def %s_fixed_record_bytes, do: @%s_record_bytes\n", snake, snake)
	g.pf("  def %s_fixed_hash, do: @%s_hash\n", snake, snake)
	g.pf("  def %s_fixed_layout, do: @%s_layout\n", snake, snake)
	g.pf("  def %s_fixed_plan, do: @%s_plan\n", snake, snake)
	g.pf("  def %s_fixed_prefill, do: @%s_prefill\n", snake, snake)
	g.pf("  def %s_fixed_dst, do: @%s_dst\n\n", snake, snake)

	g.pf("  # A FILE: THE HEADER (docs/SPEC-TABLES.md §3, one rule for all five forms)\n")
	g.pf("  # — the form byte, seven reserved zero bytes, the LAYOUT HASH at 8 — then the\n")
	g.pf("  # layout behind its u32 length, then the records to the end of it.\n")
	g.pf("  def %s_fixed_measure(count) do\n", snake)
	head := fmt.Sprintf("R.file_header_bytes() + byte_size(@%s_layout) +", snake)
	tail := fmt.Sprintf("count * @%s_record_bytes", snake)
	if fits(4, 0, head+" "+tail) {
		g.pf("    %s %s\n", head, tail)
	} else {
		// `mix format` breaks a long `+` chain AFTER the operator, two columns in
		g.pf("    %s\n      %s\n", head, tail)
	}
	g.pf("  end\n\n")

	g.pf("  # THE WRITE: the hash, then ONE binary construction whose slack segments are\n")
	g.pf("  # literal zeros. There is no measuring pass, no id interning, no trailer and\n")
	g.pf("  # no second walk.\n")
	g.pf("  def %s_fixed_record(value) do\n", snake)
	g.pf("    [<<@%s_hash::little-unsigned-64>>, %s_fixed_write_body(value)]\n", snake, snake)
	g.pf("  end\n\n")

	g.pf("  def %s_fixed_save(values) when is_list(values) do\n", snake)
	g.pf("    IO.iodata_to_binary([\n")
	g.pf("      R.file_header(@%s_hash, byte_size(@%s_layout)),\n", snake, snake)
	g.pf("      @%s_layout,\n", snake)
	g.pf("      Enum.map(values, &%s_fixed_record/1)\n", snake)
	g.pf("    ])\n")
	g.pf("  end\n\n")

	g.emitLoad(st, snake)
}

// emitLoad prints the root's reader: the form byte, the layout, ONE plan and
// ONE loop, and a verdict that is a tagged tuple and never an exception.
func (g *fixedGen) emitLoad(st *ir.Struct, snake string) {
	g.pf("  @doc \"\"\"\n")
	g.pf("  Read a form-3 FILE of %s records.\n\n", st.Name)
	g.pf("  `{:ok, values, report}` or `{:error, reason, report}`, the reason BY NAME.\n")
	g.pf("  Hostile bytes never raise: this is the family read verdict, the same one\n")
	g.pf("  the packet codec answers with.\n\n")
	g.pf("  THE SAME LOOP RUNS OVER THE SAME PLAN whether the writer is this build or\n")
	g.pf("  another, and the only thing that differs is which plan it was handed.\n")
	g.pf("  \"\"\"\n")
	g.pf("  def %s_fixed_load(data, opts \\\\ []) when is_binary(data) do\n", snake)
	g.pf("    report = R.report()\n\n")
	g.pf("    # A FORM BYTE THIS BUILD DOES NOT CARRY IS A REFUSAL BY NAME and never\n")
	g.pf("    # damage: nothing is decoded and no counter moves. The registry is\n")
	g.pf("    # ORDERED, so the name says which direction (§3).\n")
	g.pf("    case R.read_file_header(data) do\n")
	g.pf("      {:ok, stated, layout, records} ->\n")
	g.pf("        %s_fixed_records(stated, layout, records, report, opts)\n\n", snake)
	g.pf("      {:error, why} ->\n")
	g.pf("        {:error, why, report}\n")
	g.pf("    end\n")
	g.pf("  end\n\n")

	g.pf("  defp %s_fixed_records(stated, layout, records, report, opts) do\n", snake)
	g.pf("    hash = R.hash(layout)\n\n")
	g.pf("    with {:ok, plan, size, report} <- %s_fixed_plan_for(hash, layout, report, opts),\n", snake)
	g.pf("         :ok <- %s_fixed_header_names_it(stated, hash),\n", snake)
	g.pf("         {:ok, bodies} <- %s_fixed_split(records, hash, size, []) do\n", snake)
	g.pf("      {values, report} =\n")
	g.pf("        Enum.map_reduce(bodies, report, fn body, report ->\n")
	g.pf("          {image, report} = R.run(plan, body, @%s_prefill, report)\n", snake)
	g.pf("          {%s_fixed_decode(image), report}\n", snake)
	g.pf("        end)\n\n")
	g.pf("      {:ok, values, report}\n")
	g.pf("    else\n")
	g.pf("      {:error, why, report} -> {:error, why, report}\n")
	g.pf("      {:error, why} -> {:error, why, report}\n")
	g.pf("    end\n")
	g.pf("  end\n\n")

	g.pf("  # THE PLAN: the identity plan for this build's own hash, and for any other\n")
	g.pf("  # hash the one compiled ONCE from the writer's layout and cached by hash.\n")
	g.pf("  # A caller may own the plan instead and hand it in through `plan:`.\n")
	g.emitPlanForHead(snake)
	g.pf("    {:ok, @%s_plan, @%s_body_bytes, report}\n", snake, snake)
	g.pf("  end\n\n")
	g.pf("  defp %s_fixed_plan_for(hash, layout, report, opts) do\n", snake)
	g.pf("    cap = Keyword.get(opts, :plan_capacity, R.plan_capacity())\n\n")
	g.pf("    case Keyword.get(opts, :plan) || R.cached(__MODULE__, hash) do\n")
	g.pf("      {_plan, _size, made} when made > cap ->\n")
	g.pf("        # THE PLAN'S CAPACITY IS THE CALLER'S BOUND AND NOT THE CACHE'S, so a\n")
	g.pf("        # plan already held for this peer is refused by the same name.\n")
	g.pf("        {:error, :plan_too_large, report}\n\n")
	g.pf("      {plan, size, _made} ->\n")
	g.pf("        {:ok, plan, size, report}\n\n")
	g.pf("      nil ->\n")
	g.pf("        # THE LAYOUT IS VALIDATED BEFORE A SINGLE RECORD BYTE IS TOUCHED, and\n")
	g.pf("        # every rule it fails refuses under ITS OWN NAME.\n")
	g.pf("        with {:ok, theirs} <- R.parse_layout(layout) do\n")
	g.pf("          mine = R.my_layout(__MODULE__, @%s_layout)\n\n", snake)
	g.pf("          case R.compile(theirs, mine, @%s_dst, cap, report) do\n", snake)
	g.pf("            {:ok, plan, report} ->\n")
	g.pf("              size = R.size_at(theirs, 0)\n")
	g.pf("              R.cache(__MODULE__, hash, {plan, size, length(plan)})\n")
	g.pf("              {:ok, plan, size, report}\n\n")
	g.pf("            {:error, why, report} ->\n")
	g.pf("              {:error, why, report}\n")
	g.pf("          end\n")
	g.pf("        end\n")
	g.pf("    end\n")
	g.pf("  end\n\n")

	g.pf("  # THE HEADER NAMES THE LAYOUT ONCE, and it is checked LAST of the three: the\n")
	g.pf("  # layout's own rules each refuse under their own name first, so a broken\n")
	g.pf("  # layout is never reported as a lying header (docs/SPEC-TABLES.md §3).\n")
	g.pf("  defp %s_fixed_header_names_it(stated, hash) when stated == hash, do: :ok\n", snake)
	g.pf("  defp %s_fixed_header_names_it(_stated, _hash), do: {:error, :layout_malformed}\n\n", snake)

	g.pf("  # THE RECORDS FILL THE REST OF THE FILE and there is no count: a reader knows\n")
	g.pf("  # the record size from the layout, so the count is arithmetic. BYTES LEFT\n")
	g.pf("  # OVER ARE `malformed`, which is §3's rule for the same reason.\n")
	g.pf("  defp %s_fixed_split(<<>>, _hash, _size, acc), do: {:ok, :lists.reverse(acc)}\n\n", snake)
	g.pf("  defp %s_fixed_split(records, hash, size, acc) do\n", snake)
	g.pf("    case records do\n")
	g.pf("      <<h::little-unsigned-64, body::binary-size(^size), rest::binary>> when h == hash ->\n")
	g.pf("        %s_fixed_split(rest, hash, size, [body | acc])\n\n", snake)
	g.pf("      <<_::little-unsigned-64, _::binary-size(^size), _::binary>> ->\n")
	g.pf("        # A RECORD WHOSE HASH NAMES NO LAYOUT THIS READER HOLDS IS A REFUSAL BY\n")
	g.pf("        # NAME, never a guess and never damage.\n")
	g.pf("        {:error, :no_layout}\n\n")
	g.pf("      _ ->\n")
	g.pf("        {:error, :malformed}\n")
	g.pf("    end\n")
	g.pf("  end\n\n")
}

// emitPlanForHead prints the identity clause's head, breaking the guard onto
// its own line where the one-line form outgrows the formatter's width.
func (g *fixedGen) emitPlanForHead(snake string) {
	head := fmt.Sprintf("  defp %s_fixed_plan_for(hash, _layout, report, _opts)", snake)
	guard := fmt.Sprintf(" when hash == @%s_hash do", snake)
	if fits(0, 0, head+guard) {
		g.pf("%s%s\n", head, guard)
		return
	}
	g.pf("%s\n       %s\n", head, strings.TrimSpace(guard))
}

// dstLiteral spells MY SIDE of the layout: one row per entry.
func (g *fixedGen) dstLiteral(w *fixedWalk) string {
	var sb strings.Builder
	sb.WriteString("{\n")
	for i, d := range w.dst {
		lo, hi := "nil", "nil"
		if d.lo != "" {
			lo, hi = d.lo, d.hi
		}
		sep := ","
		if i == len(w.dst)-1 {
			sep = ""
		}
		row := tupleNode{items: []string{
			fmt.Sprintf("%d", d.dst), fmt.Sprintf("%d", d.stride), fmt.Sprintf("%d", d.aux),
			fmt.Sprintf("%d", d.counted), fmt.Sprintf("%d", d.arg), lo, hi,
		}}
		fmt.Fprintf(&sb, "    %s%s\n", row.render(4, 4, len(sep)), sep)
	}
	sb.WriteString("  }")
	return sb.String()
}

func (g *fixedGen) planLiteral(plan []fixedPlanEntry) string {
	if len(plan) == 0 {
		return "[]"
	}
	var sb strings.Builder
	sb.WriteString("[\n")
	for i, e := range plan {
		sep := ","
		if i == len(plan)-1 {
			sep = ""
		}
		fmt.Fprintf(&sb, "    %s%s\n", e.render(4, len(sep)), sep)
	}
	sb.WriteString("  ]")
	return sb.String()
}
