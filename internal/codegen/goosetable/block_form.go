// THE BLOCK FORM (docs/SPEC-TABLES.md §19) for Goose: both halves — the
// builder (§19.1) and the reflective read (§19.2) — emitted ON THE SIDE into
// <Base>Block.goose. Nothing declares it: every fixed table has one, and a
// <Base>Table.goose carries not one symbol of it (the zero-cost gate, §19), so
// a consumer that never reads a block imports nothing and compiles nothing for
// it. The shared clauses, the word moves and the descriptor types are
// runtime/block_form.goose.
//
// A BLOCK IS A `u8[:]` VIEW AND AN OFFSET, and a row is reached by stride: a
// block array is (offset_of, count, stride) read OUT OF THE INSTANCE, row i is
// offset_of + i * stride, and no handle is built per row (docs/PORTING.md M7).
// The typed accessors take the view and that offset (<Row>Row<Field>(b, at)),
// and a producer writes a whole TYPED row with <Table>Block<Array>Put(b, i, v);
// the descriptors (BlockRecords, one per unit) carry the same layout as data
// for a consumer that walks any block without a generated name.
//
// The layout is ir.Blocks' and no other (§19.3): every offset, size, pitch and
// maximum below is the compiler's model, which the build version digests, so a
// producer and a consumer of one build agree on every byte by construction and
// a consumer of another build refuses at open.
//
// THE BASE'S ALIGNMENT IS THE CALLER'S `lead` (runtime/block_form.goose): a
// Goose slice has no address a program can measure, so Open and Begin take
// the residue of the buffer's address modulo sixty-four as the caller states
// it, and Open checks it last, as §19.2 orders it (docs/PORTING.md I5).
package goosetable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() {
	RegisterModule("block_form", generateBlockModules)
	RegisterUnit("block_form", "block", emitBlockPackage)
}

// blockRecordKey separates a table's PROJECTION from the same table laid out
// as a ROW (a fixed table can be both: an element of another table's array,
// and a block of its own).
func blockRecordKey(name string, projection bool) string {
	if projection {
		return "projection " + name
	}
	return "row " + name
}

// blockRecordIndex is the unit's block record array: every row record, then
// every projection, each sorted, so an index is a fact of the declaration and
// not of file order.
func blockRecordIndex(blocks *ir.BlockUnit) (keys []string, index map[string]int64, rowIndex func(*ir.Struct) int64) {
	for _, name := range blocks.Order {
		keys = append(keys, blockRecordKey(name, false))
	}
	tables := append([]*ir.BlockLayout(nil), blocks.Tables...)
	sort.Slice(tables, func(i, j int) bool { return tables[i].Table.Name < tables[j].Table.Name })
	for _, bl := range tables {
		keys = append(keys, blockRecordKey(bl.Table.Name, true))
	}
	index = map[string]int64{}
	for i, k := range keys {
		index[k] = int64(i)
	}
	rowIndex = func(st *ir.Struct) int64 {
		if i, ok := index[blockRecordKey(st.Name, false)]; ok {
			return i
		}
		return -1
	}
	return keys, index, rowIndex
}

// emitBlockPackage writes the unit's block data into <package>_block.goose
// (PackageModule; docs/PORTING.md J2): the descriptors, every row record's
// accessors, and each table's projection's place in BlockRecords. It is named
// for the package, so a file that sorts earlier moves no byte of any file's
// <Base>Block.goose.
func emitBlockPackage(g *goose.Gen) error {
	u := g.Unit
	blocks := ir.Blocks(u)
	if blocks == nil || len(blocks.Tables) == 0 {
		return nil
	}
	keys, index, rowIndex := blockRecordIndex(blocks)
	g.Import(accelModule)
	g.Import(cookPackage(u)) // BuildVersion and the row accessors
	emitBlockRecords(g, u, blocks, keys, rowIndex)
	g.Pf("// ---- each table's projection: its place in BlockRecords ----\n")
	tables := append([]*ir.BlockLayout(nil), blocks.Tables...)
	sort.Slice(tables, func(i, j int) bool { return tables[i].Table.Name < tables[j].Table.Name })
	for _, bl := range tables {
		n := bl.Table.Name
		g.Declare(n + "BlockRecord")
		g.Pf("let %sBlockRecord: i64 = %d;\n", n, index[blockRecordKey(n, true)])
	}
	g.Pf("\n")
	return nil
}

