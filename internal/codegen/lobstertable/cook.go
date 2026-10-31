// The cooked form: Cook, CookMeasure, Open and the header check
// (docs/SPEC-TABLES.md §7, §7.1, §7.2, §7.6; docs/PORT-LOBSTER.md, THE CARD
// MAP). The per-table entry points name the unit's build version and the
// record's own layout; the walk itself lives once in runtime/cook.lobster
// (internal/tablecook/cook.go, ReadHeader, Layout, Region.Write and Uncook).
package lobstertable

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() { Register("cook", emitCook) }

// emitCook joins every table module with the cooked form: the layout of every
// record and union as data, the per-table CookMeasure/Cook pair (§7.6), the
// cooked record read (§7, Uncook) and Open's header match (§7, §7.1).
func emitCook(g *lobster.Gen) error {
	emitted := false
	// a packet type gets cooked storage only when a table reaches it: an
	// unreached one has no descriptor for the cooked read to fill
	reach := closure(g.Unit)
	candidates := append([]*ir.Struct{}, g.File.Tables...)
	for _, d := range g.File.Decls {
		if st, ok := d.(*ir.Struct); ok && reach[st.Name] {
			candidates = append(candidates, st)
		}
	}
	seen := map[string]bool{}
	for _, st := range candidates {
		if seen[st.Name] || st.IsMapEntry() || !st.CarriesForm("cook") {
			continue
		}
		seen[st.Name] = true
		emitCookTable(g, st)
		emitted = true
	}
	// The layouts of the closure's by-value packet types and unions ride in
	// the same module as their descriptors, so a walk resolves them by name.
	layouts := map[string]bool{}
	for _, st := range candidates {
		if layouts[st.Name] || st.IsMapEntry() {
			continue
		}
		layouts[st.Name] = true
		if !st.CarriesForm("cook") {
			emitCookTable(g, st)
		}
	}
	unions := append([]*ir.Union{}, g.File.TableUnions...)
	for _, d := range g.File.Decls {
		if un, ok := d.(*ir.Union); ok && reach[un.Name] {
			unions = append(unions, un)
		}
	}
	useen := map[string]bool{}
	for _, un := range unions {
		if useen[un.Name] {
			continue
		}
		useen[un.Name] = true
		emitCookUnion(g, un)
	}
	if emitted {
		g.Import("cook")
	}
	return nil
}

