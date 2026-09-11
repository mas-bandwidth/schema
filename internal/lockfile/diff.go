// The CHECK: the lock in the tree IS the live sequence, entry for entry and
// value for value (docs/SPEC-TABLES.md §2.10) — and the one writer's own
// reading, where the locked sequence must be a PREFIX of the live one, with
// `deprecated` allowed only to turn on.
//
// The two readings differ in ONE thing: what to make of a declaration that has
// moved past the lock. `schema lock` must accept that — an append to a table,
// an append to an enum, a new table, a new type in the closure and a
// deprecation flip are the changes the rule allows, and accepting them is what
// makes the file movable. Every compile must refuse it — a lock that trails
// the declaration is a record of a layout nobody has, and the file in the tree
// is the record.
//
// Every refusal here names the declaration and the FIRST differing line,
// because the first one is the one a person can fix; a list of every
// consequence of a single reorder teaches nothing the first line did not. And
// every refusal names its remedy, which is always one of three: append,
// deprecate and add, or a new table under a new name.
package lockfile

import (
	"fmt"
	"strings"
)

// A Policy is which of the two readings a comparison takes.
type Policy int

const (
	// Current is the CHECK's reading, on every compile (`schema check`,
	// `schema generate`): the lock must be the live sequence exactly. A
	// declaration the lock has not caught up to is a refusal that says to run
	// `schema lock`, because the committed file is the record and a record
	// only records what it is current with.
	Current Policy = iota

	// Appendable is `schema lock`'s own reading: the locked sequence must be a
	// PREFIX of the live one. It is what lets the ONE WRITER move the file —
	// it accepts an append, a new block and a deprecation flip, and it refuses
	// everything the check refuses, so the command that moves the lock cannot
	// be the one that breaks the rule.
	Appendable
)

