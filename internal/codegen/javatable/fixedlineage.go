package javatable

// THE LINEAGE AS STATIC DATA ON THE JAVA LEG (docs/FIXED-FORM-ALGORITHM.md §5.2,
// COMPILE(lock, T)).
//
// A fixed table reads BACKWARD and never forward: a file is matched on the eight
// bytes of its header's hash against a lineage THE BUILD laid down, and nothing
// parses a stranger's layout, on any path. So this backend needs one input it
// never had — per fixed table, the locked layouts OLDEST FIRST, the current one
// LAST — and that input is the lock's: `lockfile.Lineage(lock, T)` and
// `lockfile.Floor(lock, T)` are the CALLER's calls, never this package's, so the
// disk is read in one place and a test can play the lock in one line (§5.9 #1).
//
// [GenerateLineage] is the entry point that takes it; [Generate] is the same call
// with no lineage, which is the NO-LOCK case: a unit that was never locked
// promises nothing, so its table carries the one entry it can always compute —
// its own — and refuses every other hash by name.

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

// FixedLineageEntry is ONE locked layout of one fixed table: the five facts
// §5.2's table asks the lock for, and the two the operator writes.
//
// Wire is the eight bytes the header and every record carry — the lineage's key
// and THE ONLY FACT A FILE IS MATCHED ON. Layout is the bytes verbatim, what
// LOAD compares and what PLAN walks. Record is the body size, taken from the
// lock and never from the file. Retired and Reason are the operator's:
// `schema lock --retire T@0x<hash> --reason "…"`, which keeps the entry and moves
// the floor.
type FixedLineageEntry struct {
	Wire    uint64
	Layout  []byte
	Record  int64
	Retired bool
	Reason  string
}

// FixedLineageOf computes the entry a unit's OWN build locks for table `name`:
// its layout bytes, the wire hash over them and the definitions digest, and its
// record size. It is what a lock records at commit, and it is how a test — or a
// build reading a sibling generation — states an older entry without a lock.
//
// THE HASH IS THE COMPILER'S OWN FUNCTION and never a private re-derivation
// (§5.9 #12): `ir.TableFixedLayoutHash(layout, st)` has no digest argument for a
// leg to omit, so this leg's hashes move WITH the reference the day §5.8 row 10
// lands.
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
			Record: 8 + fixedTypeBytes(st),
		}, true
	}
	return FixedLineageEntry{}, false
}

// fixedLineage is the lineage this emitter lays down for one table: the entries
// the build handed it, OLDEST FIRST, with the table's OWN entry appended last
// when the lineage does not already end on it (§5.9 #2 — `R.lineage` is never
// empty, so the identity plan is always reachable). The floor is 1 + the highest
// RETIRED index, or 0 when none is (§5.2), derived here rather than taken as an
// argument so the emitted constant cannot disagree with the emitted entries.
func (g *fixedGen) fixedLineage(st *ir.Struct, own FixedLineageEntry) ([]FixedLineageEntry, int) {
	entries := append([]FixedLineageEntry(nil), g.lineage[st.Name]...)
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
