// THE FIXED FORM's COMPILER-WIDE facts (docs/SPEC-TABLES.md §3.4), form byte 3:
// the shape of the LAYOUT on the wire, the two size bounds the compiler holds a
// fixed table to, and WHICH TABLES the form is emitted for.
//
// The sizes, the walk and the hash — everything that produces BYTES — are in
// fixedform.go beside them; these are the answers the COMPILER gives about a
// unit rather than the answers the wire gives about a record. They live here,
// in ir, for the reason those do: the C++ reference emits its layout from the
// same functions the compiler warns and refuses from, so the reference's bytes
// and the compiler's verdict cannot drift apart.
package ir

import "fmt"

// THE LAYOUT'S SHAPE, in bytes: a u32 entry count, then a run of seventeen-byte
// entries. THERE IS NO VERSION BYTE INSIDE THE LAYOUT (docs/SPEC-TABLES.md
// §3.4): the FORM BYTE versions everything behind it, the layout's own format
// included, so a layout format change is a NEW FORM BYTE and never a wider
// entry or a byte in front of the count.
const (
	FixedLayoutHeaderBytes = 4 // the entry count, and nothing else
	FixedEntryBytes        = 17
)

// THE TWO SIZE BOUNDS, and the owner's reason for them: *"effectively, fixed
// tables should only be used for small things."* A fixed record is a value
// copied whole at every bound it declares, so a big one is a big copy on every
// read and write and a big zero-fill behind every unused byte — which is the
// cost this form trades bytes for speed to avoid.
//
// The compiler WARNS above the first, and above the second it stops emitting
// the fixed form for that table altogether — see [FixedRecordBounds] for
// why those are different kinds of thing. A reader holds an UNTRUSTED PEER's
// layout to the same 65536, because a record whose size the layout states is
// past it is one this build will not decode, so the two sides agree by
// construction.
const (
	FixedRecordWarnBytes = 4096
	FixedRecordMaxBytes  = 65536
)

// FixedRoots is every table of the unit the fixed form applies to, in
// declaration order: a table whose mode is FIXED (§2.2), which is every table
// that is not in [VariableTables] and is not a map's synthesised entry.
//
// THIS IS THE SELECTION POINT FOR FORM 3, AND #823'S KEYWORD REPLACES IT. §3.4
// selects the form BY THE KEYWORD: a `fixed table` encodes as form 3 always, a
// `table` encodes as form 1, and there is no path between them. The keyword is
// on branch `fixed-table-keyword` and is not merged, so until it lands the
// selection is the DERIVED mode below — the only thing in this tree that marks
// a table fixed. When it lands, this function reads [Struct.FixedDeclared].
//
// It is the compiler's own answer and not a backend's: a backend may carry the
// form for fewer types than this — the C++ reference's compile-time plan bound
// (§3.4's "held by test") and the record ceiling [FixedFormRoots] applies
// are both such narrowings — but the SIZE BOUNDS are reported over ALL of them,
// because a table nobody warned about is a table nobody fixed.
func FixedRoots(u *Unit) []*Struct {
	variable := VariableTables(u)
	var out []*Struct
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if !st.IsTable || st.IsMapEntry() || variable[st.Name] {
				continue
			}
			out = append(out, st)
		}
	}
	return out
}

// FixedFormRoots is every table of the unit the FIXED FORM is emitted for:
// [FixedRoots] minus the ones whose record body is past
// [FixedRecordMaxBytes].
//
// THE 65536 IS THE WIRE'S CEILING AND NOT A PREFERENCE. A reader holds an
// untrusted peer's layout to it (§3.4's `layout_record_too_large`), so a record
// larger than it is one no conforming reader will decode — which makes emitting
// a writer for it a way to produce bytes nobody can read.
//
// A table that was merely DERIVED into the form (§2.2) and is past the ceiling
// keeps form 1, which §3.4 guarantees it never lost, and the compiler says so
// by name rather than dropping the form in silence. A table whose author
// DECLARED it fixed never reaches here at all: [FixedRecordBounds]
// refuses the compile, because the whole of #823's keyword is that a declared
// fixed table encodes as form 3 and a silent demotion to form 1 would be the
// surprise the keyword exists to prevent.
func FixedFormRoots(u *Unit) []*Struct {
	var out []*Struct
	for _, st := range FixedRoots(u) {
		if !FixedFormCarried(st) {
			continue // and if it was DECLARED fixed, FixedRecordBounds already refused the compile
		}
		out = append(out, st)
	}
	return out
}

// FixedFormCarried is THE WIRE's own narrowing, applied by every backend
// (docs/SPEC-TABLES.md §3.4). A reader holds an untrusted peer's layout to
// [FixedRecordMaxBytes], so a record larger than it is one no conforming reader
// decodes — which makes emitting a writer for it a way to produce bytes nobody
// can read.
//
// It is HERE and not in each backend's root selection because it is the same
// number on both sides of the wire, and a backend that narrowed by a different
// one would emit a form its own peers refuse. A backend may narrow FURTHER for
// reasons of its own — a compile-time plan bound, a closure shape it does not
// lay out — and those stay the backend's.
func FixedFormCarried(st *Struct) bool { return FixedTypeBytes(st) <= FixedRecordMaxBytes }