// generateBlockModules writes <Base>Block.goose for every file that declares a
// table with a block form: each table's handle. The unit's descriptors and
// every row record's accessors live in <package>_block.goose (emitBlockPackage).
func generateBlockModules(u *ir.Unit, names map[string]string) (map[string][]byte, error) {
	out := map[string][]byte{}
	blocks := ir.Blocks(u)
	if blocks == nil || len(blocks.Tables) == 0 {
		return out, nil
	}
	if err := sideModuleRefusal(u, "Block"); err != nil {
		return nil, err
	}
	for _, f := range u.Files {
		var mine []*ir.BlockLayout
		for _, st := range f.Tables {
			if bl := blocks.Block(st.Name); bl != nil {
				mine = append(mine, bl)
			}
		}
		if len(mine) == 0 {
			continue
		}
		g := goose.NewGen(u, f, names)
		g.Suffix, g.Kind = "Block", "block form"
		g.Import(accelModule)
		g.Import(cookPackage(u)) // BuildVersion
		g.Import(PackageModule(u, "block"))
		for _, bl := range mine {
			emitBlockHandle(g, u, bl)
		}
		if err := g.Err(); err != nil {
			return nil, err
		}
		out[f.Base+"Block.goose"] = g.Assemble()
	}
	return out, nil
}

// emitBlockRecords is the unit's descriptors as data and the row accessors of
// every row record (docs/SPEC-TABLES.md §19.2).
func emitBlockRecords(g *goose.Gen, u *ir.Unit, blocks *ir.BlockUnit, keys []string, rowIndex func(*ir.Struct) int64) {
	rt := goose.RuntimeNamespace
	g.Pf("// ---- the block form's descriptors (docs/SPEC-TABLES.md §8.1, §19.2) ----\n")
	g.Pf("// Every record a block of this unit reaches, AS DATA: the rows and the\n")
	g.Pf("// types they nest, then each table's projection. A consumer holding these\n")
	g.Pf("// reads any block of this build with no generated struct and no name of\n")
	g.Pf("// this unit's spelling. AccelField.record indexes BlockRecords.\n")
	byTable := map[string]*ir.BlockLayout{}
	for _, bl := range blocks.Tables {
		byTable[bl.Table.Name] = bl
	}
	type entry struct {
		name string
		ml   *ir.MemberLayout
		bl   *ir.BlockLayout // non-nil for a projection
	}
	entries := make([]entry, len(keys))
	for i, k := range keys {
		if name, ok := strings.CutPrefix(k, "projection "); ok {
			bl := byTable[name]
			entries[i] = entry{name, &bl.Projection, bl}
		} else {
			name = strings.TrimPrefix(k, "row ")
			entries[i] = entry{name, blocks.Layout(name), nil}
		}
	}
	for i, e := range entries {
		sym := fmt.Sprintf("_BlockFields%d", i)
		g.Declare(sym)
		g.Pf("let %s: %s::AccelField[%d] = [\n", sym, rt, max(len(e.ml.Fields), 1))
		if len(e.ml.Fields) == 0 {
			g.Pf("    %s::AccelField { record: -1, count_offset: -1, present_offset: -1, offset_of_offset: -1, stride_offset: -1, .. },\n", rt)
		}
		for _, fl := range e.ml.Fields {
			if e.bl != nil {
				if a := e.bl.ArrayByName(fl.Field.Name); a != nil {
					g.Pf("    %s,\n", accelField{name: fl.Field.Name, offset: fl.Offset, size: fl.Size, kind: ir.TableScalarKind(fl.Field),
						isArray: true, counted: true, arrayBound: a.Max, countOffset: a.CountOffset, presentOffset: -1,
						outOfLine: true, offsetOfOffset: a.OffsetOfOffset, strideOffset: a.StrideOffset, stride: a.Stride,
						record: rowIndex(a.Elem)}.render())
					continue
				}
			}
			g.Pf("    %s,\n", accelFieldOf(u, fl, e.bl != nil, rowIndex).render())
		}
		g.Pf("];\n")
	}
	g.Declare("BlockRecords")
	g.Pf("let BlockRecords: %s::AccelRecord[%d] = [\n", rt, len(entries))
	for i, e := range entries {
		g.Pf("    %s::AccelRecord { name: %q, size: %d, align: %d, fields: _BlockFields%d[0..%d], .. },\n", rt, e.name, e.ml.Size, e.ml.Align, i, len(e.ml.Fields))
	}
	g.Pf("];\n\n")
	g.Pf("\n")
}

