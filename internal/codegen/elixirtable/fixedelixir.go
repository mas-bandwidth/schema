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
	// lineage is the lock's entries per fixed table, OLDEST FIRST, the current
	// layout last (§5.2); nil for a unit that was never locked.
	lineage map[string][]FixedLineageEntry
	body    strings.Builder
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
func generateFixed(u *ir.Unit, ns string, lineage map[string][]FixedLineageEntry) (map[string][]byte, error) {
	roots := fixedRoots(u)
	if len(roots) == 0 {
		return nil, nil
	}
	closure := fixedClosure(roots)
	out := map[string][]byte{}
	out[FixedRuntimeModule+".ex"] = fixedRuntimeModule(u, ns)
	for _, f := range u.Files {
		g := &fixedGen{unit: u, ns: ns, file: f, lineage: lineage}
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
	g.emitLineageInit(myRoots)
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
// THERE IS ONE CLAUSE AND THE CLAMP IS IN IT. A record read through the
// identity plan and a record read through a compiled one reach the SAME head,
// so the bound every value is held to is written once and exercised by every
// read (Glenn, 2026-09-10: "one codepath always, whether version matches or
// not... fewer cases to test"). An in-range clause that skipped the clamp
// would be a second projection to test and a second place for a bound to go
// stale, and this form has neither.
//
// IT WALKS ONLY WHAT A READ CAN HAVE WRITTEN, which is the reference's rule
// (internal/codegen/cpptable/fixedform.go): a counted array's LIVE elements
// and never its slack, a union's named arm and no other, an optional's payload
// only when its present byte says so. Slack is unspecified on read and moves
// no counter (docs/SPEC-TABLES.md §3.4).

// rseg is one type's decode: the MATCH segments consuming its image, the
// STATEMENTS that turn what they bound into terms and move the count, and the
// FIELDS it contributes to the struct. One of each, because there is one
// clause.
type rseg struct {
	match []string
	post  []postStmt
	keys  []string
	vals  []node
}

// postStmt is one `name = value` the projection makes after its match: a SHAPE
// and not a string, because a comprehension and a call break differently and
// the formatter knows which is which.
type postStmt struct {
	name string
	val  node
}

func (r *rseg) field(name string, v node) {
	r.keys = append(r.keys, name)
	r.vals = append(r.vals, v)
}

func (r *rseg) stmt(name string, v node) {
	r.post = append(r.post, postStmt{name: name, val: v})
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

// readField is one field's decode. Where the field is OPTIONAL its payload is
// decoded whole — the value carries it either way, as the reference's storage
// does — but its bounds move the count only when the present byte says the
// writer put a value there.
func (g *fixedGen) readField(f *ir.Field, prefix string) rseg {
	v := prefix + f.Name
	var out rseg
	if f.Type.Optional {
		out.match = append(out.match, "p_"+v+"::unsigned-8")
		flag := rawf("p_%s != 0", v)
		out.field(f.Name+"_present", flag)
		var payload rseg
		g.readPayload(&payload, f, v, rawf("p_%s != 0", v))
		// THE COUNT AND THE DAMAGE FLAG MOVE ONLY WHEN PRESENT: the payload's
		// statements run against a fork of both, and the fork is kept only if
		// the flag was set. An absent optional's payload is unspecified (§3.4)
		// and does not fire the content rule.
		out.match = append(out.match, payload.match...)
		out.stmt("c_"+v, raw("c"))
		out.stmt("m_"+v, raw("m"))
		for _, p := range payload.post {
			out.post = append(out.post, postStmt{name: forkName(p.name, v), val: forkNode(p.val, v)})
		}
		out.stmt("c", rawf("if(p_%s != 0, do: c_%s, else: c)", v, v))
		out.stmt("m", rawf("if(p_%s != 0, do: m_%s, else: m)", v, v))
		out.keys = append(out.keys, payload.keys...)
		out.vals = append(out.vals, payload.vals...)
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
		g.readElements(out, f, v, rawf("%d", f.KeyEnumRef.Max))
	case f.Array == ir.ArrayFixed:
		g.readElements(out, f, v, rawf("%d", f.ArrayBound))
	case f.Array == ir.ArrayCounted:
		// THE COUNT IS CLAMPED HERE, to this reader's own bound, counting one
		// `clamped` if it moved (§4), and ONLY THE LIVE ELEMENTS ARE WALKED.
		seg := "n_" + v + "::little-signed-32"
		out.match = append(out.match, seg)
		out.stmt("c", rawf("R.clamps(c, n_%s, 0, %d)", v, f.ArrayBound))
		g.readElements(out, f, v, rawf("min(max(n_%s, 0), %d)", v, f.ArrayBound))
	case f.Type.Kind == ir.TString:
		g.readText(out, f, v, 1, present)
	case f.Type.Kind == ir.TBytes:
		segN := "n_" + v + "::little-signed-32"
		segB := fmt.Sprintf("b_%s::binary-size(%d)", v, f.Type.Size)
		out.match = append(out.match, segN, segB)
		out.stmt("c", rawf("R.clamps(c, n_%s, 0, %d)", v, f.Type.Size))
		out.field(f.Name, rawf("binary_part(b_%s, 0, min(max(n_%s, 0), %d))", v, v, f.Type.Size))
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
	out.stmt("c", rawf("R.clamps(c, n_%s, 0, %d)", v, bound))
	used := rawf("binary_part(b_%s, 0, %d * min(max(n_%s, 0), %d))", v, unit, v, bound)
	if unit == 1 {
		used = rawf("binary_part(b_%s, 0, min(max(n_%s, 0), %d))", v, v, bound)
	}
	def := g.fixedElemDefaultTerm(f)
	helper := "R.text"
	if f.Type.Kind == ir.TWString {
		helper = "R.wtext"
	}
	out.stmt("{t_"+v+", m}", call(helper, used, raw(def), raw("m"), present))
	out.field(f.Name, raw("t_"+v))
}

// readElements binds a field's whole run of elements and walks the LIVE ones
// into a list, the count riding beside it. A run of plain leaves nothing bounds
// is a comprehension; a small fixed run of unbounded leaves is matched in the
// parent so there is no sub-binary per field; anything with a bound in it is a
// walk of its own.
func (g *fixedGen) readElements(out *rseg, f *ir.Field, v string, live node) {
	elem := fixedElementBytes(f)
	bound := fixedRunBound(f)
	if f.Array == ir.ArrayFixed && bound > 0 && bound <= 16 && f.Type.Kind != ir.TNamed {
		d := g.leafDec(f, "e")
		if d.count == nil {
			var terms []string
			for i := range bound {
				name := fmt.Sprintf("f_%s_%d", v, i)
				out.match = append(out.match, strings.Replace(d.spec, "f_e", name, 1))
				// EACH SLOT IS WRAPPED THE WAY ONE LEAF IS. The segment binds
				// what the wire holds — a float's BITS, a bool's byte — and the
				// term is what the leaf makes of it, so a run of floats matched
				// in the parent lands floats and not integers.
				terms = append(terms, strings.ReplaceAll(d.wrap.flat(), "f_e", name))
			}
			out.field(f.Name, listNode{items: terms})
			return
		}
	}
	seg := fmt.Sprintf("b_%s::binary-size(%d)", v, bound*elem)
	out.match = append(out.match, seg)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			name := fmt.Sprintf("%s%s_fixed_list", g.moduleOf(r), ir.RustSnake(r.Name))
			out.stmt("{l_"+v+", c, m}", call(name, raw("b_"+v), live, raw("c"), raw("m")))
			out.field(f.Name, raw("l_"+v))
			return
		case *ir.Union:
			name := fmt.Sprintf("%s%s_fixed_list", g.unionModule(r), ir.RustSnake(r.Name))
			out.stmt("{l_"+v+", c, m}", call(name, raw("b_"+v), live, raw("c"), raw("m")))
			out.field(f.Name, raw("l_"+v))
			return
		}
	}
	d := g.leafDec(f, "e")
	if d.count == nil {
		out.stmt("l_"+v, forNode{gen: fmt.Sprintf("<<%s <- %s>>", d.spec, "b_"+v), body: d.wrap})
		if f.Array == ir.ArrayCounted {
			out.stmt("l_"+v, call("Enum.take", raw("l_"+v), live))
		}
		out.field(f.Name, raw("l_"+v))
		return
	}
	out.stmt("{l_"+v+", c, m}", call(g.leafListName(v), raw("b_"+v), live, raw("c"), raw("m")))
	out.field(f.Name, raw("l_"+v))
	g.leafLists = append(g.leafLists, leafList{
		name: g.leafListName(v), spec: d.spec, wrap: d.wrap,
		count: d.count, bytes: ir.TableFixedStorageBytes(f.Type),
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
	name  string
	spec  string
	wrap  node
	count node
	bytes int64
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
				out.post = append(out.post, sub.post...)
				out.field(field, g.structNodeOf(r, sub))
				return
			}
			out.match = append(out.match, fmt.Sprintf("b_%s::binary-size(%d)", v, fixedTypeBytes(r)))
			out.stmt("{f_"+v+", c, m}", rawf("%s(b_%s, c, m)", g.decodeName(r), v))
			out.field(field, raw("f_"+v))
			return
		case *ir.Union:
			out.match = append(out.match, fmt.Sprintf("b_%s::binary-size(%d)", v, fixedElementBytes(f)))
			out.stmt("{f_"+v+", c, m}", rawf("%s%s_fixed_decode(b_%s, c, m)", g.unionModule(r), ir.RustSnake(r.Name), v))
			out.field(field, raw("f_"+v))
			return
		}
	}
	d := g.leafDec(f, v)
	out.match = append(out.match, d.spec)
	if d.count != nil {
		out.stmt("c", d.count)
	}
	out.field(field, d.wrap)
}

func (g *fixedGen) decodeName(st *ir.Struct) string {
	return fmt.Sprintf("%s%s_fixed_decode", g.moduleOf(st), ir.RustSnake(st.Name))
}

// leafDec is a LEAF's match segment, the term it makes, and the count
// statement it moves.
//
// A RANGED INTEGER CLAMPS HERE, against the READER's own bounds and nothing
// else — the bounds do not ride on this form (§3.4). AN ENUM ORDINAL PAST THE
// LAST VARIANT IS NOT A VARIANT: it lands None (0) and counts one `clamped`,
// which is the same answer §3 gives a value outside its range.
//
// A FLOAT IS MATCHED AS ITS BITS AND NEVER AS A FLOAT, because `::float-32`
// does not match a NaN or an infinity and a form that read those as a match
// failure would need a second clause for them. The bits are one match for
// every value the kind can hold.
type leafDec struct {
	spec  string
	wrap  node
	count node
}

func (g *fixedGen) leafDec(f *ir.Field, v string) leafDec {
	name := "f_" + v
	width := ir.TableFixedStorageBytes(f.Type) * 8
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Enum:
			n := len(r.Variants)
			return leafDec{
				spec:  fmt.Sprintf("%s::little-unsigned-%d", name, r.StorageBits),
				wrap:  rawf("if(%s <= %d, do: %s, else: 0)", name, n, name),
				count: rawf("R.past(c, %s, %d)", name, n),
			}
		case *ir.Flags:
			return leafDec{spec: name + "::little-unsigned-64", wrap: raw(name)}
		}
	}
	switch f.Type.Kind {
	case ir.TBool:
		// A BOOL LANDS AS `byte != 0`, which is what a form that writes 0 or 1
		// and reads anything owes a hostile writer.
		return leafDec{spec: name + "::unsigned-8", wrap: raw(name + " != 0")}
	case ir.TFloat32:
		return leafDec{spec: name + "::little-unsigned-32", wrap: rawf("R.f32_value(%s)", name)}
	case ir.TFloat64:
		return leafDec{spec: name + "::little-unsigned-64", wrap: rawf("R.f64_value(%s)", name)}
	}
	spec := fmt.Sprintf("%s::little-unsigned-%d", name, width)
	if f.Type.Signed {
		spec = fmt.Sprintf("%s::little-signed-%d", name, width)
	}
	lo, hi := fixedRangeOf(f)
	if lo == "nil" {
		return leafDec{spec: spec, wrap: raw(name)}
	}
	return leafDec{
		spec:  spec,
		wrap:  call("min", call("max", raw(name), raw(lo)), raw(hi)),
		count: call("R.clamps", raw("c"), raw(name), raw(lo), raw(hi)),
	}
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
	g.emitMatchHead("def", name, r.match, args, col, "_::binary", nil)
	g.emitPost(r.post, 4)
	g.pf("    %s\n", triple(g.structNodeOf(st, r), raw("c"), raw("m")).render(4, 4, 0))
	g.pf("  end\n\n")
}

