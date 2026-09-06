package ir

import (
	"strconv"
	"strings"
)

// DocLines is a doc comment's text as the lines a target writes above the
// item as LINE comments (SPEC §4.1): each source line contributes one, an
// empty line in the text contributing an empty comment line. Nil when the
// item carries no doc comment, so an unannotated declaration gains no line.
func DocLines(doc string) []string {
	if doc == "" {
		return nil
	}
	return strings.Split(doc, "\n")
}

// DocComment renders a doc comment as line comments for a target: marker is
// the target's line-comment opener ("//", "///", "#") and indent the item's
// indentation. The empty string when there is no doc comment.
func DocComment(doc, indent, marker string) string {
	var b strings.Builder
	for _, l := range DocLines(doc) {
		b.WriteString(indent)
		b.WriteString(marker)
		if l != "" {
			b.WriteString(" ")
			b.WriteString(l)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// QuoteDoc renders a doc comment as a double-quoted string literal under the
// C family's escape rule (SPEC §4.1): a quote, a backslash and the newline
// that joins the lines are what it covers, and printable text passes as
// written. C, C++, C#, Go, Java, JavaScript and Rust all read it.
func QuoteDoc(doc string) string { return strconv.Quote(doc) }

// QuoteDocDart renders a doc comment as a single-quoted Dart string literal:
// a backslash, a quote, the interpolation sigil and a newline are escaped.
func QuoteDocDart(doc string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`, `$`, `\$`, "\n", `\n`, "\r", `\r`, "\t", `\t`)
	return "'" + r.Replace(doc) + "'"
}

// QuoteDocElixir renders a doc comment as a double-quoted Elixir string
// literal: a backslash, a quote, the interpolation opener and a newline are
// escaped.
func QuoteDocElixir(doc string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `#{`, `\#{`, "\n", `\n`, "\r", `\r`, "\t", `\t`)
	return `"` + r.Replace(doc) + `"`
}

// QuotedTags renders a tag list as quoted literals joined by ", " under the
// C family's rule. The empty string for no tags.
func QuotedTags(tags []string) string {
	q := make([]string, len(tags))
	for i, t := range tags {
		q[i] = strconv.Quote(t)
	}
	return strings.Join(q, ", ")
}
