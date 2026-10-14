package cpptable

// TestFixedRoadmapCppT10FloatsComposites proves the two capability tasks of the
// roadmap node `fixed-tables` this card carries, in roadmap order:
//
//   - `enum-values/cpp/valid-data` — "Enums and None: valid-data write/read
//     acceptance" (docs/roadmap.sexp:5480), contract "§3.4 record, enum".
//   - `nested-keyed-arrays/cpp/valid-data` — "Nested enum-keyed arrays:
//     valid-data write/read acceptance" (docs/roadmap.sexp:6453), contract
//     "ALGORITHM §7 keyed corpus".
//
// A capability is met when the leg WRITES and READS the construct with
// NON-DEFAULT and BOUNDARY values, asserted, in a target a gate runs: this
// file generates the leg's own C++ fixed-form header for each task, compiles a
// probe against it, and runs the probe, which writes with the generated
// `...FixedSave` and reads with the generated `...FixedLoad`.
//
// THE PAGE SENTENCES EACH ASSERTION IMPLEMENTS:
//
//   - docs/SPEC-TABLES.md §3.4: "A FIXED-TABLE RECORD IS AN EIGHT-BYTE HASH OF
//     THE WRITER'S LAYOUT, AND THEN THE VALUES IN DECLARED ORDER, EVERY FIELD
//     AT ITS BOUND."
//   - docs/FIXED-FORM-ALGORITHM.md:184: "an ENUM | the ordinal width `1/2/4/8`
//     ... the ordinal is the variant's POSITION FROM `1`; `0` is `None`".
//   - docs/FIXED-FORM-ALGORITHM.md:191: "`[Enum]T` | `slots * C(T)` | every
//     slot live, in the key enum's declared order; no key rides".
//   - docs/SPEC-TABLES.md §2.4: "An array bound that NAMES A DECLARED ENUM is
//     an enum-keyed array." and the key "takes no room" for `None`.
//
// The reader's own default is the nested element's declared default (`Qty.n =
// 7`), which is the value the wire must NOT carry for an unwritten slot: the
// slots ARE live, so record 0 below is all defaults and every slot still lands
// its declared 7 rather than zero.
//
// NO SIBLING RUNTIME IS NEEDED: a fixed-table-only unit's `<Base>Table.h`
// includes only its own dependency-free data header, so the probe compiles with
// the C++ compiler alone.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
)

// cppRunCapability generates the leg's fixed-form headers for `schema`, writes
// a probe that includes them and runs `body` (C++ that returns nonzero on a
// failed assertion), compiles the probe and runs it. It is the tests'
// generate-and-run-unit helper; a fixed-table-only unit needs no sibling
// runtime, so the only include path is the generated directory.
func cppRunCapability(t *testing.T, schema, pkg, body string) ([]byte, error) {
	t.Helper()
	return cppRunUnit(t, schema, pkg, body, nil)
}

