package recipescan

import (
	"strings"
)

// A token is one shell word or one control operator. Words keep their quotes;
// operators are the text of the operator. A newline that ends a command is the
// operator ";".
type token struct {
	text string
	op   bool
	line int // 0-based line offset inside the scanned text
}

// lex splits shell text into words and control operators. It understands
// single and double quotes, backslash escapes, $(...) and backtick command
// substitution, ${...}, comments, and redirections that contain an ampersand
// (2>&1, >&2, &>file), which are part of a word and never the background
// operator. In makefile mode "$$" is one dollar and a single "$(" or "${"
// opens a make expansion, which is kept inside the word like a substitution.
//
// subs receives the inner text of every command substitution, so the caller
// can scan it as shell text of its own.
func lex(src string, makefile bool) (toks []token, subs []sub) {
	var word strings.Builder
	line := 0
	wordLine := 0
	inWord := false
	flush := func() {
		if inWord {
			toks = append(toks, token{text: word.String(), line: wordLine})
			word.Reset()
			inWord = false
		}
	}
	start := func() {
		if !inWord {
			inWord = true
			wordLine = line
		}
	}
	op := func(s string) {
		flush()
		toks = append(toks, token{text: s, op: true, line: line})
	}
	n := len(src)
	i := 0
	var pendingHeredocs []string
	// dollar reports whether src[i:] starts a shell dollar, and its width.
	dollar := func(i int) (bool, int) {
		if makefile {
			if strings.HasPrefix(src[i:], "$$") {
				return true, 2
			}
			return false, 0
		}
		if src[i] == '$' {
			return true, 1
		}
		return false, 0
	}
	// balanced returns the index just past the bracket that closes the one
	// opened before position i. Quotes inside are honoured.
	var balanced func(i int, open, shut byte) int
	balanced = func(i int, open, shut byte) int {
		depth := 1
		for i < n {
			c := src[i]
			switch {
			case c == '\\' && i+1 < n:
				i += 2
				continue
			case c == '\'':
				j := strings.IndexByte(src[i+1:], '\'')
				if j < 0 {
					return n
				}
				i += j + 2
				continue
			case c == open:
				depth++
			case c == shut:
				depth--
				if depth == 0 {
					return i + 1
				}
			}
			if c == '\n' {
				line++
			}
			i++
		}
		return n
	}
	// substitution consumes a dollar construct or a make expansion at i and
	// returns the index after it, recording command substitutions.
	substitution := func(i int) int {
		begin := i
		if ok, w := dollar(i); ok {
			j := i + w
			if j < n && src[j] == '(' {
				if j+1 < n && src[j+1] == '(' { // arithmetic
					end := balanced(j+1, '(', ')')
					word.WriteString(src[begin:end])
					return end
				}
				at := line
				end := balanced(j+1, '(', ')')
				inner := src[j+1 : max(j+1, end-1)]
				subs = append(subs, sub{text: inner, line: at})
				word.WriteString(src[begin:end])
				return end
			}
			if j < n && src[j] == '{' {
				end := balanced(j+1, '{', '}')
				word.WriteString(src[begin:end])
				return end
			}
			word.WriteString(src[begin:j])
			return j
		}
		// A make expansion: $(...) or ${...} or $x.
		if makefile && src[i] == '$' && i+1 < n {
			switch src[i+1] {
			case '(':
				end := balanced(i+2, '(', ')')
				word.WriteString(src[begin:end])
				return end
			case '{':
				end := balanced(i+2, '{', '}')
				word.WriteString(src[begin:end])
				return end
			}
			word.WriteString(src[i : i+2])
			return i + 2
		}
		word.WriteByte(src[i])
		return i + 1
	}
	for i < n {
		c := src[i]
		switch {
		case c == '\\' && i+1 < n:
			if src[i+1] == '\n' {
				line++
				i += 2
				continue
			}
			start()
			word.WriteString(src[i : i+2])
			i += 2
		case c == '\'':
			start()
			j := strings.IndexByte(src[i+1:], '\'')
			end := n
			if j >= 0 {
				end = i + j + 2
			}
			line += strings.Count(src[i:end], "\n")
			word.WriteString(src[i:end])
			i = end
		case c == '"':
			start()
			word.WriteByte('"')
			i++
			for i < n && src[i] != '"' {
				switch {
				case src[i] == '\\' && i+1 < n:
					word.WriteString(src[i : i+2])
					i += 2
				case src[i] == '$':
					i = substitution(i)
				case src[i] == '`':
					j := strings.IndexByte(src[i+1:], '`')
					end := n
					if j >= 0 {
						end = i + j + 2
						subs = append(subs, sub{text: src[i+1 : end-1], line: line})
					}
					word.WriteString(src[i:end])
					i = end
				default:
					if src[i] == '\n' {
						line++
					}
					word.WriteByte(src[i])
					i++
				}
			}
			if i < n {
				word.WriteByte('"')
				i++
			}
		case c == '`':
			start()
			j := strings.IndexByte(src[i+1:], '`')
			end := n
			if j >= 0 {
				end = i + j + 2
				subs = append(subs, sub{text: src[i+1 : end-1], line: line})
			}
			word.WriteString(src[i:end])
			i = end
		case c == '$':
			start()
			i = substitution(i)
		case c == '<' && i+1 < n && src[i+1] == '<' && !(i+2 < n && src[i+2] == '<'):
			// a here-document: its body is data, not shell text
			j := i + 2
			if j < n && src[j] == '-' {
				j++
			}
			for j < n && (src[j] == ' ' || src[j] == '\t') {
				j++
			}
			k := j
			for k < n && src[k] != '\n' && src[k] != ' ' && src[k] != '\t' && src[k] != ';' && src[k] != ')' && src[k] != '|' && src[k] != '&' {
				k++
			}
			delim := strings.Trim(src[j:k], `'"\`)
			start()
			word.WriteString(src[i:k])
			i = k
			if delim == "" {
				continue
			}
			pendingHeredocs = append(pendingHeredocs, delim)
		case c == '#' && !inWord:
			for i < n && src[i] != '\n' {
				i++
			}
		case c == '\n':
			op(";")
			line++
			i++
			for _, delim := range pendingHeredocs {
				for i < n {
					end := strings.IndexByte(src[i:], '\n')
					var l string
					if end < 0 {
						l = src[i:]
						i = n
					} else {
						l = src[i : i+end]
						i += end + 1
						line++
					}
					if strings.TrimLeft(l, "\t") == delim {
						break
					}
				}
			}
			pendingHeredocs = nil
		case c == ' ' || c == '\t' || c == '\r':
			flush()
			i++
		case c == ';':
			if i+1 < n && src[i+1] == ';' {
				op(";;")
				i += 2
			} else {
				op(";")
				i++
			}
		case c == '|':
			switch {
			case i+1 < n && src[i+1] == '|':
				op("||")
				i += 2
			case i+1 < n && src[i+1] == '&':
				op("|")
				i += 2
			default:
				op("|")
				i++
			}
		case c == '&':
			switch {
			case i+1 < n && src[i+1] == '&':
				op("&&")
				i += 2
			case i+1 < n && src[i+1] == '>': // &>file
				start()
				word.WriteString("&>")
				i += 2
			case inWord && strings.HasSuffix(word.String(), ">"), inWord && strings.HasSuffix(word.String(), "<"):
				// >&2, 2>&1, <&3
				word.WriteByte('&')
				i++
			default:
				op("&")
				i++
			}
		case c == '(' || c == ')':
			op(string(c))
			i++
		default:
			start()
			word.WriteByte(c)
			i++
		}
	}
	flush()
	return toks, subs
}

type sub struct {
	text string
	line int
}

// unquote strips one layer of matching quotes from a word.
func unquote(w string) string {
	if len(w) >= 2 && (w[0] == '\'' || w[0] == '"') && w[len(w)-1] == w[0] {
		return w[1 : len(w)-1]
	}
	return w
}

// isAssign reports whether a word is a leading VAR=value assignment.
func isAssign(w string) bool {
	eq := strings.IndexByte(w, '=')
	if eq <= 0 {
		return false
	}
	for i := 0; i < eq; i++ {
		c := w[i]
		if c != '_' && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') && (i == 0 || c < '0' || c > '9') {
			return false
		}
	}
	return true
}
