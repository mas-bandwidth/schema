// THE BLOCK FORM in Squirrel (docs/SPEC-TABLES.md §19.1): the BUILDER, the
// producer half the READ side's block.go does not carry. The runtime is
// runtime/blockbuild.nut (schema_table_blockbuild.nut beside the module).
//
// THE BUILDER IS SINGLE-THREADED. §19.1 makes the multi-threaded fill an
// obligation on a HOST-language implementation — a Go or C++ producer hands N
// workers disjoint index ranges of the row storage. Squirrel has one thread,
// so that property is NOT CARRIED here: this builder states the layout and
// hands back the row storage, and the fill is the caller's serial loop. What
// §19.1 obliges of the fill PATH is carried: Begin asks the caller's allocator
// exactly once, at create time, and Begin, the accessors and the writers ask
// for nothing.
//
// STORAGE IS THE CALLER'S, asked for EXACTLY ONCE (§19.1), and the base is the
// caller's named lead into it — the same analogue the cook and the block reader
// use, because a Squirrel blob has no address. The default allocator is the
// standard blob class; the default base is 0.
package squirreltable

import (
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() {
	RegisterSurface(Surface{Name: "blockbuild", Module: blockBuildModule})
}

// blockBuildModule emits the builder's entry points once per module
// (docs/SPEC-TABLES.md §19.1). There is no per-table emission: every fact the
// builder needs — the projection, each array's triple offsets, the row
// records — is already in the module's blockForms and blockRecords.
func blockBuildModule(m *Module) {
	m.Runtime("blockbuild")
	m.Printf("// THE BLOCK FORM (docs/SPEC-TABLES.md §19.1): the BUILDER. Create asks the\n")
	m.Printf("// caller's allocator EXACTLY ONCE; Begin writes the prologue and the array\n")
	m.Printf("// triples and touches no row; blockRows hands back the row storage and\n")
	m.Printf("// blockSet writes a field through the same descriptors the reader walks.\n")
	m.Printf("// The multi-threaded fill §19.1 obliges of a host-language producer is NOT\n")
	m.Printf("// CARRIED: this leg is single-threaded, and the caller's fill is serial.\n")
	// The read descriptors in block.go carry no keyed mark (that file is the
	// reader's, not this builder's). An enum-keyed array's path index is the
	// enum's ordinal, dense from 1 (§2.4), while its slot is zero-based, so the
	// builder emits the keyed marks it needs here rather than widening the
	// reader's records.
	m.Printf("_M.blockKeyed <- {\n")
	blocks := ir.Blocks(m.Unit)
	if blocks != nil {
		for _, name := range blocks.Order {
			ml := blocks.Layout(name)
			if ml == nil {
				continue
			}
			wrote := false
			for _, fl := range ml.Fields {
				if fl.Field.KeyEnum == "" {
					continue
				}
				if !wrote {
					m.Printf("  %s = { ", squirrel.TableKey(name))
					wrote = true
				}
				m.Printf("%s = true, ", squirrel.TableKey(fl.Field.Name))
			}
			if wrote {
				m.Printf("},\n")
			}
		}
		for _, bl := range blocks.Tables {
			wrote := false
			for _, fl := range bl.Projection.Fields {
				if fl.Field.KeyEnum == "" {
					continue
				}
				if !wrote {
					m.Printf("  %s = { ", squirrel.TableKey(bl.Table.Name))
					wrote = true
				}
				m.Printf("%s = true, ", squirrel.TableKey(fl.Field.Name))
			}
			if wrote {
				m.Printf("},\n")
			}
		}
	}
	m.Printf("};\n")
	m.Printf("_M.blockCreate <- function(root, alloc = null, free = null, lead = 0) { return _rt_blockbuild.Create(_M, root, alloc, free, lead); };\n")
	m.Printf("_M.blockDestroy <- function(storage) { return _rt_blockbuild.Destroy(_M, storage); };\n")
	m.Printf("_M.blockBegin <- function(root, storage, counts, refusal = null) { return _rt_blockbuild.Begin(_M, root, storage, counts, refusal); };\n")
	m.Printf("_M.blockBytes <- function(block) { return _rt_blockbuild.Bytes(_M, block); };\n")
	m.Printf("_M.blockRows <- function(block, name) { return _rt_blockbuild.Rows(_M, block, name); };\n")
	m.Printf("_M.blockSet <- function(block, record, at, path, value) { return _rt_blockbuild.Set(_M, block, record, at, path, value); };\n\n")
}