// emitCookTable writes <Name>CookTable, the record's storage pieces as data
// (docs/SPEC-TABLES.md §7.2, §20.3), and the four entry points §7.6 and §7
// claim: CookMeasure, Cook, ReadCook and Open.
func emitCookTable(g *lobster.Gen, st *ir.Struct) {
	u := g.Unit
	ml := ir.RecordLayout(u, st)
	record := ml.Size
	if recordHasExtent(u, st) {
		record = alignUp(ml.Size, ir.RegionAlignFloor)
	}
	g.Pf("// %sCookTable — %s's cooked storage as data (docs/SPEC-TABLES.md §7.2):\n", st.Name, st.Name)
	g.Pf("// each field's pieces and where a reference points, the same numbers\n")
	g.Pf("// the build version's record lines come from (§20.3).\n")
	g.Pf("let %sCookTable = %s.cook_table(%s.CookTable {\n", st.Name, rt, rt)
	g.Pf("    name: %s,\n", lobster.StringLit([]byte(st.Name)))
	g.Pf("    size: %d,\n", ml.Size)
	g.Pf("    align: %d,\n", ml.Align)
	g.Pf("    record: %d,\n", record)
	g.Pf("    extent: %s,\n", boolLit(recordHasExtent(u, st)))
	g.Pf("    build: %s,\n", buildLit(u))
	g.Pf("    fields: [\n")
	for i := range ml.Fields {
		fl := &ml.Fields[i]
		sep := ","
		if i == len(ml.Fields)-1 {
			sep = ""
		}
		g.Pf("        %s%s\n", cookField(g, fl.Field, fl.Offset), sep)
	}
	g.Pf("    ]::%s.CookField\n", rt)
	g.Pf("})\n\n")

	g.Pf("// %sCookMeasure is the whole file's length: the header, the region and\n", st.Name)
	g.Pf("// the directory, in int64 (docs/SPEC-TABLES.md §7.6).\n")
	g.Pf("def %sCookMeasure(rec:%s.Record) -> int:\n", st.Name, rt)
	g.Pf("    return %s.cook_measure(rec, %s)\n\n", rt, buildLit(u))

	g.Pf("// %sCook writes the cook of one loaded record in the order the caller\n", st.Name)
	g.Pf("// names, byte for byte with the tool's writer (docs/SPEC-TABLES.md §7.6).\n")
	g.Pf("def %sCook(rec:%s.Record, big:bool) -> string:\n", st.Name, rt)
	g.Pf("    return %s.cook_write(rec, %s, big)\n\n", rt, buildLit(u))

	g.Pf("// %sReadCook reads a cooked file back into the record model, which the\n", st.Name)
	g.Pf("// wire's save then writes: the proof no fact was lost (§7.5, Uncook).\n")
	g.Pf("def %sReadCook(data:string) -> %s.Record?:\n", st.Name, rt)
	g.Pf("    return %s.cook_read(data, %s, %sDescriptor, %sCookTable)\n\n", rt, buildLit(u), st.Name, st.Name)

	g.Pf("// %sOpen matches the cooked header for %s (docs/SPEC-TABLES.md §7.1):\n", st.Name, st.Name)
	g.Pf("// the magic and the order it establishes, the build version, the reserved\n")
	g.Pf("// words, the alignment and both part lengths. It points at the region and\n")
	g.Pf("// allocates nothing; the check lives once in the runtime.\n")
	g.Pf("def %sOpen(data:string) -> bool:\n", st.Name)
	g.Pf("    return %sOpenReason(data, 0, false) == \"ok\"\n\n", st.Name)

	g.Pf("// %sOpenReason is Open with the reason beside the refusal (docs/SPEC-TABLES.md\n", st.Name)
	g.Pf("// §7): the first failing clause as the refusal enum's value name, or \"ok\".\n")
	g.Pf("// data is exactly the extent the caller claims; lead is how far past an\n")
	g.Pf("// aligned base the buffer sits, and nobuf is a NULL buffer.\n")
	g.Pf("def %sOpenReason(data:string, lead:int, nobuf:bool) -> string:\n", st.Name)
	g.Pf("    return %s.cook_open_reason(data, %s, %d, %d, lead, nobuf)\n\n",
		rt, buildLit(u), ml.Align, ml.Size)
}

// emitCookUnion writes <Name>CookUnion, a union's overlay as data: the tag's
// width, the arms' shared offset and each arm's own pieces (§7.2).
func emitCookUnion(g *lobster.Gen, un *ir.Union) {
	u := g.Unit
	size, _, tag, armOffset := ir.UnionLayout(u, un)
	g.Pf("// %sCookUnion — union %s's cooked overlay (docs/SPEC-TABLES.md §7.2):\n", un.Name, un.Name)
	g.Pf("// the tag at zero, the set arm at the arms' shared offset; the rest zero.\n")
	g.Pf("let %sCookUnion = %s.cook_union_of(%s.CookUnion {\n", un.Name, rt, rt)
	g.Pf("    name: %s,\n", lobster.StringLit([]byte(un.Name)))
	g.Pf("    size: %d,\n", size)
	g.Pf("    tag: %d,\n", tag)
	g.Pf("    arm: %d,\n", armOffset)
	g.Pf("    extent: %s,\n", boolLit(unionHasExtent(u, un, map[*ir.Union]bool{})))
	g.Pf("    arms: [\n")
	for i, v := range un.Variants {
		sep := ","
		if i == len(un.Variants)-1 {
			sep = ""
		}
		if v.Void() {
			g.Pf("        %s.CookField {}%s\n", rt, sep)
			continue
		}
		f := *v.F
		f.Name = v.Name
		g.Pf("        %s%s\n", cookField(g, &f, 0), sep)
	}
	g.Pf("    ]::%s.CookField\n", rt)
	g.Pf("})\n\n")
}

func buildLit(u *ir.Unit) string {
	return lobster.IntLit(new(big.Int).SetUint64(ir.BuildVersion(u)))
}

