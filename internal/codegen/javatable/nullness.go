package javatable

import (
	"regexp"
	"strconv"
	"strings"
)

// A package marker would also apply to the packet compiler's unmarked output.
// Each javaFile has one public type, so mark that type alone (§16).
func nullMarkedTopLevel(body string) string {
	if m := javaTopLevelType.FindStringIndex(body); m != nil {
		return body[:m[0]] + "@NullMarked\n" + body[m[0]:]
	}
	return body
}

// javaTopLevelType is the first top-level public type declaration: a class,
// interface, enum or record, at column 0.
var javaTopLevelType = regexp.MustCompile(`(?m)^public (?:(?:final|abstract|sealed|non-sealed|strictfp) )*(?:class|interface|enum|record) `)

// A generated method that returns the null sentinel has a nullable result.
// Find the declaration from its emitted Java body, not its schema name: this
// covers the runtime, accelerator, and per-table reader/writer methods alike.
// Only a literal null result (or a null arm of a result conditional) qualifies.
var javaMethod = regexp.MustCompile(`(?m)^([ \t]*(?:(?:public|protected|private|static|final|synchronized|native|abstract|strictfp)[ \t]+)*)([A-Za-z_$][A-Za-z0-9_.$<>,? \[\]@]*?)[ \t]+([A-Za-z_$][A-Za-z0-9_$]*)[ \t]*\([^;{}]*\)[ \t]*(?:throws[ \t]+[^{;]+)?\{`)
var javaNullReturn = regexp.MustCompile(`\breturn[ \t\r\n]+null\b|\breturn\b[^;{}]*[?:][ \t\r\n]*null\b`)

func nullableReturns(body string) string {
	for {
		next := nullableReturnsPass(body)
		if next == body {
			return body
		}
		body = next
	}
}

// javaReturnCall is a return of one plain call, "return open(data, 0, n);".
var javaReturnCall = regexp.MustCompile(`\breturn[ \t\r\n]+([A-Za-z_$][A-Za-z0-9_$]*)[ \t]*\(`)

// nullableReturnsPass annotates, once, every method whose result is the null
// sentinel: a literal null result, or the result of a call to a method of the
// same file, by name and arity, that is already nullable.
func nullableReturnsPass(body string) string {
	code := javaCodeOnly(body)
	matches := javaMethod.FindAllStringSubmatchIndex(code, -1)
	nullable := map[string]bool{}
	for _, m := range matches {
		if javaStatementWord(code[m[4]:m[5]]) || javaStatementWord(code[m[6]:m[7]]) {
			continue
		}
		if strings.Contains(body[m[4]:m[5]], "@Nullable") {
			nullable[javaMethodKey(code, m)] = true
		}
	}
	var insertions [][2]int
	for _, m := range matches {
		open := m[1] - 1
		close := matchingJavaBrace(code, open)
		if close < 0 {
			continue
		}
		start, end := m[4], m[5] // the return type, after Java modifiers
		if javaStatementWord(code[start:end]) || javaStatementWord(code[m[6]:m[7]]) {
			// "else if (...) {" and "for (...) {" match the shape of a
			// declaration; a statement is never a method.
			continue
		}
		start += javaTypeStart(code[start:end])
		if strings.Contains(body[start:end], "@Nullable") || javaPrimitive(code[start:end]) {
			continue
		}
		inner := code[open+1 : close]
		if !javaNullReturn.MatchString(inner) && !returnsNullableCall(inner, nullable) {
			continue
		}
		insertions = append(insertions, [2]int{start, end})
	}
	for i := len(insertions) - 1; i >= 0; i-- {
		at := insertions[i]
		body = body[:at[0]] + javaNullableType(body[at[0]:at[1]]) + body[at[1]:]
	}
	return body
}

// javaMethodKey names a declaration by its name and parameter count.
func javaMethodKey(code string, m []int) string {
	name := code[m[6]:m[7]]
	open := strings.Index(code[m[7]:], "(") + m[7]
	return name + "/" + javaArity(code[open+1:])
}

// returnsNullableCall reports a "return f(...)" of a nullable f in body.
func returnsNullableCall(body string, nullable map[string]bool) bool {
	for _, m := range javaReturnCall.FindAllStringSubmatchIndex(body, -1) {
		name := body[m[2]:m[3]]
		if nullable[name+"/"+javaArity(body[m[1]:])] {
			return true
		}
	}
	return false
}

// javaArity counts the top-level arguments of the list that s opens (s starts
// just after the opening parenthesis), as a decimal string.
func javaArity(s string) string {
	depth, n, any := 0, 0, false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '(', '[', '{', '<':
			depth++
			any = true
		case ')', ']', '}', '>':
			if depth == 0 {
				if any {
					n++
				}
				return strconv.Itoa(n)
			}
			depth--
		case ',':
			if depth == 0 {
				n++
			}
		case ' ', '\t', '\r', '\n':
		default:
			any = true
		}
	}
	return "?"
}

