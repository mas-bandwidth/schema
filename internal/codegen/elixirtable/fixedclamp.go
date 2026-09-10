package elixirtable

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE CLAMP COUNT, on the identity path (docs/SPEC-TABLES.md §3.4, §4).
//
// THE VALUE AND THE COUNT ARE TWO DIFFERENT QUESTIONS and this port answers
// them in two places. The projection clamps: `min(max(v, lo), hi)` sits inside
// the one binary pattern match, which is where a ranged integer, a count and a
// text length are held to the READER's own bounds — the read side always
// checks, and it checks here now that the plan no longer carries an op to check
// with (internal/codegen/elixirtable/fixedplan.go). The report counts: the
// `_fixed_clamped` pass this file emits reads the same image, over the same
// segments the projection matched, and answers ONE INTEGER — how many bounds
// the record's values crossed — which `FixedRuntime.clamped/2` adds to §4's
// counter in one update.
//
// WHY NOT ONE PASS. Threading a counter through the projection would put a
// tuple on the heap at every nested type and at every element of every list,
// because that is the only way a BEAM function returns two things; a record of
// eighty small elements would allocate eighty tuples to carry a number that is
// almost always zero. The counting pass allocates nothing: its match SKIPS
// every byte no bound is declared on — neighbouring skips merged into one — and
// its body is a column of `c = R.clamps(c, ...)` over integers.
//
// THE TWO PASSES CANNOT DRIFT. The counting head is DERIVED from the
// projection's own match segments rather than walked again, so a field's offset
// is a fact stated once; only the terms are walked, in the same order and by
// the same cases as the projection's own walk.
//
// A COMPILED PLAN COUNTS AS IT ALWAYS DID. Its `clamp`, `count` and `text` ops
// still hold a foreign writer's values to this reader's bounds and still bump
// `clamped` as they fire; the image they hand the projection is already in
// range, so this pass counts zero over it and nothing is counted twice.

// ---------------------------------------------------------------------------
// WHAT CAN CLAMP
// ---------------------------------------------------------------------------

// fixedClampsType reports whether a type's projection can move `clamped`.
func fixedClampsType(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if fixedClampsField(f) {
			return true
		}
	}
	return false
}

// fixedClampsField mirrors readField's own cases, and must keep mirroring them:
// a counted array has a count to clamp and a text has a length, whatever they
// carry, and everything else asks its element.
func fixedClampsField(f *ir.Field) bool {
	switch {
	case f.KeyEnum != "" || f.Array == ir.ArrayFixed:
		return fixedClampsElement(f)
	case f.Array == ir.ArrayCounted:
		return true
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TWString, f.Type.Kind == ir.TBytes:
		return true
	}
	return fixedClampsElement(f)
}

func fixedClampsElement(f *ir.Field) bool {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return fixedClampsType(r)
		case *ir.Union:
			return fixedClampsUnion(r)
		}
	}
	lo, _ := fixedRangeOf(f)
	return lo != "nil"
}

