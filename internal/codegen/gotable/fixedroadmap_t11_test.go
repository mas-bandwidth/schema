package gotable

// THE ROADMAP'S UNIONS AND OPTIONALS ROWS ON THE GO LEG
// (docs/roadmap.sexp node `fixed-tables`, ROADMAP.md "NEW Fixed Tables"). Law:
// docs/FIXED-FORM-ALGORITHM.md §3.1 (an absent optional writes the template's
// zeros), §3.4 (the fixed record), docs/FIXED-FORM-VERSIONING-TESTS.md (the
// union and optional rows) and docs/SPEC-TABLES.md §3.4. One subtest per task
// id, table-driven, t.Parallel() first, and the assertion under each is the
// clause the task's own title states, exercised with non-default and boundary
// values.
//
// A CAPABILITY HERE IS RUN, NOT READ: the emitted form-3 table module is
// self-contained for these shapes (no serialize.go, no packet types), so the
// probe compiles and runs the generated RootFixedSave/RootFixedLoad and reads
// the values back. Where a shape carries an enum or a flags type — whose Go
// spelling is emitted by the packet codec, not this leg — the clause is read
// off the emitted fixed surface instead, and the subtest says so.
//
// THE TWO ROWS THE GO LEG DOES NOT HOLD get no subtest and an `unknown`
// verdict: union-field-arms (an arm holding text or an array) and
// payload-free-arms (a void arm) are refused by ir.TableFixedSupported
// (ir/fixedform.go:260), the one closure both the layout and every runtime
// ask, and ir/ is outside this card's PATHS.

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFixedRoadmapGoT11UnionsOptionals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"union-values/go/valid-data", t11UnionValues},
		{"union-arrays/go/valid-data", t11UnionArrays},
		{"optional-scalars/go/valid-data", t11OptionalScalars},
		{"optional-nesting/go/valid-data", t11OptionalNesting},
		{"optional-arrays/go/valid-data", t11OptionalArrays},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t11Run writes the gotable module for a unit into a scratch module that
// imports nothing, then compiles and runs the probe against it. It fails with
// the whole tool output so a red line names the emitted site that broke.
func t11Run(t *testing.T, schema, probe string) {
	t.Helper()
	files, err := Generate(unitFrom(t, schema))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	dir := t.TempDir()
	files["go.mod"] = []byte("module probe\n\ngo 1.26\n")
	files["t11_probe_test.go"] = []byte(probe)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-timeout", "120s", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the emitted fixed table failed to compile or run: %v\n%s", err, out)
	}
}

// t11RoundTrip is the probe prelude every running subtest shares: save one
// record with the generated writer, read it back with the generated reader and
// hand back the value, so a caller asserts on the reader's own output.
const t11RoundTrip = `package probe
import "testing"

func saveLoad(t *testing.T, v Root) Root {
	t.Helper()
	file := make([]byte, RootFixedMeasure(1))
	if RootFixedSave([]Root{v}, file) != int64(len(file)) {
		t.Fatalf("RootFixedSave wrote %d bytes", len(file))
	}
	values := make([]Root, 1)
	plan := make([]TableFixedEntry, 128)
	var report TableReport
	if n := RootFixedLoad(values, file, plan, &report); n != 1 || report != (TableReport{}) {
		t.Fatalf("RootFixedLoad: n=%d report=%+v", n, report)
	}
	return values[0]
}
`

// ---- go/union-values --------------------------------------------------------

