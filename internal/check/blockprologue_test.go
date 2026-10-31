// THE BLOCK FORM'S GENERATED PROLOGUE (docs/SPEC-TABLES.md §2.7, row T2.7-2;
// §19.1, §11): every fixed table's block projection opens with `magic`,
// `build_version` and `byte_order`, so a table field may not be named after
// any of the three. The claim is on every table, fixed or not, because a table
// gains and loses the block form as its closure gains and loses a pointer.
package check

import (
	"strings"
	"testing"
)

// TestFixedTableFieldNamedAfterTheBlockPrologueIsRefused pins row T2.7-2:
// each of the three prologue names, declared as a field of a fixed table, is
// refused, and the refusal names the table, the field and the prologue. The
// same names on a variable table are refused too, and a fixed table whose fields
// only resemble the names is accepted, so the refusal is the names and not
// the shape of the declaration.
func TestFixedTableFieldNamedAfterTheBlockPrologueIsRefused(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"magic", "build_version", "byte_order"} {
		for _, kind := range []string{"fixed table", "table"} {
			t.Run(kind+" "+name, func(t *testing.T) {
				t.Parallel()
				src := "package t\n" + kind + " Header\n{\n    id uint32\n    " + name + " uint32\n}\n"
				got := refuse(t, src)
				for _, want := range []string{"table Header", "field " + name, "collides with the block form's generated prologue"} {
					if !strings.Contains(got, want) {
						t.Fatalf("%s: the refusal does not say %q:\n%s", src, want, got)
					}
				}
			})
		}
	}
	t.Run("near names are accepted", func(t *testing.T) {
		t.Parallel()
		buildUnit(t, "package t\nfixed table Header\n{\n    magic_number uint32\n    version_build uint32\n    order_byte uint8\n}\n")
	})
}
