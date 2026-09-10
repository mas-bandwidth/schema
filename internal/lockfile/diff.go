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
		lv := live.Table(lk.Name)
		if lv == nil || lv.Decl != lk.Decl {
			errs = append(errs, gone(lk))
			continue
		}
		if err := diffTable(lk, lv, policy); err != nil {
			errs = append(errs, err)
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
	return errs
}

// gone is the refusal for a locked block the unit no longer declares. A fixed
// table's layout is a promise to every record already written; a `type`, an
// enum, a flags mask or a union leaving the closure is a field that changed
// what it holds, which is the same promise broken one level in.
func gone(lk *Table) error {
	if lk.Decl == DeclFixedTable {
		return fmt.Errorf("fixed table %s is in the lock and this unit no longer declares it as a fixed table — a fixed table's layout is a promise to every record already written, and a promise is not withdrawn; compaction is a NEW table under a NEW name (docs/SPEC-TABLES.md §2.10)",
			lk.Name)
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
			return fmt.Errorf("%s, is in the lock and gone from the declaration — a fixed table evolves APPEND-ONLY: a field is deprecated in place, never removed, because the record has no ids in it and every field after a removed one slides (docs/SPEC-TABLES.md §2.10); restore it and mark it `| deprecated`",
				where)
		}
		got := lv.Entries[i]
		if got.Id != want.Id {
			return fmt.Errorf("%s, is field %s (id=0x%016x) in the declaration — a fixed table evolves APPEND-ONLY: a field is added at the BOTTOM, never inserted, moved or removed, because the record has no ids in it and a reader at this offset would read one field as the other (docs/SPEC-TABLES.md §2.10)",
				where, got.Name, got.Id)
		}
		if got.Optional != want.Optional {
			return fmt.Errorf("%s, is %s in the lock and %s in the declaration — a field already in the lock keeps its `?`: an optional carries a presence bool beside its value INSIDE the record, so a reader that expects one where none is written takes the next field's first byte for the answer (docs/SPEC-TABLES.md §2.10); deprecate this field and append a new one",
				where, optionalText(want.Optional), optionalText(got.Optional))
		}
		if got.Width != want.Width {
			return fmt.Errorf("%s, is %d bytes wide in the lock and %d in the declaration — a field already in the lock keeps its width: a fixed record is walked by offset, so widening one field moves every field after it (docs/SPEC-TABLES.md §2.10); deprecate this field and append a new one",
				where, want.Width, got.Width)
		}
		if got.Kind != want.Kind {
			return fmt.Errorf("%s, is kind %d in the lock and kind %d in the declaration — a field already in the lock keeps its type: every record already written holds the old one, and nothing on the wire says which (docs/SPEC-TABLES.md §2.10); deprecate this field and append a new one",
				where, want.Kind, got.Kind)
		}
		if want.HeldName != got.HeldName {
			return fmt.Errorf("%s, held %s in the lock and %s in the declaration — a field already in the lock keeps the type it holds: every record already written holds the old one, and nothing on the wire says which (docs/SPEC-TABLES.md §2.10); deprecate this field and append a new one",
				where, heldName(want.HeldName), heldName(got.HeldName))
		}
		if want.ElemKind != got.ElemKind || want.ElemWidth != got.ElemWidth {
			return fmt.Errorf("%s, holds %s elements in the lock and %s elements in the declaration — a field already in the lock keeps its element type: every record already written holds the old one, and nothing on the wire says which (docs/SPEC-TABLES.md §2.10); deprecate this field and append a new one",
				where, elemWord(want.ElemKind, want.ElemWidth), elemWord(got.ElemKind, got.ElemWidth))
		}
		if want.KeyName != got.KeyName {
			return fmt.Errorf("%s, is keyed by %s in the lock and %s in the declaration — a field already in the lock keeps the enum it is keyed by: the key's variants ARE the slots, in declared order, so every record already written put its values in the slots the old list numbered (docs/SPEC-TABLES.md §2.10); deprecate this field and append a new one",
				where, heldName(want.KeyName), heldName(got.KeyName))
		}
		if !got.sameRange(want) {
			return fmt.Errorf("%s, is %s in the lock and %s in the declaration — a field already in the lock keeps its range: the bounds and the resolution are the scale a stored value is read back at, so moving them reads every record already written as a different number (docs/SPEC-TABLES.md §2.10); deprecate this field and append a new one",
				where, want.rangeText(), got.rangeText())
		}
		if got.Default != want.Default {
			return fmt.Errorf("%s, defaults to %s in the lock and %s in the declaration — a field already in the lock keeps its default: an older writer's missing field is filled from this default, and a deprecated slot holds it, so a record written before the change reads differently after it (docs/SPEC-TABLES.md §2.10); deprecate this field and append a new one",
				where, want.Default, got.Default)
		}
		if want.Deprecated && !got.Deprecated {
			return fmt.Errorf("%s, is deprecated in the lock and live in the declaration — deprecation is ONE-WAY: readers were told to ignore this slot and the writers that have run since left it at its default, so what comes back is not data (docs/SPEC-TABLES.md §2.10); append a new field instead",
				where)
		}
		// the marker turned ON: the change the rule allows, and the check
		// still wants it written down
		if policy == Current && !want.Deprecated && got.Deprecated {
			return stale(lv.Decl, lv.Name, entryName(lv, i), "deprecated in the declaration and live in the lock")
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
			return fmt.Errorf("%s, is in the lock and gone from the declaration — an enum, a flags mask or a union a fixed table reaches evolves APPEND-ONLY: every %s keeps its place forever, because a fixed record stores the PLACE and not the name (docs/SPEC-TABLES.md §2.10); restore it, and add the new one at the END",
				where, lk.Child())
		}
		if lv.Values[i] != want {
			return fmt.Errorf("%s, is %s in the declaration — an enum, a flags mask or a union a fixed table reaches evolves APPEND-ONLY: a new %s goes at the END, and one already in the lock is never inserted, moved or renamed, because a fixed record stores the PLACE and a reader would read one value as the other (docs/SPEC-TABLES.md §2.10); restore it, and add the new one at the END",
				where, lv.Values[i], lk.Child())
		}
		if lk.Decl == DeclUnion {
			wantP := payloadTypeName(valuePayload(lk, i))
			gotP := payloadTypeName(valuePayload(lv, i))
			if wantP != gotP {
				return fmt.Errorf("%s, held %s in the lock and %s in the declaration — an arm already in the lock keeps the type it holds: every record already written holds the old one, and nothing on the wire says which (docs/SPEC-TABLES.md §2.10); deprecate this field and append a new one",
					where, heldName(wantP), heldName(gotP))
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
