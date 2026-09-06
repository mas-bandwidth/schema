// Package scanner tokenizes *.schema source (SPEC §4.1).
//
// Newlines are terminator tokens, suppressed immediately after opening
// punctuation, separators, infix operators and `else`, and immediately before
// closing punctuation. Comments are skipped, with one exception: a `///` DOC
// comment (SPEC §4.1) is the one comment the compiler reads, and a run of
// `///` lines scans as a single Doc token carrying the block's text. A block
// comment containing a newline acts as a newline, Go's rule.
package scanner

import (
	"bytes"
	"fmt"
	"strings"
)

type Kind int

const (
	EOF Kind = iota
	Newline
	Ident
	Int
	Float
	String  // "..." — attribute values only (SPEC §4.2); Text keeps the quotes
	Comment // raw-scan mode only (schemafmt) — Text carries the comment verbatim
	// Doc is one `///` DOC COMMENT (SPEC §4.1): a contiguous run of `///`
	// lines, each on a line of its own, scanned as ONE token whose Text is the
	// block's text. That is what follows each marker, with at most one leading
	// space and all trailing whitespace dropped, joined by single newlines. The
	// run's own newlines are consumed with it, so the token sits directly
	// before the item it documents. Never produced in raw-scan mode, where
	// every comment is a Comment token verbatim.
	Doc

	LBrace // {
	RBrace // }
	LParen // (
	RParen // )
	LBrack // [
	RBrack // ]
	Comma
	Colon
	Assign   // =
	Not      // !
	Question // ? — the OPTIONAL type prefix in a table body (docs/SPEC-TABLES.md §2.3)
	Dot      // .
	DotDot   // ..
	LessEq   // <= — retired spelling; scanned so the parser can refuse it by name (SPEC §4.3)
	Pipe     // | — opens a line's qualification section (SPEC §4.2)
	Plus
	Minus
	Star
	Slash
	Percent

	keywordStart
	KwPackage
	KwConst
	KwEnum
	KwType
	KwTable
	KwMap // `map[K]V` — a table field's lookup (docs/SPEC-TABLES.md §2.8)
	KwMessage
	KwObject
	KwIf
	KwElse
	KwSwitch
	KwCase
	KwAlign
	KwReserved
	KwBits
	KwBool
	KwFloat32
	KwFloat64
	KwString
	KwWString // wstring(N) — wide text, N in UTF-16 code units (SPEC §4.12)
	KwBytes
	KwFixed  // fixed(I, F) — signed fixed point (SPEC §4.3)
	KwUfixed // ufixed(I, F) — unsigned fixed point (SPEC §4.3)
	KwInt8
	KwInt16
	KwInt32
	KwInt64
	KwUint8
	KwUint16
	KwUint32
	KwUint64
	KwInt128  // live: ranged 128-bit integer (SPEC §4.3)
	KwUint128 // live: raw 128-bit unsigned integer (SPEC §4.3)
	KwInt     // reserved: "did you mean int32?"
	KwUint    // reserved: "did you mean uint32?"
	keywordEnd
)

var keywords = map[string]Kind{
	"package": KwPackage, "const": KwConst, "enum": KwEnum, "type": KwType,
	"table": KwTable, "map": KwMap,
	"message": KwMessage, "object": KwObject, "if": KwIf, "else": KwElse,
	"switch": KwSwitch, "case": KwCase, "align": KwAlign, "reserved": KwReserved,
	"bits": KwBits, "bool": KwBool, "float32": KwFloat32, "float64": KwFloat64,
	"string": KwString, "wstring": KwWString, "bytes": KwBytes, "fixed": KwFixed, "ufixed": KwUfixed,
	"int8": KwInt8, "int16": KwInt16, "int32": KwInt32, "int64": KwInt64,
	"uint8": KwUint8, "uint16": KwUint16, "uint32": KwUint32, "uint64": KwUint64,
	"int128": KwInt128, "uint128": KwUint128, "int": KwInt, "uint": KwUint,
}

