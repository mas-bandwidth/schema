// fixedevolve.go — the fixed form's evolution, plan compiler, prefill, bounds
// pass, and named refusals (docs/SPEC-TABLES.md §3.4, §6.5;
// docs/FIXED-FORM-ALGORITHM.md §1–§5; docs/PORT-LOBSTER.md).
package lobstertable

import (
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() {
	Register("fixed-form-evolve", emitFixedEvolve)
}

// FixedLineageEntry is one locked layout of one fixed table (docs/FIXED-FORM-ALGORITHM.md §5.2):
// Wire is the eight-byte layout hash, Layout is the layout bytes verbatim, Record
// is the record size (8 + body bytes) from the lock, and Retired/Reason record
// an operator retirement that raises the floor.
type FixedLineageEntry struct {
	Wire    uint64
	Layout  []byte
	Record  int64
	Retired bool
	Reason  string
}

var (
	lineageMu   sync.Mutex
	unitLineage = map[*ir.Unit]map[string][]FixedLineageEntry{}
)

// GenerateLineage emits the Lobster table modules for u with explicit per-table
// lineage entries (oldest first; the table's own current layout is appended
// last if not already present).
func GenerateLineage(u *ir.Unit, lineage map[string][]FixedLineageEntry) (map[string][]byte, error) {
	lineageMu.Lock()
	unitLineage[u] = lineage
	lineageMu.Unlock()
	defer func() {
		lineageMu.Lock()
		delete(unitLineage, u)
		lineageMu.Unlock()
	}()
	return Generate(u)
}

// FixedLineageOf computes the entry a unit's own build locks for table name:
// its layout bytes, the wire hash over them and the definitions digest, and its
// record size (8 + body bytes).
func FixedLineageOf(u *ir.Unit, name string) (FixedLineageEntry, bool) {
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if st.Name != name {
				continue
			}
			entries := ir.TableFixedWalkRoot(st)
			layout := ir.TableFixedLayoutBytes(entries)
			return FixedLineageEntry{
				Wire:   ir.TableFixedLayoutHash(layout, st),
				Layout: layout,
				Record: 8 + ir.TableFixedTypeBytes(st),
			}, true
		}
	}
	return FixedLineageEntry{}, false
}

func lookupUnitLineage(u *ir.Unit) (map[string][]FixedLineageEntry, bool) {
	lineageMu.Lock()
	defer lineageMu.Unlock()
	m, ok := unitLineage[u]
	return m, ok
}

func fixedTableQualifies(g *lobster.Gen, st *ir.Struct) bool {
	if st == nil || st.IsMapEntry() || !ir.TableFixedSupported(st, 0) {
		return false
	}
	if ir.TableFixedTypeBytes(st) > ir.TableFixedRecordMaxBytes || !ir.TableFixedWithinDepth(st) {
		return false
	}
	if st.IsTable && (g.Unit.TableUnions[st.Name] != nil || ir.VariableTables(g.Unit)[st.Name]) {
		return false
	}
	return fixedLobsterSimple(st, 0)
}

func emitFixedEvolve(g *lobster.Gen) error {
	var local []*ir.Struct
	localSet := map[string]bool{}
	addCandidate := func(st *ir.Struct) {
		if !localSet[st.Name] && fixedTableQualifies(g, st) {
			localSet[st.Name] = true
			local = append(local, st)
		}
	}
	for _, st := range g.File.Tables {
		addCandidate(st)
	}
	for _, d := range g.File.Decls {
		if st, ok := d.(*ir.Struct); ok {
			addCandidate(st)
		}
	}
	if len(local) == 0 {
		return nil
	}
	g.Import("fixed")
	g.Import("fixedevolve")

	// Collect local structs in post-order so nested structs emit their
	// helpers before enclosing structs call them.
	visited := map[string]bool{}
	var order []*ir.Struct
	var visit func(st *ir.Struct)
	visit = func(st *ir.Struct) {
		if visited[st.Name] {
			return
		}
		visited[st.Name] = true
		for _, f := range st.Fields {
			fixedVisitFieldDeps(g, f, visit)
		}
		if localSet[st.Name] {
			order = append(order, st)
		}
	}
	for _, st := range local {
		visit(st)
	}

	explicitMap, hasExplicit := lookupUnitLineage(g.Unit)
	var sibMap map[string][]FixedLineageEntry
	if !hasExplicit {
		sibMap = discoverSiblingLineage(g.Unit)
	}

	for _, st := range order {
		emitFixedResetFn(g, st)
		emitFixedLeafWalkFn(g, st)
		emitFixedScatterFn(g, st)
		emitFixedClampBodyFn(g, st)
		emitFixedCheckBodyFn(g, st)
		emitFixedFiniteBodyFn(g, st)
		if ir.TableFixedClampNeeded(st) {
			emitFixedClampFn(g, st)
		}
		var older []FixedLineageEntry
		if hasExplicit && explicitMap != nil {
			older = explicitMap[st.Name]
		} else if sibMap != nil {
			older = sibMap[st.Name]
		}
		emitFixedEvolveRoot(g, st, older)
	}
	return nil
}

func fixedVisitFieldDeps(g *lobster.Gen, f *ir.Field, visit func(*ir.Struct)) {
	if f.Type.Kind != ir.TNamed {
		return
	}
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		requireHome(g, r.Name)
		g.TypeRef(r.Name, r.IsTable)
		visit(r)
	case *ir.Union:
		requireHome(g, r.Name)
		g.TypeRef(r.Name, g.Unit.TableUnions[r.Name] != nil)
		for _, v := range r.Variants {
			if v.F != nil {
				fixedVisitFieldDeps(g, v.F, visit)
			}
		}
	case *ir.Enum:
		g.TypeRef(r.Name, false)
	case *ir.Flags:
		g.TypeRef(r.Name, false)
	}
}

// discoverSiblingLineage checks if u was loaded from a VNEW_*.schema or
// VMID_*.schema file on disk and loads older sibling files (VOLD_, VMID_,
// VBRA_, VBRB_) in the same directory when present.
func discoverSiblingLineage(u *ir.Unit) map[string][]FixedLineageEntry {
	if len(u.Files) == 0 || u.Files[0].Path == "" {
		return nil
	}
	p := u.Files[0].Path
	base := filepath.Base(p)
	dir := filepath.Dir(p)
	var rest string
	var prefixes []string
	switch {
	case strings.HasPrefix(base, "VNEW_"):
		rest = strings.TrimPrefix(base, "VNEW_")
		prefixes = []string{"VOLD_", "VMID_", "VBRA_", "VBRB_"}
	case strings.HasPrefix(base, "VMID_"):
		rest = strings.TrimPrefix(base, "VMID_")
		prefixes = []string{"VOLD_"}
	default:
		return nil
	}
	out := map[string][]FixedLineageEntry{}
	for _, pref := range prefixes {
		sibPath := filepath.Join(dir, pref+rest)
		data, err := os.ReadFile(sibPath)
		if err != nil {
			continue
		}
		pf, perrs := parser.Parse(sibPath, data)
		if len(perrs) > 0 {
			continue
		}
		sibName := filepath.Base(sibPath)
		su, cerrs := check.Unit([]check.SourceFile{{
			Path: sibPath, Name: sibName, Base: strings.TrimSuffix(sibName, ".schema"), Bytes: data, AST: pf,
		}})
		if len(cerrs) > 0 {
			continue
		}
		for _, f := range su.Files {
			for _, st := range f.Tables {
				if e, ok := FixedLineageOf(su, st.Name); ok {
					out[st.Name] = append(out[st.Name], e)
				}
			}
		}
	}
	return out
}

// ---- Reset ------------------------------------------------------------------

func emitFixedResetFn(g *lobster.Gen, st *ir.Struct) {
	g.Pf("// %sReset restores v to its declared defaults in place (docs/SPEC-TABLES.md §3.4).\n", st.Name)
	g.Pf("def %sReset(v:%s):\n", st.Name, st.Name)
	if len(st.Fields) == 0 {
		g.Pf("    pass()\n\n")
		return
	}
	for _, f := range st.Fields {
		emitResetField(g, f, "v."+ir.GoExportName(f.Name), "    ")
	}
	g.Pf("\n")
}

func emitResetField(g *lobster.Gen, f *ir.Field, expr, ind string) {
	if f.Type.Optional {
		present := slotExprFull(expr, f, ir.GoExportName(f.Name+"_present"))
		g.Pf("%s%s = false\n", ind, present)
	}
	emitResetPayload(g, f, expr, ind)
}

