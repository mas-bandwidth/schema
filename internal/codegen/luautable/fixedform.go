// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3, in Luau: the
// emitter lays each fixed root's layout, hash and record size down as data in
// its <Base>Table.luau module, and the runtime (runtime/schema_fixed.luau) reads and
// writes the form driven by that data and the field descriptors of
// luautable.go. The layout arithmetic and the hash are the shared ir walk's
// (docs/FIXED-FORM-ALGORITHM.md §1, §5.2), so two writers whose blocks agree
// byte for byte agree on every id, kind, size and position in the closure.
package luautable

import (
	"math/big"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() {
	Register("fixedform", emitFixedForm)
}

// emitFixedForm joins the fixed form (form byte 3) to every table module: for
// each root the unit declares in THIS file, one block of static data — the
// wire hash, the record body's byte size, and the layout as emitted by the
// shared walk, then the root's lineage and floor (fixedlineage.go). The
// runtime builds its plans from this data and the descriptors
// (docs/FIXED-FORM-ALGORITHM.md §4, §5.2).
func emitFixedForm(g *luau.Gen) error {
	for _, t := range ir.TableFixedFormRoots(g.Unit) {
		if g.Unit.DeclFile[t.Name] != g.File.Base {
			continue
		}
		emitRootData(g, t)
	}
	return nil
}

// emitRootData writes one root's fixed-form data block.
func emitRootData(g *luau.Gen, root *ir.Struct) {
	entries := ir.TableFixedWalkRoot(root)
	layout := ir.TableFixedLayoutBytes(entries)
	hash := ir.TableFixedLayoutHash(layout, root)
	record := ir.TableFixedTypeBytes(root)
	name := root.Name
	g.Pf("-- %s: fixed form, form byte 3 (docs/SPEC-TABLES.md §3.4)\n", name)
	g.Pf("%s.%sFixedHash = %s\n", g.Self, name, luau.IntegerLit(new(big.Int).SetUint64(hash)))
	g.Pf("%s.%sFixedRecordBytes = %d\n", g.Self, name, record)
	g.Pf("%s.%sFixedLayout = %s.fixed_layout({\n", g.Self, name, g.Require("schema_fixed"))
	for _, e := range entries {
		g.Pf("\t{ id = %s, kind = %d, size = %d, children = %d }, -- %s\n",
			luau.IntegerLit(new(big.Int).SetUint64(e.ID)), e.Kind, e.Size, e.Children, e.Note)
	}
	g.Pf("})\n\n")
	emitLineage(g, root, FixedLineageEntry{Wire: hash, Layout: layout, Record: fixedHashBytes + record})
}