// Diff compares a committed lock against a live rendering. The result is the
// refusals, in block order and one per block: the first differing line is the
// finding, and the lines after it are that finding's consequences.
//
// Under [Appendable] a live block the lock does not carry is NEW and passes:
// locking a table, a nested record, an enum, a flags mask or a union is what
// `schema lock` does, and a declaration nothing has promised anything about
// promises nothing. Under [Current] it is a stale lock, like any other drift.
func Diff(locked, live *Unit, policy Policy) []error {
	var errs []error
	// ONE LINEAGE FINDING IS A CONSEQUENCE AND IS HELD BACK: the declaration's
	// own layout not being the lineage's LAST entry. A root's wire layout hash
	// moves because a field moved, or a nested type, enum, flags mask or union
	// moved — every one of those is inlined into the layout bytes — so that
	// finding is the shadow of a finding below, and the sentence a person acts
	// on is the other one. It is reported when nothing else refused, which is
	// the case where the record itself is what is wrong.
	//
	// EVERY OTHER LINEAGE FINDING IS REPORTED UNCONDITIONALLY, because it is a
	// fault OF THE RECORD and not a consequence of any change to the law: a
	// `lineage=` roll-up that does not match the lines under it (diffRollup — a
	// layout a hand DELETED, §11.8), and a lock that carries no lineage at all.
	// Neither follows from a field moving, and holding either behind an
	// unrelated refusal would mean a fleet's record could be broken in the same
	// commit as an ordinary drift and be reported only once the drift was fixed.
	var drift []error
	// THE RETIRED TABLE'S BLOCKS (bill §11.5): a retired table whose declaration
	// has been dropped, and the types, enums, flags and unions nothing live
	// reaches any more because of it, are the record of something that shipped
	// and are kept rather than refused.
	orphans := retiredOrphans(locked, live)
	for i := range locked.Tables {
		lk := &locked.Tables[i]
		// THE SELF-CONSISTENCY CHECK, FIRST. The layout hash and the entries
		// above it are one statement written twice, so a lock whose halves
		// disagree has been edited by a hand rather than written by the
		// compiler — and nothing below it can be trusted to mean what it
		// says.
		if got := LayoutHash(lk.Entries); got != lk.Layout {
			errs = append(errs, fmt.Errorf("%s %s: the lock records layout=0x%016x over entries that hash to 0x%016x — the lock is written by the compiler and a hand-edit does not hold; regenerate it with `schema lock` and make the schema change you meant instead (docs/SPEC-TABLES.md §2.10)",
				lk.Decl, lk.Name, lk.Layout, got))
			continue
		}
		// AND THE SET OF LINEAGE LINES, bound the same way (lineage.go): the
		// `lineage=` roll-up and the lines under it are one statement written
		// twice, so a DELETED layout — the one edit §11.8 forbids fleet-wide —
		// is visible here and nowhere else. It is checked against the file's own
		// token, so a lock from before the token is salvage rather than a
		// refusal, and a table whose roll-up disagrees says nothing further that
		// can be trusted.
		if lk.Decl == DeclFixedTable && lk.rollupWritten && locked.Version == Version {
			if err := diffRollup(lk); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		lv := live.Table(lk.Name)
		if lv == nil || lv.Decl != lk.Decl {
			if !orphans[lk.Name] {
				errs = append(errs, gone(lk, lv))
			}
			continue
		}
		// THE FORM'S CEILING, BEFORE THE LAW AND UNDER BOTH READINGS
		// (lineage.go). It goes first because a record past the ceiling is a
		// table whose WIRE has changed, which no refusal below it would name:
		// every monotone fact widened legally, so [Current] would report the
		// widening as an ordinary stale lock and `schema lock` would write it.
		if lk.Decl == DeclFixedTable {
			if err := diffCeiling(lk, lv); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		if err := diffTable(lk, lv, policy); err != nil {
			errs = append(errs, err)
			continue
		}
		// THE LINEAGE, after the law (lineage.go): the law says the declaration
		// may stand where it stands, and the lineage says the record ends there.
		//
		// IT IS REPORTED UNCONDITIONALLY. A lineage finding used to be held back
		// until nothing else refused, on the reading that the lineage is never
		// the account of a change, only its consequence — a root's hash moves
		// because a field or a nested type moved, and that is the sentence a
		// person acts on. But the lineage is the RECORD, and a fault in it is a
		// fault of its own however many other lines also moved: a truncated
		// lineage, a lock from a rendering that had none, a declaration the
		// record does not end with. A finding about the record is never a
		// consequence of a finding about the law, so it says so here.
		if lk.Decl == DeclFixedTable {
			if err := diffLineage(lk, lv, policy); err != nil {
				if isLineageDrift(err) {
					drift = append(drift, err)
				} else {
					errs = append(errs, err)
				}
			}
		}
	}
	for i := range locked.Values {
		lk := &locked.Values[i]
		if got := ValuesHash(lk); got != lk.Hash {
			errs = append(errs, fmt.Errorf("%s %s: the lock records values=0x%016x over %ss that hash to 0x%016x — the lock is written by the compiler and a hand-edit does not hold; regenerate it with `schema lock` and make the schema change you meant instead (docs/SPEC-TABLES.md §2.10)",
				lk.Decl, lk.Name, lk.Hash, lk.Child(), got))
			continue
		}
		lv := live.List(lk.Name)
		if lv == nil || lv.Decl != lk.Decl {
			if orphans[lk.Name] {
				continue
			}
			errs = append(errs, fmt.Errorf("%s %s is in the lock and this unit's fixed tables no longer reach it — the lock holds every type a fixed record is made of, so a type leaving the closure is a field that changed what it holds; restore it, or deprecate that field and append a new one (docs/SPEC-TABLES.md §2.10)",
				lk.Decl, lk.Name))
			continue
		}
		if err := diffValues(lk, lv, policy); err != nil {
			errs = append(errs, err)
		}
	}
	if policy == Current {
		// a whole block the lock has not caught up to, reported at its first
		// line for the same reason every other finding is
		for i := range live.Tables {
			lv := &live.Tables[i]
			if lk := locked.Table(lv.Name); lk == nil || lk.Decl != lv.Decl {
				errs = append(errs, stale(lv.Decl, lv.Name, entryName(lv, 0), "in the declaration and not in the lock"))
			}
		}
		for i := range live.Values {
			lv := &live.Values[i]
			if lk := locked.List(lv.Name); lk == nil || lk.Decl != lv.Decl {
				errs = append(errs, stale(lv.Decl, lv.Name, valueName(lv, 0), "in the declaration and not in the lock"))
			}
		}
	}
	if len(errs) == 0 {
		errs = drift
	}
	return errs
}

// gone is the refusal for a locked block the unit no longer declares. A fixed
// table's layout is a promise to every record already written; a `type`, an
// enum, a flags mask or a union leaving the closure is a field that changed
// what it holds, which is the same promise broken one level in.
func gone(lk, lv *Table) error {
	// THE `fixed` KEYWORD IS A FORM AND NOT A VERSION (the bill §2, last row):
	// a table that was fixed and is now plain, or the reverse, is a different
	// wire, so the refusal names the keyword rather than a narrowing.
	if lv != nil && lv.Decl != lk.Decl {
		rule := "fixed removed"
		if lk.Decl == DeclType && lv.Decl == DeclFixedTable {
			rule = "fixed added"
		}
		return fmt.Errorf("%s %s is a %s in the declaration: %s — the `fixed` keyword is a FORM and not a version: the fixed record and the variable table are two wires, and neither reads the other's bytes (docs/FIXED-FORM-BILL-READS-BACKWARD.md §2, docs/SPEC-TABLES.md §2.10); a change of form is a NEW table under a NEW name",
			lk.Decl, lk.Name, lv.Decl, rule)
	}
	if lk.Decl == DeclFixedTable {
		return fmt.Errorf("fixed table %s is in the lock and this unit no longer declares it as a fixed table: fixed removed — a fixed table's layout is a promise to every record already written, and a promise is not withdrawn; compaction is a NEW table under a NEW name, and when NOTHING LIVE SPEAKS THIS ONE retire it first (`schema lock --retire %s --reason \"...\"`), which keeps its lineage and lets the declaration go (docs/FIXED-FORM-BILL-READS-BACKWARD.md §11.5, docs/SPEC-TABLES.md §2.10)",
			lk.Name, lk.Name)
	}
	return fmt.Errorf("type %s is in the lock and this unit's fixed tables no longer nest it by value — a nested record's fields ARE the holder's bytes, so a type leaving the closure is a field that changed what it holds; restore it, or deprecate that field and append a new one (docs/SPEC-TABLES.md §2.10)",
		lk.Name)
}

// diffTable is the rule over one record, and it stops at the first differing
// entry. THE ORDER THE FACTS ARE COMPARED IN IS THE ORDER THAT NAMES THE
// CHANGE BEST: which field is at this offset; then the `?`, which is the exact
// account of a move the width would report vaguely; then the width, the fact
// that slides every field after it; then the kind, the held type, the array
// element, the enum a keyed array is keyed by, the range and the default —
// the facts that move no byte and change what the bytes say.
func diffTable(lk, lv *Table, policy Policy) error {
	for i, want := range lk.Entries {
		where := fmt.Sprintf("%s %s: entry %d, field %s (id=0x%016x)", lk.Decl, lk.Name, i+1, want.Name, want.Id)
		if i >= len(lv.Entries) {
			return fmt.Errorf("%s, is in the lock and gone from the declaration: field removed — a fixed table evolves APPEND-ONLY: a field is deprecated in place, never removed, because the record has no ids in it and every field after a removed one slides (docs/SPEC-TABLES.md §2.10); restore it and mark it `| deprecated`",
				where)
		}
		got := lv.Entries[i]
		if got.Id != want.Id {
			return fmt.Errorf("%s, is field %s (id=0x%016x) in the declaration: %s — a fixed table evolves APPEND-ONLY: a field is added at the BOTTOM, never inserted, moved or removed, because the record has no ids in it and a reader at this offset would read one field as the other (docs/SPEC-TABLES.md §2.10)",
				where, got.Name, got.Id, idRule(lk, lv, want, got))
		}
		// THE MONOTONE LAW over every other fact: a narrowing is a break and a
		// widening is a change the record must carry (monotone.go).
		widened, what, err := monotone(where, want, got)
		if err != nil {
			return err
		}
		if widened && policy == Current {
			return stale(lv.Decl, lv.Name, entryName(lv, i), what)
		}
	}
	if policy == Current && len(lv.Entries) > len(lk.Entries) {
		return stale(lv.Decl, lv.Name, entryName(lv, len(lk.Entries)), "in the declaration and not in the lock")
	}
	return nil
}

func optionalText(optional bool) string {
	if optional {
		return "optional"
	}
	return "plain"
}

func heldName(name string) string {
	if name == "" {
		return "nothing"
	}
	return name
}

func payloadTypeName(p string) string {
	name, _, ok := strings.Cut(p, "@")
	if ok {
		return name
	}
	return p
}

func elemWord(kind int, width int64) string {
	if kind == 0 && width == 0 {
		return "nothing"
	}
	if w := kindWord(kind); w != "" {
		return w
	}
	return fmt.Sprintf("kind %d width %d", kind, width)
}

func kindWord(kind int) string {
	switch kind {
	case 1:
		return "bool"
	case 2:
		return "int8"
	case 3:
		return "int16"
	case 4:
		return "int32"
	case 5:
		return "int64"
	case 6:
		return "uint8"
	case 7:
		return "uint16"
	case 8:
		return "uint32"
	case 9:
		return "uint64"
	case 10:
		return "float32"
	case 11:
		return "float64"
	case 13:
		return "table"
	case 15:
		return "union"
	}
	return ""
}

// diffValues is the same rule over an enum, a flags mask or a union. THE LIST
// IS A NUMBERING: in a fixed record an enum rides as its dense ordinal, a
// flags variant is its bit position, and a union's tag is its arm's position —
// so a value's PLACE in this list is what a stored number means, and the list
// grows at the end or it lies.
func diffValues(lk, lv *ValueList, policy Policy) error {
	for i, want := range lk.Values {
		where := fmt.Sprintf("%s %s: %s %d, %s", lk.Decl, lk.Name, lk.Child(), i+1, want)
		if i >= len(lv.Values) {
			return fmt.Errorf("%s, is in the lock and gone from the declaration: %s removed — an enum, a flags mask or a union a fixed table reaches evolves APPEND-ONLY: every %s keeps its place forever, because a fixed record stores the PLACE and not the name (docs/SPEC-TABLES.md §2.10); restore it, and add the new one at the END",
				where, lk.ruleChild(), lk.Child())
		}
		if lv.Values[i] != want {
			return fmt.Errorf("%s, is %s in the declaration: %s — an enum, a flags mask or a union a fixed table reaches evolves APPEND-ONLY: a new %s goes at the END, and one already in the lock is never inserted, moved or renamed, because a fixed record stores the PLACE and a reader would read one value as the other (docs/SPEC-TABLES.md §2.10); restore it, and add the new one at the END",
				where, lv.Values[i], valueRule(lk, lv, want, lv.Values[i]), lk.Child())
		}
		if lk.Decl == DeclUnion {
			wantP := payloadTypeName(valuePayload(lk, i))
			gotP := payloadTypeName(valuePayload(lv, i))
			if wantP != gotP {
				return fmt.Errorf("%s, held %s in the lock and %s in the declaration: arm payload changed (%s -> %s) — an arm already in the lock keeps the type it holds: every record already written holds the old one, and nothing on the wire says which (docs/SPEC-TABLES.md §2.10); deprecate this field and append a new one",
					where, heldName(wantP), heldName(gotP), heldName(wantP), heldName(gotP))
			}
		}
	}
	if policy == Current && len(lv.Values) > len(lk.Values) {
		return stale(lv.Decl, lv.Name, valueName(lv, len(lk.Values)), "in the declaration and not in the lock")
	}
	return nil
}

// entryName names a record's i'th entry the way a refusal names it, and "" for
// a record with no such entry — a table with no fields yet has nothing to name.
func entryName(lv *Table, i int) string {
	if i >= len(lv.Entries) {
		return ""
	}
	e := lv.Entries[i]
	return fmt.Sprintf("entry %d, field %s (id=0x%016x)", i+1, e.Name, e.Id)
}

// valueName is [entryName] for a value list.
func valueName(lv *ValueList, i int) string {
	if i >= len(lv.Values) {
		return ""
	}
	return fmt.Sprintf("%s %d, %s", lv.Child(), i+1, lv.Values[i])
}

// stale is THE ONE REFUSAL for a lock the declaration has moved past: an
// appended field, an appended variant or arm, a new block, a deprecation flip.
// Each of those is a change the append-only rule ALLOWS, and every one of them
// still has to be written down, so the sentence is one sentence and the remedy
// is one command.
//
// what is the clause naming the difference; the rest is why a lock is only
// ever current, and it does not vary. line is the block-local position, and is
// empty on a block that has no line to name yet.
func stale(decl, name, line, what string) error {
	subject := fmt.Sprintf("%s %s is", decl, name)
	if line != "" {
		subject = fmt.Sprintf("%s %s: %s, is", decl, name, line)
	}
	return fmt.Errorf("%s %s — the lock is the committed record of what a fixed table's readers stand on, and this declaration has moved past it: an append, a new block and a deprecation are the changes the rule allows, and a change the rule allows is still a change the record must carry (docs/SPEC-TABLES.md §2.10); write it with `schema lock`",
		subject, what)
}
