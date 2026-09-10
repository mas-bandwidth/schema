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
//	THE WRITE is one binary APPENDED TO, the packet codec's `data =
//	<<data::binary, ...>>`. Where the reference memcpy's a constant template
//	and stores over it, the appends' own slack segments are literal zeros —
//	the same bytes, one pass, nothing to memset, and a whole file is one
//	binary grown in place.
//
//	THE READ is ONE BINARY PATTERN MATCH that projects the record image into
//	terms, holding every bounded value to this reader's own bounds as it
//	lands and counting what it held. For a record whose hash is this build's
//	own that match runs straight over the file; for any other hash a plan
//	compiled once from the writer's own layout and cached by hash lands the
//	record onto the prefill first, and the SAME projection reads the result.
//
// NOTHING HERE IS TAKEN FROM FORM 1, which this backend does not carry at all
// (schema#515): there is no field reference, no kind byte, no length, no
// trailer and no probe anywhere in it.
package elixirtable

import (
	"fmt"
	"regexp"
	"strconv"
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
	// owner is the snake name of the declaration whose body is being emitted:
	// what a ranged leaf's bounds attribute is named after.
	owner string
	// leafLists are the walks over runs of BOUNDED leaves one type's projection
	// asked for, emitted beside it.
	leafLists []leafList
	// blank and lineStart are where the output stands, for emitLines to keep
	// the formatter's one blank line and never two.
	blank     bool
	lineStart bool
}

func (g *fixedGen) pf(format string, args ...any) {
	s := fmt.Sprintf(format, args...)
	g.body.WriteString(s)
	// blank remembers whether the output stands at a point where the formatter
	// allows no blank line: after one already, or right after a `do`.
	switch {
	case strings.HasSuffix(s, "\n\n") || strings.HasSuffix(s, "do\n"):
		g.blank = true
	case s == "\n":
		g.blank = g.blank || g.lineStart
	case strings.HasSuffix(s, "\n"):
		g.blank = false
	}
	g.lineStart = strings.HasSuffix(s, "\n")
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
	g.emitRangeAttrs(here, unions)

	for _, u := range unions {
		g.owner = ir.RustSnake(u.Name)
		g.unionHelpers(u)
	}
	for _, st := range here {
		g.owner = ir.RustSnake(st.Name)
		g.typeHelpers(st)
	}
	g.owner = ""
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
// THE WRITE: one binary, APPENDED TO
// ---------------------------------------------------------------------------
//
// THE PACKET CODEC BUILDS ITS WIRE BY APPENDING TO ONE BINARY — `data =
// <<data::binary, ...>>` — and the BEAM rewards exactly that shape: the first
// append allocates a writable binary with room to grow and every append after
// it lands in place, so a whole file of records is ONE allocation that doubles
// a few times and never a tree of small binaries flattened at the end. The
// fixed form's writer is that shape. A type's body is `_fixed_write_into(value,
// acc)`: it destructures the value ONCE, appends its scalar run in one
// construction, walks its arrays by appending each element, and hands the
// binary back. The slack behind a short array, a short text, a narrow arm or an
// absent optional is a zero segment of exactly its width inside the same
// construction — the reference's zeroed template, written as one segment.

// wseg is one piece of a type's body. An INLINE piece is a binary segment and
// joins its neighbours in ONE append; a STATEMENT piece is a whole line that
// rebinds `acc` — an element walk, a union arm, an optional's branch — and the
// inline pieces on either side of it are two appends.
type wseg struct {
	inline string
	stmt   func(col int) []string
}

func (g *fixedGen) writeType(st *ir.Struct, val string) []wseg {
	var out []wseg
	for _, f := range st.Fields {
		out = append(out, g.writeField(f, val)...)
	}
	return out
}

// writeField is one field's pieces: the PRESENT FLAG where it has one, and
// then the payload.
//
// AN ABSENT OPTIONAL'S PAYLOAD IS ZERO (docs/SPEC-TABLES.md §3.4). The payload
// rides WHOLE whether or not the flag is set, and when the flag is 0 what rides
// is zeros — an absent optional is a hole in the record and not a window into
// the writer's own value. So the payload is a BRANCH, and a branch is a
// statement: the present arm appends the payload, the absent arm appends the
// zeros of exactly the payload's width.
func (g *fixedGen) writeField(f *ir.Field, val string) []wseg {
	name := g.fieldRef(val, f.Name)
	if f.Type.Optional {
		zeros := fixedFieldBytes(f) - ir.TableFixedPresentBytes
		payload := g.writePayload(f, val)
		return []wseg{
			{inline: fmt.Sprintf("if(%s_present, do: 1, else: 0)::unsigned-8", name)},
			{stmt: func(col int) []string {
				ind := indentOf(col)
				lines := []string{"", ind + "acc =", ind + "  if " + name + "_present do"}
				lines = append(lines, valueOf(g.writeStatements(payload, col+4))...)
				lines = append(lines, ind+"  else", fmt.Sprintf("%s    <<acc::binary, 0::size(%d)-unit(8)>>", ind, zeros), ind+"  end", "")
				return lines
			}},
		}
	}
	return g.writePayload(f, val)
}

// valueOf turns a block of `acc = ...` statements into one whose LAST line is
// the value itself: a branch answers its binary rather than rebinding a name
// nothing reads.
func valueOf(lines []string) []string {
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	last := len(lines) - 1
	for last >= 0 && !strings.Contains(lines[last], "acc =") && !strings.Contains(lines[last], " = ") {
		last--
	}
	if last < 0 {
		return lines
	}
	trim := strings.TrimSpace(lines[last])
	ind := lines[last][:len(lines[last])-len(trim)]
	if trim == "acc =" {
		out := append([]string{}, lines[:last]...)
		for _, l := range lines[last+1:] {
			out = append(out, strings.Replace(l, ind+"  ", ind, 1))
		}
		return out
	}
	lines[last] = ind + strings.TrimPrefix(trim, "acc = ")
	return lines
}

// fieldRef is how a field of `val` is reached: the DESTRUCTURED LOCAL where
// `val` is the function's own argument, and a map access under an inlined
// nested type, whose fields the one destructuring did not name.
func (g *fixedGen) fieldRef(val, field string) string {
	if val == "value" {
		return "v_" + field
	}
	return val + "." + field
}

func (g *fixedGen) writePayload(f *ir.Field, val string) []wseg {
	var out []wseg
	name := g.fieldRef(val, f.Name)
	switch {
	case f.KeyEnum != "":
		out = append(out, g.writeElements(f, name, f.KeyEnumRef.Max, 0)...)
	case f.Array == ir.ArrayFixed:
		out = append(out, g.writeElements(f, name, f.ArrayBound, 0)...)
	case f.Array == ir.ArrayCounted:
		// THE COUNT, THEN MAX ELEMENTS, the slack zero-filled. The count is
		// HELD TO ITS DECLARED RANGE on the way out: the variable leg raises
		// on the same number, and a zero segment of negative width would
		// only say `badarg`.
		out = append(out, g.writeElements(f, name, f.ArrayBound, f.ArrayMin)...)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		// THE LENGTH, THEN THE UNITS, THEN THE ZEROS OUT TO THE BOUND, and no
		// clamp: a value past its declared bound is a caller's contract break,
		// which this port raises on for the packet port's own reason — the
		// BEAM has no compile-out assert, so a check here is always on.
		out = append(out, wseg{inline: fmt.Sprintf("byte_size(%s)::little-signed-32", name)})
		out = append(out, wseg{inline: fmt.Sprintf("%s::binary", name)})
		out = append(out, wseg{inline: fmt.Sprintf("0::size(R.text_slack(%s, %d))-unit(8)", name, f.Type.Size)})
	case f.Type.Kind == ir.TWString:
		out = append(out, wseg{inline: fmt.Sprintf("div(byte_size(%s), 2)::little-signed-32", name)})
		out = append(out, wseg{inline: fmt.Sprintf("%s::binary", name)})
		out = append(out, wseg{inline: fmt.Sprintf("0::size(R.text_slack(%s, %d))-unit(8)", name, 2*f.Type.Size)})
	default:
		out = append(out, g.writeElement(f, name)...)
	}
	return out
}

// writeElements is a field's whole run of elements: the live count as a local
// (and on the wire where the array is counted), one append per element, and
// the slack behind the last one as a zero segment joining whatever follows.
// A fixed or keyed array has no count on the wire, and a short one is padded
// the way the reference's template pads it.
func (g *fixedGen) writeElements(f *ir.Field, name string, bound, min int64) []wseg {
	elem := fixedElementBytes(f)
	n := "n_" + strings.ReplaceAll(strings.TrimPrefix(name, "v_"), ".", "_")
	var out []wseg
	out = append(out, wseg{stmt: func(col int) []string {
		return []string{fmt.Sprintf("%s%s = R.count(%s, %d, %d)", indentOf(col), n, name, min, bound)}
	}})
	if f.Array == ir.ArrayCounted {
		out = append(out, wseg{inline: n + "::little-signed-32"})
	}
	walk := g.elementWalk(f, name)
	out = append(out, wseg{stmt: func(col int) []string { return g.assign("acc", walk, col) }})
	out = append(out, wseg{inline: fmt.Sprintf("0::size((%d - %s) * %d)-unit(8)", bound, n, elem)})
	return out
}

// elementWalk is `R.each` over a field's elements: a CAPTURE where the
// element's writer already is a two-argument function, and an appending fn for
// a leaf.
func (g *fixedGen) elementWalk(f *ir.Field, name string) node {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return rawf("R.each(%s, acc, &%s%s_fixed_write_into/2)", name, g.moduleOf(r), ir.RustSnake(r.Name))
		case *ir.Union:
			return rawf("R.each(%s, acc, &%s%s_fixed_write_into/2)", name, g.unionModule(r), ir.RustSnake(r.Name))
		}
	}
	return eachNode{list: name, body: binNode{segs: []string{"acc::binary", g.leafSegment(f, "e")}}}
}

