// THE FIXED FORM's target-independent facts (docs/SPEC-TABLES.md §3.4), form
// byte 3: the ONE kind its LAYOUT adds to §3's closed set, the constant size of
// a field and of a type, and the two size bounds the compiler holds a fixed
// table to.
//
// They live here, beside the kind vocabulary, for the reason the kinds do: a
// record's constant size is WIRE LAW and not a backend's arithmetic. The C++
// reference emits its layout and its template from these functions and the
// compiler warns and refuses from the same ones, so the reference's bytes and
// the compiler's verdict cannot drift apart.
package ir

import "fmt"

// TableKindOptional is the ONE kind §3.4's LAYOUT adds to §3's closed set: the
// OPTIONAL WRAPPER, one child, whose constant size is one present byte plus its
// child's. It is a LAYOUT kind and not a WIRE kind — nothing rides under it in
// a record — and it exists because on this form `?T` and a plain `T` are ONE
// BYTE APART where §2.3's form-1 rule makes them wire-identical.
const TableKindOptional = 35

// THE LAYOUT'S SHAPE, in bytes: a u32 entry count, then a run of seventeen-byte
// entries. THERE IS NO VERSION BYTE INSIDE THE LAYOUT (docs/SPEC-TABLES.md
// §3.4): the FORM BYTE versions everything behind it, the layout's own format
// included, so a layout format change is a NEW FORM BYTE and never a wider
// entry or a byte in front of the count.
const (
	TableFixedLayoutHeaderBytes = 4 // the entry count, and nothing else
	TableFixedEntryBytes        = 17
)

// THE TWO SIZE BOUNDS, and the owner's reason for them: *"effectively, fixed
// tables should only be used for small things."* A fixed record is a value
// copied whole at every bound it declares, so a big one is a big copy on every
// read and write and a big zero-fill behind every unused byte — which is the
// cost this form trades bytes for speed to avoid.
//
// The compiler WARNS above the first, and above the second it stops emitting
// the fixed form for that table altogether — see [TableFixedRecordBounds] for
// why those are different kinds of thing. A reader holds an UNTRUSTED PEER's
// layout to the same 65536, because a record whose size the layout states is
// past it is one this build will not decode, so the two sides agree by
// construction.
const (
	TableFixedRecordWarnBytes = 4096
	TableFixedRecordMaxBytes  = 65536
)

// The head of a field's constant size: a count, a length and a present flag,
// which are what stand in front of every byte of declared slack.
const (
	TableFixedCountBytes   = int64(4)
	TableFixedPresentBytes = int64(1)
)

// TableFixedStorageBytes is a LEAF's declared storage width — the width SPEC.md
// gives the declaration, identical in every port, which is what makes the
// identity plan a copy rather than a conversion (§3.4).
func TableFixedStorageBytes(t FieldType) int64 {
	switch t.Kind {
	case TBool:
		return 1
	case TInt, TFixed:
		return int64(t.Width / 8)
	case TBits:
		if t.Width <= 32 {
			return 4
		}
		return 8
	case TFloat32:
		return 4
	case TFloat64:
		return 8
	case TNamed:
		switch r := t.Ref.(type) {
		case *Enum:
			return int64(r.StorageBits / 8)
		case *Flags:
			return 8 // the raw mask, as §3 carries it
		}
	}
	return 0
}

// TableFixedTypeBytes is C(T): the sum of its fields' constant sizes. Every one
// is a compile-time constant, which is why a backend's MeasureBody is a
// constexpr and not a function that reads the value.
func TableFixedTypeBytes(st *Struct) int64 {
	var n int64
	for _, f := range st.Fields {
		n += TableFixedFieldBytes(f)
	}
	return n
}

// TableFixedFieldBytes is C(f) — §3.4's constant-size table in one function.
func TableFixedFieldBytes(f *Field) int64 {
	var payload int64
	switch {
	case f.KeyEnum != "":
		payload = f.KeyEnumRef.Max * TableFixedElementBytes(f)
	case f.Array == ArrayFixed:
		payload = f.ArrayBound * TableFixedElementBytes(f)
	case f.Array == ArrayCounted:
		payload = TableFixedCountBytes + f.ArrayBound*TableFixedElementBytes(f)
	case f.Type.Kind == TString:
		payload = TableFixedCountBytes + f.Type.Size
	case f.Type.Kind == TWString:
		payload = TableFixedCountBytes + 2*f.Type.Size
	case f.Type.Kind == TBytes:
		payload = TableFixedCountBytes + f.Type.Size
	default:
		payload = TableFixedElementBytes(f)
	}
	if f.Type.Optional {
		return TableFixedPresentBytes + payload
	}
	return payload
}