func emitResetPayload(g *lobster.Gen, f *ir.Field, expr, ind string) {
	switch {
	case f.KeyEnumRef != nil:
		iv := fmt.Sprintf("i_rst_%s", ir.GoExportName(f.Name))
		ef := *f
		ef.KeyEnum = ""
		ef.KeyEnumRef = nil
		ef.HasDefault = false
		g.Pf("%sfor(%d) %s:\n", ind, f.KeyEnumRef.Max, iv)
		emitResetElement(g, &ef, fmt.Sprintf("%s[%s]", expr, iv), ind+"    ")
		return
	case f.Array == ir.ArrayFixed:
		iv := fmt.Sprintf("i_rst_%s", ir.GoExportName(f.Name))
		ef := *f
		ef.Array = ir.ArrayNone
		ef.HasDefault = false
		g.Pf("%sfor(%d) %s:\n", ind, f.ArrayBound, iv)
		emitResetElement(g, &ef, fmt.Sprintf("%s[%s]", expr, iv), ind+"    ")
		return
	case f.Array == ir.ArrayCounted:
		count := slotExpr(expr, f, "Count")
		g.Pf("%s%s = %d\n", ind, count, f.BornCount())
		iv := fmt.Sprintf("i_rst_%s", ir.GoExportName(f.Name))
		ef := *f
		ef.Array = ir.ArrayNone
		ef.HasDefault = false
		g.Pf("%sfor(%d) %s:\n", ind, f.ArrayBound, iv)
		emitResetElement(g, &ef, fmt.Sprintf("%s[%s]", expr, iv), ind+"    ")
		return
	}
	emitResetElement(g, f, expr, ind)
}

func emitResetElement(g *lobster.Gen, f *ir.Field, expr, ind string) {
	t := f.Type
	switch t.Kind {
	case ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Struct:
			requireHome(g, r.Name)
			g.Pf("%s%sReset(%s)\n", ind, r.Name, expr)
			return
		case *ir.Union:
			g.Pf("%s%s.Type = %s\n", ind, expr, lobster.VariantName(r.Name+"Type", "None"))
			for _, v := range r.Variants {
				if v.Void() {
					continue
				}
				af := *v.F
				af.Name = v.Name
				emitResetPayload(g, &af, expr+"."+ir.GoExportName(v.Name), ind)
			}
			return
		case *ir.Enum:
			g.TypeRef(r.Name, false)
			v := "None"
			for _, variant := range r.Variants {
				if f.HasDefault && variant == f.DefVariant {
					v = variant
				}
			}
			g.Pf("%s%s = %s\n", ind, expr, lobster.VariantName(r.Name, v))
			return
		case *ir.Flags:
			g.TypeRef(r.Name, false)
			val := new(big.Int)
			if f.HasDefault && f.DefInt != nil {
				val = f.DefInt
			}
			g.Pf("%s%s = %s\n", ind, expr, lobster.IntLit(val))
			return
		}
	case ir.TString, ir.TBytes, ir.TWString:
		unit := int64(1)
		if t.Kind == ir.TWString {
			unit = 2
		}
		length := slotExpr(expr, f, "Length")
		g.Pf("%s%s = schema_rt.zeros(%d)\n", ind, expr, unit*t.Size)
		if f.HasDefault && len(f.DefBytes) > 0 && t.Kind != ir.TWString {
			g.Pf("%s%s = schema_rt.put(%s, %s)\n", ind, expr, expr, lobster.StringLit(f.DefBytes))
			g.Pf("%s%s = %d\n", ind, length, len(f.DefBytes))
		} else {
			g.Pf("%s%s = 0\n", ind, length)
		}
		return
	case ir.TBool:
		g.Pf("%s%s = %s\n", ind, expr, strconv.FormatBool(f.HasDefault && f.DefBool))
		return
	case ir.TFloat32:
		if f.HasDefault {
			if !f.HasFloatRange && f.DefFloat == 0 && math.Signbit(f.DefFloat) {
				g.Pf("%s%s = schema_rt.f64frombits(0x8000000000000000)\n", ind, expr)
			} else {
				g.Pf("%s%s = %s\n", ind, expr, lobster.FormatFloat32(f.DefFloat))
			}
		} else {
			g.Pf("%s%s = 0.0\n", ind, expr)
		}
		return
	case ir.TFloat64:
		if f.HasDefault {
			if !f.HasFloatRange && f.DefFloat == 0 && math.Signbit(f.DefFloat) {
				g.Pf("%s%s = schema_rt.f64frombits(0x8000000000000000)\n", ind, expr)
			} else {
				g.Pf("%s%s = %s\n", ind, expr, lobster.FormatFloat(f.DefFloat))
			}
		} else {
			g.Pf("%s%s = 0.0\n", ind, expr)
		}
		return
	case ir.TInt, ir.TBits, ir.TFixed:
		val := new(big.Int)
		if f.HasDefault && f.DefInt != nil {
			val = f.DefInt
		}
		if t.Width > 64 {
			g.Pf("%s%s = %s\n", ind, expr, lobster.Int128Lit(val))
		} else {
			g.Pf("%s%s = %s\n", ind, expr, lobster.IntLit(val))
		}
		return
	}
}

// ---- Identity Plan Leaves (<Name>FixedLeaves) -------------------------------

func emitFixedLeafWalkFn(g *lobster.Gen, st *ir.Struct) {
	g.Pf("def %sFixedLeaves(out:[schema_rt.TableFixedEntry], src:int, dst:int) -> int:\n", st.Name)
	off := int64(0)
	emitted := false
	for _, f := range st.Fields {
		if !f.Deprecated {
			emitFixedFieldLeaves(g, f, off, "    ")
			emitted = true
		}
		off += ir.TableFixedFieldBytes(f)
	}
	if !emitted {
		g.Pf("    pass()\n")
	}
	g.Pf("    return length(out)\n\n")
}

func emitFixedFieldLeaves(g *lobster.Gen, f *ir.Field, off int64, ind string) {
	base := off
	if f.Type.Optional {
		g.Pf("%spush(out, schema_rt.TableFixedEntry { src: src + %d, dst: dst + %d, size: 1, grd: schema_rt.tableFixedNoGuard, op: schema_rt.tableFixedBool })\n", ind, base, base)
		base += ir.TableFixedPresentBytes
	}
	emitFixedPayloadLeaves(g, f, "src", "dst", base, ind)
}

func emitFixedPayloadLeaves(g *lobster.Gen, f *ir.Field, srcBase, dstBase string, base int64, ind string) {
	switch {
	case f.KeyEnumRef != nil:
		emitFixedElementLeavesLoop(g, f, srcBase, dstBase, base, f.KeyEnumRef.Max, ind)
	case f.Array == ir.ArrayFixed:
		emitFixedElementLeavesLoop(g, f, srcBase, dstBase, base, f.ArrayBound, ind)
	case f.Array == ir.ArrayCounted:
		g.Pf("%spush(out, schema_rt.TableFixedEntry { src: %s + %d, dst: %s + %d, size: %d, grd: schema_rt.tableFixedNoGuard, op: schema_rt.tableFixedCount })\n",
			ind, srcBase, base, dstBase, base, f.ArrayBound)
		emitFixedElementLeavesLoop(g, f, srcBase, dstBase, base+ir.TableFixedCountBytes, f.ArrayBound, ind)
	case f.Type.Kind == ir.TString:
		g.Pf("%spush(out, schema_rt.TableFixedEntry { src: %s + %d, dst: %s + %d, size: %d, aux: %s + %d, grd: schema_rt.tableFixedNoGuard, op: schema_rt.tableFixedText, meta: 1 })\n",
			ind, srcBase, base, dstBase, base, f.Type.Size, dstBase, base+4)
	case f.Type.Kind == ir.TWString:
		g.Pf("%spush(out, schema_rt.TableFixedEntry { src: %s + %d, dst: %s + %d, size: %d, aux: %s + %d, grd: schema_rt.tableFixedNoGuard, op: schema_rt.tableFixedText, meta: 2 })\n",
			ind, srcBase, base, dstBase, base, 2*f.Type.Size, dstBase, base+4)
	case f.Type.Kind == ir.TBytes:
		g.Pf("%spush(out, schema_rt.TableFixedEntry { src: %s + %d, dst: %s + %d, size: %d, aux: %s + %d, grd: schema_rt.tableFixedNoGuard, op: schema_rt.tableFixedText, meta: 3 })\n",
			ind, srcBase, base, dstBase, base, f.Type.Size, dstBase, base+4)
	default:
		es := fmt.Sprintf("%s + %d", srcBase, base)
		ed := fmt.Sprintf("%s + %d", dstBase, base)
		emitFixedElementLeavesAt(g, f, es, ed, base, ind)
	}
}

func emitFixedElementLeavesLoop(g *lobster.Gen, f *ir.Field, srcBase, dstBase string, base, count int64, ind string) {
	elem := ir.TableFixedElementBytes(f)
	iv := fmt.Sprintf("i_lf_%s_%d", ir.GoExportName(f.Name), base)
	g.Pf("%sfor(%d) %s:\n", ind, count, iv)
	es := fmt.Sprintf("%s + %d + %s * %d", srcBase, base, iv, elem)
	ed := fmt.Sprintf("%s + %d + %s * %d", dstBase, base, iv, elem)
	emitFixedElementLeavesAt(g, f, es, ed, base, ind+"    ")
}

