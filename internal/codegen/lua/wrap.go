package lua

import (
	"strings"
	"unicode/utf8"
)

// MaxLineLength is the longest line the Lua emitters write: luacheck's own
// default (W631), which the strict lint of the generated Lua holds at zero
// warnings with no line-length allowance
// (make/schema-lua-generated-luacheck-gate.mk).
const MaxLineLength = 120

// Wrap reflows every line of a generated Lua chunk longer than MaxLineLength
// into lines that fit, without changing what the chunk means. It is the last
// step of both emitters, so a descriptor row, a layout string or a guard of
// any length comes out clean under the strict lint whatever the schema
// declares.
//
// A long line is cut, in this order of preference, at the rightmost place that
// leaves the head within the limit:
//
//   - a trailing comment moves to its own line above the statement;
//   - after ", " (a list of arguments, fields, names or values);
//   - before " and ", " or " or " .. ";
//   - inside a quoted string, between two escapes or characters, as
//     "head" .. "tail" (never inside an escape sequence).
//
// A continuation line is indented four spaces past the line it continues. A
// comment line too long is re-flowed word by word under the same "--" prefix.
// None of these cuts can change the chunk's tokens: Lua reads a newline
// between two tokens as whitespace, a cut after "," or before a binary
// operator can never be read as the start of a call, and two adjacent string
// literals joined by ".." are the one string they were. A line with no such
// place (one token longer than the limit) is left as it is, and the lint
// names it.
func Wrap(src []byte) []byte {
	if !hasLongLine(src) {
		return src
	}
	lines := strings.Split(string(src), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, wrapLine(line)...)
	}
	return []byte(strings.Join(out, "\n"))
}

func hasLongLine(src []byte) bool {
	for len(src) > 0 {
		i := 0
		for i < len(src) && src[i] != '\n' {
			i++
		}
		if i > MaxLineLength && utf8.RuneCount(src[:i]) > MaxLineLength {
			return true
		}
		if i == len(src) {
			return false
		}
		src = src[i+1:]
	}
	return false
}

func width(s string) int { return utf8.RuneCountInString(s) }

func wrapLine(line string) []string {
	if width(line) <= MaxLineLength {
		return []string{line}
	}
	indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
	body := line[len(indent):]
	if strings.HasPrefix(body, "--") {
		return wrapComment(indent, body)
	}
	if at := commentStart(body); at >= 0 {
		code := strings.TrimRight(body[:at], " ")
		comment := body[at:]
		return append(wrapComment(indent, comment), wrapCode(indent, indent+code)...)
	}
	return wrapCode(indent, line)
}

// wrapComment re-flows one comment word by word under its own "--" prefix.
func wrapComment(indent, comment string) []string {
	prefix := "--"
	text := strings.TrimPrefix(comment, "--")
	if strings.HasPrefix(text, " ") {
		prefix = "-- "
		text = text[1:]
	}
	if strings.HasPrefix(text, "[") {
		// a long comment opener is never split
		return []string{indent + comment}
	}
	var out []string
	cur := indent + prefix
	empty := true
	for _, word := range strings.Fields(text) {
		if !empty && width(cur)+1+width(word) > MaxLineLength {
			out = append(out, cur)
			cur = indent + prefix
			empty = true
		}
		if !empty {
			cur += " "
		}
		cur += word
		empty = false
	}
	return append(out, cur)
}

// cut is one place a code line may be split: the head ends at head, the tail
// resumes at tail, and quote (when nonzero) marks a cut inside a string
// literal of that quote (the head gains a closing quote and the tail opens
// with `.. "`).
type cut struct {
	head, tail int
	quote      byte
}

func wrapCode(indent, line string) []string {
	cont := indent + "    "
	// The cuts are found once, over the whole line, and the line is walked
	// left to right taking the rightmost cut that fits each time: linear in
	// the line, which matters for a layout string hundreds of kilobytes long.
	cs := cuts(line)
	runes := make([]int, len(line)+1) // runes[i] is the rune count of line[:i]
	for i := range len(line) {
		runes[i+1] = runes[i]
		if utf8.RuneStart(line[i]) {
			runes[i+1]++
		}
	}
	var out []string
	head := "" // what the current output line starts with before line[start:]
	headW := 0 // its width
	start := 0 // where the current output line's text begins in line
	next := 0  // the first cut not yet behind start
	for headW+runes[len(line)]-runes[start] > MaxLineLength {
		for next < len(cs) && cs[next].head <= start {
			next++
		}
		best := -1
		for j := next; j < len(cs); j++ {
			c := cs[j]
			w := headW + runes[c.head] - runes[start]
			if c.quote != 0 {
				w++
			}
			if w > MaxLineLength {
				break
			}
			if runes[c.head]-runes[start] > 8 {
				best = j
			}
		}
		if best < 0 {
			break
		}
		c := cs[best]
		if c.quote != 0 {
			q := string(c.quote)
			out = append(out, head+line[start:c.head]+q)
			head = cont + ".. " + q
		} else {
			out = append(out, strings.TrimRight(head+line[start:c.head], " "))
			head = cont
		}
		headW = width(head)
		start = c.tail
		for c.quote == 0 && start < len(line) && line[start] == ' ' {
			start++
		}
		next = best + 1
	}
	return append(out, head+line[start:])
}