// TableFixedElementBytes is the constant of ONE element of a field.
func TableFixedElementBytes(f *Field) int64 {
	if f.Type.Kind == TNamed {
		switch r := f.Type.Ref.(type) {
		case *Struct:
			return TableFixedTypeBytes(r)
		case *Union:
			// the tag, then the WIDEST ARM: the slack behind a narrower one is
			// zero on write and ignored on read (§3.4)
			widest := int64(0)
			for _, v := range r.Variants {
				if v.F == nil {
					continue
				}
				if n := TableFixedFieldBytes(v.F); n > widest {
					widest = n
				}
			}
			return int64(StorageBitsFor(r.Max)/8) + widest
		}
	}
	return TableFixedStorageBytes(f.Type)
}

// TableFixedRoots is every table of the unit the fixed form applies to, in
// declaration order: a table whose mode is FIXED (§2.2), which is every table
// that is not in [VariableTables] and is not a map's synthesised entry.
//
// It is the compiler's own answer and not a backend's: a backend may carry the
// form for fewer types than this — the C++ reference's compile-time plan bound
// (§3.4's "held by test") and the record ceiling [TableFixedFormRoots] applies
// are both such narrowings — but the SIZE BOUNDS are reported over ALL of them,
// because a table nobody warned about is a table nobody fixed.
func TableFixedRoots(u *Unit) []*Struct {
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

// TableFixedFormRoots is every table of the unit the FIXED FORM is emitted for:
// [TableFixedRoots] minus the ones whose record body is past
// [TableFixedRecordMaxBytes].
//
// THE 65536 IS THE WIRE'S CEILING AND NOT A PREFERENCE. A reader holds an
// untrusted peer's layout to it (§3.4's `layout_record_too_large`), so a record
// larger than it is one no conforming reader will decode — which makes emitting
// a writer for it a way to produce bytes nobody can read. A table past it keeps
// FORM 1, which §3.4 guarantees it never lost, and the compiler says so by
// name rather than dropping the form in silence.
func TableFixedFormRoots(u *Unit) []*Struct {
	var out []*Struct
	for _, st := range TableFixedRoots(u) {
		if TableFixedTypeBytes(st) > TableFixedRecordMaxBytes {
			continue
		}
		out = append(out, st)
	}
	return out
}

// TableFixedRecordBounds holds every fixed table of the unit to §3.4's two size
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
// knob and never a wire fact, which is why the wire ceiling above does not move
// with it.
func TableFixedRecordBounds(u *Unit, limit int64) (warnings []string, errs []error) {
	for _, st := range TableFixedRoots(u) {
		n := TableFixedTypeBytes(st)
		if limit > 0 && n > limit {
			errs = append(errs, fmt.Errorf(
				"table %s: a fixed table's record body is %d bytes, past the %d-byte --fixed-record-limit (docs/SPEC-TABLES.md §3.4) — a fixed record is copied WHOLE at every bound it declares, and *\"effectively, fixed tables should only be used for small things\"*; shrink the bounds it declares, make it a variable table (§2.2) with an unbounded array, a map or a pointer, or raise the limit",
				st.Name, n, limit))
			continue
		}
		switch {
		case n > TableFixedRecordMaxBytes:
			warnings = append(warnings, fmt.Sprintf(
				"table %s: a fixed table's record body is %d bytes, past the %d-byte fixed-form ceiling (docs/SPEC-TABLES.md §3.4) — THE FIXED FORM IS NOT EMITTED FOR IT, because no conforming reader decodes a record that size; it keeps form 1",
				st.Name, n, TableFixedRecordMaxBytes))
		case n > TableFixedRecordWarnBytes:
			warnings = append(warnings, fmt.Sprintf(
				"table %s: a fixed table's record body is %d bytes, past the %d-byte advisory bound (docs/SPEC-TABLES.md §3.4) — every read and write copies all of it and zero-fills the slack, and fixed tables are for small things; the fixed form stops being emitted at %d",
				st.Name, n, TableFixedRecordWarnBytes, TableFixedRecordMaxBytes))
		}
	}
	return warnings, errs
}
