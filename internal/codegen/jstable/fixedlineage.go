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
	"fmt"
	"sort"

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

// ---------------------------------------------------------------------------
// A HANDED ENTRY THAT IS A LOCK BUG FAILS THE BUILD (§5.9 #8, §5.9 #26)
// ---------------------------------------------------------------------------
//
// §5.9 #8 and #26 are one rule: a lineage entry whose LAYOUT is not a parseable
// layout, or whose RECORD SIZE is `8` or less or not what its own layout
// accounts for, is A BUG IN THE LOCK — so **the BUILD FAILS**, naming the table
// and the entry's hash. It is not a wire event: routing it into LOAD would
// report a lock bug as `malformed` on a file that did nothing wrong, and
// routing it into §5.3 step 9 would report it as arithmetic over a file's tail.
//
// THE RUN-TIME NAME STAYS, and is not the same path. `TableFixedLineagePlans`
// still answers `layout_malformed` on an entry it cannot parse, because this
// backend's emitted reader takes its lineage AS DATA at module load and a HOST
// handing entries in at run time cannot fail a build that already ran — that is
// the second half of #8, and the half this function makes unreachable from the
// generator. The emitted runtime's comment at that point claims "the generator
// holds that", and before this it did not.
//
// WHAT "PARSEABLE" MEANS HERE IS THE READER'S OWN FOUR RULES, re-stated in Go
// over the same bytes (fixedruntime.go, TableFixedParseLayout): the entry count
// fits the length exactly, the root is a TABLE with a body inside the record
// cap, every kind is in the closed set and used as its definition allows with
// every size the one its children account for, and the pre-order walk consumes
// EXACTLY the entries. A generator check looser than the reader's would pass an
// entry the reader then refuses at run time, which is the hole this closes; a
// check stricter than it would fail a build over a layout that reads fine.
func refuseFixedLineage(lineage map[string][]FixedLineageEntry) error {
	names := make([]string, 0, len(lineage))
	for name := range lineage {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, e := range lineage[name] {
			root, ok := fixedLayoutParses(e.Layout)
			if !ok {
				return fmt.Errorf("fixed table %s: the lineage entry 0x%016x is not a parseable layout — it is a bug in the LOCK and never a wire event, so the build fails here (docs/FIXED-FORM-ALGORITHM.md §5.2, §5.9 #8)", name, e.Wire)
			}
			// A RETIRED ENTRY IS STILL CHECKED. The floor stops a FILE from
			// selecting it; it does not make the recorded bytes lawful, and an
			// entry nobody may read is still a lock the operator has to fix.
			if e.Record <= fixedHashBytes {
				return fmt.Errorf("fixed table %s: the lineage entry 0x%016x records a record size of %d, which is not a record — the hash alone is %d bytes, so a lawful entry is larger; it is a bug in the LOCK and the build fails here, never §5.3 step 9's arithmetic over a file's tail (§5.9 #26)", name, e.Wire, e.Record, fixedHashBytes)
			}
			if want := fixedHashBytes + int64(root); e.Record != want {
				return fmt.Errorf("fixed table %s: the lineage entry 0x%016x records a record size of %d, and its own layout accounts for %d (the %d-byte hash and a %d-byte body) — it is a bug in the LOCK and the build fails here (§5.9 #26)", name, e.Wire, e.Record, want, fixedHashBytes, root)
			}
		}
	}
	return nil
}

// fixedLayoutParses answers whether `b` is a layout this form's reader would
// parse, and the ROOT's body size, which is what #26 holds a record size to.
func fixedLayoutParses(b []byte) (int, bool) {
	if len(b) < 4 {
		return 0, false
	}
	count := int(fixedLayoutU32(b, 0))
	// 1. THE ENTRY COUNT FITS THE LAYOUT LENGTH EXACTLY
	if count == 0 || count*fixedEntryBytes+4 != len(b) {
		return 0, false
	}
	v := fixedLayoutWalk{bytes: b, count: count}
	// 2. THE ROOT IS A TABLE, and its size is the record's body
	if v.kind(0) != 13 {
		return 0, false
	}
	root := v.size(0)
	if root == 0 || root > fixedLayoutRecordMaxBytes {
		return 0, false
	}
	// 3 and 4. EVERY ENTRY LAWFUL, and the walk consumes exactly the entries
	used, ok := v.check(0, 0)
	if !ok || used != count {
		return 0, false
	}
	return root, true
}

// fixedLayoutWalk reads the layout's entries the way the emitted reader's view
// does: a u64 id, a kind byte, a u32 size and a u32 child count, in pre-order.
type fixedLayoutWalk struct {
	bytes []byte
	count int
}

const (
	fixedLayoutRecordMaxBytes = 65536 // TableFixedRecordMaxBytes
	fixedLayoutMaxDepth       = 64    // TableFixedMaxDepth
)

func (v fixedLayoutWalk) at(i int) int   { return 4 + i*fixedEntryBytes }
func (v fixedLayoutWalk) kind(i int) int { return int(v.bytes[v.at(i)+8]) }
func (v fixedLayoutWalk) size(i int) int { return int(fixedLayoutU32(v.bytes, v.at(i)+9)) }
func (v fixedLayoutWalk) kids(i int) int { return int(fixedLayoutU32(v.bytes, v.at(i)+13)) }

