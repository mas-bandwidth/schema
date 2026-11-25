package ccommon

import (
	"fmt"
	"regexp"
	"strings"
)

// SinglePassConditionals rewrites every #if, #ifdef or #ifndef chain in a
// generated C or C++ file that carries an #elif or an #else into a run of
// independent conditionals, one per branch, each of which states the whole
// condition its branch is taken under:
//
//	#if A            #if A
//	    x                x
//	#elif B    ->    #endif
//	    y            #if !( A ) && ( B )
//	#else                y
//	    z            #endif
//	#endif           #if !( A ) && !( B )
//	                     z
//	                 #endif
//
// WHY: clang-format lays a file out once for every branch an #if chain can
// take, nesting level by nesting level, so a header whose runtime carries
// three-way chains (the compiler, byte-order and inline-spelling tests) was
// formatted five or six times over. A 60,000-line table header took 5-9 s of
// CPU for the format gates (tables-c-clean, tables-cpp-format) and with the
// fixed form's arrays in it 41-50 s. A run of single-branch conditionals has
// one layout, so the file is read once. What the preprocessor keeps is
// unchanged: exactly one branch's text, chosen by the same conditions.
//
// THAT EQUIVALENCE HOLDS ONLY WHILE NO BRANCH CHANGES A LATER CONDITION AND
// EVERY CONDITION CAN BE EVALUATED ANYWHERE: the rewrite evaluates every
// condition after the branches before it, and evaluates an #elif the chain
// never reached. So a chain that carries an #elif or #else may test only
// defined( NAME ), integer literals and implementation-reserved names
// (ppDefinedOnly: a program's macro could expand to anything, and
// __has_builtin( x ) is a syntax error where it is not defined), and a branch
// that #defines or #undefs a name its chain reads, or #includes a project
// header that might (a system header, <x.h>, is taken not to redefine the
// compiler's own names), is refused by name. So is what the parser does not
// know: #elifdef and #elifndef, a carriage return, a chain that is never
// closed, and an #elif, #else or #endif with no #if.
func SinglePassConditionals(src string) (string, error) {
	if i := strings.IndexByte(src, '\r'); i >= 0 {
		return "", fmt.Errorf("line %d: a carriage return; the rewrite reads \\n line endings only", strings.Count(src[:i], "\n")+1)
	}
	lines := strings.SplitAfter(src, "\n")
	p := &ppParser{lines: lines}
	nodes, err := p.block(0)
	if err != nil {
		return "", err
	}
	if p.at < len(lines) {
		return "", fmt.Errorf("line %d: #%s with no #if", p.at+1, p.word(p.at))
	}
	var b strings.Builder
	for _, n := range nodes {
		if err := n.write(&b); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}

// ppNode is a run of plain lines or one conditional chain.
type ppNode struct {
	text     []string   // plain lines, when chain is nil
	branches []ppBranch // the chain: its #if and every #elif / #else
	endif    string     // the chain's #endif line, as written
}

type ppBranch struct {
	line   string // the directive as written (the #if is kept verbatim)
	indent string // the directive's leading whitespace
	word   string // if, ifdef, ifndef, elif, else
	cond   string // the condition in #if spelling; "" for #else
	body   []*ppNode
	start  int // the directive's line number, for errors
}

type ppParser struct {
	lines     []string
	at        int
	inComment bool // inside a /* */ comment at the start of line at
}

var ppDirective = regexp.MustCompile(`^([ \t]*)#[ \t]*([a-z]+)\b(.*)$`)

// directive reports whether line i is a preprocessor directive outside a
// block comment, with its leading whitespace, word and the rest of the line
// (continuation lines joined).
func (p *ppParser) directive(i int) (indent, word, rest string, ok bool) {
	if p.inComment {
		return "", "", "", false
	}
	m := ppDirective.FindStringSubmatch(strings.TrimRight(p.lines[i], "\r\n"))
	if m == nil {
		return "", "", "", false
	}
	return m[1], m[2], m[3], true
}

func (p *ppParser) word(i int) string {
	_, w, _, _ := p.directive(i)
	return w
}

// advance moves past line i, tracking whether a /* */ comment is open
// after it. String and character literals and // comments are skipped, so a
// "/*" inside one opens nothing.
func (p *ppParser) advance() {
	line := p.lines[p.at]
	p.at++
	for k := 0; k < len(line); k++ {
		if p.inComment {
			if strings.HasPrefix(line[k:], "*/") {
				p.inComment = false
				k++
			}
			continue
		}
		switch c := line[k]; {
		case c == '"' || c == '\'':
			for k++; k < len(line) && line[k] != c; k++ {
				if line[k] == '\\' {
					k++
				}
			}
		case strings.HasPrefix(line[k:], "//"):
			return
		case strings.HasPrefix(line[k:], "/*"):
			p.inComment = true
			k++
		}
	}
}

// continued joins a directive's backslash-continued lines, advancing past
// them, and returns the directive's rest as one line.
func (p *ppParser) continued(rest string) string {
	for strings.HasSuffix(strings.TrimRight(rest, " \t"), "\\") && p.at < len(p.lines) {
		rest = strings.TrimRight(strings.TrimSuffix(strings.TrimRight(rest, " \t"), "\\"), " \t") + " " + strings.TrimSpace(strings.TrimRight(p.lines[p.at], "\r\n"))
		p.advance()
	}
	return rest
}

// block parses lines until an #elif, #else or #endif at this level (left
// unconsumed) or the end of the file.
func (p *ppParser) block(depth int) ([]*ppNode, error) {
	var nodes []*ppNode
	var text []string
	flush := func() {
		if len(text) > 0 {
			nodes = append(nodes, &ppNode{text: text})
			text = nil
		}
	}
	for p.at < len(p.lines) {
		indent, word, rest, ok := p.directive(p.at)
		if !ok {
			text = append(text, p.lines[p.at])
			p.advance()
			continue
		}
		switch word {
		case "elif", "else", "endif":
			flush()
			return nodes, nil
		case "elifdef", "elifndef":
			return nil, fmt.Errorf("line %d: #%s is not one the rewrite knows", p.at+1, word)
		case "if", "ifdef", "ifndef":
			flush()
			n, err := p.chain(indent, word, rest, depth)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, n)
		default:
			start := p.at
			p.advance()
			text = append(text, p.lines[start:p.at]...)
			if strings.HasSuffix(strings.TrimRight(rest, " \t\r\n"), "\\") {
				// a continued #define: its lines are its own
				for p.at < len(p.lines) && strings.HasSuffix(strings.TrimRight(p.lines[p.at-1], " \t\r\n"), "\\") {
					text = append(text, p.lines[p.at])
					p.advance()
				}
			}
		}
	}
	flush()
	return nodes, nil
}

