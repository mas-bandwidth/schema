package pythontable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The emitter keeps its own name guards behind the checker's (SPEC §4.6:
// schema check runs every target's whole name check), so a test reaches a
// guard only through a unit renamed after the check, and shows the check
// refuses the same name first.

// checkRefuses requires schema check to refuse src with a diagnostic that
// contains want.
func checkRefuses(t *testing.T, src, want string) {
	t.Helper()
	ast, perrs := parser.Parse("Records.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	_, cerrs := check.Unit([]check.SourceFile{{
		Path: "Records.schema", Name: "Records.schema", Base: "Records",
		Bytes: []byte(src), AST: ast,
	}})
	for _, e := range cerrs {
		if strings.Contains(e.Error(), want) {
			return
		}
	}
	t.Fatalf("schema check did not refuse with %q: %v\n%s", want, cerrs, src)
}

// renameTable renames the unit's table from to the name to, after the check:
// the declaration, the unit's lookups, and every field that names it.
func renameTable(t *testing.T, u *ir.Unit, from, to string) {
	t.Helper()
	s, ok := u.Tables[from]
	if !ok {
		t.Fatalf("renameTable: the unit has no table %s", from)
	}
	s.Name = to
	delete(u.Tables, from)
	u.Tables[to] = s
	if base, ok := u.DeclFile[from]; ok {
		delete(u.DeclFile, from)
		u.DeclFile[to] = base
	}
	for _, owners := range []map[string]*ir.Struct{u.Structs, u.Tables} {
		for _, o := range owners {
			for _, f := range o.Fields {
				if f.Type.Ref == ir.Decl(s) {
					f.Type.Name = to
				}
			}
		}
	}
}

// loadTableRenamingField loads "table Root { placeholder int32 }" and renames
// its one field to name, after the check.
func loadTableRenamingField(t *testing.T, name string) *ir.Unit {
	t.Helper()
	u := loadUnit(t, "package probe\ntable Root { placeholder int32 }\n")
	u.Tables["Root"].Fields[0].Name = name
	return u
}