// Keep braces and keywords in Java code while blanking comments and literals.
// Their braces and the word "return" are not executable method structure.
func javaCodeOnly(s string) string {
	b := []byte(s)
	const (
		code = iota
		lineComment
		blockComment
		stringLiteral
		charLiteral
	)
	state := code
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch state {
		case code:
			switch {
			case c == '/' && i+1 < len(b) && b[i+1] == '/':
				b[i], b[i+1] = ' ', ' '
				i++
				state = lineComment
			case c == '/' && i+1 < len(b) && b[i+1] == '*':
				b[i], b[i+1] = ' ', ' '
				i++
				state = blockComment
			case c == '"':
				b[i] = ' '
				state = stringLiteral
			case c == '\'':
				b[i] = ' '
				state = charLiteral
			}
		case lineComment:
			if c == '\n' {
				state = code
			} else {
				b[i] = ' '
			}
		case blockComment:
			if c == '*' && i+1 < len(b) && b[i+1] == '/' {
				b[i], b[i+1] = ' ', ' '
				i++
				state = code
			} else if c != '\n' {
				b[i] = ' '
			}
		case stringLiteral, charLiteral:
			if c == '\\' && i+1 < len(b) {
				b[i] = ' '
				i++
				if b[i] != '\n' {
					b[i] = ' '
				}
			} else if (state == stringLiteral && c == '"') || (state == charLiteral && c == '\'') {
				b[i] = ' '
				state = code
			} else if c != '\n' {
				b[i] = ' '
			}
		}
	}
	return string(b)
}

func matchingJavaBrace(code string, open int) int {
	depth := 0
	for i := open; i < len(code); i++ {
		switch code[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// javaStatementWord reports a Java keyword that opens or continues a statement,
// so a regexp match over "else if (x) {" is not taken for a declaration.
func javaStatementWord(s string) bool {
	f := strings.Fields(s)
	if len(f) == 0 {
		return false
	}
	switch f[len(f)-1] {
	case "if", "else", "for", "while", "do", "switch", "catch", "try", "synchronized",
		"return", "new", "throw", "case", "default", "finally":
		return true
	}
	return false
}

// javaSimpleNameOffset is the offset of the simple name in a possibly
// qualified type, ignoring dots inside type arguments.
func javaSimpleNameOffset(t string) int {
	depth, at := 0, 0
	for i := 0; i < len(t); i++ {
		switch t[i] {
		case '<':
			depth++
		case '>':
			depth--
		case '.':
			if depth == 0 {
				at = i + 1
			}
		}
	}
	return at
}

// javaNullableType annotates the outermost type of a Java type expression by
// the JSpecify type-use rules (JLS 9.7.4): byte @Nullable [] for an array,
// Ship.@Nullable Value for a qualified name, @Nullable T otherwise.
func javaNullableType(t string) string {
	if i := javaTopLevelBrackets(t); i >= 0 {
		return strings.TrimRight(t[:i], " ") + " @Nullable " + t[i:]
	}
	at := javaSimpleNameOffset(t)
	return t[:at] + "@Nullable " + t[at:]
}

// javaTopLevelBrackets is the offset of the first "[]" outside type arguments,
// or -1: List<byte[]> is not an array type.
func javaTopLevelBrackets(t string) int {
	depth := 0
	for i := 0; i < len(t); i++ {
		switch t[i] {
		case '<':
			depth++
		case '>':
			depth--
		case '[':
			if depth == 0 && strings.HasPrefix(t[i:], "[]") {
				return i
			}
		}
	}
	return -1
}

// javaTypeStart skips the modifiers and the type-parameter list the method
// regexp can take into its return-type group ("static <T> T").
func javaTypeStart(t string) int {
	i := 0
	for {
		for i < len(t) && (t[i] == ' ' || t[i] == '\t') {
			i++
		}
		if i < len(t) && t[i] == '<' {
			depth := 0
			for ; i < len(t); i++ {
				if t[i] == '<' {
					depth++
				} else if t[i] == '>' {
					depth--
					if depth == 0 {
						i++
						break
					}
				}
			}
			continue
		}
		word := i
		for word < len(t) && t[word] != ' ' && t[word] != '\t' {
			word++
		}
		switch t[i:word] {
		case "public", "protected", "private", "static", "final", "synchronized", "native", "abstract", "strictfp":
			i = word
			continue
		}
		return i
	}
}

// javaPrimitive reports a primitive or void type, which is never nullable.
func javaPrimitive(t string) bool {
	switch strings.TrimSpace(t) {
	case "void", "boolean", "byte", "short", "int", "long", "char", "float", "double":
		return true
	}
	return false
}
