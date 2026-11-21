package main

import (
	"regexp"
	"strconv"
	"strings"
)

// The text operators are the Go operators' faults again, for code this
// repository holds as text: an emitted-code template inside a generator's
// string literal, and an embedded runtime file. There is no syntax tree for
// seventeen languages here, so each operator is a narrow rewrite of one line,
// and a line's comment is never touched.
const (
	opTextCmpFlip   = "text-cmp-flip"    // ` < ` to ` <= `, ` == ` to ` != `, and back
	opTextOffByOne  = "text-off-by-one"  // a decimal integer, plus one
	opTextBitWidth  = "text-bit-width"   // 8, 16, 32, 64 and their minus-ones on a line about bits, plus one
	opTextNegate    = "text-negate-cond" // an `if` condition negated
	opTextDropCheck = "text-drop-check"  // an `if` condition that compares magnitudes becomes false
	opTextAccept    = "text-accept"      // `return false` becomes `return true`: a refusal deleted
	opTextDropLine  = "text-drop-line"   // one write/put/append statement line deleted, in a runtime file
)

// textRules is what the text operators need to know about a target's
// language: how an `if` ends its condition, what starts a comment, and how
// false is spelled.
type textRules struct {
	// Syntax is how a condition is delimited: "paren" (`if ( c )`), "brace"
	// (`if c {`), "then" (`if c then`), "colon" (`if c:`), "do" (`if c do`),
	// or "line" (`if c` to the end of the line, and `if c then`).
	Syntax string
	// Comment starts a comment that runs to the end of the line.
	Comment string
	// False is the language's false literal.
	False string
}

var (
	textCmp   = regexp.MustCompile(` (<=|>=|==|!=|~=|<|>) `)
	textInt   = regexp.MustCompile(`[0-9]+`)
	textIf    = regexp.MustCompile(`\bif\b`)
	textRel   = regexp.MustCompile(` (<=|>=|<|>) `)
	textRet   = regexp.MustCompile(`\breturn (false|False)\b`)
	textBits  = regexp.MustCompile(`(?i)bits|width|shift|<<|>>`)
	textWrite = regexp.MustCompile(`(?i)^\s*[\w.:@]*(write|put|append|push|emit)\w*\s*\(.*\)\s*;?\s*$`)
)

var textCmpFlip = map[string]string{"<": "<=", "<=": "<", ">": ">=", ">=": ">", "==": "!=", "!=": "==", "~=": "=="}

var widths = map[int64]bool{7: true, 8: true, 15: true, 16: true, 31: true, 32: true, 63: true, 64: true}

// enumerate lists the text mutants of one region: text is the region and base
// its offset in the file. interpreted says the region is the inside of a Go
// interpreted string literal, where the two characters `\n` end a line and a
// backslash escapes what follows. wholeFile says the region is a runtime file,
// which alone gets the line-deleting operator.
func (r *textRules) enumerate(base int, text string, interpreted, wholeFile bool) []Mutant {
	var muts []Mutant
	add := func(op string, start, end int, replacement string) {
		muts = append(muts, Mutant{Op: op, Start: base + start, End: base + end, Old: text[start:end], New: replacement})
	}
	for _, ln := range splitLines(text, interpreted) {
		line := text[ln[0]:ln[1]]
		code := line
		if r.Comment != "" {
			if i := commentStart(line, r.Comment); i >= 0 {
				code = line[:i]
			}
		}
		at := ln[0]
		for _, m := range textCmp.FindAllStringSubmatchIndex(code, -1) {
			op := code[m[2]:m[3]]
			flipped := textCmpFlip[op]
			if op == "==" && r.Syntax == "then" {
				flipped = "~="
			}
			add(opTextCmpFlip, at+m[2], at+m[3], flipped)
		}
		aboutBits := textBits.MatchString(code)
		for _, m := range textInt.FindAllStringIndex(code, -1) {
			if !standaloneInt(code, m[0], m[1]) {
				continue
			}
			v, err := strconv.ParseInt(code[m[0]:m[1]], 10, 64)
			if err != nil || v > 1<<20 || (len(code[m[0]:m[1]]) > 1 && code[m[0]] == '0') {
				continue
			}
			op := opTextOffByOne
			if aboutBits && widths[v] {
				op = opTextBitWidth
			}
			add(op, at+m[0], at+m[1], strconv.FormatInt(v+1, 10))
		}
		for _, m := range textIf.FindAllStringIndex(code, -1) {
			start, end, ok := r.condition(code, m[1])
			if !ok {
				continue
			}
			cond := code[start:end]
			if r.Syntax == "then" || r.Syntax == "colon" {
				add(opTextNegate, at+start, at+end, "not ("+cond+")")
			} else {
				add(opTextNegate, at+start, at+end, "!("+cond+")")
			}
			if textRel.MatchString(cond) {
				add(opTextDropCheck, at+start, at+end, r.False)
			}
		}
		for _, m := range textRet.FindAllStringSubmatchIndex(code, -1) {
			accept := "true"
			if code[m[2]:m[3]] == "False" {
				accept = "True"
			}
			add(opTextAccept, at+m[2], at+m[3], accept)
		}
		if wholeFile && textWrite.MatchString(code) && balanced(code) {
			add(opTextDropLine, at, at+len(code), "")
		}
	}
	return muts
}

