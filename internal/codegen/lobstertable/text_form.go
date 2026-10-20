// The hand-editable text form (docs/SPEC-TABLES.md §16.9;
// docs/PORT-LOBSTER.md, THE CARD MAP): runtime/text_form.lobster carries the
// strict s-expression read, the retained-byte save and the leaf edit, and
// this part joins every table module to it through Register.
package lobstertable

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() { Register("text-form", emitTextForm) }

// emitTextForm joins every table module with the text form's runtime; the
// walk itself lives once, in runtime/text_form.lobster, over the descriptors
// this package emits (docs/PORT-LOBSTER.md, THE RUNTIME LAYOUT).
//
// Two facts the descriptors do not carry are registered here, from the IR:
// a flags field's variant names (its shape is an int, §16.9.2) and an
// enum-keyed array's key variants (the load's own registry, §16.9.2).
func emitTextForm(g *lobster.Gen) error {
	g.Import("text_form")
	var seen = map[string]bool{}
	emit := func(st *ir.Struct) {
		if st == nil || seen[st.Name] {
			return
		}
		seen[st.Name] = true
		for _, f := range st.Fields {
			key := lobster.StringLit([]byte(st.Name)) + ", " + lobster.StringLit([]byte(f.Name))
			if f.KeyEnumRef != nil {
				var vs []string
				for i := range f.KeyEnumRef.Variants {
					vs = append(vs, fmt.Sprintf("schema_rt.EnumVariant { name: %s, id: %s }",
						lobster.StringLit([]byte(f.KeyEnumRef.Variants[i])),
						lobster.IntLit(new(big.Int).SetUint64(ir.TableWireId(f.KeyEnumRef.VariantWireName(i))))))
				}
				g.Pf("schema_rt.keyed_keys(%s, [%s]::schema_rt.EnumVariant)\n", key, strings.Join(vs, ", "))
			}
			if fl, ok := f.Type.Ref.(*ir.Flags); ok && f.Type.Kind == ir.TNamed {
				var vs []string
				for _, v := range fl.Variants {
					vs = append(vs, lobster.StringLit([]byte(v)))
				}
				g.Pf("schema_rt.text_flags(%s, [%s]::string)\n", key, strings.Join(vs, ", "))
			}
		}
	}
	for _, t := range g.File.Tables {
		emit(t)
	}
	for _, d := range g.File.Decls {
		if st, ok := d.(*ir.Struct); ok {
			emit(st)
		}
	}
	return nil
}
