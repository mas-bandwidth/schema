// The CHECK: the lock in the tree IS the live sequence, entry for entry
// (docs/SPEC-TABLES.md §2.10) — and the one writer's own reading, where the
// locked sequence must be a PREFIX of the live one, with `deprecated` allowed
// only to turn on.
//
// The two readings differ in ONE thing: what to make of a declaration that has
// moved past the lock. `schema lock` must accept that — an append, a new table
// and a deprecation flip are the changes the rule allows, and accepting them is
// what makes the file movable. Every compile must refuse it — a lock that
// trails the declaration is a record of a layout nobody has, and the file in
// the tree is the record.
//
// Every refusal here names the table and the FIRST differing entry, because
// the first one is the one a person can fix; a list of every consequence of a
// single reorder teaches nothing the first line did not.
package lockfile

import (
	"fmt"
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
	// it accepts an append, a new table and a deprecation flip, and it refuses
	// everything the check refuses, so the command that moves the lock cannot
	// be the one that breaks the rule.
	Appendable
)

// Diff compares a committed lock against a live rendering. The result is the
// refusals, in table order and one per table: the first differing entry is
// the finding, and the entries after it are that finding's consequences.
//
// Under [Appendable] a live table the lock does not carry is NEW and passes:
// locking a table is what `schema lock` does, and a table nothing has promised
// anything about promises nothing. Under [Current] it is a stale lock, like
// any other drift.
func Diff(locked, live *Unit, policy Policy) []error {
	var errs []error
	for i := range locked.Tables {
		lk := &locked.Tables[i]
		// THE SELF-CONSISTENCY CHECK, FIRST. The layout hash and the entries
		// above it are one statement written twice, so a lock whose halves
		// disagree has been edited by a hand rather than written by the
		// compiler — and nothing below it can be trusted to mean what it
		// says.
		if got := LayoutHash(lk.Entries); got != lk.Layout {
			errs = append(errs, fmt.Errorf("fixed table %s: the lock records layout=0x%016x over entries that hash to 0x%016x — the lock is written by the compiler and a hand-edit does not hold; regenerate it with `schema lock` and make the schema change you meant instead (docs/SPEC-TABLES.md §2.10)",
				lk.Name, lk.Layout, got))
			continue
		}
		lv := live.Table(lk.Name)
		if lv == nil {
			errs = append(errs, fmt.Errorf("fixed table %s is in the lock and this unit no longer declares it as a fixed table — a fixed table's layout is a promise to every record already written, and a promise is not withdrawn; compaction is a NEW table under a NEW name (docs/SPEC-TABLES.md §2.10)",
				lk.Name))
			continue
		}
		if err := diffTable(lk, lv, policy); err != nil {
			errs = append(errs, err)
		}
	}
	if policy == Current {
		// a whole table the lock has not caught up to, reported at its first
		// entry for the same reason every other finding is
		for i := range live.Tables {
			lv := &live.Tables[i]
			if locked.Table(lv.Name) == nil {
				errs = append(errs, stale(lv, 0, "in the declaration and not in the lock"))
			}
		}
	}
	return errs
}

// diffTable is the rule over one table, and it stops at the first differing
// entry.
func diffTable(lk, lv *Table, policy Policy) error {
	for i, want := range lk.Entries {
		where := fmt.Sprintf("fixed table %s: entry %d, field %s (id=0x%016x)", lk.Name, i+1, want.Name, want.Id)
		if i >= len(lv.Entries) {
			return fmt.Errorf("%s, is in the lock and gone from the declaration — a fixed table evolves APPEND-ONLY: a field is deprecated in place, never removed, because the record has no ids in it and every field after a removed one slides (docs/SPEC-TABLES.md §2.10); restore it and mark it `| deprecated`",
				where)
		}
		got := lv.Entries[i]
		if got.Id != want.Id {
			return fmt.Errorf("%s, is field %s (id=0x%016x) in the declaration — a fixed table evolves APPEND-ONLY: a field is added at the BOTTOM, never inserted, moved or removed, because the record has no ids in it and a reader at this offset would read one field as the other (docs/SPEC-TABLES.md §2.10)",
				where, got.Name, got.Id)
		}
		if got.Width != want.Width {
			return fmt.Errorf("%s, is %d bytes wide in the lock and %d in the declaration — a field already in the lock keeps its width: a fixed record is walked by offset, so widening one field moves every field after it (docs/SPEC-TABLES.md §2.10); deprecate this field and append a new one",
				where, want.Width, got.Width)
		}
		if got.Kind != want.Kind {
			return fmt.Errorf("%s, is kind %d in the lock and kind %d in the declaration — a field already in the lock keeps its type: every record already written holds the old one, and nothing on the wire says which (docs/SPEC-TABLES.md §2.10); deprecate this field and append a new one",
				where, want.Kind, got.Kind)
		}
		if want.Deprecated && !got.Deprecated {
			return fmt.Errorf("%s, is deprecated in the lock and live in the declaration — deprecation is ONE-WAY: readers were told to ignore this slot and the writers that have run since left it at its default, so what comes back is not data (docs/SPEC-TABLES.md §2.10); append a new field instead",
				where)
		}
		// the marker turned ON: the change the rule allows, and the check
		// still wants it written down
		if policy == Current && !want.Deprecated && got.Deprecated {
			return stale(lv, i, "deprecated in the declaration and live in the lock")
		}
	}
	if policy == Current && len(lv.Entries) > len(lk.Entries) {
		return stale(lv, len(lk.Entries), "in the declaration and not in the lock")
	}
	return nil
}

// stale is THE ONE REFUSAL for a lock the declaration has moved past: an
// append, a new table, a deprecation flip. Each of those is a change the
// append-only rule ALLOWS, and every one of them still has to be written down,
// so the sentence is one sentence and the remedy is one command.
//
// what is the clause naming the difference; the rest is why a lock is only
// ever current, and it does not vary.
func stale(lv *Table, i int, what string) error {
	subject := fmt.Sprintf("fixed table %s is", lv.Name)
	if i < len(lv.Entries) {
		e := lv.Entries[i]
		subject = fmt.Sprintf("fixed table %s: entry %d, field %s (id=0x%016x), is", lv.Name, i+1, e.Name, e.Id)
	}
	return fmt.Errorf("%s %s — the lock is the committed record of a fixed table's layout, and this declaration has moved past it: an append, a new table and a deprecation are the changes the rule allows, and a change the rule allows is still a change the record must carry (docs/SPEC-TABLES.md §2.10); write it with `schema lock`",
		subject, what)
}