// writeElement is ONE element: inline where its image is a run of segments,
// a statement where it is not.
func (g *fixedGen) writeElement(f *ir.Field, name string) []wseg {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			// A NESTED TYPE WHOSE IMAGE IS A RUN OF BYTES IS INLINED, which is
			// what makes a record of scalars and nested scalars ONE append and
			// not a chain of them.
			if fixedFlatType(r) {
				return g.writeType(r, name)
			}
			return []wseg{{stmt: g.intoStmt(fmt.Sprintf("%s%s_fixed_write_into(%s, acc)", g.moduleOf(r), ir.RustSnake(r.Name), name))}}
		case *ir.Union:
			return []wseg{{stmt: g.intoStmt(fmt.Sprintf("%s%s_fixed_write_into(%s, acc)", g.unionModule(r), ir.RustSnake(r.Name), name))}}
		}
	}
	return []wseg{{inline: g.leafSegment(f, name)}}
}

// intoStmt is `acc = <call>` at whatever column the body lands.
func (g *fixedGen) intoStmt(call string) func(col int) []string {
	return func(col int) []string { return g.assign("acc", raw(call), col) }
}

// leafSegment is a LEAF's binary segment: its DECLARED STORAGE IMAGE,
// LITTLE-ENDIAN, at its DECLARED STORAGE WIDTH. The width is the
// DECLARATION's, which SPEC.md fixes identically in every port, which is why a
// bits(12) costs four bytes here where §3 spends two.
func (g *fixedGen) leafSegment(f *ir.Field, name string) string {
	width := ir.TableFixedStorageBytes(f.Type) * 8
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
	// AN INTEGER LEAF IS HELD TO ITS RANGE ON THE WAY OUT. The reference spends
	// a debug-only `schema_assert` on a writer's contract; the BEAM has no
	// compile-out assert, so this port raises, which is exactly what its own
	// packet codec does for the same field. Without it a value past the
	// DECLARED range rides and the peer clamps it — and a value past the
	// STORAGE WIDTH is truncated by the binary construction in silence.
	//
	// A DECLARED RANGE RIDES AS A NAMED CONSTANT and the storage domain as the
	// WIDTH itself, for one reason: a bound can be a thirty-nine-digit number
	// (`int128 | min = ..., max = ...`), and two of them spelled inside a
	// binary segment make a line no shape of this emitter's could keep inside
	// the formatter's width.
	sign := "unsigned"
	if f.Type.Signed {
		sign = "signed"
	}
	if f.HasIntRange {
		return fmt.Sprintf("R.ranged(%s, %s)::little-%s-%d", name, g.rangeAttr(f), sign, width)
	}
	return fmt.Sprintf("R.fits(%s, %d, %t)::little-%s-%d", name, width, f.Type.Signed, sign, width)
}

// rangeAttr is the module attribute one ranged leaf's DECLARED bounds ride in,
// named for the declaration that owns the field so two types in one file cannot
// collide. It is defined by emitRangeAttrs before any helper that uses it.
func (g *fixedGen) rangeAttr(f *ir.Field) string {
	return fmt.Sprintf("@r_%s_%s", g.owner, ir.RustSnake(f.Name))
}

// emitRangeAttrs defines every ranged leaf's bounds for the declarations this
// module carries, and for every declaration a nested INLINED type brings with
// it, under the OWNER whose body spells the segment.
func (g *fixedGen) emitRangeAttrs(here []*ir.Struct, unions []*ir.Union) {
	seen := map[string]bool{}
	var one func(owner string, f *ir.Field)
	one = func(owner string, f *ir.Field) {
		if f.Type.Kind == ir.TNamed {
			if r, ok := f.Type.Ref.(*ir.Struct); ok && fixedFlatType(r) && f.Array == ir.ArrayNone && f.KeyEnum == "" {
				for _, sub := range r.Fields {
					one(owner, sub)
				}
				return
			}
		}
		if !f.HasIntRange {
			return
		}
		g.owner = owner
		name := g.rangeAttr(f)
		if seen[name] {
			return
		}
		seen[name] = true
		lo, hi := fixedRangeOf(f)
		// A 128-BIT BOUND IS THIRTY-NINE DIGITS AND TWO OF THEM DO NOT FIT, so
		// the pair breaks where the formatter breaks it: the second bound
		// aligned under the first, inside the brace that opened the tuple.
		if one := fmt.Sprintf("  %s {%s, %s}", name, lo, hi); len(one) <= formatWidth {
			g.pf("%s\n", one)
			return
		}
		g.pf("  %s {%s,\n%s%s}\n", name, lo, strings.Repeat(" ", len(name)+4), hi)
	}
	// A UNION'S STRUCT ARM IS A CALL and not an inlining, so only a LEAF arm
	// spells a bound under the union's own name.
	for _, u := range unions {
		for _, v := range u.Variants {
			if v.F != nil && v.F.HasIntRange {
				one(ir.RustSnake(u.Name), v.F)
			}
		}
	}
	for _, st := range here {
		for _, f := range st.Fields {
			one(ir.RustSnake(st.Name), f)
		}
	}
	if len(seen) > 0 {
		g.pf("\n")
	}
	g.owner = ""
}

// writeStatements turns a body's pieces into the lines of an `acc`-rebinding
// block at column `col`: every run of inline segments is one append, and every
// statement piece is itself.
func (g *fixedGen) writeStatements(segs []wseg, col int) []string {
	var lines []string
	var runs []string
	flush := func() {
		if len(runs) == 0 {
			return
		}
		lines = append(lines, g.assign("acc", binNode{segs: append([]string{"acc::binary"}, runs...)}, col)...)
		runs = nil
	}
	for _, s := range segs {
		if s.inline != "" {
			runs = append(runs, s.inline)
			continue
		}
		flush()
		lines = append(lines, s.stmt(col)...)
	}
	flush()
	return lines
}

// assign is `name = value` in the formatter's own shape: on one line where it
// fits, and otherwise the name, the `=`, and the value on its own line two
// columns in — set off by the blank line the formatter keeps on either side of
// a multi-line expression.
func (g *fixedGen) assign(name string, v node, col int) []string {
	if one := name + " = " + v.flat(); fits(col, 0, one) {
		return []string{indentOf(col) + one}
	}
	lines := []string{"", indentOf(col) + name + " ="}
	lines = append(lines, strings.Split(indentOf(col+2)+v.render(col+2, col+2, 0), "\n")...)
	return append(lines, "")
}

// eachNode is `R.each(list, acc, fn e, acc -> body end)`: on one line where it
// fits, and otherwise the formatter's shape for a call whose last argument is
// a fn — the head up to the arrow, the body two columns in, `end)` back at the
// call's column.
type eachNode struct {
	list string
	body node
}

func (n eachNode) flat() string {
	return "R.each(" + n.list + ", acc, fn e, acc -> " + n.body.flat() + " end)"
}

func (n eachNode) render(at, ind, tail int) string {
	if one := n.flat(); fits(at, tail, one) {
		return one
	}
	return "R.each(" + n.list + ", acc, fn e, acc ->\n" + indentOf(ind+2) + n.body.render(ind+2, ind+2, 0) + "\n" + indentOf(ind) + "end)"
}

