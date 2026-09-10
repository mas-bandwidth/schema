package elixirtable

import (
	"fmt"
	"strings"
)

// THE GENERATED ELIXIR IS `mix format`'s OWN SHAPE, and it is emitted that way
// rather than checked afterwards.
//
// The leg's gate runs `mix format --check-formatted` over what this backend
// writes, so an expression that outgrows the line has to break exactly where
// the formatter would break it. That is a property of the SHAPE and not of the
// text, so the emitter builds a small tree of shapes — a call, a struct
// literal, a bitstring, a list — and each one knows the two forms the
// formatter gives it: all on one line where it fits the width, and its own
// break where it does not.
//
// The width every one of them is measured against is `mix format`'s default
// line length, and the measure includes the TAIL — the comma or the closing
// bracket that will follow on the same line — because a shape that fits only
// when its comma is forgotten is a shape the formatter moves.

// node is one shape of the generated expression tree.
type node interface {
	// flat is the one-line form.
	flat() string
	// render answers the form that starts at column `at` with `tail`
	// characters following it on the closing line, and whose own broken form
	// indents from column `ind`. The two columns differ wherever a shape opens
	// after something else on its line — a struct literal behind its key, a
	// call behind an `=` — because the formatter measures from where the shape
	// STARTS and indents from where its STATEMENT does.
	render(at, ind, tail int) string
}

// raw is an expression the formatter never breaks: a variable, a literal, a
// short call this emitter has already sized.
type raw string

func (r raw) flat() string              { return string(r) }
func (r raw) render(_, _, _ int) string { return string(r) }
func fits(col, tail int, s string) bool {
	return col+len(s)+tail <= formatWidth && !strings.Contains(s, "\n")
}

func indentOf(col int) string { return strings.Repeat(" ", col) }

// callNode is `fn(a, b)`, which the formatter breaks ONE ARGUMENT PER LINE with
// the closing paren back at the call's own column.
type callNode struct {
	fn   string
	args []node
}

func call(fn string, args ...node) node { return callNode{fn: fn, args: args} }

func (c callNode) flat() string {
	parts := make([]string, 0, len(c.args))
	for _, a := range c.args {
		parts = append(parts, a.flat())
	}
	return c.fn + "(" + strings.Join(parts, ", ") + ")"
}

func (c callNode) render(at, ind, tail int) string {
	if one := c.flat(); fits(at, tail, one) {
		return one
	}
	pad := indentOf(ind + 2)
	var b strings.Builder
	b.WriteString(c.fn + "(\n")
	for i, a := range c.args {
		sep := ","
		if i == len(c.args)-1 {
			sep = ""
		}
		b.WriteString(pad + a.render(ind+2, ind+2, len(sep)) + sep + "\n")
	}
	b.WriteString(indentOf(ind) + ")")
	return b.String()
}

// structNode is `%Mod{k: v, ...}`, which the formatter breaks ONE KEY PER LINE
// with the closing brace back at the literal's own column.
//
// A VALUE THAT BREAKS BREAKS TWO WAYS, and which one is the value's own shape:
// a literal — another struct, a list — keeps its opening brace up beside the
// key and breaks inside it, while a CALL moves down to its own line two columns
// in and breaks from there. That is the formatter's rule and not a preference,
// so `hangs` below is the shape's answer to it.
type structNode struct {
	mod  string
	keys []string
	vals []node
}

func (s structNode) flat() string {
	parts := make([]string, 0, len(s.keys))
	for i, k := range s.keys {
		parts = append(parts, k+": "+s.vals[i].flat())
	}
	return "%" + s.mod + "{" + strings.Join(parts, ", ") + "}"
}

func (s structNode) render(at, ind, tail int) string {
	if one := s.flat(); fits(at, tail, one) {
		return one
	}
	pad := indentOf(ind + 2)
	var b strings.Builder
	b.WriteString("%" + s.mod + "{\n")
	for i, k := range s.keys {
		sep := ","
		if i == len(s.keys)-1 {
			sep = ""
		}
		v := s.vals[i]
		if one := k + ": " + v.flat(); fits(ind+2, len(sep), one) {
			b.WriteString(pad + one + sep + "\n")
			continue
		}
		if hangs(v) {
			b.WriteString(pad + k + ":\n" + indentOf(ind+4) + v.render(ind+4, ind+4, len(sep)) + sep + "\n")
			continue
		}
		b.WriteString(pad + k + ": " + v.render(ind+2+len(k)+2, ind+2, len(sep)) + sep + "\n")
	}
	b.WriteString(indentOf(ind) + "}")
	return b.String()
}