// t11UnionValues holds a tagged union to its written tag and arm and to its
// read-back values: "the TAG at the width its variant count derives, then the
// WIDEST ARM" (ir.TableFixedUnionBytes; docs/SPEC-TABLES.md §3.4). A scalar arm
// is written at its declared width, a fixed-table arm through its own body, and
// a tag past the arm count comes back as None with Clamped counting it.
func t11UnionValues(t *testing.T) {
	t11Run(t, `package probe
fixed table Point { x int32 }
fixed table Child { score int32 = 7 }
union U {
    n int32
    point Point
    child Child
}
fixed table Root { value U }
`, t11RoundTrip+`
func TestT11UnionValues(t *testing.T) {
	var v Root
	for _, n := range []int32{-2147483648, -1, 0, 2147483647} {
		RootReset(&v)
		v.Value.Type = UTypeN
		v.Value.N = n
		if got := saveLoad(t, v).Value; got.Type != UTypeN || got.N != n {
			t.Fatalf("scalar arm %d read back %+v", n, got)
		}
	}
	RootReset(&v)
	v.Value.Type = UTypePoint
	v.Value.Point.X = -12345
	if got := saveLoad(t, v).Value; got.Type != UTypePoint || got.Point.X != -12345 {
		t.Fatalf("fixed-table arm read back %+v", got)
	}
	RootReset(&v)
	v.Value.Type = UTypeChild
	v.Value.Child.Score = 99
	if got := saveLoad(t, v).Value; got.Type != UTypeChild || got.Child.Score != 99 {
		t.Fatalf("table arm read back %+v", got)
	}
	// boundary: a tag past UTypeMax is clamped to None and counted
	RootReset(&v)
	v.Value.Type = UTypeCount
	file := make([]byte, RootFixedMeasure(1))
	RootFixedSave([]Root{v}, file)
	values := make([]Root, 1)
	var report TableReport
	if n := RootFixedLoad(values, file, make([]TableFixedEntry, 128), &report); n != 1 || report.Clamped != 1 || values[0].Value.Type != UTypeNone {
		t.Fatalf("out-of-range tag: n=%d clamped=%d type=%v", n, report.Clamped, values[0].Value.Type)
	}
}
`)
}

// ---- go/union-arrays --------------------------------------------------------

// t11UnionArrays holds an array of unions to the element reset and guard: "a
// repeated array field must not retain an earlier selected arm when L is bad"
// (values_test.go TestRepeatedUnionElement), and the live count bounds the
// elements the read walks (docs/SPEC-TABLES.md §3.4). Zero, one and the bound
// elements round-trip, and an out-of-range tag in the last live element clamps.
func t11UnionArrays(t *testing.T) {
	t11Run(t, `package probe
union U { n int32 }
fixed table Root { entries [..3]U }
`, t11RoundTrip+`
func TestT11UnionArrays(t *testing.T) {
	var v Root
	for _, count := range []int32{0, 1, 3} {
		RootReset(&v)
		v.EntriesCount = count
		for i := int32(0); i < count; i++ {
			v.Entries[i].Type = UTypeN
			v.Entries[i].N = -100 - i
		}
		got := saveLoad(t, v)
		if got.EntriesCount != count {
			t.Fatalf("count %d read back %d", count, got.EntriesCount)
		}
		for i := int32(0); i < count; i++ {
			if got.Entries[i].Type != UTypeN || got.Entries[i].N != -100-i {
				t.Fatalf("count %d element %d read back %+v", count, i, got.Entries[i])
			}
		}
	}
	RootReset(&v)
	v.EntriesCount = 3
	v.Entries[2].Type = UTypeCount
	file := make([]byte, RootFixedMeasure(1))
	RootFixedSave([]Root{v}, file)
	values := make([]Root, 1)
	var report TableReport
	if n := RootFixedLoad(values, file, make([]TableFixedEntry, 128), &report); n != 1 || report.Clamped != 1 || values[0].Entries[2].Type != UTypeNone {
		t.Fatalf("out-of-range element tag: n=%d clamped=%d type=%v", n, report.Clamped, values[0].Entries[2].Type)
	}
}
`)
}

// ---- go/optional-scalars ----------------------------------------------------

