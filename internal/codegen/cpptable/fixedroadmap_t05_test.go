package cpptable

// TestFixedRoadmapCppT05Evolution proves this card's rows of the `fixed-tables`
// roadmap node on the cpp leg: the text tasks cpp/C3 (text length clamp), cpp/C4
// (text content refuses BY NAME), cpp/C5 (wide text code units) and cpp/R24
// (ill-formed text in the USED units). The law is docs/FIXED-FORM-ALGORITHM.md
// §4.5's `text` row and docs/SPEC-TABLES.md §3.4:
//
//   `v := SLE(4, record+src)` clamped into `[0, cap]`, `COUNT clamped` if it
//   fired. ... The CONTENT RULES apply to the USED UNITS and nothing else:
//   UTF-8 validity over `v` bytes, never over `N`; wide code units over `v`,
//   never `2N`, an astral pair counting two. A content violation REFUSES BY
//   NAME.
//
// THE PRODUCTION PATH, not a helper: the generated cpp fixed-form readers of
// test/tables/FX1.schema (`FxRoot.label string(8)`) and test/tables/FXW.schema
// (`FxWide.caption wstring(8)`), reached through cpptable's own Generate.
//
// ONE SUBTEST PER TASK THIS CARD PROVES. cpp/C3 and cpp/C5 are proved here;
// cpp/C4 and cpp/R24 are named in the verdicts as unknown — the cpp reference
// sets `malformed` where docs/FIXED-FORM-ALGORITHM.md §4.5's text row rules a
// refusal by name, and closing that gap moves the shared refusal vocabulary and
// the existing test/conformance/cpp/rows/C5.cpp outside this card's PATHS.

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

