package cfmt

import (
	"strings"
)

// line is one logical output line: a statement, a brace, a label, a
// directive or a comment. Tokens with brk set begin continuation lines.
type line struct {
	level int
	toks  []*tok
}

type parser struct {
	toks  []*tok
	i     int
	lines []*line
	bad   bool
	macro bool // the tokens are a macro's body
}

func (p *parser) peek() *tok {
	if p.i < len(p.toks) {
		return p.toks[p.i]
	}
	return nil
}

func (p *parser) peekAt(n int) *tok {
	if p.i+n < len(p.toks) {
		return p.toks[p.i+n]
	}
	return nil
}

func (p *parser) newLine(level int) *line {
	ln := &line{level: level}
	p.lines = append(p.lines, ln)
	return ln
}

// take moves the next token onto ln.
func (p *parser) take(ln *line) *tok {
	t := p.toks[p.i]
	p.i++
	ln.toks = append(ln.toks, t)
	return t
}

// trailing moves comments that sit on the same input line onto ln.
func (p *parser) trailing(ln *line) {
	for {
		t := p.peek()
		if t == nil || !isComment(t) || t.nl > 0 {
			return
		}
		p.take(ln)
	}
}

// Format lays src out in the house style. It returns src unchanged when it
// cannot lex or pair the text, or when the result would differ from src in
// anything but whitespace.
func Format(src string) string {
	out, _ := format(src)
	return out
}

// Why names the reason Format left src alone, "" when it did not.
func Why(src string) string {
	_, why := format(src)
	return why
}

func format(src string) (string, string) {
	toks, ok := lex(src)
	if !ok {
		return src, "lex"
	}
	if len(toks) == 0 {
		return src, ""
	}
	if !matchBrackets(toks) {
		return src, "brackets"
	}
	classify(toks)
	p := &parser{toks: toks}
	p.items(0, false, true)
	if p.bad || p.i != len(toks) {
		at := ""
		if p.i < len(toks) {
			for j := p.i; j < len(toks) && j < p.i+12; j++ {
				at += toks[j].s + " "
			}
		}
		return src, "parse at token " + at
	}
	out := render(p.lines)
	if why := sameTokens(toks, out); why != "" {
		return src, why
	}
	return out, ""
}

// sameDirective holds a re-laid directive to the same rule: its tokens, with
// the line continuations gone, are the input's.
func sameDirective(a, b string) bool {
	body := func(s string) ([]*tok, bool) {
		s = strings.TrimLeft(strings.ReplaceAll(s, "\\\n", " "), " \t")
		return lex(strings.TrimPrefix(s, "#"))
	}
	x, ok := body(a)
	y, ok2 := body(b)
	if !ok || !ok2 || len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i].k != y[i].k || x[i].s != y[i].s {
			return false
		}
	}
	return true
}

// sameTokens holds the output to the input's token stream: the pass moves
// whitespace and nothing else, and no two tokens may have run together.
func sameTokens(toks []*tok, out string) string {
	got, ok := lex(out)
	if !ok {
		return "the output does not lex"
	}
	if len(got) != len(toks) {
		return "the output has a different token count"
	}
	for i, t := range toks {
		if got[i].k != t.k || got[i].s != t.s {
			if t.k == kPP && got[i].k == kPP && sameDirective(t.s, got[i].s) {
				continue
			}
			return "token differs: " + t.s + " became " + got[i].s
		}
	}
	return ""
}

// items parses statements until a closing brace or the end of input.
func (p *parser) items(level int, inSwitch bool, top bool) {
	for {
		t := p.peek()
		if t == nil {
			return
		}
		if t.k == kPunct && t.s == "}" {
			if top {
				// the close of an `extern "C" {` opened at top level
				ln := p.newLine(level)
				p.take(ln)
				p.trailing(ln)
				continue
			}
			return
		}
		if inSwitch && t.k == kWord && (t.s == "case" || t.s == "default") {
			p.caseLabel(level)
			continue
		}
		lv := level
		if inSwitch {
			lv = level + 1
		}
		p.statement(lv, nil)
		if p.bad {
			return
		}
	}
}

// caseLabel parses `case X:` and what follows it on the same line.
func (p *parser) caseLabel(level int) {
	ln := p.newLine(level)
	for {
		t := p.peek()
		if t == nil {
			p.bad = true
			return
		}
		p.take(ln)
		if t.k == kPunct && t.s == ":" {
			t.role = rLabelColon
			break
		}
	}
	p.trailing(ln)
	n := p.peek()
	if n == nil {
		return
	}
	if isPunct(n, "{") {
		p.block(level, ln, blockPlain, true)
		// the break that closes a braced case sits under the label
		if b := p.peek(); b != nil && b.k == kWord && b.s == "break" {
			p.simple(p.newLine(level), level)
		}
		return
	}
	if n.nl == 0 && p.shortCase() {
		for {
			n := p.peek()
			if n == nil || n.nl > 0 || isPunct(n, "}") || (n.k == kWord && (n.s == "case" || n.s == "default")) {
				break
			}
			p.statement(level+1, ln)
		}
	}
}

