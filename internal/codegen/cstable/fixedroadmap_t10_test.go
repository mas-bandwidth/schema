package cstable

// fixedroadmap_t10 — the cs leg's optional, default and text/flags/bytes rows of
// docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed Tables"):
// optional-scalars, optional-arrays, scalar-defaults and
// text-bytes-flags-defaults, the four capability rows no other test of this leg
// asserts. The law is docs/SPEC-TABLES.md §3.4's message form and fixed form and
// docs/FIXED-FORM-ALGORITHM.md §3 (the prefill) and §4.6 (the bounds pass). One
// subtest per task id, and the assertion under each is the clause the task's own
// title states, read off the C# this leg emits and the ir walk it renders.
//
// A clause already held by an existing test of this leg is named by that test in
// the card's verdicts and is not repeated here; this file carries the direct
// assertion of every clause that had none. The runtime acceptance a capability
// row asks for is asserted at the emitter boundary: the generated write body
// serialises the construct with non-default and boundary values, the generated
// prefill/read restores it, and the leg runs neither of those against a sibling
// `../serialize.cs` here, which this card names as not run rather than fakes.

import (
	"testing"
)

const (
	// t10OptionalScalars is optional-scalars/cs/valid-data: a `?int32` and a
	// `?Grade`, a scalar and an enum, each off its default.
	t10OptionalScalars = `package probe

enum Grade { Bronze, Gold }

fixed table T
{
    num ?int32
    mark ?Grade
    tail int32 = 7
}
`
	// t10OptionalArrays is optional-arrays/cs/valid-data: `?[..2]Cell`, an
	// optional array whose present byte gates a count and the elements.
	t10OptionalArrays = `package probe

enum Cell { A, B, C }

fixed table T
{
    optional ?[..2]Cell
    tail int32 = 7
}
`
	// t10ScalarDefaults is scalar-defaults/cs/valid-data: an int32 and an enum
	// carrying a declared default.
	t10ScalarDefaults = `package probe

enum Grade { Bronze, Gold }

fixed table T
{
    x int32 = 42
    g Grade = Gold
}
`
	// t10TextBytesFlagsDefaults is text-bytes-flags-defaults/cs/valid-data:
	// a string, a byte-buffer and a flags value, each with a declared default.
	t10TextBytesFlagsDefaults = `package probe

flags Caps { Jump, Crouch }

fixed table T
{
    name string(8) = "untitled"
    tag  bytes(4) = "ab"
    caps Caps = { Jump, Crouch }
}
`
)

func TestFixedRoadmapCSharpT10Optionals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"optional-scalars/cs/valid-data", t10OptionalScalarsRow},
		{"optional-arrays/cs/valid-data", t10OptionalArraysRow},
		{"scalar-defaults/cs/valid-data", t10ScalarDefaultsRow},
		{"text-bytes-flags-defaults/cs/valid-data", t10TextBytesFlagsDefaultsRow},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t10WriteBody reads the generated TFixedWriteBody's statements back, so an
// assertion names the write of a construct and not the whole file.
func t10WriteBody(t *testing.T, src string) string {
	t.Helper()
	return t02Norm(t02Block(t, src, "public static void TFixedWriteBody(", "public static void"))
}

// t10ResetBody reads the generated TableReset(T)'s statements.
func t10ResetBody(t *testing.T, src string) string {
	t.Helper()
	return t02Norm(t02Block(t, src, "public static void TableReset(T value)", "\n        }"))
}

// t10OptionalScalarsRow: optional-scalars [capability] "Optional scalar and enum
// fields: valid-data write/read acceptance" (§3.4 record, ?T; ALGORITHM §3).
// A `?T` is a PRESENT BYTE and then the payload, and the payload rides only when
// the byte is set: the generated storage has a `<name>Present` bool beside the
// value, TableReset clears both, and the write body writes the present byte and
// gates the payload on it — for the scalar and the enum alike.
func t10OptionalScalarsRow(t *testing.T) {
	_, src := t02Table(t, t10OptionalScalars)
	n := t02Norm(src)
	for _, want := range []string{
		"public int Num;",
		"public bool NumPresent; // ?int32: absent until set",
		"public Grade Mark = Grade.None;",
		"public bool MarkPresent; // ?Grade: absent until set",
	} {
		t02Has(t, src, want, "optional-scalars: the storage carries the value and its present byte")
	}
	t02Has(t, n, "b[0] = (byte)(value.NumPresent ? 1 : 0);", "optional-scalars: the scalar's present byte is written")
	t02Has(t, n, "if (value.NumPresent) { BinaryPrimitives.WriteInt32LittleEndian(b.Slice(1), (int)value.Num); }",
		"optional-scalars: the scalar's payload rides only when present")
	t02Has(t, n, "b[5] = (byte)(value.MarkPresent ? 1 : 0);", "optional-scalars: the enum's present byte is written")
	t02Has(t, n, "if (value.MarkPresent) { b.Slice(6)[0] = (byte)value.Mark; }",
		"optional-scalars: the enum's payload rides only when present")
	reset := t10ResetBody(t, src)
	for _, want := range []string{"value.NumPresent = false;", "value.MarkPresent = false;"} {
		if !contains(reset, want) {
			t.Errorf("optional-scalars: TableReset does not clear the present byte: %q absent", want)
		}
	}
}