func fixedClampsUnion(u *ir.Union) bool {
	for _, v := range u.Variants {
		if v.F != nil && fixedClampsElement(v.F) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// THE COUNTING WALK
// ---------------------------------------------------------------------------

// clampWalk collects the terms one type's counting pass adds up, and the names
// out of the projection's match those terms read. A term is a SHAPE AND NOT A
// STRING because the accumulator it carries is `0` on the first line and `c` on
// every line after it, and the emitter is the only thing that knows which.
type clampWalk struct {
	terms []func(acc string) node
	used  map[string]bool
}

// term adds one term, and says which of the projection's bindings it reads.
func (c *clampWalk) term(build func(acc string) node, reads ...string) {
	c.terms = append(c.terms, build)
	for _, r := range reads {
		c.used[r] = true
	}
}

// bound is one value against one pair of bounds.
func (c *clampWalk) bound(name, lo, hi string) {
	c.term(func(acc string) node {
		return call("R.clamps", raw(acc), raw(name), raw(lo), raw(hi))
	}, name)
}

func (g *fixedGen) clampType(c *clampWalk, st *ir.Struct, prefix string) {
	for _, f := range st.Fields {
		g.clampField(c, f, prefix)
	}
}

func (g *fixedGen) clampField(c *clampWalk, f *ir.Field, prefix string) {
	// AN ABSENT OPTIONAL'S PAYLOAD IS IGNORED ON READ (§3.4), so it is not
	// held to a bound either: what rides there is the template and nobody
	// wrote it. LIVE elements only, slack never (rowan-256bc36cd9c9).
	if f.Type.Optional {
		inner := &clampWalk{used: map[string]bool{}}
		g.clampFieldBody(inner, f, prefix)
		if len(inner.terms) == 0 {
			return
		}
		p := "p_" + prefix + f.Name
		for name := range inner.used {
			c.used[name] = true
		}
		c.used[p] = true
		terms := inner.terms
		c.term(func(acc string) node {
			return optionalIf(p, acc, terms)
		})
		return
	}
	g.clampFieldBody(c, f, prefix)
}

func (g *fixedGen) clampFieldBody(c *clampWalk, f *ir.Field, prefix string) {
	v := prefix + f.Name
	switch {
	case f.KeyEnum != "":
		// EVERY SLOT OF A KEYED ARRAY IS LIVE (§2.4).
		g.clampElements(c, f, v, itoa(f.KeyEnumRef.Max))
	case f.Array == ir.ArrayFixed:
		g.clampElements(c, f, v, itoa(f.ArrayBound))
	case f.Array == ir.ArrayCounted:
		// THE COUNT IS CLAMPED TO THIS READER'S BOUND, one `clamped` if it
		// moved, and then the LIVE prefix only. Slack is the template's zeros
		// and a zero nobody wrote is not a value to hold to a range.
		c.bound("n_"+v, "0", itoa(f.ArrayBound))
		g.clampElements(c, f, v, fmt.Sprintf("min(max(n_%s, 0), %d)", v, f.ArrayBound))
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		c.bound("n_"+v, "0", itoa(f.Type.Size))
	case f.Type.Kind == ir.TWString:
		// A WIDE STRING'S LENGTH IS IN CODE UNITS, which is what its bound is in.
		c.bound("n_"+v, "0", itoa(f.Type.Size))
	default:
		g.clampElement(c, f, v)
	}
}

// clampElement is ONE element, following readElement's own branches: a flat
// nested type is projected inside this match and has nothing to clamp, and
// anything else is reached through its own counting pass.
func (g *fixedGen) clampElement(c *clampWalk, f *ir.Field, v string) {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			if fixedFlatType(r) {
				g.clampType(c, r, v+"_")
				return
			}
			if fixedClampsType(r) {
				g.clampCall(c, g.callClamped(r, "b_"+v), "b_"+v)
			}
			return
		case *ir.Union:
			if fixedClampsUnion(r) {
				g.clampCall(c, g.callUnionClamped(r, "b_"+v), "b_"+v)
			}
			return
		}
	}
	if lo, hi := fixedRangeOf(f); lo != "nil" {
		c.bound("f_"+v, lo, hi)
	}
}

// clampElements is a RUN of LIVE elements, which the projection bound as one
// binary: `live` is the count that walks, in declared order, and no key rides.
// A counted array passes min(max(n, 0), bound); a fixed or keyed array passes
// the bound, because every slot of those is live.
func (g *fixedGen) clampElements(c *clampWalk, f *ir.Field, v, live string) {
	elem := fixedElementBytes(f)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			if fixedClampsType(r) {
				g.clampRun(c, "R.clamps_each", v, []node{rawf("%d", elem), raw(live), raw(g.clampedCapture(r))})
			}
			return
		case *ir.Union:
			if fixedClampsUnion(r) {
				g.clampRun(c, "R.clamps_each", v, []node{rawf("%d", elem), raw(live), raw(g.unionClampedCapture(r))})
			}
			return
		}
	}
	lo, hi := fixedRangeOf(f)
	if lo == "nil" {
		return
	}
	signed := "false"
	if f.Type.Signed {
		signed = "true"
	}
	g.clampRun(c, "R.clamps_run", v, []node{rawf("%d", elem), raw(live), raw(signed), raw(lo), raw(hi)})
}

// optionalIf counts the payload only when the present byte is set. The
// keyword form is mix format's own one-line shape; a payload of more than
// one term breaks into do/end.
func optionalIf(p, acc string, terms []func(string) node) node {
	return ifBlock{cond: p, acc: acc, then: terms}
}

// ifBlock is `if p != 0, do: inner, else: acc`.
type ifBlock struct {
	cond string
	acc  string
	then []func(string) node
}

