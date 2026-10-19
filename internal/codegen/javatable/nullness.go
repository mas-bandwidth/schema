package javatable

import (
	"regexp"
	"strings"
)

// A package marker would also apply to the packet compiler's unmarked output.
// Each javaFile has one public type, so mark that type alone (§16).
func nullMarkedTopLevel(body string) string {
	for _, prefix := range []string{"public final class ", "public class ", "public interface ", "public enum "} {
		if strings.HasPrefix(body, prefix) {
			return "@NullMarked\n" + body
		}
		if i := strings.Index(body, "\n"+prefix); i >= 0 {
			return body[:i+1] + "@NullMarked\n" + body[i+1:]
		}
	}
	return body
}

// A generated method that returns the null sentinel has a nullable result.
// Find the declaration from its emitted Java body, not its schema name: this
// covers the runtime, accelerator, and per-table reader/writer methods alike.
// Only a literal null result (or a null arm of a result conditional) qualifies.
var javaMethod = regexp.MustCompile(`(?m)^([ \t]*(?:(?:public|protected|private|static|final|synchronized|native|abstract|strictfp)[ \t]+)*)([A-Za-z_$][A-Za-z0-9_.$<>,? \[\]@]*?)[ \t]+([A-Za-z_$][A-Za-z0-9_$]*)[ \t]*\([^;{}]*\)[ \t]*(?:throws[ \t]+[^{;]+)?\{`)
var javaNullReturn = regexp.MustCompile(`\breturn[ \t\r\n]+null\b|\breturn\b[^;{}]*[?:][ \t\r\n]*null\b`)

func nullableReturns(body string) string {
	code := javaCodeOnly(body)
	matches := javaMethod.FindAllStringSubmatchIndex(code, -1)
	var insertions []int
	for _, m := range matches {
		open := m[1] - 1
		close := matchingJavaBrace(code, open)
		if close < 0 || !javaNullReturn.MatchString(code[open+1:close]) {
			continue
		}
		start := m[4] // the return type, after Java modifiers
		if strings.HasPrefix(body[start:], "@Nullable ") {
			continue
		}
		insertions = append(insertions, start)
	}
	for i := len(insertions) - 1; i >= 0; i-- {
		at := insertions[i]
		body = body[:at] + "@Nullable " + body[at:]
	}
	return body
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