// cppRunFixedProbe generates the cpp fixed form for `schemaPath` (a unit under
// test/tables), compiles a probe around `root`'s own generated FixedLoad, and
// returns the probe's output. `body` is the C++ between the load and `return 0`,
// where `back` is the decoded array, `r` the report, `data`/`len` the file and
// `plan` the caller's entry storage.
func cppRunFixedProbe(t *testing.T, schemaPath, root, body string) ([]byte, error) {
	t.Helper()
	u := loadUnit(t, schemaPath)
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
	pkg := u.Package
	probe := fmt.Sprintf(`#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <vector>
#include "%s"

using namespace %s;


static int64_t find_bytes( const uint8_t * d, int64_t len, const uint8_t * needle, int64_t k )
{
    for ( int64_t i = 0; i + k <= len; ++i ) { if ( memcmp( d + i, needle, (size_t) k ) == 0 ) { return i; } }
    return -1;
}

int main( void )
{
    static uint8_t data[1 << 20];
    std::vector<%s> store( 4 );
    %s * back = store.data();
    TableReport r;
    static TableFixedEntry plan[4096];
    int64_t n = -1;

%s
    return 0;
}
`, header, pkg, root, root, body)
	if err := os.WriteFile(filepath.Join(outDir, "probe.cpp"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	serialize, _ := filepath.Abs("../../../../serialize")
	args := []string{"-std=c++17", "-O1", "-I" + outDir, "-I" + serialize, filepath.Join(outDir, "probe.cpp"), "-o", filepath.Join(outDir, "probe")}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		return out, fmt.Errorf("the probe did not compile: %w", err)
	}
	return exec.Command(filepath.Join(outDir, "probe")).CombinedOutput()
}

func TestFixedRoadmapCppT05Evolution(t *testing.T) {
	t.Parallel()

	t.Run("cpp/C3", func(t *testing.T) {
		t.Parallel()
		body := `
    // A lawful one-record file: label "QWER" (4 bytes, cap 8), neighbours set.
    {
        FxRoot v;
        FxRootReset( v );
        memcpy( v.label, "ABCDEFGH", 8 ); v.label_length = 8;
        v.keep = 11; v.narrow = 22; v.renamed = 33;
        n = FxRootFixedSave( &v, 1, data, (int64_t) sizeof( data ) );
        if ( n <= 0 ) { printf( "C3: save failed\n" ); return 1; }
    }
    const int64_t filelen = n;
    // the label payload "QWER", and its SLE(4) length word four bytes before
    static const uint8_t mark[8] = { 'A','B','C','D','E','F','G','H' };
    int64_t at = find_bytes( data, n, mark, 8 );
    if ( at < 4 ) { printf( "C3: the label payload is not locatable: %lld\n", (long long) at ); return 1; }
    uint8_t * word = data + at - 4;

    // CLEAN AT THE CAP: length 8 is IN [0, cap] — no counter moves.
    TableFixedPut32( word, 8 );
    {
        memset( &r, 0, sizeof( r ) );
        for ( int k = 0; k < 4; ++k ) { FxRootReset( back[k] ); }
        n = FxRootFixedLoad( back, 4, data, filelen, plan, 4096, NULL, &r );
        if ( n != 1 || r.refused || r.malformed || r.clamped != 0 )
            { printf( "C3: clean-at-cap n=%lld refused=%d malformed=%d clamped=%d\n", (long long) n, (int) r.refused, (int) r.malformed, r.clamped ); return 1; }
        if ( back[0].label_length != 8 || back[0].keep != 11 || back[0].narrow != 22 || back[0].renamed != 33 )
            { printf( "C3: clean-at-cap did not land: len=%d keep=%u narrow=%u renamed=%d\n", back[0].label_length, back[0].keep, (unsigned) back[0].narrow, back[0].renamed ); return 1; }
    }
    // ONE PAST THE CAP: 9 clamps to 8, COUNT clamped once, the record reads on.
    TableFixedPut32( word, 9 );
    {
        memset( &r, 0, sizeof( r ) );
        for ( int k = 0; k < 4; ++k ) { FxRootReset( back[k] ); }
        n = FxRootFixedLoad( back, 4, data, filelen, plan, 4096, NULL, &r );
        if ( n != 1 || r.refused || r.malformed || r.clamped != 1 )
            { printf( "C3: over-cap n=%lld refused=%d malformed=%d clamped=%d\n", (long long) n, (int) r.refused, (int) r.malformed, r.clamped ); return 1; }
        if ( back[0].label_length != 8 )
            { printf( "C3: over-cap length is not the cap 8: %d\n", back[0].label_length ); return 1; }
        if ( back[0].keep != 11 || back[0].narrow != 22 || back[0].renamed != 33 )
            { printf( "C3: the clamp is one field's, neighbours moved\n" ); return 1; }
    }
    // THE NEGATIVE END: -1 clamps to 0, COUNT clamped once.
    TableFixedPut32( word, 0xFFFFFFFFu );
    {
        memset( &r, 0, sizeof( r ) );
        for ( int k = 0; k < 4; ++k ) { FxRootReset( back[k] ); }
        n = FxRootFixedLoad( back, 4, data, filelen, plan, 4096, NULL, &r );
        if ( n != 1 || r.refused || r.malformed || r.clamped != 1 || back[0].label_length != 0 )
            { printf( "C3: negative-end n=%lld clamped=%d len=%d\n", (long long) n, r.clamped, back[0].label_length ); return 1; }
    }
    printf( "C3 text length clamp holds\n" );
`
		out, err := cppRunFixedProbe(t, filepath.Join("..", "..", "..", "test", "tables", "FX1.schema"), "FxRoot", body)
		if err != nil {
			t.Fatalf("cpp/C3: %v\n%s", err, out)
		}
	})

	t.Run("cpp/C5", func(t *testing.T) {
		t.Parallel()
		body := `
    // A lawful one-record file: caption wstring(8) = "wxyz" (4 units, cap 8),
    // its code-unit payload little-endian. FxWide reaches it at a nested level.
    {
        FxWide v;
        FxWideReset( v );
        const char16_t u[8] = { u'w',u'x',u'y',u'z',u'W',u'X',u'Y',u'Z' };
        memcpy( v.caption, u, sizeof( u ) ); v.caption_length = 8;
        v.seq = 1000;
        n = FxWideFixedSave( &v, 1, data, (int64_t) sizeof( data ) );
        if ( n <= 0 ) { printf( "C5: save failed\n" ); return 1; }
    }
    const int64_t filelen = n;
    static const uint8_t cu[16] = { 'w',0,'x',0,'y',0,'z',0,'W',0,'X',0,'Y',0,'Z',0 };
    int64_t at = find_bytes( data, n, cu, 16 );
    if ( at < 4 ) { printf( "C5: the wide payload is not locatable: %lld\n", (long long) at ); return 1; }
    uint8_t * word = data + at - 4;

    // THE CLAMP IS IN CODE UNITS, NOT BYTES: a used length of 16 bytes is 8
    // units, and 16 > cap 8 clamps to 8 units — a reader whose cap was the byte
    // capacity would keep 16 or land 16, both red.
    TableFixedPut32( word, 16 );
    {
        memset( &r, 0, sizeof( r ) );
        for ( int k = 0; k < 4; ++k ) { FxWideReset( back[k] ); }
        n = FxWideFixedLoad( back, 4, data, filelen, plan, 4096, NULL, &r );
        if ( n != 1 || r.refused || r.malformed || r.clamped != 1 || back[0].caption_length != 8 )
            { printf( "C5: byte-vs-unit clamp n=%lld clamped=%d len=%d\n", (long long) n, r.clamped, back[0].caption_length ); return 1; }
        if ( back[0].seq != 1000 )
            { printf( "C5: the neighbour seq moved: %u\n", back[0].seq ); return 1; }
    }
    // CONTENT OVER v, NEVER 2N: with the used length v=4, a lone surrogate at
    // unit 5 is PAST the walked range and the read stays clean — the check is
    // over the USED UNITS and nothing else.
    TableFixedPut32( word, 4 );
    data[at + 10] = 0x00; data[at + 11] = 0xD8; // a lone high surrogate at unit 5
    {
        memset( &r, 0, sizeof( r ) );
        for ( int k = 0; k < 4; ++k ) { FxWideReset( back[k] ); }
        n = FxWideFixedLoad( back, 4, data, filelen, plan, 4096, NULL, &r );
        if ( n != 1 || r.malformed || r.refused )
            { printf( "C5: a surrogate past v=4 was walked: n=%lld malformed=%d refused=%d\n", (long long) n, (int) r.malformed, (int) r.refused ); return 1; }
    }
    // INSIDE v: the same surrogate at unit 2 with v=4 IS content the walk
    // checks — the check fires (the verdict itself is cpp/C4's; here it is that
    // the walk reaches it at all).
    data[at + 10] = 'X'; data[at + 11] = 0x00; // restore unit 5
    data[at + 4] = 0x00; data[at + 5] = 0xD8;  // a lone surrogate at unit 2
    {
        memset( &r, 0, sizeof( r ) );
        for ( int k = 0; k < 4; ++k ) { FxWideReset( back[k] ); }
        n = FxWideFixedLoad( back, 4, data, filelen, plan, 4096, NULL, &r );
        if ( !( r.malformed || r.refused ) )
            { printf( "C5: a surrogate inside v=4 was NOT content-checked\n" ); return 1; }
    }
    printf( "C5 wide code units hold\n" );
`
		out, err := cppRunFixedProbe(t, filepath.Join("..", "..", "..", "test", "tables", "FXW.schema"), "FxWide", body)
		if err != nil {
			t.Fatalf("cpp/C5: %v\n%s", err, out)
		}
	})
}