func (g *fixedGen) emitListHelpers(st *ir.Struct, snake string, size int64, r rseg) {
	g.pf("  # A RUN OF %s: the LIVE elements walked into a list, the count riding beside\n", st.Name)
	g.pf("  # it, and not a byte of the slack behind them read. Several elements per clause\n")
	g.pf("  # where the record is small, consed onto the recursive tail in order, no reverse.\n")
	g.pf("  # THE UNROLLED CLAUSE IS THE SAME CLAUSE k TIMES — the same clamps, the same\n")
	g.pf("  # counters — so it saves the match and the call and never a bound.\n")
	g.emitForwardListBase("def", snake+"_fixed_list")
	if fixedFlatType(st) {
		if k := listUnrollK(size); k > 1 {
			g.emitUnrolledFlatList(st, snake, r, k)
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
		if k := listUnrollK(l.bytes); k > 1 {
			g.emitUnrolledLeafList(l, k)
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

func (g *fixedGen) emitForwardListBase(kw, name string) {
	g.emitShortDef(fmt.Sprintf("%s %s(_bin, 0, c, m)", kw, name), "{[], c, m}")
}

func (g *fixedGen) emitUnrolledFlatList(st *ir.Struct, snake string, r rseg, k int) {
	var segs []string
	elems := make([]rseg, k)
	for i := range k {
		elems[i] = suffixRseg(r, strconv.Itoa(i))
		segs = append(segs, elems[i].match...)
	}
	g.emitMatchHead("def", snake+"_fixed_list", segs, []string{"n", "c", "m"}, 2, "rest::binary",
		[]string{fmt.Sprintf("n >= %d", k)})
	names := make([]string, k)
	for i, el := range elems {
		names[i] = fmt.Sprintf("v%d", i)
		// THE COUNT THREADS THROUGH THE k ELEMENTS IN ORDER, which is the
		// order the one-element clause would have visited them in, so the
		// unroll cannot change what `clamped` reports.
		for _, p := range el.post {
			g.emitLines(g.assign(p.name, p.val, 4))
		}
		g.emitLines(g.assign(names[i], g.structNodeOf(st, el), 4))
	}
	g.pf("    {tail, c, m} = %s_fixed_list(rest, n - %d, c, m)\n", snake, k)
	g.pf("    {[%s | tail], c, m}\n", strings.Join(names, ", "))
	g.pf("  end\n\n")
}

func (g *fixedGen) emitUnrolledLeafList(l leafList, k int) {
	segs := make([]string, k)
	names := make([]string, k)
	for i := range k {
		suf := strconv.Itoa(i)
		segs[i] = suffixIdent(l.spec, suf)
		names[i] = "v" + suf
	}
	g.emitMatchHead("defp", l.name, segs, []string{"n", "c", "m"}, 2, "rest::binary",
		[]string{fmt.Sprintf("n >= %d", k)})
	for i := range k {
		// THE COUNT THREADS THROUGH THE k ELEMENTS IN ORDER, which is the order
		// the one-element clause would have visited them in, so the unroll
		// cannot change what `clamped` reports. Each element's term is BOUND
		// rather than written into the cons, which keeps the tail one line.
		suf := strconv.Itoa(i)
		g.emitLines(g.assign("c", raw(suffixIdent(l.count.flat(), suf)), 4))
		g.emitLines(g.assign(names[i], raw(suffixIdent(l.wrap.flat(), suf)), 4))
	}
	g.pf("    {tail, c, m} = %s(rest, n - %d, c, m)\n", l.name, k)
	g.pf("    {[%s | tail], c, m}\n", strings.Join(names, ", "))
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
		match: suffixAll(r.match, suf),
		keys:  append([]string{}, r.keys...),
	}
	for _, p := range r.post {
		out.post = append(out.post, postStmt{name: p.name, val: raw(suffixIdent(p.val.flat(), suf))})
	}
	for _, v := range r.vals {
		out.vals = append(out.vals, raw(suffixIdent(v.flat(), suf)))
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
	entries := ir.TableFixedWalkRoot(st)
	layout := ir.TableFixedLayoutBytes(entries)
	hash := ir.TableFixedLayoutHash(layout, st)
	body := fixedTypeBytes(st)
	plan := fixedIdentityPlan(st)
	dst := imageDstRows(g.unit, entries)

	g.pf("  # ---- %s, the fixed form ----\n\n", st.Name)
	g.pf("  # MEASURE IS A CONSTANT on this form: the body is the same size for every\n")
	g.pf("  # value the type can hold (docs/SPEC-TABLES.md §3.4).\n")
	g.pf("  @%s_body_bytes %d\n", snake, body)
	g.pf("  @%s_record_bytes %d\n", snake, fixedHashBytes+body)
	g.pf("  @%s_hash 0x%016X\n\n", snake, hash)

	g.pf("  # THE LAYOUT: %d entries, a PRE-ORDER walk of the closure in the writer's\n", len(entries))
	g.pf("  # declared order — an id, a kind, a constant size and a child count each,\n")
	g.pf("  # seventeen bytes, every number little-endian. Every byte is settled by the\n")
	g.pf("  # compiler, and its fnv1a64 is the eight bytes every record carries.\n")
	g.pf("  @%s_layout %s\n\n", snake, fixedBinaryLiteral(layout, len("  @"+snake+"_layout ")))

	g.pf("  # MY SIDE of the layout, one row per entry: the storage facts a layout entry\n")
	g.pf("  # cannot carry. Here they are offsets into THIS BUILD's own record image,\n")
	g.pf("  # where the C++ reference's rows carry an offsetof into a struct.\n")
	g.pf("  @%s_dst %s\n\n", snake, g.dstLiteral(dst))

	g.pf("  # THE PREFILL: the DECLARED DEFAULTS as record bytes. A field this record\n")
	g.pf("  # does not carry has no plan entry at all, so these bytes are what lands\n")
	g.pf("  # there and the loop never learns the field existed.\n")
	g.pf("  @%s_prefill %s\n\n", snake, fixedBinaryLiteral(fixedDefaultsImage(st), len("  @"+snake+"_prefill ")))

	g.pf("  # THE IDENTITY PLAN, coalesced by the same rule the runtime's compiler uses:\n")
	g.pf("  # two neighbouring copies whose source and destination both advance together\n")
	g.pf("  # are one entry. In the image domain source and destination are the same\n")
	g.pf("  # number, so a record of plain scalars collapses to a SINGLE run. THE READ\n")
	g.pf("  # RUNS THIS PLAN like any other: a plan whose one entry covers the whole\n")
	g.pf("  # image lands the body itself as the image, which is a fact about the PLAN's\n")
	g.pf("  # SHAPE and not about whose build wrote the record (FixedRuntime.assemble/2).\n")
	g.pf("  @%s_plan %s\n\n", snake, g.planLiteral(plan))

	// ---- THE LINEAGE, OLDEST FIRST, THE CURRENT LAYOUT LAST (§5.2) ----
	//
	// It is THE BUILD's data and never the wire's: a file is matched on its
	// header's hash against these entries, and the layout it carries is held to
	// a BYTE COMPARISON against the bytes the lock recorded, never walked.
	lineage, floor := g.fixedLineage(st, FixedLineageEntry{
		Wire: hash, Layout: layout, Record: fixedHashBytes + body,
	})
	g.pf("  # THE LINEAGE: every layout of %s this build serves, OLDEST FIRST and\n", st.Name)
	g.pf("  # THE CURRENT ONE LAST (§5.2). A file is matched on its header's hash\n")
	g.pf("  # against these and on NOTHING ELSE; the layout it carries is COMPARED with\n")
	g.pf("  # the bytes the lock recorded and never parsed. A hash no entry holds is\n")
	g.pf("  # `:layout_newer` — ship the reader. The four members are §5.9 #19's, in\n")
	g.pf("  # its order, with the byte length riding BESIDE the bytes.\n")
	g.pf("  @%s_known {\n", snake)
	for i, e := range lineage {
		sep := ","
		if i == len(lineage)-1 {
			sep = ""
		}
		if e.Retired {
			g.pf("    # RETIRED: %s\n", e.Reason)
		}
		g.pf("    %%{\n")
		g.pf("      hash: 0x%016X,\n", e.Wire)
		// `mix format` WRAPS A LONG VALUE ONTO THE NEXT LINE, two columns in, and
		// the leg's gate compares against what it writes: the flat form while the
		// pair fits the width, the wrapped one once it does not.
		lit := fixedBinaryLiteral(e.Layout, len("      layout: "))
		if len("      layout: ")+len(lit)+1 <= formatWidth {
			g.pf("      layout: %s,\n", lit)
		} else {
			g.pf("      layout:\n        %s,\n", fixedBinaryLiteral(e.Layout, len("        ")))
		}
		g.pf("      layout_bytes: %d,\n", len(e.Layout))
		g.pf("      record_bytes: %d\n", e.Record)
		g.pf("    }%s\n", sep)
	}
	g.pf("  }\n\n")
	g.pf("  # THE FLOOR: 1 + the highest RETIRED index, 0 when none is (§5.2). Below it\n")
	g.pf("  # a layout this build once served is retired and the answer is\n")
	g.pf("  # `:layout_unsupported` — upgrade the client — rather than `:layout_newer`.\n")
	g.pf("  @%s_floor %d\n\n", snake, floor)
	retired := []string{}
	for _, e := range lineage {
		if e.Retired {
			retired = append(retired, fmt.Sprintf("{0x%016X, %q}", e.Wire, e.Reason))
		}
	}
	g.pf("  # THE RETIRED HASHES and the operator's own sentence for each, which is what\n")
	g.pf("  # `:layout_unsupported` means in words.\n")
	g.pf("  @%s_retired [%s]\n\n", snake, strings.Join(retired, ", "))

	g.pf("  def %s_fixed_body_bytes, do: @%s_body_bytes\n", snake, snake)
	g.pf("  def %s_fixed_record_bytes, do: @%s_record_bytes\n", snake, snake)
	g.pf("  def %s_fixed_hash, do: @%s_hash\n", snake, snake)
	g.pf("  def %s_fixed_layout, do: @%s_layout\n", snake, snake)
	g.pf("  def %s_fixed_plan, do: @%s_plan\n", snake, snake)
	g.pf("  def %s_fixed_prefill, do: @%s_prefill\n", snake, snake)
	g.pf("  def %s_fixed_dst, do: @%s_dst\n", snake, snake)
	g.pf("  def %s_fixed_known, do: @%s_known\n", snake, snake)
	g.pf("  def %s_fixed_floor, do: @%s_floor\n", snake, snake)
	g.pf("  def %s_fixed_retired, do: @%s_retired\n\n", snake, snake)

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

// emitLoad prints the root's reader: the form byte, the layout, the plan the
// layout's hash selects, and THE ONE LOOP that runs it. There is no second
// path for a record this build wrote — the identity plan is data, and the same
// `R.run` walks it (Glenn, 2026-09-10: "one codepath always, whether version
// matches or not"). The verdict is a tagged tuple and never an exception.
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
	g.pf("      {:error, :malformed} ->\n")
	g.pf("        # STEP 1: a file shorter than a header is the RESIDUE and not a bucket\n")
	g.pf("        # a named rule falls into — `malformed`, and no reason (§5.3).\n")
	g.pf("        {:error, :malformed, %%{report | malformed: true}}\n\n")
	g.pf("      {:error, why} ->\n")
	g.pf("        {:error, why, report}\n")
	g.pf("    end\n")
	g.pf("  end\n\n")

	g.pf("  # THE READ: §5.3's ELEVEN STEPS, IN ORDER. The header's hash is TAKEN AS\n")
	g.pf("  # GIVEN — the definitions digest is not on the wire, so the hash cannot be\n")
	g.pf("  # re-derived from the layout behind it — then the LINEAGE SELECT, then the\n")
	g.pf("  # floor, then the BYTE COMPARISON against the bytes the lock recorded.\n")
	g.pf("  # NOTHING PARSES A STRANGER'S LAYOUT, ON ANY PATH.\n")
	g.pf("  defp %s_fixed_records(stated, layout, records, report, opts) do\n", snake)
	g.pf("    copy = Keyword.get(opts, :copy, false)\n")
	g.pf("    cap = Keyword.get(opts, :plan_capacity, R.plan_capacity())\n")
	g.pf("    bcap = Keyword.get(opts, :batch_capacity, R.batch_capacity())\n\n")
	g.pf("    # A CALLER'S `plan:` IS ACCEPTED AND NOT READ (§5.9 #16): §5.6 retires\n")
	g.pf("    # MECHANISMS and not API, so the argument keeps its place and the plan a\n")
	g.pf("    # load runs is THE BUILD'S. `plan_capacity:` stays what §5.9 #5 says it is\n")
	g.pf("    # — a CAPACITY DECLARATION, checked and never written through.\n")
	g.pf("    _ = Keyword.get(opts, :plan)\n\n")
	g.pf("    case R.select(@%s_known, @%s_floor, stated, layout) do\n", snake, snake)
	g.pf("      {:error, why, file_hash} ->\n")
	g.pf("        # BOTH LAYOUT REFUSALS REPORT THE FILE'S HASH (§5.9 #7), on\n")
	g.pf("        # `layout_hash`, LAST on the report and zero on every other path\n")
	g.pf("        # (§5.9 #15). REFUSE IS TOTAL: no counter moves, nothing is decoded and\n")
	g.pf("        # not one destination byte is written — the prefill included.\n")
	g.pf("        {:error, why, %%{report | layout_hash: file_hash}}\n\n")
	g.pf("      {:ok, i} ->\n")
	g.pf("        %s_fixed_lane(i, records, stated, report, cap, bcap, copy)\n", snake)
	g.pf("    end\n")
	g.pf("  end\n\n")

	g.pf("  # THE PLAN IS THE BUILD'S, one per lineage entry, laid down at MODULE LOAD\n")
	g.pf("  # from the lock's own bytes (§5.9 #3). Nothing on this path compiles and\n")
	g.pf("  # nothing on it can fail for want of a plan: an entry whose plan would not\n")
	g.pf("  # build carries its own refusal reason, and this is where it is read.\n")
	g.pf("  defp %s_fixed_lane(i, records, hash, report, cap, bcap, copy) do\n", snake)
	g.pf("    case R.lineage_lane(__MODULE__, :%s, i) do\n", snake)
	g.pf("      :identity ->\n")
	g.pf("        %s\n", call(snake+"_fixed_run",
		rawf("@%s_plan", snake), rawf("@%s_body_bytes", snake), raw("records"), raw("hash"),
		raw("report"), raw("copy"), raw("bcap"), raw("{0, 0}")).render(8, 8, 0))
	g.pf("\n")
	g.pf("      {:ok, _plan, _size, made, _u, _k} when made > cap ->\n")
	g.pf("        # THE DECLARED CAPACITY A BUILD HOLDS AN ENTRY TO IS REAL (§5.9 #4) and\n")
	g.pf("        # the NAME is the contract (§5.9 #5).\n")
	g.pf("        {:error, :plan_too_large, report}\n\n")
	g.pf("      {:ok, plan, size, _made, u, k} ->\n")
	g.pf("        %s_fixed_run(plan, size, records, hash, report, copy, bcap, {u, k})\n\n", snake)
	g.pf("      {:error, why} ->\n")
	g.pf("        {:error, why, report}\n")
	g.pf("    end\n")
	g.pf("  end\n\n")

	g.pf("  # ONE PREFILL-AND-PROJECT PER RECORD, and the per-record hash checked BEFORE\n")
	g.pf("  # a byte is landed. Neither plan clamps a ranged integer: the generated\n")
	g.pf("  # projection holds the bound after the loop, the same pass for either plan.\n")
	g.pf("  defp %s_fixed_run(plan, size, records, hash, report, copy, bcap, census) do\n", snake)
	g.pf("    {census_u, census_k} = census\n\n")
	g.pf("    # §5.3 STEP 10, BEFORE A RECORD IS SPLIT OFF: `n := rest / record_bytes`,\n")
	g.pf("    # and a batch past the caller's room is a REFUSAL BY NAME. Step 9 runs\n")
	g.pf("    # first and owns the ragged tail, so a `rest` that is not a whole number\n")
	g.pf("    # of records passes this and the split reports `malformed`.\n")
	g.pf("    with :ok <- R.batch_within(byte_size(records), size + 8, bcap),\n")
	g.pf("         {:ok, bodies} <- %s_fixed_split(records, hash, size, []) do\n", snake)
	g.pf("      {values, report} =\n")
	g.pf("        Enum.map_reduce(bodies, report, fn body, report ->\n")
	g.pf("          {image, report} = R.run(plan, R.detach(body, copy), @%s_prefill, report)\n", snake)
	g.pf("          {value, c, m} = %s_fixed_decode(image, 0, false)\n", snake)
	g.pf("          {value, R.damaged(R.clamped(report, c), m)}\n")
	g.pf("        end)\n\n")
	g.pf("      # THE COMPILE CENSUS LANDS ONCE, AFTER THE LOOP, and only on a read that\n")
	g.pf("      # RETURNS (§5.9 #6): §5.4 says once per peer and never per record, and a\n")
	g.pf("      # refusal that came after would have moved a counter.\n")
	g.pf("      {:ok, values, R.census(report, census_u, census_k)}\n")
	g.pf("    else\n")
	g.pf("      {:error, :malformed} -> {:error, :malformed, %%{report | malformed: true}}\n")
	g.pf("      {:error, why} -> {:error, why, report}\n")
	g.pf("    end\n")
	g.pf("  end\n\n")

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

// emitLineageInit prints the module's @on_load: ONE PLAN PER LINEAGE ENTRY,
// built at MODULE LOAD from the lock's own bytes and held in :persistent_term
// (docs/FIXED-FORM-ALGORITHM.md §5.9 #3, #20). THE BEAM RUNS IT BEFORE ANY
// CALLER CAN REACH THE MODULE, so it is off every load path; and IT CANNOT
// THROW, because every entry is a lane carrying its own refusal reason — so the
// hook always answers `:ok` and the module always loads.
func (g *fixedGen) emitLineageInit(roots []*ir.Struct) {
	if len(roots) == 0 {
		return
	}
	g.pf("  # ONE PLAN PER LINEAGE ENTRY, BUILT AT MODULE LOAD from the lock's own bytes\n")
	g.pf("  # and held in `:persistent_term` (§5.9 #3, #20). The BEAM runs `@on_load`\n")
	g.pf("  # before any caller can reach this module, so the build is OFF EVERY LOAD\n")
	g.pf("  # PATH and a load can never fail for want of a plan. IT CANNOT THROW: every\n")
	g.pf("  # entry is a LANE WITH ITS OWN REFUSAL REASON, so the hook always answers\n")
	g.pf("  # `:ok` and the module always loads.\n")
	g.pf("  @on_load :__fixed_lineage__\n\n")
	g.pf("  @doc false\n")
	g.pf("  def __fixed_lineage__ do\n")
	for _, st := range roots {
		snake := ir.RustSnake(st.Name)
		g.pf("    R.lineage_init(\n")
		g.pf("      __MODULE__,\n")
		g.pf("      :%s,\n", snake)
		g.pf("      @%s_known,\n", snake)
		// THE READER'S OWN WIRE HASH, beside the lineage it is a key of (§5.3 step
		// 8). It cannot be computed from the layout bytes in the runtime — the
		// definitions digest is not in them — so the compiler hands it in.
		g.pf("      @%s_hash,\n", snake)
		g.pf("      @%s_layout,\n", snake)
		g.pf("      @%s_dst,\n", snake)
		g.pf("      R.plan_capacity()\n")
		g.pf("    )\n\n")
	}
	g.pf("    :ok\n")
	g.pf("  end\n\n")
}

// dstLiteral spells MY SIDE of the layout: one row per entry.
func (g *fixedGen) dstLiteral(dst []fixedDst) string {
	var sb strings.Builder
	sb.WriteString("{\n")
	for i, d := range dst {
		sep := ","
		if i == len(dst)-1 {
			sep = ""
		}
		row := tupleNode{items: []string{
			fmt.Sprintf("%d", d.dst), fmt.Sprintf("%d", d.stride), fmt.Sprintf("%d", d.aux),
			fmt.Sprintf("%d", d.counted), fmt.Sprintf("%d", d.arg),
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
