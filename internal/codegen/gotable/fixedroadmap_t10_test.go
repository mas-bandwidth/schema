package gotable

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE ROADMAP'S COMPOSITE-VALUE ROWS ON THE GO LEG (docs/roadmap.sexp node
// `fixed-tables`, ROADMAP.md "NEW Fixed Tables"). Law: docs/SPEC-TABLES.md §3.4
// — "A FIXED-TABLE RECORD IS AN EIGHT-BYTE HASH OF THE WRITER'S LAYOUT, AND
// THEN THE VALUES IN DECLARED ORDER, EVERY FIELD AT ITS BOUND" — and
// docs/FIXED-FORM-ALGORITHM.md §3.1 (the write) and §4.4 (the run), the read
// the writer mirrors. One subtest per task id, table-driven, t.Parallel()
// first. Each subtest WRITES a record with its construct at non-default and
// boundary values and READS it back through the leg's own emitted
// `XFixedSave`/`XFixedLoad`, so the capability is proved by a value round-trip
// and not by the shape of the emitted text.
//
// The generated unit here is the leg's TABLE module alone (plus the packet
// types a `type`, `enum` or `flags` declaration needs), built and run in a
// throwaway module. The one narrowing: the packet half of a nested `type` is
// emitted against the sibling `serialize.go` runtime and is not compiled here,
// so the `nested-types` probe declares the value struct the table module
// splices. The fixed writer/reader under test is the leg's table module in
// every case.

// t10Case is one composite construct: the schema that declares it, the fixed
// table it lands in, and the probe package that round-trips it.
type t10Case struct {
	id        string
	schema    string
	probe     string
	tableOnly bool
}

// t10Prelude is the probe's one round-trip helper: write ONE Root through the
// leg's combined writer, read it back through the leg's combined reader, and
// insist the load was clean. docs/SPEC-TABLES.md §3.4 record form.
const t10Prelude = `package probe
import "testing"

func roundtrip(t *testing.T, v Root) Root {
	t.Helper()
	buf := make([]byte, RootFixedMeasure(1))
	if n := RootFixedSave([]Root{v}, buf); n != int64(len(buf)) {
		t.Fatalf("save returned %d, want %d", n, len(buf))
	}
	back := make([]Root, 1)
	plan := make([]TableFixedEntry, 256)
	var r TableReport
	if n := RootFixedLoad(back, buf, plan, &r); n != 1 || r != (TableReport{}) {
		t.Fatalf("load returned %d, report %+v", n, r)
	}
	return back[0]
}
`

func TestFixedRoadmapGoT10FloatsComposites(t *testing.T) {
	t.Parallel()
	for _, tc := range t10Cases {
		tc := tc
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			t10Run(t, tc.schema, tc.probe, tc.tableOnly)
		})
	}
}

// t10Run generates the schema's unit, writes it with the probe into a
// throwaway module and runs `go test` there. The packet half is emitted too
// except for a construct whose packet types need the absent sibling runtime.
func t10Run(t *testing.T, schema, probe string, tableOnly bool) {
	t.Helper()
	u := unitFrom(t, schema)
	files := map[string][]byte{}
	if !tableOnly {
		packet, err := golang.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		maps.Copy(files, packet)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module probe\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	files["values_test.go"] = []byte(probe)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated fixed-table runtime: %v\n%s", err, out)
	}
}

