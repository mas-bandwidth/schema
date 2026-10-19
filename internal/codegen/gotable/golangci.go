package gotable

import (
	"go/format"
	"strings"
)

// golangciLines is the table emitter's twin of the packet emitter's
// golangciLintDirective: a line-scoped //nolint with its justification, never a
// blanket exclusion. errcheck/gosec's G104 see the sticky-stream calls the
// generated writer/reader ends with stream.Err(); gosec's G115 sees the
// wire-width conversions (range-checked or masked, SPEC §4.3); G103 sees the
// unsafe.Sizeof/Offsetof layout model the table descriptors are defined by
// (docs/SPEC-TABLES.md §8).
// goCommentStart is the index of a line comment outside string and backtick
// literals, or -1. Generated strings carry "//" (URLs, snippets), so a blind
// strings.Index would put the directive inside a literal.
func goCommentStart(line string) int {
	inString, inRaw, escaped := false, false, false
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case inRaw:
			if c == '`' {
				inRaw = false
			}
		case inString:
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
		case c == '"':
			inString = true
		case c == '`':
			inRaw = true
		case c == '/' && i+1 < len(line) && line[i+1] == '/':
			return i
		}
	}
	return -1
}

func golangciLines(text string) string {
	lines := strings.SplitAfter(text, "\n")
	for i, line := range lines {
		body := strings.TrimSuffix(line, "\n")
		var linters, reasons []string
		gosecAdded := false
		if strings.Contains(body, "stream.Serialize") {
			linters = append(linters, "errcheck", "gosec")
			reasons = append(reasons, "the stream latches its error and the generated method returns stream.Err() at its end", "G104 is the same latched error")
			gosecAdded = true
		}
		if golangciTableConversion(body) {
			if !gosecAdded {
				linters = append(linters, "gosec")
			}
			reasons = append(reasons, "G115 wire-width conversions are range-checked or masked (mas-bandwidth/schema SPEC §4.3)")
			gosecAdded = true
		}
		if strings.Contains(body, "unsafe.") {
			if !gosecAdded {
				linters = append(linters, "gosec")
			}
			reasons = append(reasons, "G103 the layout model is unsafe.Sizeof/Offsetof on purpose (mas-bandwidth/schema docs/SPEC-TABLES.md §8)")
			gosecAdded = true
		}
		if len(linters) == 0 || strings.Contains(body, "//nolint:") {
			continue
		}
		directive := "//nolint:" + strings.Join(linters, ",") + " // " + strings.Join(reasons, "; ")
		if j := goCommentStart(body); j >= 0 {
			body = body[:j] + directive + "; //" + strings.TrimPrefix(body[j:], "//")
		} else {
			body += " " + directive
		}
		lines[i] = body
		if strings.HasSuffix(line, "\n") {
			lines[i] += "\n"
		}
	}
	return strings.Join(lines, "")
}

func golangciTableConversion(line string) bool {
	for _, tok := range []string{"int8(", "uint8(", "int16(", "uint16(", "int32(", "uint32(", "int64(", "uint64(", "int(", "byte(", "uintptr("} {
		for idx := 0; ; {
			j := strings.Index(line[idx:], tok)
			if j < 0 {
				break
			}
			pos := idx + j
			if pos == 0 || !isIdentByte(line[pos-1]) {
				return true
			}
			idx = pos + len(tok)
		}
	}
	return false
}

func isIdentByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// formatSource annotates the finished generated source (never a fragment: a
// //nolint appended to half a line would comment out the rest of it), then runs
// go/format over it.
func formatSource(src []byte) ([]byte, error) {
	formatted, err := format.Source(src)
	if err != nil {
		return nil, err
	}
	// Annotate the FORMATTED lines: a minified raw-string block splits across
	// lines here, so a directive appended before formatting would land on the
	// wrong line after gofmt moves the code around.
	return format.Source([]byte(golangciLines(string(formatted))))
}