// shortCase reports whether the statements after a case label, on the
// label's input line, may stay there: one or two simple statements,
// followed by the next label or the closing brace.
func (p *parser) shortCase() bool {
	j := p.i
	stmts := 0
	for {
		if j >= len(p.toks) {
			return false
		}
		t := p.toks[j]
		if j > p.i && t.nl > 0 {
			break
		}
		if isPunct(t, "}") || (t.k == kWord && (t.s == "case" || t.s == "default")) {
			break
		}
		if t.k == kWord && (ctrlKeywords[t.s] || t.s == "do") || isComment(t) || t.k == kPP || isPunct(t, "{") {
			return false
		}
		// one statement: to its `;` at depth 0
		depth := 0
		for j < len(p.toks) {
			u := p.toks[j]
			if j > p.i && u.nl > 0 {
				return false
			}
			if u.k == kPunct {
				switch u.s {
				case "(", "[", "{":
					depth++
				case ")", "]", "}":
					depth--
				}
			}
			j++
			if depth == 0 && isPunct(u, ";") {
				break
			}
		}
		stmts++
	}
	if stmts == 0 || stmts > 2 {
		return false
	}
	t := p.toks[j]
	return isPunct(t, "}") || (t.k == kWord && (t.s == "case" || t.s == "default"))
}

type blockKind int

const (
	blockPlain blockKind = iota
	blockSwitch
	blockRecord
	blockEnum
)

// canMerge reports whether the brace at index o may stay on one line with
// its contents: the whole block on one input line, holding at most one
// simple statement.
func (p *parser) canMerge(o int) bool {
	c := p.toks[o].match
	if c <= o {
		return false
	}
	semis := 0
	for j := o + 1; j <= c; j++ {
		t := p.toks[j]
		if t.nl > 0 || t.k == kPP || isComment(t) {
			return false
		}
		if j == c {
			break
		}
		if t.k == kWord && (ctrlKeywords[t.s] || t.s == "do") && j == o+1 {
			return false
		}
		if isPunct(t, "{") && t.role != rInitOpen && !p.isInitBraceAt(j) {
			return false
		}
		if isPunct(t, ";") && p.depthBetween(o, j) == 0 {
			semis++
		}
	}
	if c == o+1 {
		return true
	}
	return semis == 1 && isPunct(p.toks[c-1], ";")
}

// depthBetween counts the brackets open between o (exclusive) and j.
func (p *parser) depthBetween(o, j int) int {
	d := 0
	for k := o + 1; k < j; k++ {
		t := p.toks[k]
		if t.k != kPunct {
			continue
		}
		if isOpen(t.s) {
			d++
		} else if isClose(t.s) {
			d--
		}
	}
	return d
}

// isInitBraceAt: a brace inside an expression (after `=`, `,`, `(`,
// `return`, or a compound literal's parens).
func (p *parser) isInitBraceAt(j int) bool {
	pr := prevCodeAll(p.toks, j)
	if pr == nil {
		return false
	}
	if pr.k == kPunct {
		switch pr.s {
		case "=", ",", "(", "{", "[":
			return true
		case ")":
			return pr.role == rGroupClose
		}
	}
	if pr.k == kWord && pr.s == "return" {
		return true
	}
	return false
}

func prevCodeAll(toks []*tok, i int) *tok {
	for j := i - 1; j >= 0; j-- {
		if !isComment(toks[j]) && toks[j].k != kPP {
			return toks[j]
		}
	}
	return nil
}