func (i ifBlock) keywordIf() string {
	if len(i.then) != 1 {
		return ""
	}
	return "if(" + i.cond + " != 0, do: " + i.then[0](i.acc).flat() + ", else: " + i.acc + ")"
}

func (i ifBlock) flat() string {
	if one := i.keywordIf(); one != "" {
		return one
	}
	return i.render(0, 0, 0)
}

func (i ifBlock) render(at, ind, tail int) string {
	if one := i.keywordIf(); one != "" && fits(at, tail, one) {
		return one
	}
	inner := indentOf(ind + 2)
	var b strings.Builder
	b.WriteString("if " + i.cond + " != 0 do\n")
	for j, build := range i.then {
		a := i.acc
		if j > 0 {
			a = "c"
		}
		t := build(a)
		if j < len(i.then)-1 {
			b.WriteString(inner + "c = " + t.flat() + "\n")
			continue
		}
		b.WriteString(inner + t.flat() + "\n")
	}
	b.WriteString(indentOf(ind) + "else\n")
	b.WriteString(inner + i.acc + "\n")
	b.WriteString(indentOf(ind) + "end")
	return b.String()
}

// clampCall is a term that is ONE call whose only accumulator is what it adds
// to: a nested type's own pass, which starts its own count at zero.
func (g *fixedGen) clampCall(c *clampWalk, expr, reads string) {
	c.term(func(acc string) node {
		if acc == "0" {
			return raw(expr)
		}
		return raw(acc + " + " + expr)
	}, reads)
}

// clampRun is a term over a whole RUN of elements, which takes the accumulator
// itself so the run adds into it without a `+` chain of its own.
func (g *fixedGen) clampRun(c *clampWalk, fn, v string, rest []node) {
	c.term(func(acc string) node {
		return call(fn, append([]node{raw(acc), raw("b_" + v)}, rest...)...)
	}, "b_"+v)
}

func (g *fixedGen) callClamped(st *ir.Struct, expr string) string {
	return fmt.Sprintf("%s%s_fixed_clamped(%s)", g.moduleOf(st), ir.RustSnake(st.Name), expr)
}

func (g *fixedGen) clampedCapture(st *ir.Struct) string {
	return fmt.Sprintf("&%s%s_fixed_clamped/1", g.moduleOf(st), ir.RustSnake(st.Name))
}

func (g *fixedGen) callUnionClamped(u *ir.Union, expr string) string {
	return fmt.Sprintf("%s%s_fixed_clamped(%s)", g.unionModule(u), ir.RustSnake(u.Name), expr)
}

func (g *fixedGen) unionClampedCapture(u *ir.Union) string {
	return fmt.Sprintf("&%s%s_fixed_clamped/1", g.unionModule(u), ir.RustSnake(u.Name))
}

// ---------------------------------------------------------------------------
// THE COUNTING PASS, emitted
// ---------------------------------------------------------------------------

// clampHelper prints one TYPE's counting pass, or nothing when the type has no
// bound anywhere in it. `match` is the projection's own segments.
func (g *fixedGen) clampHelper(st *ir.Struct, match []string) {
	if !fixedClampsType(st) {
		return
	}
	c := &clampWalk{used: map[string]bool{}}
	g.clampType(c, st, "")
	if len(c.terms) == 0 {
		return
	}
	snake := ir.RustSnake(st.Name)
	g.pf("  # %s's `clamped`: how many of its LIVE values crossed a bound this reader\n", st.Name)
	g.pf("  # declared. Slack behind a count is never counted, and an absent optional's\n")
	g.pf("  # payload is not a value to hold. THE PROJECTION BESIDE THIS ONE ALREADY\n")
	g.pf("  # HELD THE LIVE ONES THERE; this says how many times it had to.\n")
	g.emitMatchHead(snake+"_fixed_clamped", clampMatch(match, c.used), 2)
	g.emitClampBody(c.terms, 4)
	g.pf("  end\n\n")
}

