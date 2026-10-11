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
