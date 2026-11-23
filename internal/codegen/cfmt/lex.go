// Package cfmt lays generated C out in the house style, the repo's
// .clang-format, so that generated C is format-canonical without the user
// needing clang-format installed (docs/PORTING.md J4, make/c.mk
// tables-c-clean). It is not a general C formatter: it knows the constructs
// the table emitter writes, and the tables-c-clean gate holds its output to
// what the pinned clang-format writes over the whole corpus. It moves
// whitespace only; Format refuses (returns the input unchanged) when the
// non-whitespace text would differ.
package cfmt

import "strings"

type kind int

const (
	kWord kind = iota
	kNum
	kStr
	kChar
	kPunct
	kBlockComment
	kLineComment
	kPP
)

// role is what a token is doing, decided by classify and the parser.
type role int

const (
	rNone role = iota
	rUnary
	rBinary
	rPtrCont // a second `*` of a pointer declarator, `char ** p`
	rPrefix  // ++x
	rPostfix // x++
	rCallOpen
	rCtrlOpen
	rCastOpen
	rGroupOpen
	rCallClose
	rCtrlClose
	rCastClose
	rGroupClose
	rInitOpen
	rInitClose
	rBlockOpen
	rBlockClose
	rLabelColon
)

type tok struct {
	k     kind
	s     string
	nl    int    // newlines before this token in the input
	ws    string // the whitespace before it on its own input line
	role  role
	match int // index of the matching bracket, -1 otherwise
	idx   int
	// layout, set by the parser
	brk   bool // starts a continuation line
	force bool // must start one: an element of a list that ends in a comma
}

var puncts = []string{
	">>=", "<<=", "...",
	"->", "++", "--", "<<", ">>", "<=", ">=", "==", "!=", "&&", "||",
	"+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "##",
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '$'
}

func isIdentChar(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

// lex splits src into tokens. ok is false on anything it does not
// understand (an unterminated literal or comment), and Format then leaves
// the text alone.
func lex(src string) (toks []*tok, ok bool) {
	i, nl := 0, 0
	lineStart := 0
	atLineStart := true
	for i < len(src) {
		c := src[i]
		if c == '\n' {
			nl++
			i++
			lineStart = i
			atLineStart = true
			continue
		}
		if c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v' {
			i++
			continue
		}
		wsStart := i
		for wsStart > lineStart && (src[wsStart-1] == ' ' || src[wsStart-1] == '\t') {
			wsStart--
		}
		t := &tok{nl: nl, ws: src[wsStart:i], match: -1}
		nl = 0
		start := i
		switch {
		case c == '#' && atLineStart:
			// a directive runs to the end of the line, with continuations
			for i < len(src) {
				if src[i] == '\n' {
					if i > 0 && src[i-1] == '\\' {
						i++
						continue
					}
					break
				}
				if strings.HasPrefix(src[i:], "/*") {
					end := strings.Index(src[i+2:], "*/")
					if end < 0 {
						return nil, false
					}
					i += 2 + end + 2
					continue
				}
				i++
			}
			t.k = kPP
		case strings.HasPrefix(src[i:], "/*"):
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return nil, false
			}
			i += 2 + end + 2
			t.k = kBlockComment
		case strings.HasPrefix(src[i:], "//"):
			for i < len(src) && src[i] != '\n' {
				i++
			}
			t.k = kLineComment
		case c == '"' || c == '\'':
			i++
			for i < len(src) && src[i] != c {
				if src[i] == '\\' {
					i++
				}
				if i < len(src) && src[i] == '\n' {
					return nil, false
				}
				i++
			}
			if i >= len(src) {
				return nil, false
			}
			i++
			t.k = kStr
			if c == '\'' {
				t.k = kChar
			}
		case isIdentStart(c):
			for i < len(src) && isIdentChar(src[i]) {
				i++
			}
			t.k = kWord
			// an encoding prefix glued to a literal: L"x", u8"x"
			if i < len(src) && (src[i] == '"' || src[i] == '\'') {
				switch src[start:i] {
				case "L", "u", "U", "u8":
					q := src[i]
					i++
					for i < len(src) && src[i] != q {
						if src[i] == '\\' {
							i++
						}
						i++
					}
					if i >= len(src) {
						return nil, false
					}
					i++
					t.k = kStr
				}
			}
		case c >= '0' && c <= '9' || (c == '.' && i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9'):
			for i < len(src) {
				d := src[i]
				if isIdentChar(d) || d == '.' {
					i++
					continue
				}
				if (d == '+' || d == '-') && (src[i-1] == 'e' || src[i-1] == 'E' || src[i-1] == 'p' || src[i-1] == 'P') && !strings.HasPrefix(strings.ToLower(src[start:]), "0x") {
					i++
					continue
				}
				if (d == '+' || d == '-') && (src[i-1] == 'p' || src[i-1] == 'P') {
					i++
					continue
				}
				break
			}
			t.k = kNum
		default:
			t.k = kPunct
			n := 1
			for _, p := range puncts {
				if strings.HasPrefix(src[i:], p) {
					n = len(p)
					break
				}
			}
			i += n
		}
		t.s = src[start:i]
		atLineStart = false
		t.idx = len(toks)
		toks = append(toks, t)
	}
	return toks, true
}
