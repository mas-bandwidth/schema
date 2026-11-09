// THE LINEAGE AS STATIC DATA (docs/FIXED-FORM-ALGORITHM.md §5.2, COMPILE(lock, T)).
//
// A fixed table reads BACKWARD and never forward: a file is matched on the
// eight bytes of its header's hash against a lineage the BUILD laid down, and
// nothing parses a stranger's layout, on any path. So this backend needs one
// input it never had: per fixed table, the locked layouts, OLDEST FIRST, and
// the current one LAST. That input is the lock's, handed down by the driver
// (compiler/lineage.go) as DATA; a build with no lock for a unit hands
// nothing, which leaves a table with the single entry it can always compute:
// its own.
package squirreltable

import (
	"fmt"
	"sync"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// FixedLineageEntry is ONE locked layout of one fixed table: the five facts
// §5.2's table asks the lock for, and the two the operator writes.
type FixedLineageEntry struct {
	Wire    uint64
	Layout  []byte
	Record  int64
	Retired bool
	Reason  string
	// Ranges are the WRITER's ranged integer leaves, on the raw scale
	// (docs/FIXED-FORM-ALGORITHM.md §4.6): a record read under this entry's
	// compiled plan refuses out_of_range past them, as it does past this
	// reader's own ranges.
	Ranges []ir.TableFixedWriterRange
}

// FixedLineageOf computes the entry a unit's OWN build locks for table `name`:
// its layout bytes, the wire hash over them, and its record size. It is what a
// lock records at commit, and it is how a test states an older entry without a
// lock.
func FixedLineageOf(u *ir.Unit, name string) (FixedLineageEntry, bool) {
	for _, st := range ir.TableFixedFormRoots(u) {
		if st.Name != name || fixedRefusal(st) != "" || !fixedSupported(st) {
			continue
		}
		w := fixedWalkRoot(st)
		layout := fixedLayoutBytes(w.entries)
		return FixedLineageEntry{
			Wire:   ir.TableFixedLayoutHash(layout, st),
			Layout: layout,
			Record: int64(fixedHashBytes) + fixedTypeBytes(st),
			Ranges: ir.TableFixedWriterRanges(st),
		}, true
	}
	return FixedLineageEntry{}, false
}

// The lineage rides a package registry keyed by the unit, not a field on
// Module (which lives in squirrelreltable.go, the shared registrar this card
// does not edit): GenerateLineage deposits it for the duration of the call and
// the fixed-form surface reads it back by the unit it is emitting. A mutex
// keeps concurrent builds of different units apart.
var (
	lineageMu     sync.Mutex
	lineageByUnit = map[*ir.Unit]map[string][]FixedLineageEntry{}
)

func lineageOf(u *ir.Unit) map[string][]FixedLineageEntry {
	lineageMu.Lock()
	defer lineageMu.Unlock()
	return lineageByUnit[u]
}

// GenerateLineage is the second entry point of §5.9 #1: the driver read the
// unit's lock once (compiler/lineage.go) and hands the lineage down as DATA,
// so this backend opens no file. A nil lineage is the no-lock case and keeps
// today's reader.
func GenerateLineage(u *ir.Unit, lineage map[string][]FixedLineageEntry) (map[string][]byte, error) {
	lineageMu.Lock()
	prev, had := lineageByUnit[u]
	if lineage == nil {
		delete(lineageByUnit, u)
	} else {
		lineageByUnit[u] = lineage
	}
	lineageMu.Unlock()
	defer func() {
		lineageMu.Lock()
		if had {
			lineageByUnit[u] = prev
		} else {
			delete(lineageByUnit, u)
		}
		lineageMu.Unlock()
	}()
	return Generate(u)
}

// generateLineage is the lineage the emitter lays down for one table: the
// entries the build handed it, OLDEST FIRST, with the table's OWN entry
// appended last when the lineage does not already end on it. The floor is 1 +
// the highest retired index, or 0 when none is (§5.2).
func generateLineage(handed []FixedLineageEntry, own FixedLineageEntry) ([]FixedLineageEntry, int) {
	entries := append([]FixedLineageEntry(nil), handed...)
	last := len(entries) - 1
	if last < 0 || entries[last].Wire != own.Wire {
		entries = append(entries, own)
	}
	floor := 0
	for i, e := range entries {
		if e.Retired {
			floor = i + 1
		}
	}
	return entries, floor
}

// fixedLayoutEntries parses a lock's layout bytes — a u32 count and a run of
// seventeen-byte entries — into the same rows the walk wrote (§1, §5.2).
func fixedLayoutEntries(b []byte) ([]fixedLayoutEntry, bool) {
	if len(b) < 4 {
		return nil, false
	}
	count := int(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24)
	if count <= 0 || 4+count*fixedEntryBytes != len(b) {
		return nil, false
	}
	out := make([]fixedLayoutEntry, count)
	for i := range out {
		at := 4 + i*fixedEntryBytes
		e := b[at:]
		out[i] = fixedLayoutEntry{
			id:       uint64(e[0]) | uint64(e[1])<<8 | uint64(e[2])<<16 | uint64(e[3])<<24 | uint64(e[4])<<32 | uint64(e[5])<<40 | uint64(e[6])<<48 | uint64(e[7])<<56,
			kind:     int(e[8]),
			size:     int64(uint32(e[9]) | uint32(e[10])<<8 | uint32(e[11])<<16 | uint32(e[12])<<24),
			children: int(uint32(e[13]) | uint32(e[14])<<8 | uint32(e[15])<<16 | uint32(e[16])<<24),
		}
	}
	return out, true
}

// fixedPlanWidens is §4.2's ladder: an integer into a wider integer, and f32
// into f64. A pair off every ladder is the kind mismatch, which leaves the
// field at its default and is never reinterpreted.
func fixedPlanWidens(src, dst int) bool {
	ints := func(k int) bool { return k >= ir.TableKindI8 && k <= ir.TableKindU64 }
	if ints(src) && ints(dst) {
		return true
	}
	return src == ir.TableKindF32 && dst == ir.TableKindF64
}

// fixedPlanLeafWidens reports whether one planned leaf reads at a narrower kind
// than the field it lands in, the move the load report counts as widened.
func fixedPlanLeafWidens(f *ir.Field, node *fixedLayoutNode) bool {
	if f == nil || node == nil {
		return false
	}
	dstKind := ir.TableWireScalarKind(f)
	dstSize := fixedStorageBytes(f.Type)
	if node.kind == dstKind && node.size == dstSize {
		return false
	}
	return fixedPlanWidens(node.kind, dstKind) && node.size <= dstSize
}

type fixedLayoutNode struct {
	id       uint64
	kind     int
	size     int64
	children []*fixedLayoutNode
}

func fixedParseLayoutTree(b []byte) (*fixedLayoutNode, string) {
	entries, ok := fixedLayoutEntries(b)
	if !ok || len(entries) == 0 {
		return nil, "layout_malformed"
	}
	i := 0
	var parse func() *fixedLayoutNode
	parse = func() *fixedLayoutNode {
		if i >= len(entries) {
			return nil
		}
		e := entries[i]
		i++
		if (e.kind < 1 || e.kind > 16) && (e.kind < 18 || e.kind > 30) && e.kind != 32 && e.kind != 33 && e.kind != fixedKindOptional && e.kind != 36 {
			return nil
		}
		n := &fixedLayoutNode{
			id:   e.id,
			kind: e.kind,
			size: e.size,
		}
		for k := 0; k < e.children; k++ {
			ch := parse()
			if ch == nil {
				return nil
			}
			n.children = append(n.children, ch)
		}
		return n
	}
	root := parse()
	if root == nil || i != len(entries) || root.kind != ir.TableKindTable {
		return nil, "layout_malformed"
	}
	var checkBounds func(n *fixedLayoutNode) bool
	checkBounds = func(n *fixedLayoutNode) bool {
		if n.size < 0 || n.size > ir.TableFixedRecordMaxBytes {
			return false
		}
		switch n.kind {
		case ir.TableKindTable:
			if n.size == 0 {
				return false
			}
			var sum int64
			for _, c := range n.children {
				sum += c.size
				if !checkBounds(c) {
					return false
				}
			}
			return sum == n.size
		case fixedKindOptional:
			if len(n.children) != 1 || 1+n.children[0].size != n.size {
				return false
			}
			return checkBounds(n.children[0])
		default:
			for _, c := range n.children {
				if !checkBounds(c) {
					return false
				}
			}
			return true
		}
	}
	if !checkBounds(root) {
		return nil, "layout_record_too_large"
	}
	leaves := 0
	var countLeaves func(n *fixedLayoutNode)
	countLeaves = func(n *fixedLayoutNode) {
		if len(n.children) == 0 {
			leaves++
			return
		}
		for _, c := range n.children {
			countLeaves(c)
		}
	}
	countLeaves(root)
	if leaves > ir.TableFixedLeafCap {
		return nil, "layout_record_too_large"
	}
	return root, ""
}

func fixedValidateLayout(b []byte) string {
	_, reason := fixedParseLayoutTree(b)
	return reason
}

func emitFixedPlanBody(m *Module, st *ir.Struct, e FixedLineageEntry) {
	root, reason := fixedParseLayoutTree(e.Layout)
	if reason != "" || root == nil {
		return
	}
	emitFixedPlanStruct(m, st, root, 0, "v", "  ", "0")
	emitFixedWriterRanges(m, st, root, e.Ranges)
}

// emitFixedWriterRanges is the bounds pass's WRITER half
// (docs/FIXED-FORM-ALGORITHM.md §4.6): after the plan has landed a record, a
// root-level ranged leaf the older writer bounded is held to THAT writer's
// range, which is the narrower one, so a value inside this reader's range but
// outside the writer's — a value the writer could not have written — refuses
// out_of_range and never lands as though it were lawful. A field the writer's
// layout does not carry, or whose kind moved, holds no writer range
// (ir.TableFixedWriterField).
func emitFixedWriterRanges(m *Module, st *ir.Struct, root *fixedLayoutNode, ranges []ir.TableFixedWriterRange) {
	carried := map[uint64]bool{}
	for _, c := range root.children {
		carried[c.id] = true
	}
	for _, r := range ranges {
		for _, f := range st.Fields {
			w, ok := ir.TableFixedWriterField(f, r)
			if !ok || !carried[ir.TableFieldWireId(f)] {
				continue
			}
			expr := "v." + squirrel.Member(f)
			storage := fixedStorageBytes(f.Type)
			if f.Array == ir.ArrayFixed {
				m.Printf("  for (local i = 0; i < %s.len(); i++) {\n", expr)
				m.Printf("    local x = %s[i];\n", expr)
				fixedEmitRangeGuard(m, w, "x", "    ", storage)
				m.Printf("  }\n")
			} else {
				fixedEmitRangeGuard(m, w, expr, "  ", storage)
			}
			break
		}
	}
}

func emitFixedPlanStruct(m *Module, st *ir.Struct, node *fixedLayoutNode, baseAt int64, expr, ind, dyn string) {
	type childHit struct {
		node *fixedLayoutNode
		at   int64
	}
	byID := map[uint64]childHit{}
	cur := baseAt
	for _, c := range node.children {
		if c.id != 0 {
			if _, exists := byID[c.id]; !exists {
				byID[c.id] = childHit{node: c, at: cur}
			}
		}
		cur += c.size
	}
	for _, f := range st.Fields {
		fid := ir.TableFieldWireId(f)
		hit, ok := byID[fid]
		if !ok {
			continue
		}
		fExpr := expr + "." + squirrel.Member(f)
		child := hit.node
		cAt := hit.at
		if f.Type.Optional {
			if child.kind == fixedKindOptional && len(child.children) == 1 {
				inner := child.children[0]
				if !fixedPlanCompat(f, inner) {
					continue
				}
				off := fmt.Sprintf("%d", cAt)
				if dyn != "0" {
					off = fmt.Sprintf("%d + %s", cAt, dyn)
				}
				m.Printf("%sif (at + %s + %d <= lim) {\n", ind, off, child.size)
				// a present byte other than 0 or 1 is damage (§3.4)
				m.Printf("%s  if (b[at + %s] > 1) return \"wire_damaged\";\n", ind, off)
				m.Printf("%s  %sPresent = b[at + %s] != 0;\n", ind, fExpr, off)
				m.Printf("%s  if (%sPresent) {\n", ind, fExpr)
				emitFixedPlanDecodeValue(m, f, inner, cAt+1, fExpr, ind+"    ", dyn, false)
				m.Printf("%s  }\n", ind)
				m.Printf("%s}\n", ind)
			} else if fixedPlanCompat(f, child) {
				off := fmt.Sprintf("%d", cAt)
				if dyn != "0" {
					off = fmt.Sprintf("%d + %s", cAt, dyn)
				}
				m.Printf("%sif (at + %s + %d <= lim) {\n", ind, off, child.size)
				m.Printf("%s  %sPresent = true;\n", ind, fExpr)
				emitFixedPlanDecodeValue(m, f, child, cAt, fExpr, ind+"  ", dyn, false)
				m.Printf("%s}\n", ind)
			}
		} else if fixedPlanCompat(f, child) {
			off := fmt.Sprintf("%d", cAt)
			if dyn != "0" {
				off = fmt.Sprintf("%d + %s", cAt, dyn)
			}
			m.Printf("%sif (at + %s + %d <= lim) {\n", ind, off, child.size)
			emitFixedPlanDecodeValue(m, f, child, cAt, fExpr, ind+"  ", dyn, false)
			m.Printf("%s}\n", ind)
		}
	}
}

func fixedPlanCompat(f *ir.Field, node *fixedLayoutNode) bool {
	if f == nil || node == nil {
		return false
	}
	switch {
	case f.KeyEnum != "":
		return node.kind == ir.TableKindKeyed && len(node.children) == 2
	case f.Array != ir.ArrayNone:
		return node.kind == ir.TableKindArray && len(node.children) == 1
	case f.Type.Kind == ir.TBytes:
		return (node.kind == ir.TableKindArray || node.kind == ir.TableKindString) && node.size >= 4
	case f.Type.Kind == ir.TString:
		return node.kind == ir.TableKindString && node.size >= 4
	case f.Type.Kind == ir.TWString:
		return node.kind == ir.TableKindWstring && node.size >= 4
	}
	return fixedPlanElemCompat(f, node)
}

func fixedPlanElemCompat(f *ir.Field, node *fixedLayoutNode) bool {
	if f.Type.Kind == ir.TNamed {
		switch f.Type.Ref.(type) {
		case *ir.Struct:
			return node.kind == ir.TableKindTable
		case *ir.Union:
			return node.kind == ir.TableKindUnion
		case *ir.Enum:
			return node.kind == ir.TableKindEnum && node.size >= 1
		case *ir.Flags:
			return node.kind >= ir.TableKindU8 && node.kind <= ir.TableKindU64
		}
	}
	dstKind := ir.TableWireScalarKind(f)
	dstSize := fixedStorageBytes(f.Type)
	if node.kind == dstKind && node.size == dstSize {
		return true
	}
	return fixedPlanWidens(node.kind, dstKind) && node.size <= dstSize
}

func emitFixedPlanDecodeValue(m *Module, f *ir.Field, node *fixedLayoutNode, at int64, expr, ind, dyn string, element bool) {
	off := fmt.Sprintf("%d", at)
	if dyn != "0" {
		off = fmt.Sprintf("%d + %s", at, dyn)
	}
	if !element {
		switch {
		case f.KeyEnum != "" && node.kind == ir.TableKindKeyed && len(node.children) == 2:
			elem := node.children[1]
			if !fixedPlanElemCompat(f, elem) || elem.size <= 0 {
				return
			}
			n := min(node.size/elem.size, f.KeyEnumRef.Max)
			iv := fmt.Sprintf("i%d", len(ind)/2)
			m.Printf("%sfor (local %s = 0; %s < %d; %s++) {\n", ind, iv, iv, n, iv)
			emitFixedPlanDecodeValue(m, f, elem, at, expr+"["+iv+"]", ind+"  ", dynAdd(dyn, iv+" * "+fmt.Sprint(elem.size)), true)
			m.Printf("%s}\n", ind)
			return
		case f.Array != ir.ArrayNone && node.kind == ir.TableKindArray && len(node.children) == 1:
			elem := node.children[0]
			if !fixedPlanElemCompat(f, elem) || elem.size <= 0 {
				return
			}
			iv := fmt.Sprintf("i%d", len(ind)/2)
			if f.Array == ir.ArrayCounted {
				oldCap := (node.size - 4) / elem.size
				capLim := min(oldCap, f.ArrayBound)
				m.Printf("%slocal c = _rt_fixed.geti32(b, at + %s);\n", ind, off)
				m.Printf("%sif (c < 0 || c > %d) return \"count_over_bound\";\n", ind, capLim)
				m.Printf("%sif (%s.len() > c) %s.resize(c); while (%s.len() < c) %s.append(%s);\n", ind, expr, expr, expr, expr, elemZero(f))
				m.Printf("%sfor (local %s = 0; %s < c; %s++) {\n", ind, iv, iv, iv)
				emitFixedPlanDecodeValue(m, f, elem, at+4, expr+"["+iv+"]", ind+"  ", dynAdd(dyn, iv+" * "+fmt.Sprint(elem.size)), true)
				m.Printf("%s}\n", ind)
			} else {
				n := min(node.size/elem.size, f.ArrayBound)
				m.Printf("%swhile (%s.len() < %d) %s.append(%s);\n", ind, expr, f.ArrayBound, expr, elemZero(f))
				m.Printf("%sfor (local %s = 0; %s < %d; %s++) {\n", ind, iv, iv, n, iv)
				emitFixedPlanDecodeValue(m, f, elem, at, expr+"["+iv+"]", ind+"  ", dynAdd(dyn, iv+" * "+fmt.Sprint(elem.size)), true)
				m.Printf("%s}\n", ind)
			}
			return
		case fixedIsText(f):
			oldCap := node.size - 4
			if f.Type.Kind == ir.TWString {
				oldCap = (node.size - 4) / 2
			}
			capLim := min(oldCap, f.Type.Size)
			m.Printf("%slocal n = _rt_fixed.geti32(b, at + %s);\n", ind, off)
			m.Printf("%sif (n < 0 || n > %d) return \"length_over_bound\";\n", ind, capLim)
			switch f.Type.Kind {
			case ir.TString:
				m.Printf("%sif (_rt_fixed.textIllFormed(b, at + %s + 4, n)) return \"text_ill_formed\";\n", ind, off)
				m.Printf("%slocal s = \"\"; for (local i = 0; i < n; i++) s += b[at + %s + 4 + i].tochar();\n", ind, off)
				m.Printf("%s%s = s;\n", ind, expr)
			case ir.TWString:
				m.Printf("%slocal w = _rt_fixed.wideUnits(b, at + %s + 4, n);\n", ind, off)
				m.Printf("%sif (w == null) return \"text_ill_formed\";\n", ind)
				m.Printf("%s%s = w;\n", ind, expr)
			default:
				m.Printf("%s%s = blob(n); for (local i = 0; i < n; i++) %s[i] = b[at + %s + 4 + i];\n", ind, expr, expr, off)
			}
			return
		}
	}
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			emitFixedPlanStruct(m, r, node, at, expr, ind, dyn)
			return
		case *ir.Union:
			nArms := len(node.children)
			tagW := int64(1)
			if nArms > 65535 {
				tagW = 4
			} else if nArms > 255 {
				tagW = 2
			}
			m.Printf("%s%s.Type = _rt_fixed.getu(b, at + %s, %d);\n", ind, expr, off, tagW)
			maxArms := min(len(r.Variants), nArms)
			// A TAG PAST THE WRITER'S ARMS is not one the writer wrote: it
			// REFUSES out_of_range on the compiled plan exactly as on the
			// identity one, and never lands None (docs/FIXED-FORM-ALGORITHM.md
			// §4.6, §5.4; §5.8 row 12)
			m.Printf("%sif (%s.Type < 0 || %s.Type > %d) return \"out_of_range\";\n", ind, expr, expr, maxArms)
			for i := range maxArms {
				v := r.Variants[i]
				armNode := node.children[i]
				if v.F == nil || armNode == nil || armNode.kind == ir.TableKindNoPayload {
					continue
				}
				if !fixedPlanCompat(v.F, armNode) {
					continue
				}
				m.Printf("%sif (%s.Type == %d) {\n", ind, expr, i+1)
				emitFixedPlanDecodeValue(m, v.F, armNode, at+tagW, expr+"."+ir.GoExportName(v.Name), ind+"  ", dyn, false)
				m.Printf("%s}\n", ind)
			}
			return
		case *ir.Enum:
			land := "null"
			if !element && f.HasDefault && f.DefVariant != "" && f.DefVariant != "None" {
				land = squirrel.StringLit([]byte(f.DefVariant))
			}
			m.Printf("%s{\n", ind)
			m.Printf("%s  local ev = _rt_fixed.getu(b, at + %s, %d);\n", ind, off, node.size)
			// AN ORDINAL PAST THE WRITER'S VARIANTS — the plan's count, never
			// this reader's longer one — is FORGED and refuses out_of_range
			// (docs/FIXED-FORM-ALGORITHM.md §4.6, §5.4; §5.8 row 12)
			top := min(len(node.children), len(r.Variants))
			m.Printf("%s  if (ev < 0 || ev > %d) return \"out_of_range\";\n", ind, top)
			m.Printf("%s  local val = null;\n", ind)
			for i, v := range r.Variants {
				kw := "if"
				if i > 0 {
					kw = "else if"
				}
				value := squirrel.StringLit([]byte(v))
				if i < len(r.VariantDeprecated) && r.VariantDeprecated[i] {
					value = land
				}
				m.Printf("%s  %s (ev == %d) val = %s;\n", ind, kw, i+1, value)
			}
			m.Printf("%s  %s = val;\n%s}\n", ind, expr, ind)
			return
		case *ir.Flags:
			m.Printf("%s%s = _rt_fixed.getu(b, at + %s, %d);\n", ind, expr, off, node.size)
			return
		}
	}
	if node.size == 16 {
		m.Printf("%s%s = [_rt_fixed.u64le(b, at + %s), _rt_fixed.u64le(b, at + %s + 8)];\n", ind, expr, off, off)
		fixedEmitRange128Guard(m, f, expr, ind)
		return
	}
	if f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64 {
		get := "getF64"
		if node.size == 4 || node.kind == ir.TableKindF32 {
			get = "getF32"
		}
		m.Printf("%s{\n%s  local x = _rt_fixed.%s(b, at + %s);\n", ind, ind, get, off)
		m.Printf("%s  if (x == null) { _M.FixedField <- %s; return \"non_finite_float\"; }\n", ind, squirrel.StringLit([]byte(FloatOwner(m.Unit, f))))
		if fixedPlanLeafWidens(f, node) {
			m.Printf("%s  _M.FixedWidened <- _M.FixedWidened + 1;\n", ind)
		} else {
			fixedEmitRangeGuard(m, f, "x", ind+"  ", fixedStorageBytes(f.Type))
		}
		m.Printf("%s  %s = x;\n%s}\n", ind, expr, ind)
		return
	}
	if f.Type.Kind == ir.TBool {
		m.Printf("%sif (b[at + %s] > 1) return \"wire_damaged\";\n", ind, off)
		m.Printf("%s%s = b[at + %s] != 0;\n", ind, expr, off)
		return
	}
	if node.kind >= ir.TableKindU8 && node.kind <= ir.TableKindU64 || (node.kind >= ir.TableKindUFixed8 && node.kind <= ir.TableKindUFixed64) {
		if fixedPlanLeafWidens(f, node) {
			m.Printf("%s_M.FixedWidened <- _M.FixedWidened + 1;\n", ind)
		}
		m.Printf("%s%s = _rt_fixed.getu(b, at + %s, %d);\n", ind, expr, off, node.size)
		fixedEmitRangeGuard(m, f, expr, ind, node.size)
		return
	}
	if fixedPlanLeafWidens(f, node) {
		m.Printf("%s_M.FixedWidened <- _M.FixedWidened + 1;\n", ind)
	}
	m.Printf("%s%s = _rt_fixed.get(b, at + %s, %d);\n", ind, expr, off, node.size)
	fixedEmitRangeGuard(m, f, expr, ind, node.size)
}