// statement parses one statement at level. With ln non-nil the statement
// continues that line (a short if body, a case body), else it starts a new
// one.
func (p *parser) statement(level int, ln *line) {
	t := p.peek()
	if t == nil {
		return
	}
	if t.k == kPP {
		pl := p.newLine(0)
		p.take(pl)
		return
	}
	if isComment(t) {
		cl := ln
		if cl == nil || t.nl > 0 {
			cl = p.newLine(level)
		}
		p.take(cl)
		return
	}
	if ln == nil && t.k == kWord && t.s != "default" && t.s != "case" && isPunct(p.peekAt(1), ":") {
		// a goto label sits one level out, alone on its line (`name:;`
		// keeps its empty statement)
		lv := level - 1
		if lv < 0 {
			lv = 0
		}
		ll := p.newLine(lv)
		p.take(ll)
		p.take(ll).role = rLabelColon
		if n := p.peek(); isPunct(n, ";") && n.nl == 0 {
			p.take(ll)
		}
		p.trailing(ll)
		return
	}
	if ln == nil {
		ln = p.newLine(level)
	}
	if t.k == kWord {
		switch t.s {
		case "if", "while", "for", "switch":
			p.take(ln)
			if !isPunct(p.peek(), "(") {
				p.bad = true
				return
			}
			p.expr(ln, level, p.toks[p.i].match)
			if t.s == "switch" {
				p.block(level, ln, blockSwitch, false)
				return
			}
			p.body(level, ln, false)
			if t.s == "if" {
				n := p.peek()
				if n != nil && n.k == kWord && n.s == "else" {
					el := p.newLine(level)
					p.take(el)
					p.trailing(el)
					n = p.peek()
					switch {
					case n == nil:
					case n.k == kWord && n.s == "if" && n.nl == 0:
						p.statement(level, el)
					default:
						p.body(level, el, true)
					}
				}
			}
			return
		case "do":
			p.take(ln)
			n := p.peek()
			var last *line
			if isPunct(n, "{") {
				if n.nl == 0 && p.canMerge(p.i) {
					p.inlineBlock(ln, level)
					last = ln
				} else {
					last = p.block(level, ln, blockPlain, true)
				}
			} else {
				p.statement(level+1, nil)
				last = p.newLine(level)
			}
			w := p.peek()
			if w == nil || w.k != kWord || w.s != "while" {
				p.bad = true
				return
			}
			p.simple(last, level)
			return
		}
	}
	if isPunct(t, "{") {
		p.block(level, ln, blockPlain, true)
		return
	}
	p.simple(ln, level)
}

// body parses the statement a control head governs.
func (p *parser) body(level int, ln *line, isElse bool) {
	p.trailing(ln)
	n := p.peek()
	if n == nil {
		return
	}
	if isPunct(n, "{") {
		if !isElse && n.nl == 0 && p.canMerge(p.i) {
			p.inlineBlock(ln, level)
			p.trailing(ln)
			return
		}
		p.block(level, ln, blockPlain, true)
		return
	}
	if isPunct(n, ";") {
		p.take(ln)
		p.trailing(ln)
		return
	}
	// a simple statement stays on the head's line; a control statement
	// never does
	if n.nl == 0 && !isComment(n) && !(n.k == kWord && (ctrlKeywords[n.s] || n.s == "do")) {
		p.statement(level, ln)
		return
	}
	p.statement(level+1, nil)
}

// inlineBlock moves a mergeable `{ ... }` onto ln.
func (p *parser) inlineBlock(ln *line, level int) {
	o := p.take(ln)
	o.role = rBlockOpen
	c := o.match
	for p.i < c {
		p.simple(ln, level)
		if p.bad {
			return
		}
	}
	cl := p.take(ln)
	cl.role = rBlockClose
}

// block parses a braced block laid out Allman style; it returns the line
// holding the closing brace. head is the line the block belongs to: when
// mayMerge and the block fits on one input line, it stays there.
func (p *parser) block(level int, head *line, kind blockKind, separate bool) *line {
	o := p.peek()
	if o == nil || !isPunct(o, "{") {
		p.bad = true
		return head
	}
	if len(head.toks) == 0 {
		// a block on its own: the head line is the brace line
	} else {
		head = p.newLine(level)
	}
	p.take(head).role = rBlockOpen
	p.trailing(head)
	c := o.match
	switch kind {
	case blockSwitch:
		p.items(level+1, true, false)
	case blockEnum:
		p.enumBody(level+1, c)
	default:
		p.items(level+1, false, false)
	}
	if p.i != c {
		p.bad = true
		return head
	}
	cl := p.newLine(level)
	p.take(cl).role = rBlockClose
	p.trailing(cl)
	return cl
}

// enumBody puts each enumerator on its own line.
func (p *parser) enumBody(level, c int) {
	for p.i < c {
		t := p.peek()
		if t.k == kPP {
			p.take(p.newLine(0))
			continue
		}
		ln := p.newLine(level)
		if isComment(t) {
			p.take(ln)
			continue
		}
		for p.i < c {
			u := p.peek()
			if u.k == kPP {
				break
			}
			if isComment(u) && u.nl > 0 {
				break
			}
			p.take(ln)
			if u.k == kPunct && isOpen(u.s) {
				p.expr(ln, level, u.match)
				continue
			}
			if isPunct(u, ",") {
				p.trailing(ln)
				break
			}
		}
	}
}