func (k Kind) IsKeyword() bool { return k > keywordStart && k < keywordEnd }

type Pos struct {
	File string
	Line int
	Col  int
}

func (p Pos) String() string { return fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Col) }

type Token struct {
	Kind Kind
	Text string
	Pos  Pos
}

type Error struct {
	Pos Pos
	Msg string
}

func (e Error) Error() string { return fmt.Sprintf("%s: %s", e.Pos, e.Msg) }

// Scan tokenizes src and applies the newline suppression rules. The returned
// slice always ends with an EOF token.
func Scan(file string, src []byte) ([]Token, []error) {
	if bytes.HasPrefix(src, []byte{0xEF, 0xBB, 0xBF}) {
		return nil, []error{Error{Pos{file, 1, 1},
			"UTF-8 BOM rejected: schema source is plain UTF-8 with no BOM (SPEC §4.1)"}}
	}
	s := &state{file: file, src: src, line: 1, col: 1}
	var raw []Token
	for {
		t := s.next()
		raw = append(raw, t)
		if t.Kind == EOF {
			break
		}
	}
	return filter(raw), s.errs
}

// ScanRaw tokenizes src preserving comments and every newline — the
// comment-carrying scan schemafmt requires (SPEC §4.1, §7.4). No suppression
// filter runs: the formatter assembles its own lines.
func ScanRaw(file string, src []byte) ([]Token, []error) {
	if bytes.HasPrefix(src, []byte{0xEF, 0xBB, 0xBF}) {
		return nil, []error{Error{Pos{file, 1, 1},
			"UTF-8 BOM rejected: schema source is plain UTF-8 with no BOM (SPEC §4.1)"}}
	}
	s := &state{file: file, src: src, line: 1, col: 1, keepComments: true}
	var raw []Token
	for {
		t := s.next()
		raw = append(raw, t)
		if t.Kind == EOF {
			break
		}
	}
	return raw, s.errs
}

type state struct {
	file         string
	src          []byte
	off          int
	line         int
	col          int
	errs         []error
	keepComments bool // raw-scan mode: comments become tokens
	pipeLine     bool // right of | on the current line: no wrapping there (SPEC §4.1)
	// lineHasCode reports that a code token has been scanned on the current
	// physical line, which is what makes a `///` there a TRAILING doc
	// comment, refused by name (SPEC §4.1)
	lineHasCode bool
}

func (s *state) pos() Pos { return Pos{s.file, s.line, s.col} }
func (s *state) errf(p Pos, format string, args ...any) {
	s.errs = append(s.errs, Error{p, fmt.Sprintf(format, args...)})
}

func (s *state) peek() byte {
	if s.off >= len(s.src) {
		return 0
	}
	return s.src[s.off]
}

func (s *state) peek2() byte {
	if s.off+1 >= len(s.src) {
		return 0
	}
	return s.src[s.off+1]
}

func (s *state) peekAt(n int) byte {
	if s.off+n >= len(s.src) {
		return 0
	}
	return s.src[s.off+n]
}