// destructure is the ONE map match a body opens with: every field of the value
// into a local, which is the packet codec's own first line.
func (g *fixedGen) destructure(st *ir.Struct, col int) []string {
	var keys []string
	for _, f := range st.Fields {
		if f.Type.Optional {
			keys = append(keys, fmt.Sprintf("%s_present: v_%s_present", f.Name, f.Name))
		}
		keys = append(keys, fmt.Sprintf("%s: v_%s", f.Name, f.Name))
	}
	if one := "%{" + strings.Join(keys, ", ") + "} = value"; fits(col, 0, one) {
		return []string{indentOf(col) + one}
	}
	lines := []string{indentOf(col) + "%{"}
	for i, k := range keys {
		sep := ","
		if i == len(keys)-1 {
			sep = ""
		}
		lines = append(lines, indentOf(col+2)+k+sep)
	}
	return append(lines, indentOf(col)+"} = value", "")
}

// emitLines prints prepared lines, keeping the formatter's one blank line and
// never two, and none right after a `do`.
func (g *fixedGen) emitLines(lines []string) {
	for _, l := range lines {
		if l == "" && g.blank {
			continue
		}
		g.pf("%s\n", l)
	}
}

// ---------------------------------------------------------------------------
// THE READ: one binary pattern match, and the count beside it
// ---------------------------------------------------------------------------
//
// THE PROJECTION IS ONE MATCH AND ONE PASS. A type's `_fixed_decode(image, c, m)`
// matches its whole image in one head, holds every ranged value to the READER's
// own bounds as it lands, and answers `{value, c, m}` where `c` is the `clamped`
// count that came in plus every bound the record crossed and `m` is the
// CONTENT-RULE flag riding beside it. The count rides as a plain integer
// accumulator: a run of elements is a recursive walk with the count beside the
// list it builds, so a record of eighty elements allocates ONE tuple for the
// list and none for the count.
//
// THE FAST CLAUSE IS THE PACKET READER'S SHAPE. A record whose every ranged
// leaf is already inside this reader's bounds (and whose floats are finite)
// matches STRAIGHT INTO THE STRUCT: no `R.clamps`, no `min/max`, no
// `R.f32_value` binary per float, no helper per scalar. The slow clause is the
// hostile path and keeps the clamp. :eprof of the paired load named
// `R.clamps` and the two list walkers as the remaining read; the float helper
// was one percent.
//
// IT WALKS ONLY WHAT A READ CAN HAVE WRITTEN, which is the reference's rule
// (internal/codegen/cpptable/fixedform.go): a counted array's LIVE elements
// and never its slack, a union's named arm and no other, an optional's payload
// only when its present byte says so. Slack is unspecified on read and moves
// no counter (docs/SPEC-TABLES.md §3.4).

// rseg is one type's decode: the MATCH segments consuming its image, the
// STATEMENTS that turn what they bound into terms and move the count, and the
// FIELDS it contributes to the struct. The FAST half is the in-range clause:
// floats matched as floats, ranged leaves used as they stand, no clamp helper.
type rseg struct {
	match     []string
	matchFast []string
	post      []postStmt
	postFast  []postStmt
	keys      []string
	vals      []node
	valsFast  []node
	guards    []string
}

// postStmt is one `name = value` the projection makes after its match: a SHAPE
// and not a string, because a comprehension and a call break differently and
// the formatter knows which is which.
type postStmt struct {
	name string
	val  node
}

func (r *rseg) field(name string, slow, fast node) {
	r.keys = append(r.keys, name)
	r.vals = append(r.vals, slow)
	r.valsFast = append(r.valsFast, fast)
}

func (r *rseg) stmt(name string, v node) {
	r.post = append(r.post, postStmt{name: name, val: v})
}

func (r *rseg) stmtBoth(name string, v node) {
	p := postStmt{name: name, val: v}
	r.post = append(r.post, p)
	r.postFast = append(r.postFast, p)
}

func (r *rseg) merge(o rseg) {
	r.match = append(r.match, o.match...)
	r.matchFast = append(r.matchFast, o.matchFast...)
	r.post = append(r.post, o.post...)
	r.postFast = append(r.postFast, o.postFast...)
	r.keys = append(r.keys, o.keys...)
	r.vals = append(r.vals, o.vals...)
	r.valsFast = append(r.valsFast, o.valsFast...)
	r.guards = append(r.guards, o.guards...)
}

func (r rseg) hasFast() bool {
	if len(r.guards) > 0 {
		return true
	}
	if len(r.matchFast) != len(r.match) {
		return true
	}
	for i := range r.match {
		if r.matchFast[i] != r.match[i] {
			return true
		}
	}
	return false
}

func (r rseg) when() string {
	if len(r.guards) == 0 {
		return ""
	}
	return strings.Join(r.guards, " and ")
}

func (g *fixedGen) readType(st *ir.Struct, prefix string) rseg {
	var out rseg
	for _, f := range st.Fields {
		out.merge(g.readField(f, prefix))
	}
	return out
}

// readField is one field's decode. Where the field is OPTIONAL its payload is
// decoded whole — the value carries it either way, as the reference's storage
// does — but its bounds move the count only when the present byte says the
// writer put a value there.
func (g *fixedGen) readField(f *ir.Field, prefix string) rseg {
	v := prefix + f.Name
	var out rseg
	if f.Type.Optional {
		out.match = append(out.match, "p_"+v+"::unsigned-8")
		out.matchFast = append(out.matchFast, "p_"+v+"::unsigned-8")
		flag := rawf("p_%s != 0", v)
		out.field(f.Name+"_present", flag, flag)
		var payload rseg
		g.readPayload(&payload, f, v, rawf("p_%s != 0", v))
		// THE COUNT AND THE DAMAGE FLAG MOVE ONLY WHEN PRESENT: the payload's
		// statements run against a fork of both, and the fork is kept only if
		// the flag was set. An absent optional's payload is unspecified (§3.4)
		// and does not fire the content rule.
		out.match = append(out.match, payload.match...)
		out.matchFast = append(out.matchFast, payload.matchFast...)
		out.stmtBoth("c_"+v, raw("c"))
		out.stmtBoth("m_"+v, raw("m"))
		for _, p := range payload.post {
			out.post = append(out.post, postStmt{name: forkName(p.name, v), val: forkNode(p.val, v)})
		}
		for _, p := range payload.postFast {
			out.postFast = append(out.postFast, postStmt{name: forkName(p.name, v), val: forkNode(p.val, v)})
		}
		out.stmtBoth("c", rawf("if(p_%s != 0, do: c_%s, else: c)", v, v))
		out.stmtBoth("m", rawf("if(p_%s != 0, do: m_%s, else: m)", v, v))
		out.keys = append(out.keys, payload.keys...)
		out.vals = append(out.vals, payload.vals...)
		out.valsFast = append(out.valsFast, payload.valsFast...)
		return out
	}
	g.readPayload(&out, f, v, raw("true"))
	return out
}

// forkName and forkNode rename the count and the damage flag an optional's
// payload moves: the statements were emitted against `c` and `m` and here they
// read and write `c_<v>` and `m_<v>`.
func forkName(name, v string) string {
	name = strings.ReplaceAll(name, ", c, m}", ", c_"+v+", m_"+v+"}")
	name = strings.ReplaceAll(name, ", m}", ", m_"+v+"}")
	name = strings.ReplaceAll(name, ", c}", ", c_"+v+"}")
	if name == "c" {
		return "c_" + v
	}
	if name == "m" {
		return "m_" + v
	}
	return name
}

func forkNode(n node, v string) node {
	return raw(forkText(n.flat(), v))
}

func forkText(s, v string) string {
	s = strings.ReplaceAll(s, ", c, m)", ", c_"+v+", m_"+v+")")
	s = strings.ReplaceAll(s, "(c, ", "(c_"+v+", ")
	s = strings.ReplaceAll(s, ", c)", ", c_"+v+")")
	s = strings.ReplaceAll(s, ", m)", ", m_"+v+")")
	return s
}

