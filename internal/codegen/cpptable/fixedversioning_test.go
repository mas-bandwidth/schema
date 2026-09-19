package cpptable

// THE VERSIONING HARNESS ON THE CPP LEG (docs/FIXED-FORM-VERSIONING-TESTS.md,
// the procedures in docs/FIXED-FORM-ALGORITHM.md §5). ctable and gotable each
// carry one, generate source, compile it and run a probe against the corpus;
// cpptable has none — this file is the place, proven by exactly one row, §5.8
// row 4 `writer_bound_count`.
//
// THE LINEAGE COMES FROM THE SIBLING-FILENAME CONVENTION, not handed-in
// entries: GenerateLineage takes a nil lock and reads the VOLD_ peer off the
// sibling path — §5.8 row 1's INTERIM. A row is probed by copying
// VOLD_/VNEW_<row>.schema into one temp dir and loading the NEW one.
//
// The bytes are the C++ reference's corpus (build/fixedform-corpus), the same
// byte oracle the C leg reads.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- the harness ------------------------------------------------------------

func cppFixedCorpus(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("../../../build/fixedform-corpus")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old_array_bounded_grow.bin")); err != nil {
		// THE SKIP IS A FAILURE WHEN SOMETHING PROMISED THE CORPUS. A bare
		// `go test ./...` on a tree that never built the oracle has nothing to
		// read and says so; under SCHEMA_REQUIRE_CORPUS a missing file means the
		// build did not do what it was told — and a suite that skips itself there
		// would report green over the row having never run.
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and the reference's byte oracle is not in %s: %v", dir, err)
		}
		t.Skip("the reference's byte oracle is not built: make tables-fixedform-corpus")
	}
	return dir
}

// cppFixedRootName is §5.9 #9's answer for a corpus row, copied from the C
// leg's cFixedRootName (a different package, so it cannot be called): the
// file's root is the OUTER table, the fixed table no other fixed table of the
// unit names by value.
func cppFixedRootName(t *testing.T, u *ir.Unit) string {
	t.Helper()
	roots := ir.TableFixedRoots(u)
	if len(roots) == 0 {
		t.Fatal("a versioning row declares a fixed root")
	}
	named := map[string]bool{}
	for _, st := range roots {
		for _, f := range st.Fields {
			named[f.Type.Name] = true
		}
	}
	for _, st := range roots {
		if !named[st.Name] {
			return st.Name
		}
	}
	return roots[len(roots)-1].Name
}

// cppRunVersionProbe generates the READER's unit — the VNEW_ file copied
// beside its VOLD_ peer, which is how a nil lock finds the lineage — compiles
// a probe and runs it against `file`. `forge` is C++ that damages the bytes
// before the load (the forged count word), and `body` is the assertions after
// it. The type-wire headers come from cpp.Generate and the table headers from
// GenerateLineage with a nil lock, exactly the way the compiler driver merges
// the two.
func cppRunVersionProbe(t *testing.T, row, file, body, forge string) ([]byte, error) {
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

	u := loadUnit(t, newPath)

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

	// THE INTERIM MUST HAVE FIRED before the probe is trusted: the header has
	// to carry a FixedLineage[] with more than the identity — the VOLD_ peer
	// picked up by the sibling-filename convention. Identity-only means the
	// convention missed the peer and the row cannot be probed; inventing a lock
	// to work around it is not allowed.
	text := string(files[header])
	if !strings.Contains(text, "FixedLineage[]") {
		t.Fatalf("the generated header carries no FixedLineage[]; the nil-lock convention did not fire:\n%s", text)
	}
	if n := strings.Count(text, "ull, //"); n <= 1 {
		t.Fatalf("the nil-lock convention picked up no VOLD_ peer (identity only, %d note); the row cannot be probed:\n%s", n, text)
	}

	root := cppFixedRootName(t, u)
	pkg := u.Package

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
		return out, fmt.Errorf("the probe did not compile: %w", err)
	}
	return exec.Command(filepath.Join(outDir, "probe")).CombinedOutput()
}

// ---- the row ----------------------------------------------------------------

// TestFixedVersioningWriterBoundCount is §5.8 row 4: VOLD_/VNEW_
// array_bounded_grow — vals [..4]int32 on the OLD side, [..8]int32 on the NEW
// — plus old_array_bounded_grow.bin with the count word forged to 7, BETWEEN
// the writer's 4 and the reader's 8. The NEW build reads the forged OLD file
// through its lineage, so the plan is COMPILED and the count's bound is the
// WRITER's (4). `clamped` is `== 1` and never `>= 1` (§5.4).
func TestFixedVersioningWriterBoundCount(t *testing.T) {
	corpus := cppFixedCorpus(t)
	body := `
    if ( n != 1 ) { printf( "n is not 1: %lld\n", (long long) n ); return 1; }
    if ( back[0].vals_count != 4 ) { printf( "vals_count is not the writer's 4: %d\n", back[0].vals_count ); return 1; }
    if ( back[0].lead != 0xAAAAAAAAu || back[0].trail != 0xBBBBBBBBu )
        { printf( "a bracket moved: lead=%u trail=%u\n", back[0].lead, back[0].trail ); return 1; }
    {
        const int32_t want[4] = { 1000, 1001, 1002, 1003 };
        int k;
        for ( k = 0; k < 4; ++k )
            if ( back[0].vals[k] != want[k] ) { printf( "vals[%d] is not the old writer's %d: %d\n", k, want[k], back[0].vals[k] ); return 1; }
        for ( k = 4; k < 8; ++k )
            if ( back[0].vals[k] != 0 ) { printf( "vals[%d] is not the reader's declared default 0: %d\n", k, back[0].vals[k] ); return 1; }
    }
    if ( r.clamped != 1 ) { printf( "clamped is not exactly 1: %d\n", r.clamped ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "counters moved on a clean read: unknown=%d kind_mismatch=%d widened=%d duplicate=%d\n", r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
    if ( r.malformed || r.refused ) { printf( "a clean read is not a refusal: malformed=%d refused=%d\n", (int) r.malformed, (int) r.refused ); return 1; }
    if ( r.reason != newer_form ) { printf( "reason is not the clean value: %d\n", (int) r.reason ); return 1; }
`
	forge := `
    /* THE FORGE (docs/FIXED-FORM-VERSIONING-TESTS.md §5.8 row 4): the OLD
       record is lead 0xAAAAAAAA then vals_count 4, both little-endian uint32,
       so the eight-byte needle is AA AA AA AA 04 00 00 00. Find it, assert it
       occurs EXACTLY once — a locator that matches twice is not a locator —
       and write the little-endian 7 over the count word, a value BETWEEN the
       writer's 4 and the reader's 8. */
    {
        static const uint8_t needle[8] = { 0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00 };
        int64_t found = -1;
        for ( int64_t i = 0; i + 8 <= len; ++i )
        {
            if ( memcmp( data + i, needle, 8 ) == 0 )
            {
                if ( found >= 0 ) { printf( "the locator matched twice: %lld and %lld\n", (long long) found, (long long) i ); return 1; }
                found = i;
            }
        }
        if ( found < 0 ) { printf( "the locator matched nowhere\n" ); return 1; }
        TableFixedPut32( data + found + 4, 7 ); /* the forge */
    }
`
	out, err := cppRunVersionProbe(t, "array_bounded_grow",
		filepath.Join(corpus, "old_array_bounded_grow.bin"), body, forge)
	if err != nil {
		t.Fatalf("writer_bound_count: %v\n%s", err, out)
	}
}
