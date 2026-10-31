// THE COOKED FORM (docs/SPEC-TABLES.md §7) for Goose: the read side — every
// table's <Name>CookOpen, which opens a cook in O(1) (docs/PORTING.md M8) — and
// the unit's cook records as data, emitted ON THE SIDE into <Base>Cook.goose
// so a <Base>Table.goose carries none of it. The header check is
// runtime/cook_form.goose's cook_check, and a generated Open is that one
// enumeration plus its own root's two layout facts, exactly as the C and C++
// legs spell it (internal/codegen/ctable/cook.go).
//
// THE COOK'S PACKAGE MODULE, <package>_cook.goose (PackageModule; docs/PORTING.md
// J2), carries what the unit has once: BuildVersion (§20), the cook records
// (CookRecords: every record of the table closure at the layout
// ir.RecordLayout computes, the one model the build version digests), each
// table's place in them (<Name>CookRecord), and every closure record's typed
// accessors, <Row>Row<Field>(b, at) — a cooked record IS the blittable row the
// block form reads (§19.2), so one set of accessors serves both accelerators,
// and the block modules import it. It is named for the package, so a file
// that sorts earlier moves no byte of any file's <Base>Cook.goose.
//
// A REGION IS A VIEW: schema_rt::cook_region gives an opened cook's data part,
// the root at its offset zero, and a pointer slot is followed by one add
// (schema_rt::cook_deref), bounded against the region.
//
// The WRITE side (Cook, CookMeasure, §7.6) is cook_write.go's: every fixed
// root's write from its TYPED record, and the typed copy out of a load.
package goosetable

