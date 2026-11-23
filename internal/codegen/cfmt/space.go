package cfmt

var ctrlKeywords = map[string]bool{"if": true, "for": true, "while": true, "switch": true}

// Keywords after which a `(` is not a call and a `*`/`&`/`-` is unary.
var operandKeywords = map[string]bool{"return": true, "case": true, "else": true, "do": true, "goto": true}

// Words that name a type on their own, so `(word) x` is a cast.
var typeKeywords = map[string]bool{
	"void": true, "char": true, "short": true, "int": true, "long": true, "float": true, "double": true,
	"signed": true, "unsigned": true, "const": true, "volatile": true, "struct": true, "union": true,
	"enum": true, "_Bool": true, "bool": true,
}

var binaryOps = map[string]bool{
	"=": true, "+=": true, "-=": true, "*=": true, "/=": true, "%=": true, "&=": true, "|=": true, "^=": true,
	"<<=": true, ">>=": true, "==": true, "!=": true, "<": true, ">": true, "<=": true, ">=": true,
	"&&": true, "||": true, "|": true, "^": true, "&": true, "*": true, "/": true, "%": true, "+": true,
	"-": true, "<<": true, ">>": true, "?": true, ":": true,
}

func isComment(t *tok) bool { return t.k == kBlockComment || t.k == kLineComment }

func isOpen(s string) bool  { return s == "(" || s == "[" || s == "{" }
func isClose(s string) bool { return s == ")" || s == "]" || s == "}" }

// matchBrackets pairs (), [] and {} over toks. ok is false on a mismatch.
func matchBrackets(toks []*tok) bool {
	var stack []int
	for i, t := range toks {
		if t.k != kPunct {
			continue
		}
		if isOpen(t.s) {
			stack = append(stack, i)
		} else if isClose(t.s) {
			if len(stack) == 0 {
				return false
			}
			o := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			want := map[string]string{")": "(", "]": "[", "}": "{"}[t.s]
			if toks[o].s != want {
				return false
			}
			toks[o].match, t.match = i, o
		}
	}
	return len(stack) == 0
}

func prevCode(toks []*tok, i int) *tok {
	for j := i - 1; j >= 0; j-- {
		if !isComment(toks[j]) {
			if toks[j].k == kPP {
				return nil
			}
			return toks[j]
		}
	}
	return nil
}

func nextCode(toks []*tok, i int) *tok {
	for j := i + 1; j < len(toks); j++ {
		if !isComment(toks[j]) {
			return toks[j]
		}
	}
	return nil
}

func isPunct(t *tok, s string) bool { return t != nil && t.k == kPunct && t.s == s }

// isOperandEnd: a token after which a `*`, `&`, `-`, `+` is binary.
func isOperandEnd(p *tok) bool {
	if p == nil {
		return false
	}
	switch p.k {
	case kWord:
		return !operandKeywords[p.s] && p.s != "sizeof"
	case kNum, kStr, kChar:
		return true
	case kPunct:
		switch p.s {
		case ")":
			return p.role != rCastClose && p.role != rCtrlClose
		case "]":
			return true
		case "}":
			return p.role == rInitClose
		case "++", "--":
			return p.role == rPostfix
		}
	}
	return false
}

// castContent reports whether toks[o+1:c] spells a type.
func castContent(toks []*tok, o, c int) bool {
	if c <= o+1 {
		return false
	}
	words, stars := 0, 0
	sawStarThenWord := false
	for j := o + 1; j < c; j++ {
		t := toks[j]
		switch {
		case t.k == kWord:
			if stars > 0 && !typeKeywords[t.s] {
				sawStarThenWord = true
			}
			words++
		case t.k == kPunct && t.s == "*":
			stars++
		default:
			return false
		}
	}
	if words == 0 || toks[o+1].k != kWord {
		return false
	}
	if sawStarThenWord {
		return false // `a * b`
	}
	return true
}

// operandStart: a token that can begin the operand of a cast.
func operandStart(t *tok) bool {
	if t == nil {
		return false
	}
	switch t.k {
	case kWord:
		return !binaryOps[t.s]
	case kNum, kStr, kChar:
		return true
	case kPunct:
		switch t.s {
		case "(", "!", "~", "-", "+", "*", "&", "++", "--":
			return true
		}
	}
	return false
}