// cookField renders one field's storage pieces and its containers' element
// facts (docs/SPEC-TABLES.md §7.2). off is the field's offset inside its
// record; a union arm's is zero, because the arm overlay begins there.
func cookField(g *lobster.Gen, f *ir.Field, off int64) string {
	u := g.Unit
	pieces := ir.FieldPieces(u, f, off)
	parts := []string{fmt.Sprintf("off: %d", off)}
	size := int64(0)
	if len(pieces) > 0 {
		size = pieces[0].Size
		parts = append(parts, fmt.Sprintf("size: %d", size))
		if len(pieces) > 1 && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString || f.Type.Kind == ir.TBytes || f.Array == ir.ArrayCounted || f.IsMap()) {
			parts = append(parts, fmt.Sprintf("count: %d", pieces[1].Offset-off))
		}
	}
	if f.Type.Optional && len(pieces) > 0 {
		parts = append(parts, fmt.Sprintf("present: %d", pieces[len(pieces)-1].Offset-off))
	}
	// The element's own pieces: an array's stride and a list's element bytes.
	ef := *f
	ef.Array = ir.ArrayNone
	ef.ArrayBound = 0
	ef.KeyEnum = ""
	ef.KeyEnumRef = nil
	ef.Type.Optional = false
	ep := ir.FieldPieces(u, &ef, 0)
	var esize int64
	var ecount int64 = -1
	for _, p := range ep {
		if p.Offset+p.Size > esize {
			esize = p.Offset + p.Size
		}
	}
	if len(ep) > 1 && (ef.Type.Kind == ir.TString || ef.Type.Kind == ir.TWString || ef.Type.Kind == ir.TBytes) {
		ecount = ep[1].Offset
	}
	if f.Array != ir.ArrayNone || f.IsMap() {
		parts = append(parts, fmt.Sprintf("elem: %d", esize), fmt.Sprintf("esize: %d", esize), fmt.Sprintf("ecount: %d", ecount))
	}
	if f.Array == ir.ArrayList {
		lsize, lalign := ir.ListElementLayout(u, f)
		parts = append(parts, fmt.Sprintf("lsize: %d", lsize), fmt.Sprintf("lalign: %d", lalign),
			fmt.Sprintf("lvalue: %d", esize), fmt.Sprintf("lcount: %d", ecount))
	}
	if f.Type.Kind == ir.TNamed {
		if un, ok := f.Type.Ref.(*ir.Union); ok {
			_, _, tag, arm := ir.UnionLayout(u, un)
			parts = append(parts, fmt.Sprintf("tag: %d", tag), fmt.Sprintf("arm: %d", arm))
		}
	}
	return rt + ".CookField { " + strings.Join(parts, ", ") + " }"
}

func alignUp(v, a int64) int64 {
	if a <= 1 {
		return v
	}
	return (v + a - 1) / a * a
}

// recordHasExtent reports a record with any container reachable by value
// (docs/SPEC-TABLES.md §2.9, §7.2): its node is the record plus the extent.
func recordHasExtent(u *ir.Unit, st *ir.Struct) bool {
	if st == nil {
		return false
	}
	for _, f := range st.Fields {
		if f.IsMap() || f.IsList() {
			return true
		}
		if f.Type.Pointer || f.Type.Blob() || f.Type.Kind != ir.TNamed {
			continue
		}
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			if recordHasExtent(u, ref) {
				return true
			}
		case *ir.Union:
			if unionHasExtent(u, ref, map[*ir.Union]bool{}) {
				return true
			}
		}
	}
	return false
}

func unionHasExtent(u *ir.Unit, un *ir.Union, seen map[*ir.Union]bool) bool {
	if un == nil || seen[un] {
		return false
	}
	seen[un] = true
	for _, v := range un.Variants {
		if v.F == nil {
			continue
		}
		if v.F.IsMap() || v.F.IsList() {
			return true
		}
		if v.F.Type.Pointer || v.F.Type.Blob() || v.F.Type.Kind != ir.TNamed {
			continue
		}
		switch ref := v.F.Type.Ref.(type) {
		case *ir.Struct:
			if recordHasExtent(u, ref) {
				return true
			}
		case *ir.Union:
			if unionHasExtent(u, ref, seen) {
				return true
			}
		}
	}
	return false
}