func (p *ppParser) chain(indent, word, rest string, depth int) (*ppNode, error) {
	n := &ppNode{}
	for {
		start := p.at
		line := p.lines[start]
		p.advance()
		rest = p.continued(rest)
		br := ppBranch{line: strings.Join(p.lines[start:p.at], ""), indent: indent, word: word, start: start + 1}
		cond := ppCondition(rest)
		switch word {
		case "if", "elif":
			br.cond = cond
		case "ifdef":
			br.cond = "defined( " + cond + " )"
		case "ifndef":
			br.cond = "!defined( " + cond + " )"
		}
		if word != "else" && br.cond == "" {
			return nil, fmt.Errorf("line %d: #%s with no condition: %q", start+1, word, strings.TrimSpace(line))
		}
		body, err := p.block(depth + 1)
		if err != nil {
			return nil, err
		}
		br.body = body
		n.branches = append(n.branches, br)
		if p.at >= len(p.lines) {
			return nil, fmt.Errorf("line %d: #%s is never closed", n.branches[0].start, n.branches[0].word)
		}
		indent, word, rest, _ = p.directive(p.at)
		switch word {
		case "endif":
			n.endif = p.lines[p.at]
			p.advance()
			return n, n.check()
		case "else", "elif":
			if last := n.branches[len(n.branches)-1]; last.word == "else" {
				return nil, fmt.Errorf("line %d: #%s after #else", p.at+1, word)
			}
		}
	}
}

// ppCondition is a directive's condition with a trailing comment removed.
func ppCondition(rest string) string {
	rest = strings.TrimSpace(rest)
	if i := strings.Index(rest, "//"); i >= 0 {
		rest = strings.TrimSpace(rest[:i])
	}
	if i := strings.Index(rest, "/*"); i >= 0 && strings.HasSuffix(rest, "*/") {
		rest = strings.TrimSpace(rest[:i])
	}
	return rest
}

