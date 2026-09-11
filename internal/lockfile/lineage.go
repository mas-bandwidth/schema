// THE LINEAGE: every layout a fixed table has ever had, oldest first
// (docs/FIXED-FORM-BILL-READS-BACKWARD.md §6b, §11.6, §11.7, §11.8;
// docs/FIXED-FORM-ALGORITHM.md §5.2).
//
// WHY IT IS HERE AND NOT DERIVED. The lock's field entries are the LAW: what a
// widening may do to the declaration. They are a projection of the declaration
// as it stands, so they say what the CURRENT layout is and nothing about the
// layouts that shipped before it. A reader compiled from this unit must accept
// every layout a writer in the field still carries, and §5.2 compiles one plan
// per such layout AT BUILD TIME from static data — so the list of them is a
// RECORD, kept by the one command that moves the file, and the bill's §11.7
// names this file as its home: "the lock must hold what COMPILE reads."
//
// Four facts per entry, and the bill asks for each by name:
//
//   - the WIRE hash — `ir.TableFixedLayoutHash` over the layout bytes and the
//     §13 definitions digest, the same eight bytes the file header and every
//     record carry. It is the ONLY thing a file is matched on (§12.4), so it is
//     the lineage's key, and it is the wire's number rather than the lock's own
//     text projection (`layout=`), which §11.7 called out as the gap;
//   - the LAYOUT BYTES verbatim, because LOAD compares a file's layout to these
//     with memcmp and NEVER PARSES A STRANGER'S LAYOUT (§6b, §12.4), and PLAN
//     walks these trusted bytes at build time;
//   - the DEFINITIONS DIGEST (§13): every fact of §2 that is not wire shape —
//     each range, each flags bit count, each `bits(N)`, each `fixed` I and F,
//     each reader-side limit. The hash binds it and it never rides the wire;
//   - the RECORD BODY SIZE, taken from the lock and never from the file.
//
// And two the operator writes: a RETIRED mark and its REASON (§11.4, §11.5),
// the one non-append edit this file permits. A retired entry STAYS IN THE
// LINEAGE FOREVER — it is what lets LOAD say `layout_unsupported` (upgrade the
// client) where an unknown hash says `layout_newer` (ship the reader), and the
// two answers point the operator in opposite directions.
package lockfile

