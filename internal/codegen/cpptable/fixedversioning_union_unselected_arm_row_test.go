package cpptable

// The union_unselected_arm row (docs/FIXED-FORM-VERSIONING-TESTS.md, schema#1157,
// 2026-09-19, LAWFUL AS IS): after a read that RETURNS, an arm the landed tag did
// not select is UNDEFINED — one word, for every target (Glenn, 2026-09-19: "as
// designed it is 'undefined'"). A union read DEFINES the tag and the SELECTED arm
// and nothing else. THE ROW'S CONTENT IS THE ASSERTION IT REFUSES TO MAKE: it does
// not compare pick.beta or pick.gamma; a reader who "completes" it by asserting
// pick.gamma.p == 0 reversed a ruling and should read the issue first. That this
// leg OVERLAYS its arms — a real C union has no other arm to reset — is an
// OBSERVATION AND NOT A GUARANTEE, and nothing may be relied on it. Named *_arm_row_test.go, not *_arm_test.go: Go reads a trailing _arm
// before _test.go as a GOARCH build constraint and silently skips the file.

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

func TestFixedVersioningUnionUnselectedArm(t *testing.T) {
	corpus := cppFixedCorpus(t)
	file := filepath.Join(corpus, "old_union_append.bin")

	// THE SELECTED ARM AND THE TAG ARE THE WHOLE OF WHAT A UNION READ PROMISES.
	// Nothing here names beta or gamma.
	body := `
    if ( n != 1 ) { printf( "the file carries one record, not %lld\n", (long long) n ); return 1; }
    if ( back[0].pick.type != PickType::Alpha ) { printf( "pick.type is the writer's alpha arm, not %d\n", (int) back[0].pick.type ); return 1; }
    if ( back[0].pick.alpha.m != 7 ) { printf( "pick.alpha.m is the writer's 7, not %d\n", (int) back[0].pick.alpha.m ); return 1; }
    if ( back[0].seq != 15 ) { printf( "seq is the writer's 15, not %d\n", (int) back[0].seq ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.clamped != 0 || r.duplicate != 0 || r.retained != 0 || r.retain_lost != 0 )
        { printf( "a clean backward read moved a counter: unknown=%d kind_mismatch=%d widened=%d clamped=%d duplicate=%d retained=%d retain_lost=%d\n", r.unknown, r.kind_mismatch, r.widened, r.clamped, r.duplicate, r.retained, r.retain_lost ); return 1; }
    if ( r.malformed ) { printf( "a clean backward read is not malformed\n" ); return 1; }
    if ( r.refused ) { printf( "a clean backward read is not a refusal: reason=%d\n", (int) r.reason ); return 1; }
`

	// probe 1, the compiled column: reader VNEW, older VOLD. The poison is the
	// row's own: nothing here names beta or gamma.
	out, err := cppRunVersionProbe(t, "union_append", file, body, "")
	if err != nil {
		t.Fatalf("union_unselected_arm, the compiled column: %v\n%s", err, out)
	}

	// probe 2, the identity column: reader VOLD, no older — its own hash, its
	// own plan, the same file.
	{
		dir := t.TempDir()
		oldData, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", "VOLD_union_append.schema"))
		if err != nil {
			t.Fatal(err)
		}
		oldPath := filepath.Join(dir, "VOLD_union_append.schema")
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
`, header, pkg, file, root, "", root, root, body)

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
			t.Fatalf("union_unselected_arm, the identity column: the probe did not compile: %v\n%s", err, out)
		}
		out, err := exec.Command(filepath.Join(outDir, "probe")).CombinedOutput()
		if err != nil {
			t.Fatalf("union_unselected_arm, the identity column: %v\n%s", err, out)
		}
	}
}
