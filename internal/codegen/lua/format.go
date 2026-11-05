package lua

import (
	"strings"
	"unicode/utf8"
)

// MaxLineLength is the longest line the Lua emitters write: luacheck's own
// default (W631) and the pinned StyLua's column_width (make/lua-stylua.toml).
const MaxLineLength = 120

// Format lays out a generated Lua chunk the way the pinned StyLua does
// (make/lua-stylua.toml), so the generated corpus is StyLua-canonical as
// written and the format gate (make/schema-lua-generated-luacheck-gate.mk,
// tables-lua-format) is a no-diff check with no allowance file. It is the
// last step of both emitters. The emitters write StyLua's shapes for what fits
// on a line (an `if ... then return false end` on one line, a loop body on its
// own line, no redundant parentheses); Format handles what depends on width:
//
//   - a table constructor whose one-line form does not fit is expanded one
//     field per line, each field with a trailing comma, and each field's own
//     table laid out by the same rule (StyLua keeps a table on one line
//     exactly when it fits);
//   - a string literal longer than the line is spelled as fitting pieces
//     joined by "..", one piece per line, each continuation hung two spaces
//     in with the operator first (StyLua's binary-expression hang; it never
//     splits a string itself, so a piece is cut only where luacheck's 120
//     columns demand it, and never inside an escape);
//   - an `if <cond> then <body> end` too long for one line becomes the block
//     form, and a condition too long for `if <cond> then` hangs one operand
//     of its lowest-precedence `or` (else `and`) chain per line;
//   - a trailing comment that would push its line past the limit moves to its
//     own line above the statement, and a comment line too long is re-flowed
//     word by word under the same prefix.
//
// None of these changes a token: Lua reads a newline between two tokens as
// whitespace, and adjacent literals joined by ".." are the one string they
// were (a piece is cut only where ".." binds no looser than its neighbours).
func Format(src []byte) []byte {
	if !hasLongLine(src) {
		return src
	}
	lines := strings.Split(string(src), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, formatLine(line)...)
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

func fits(s string) bool { return width(s) <= MaxLineLength }

func formatLine(line string) []string {
	if fits(line) {
		return []string{line}
	}
	indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
	body := line[len(indent):]
	if strings.HasPrefix(body, "--") {
		return formatComment(indent, body)
	}
	if at := commentStart(body); at >= 0 {
		code := strings.TrimRight(body[:at], " ")
		return append(formatComment(indent, body[at:]), formatCode(indent, code)...)
	}
	return formatCode(indent, body)
}

// formatComment re-flows one comment word by word under its own "--" prefix.
func formatComment(indent, comment string) []string {
	if fits(indent + comment) {
		return []string{indent + comment}
	}
	prefix := "--"
	text := strings.TrimPrefix(comment, "--")
	if strings.HasPrefix(text, " ") {
		prefix = "-- "
		text = text[1:]
	}
	if strings.HasPrefix(text, "[") {
		return []string{indent + comment} // a long comment is never split
	}
	var out []string
	cur := indent + prefix
	empty := true
	for word := range strings.FieldsSeq(text) {
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

// formatCode lays out one statement (no comment) at indent.
func formatCode(indent, code string) []string {
	if fits(indent + code) {
		return []string{indent + code}
	}
	if out, ok := formatIf(indent, code); ok {
		return out
	}
	// prefix VALUE suffix, where VALUE is one table constructor or one
	// string literal and suffix is "" or ","
	suffix := ""
	core := code
	if strings.HasSuffix(core, ",") {
		suffix = ","
		core = core[:len(core)-1]
	}
	if open := valueStart(core); open >= 0 {
		prefix, value := core[:open], core[open:]
		switch value[0] {
		case '{':
			if end := matchBrace(value, 0); end == len(value)-1 {
				if t, ok := parseTable(value); ok {
					return renderTable(t, indent, prefix, suffix)
				}
			}
		case '"', '\'':
			if end := stringEnd(value, 0); end == len(value)-1 {
				return splitString(indent, prefix, value, suffix)
			}
		}
	}
	return []string{indent + code}
}

// valueStart is where the value of `<target> = <value>`, `return <value>` or
// a bare `<value>` begins, or -1.
func valueStart(s string) int {
	if s != "" && (s[0] == '{' || s[0] == '"' || s[0] == '\'') {
		return 0
	}
	if strings.HasPrefix(s, "return ") {
		return len("return ")
	}
	if i := topLevelAssign(s); i >= 0 && i+3 <= len(s) {
		return i + 3
	}
	return -1
}

// topLevelAssign is the offset of the first " = " outside strings and
// brackets, or -1.
func topLevelAssign(s string) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"', '\'':
			i = stringEnd(s, i)
			if i < 0 {
				return -1
			}
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			depth--
		case ' ':
			if depth == 0 && strings.HasPrefix(s[i:], " = ") {
				return i
			}
		}
	}
	return -1
}

