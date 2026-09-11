package jstable

// THE LINEAGE AS STATIC DATA (docs/FIXED-FORM-ALGORITHM.md §5.2, COMPILE(lock, T)).
//
// A fixed table reads BACKWARD and never forward: a file is matched on the
// eight bytes of its header's hash against a lineage the BUILD laid down, and
// **nothing parses a stranger's layout, on any path**. So this backend needs one
// input it never had: per fixed table, the locked layouts, OLDEST FIRST, the
// current one LAST. That input is the lock's — `lockfile.Lineage(lock, T)` and
// `lockfile.Floor(lock, T)`, the caller's three calls — and a build with no lock
// for a unit hands nothing, which leaves a table with the single entry it can
// always compute: its own (§5.9 #1, #2).
//
// [GenerateLineage] is the entry point that takes it. [Generate] is the same
// call with no lineage, so a unit with no lock keeps exactly today's behaviour
// for its own hash and refuses every other one BY NAME.

import (
	"encoding/base64"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// FixedLineageEntry is ONE locked layout of one fixed table: the five facts
// §5.2's table asks the lock for, and the two the operator writes.
//
// Wire is the eight bytes the header and every record carry — the lineage's key
// and THE ONLY FACT A FILE IS MATCHED ON. Layout is the bytes verbatim, what
// LOAD compares and what PLAN walks. Record is the body size, taken from the
// lock and never from the file. Retired and Reason are the operator's:
// `schema lock --retire T@0x<hash> --reason "…"`, which keeps the entry and
// moves the floor.
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
// THE HASH IS THE COMPILER'S OWN `ir.TableFixedLayoutHash(layout, st)` and never
// a private re-derivation (§5.9 #12): the signature has no digest argument for a
// leg to omit, so a hash this backend publishes moves WITH the reference the day
// §5.8 row 10 lands.
func FixedLineageOf(u *ir.Unit, name string) (FixedLineageEntry, bool) {
	for _, st := range jsFixedUnitRoots(u) {
		if st.Name != name {
			continue
		}
		w := fixedWalkRoot(st)
		layout := fixedLayoutBytes(w.entries)
		return FixedLineageEntry{
			Wire:   ir.TableFixedLayoutHash(layout, st),
			Layout: layout,
			Record: fixedHashBytes + fixedTypeBytes(st),
		}, true
	}
	return FixedLineageEntry{}, false
}

// fixedLineage is the lineage the emitter lays down for one table: the entries
// the build handed it, OLDEST FIRST, with the table's OWN entry appended last
// when the lineage does not already end on it (§5.9 #2 — R.lineage is never
// empty, so the identity plan is always reachable). The floor is 1 + the highest
// retired index, or 0 when none is (§5.2).
func fixedLineageFor(handed []FixedLineageEntry, own FixedLineageEntry) ([]FixedLineageEntry, int) {
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

// fixedLayoutBase64 is how a known layout's bytes ride in a JavaScript bundle.
//
// BUNDLE SIZE IS A COST THE BILL NAMES (bill §11): a lineage of five layouts
// emitted as `new Uint8Array([0x0d, 0x00, …])` literals is six source bytes per
// wire byte, and a lineage grows forever — a retired entry stays in it. A BASE64
// STRING CONSTANT DECODED ONCE at module load is four source bytes per three
// wire bytes and one decode per table per process, off every load path, which is
// the same timing §5.9 #3 already admits for the plans themselves. The reader's
// OWN layout keeps its Uint8Array literal: it is the one a record is written
// from, it is already in the module, and moving it would move the write path.
func fixedLayoutBase64(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

// jsFixedUnitRoots is every fixed-form root of a unit, in file order: §5.9 #9's
// rule that EVERY declared `fixed table` is a root with its own layout, its own
// hash and its own lineage, held to this backend's own refusal filter.
func jsFixedUnitRoots(u *ir.Unit) []*ir.Struct {
	var out []*ir.Struct
	for _, f := range u.Files {
		out = append(out, fixedRoots(u, f.Tables)...)
	}
	return out
}