var t10Cases = []t10Case{
	{
		id:     "flags-values/go/valid-data",
		schema: "package probe\nflags Caps { Jump, Fly, Swim }\nfixed table Root { caps Caps }\n",
		// §3.4 "EVERY FIELD AT ITS BOUND": a flags mask is one whole-number
		// store. Empty, each named bit, a composition and the full mask.
		probe: t10Prelude + `
func TestFlags(t *testing.T) {
	for _, want := range []Caps{0, CapsJump, CapsFly | CapsSwim, CapsJump | CapsFly | CapsSwim} {
		var v Root
		RootReset(&v)
		v.Caps = want
		if got := roundtrip(t, v); got.Caps != want {
			t.Fatalf("flags %d: got %d", want, got.Caps)
		}
	}
}
`,
	},
	{
		id:     "enum-values/go/valid-data",
		schema: "package probe\nenum Grade { Gold, Silver, Bronze }\nfixed table Root { grade Grade }\n",
		// §3.4: an enum lands at its ordinal width. None (0), the first and
		// last named variants, and the exported extent.
		probe: t10Prelude + `
func TestEnum(t *testing.T) {
	for _, want := range []Grade{0, GradeGold, GradeSilver, GradeBronze, GradeMax} {
		var v Root
		RootReset(&v)
		v.Grade = want
		if got := roundtrip(t, v); got.Grade != want {
			t.Fatalf("grade %d: got %d", want, got.Grade)
		}
	}
}
`,
	},
	{
		id:     "utf8-values/go/valid-data",
		schema: "package probe\nfixed table Root { name string(12) }\n",
		// §3.4: a bounded UTF-8 string is a length word and its units. Empty,
		// an ASCII unit, a multi-byte unit and the full capacity.
		probe: t10Prelude + `
func TestUtf8(t *testing.T) {
	for _, want := range []string{"", "a", "héllo", "0123456789ab"} {
		var v Root
		RootReset(&v)
		copy(v.Name[:], want)
		v.NameLength = int32(len(want))
		got := roundtrip(t, v)
		if int(got.NameLength) != len(want) || string(got.Name[:got.NameLength]) != want {
			t.Fatalf("string %q: got %q len %d", want, got.Name[:got.NameLength], got.NameLength)
		}
	}
}
`,
	},
	{
		id:     "utf16-values/go/valid-data",
		schema: "package probe\nfixed table Root { wide wstring(10) }\n",
		// §3.4: UTF-16 stores one unit at a time at two bytes each. Empty, a
		// BMP unit, a wide pair and the full capacity.
		probe: t10Prelude + `
func TestUtf16(t *testing.T) {
	for _, want := range [][]uint16{nil, {0x0041}, {0x00E9, 0x4E2D}, {1, 2, 3, 4, 5, 6, 7, 8, 9, 10}} {
		var v Root
		RootReset(&v)
		copy(v.Wide[:], want)
		v.WideLength = int32(len(want))
		got := roundtrip(t, v)
		if int(got.WideLength) != len(want) {
			t.Fatalf("wstring %v: got len %d", want, got.WideLength)
		}
		for i := range want {
			if got.Wide[i] != want[i] {
				t.Fatalf("wstring %v: got %v", want, got.Wide[:got.WideLength])
			}
		}
	}
}
`,
	},
	{
		id:     "byte-buffers/go/valid-data",
		schema: "package probe\nfixed table Root { blob bytes(6) }\n",
		// §3.4: a bounded byte buffer is a length word and raw bytes. Empty, a
		// single zero, the extremes and the full capacity.
		probe: t10Prelude + `
func TestBytes(t *testing.T) {
	for _, want := range [][]byte{nil, {0x00}, {0xFF, 0x00, 0x5A}, {1, 2, 3, 4, 5, 6}} {
		var v Root
		RootReset(&v)
		copy(v.Blob[:], want)
		v.BlobLength = int32(len(want))
		got := roundtrip(t, v)
		if int(got.BlobLength) != len(want) {
			t.Fatalf("bytes %v: got len %d", want, got.BlobLength)
		}
		for i := range want {
			if got.Blob[i] != want[i] {
				t.Fatalf("bytes %v: got %v", want, got.Blob[:got.BlobLength])
			}
		}
	}
}
`,
	},
	{
		id:        "nested-types/go/valid-data",
		schema:    "package probe\ntype Badge { title string(8) }\nfixed table Root { badge Badge }\n",
		tableOnly: true,
		// §3.4: a nested `type` by value is its own leaves, spliced where the
		// field sits. The value struct the table module splices is declared
		// here because the packet half of a `type` is emitted against the
		// absent sibling runtime.
		probe: t10Prelude + `
type Badge struct {
	Title       [8]byte
	TitleLength int32
}

func TestNested(t *testing.T) {
	for _, want := range []string{"", "hi", "abcdefgh"} {
		var v Root
		RootReset(&v)
		copy(v.Badge.Title[:], want)
		v.Badge.TitleLength = int32(len(want))
		got := roundtrip(t, v)
		if int(got.Badge.TitleLength) != len(want) || string(got.Badge.Title[:got.Badge.TitleLength]) != want {
			t.Fatalf("nested %q: got %q len %d", want, got.Badge.Title[:got.Badge.TitleLength], got.Badge.TitleLength)
		}
	}
}
`,
	},
	{
		id:     "nested-fixed-tables/go/valid-data",
		schema: "package probe\nfixed table Child { n int32 }\nfixed table Root { child Child }\n",
		// §3.4: a nested `fixed table` splices its own leaves into the
		// parent's. Zero, a negative and the extremes.
		probe: t10Prelude + `
func TestNestedFixed(t *testing.T) {
	for _, want := range []int32{0, -1, -1 << 31, 1<<31 - 1} {
		var v Root
		RootReset(&v)
		v.Child.N = want
		if got := roundtrip(t, v); got.Child.N != want {
			t.Fatalf("nested fixed %d: got %d", want, got.Child.N)
		}
	}
}
`,
	},
	{
		id:     "fixed-arrays/go/valid-data",
		schema: "package probe\nfixed table Root { tag [3]int32 }\n",
		// §3.4: a fixed-length array is one store per slot. Every slot set,
		// with negative and extremal values.
		probe: t10Prelude + `
func TestFixedArray(t *testing.T) {
	want := [3]int32{-1 << 31, 0, 1<<31 - 1}
	var v Root
	RootReset(&v)
	v.Tag = want
	if got := roundtrip(t, v); got.Tag != want {
		t.Fatalf("fixed array: got %v want %v", got.Tag, want)
	}
}
`,
	},
	{
		id:     "counted-arrays/go/valid-data",
		schema: "package probe\nfixed table Root { nums [..4]uint16 }\n",
		// §3.4: a bounded counted array is a live count and then the slots.
		// Empty, one, and the full capacity with the element extremes.
		probe: t10Prelude + `
func TestCounted(t *testing.T) {
	full := [4]uint16{0, 1, 65535, 32768}
	for count := 0; count <= 4; count++ {
		var v Root
		RootReset(&v)
		v.Nums = full
		v.NumsCount = int32(count)
		got := roundtrip(t, v)
		if int(got.NumsCount) != count {
			t.Fatalf("count %d: got count %d", count, got.NumsCount)
		}
		for i := 0; i < count; i++ {
			if got.Nums[i] != full[i] {
				t.Fatalf("count %d slot %d: got %d want %d", count, i, got.Nums[i], full[i])
			}
		}
	}
}
`,
	},
	{
		id:     "keyed-arrays/go/valid-data",
		schema: "package probe\nenum Key { A, B }\nfixed table Root { slots [Key]int32 }\n",
		// §3.4: an enum-keyed array holds one slot per NAMED variant, the key
		// k at index k-1. Both slots set to distinct extremal values.
		probe: t10Prelude + `
func TestKeyed(t *testing.T) {
	var v Root
	RootReset(&v)
	v.Slots[KeyA-1] = -1 << 31
	v.Slots[KeyB-1] = 1<<31 - 1
	got := roundtrip(t, v)
	if got.Slots[KeyA-1] != -1<<31 || got.Slots[KeyB-1] != 1<<31-1 {
		t.Fatalf("keyed: got %v", got.Slots)
	}
}
`,
	},
	{
		id:     "nested-keyed-arrays/go/valid-data",
		schema: "package probe\nenum Key { A, B }\nfixed table Child { n int32 }\nfixed table Root { slots [Key]Child }\n",
		// §3.4: an enum-keyed array of nested values holds one nested leaf run
		// per named variant. Both slots set to distinct values.
		probe: t10Prelude + `
func TestNestedKeyed(t *testing.T) {
	var v Root
	RootReset(&v)
	v.Slots[KeyA-1].N = 11
	v.Slots[KeyB-1].N = -22
	got := roundtrip(t, v)
	if got.Slots[KeyA-1].N != 11 || got.Slots[KeyB-1].N != -22 {
		t.Fatalf("nested keyed: got %v", got.Slots)
	}
}
`,
	},
}
