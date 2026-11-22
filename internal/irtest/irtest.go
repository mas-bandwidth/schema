// Package irtest holds helpers for tests that hand a backend a unit the
// checker refuses. A backend keeps its own name guards behind the checker's
// (SPEC §4.6: a basename that hides a runtime module, or two basenames
// differing only by case, is refused at check for every target), and its
// tests reach those guards only through a unit renamed after the check.
package irtest

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Rebase renames the unit's file whose basename is from to the basename to,
// after the check, as if the checker had accepted that name: the file's Base
// and Path, the unit's declaration-to-file map, and the Files order (sorted
// by basename, as the loader sorts its paths). The test fails when the unit
// has no file named from.
func Rebase(t testing.TB, u *ir.Unit, from, to string) {
	t.Helper()
	i := slices.IndexFunc(u.Files, func(f *ir.File) bool { return f.Base == from })
	if i < 0 {
		t.Fatalf("irtest.Rebase: the unit has no file %s.schema", from)
	}
	f := u.Files[i]
	f.Base = to
	f.Path = filepath.Join(filepath.Dir(f.Path), to+".schema")
	for name, base := range u.DeclFile {
		if base == from {
			u.DeclFile[name] = to
		}
	}
	slices.SortStableFunc(u.Files, func(a, b *ir.File) int { return strings.Compare(a.Base, b.Base) })
}

// RenameStruct renames the unit's packet type from to the name to, after the
// check, as if the checker had accepted that name: the declaration, the
// unit's lookups, and every field of the unit's types and tables that names
// it. The test fails when the unit has no packet type named from.
func RenameStruct(t testing.TB, u *ir.Unit, from, to string) {
	t.Helper()
	s, ok := u.Structs[from]
	if !ok {
		t.Fatalf("irtest.RenameStruct: the unit has no type %s", from)
	}
	s.Name = to
	delete(u.Structs, from)
	u.Structs[to] = s
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
