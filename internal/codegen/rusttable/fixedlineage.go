package rusttable

// THE LINEAGE AS STATIC DATA (docs/FIXED-FORM-ALGORITHM.md §5.2, COMPILE(lock, T)).
//
// A fixed table reads BACKWARD and never forward: a file is matched on the
// eight bytes of its header's hash against a lineage THE BUILD laid down, and
// nothing parses a stranger's layout, on any path. So this backend needs one
// input it never had: per fixed table, the locked layouts, OLDEST FIRST, the
// current one LAST. That input is the lock's — `lockfile.Lineage(lock, T)` and
// `lockfile.Floor(lock, T)`, read by the CALLER (§5.9 #1: the backend opens no
// file) — and a build that has no lock for a unit hands nothing, which leaves a
// table with the single entry it can always compute: its own.
//
// [GenerateLineage] is the entry point that takes it. [Generate] is the same
// call with no lineage, so a unit with no lock keeps exactly today's behaviour
// for its own hash and refuses every other one BY NAME.

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

// FixedLineageEntry is ONE locked layout of one fixed table: the five facts
// §5.2's table asks the lock for, and the two the operator writes.
//
// Wire is the eight bytes the header and every record carry — the lineage's key
// and THE ONLY FACT A FILE IS MATCHED ON. Layout is the bytes verbatim, what
// LOAD compares and what PLAN walks. Record is the body size plus the record's
// own hash, taken from the lock and never from the file. Retired and Reason are
// the operator's: `schema lock --retire T@0x<hash> --reason "…"`, which keeps
// the entry and moves the floor.
type FixedLineageEntry struct {
	Wire    uint64
	Layout  []byte
	Record  int64
	Retired bool
	Reason  string
}

// FixedLineageOf computes the entry a unit's OWN build locks for table `name`:
// its layout bytes, the wire hash over them and the definitions digest
// (ir.TableFixedLayoutHash computes the digest AT the hash site, §5.9 #12), and
// its record size. It is what a lock records at commit, and it is how a test —
// or a build reading a sibling generation — states an older entry with no lock.
func FixedLineageOf(u *ir.Unit, name string) (FixedLineageEntry, bool) {
	for _, st := range ir.TableFixedRoots(u) {
		if st.Name != name {
			continue
		}
		layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
		return FixedLineageEntry{
			Wire:   ir.TableFixedLayoutHash(layout, st),
			Layout: layout,
			Record: int64(8 + ir.TableFixedTypeBytes(st)),
		}, true
	}
	return FixedLineageEntry{}, false
}

// fixedLineage is the lineage the emitter lays down for one table: the entries
// the build handed it, OLDEST FIRST, with the table's OWN entry appended last
// when the lineage does not already end on it (§5.9 #2, so R.own_hash always
// resolves and the IDENTITY plan is always reachable). The floor is 1 + the
// highest retired index, or 0 when none is (§5.2).
func (g *gen) fixedLineage(st *ir.Struct, own FixedLineageEntry) ([]FixedLineageEntry, int) {
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