// classify sets the roles the spacing rules read: what each paren is, and
// whether each `*`, `&`, `-`, `+`, `++`, `--` is unary or binary. Brace roles
// and label colons are the parser's.
func classify(toks []*tok) {
	for i, t := range toks {
		if t.k != kPunct {
			continue
		}
		switch t.s {
		case "(":
			p := prevCode(toks, i)
			r := rGroupOpen
			switch {
			case p != nil && p.k == kWord && ctrlKeywords[p.s]:
				r = rCtrlOpen
			case p != nil && p.k == kWord && !operandKeywords[p.s] && !typeKeywords[p.s]:
				r = rCallOpen
			case p != nil && p.k == kPunct && (p.s == "]" || (p.s == ")" && p.role != rCastClose && p.role != rCtrlClose)):
				r = rCallOpen
			}
			// `int64_t ( *name )( args )`: a function pointer's declarator
			if r == rCallOpen && p.k == kWord && t.match == i+3 && isPunct(toks[i+1], "*") && toks[i+2].k == kWord && isPunct(nextCode(toks, t.match), "(") && p.s != "sizeof" {
				r = rGroupOpen
			}
			if r == rGroupOpen && t.match > 0 && castContent(toks, i, t.match) {
				n := nextCode(toks, t.match)
				if operandStart(n) {
					r = rCastOpen
				}
				// `(T){ ... }` is a compound literal, laid out as a group
				if isPunct(n, "{") {
					r = rGroupOpen
				}
			}
			t.role = r
			if t.match >= 0 {
				toks[t.match].role = r + (rCallClose - rCallOpen)
			}
		case "*", "&", "-", "+":
			if isOperandEnd(prevCode(toks, i)) {
				t.role = rBinary
			} else {
				t.role = rUnary
			}
			if t.s == "*" {
				p := prevCode(toks, i)
				if p != nil && isPunct(p, "*") && (p.role == rBinary || p.role == rPtrCont) && t.ws == "" && t.nl == 0 {
					t.role = rPtrCont
				}
			}
		case "++", "--":
			if isOperandEnd(prevCode(toks, i)) {
				t.role = rPostfix
			} else {
				t.role = rPrefix
			}
		}
	}
}

func isUnaryTok(t *tok) bool {
	if t.k != kPunct {
		return false
	}
	switch t.s {
	case "!", "~":
		return true
	case "*", "&", "-", "+":
		return t.role == rUnary
	case "++", "--":
		return t.role == rPrefix
	}
	return false
}

// space reports whether one space separates a and b on a line.
func space(a, b *tok) bool {
	if isComment(b) {
		return true
	}
	if b.k == kPunct && b.s == ":" && b.role == rLabelColon {
		return false
	}
	if isPunct(b, "]") {
		return false
	}
	if a.k == kStr && isPunct(b, "(") {
		return false // __asm__( "" : "+w"( x ) )
	}
	if a.k == kPunct && a.s == "(" {
		if isPunct(b, ")") {
			return false
		}
		return a.role != rCastOpen
	}
	if b.k == kPunct && b.s == ")" {
		return b.role != rCastClose
	}
	if b.k == kPunct && b.s == "(" {
		switch b.role {
		case rCtrlOpen:
			return true
		case rCallOpen:
			return false
		}
		if a.k == kPunct {
			switch a.s {
			case "[", "!", "~", ".", "->":
				return false
			case "*", "&", "-", "+":
				return a.role != rUnary
			case "++", "--":
				return a.role != rPrefix
			}
		}
		return true
	}
	if a.k == kPunct && a.s == ")" {
		if a.role == rCastClose {
			return true
		}
		if b.k == kPunct {
			switch b.s {
			case "[", ".", "->", ",", ";":
				return false
			case "++", "--":
				return b.role != rPostfix
			case "{":
				return b.role != rInitOpen
			}
		}
		return true
	}
	if b.k == kPunct && (b.s == "," || b.s == ";") {
		return false
	}
	if a.k == kPunct && (a.s == "," || a.s == ";") {
		return true
	}
	if a.k == kPunct && a.s == "[" || b.k == kPunct && b.s == "]" {
		return false
	}
	if b.k == kPunct && b.s == "[" {
		return false
	}
	if a.k == kPunct && (a.s == "." || a.s == "->") || b.k == kPunct && (b.s == "." || b.s == "->") {
		return false
	}
	if b.k == kPunct && (b.s == "++" || b.s == "--") && b.role == rPostfix {
		return false
	}
	if isUnaryTok(a) {
		// `- -x` stays two tokens
		return b.k == kPunct && (a.s == "-" || a.s == "+" || a.s == "&") && b.s[0] == a.s[0]
	}
	if b.role == rPtrCont {
		return false
	}
	if b.k == kPunct && b.s == ":" && b.role == rLabelColon {
		return false
	}
	if a.k == kPunct && a.s == "{" && isPunct(b, "}") {
		return false
	}
	if a.k == kPunct && a.s == "##" || b.k == kPunct && b.s == "##" {
		return false
	}
	return true
}
