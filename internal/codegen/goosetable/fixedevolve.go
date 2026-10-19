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
	for i := 0; i < n; i++ {
		at := 4 + 17*i
		var id uint64
		for b := 0; b < 8; b++ {
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

// topLevel is the root and its immediate fields, each subtree skipped: the
// fields that decide an append-only widening.
func topLevel(entries []rawEntry) []rawEntry {
	if len(entries) == 0 {
		return nil
	}
	var endOf func(i int) int
	endOf = func(i int) int {
		end := i + 1
		for c := 0; c < entries[i].children && end < len(entries); c++ {
			end = endOf(end)
		}
		return end
	}
	out := []rawEntry{entries[0]}
	for i := 1; i < len(entries); i = endOf(i) {
		out = append(out, entries[i])
	}
	return out
}

// appendOnly answers whether an OLD record body is a prefix of the current
// fields' wire images: the old top-level fields are the current ones in the
// same order, at the same ids, kinds and sizes. That is the class of widening
// this card lands by copying the old body over a default image; a field whose
// size or kind moved needs the compiled plan (docs/FIXED-FORM-ALGORITHM.md
// §5.2), which is named as owed in the report.
func appendOnly(oldLayout []byte, currentLayout []byte) bool {
	old, cur := topLevel(parseLayout(oldLayout)), topLevel(parseLayout(currentLayout))
	if len(old) < 1 || len(cur) < len(old) {
		return false
	}
	// The root's size and child count move with the appended field; the test
	// is on the fields, which keep their ids, kinds and sizes.
	for i := 1; i < len(old); i++ {
		// An appended enum variant or union arm moves the field's child count
		// but not its wire size, so the comparison is id, kind and size.
		if old[i].id != cur[i].id || old[i].kind != cur[i].kind || old[i].size != cur[i].size {
			return false
		}
	}
	return true
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
	e.p("    if pick < %sFixedEvolveFloor {\n        report.hash = h;\n        return schema_rt::fixed_evolve_refuse(report, schema_rt::FixedRefuseLayoutUnsupported, false);\n    }\n", n)
	e.p("    if !supported {\n        return schema_rt::fixed_evolve_refuse(report, schema_rt::FixedRefuseLayoutMalformed, false);\n    }\n")
	e.p("    if !schema_rt::fixed_evolve_layout_ok(data, layout_length, known) {\n        return schema_rt::fixed_evolve_refuse(report, schema_rt::FixedRefuseLayoutMalformed, false);\n    }\n")
	e.p("    let rest = data.len - schema_rt::FixedHeaderBytes - 4 - layout_length;\n")
	e.p("    if record_bytes <= 8 || rest %% record_bytes != 0 {\n        return schema_rt::fixed_evolve_refuse(report, schema_rt::FixedRefuseTruncated, true);\n    }\n")
	e.p("    let count = rest / record_bytes;\n")
	e.p("    if count > (values.len as i64) {\n        return schema_rt::fixed_evolve_refuse(report, schema_rt::FixedRefuseBatchTooLarge, false);\n    }\n")
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
		for ci, c := range plan.copies {
			q := fmt.Sprintf("q%d", ci)
			nCopy := c.oldSize
			if c.newSize < nCopy {
				nCopy = c.newSize
			}
			e.p("            var %s: i64 = 0;\n", q)
			e.p("            while %s < %d {\n                img[%d + %s] = data[body + %d + %s];\n                %s += 1;\n            }\n", q, nCopy, c.curOff, q, c.oldOff, q, q)
			if c.oldSize < c.newSize {
				high := c.newSize - c.oldSize
				if c.signFill {
					fill := fmt.Sprintf("fill%d", ci)
					e.p("            var %s: u8 = 0;\n", fill)
					e.p("            if (data[body + %d] & 0x80) != 0 { %s = 255; }\n", c.oldOff+c.oldSize-1, fill)
					e.p("            %s = 0;\n            while %s < %d {\n                img[%d + %s] = %s;\n                %s += 1;\n            }\n", q, q, high, c.curOff+c.oldSize, q, fill, q)
				} else if c.zeroFill {
					e.p("            %s = 0;\n            while %s < %d {\n                img[%d + %s] = 0;\n                %s += 1;\n            }\n", q, q, high, c.curOff+c.oldSize, q, q)
				}
			}
		}
		if plan.widened > 0 {
			e.p("            report.widened += %d;\n", plan.widened)
		}
		e.p("            let r = _%sFixedScan(img, 0);\n", n)
		e.p("            if r != 0 { return schema_rt::fixed_evolve_refuse(report, r, false); }\n")
		e.p("            _%sFixedRead(img, 0, values[k]);\n", n)
	}
	e.p("        }\n")
	e.p("        report.clamped += _%sFixedClamp(values[k]);\n", n)
	e.p("        k += 1;\n")
	e.p("    }\n")
	e.p("    return count;\n")
	e.p("}\n")
}

// evolveCopy is one field's move from the old record image to the current
// one: the old field's wire offset and size, the current field's offset and
// size, and whether a widening fills its high bytes by sign or zero.
type evolveCopy struct {
	curOff, oldOff     int64
	oldSize, newSize   int64
	signFill, zeroFill bool
	widened            bool
}

// evolvePlan is one older entry's image plan. The old field is placed at the
// current field's offset; the bytes the old file has not are the declared
// defaults the prefill already laid (§4.3). A field whose size or kind moved
// down, or a kind this card cannot move, makes the plan unsupported.
type evolvePlan struct {
	copies    []evolveCopy
	widened   int
	supported bool
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

// fullIndex is the position of id in the pre-order entries.
func fullIndex(entries []rawEntry, id uint64) int {
	for i := range entries {
		if entries[i].id == id {
			return i
		}
	}
	return -1
}

// compileEvolvePlan lays the old record image into the current one
// (docs/FIXED-FORM-ALGORITHM.md §5.2's plan, in the one shape this card
// lands: field blocks by wire id, a widened leaf extended, the tail default).
func compileEvolvePlan(st *ir.Struct, oldLayout, curLayout []byte) evolvePlan {
	oldAll := parseLayout(oldLayout)
	curAll := parseLayout(curLayout)
	oldTop := topLevel(oldAll)
	curTop := topLevel(curAll)
	if len(oldTop) == 0 || len(curTop) != len(st.Fields)+1 {
		return evolvePlan{}
	}
	oldOffsets := map[uint64]int64{}
	var oldOff int64
	for _, oe := range oldTop[1:] {
		oldOffsets[oe.id] = oldOff
		oldOff += oe.size
	}
	plan := evolvePlan{supported: true}
	var curOff int64
	for i, f := range st.Fields {
		newE := curTop[i+1]
		oldE := rawEntry{}
		found := false
		for _, oe := range oldTop[1:] {
			if oe.id == newE.id {
				oldE, found = oe, true
				break
			}
		}
		if !found {
			curOff += ir.TableFixedFieldBytes(f)
			continue
		}
		if oldE.size > newE.size {
			return evolvePlan{}
		}
		sign := false
		switch {
		case oldE.kind == newE.kind:
			if oldE.kind == 35 {
				return evolvePlan{}
			}
			if oldE.kind == 14 {
				oi := fullIndex(oldAll, oldE.id)
				ci := fullIndex(curAll, newE.id)
				if oi < 0 || ci < 0 || oi+1 >= len(oldAll) || ci+1 >= len(curAll) ||
					oldAll[oi+1].kind != curAll[ci+1].kind || oldAll[oi+1].size != curAll[ci+1].size {
					return evolvePlan{}
				}
			}
		case signedKind(oldE.kind) && signedKind(newE.kind):
			sign = true
		case unsignedKind(oldE.kind) && unsignedKind(newE.kind):
		default:
			return evolvePlan{}
		}
		c := evolveCopy{curOff: curOff, oldOff: oldOffsets[oldE.id], oldSize: oldE.size, newSize: newE.size}
		if oldE.size < newE.size {
			if sign {
				c.signFill = true
			} else if oldE.kind == 30 || unsignedKind(oldE.kind) {
				// A scalar or ordinal tail is zero; a composite's tail is the
				// declared default the prefill already laid (§4.3).
				c.zeroFill = true
			}
			if oldE.kind == 30 || signedKind(oldE.kind) || unsignedKind(oldE.kind) {
				c.widened = true
				plan.widened += 1
			}
		}
		plan.copies = append(plan.copies, c)
		curOff += newE.size
	}
	return plan
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

// findRoot resolves a root by name in the unit (the emitter passes one of
// fixedRootsOf's, so the lookup cannot fail in practice).
func (e *fixedEvolveEmit) findRoot(name string) *ir.Struct {
	for _, st := range ir.TableFixedFormRoots(e.g.Unit) {
		if st.Name == name {
			return st
		}
	}
	panic("fixedevolve: root " + name + " not in the unit")
}