func emitFixedElementLeavesAt(g *lobster.Gen, f *ir.Field, es, ed string, salt int64, ind string) {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			requireHome(g, r.Name)
			g.Pf("%s%sFixedLeaves(out, %s, %s)\n", ind, r.Name, es, ed)
			return
		case *ir.Union:
			tag := ir.TableFixedUnionTagBytes(r)
			g.Pf("%spush(out, schema_rt.TableFixedEntry { src: %s, dst: %s, size: %d, grd: schema_rt.tableFixedNoGuard, op: schema_rt.tableFixedCopy })\n",
				ind, es, ed, tag)
			for i, v := range r.Variants {
				if v.F == nil || v.F.Deprecated {
					continue
				}
				gv := fmt.Sprintf("grd_at_%s_%d_%d", ir.GoExportName(v.Name), salt, i)
				qv := fmt.Sprintf("q_%s_%d_%d", ir.GoExportName(v.Name), salt, i)
				g.Pf("%slet %s = length(out)\n", ind, gv)
				af := *v.F
				af.Name = v.Name
				emitFixedPayloadLeaves(g, &af, es, ed, tag, ind)
				g.Pf("%svar %s = %s\n", ind, qv, gv)
				g.Pf("%swhile %s < length(out):\n", ind, qv)
				g.Pf("%s    push(out[%s].chain, schema_rt.TableFixedGuardLink { grd: %s, arg: %d, arg_w: %d })\n",
					ind, qv, es, i+1, tag)
				g.Pf("%s    %s += 1\n", ind, qv)
			}
			return
		}
	}
	op := "schema_rt.tableFixedCopy"
	if f.Type.Kind == ir.TBool {
		op = "schema_rt.tableFixedBool"
	}
	g.Pf("%spush(out, schema_rt.TableFixedEntry { src: %s, dst: %s, size: %d, grd: schema_rt.tableFixedNoGuard, op: %s })\n",
		ind, es, ed, ir.TableFixedElementBytes(f), op)
}

// ---- Scatter from Canonical Flat Body Image (<Name>FixedScatter) ------------

func emitFixedScatterFn(g *lobster.Gen, st *ir.Struct) {
	name := fixedName(st)
	g.Pf("// %sScatter reads one canonical flat body image at at into v in place.\n", name)
	g.Pf("def %sScatter(data:string, at:int, v:%s):\n", name, st.Name)
	if len(st.Fields) == 0 {
		g.Pf("    pass()\n\n")
		return
	}
	off := int64(0)
	for _, f := range st.Fields {
		scatterField(g, st, f, "v."+ir.GoExportName(f.Name), "at", off, "    ")
		off += ir.TableFixedFieldBytes(f)
	}
	g.Pf("\n")
}

func scatterField(g *lobster.Gen, owner *ir.Struct, f *ir.Field, expr, base string, off int64, ind string) {
	if f.Type.Optional {
		present := slotExprFull(expr, f, ir.GoExportName(f.Name+"_present"))
		g.Pf("%s%s = read_uint8_le(data, %s + %d) != 0\n", ind, present, base, off)
		g.Pf("%sif %s:\n", ind, present)
		scatterPayload(g, owner, f, expr, base, off+ir.TableFixedPresentBytes, ind+"    ")
		g.Pf("%selse:\n", ind)
		emitResetPayload(g, f, expr, ind+"    ")
		return
	}
	scatterPayload(g, owner, f, expr, base, off, ind)
}

func scatterPayload(g *lobster.Gen, owner *ir.Struct, f *ir.Field, expr, base string, off int64, ind string) {
	switch {
	case f.KeyEnumRef != nil:
		elem := ir.TableFixedElementBytes(f)
		iv := loopVar(f, off)
		g.Pf("%sfor(%d) %s:\n", ind, f.KeyEnumRef.Max, iv)
		scatterElement(g, f, fmt.Sprintf("%s[%s]", expr, iv), base, off, elem, iv, ind+"    ")
		return
	case f.Array == ir.ArrayFixed:
		elem := ir.TableFixedElementBytes(f)
		iv := loopVar(f, off)
		g.Pf("%sfor(%d) %s:\n", ind, f.ArrayBound, iv)
		scatterElement(g, f, fmt.Sprintf("%s[%s]", expr, iv), base, off, elem, iv, ind+"    ")
		return
	case f.Array == ir.ArrayCounted:
		count := slotExpr(expr, f, "Count")
		g.Pf("%s%s = read_int32_le(data, %s + %d)\n", ind, count, base, off)
		elem := ir.TableFixedElementBytes(f)
		iv := loopVar(f, off)
		g.Pf("%sfor(%s) %s:\n", ind, count, iv)
		scatterElement(g, f, fmt.Sprintf("%s[%s]", expr, iv), base, off+ir.TableFixedCountBytes, elem, iv, ind+"    ")
		// Reset trailing slack slots [count..ArrayBound) to element defaults.
		ef := *f
		ef.Array = ir.ArrayNone
		ef.HasDefault = false
		kv := fmt.Sprintf("k_slk_%s_%d", ir.GoExportName(f.Name), off)
		g.Pf("%svar %s = %s\n", ind, kv, count)
		g.Pf("%swhile %s < %d:\n", ind, kv, f.ArrayBound)
		emitResetElement(g, &ef, fmt.Sprintf("%s[%s]", expr, kv), ind+"    ")
		g.Pf("%s    %s += 1\n", ind, kv)
		return
	}
	scatterElement(g, f, expr, base, off, 0, "", ind)
}

func scatterElement(g *lobster.Gen, f *ir.Field, expr, base string, off, stride int64, idx, ind string) {
	at := fmt.Sprintf("%s + %d", base, off)
	if idx != "" {
		at = fmt.Sprintf("%s + %d + %s * %d", base, off, idx, stride)
	}
	t := f.Type
	switch t.Kind {
	case ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Struct:
			requireHome(g, r.Name)
			g.Pf("%s%sScatter(data, %s, %s)\n", ind, fixedName(r), at, expr)
			return
		case *ir.Union:
			scatterUnion(g, r, expr, at, off, ind)
			return
		case *ir.Enum:
			g.TypeRef(r.Name, false)
			w := r.StorageBits / 8
			g.Pf("%s%s = %s(%s(data, %s))\n", ind, expr, r.Name, readInt(w, false), at)
			return
		case *ir.Flags:
			g.TypeRef(r.Name, false)
			g.Pf("%s%s = read_uint64_le(data, %s)\n", ind, expr, at)
			return
		}
	case ir.TBool:
		g.Pf("%s%s = read_uint8_le(data, %s) != 0\n", ind, expr, at)
		return
	case ir.TFloat32:
		g.Pf("%s%s = read_float32_le(data, %s)\n", ind, expr, at)
		return
	case ir.TFloat64:
		g.Pf("%s%s = read_float64_le(data, %s)\n", ind, expr, at)
		return
	case ir.TString, ir.TBytes, ir.TWString:
		unit := int64(1)
		if t.Kind == ir.TWString {
			unit = 2
		}
		length := slotExpr(expr, f, "Length")
		tmp := fmt.Sprintf("sbuf_%s_%d", ir.GoExportName(f.Name), off)
		jv := fmt.Sprintf("j_sc_%s_%d", ir.GoExportName(f.Name), off)
		g.Pf("%s%s = read_int32_le(data, %s)\n", ind, length, at)
		g.Pf("%svar %s = schema_rt.zeros(%d)\n", ind, tmp, unit*t.Size)
		g.Pf("%sfor(%s * %d) %s: %s = write_int8_le(%s, %s, read_uint8_le(data, %s + 4 + %s))\n",
			ind, length, unit, jv, tmp, tmp, jv, at, jv)
		g.Pf("%s%s = %s\n", ind, expr, tmp)
		return
	case ir.TInt, ir.TFixed, ir.TBits:
		if t.Kind == ir.TBits {
			w := 8
			if t.Width <= 32 {
				w = 4
			}
			g.Pf("%s%s = %s(data, %s)\n", ind, expr, readInt(w, false), at)
			return
		}
		if t.Width > 64 {
			g.Pf("%s%s = schema_rt.Int128 { lo: read_uint64_le(data, %s), hi: read_uint64_le(data, %s + 8) }\n", ind, expr, at, at)
			return
		}
		g.Pf("%s%s = %s(data, %s)\n", ind, expr, readInt(t.Width/8, t.Signed), at)
		return
	}
}

func scatterUnion(g *lobster.Gen, un *ir.Union, expr, at string, salt int64, ind string) {
	tag := ir.TableFixedUnionTagBytes(un)
	g.Pf("%s%s.Type = %sType(%s(data, %s))\n", ind, expr, un.Name, readInt(int(tag), false), at)
	for i, v := range un.Variants {
		if v.Void() {
			continue
		}
		g.Pf("%sif int(%s.Type) == %d:\n", ind, expr, i+1)
		f := *v.F
		f.Name = v.Name
		scatterPayload(g, nil, &f, expr+"."+ir.GoExportName(v.Name), at, tag, ind+"    ")
	}
}