// emitClampBody prints the column of accumulations, in `mix format`'s own
// shape: a term that fits is one line, a term that does not puts the call under
// its own `=` with a blank line either side of it, and the accumulator is the
// last expression.
func (g *fixedGen) emitClampBody(terms []func(acc string) node, col int) {
	if len(terms) == 1 {
		t := terms[0]("0")
		g.pf("%s%s\n", indentOf(col), t.render(col, col, 0))
		return
	}
	broke := false
	for i, build := range terms {
		acc := "c"
		if i == 0 {
			acc = "0"
		}
		t := build(acc)
		stmt := "c = " + t.flat()
		if fits(col, 0, stmt) {
			g.pf("%s%s\n", indentOf(col), stmt)
			broke = false
			continue
		}
		if i > 0 && !broke {
			g.pf("\n")
		}
		g.pf("%sc =\n%s%s\n\n", indentOf(col), indentOf(col+2), t.render(col+2, col+2, 0))
		broke = true
	}
	g.pf("%sc\n", indentOf(col))
}

// clampMatch is the counting pass's head, derived from the projection's own
// match segments: every binding this pass does not read becomes a SKIP, and
// neighbouring skips of known width become ONE skip. Deriving it is what makes
// a field's offset a fact stated once — a second walk could disagree with the
// first and the disagreement would be silent.
func clampMatch(match []string, used map[string]bool) []string {
	var out []string
	skip := int64(0)
	flush := func() {
		if skip > 0 {
			out = append(out, fmt.Sprintf("_::binary-size(%d)", skip))
			skip = 0
		}
	}
	for _, seg := range match {
		name, spec, _ := strings.Cut(seg, "::")
		if used[name] {
			flush()
			out = append(out, seg)
			continue
		}
		if n, ok := fixedSegBytes(spec); ok {
			skip += n
			continue
		}
		flush()
		if strings.HasPrefix(name, "_") {
			out = append(out, seg)
			continue
		}
		out = append(out, "_"+seg)
	}
	flush()
	return out
}

// fixedSegBytes is how many bytes a match segment consumes, or false where the
// segment's width is not a constant this emitter wrote.
func fixedSegBytes(spec string) (int64, bool) {
	if rest, ok := strings.CutPrefix(spec, "binary-size("); ok {
		num, closed := strings.CutSuffix(rest, ")")
		if !closed {
			return 0, false
		}
		v, err := strconv.ParseInt(num, 10, 64)
		return v, err == nil
	}
	i := strings.LastIndex(spec, "-")
	if i < 0 {
		return 0, false
	}
	bits, err := strconv.ParseInt(spec[i+1:], 10, 64)
	if err != nil || bits <= 0 || bits%8 != 0 {
		return 0, false
	}
	return bits / 8, true
}

// unionClampHelper prints a UNION's counting pass: the tag, then the arm it
// names and no other — which is exactly what the plan's guard used to decide.
func (g *fixedGen) unionClampHelper(u *ir.Union) {
	if !fixedClampsUnion(u) {
		return
	}
	snake := ir.RustSnake(u.Name)
	tag := fixedUnionTagBytes(u)
	g.pf("  # union %s's `clamped`: the arm the TAG names, and no other. AN ARM'S\n", u.Name)
	g.pf("  # BOUNDS ARE ONLY ITS OWN — a value under a tag this record does not carry\n")
	g.pf("  # was never read and never counted.\n")
	g.pf("  def %s_fixed_clamped(<<tag::little-unsigned-%d, arm::binary>>) do\n", snake, tag*8)
	g.pf("    case tag do\n")
	arms := make([]caseArm, 0, len(u.Variants)+1)
	for i, v := range u.Variants {
		if v.F == nil {
			continue
		}
		arms = append(arms, caseArm{label: fmt.Sprintf("%d", i+1), body: raw(g.armClamped(v.F))})
	}
	arms = append(arms, caseArm{label: "_", body: raw("0")})
	g.emitArms(arms, 6)
	g.pf("    end\n")
	g.pf("  end\n\n")
}

// armClamped is ONE arm's count, following armDecode's own two shapes.
func (g *fixedGen) armClamped(f *ir.Field) string {
	if f.Type.Kind == ir.TNamed {
		if r, ok := f.Type.Ref.(*ir.Struct); ok {
			if !fixedClampsType(r) {
				return "0"
			}
			return g.callClamped(r, fmt.Sprintf("binary_part(arm, 0, %d)", fixedTypeBytes(r)))
		}
	}
	lo, hi := fixedRangeOf(f)
	if lo == "nil" {
		return "0"
	}
	spec, _ := g.leafMatch(f, "arm")
	return fmt.Sprintf("(fn <<%s, _::binary>> -> R.clamps(0, f_arm, %s, %s) end).(arm)", spec, lo, hi)
}
