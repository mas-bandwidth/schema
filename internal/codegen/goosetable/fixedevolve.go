// fixedevolve.go — the fixed form's EVOLUTION (docs/FIXED-FORM-VERSIONING-TESTS.md;
// docs/FIXED-FORM-ALGORITHM.md §5; docs/PORT-GOOSE.md, THE RUNTIME LAYOUT).
//
// The fixed form reads BACKWARD and never forward: a file is matched on the
// eight bytes of its header hash against the lineage the BUILD laid down, and
// the bytes it carries are held to a BYTE COMPARISON against the entry the
// lock recorded — never walked (§5.3). A hash in no entry is layout_unknown
// (SPEC-TABLES §21.3; layout_newer is reserved until the lineage proves
// direction), a hash below the retirement floor is layout_unsupported.
//
// The lineage is the input this card adds to the Goose build: GenerateLineage
// takes the entries, Generate is the same call with none. The read path is
// emitted per root as <Root>FixedEvolveLoad; the identity entry is the fixed
// form's own reader, and an append-only older entry lands its old body over a
// current-layout image born at the declared defaults (§4.3), so the appended
// tail lands the default the widening promises (field_append) and an enum or
// flags widening of the same width lands whole.
package goosetable

import (
	"fmt"
	"sync"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// FixedEvolutionEntry is ONE locked layout of one fixed table, the facts
// docs/FIXED-FORM-ALGORITHM.md §5.2 asks the lock for: the wire hash a file
// is matched on, the layout bytes verbatim, the record size taken from the
// lock and never from the file, and the operator's retirement. It is the
// Goose counterpart of internal/codegen/gotable's FixedLineageEntry.
type FixedEvolutionEntry struct {
	Wire    uint64
	Layout  []byte
	Record  int64
	Retired bool
	Reason  string
}

// FixedEvolutionOf is the entry a unit's OWN build locks for table name: its
// layout bytes, the hash over them and its record size
// (docs/FIXED-FORM-ALGORITHM.md §1, §5.2). It is how a test states an older
// entry without a lock.
func FixedEvolutionOf(u *ir.Unit, name string) (FixedEvolutionEntry, bool) {
	for _, st := range ir.TableFixedFormRoots(u) {
		if st.Name != name {
			continue
		}
		layout, hash, body := fixedRootFacts(st)
		return FixedEvolutionEntry{Wire: hash, Layout: layout, Record: 8 + body}, true
	}
	return FixedEvolutionEntry{}, false
}

// evolveLineage is the map GenerateLineage hands the registered part. The
// generator's Part takes no lineage argument (docs/PORT-GOOSE.md, THE
// RUNTIME LAYOUT), so the one call that carries one sets it for the duration
// of Generate and holds the lock; a plain Generate sees nil.
var (
	evolveMu      sync.Mutex
	evolveLineage map[string][]FixedEvolutionEntry
)

// GenerateLineage generates the table surface with a fixed-table lineage: per
// table, the locked layouts OLDEST FIRST; the table's own entry is appended
// last when the lineage does not already end on it, and the floor is 1 + the
// highest retired index (§5.2). A hash the lineage does not hold is refused
// layout_unknown, and a retired entry below the floor layout_unsupported.
func GenerateLineage(u *ir.Unit, lineage map[string][]FixedEvolutionEntry) (map[string][]byte, error) {
	evolveMu.Lock()
	defer evolveMu.Unlock()
	evolveLineage = lineage
	defer func() { evolveLineage = nil }()
	return Generate(u)
}

// fixedEvolvePart is the [Register]ed part.
func fixedEvolvePart(g *goose.Gen) error {
	roots := fixedRootsOf(g)
	if len(roots) > 0 {
		g.Import("fixedevolve") // schema_rt::FixedEvolveReport and the plan walk
	}
	for _, st := range roots {
		e := &fixedEvolveEmit{g: g}
		e.emitRoot(st)
	}
	return nil
}

func init() {
	Register("fixedevolve", fixedEvolvePart)
}

type fixedEvolveEmit struct {
	g *goose.Gen
}

func (e *fixedEvolveEmit) p(format string, args ...any) {
	e.g.Pf(format, args...)
}

// rawEntry is one parsed layout entry. The layout is u32 count then
// 17-byte entries: id (u64), kind (u8), size (u32), children (u32)
// (docs/FIXED-FORM-ALGORITHM.md §1).
type rawEntry struct {
	id       uint64
	kind     int
	size     int64
	children int
}

func parseLayout(data []byte) []rawEntry {
	if len(data) < 4 {
		return nil
	}
	n := int(uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24)
	if 4+17*n > len(data) {
		return nil
	}
	out := make([]rawEntry, n)
	for i := range n {
		at := 4 + 17*i
		var id uint64
		for b := range 8 {
			id |= uint64(data[at+b]) << (8 * b)
		}
		out[i] = rawEntry{
			id:       id,
			kind:     int(data[at+8]),
			size:     int64(uint32(data[at+9]) | uint32(data[at+10])<<8 | uint32(data[at+11])<<16 | uint32(data[at+12])<<24),
			children: int(uint32(data[at+13]) | uint32(data[at+14])<<8 | uint32(data[at+15])<<16 | uint32(data[at+16])<<24),
		}
	}
	return out
}

// evolveEntries is the lineage the emitter lays down for one table: oldest
// first, the table's own entry last when absent, with the retirement floor.
func (e *fixedEvolveEmit) evolveEntries(st *ir.Struct, own FixedEvolutionEntry) ([]FixedEvolutionEntry, int) {
	entries := append([]FixedEvolutionEntry(nil), evolveLineage[st.Name]...)
	if len(entries) == 0 || entries[len(entries)-1].Wire != own.Wire {
		entries = append(entries, own)
	}
	floor := 0
	for i, en := range entries {
		if en.Retired {
			floor = i + 1
		}
	}
	return entries, floor
}

// emitRoot writes <Root>FixedEvolveLoad and the lineage it is handed.
func (e *fixedEvolveEmit) emitRoot(st *ir.Struct) {
	layout, hash, body := fixedRootFacts(st)
	own := FixedEvolutionEntry{Wire: hash, Layout: layout, Record: 8 + body}
	entries, _ := e.evolveEntries(st, own)
	n := st.Name
	typ := n

	e.p("\n// ---- %s's lineage (docs/FIXED-FORM-ALGORITHM.md §5.2): oldest first, the floor is below it. ----\n", n)
	// The identity entry is pick 0; older entries follow it. The own entry may
	// also appear inside the lineage, so its index is found by hash.
	var olds []FixedEvolutionEntry
	for _, en := range entries {
		if en.Wire != hash {
			olds = append(olds, en)
		}
	}
	var plans []evolvePlan
	for _, en := range olds {
		plans = append(plans, compileEvolvePlan(st, en.Layout, layout))
	}
	// The floor in the emitted numbering: pick 0 is identity and always
	// supported, so a retired older entry at pick p moves the floor to p+1.
	floor := 0
	for i, en := range olds {
		if en.Retired {
			floor = i + 2
		}
	}
	e.p("let %sFixedEvolveFloor: i64 = %d;\n", n, floor)
	for i, en := range olds {
		e.emitEntry(n, i+1, en, st)
	}

	e.p("// %sFixedEvolveLoad reads a fixed-form file through the lineage: the identity\n", n)
	e.p("// entry uses the fixed form's own scan and run, an append-only older entry the\n")
	e.p("// old body over a current-layout image born at its defaults (§4.3, §5.3).\n")
	e.p("fn %sFixedEvolveLoad(values: %s[:], data: const u8[:], report: schema_rt::FixedEvolveReport&) -> i64 {\n", n, typ)
	e.p("    report = schema_rt::FixedEvolveReport { .. };\n")
	e.p("    let fr = schema_rt::fixed_evolve_header(data, report);\n")
	e.p("    if fr < 0 { return -1; }\n")
	e.p("    let h = schema_rt::fixed_u64(data, 8);\n")
	e.p("    let layout_length = schema_rt::fixed_u32(data, schema_rt::FixedHeaderBytes) as i64;\n")
	e.p("    var pick: i64 = -1;\n")
	e.p("    var known: const u8[:] = %sFixedLayout;\n", n)
	e.p("    var record_bytes: i64 = %sFixedRecordBytes;\n", n)
	e.p("    var supported: bool = true;\n")
	e.p("    if h == %sFixedHash {\n        pick = 0;\n", n)
	for i, en := range olds {
		e.p("    } else if h == 0x%016x {\n        pick = %d;\n        known = %sFixedEvolveLayout%d;\n        record_bytes = %d;\n        supported = %sFixedEvolveSupported%d;\n", en.Wire, i+1, n, i+1, en.Record, n, i+1)
	}
	e.p("    }\n")
	e.p("    if pick < 0 {\n        report.hash = h;\n        return schema_rt::fixed_evolve_refuse(report, schema_rt::FixedRefuseLayoutUnknown, false);\n    }\n")
	e.p("    if pick > 0 && pick < %sFixedEvolveFloor {\n        report.hash = h;\n        return schema_rt::fixed_evolve_refuse(report, schema_rt::FixedRefuseLayoutUnsupported, false);\n    }\n", n)
	e.p("    if !supported {\n        return schema_rt::fixed_evolve_refuse(report, schema_rt::FixedRefuseLayoutMalformed, false);\n    }\n")
	e.p("    if !schema_rt::fixed_evolve_layout_ok(data, layout_length, known) {\n        return schema_rt::fixed_evolve_refuse(report, schema_rt::FixedRefuseLayoutMalformed, false);\n    }\n")
	e.p("    let rest = data.len - schema_rt::FixedHeaderBytes - 4 - layout_length;\n")
	e.p("    if record_bytes <= 8 || rest %% record_bytes != 0 {\n        return schema_rt::fixed_evolve_refuse(report, schema_rt::FixedRefuseTruncated, true);\n    }\n")
	e.p("    let count = rest / record_bytes;\n")
	e.p("    if count > values.len {\n        return schema_rt::fixed_evolve_refuse(report, schema_rt::FixedRefuseBatchTooLarge, false);\n    }\n")
	e.p("    let first = schema_rt::FixedHeaderBytes + 4 + layout_length;\n")
	e.p("    var at: i64 = first;\n")
	e.p("    while at < data.len {\n        if schema_rt::fixed_u64(data, at) != h {\n            return schema_rt::fixed_evolve_refuse(report, schema_rt::FixedRefuseNoLayout, false);\n        }\n        at += record_bytes;\n    }\n")
	e.p("    var k: i64 = 0;\n")
	e.p("    while k < count {\n")
	e.p("        let rec = first + k * record_bytes;\n")
	e.p("        let body = rec + 8;\n")
	e.p("        if pick == 0 {\n")
	e.p("            let r = _%sFixedScan(data, body);\n", n)
	e.p("            if r != 0 { return schema_rt::fixed_evolve_refuse(report, r, false); }\n")
	e.p("            _%sFixedRead(data, body, values[k]);\n", n)
	for i, plan := range plans {
		e.p("        } else if pick == %d {\n", i+1)
		e.p("            var def: %s = %s { .. };\n", typ, typ)
		e.p("            var img: u8[>..] = [];\n")
		e.p("            _%sFixedWrite(img, def);\n", n)
		seq := 0
		e.emitNodes(plan.nodes, "body", "", &seq)
		e.p("            let r = _%sFixedScan(img, 0);\n", n)
		e.p("            if r != 0 { return schema_rt::fixed_evolve_refuse(report, r, false); }\n")
		e.p("            _%sFixedRead(img, 0, values[k]);\n", n)
	}
	e.p("        }\n")
	e.p("        report.clamped += _%sFixedClamp(values[k]);\n", n)
	e.p("        k += 1;\n")
	e.p("    }\n")
	// THE CENSUS LANDS ONCE, AFTER THE RECORD LOOP, ON A READ THAT RETURNS
	// (docs/FIXED-FORM-ALGORITHM.md §5.4, §5.8 row 11). It is per FIELD
	// per PEER, never per record; a refusal has already returned and counted
	// nothing.
	for i, plan := range plans {
		if plan.unknown == 0 && plan.kindMismatch == 0 {
			continue
		}
		e.p("    if pick == %d {\n", i+1)
		if plan.unknown > 0 {
			e.p("        report.unknown += %d;\n", plan.unknown)
		}
		if plan.kindMismatch > 0 {
			e.p("        report.kind_mismatch += %d;\n", plan.kindMismatch)
		}
		e.p("    }\n")
	}
	e.p("    return count;\n")
	e.p("}\n")
}

// addExpr joins a base expression and offsets without a leading "+".
func addExpr(base string, parts ...string) string {
	out := base
	for _, p := range parts {
		if out == "" {
			out = p
			continue
		}
		out += " + " + p
	}
	return out
}

// emitNodes writes one plan's steps over `img`, the current-layout default
// image. oldBase and curBase are the expressions the offsets are relative to;
// a node's widening counts itself once per execution, so an element loop's
// widen counts min(their_n, my_n) and a scalar counts once per record
// (docs/FIXED-FORM-ALGORITHM.md §5.4).
func (e *fixedEvolveEmit) emitNodes(nodes []evolveNode, oldBase, curBase string, seq *int) {
	for _, n := range nodes {
		switch n.op {
		case nodeCopy:
			if n.size == 1 {
				e.p("            img[%s] = data[%s];\n", addExpr(curBase, fmt.Sprintf("%d", n.curOff)), addExpr(oldBase, fmt.Sprintf("%d", n.oldOff)))
				continue
			}
			v := *seq
			*seq = *seq + 1
			q := fmt.Sprintf("q%d", v)
			e.p("            var %s: i64 = 0;\n", q)
			e.p("            while %s < %d {\n                img[%s] = data[%s];\n                %s += 1;\n            }\n",
				q, n.size, addExpr(curBase, fmt.Sprintf("%d", n.curOff), q), addExpr(oldBase, fmt.Sprintf("%d", n.oldOff), q), q)
		case nodeSet:
			for b := int64(0); b < n.size; b++ {
				e.p("            img[%s] = %d;\n", addExpr(curBase, fmt.Sprintf("%d", n.curOff+b)), byte(n.arg>>(8*b)))
			}
		case nodeWiden:
			if n.float {
				// f32 -> f64: read the old bits, convert by the value, write the
				// f64 bits back (the FU ruling's quiet bit rides).
				v := *seq
				*seq = *seq + 1
				e.p("            var u%d: u32 = schema_rt::fixed_u32(data, %s);\n", v, addExpr(oldBase, fmt.Sprintf("%d", n.oldOff)))
				e.p("            var fv%d: f32 = schema_rt::fixed_bits_f32(u%d);\n", v, v)
				e.p("            var d%d: f64 = fv%d;\n", v, v)
				e.p("            var db%d: u64 = schema_rt::fixed_f64_bits(d%d);\n", v, v)
				e.p("            var q%d: i64 = 0;\n", v)
				e.p("            while q%d < 8 {\n                img[%s] = (db%d >> (8 * q%d)) as! u8;\n                q%d += 1;\n            }\n",
					v, addExpr(curBase, fmt.Sprintf("%d", n.curOff), fmt.Sprintf("q%d", v)), v, v, v)
				e.p("            report.widened += 1;\n")
				continue
			}
			if n.oldSize > 0 {
				v := *seq
				*seq = *seq + 1
				q := fmt.Sprintf("q%d", v)
				e.p("            var %s: i64 = 0;\n", q)
				e.p("            while %s < %d {\n                img[%s] = data[%s];\n                %s += 1;\n            }\n",
					q, n.oldSize, addExpr(curBase, fmt.Sprintf("%d", n.curOff), q), addExpr(oldBase, fmt.Sprintf("%d", n.oldOff), q), q)
			}
			if n.newSize > n.oldSize {
				v := *seq
				*seq = *seq + 1
				q := fmt.Sprintf("q%d", v)
				e.p("            var %s: i64 = %d;\n", q, n.oldSize)
				if n.signed {
					e.p("            var s%d: u8 = 0;\n", v)
					e.p("            if (data[%s] & 0x80) != 0 { s%d = 255; }\n", addExpr(oldBase, fmt.Sprintf("%d", n.oldOff+n.oldSize-1)), v)
					e.p("            while %s < %d {\n                img[%s] = s%d;\n                %s += 1;\n            }\n",
						q, n.newSize, addExpr(curBase, fmt.Sprintf("%d", n.curOff), q), v, q)
				} else {
					e.p("            while %s < %d {\n                img[%s] = 0;\n                %s += 1;\n            }\n",
						q, n.newSize, addExpr(curBase, fmt.Sprintf("%d", n.curOff), q), q)
				}
			}
			if n.widened {
				e.p("            report.widened += 1;\n")
			}
		case nodeCountGuard:
			// A COUNTED ARRAY NEVER LANDS THE WRITER'S COUNT WORD UNCHECKED
			// (docs/FIXED-FORM-ALGORITHM.md §5.8 row 4; docs/SPEC-TABLES.md
			// §3.4): a word below zero or past the plan's bound — the
			// writer's min(their_n, my_n) — refuses count_over_bound before the
			// count word or any destination byte lands, as gotable's
			// tableFixedTextRefusal does.
			v := *seq
			*seq = *seq + 1
			e.p("            let cg%d: i64 = schema_rt::fixed_i32(data, %s);\n", v, addExpr(oldBase, fmt.Sprintf("%d", n.oldOff)))
			e.p("            if cg%d < 0 || cg%d > %d {\n                return schema_rt::fixed_evolve_refuse(report, schema_rt::FixedRefuseCountOverBound, false);\n            }\n", v, v, n.count)
		case nodeOrdinal:
			// A SAME-WIDTH ORDINAL IS REMAPPED THROUGH THE WRITER'S VARIANT SET
			// (docs/FIXED-FORM-ALGORITHM.md §5.2 EMIT kind 30; §5.4, §5.8 row
			// 12): a raw past the writer's count lands None, and the bounds pass
			// counts it once per field per record, exactly as gotable's
			// tableFixedOrdinal and tableFixedForgedBounds do.
			v := *seq
			*seq = *seq + 1
			e.p("            let or%d: u64 = schema_rt::fixed_evolve_read_u(data, %s, %d);\n", v, addExpr(oldBase, fmt.Sprintf("%d", n.oldOff)), n.size)
			e.p("            var ov%d: u64 = 0;\n", v)
			for raw := int64(1); raw <= n.count; raw++ {
				if raw < int64(len(n.remap)) && n.remap[raw] != 0 {
					e.p("            if or%d == %d { ov%d = %d; }\n", v, raw, v, n.remap[raw])
				}
			}
			e.p("            if or%d > %d { report.clamped += 1; }\n", v, n.count)
			for b := int64(0); b < n.size; b++ {
				e.p("            img[%s] = ((ov%d >> %d) & 255) as! u8;\n", addExpr(curBase, fmt.Sprintf("%d", n.curOff+b)), v, 8*b)
			}
		case nodeForgedTag:
			// A FORGED UNION TAG PAST THE WRITER'S ARM SET LANDS NONE AND IS
			// COUNTED ONCE (docs/FIXED-FORM-ALGORITHM.md §5.4, §5.8 row 12),
			// matching gotable's unguarded None const and tableFixedForgedBounds.
			v := *seq
			*seq = *seq + 1
			e.p("            let ft%d: u64 = schema_rt::fixed_evolve_read_u(data, %s, %d);\n", v, addExpr(oldBase, fmt.Sprintf("%d", n.oldOff)), n.tagWidth)
			e.p("            if ft%d > %d { report.clamped += 1; }\n", v, n.count)
		case nodeLoop:
			v := *seq
			*seq = *seq + 1
			i := fmt.Sprintf("e%d", v)
			if n.counted {
				cv := fmt.Sprintf("c%d", v)
				e.p("            var %s: i64 = schema_rt::fixed_u32(data, %s);\n", cv, addExpr(oldBase, fmt.Sprintf("%d", n.oldOff)))
				e.p("            if %s > %d { %s = %d; }\n", cv, n.count, cv, n.count)
				e.p("            var %s: i64 = 0;\n", i)
				e.p("            while %s < %s {\n", i, cv)
			} else {
				e.p("            var %s: i64 = 0;\n", i)
				e.p("            while %s < %d {\n", i, n.count)
			}
			oldElem := addExpr(oldBase, fmt.Sprintf("%d", n.oldOff), fmt.Sprintf("%s * %d", i, n.oldStride))
			curElem := addExpr(curBase, fmt.Sprintf("%d", n.curOff), fmt.Sprintf("%s * %d", i, n.newStride))
			e.emitNodes(n.body, oldElem, curElem, seq)
			e.p("                %s += 1;\n            }\n", i)
		case nodeDefault:
			v := *seq
			*seq = *seq + 1
			e.p("            var ad%d: %s = %s { .. };\n", v, n.defaultType, n.defaultType)
			e.p("            var ab%d: u8[>..] = [];\n", v)
			e.p("            _%sFixedWrite(ab%d, ad%d);\n", n.defaultType, v, v)
			e.p("            var aj%d: i64 = 0;\n", v)
			e.p("            while aj%d < ab%d.len {\n                img[%s] = ab%d[aj%d];\n                aj%d += 1;\n            }\n",
				v, v, addExpr(curBase, fmt.Sprintf("%d", n.curOff), fmt.Sprintf("aj%d", v)), v, v, v)
		case nodeIf:
			e.p("            if schema_rt::fixed_evolve_read_u(data, %s, %d) == %d {\n",
				addExpr(oldBase, fmt.Sprintf("%d", n.oldOff)), n.tagWidth, n.arg)
			e.emitNodes(n.body, oldBase, curBase, seq)
			e.p("            }\n")
		}
	}
}

// evolveNode is one step of a compiled plan (docs/FIXED-FORM-ALGORITHM.md
// §5.2): a byte copy, a widening, a constant (an optional's present byte or a
// union's tag), an array's element loop, or a union arm guarded by the old
// tag. A node's offsets are relative to the base expression its emitter is
// handed, so a nested struct, an array element and a union arm all reuse the
// same plan shape.
type evolveNode struct {
	op                         int
	oldOff, curOff             int64
	size, oldSize, newSize     int64
	oldStride, newStride, head int64
	count                      int64
	counted                    bool
	arg                        uint64
	tagWidth                   int64
	defaultType                string
	remap                      []uint64
	signed, float, widened     bool
	body                       []evolveNode
}

const (
	nodeCopy = iota
	nodeWiden
	nodeSet
	nodeLoop
	nodeIf
	nodeDefault
	nodeCountGuard
	nodeOrdinal
	nodeForgedTag
)

// evolvePlan is one older entry's compiled plan. The old bytes are laid over
// the current-layout default image the prefill wrote (§4.3); the census is
// ONCE PER FIELD PER PEER and lands only on a read that returns (§5.4).
type evolvePlan struct {
	nodes        []evolveNode
	unknown      int
	kindMismatch int
	supported    bool
}

func signedKind(k int) bool {
	switch k {
	case 2, 3, 4, 5, 20, 21, 22, 23, 24:
		return true
	}
	return false
}

func unsignedKind(k int) bool {
	switch k {
	case 6, 7, 8, 9, 25, 26, 27, 28, 29:
		return true
	}
	return false
}

// evolveWidens is the versioning ladder (docs/FIXED-FORM-ALGORITHM.md §1,
// §5.1): an integer or fixed-point rung upward, or f32 into f64. It is the
// same test the Go engine's tableFixedWidens makes.
func evolveWidens(from, to int) bool {
	switch {
	case from >= 6 && from <= 9 && to >= 6 && to <= 9:
		return to > from
	case from >= 2 && from <= 5 && to >= 2 && to <= 5:
		return to > from
	case from >= 20 && from <= 24 && to >= 20 && to <= 24:
		return to > from
	case from >= 25 && from <= 29 && to >= 25 && to <= 29:
		return to > from
	}
	return from == 10 && to == 11
}

// evolveCompiler walks the two layouts in pre-order by wire id, exactly as the
// Go engine's tableFixedCompileEntry does. An old child the current layout
// does not name is the unknown census; a field whose kind moved outside the
// ladder is a kind mismatch. Neither is a refusal: both are counted and the
// field is left at its default.
type evolveCompiler struct {
	oldAll, curAll []rawEntry
	counted        map[uint64]bool
	armDefaults    map[uint64]string
	plan           *evolvePlan
}

// subtree is how many pre-order entries the subtree rooted at i occupies.
func (c *evolveCompiler) subtree(all []rawEntry, i int) int {
	if i < 0 || i >= len(all) {
		return 1
	}
	pending, n, at := 1, 0, i
	for pending > 0 && at < len(all) {
		pending--
		n++
		pending += all[at].children
		at++
	}
	return n
}

// widestArm is the union's widest arm, so its tag width is the entry's size
// less that arm (§5.2 THE GUARD'S WIDTH).
func widestArm(all []rawEntry, i int) int64 {
	var widest int64
	if i < 0 || i >= len(all) {
		return 0
	}
	at := i + 1
	for k := 0; k < all[i].children && at < len(all); k++ {
		if all[at].size > widest {
			widest = all[at].size
		}
		at += 1
	}
	return widest
}

func (c *evolveCompiler) match(oi, ci int, oldOff, curOff int64, census bool, depth int) []evolveNode {
	if depth > 64 || oi < 0 || oi >= len(c.oldAll) || ci < 0 || ci >= len(c.curAll) {
		return nil
	}
	te, me := c.oldAll[oi], c.curAll[ci]
	// `T` read into `?T` (optional_add): the writer carried no present byte,
	// so the plan lands present := 1 and compiles the payload against the
	// wrapper's child. A `?T` read into `?T` copies the present byte.
	if me.kind == 35 {
		if te.kind == 35 {
			out := []evolveNode{{op: nodeCopy, oldOff: oldOff, curOff: curOff, size: 1}}
			return append(out, c.match(oi+1, ci+1, oldOff+1, curOff+1, census, depth+1)...)
		}
		out := []evolveNode{{op: nodeSet, curOff: curOff, arg: 1, size: 1}}
		return append(out, c.match(oi, ci+1, oldOff, curOff+1, census, depth+1)...)
	}
	if te.kind != me.kind {
		if evolveWidens(te.kind, me.kind) {
			return []evolveNode{{op: nodeWiden, oldOff: oldOff, curOff: curOff, oldSize: te.size,
				newSize: me.size, signed: signedKind(te.kind), float: te.kind == 10 && me.kind == 11, widened: true}}
		}
		// SPEC-TABLES §4: a named position whose kind moved is kind_mismatch,
		// counted and dropped, never a silent byte copy.
		c.plan.kindMismatch++
		return nil
	}
	switch te.kind {
	case 13: // a table or a nested struct: match the children by wire id
		return c.matchChildren(oi, ci, oldOff, curOff, census, depth)
	case 14: // an array: its count word, then its elements at their strides
		return c.array(oi, ci, oldOff, curOff, census, depth)
	case 15: // a union: the tag, then the selected arm
		return c.union(oi, ci, oldOff, curOff, census, depth)
	case 16: // a keyed array: the keys only ever append, so the old bytes stand
		n := min(me.size, te.size)
		return []evolveNode{{op: nodeCopy, oldOff: oldOff, curOff: curOff, size: n}}
	case 12, 33: // string / wstring: length and bytes, slack left at default
		n := min(me.size, te.size)
		return []evolveNode{{op: nodeCopy, oldOff: oldOff, curOff: curOff, size: n}}
	case 30: // an ordinal: a grown width is an unsigned widen, one count each
		if te.size < me.size {
			return []evolveNode{{op: nodeWiden, oldOff: oldOff, curOff: curOff, oldSize: te.size,
				newSize: me.size, widened: true}}
		}
		if te.size > me.size {
			c.plan.kindMismatch++
			return nil
		}
		// THE WRITER'S VARIANT TABLE (gotable tableFixedCompileEntry, kind 30):
		// each old variant names the reader ordinal by wire id, so a variant the
		// old writer could not have written never lands a name only this reader
		// has. A raw past the writer's count lands zero (None).
		remap := make([]uint64, int(te.children)+1)
		for j := 0; j < te.children && oi+1+j < len(c.oldAll); j++ {
			vid := c.oldAll[oi+1+j].id
			for k := 0; k < me.children && ci+1+k < len(c.curAll); k++ {
				if c.curAll[ci+1+k].id == vid {
					remap[j+1] = uint64(k + 1)
					break
				}
			}
		}
		return []evolveNode{{op: nodeOrdinal, oldOff: oldOff, curOff: curOff, size: te.size, count: int64(te.children), remap: remap}}
	default:
		if te.size == me.size {
			return []evolveNode{{op: nodeCopy, oldOff: oldOff, curOff: curOff, size: te.size}}
		}
		if te.size < me.size {
			return []evolveNode{{op: nodeWiden, oldOff: oldOff, curOff: curOff, oldSize: te.size,
				newSize: me.size, signed: signedKind(te.kind), widened: true}}
		}
		c.plan.kindMismatch++
		return nil
	}
}

// matchChildren pairs the old entry's children with the current entry's by
// wire id. An old child the current layout does not name is the unknown
// census, counted ONCE per field per peer (§5.4) — never once per array
// element, which is why the recursion carries one census flag.
func (c *evolveCompiler) matchChildren(oi, ci int, oldOff, curOff int64, census bool, depth int) []evolveNode {
	te, me := c.oldAll[oi], c.curAll[ci]
	var out []evolveNode
	oc, oat := oi+1, oldOff
	for k := 0; k < te.children; k++ {
		oe := c.oldAll[oc]
		found, fat := -1, int64(0)
		cc, cat := ci+1, curOff
		for j := 0; j < me.children; j++ {
			ce := c.curAll[cc]
			if ce.id == oe.id {
				found, fat = cc, cat
				break
			}
			cat += ce.size
			cc += c.subtree(c.curAll, cc)
		}
		if found < 0 {
			if census {
				c.plan.unknown++
			}
		} else {
			out = append(out, c.match(oc, found, oat, fat, census, depth+1)...)
		}
		oat += oe.size
		oc += c.subtree(c.oldAll, oc)
	}
	return out
}

// array compiles a counted or fixed array: the count word verbatim (a counted
// array's), then one element loop whose body is the element plan at the two
// strides. The loop is min(their_n, my_n), from the layouts' own sizes, so a
// growing bound leaves its new slots at the declared default (§5.3 step 11).
func (c *evolveCompiler) array(oi, ci int, oldOff, curOff int64, census bool, depth int) []evolveNode {
	te, me := c.oldAll[oi], c.curAll[ci]
	if te.children < 1 || me.children < 1 || oi+1 >= len(c.oldAll) || ci+1 >= len(c.curAll) {
		return nil
	}
	oe, ce := c.oldAll[oi+1], c.curAll[ci+1]
	if oe.size <= 0 || ce.size <= 0 {
		return nil
	}
	head := int64(0)
	if c.counted[te.id] || c.counted[me.id] {
		head = 4
	}
	oldN := (te.size - head) / oe.size
	newN := (me.size - head) / ce.size
	n := min(newN, oldN)
	var out []evolveNode
	if head == 4 {
		// THE COUNT WORD IS HELD TO THE PLAN'S BOUND, NEVER COPIED FIRST
		// (docs/FIXED-FORM-ALGORITHM.md §5.8 row 4; docs/SPEC-TABLES.md
		// §3.4): a forged word refuses count_over_bound before any destination
		// byte. Only a lawful count lands, and it lands verbatim.
		out = append(out, evolveNode{op: nodeCountGuard, oldOff: oldOff, count: n})
		out = append(out, evolveNode{op: nodeCopy, oldOff: oldOff, curOff: curOff, size: 4})
	}
	body := c.match(oi+1, ci+1, head, head, census, depth+1)
	if len(body) > 0 {
		loop := evolveNode{op: nodeLoop, oldOff: oldOff, curOff: curOff, head: head,
			oldStride: oe.size, newStride: ce.size, count: n, body: body}
		if head == 4 {
			loop.counted = true
		}
		out = append(out, loop)
	}
	return out
}

// union compiles the tag and each arm the WRITER could have selected, guarded
// at the writer's tag width: a matching arm stamps the current ordinal and
// lands its payload; the appended arm the writer never wrote stays at the
// current default (docs/FIXED-FORM-VERSIONING-TESTS.md, union_append).
func (c *evolveCompiler) union(oi, ci int, oldOff, curOff int64, census bool, depth int) []evolveNode {
	te, me := c.oldAll[oi], c.curAll[ci]
	oldTag := te.size - widestArm(c.oldAll, oi)
	curTag := me.size - widestArm(c.curAll, ci)
	if oldTag < 1 || oldTag > 8 || curTag < 1 || curTag > 8 {
		c.plan.kindMismatch++
		return nil
	}
	// THE NONE TAG LANDS FIRST AND UNGUARDED (gotable's none const,
	// docs/FIXED-FORM-ALGORITHM.md §5.4, §5.8 row 12): a tag past the writer's
	// arm set selects no arm, leaves the tag at None and is counted once by the
	// bounds node below. The default image wrote the union at the reader's
	// default, so the None tag is stamped over it before any arm can answer.
	out := []evolveNode{{op: nodeSet, curOff: curOff, arg: 0, size: curTag}}
	if te.children > 0 {
		out = append(out, evolveNode{op: nodeForgedTag, oldOff: oldOff, tagWidth: oldTag, count: int64(te.children)})
	}
	oa := oi + 1
	for j := 0; j < te.children && oa < len(c.oldAll); j++ {
		oe := c.oldAll[oa]
		ca := ci + 1
		found, ordinal := -1, 0
		for k := 0; k < me.children && ca < len(c.curAll); k++ {
			ce := c.curAll[ca]
			if ce.id == oe.id {
				found, ordinal = ca, k+1
				break
			}
			ca += c.subtree(c.curAll, ca)
		}
		if found >= 0 {
			var body []evolveNode
			if t := c.armDefaults[oe.id]; t != "" {
				// THE SELECTED ARM'S OWN DECLARED DEFAULTS go down before the
				// old fields: the default image wrote the union at tag 0, so
				// another arm's payload there is the writer's slack. This is
				// what lands union_arm_payload_widen's appended y = 55.
				body = append(body, evolveNode{op: nodeDefault, curOff: curOff + curTag, defaultType: t})
			}
			body = append(body, c.match(oa, found, oldOff+oldTag, curOff+curTag, census, depth+1)...)
			body = append([]evolveNode{{op: nodeSet, curOff: curOff, arg: uint64(ordinal), size: curTag}}, body...)
			out = append(out, evolveNode{op: nodeIf, oldOff: oldOff, tagWidth: oldTag, arg: uint64(j + 1), body: body})
		}
		oa += c.subtree(c.oldAll, oa)
	}
	return out
}

// compileEvolvePlan compiles one older entry against the current layout
// (docs/FIXED-FORM-ALGORITHM.md §5.2's plan; docs/PORT-GOOSE.md, THE RUNTIME
// LAYOUT). The plan is a tree because a layout is: structs and array elements
// are matched by wire id, arrays loop at their two strides, and a union's arms
// answer to the writer's tag. A layout this walk cannot place is unsupported.
func compileEvolvePlan(st *ir.Struct, oldLayout, curLayout []byte) evolvePlan {
	oldAll := parseLayout(oldLayout)
	curAll := parseLayout(curLayout)
	if len(oldAll) == 0 || len(curAll) == 0 {
		return evolvePlan{}
	}
	counted := map[uint64]bool{}
	for _, en := range ir.TableFixedWalkRoot(st) {
		if en.Kind == ir.TableKindArray && en.Dst.Counted != 0 {
			counted[en.ID] = true
		}
	}
	arms := map[uint64]string{}
	collectArmDefaults(st, map[string]bool{}, arms)
	plan := evolvePlan{supported: true}
	c := &evolveCompiler{oldAll: oldAll, curAll: curAll, counted: counted, armDefaults: arms, plan: &plan}
	plan.nodes = c.match(0, 0, 0, 0, true, 0)
	return plan
}

// collectArmDefaults maps a union arm's wire id to the named struct its
// payload holds, so a selected arm's declared defaults can be laid before the
// old bytes the writer carried.
func collectArmDefaults(st *ir.Struct, seen map[string]bool, out map[uint64]string) {
	if st == nil || seen[st.Name] {
		return
	}
	seen[st.Name] = true
	for _, f := range st.Fields {
		if f.Type.Kind != ir.TNamed {
			continue
		}
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			collectArmDefaults(r, seen, out)
		case *ir.Union:
			for _, v := range r.Variants {
				if v.F == nil || v.F.Type.Kind != ir.TNamed {
					continue
				}
				if s, ok := v.F.Type.Ref.(*ir.Struct); ok {
					out[ir.TableWireId(v.WireName())] = s.Name
					collectArmDefaults(s, seen, out)
				}
			}
		}
	}
}

// emitEntry bakes one older entry: its layout bytes, record size and whether
// the image plan can land it.
func (e *fixedEvolveEmit) emitEntry(root string, idx int, en FixedEvolutionEntry, st *ir.Struct) {
	curLayout, _, _ := fixedRootFacts(st)
	plan := compileEvolvePlan(st, en.Layout, curLayout)
	e.p("let %sFixedEvolveLayout%d: const u8[:] = \"%s\";\n", root, idx, fixedGooseBytes(en.Layout))
	e.p("let %sFixedEvolveSupported%d: bool = %t;\n", root, idx, plan.supported)
	if !plan.supported {
		e.p("// %s's entry 0x%016x is not a widening this card lands: the general compiled plan is owed.\n", root, en.Wire)
	}
}