// ---- Read-Side Clamp & Write-Side Check -------------------------------------

type fixedPass int

const (
	fixedPassClamp fixedPass = iota
	fixedPassCheck
)

func emitFixedClampBodyFn(g *lobster.Gen, st *ir.Struct) {
	g.Pf("def %sFixedClampBody(v:%s, report:schema_rt.TableReport):\n", st.Name, st.Name)
	emitted := false
	for _, f := range st.Fields {
		if emitFixedBoundField(g, f, "v", "    ", fixedPassClamp) {
			emitted = true
		}
	}
	if !emitted {
		g.Pf("    pass()\n")
	}
	g.Pf("\n")
}

func emitFixedClampFn(g *lobster.Gen, st *ir.Struct) {
	g.Pf("def %sFixedClamp(v:%s, report:schema_rt.TableReport):\n", st.Name, st.Name)
	g.Pf("    %sFixedClampBody(v, report)\n\n", st.Name)
}

func emitFixedCheckBodyFn(g *lobster.Gen, st *ir.Struct) {
	g.Pf("def %sFixedCheckBody(v:%s) -> bool:\n", st.Name, st.Name)
	for _, f := range st.Fields {
		emitFixedBoundField(g, f, "v", "    ", fixedPassCheck)
	}
	g.Pf("    return true\n\n")
}

func fixedCheckNeededStruct(st *ir.Struct, seen map[string]bool) bool {
	if seen[st.Name] {
		return false
	}
	seen[st.Name] = true
	for _, f := range st.Fields {
		if fixedCheckNeededField(f, seen) {
			return true
		}
	}
	return false
}

func fixedCheckNeededField(f *ir.Field, seen map[string]bool) bool {
	if f.Deprecated {
		return false
	}
	// A FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3): the writer's contract
	// refuses a NaN or an infinity, so every float is checked.
	if f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64 {
		return true
	}
	if f.Array == ir.ArrayCounted {
		return true
	}
	if f.Type.Kind == ir.TNamed {
		if r, ok := f.Type.Ref.(*ir.Struct); ok {
			return fixedCheckNeededStruct(r, seen)
		}
		return ir.TableFixedClampNeededField(f)
	}
	switch f.Type.Kind {
	case ir.TString, ir.TWString, ir.TBytes:
		return true
	}
	return ir.TableFixedClampNeededField(f)
}

func fixedBoundNeeded(f *ir.Field, chk bool) bool {
	if chk && f.Deprecated {
		return false
	}
	if f.Array == ir.ArrayCounted && chk {
		return true
	}
	if f.Array != ir.ArrayNone || f.KeyEnumRef != nil {
		elem := *f
		elem.Array, elem.KeyEnum, elem.KeyEnumRef = ir.ArrayNone, "", nil
		return fixedBoundNeeded(&elem, chk)
	}
	if f.Type.Kind == ir.TNamed {
		if r, ok := f.Type.Ref.(*ir.Struct); ok {
			return fixedBoundNeededStruct(r, chk)
		}
		return ir.TableFixedClampNeededField(f)
	}
	if chk {
		switch f.Type.Kind {
		case ir.TString, ir.TWString, ir.TBytes, ir.TFloat32, ir.TFloat64:
			return true
		}
	}
	if !chk && f.HasFloatRange && (f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64) {
		return true
	}
	_, ok := lobsterScalarWidthBound(f, "x")
	return ok || len(lobsterScalarRangeBounds(f, "x")) > 0
}

func fixedBoundNeededStruct(r *ir.Struct, chk bool) bool {
	if chk {
		return fixedCheckNeededStruct(r, map[string]bool{})
	}
	return ir.TableFixedClampNeeded(r)
}

func fixedArmNeeded(r *ir.Union, chk bool) bool {
	for _, v := range r.Variants {
		if v.F != nil && fixedBoundNeeded(v.F, chk) {
			return true
		}
	}
	return false
}

func emitFixedBoundField(g *lobster.Gen, f *ir.Field, val, ind string, pass fixedPass) bool {
	chk := pass != fixedPassClamp
	if chk && f.Deprecated {
		return false
	}
	if !fixedBoundNeeded(f, chk) {
		return false
	}
	if f.Type.Optional {
		g.Pf("%sif %s.%s:\n", ind, val, ir.GoExportName(f.Name+"_present"))
		if !emitFixedBoundPayload(g, f, val, ind+"    ", pass) {
			g.Pf("%s    pass()\n", ind)
		}
		return true
	}
	return emitFixedBoundPayload(g, f, val, ind, pass)
}

func emitFixedBoundPayload(g *lobster.Gen, f *ir.Field, val, ind string, pass fixedPass) bool {
	chk := pass != fixedPassClamp
	elem := *f
	elem.Array, elem.KeyEnum, elem.KeyEnumRef = ir.ArrayNone, "", nil
	loop := func(countExpr string) bool {
		if !fixedBoundNeeded(&elem, chk) {
			return false
		}
		iv := fmt.Sprintf("i_bd_%s", ir.GoExportName(f.Name))
		g.Pf("%sfor(%s) %s:\n", ind, countExpr, iv)
		if !emitFixedBoundElement(g, f, fmt.Sprintf("%s.%s[%s]", val, ir.GoExportName(f.Name), iv), ind+"    ", pass) {
			g.Pf("%s    pass()\n", ind)
		}
		return true
	}
	switch {
	case f.KeyEnumRef != nil:
		return loop(strconv.FormatInt(f.KeyEnumRef.Max, 10))
	case f.Array == ir.ArrayFixed:
		return loop(strconv.FormatInt(f.ArrayBound, 10))
	case f.Array == ir.ArrayCounted:
		emitted := false
		cnt := fmt.Sprintf("%s.%sCount", val, ir.GoExportName(f.Name))
		if chk {
			g.Pf("%sif %s < 0 or %s > %d:\n%s    return false\n", ind, cnt, cnt, f.ArrayBound, ind)
			emitted = true
		}
		if loop(cnt) {
			emitted = true
		}
		return emitted
	default:
		return emitFixedBoundElement(g, f, val+"."+ir.GoExportName(f.Name), ind, pass)
	}
}

type lobsterScalarBound struct {
	cond string
	set  string
}

func emitFixedBoundElement(g *lobster.Gen, f *ir.Field, expr, ind string, pass fixedPass) bool {
	chk := pass != fixedPassClamp
	applyBounds := func(conds []lobsterScalarBound) bool {
		if len(conds) == 0 {
			return false
		}
		if chk {
			var parts []string
			for _, c := range conds {
				parts = append(parts, "("+c.cond+")")
			}
			g.Pf("%sif %s:\n%s    return false\n", ind, strings.Join(parts, " or "), ind)
			return true
		}
		for i, c := range conds {
			kw := "if"
			if i > 0 {
				kw = "elif"
			}
			g.Pf("%s%s %s:\n%s    %s = %s\n%s    report.clamped += 1\n", ind, kw, c.cond, ind, expr, c.set, ind)
		}
		return true
	}

	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			if fixedBoundNeededStruct(r, chk) {
				requireHome(g, r.Name)
				if chk {
					g.Pf("%sif not %sFixedCheckBody(%s):\n%s    return false\n", ind, r.Name, expr, ind)
				} else {
					g.Pf("%s%sFixedClampBody(%s, report)\n", ind, r.Name, expr)
				}
				return true
			}
			return false
		case *ir.Union:
			g.TypeRef(r.Name, g.Unit.TableUnions[r.Name] != nil)
			if chk {
				g.Pf("%sif schema_rt.ult(%d, int(%s.Type)):\n%s    return false\n", ind, r.Max, expr, ind)
			} else {
				g.Pf("%sif schema_rt.ult(%d, int(%s.Type)):\n%s    %s.Type = %s\n%s    report.clamped += 1\n",
					ind, r.Max, expr, ind, expr, lobster.VariantName(r.Name+"Type", "None"), ind)
			}
			if !fixedArmNeeded(r, chk) {
				return true
			}
			for i, v := range r.Variants {
				if v.F == nil || !fixedBoundNeeded(v.F, chk) {
					continue
				}
				arm := *v.F
				arm.Name = v.Name
				g.Pf("%sif int(%s.Type) == %d:\n", ind, expr, i+1)
				if !emitFixedBoundField(g, &arm, expr, ind+"    ", pass) {
					g.Pf("%s    pass()\n", ind)
				}
			}
			return true
		case *ir.Enum:
			g.TypeRef(r.Name, false)
			if chk {
				for i := range r.Variants {
					if enumVariantDeprecated(r, i) {
						g.Pf("%sif int(%s) == %d:\n%s    return false\n", ind, expr, i+1, ind)
					}
				}
				g.Pf("%sif schema_rt.ult(%d, int(%s)):\n%s    return false\n", ind, r.Max, expr, ind)
			} else {
				g.Pf("%sif schema_rt.ult(%d, int(%s)):\n%s    %s = %s\n%s    report.clamped += 1\n",
					ind, r.Max, expr, ind, expr, lobster.VariantName(r.Name, "None"), ind)
				defVariant := "None"
				if f.Array == ir.ArrayNone && f.KeyEnum == "" && f.HasDefault && f.DefVariant != "" {
					for _, v := range r.Variants {
						if v == f.DefVariant {
							defVariant = v
						}
					}
				}
				for i := range r.Variants {
					if enumVariantDeprecated(r, i) {
						g.Pf("%sif int(%s) == %d:\n%s    %s = %s\n",
							ind, expr, i+1, ind, expr, lobster.VariantName(r.Name, defVariant))
					}
				}
			}
			return true
		}
		return false
	}

	if chk {
		switch f.Type.Kind {
		case ir.TFloat32:
			// the writer's finite rule (docs/SPEC-TABLES.md §3.3): a NaN or an
			// infinity, at the width it is written, is invalid_value
			g.Pf("%sif not schema_rt.finite_f32(%s):\n%s    return false\n", ind, expr, ind)
			return true
		case ir.TFloat64:
			g.Pf("%sif not schema_rt.finite_f64(%s):\n%s    return false\n", ind, expr, ind)
			return true
		case ir.TString:
			length := slotExpr(expr, f, "Length")
			g.Pf("%sif %s < 0 or %s > %d or not schema_rt.fixed_utf8_valid_no_nul(%s, 0, %s):\n%s    return false\n",
				ind, length, length, f.Type.Size, expr, length, ind)
			return true
		case ir.TWString:
			length := slotExpr(expr, f, "Length")
			g.Pf("%sif %s < 0 or %s > %d or schema_rt.fixed_wide_ill_formed(%s, 0, %s):\n%s    return false\n",
				ind, length, length, f.Type.Size, expr, length, ind)
			return true
		case ir.TBytes:
			length := slotExpr(expr, f, "Length")
			g.Pf("%sif %s < 0 or %s > %d:\n%s    return false\n", ind, length, length, f.Type.Size, ind)
			return true
		}
	}

	if !chk && f.HasFloatRange {
		switch f.Type.Kind {
		case ir.TFloat32:
			lo, hi := lobster.FormatFloat32(f.FMin), lobster.FormatFloat32(f.FMax)
			emitCompressedFloatClamp(g, expr, lo, hi, ind)
			return true
		case ir.TFloat64:
			lo, hi := lobster.FormatFloat(f.FMin), lobster.FormatFloat(f.FMax)
			emitCompressedFloatClamp(g, expr, lo, hi, ind)
			return true
		}
	}

	emitted := applyBounds(lobsterScalarRangeBounds(f, expr))
	if b, ok := lobsterScalarWidthBound(f, expr); ok {
		if applyBounds([]lobsterScalarBound{b}) {
			emitted = true
		}
	}
	return emitted
}

