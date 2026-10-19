package cpptable

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

// writer_bound_count with TWO lineage peers (§5.8 row 4). The reader is
// VNEW_array_bounded_grow ([..8]int32). It holds two peers with DIFFERENT
// bounds: VOLD_array_bounded_grow ([..4]int32), which WROTE the file, and
// VMID_array_bounded_grow ([..6]int32), the distractor, which did not. The
// count word is forged to 7 exactly as the landed single-peer test forges it.
// A count past the PLAN's bound is the WRITING peer's (4) — the number the
// plan carries — so the load refuses `count_over_bound` by name, the Go
// oracle's count_over_bound_ends row (internal/codegen/gotable/
// fixedversioning_count_clamp_ends_test.go). Nothing is decoded and no counter
// moves.
//
// The C++ leg's sibling-filename convention (fixturePeerPaths) discovers every
// VOLD_/VMID_/VBRA_/VBRB_ sibling beside VNEW_, but cppRunVersionProbe copies
// only VOLD_ and VNEW_ into its temp dir, so VMID_ would never be found there.
// This test copies all three and drives the same pipeline by hand, so the
// reader really is handed BOTH peers before it reads the writer's file.
func TestFixedVersioningWriterBoundCountTwoPeers(t *testing.T) {
	corpus := cppFixedCorpus(t)
	row := "array_bounded_grow"

	dir := t.TempDir()
	for _, prefix := range []string{"VOLD_", "VMID_", "VNEW_"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", prefix+row+".schema"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, prefix+row+".schema"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	u := loadUnit(t, filepath.Join(dir, "VNEW_"+row+".schema"))

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

	// BOTH peers must be in the lineage, or this is just the single-peer test.
	text := string(files[header])
	if !strings.Contains(text, "FixedLineage[]") {
		t.Fatalf("the generated header carries no FixedLineage[]; the nil-lock convention did not fire:\n%s", text)
	}
	if !strings.Contains(text, "vold_array_bounded_grow") || !strings.Contains(text, "vmid_array_bounded_grow") {
		t.Fatalf("the lineage is missing a peer; the two-peer probe needs VOLD_ and VMID_ both:\n%s", text)
	}

	root := cppFixedRootName(t, u)
	pkg := u.Package

	body := `
    if ( n != -1 ) { printf( "n is not -1: %lld\n", (long long) n ); return 1; }
    if ( !r.refused ) { printf( "refused is false\n" ); return 1; }
    if ( r.reason != count_over_bound ) { printf( "reason=%d, not count_over_bound\n", (int) r.reason ); return 1; }
    if ( r.malformed ) { printf( "malformed set — a refusal by name is not malformed\n" ); return 1; }
    if ( r.clamped != 0 || r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "a counter moved on a refusal: clamped=%d unknown=%d kind_mismatch=%d widened=%d duplicate=%d\n", r.clamped, r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
    {
        const uint8_t * p = (const uint8_t *) back;
        for ( int64_t i = 0; i < (int64_t) sizeof( back ); ++i )
            if ( p[i] != 0x5A ) { printf( "REFUSE wrote destination byte %lld: 0x%02x\n", (long long) i, p[i] ); return 1; }
    }
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
    memset( back, 0x5A, sizeof( back ) ); /* THE POISON, after the Reset and before the load */
    memset( &r, 0, sizeof( r ) );
    n = %sFixedLoad( back, 8, data, len, plan, 4096, NULL, &r );
%s
    return 0;
}
`, header, pkg, filepath.Join(corpus, "old_array_bounded_grow.bin"), root, forge, root, root, body)

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
		t.Fatalf("writer_bound_count, two peers: the probe did not compile: %v\n%s", err, out)
	}
	out, err := exec.Command(filepath.Join(outDir, "probe")).CombinedOutput()
	if err != nil {
		t.Fatalf("writer_bound_count, two peers: %v\n%s", err, out)
	}
}
