package gotable

import (
	"fmt"
	"strings"
)

// Runtime specialization must fail closed if its source seam moves. A missing
// replacement otherwise emits a valid-looking codec with a different contract.
func tableSourceIndex(source, marker string) int {
	if count := strings.Count(source, marker); count != 1 {
		panic(fmt.Sprintf("Go table source seam %q occurs %d times, want one", marker, count))
	}
	return strings.Index(source, marker)
}
func tableSourceSpan(source, start, end string) string {
	a, b := tableSourceIndex(source, start), tableSourceIndex(source, end)
	if b <= a {
		panic(fmt.Sprintf("Go table source seam %q precedes %q", end, start))
	}
	return source[a:b]
}
func tableSourceFunction(source, marker string) string {
	start := tableSourceIndex(source, marker)
	end := strings.Index(source[start:], "\n}\n")
	if end < 0 {
		panic(fmt.Sprintf("Go table source seam %q has no function end", marker))
	}
	return source[start : start+end+3]
}
func tableSourceReplace(source, old, replacement string, count int) string {
	if got := strings.Count(source, old); got == 0 || count >= 0 && got != count {
		panic(fmt.Sprintf("Go table source seam %q occurs %d times, want %d", old, got, count))
	}
	return strings.Replace(source, old, replacement, count)
}
func tableSourceRewriter(source string, replacements ...string) string {
	if len(replacements)%2 != 0 {
		panic("Go table source rewrite has an unmatched replacement")
	}
	for i := 0; i < len(replacements); i += 2 {
		source = tableSourceReplace(source, replacements[i], replacements[i+1], -1)
	}
	return source
}