func (g *fixedGen) readPayload(out *rseg, f *ir.Field, v string, present node) {
	switch {
	case f.KeyEnum != "":
		g.readElements(out, f, v, rawf("%d", f.KeyEnumRef.Max), rawf("%d", f.KeyEnumRef.Max))
	case f.Array == ir.ArrayFixed:
		g.readElements(out, f, v, rawf("%d", f.ArrayBound), rawf("%d", f.ArrayBound))
	case f.Array == ir.ArrayCounted:
		// THE COUNT IS CLAMPED HERE, to this reader's own bound, counting one
		// `clamped` if it moved (§4), and ONLY THE LIVE ELEMENTS ARE WALKED.
		seg := "n_" + v + "::little-signed-32"
		out.match = append(out.match, seg)
		out.matchFast = append(out.matchFast, seg)
		out.stmt("c", rawf("R.clamps(c, n_%s, 0, %d)", v, f.ArrayBound))
		out.guards = append(out.guards, fmt.Sprintf("n_%s >= 0", v), fmt.Sprintf("n_%s <= %d", v, f.ArrayBound))
		g.readElements(out, f, v,
			rawf("min(max(n_%s, 0), %d)", v, f.ArrayBound),
			raw("n_"+v))
	case f.Type.Kind == ir.TString:
		g.readText(out, f, v, 1, present)
	case f.Type.Kind == ir.TBytes:
		segN := "n_" + v + "::little-signed-32"
		segB := fmt.Sprintf("b_%s::binary-size(%d)", v, f.Type.Size)
		out.match = append(out.match, segN, segB)
		out.matchFast = append(out.matchFast, segN, segB)
		out.stmt("c", rawf("R.clamps(c, n_%s, 0, %d)", v, f.Type.Size))
		out.guards = append(out.guards, fmt.Sprintf("n_%s >= 0", v), fmt.Sprintf("n_%s <= %d", v, f.Type.Size))
		out.field(f.Name,
			rawf("binary_part(b_%s, 0, min(max(n_%s, 0), %d))", v, v, f.Type.Size),
			rawf("binary_part(b_%s, 0, n_%s)", v, v))
	case f.Type.Kind == ir.TWString:
		g.readText(out, f, v, 2, present)
	default:
		g.readElement(out, f, v, f.Name)
	}
}

// readText is a string(N) or wstring(N): the length is clamped to this
// reader's bound, the used units are held to the CONTENT rule, and a payload
// that is not the text its kind says it is reads the declared default with
// `malformed` beside the count. bytes(N) does not come here.
func (g *fixedGen) readText(out *rseg, f *ir.Field, v string, unit int64, present node) {
	bound := f.Type.Size
	bytes := unit * bound
	segN := "n_" + v + "::little-signed-32"
	segB := fmt.Sprintf("b_%s::binary-size(%d)", v, bytes)
	out.match = append(out.match, segN, segB)
	out.matchFast = append(out.matchFast, segN, segB)
	out.stmt("c", rawf("R.clamps(c, n_%s, 0, %d)", v, bound))
	out.guards = append(out.guards, fmt.Sprintf("n_%s >= 0", v), fmt.Sprintf("n_%s <= %d", v, bound))
	usedSlow := rawf("binary_part(b_%s, 0, %d * min(max(n_%s, 0), %d))", v, unit, v, bound)
	usedFast := rawf("binary_part(b_%s, 0, %d * n_%s)", v, unit, v)
	if unit == 1 {
		usedSlow = rawf("binary_part(b_%s, 0, min(max(n_%s, 0), %d))", v, v, bound)
		usedFast = rawf("binary_part(b_%s, 0, n_%s)", v, v)
	}
	def := g.fixedElemDefaultTerm(f)
	helper := "R.text"
	if f.Type.Kind == ir.TWString {
		helper = "R.wtext"
	}
	out.stmt("{t_"+v+", m}", call(helper, usedSlow, raw(def), raw("m"), present))
	out.postFast = append(out.postFast, postStmt{
		name: "{t_" + v + ", m}",
		val:  call(helper, usedFast, raw(def), raw("m"), present),
	})
	out.field(f.Name, raw("t_"+v), raw("t_"+v))
}

// readElements binds a field's whole run of elements and walks the LIVE ones
// into a list, the count riding beside it. A run of plain leaves nothing bounds
// is a comprehension; a small fixed run of unbounded leaves is matched in the
// parent so there is no sub-binary per field; anything with a bound in it is a
// walk of its own.
func (g *fixedGen) readElements(out *rseg, f *ir.Field, v string, live, liveFast node) {
	elem := fixedElementBytes(f)
	bound := fixedRunBound(f)
	if f.Array == ir.ArrayFixed && bound > 0 && bound <= 16 && f.Type.Kind != ir.TNamed {
		d := g.leafDec(f, "e")
		if d.count == nil {
			var names []string
			for i := int64(0); i < bound; i++ {
				name := fmt.Sprintf("f_%s_%d", v, i)
				seg := strings.Replace(d.spec, "f_e", name, 1)
				segFast := strings.Replace(d.specFast, "f_e", name, 1)
				out.match = append(out.match, seg)
				out.matchFast = append(out.matchFast, segFast)
				names = append(names, name)
			}
			list := raw("[" + strings.Join(names, ", ") + "]")
			out.field(f.Name, list, list)
			return
		}
	}
	seg := fmt.Sprintf("b_%s::binary-size(%d)", v, bound*elem)
	out.match = append(out.match, seg)
	out.matchFast = append(out.matchFast, seg)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			callSlow := call(fmt.Sprintf("%s%s_fixed_list", g.moduleOf(r), ir.RustSnake(r.Name)), raw("b_"+v), live, raw("c"), raw("m"))
			callFast := call(fmt.Sprintf("%s%s_fixed_list", g.moduleOf(r), ir.RustSnake(r.Name)), raw("b_"+v), liveFast, raw("c"), raw("m"))
			out.stmt("{l_"+v+", c, m}", callSlow)
			out.postFast = append(out.postFast, postStmt{name: "{l_" + v + ", c, m}", val: callFast})
			out.field(f.Name, raw("l_"+v), raw("l_"+v))
			return
		case *ir.Union:
			callSlow := call(fmt.Sprintf("%s%s_fixed_list", g.unionModule(r), ir.RustSnake(r.Name)), raw("b_"+v), live, raw("c"), raw("m"))
			callFast := call(fmt.Sprintf("%s%s_fixed_list", g.unionModule(r), ir.RustSnake(r.Name)), raw("b_"+v), liveFast, raw("c"), raw("m"))
			out.stmt("{l_"+v+", c, m}", callSlow)
			out.postFast = append(out.postFast, postStmt{name: "{l_" + v + ", c, m}", val: callFast})
			out.field(f.Name, raw("l_"+v), raw("l_"+v))
			return
		}
	}
	d := g.leafDec(f, "e")
	if d.count == nil {
		out.stmtBoth("l_"+v, forNode{gen: fmt.Sprintf("<<%s <- %s>>", d.spec, "b_"+v), body: d.wrap})
		if f.Array == ir.ArrayCounted {
			takeSlow := call("Enum.take", raw("l_"+v), live)
			takeFast := call("Enum.take", raw("l_"+v), liveFast)
			out.stmt("l_"+v, takeSlow)
			out.postFast = append(out.postFast, postStmt{name: "l_" + v, val: takeFast})
		}
		out.field(f.Name, raw("l_"+v), raw("l_"+v))
		return
	}
	callSlow := call(g.leafListName(v), raw("b_"+v), live, raw("c"), raw("m"))
	callFast := call(g.leafListName(v), raw("b_"+v), liveFast, raw("c"), raw("m"))
	out.stmt("{l_"+v+", c, m}", callSlow)
	out.postFast = append(out.postFast, postStmt{name: "{l_" + v + ", c, m}", val: callFast})
	out.field(f.Name, raw("l_"+v), raw("l_"+v))
	g.leafLists = append(g.leafLists, leafList{
		name: g.leafListName(v), spec: d.spec, wrap: d.wrap, wrapFast: d.wrapFast,
		count: d.count, guards: d.guards, bytes: ir.TableFixedStorageBytes(f.Type),
	})
}

// fixedRunBound is the slots a field's run holds.
func fixedRunBound(f *ir.Field) int64 {
	if f.KeyEnum != "" {
		return f.KeyEnumRef.Max
	}
	return f.ArrayBound
}

func (g *fixedGen) leafListName(v string) string {
	return g.owner + "_" + strings.ReplaceAll(v, ".", "_") + "_fixed_list"
}

// leafList is a walk over a run of BOUNDED leaves — a ranged integer, an enum
// ordinal — which a comprehension cannot count.
type leafList struct {
	name     string
	spec     string
	wrap     node
	wrapFast node
	count    node
	guards   []string
	bytes    int64
}

