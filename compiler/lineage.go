// THE LINEAGE THE DRIVER HANDS A BACKEND (docs/FIXED-FORM-ALGORITHM.md §5.2's
// COMPILE(lock, T), and §5.9 #1).
//
// A fixed table reads BACKWARD: a file is matched on the eight bytes of its
// header's hash against a lineage THE BUILD laid down, and nothing parses a
// stranger's layout on any path. So a backend needs one input it cannot compute
// — the layouts this unit has ALREADY shipped — and §5.9 #1 says exactly where
// it comes from: `lockfile.Open`, `lockfile.Lineage` and `lockfile.Floor` are
// THE CALLER'S THREE CALLS, so the disk is read in ONE place and a backend opens
// no file. This file is that one place.
//
// Until #921 nothing in the tree made the call. Every table backend that grew a
// `GenerateLineage` grew it for its own versioning harness, `schema generate`
// called the plain `Generate`, and a build of a unit whose committed lock held
// five layouts shipped a reader for one of them. The lock was right, the
// emitters were right, and the compiler dropped the lineage on the floor.
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A FixedLineage is one unit's locked lineage, read once per generate: per
// FIXED TABLE the lock's entries OLDEST FIRST — the current layout last — and
// the floor that entry's retired marks cut at.
//
// A unit with NO LOCK has no FixedLineage at all (a nil one): a unit that was
// never locked promises nothing, so every table carries the single entry it can
// always compute, its own (§5.9 #2). Every accessor is nil-safe for exactly
// that reason.
type FixedLineage struct {
	// lock is the parsed file itself, for the one backend that still reads it
	// directly — the C++ reference, whose lineage also comes from the sibling
	// filename convention §5.8 row 1 holds as an interim. The OPEN is shared
	// even there; the interpretation is not, yet.
	lock *lockfile.Unit

	// entries and floor are keyed by the table's DECLARED name, which is what a
	// backend's emitter holds; the LOCK is keyed by the wire name, and the
	// translation happens here once.
	//
	// AN ENTRY'S `Record` IS §5.2's `record_bytes`: THE WHOLE RECORD, `8 +
	// C(root)` — the record's own eight hash bytes and then the body — which is
	// NOT the number the lock carries. The lock records the BODY
	// (internal/lockfile/lineage.go's `LineageEntry.Record`,
	// `ir.TableFixedTypeBytes`) and [openFixedLineage] adds the eight once, so
	// the value every backend is handed is already the one its static data
	// needs. A backend adds nothing.
	entries map[string][]lockfile.LineageEntry
	floor   map[string]int
}

// fixedRecordHashBytes is the eight bytes of a record's OWN layout hash, the
// ones that stand in front of every record's body (docs/FIXED-FORM-ALGORITHM.md
// §5.2: "record_bytes is 8 + C(root)", and §5.3 step 9 divides the tail behind
// the layout by that whole number). Every table backend spells its own entry as
// `8 + body`; this is the same eight, for the entries that come off the lock.
const fixedRecordHashBytes = int64(8)

// lineageGenerator is the SECOND ENTRY POINT of §5.9 #1, as the driver sees it:
// a generator that takes the lock's lineage as data. A target implements it when
// its table backend exports `GenerateLineage`; a target that does not is called
// through plain [Generator.Generate] and emits a lineage of one, which is what
// it did before and still the right answer for a unit with no lock.
type lineageGenerator interface {
	GenerateLineage(u *ir.Unit, opts Options, lineage *FixedLineage) (map[string][]byte, error)
}

// unitSchemaPaths are the unit's *.schema files, which is where the lock lives
// — beside them, under [SchemaLockFileName].
func unitSchemaPaths(u *ir.Unit) []string {
	if u == nil {
		return nil
	}
	var paths []string
	for _, f := range u.Files {
		if f != nil && f.Path != "" {
			paths = append(paths, f.Path)
		}
	}
	return paths
}

