package cfmt

import "strings"

const indentWidth = 4

// render writes the lines out.
func render(lines []*line) string {
	var b strings.Builder
	prevOpens := false
	first := true
	for _, ln := range lines {
		if len(ln.toks) == 0 {
			continue
		}
		t0 := ln.toks[0]
		blank := 0
		if t0.nl > 1 {
			blank = t0.nl - 1
		}
		if blank > 2 {
			blank = 2
		}
		if first {
			blank = 0
		}
		if prevOpens {
			blank = 0
		}
		if isPunct(t0, "}") && (len(ln.toks) == 1 || (len(ln.toks) == 2 && isPunct(ln.toks[1], ";"))) {
			blank = 0
		}
		for k := 0; k < blank; k++ {
			b.WriteByte('\n')
		}
		renderLine(&b, ln)
		b.WriteByte('\n')
		last := ln.toks[len(ln.toks)-1]
		for k := len(ln.toks) - 1; k >= 0 && isComment(ln.toks[k]); k-- {
			last = ln.toks[k]
		}
		if isComment(last) && len(ln.toks) > 0 {
			// the last code token
			last = nil
			for k := len(ln.toks) - 1; k >= 0; k-- {
				if !isComment(ln.toks[k]) {
					last = ln.toks[k]
					break
				}
			}
		}
		prevOpens = last != nil && isPunct(last, "{")
		first = false
	}
	return b.String()
}

func renderLine(b *strings.Builder, ln *line) {
	base := ln.level * indentWidth
	t0 := ln.toks[0]
	if t0.k == kPP {
		b.WriteString(formatPP(t0.s))
		renderRest(b, ln, 1, base, 0)
		return
	}
	b.WriteString(strings.Repeat(" ", base))
	renderRest(b, ln, 0, base, base)
}

// renderRest writes ln.toks[from:]; phys is the indent of the physical line
// being written.
func renderRest(b *strings.Builder, ln *line, from int, base int, phys int) {
	type open struct{ phys int }
	var stack []open
	var prev *tok
	if from > 0 {
		prev = ln.toks[from-1]
	}
	for k := from; k < len(ln.toks); k++ {
		t := ln.toks[k]
		if t.brk && prev != nil {
			b.WriteByte('\n')
			if t.k == kPP {
				b.WriteString(formatPP(t.s))
				prev = t
				phys = 0
				continue
			}
			ind := base + indentWidth
			if len(stack) > 0 {
				ind = stack[len(stack)-1].phys + indentWidth
			}
			if t.k == kPunct && isClose(t.s) && len(stack) > 0 {
				ind = stack[len(stack)-1].phys
			}
			b.WriteString(strings.Repeat(" ", ind))
			phys = ind
		} else if prev != nil {
			if isComment(t) {
				if t.ws == "" {
					b.WriteByte(' ')
				} else {
					b.WriteString(t.ws)
				}
			} else if space(prev, t) {
				b.WriteByte(' ')
			}
		}
		b.WriteString(t.s)
		if t.k == kPunct {
			if isOpen(t.s) {
				stack = append(stack, open{phys})
			} else if isClose(t.s) && len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
		prev = t
	}
}

// formatPP lays out one preprocessor directive.
func formatPP(s string) string {
	s = strings.TrimRight(s, " \t")
	rest := strings.TrimLeft(s, " \t")
	rest = strings.TrimPrefix(rest, "#")
	rest = strings.TrimLeft(rest, " \t")
	n := 0
	for n < len(rest) && isIdentChar(rest[n]) {
		n++
	}
	dir, body := rest[:n], rest[n:]
	switch dir {
	case "define", "if", "elif", "ifdef", "ifndef", "else", "endif", "undef":
	default:
		return "#" + dir + body
	}
	if strings.Contains(body, "\\\n") {
		return "#" + dir + body
	}
	toks, ok := lex(body)
	if !ok || len(toks) == 0 {
		return "#" + dir + strings.TrimLeft(body, " \t")
	}
	for _, t := range toks {
		t.nl = 0
	}
	if !matchBrackets(toks) {
		return "#" + dir + body
	}
	var b strings.Builder
	b.WriteString("#" + dir + " ")
	if dir == "define" {
		b.WriteString(toks[0].s)
		toks = toks[1:]
		if len(toks) > 0 && isPunct(toks[0], "(") && toks[0].ws == "" {
			// parameters: to the first `)`
			j := 0
			for j < len(toks) && !isPunct(toks[j], ")") {
				j++
			}
			if j >= len(toks) {
				return "#" + dir + body
			}
			params := toks[:j+1]
			toks = toks[j+1:]
			if j == 1 {
				b.WriteString("()")
			} else {
				b.WriteString("( ")
				for k := 1; k < j; k++ {
					if k > 1 && !isPunct(params[k], ",") {
						b.WriteByte(' ')
					}
					b.WriteString(params[k].s)
				}
				b.WriteString(" )")
			}
		}
		if len(toks) == 0 {
			return b.String()
		}
		if !isComment(toks[0]) {
			b.WriteByte(' ')
		}
	}
	reindex(toks)
	if !matchBrackets(toks) {
		return "#" + dir + body
	}
	classify(toks)
	if dir == "define" && toks[0].k == kWord && toks[0].s == "do" {
		// a statement macro: the body goes below the name, laid out as the
		// statement it is
		p := &parser{toks: toks, macro: true}
		p.statement(1, nil)
		if !p.bad && p.i == len(toks) {
			head := strings.TrimRight(b.String(), " ")
			var m strings.Builder
			m.WriteString(head)
			for _, ln := range p.lines {
				if len(ln.toks) == 0 {
					continue
				}
				m.WriteString(" \\\n")
				renderLine(&m, ln)
			}
			return m.String()
		}
	}
	writeSpaced(&b, toks)
	return strings.TrimRight(b.String(), " ")
}

func reindex(toks []*tok) {
	for i, t := range toks {
		t.idx = i
		t.match = -1
		t.role = rNone
	}
}

func writeSpaced(b *strings.Builder, toks []*tok) {
	for k, t := range toks {
		if k > 0 {
			if isComment(t) {
				if t.ws == "" {
					b.WriteByte(' ')
				} else {
					b.WriteString(t.ws)
				}
			} else if space(toks[k-1], t) {
				b.WriteByte(' ')
			}
		} else if isComment(t) {
			// a comment straight after the name keeps its spacing
			b.WriteString(strings.TrimPrefix(t.ws, " "))
		}
		b.WriteString(t.s)
	}
}