// scanDoc scans a `///` DOC COMMENT from its first marker (SPEC §4.1): a
// contiguous run of `///` lines, each carrying nothing but the comment, whose
// last line immediately precedes the item it documents. The text is the block
// verbatim with the marker removed. At most one leading space is dropped per
// line, trailing whitespace is dropped, the lines join by single newlines, and
// nothing else is interpreted. The run's newlines are consumed so the token
// sits directly before the item.
//
// Every `///` line is part of a doc comment or is REFUSED BY NAME, each
// refusal naming `//` as the spelling that works: a `///` that TRAILS code on
// the item's own line (a qualification section included), and a block held
// off its item by a blank line or by an ordinary comment line. A block above
// something that carries no doc comment, such as package, a closing brace or
// a `const(` item, reaches the parser as a Doc token and is refused there,
// where the item is known. On a refusal nothing is returned and the line is
// skipped.
func (s *state) scanDoc() (Token, bool) {
	p := s.pos()
	if s.lineHasCode {
		s.errf(p, "a doc comment stands on its own line above the item. A /// that trails code on the item's own line is refused. Write // for a trailing comment (SPEC §4.1)")
		for s.off < len(s.src) && s.peek() != '\n' {
			s.advance()
		}
		return Token{}, false
	}
	var lines []string
	for {
		s.advance()
		s.advance()
		s.advance() // the marker
		start := s.off
		for s.off < len(s.src) && s.peek() != '\n' {
			s.advance()
		}
		text := strings.TrimRight(string(s.src[start:s.off]), " \t\r")
		text = strings.TrimPrefix(text, " ")
		lines = append(lines, text)
		// the run continues when the next line is another `///` line
		j := s.off + 1
		for j < len(s.src) && (s.src[j] == ' ' || s.src[j] == '\t' || s.src[j] == '\r') {
			j++
		}
		if s.off < len(s.src) && j+2 < len(s.src) && s.src[j] == '/' && s.src[j+1] == '/' && s.src[j+2] == '/' {
			s.advance() // the newline
			for s.off < j {
				s.advance()
			}
			continue
		}
		break
	}
	text := strings.Join(lines, "\n")
	// what follows the block: the item on the very next line, or a refusal
	if s.off >= len(s.src) {
		s.errf(p, "a doc comment touches the item it documents. This /// block has nothing under it. Write // for a comment that documents nothing (SPEC §4.1)")
		return Token{}, false
	}
	j := s.off + 1
	for j < len(s.src) && (s.src[j] == ' ' || s.src[j] == '\t' || s.src[j] == '\r') {
		j++
	}
	switch {
	case j >= len(s.src):
		s.errf(p, "a doc comment touches the item it documents. This /// block has nothing under it. Write // for a comment that documents nothing (SPEC §4.1)")
		return Token{}, false
	case s.src[j] == '\n':
		s.errf(p, "a doc comment touches the item it documents. This /// block is separated from it by a blank line. Close the gap, or write // for a working note (SPEC §4.1)")
		return Token{}, false
	case s.src[j] == '/' && j+1 < len(s.src) && (s.src[j+1] == '/' || s.src[j+1] == '*'):
		s.errf(p, "a doc comment touches the item it documents. This /// block is separated from it by a comment line. Move the /// block directly above the item, or write // for a working note (SPEC §4.1)")
		return Token{}, false
	}
	s.advance() // the newline: the token sits directly before the item
	return Token{Doc, text, p}, true
}

