package cpptable

// TestFixedRoadmapCppT05Evolution proves this card's rows of the `fixed-tables`
// roadmap node on the cpp leg: the text tasks cpp/C3 (text length clamp), cpp/C4
// (text content refuses BY NAME) and cpp/R24 (ill-formed text in the USED
// units). The law is docs/FIXED-FORM-ALGORITHM.md
// §4.5's `text` row and docs/SPEC-TABLES.md §3.4:
//
//   `v := SLE(4, record+src)` clamped into `[0, cap]`, `COUNT clamped` if it
//   fired. ... The CONTENT RULES apply to the USED UNITS and nothing else:
//   UTF-8 validity over `v` bytes, never over `N`. A content violation
//   REFUSES BY NAME.
//
// THE PRODUCTION PATH, not a helper: the generated cpp fixed-form reader of
// test/tables/FX1.schema (`FxRoot.label string(8)`), reached through
// cpptable's own Generate.
//
// ONE SUBTEST PER TASK THIS CARD PROVES, three of them. The cpp emitter now
// rules a content violation a REFUSAL BY NAME (TableMessageReason::text_ill_formed), the verdict
// the packet reader gives, where the reference set `malformed`: the shared
// vocabulary member lives in cpptable.go and the clamp that raises it is this
// leg's own. The leg's fixed-form harnesses (test/tables/fixedform_main.cpp,
// fixedform_properties.cpp) carry the same verdict.

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
    // ONE PAST THE CAP: 9 refuses length_over_bound by name, nothing decoded,
    // no counter moved, no destination byte written (docs/SPEC-TABLES.md §3.4,
    // docs/FIXED-FORM-ALGORITHM.md §4.5).
    TableFixedPut32( word, 9 );
    {
        memset( &r, 0, sizeof( r ) );
        for ( int k = 0; k < 4; ++k ) { FxRootReset( back[k] ); }
        n = FxRootFixedLoad( back, 4, data, filelen, plan, 4096, NULL, &r );
        if ( n != -1 || !r.refused || r.malformed || r.reason != length_over_bound )
            { printf( "C3: over-cap n=%lld refused=%d malformed=%d reason=%d\n", (long long) n, (int) r.refused, (int) r.malformed, (int) r.reason ); return 1; }
        if ( r.clamped != 0 )
            { printf( "C3: over-cap refused but a counter moved: clamped=%d\n", r.clamped ); return 1; }
    }
    // THE NEGATIVE END: -1 refuses length_over_bound by name.
    TableFixedPut32( word, 0xFFFFFFFFu );
    {
        memset( &r, 0, sizeof( r ) );
        for ( int k = 0; k < 4; ++k ) { FxRootReset( back[k] ); }
        n = FxRootFixedLoad( back, 4, data, filelen, plan, 4096, NULL, &r );
        if ( n != -1 || !r.refused || r.malformed || r.reason != length_over_bound )
            { printf( "C3: negative-end n=%lld refused=%d malformed=%d reason=%d\n", (long long) n, (int) r.refused, (int) r.malformed, (int) r.reason ); return 1; }
        if ( r.clamped != 0 )
            { printf( "C3: negative-end refused but a counter moved: clamped=%d\n", r.clamped ); return 1; }
    }
    printf( "C3 text length clamp holds\n" );
`
		out, err := cppRunFixedProbe(t, filepath.Join("..", "..", "..", "test", "tables", "FX1.schema"), "FxRoot", body)
		if err != nil {
			t.Fatalf("cpp/C3: %v\n%s", err, out)
		}
	})

	t.Run("cpp/C4", func(t *testing.T) {
		t.Parallel()
		body := `
    // A lawful one-record file whose label carries 4 USED bytes; bytes 4..7 are
    // SLACK, unspecified on read and never judged.
    {
        FxRoot v;
        FxRootReset( v );
        memcpy( v.label, "QWER", 4 ); v.label_length = 4;
        v.keep = 5; v.narrow = 6; v.renamed = 7;
        n = FxRootFixedSave( &v, 1, data, (int64_t) sizeof( data ) );
        if ( n <= 0 ) { printf( "C4: save failed\n" ); return 1; }
    }
    const int64_t filelen = n;
    static const uint8_t mark[4] = { 'Q','W','E','R' };
    int64_t at = find_bytes( data, n, mark, 4 );
    if ( at < 0 ) { printf( "C4: the label payload is not locatable: %lld\n", (long long) at ); return 1; }

    // SLACK IS NOT JUDGED: a byte that is not UTF-8 PAST the used length v=4
    // reads clean — the content rule runs over the used units and nothing else.
    data[at + 6] = 0xFF;
    {
        memset( &r, 0, sizeof( r ) );
        for ( int k = 0; k < 4; ++k ) { FxRootReset( back[k] ); }
        n = FxRootFixedLoad( back, 4, data, filelen, plan, 4096, NULL, &r );
        if ( n != 1 || r.refused || r.malformed )
            { printf( "C4: a slack byte past v was judged: n=%lld refused=%d malformed=%d\n", (long long) n, (int) r.refused, (int) r.malformed ); return 1; }
    }
    // ILL-FORMED CONTENT INSIDE v REFUSES BY NAME, the verdict the packet
    // reader gives (docs/FIXED-FORM-ALGORITHM.md §4.5, fix 11): refused plus the
    // name, never malformed, nothing decoded, no counter moved.
    data[at + 1] = 0xFF;
    {
        memset( &r, 0, sizeof( r ) );
        for ( int k = 0; k < 4; ++k ) { FxRootReset( back[k] ); }
        n = FxRootFixedLoad( back, 4, data, filelen, plan, 4096, NULL, &r );
        if ( n != -1 || !r.refused || r.malformed || r.reason != TableMessageReason::text_ill_formed )
            { printf( "C4: ill-formed content n=%lld refused=%d malformed=%d reason=%d\n", (long long) n, (int) r.refused, (int) r.malformed, (int) r.reason ); return 1; }
        if ( r.clamped != 0 || r.widened != 0 )
            { printf( "C4: a refusal moved a counter: clamped=%d widened=%d\n", r.clamped, r.widened ); return 1; }
    }
    printf( "C4 text content refuses by name\n" );