// readElement binds ONE element, inlining a nested type whose image is a run of
// leaves so the whole projection stays ONE binary pattern match.
func (g *fixedGen) readElement(out *rseg, f *ir.Field, v, field string) {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			if fixedFlatType(r) {
				sub := g.readType(r, v+"_")
				out.match = append(out.match, sub.match...)
				out.matchFast = append(out.matchFast, sub.matchFast...)
				out.post = append(out.post, sub.post...)
				out.postFast = append(out.postFast, sub.postFast...)
				out.guards = append(out.guards, sub.guards...)
				out.field(field, g.structNodeOf(r, sub), g.structNodeFast(r, sub))
				return
			}
			seg := fmt.Sprintf("b_%s::binary-size(%d)", v, fixedTypeBytes(r))
			out.match = append(out.match, seg)
			out.matchFast = append(out.matchFast, seg)
			calln := rawf("%s(b_%s, c, m)", g.decodeName(r), v)
			out.stmtBoth("{f_"+v+", c, m}", calln)
			out.field(field, raw("f_"+v), raw("f_"+v))
			return
		case *ir.Union:
			seg := fmt.Sprintf("b_%s::binary-size(%d)", v, fixedElementBytes(f))
			out.match = append(out.match, seg)
			out.matchFast = append(out.matchFast, seg)
			calln := rawf("%s%s_fixed_decode(b_%s, c, m)", g.unionModule(r), ir.RustSnake(r.Name), v)
			out.stmtBoth("{f_"+v+", c, m}", calln)
			out.field(field, raw("f_"+v), raw("f_"+v))
			return
		}
	}
	d := g.leafDec(f, v)
	out.match = append(out.match, d.spec)
	out.matchFast = append(out.matchFast, d.specFast)
	if d.count != nil {
		out.stmt("c", d.count)
	}
	out.guards = append(out.guards, d.guards...)
	out.field(field, d.wrap, d.wrapFast)
}

func (g *fixedGen) decodeName(st *ir.Struct) string {
	return fmt.Sprintf("%s%s_fixed_decode", g.moduleOf(st), ir.RustSnake(st.Name))
}

// leafDec is a LEAF's match segment, the term it makes, the fast-path term,
// the count statement it moves, and the guard the fast clause uses.
//
// A RANGED INTEGER CLAMPS HERE, against the READER's own bounds and nothing
// else — the bounds do not ride on this form (§3.4). AN ENUM ORDINAL PAST THE
// LAST VARIANT IS NOT A VARIANT: it lands None (0) and counts one `clamped`,
// which is the same answer §3 gives a value outside its range.
type leafDec struct {
	spec     string
	specFast string
	wrap     node
	wrapFast node
	count    node
	guards   []string
}

func (g *fixedGen) leafDec(f *ir.Field, v string) leafDec {
	name := "f_" + v
	width := ir.TableFixedStorageBytes(f.Type) * 8
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Enum:
			n := len(r.Variants)
			spec := fmt.Sprintf("%s::little-unsigned-%d", name, r.StorageBits)
			return leafDec{
				spec: spec, specFast: spec,
				wrap:     rawf("if(%s <= %d, do: %s, else: 0)", name, n, name),
				wrapFast: raw(name),
				count:    rawf("R.past(c, %s, %d)", name, n),
				guards:   []string{fmt.Sprintf("%s <= %d", name, n)},
			}
		case *ir.Flags:
			spec := name + "::little-unsigned-64"
			return leafDec{spec: spec, specFast: spec, wrap: raw(name), wrapFast: raw(name)}
		}
	}
	switch f.Type.Kind {
	case ir.TBool:
		// A BOOL LANDS AS `byte != 0`, which is what a form that writes 0 or 1
		// and reads anything owes a hostile writer.
		spec := name + "::unsigned-8"
		w := raw(name + " != 0")
		return leafDec{spec: spec, specFast: spec, wrap: w, wrapFast: w}
	case ir.TFloat32:
		return leafDec{
			spec:     name + "::little-unsigned-32",
			specFast: name + "::float-32-little",
			wrap:     rawf("R.f32_value(%s)", name),
			wrapFast: raw(name),
		}
	case ir.TFloat64:
		return leafDec{
			spec:     name + "::little-unsigned-64",
			specFast: name + "::float-64-little",
			wrap:     rawf("R.f64_value(%s)", name),
			wrapFast: raw(name),
		}
	}
	spec := fmt.Sprintf("%s::little-unsigned-%d", name, width)
	if f.Type.Signed {
		spec = fmt.Sprintf("%s::little-signed-%d", name, width)
	}
	lo, hi := fixedRangeOf(f)
	if lo == "nil" {
		return leafDec{spec: spec, specFast: spec, wrap: raw(name), wrapFast: raw(name)}
	}
	return leafDec{
		spec: spec, specFast: spec,
		wrap:     call("min", call("max", raw(name), raw(lo)), raw(hi)),
		wrapFast: raw(name),
		count:    call("R.clamps", raw("c"), raw(name), raw(lo), raw(hi)),
		guards:   []string{fmt.Sprintf("%s >= %s", name, lo), fmt.Sprintf("%s <= %s", name, hi)},
	}
}

func (g *fixedGen) structNodeOf(st *ir.Struct, r rseg) node {
	return structNode{mod: g.ns + "." + st.Name, keys: r.keys, vals: r.vals}
}

func (g *fixedGen) structNodeFast(st *ir.Struct, r rseg) node {
	return structNode{mod: g.ns + "." + st.Name, keys: r.keys, vals: r.valsFast}
}

// ---------------------------------------------------------------------------
// THE HELPERS one TYPE gets
// ---------------------------------------------------------------------------

func (g *fixedGen) typeHelpers(st *ir.Struct) {
	snake := ir.RustSnake(st.Name)
	size := fixedTypeBytes(st)

	g.pf("  # %s's body: %d bytes, the values in DECLARED ORDER, every field at its\n", st.Name, size)
	g.pf("  # declared storage width, nothing padded between fields.\n")
	g.emitShortDef(fmt.Sprintf("def %s_fixed_write_body(value)", snake), fmt.Sprintf("%s_fixed_write_into(value, <<>>)", snake))

	g.pf("  # %s's body APPENDED to `acc`: the value destructured once, then one append per\n", st.Name)
	g.pf("  # run of scalars, one per element of every array, and the slack as zero segments.\n")
	g.pf("  def %s_fixed_write_into(value, acc) do\n", snake)
	g.emitLines(g.destructure(st, 4))
	g.emitLines(g.writeStatements(g.writeType(st, "value"), 4))
	g.pf("    acc\n")
	g.pf("  end\n\n")

	g.pf("  # %s's projection: ONE binary pattern match over its %d bytes of record\n", st.Name, size)
	g.pf("  # image, the struct it makes, the `clamped` it moved and the content flag.\n")
	g.leafLists = nil
	r := g.readType(st, "")
	g.pf("  def %s_fixed_decode(bin, c, m \\\\ false)\n\n", snake)
	g.emitDecodeClauses(st, snake+"_fixed_decode", r, []string{"c", "m"}, 2)
	g.emitListHelpers(st, snake, size, r)
	g.leafLists = nil
}

func (g *fixedGen) emitDecodeClauses(st *ir.Struct, name string, r rseg, args []string, col int) {
	resultSlow := triple(g.structNodeOf(st, r), raw("c"), raw("m"))
	if r.hasFast() {
		g.emitMatchHead("def", name, r.matchFast, args, col, "_::binary", r.guards)
		g.emitPost(r.postFast, 4)
		g.pf("    %s\n", triple(g.structNodeFast(st, r), raw("c"), raw("m")).render(4, 4, 0))
		g.pf("  end\n\n")
	}
	g.emitMatchHead("def", name, r.match, args, col, "_::binary", nil)
	g.emitPost(r.post, 4)
	g.pf("    %s\n", resultSlow.render(4, 4, 0))
	g.pf("  end\n\n")
}