func fixedLayoutU32(b []byte, at int) uint32 {
	return uint32(b[at]) | uint32(b[at+1])<<8 | uint32(b[at+2])<<16 | uint32(b[at+3])<<24
}

// check validates the subtree rooted at i and answers how many entries it
// occupies — the same arithmetic, rule for rule, as the emitted reader's
// TableFixedCheckEntry.
func (v fixedLayoutWalk) check(i, depth int) (int, bool) {
	if i < 0 || i >= v.count || depth > fixedLayoutMaxDepth {
		return 0, false
	}
	kind, size := v.kind(i), v.size(i)
	if !fixedLayoutKnownKind(kind) || size > fixedLayoutRecordMaxBytes {
		return 0, false
	}
	kids := v.kids(i)
	if kids < 0 || kids > v.count {
		return 0, false
	}
	at, sum, widest := i+1, 0, 0
	firstSize, secondSize, firstKind, firstKids := 0, 0, 0, 0
	kidsAreVariants := true
	for k := 0; k < kids; k++ {
		child := at
		used, ok := v.check(child, depth+1)
		if !ok {
			return 0, false
		}
		childSize := v.size(child)
		if k == 0 {
			firstSize, firstKind, firstKids = childSize, v.kind(child), v.kids(child)
		}
		if k == 1 {
			secondSize = childSize
		}
		if v.kind(child) != 32 {
			kidsAreVariants = false
		}
		sum += childSize
		if childSize > widest {
			widest = childSize
		}
		if sum > fixedLayoutRecordMaxBytes {
			return 0, false
		}
		at += used
	}
	switch leaf := fixedLayoutLeafSize(kind, size); leaf {
	case 0:
		return 0, false
	case 1:
		if kids != 0 {
			return 0, false
		}
		return at - i, true
	}
	switch kind {
	case 13: // a TABLE: its size is the SUM of its fields'
		if size != sum {
			return 0, false
		}
	case 35: // the OPTIONAL WRAPPER: one present byte in front of one child
		if kids != 1 || size != sum+1 {
			return 0, false
		}
	case 14: // an ARRAY: a whole number of elements, behind a count or not
		if kids != 1 || firstSize == 0 {
			return 0, false
		}
		bare := size%firstSize == 0
		counted := size >= 4 && (size-4)%firstSize == 0
		if !bare && !counted {
			return 0, false
		}
	case 16: // an ENUM-KEYED array: the KEY ENUM then the ELEMENT
		if kids != 2 || firstKind != 30 || secondSize == 0 || size%secondSize != 0 {
			return 0, false
		}
		if size/secondSize < firstKids {
			return 0, false
		}
	case 15: // a UNION: the TAG at its own width, then the WIDEST ARM
		if kids == 0 || size <= widest || !fixedLayoutOrdinalWidth(size-widest) {
			return 0, false
		}
	case 30: // an ENUM: the ordinal's storage width, children are VARIANTS
		if !fixedLayoutOrdinalWidth(size) {
			return 0, false
		}
		if kids != 0 && !kidsAreVariants {
			return 0, false
		}
	case 12: // string(N): the length, then N bytes
		if kids != 0 || size < 4 {
			return 0, false
		}
	case 33: // wstring(N): the length in CODE UNITS, then 2N bytes
		if kids != 0 || size < 4 || (size-4)%2 != 0 {
			return 0, false
		}
	default:
		return 0, false
	}
	return at - i, true
}

// fixedLayoutKnownKind is the CLOSED KIND SET: §3's kinds 1..30, the no-payload
// variant 32, wstring 33, and the optional wrapper 35 this form's layout adds.
func fixedLayoutKnownKind(kind int) bool {
	return (kind >= 1 && kind <= 30) || kind == 32 || kind == 33 || kind == 35
}

func fixedLayoutOrdinalWidth(n int) bool { return n == 1 || n == 2 || n == 4 || n == 8 }

// fixedLayoutLeafSize answers 1 when the kind is a leaf at a size it admits, 0
// when it is a leaf at a size it does not, and -1 when it is not a leaf.
func fixedLayoutLeafSize(kind, size int) int {
	ok := func(sizes ...int) int {
		for _, s := range sizes {
			if size == s {
				return 1
			}
		}
		return 0
	}
	switch kind {
	case 1, 2:
		return ok(1)
	case 3:
		return ok(2)
	case 4:
		return ok(4)
	case 5:
		return ok(8)
	case 6: // u8, and bits( 1 .. 8 ) at its declared storage width
		return ok(1, 4)
	case 7: // u16, and bits( 9 .. 16 )
		return ok(2, 4)
	case 8:
		return ok(4)
	case 9:
		return ok(8)
	case 10:
		return ok(4)
	case 11:
		return ok(8)
	case 17:
		return ok(4)
	case 18, 19:
		return ok(16)
	case 20, 25:
		return ok(1)
	case 21, 26:
		return ok(2)
	case 22, 27:
		return ok(4)
	case 23, 28:
		return ok(8)
	case 24, 29:
		return ok(16)
	case 32:
		return ok(0)
	}
	return -1
}