// hangs reports a shape that, when it breaks behind a `key:`, goes to its own
// line rather than opening beside the key.
func hangs(n node) bool {
	_, ok := n.(callNode)
	return ok
}

// binNode is `<<a, b, c>>`, which the formatter FILLS GREEDILY to the width
// with continuations two columns in — the one shape it does not break one item
// per line, so long as every item is short.
type binNode struct{ segs []string }

func (n binNode) flat() string { return "<<" + strings.Join(n.segs, ", ") + ">>" }

func (n binNode) render(at, ind, tail int) string {
	if one := n.flat(); fits(at, tail, one) {
		return one
	}
	cont := indentOf(ind + 2)
	var b strings.Builder
	b.WriteString("<<" + n.segs[0])
	line := at + 2 + len(n.segs[0])
	for i := 1; i < len(n.segs); i++ {
		after := 1
		if i == len(n.segs)-1 {
			after = 2 + tail
		}
		if line+2+len(n.segs[i])+after > formatWidth {
			b.WriteString(",\n" + cont + n.segs[i])
			line = len(cont) + len(n.segs[i])
			continue
		}
		b.WriteString(", " + n.segs[i])
		line += 2 + len(n.segs[i])
	}
	b.WriteString(">>")
	return b.String()
}

// listNode is `[a, b]`, which the formatter breaks ONE ITEM PER LINE with the
// closing bracket back at the list's own column.
type listNode struct{ items []node }

func (l listNode) flat() string {
	parts := make([]string, 0, len(l.items))
	for _, it := range l.items {
		parts = append(parts, it.flat())
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func (l listNode) render(at, ind, tail int) string {
	if one := l.flat(); fits(at, tail, one) {
		return one
	}
	pad := indentOf(ind + 2)
	var b strings.Builder
	b.WriteString("[\n")
	for i, it := range l.items {
		sep := ","
		if i == len(l.items)-1 {
			sep = ""
		}
		b.WriteString(pad + it.render(ind+2, ind+2, len(sep)) + sep + "\n")
	}
	b.WriteString(indentOf(ind) + "]")
	return b.String()
}

func rawf(format string, args ...any) node { return raw(fmt.Sprintf(format, args...)) }

// tupleNode is `{a, b, c}`, which the formatter FILLS GREEDILY to the width
// with continuations ONE column in — aligned inside the brace rather than two
// columns from the statement, which is the tuple's own shape and not the
// list's.
type tupleNode struct{ items []string }

func (t tupleNode) flat() string { return "{" + strings.Join(t.items, ", ") + "}" }

func (t tupleNode) render(at, _, tail int) string {
	if one := t.flat(); fits(at, tail, one) {
		return one
	}
	cont := indentOf(at + 1)
	var b strings.Builder
	b.WriteString("{" + t.items[0])
	line := at + 1 + len(t.items[0])
	for i := 1; i < len(t.items); i++ {
		after := 1
		if i == len(t.items)-1 {
			after = 1 + tail
		}
		if line+2+len(t.items[i])+after > formatWidth {
			b.WriteString(",\n" + cont + t.items[i])
			line = len(cont) + len(t.items[i])
			continue
		}
		b.WriteString(", " + t.items[i])
		line += 2 + len(t.items[i])
	}
	b.WriteString("}")
	return b.String()
}

// forNode is `for <generator>, do: <body>` — a comprehension, which the
// formatter breaks after the GENERATOR's comma and puts the `do:` two columns
// in, rather than one item per line the way a call breaks.
type forNode struct {
	gen  string
	body node
}

func (f forNode) flat() string { return "for " + f.gen + ", do: " + f.body.flat() }

func (f forNode) render(at, ind, tail int) string {
	if one := f.flat(); fits(at, tail, one) {
		return one
	}
	pad := indentOf(ind + 2)
	return "for " + f.gen + ",\n" + pad + "do: " + f.body.render(ind+2+4, ind+2, tail)
}