import (
	"fmt"
	"sort"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cookModule is the cook's runtime module.
const cookModule = "cook_form"

func init() {
	RegisterModule("cook_form", generateCookModules)
	RegisterUnit("cook_form", "cook", emitCookPackage)
}

// cookPackage is the unit's cook package module, <package>_cook, or "" for a
// unit that declares no table and so has no cook.
func cookPackage(u *ir.Unit) string {
	for _, f := range u.Files {
		if len(f.Tables) > 0 {
			return PackageModule(u, "cook")
		}
	}
	return ""
}

// emitCookPackage writes the unit's cook data into <package>_cook.goose: the
// build version, the records, the accessors and the record writers, then each
// table's place in the records.
func emitCookPackage(g *goose.Gen) error {
	u := g.Unit
	if cookPackage(u) == "" {
		return nil
	}
	records := cookRecords(u)
	index := map[string]int64{}
	for i, st := range records {
		index[st.Name] = int64(i)
	}
	recordIndex := func(st *ir.Struct) int64 {
		if i, ok := index[st.Name]; ok {
			return i
		}
		return -1
	}
	g.Import(accelModule)
	g.Import(cookModule)
	emitCookHome(g, u, records, recordIndex)
	g.Pf("// ---- each table's cook record: its place in CookRecords ----\n")
	for _, f := range u.Files {
		for _, st := range f.Tables {
			g.Declare(st.Name + "CookRecord")
			g.Pf("let %sCookRecord: i64 = %d;\n", st.Name, index[st.Name])
		}
	}
	g.Pf("\n")
	return nil
}

// cookRecords is every record of the table closure, sorted by name: the
// tables and the packet types a table reaches.
func cookRecords(u *ir.Unit) []*ir.Struct {
	var out []*ir.Struct
	for name := range ir.TableClosure(u) {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st != nil && !st.IsMapEntry() {
			out = append(out, st)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func generateCookModules(u *ir.Unit, names map[string]string) (map[string][]byte, error) {
	out := map[string][]byte{}
	home := cookPackage(u)
	if home == "" {
		return out, nil
	}
	if err := sideModuleRefusal(u, "Cook"); err != nil {
		return nil, err
	}
	for _, f := range u.Files {
		if len(f.Tables) == 0 {
			continue
		}
		g := goose.NewGen(u, f, names)
		g.Suffix, g.Kind = "Cook", "cooked form"
		g.Import(accelModule)
		g.Import(cookModule)
		g.Import(home)
		for _, st := range f.Tables {
			emitCookOpen(g, u, st)
			emitCookRoot(g, u, st)
		}
		if err := g.Err(); err != nil {
			return nil, err
		}
		out[f.Base+"Cook.goose"] = g.Assemble()
	}
	return out, nil
}

func emitCookHome(g *goose.Gen, u *ir.Unit, records []*ir.Struct, recordIndex func(*ir.Struct) int64) {
	rt := goose.RuntimeNamespace
	g.Declare("BuildVersion")
	g.Pf("// THE BUILD VERSION (docs/SPEC-TABLES.md §20): one digest over every fact\n")
	g.Pf("// the bytes this build cooks or blocks depend on. It is the number a cook's\n")
	g.Pf("// header and a block's prologue carry, and the number Open compares.\n")
	g.Pf("let BuildVersion: u64 = 0x%016x;\n\n", ir.BuildVersion(u))
	g.Pf("// ---- the cook's records (docs/SPEC-TABLES.md §7.2, §8.1) ----\n")
	g.Pf("// Every record of the table closure AS DATA, at the layout the build version\n")
	g.Pf("// digests. AccelField.record indexes CookRecords.\n")
	for i, st := range records {
		ml := ir.RecordLayout(u, st)
		sym := fmt.Sprintf("_CookFields%d", i)
		g.Declare(sym)
		g.Pf("let %s: %s::AccelField[%d] = [\n", sym, rt, max(len(ml.Fields), 1))
		if len(ml.Fields) == 0 {
			g.Pf("    %s::AccelField { record: -1, count_offset: -1, present_offset: -1, offset_of_offset: -1, stride_offset: -1, .. },\n", rt)
		}
		for _, fl := range ml.Fields {
			g.Pf("    %s,\n", accelFieldOf(u, fl, false, recordIndex).render())
		}
		g.Pf("];\n")
	}
	g.Declare("CookRecords")
	g.Pf("let CookRecords: %s::AccelRecord[%d] = [\n", rt, len(records))
	for i, st := range records {
		ml := ir.RecordLayout(u, st)
		g.Pf("    %s::AccelRecord { name: %q, size: %d, align: %d, type_id: 0x%016x, fields: _CookFields%d[0..%d] },\n",
			rt, st.Name, ml.Size, ml.Align, ir.TableWireId(st.WireName()), i, len(ml.Fields))
	}
	g.Pf("];\n\n")
	g.Pf("// ---- the record accessors (docs/SPEC-TABLES.md §7.2, §19.2) ----\n")
	g.Pf("// <Row>Row<Field>(b, at) reads one field of the record at `at` in a view —\n")
	g.Pf("// a cook's region, or a block — and <Row>RowSet<Field> writes it. A cooked\n")
	g.Pf("// record IS the blittable row, so one set serves both accelerators.\n")
	for _, st := range records {
		ml := ir.RecordLayout(u, st)
		g.Declare(st.Name + "RowSize")
		g.Pf("let %sRowSize: i64 = %d;\n", st.Name, ml.Size)
		emitAccessors(g, u, st.Name+"Row", ml.Fields, false)
	}
	g.Pf("\n")
	emitCookWrite(g, u, records)
}

// emitCookOpen is one table's Open: match the header and point (§7). It is
// O(1) in the file's size — the header and nothing per node.
func emitCookOpen(g *goose.Gen, u *ir.Unit, st *ir.Struct) {
	rt := goose.RuntimeNamespace
	ml := ir.RecordLayout(u, st)
	n := st.Name
	g.Declare(n + "CookOpenReason")
	g.Declare(n + "CookOpen")
	g.Pf("// %s's cook: match the header and POINT (docs/SPEC-TABLES.md §7). On a\n", n)
	g.Pf("// match the root is at offset zero of schema_rt::cook_region(b); on any\n")
	g.Pf("// refusal the caller falls back to the wire. `lead` is the buffer's address\n")
	g.Pf("// modulo sixty-four, below zero for no buffer. It answers schema_rt::TableRefuse*.\n")
	g.Pf("fn %sCookOpenReason(b: const u8[:], lead: i64) -> i64 { return %s::cook_check(b, lead, %d, %d, BuildVersion); }\n", n, rt, ml.Size, ml.Align)
	g.Pf("fn %sCookOpen(b: const u8[:], lead: i64) -> bool { return %sCookOpenReason(b, lead) == %s::TableRefuseOk; }\n\n", n, n, rt)
}