// emitBlockHandle is one table's block: its constants, Open (§19.2), Begin
// (§19.1), BlockBytes, and per out-of-line array the count, the pitch and the
// row offset READ OUT OF THE INSTANCE (M7).
func emitBlockHandle(g *goose.Gen, u *ir.Unit, bl *ir.BlockLayout) {
	rt := goose.RuntimeNamespace
	n := bl.Table.Name
	p := bl.Projection.Size
	decl := func(name string) string {
		g.Declare(name)
		return name
	}
	g.Pf("// ---- %s's block (docs/SPEC-TABLES.md §19) ----\n", n)
	g.Pf("// The storage a producer allocates, sized from the declared maxima (§19.1).\n")
	g.Pf("let %s: i64 = %d;\n", decl(n+"BlockMaxBytes"), bl.MaxBytes)
	g.Pf("let %s: i64 = %d;\n", decl(n+"BlockProjectionBytes"), p)
	for _, a := range bl.Arrays {
		field := ir.GoExportName(a.Field.Name)
		g.Pf("// %s: this build's constants, asserted against, never indexed with (§19.2)\n", a.Field.Name)
		g.Pf("let %s: i64 = %d;\n", decl(n+"Block"+field+"Stride"), a.Stride)
		g.Pf("let %s: i64 = %d;\n", decl(n+"Block"+field+"Max"), a.Max)
	}
	if len(bl.Arrays) > 0 {
		g.Pf("// The counts Begin lays the block out from, gathered before Begin.\n")
		g.Pf("struct %s {\n", decl(n+"BlockCounts"))
		for _, a := range bl.Arrays {
			g.Pf("    %s: i64,\n", ir.GoExportName(a.Field.Name))
		}
		g.Pf("}\n")
	}
	// OPEN
	g.Pf("// %sBlockOpenReason checks once and points, and this is the WHOLE check\n", n)
	g.Pf("// (docs/SPEC-TABLES.md §19.2), first failing clause first: the bytes against\n")
	g.Pf("// the projection, the magic read first, the byte order word, the build\n")
	g.Pf("// version, then per array the pitch, the count against the declared maximum,\n")
	g.Pf("// the offset_of and the extent, then the used extent against the bytes, and\n")
	g.Pf("// LAST the base's alignment — `lead`, the buffer's address modulo sixty-four,\n")
	g.Pf("// below zero for no buffer at all. It answers schema_rt::TableRefuse*.\n")
	g.Pf("fn %s(b: const u8[:], lead: i64) -> i64 {\n", decl(n+"BlockOpenReason"))
	g.Pf("    if lead < 0 { return %s::TableRefuseUnalignedBase; }\n", rt)
	g.Pf("    let head = %s::block_check_head(b, %d, BuildVersion);\n", rt, p)
	g.Pf("    if head != %s::TableRefuseOk { return head; }\n", rt)
	g.Pf("    var used: i64 = %d;\n", p)
	for _, a := range bl.Arrays {
		start := max(int64(ir.BlockAlign), a.ElemAlign())
		g.Pf("    let r%s = %s::block_check_array(b, %d, %d, %d, %d, %d, used); // %s\n",
			ir.GoExportName(a.Field.Name), rt, a.TripleOffset, a.Stride, a.Max, start, p, a.Field.Name)
		g.Pf("    if r%s != %s::TableRefuseOk { return r%s; }\n", ir.GoExportName(a.Field.Name), rt, ir.GoExportName(a.Field.Name))
	}
	g.Pf("    return %s::block_check_tail(b, used, lead);\n", rt)
	g.Pf("}\n")
	g.Pf("fn %s(b: const u8[:], lead: i64) -> bool { return %sBlockOpenReason(b, lead) == %s::TableRefuseOk; }\n", decl(n+"BlockOpen"), n, rt)
	// BEGIN
	g.Pf("// %sBlockBegin lays the block out from the counts (docs/SPEC-TABLES.md\n", n)
	g.Pf("// §19.1): every count is checked against its declared maximum BEFORE any\n")
	g.Pf("// byte changes, and a refusal names the array, the count and the maximum\n")
	g.Pf("// — Begin does not clamp. It writes the prologue and the triples and NOT ONE\n")
	g.Pf("// OTHER BYTE, and answers the used extent, or -1. `b` is the storage, at\n")
	g.Pf("// least %sBlockMaxBytes long; `lead` its base's residue modulo sixty-four.\n", n)
	if len(bl.Arrays) > 0 {
		g.Pf("fn %s(b: u8[:], lead: i64, c: const %sBlockCounts&, refusal: %s::TableBlockRefusal&) -> i64 {\n", decl(n+"BlockBegin"), n, rt)
	} else {
		g.Pf("fn %s(b: u8[:], lead: i64) -> i64 {\n", decl(n+"BlockBegin"))
	}
	g.Pf("    if lead < 0 || lead %% %s::TableBlockAlign != 0 || b.len < %d { return -1; }\n", rt, bl.MaxBytes)
	for _, a := range bl.Arrays {
		field := ir.GoExportName(a.Field.Name)
		g.Pf("    if c.%s < 0 || c.%s > %d {\n", field, field, a.Max)
		g.Pf("        refusal = %s::TableBlockRefusal { array: %q, count: c.%s, maximum: %d };\n", rt, a.Field.Name, field, a.Max)
		g.Pf("        return -1;\n")
		g.Pf("    }\n")
	}
	g.Pf("    %s::block_begin_head(b, BuildVersion);\n", rt)
	g.Pf("    var offset: i64 = %d;\n", p)
	for _, a := range bl.Arrays {
		start := max(int64(ir.BlockAlign), a.ElemAlign())
		g.Pf("    offset = %s::block_begin_array(b, %d, offset, %d, c.%s, %d);\n", rt, a.TripleOffset, start, ir.GoExportName(a.Field.Name), a.Stride)
	}
	g.Pf("    return (offset + %d) / %d * %d;\n", ir.BlockAlign-1, ir.BlockAlign, ir.BlockAlign)
	g.Pf("}\n")
	// BYTES
	g.Pf("// %sBlockBytes is the used extent of an opened block (§19.1): the greatest\n", n)
	g.Pf("// array end read out of the instance, rounded up to sixty-four.\n")
	g.Pf("fn %s(b: const u8[:]) -> i64 {\n", decl(n+"BlockBytes"))
	g.Pf("    var used: i64 = %d;\n", p)
	for _, a := range bl.Arrays {
		g.Pf("    let e%s = (%s::accel_u64(b, %d) + %s::accel_u32(b, %d) * %s::accel_u32(b, %d)) as! i64;\n",
			ir.GoExportName(a.Field.Name), rt, a.OffsetOfOffset, rt, a.CountOffset, rt, a.StrideOffset)
		g.Pf("    if e%s > used { used = e%s; }\n", ir.GoExportName(a.Field.Name), ir.GoExportName(a.Field.Name))
	}
	g.Pf("    return (used + %d) / %d * %d;\n", ir.BlockAlign-1, ir.BlockAlign, ir.BlockAlign)
	g.Pf("}\n")
	// THE ARRAYS, by stride (M7)
	for _, a := range bl.Arrays {
		field := ir.GoExportName(a.Field.Name)
		g.Pf("// %s: the rows the producer filled, at the pitch the INSTANCE gives; row i\n", a.Field.Name)
		g.Pf("// is <%s>At(b, i), one multiply and one add, and no handle per row (M7).\n", field)
		g.Pf("fn %s(b: const u8[:]) -> i64 { return %s::accel_u32(b, %d) as! i64; }\n", decl(n+"Block"+field+"Count"), rt, a.CountOffset)
		g.Pf("fn %s(b: const u8[:]) -> i64 { return %s::accel_u32(b, %d) as! i64; }\n", decl(n+"Block"+field+"Pitch"), rt, a.StrideOffset)
		g.Pf("fn %s(b: const u8[:], i: i64) -> i64 { return (%s::accel_u64(b, %d) as! i64) + i * %s(b); }\n",
			decl(n+"Block"+field+"At"), rt, a.OffsetOfOffset, n+"Block"+field+"Pitch")
		// THE PRODUCER'S TYPED ROW (§19.1): the generated record, written into
		// row i where it lies, field by field at the offsets of the one layout
		// model — a block row IS the record a cook lays out (§19.2), so the
		// the cook package module's <Row>CookPut is the writer, in this build's order
		if ok, _ := cookWritable(u, a.Elem, map[string]bool{}); ok && ir.RecordLayout(u, a.Elem).Size == a.Stride {
			g.Pf("fn %s(b: u8[:], i: i64, v: const %s&) -> bool { return %sCookPut(v, b, %s(b, i), false); }\n",
				decl(n+"Block"+field+"Put"), g.TypeRef(a.Elem.Name, a.Elem.IsTable), a.Elem.Name, n+"Block"+field+"At")
		}
	}
	// the projection's own inline fields
	emitAccessors(g, u, n+"Block", bl.Projection.Fields, true)
	g.Pf("\n")
}
