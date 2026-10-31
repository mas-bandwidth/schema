// THE LINEAGE AS STATIC DATA (docs/FIXED-FORM-ALGORITHM.md §5.2, COMPILE(lock, T)).
//
// A fixed table reads BACKWARD and never forward: a file is matched on the
// eight bytes of its header's hash against a lineage the BUILD laid down, and
// nothing parses a stranger's layout on any path. So the backend takes one
// input it cannot compute, per fixed table the locked layouts OLDEST FIRST and
// the current one LAST, from the caller (compiler/lineage.go makes the lock's
// three calls, §5.9 #1). [GenerateLineage] is the entry point that takes it;
// [Generate] is the same call with no lineage, so a unit with no lock carries
// the single entry it can always compute, its own (§5.9 #2).
package luautable

import (
	"encoding/binary"
	"fmt"
	"sort"
	"sync"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// FixedLineageEntry is ONE locked layout of one fixed table: the facts §5.2's
// table asks the lock for, and the two the operator writes.
//
// Wire is the eight bytes the header and every record carry, the only fact a
// file is matched on. Layout is the bytes verbatim, what LOAD compares and
// what the plan walks. Record is §5.2's record_bytes, THE WHOLE RECORD: eight
// hash bytes plus the body, already added by the compiler for every backend
// (compiler/lineage.go), so this backend adds nothing. Retired and Reason are
// `schema lock --retire T@0x<hash> --reason "…"`'s, which keep the entry and
// move the floor.
type FixedLineageEntry struct {
	Wire    uint64
	Layout  []byte
	Record  int64
	Retired bool
	Reason  string
}

// fixedHashBytes is the eight bytes of a record's own hash, in front of every
// record body (§5.2: record_bytes is 8 + C(root)).
const fixedHashBytes = 8

// FixedLineageOf computes the entry a unit's OWN build locks for table name:
// its layout bytes, the wire hash over them and the definitions digest (the
// compiler's own ir.TableFixedLayoutHash, never a re-derivation, §5.9 #12),
// and its whole record size. It is how a test states an older entry without a
// lock.
func FixedLineageOf(u *ir.Unit, name string) (FixedLineageEntry, bool) {
	for _, st := range ir.TableFixedFormRoots(u) {
		if st.Name != name {
			continue
		}
		layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
		return FixedLineageEntry{
			Wire:   ir.TableFixedLayoutHash(layout, st),
			Layout: layout,
			Record: fixedHashBytes + ir.TableFixedTypeBytes(st),
		}, true
	}
	return FixedLineageEntry{}, false
}

// lineages hands one Generate call's lineage to the fixedform part, which runs
// per module behind the Part signature: keyed by the Gen it belongs to, set
// before the parts run and dropped after, so two concurrent generates never
// see each other's.
var lineages sync.Map // *luau.Gen -> map[string][]FixedLineageEntry

func lineageOf(g *luau.Gen) map[string][]FixedLineageEntry {
	v, ok := lineages.Load(g)
	if !ok {
		return nil
	}
	return v.(map[string][]FixedLineageEntry)
}

// fixedLineageFor is the lineage the emitter lays down for one table: the
// entries the build handed it, OLDEST FIRST, with the table's OWN entry
// appended last when the lineage does not already end on it (§5.9 #2, so the
// identity plan is always reachable), and the floor, 1 + the highest retired
// index or 0 when none is (§5.2).
func fixedLineageFor(handed []FixedLineageEntry, own FixedLineageEntry) ([]FixedLineageEntry, int) {
	entries := append([]FixedLineageEntry(nil), handed...)
	if last := len(entries) - 1; last < 0 || entries[last].Wire != own.Wire {
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

// refuseFixedLineage FAILS THE BUILD on a handed entry that is a bug in the
// lock (§5.9 #8, #26): a layout that does not parse under the reader's rules,
// or a record size that is not the eight hash bytes plus the body its own
// layout accounts for. It is never a wire event. Only the lineage of a root
// this form is emitted for is this form's business: a table past §3.4's
// 65536-byte ceiling keeps form 1, and the lock's entry for it is neither an
// error nor data (§5.2, #931).
func refuseFixedLineage(u *ir.Unit, lineage map[string][]FixedLineageEntry) error {
	emitted := map[string]bool{}
	for _, st := range ir.TableFixedFormRoots(u) {
		emitted[st.Name] = true
	}
	names := make([]string, 0, len(lineage))
	for name := range lineage {
		if emitted[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		for _, e := range lineage[name] {
			body, ok := fixedLayoutParses(e.Layout)
			if !ok {
				return fmt.Errorf("fixed table %s: the lineage entry 0x%016x is not a parseable layout: a bug in the LOCK, never a wire event, so the build fails here (docs/FIXED-FORM-ALGORITHM.md §5.2, §5.9 #8)", name, e.Wire)
			}
			if want := fixedHashBytes + int64(body); e.Record != want {
				return fmt.Errorf("fixed table %s: the lineage entry 0x%016x records a record size of %d and its own layout accounts for %d (the %d-byte hash and a %d-byte body): a bug in the LOCK, so the build fails here (§5.9 #26)", name, e.Wire, e.Record, want, fixedHashBytes, body)
			}
		}
	}
	return nil
}

// fixedLayoutParses answers whether b is a layout the reader would walk, and
// the ROOT's body size: the entry count fits the length exactly, the root is
// a table whose body is inside the record cap, every child sits inside its
// parent, and the pre-order walk consumes exactly the entries
// (runtime/schema_fixed.luau's parseNode and compile, rule for rule).
func fixedLayoutParses(b []byte) (int, bool) {
	if len(b) < 4 {
		return 0, false
	}
	count := int(binary.LittleEndian.Uint32(b))
	if count == 0 || count > 4096 || 4+count*ir.TableFixedEntryBytes != len(b) {
		return 0, false
	}
	at := func(i int) int { return 4 + i*ir.TableFixedEntryBytes }
	kind := func(i int) int { return int(b[at(i)+8]) }
	size := func(i int) int { return int(binary.LittleEndian.Uint32(b[at(i)+9:])) }
	kids := func(i int) int { return int(binary.LittleEndian.Uint32(b[at(i)+13:])) }
	var walk func(i, depth int) (int, bool)
	walk = func(i, depth int) (int, bool) {
		if i >= count || depth > ir.TableFixedMaxDepth || size(i) > ir.TableFixedRecordMaxBytes {
			return 0, false
		}
		next, sum := i+1, 0
		for range kids(i) {
			n, ok := walk(next, depth+1)
			if !ok {
				return 0, false
			}
			sum += size(next)
			next = n
		}
		if kind(i) == 13 && sum != size(i) {
			return 0, false
		}
		return next, true
	}
	if kind(0) != 13 || size(0) == 0 || size(0) > ir.TableFixedRecordMaxBytes {
		return 0, false
	}
	end, ok := walk(0, 0)
	if !ok || end != count {
		return 0, false
	}
	return size(0), true
}

// emitLineage writes one root's lineage beside its own fixed data: one Known
// per entry, OLDEST FIRST, the reader's own LAST and spelled by name, and the
// floor (§5.2). An older entry's layout rides as the entries its bytes hold,
// decoded here once so the runtime never parses one.
func emitLineage(g *luau.Gen, root *ir.Struct, own FixedLineageEntry) {
	entries, floor := fixedLineageFor(lineageOf(g)[root.Name], own)
	name := root.Name
	fx := g.Require("schema_fixed")
	g.Pf("-- %s: the fixed lineage, OLDEST FIRST, this build's own LAST, and the\n", name)
	g.Pf("-- floor below which a known layout is layout_unsupported (docs/FIXED-FORM-ALGORITHM.md §5.2)\n")
	g.Pf("%s.%sFixedLineage = table.freeze({\n", g.Self, name)
	for _, e := range entries {
		if e.Wire == own.Wire {
			g.Pf("\ttable.freeze({ hash = 0x%016xi, layout = %s.%sFixedLayout, layoutBytes = %d, recordBytes = %d }),\n",
				e.Wire, g.Self, name, len(e.Layout), e.Record)
			continue
		}
		if e.Retired {
			g.Pf("\t-- RETIRED: %s\n", e.Reason)
		}
		g.Pf("\ttable.freeze({ hash = 0x%016xi, layout = %s.fixed_layout({\n", e.Wire, fx)
		n := int(binary.LittleEndian.Uint32(e.Layout))
		for i := range n {
			p := 4 + i*ir.TableFixedEntryBytes
			g.Pf("\t\t{ id = 0x%016xi, kind = %d, size = %d, children = %d },\n",
				binary.LittleEndian.Uint64(e.Layout[p:]), e.Layout[p+8],
				binary.LittleEndian.Uint32(e.Layout[p+9:]), binary.LittleEndian.Uint32(e.Layout[p+13:]))
		}
		g.Pf("\t}), layoutBytes = %d, recordBytes = %d }),\n", len(e.Layout), e.Record)
	}
	g.Pf("})\n")
	g.Pf("%s.%sFixedFloor = %d\n\n", g.Self, name, floor)
}
