package cpptable

// TestFixedVersioningRefuseWritesNothing is §5.8 row 9 of
// docs/FIXED-FORM-VERSIONING-TESTS.md. The NEW reader is handed the OLD
// lineage entry and reads old_nested_append.bin with record 0's per-record
// hash inverted. The header's hash is left alone so it still selects the OLD
// entry, whose compiled plan carries a nonempty fill list (Vec.w = 88) when
// step 11 compares the record's own hash; only the record hash is forged, a
// header-hash forge being a different row. The refusal is total: malformed is
// FALSE because a refusal by name is not a malformed file, and the poisoned
// storage stays 0x5A in every byte, not the prefill's 88 and not a zero. The
// poison goes AFTER the Reset loop, before the load; put it before and Reset
// erases it, leaving the row the vacuity this shift repairs.
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

func TestFixedVersioningRefuseWritesNothing(t *testing.T) {
	corpus := cppFixedCorpus(t)
	row := "nested_append"

	dir := t.TempDir()
	for _, prefix := range []string{"VOLD_", "VNEW_"} {
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

	// The VOLD_ peer must be in the lineage, or this is not a NEW-reads-OLD row.
	text := string(files[header])
	if !strings.Contains(text, "FixedLineage[]") {
		t.Fatalf("the generated header carries no FixedLineage[]; the nil-lock convention did not fire:\n%s", text)
	}
	if !strings.Contains(text, "vold_nested_append") {
		t.Fatalf("the lineage is missing the VOLD_ peer; the probe needs it to select the OLD entry:\n%s", text)
	}

	root := cppFixedRootName(t, u)
	pkg := u.Package

	poison := `    memset( back, 0x5A, sizeof( back ) );`

	forge := `
    /* THE FORGE (docs/FIXED-FORM-VERSIONING-TESTS.md §5.8 row 9): invert record
       0's PER-RECORD hash and leave the header's hash alone, so the read still
       selects the OLD entry and the refusal happens at step 11, comparing the
       record's own hash. */
    {
        uint32_t lb = TableFixedGet32( data + kTableFixedHeaderBytes );
        int64_t off = kTableFixedHeaderBytes + 4 + (int64_t) lb;
        if ( len < off + 8 ) { printf( "refuse_writes_nothing: record 0 is out of the file: len=%lld off=%lld\n", (long long) len, (long long) off ); return 1; }
        for ( int i = 0; i < 8; ++i )
        {
            data[off + i] = (uint8_t)~data[off + i]; /* the forge */
        }
    }
`

	body := `
    if ( n != -1 ) { printf( "refuse_writes_nothing: the forged record hash did not refuse: n=%lld\n", (long long) n ); return 1; }
    if ( !r.refused ) { printf( "refuse_writes_nothing: refused is false\n" ); return 1; }
    if ( r.reason != no_layout ) { printf( "refuse_writes_nothing: reason=%d, not no_layout\n", (int) r.reason ); return 1; }
    if ( r.malformed ) { printf( "refuse_writes_nothing: malformed set — a refusal by name is not a malformed file\n" ); return 1; }
    if ( r.clamped != 0 || r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "refuse_writes_nothing: a counter moved: clamped=%d unknown=%d kind_mismatch=%d widened=%d duplicate=%d\n", r.clamped, r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
    {
        const uint8_t * p = (const uint8_t *) back;
        int64_t i;
        for ( i = 0; i < (int64_t) sizeof( back ); ++i )
        {
            if ( p[i] != 0x5A ) { printf( "refuse_writes_nothing: byte %lld is 0x%02x — REFUSE wrote a destination byte\n", (long long) i, p[i] ); return 1; }
        }
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
%s
    memset( &r, 0, sizeof( r ) );
    n = %sFixedLoad( back, 8, data, len, plan, 4096, NULL, &r );
%s
    return 0;
}
`, header, pkg, filepath.Join(corpus, "old_nested_append.bin"), root, forge, root, poison, root, body)

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
		t.Fatalf("refuse_writes_nothing: the probe did not compile: %v\n%s", err, out)
	}
	out, err := exec.Command(filepath.Join(outDir, "probe")).CombinedOutput()
	if err != nil {
		t.Fatalf("refuse_writes_nothing: %v\n%s", err, out)
	}
}
