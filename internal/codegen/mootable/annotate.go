package mootable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE DESCRIPTOR ANNOTATION COLUMNS (docs/SPEC-TABLES.md §8.1) for moo: what a
// PERSON wrote about a declaration — its `///` doc comment (SPEC §4.1) and its
// tags (SPEC §4.2) — and the declared type's spelling, on every field, union
// arm and table descriptor the table surface writes.
//
//   - type_name is ir.TableTypeSpelling of the field or arm;
//   - arm_offset is a union arm's offset in the union's storage at the record
//     layout (ir.UnionLayout): the tag at 0, then every arm at the one payload
//     base (§7.2), so the distance between any two arms is zero;
//   - doc is the `///` block verbatim, and ABSENCE HAS ONE SPELLING:
//     St.DocNone, the one empty doc schema_tables.moo defines;
//   - tags is the list in DECLARED order, and absent (nil) when there are
//     none: a moo list carries its own length, so no count column rides
//     beside it.

// annotateField adds a field's or a union arm's §8.1 columns to its rendered
// descriptor d. armOffset is 0 on a field, which carries no arm_offset.
func annotateField(g *Gen, d string, f *ir.Field, doc string, tags []string, armOffset int64) string {
	var b strings.Builder
	b.WriteString(strings.TrimSuffix(d, " }"))
	fmt.Fprintf(&b, ", type_name = %s", textLit(ir.TableTypeSpelling(f)))
	if armOffset != 0 {
		fmt.Fprintf(&b, ", arm_offset = %d", armOffset)
	}
	b.WriteString(", " + annotation(doc, tags) + " }")
	return b.String()
}

// annotation is a row's doc and its tags, as descriptor fields.
func annotation(doc string, tags []string) string {
	d := "doc = St.DocNone"
	if doc != "" {
		d = "doc = " + textLit(doc)
	}
	if len(tags) == 0 {
		return d
	}
	return d + ", tags = " + tagList(tags)
}

// tagList is a tag list as a moo array of string literals, in declared order.
func tagList(tags []string) string {
	parts := make([]string, len(tags))
	for i, t := range tags {
		parts[i] = textLit(t)
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

// armOffset is the offset of a union's payload base at the record layout
// (ir.UnionLayout): the tag at 0, then every arm at this one offset.
func armOffset(u *ir.Unit, un *ir.Union) int64 {
	_, _, _, offset := ir.UnionLayout(u, un)
	return offset
}

// textLit is s as a moo string literal, escaped by the language's own rule
// (SPEC-TABLES §8.1): printable ASCII is written as it is, a quote and a
// backslash are escaped, and every other byte, UTF-8 included, is \xNN, so the
// literal holds exactly the bytes the author wrote.
func textLit(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c < 0x20 || c >= 0x7f:
			fmt.Fprintf(&b, `\x%02x`, c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}
