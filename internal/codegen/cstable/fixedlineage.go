package cstable

// THE LINEAGE AS STATIC DATA (docs/FIXED-FORM-ALGORITHM.md §5.2, COMPILE(lock, T)).
//
// A fixed table reads BACKWARD and never forward: a file is matched on the
// eight bytes of its header's hash against a lineage the BUILD laid down, and
// NOTHING PARSES A STRANGER'S LAYOUT, ON ANY PATH. So this backend needs one
// input it never had: per fixed table, the locked layouts, OLDEST FIRST, the
// current one LAST. That input is the lock's — `lockfile.Lineage(lock, T)` and
// `lockfile.Floor(lock, T)`, the caller's two calls — and a build with no lock
// for a unit hands nothing, which leaves a table the single entry it can always
// compute: its own (§5.9 #1, #2).
//
// [GenerateLineage] is the second entry point §5.9 #1 names; [Generate] is the
// same call with no lineage, so a unit with no lock keeps exactly today's
// behaviour for its own hash and refuses every other one BY NAME.

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

// FixedLineageEntry is ONE locked layout of one fixed table: the four facts
// §5.2's table asks the lock for, and the two the operator writes — the SIX
// FACTS lockfile.LineageEntry carries, under the same names, so the caller that
// reads the lock hands this backend its rows field for field and a fact cannot
// go missing in the hand-off.
//
// Wire is the eight bytes the header and every record carry — the lineage's key
// and THE ONLY FACT A FILE IS MATCHED ON. Layout is the bytes verbatim, what
// LOAD compares and what PLAN walks. Digest is §13's DEFINITIONS DIGEST: the
// facts of §2 that are not wire shape — each range, each flags bit count, each
// `bits(N)`, each `fixed` I and F, each reader-side limit. It is what the
// layout bytes cannot carry and the hash must bind, it NEVER RIDES THE WIRE,
// and it is empty for a table that declares none of them (which leaves Wire
// equal to a hash of the layout bytes alone). Record is the body size, taken
// from the lock and NEVER from the file. Retired and Reason are the operator's:
// `schema lock --retire T@0x<hash> --reason "…"`, which keeps the entry and
// moves the floor.
//
// Digest is carried and not recomputed, for the reason lockfile gives: a
// HISTORICAL layout has no live *Struct to derive it from, so the two runs that
// made the hash are the only record of it. Nothing in the generated C# reads it
// — a file is matched on Wire and held to a byte comparison against Layout —
// but dropping it here would make this backend the one place in the chain where
// a locked fact is silently discarded, and a later gate over the lock's rows
// would have nothing on this side to compare.
type FixedLineageEntry struct {
	Wire    uint64
	Layout  []byte
	Digest  []byte
	Record  int64
	Retired bool
	Reason  string
}

// FixedLineageOf computes the entry a unit's OWN build locks for table `name`:
// its layout bytes, the wire hash over them and the definitions digest (through
// ir.TableFixedLayoutHash and never a private re-derivation, §5.9 #12), and its
// record size. It is what a lock records at commit, and it is how a test — or a
// build reading a sibling generation — states an older entry without a lock.
func FixedLineageOf(u *ir.Unit, name string) (FixedLineageEntry, bool) {
	for _, st := range ir.TableFixedRoots(u) {
		if st.Name != name {
			continue
		}
		g := &tableGen{unit: u}
		layout := fixedLayoutOf(g, st)
		return FixedLineageEntry{
			Wire:   ir.TableFixedLayoutHash(layout, st),
			Layout: layout,
			Digest: ir.TableFixedDefinitionsDigest(st),
			Record: 8 + fixedTypeBytes(st),
		}, true
	}
	return FixedLineageEntry{}, false
}

// fixedLayoutOf walks the table the one way §1 states — pre-order, the root
// first, every entry followed by its own children — and returns the LAYOUT
// BYTES. It is the same walk emitFixedRoot makes; the plan, the dst rows and
// the slots it also builds are of no interest here and are dropped.
func fixedLayoutOf(g *tableGen, st *ir.Struct) []byte {
	b := &fixedRootBuild{g: g, argw: 1}
	b.entries = append(b.entries, fixedBlockEntry{
		id:       ir.TableWireId(st.WireName()),
		kind:     ir.TableKindTable,
		size:     fixedTypeBytes(st),
		children: len(st.Fields),
		note:     st.Name,
	})
	b.dstRows = append(b.dstRows, "new TableFixedDst(0, 0, 0, 0, 0)")
	wireOff := int64(0)
	for _, f := range st.Fields {
		b.buildField(st, f, "t", 0, &wireOff, kFixedNoGuard, 0)
	}
	return fixedBlockBytes(b.entries)
}

// fixedLineage is the lineage the emitter lays down for one table: the entries
// the build handed it, OLDEST FIRST, with the table's OWN entry appended last
// when the lineage does not already end on it (§5.9 #2). The floor is 1 + the
// highest retired index, or 0 when none is (§5.2).
func (g *tableGen) fixedLineage(st *ir.Struct, own FixedLineageEntry) ([]FixedLineageEntry, int) {
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
