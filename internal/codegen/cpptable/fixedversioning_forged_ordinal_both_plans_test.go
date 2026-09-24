package cpptable

// TestFixedVersioningForgedOrdinalBothPlans is §5.8 row 12: VOLD_/VNEW_enum_append
// — Tier { Bronze, Silver, Gold } → + Platinum — plus old_enum_append.bin with
// r0.tier forged to 4, an ordinal past the writer's three variants. The SAME
// bytes are read TWICE: the NEW build (compiled plan) and the OLD build
// (identity plan). The counter asserted EXACTLY is `clamped == 1` on BOTH — the
// bounds pass's count, once per field — and never `>= 1`, because a leg that
// also counts in the `ordinal` op lands 2 and passes a `>=` check.
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

func TestFixedVersioningForgedOrdinalBothPlans(t *testing.T) {
	corpus := cppFixedCorpus(t)
	file := filepath.Join(corpus, "old_enum_append.bin")

	// THE FORGE: locate r0.tier by its declared value 3 (Gold) and its neighbour
	// r0.seq = 9 — the manifest's lawful writer values, `03 09 00 00 00` — assert
	// the needle occurs EXACTLY once, and overwrite the tier byte with 4.
	forge := `
    {
        static const uint8_t needle[5] = { 0x03, 0x09, 0x00, 0x00, 0x00 };
        int64_t found = -1;
        for ( int64_t i = 0; i + 5 <= len; ++i )
        {
            if ( memcmp( data + i, needle, 5 ) == 0 )
            {
                if ( found >= 0 ) { printf( "the locator (tier=Gold, seq=9) matched twice\n" ); return 1; }
                found = i;
            }
        }
        if ( found < 0 ) { printf( "the locator (tier=Gold, seq=9) matched nowhere\n" ); return 1; }
        data[found] = 4; /* the forge */
    }
`

	body := `
    if ( n != 1 ) { printf( "the forged file reads one record, not %lld\n", (long long) n ); return 1; }
    if ( r.refused || r.malformed || r.reason != newer_form )
        { printf( "the forged read is not a refusal: refused=%d malformed=%d reason=%d\n", (int) r.refused, (int) r.malformed, (int) r.reason ); return 1; }
    if ( back[0].tier != Tier::None ) { printf( "the forged ordinal lands None, not %d\n", (int) back[0].tier ); return 1; }
    if ( back[0].seq != 9 ) { printf( "the scalar after the enum must stand at 9, not %d\n", (int) back[0].seq ); return 1; }
    if ( r.clamped != 1 ) { printf( "clamped is the bounds pass's count, once per field: %d\n", r.clamped ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "only clamped may move on a forged ordinal: unknown=%d kind_mismatch=%d widened=%d duplicate=%d\n", r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
`

	// THE COMPILED PLAN: the NEW build reads the forged OLD file through the
	// lineage, so the plan is compiled from the OLD layout (three variants).
	out, err := cppRunVersionProbe(t, "enum_append", file, body, forge)
	if err != nil {
		t.Fatalf("forged_ordinal_both_plans, the compiled plan: %v\n%s", err, out)
	}

	// THE IDENTITY PLAN: the OLD build reads its own file on its own hash. The
	// harness's cppRunVersionProbe hardcodes the NEW build (loadUnit(newPath)),
	// so the OLD build is generated here — the same probe, the same template,
	// only the loaded schema differs.
	{
		dir := t.TempDir()
		oldData, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", "VOLD_enum_append.schema"))
		if err != nil {
			t.Fatal(err)
		}
		oldPath := filepath.Join(dir, "VOLD_enum_append.schema")
		if err := os.WriteFile(oldPath, oldData, 0o644); err != nil {
			t.Fatal(err)
		}

		uOld := loadUnit(t, oldPath)
		oldFiles, err := cpp.Generate(uOld)
		if err != nil {
			t.Fatal(err)
		}
		oldTables, err := GenerateLineage(uOld, nil)
		if err != nil {
			t.Fatal(err)
		}
		maps.Copy(oldFiles, oldTables)

		outDir := t.TempDir()
		var header string
		for name, data := range oldFiles {
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

		root := cppFixedRootName(t, uOld)
		pkg := uOld.Package

		probe := fmt.Sprintf(`#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "%s"

using namespace %s;

int main( void )
{
    static uint8_t data[1 << 20];
    int64_t len;
    FILE * f = fopen( %q, "rb" );
    %s back[8];
    TableReport r;
    static TableFixedEntry plan[4096];
    int64_t n;
    if ( f == NULL ) { printf( "the corpus file is missing\n" ); return 1; }
    len = (int64_t) fread( data, 1, sizeof( data ), f );
    fclose( f );
    if ( len < 20 ) { printf( "the corpus file is short\n" ); return 1; }
%s
    memset( back, 0, sizeof( back ) );
    for ( int k = 0; k < 8; ++k ) { %sReset( back[k] ); }
    memset( &r, 0, sizeof( r ) );
    n = %sFixedLoad( back, 8, data, len, plan, 4096, NULL, &r );
%s
    return 0;
}
`, header, pkg, file, root, forge, root, root, body)

		if err := os.WriteFile(filepath.Join(outDir, "probe.cpp"), []byte(probe), 0o600); err != nil {
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
		args := []string{"-std=c++17", "-O1", "-I" + outDir, "-I" + serialize, filepath.Join(outDir, "probe.cpp"), "-o", filepath.Join(outDir, "probe")}
		build := exec.Command(cxx, args...)
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("forged_ordinal_both_plans, the identity plan: the probe did not compile: %v\n%s", err, out)
		}
		out, err := exec.Command(filepath.Join(outDir, "probe")).CombinedOutput()
		if err != nil {
			t.Fatalf("forged_ordinal_both_plans, the identity plan: %v\n%s", err, out)
		}
	}
}