// stringEnd is the offset of the closing quote of the string literal opening
// at s[i], or -1.
func stringEnd(s string, i int) int {
	q := s[i]
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case q:
			return j
		}
	}
	return -1
}

// matchBrace is the offset of the bracket closing the one at s[i], or -1.
func matchBrace(s string, i int) int {
	depth := 0
	for j := i; j < len(s); j++ {
		switch c := s[j]; c {
		case '"', '\'':
			j = stringEnd(s, j)
			if j < 0 {
				return -1
			}
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return -1
}

// table is a parsed table constructor: each field is its key prefix ("name =
// ", "[k] = " or "" for a positional value) and its value, a nested table or
// opaque text.
type table struct{ fields []field }

type field struct {
	key   string
	value string // opaque, when sub is nil
	sub   *table
}

// parseTable reads a one-line table constructor the emitters write.
func parseTable(s string) (*table, bool) {
	if len(s) < 2 || s[0] != '{' || s[len(s)-1] != '}' {
		return nil, false
	}
	t := &table{}
	inner := s[1 : len(s)-1]
	for _, part := range splitTop(inner, ',') {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		f := field{}
		if i := topLevelAssign(part); i >= 0 {
			f.key = part[:i+3]
			part = part[i+3:]
		}
		if part != "" && part[0] == '{' && matchBrace(part, 0) == len(part)-1 {
			sub, ok := parseTable(part)
			if !ok {
				return nil, false
			}
			f.sub = sub
		} else {
			f.value = part
		}
		t.fields = append(t.fields, f)
	}
	return t, true
}

// splitTop splits s at every sep outside strings and brackets.
func splitTop(s string, sep byte) []string {
	var out []string
	depth, from := 0, 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"' || c == '\'':
			if e := stringEnd(s, i); e >= 0 {
				i = e
			}
		case c == '(' || c == '{' || c == '[':
			depth++
		case c == ')' || c == '}' || c == ']':
			depth--
		case c == sep && depth == 0:
			out = append(out, s[from:i])
			from = i + 1
		}
	}
	return append(out, s[from:])
}

// TableOf spells a one-line table constructor of the given items as StyLua
// does: "{ a, b }", and "{}" when there are none.
func TableOf(items []string) string {
	if len(items) == 0 {
		return "{}"
	}
	return "{ " + strings.Join(items, ", ") + " }"
}

