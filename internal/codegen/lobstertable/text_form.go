// The hand-editable text form (docs/SPEC-TABLES.md §16.9;
// docs/PORT-LOBSTER.md, THE CARD MAP): runtime/text_form.lobster carries the
// strict s-expression read, the retained-byte save and the leaf edit, and
// this part joins every table module to it through Register.
package lobstertable

import "github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"

func init() { Register("text-form", emitTextForm) }

// emitTextForm joins every table module with the text form's runtime; the
// walk itself lives once, in runtime/text_form.lobster, over the descriptors
// this package emits (docs/PORT-LOBSTER.md, THE RUNTIME LAYOUT).
func emitTextForm(g *lobster.Gen) error {
	g.Import("text_form")
	return nil
}