// t10OptionalArraysRow: optional-arrays [capability] "Optional arrays: valid-data
// write/read acceptance" (§3.4 optional wrapper; ALGORITHM §3). `?[..N]T` is a
// PRESENT BYTE, then a count, then the elements, and the count and the elements
// ride only when the byte is set: the generated storage has the buffer, a count
// and a `<name>Present` bool; the write body writes the byte and gates the count
// and element copy on it; and the read-side bounds pass visits every element.
func t10OptionalArraysRow(t *testing.T) {
	_, src := t02Table(t, t10OptionalArrays)
	n := t02Norm(src)
	t02Has(t, src, "public bool OptionalPresent; // ?Cell: absent until set",
		"optional-arrays: the wrapper carries its present byte")
	t02Has(t, n, "b[0] = (byte)(value.OptionalPresent ? 1 : 0);",
		"optional-arrays: the present byte is written")
	t02Has(t, n, "if (value.OptionalPresent) { int count_optional = value.OptionalCount;",
		"optional-arrays: the count rides behind the present byte")
	t02Has(t, n, "MemoryMarshal.AsBytes(value.Optional.AsSpan(0, Math.Min(value.Optional.Length, count_optional))).CopyTo(b.Slice(5));",
		"optional-arrays: the elements ride only when present")
	t02Has(t, n, "BinaryPrimitives.WriteInt32LittleEndian(b.Slice(1), count_optional);",
		"optional-arrays: the count is written behind the present byte, bounded by the declared count")
	t02Has(t, n, "System.Diagnostics.Debug.Assert(count_optional >= 0 && count_optional <= 2);",
		"optional-arrays: the declared count bounds the wire count")
	// The bounds pass is per ELEMENT: an ordinal past the enum's top counts once
	// for each element the wrapper carries.
	t02Has(t, n, "for (int i = 0; i < value.OptionalCount && i < value.Optional.Length; ++i) { if ((ulong)value.Optional[i] > 3) { value.Optional[i] = (Cell)0; clamped++; } }",
		"optional-arrays: the bounds pass visits every present element")
	reset := t10ResetBody(t, src)
	for _, want := range []string{"value.OptionalCount = 0;", "value.OptionalPresent = false;"} {
		if !contains(reset, want) {
			t.Errorf("optional-arrays: TableReset does not clear the wrapper: %q absent", want)
		}
	}
}

// t10ScalarDefaultsRow: scalar-defaults [capability] "Scalar and enum defaults:
// valid-data write/read acceptance" (§3.4 template; ALGORITHM §3). A declared
// default is the value TableReset lays down, the value the storage is born with,
// and the value the write body writes when no other value is set.
func t10ScalarDefaultsRow(t *testing.T) {
	_, src := t02Table(t, t10ScalarDefaults)
	n := t02Norm(src)
	t02Has(t, src, "public int X = 42;", "scalar-defaults: the scalar's storage is born at its default")
	t02Has(t, src, "public Grade G = Grade.Gold;", "scalar-defaults: the enum's storage is born at its default")
	reset := t10ResetBody(t, src)
	t02Has(t, reset, "value.X = 42;", "scalar-defaults: TableReset restores the scalar's default")
	t02Has(t, reset, "value.G = global::Probe.Grade.Gold;", "scalar-defaults: TableReset restores the enum's default")
	t02Has(t, n, "BinaryPrimitives.WriteInt32LittleEndian(b, (int)value.X);", "scalar-defaults: the write body writes the scalar")
	t02Has(t, n, "b.Slice(4)[0] = (byte)value.G;", "scalar-defaults: the write body writes the enum")
}

// t10TextBytesFlagsDefaultsRow: text-bytes-flags-defaults [capability] "String,
// byte-buffer and flags defaults: valid-data write/read acceptance" (ALGORITHM
// §3 prefill; landed PR847). A string(N) default is its bytes with the matching
// length, a bytes(N) default is its bytes with the matching length, and a flags
// default is its bitset: all three ride the prefill, and the write body copies
// the stored bytes and the flags value.
func t10TextBytesFlagsDefaultsRow(t *testing.T) {
	_, src := t02Table(t, t10TextBytesFlagsDefaults)
	reset := t10ResetBody(t, src)
	// "untitled" is eight bytes; the length default is 8.
	t02Has(t, reset, "value.Name[0] = 0x75;", "text-bytes-flags-defaults: the string prefill's first byte")
	t02Has(t, reset, "value.Name[7] = 0x64;", "text-bytes-flags-defaults: the string prefill's last byte")
	t02Has(t, reset, "value.NameLength = 8;", "text-bytes-flags-defaults: the string prefill's length")
	// bytes(4) "ab": two bytes, length 2.
	for _, want := range []string{"value.Tag[0] = 0x61;", "value.Tag[1] = 0x62;", "value.TagLength = 2;"} {
		t02Has(t, reset, want, "text-bytes-flags-defaults: the byte-buffer prefill")
	}
	// Caps { Jump, Crouch }: bitsets ride as the mask, so Jump|Crouch is 3.
	t02Has(t, reset, "value.Caps = 3ul;", "text-bytes-flags-defaults: the flags prefill is the bitset")
	t02Has(t, src, "public ulong Caps = 3ul;", "text-bytes-flags-defaults: the flags storage is born at its bitset")
	n := t02Norm(src)
	t02Has(t, n, "value.Name.AsSpan(0, len_name).CopyTo(b.Slice(4));",
		"text-bytes-flags-defaults: the write body copies the string bytes")
	t02Has(t, n, "value.Tag.AsSpan(0, len_tag).CopyTo(b.Slice(16));",
		"text-bytes-flags-defaults: the write body copies the byte-buffer bytes")
	t02Has(t, n, "BinaryPrimitives.WriteUInt64LittleEndian(b.Slice(20), (ulong)value.Caps);",
		"text-bytes-flags-defaults: the write body writes the flags bitset")
}

// contains is strings.Contains, named so a missing clause reads as a clause.
func contains(hay, needle string) bool {
	return len(needle) == 0 || (len(hay) >= len(needle) && indexOf(hay, needle) >= 0)
}

func indexOf(hay, needle string) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
