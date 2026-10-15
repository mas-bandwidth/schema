package cpptable

// fixedroadmap_t18 — cpp/R16 "the counters exactly" (docs/FIXED-FORM-ALGORITHM.md
// §5.4, docs/roadmap.sexp node `fixed-tables`). One fixture per row of the §5.4
// table, each WRITTEN and READ by this leg's own C++ toolchain — the OLD
// writer's `...FixedSave` writes the records, the NEW reader's `...FixedLoad`
// reads them through the lineage, and the report's exact counters are asserted.
// This is the same generate-compile-run unit cppRunVersionProbe uses, but the
// bytes are written here (never read from build/fixedform-corpus), so the row
// runs wherever g++ does.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// cppT18Pair is the OLD and NEW generation of one row, generated side by side.
type cppT18Pair struct {
	outDir               string
	oldHeader, newHeader string
	oldPkg, newPkg       string
	root                 string
}

// cppT18Generate stages VOLD_/VNEW_<row>.schema in one temp dir, generates the
// OLD unit (the writer) and the NEW unit (the reader, whose lineage is the OLD
// peer via the nil-lock sibling convention), and answers the two headers and
// their namespaces.
func cppT18Generate(t *testing.T, row string) cppT18Pair {
	t.Helper()
	dir := t.TempDir()
	oldData, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", "VOLD_"+row+".schema"))
	if err != nil {
		t.Fatal(err)
	}
	newData, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", "VNEW_"+row+".schema"))
	if err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(dir, "VOLD_"+row+".schema")
	newPath := filepath.Join(dir, "VNEW_"+row+".schema")
	if err := os.WriteFile(oldPath, oldData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, newData, 0o644); err != nil {
		t.Fatal(err)
	}

	oldUnit := loadUnit(t, oldPath)
	newUnit := loadUnit(t, newPath)

	oldFiles, err := cpp.Generate(oldUnit)
	if err != nil {
		t.Fatal(err)
	}
	oldTables, err := GenerateLineage(oldUnit, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(oldFiles, oldTables)

	newFiles, err := cpp.Generate(newUnit)
	if err != nil {
		t.Fatal(err)
	}
	newTables, err := GenerateLineage(newUnit, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(newFiles, newTables)

	outDir := t.TempDir()
	var pair cppT18Pair
	pair.outDir = outDir
	for name, data := range oldFiles {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(outDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			pair.oldHeader = name
		}
	}
	for name, data := range newFiles {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(outDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			pair.newHeader = name
		}
	}
	if pair.oldHeader == "" || pair.newHeader == "" || pair.oldHeader == pair.newHeader {
		t.Fatalf("t18: the two generations did not yield two distinct Table.h headers (old=%q new=%q)", pair.oldHeader, pair.newHeader)
	}
	pair.oldPkg = oldUnit.Package
	pair.newPkg = newUnit.Package
	pair.root = cppFixedRootName(t, newUnit)
	return pair
}

// cppT18Run compiles and runs one probe against the pair. The probe includes
// both headers and opens the OLD namespace, so the OLD writer, OLD reader and
// OLD report/plan types are unqualified, while the NEW reader's types are
// `%[2]s::`-qualified.
func cppT18Run(t *testing.T, pair cppT18Pair, body string) string {
	t.Helper()
	dir := t.TempDir()
	probe := fmt.Sprintf(`#include <cstdio>
#include <cstring>
#include "%s"
#include "%s"

using namespace %s;

int main( void )
{
%s
    return 0;
}
`, pair.oldHeader, pair.newHeader, pair.oldPkg, body)
	if err := os.WriteFile(filepath.Join(dir, "probe.cpp"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	serialize, err := filepath.Abs("../../../../serialize")
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c++17", "-O1", "-I" + dir, "-I" + pair.outDir, "-I" + serialize, filepath.Join(dir, "probe.cpp"), "-o", filepath.Join(dir, "probe")}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("t18: the probe did not compile: %v\n%s", err, out)
	}
	out, err := exec.Command(filepath.Join(dir, "probe")).CombinedOutput()
	if err != nil {
		t.Fatalf("t18: the probe failed: %v\n%s", err, out)
	}
	return string(out)
}

// t18Read is the NEW reader's compiled-plan read, qualified into the NEW
// namespace: `%[1]s` is the shared root name, `%[2]s` the NEW package.
const t18Read = `
        %[2]s::%[1]s back[%[3]d];
        static %[2]s::TableFixedEntry plan[4096];
        %[2]s::TableReport r; memset( &r, 0, sizeof( r ) );
        const int64_t n = %[2]s::%[1]sFixedLoad( back, %[3]d, buffer, written, plan, 4096, NULL, &r );
`

// TestFixedRoadmapCppT18ExactCounters is cpp/R16: the §5.4 counters, exactly.
func TestFixedRoadmapCppT18ExactCounters(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the c++ compiler (the leg's own toolchain)")
	t.Run("cpp/R16", func(t *testing.T) {
		t.Parallel()

		// widened: int16 -> int32, one widen entry per record. N=4 -> widened=4.
		pair := cppT18Generate(t, "int_widen")
		cppT18Run(t, pair, fmt.Sprintf(`
    {
        %[1]s want[4];
        for ( int k = 0; k < 4; ++k ) { want[k].lead = 1u; want[k].v = (int16_t) ( k + 1 ); want[k].trail = 2u; }
        static uint8_t buffer[1 << 16];
        const int64_t written = %[1]sFixedSave( want, 4, buffer, (int64_t) sizeof( buffer ) );
        if ( written < 0 ) { printf( "widen: save failed\n" ); return 1; }
`+t18Read+`
        if ( n != 4 ) { printf( "widen: n=%%lld\n", (long long) n ); return 1; }
        if ( r.widened != 4 ) { printf( "widen: widened=%%d want 4\n", r.widened ); return 1; }
        if ( r.unknown != 0 || r.kind_mismatch != 0 || r.clamped != 0 ) { printf( "widen: a counter moved\n" ); return 1; }
    }
`, pair.root, pair.newPkg, 4))

		// folded widened element run: [..4]int16 -> [..4]int32, one widen entry per
		// element, never folded: min(their_n, my_n) = 4 per record.
		pair = cppT18Generate(t, "array_elem_widen")
		cppT18Run(t, pair, fmt.Sprintf(`
    {
        %[1]s want;
        want.lead = 1u; want.trail = 2u; want.vals_count = 4;
        for ( int k = 0; k < 4; ++k ) { want.vals[k] = (int16_t) ( 100 + k ); }
        static uint8_t buffer[1 << 16];
        const int64_t written = %[1]sFixedSave( &want, 1, buffer, (int64_t) sizeof( buffer ) );
        if ( written < 0 ) { printf( "elem-widen: save failed\n" ); return 1; }
`+t18Read+`
        if ( n != 1 ) { printf( "elem-widen: n=%%lld\n", (long long) n ); return 1; }
        if ( r.widened != 4 ) { printf( "elem-widen: widened=%%d want 4\n", r.widened ); return 1; }
        if ( r.unknown != 0 || r.kind_mismatch != 0 || r.clamped != 0 ) { printf( "elem-widen: a counter moved\n" ); return 1; }
    }
`, pair.root, pair.newPkg, 1))

		// clamped count: [..4]int32 -> [..8]int32, the count forged to 7 (past the
		// writer's 4). One clamp per record; N=3 -> clamped=3.
		pair = cppT18Generate(t, "array_bounded_grow")
		cppT18Run(t, pair, fmt.Sprintf(`
    {
        %[1]s want[3];
        for ( int k = 0; k < 3; ++k )
        {
            want[k].lead = 1u; want[k].trail = 2u; want[k].vals_count = 4;
            for ( int j = 0; j < 4; ++j ) { want[k].vals[j] = 1000 + j; }
        }
        static uint8_t buffer[1 << 16];
        const int64_t written = %[1]sFixedSave( want, 3, buffer, (int64_t) sizeof( buffer ) );
        if ( written < 0 ) { printf( "count: save failed\n" ); return 1; }
        {
            static const uint8_t needle[8] = { 0x01, 0x00, 0x00, 0x00, 0x04, 0x00, 0x00, 0x00 };
            int found = 0;
            for ( int64_t i = 0; i + 8 <= written; ++i )
                if ( memcmp( buffer + i, needle, 8 ) == 0 ) { TableFixedPut32( buffer + i + 4, 7 ); found++; }
            if ( found != 3 ) { printf( "count: locator matched %%d times, want 3\n", found ); return 1; }
        }
`+t18Read+`
        if ( n != 3 ) { printf( "count: n=%%lld\n", (long long) n ); return 1; }
        if ( r.clamped != 3 ) { printf( "count: clamped=%%d want 3\n", r.clamped ); return 1; }
        if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 ) { printf( "count: a counter moved\n" ); return 1; }
    }
`, pair.root, pair.newPkg, 3))

		// forged ordinal past the writer's variant count: tier=Gold(3) forged to 4,
		// one past the writer's three variants. clamped == 1 on the COMPILED plan
		// (NEW reader) exactly as on the IDENTITY plan (OLD reader), never 2.
		pair = cppT18Generate(t, "enum_append")
		cppT18Run(t, pair, fmt.Sprintf(`
    {
        %[1]s want;
        want.tier = Tier::Gold; want.seq = 9;
        static uint8_t buffer[1 << 16];
        const int64_t written = %[1]sFixedSave( &want, 1, buffer, (int64_t) sizeof( buffer ) );
        if ( written < 0 ) { printf( "ordinal: save failed\n" ); return 1; }
        {
            static const uint8_t needle[5] = { 0x03, 0x09, 0x00, 0x00, 0x00 };
            int found = 0;
            for ( int64_t i = 0; i + 5 <= written; ++i )
                if ( memcmp( buffer + i, needle, 5 ) == 0 ) { buffer[i] = 4; found++; }
            if ( found != 1 ) { printf( "ordinal: locator matched %%d times, want 1\n", found ); return 1; }
        }
`+t18Read+`
        if ( n != 1 ) { printf( "ordinal compiled: n=%%lld\n", (long long) n ); return 1; }
        if ( back[0].tier != %[2]s::Tier::None ) { printf( "ordinal compiled: lands %%d, not None\n", (int) back[0].tier ); return 1; }
        if ( r.clamped != 1 ) { printf( "ordinal compiled: clamped=%%d want 1\n", r.clamped ); return 1; }
        {
            %[1]s back0;
            static TableFixedEntry plan[4096];
            TableReport rr; memset( &rr, 0, sizeof( rr ) );
            const int64_t m = %[1]sFixedLoad( &back0, 1, buffer, written, plan, 4096, NULL, &rr );
            if ( m != 1 ) { printf( "ordinal identity: n=%%lld\n", (long long) m ); return 1; }
            if ( back0.tier != Tier::None ) { printf( "ordinal identity: lands %%d, not None\n", (int) back0.tier ); return 1; }
            if ( rr.clamped != 1 ) { printf( "ordinal identity: clamped=%%d want 1\n", rr.clamped ); return 1; }
        }
    }
`, pair.root, pair.newPkg, 1))

		// clean appended field: every counter 0, the reader's tail is its default 77.
		pair = cppT18Generate(t, "field_append")
		cppT18Run(t, pair, fmt.Sprintf(`
    {
        %[1]s want;
        want.x = 11; want.y = 22; want.z = 33;
        static uint8_t buffer[1 << 16];
        const int64_t written = %[1]sFixedSave( &want, 1, buffer, (int64_t) sizeof( buffer ) );
        if ( written < 0 ) { printf( "append: save failed\n" ); return 1; }
`+t18Read+`
        if ( n != 1 ) { printf( "append: n=%%lld\n", (long long) n ); return 1; }
        if ( back[0].x != 11 || back[0].y != 22 || back[0].z != 33 || back[0].w != 77 ) { printf( "append: values did not land\n" ); return 1; }
        if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.clamped != 0 ) { printf( "append: a counter moved\n" ); return 1; }
    }
`, pair.root, pair.newPkg, 1))

		// unknown: Item.drop removed, read across the four-element array of tables.
		// Once per field per peer, never per record nor per element: unknown == 1.
		pair = cppT18Generate(t, "unknown_census")
		cppT18Run(t, pair, fmt.Sprintf(`
    {
        %[1]s want;
        want.lead = 1u; want.trail = 2u;
        for ( int k = 0; k < 4; ++k ) { want.items[k].a = 10 + k; want.items[k].drop = 50 + k; }
        static uint8_t buffer[1 << 16];
        const int64_t written = %[1]sFixedSave( &want, 1, buffer, (int64_t) sizeof( buffer ) );
        if ( written < 0 ) { printf( "unknown: save failed\n" ); return 1; }
`+t18Read+`
        if ( n != 1 ) { printf( "unknown: n=%%lld\n", (long long) n ); return 1; }
        if ( r.unknown != 1 ) { printf( "unknown: unknown=%%d want 1\n", r.unknown ); return 1; }
        if ( r.kind_mismatch != 0 || r.widened != 0 || r.clamped != 0 ) { printf( "unknown: a counter moved\n" ); return 1; }
    }
`, pair.root, pair.newPkg, 1))
	})
}