// emitCompressedFloatClamp is the bounds pass's compressed-float clamp. A NaN
// or an infinity never reaches it: the finite pass refused the batch first
// (docs/SPEC-TABLES.md §3.3, ruling 1 of 2026-10-27).
func emitCompressedFloatClamp(g *lobster.Gen, expr, lo, hi, ind string) {
	g.Pf("%sif %s < %s:\n%s    %s = %s\n%s    report.clamped += 1\n", ind, expr, lo, ind, expr, lo, ind)
	g.Pf("%selif %s > %s:\n%s    %s = %s\n%s    report.clamped += 1\n", ind, expr, hi, ind, expr, hi, ind)
	g.Pf("%selif %s == 0.0:\n%s    %s = 0.0\n", ind, expr, ind, expr)
}

func enumVariantDeprecated(e *ir.Enum, i int) bool {
	if i < 0 || i >= len(e.VariantTags) {
		return false
	}
	return slices.Contains(e.VariantTags[i], "deprecated")
}

func tableStorageRange(signed bool, bits int) (*big.Int, *big.Int) {
	one := big.NewInt(1)
	if signed {
		hi := new(big.Int).Lsh(one, uint(bits-1))
		return new(big.Int).Neg(hi), new(big.Int).Sub(hi, one)
	}
	return big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(one, uint(bits)), one)
}

func tableClampEnds(f *ir.Field, widthBytes int) (low, high bool) {
	signed := ir.TableKindSigned(ir.TableScalarKind(f))
	lo, hi := tableStorageRange(signed, widthBytes*8)
	rlo, rhi, ok := ir.TableRawRange(f)
	if !ok {
		return false, false
	}
	return rlo.Cmp(lo) > 0, rhi.Cmp(hi) < 0
}

func lobsterScalarRangeBounds(f *ir.Field, expr string) []lobsterScalarBound {
	switch {
	case f.Type.Kind == ir.TFloat32 && f.HasFloatRange:
		lo, hi := lobster.FormatFloat32(f.FMin), lobster.FormatFloat32(f.FMax)
		return []lobsterScalarBound{
			{expr + " != " + expr, lo},
			{expr + " < " + lo, lo},
			{expr + " > " + hi, hi},
		}
	case f.Type.Kind == ir.TFloat64 && f.HasFloatRange:
		lo, hi := lobster.FormatFloat(f.FMin), lobster.FormatFloat(f.FMax)
		return []lobsterScalarBound{
			{expr + " != " + expr, lo},
			{expr + " < " + lo, lo},
			{expr + " > " + hi, hi},
		}
	case f.Type.Kind == ir.TNamed:
		return nil
	}
	rlo, rhi, ok := ir.TableRawRange(f)
	if !ok {
		return nil
	}
	width := int(ir.TableFixedStorageBytes(f.Type))
	signed := ir.TableKindSigned(ir.TableScalarKind(f))
	low, high := tableClampEnds(f, width)
	var out []lobsterScalarBound
	if width == 16 {
		lo, hi := lobster.Int128Lit(rlo), lobster.Int128Lit(rhi)
		ltFn := "schema_rt.u128_lt"
		if signed {
			ltFn = "schema_rt.i128_lt"
		}
		if low {
			out = append(out, lobsterScalarBound{fmt.Sprintf("%s(%s, %s)", ltFn, expr, lo), lo})
		}
		if high {
			out = append(out, lobsterScalarBound{fmt.Sprintf("%s(%s, %s)", ltFn, hi, expr), hi})
		}
		return out
	}
	lo, hi := lobster.IntLit(rlo), lobster.IntLit(rhi)
	if signed {
		if low {
			out = append(out, lobsterScalarBound{fmt.Sprintf("%s < %s", expr, lo), lo})
		}
		if high {
			out = append(out, lobsterScalarBound{fmt.Sprintf("%s > %s", expr, hi), hi})
		}
		return out
	}
	if low {
		out = append(out, lobsterScalarBound{fmt.Sprintf("schema_rt.ult(%s, %s)", expr, lo), lo})
	}
	if high {
		out = append(out, lobsterScalarBound{fmt.Sprintf("schema_rt.ult(%s, %s)", hi, expr), hi})
	}
	return out
}

func lobsterScalarWidthBound(f *ir.Field, expr string) (lobsterScalarBound, bool) {
	if f.Type.Kind != ir.TBits || int64(f.Type.Width) >= 8*ir.TableFixedStorageBytes(f.Type) {
		return lobsterScalarBound{}, false
	}
	maxv := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
	lit := lobster.IntLit(maxv)
	return lobsterScalarBound{fmt.Sprintf("schema_rt.ult(%s, %s)", lit, expr), lit}, true
}

// ---- Canonical Flat Body Prefill Image & Destination Table ------------------

type lobsterFixedDstSpec struct {
	Dst     int64
	Stride  int64
	Aux     int64
	Counted int
	Arg     int
	Meta    int
	Note    string
}

func fixedDstArg(flavour int, dep bool) int {
	a := flavour
	if dep {
		a |= 0x80
	}
	return a
}