// commentStart is the byte offset of a trailing "--" comment outside any
// string, or -1.
func commentStart(s string) int {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch {
		case c == '"' || c == '\'':
			quote = c
		case c == '[' && longBracket(s[i:]) > 0:
			end := strings.Index(s[i:], "]"+strings.Repeat("=", longBracket(s[i:])-2)+"]")
			if end < 0 {
				return -1
			}
			i += end
		case c == '-' && strings.HasPrefix(s[i:], "--"):
			return i
		}
	}
	return -1
}

// longBracket is the length of a long-bracket opener ([[, [=[, ...) at the
// start of s, or 0.
func longBracket(s string) int {
	if len(s) < 2 || s[0] != '[' {
		return 0
	}
	i := 1
	for i < len(s) && s[i] == '=' {
		i++
	}
	if i < len(s) && s[i] == '[' {
		return i + 1
	}
	return 0
}

// cuts lists every place line may be split without changing its tokens.
func cuts(line string) []cut {
	var out []cut
	start := len(line) - len(strings.TrimLeft(line, " "))
	for i := start; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"' || c == '\'':
			j := i + 1
			var inside []cut
			for j < len(line) && line[j] != c {
				if line[j] == '\\' {
					j += escapeLen(line[j:])
				} else {
					j += utf8RuneLen(line[j:])
				}
				inside = append(inside, cut{head: j, tail: j, quote: c})
			}
			// a cut right before the closing quote would leave an empty tail
			if n := len(inside); n > 0 && inside[n-1].head >= j {
				inside = inside[:n-1]
			}
			if concatSafe(line[:i], line[min(j+1, len(line)):]) {
				out = append(out, inside...)
			}
			i = j
		case c == '[' && longBracket(line[i:]) > 0:
			n := longBracket(line[i:])
			end := strings.Index(line[i+n:], "]"+strings.Repeat("=", n-2)+"]")
			if end < 0 {
				return out
			}
			i += n + end + n - 1
		case c == '-' && strings.HasPrefix(line[i:], "--"):
			return out
		case c == ',' && i+1 < len(line) && line[i+1] == ' ':
			out = append(out, cut{head: i + 1, tail: i + 2})
		case c == ' ':
			for _, op := range []string{" and ", " or ", " .. "} {
				if strings.HasPrefix(line[i:], op) {
					out = append(out, cut{head: i, tail: i + 1})
				}
			}
		}
	}
	return out
}

// escapeLen is the byte length of the escape sequence at the start of s
// (s[0] == '\\'), so a string cut never lands inside one.
func escapeLen(s string) int {
	if len(s) < 2 {
		return len(s)
	}
	switch c := s[1]; {
	case c == 'x':
		return min(4, len(s))
	case c >= '0' && c <= '9':
		n := 2
		for n < 4 && n < len(s) && s[n] >= '0' && s[n] <= '9' {
			n++
		}
		return n
	case c == 'u':
		if end := strings.IndexByte(s, '}'); end > 0 {
			return end + 1
		}
		return len(s)
	case c == 'z':
		n := 2
		for n < len(s) && (s[n] == ' ' || s[n] == '\t') {
			n++
		}
		return n
	}
	return 2
}

func utf8RuneLen(s string) int {
	_, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return 1
	}
	return n
}

// concatSafe reports whether a string literal standing between before and
// after can be spelled as two literals joined by "..": only where the
// operators around it bind no tighter than "..". Before it, an assignment,
// a comparison, an opening bracket, a comma or another ".."; after it, the
// end of the line, a closing bracket, a comma, "..", a comparison or a
// keyword that ends an expression. Never after "#", an arithmetic operator,
// or a name (a call such as require"x").
func concatSafe(before, after string) bool {
	b := strings.TrimRight(before, " ")
	a := strings.TrimLeft(after, " ")
	okBefore := b == "" || strings.HasSuffix(b, "..") || strings.HasSuffix(b, "=") ||
		strings.HasSuffix(b, "<") || strings.HasSuffix(b, ">") ||
		strings.ContainsAny(b[max(len(b)-1, 0):], "({[,")
	if !okBefore {
		return false
	}
	if a == "" || strings.HasPrefix(a, "..") || strings.ContainsAny(a[:1], ")}],") {
		return true
	}
	for _, op := range []string{"==", "~=", "<", ">", "then", "and", "or", "do", "end", ";"} {
		if strings.HasPrefix(a, op) {
			return true
		}
	}
	return false
}