import (
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// A LineageEntry is ONE LOCKED LAYOUT of a fixed table: a version of the
// record, in the form COMPILE reads it.
type LineageEntry struct {
	// Wire is the eight bytes the file header and every record carry — fnv1a64
	// over Layout and then Digest (algorithm §5.2's HASH). It is the lineage's
	// key and the only fact a file is matched on.
	Wire uint64

	// Layout is the layout bytes exactly as they ride
	// (ir.TableFixedLayoutBytes): a u32 entry count and a run of
	// seventeen-byte entries. LOAD compares a file's bytes to these and PLAN
	// walks them at build time; nothing parses a stranger's copy.
	Layout []byte

	// Digest is the §13 DEFINITIONS DIGEST — the facts of §2 that are not wire
	// shape, which the layout bytes cannot carry and the hash must bind. It is
	// empty where a table carries none of them, and an empty digest leaves the
	// hash equal to a hash of the layout bytes alone, so a table with no range,
	// no `bits(N)`, no `fixed(I,F)`, no flags and no reader-side limit does not
	// move the day the digest lands.
	Digest []byte

	// Record is the record's BODY size in bytes (ir.TableFixedTypeBytes), the
	// size LOAD takes from the lock rather than from the file.
	Record int64

	// Retired marks a layout no reader this build serves will accept any more
	// (§11.4): `schema lock --retire T@<hash>`. The entry stays; COMPILE emits
	// the retired hashes beside the supported ones so LOAD says
	// `layout_unsupported` rather than `layout_newer`.
	Retired bool

	// Reason is the sentence the operator wrote when retiring the entry, or the
	// table. A retirement is a DECLARATION — the one thing in this file that is
	// not an append — so it wants a sentence a person reads years later, the
	// way moving the tables baseline does (SPEC §18.4).
	Reason string
}

// lineageWireHash is algorithm §5.2's HASH: fnv1a64 over the layout bytes and
// then the definitions digest.
//
// It is ir's own function over the two runs concatenated, which is exactly what
// ir.TableFixedLayoutHash will compute once #910's fix gives it the digest as a
// second input — and while the digest is empty it is byte for byte the number
// every backend already emits for the header (§13: "an empty digest leaves the
// hash equal to a hash of the layout bytes alone").
func lineageWireHash(layout, digest []byte) uint64 {
	if len(digest) == 0 {
		return ir.TableFixedLayoutHash(layout)
	}
	both := make([]byte, 0, len(layout)+len(digest))
	both = append(both, layout...)
	both = append(both, digest...)
	return ir.TableFixedLayoutHash(both)
}

// renderLineage is the LIVE entry: the one layout the declaration has right
// now. A rendering knows one layout and the committed file knows the rest, so
// [Render] produces a lineage of exactly one entry and [Update] is what carries
// the history forward onto it.
func renderLineage(u *ir.Unit, st *ir.Struct) []LineageEntry {
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
	// THE DIGEST IS EMPTY UNTIL ir COMPUTES IT (#910, bill §13). The entry
	// records it as a fact of its own so the lock's shape is final now and the
	// day ir grows `DIGEST(T)` only this line moves — and on that day every
	// table carrying a range, a flags mask, a `bits(N)`, a `fixed(I,F)` or a
	// reader-side limit earns one lineage entry, which is the correct record
	// of what happened: its files' hashes moved.
	var digest []byte
	return []LineageEntry{{
		Wire:   lineageWireHash(layout, digest),
		Layout: layout,
		Digest: digest,
		Record: ir.TableFixedTypeBytes(st),
	}}
}

// line is one lineage entry's text. The wire hash leads, because it is what a
// file is matched on; the reason is LAST because it is the one token that holds
// a sentence.
func (e LineageEntry) line() string {
	s := fmt.Sprintf("lineage wire=0x%016x record=%d bytes=%x", e.Wire, e.Record, e.Layout)
	if len(e.Digest) != 0 {
		s += fmt.Sprintf(" digest=%x", e.Digest)
	}
	if e.Retired {
		s += " retired"
	}
	if e.Reason != "" {
		s += " reason=" + e.Reason
	}
	return s
}

// parseLineage reads one lineage line. The REASON is the rest of the line, so a
// sentence with spaces in it survives the round trip; every other token is a
// single field.
func parseLineage(line string) (LineageEntry, error) {
	e := LineageEntry{}
	body := strings.TrimSpace(line)
	if at := strings.Index(body, " reason="); at >= 0 {
		e.Reason = strings.TrimSpace(body[at+len(" reason="):])
		body = body[:at]
	}
	fields := strings.Fields(body)
	seen := map[string]bool{}
	for _, tok := range fields[1:] {
		key, val, valued := strings.Cut(tok, "=")
		if seen[key] {
			return LineageEntry{}, fmt.Errorf("a lineage line carries %s once", key)
		}
		seen[key] = true
		switch {
		case !valued && key == "retired":
			e.Retired = true
		case key == "wire":
			h, err := parseHex(tok, "wire")
			if err != nil {
				return LineageEntry{}, err
			}
			e.Wire = h
		case key == "record":
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil || n < 0 {
				return LineageEntry{}, fmt.Errorf("%q is not a record size", tok)
			}
			e.Record = n
		case key == "bytes":
			b, err := hex.DecodeString(val)
			if err != nil {
				return LineageEntry{}, fmt.Errorf("%q is not the layout's bytes as hex", tok)
			}
			e.Layout = b
		case key == "digest":
			b, err := hex.DecodeString(val)
			if err != nil {
				return LineageEntry{}, fmt.Errorf("%q is not the definitions digest as hex", tok)
			}
			e.Digest = b
		default:
			return LineageEntry{}, fmt.Errorf("%q is not a fact a lineage line carries", tok)
		}
	}
	if !seen["wire"] || !seen["bytes"] || !seen["record"] {
		return LineageEntry{}, fmt.Errorf("a lineage line is `lineage wire=0x... record=N bytes=<hex> [digest=<hex>] [retired] [reason=<text>]`")
	}
	if len(e.Layout) == 0 {
		return LineageEntry{}, fmt.Errorf("a lineage entry carries the layout's bytes; a reader compares a file's layout to them and never parses its own copy (docs/FIXED-FORM-BILL-READS-BACKWARD.md §6b)")
	}
	// THE ENTRY IS ONE STATEMENT MADE TWICE, like the layout= hash over the
	// field lines: the wire hash IS fnv1a64 over these bytes and this digest,
	// so a hand that moves either without the other is caught here.
	if got := lineageWireHash(e.Layout, e.Digest); got != e.Wire {
		return LineageEntry{}, fmt.Errorf("this lineage entry records wire=0x%016x over bytes that hash to 0x%016x — the wire hash IS the hash of the layout bytes and the definitions digest, so the two halves of this line disagree",
			e.Wire, got)
	}
	return e, nil
}

// Open is how a BACKEND reads this file: it locates the unit's lock from the
// unit's source paths and parses it. ok is false when the unit has no lock —
// which is not an error, because a unit that has never been locked promises
// nothing — and the error is a lock that cannot be read.
//
// A backend wants exactly two things from what comes back, per fixed table:
// [Lineage] and [Floor]. It is the pair algorithm §5.2's COMPILE is written
// against.
func Open(paths []string) (*Unit, bool, error) {
	path, ok, err := Locate(paths)
	if err != nil || !ok {
		return nil, false, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	lock, err := Parse(path, data)
	if err != nil {
		return nil, false, fmt.Errorf("%w — the compiler owns this file; write it with: schema lock (docs/SPEC-TABLES.md §2.10)", err)
	}
	return lock, true, nil
}

// Lineage is THE ACCESSOR COMPILE READS (algorithm §5.2): one fixed table's
// locked layouts, OLDEST FIRST, the current one last. Nil when the lock does
// not carry the table, or carries it as a nested `type` rather than a fixed
// table.
//
// It is the source that replaces the filename convention the C++ backend reads
// its lineage from today (internal/codegen/cpptable/lineage.go: VOLD_/VNEW_ and
// the numbered evolution sets, an interim named as an interim). A backend walks
// this slice in order: entry i's Wire is `R.lineage[i]`, its Layout is
// `R.known[i].layout`, its Record is `R.known[i].record_bytes`, and the floor
// is one past the highest Retired index — which is exactly §5.2's COMPILE.
func Lineage(lock *Unit, table string) []LineageEntry {
	if lock == nil {
		return nil
	}
	t := lock.Table(table)
	if t == nil || t.Decl != DeclFixedTable {
		return nil
	}
	return t.Lineage
}

// Floor is §5.2's one number: the lowest lineage index a reader built from this
// lock accepts — one past the highest RETIRED entry, and 0 when none is. Below
// it a known hash is `layout_unsupported` (upgrade the client); outside the
// lineage altogether it is `layout_newer` (ship the reader).
func Floor(lock *Unit, table string) int {
	floor := 0
	for i, e := range Lineage(lock, table) {
		if e.Retired {
			floor = i + 1
		}
	}
	return floor
}

// salvageLineage is §11.8: "A RENDERING-VERSION BUMP SALVAGES, NEVER DELETES."
//
// A lock written before the lineage existed holds ONE layout and no history, so
// the salvage takes that single layout as the first lineage entry — the bill's
// own sentence for this case (§11.8, and §11.9: "a fixed table's layout is
// locked before its first file ships; the first lineage entry is the first
// layout"). The bytes of it come from the declaration the old lock is held
// against, because a pre-lineage lock never recorded them; everything the old
// file DID record — the field lines, the defaults, the deprecation marks, the
// closure — is carried forward unchanged and is still the law this unit is held
// to. The old remedy, "delete it and write it again", would have wiped the
// lineage fleet-wide, which is the one thing this file must never do.
func salvageLineage(locked, live *Unit) {
	if locked == nil || locked.Version >= Version {
		return
	}
	for i := range locked.Tables {
		lk := &locked.Tables[i]
		if lk.Decl != DeclFixedTable || len(lk.Lineage) != 0 {
			continue
		}
		if lv := live.Table(lk.Name); lv != nil && lv.Decl == DeclFixedTable {
			lk.Lineage = append([]LineageEntry(nil), lv.Lineage...)
		}
	}
}

// mergeLineage carries the committed history forward onto the rendering that is
// about to be written, and appends the live layout when the hash has MOVED —
// "one entry per layout-hash change AT COMMIT, judged by the check that runs
// there; local iterations before a commit collapse to one entry" (§11.6).
//
// Nothing is ever dropped and nothing already written is rewritten, so a
// retired entry keeps its mark and its reason.
func mergeLineage(locked, live *Unit) {
	for i := range live.Tables {
		lv := &live.Tables[i]
		if lv.Decl != DeclFixedTable || len(lv.Lineage) != 1 {
			continue
		}
		lk := locked.Table(lv.Name)
		if lk == nil || lk.Decl != DeclFixedTable || len(lk.Lineage) == 0 {
			// a table the lock has never held: its own layout is the first
			// entry of its lineage (§11.9)
			continue
		}
		cur := lv.Lineage[0]
		history := append([]LineageEntry(nil), lk.Lineage...)
		if history[len(history)-1].Wire == cur.Wire {
			lv.Lineage = history
			continue
		}
		lv.Lineage = append(history, cur)
	}
}

// lineageWarnAt is §11.6's warning: "a warning names a lineage past thirty-two
// entries per table; the remedies are the floor and the new table." The lock
// has no tolerant class — every finding in [Diff] is a refusal — so the warning
// rides where a person running `schema lock` sees it.
const lineageWarnAt = 32

// LongLineages names every fixed table whose lineage has grown past
// [lineageWarnAt], with its length, in name order. It is a WARNING and never a
// refusal: every layout in the list is real and so is every record that carries
// one. The remedies are the bill's — retire the entries nothing live speaks
// (`schema lock --retire T@<hash>`), or start a new table.
func LongLineages(lock *Unit) []string {
	var out []string
	for i := range lock.Tables {
		t := &lock.Tables[i]
		if t.Decl == DeclFixedTable && len(t.Lineage) > lineageWarnAt {
			out = append(out, fmt.Sprintf("fixed table %s carries %d layouts, past the %d-entry advisory bound (docs/FIXED-FORM-BILL-READS-BACKWARD.md §11.6) — COMPILE lays one plan per supported layout down as static data in every leg, so the remedies are to retire what nothing live speaks (`schema lock --retire %s@<hash>`) or to start a new table",
				t.Name, len(t.Lineage), lineageWarnAt, t.Name))
		}
	}
	return out
}

// Warnings reads the lock beside these paths and returns its advisories — the
// long-lineage warning of §11.6 today. A unit with no lock has none, and an
// unreadable lock has none either: [Check] is where a lock that cannot be read
// is reported.
func Warnings(paths []string) []string {
	path, ok, err := Locate(paths)
	if err != nil || !ok {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lock, err := Parse(path, data)
	if err != nil {
		return nil
	}
	return LongLineages(lock)
}

// diffLineage holds the lock's history to the declaration: under [Current] the
// declaration's own layout must BE the lineage's last entry, because a reader
// built from this lock compiles the identity plan for that entry and one plan
// per entry before it. Under [Appendable] the answer is a new entry, which is
// what `schema lock` is for.
func diffLineage(lk, lv *Table, policy Policy) error {
	if len(lk.Lineage) == 0 {
		return fmt.Errorf("fixed table %s: the lock carries no lineage for it — every fixed table's layouts are the record a reader is compiled from (docs/FIXED-FORM-BILL-READS-BACKWARD.md §6b, §11.7); write it with `schema lock` (docs/SPEC-TABLES.md §2.10)",
			lk.Name)
	}
	if policy != Current || len(lv.Lineage) != 1 {
		return nil
	}
	last, cur := lk.Lineage[len(lk.Lineage)-1], lv.Lineage[0]
	if last.Wire == cur.Wire {
		return nil
	}
	return stale(lk.Decl, lk.Name,
		fmt.Sprintf("its layout hashes to 0x%016x", cur.Wire),
		fmt.Sprintf("not the lineage's last entry (0x%016x): the declaration has a layout the record does not end with", last.Wire))
}

// retiredText is the tail a retired lineage entry or a retired table carries:
// the mark, then the reason LAST, because the reason is the one token that holds
// a sentence and the rest of the line is its text.
func retiredText(retired bool, reason string) string {
	s := ""
	if retired {
		s += " retired"
	}
	if reason != "" {
		s += " reason=" + reason
	}
	return s
}

// splitRetired takes the `retired` mark and the rest-of-line reason off a line,
// and hands back the head for a field-by-field read.
func splitRetired(line string) (head string, retired bool, reason string) {
	head = strings.TrimSpace(line)
	if at := strings.Index(head, " reason="); at >= 0 {
		reason = strings.TrimSpace(head[at+len(" reason="):])
		head = head[:at]
	}
	if strings.HasSuffix(head, " retired") {
		retired = true
		head = strings.TrimSuffix(head, " retired")
	}
	return head, retired, reason
}

// Retire is the lock's ONE NON-APPEND EDIT, and the bill gives it two forms
// (§11.4, §11.5):
//
//   - `schema lock --retire T@<hash> --reason "..."` marks ONE LINEAGE ENTRY
//     retired. The entry stays in the lineage forever; COMPILE emits the retired
//     hashes beside the supported ones so LOAD answers a file carrying that
//     layout with `layout_unsupported` — "upgrade the client" — rather than
//     `layout_newer`, which means "ship the reader". The two answers point the
//     operator in opposite directions, and that is why a retired entry is kept
//     rather than dropped.
//   - `schema lock --retire T --reason "..."` marks THE WHOLE TABLE retired. Its
//     block and its lineage stay; what the mark buys is that the declaration may
//     then be dropped, and that the "fixed removed" refusal applies only to a
//     table nobody has retired. A table is retired, never removed, until nothing
//     live speaks it.
//
// BOTH WANT A REASON. Every other write this file takes is an append, which
// breaks nothing and declares nothing; a retirement declares that a layout no
// reader will serve again, which is the kind of sentence the tables baseline
// keeps for the same reason (SPEC §18.4).
//
// The lock must be CURRENT first: retiring is a statement about what the record
// holds, so the record is brought up to date with `schema lock` before a person
// makes one.
func Retire(u *ir.Unit, paths []string, target, reason string) (path string, rewrote bool, err error) {
	if strings.TrimSpace(reason) == "" {
		return "", false, fmt.Errorf("--retire wants --reason: retiring a layout says no reader will ever serve it again, which is the one DECLARATION this file holds and the one write that is not an append (docs/FIXED-FORM-BILL-READS-BACKWARD.md §11.4)")
	}
	if strings.ContainsAny(reason, "\n\r") {
		return "", false, fmt.Errorf("--reason is one line: it rides at the end of the line it belongs to")
	}
	name, hashText, wantEntry := strings.Cut(target, "@")
	path, ok, err := Locate(paths)
	if err != nil {
		return "", false, err
	}
	if !ok {
		return "", false, fmt.Errorf("this unit has no %s — there is nothing to retire until the table is locked (docs/SPEC-TABLES.md §2.10)", FileName)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	locked, err := Parse(path, data)
	if err != nil {
		return "", false, fmt.Errorf("%w — the compiler owns this file; write it with: schema lock (docs/SPEC-TABLES.md §2.10)", err)
	}
	live := Render(u)
	salvageLineage(locked, live)
	if errs := Diff(locked, live, Current); len(errs) > 0 {
		return "", false, fmt.Errorf("%s: the lock is not current, so a retirement would be a statement about a record nobody has: run `schema lock` first — %w", path, errs[0])
	}
	locked.Version = Version

	t := locked.Table(name)
	if t == nil || t.Decl != DeclFixedTable {
		return "", false, fmt.Errorf("%s: this lock carries no fixed table %s — `--retire` names a LOCKED table, with or without one of its layouts (`%s@<hash>`)", path, name, name)
	}
	before := string(data)
	switch {
	case !wantEntry:
		t.Retired, t.Reason = true, reason
	default:
		h, perr := parseHex("wire="+hashText, "wire")
		if perr != nil {
			return "", false, fmt.Errorf("%s: `--retire %s` names one of this table's layout hashes: %w", path, target, perr)
		}
		at := -1
		for i := range t.Lineage {
			if t.Lineage[i].Wire == h {
				at = i
			}
		}
		if at < 0 {
			return "", false, fmt.Errorf("%s: fixed table %s has no layout 0x%016x in its lineage — a hash nothing ever locked cannot be retired, and a file carrying it is `layout_newer` already (docs/FIXED-FORM-BILL-READS-BACKWARD.md §11.9)", path, name, h)
		}
		if at == len(t.Lineage)-1 {
			return "", false, fmt.Errorf("%s: 0x%016x is fixed table %s's CURRENT layout — a reader built from this lock reads its own records, so the current entry is never retired; retire the table instead (`--retire %s`) when nothing live speaks it (docs/FIXED-FORM-BILL-READS-BACKWARD.md §11.4, §11.5)", path, h, name, name)
		}
		t.Lineage[at].Retired, t.Lineage[at].Reason = true, reason
	}
	text := locked.Text()
	if text == before {
		return path, false, nil
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", false, err
	}
	return path, true, nil
}

// retiredOrphans names every locked block that is in this file ONLY because a
// retired table's declaration has been dropped (§11.5): the retired tables
// themselves, and the `type`, enum, flags and union blocks nothing the unit
// still declares reaches. They are carried forward rather than refused, because
// a retired table's lineage stays forever and the blocks its entries name are
// part of what it says.
func retiredOrphans(locked, live *Unit) map[string]bool {
	var retired []string
	for i := range locked.Tables {
		lk := &locked.Tables[i]
		if lk.Decl != DeclFixedTable || !lk.Retired {
			continue
		}
		if lv := live.Table(lk.Name); lv == nil || lv.Decl != DeclFixedTable {
			retired = append(retired, lk.Name)
		}
	}
	if len(retired) == 0 {
		return nil
	}
	// what the LIVE declarations still reach, walked over the locked blocks
	keep := map[string]bool{}
	var walk func(name string)
	walk = func(name string) {
		if keep[name] {
			return
		}
		keep[name] = true
		if t := locked.Table(name); t != nil {
			for _, e := range t.Entries {
				if e.HeldName != "" {
					walk(e.HeldName)
				}
				if e.KeyName != "" {
					walk(e.KeyName)
				}
			}
		}
		if v := locked.List(name); v != nil {
			for _, p := range v.Payloads {
				if at := strings.Index(p, "@"); at > 0 {
					walk(p[:at])
				}
			}
		}
	}
	for i := range live.Tables {
		walk(live.Tables[i].Name)
	}
	orphans := map[string]bool{}
	for i := range locked.Tables {
		if n := locked.Tables[i].Name; !keep[n] {
			orphans[n] = true
		}
	}
	for i := range locked.Values {
		if n := locked.Values[i].Name; !keep[n] {
			orphans[n] = true
		}
	}
	return orphans
}

// carryRetired puts the blocks of [retiredOrphans] back onto the rendering that
// is about to be written, each in the name order its group is written in, so a
// retired table's record — its entries, its lineage, its reason — survives the
// day its declaration goes.
func carryRetired(locked, live *Unit) {
	orphans := retiredOrphans(locked, live)
	if len(orphans) == 0 {
		return
	}
	for i := range locked.Tables {
		lk := locked.Tables[i]
		if !orphans[lk.Name] || live.Table(lk.Name) != nil {
			continue
		}
		at := len(live.Tables)
		for j := range live.Tables {
			if live.Tables[j].Decl == lk.Decl && live.Tables[j].Name > lk.Name {
				at = j
				break
			}
			if lk.Decl == DeclFixedTable && live.Tables[j].Decl == DeclType {
				at = j
				break
			}
		}
		live.Tables = append(live.Tables, Table{})
		copy(live.Tables[at+1:], live.Tables[at:])
		live.Tables[at] = lk
	}
	for i := range locked.Values {
		lk := locked.Values[i]
		if !orphans[lk.Name] || live.List(lk.Name) != nil {
			continue
		}
		rank := func(decl string) int {
			switch decl {
			case DeclEnum:
				return 0
			case DeclFlags:
				return 1
			default:
				return 2
			}
		}
		at := len(live.Values)
		for j := range live.Values {
			if rank(live.Values[j].Decl) > rank(lk.Decl) || (live.Values[j].Decl == lk.Decl && live.Values[j].Name > lk.Name) {
				at = j
				break
			}
		}
		live.Values = append(live.Values, ValueList{})
		copy(live.Values[at+1:], live.Values[at:])
		live.Values[at] = lk
	}
}