func (s *state) advance() {
	c := s.src[s.off]
	s.off++
	if c == '\n' {
		s.line++
		s.col = 1
	} else {
		s.col++
	}
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool { return isIdentStart(c) || (c >= '0' && c <= '9') }

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// next scans one token and keeps the per-line state a doc comment's placement
// rule reads: a Newline opens a fresh line, and any other token is CODE on the
// line it sits on.
func (s *state) next() Token {
	t := s.scanToken()
	s.lineHasCode = t.Kind != Newline && t.Kind != EOF
	return t
}

func (s *state) scanToken() Token {
	for {
		// skip spaces, carriage returns and comments; newlines are tokens
		for {
			c := s.peek()
			if c == ' ' || c == '\t' || c == '\r' {
				s.advance()
				continue
			}
			if c == '/' && s.peek2() == '/' && !s.keepComments && s.peekAt(2) == '/' {
				// a `///` DOC line (SPEC §4.1): the one comment the compiler
				// reads. In raw-scan mode it stays a Comment token verbatim,
				// which is what the formatter preserves.
				if t, ok := s.scanDoc(); ok {
					return t
				}
				continue
			}
			if c == '/' && s.peek2() == '/' {
				p := s.pos()
				start := s.off
				for s.off < len(s.src) && s.peek() != '\n' {
					s.advance()
				}
				if s.keepComments {
					return Token{Comment, string(s.src[start:s.off]), p}
				}
				continue
			}
			if c == '/' && s.peek2() == '*' {
				p := s.pos()
				start := s.off
				startLine := s.line
				s.advance()
				s.advance()
				closed := false
				for s.off < len(s.src) {
					if s.peek() == '*' && s.peek2() == '/' {
						s.advance()
						s.advance()
						closed = true
						break
					}
					s.advance()
				}
				if !closed {
					s.errf(p, "unterminated block comment")
				}
				if s.keepComments {
					return Token{Comment, string(s.src[start:s.off]), p}
				}
				if s.pipeLine {
					// the qualification section runs to the PHYSICAL end of
					// line; a block comment could swallow that newline and
					// let the section silently span lines (SPEC §4.1)
					s.errf(p, "a /* */ comment cannot sit right of | — the qualification section runs to the end of the line; use a trailing // comment (SPEC §4.1)")
				}
				if s.line > startLine {
					// a block comment spanning lines acts as a newline (Go's rule)
					s.pipeLine = false
					return Token{Newline, "\n", p}
				}
				continue
			}
			break
		}

		p := s.pos()
		if s.off >= len(s.src) {
			return Token{EOF, "", p}
		}
		c := s.peek()

		switch {
		case c == '\n':
			s.advance()
			s.pipeLine = false
			return Token{Newline, "\n", p}

		case isIdentStart(c):
			start := s.off
			for s.off < len(s.src) && isIdentPart(s.peek()) {
				s.advance()
			}
			text := string(s.src[start:s.off])
			if k, ok := keywords[text]; ok {
				return Token{k, text, p}
			}
			return Token{Ident, text, p}

		case isDigit(c):
			return s.scanNumber(p)

		case c == '"':
			start := s.off
			s.advance()
			for s.off < len(s.src) && s.peek() != '"' && s.peek() != '\n' {
				s.advance()
			}
			if s.off >= len(s.src) || s.peek() != '"' {
				s.errf(p, "unterminated string literal")
				return Token{String, string(s.src[start:s.off]) + `"`, p}
			}
			s.advance()
			return Token{String, string(s.src[start:s.off]), p}

		default:
			s.advance()
			switch c {
			case '{':
				return Token{LBrace, "{", p}
			case '}':
				return Token{RBrace, "}", p}
			case '(':
				return Token{LParen, "(", p}
			case ')':
				return Token{RParen, ")", p}
			case '[':
				return Token{LBrack, "[", p}
			case ']':
				return Token{RBrack, "]", p}
			case ',':
				return Token{Comma, ",", p}
			case ':':
				return Token{Colon, ":", p}
			case '=':
				return Token{Assign, "=", p}
			case '!':
				return Token{Not, "!", p}
			case '?':
				return Token{Question, "?", p}
			case '+':
				return Token{Plus, "+", p}
			case '-':
				return Token{Minus, "-", p}
			case '*':
				return Token{Star, "*", p}
			case '/':
				return Token{Slash, "/", p}
			case '%':
				return Token{Percent, "%", p}
			case '|':
				s.pipeLine = true
				return Token{Pipe, "|", p}
			case '.':
				if s.peek() == '.' {
					s.advance()
					return Token{DotDot, "..", p} // maximal munch: .. wins over .
				}
				return Token{Dot, ".", p}
			case '<':
				if s.peek() == '=' {
					s.advance()
					return Token{LessEq, "<=", p}
				}
				s.errf(p, "unexpected character %q — counts are range literals: [..N] or [A..B] (SPEC §4.3)", "<")
			default:
				s.errf(p, "unexpected character %q", string(c))
			}
			// error recovery: skip the bad character and keep scanning
		}
	}
}

func (s *state) scanNumber(p Pos) Token {
	start := s.off
	if s.peek() == '0' && (s.peek2() == 'x' || s.peek2() == 'X') {
		s.advance()
		s.advance()
		for isHexDigit(s.peek()) {
			s.advance()
		}
		if s.off == start+2 {
			s.errf(p, "malformed hex literal")
		}
		return Token{Int, string(s.src[start:s.off]), p}
	}
	if s.peek() == '0' && (s.peek2() == 'b' || s.peek2() == 'B') {
		s.advance()
		s.advance()
		for s.peek() == '0' || s.peek() == '1' {
			s.advance()
		}
		if s.off == start+2 {
			s.errf(p, "malformed binary literal")
		}
		return Token{Int, string(s.src[start:s.off]), p}
	}
	for isDigit(s.peek()) {
		s.advance()
	}
	isFloat := false
	// a '.' begins a fraction only if not '..' (maximal munch: 1..5 is 1 .. 5)
	if s.peek() == '.' && s.peek2() != '.' {
		isFloat = true
		s.advance()
		for isDigit(s.peek()) {
			s.advance()
		}
	}
	if s.peek() == 'e' || s.peek() == 'E' {
		saveOff, saveCol := s.off, s.col
		s.advance()
		if s.peek() == '+' || s.peek() == '-' {
			s.advance()
		}
		if isDigit(s.peek()) {
			isFloat = true
			for isDigit(s.peek()) {
				s.advance()
			}
		} else {
			// not an exponent — an identifier follows the number; restore the
			// column with the offset or every later position on the line drifts
			s.off, s.col = saveOff, saveCol
		}
	}
	kind := Int
	if isFloat {
		kind = Float
	}
	return Token{kind, string(s.src[start:s.off]), p}
}

func isHexDigit(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// filter applies the newline suppression rules (SPEC §4.1) and collapses runs.
func filter(raw []Token) []Token {
	// exactly §4.1's after-set: { ( [ , : = else and the infix operators
	// (+ - * / % ..) — NOT the unary ! and NOT the bound marker <=
	suppressAfter := map[Kind]bool{
		LBrace: true, LParen: true, LBrack: true, Comma: true, Colon: true,
		Assign: true, KwElse: true, DotDot: true,
		Plus: true, Minus: true, Star: true, Slash: true, Percent: true,
	}
	suppressBefore := map[Kind]bool{RParen: true, RBrack: true, RBrace: true}

	var out []Token
	sincePipe := false // right of |, ALL suppression is off (SPEC §4.1)
	// pipeNewline marks the last emitted Newline as the one that TERMINATED a
	// qualification section: a comma on the next line is then a separator
	// after a qualified variant, which the grammar refuses, and never a
	// continuation of the section (SPEC §4.1)
	pipeNewline := false
	for _, t := range raw {
		if t.Kind == Pipe {
			sincePipe = true
		}
		if t.Kind == Newline {
			if len(out) == 0 {
				continue // leading newlines
			}
			last := out[len(out)-1].Kind
			if last == Newline || (suppressAfter[last] && !sincePipe) {
				continue
			}
			pipeNewline = sincePipe
			sincePipe = false
			out = append(out, t)
			continue
		}
		if suppressBefore[t.Kind] && len(out) > 0 && out[len(out)-1].Kind == Newline {
			out = out[:len(out)-1] // newline immediately before ) ] } is suppressed
		}
		if t.Kind == Comma && len(out) > 0 && out[len(out)-1].Kind == Newline && !pipeNewline {
			// a newline immediately BEFORE a comma is suppressed too, so a
			// variant list that puts its separators at the head of the line
			// sees one separator rather than two (SPEC §4.1)
			out = out[:len(out)-1]
		}
		if t.Kind == EOF && len(out) > 0 && out[len(out)-1].Kind == Newline {
			out = out[:len(out)-1] // EOF synthesizes its own terminator in the parser
		}
		out = append(out, t)
	}
	if len(out) == 0 || out[len(out)-1].Kind != EOF {
		out = append(out, Token{Kind: EOF})
	}
	return out
}