func (g *fixedGen) emitListHelpers(st *ir.Struct, snake string, size int64, r rseg) {
	g.pf("  # A RUN OF %s: the LIVE elements walked into a list, the count riding beside\n", st.Name)
	g.pf("  # it, and not a byte of the slack behind them read. Several elements per clause\n")
	g.pf("  # where the record is small, consed onto the recursive tail in order, no reverse.\n")
	g.pf("  # Hostile falls through to one element and the clamp.\n")
	g.emitForwardListBase("def", snake+"_fixed_list")
	if fixedFlatType(st) {
		if k := listUnrollK(size); k > 1 && r.hasFast() && r.unrollable() {
			g.emitUnrolledFlatList(st, snake, r, k)
		}
		if r.hasFast() {
			g.emitMatchHead("def", snake+"_fixed_list", r.matchFast, []string{"n", "c", "m"}, 2, "rest::binary", r.guards)
			g.emitPost(r.postFast, 4)
			g.emitLines(g.assign("v", g.structNodeFast(st, r), 4))
			g.pf("    {tail, c, m} = %s_fixed_list(rest, n - 1, c, m)\n", snake)
			g.pf("    {[v | tail], c, m}\n")
			g.pf("  end\n\n")
		}
		g.emitMatchHead("def", snake+"_fixed_list", r.match, []string{"n", "c", "m"}, 2, "rest::binary", nil)
		g.emitPost(r.post, 4)
		g.emitLines(g.assign("v", g.structNodeOf(st, r), 4))
		g.pf("    {tail, c, m} = %s_fixed_list(rest, n - 1, c, m)\n", snake)
		g.pf("    {[v | tail], c, m}\n")
	} else {
		g.pf("  def %s_fixed_list(<<e::binary-size(%d), rest::binary>>, n, c, m) do\n", snake, size)
		g.pf("    {v, c, m} = %s_fixed_decode(e, c, m)\n", snake)
		g.pf("    {tail, c, m} = %s_fixed_list(rest, n - 1, c, m)\n", snake)
		g.pf("    {[v | tail], c, m}\n")
	}
	g.pf("  end\n\n")

	for _, l := range g.leafLists {
		g.emitForwardListBase("defp", l.name)
		if k := listUnrollK(l.bytes); k > 1 && len(l.guards) > 0 {
			g.emitUnrolledLeafList(l, k)
		}
		if len(l.guards) > 0 {
			g.emitMatchHead("defp", l.name, []string{l.spec}, []string{"n", "c", "m"}, 2, "rest::binary", l.guards)
			g.pf("    {tail, c, m} = %s(rest, n - 1, c, m)\n", l.name)
			g.pf("    {[%s | tail], c, m}\n", l.wrapFast.flat())
			g.pf("  end\n\n")
		}
		g.emitMatchHead("defp", l.name, []string{l.spec}, []string{"n", "c", "m"}, 2, "rest::binary", nil)
		g.emitLines(g.assign("{tail, c, m}", call(l.name, raw("rest"), raw("n - 1"), l.count, raw("m")), 4))
		g.pf("    {[%s | tail], c, m}\n", l.wrap.flat())
		g.pf("  end\n\n")
	}
}

// listUnrollK is how many LIVE elements one fast clause matches. The packet
// reader's stats walk takes four 18-bit elements per clause; this form's
// elements are whole bytes, and eight 8-byte MixedStats is one cache line and
// the k that moved the identity load. Past that the match grew faster than the
// calls it saved. A larger record stays one element.
func listUnrollK(size int64) int {
	const maxK = 8
	const budget = 64
	if size <= 0 {
		return 1
	}
	k := int(budget / size)
	if k < 1 {
		return 1
	}
	if k > maxK {
		return maxK
	}
	return k
}

func (r rseg) unrollable() bool {
	// The slow clause's clamp statements ride `post`. The fast unroll only
	// needs matchFast, guards and valsFast, so a ranged leaf is still a leaf.
	return len(r.postFast) == 0
}

func (g *fixedGen) emitForwardListBase(kw, name string) {
	g.emitShortDef(fmt.Sprintf("%s %s(_bin, 0, c, m)", kw, name), "{[], c, m}")
}

func (g *fixedGen) emitUnrolledFlatList(st *ir.Struct, snake string, r rseg, k int) {
	var segs []string
	guards := []string{fmt.Sprintf("n >= %d", k)}
	elems := make([]rseg, k)
	for i := 0; i < k; i++ {
		elems[i] = suffixRseg(r, strconv.Itoa(i))
		segs = append(segs, elems[i].matchFast...)
		guards = append(guards, elems[i].guards...)
	}
	g.emitMatchHead("def", snake+"_fixed_list", segs, []string{"n", "c", "m"}, 2, "rest::binary", guards)
	names := make([]string, k)
	for i, el := range elems {
		names[i] = fmt.Sprintf("v%d", i)
		g.emitLines(g.assign(names[i], g.structNodeFast(st, el), 4))
	}
	g.pf("    {tail, c, m} = %s_fixed_list(rest, n - %d, c, m)\n", snake, k)
	g.pf("    {[%s | tail], c, m}\n", strings.Join(names, ", "))
	g.pf("  end\n\n")
}

func (g *fixedGen) emitUnrolledLeafList(l leafList, k int) {
	segs := make([]string, k)
	guards := []string{fmt.Sprintf("n >= %d", k)}
	wraps := make([]string, k)
	for i := 0; i < k; i++ {
		suf := strconv.Itoa(i)
		segs[i] = suffixIdent(l.spec, suf)
		guards = append(guards, suffixAll(l.guards, suf)...)
		wraps[i] = suffixIdent(l.wrapFast.flat(), suf)
	}
	g.emitMatchHead("defp", l.name, segs, []string{"n", "c", "m"}, 2, "rest::binary", guards)
	g.pf("    {tail, c, m} = %s(rest, n - %d, c, m)\n", l.name, k)
	g.pf("    {[%s | tail], c, m}\n", strings.Join(wraps, ", "))
	g.pf("  end\n\n")
}

var matchIdent = regexp.MustCompile(`\bf_[A-Za-z0-9_]+\b`)

func suffixIdent(s, suf string) string {
	return matchIdent.ReplaceAllStringFunc(s, func(m string) string { return m + "_" + suf })
}

func suffixAll(ss []string, suf string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = suffixIdent(s, suf)
	}
	return out
}

func suffixRseg(r rseg, suf string) rseg {
	out := rseg{
		match:     suffixAll(r.match, suf),
		matchFast: suffixAll(r.matchFast, suf),
		keys:      append([]string{}, r.keys...),
		guards:    suffixAll(r.guards, suf),
	}
	for _, v := range r.vals {
		out.vals = append(out.vals, raw(suffixIdent(v.flat(), suf)))
	}
	for _, v := range r.valsFast {
		out.valsFast = append(out.valsFast, raw(suffixIdent(v.flat(), suf)))
	}
	return out
}

// emitShortDef prints `def head, do: body`, broken after the head's comma with
// `do:` four columns in where the one line outgrows the width.
func (g *fixedGen) emitShortDef(head, body string) {
	if one := "  " + head + ", do: " + body; fits(0, 0, one) {
		g.pf("%s\n\n", one)
		return
	}
	g.pf("  %s,\n    do: %s\n\n", head, body)
}

// tuple2 is `{a, b}` as a shape: a struct that breaks keeps its brace beside
// the tuple's own.
func tuple2(a, b node) node {
	return pairNode{a: a, b: b}
}

// triple is `{a, b, c}` — the projection's `{value, clamped, malformed}`.
func triple(a, b, c node) node {
	return tripleNode{a: a, b: b, c: c}
}

type tripleNode struct{ a, b, c node }

func (t tripleNode) flat() string {
	return "{" + t.a.flat() + ", " + t.b.flat() + ", " + t.c.flat() + "}"
}

func (t tripleNode) render(at, ind, tail int) string {
	if one := t.flat(); fits(at, tail, one) {
		return one
	}
	return "{" + t.a.render(at+1, ind+1, tail+2+len(t.b.flat())+2+len(t.c.flat())) + ", " + t.b.flat() + ", " + t.c.flat() + "}"
}

type pairNode struct{ a, b node }

func (p pairNode) flat() string { return "{" + p.a.flat() + ", " + p.b.flat() + "}" }

func (p pairNode) render(at, ind, tail int) string {
	if one := p.flat(); fits(at, tail, one) {
		return one
	}
	return "{" + p.a.render(at+1, ind+1, tail+3+len(p.b.flat())) + ", " + p.b.flat() + "}"
}

// emitPost prints a projection's `name = value` statements in the formatter's
// own shape: on one line where it fits, and otherwise the name, the `=`, and
// the value on its own line two columns in — with the blank line the formatter
// puts after a multi-line assignment, and the one it puts after the whole block
// before the struct the projection makes.
func (g *fixedGen) emitPost(post []postStmt, col int) {
	for _, p := range post {
		g.emitLines(g.assign(p.name, p.val, col))
	}
	if len(post) > 0 {
		g.emitLines([]string{""})
	}
}

// emitMatchHead prints a function head whose FIRST argument is the whole image
// matched at once — filled greedily to the format width — and whose others
// follow it. Where the one-line head outgrows the width the formatter puts
// every argument on its own line and the closing paren under `do`, and this
// writes that shape rather than checking it after.
func (g *fixedGen) emitMatchHead(kw, name string, match []string, args []string, col int, rest string, guards []string) {
	ind := indentOf(col)
	head := fmt.Sprintf("%s %s(", kw, name)
	if rest == "" {
		rest = "_::binary"
	}
	pattern := binNode{segs: append(append([]string{}, match...), rest)}
	tail := ""
	if len(args) > 0 {
		tail = ", " + strings.Join(args, ", ")
	}
	when := ""
	if len(guards) > 0 {
		when = " when " + strings.Join(guards, " and ")
	}
	suffix := when + " do"
	if one := ind + head + pattern.render(col+len(head), col+len(head), len(tail)+len(suffix)+1) + tail + ")" + suffix; fits(0, 0, one) {
		g.pf("%s\n", one)
		return
	}
	// mix format hangs arguments two columns in from the closing paren, and the
	// closing paren hangs two columns in from `def`/`defp` plus the keyword's
	// extra letter: `def` closes at col+4, `defp` at col+5.
	closeCol := col + 1 + len(kw)
	argCol := closeCol + 2
	g.pf("%s%s\n", ind, head)
	g.pf("%s%s%s\n", indentOf(argCol), pattern.render(argCol, argCol, 1), commaIf(len(args) > 0))
	for i, a := range args {
		g.pf("%s%s%s\n", indentOf(argCol), a, commaIf(i < len(args)-1))
	}
	if len(guards) == 0 {
		g.pf("%s) do\n", indentOf(closeCol))
		return
	}
	g.pf("%s)\n", indentOf(closeCol))
	g.emitWhen(guards, closeCol-4)
}