// openFixedLineage makes the caller's three calls of §5.9 #1 for one unit: it
// locates and parses the lock beside the unit's schema files, and reads the
// lineage and the floor of every fixed table the unit declares.
//
// NO LOCK IS NOT AN ERROR and returns a nil lineage: a unit that has never been
// locked promises nothing. A lock that will not PARSE is an error, and a loud
// one — the alternative is shipping a reader for one layout out of five and
// saying nothing, which is the defect this whole path exists to close.
func openFixedLineage(u *ir.Unit) (*FixedLineage, error) {
	paths := unitSchemaPaths(u)
	if len(paths) == 0 {
		return nil, nil
	}
	lock, ok, err := lockfile.Open(paths)
	if err != nil {
		return nil, fmt.Errorf("reading the unit's %s for the fixed form's lineage (docs/FIXED-FORM-ALGORITHM.md §5.2): %w", SchemaLockFileName, err)
	}
	if !ok || lock == nil {
		return nil, nil
	}
	l := &FixedLineage{lock: lock, entries: map[string][]lockfile.LineageEntry{}, floor: map[string]int{}}
	for _, st := range ir.TableFixedRoots(u) {
		if st == nil {
			continue
		}
		entries := lockfile.Lineage(lock, st.WireName())
		if len(entries) == 0 {
			continue
		}
		floor := lockfile.Floor(lock, st.WireName())
		// ONE STATEMENT MADE TWICE, the way the lock's own lines are: the floor
		// is 1 + the highest retired index, and a backend that derives it from
		// the entries it was handed must land on the same number the lock's own
		// accessor reports. A disagreement is a defect here or there, never
		// something to emit.
		derived := 0
		for i, e := range entries {
			if e.Retired {
				derived = i + 1
			}
		}
		if derived != floor {
			return nil, fmt.Errorf("%s: the lock's floor for %s is %d and its retired marks say %d (docs/FIXED-FORM-ALGORITHM.md §5.2)", SchemaLockFileName, st.Name, floor, derived)
		}
		// §5.2's `record_bytes` IS THE WHOLE RECORD and the lock records the
		// BODY, so the eight hash bytes are added HERE, ONCE, for every backend
		// the driver hands entries to. The Java wiring (#920) found the other
		// way round: each leg's entry for its OWN layout is `8 + body` already
		// (gotable, ctable, rusttable all spell it that way), so passing the
		// lock's number through shipped a LOCKED unit's OLDER entries eight
		// bytes short — and step 9 divides the file's tail by that number, so
		// every record an older peer wrote is misread or refused as ragged. It
		// was invisible only because no harness fixture carries a lock and the
		// leg's own entry wins for the current layout.
		//
		// THE SLICE IS COPIED BEFORE THE NUMBER MOVES: `lockfile.Lineage` hands
		// back the parsed file's own entries, and the C++ reference reads that
		// same parsed lock through [FixedLineage.Lock] — where the conversion
		// is its own (`internal/codegen/cpptable/lineage.go`, `8+e.Record`).
		whole := make([]lockfile.LineageEntry, len(entries))
		copy(whole, entries)
		for i := range whole {
			whole[i].Record += fixedRecordHashBytes
		}
		l.entries[st.Name] = whole
		l.floor[st.Name] = floor
	}
	return l, nil
}

// Entries is the lineage of one fixed table, by DECLARED name: the lock's
// entries OLDEST FIRST, the current layout last. Nil for a table the lock does
// not carry, and for a unit with no lock.
//
// Each entry's `Record` is §5.2's `record_bytes` — THE WHOLE RECORD, eight hash
// bytes plus the body — and not the body size the lock's line carries. A
// backend writes it into its static data as it stands and adds nothing to it.
func (l *FixedLineage) Entries(table string) []lockfile.LineageEntry {
	if l == nil {
		return nil
	}
	return l.entries[table]
}

// Floor is §5.2's one number for one fixed table: 1 + the highest RETIRED index,
// 0 when none is. Below it a layout this build once served is retired and the
// answer is `layout_unsupported` — upgrade the client; outside the lineage it is
// `layout_newer` — ship the reader.
func (l *FixedLineage) Floor(table string) int {
	if l == nil {
		return 0
	}
	return l.floor[table]
}

// Lock is the parsed lock itself, for the C++ reference alone: its lineage is
// still built from sibling schema files by filename convention (§5.8 row 1's
// interim, `internal/codegen/cpptable/lineage.go`), so it needs the file and not
// the entries. Nil for a unit with no lock. The day that interim goes away this
// accessor goes with it.
func (l *FixedLineage) Lock() *lockfile.Unit {
	if l == nil {
		return nil
	}
	return l.lock
}