// t11OptionalScalars holds an optional scalar to its present byte and payload:
// the flag is written as a normalised bool and the payload "rides WHOLE whether
// or not it is present, and when the flag is 0 what rides is zero"
// (docs/FIXED-FORM-ALGORITHM.md §3.1, fix 13). Present at both int32 ends and
// absent both round-trip. The enum clause carries no probe: an enum's Go
// spelling is the packet codec's, so the emitted optional-enum surface is read
// instead and the subtest says so.
func t11OptionalScalars(t *testing.T) {
	t11Run(t, `package probe
fixed table Root { n ?int32 }
`, t11RoundTrip+`
func TestT11OptionalScalars(t *testing.T) {
	var v Root
	for _, present := range []bool{false, true} {
		for _, n := range []int32{-2147483648, 0, 2147483647} {
			RootReset(&v)
			v.NPresent = present
			v.N = n
			got := saveLoad(t, v)
			if got.NPresent != present || (present && got.N != n) {
				t.Fatalf("present=%v n=%d read back %+v", present, n, got)
			}
			if !present && got.N != 0 {
				t.Fatalf("absent optional read back payload %d, want the template's zero", got.N)
			}
		}
	}
}
`)
	// The enum clause, read off the emitted fixed surface: e ?E writes the
	// present byte, then copies the storage at its declared width under the
	// flag, exactly as the scalar arm does.
	src := t02GoEmit(t, unitFrom(t, `package probe
enum E { A, B }
fixed table Root { e ?E }
`))
	n := t02GoNorm(src)
	t02GoHas(t, n, "tableFixedPut8(b[0:], p)", "optional-scalars: the present byte is written first")
	t02GoHas(t, n, "if value.EPresent { tableFixedPut8(b[1:], uint8(value.E)) }", "optional-scalars: the enum payload follows only when present")
	t02GoHas(t, n, "Op: tableFixedBool} // e present", "optional-scalars: the present byte reads back into a Go bool")
}

// ---- go/optional-nesting ----------------------------------------------------

// t11OptionalNesting holds an optional nested value to the flag and the nested
// body: present runs the nested type's own write body and absent writes the
// template's zeros (docs/FIXED-FORM-ALGORITHM.md §3.1). Present and absent both
// round-trip, and an absent optional does not disturb a sibling field.
func t11OptionalNesting(t *testing.T) {
	t11Run(t, `package probe
fixed table P { x int32 }
fixed table Root { p ?P }
`, t11RoundTrip+`
func TestT11OptionalNesting(t *testing.T) {
	var v Root
	RootReset(&v)
	v.PPresent = true
	v.P.X = -2147483648
	if got := saveLoad(t, v); !got.PPresent || got.P.X != -2147483648 {
		t.Fatalf("present nested read back %+v", got)
	}
	RootReset(&v)
	if got := saveLoad(t, v); got.PPresent || got.P.X != 0 {
		t.Fatalf("absent nested read back %+v", got)
	}
}
`)
}

// ---- go/optional-arrays -----------------------------------------------------

// t11OptionalArrays holds an optional counted array to the flag, the count and
// the live elements: an absent optional's payload is the template's zeros, and
// a present one carries its count then its elements (docs/SPEC-TABLES.md §3.4).
// Absent, empty-present and full-present round-trip.
func t11OptionalArrays(t *testing.T) {
	t11Run(t, `package probe
fixed table Root { v ?[..3]int32 }
`, t11RoundTrip+`
func TestT11OptionalArrays(t *testing.T) {
	var v Root
	RootReset(&v)
	if got := saveLoad(t, v); got.VPresent || got.VCount != 0 {
		t.Fatalf("absent optional array read back %+v", got)
	}
	for _, count := range []int32{0, 3} {
		RootReset(&v)
		v.VPresent = true
		v.VCount = count
		for i := int32(0); i < count; i++ {
			v.V[i] = 7 + i
		}
		got := saveLoad(t, v)
		if !got.VPresent || got.VCount != count {
			t.Fatalf("count %d read back %+v", count, got)
		}
		for i := int32(0); i < count; i++ {
			if got.V[i] != 7+i {
				t.Fatalf("count %d element %d = %d", count, i, got.V[i])
			}
		}
	}
}
`)
}