func buildLobsterFixedDst(st *ir.Struct) []lobsterFixedDstSpec {
	var out []lobsterFixedDstSpec
	push := func(spec lobsterFixedDstSpec) {
		out = append(out, spec)
	}
	var walkField func(f *ir.Field, off int64)
	var walkPayload func(f *ir.Field, off int64, dep bool, note string)
	var walkElement func(f *ir.Field, off int64, dep bool, note string)

	walkEnum := func(e *ir.Enum, off int64, dep bool, note string) {
		a := fixedDstArg(0, dep)
		push(lobsterFixedDstSpec{Dst: off, Arg: a, Meta: a, Note: note})
		for _, v := range e.Variants {
			push(lobsterFixedDstSpec{Note: v})
		}
	}

	walkElement = func(f *ir.Field, off int64, dep bool, note string) {
		a := fixedDstArg(0, dep)
		if f.Type.Kind == ir.TNamed {
			switch r := f.Type.Ref.(type) {
			case *ir.Struct:
				push(lobsterFixedDstSpec{Dst: off, Arg: a, Meta: a, Note: note})
				subOff := int64(0)
				for _, sub := range r.Fields {
					walkField(sub, subOff)
					subOff += ir.TableFixedFieldBytes(sub)
				}
				return
			case *ir.Union:
				push(lobsterFixedDstSpec{Dst: off, Aux: off, Arg: a, Meta: a, Note: note})
				tag := ir.TableFixedUnionTagBytes(r)
				for _, v := range r.Variants {
					if v.F == nil {
						push(lobsterFixedDstSpec{Note: v.Name})
						continue
					}
					af := *v.F
					af.Name = v.Name
					walkPayload(&af, tag, af.Deprecated, v.Name)
				}
				return
			case *ir.Enum:
				walkEnum(r, off, dep, note)
				return
			}
		}
		push(lobsterFixedDstSpec{Dst: off, Arg: a, Meta: a, Note: note})
	}

	walkPayload = func(f *ir.Field, off int64, dep bool, note string) {
		a := fixedDstArg(0, dep)
		switch {
		case f.KeyEnumRef != nil:
			elem := ir.TableFixedElementBytes(f)
			push(lobsterFixedDstSpec{Dst: off, Stride: elem, Arg: a, Meta: a, Note: note})
			walkEnum(f.KeyEnumRef, 0, false, f.KeyEnum)
			walkElement(f, 0, false, "element")
		case f.Array == ir.ArrayFixed:
			elem := ir.TableFixedElementBytes(f)
			push(lobsterFixedDstSpec{Dst: off, Stride: elem, Arg: a, Meta: a, Note: note})
			walkElement(f, 0, false, "element")
		case f.Array == ir.ArrayCounted:
			elem := ir.TableFixedElementBytes(f)
			push(lobsterFixedDstSpec{Dst: off + 4, Stride: elem, Aux: off, Counted: 1, Arg: a, Meta: a, Note: note})
			walkElement(f, 0, false, "element")
		case f.Type.Kind == ir.TBytes:
			ab := fixedDstArg(3, dep)
			push(lobsterFixedDstSpec{Dst: off + 4, Stride: 1, Aux: off, Counted: 1, Arg: ab, Meta: ab, Note: note})
			push(lobsterFixedDstSpec{Note: "u8"})
		case f.Type.Kind == ir.TString:
			as := fixedDstArg(1, dep)
			push(lobsterFixedDstSpec{Dst: off, Aux: off + 4, Arg: as, Meta: as, Note: note})
		case f.Type.Kind == ir.TWString:
			aw := fixedDstArg(2, dep)
			push(lobsterFixedDstSpec{Dst: off, Aux: off + 4, Arg: aw, Meta: aw, Note: note})
		default:
			walkElement(f, off, dep, note)
		}
	}

	walkField = func(f *ir.Field, off int64) {
		dep := f.Deprecated
		a := fixedDstArg(0, dep)
		if f.Type.Optional {
			push(lobsterFixedDstSpec{Aux: off, Arg: a, Meta: a, Note: f.Name + " ?"})
			walkPayload(f, off+ir.TableFixedPresentBytes, dep, f.Name)
			return
		}
		walkPayload(f, off, dep, f.Name)
	}

	push(lobsterFixedDstSpec{Note: st.Name})
	off := int64(0)
	for _, f := range st.Fields {
		walkField(f, off)
		off += ir.TableFixedFieldBytes(f)
	}
	return out
}

func fixedResetImageBytes(st *ir.Struct) []byte {
	out := make([]byte, ir.TableFixedTypeBytes(st))
	fixedWriteResetInto(out, 0, st)
	return out
}

func fixedWriteResetInto(out []byte, base int64, st *ir.Struct) {
	at := base
	for _, f := range st.Fields {
		fixedWriteResetField(out, at, f)
		at += ir.TableFixedFieldBytes(f)
	}
}

func fixedWriteResetField(out []byte, at int64, f *ir.Field) {
	if f.Type.Optional {
		at += ir.TableFixedPresentBytes
	}
	fixedWriteResetPayload(out, at, f)
}

func fixedWriteResetPayload(out []byte, at int64, f *ir.Field) {
	switch {
	case f.KeyEnumRef != nil:
		elem := ir.TableFixedElementBytes(f)
		ef := *f
		ef.KeyEnum, ef.KeyEnumRef, ef.HasDefault = "", nil, false
		for i := range f.KeyEnumRef.Max {
			fixedWriteResetElement(out, at+i*elem, &ef)
		}
	case f.Array == ir.ArrayFixed:
		elem := ir.TableFixedElementBytes(f)
		ef := *f
		ef.Array, ef.HasDefault = ir.ArrayNone, false
		for i := range f.ArrayBound {
			fixedWriteResetElement(out, at+i*elem, &ef)
		}
	case f.Array == ir.ArrayCounted:
		fixedPutIntLE(out, at, 4, big.NewInt(f.BornCount()))
		elem := ir.TableFixedElementBytes(f)
		ef := *f
		ef.Array, ef.HasDefault = ir.ArrayNone, false
		for i := range f.ArrayBound {
			fixedWriteResetElement(out, at+ir.TableFixedCountBytes+i*elem, &ef)
		}
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		if f.HasDefault && len(f.DefBytes) > 0 {
			n := min(int64(len(f.DefBytes)), f.Type.Size)
			fixedPutIntLE(out, at, 4, big.NewInt(n))
			copy(out[at+ir.TableFixedCountBytes:at+ir.TableFixedCountBytes+n], f.DefBytes[:n])
		}
	case f.Type.Kind == ir.TWString:
		// wstring defaults prefill empty (0 code units).
	default:
		fixedWriteResetElement(out, at, f)
	}
}

func fixedWriteResetElement(out []byte, at int64, f *ir.Field) {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			fixedWriteResetInto(out, at, r)
			return
		case *ir.Union:
			tag := ir.TableFixedUnionTagBytes(r)
			for _, v := range r.Variants {
				if v.F == nil {
					continue
				}
				af := *v.F
				af.Name = v.Name
				fixedWriteResetPayload(out, at+tag, &af)
			}
			return
		case *ir.Enum:
			if f.HasDefault && f.DefVariant != "" {
				for i, v := range r.Variants {
					if v == f.DefVariant {
						fixedPutIntLE(out, at, int64(r.StorageBits/8), big.NewInt(int64(i+1)))
						break
					}
				}
			}
			return
		case *ir.Flags:
			if f.HasDefault && f.DefInt != nil {
				fixedPutIntLE(out, at, 8, f.DefInt)
			}
			return
		}
	}
	if !f.HasDefault {
		return
	}
	w := ir.TableFixedStorageBytes(f.Type)
	switch f.Type.Kind {
	case ir.TBool:
		if f.DefBool {
			out[at] = 1
		}
	case ir.TFloat32:
		fixedPutIntLE(out, at, 4, new(big.Int).SetUint64(uint64(math.Float32bits(float32(f.DefFloat)))))
	case ir.TFloat64:
		fixedPutIntLE(out, at, 8, new(big.Int).SetUint64(math.Float64bits(f.DefFloat)))
	default:
		if f.DefInt != nil {
			fixedPutIntLE(out, at, w, f.DefInt)
		}
	}
}

func fixedPutIntLE(out []byte, at, width int64, v *big.Int) {
	if width <= 0 || v == nil {
		return
	}
	n := new(big.Int).Set(v)
	if n.Sign() < 0 {
		mod := new(big.Int).Lsh(big.NewInt(1), uint(width*8))
		n.Add(n, mod)
	}
	mask := big.NewInt(0xff)
	tmp := new(big.Int)
	for i := range width {
		tmp.Rsh(n, uint(8*i))
		tmp.And(tmp, mask)
		out[at+i] = byte(tmp.Uint64())
	}
}

// ---- Root Evolution Surface (<Name>FixedLoad, <Name>FixedLoadTolerant) ------

