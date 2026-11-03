// THE DESCRIPTOR ANNOTATION COLUMNS (docs/SPEC-TABLES.md §8.1) for Goose: what
// a PERSON wrote about a declaration — its `///` doc comment (SPEC §4.1) and
// its tags (SPEC §4.2) — and the declared type's spelling, on every
// schema_rt::FieldDesc and schema_rt::TableDesc the table surface writes.
//
// ABSENCE HAS ONE SPELLING. A row with no doc names schema_rt::TableDocNone,
// the one empty doc the runtime defines; a row with no tags carries num_tags 0
// and leaves its list at the struct's empty default, so no row writes an
// empty literal of its own (§8.7).
//
// A TAG LIST IS A GLOBAL OF ITS OWN, named for its row: _<Decl>Tags for a
// declaration and _<Owner>_t<ordinal> for a field or a union arm. No schema
// name starts with an underscore, and the ordinal keeps two rows whose owner
// and member spellings concatenate to the same text apart.
package goosetable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// annotate adds a row's doc and tags to its literal, in schema_rt's
// declaration order: doc, num_tags, tags. A non-empty tag list is declared as
// the global name before the literal that names it.
func annotate(g *goose.Gen, l *lit, name, doc string, tags []string) {
	l.add("doc", docLit(doc))
	l.add("num_tags", fmt.Sprint(len(tags)))
	if len(tags) > 0 {
		g.Declare(name)
		g.Pf("let %s: (const u8[:])[%d] = [%s];\n", name, len(tags), tagList(tags))
		l.add("tags", name)
	}
}

// annotateField adds a field's or a union arm's §8.1 columns after its wire
// facts: the declared type's spelling, an arm's offset in its union's storage
// (the payload base every arm overlays), then its doc and tags.
func annotateField(g *goose.Gen, l *lit, owner string, f *ir.Field, ordinal, tag int) {
	l.add("type_name", stringLit(ir.TableTypeSpelling(f)))
	if tag > 0 {
		if offset := armOffset(g.Unit, owner); offset != 0 {
			l.add("arm_offset", fmt.Sprint(offset))
		}
	}
	annotate(g, l, tagsName(owner, ordinal), f.Doc, f.Tags)
}

// tagsName is the tag-list global of a field or a union arm.
func tagsName(owner string, ordinal int) string {
	return fmt.Sprintf("_%s_t%d", owner, ordinal)
}

// armOffset is the offset of a union's payload base at the record layout
// (ir.UnionLayout): the tag at 0, then every arm at this one offset.
func armOffset(u *ir.Unit, union string) int64 {
	un := u.Unions[union]
	if un == nil {
		un = u.TableUnions[union]
	}
	if un == nil {
		return 0
	}
	_, _, _, offset := ir.UnionLayout(u, un)
	return offset
}

// docLit is a doc column's value: the one empty doc, or the text as a Goose
// string literal.
func docLit(doc string) string {
	if doc == "" {
		return rt("TableDocNone")
	}
	return stringLit(doc)
}

// tagList is a tag list's elements, each a string literal, in declared order.
func tagList(tags []string) string {
	parts := make([]string, len(tags))
	for i, t := range tags {
		parts[i] = stringLit(t)
	}
	return strings.Join(parts, ", ")
}

// stringLit is s as a Goose string literal, escaped by the language's own
// rule and by no other (the pinned goose_spec.md §2: `\n \t \r \0 \\ \" \'
// \xNN`): a quote, a backslash and the control bytes are escaped, and every
// other byte, UTF-8 included, is written as it is.
func stringLit(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			b.WriteString(`\"`)
		case c == '\\':
			b.WriteString(`\\`)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\t':
			b.WriteString(`\t`)
		case c == '\r':
			b.WriteString(`\r`)
		case c < 0x20 || c == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}
