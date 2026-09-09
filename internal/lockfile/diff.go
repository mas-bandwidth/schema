// The CHECK: the locked sequence must be a PREFIX of the live one, entry for
// entry, with `deprecated` allowed only to turn on (docs/SPEC-TABLES.md
// §2.10).
//
// Every refusal here names the table and the FIRST differing entry, because
// the first one is the one a person can fix; a list of every consequence of a
// single reorder teaches nothing the first line did not.
package lockfile

import (
	"fmt"
)

// Diff compares a committed lock against a live rendering. The result is the
// refusals, in table order and one per table: the first differing entry is
// the finding, and the entries after it are that finding's consequences.
//
// A live table the lock does not carry is NEW and passes: locking a table is
// what `schema lock` does, and a table nothing has promised anything about
// promises nothing.
func Diff(locked, live *Unit) []error {
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
		if err := diffTable(lk, lv); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// diffTable is the prefix rule over one table, and it stops at the first
// differing entry.
func diffTable(lk, lv *Table) error {
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
	}
	return nil
}