func emitFixedEvolveRoot(g *lobster.Gen, st *ir.Struct, older []FixedLineageEntry) {
	name := fixedName(st)
	entries := ir.TableFixedWalkRoot(st)
	layout := ir.TableFixedLayoutBytes(entries)
	hash := ir.TableFixedLayoutHash(layout, st)
	bodyBytes := ir.TableFixedTypeBytes(st)
	recordBytes := 8 + bodyBytes

	own := FixedLineageEntry{
		Wire:   hash,
		Layout: layout,
		Record: recordBytes,
	}
	known := append([]FixedLineageEntry(nil), older...)
	if len(known) == 0 || known[len(known)-1].Wire != own.Wire {
		known = append(known, own)
	}
	floor := 0
	for i, e := range known {
		if e.Retired {
			floor = i + 1
		}
	}

	dstSpecs := buildLobsterFixedDst(st)
	g.Pf("let %sBodyBytes = %d\n", name, bodyBytes)
	g.Pf("let %sDst:[schema_rt.TableFixedDst] = [\n", name)
	for i, d := range dstSpecs {
		sep := ","
		if i == len(dstSpecs)-1 {
			sep = ""
		}
		g.Pf("    schema_rt.TableFixedDst { dst: %d, stride: %d, aux: %d, counted: %d, arg: %d, meta: %d }%s // %s\n",
			d.Dst, d.Stride, d.Aux, d.Counted, d.Arg, d.Meta, sep, d.Note)
	}
	g.Pf("]\n\n")

	g.Pf("def %sBuildPlan() -> schema_rt.TableFixedPlan:\n", name)
	g.Pf("    let scratch:[schema_rt.TableFixedEntry] = []\n")
	g.Pf("    %sLeaves(scratch, 0, 0)\n", name)
	g.Pf("    return schema_rt.fixed_build_plan(scratch)\n\n")
	g.Pf("let %sPlan = %sBuildPlan()\n\n", name, name)

	g.Pf("let %sKnown:[schema_rt.TableFixedKnownLayout] = [\n", name)
	for i, e := range known {
		sep := ","
		if i == len(known)-1 {
			sep = ""
		}
		g.Pf("    schema_rt.TableFixedKnownLayout {\n")
		g.Pf("        hash: %s,\n", idLit(e.Wire))
		g.Pf("        layout: %s,\n", lobster.StringLit(e.Layout))
		g.Pf("        layout_bytes: %d,\n", len(e.Layout))
		g.Pf("        record: %d\n", e.Record)
		g.Pf("    }%s\n", sep)
	}
	g.Pf("]\n\n")

	g.Pf("let %sFloor = %d\n", name, floor)
	g.Pf("let %sLineagePlans = schema_rt.fixed_lineage_plans(%sKnown, %sLayoutBytes(), %sDst, %sHash)\n",
		name, name, name, name, name)

	resetImg := fixedResetImageBytes(st)
	g.Pf("let %sResetImage = %s\n", name, lobster.StringLit(resetImg))
	g.Pf("let %sHoles = schema_rt.fixed_holes(%sPlan.entries, %sPlan.count, %d)\n",
		name, name, name, len(resetImg))
	g.Pf("let %sLineageHoles = schema_rt.fixed_lineage_holes(%sLineagePlans, %d)\n",
		name, name, len(resetImg))
	g.Pf("let %sPlanMax = schema_rt.fixed_plan_max(%sPlan, %sLineagePlans)\n\n",
		name, name, name)

	trailer := ""
	if st.Integrity != "" {
		trailer = " + 4"
	}
	g.Pf("def %sMeasure(count:int) -> int:\n", name)
	g.Pf("    return schema_rt.TableFixedHeaderBytes + 4 + %d + count * (8 + %d)%s\n\n",
		len(layout), bodyBytes, trailer)

	// Checked writer: <Name>FixedSaveChecked(values, buffer) -> int, string, string
	g.Pf("def %sSaveChecked(values:[%s], buffer:string) -> int, string, string:\n", name, st.Name)
	g.Pf("    let need = %sMeasure(length(values))\n", name)
	g.Pf("    if length(buffer) < need:\n")
	g.Pf("        return -1, buffer, schema_rt.TableRefuseTruncated\n")
	if fixedCheckNeededStruct(st, map[string]bool{}) {
		g.Pf("    for(values) v:\n")
		g.Pf("        if not %sFixedCheckBody(v):\n", st.Name)
		g.Pf("            return -1, buffer, schema_rt.TableRefuseInvalidValue\n")
	}
	g.Pf("    let wire = %sSave(values)\n", name)
	g.Pf("    let out = write_substring(buffer, 0, wire, false)\n")
	if st.Integrity != "" {
		g.Pf("    let sealed, out2 = schema_rt.fixed_integrity_seal(out, need - 4)\n")
		g.Pf("    return sealed, out2, \"\"\n\n")
	} else {
		g.Pf("    return need, out, \"\"\n\n")
	}

	// Strict 4-arg <Name>FixedLoad(values, data, plan, report) -> int
	g.Pf("def %sLoad(values:[%s], data:string, plan:[schema_rt.TableFixedEntry], report:schema_rt.TableReport?) -> int:\n", name, st.Name)
	g.Pf("    let r = if report: report else: schema_rt.TableReport {}\n")
	g.Pf("    schema_rt.fixed_reset_report(r)\n")
	g.Pf("    let n = %sLoadTolerant(values, data, plan, r)\n", name)
	g.Pf("    if n < 0:\n")
	g.Pf("        return -1\n")
	g.Pf("    if schema_rt.fixed_report_err(r) != \"\":\n")
	g.Pf("        for(n) k:\n")
	g.Pf("            %sReset(values[k])\n", st.Name)
	g.Pf("        return -1\n")
	g.Pf("    return n\n\n")

	// Tolerant 4-arg <Name>FixedLoadTolerant(values, data, plan, report) -> int
	g.Pf("def %sLoadTolerant(values:[%s], data_in:string, plan:[schema_rt.TableFixedEntry], report_in:schema_rt.TableReport?) -> int:\n", name, st.Name)
	g.Pf("    let report = if report_in: report_in else: schema_rt.TableReport {}\n")
	g.Pf("    schema_rt.fixed_reset_report(report)\n")
	if st.Integrity != "" {
		g.Pf("    var data = data_in\n")
	} else {
		g.Pf("    let data = data_in\n")
	}
	g.Pf("    if length(data) < 1:\n")
	g.Pf("        report.malformed = true\n")
	g.Pf("        report.reason = schema_rt.TableRefuseTruncated\n")
	g.Pf("        return -1\n")
	g.Pf("    let form = read_uint8_le(data, 0)\n")
	g.Pf("    if form != schema_rt.TableFixedForm:\n")
	g.Pf("        if form == 1:\n")
	g.Pf("            return schema_rt.fixed_refuse(report, schema_rt.TableRefusePreviousForm)\n")
	g.Pf("        if form == 2:\n")
	g.Pf("            return schema_rt.fixed_refuse(report, schema_rt.TableRefuseMessageFormAsFile)\n")
	g.Pf("        return schema_rt.fixed_refuse(report, schema_rt.TableRefuseNewerForm)\n")
	if st.Integrity != "" {
		g.Pf("    if not schema_rt.fixed_integrity_holds(data):\n")
		g.Pf("        report.verdict = schema_rt.TableOpenDamaged\n")
		g.Pf("        report.malformed = true\n")
		g.Pf("        report.reason = schema_rt.TableRefuseIntegrityMismatch\n")
		g.Pf("        return -1\n")
		g.Pf("    data = substring(data, 0, length(data) - 4)\n")
	}
	g.Pf("    if length(data) < schema_rt.TableFixedHeaderBytes + 4:\n")
	g.Pf("        report.malformed = true\n")
	g.Pf("        report.reason = schema_rt.TableRefuseTruncated\n")
	g.Pf("        return -1\n")
	g.Pf("    var r_idx = 1\n")
	g.Pf("    while r_idx < schema_rt.TableFixedHashAt:\n")
	g.Pf("        if read_uint8_le(data, r_idx) != 0:\n")
	g.Pf("            report.malformed = true\n")
	g.Pf("            report.reason = schema_rt.TableRefuseReservedNotZero\n")
	g.Pf("            return -1\n")
	g.Pf("        r_idx += 1\n")
	g.Pf("    let layout_bytes = read_uint32_le(data, schema_rt.TableFixedHeaderBytes)\n")
	g.Pf("    if layout_bytes < 0 or layout_bytes + schema_rt.TableFixedHeaderBytes + 4 > length(data):\n")
	g.Pf("        return schema_rt.fixed_refuse(report, schema_rt.TableRefuseLayoutMalformed)\n")
	g.Pf("    let hash = read_uint64_le(data, schema_rt.TableFixedHashAt)\n")
	g.Pf("    let pick = schema_rt.fixed_select(%sKnown, hash)\n", name)
	g.Pf("    if pick < 0:\n")
	g.Pf("        return schema_rt.fixed_refuse_hash(report, schema_rt.TableRefuseLayoutUnknown, hash)\n")
	g.Pf("    if pick < %sFloor:\n", name)
	g.Pf("        return schema_rt.fixed_refuse_hash(report, schema_rt.TableRefuseLayoutUnsupported, hash)\n")
	g.Pf("    let known = %sKnown[pick]\n", name)
	g.Pf("    if layout_bytes != known.layout_bytes:\n")
	g.Pf("        return schema_rt.fixed_refuse(report, schema_rt.TableRefuseLayoutMalformed)\n")
	g.Pf("    for(layout_bytes) l_i:\n")
	g.Pf("        if read_uint8_le(data, schema_rt.TableFixedHeaderBytes + 4 + l_i) != read_uint8_le(known.layout, l_i):\n")
	g.Pf("            return schema_rt.fixed_refuse(report, schema_rt.TableRefuseLayoutMalformed)\n")
	g.Pf("    var entries = %sPlan.entries\n", name)
	g.Pf("    var entry_count = %sPlan.count\n", name)
	g.Pf("    var holes = %sHoles\n", name)
	g.Pf("    let record_bytes = known.record\n")
	g.Pf("    var census_unknown = 0\n")
	g.Pf("    var census_kind = 0\n")
	g.Pf("    if hash != %sHash:\n", name)
	g.Pf("        let lane = %sLineagePlans[pick]\n", name)
	g.Pf("        if lane.why != \"\":\n")
	g.Pf("            return schema_rt.fixed_refuse(report, lane.why)\n")
	g.Pf("        entries = lane.entries\n")
	g.Pf("        entry_count = lane.count\n")
	g.Pf("        holes = %sLineageHoles[pick]\n", name)
	g.Pf("        census_unknown = lane.unknown\n")
	g.Pf("        census_kind = lane.kind_mismatch\n")
	g.Pf("    if entry_count > length(plan):\n")
	g.Pf("        return schema_rt.fixed_refuse(report, schema_rt.TableRefusePlanTooLarge)\n")
	g.Pf("    let records_start = schema_rt.TableFixedHeaderBytes + 4 + layout_bytes\n")
	g.Pf("    let rest = length(data) - records_start\n")
	g.Pf("    if record_bytes <= 8 or (rest %% record_bytes) != 0:\n")
	g.Pf("        report.malformed = true\n")
	g.Pf("        report.reason = schema_rt.TableRefuseTruncated\n")
	g.Pf("        return -1\n")
	g.Pf("    let n = rest / record_bytes\n")
	g.Pf("    if n > length(values):\n")
	g.Pf("        return schema_rt.fixed_refuse(report, schema_rt.TableRefuseBatchTooLarge)\n")
	g.Pf("    for(n) k:\n")
	g.Pf("        let rec_at = records_start + k * record_bytes\n")
	g.Pf("        if read_uint64_le(data, rec_at) != hash:\n")
	g.Pf("            return schema_rt.fixed_refuse(report, schema_rt.TableRefuseNoLayout)\n")
	g.Pf("        let why = schema_rt.fixed_text_refusal(entries, entry_count, data, rec_at + 8)\n")
	g.Pf("        if why != \"\":\n")
	g.Pf("            return schema_rt.fixed_refuse(report, why)\n")
	g.Pf("    for(n) k:\n")
	g.Pf("        let rec_at = records_start + k * record_bytes\n")
	g.Pf("        var img = schema_rt.fixed_prefill_holes(schema_rt.zeros(%d), %sResetImage, holes)\n", bodyBytes, name)
	g.Pf("        let img2, why = schema_rt.fixed_run(entries, entry_count, data, rec_at + 8, img, report)\n")
	g.Pf("        if why != \"\":\n")
	g.Pf("            return schema_rt.fixed_refuse(report, why)\n")
	g.Pf("        img = img2\n")
	g.Pf("        %sScatter(img, 0, values[k])\n", name)
	if fixedFiniteNeededStruct(st, map[string]bool{}) {
		// A FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3): the finite pass runs
		// first, before the bounds pass, over the storage just written; a NaN
		// or an infinity refuses the whole batch as non_finite_float, the
		// tolerant read included, and every record this call wrote goes back
		// to its defaults.
		g.Pf("        let nf = %sFixedFiniteBody(values[k])\n", st.Name)
		g.Pf("        if nf != \"\":\n")
		g.Pf("            for(k + 1) j:\n")
		g.Pf("                %sReset(values[j])\n", st.Name)
		g.Pf("            return schema_rt.fixed_refuse_non_finite(report, nf)\n")
	}
	g.Pf("        schema_rt.fixed_forged_bounds(entries, entry_count, data, rec_at + 8, report)\n")
	if ir.TableFixedClampNeeded(st) {
		g.Pf("        %sFixedClamp(values[k], report)\n", st.Name)
	}
	g.Pf("    report.unknown += census_unknown\n")
	g.Pf("    report.kind_mismatch += census_kind\n")
	g.Pf("    return n\n\n")
}