// emitWhen writes a `when` chain the way mix format fills one: `when` under the
// closing paren (four columns in from `def`), trailing `and` at the break,
// continuations two columns in from the first guard.
func (g *fixedGen) emitWhen(guards []string, col int) {
	prefix := indentOf(col+4) + "when "
	cont := strings.Repeat(" ", len(prefix)+2)
	line := prefix + guards[0]
	for i := 1; i < len(guards); i++ {
		piece := " and " + guards[i]
		end := ""
		if i == len(guards)-1 {
			end = " do"
		}
		if !fits(0, 0, line+piece+end) {
			g.pf("%s and\n", line)
			line = cont + guards[i]
			continue
		}
		line += piece
	}
	g.pf("%s do\n", line)
}

func commaIf(b bool) string {
	if b {
		return ","
	}
	return ""
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
	g.emitShortDef(fmt.Sprintf("def %s_fixed_write_arm(value)", snake), fmt.Sprintf("%s_fixed_write_into(value, <<>>)", snake))
	g.pf("  def %s_fixed_write_into(value, acc) do\n", snake)
	g.pf("    acc = <<acc::binary, value.type::little-unsigned-%d>>\n\n", tag*8)
	g.pf("    case value.type do\n")
	arms := make([]caseArm, 0, len(u.Variants)+1)
	for i, v := range u.Variants {
		if v.F == nil {
			continue
		}
		arms = append(arms, caseArm{label: fmt.Sprintf("%d", i+1), body: g.armWriter(v.F, "value."+v.Name, widest-fixedFieldBytes(v.F))})
	}
	arms = append(arms, caseArm{label: "_", body: rawf("<<acc::binary, 0::size(%d)-unit(8)>>", widest)})
	g.emitArms(arms, 6)
	g.pf("    end\n")
	g.pf("  end\n\n")

	g.pf("  # union %s's projection: the tag, then the arm it names and no other. A TAG\n", u.Name)
	g.pf("  # BEYOND THE DECLARED ARMS lands None and counts one `clamped`.\n")
	g.pf("  def %s_fixed_decode(<<tag::little-unsigned-%d, arm::binary>>, c, m \\\\ false) do\n", snake, tag*8)
	g.pf("    case tag do\n")
	first := true
	for i, v := range u.Variants {
		if v.F == nil {
			continue
		}
		if !first {
			g.pf("\n")
		}
		first = false
		g.pf("      %d ->\n", i+1)
		for _, l := range g.armDecode(v.F, u, v.Name, i+1) {
			g.pf("        %s\n", l)
		}
	}
	g.pf("\n      0 ->\n        {%%%s.%s{}, c, m}\n", g.ns, u.Name)
	g.pf("\n      _ ->\n        {%%%s.%s{}, c + 1, m}\n", g.ns, u.Name)
	g.pf("    end\n")
	g.pf("  end\n\n")

	g.pf("  # A RUN OF %s: the LIVE elements walked into a list, the count beside it.\n", u.Name)
	g.emitForwardListBase("def", snake+"_fixed_list")
	g.pf("  def %s_fixed_list(<<e::binary-size(%d), rest::binary>>, n, c, m) do\n", snake, total)
	g.pf("    {v, c, m} = %s_fixed_decode(e, c, m)\n", snake)
	g.pf("    {tail, c, m} = %s_fixed_list(rest, n - 1, c, m)\n", snake)
	g.pf("    {[v | tail], c, m}\n")
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

// armWriter is ONE arm's bytes appended to `acc`, then the slack out to the
// widest arm as a zero segment.
func (g *fixedGen) armWriter(f *ir.Field, name string, slack int64) node {
	if f.Type.Kind == ir.TNamed {
		if r, ok := f.Type.Ref.(*ir.Struct); ok {
			into := fmt.Sprintf("%s%s_fixed_write_into(%s, acc)", g.moduleOf(r), ir.RustSnake(r.Name), name)
			if slack == 0 {
				return raw(into)
			}
			return binNode{segs: []string{into + "::binary", fmt.Sprintf("0::size(%d)-unit(8)", slack)}}
		}
	}
	segs := []string{"acc::binary", g.leafSegment(f, name)}
	if slack > 0 {
		segs = append(segs, fmt.Sprintf("0::size(%d)-unit(8)", slack))
	}
	return binNode{segs: segs}
}

// armDecode is one arm's lines: the arm's value out of `arm`, and the union
// struct naming it, with the count moved by whatever the arm bounds.
func (g *fixedGen) armDecode(f *ir.Field, u *ir.Union, field string, tag int) []string {
	mod := g.ns + "." + u.Name
	if f.Type.Kind == ir.TNamed {
		if r, ok := f.Type.Ref.(*ir.Struct); ok {
			return []string{
				fmt.Sprintf("{v, c, m} = %s(arm, c, m)", g.decodeName(r)),
				fmt.Sprintf("{%%%s{type: %d, %s: v}, c, m}", mod, tag, field),
			}
		}
	}
	d := g.leafDec(f, "arm")
	lines := []string{fmt.Sprintf("<<%s, _::binary>> = arm", d.spec)}
	if d.count != nil {
		lines = append(lines, "c = "+d.count.flat())
	}
	return append(lines, fmt.Sprintf("{%%%s{type: %d, %s: %s}, c, m}", mod, tag, field, d.wrap.flat()))
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
	g.pf("  # number, so a record of plain scalars collapses to a SINGLE run. THE IDENTITY\n")
	g.pf("  # READ DOES NOT RUN IT — a record whose hash is this build's own is projected\n")
	g.pf("  # straight out of the file — it is here for a caller who compiles plans.\n")
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

	g.pf("  # THE WRITE: the hash, then the body APPENDED to it. There is no measuring\n")
	g.pf("  # pass, no id interning, no trailer, no second walk and no flatten: a file\n")
	g.pf("  # of records is one binary every record was appended to in place.\n")
	g.pf("  def %s_fixed_record(value) do\n", snake)
	g.pf("    %s_fixed_write_into(value, <<@%s_hash::little-unsigned-64>>)\n", snake, snake)
	g.pf("  end\n\n")

	g.pf("  def %s_fixed_save(values) when is_list(values) do\n", snake)
	g.emitLines(g.assign("head", binNode{segs: []string{
		fmt.Sprintf("R.file_header(@%s_hash, byte_size(@%s_layout))::binary", snake, snake),
		fmt.Sprintf("@%s_layout::binary", snake),
	}}, 4))
	g.emitLines([]string{""})
	g.pf("    R.each(values, head, fn value, acc ->\n")
	g.pf("      %s\n", call(snake+"_fixed_write_into", raw("value"), rawf("<<acc::binary, @%s_hash::little-unsigned-64>>", snake)).render(6, 6, 0))
	g.pf("    end)\n")
	g.pf("  end\n\n")

	g.emitLoad(st, snake)
}

// emitLoad prints the root's reader: the form byte, the layout, and then ONE
// of two paths — the IDENTITY path, where the hash is this build's own and
// every record is projected straight out of the file, or the FOREIGN path,
// where a plan compiled once from the writer's layout lands each record onto
// the prefill first. The verdict is a tagged tuple and never an exception.
func (g *fixedGen) emitLoad(st *ir.Struct, snake string) {
	g.pf("  @doc \"\"\"\n")
	g.pf("  Read a form-3 FILE of %s records.\n\n", st.Name)
	g.pf("  `{:ok, values, report}` or `{:error, reason, report}`, the reason BY NAME.\n")
	g.pf("  Hostile bytes never raise: this is the family read verdict, the same one\n")
	g.pf("  the packet codec answers with.\n\n")
	g.pf("  A record whose hash is this build's own is ONE binary pattern match; any\n")
	g.pf("  other hash runs a plan compiled once from the writer's layout, and the\n")
	g.pf("  same projection reads what the plan landed.\n")
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

	g.pf("  # THE LAYOUT IS THIS BUILD'S OWN OR IT IS NOT, and that is one comparison of\n")
	g.pf("  # bytes before any hash is computed; a stranger's layout is hashed.\n")
	g.pf("  defp %s_fixed_records(stated, layout, records, report, opts) do\n", snake)
	g.pf("    hash = if(layout == @%s_layout, do: @%s_hash, else: R.hash(layout))\n", snake, snake)
	g.pf("    copy = Keyword.get(opts, :copy, false)\n\n")
	g.pf("    if hash == @%s_hash do\n", snake)
	g.pf("      %s_fixed_identity(stated, records, copy, report)\n", snake)
	g.pf("    else\n")
	g.pf("      %s_fixed_foreign(hash, stated, layout, records, copy, report, opts)\n", snake)
	g.pf("    end\n")
	g.pf("  end\n\n")

	g.pf("  # THE IDENTITY PATH: no plan, no prefill, no copy. Each record's hash is\n")
	g.pf("  # matched as a literal and its body projected in place.\n")
	g.pf("  defp %s_fixed_identity(stated, records, copy, report) do\n", snake)
	g.pf("    with :ok <- %s_fixed_header_names_it(stated, @%s_hash),\n", snake, snake)
	g.pf("         {:ok, values, c, m} <- %s_fixed_identity_loop(records, copy, [], 0, false) do\n", snake)
	g.pf("      {:ok, values, R.damaged(R.clamped(report, c), m)}\n")
	g.pf("    else\n")
	g.pf("      {:error, why} -> {:error, why, report}\n")
	g.pf("    end\n")
	g.pf("  end\n\n")

	g.pf("  # THE RECORDS FILL THE REST OF THE FILE and there is no count: a reader knows\n")
	g.pf("  # the record size from the layout, so the count is arithmetic. BYTES LEFT\n")
	g.pf("  # OVER ARE `malformed`, which is §3's rule for the same reason; a record\n")
	g.pf("  # whose hash names no layout this reader holds is a refusal by name.\n")
	g.pf("  # `malformed` ALSO RIDES BESIDE THE COUNT on a well-framed file: ill-formed\n")
	g.pf("  # text is one field's damage and the rest of the record stands.\n")
	g.pf("  defp %s_fixed_identity_loop(<<>>, _copy, acc, c, m),\n", snake)
	g.pf("    do: {:ok, :lists.reverse(acc), c, m}\n\n")
	g.emitIdentityRecord(snake, false)
	g.emitIdentityRecord(snake, true)
	g.pf("  defp %s_fixed_identity_loop(\n", snake)
	g.pf("         <<_::little-unsigned-64, _::binary-size(@%s_body_bytes), _::binary>>,\n", snake)
	g.pf("         _copy,\n")
	g.pf("         _acc,\n")
	g.pf("         _c,\n")
	g.pf("         _m\n")
	g.pf("       ) do\n")
	g.pf("    {:error, :no_layout}\n")
	g.pf("  end\n\n")
	g.emitShortDef(fmt.Sprintf("defp %s_fixed_identity_loop(_records, _copy, _acc, _c, _m)", snake), "{:error, :malformed}")

	g.pf("  # THE FOREIGN PATH: the plan compiled once from the writer's layout and cached\n")
	g.pf("  # by hash, run over each record onto the prefill, and the same projection\n")
	g.pf("  # over what it landed. The plan's own ops already held the writer's values\n")
	g.pf("  # to this reader's bounds and counted, so the projection counts nothing more.\n")
	g.pf("  defp %s_fixed_foreign(hash, stated, layout, records, copy, report, opts) do\n", snake)
	g.pf("    with {:ok, plan, size, report} <- %s_fixed_plan_for(hash, layout, report, opts),\n", snake)
	g.pf("         :ok <- %s_fixed_header_names_it(stated, hash),\n", snake)
	g.pf("         {:ok, bodies} <- %s_fixed_split(records, hash, size, []) do\n", snake)
	g.pf("      {values, report} =\n")
	g.pf("        Enum.map_reduce(bodies, report, fn body, report ->\n")
	g.pf("          {image, report} = R.run(plan, R.detach(body, copy), @%s_prefill, report)\n", snake)
	g.pf("          {value, c, m} = %s_fixed_decode(image, 0, false)\n", snake)
	g.pf("          {value, R.damaged(R.clamped(report, c), m)}\n")
	g.pf("        end)\n\n")
	g.pf("      {:ok, values, report}\n")
	g.pf("    else\n")
	g.pf("      {:error, why, report} -> {:error, why, report}\n")
	g.pf("      {:error, why} -> {:error, why, report}\n")
	g.pf("    end\n")
	g.pf("  end\n\n")

	g.pf("  # THE PLAN for any hash but this build's own: the one compiled ONCE from the\n")
	g.pf("  # writer's layout and cached by hash. A caller may own the plan instead and\n")
	g.pf("  # hand it in through `plan:`.\n")
	g.pf("  defp %s_fixed_plan_for(hash, layout, report, opts) do\n", snake)
	g.pf("    cap = Keyword.get(opts, :plan_capacity, R.plan_capacity())\n\n")
	g.pf("    case Keyword.get(opts, :plan) || R.cached(__MODULE__, hash) do\n")
	g.pf("      {_plan, _size, made} when made > cap ->\n")
	g.pf("        # THE PLAN'S CAPACITY IS THE CALLER'S BOUND AND NOT THE CACHE'S, so a\n")
	g.pf("        # plan already held for this peer is refused by the same name — and by\n")
	g.pf("        # the SAME NUMBER the compiler measured, which is what keeps a caller\n")
	g.pf("        # with a small capacity from inheriting a plan minted under a large one.\n")
	g.pf("        {:error, :plan_too_large, report}\n\n")
	g.pf("      {plan, size, _made} ->\n")
	g.pf("        {:ok, plan, size, report}\n\n")
	g.pf("      nil ->\n")
	g.pf("        # THE LAYOUT IS VALIDATED BEFORE A SINGLE RECORD BYTE IS TOUCHED, and\n")
	g.pf("        # every rule it fails refuses under ITS OWN NAME.\n")
	g.pf("        with {:ok, theirs} <- R.parse_layout(layout) do\n")
	g.pf("          mine = R.my_layout(__MODULE__, @%s_layout)\n\n", snake)
	g.pf("          case R.compile(theirs, mine, @%s_dst, cap, report) do\n", snake)
	g.pf("            {:ok, plan, made, report} ->\n")
	g.pf("              size = R.size_at(theirs, 0)\n")
	g.pf("              R.cache(__MODULE__, hash, {plan, size, made})\n")
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

	g.pf("  defp %s_fixed_split(<<>>, _hash, _size, acc), do: {:ok, :lists.reverse(acc)}\n\n", snake)
	g.pf("  defp %s_fixed_split(records, hash, size, acc) do\n", snake)
	g.pf("    case records do\n")
	g.pf("      <<h::little-unsigned-64, body::binary-size(^size), rest::binary>> when h == hash ->\n")
	g.pf("        %s_fixed_split(rest, hash, size, [body | acc])\n\n", snake)
	g.pf("      <<_::little-unsigned-64, _::binary-size(^size), _::binary>> ->\n")
	g.pf("        {:error, :no_layout}\n\n")
	g.pf("      _ ->\n")
	g.pf("        {:error, :malformed}\n")
	g.pf("    end\n")
	g.pf("  end\n\n")
}

// emitIdentityRecord is one identity-loop clause for `copy: false` (the body
// is already a sub-binary of the file) or `copy: true` (`:binary.copy/1` once
// per record, no helper). The hash is a LITERAL so the match is the identity.
func (g *fixedGen) emitIdentityRecord(snake string, copy bool) {
	g.pf("  defp %s_fixed_identity_loop(\n", snake)
	g.pf("         %s,\n", binNode{segs: []string{
		fmt.Sprintf("@%s_hash::little-unsigned-64", snake),
		fmt.Sprintf("body::binary-size(@%s_body_bytes)", snake),
		"rest::binary",
	}}.render(9, 9, 1))
	if copy {
		g.pf("         true,\n")
	} else {
		g.pf("         false,\n")
	}
	g.pf("         acc,\n")
	g.pf("         c,\n")
	g.pf("         m\n")
	g.pf("       ) do\n")
	if copy {
		g.pf("    {value, c, m} = %s_fixed_decode(:binary.copy(body), c, m)\n", snake)
	} else {
		g.pf("    {value, c, m} = %s_fixed_decode(body, c, m)\n", snake)
	}
	g.pf("    %s_fixed_identity_loop(rest, %t, [value | acc], c, m)\n", snake, copy)
	g.pf("  end\n\n")
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