// expr moves tokens through index end (inclusive) onto ln, keeping the
// input's line breaks where a break is allowed. It starts at p.i, which may
// be an opening bracket whose match is end.
func (p *parser) expr(ln *line, level int, end int) {
	for p.i <= end && p.i < len(p.toks) {
		t := p.peek()
		if isPunct(t, "{") && t.role == rNone {
			t.role = rInitOpen
			p.toks[t.match].role = rInitClose
			p.trailingComma(p.i)
		}
		p.place(ln, t)
		p.i++
	}
}

// trailingComma: a braced list whose last element carries a comma is laid
// out one element to a line, the closing brace on its own.
func (p *parser) trailingComma(o int) {
	c := p.toks[o].match
	if last := prevCodeAll(p.toks, c); c <= o+1 || !isPunct(last, ",") {
		return
	}
	first := func(j int) {
		for j < c && isComment(p.toks[j]) && p.toks[j].nl == 0 {
			j++
		}
		p.toks[j].force = true
	}
	p.toks[o+1].force = true
	depth := 0
	for j := o + 1; j < c; j++ {
		t := p.toks[j]
		if t.k != kPunct {
			continue
		}
		switch {
		case isOpen(t.s):
			depth++
		case isClose(t.s):
			depth--
		case t.s == "," && depth == 0:
			first(j + 1)
		}
	}
	p.toks[c].force = true
}

// place appends t to ln, deciding whether the input's line break before it
// survives.
func (p *parser) place(ln *line, t *tok) {
	if t.force && len(ln.toks) > 0 {
		t.brk = true
	} else if len(ln.toks) > 0 && (t.nl > 0 || t.k == kPP) {
		prev := ln.toks[len(ln.toks)-1]
		if t.k == kPP || isComment(t) || isComment(prev) || prev.k == kPP {
			t.brk = true
		} else if canBreakBefore(prev, t) {
			t.brk = true
		}
	}
	ln.toks = append(ln.toks, t)
}

// canBreakBefore: clang-format keeps an input break only where it may break.
func canBreakBefore(prev, t *tok) bool {
	if prev.k == kPunct {
		switch prev.s {
		case ",", "(", "{", "[", ";":
			return true
		case "?", ":":
			return true
		}
		if binaryOps[prev.s] && prev.role != rUnary && prev.role != rPtrCont && prev.s != "*" && prev.s != "&" {
			return true
		}
		if (prev.s == "*" || prev.s == "&") && prev.role == rBinary {
			return true
		}
	}
	if t.k == kPunct && t.s == "}" && t.role == rInitClose {
		return false
	}
	if t.k == kStr && prev.k == kStr {
		return true
	}
	return false
}

// simple parses a statement or declaration onto ln: to its `;`, or through
// the body of a function, struct, union or enum it defines.
func (p *parser) simple(ln *line, level int) {
	hasRecord, hasEnum := false, false
	for {
		t := p.peek()
		if t == nil {
			// a macro body ends without its `;`
			p.bad = !p.macro
			return
		}
		if t.k == kWord {
			switch t.s {
			case "struct", "union":
				hasRecord = true
			case "enum":
				hasEnum = true
			}
		}
		switch {
		case isPunct(t, ";"):
			p.place(ln, t)
			p.i++
			p.trailing(ln)
			return
		case isPunct(t, "}"):
			// a statement that ends at a closing brace (a merged block's
			// last token is handled by the caller)
			return
		case isPunct(t, "{"):
			if p.isInitBraceAt(p.i) {
				p.expr(ln, level, t.match)
				continue
			}
			pr := prevCodeAll(p.toks, p.i)
			switch {
			case pr != nil && isPunct(pr, ")"):
				// a function body
				if p.canMerge(p.i) {
					if t.nl > 0 {
						ln = p.newLine(level)
					}
					p.inlineBlock(ln, level)
					p.trailing(ln)
				} else {
					p.block(level, ln, blockPlain, true)
				}
				return
			case hasEnum:
				ln = p.block(level, ln, blockEnum, true)
				continue
			case hasRecord:
				ln = p.block(level, ln, blockRecord, true)
				continue
			case pr != nil && pr.k == kStr:
				// extern "C" {: the brace stays on the line, and the
				// contents are not indented
				p.place(ln, t)
				t.role = rBlockOpen
				p.i++
				p.trailing(ln)
				p.items(level, false, false)
				if isPunct(p.peek(), "}") {
					cl := p.newLine(level)
					p.take(cl)
					p.trailing(cl)
				}
				return
			default:
				p.expr(ln, level, t.match)
				continue
			}
		case t.k == kPunct && (t.s == "(" || t.s == "["):
			p.expr(ln, level, t.match)
			continue
		}
		p.place(ln, t)
		p.i++
	}
}