`
		out, err := cppRunFixedProbe(t, filepath.Join("..", "..", "..", "test", "tables", "FX1.schema"), "FxRoot", body)
		if err != nil {
			t.Fatalf("cpp/C4: %v\n%s", err, out)
		}
	})

	t.Run("cpp/R24", func(t *testing.T) {
		t.Parallel()
		body := `
    // A lawful one-record file whose label carries 4 USED bytes "QWER"; the
    // content rule walks the USED bytes and nothing else.
    {
        FxRoot v;
        FxRootReset( v );
        memcpy( v.label, "QWER", 4 ); v.label_length = 4;
        v.keep = 5; v.narrow = 6; v.renamed = 7;
        n = FxRootFixedSave( &v, 1, data, (int64_t) sizeof( data ) );
        if ( n <= 0 ) { printf( "R24: save failed\n" ); return 1; }
    }
    const int64_t filelen = n;
    static const uint8_t mark[4] = { 'Q','W','E','R' };
    int64_t at = find_bytes( data, n, mark, 4 );
    if ( at < 4 ) { printf( "R24: the label payload is not locatable: %lld\n", (long long) at ); return 1; }

    // ILL-FORMED TEXT IN THE USED BYTES REFUSES BY NAME (TableMessageReason::text_ill_formed).
    data[at + 2] = 0xC0;
    {
        memset( &r, 0, sizeof( r ) );
        for ( int k = 0; k < 4; ++k ) { FxRootReset( back[k] ); }
        n = FxRootFixedLoad( back, 4, data, filelen, plan, 4096, NULL, &r );
        if ( n != -1 || !r.refused || r.malformed || r.reason != TableMessageReason::text_ill_formed )
            { printf( "R24: ill-formed used bytes n=%lld refused=%d malformed=%d reason=%d\n", (long long) n, (int) r.refused, (int) r.malformed, (int) r.reason ); return 1; }
        if ( r.clamped != 0 || r.widened != 0 )
            { printf( "R24: a refusal moved a counter: clamped=%d widened=%d\n", r.clamped, r.widened ); return 1; }
    }
    // THE LENGTH REFUSAL AND THE CONTENT REFUSAL ON THE SAME RECORD: the length
    // word forged ONE PAST THE CAP (9 bytes) AND the used bytes ill-formed. The
    // count/length pre-scan runs first and refuses length_over_bound by name —
    // a count or length past the PLAN's bound is checked before any destination
    // byte is written (docs/SPEC-TABLES.md §3.4). REFUSE is total: no counter
    // moves and nothing is decoded.
    TableFixedPut32( data + at - 4, 9 );
    {
        memset( &r, 0, sizeof( r ) );
        for ( int k = 0; k < 4; ++k ) { FxRootReset( back[k] ); }
        n = FxRootFixedLoad( back, 4, data, filelen, plan, 4096, NULL, &r );
        if ( n != -1 || !r.refused || r.malformed || r.reason != length_over_bound )
            { printf( "R24: length+content n=%lld refused=%d malformed=%d reason=%d\n", (long long) n, (int) r.refused, (int) r.malformed, (int) r.reason ); return 1; }
        if ( r.clamped != 0 || r.widened != 0 )
            { printf( "R24: the length+content refusal moved a counter: clamped=%d widened=%d\n", r.clamped, r.widened ); return 1; }
    }
    printf( "R24 ill-formed used bytes refuse by name\n" );
`
		out, err := cppRunFixedProbe(t, filepath.Join("..", "..", "..", "test", "tables", "FX1.schema"), "FxRoot", body)
		if err != nil {
			t.Fatalf("cpp/R24: %v\n%s", err, out)
		}
	})
}