// cppRunUnit is cppRunCapability with EXTRA include directories: a unit whose
// generated header names serialize.h's 128-bit storage type (cpptable.go's
// unitHas128) takes the sibling serialize checkout's include path, the same
// directory cppRunVersionProbe passes. Every other step is identical.
func cppRunUnit(t *testing.T, schema, pkg, body string, includes []string) ([]byte, error) {
	t.Helper()
	dir := t.TempDir()
	base := filepath.Base(schema)
	data, err := os.ReadFile(schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, base), data, 0o644); err != nil {
		t.Fatal(err)
	}
	// THE NIL-LOCK SIBLING CONVENTION reads a VOLD_ peer off the sibling path
	// (docs/FIXED-FORM-ALGORITHM.md §5.8 row 1's interim), so a VNEW_ side is
	// copied beside its peer exactly as the driver merges the two.
	if after, ok := strings.CutPrefix(base, "VNEW_"); ok {
		peer := filepath.Join(filepath.Dir(schema), "VOLD_"+after)
		if pd, err := os.ReadFile(peer); err == nil {
			if err := os.WriteFile(filepath.Join(dir, filepath.Base(peer)), pd, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	u := loadUnit(t, filepath.Join(dir, base))
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)

	outDir := t.TempDir()
	var header string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(outDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
	}
	if header == "" {
		t.Fatal("no Table.h was generated")
	}

	probe := fmt.Sprintf(`#include <cstdio>
#include <cstring>
#include "%s"

using namespace %s;

int main( void )
{
%s
    return 0;
}
`, header, pkg, body)
	if err := os.WriteFile(filepath.Join(outDir, "probe.cpp"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	args := []string{"-std=c++17", "-O1", "-I" + outDir}
	args = append(args, includeArgs(includes)...)
	args = append(args, filepath.Join(outDir, "probe.cpp"), "-o", filepath.Join(outDir, "probe"))
	build := exec.Command(cxx, args...)
	if out, err := build.CombinedOutput(); err != nil {
		return out, fmt.Errorf("the probe did not compile: %w", err)
	}
	return exec.Command(filepath.Join(outDir, "probe")).CombinedOutput()
}

// includeArgs is one `-I<dir>` per extra include directory, in the order given.
func includeArgs(dirs []string) []string {
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, "-I"+d)
	}
	return out
}

// THE ENUM ROW'S PROBE: docs/roadmap.sexp:5480. Four records carry the enum's
// boundary (`None`, the zero that names no variant) and its first and last
// named variants (`Bronze`, `Platinum`), with the outer `seq` at the int32
// ends; the wire is checked for the ordinal the page names (§184) and the
// read-back is checked value for value.
const cppEnumValuesBody = `
    Lineage want[4];
    want[0].tier = Tier::None;     want[0].seq = 0;
    want[1].tier = Tier::Bronze;   want[1].seq = -2147483647 - 1;
    want[2].tier = Tier::Gold;     want[2].seq = 2147483647;
    want[3].tier = Tier::Platinum; want[3].seq = -7;

    static uint8_t buffer[1 << 16];
    int64_t written = LineageFixedSave( want, 4, buffer, (int64_t) sizeof( buffer ) );
    if ( written != LineageFixedMeasure( 4 ) ) { printf( "enum: save wrote %lld for 4 records\n", (long long) written ); return 1; }

    {
        const int64_t body0 = kTableFixedHeaderBytes + 4 + LineageFixedLayoutBytes + 8;
        const uint8_t ord[4] = { 0, 1, 3, 4 };
        for ( int64_t k = 0; k < 4; ++k )
            if ( buffer[body0 + k * LineageFixedRecordBytes] != ord[k] )
                { printf( "enum: record %lld rode ordinal %u, not %u\n", (long long) k, buffer[body0 + k * LineageFixedRecordBytes], ord[k] ); return 1; }
    }

    static TableFixedEntry plan[4096];
    TableReport r; memset( &r, 0, sizeof( r ) );
    Lineage got[4];
    int64_t n = LineageFixedLoad( got, 4, buffer, written, plan, 4096, NULL, &r );
    if ( n != 4 ) { printf( "enum: load n=%lld malformed=%d refused=%d\n", (long long) n, (int) r.malformed, (int) r.refused ); return 1; }
    if ( r.malformed || r.refused || r.unknown != 0 || r.kind_mismatch != 0 || r.clamped != 0 || r.widened != 0 )
        { printf( "enum: a clean round-trip is not a refusal and moves no counter: malformed=%d refused=%d u=%d km=%d c=%d w=%d\n", (int) r.malformed, (int) r.refused, r.unknown, r.kind_mismatch, r.clamped, r.widened ); return 1; }
    for ( int64_t k = 0; k < 4; ++k )
        if ( got[k].tier != want[k].tier || got[k].seq != want[k].seq )
            { printf( "enum: record %lld lands tier=%d seq=%d, wrote tier=%d seq=%d\n", (long long) k, (int) got[k].tier, got[k].seq, (int) want[k].tier, want[k].seq ); return 1; }
`

// THE NESTED-KEYED-ARRAY ROW'S PROBE: docs/roadmap.sexp:6453. Three records
// carry the bracket of the keyed array — all slots at the nested element's
// declared default (`Qty.n = 7`), the slot ends together with the outer int32
// ends, and four distinct non-default values — writing and reading every live
// slot with no key on the wire (§191).
const cppNestedKeyedBody = `
    Lineage want[3];
    want[0].seq = 0;
    want[1].slots[Tier::Bronze].n = -2147483647 - 1;
    want[1].slots[Tier::Silver].n = -1;
    want[1].slots[Tier::Gold].n = 0;
    want[1].slots[Tier::Platinum].n = 2147483647;
    want[1].seq = 2147483647;
    want[2].slots[Tier::Bronze].n = 100;
    want[2].slots[Tier::Silver].n = 200;
    want[2].slots[Tier::Gold].n = 300;
    want[2].slots[Tier::Platinum].n = 400;
    want[2].seq = -5;

    static uint8_t buffer[1 << 16];
    int64_t written = LineageFixedSave( want, 3, buffer, (int64_t) sizeof( buffer ) );
    if ( written != LineageFixedMeasure( 3 ) ) { printf( "keyed: save wrote %lld for 3 records\n", (long long) written ); return 1; }

    {
        const int64_t r2 = kTableFixedHeaderBytes + 4 + LineageFixedLayoutBytes + 2 * LineageFixedRecordBytes + 8;
        const int32_t slots[4] = { 100, 200, 300, 400 };
        for ( int64_t i = 0; i < 4; ++i )
            if ( (int32_t) TableFixedGet32( buffer + r2 + i * 4 ) != slots[i] )
                { printf( "keyed: slot %lld rode %d, not %d\n", (long long) i, (int32_t) TableFixedGet32( buffer + r2 + i * 4 ), slots[i] ); return 1; }
        if ( (int32_t) TableFixedGet32( buffer + r2 + 16 ) != -5 )
            { printf( "keyed: the outer seq rode %d, not -5\n", (int32_t) TableFixedGet32( buffer + r2 + 16 ) ); return 1; }
    }

    static TableFixedEntry plan[4096];
    TableReport r; memset( &r, 0, sizeof( r ) );
    Lineage got[3];
    int64_t n = LineageFixedLoad( got, 3, buffer, written, plan, 4096, NULL, &r );
    if ( n != 3 ) { printf( "keyed: load n=%lld malformed=%d refused=%d\n", (long long) n, (int) r.malformed, (int) r.refused ); return 1; }
    if ( r.malformed || r.refused || r.unknown != 0 || r.kind_mismatch != 0 || r.clamped != 0 || r.widened != 0 )
        { printf( "keyed: a clean round-trip is not a refusal and moves no counter: malformed=%d refused=%d u=%d km=%d c=%d w=%d\n", (int) r.malformed, (int) r.refused, r.unknown, r.kind_mismatch, r.clamped, r.widened ); return 1; }
    for ( int64_t k = 0; k < 3; ++k )
    {
        if ( got[k].seq != want[k].seq ) { printf( "keyed: record %lld lands seq=%d, wrote %d\n", (long long) k, got[k].seq, want[k].seq ); return 1; }
        if ( got[k].slots[Tier::Bronze].n != want[k].slots[Tier::Bronze].n || got[k].slots[Tier::Silver].n != want[k].slots[Tier::Silver].n ||
             got[k].slots[Tier::Gold].n != want[k].slots[Tier::Gold].n || got[k].slots[Tier::Platinum].n != want[k].slots[Tier::Platinum].n )
            { printf( "keyed: record %lld slot values do not round-trip: [%d %d %d %d] vs [%d %d %d %d]\n", (long long) k,
                got[k].slots[Tier::Bronze].n, got[k].slots[Tier::Silver].n, got[k].slots[Tier::Gold].n, got[k].slots[Tier::Platinum].n,
                want[k].slots[Tier::Bronze].n, want[k].slots[Tier::Silver].n, want[k].slots[Tier::Gold].n, want[k].slots[Tier::Platinum].n ); return 1; }
    }
`

// TestFixedRoadmapCppT10FloatsComposites is table-driven, one subtest per task
// id, and every subtest opens with t.Parallel().
func TestFixedRoadmapCppT10FloatsComposites(t *testing.T) {
	t.Parallel()
	table := []struct {
		id     string
		schema string
		pkg    string
		body   string
	}{
		{
			id:     "enum-values/cpp/valid-data",
			schema: filepath.Join("..", "..", "..", "test", "tables", "VNEW_enum_append.schema"),
			pkg:    "vnew_enum_append",
			body:   cppEnumValuesBody,
		},
		{
			id:     "nested-keyed-arrays/cpp/valid-data",
			schema: filepath.Join("..", "..", "..", "test", "tables", "VNEW_keyed_array_enum_append.schema"),
			pkg:    "vnew_keyed_array_enum_append",
			body:   cppNestedKeyedBody,
		},
	}
	for _, c := range table {
		t.Run(c.id, func(t *testing.T) {
			t.Parallel()
			out, err := cppRunCapability(t, c.schema, c.pkg, c.body)
			if err != nil {
				t.Fatalf("%s: %v\n%s", c.id, err, out)
			}
		})
	}
}