// splitLines returns the [start, end) of every line of text.
func splitLines(text string, interpreted bool) [][2]int {
	var lines [][2]int
	start := 0
	for i := 0; i < len(text); i++ {
		switch {
		case text[i] == '\n':
			lines = append(lines, [2]int{start, i})
			start = i + 1
		case interpreted && text[i] == '\\' && i+1 < len(text):
			if text[i+1] == 'n' {
				lines = append(lines, [2]int{start, i})
				start = i + 2
			}
			i++
		}
	}
	return append(lines, [2]int{start, len(text)})
}

// commentStart is where a line's comment starts, or -1. A `#` marker counts
// only at the start of the line or after a space and before a space, so the
// Lua length operator and a C preprocessor line are not read as comments.
func commentStart(line, marker string) int {
	from := 0
	for {
		i := strings.Index(line[from:], marker)
		if i < 0 {
			return -1
		}
		i += from
		if marker != "#" {
			return i
		}
		before := i == 0 || line[i-1] == ' ' || line[i-1] == '\t'
		after := i+1 == len(line) || line[i+1] == ' ' || line[i+1] == '!'
		if before && after {
			return i
		}
		from = i + 1
	}
}

// standaloneInt reports whether the digits at [start, end) are an integer of
// their own: not part of a name, a float, a hex literal, a format verb or an
// escape.
func standaloneInt(code string, start, end int) bool {
	if start > 0 {
		c := code[start-1]
		if isWord(c) || strings.IndexByte(`.%\$#'"@[`, c) >= 0 && (c != '[' || (start > 1 && code[start-2] == '%')) {
			return false
		}
	}
	if end < len(code) {
		c := code[end]
		if isWord(c) || c == '.' {
			return false
		}
	}
	return true
}

func isWord(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// condition finds the condition of the `if` whose keyword ends at after, as
// the [start, end) of its text in code, by the language's own delimiter. An
// `if` whose delimiter is not on the line is left alone: it is prose, or a
// condition this operator cannot see whole.
func (r *textRules) condition(code string, after int) (int, int, bool) {
	i := after
	for i < len(code) && code[i] == ' ' {
		i++
	}
	if i == after && (i >= len(code) || code[i] != '(') {
		return 0, 0, false
	}
	end := -1
	switch r.Syntax {
	case "paren":
		if i >= len(code) || code[i] != '(' {
			return 0, 0, false
		}
		depth := 0
		for j := i; j < len(code); j++ {
			if code[j] == '(' {
				depth++
			} else if code[j] == ')' {
				depth--
				if depth == 0 {
					end = j
					break
				}
			}
		}
		i++
	case "brace":
		end = delimiter(code, i, " {")
	case "then":
		end = delimiter(code, i, " then")
	case "do":
		end = delimiter(code, i, " do")
	case "colon":
		end = delimiter(code, i, ":")
	case "line":
		if end = delimiter(code, i, " then"); end < 0 {
			end = len(code)
		}
	}
	if end < 0 {
		return 0, 0, false
	}
	for i < end && code[i] == ' ' {
		i++
	}
	for end > i && code[end-1] == ' ' {
		end--
	}
	if end <= i || !balanced(code[i:end]) {
		return 0, 0, false
	}
	return i, end, true
}

// delimiter is the first index of d in code at or after from that is outside
// every bracket, or -1.
func delimiter(code string, from int, d string) int {
	depth := 0
	for j := from; j < len(code); j++ {
		switch code[j] {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		}
		if depth == 0 && strings.HasPrefix(code[j:], d) {
			if d == ":" && (strings.HasPrefix(code[j:], "::") || (j > 0 && code[j-1] == ':')) {
				continue
			}
			return j
		}
	}
	return -1
}

func balanced(s string) bool {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0
}