var ppIdent = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
var ppDefines = regexp.MustCompile(`^[ \t]*#[ \t]*(?:define|undef)[ \t]+([A-Za-z_][A-Za-z0-9_]*)`)
var ppInclude = regexp.MustCompile(`^[ \t]*#[ \t]*include[ \t]*[^ \t<]`)
var ppToken = regexp.MustCompile(`^(?:[A-Za-z_][A-Za-z0-9_]*|[0-9][0-9A-Za-z]*|&&|\|\||==|!=|<=|>=|[!()<>])`)
var ppReserved = regexp.MustCompile(`^(?:__|_[A-Z])`)

// ppDefinedOnly says why cond may not be evaluated out of its chain's
// order, or "" when it may: it is built from defined( NAME ), defined NAME,
// integer literals and the implementation's own names (__x, _X, which no
// program may #define, so none expands to a defined() or changes under a
// branch), compared and joined by ! == != < > <= >= && ||. A call of any
// kind (__has_builtin( x )) is refused: it is a syntax error wherever the
// name is not a function-like macro.
func ppDefinedOnly(cond string) string {
	var toks []string
	for rest := strings.TrimSpace(cond); rest != ""; rest = strings.TrimSpace(rest) {
		t := ppToken.FindString(rest)
		if t == "" {
			return fmt.Sprintf("%q is not part of a defined() test", rest)
		}
		toks = append(toks, t)
		rest = rest[len(t):]
	}
	name := func(i int) bool { return i < len(toks) && ppIdent.MatchString(toks[i]) && toks[i] != "defined" }
	for i := 0; i < len(toks); i++ {
		switch t := toks[i]; {
		case strings.ContainsAny(t[:1], "!&|()<>=") || t[0] >= '0' && t[0] <= '9':
		case t == "defined":
			switch {
			case i+3 < len(toks) && toks[i+1] == "(" && name(i+2) && toks[i+3] == ")":
				i += 3
			case name(i + 1):
				i++
			default:
				return "a defined with no name"
			}
		case i+1 < len(toks) && toks[i+1] == "(":
			return t + "( ... ) is called"
		case !ppReserved.MatchString(t):
			return t + " is tested by value; only defined() or an implementation-reserved name (__x, _X) may be"
		}
	}
	return ""
}

// check refuses a chain one of whose branches defines or undefines a name
// a condition of the chain reads: the rewrite would read that name after
// the branch, where the chain read it before.
func (n *ppNode) check() error {
	if len(n.branches) < 2 {
		return nil
	}
	read := map[string]bool{}
	for _, br := range n.branches {
		if br.word != "else" {
			if why := ppDefinedOnly(br.cond); why != "" {
				return fmt.Errorf("line %d: #%s %s: %s; a chain with an #elif or #else is split only when every condition can be evaluated out of order", br.start, br.word, br.cond, why)
			}
		}
		for _, id := range ppIdent.FindAllString(br.cond, -1) {
			if id != "defined" {
				read[id] = true
			}
		}
	}
	var walk func(nodes []*ppNode) error
	walk = func(nodes []*ppNode) error {
		for _, c := range nodes {
			for _, line := range c.text {
				if ppInclude.MatchString(line) {
					return fmt.Errorf("line %d: a branch of this chain #includes a project file, which may change a name the chain reads; it cannot be split into independent conditionals", n.branches[0].start)
				}
				if m := ppDefines.FindStringSubmatch(line); m != nil && read[m[1]] {
					return fmt.Errorf("line %d: a branch of this chain changes %s, which a condition of the chain reads; it cannot be split into independent conditionals", n.branches[0].start, m[1])
				}
			}
			for _, br := range c.branches {
				if err := walk(br.body); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, br := range n.branches {
		if err := walk(br.body); err != nil {
			return err
		}
	}
	return nil
}

func (n *ppNode) write(b *strings.Builder) error {
	if n.branches == nil {
		for _, l := range n.text {
			b.WriteString(l)
		}
		return nil
	}
	var before []string // the negations of every earlier branch's condition
	for i, br := range n.branches {
		if i == 0 {
			b.WriteString(br.line)
		} else {
			cond := strings.Join(before, " && ")
			if br.word == "elif" {
				cond += " && ( " + br.cond + " )"
			}
			fmt.Fprintf(b, "%s#endif\n%s#if %s\n", br.indent, br.indent, cond)
		}
		before = append(before, "!( "+br.cond+" )")
		for _, c := range br.body {
			if err := c.write(b); err != nil {
				return err
			}
		}
	}
	b.WriteString(n.endif)
	return nil
}
