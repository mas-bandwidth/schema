package java

import (
	"strings"

	"github.com/mas-bandwidth/schema/v3/ir"
)

// javaBackslashEscape is the Unicode escape of a backslash (U+005C). javac
// reads it as one backslash and never translates the character it produces
// again (JLS §3.3). Spelled in two pieces so no tool reading this file sees an
// escape in it.
const javaBackslashEscape = "\\" + "u005c"

// javaDocComment renders a doc comment as Java line comments (SPEC §4.1)
// with no Unicode escape javac would translate (JAVA-1). javac translates
// Unicode escapes before it reads comments, so doc text holding a backslash-u
// sequence could end the comment and become code, or fail the build as a
// malformed escape. A backslash is eligible to begin an escape when an even
// number of backslashes precede it and a 'u' follows it; each eligible one is
// written as javaBackslashEscape, which javac reads back as the backslash
// itself. Every other byte passes as written, so the comment reads as the doc
// does.
func javaDocComment(doc, indent string) string {
	return ir.DocComment(javaCommentText(doc), indent, "//")
}

// javaCommentText is doc with every eligible Unicode-escape backslash
// written as javaBackslashEscape.
func javaCommentText(doc string) string {
	if !strings.Contains(doc, "\\") {
		return doc
	}
	var b strings.Builder
	run := 0
	for i := 0; i < len(doc); i++ {
		c := doc[i]
		if c != '\\' {
			run = 0
			b.WriteByte(c)
			continue
		}
		if run%2 == 0 && i+1 < len(doc) && doc[i+1] == 'u' {
			b.WriteString(javaBackslashEscape)
		} else {
			b.WriteByte(c)
		}
		run++
	}
	return b.String()
}