// ---- The Read-Side Finite Pass ----------------------------------------------
//
// A FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3, the same rule as
// packets; finite-tables #2226's Go shape):
// every fixed read refuses a NaN or an infinity as non_finite_float, naming
// the field, tolerant and strict alike. It runs over STORAGE after the record
// lands, so one pass covers the identity copy and a lineage plan's widenF,
// whose ordinary conversion keeps a NaN a NaN and an infinity an infinity. It
// walks only what a read can have written: a counted array's live elements,
// an optional's payload when present, a union's set arm.

func fixedFiniteNeededStruct(st *ir.Struct, seen map[string]bool) bool {
	if seen[st.Name] {
		return false
	}
	seen[st.Name] = true
	for _, f := range st.Fields {
		if fixedFiniteNeededField(f, seen) {
			return true
		}
	}
	return false
}

func fixedFiniteNeededField(f *ir.Field, seen map[string]bool) bool {
	switch f.Type.Kind {
	case ir.TFloat32, ir.TFloat64:
		return true
	case ir.TNamed:
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return fixedFiniteNeededStruct(r, seen)
		case *ir.Union:
			for _, v := range r.Variants {
				if v.F != nil && fixedFiniteNeededField(v.F, seen) {
					return true
				}
			}
		}
	}
	return false
}

// emitFixedFiniteBodyFn emits <T>FixedFiniteBody: the first non-finite float's
// <Owner>.<field> name, or "" for a record whose floats are all finite. A type
// that holds no float gets no function (§2.2's zero-cost rule).
func emitFixedFiniteBodyFn(g *lobster.Gen, st *ir.Struct) {
	if !fixedFiniteNeededStruct(st, map[string]bool{}) {
		return
	}
	g.Pf("// %sFixedFiniteBody is %s's finite rule (docs/SPEC-TABLES.md §3.3): the\n", st.Name, st.Name)
	g.Pf("// first NaN or infinity, named <Owner>.<field>, or \"\".\n")
	g.Pf("def %sFixedFiniteBody(v:%s) -> string:\n", st.Name, st.Name)
	for _, f := range st.Fields {
		emitFixedFiniteField(g, f, st.Name, "v", "    ")
	}
	g.Pf("    return \"\"\n\n")
}

func emitFixedFiniteField(g *lobster.Gen, f *ir.Field, owner, val, ind string) {
	if !fixedFiniteNeededField(f, map[string]bool{}) {
		return
	}
	if f.Type.Optional {
		g.Pf("%sif %s.%s:\n", ind, val, ir.GoExportName(f.Name+"_present"))
		emitFixedFinitePayload(g, f, owner, val, ind+"    ")
		return
	}
	emitFixedFinitePayload(g, f, owner, val, ind)
}

func emitFixedFinitePayload(g *lobster.Gen, f *ir.Field, owner, val, ind string) {
	loop := func(count string) {
		iv := fmt.Sprintf("i_nf_%s", ir.GoExportName(f.Name))
		g.Pf("%sfor(%s) %s:\n", ind, count, iv)
		emitFixedFiniteElement(g, f, owner, fmt.Sprintf("%s.%s[%s]", val, ir.GoExportName(f.Name), iv), ind+"    ")
	}
	switch {
	case f.KeyEnumRef != nil:
		loop(strconv.FormatInt(f.KeyEnumRef.Max, 10))
	case f.Array == ir.ArrayFixed:
		loop(strconv.FormatInt(f.ArrayBound, 10))
	case f.Array == ir.ArrayCounted:
		// the count was refused past its bound before the record landed;
		// min keeps the walk inside storage anyway
		cnt := fmt.Sprintf("%s.%sCount", val, ir.GoExportName(f.Name))
		loop(fmt.Sprintf("min(%s, %d)", cnt, f.ArrayBound))
	default:
		emitFixedFiniteElement(g, f, owner, val+"."+ir.GoExportName(f.Name), ind)
	}
}

func emitFixedFiniteElement(g *lobster.Gen, f *ir.Field, owner, expr, ind string) {
	switch f.Type.Kind {
	case ir.TFloat32:
		g.Pf("%sif not schema_rt.finite_f32(%s):\n%s    return %s\n", ind, expr, ind, lobster.StringLit([]byte(owner+"."+f.Name)))
		return
	case ir.TFloat64:
		g.Pf("%sif not schema_rt.finite_f64(%s):\n%s    return %s\n", ind, expr, ind, lobster.StringLit([]byte(owner+"."+f.Name)))
		return
	case ir.TNamed:
	default:
		return
	}
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		if fixedFiniteNeededStruct(r, map[string]bool{}) {
			requireHome(g, r.Name)
			iv := fmt.Sprintf("nf_%s", ir.GoExportName(f.Name))
			g.Pf("%slet %s = %sFixedFiniteBody(%s)\n", ind, iv, r.Name, expr)
			g.Pf("%sif %s != \"\":\n%s    return %s\n", ind, iv, ind, iv)
		}
	case *ir.Union:
		for i, v := range r.Variants {
			if v.F == nil || !fixedFiniteNeededField(v.F, map[string]bool{}) {
				continue
			}
			arm := *v.F
			arm.Name = v.Name
			g.Pf("%sif int(%s.Type) == %d:\n", ind, expr, i+1)
			emitFixedFiniteField(g, &arm, r.Name, expr, ind+"    ")
		}
	}
}
