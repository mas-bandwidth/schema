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
	"sync"

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
}

// FixedLineageOf computes the entry a unit's OWN build locks for table `name`:
// its layout bytes, the wire hash over them, and its record size. It is what a
// lock records at commit, and it is how a test states an older entry without a
// lock.
func FixedLineageOf(u *ir.Unit, name string) (FixedLineageEntry, bool) {
	for _, st := range ir.TableFixedRoots(u) {
		if st.Name != name {
			continue
		}
		w := fixedWalkRoot(st)
		layout := fixedLayoutBytes(w.entries)
		return FixedLineageEntry{
			Wire:   ir.TableFixedLayoutHash(layout, st),
			Layout: layout,
			Record: int64(fixedHashBytes) + fixedTypeBytes(st),
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

// fixedFieldPlan is one destination field's row of a lineage entry's plan: the
// source offset the older writer put the field at, its source width, and
// whether the value moves unchanged or widens. A field the older layout does
// not carry has no row and keeps the constructor's declared default
// (docs/FIXED-FORM-ALGORITHM.md §5.2, §5.3 step 8, §4.2).
type fixedFieldPlan struct {
	field   *ir.Field
	srcAt   int64
	srcSize int64
	exact   bool
	widen   bool
}

// fixedPlanFor walks an older entry's layout against the reader's own field
// tree and answers the plan FixedLoad runs for that entry. COMPILE lays the
// plan down at build time and LOAD selects it by hash; nothing here parses a
// file (docs/FIXED-FORM-ALGORITHM.md §5.2, §5.3).
func fixedPlanFor(st *ir.Struct, layout []byte) []fixedFieldPlan {
	old, ok := fixedLayoutEntries(layout)
	if !ok || len(old) == 0 {
		return nil
	}
	cur := fixedWalkRoot(st).entries
	oldTop := fixedTopFields(old)
	curTop := fixedTopFields(cur)
	byID := make(map[uint64]fixedTopField, len(oldTop))
	for _, e := range oldTop {
		if _, seen := byID[e.entry.id]; !seen {
			byID[e.entry.id] = e
		}
	}
	var plan []fixedFieldPlan
	for i, f := range st.Fields {
		if i >= len(curTop) {
			break
		}
		ce := curTop[i]
		oe, found := byID[ce.entry.id]
		if !found || f.Type.Optional {
			continue
		}
		switch {
		case oe.entry.kind == ce.entry.kind && oe.entry.size == ce.entry.size:
			plan = append(plan, fixedFieldPlan{field: f, srcAt: oe.at, srcSize: oe.entry.size, exact: true})
		case fixedPlanWidens(oe.entry.kind, ce.entry.kind) && oe.entry.size <= ce.entry.size:
			plan = append(plan, fixedFieldPlan{field: f, srcAt: oe.at, srcSize: oe.entry.size, widen: true})
		}
	}
	return plan
}

// fixedTopField is one ROOT-LEVEL entry of a layout and the body offset it sits
// at, so a plan names the older writer's offset and never the reader's.
type fixedTopField struct {
	entry fixedLayoutEntry
	at    int64
}

func fixedTopFields(entries []fixedLayoutEntry) []fixedTopField {
	if len(entries) == 0 {
		return nil
	}
	out := make([]fixedTopField, 0, entries[0].children)
	at := int64(0)
	for i := 1; i < len(entries) && len(out) < entries[0].children; {
		e := entries[i]
		out = append(out, fixedTopField{entry: e, at: at})
		at += e.size
		i += fixedSubtree(entries, i)
	}
	return out
}

func fixedSubtree(entries []fixedLayoutEntry, i int) int {
	pending, n, at := int64(1), 0, i
	for pending > 0 && at < len(entries) {
		pending--
		n++
		pending += int64(entries[at].children)
		at++
	}
	return n
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