// FixedRecordBounds holds every fixed table of the unit to §3.4's two size
// bounds and answers the compiler's two channels: warnings, which name the
// table and the size and change no exit code, and refusals, which fail the
// compile.
//
// THE TWO BOUNDS ARE DIFFERENT KINDS OF THING, and conflating them would lose
// the difference:
//
//   - 4096 is ADVISORY and always on. Past it the table still carries the fixed
//     form and the compiler warns, naming the table and the size, because
//     *"effectively, fixed tables should only be used for small things"* is a
//     design rule and a rule nobody is told about is not a rule.
//   - 65536 is the WIRE's CEILING. Past it the table DOES NOT CARRY THE FIXED
//     FORM at all — no conforming reader would decode such a record (§3.4) — and
//     the compiler says so by name. It keeps form 1, which it never lost. This
//     is not a refusal of the unit: §12.1's render frame and §2.8's wide text
//     are legitimate fixed tables of megabytes that were never form-3 tables,
//     and refusing the unit over a form it does not use would be refusing the
//     wrong thing.
//
// `limit` is the HARD refusal bound in bytes and it is OFF by default (zero): a
// project that wants the advisory enforced as a gate sets `--fixed-record-limit
// 4096` and a fixed table past it does not compile. It is a project's policy
// knob and never a wire fact, and it only ever LOWERS: it cannot raise the
// 65536, because 65536 is the number a PEER's reader holds this build's records
// to (`layout_record_too_large`) and a peer's build has never heard of this
// project's flag.
//
// THE 65536 REFUSES A DECLARED FIXED TABLE AND DEMOTES A DERIVED ONE, and the
// difference is #823's keyword:
//
//   - A DECLARED fixed table ([Struct.FixedDeclared]) encodes as form 3 always.
//     Past the ceiling it cannot, and there is no form 1 for it to fall back
//     to without the author being surprised — *"if we add any feature that
//     stops it from being fixed, it is a compile error … we don't want to
//     surprise the user"* — so it is a REFUSAL BY NAME and the compile fails.
//   - A table merely DERIVED into the fixed mode (§2.2) asked for nothing. Past
//     the ceiling it keeps form 1, which it never lost, and the compiler warns.
//     §12.1's render frame and §2.8's wide text are exactly this: legitimate
//     fixed-MODE tables of megabytes that were never form-3 tables. WHEN #823
//     LANDS THIS BRANCH GOES AWAY — an undeclared `table` will not select form
//     3 at all — and it is here because the keyword is not merged.
func FixedRecordBounds(u *Unit, limit int64) (warnings []string, errs []error) {
	for _, st := range FixedRoots(u) {
		n := FixedTypeBytes(st)
		if limit > 0 && n > limit {
			errs = append(errs, fmt.Errorf(
				"table %s: a fixed table's record body is %d bytes, past the %d-byte --fixed-record-limit (docs/SPEC-TABLES.md §3.4) — a fixed record is copied WHOLE at every bound it declares, and *\"effectively, fixed tables should only be used for small things\"*; shrink the bounds it declares, make it a variable table (§2.2) with an unbounded array, a map or a pointer, or raise the limit",
				st.Name, n, limit))
			continue
		}
		switch {
		case n > FixedRecordMaxBytes && st.FixedDeclared:
			errs = append(errs, fmt.Errorf(
				"table %s: a DECLARED fixed table's record body is %d bytes, past the %d-byte fixed-form ceiling (docs/SPEC-TABLES.md §3.4) — no conforming reader decodes a record that size, so this table cannot carry form 3, and a fixed table never silently falls back to form 1; shrink the bounds it declares, or drop the `fixed` keyword and let it be a variable table (§2.2)",
				st.Name, n, FixedRecordMaxBytes))
		case n > FixedRecordMaxBytes:
			warnings = append(warnings, fmt.Sprintf(
				"table %s: a fixed table's record body is %d bytes, past the %d-byte fixed-form ceiling (docs/SPEC-TABLES.md §3.4) — THE FIXED FORM IS NOT EMITTED FOR IT, because no conforming reader decodes a record that size; it keeps form 1",
				st.Name, n, FixedRecordMaxBytes))
		case n > FixedRecordWarnBytes:
			warnings = append(warnings, fmt.Sprintf(
				"table %s: a fixed table's record body is %d bytes, past the %d-byte advisory bound (docs/SPEC-TABLES.md §3.4) — every read and write copies all of it and zero-fills the slack, and fixed tables are for small things; the fixed form stops being emitted at %d",
				st.Name, n, FixedRecordWarnBytes, FixedRecordMaxBytes))
		}
	}
	return warnings, errs
}