func (t *table) inline() string {
	if len(t.fields) == 0 {
		return "{}"
	}
	parts := make([]string, len(t.fields))
	for i, f := range t.fields {
		parts[i] = f.inline()
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func (f field) inline() string {
	if f.sub != nil {
		return f.key + f.sub.inline()
	}
	return f.key + f.value
}

// renderTable lays a table out StyLua's way: on one line when it fits, else
// one field per line with a trailing comma.
func renderTable(t *table, indent, prefix, suffix string) []string {
	one := indent + prefix + t.inline() + suffix
	if fits(one) || len(t.fields) == 0 {
		return []string{one}
	}
	out := []string{indent + prefix + "{"}
	inner := indent + "  "
	for _, f := range t.fields {
		if f.sub != nil {
			out = append(out, renderTable(f.sub, inner, f.key, ",")...)
			continue
		}
		if v := f.value; v != "" && (v[0] == '"' || v[0] == '\'') && stringEnd(v, 0) == len(v)-1 {
			out = append(out, splitString(inner, f.key, v, ",")...)
			continue
		}
		out = append(out, inner+f.key+f.value+",")
	}
	return append(out, indent+"}"+suffix)
}

// splitString spells one long string literal as pieces joined by "..", the
// first after prefix and each later one on its own line, hung two spaces in.
func splitString(indent, prefix, lit, suffix string) []string {
	if fits(indent + prefix + lit + suffix) {
		return []string{indent + prefix + lit + suffix}
	}
	q := lit[:1]
	body := lit[1 : len(lit)-1]
	hang := indent + "  .. "
	var out []string
	head := indent + prefix
	for {
		room := MaxLineLength - width(head) - 2 // the two quotes
		if width(body) <= room-len(suffix) {
			out = append(out, head+quoted(q, body)+suffix)
			return out
		}
		cut := pieceEnd(body, room)
		if cut <= 0 {
			// not even one escape fits: leave the rest whole
			out = append(out, head+quoted(q, body)+suffix)
			return out
		}
		out = append(out, head+quoted(q, body[:cut]))
		body = body[cut:]
		head = hang
	}
}

// quoted spells one string piece in StyLua's AutoPreferDouble style: double
// quotes, unless the piece carries more double quotes than single quotes, when
// it takes single quotes and its escaped double quotes lose the backslash. A
// piece is only ever written as the emitter quoted it otherwise.
func quoted(q, body string) string {
	if q != `"` {
		return q + body + q
	}
	dq, sq := 0, 0
	for i := 0; i < len(body); {
		n := 1
		switch body[i] {
		case '\\':
			n = escapeLen(body[i:])
			if body[i:i+n] == `\"` {
				dq++
			}
		case '\'':
			sq++
		}
		i += n
	}
	if dq <= sq {
		return `"` + body + `"`
	}
	var b strings.Builder
	b.WriteByte('\'')
	for i := 0; i < len(body); {
		n := 1
		if body[i] == '\\' {
			n = escapeLen(body[i:])
		}
		if body[i:i+n] == `\"` {
			b.WriteByte('"')
		} else {
			b.WriteString(body[i : i+n])
		}
		i += n
	}
	b.WriteByte('\'')
	return b.String()
}

// pieceEnd is the longest prefix of a string body that is at most room
// characters wide and does not end inside an escape sequence.
func pieceEnd(body string, room int) int {
	end, w := 0, 0
	for end < len(body) {
		var n int
		if body[end] == '\\' {
			n = escapeLen(body[end:])
		} else {
			_, n = utf8.DecodeRuneInString(body[end:])
		}
		cw := width(body[end : end+n])
		if w+cw > room {
			break
		}
		end += n
		w += cw
	}
	return end
}

// formatIf lays out `if <cond> then <body> end` (no elseif or else) that does
// not fit on one line: the block form, and the condition hung by operand when
// `if <cond> then` itself does not fit.
func formatIf(indent, code string) ([]string, bool) {
	if !strings.HasPrefix(code, "if ") || !strings.HasSuffix(code, " end") {
		return nil, false
	}
	then := topLevelKeyword(code, " then ")
	if then < 0 {
		return nil, false
	}
	cond := code[len("if "):then]
	body := strings.TrimSpace(code[then+len(" then ") : len(code)-len(" end")])
	if topLevelKeyword(" "+body+" ", " if ") >= 0 || topLevelKeyword(" "+body+" ", " end ") >= 0 ||
		topLevelKeyword(" "+body+" ", " else ") >= 0 || topLevelKeyword(" "+body+" ", " elseif ") >= 0 {
		return nil, false
	}
	var out []string
	if fits(indent + "if " + cond + " then") {
		out = append(out, indent+"if "+cond+" then")
	} else {
		hung, ok := hangCondition(indent+"  ", cond)
		if !ok {
			return nil, false
		}
		out = append(out, indent+"if")
		out = append(out, hung...)
		out = append(out, indent+"then")
	}
	for _, stmt := range splitTop(body, ';') {
		if stmt = strings.TrimSpace(stmt); stmt != "" {
			out = append(out, formatCode(indent+"  ", stmt)...)
		}
	}
	return append(out, indent+"end"), true
}

// hangCondition lays out an `if` condition on the lines between `if` and
// `then`: one operand of its lowest-precedence `or` (else `and`) chain per
// line (StyLua hangs a chain even when it would fit on the one line, and that
// is its fixed point); else the whole condition when it fits; else, for a
// single call (or `not` a call), the call's arguments one per line.
func hangCondition(indent, cond string) ([]string, bool) {
	for _, op := range []string{" or ", " and "} {
		operands := splitOperator(cond, op)
		if len(operands) < 2 {
			continue
		}
		out := make([]string, len(operands))
		for i, o := range operands {
			if i == 0 {
				out[i] = indent + o
			} else {
				out[i] = indent + op[1:] + o
			}
		}
		return out, true
	}
	if fits(indent + cond) {
		return []string{indent + cond}, true
	}
	head := ""
	call := cond
	if strings.HasPrefix(call, "not ") {
		head, call = "not ", call[len("not "):]
	}
	open := strings.IndexByte(call, '(')
	if open <= 0 || matchBrace(call, open) != len(call)-1 {
		return nil, false
	}
	out := []string{indent + head + call[:open+1]}
	args := splitTop(call[open+1:len(call)-1], ',')
	for i, a := range args {
		a = strings.TrimSpace(a)
		suffix := ","
		if i == len(args)-1 {
			suffix = ""
		}
		if a != "" && a[0] == '{' && matchBrace(a, 0) == len(a)-1 {
			if t, ok := parseTable(a); ok {
				out = append(out, renderTable(t, indent+"  ", "", suffix)...)
				continue
			}
		}
		out = append(out, indent+"  "+a+suffix)
	}
	return append(out, indent+")"), true
}

// topLevelKeyword is the offset of kw (with its surrounding spaces) outside
// strings and brackets, or -1.
func topLevelKeyword(s, kw string) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"', '\'':
			if e := stringEnd(s, i); e >= 0 {
				i = e
			}
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			depth--
		case ' ':
			if depth == 0 && strings.HasPrefix(s[i:], kw) {
				return i
			}
		}
	}
	return -1
}

// splitOperator splits an expression at every top-level occurrence of op.
func splitOperator(s, op string) []string {
	var out []string
	for {
		i := topLevelKeyword(s, op)
		if i < 0 {
			return append(out, s)
		}
		out = append(out, s[:i])
		s = s[i+len(op):]
	}
}

// commentStart is the byte offset of a trailing "--" comment outside any
// string, or -1.
func commentStart(s string) int {
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"' || c == '\'':
			e := stringEnd(s, i)
			if e < 0 {
				return -1
			}
			i = e
		case c == '[' && longBracket(s[i:]) > 0:
			n := longBracket(s[i:])
			end := strings.Index(s[i+n:], "]"+strings.Repeat("=", n-2)+"]")
			if end < 0 {
				return -1
			}
			i += n + end + n - 1
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

// escapeLen is the byte length of the escape sequence at the start of s
// (s[0] == '\\'), so a string piece never ends inside one.
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
