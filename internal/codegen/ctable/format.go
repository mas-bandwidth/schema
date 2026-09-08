package ctable

import (
	"regexp"
	"strings"
)

// separateCStatements keeps an unbraced guard and the following statement
// on different lines. GCC diagnoses compact `if (x) return; next();` lines
// as misleading indentation. Only whitespace between C tokens changes;
// literals, comments, for headers and preprocessor directives are preserved.
func separateCStatements(source string) string {
	var out strings.Builder
	paren, line := 0, 0
	for i := 0; i < len(source); {
		if i == line {
			first := i
			for first < len(source) && (source[first] == ' ' || source[first] == '\t') {
				first++
			}
			if first < len(source) && source[first] == '#' {
				end := first
				for end < len(source) {
					if source[end] == '\n' && (end == 0 || source[end-1] != '\\') {
						end++
						break
					}
					end++
				}
				out.WriteString(source[i:end])
				i, line = end, end
				continue
			}
		}
		if end := retainedLiteralEnd(source, i); end > i {
			out.WriteString(source[i:end])
			if last := strings.LastIndexByte(source[i:end], '\n'); last >= 0 {
				line = i + last + 1
			}
			i = end
			continue
		}
		ch := source[i]
		out.WriteByte(ch)
		switch ch {
		case '(':
			paren++
		case ')':
			paren--
		case '\n':
			line = i + 1
		case ';':
			if paren == 0 {
				next := i + 1
				for next < len(source) && (source[next] == ' ' || source[next] == '\t') {
					next++
				}
				if next < len(source) && source[next] != '\n' && source[next] != '\r' {
					indent := line
					for indent < len(source) && (source[indent] == ' ' || source[indent] == '\t') {
						indent++
					}
					out.WriteByte('\n')
					out.WriteString(source[line:indent])
					i = next
					continue
				}
			}
		}
		i++
	}
	return out.String()
}

// The compact message and retention families are the new emitters that need
// this layout. Keep established table and JSON source pins unchanged.
var compactCFunction = regexp.MustCompile(`(?m)^[ \t]*static[^\n;{}]*\b[A-Za-z_0-9]*(?:message|retain|announce)[A-Za-z_0-9]*[ \t]*\([^;{}]*\)[ \t\r\n]*\{`)

func formatCompactCFunctions(source string) string {
	var out strings.Builder
	start := 0
	for start < len(source) {
		match := compactCFunction.FindStringIndex(source[start:])
		if match == nil {
			out.WriteString(source[start:])
			break
		}
		begin, at := start+match[0], start+match[1]
		out.WriteString(source[start:begin])
		depth := 1
		for at < len(source) && depth > 0 {
			if end := retainedLiteralEnd(source, at); end > at {
				at = end
				continue
			}
			if source[at] == '{' {
				depth++
			}
			if source[at] == '}' {
				depth--
			}
			at++
		}
		out.WriteString(separateCStatements(source[begin:at]))
		start = at
	}
	return out.String()
}
