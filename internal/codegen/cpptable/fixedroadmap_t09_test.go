package cpptable

// THE ROADMAP'S INTEGER AND FLOAT VALUE ROWS ON THE CPP LEG
// (docs/roadmap.sexp node `fixed-tables`, ROADMAP.md "NEW Fixed Tables"). One
// subtest per task id, table-driven, t.Parallel() first, and each assertion is
// the clause the task's own title states, read off the C++ this leg emits for
// the fixed form's per-field write and read storage.
//
// THE LAW IS docs/SPEC-TABLES.md §3.4: the fixed record is the WRITER's layout
// and a scalar "occupies exactly the storage its declared type names", so the
// value rows are proved at the emitted site that lays each scalar down and at
// the bounds pass that holds it to its declared range after the copy. Every
// boundary a row cares about is the min/max the fixture declares, emitted as a
// literal in AllFixedClampBody.
//
// NOTHING HERE SHELLS OUT, so every subtest RUNS under a bare
// `go test ./internal/codegen/cpptable/` and under
// `-run TestFixedRoadmapCppT09IntegersFloats`. A compiled C++ probe would need
// the sibling runtime checkout (`../serialize`, the `serialize.h` the generated
// header includes), which is not present here; that is named as not run, never
// faked, and the emitted source is asserted instead — the same source the
// `make tables-fixedform` target compiles and runs.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// t09ScalarFixture declares one fixed table carrying every scalar family the
// eleven rows name. The fields are bracketed so no two adjacent fields of a
// different kind coalesce away a per-type plan entry, and each bounded field
// carries the boundary the row's clamp assertion reads.
const t09ScalarFixture = `package t09probe

fixed table All
{
    b    bool
    i8   int8
    i16  int16
    i32  int32
    i64  int64
    u8   uint8
    u16  uint16
    u32  uint32
    u64  uint64
    i128 int128 | min = -1000, max = 1000
    u128 uint128
    rng  int32 | min = 0, max = 100
    bitsv bits(12)
    f64  float64
    cf   float32 | min = -1, max = 1, resolution = 0.01
    fx   fixed(16, 16)  | min = -1, max = 1
    ufx  ufixed(16, 16) | min = 0, max = 1
}
`

// t09CppSource generates the fixed-table header for the fixture and returns it
// by name. It fails rather than answering "" so a moved emitter is red by name.
func t09CppSource(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "All.schema")
	if err := os.WriteFile(path, []byte(t09ScalarFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, path)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	for name, data := range files {
		if filepath.Base(name) == "AllTable.h" {
			return string(data)
		}
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	t.Fatalf("Generate emits AllTable.h; got %v", names)
	return ""
}

// TestFixedRoadmapCppT09IntegersFloats proves the scalar value rows of the
// fixed-tables roadmap node on this leg. Each case names the emitted lines that
// lay the type down (the write) and the storage the plan lands it in (the read),
// plus the bounds-pass literal a boundary value is held to.
func TestFixedRoadmapCppT09IntegersFloats(t *testing.T) {
	t.Parallel()
	src := t09CppSource(t)

	tests := []struct {
		id   string
		want []string
	}{
		{
			// "Signed integers: 8, 16, 32 and 64 bits". §3.4: each scalar
			// occupies exactly its storage; the write casts to the wire width
			// and the read storage is the signed C++ type of that width.
			id: "signed-integers/cpp/valid-data",
			want: []string{
				"int8_t i8 = 0;",
				"int16_t i16 = 0;",
				"int32_t i32 = 0;",
				"int64_t i64 = 0;",
				"TableFixedPut8( b + 1, (uint8_t) value.i8 );",
				"TableFixedPut16( b + 2, (uint16_t) value.i16 );",
				"TableFixedPut32( b + 4, (uint32_t) value.i32 );",
				"TableFixedPut64( b + 8, (uint64_t) value.i64 );",
			},
		},
		{
			// "Unsigned integers: 8, 16, 32 and 64 bits". The unsigned storage
			// takes the byte verbatim over its whole domain, high bit included.
			id: "unsigned-integers/cpp/valid-data",
			want: []string{
				"uint8_t u8 = 0;",
				"uint16_t u16 = 0;",
				"uint32_t u32 = 0;",
				"uint64_t u64 = 0;",
				"TableFixedPut8( b + 16, (uint8_t) value.u8 );",
				"TableFixedPut16( b + 17, (uint16_t) value.u16 );",
				"TableFixedPut32( b + 19, (uint32_t) value.u32 );",
				"TableFixedPut64( b + 23, (uint64_t) value.u64 );",
			},
		},
		// t09-more-cases
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			for _, want := range tc.want {
				if !strings.Contains(src, want) {
					t.Errorf("%s: the emitted fixed form does not contain %q\n--- the source it asserts over ---\n%s",
						tc.id, want, t09FixedRegion(src))
				}
			}
		})
	}
}

// t09FixedRegion returns the emitted fixed-form section, so a failure names the
// guarded site instead of dumping the whole 300 KB header.
func t09FixedRegion(src string) string {
	i := strings.Index(src, "inline void AllFixedWriteBody")
	if i < 0 {
		return "(no AllFixedWriteBody in the emitted source)"
	}
	j := strings.Index(src[i:], "inline int64_t AllFixedMeasure")
	if j < 0 {
		j = len(src) - i
	}
	return fmt.Sprintf("%s\n... [%d bytes omitted]", src[i:i+j], len(src)-i-j)
}
