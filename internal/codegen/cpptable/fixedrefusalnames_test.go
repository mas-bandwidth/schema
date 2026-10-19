package cpptable

// TestCPPFixedLoadNamesEveryRefusal pins docs/FIXED-FORM-ALGORITHM.md §5.3's
// naming rule on the C++ leg: every refusal of the emitted <T>FixedLoad has a
// name, the framing-damage returns included. A framing-damage return keeps
// `malformed` set BESIDE its name (truncated, reserved_not_zero), and a refusal
// by name never sets it.
//
// THE ORACLE IS THE GO LEG: internal/codegen/gotable/fixedrefusalnames_test.go,
// TestFixedLoadNamesEveryRefusal, over the same schema text
//
//	fixed table Point { x int32 = 1  y int32 = 2 }
//
// and the same input bytes. Every expected answer below is that test's answer
// transcribed into the C++ probe; the case name in each comment is the Go
// case's own `add(...)` name. layout_unsupported and layout_record_too_large
// need a multi-version lineage and are reached by their own probes, exactly as
// on Go (the same Go test delegates them to
// testFixedLoadNamesUnsupported/testFixedLoadNamesRecordTooLarge).
//
// The schemas the other C++ tests pin use a corpus file or a versioned pair.
// This one is self-contained — it generates the header from the schema text,
// writes a clean file with the generated save, forges one thing per case, and
// reads it back — so the refusal names are proved wherever `go test` runs.

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

func TestCPPFixedLoadNamesEveryRefusal(t *testing.T) {
	t.Parallel()

	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	if _, err := exec.LookPath(cxx); err != nil {
		t.Skip("the generated fixed reader is C++: no c++ on PATH")
	}

	dir := t.TempDir()
	schema := `package probe

fixed table Point
{
    x int32 = 1
    y int32 = 2
}
`
	schemaPath := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(schemaPath, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, schemaPath)

	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatalf("generate lineage: %v", err)
	}
	maps.Copy(files, tables)

	var header string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
	}
	if header == "" {
		t.Fatal("the generated reader carries no <Base>Table.h")
	}

	probe := fmt.Sprintf(`#include <cstdio>
#include <cstring>
#include <cstdint>
#include "%s"

using namespace probe;

/* THE THREE NAMES THIS CARD ADDS (docs/FIXED-FORM-ALGORITHM.md §5.3,
   docs/SPEC-TABLES.md §11): real scoped members of TableMessageReason, so a
   caller switching exhaustively on that enum sees every fixed-form outcome. */
static constexpr TableMessageReason k_layout_unknown    = TableMessageReason::layout_unknown;
static constexpr TableMessageReason k_truncated         = TableMessageReason::truncated;
static constexpr TableMessageReason k_reserved_not_zero = TableMessageReason::reserved_not_zero;

static uint8_t file[1 << 16];
static TableFixedEntry plan[64];

static int expect( const char * what, const uint8_t * data, int64_t bytes, int32_t capacity,
                   TableFixedEntry * p, int32_t plan_capacity,
                   int want_refused, int want_malformed, TableMessageReason want_reason,
                   int want_hash, uint64_t hash )
{
    Point dst[2];
    for ( int i = 0; i < 2; ++i ) { PointReset( dst[i] ); }
    TableReport r;
    memset( &r, 0, sizeof( r ) );
    const int64_t n = PointFixedLoad( dst, capacity, data, bytes, p, plan_capacity, NULL, &r );
    if ( n != -1 ) { printf( "%%s: n=%%lld, want -1\n", what, (long long) n ); return 1; }
    if ( (int) r.refused != want_refused )
        { printf( "%%s: refused=%%d want %%d (reason=%%d malformed=%%d)\n", what, (int) r.refused, want_refused, (int) r.reason, (int) r.malformed ); return 1; }
    if ( (int) r.malformed != want_malformed )
        { printf( "%%s: malformed=%%d want %%d (reason=%%d refused=%%d)\n", what, (int) r.malformed, want_malformed, (int) r.reason, (int) r.refused ); return 1; }
    if ( r.reason != want_reason )
        { printf( "%%s: reason=%%d want %%d\n", what, (int) r.reason, (int) want_reason ); return 1; }
    if ( want_hash )
    {
        if ( r.layout_hash != hash )
            { printf( "%%s: layout_hash=%%016llx want the file's %%016llx\n", what, (unsigned long long) r.layout_hash, (unsigned long long) hash ); return 1; }
    }
    else if ( r.layout_hash != 0 )
        { printf( "%%s: layout_hash=%%016llx on a path that reports no hash\n", what, (unsigned long long) r.layout_hash ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.clamped != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "%%s: a refusal moved a counter (u=%%d km=%%d c=%%d w=%%d d=%%d)\n", what, r.unknown, r.kind_mismatch, r.clamped, r.widened, r.duplicate ); return 1; }
    return 0;
}

int main( void )
{
    Point values[2];
    PointReset( values[0] );
    PointReset( values[1] );
    values[0].x = 4242; values[0].y = -7;
    values[1].x = 4243; values[1].y = -8;
    const int64_t written = PointFixedSave( values, 2, file, (int64_t) sizeof( file ) );
    if ( written != PointFixedMeasure( 2 ) ) { printf( "save wrote %%lld\n", (long long) written ); return 1; }

    {
        Point dst[2];
        TableReport r;
        memset( &r, 0, sizeof( r ) );
        if ( PointFixedLoad( dst, 2, file, written, plan, 64, NULL, &r ) != 2 )
            { printf( "the clean file did not read two records\n" ); return 1; }
    }

    static uint8_t scratch[1 << 16];

    /* internal/codegen/gotable/fixedrefusalnames_test.go case "previous_form" */
    memcpy( scratch, file, (size_t) written ); scratch[0] = 1;
    if ( expect( "previous_form", scratch, written, 2, plan, 64, 1, 0, TableMessageReason::previous_form, 0, 0 ) ) return 1;

    /* case "message_form_as_file" */
    memcpy( scratch, file, (size_t) written ); scratch[0] = 2;
    if ( expect( "message_form_as_file", scratch, written, 2, plan, 64, 1, 0, TableMessageReason::message_form_as_file, 0, 0 ) ) return 1;

    /* case "newer_form" */
    memcpy( scratch, file, (size_t) written ); scratch[0] = 6;
    if ( expect( "newer_form", scratch, written, 2, plan, 64, 1, 0, TableMessageReason::newer_form, 0, 0 ) ) return 1;

    /* case "layout_malformed": the layout's first byte is not the locked one */
    memcpy( scratch, file, (size_t) written ); scratch[kTableFixedHeaderBytes + 4] ^= 1;
    if ( expect( "layout_malformed", scratch, written, 2, plan, 64, 1, 0, TableMessageReason::layout_malformed, 0, 0 ) ) return 1;

    /* case "plan_too_large": a caller's plan that cannot hold the identity plan */
    if ( expect( "plan_too_large", file, written, 2, plan, 0, 1, 0, TableMessageReason::plan_too_large, 0, 0 ) ) return 1;

    /* case "batch_too_large": two records into room for one */
    if ( expect( "batch_too_large", file, written, 1, plan, 64, 1, 0, TableMessageReason::batch_too_large, 0, 0 ) ) return 1;

    /* case "no_layout": the first record's hash is not the file's */
    memcpy( scratch, file, (size_t) written ); scratch[written - 2 * PointFixedRecordBytes] ^= 1;
    if ( expect( "no_layout", scratch, written, 2, plan, 64, 1, 0, TableMessageReason::no_layout, 0, 0 ) ) return 1;

    /* case "truncated empty": the Go case passes a nil slice, length 0 */
    if ( expect( "truncated empty", NULL, 0, 2, plan, 64, 0, 1, k_truncated, 0, 0 ) ) return 1;

    /* case "truncated short header": file[:19] */
    if ( expect( "truncated short header", file, 19, 2, plan, 64, 0, 1, k_truncated, 0, 0 ) ) return 1;

    /* case "truncated ragged": the two-record file plus one byte */
    memcpy( scratch, file, (size_t) written ); scratch[written] = 0;
    if ( expect( "truncated ragged", scratch, written + 1, 2, plan, 64, 0, 1, k_truncated, 0, 0 ) ) return 1;

    /* case "reserved_not_zero": b[3] = 1 */
    memcpy( scratch, file, (size_t) written ); scratch[3] = 1;
    if ( expect( "reserved_not_zero", scratch, written, 2, plan, 64, 0, 1, k_reserved_not_zero, 0, 0 ) ) return 1;

    /* case "layout_unknown": a hash in no lineage entry carries the file's hash
       and NOTHING ELSE (layout_newer is reserved: SPEC-TABLES §21.3) */
    memcpy( scratch, file, (size_t) written ); scratch[kTableFixedHashAt] ^= 1;
    if ( expect( "layout_unknown", scratch, written, 2, plan, 64, 1, 0, k_layout_unknown, 1, TableFixedGet64( scratch + kTableFixedHashAt ) ) ) return 1;

    printf( "every fixed refusal carries a name\n" );
    return 0;
}
`, header)

	if err := os.WriteFile(filepath.Join(dir, "probe.cpp"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	serialize, err := filepath.Abs("../../../../serialize")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := []string{"-std=c++17", "-O1", "-I" + dir, "-I" + serialize, filepath.Join(dir, "probe.cpp"), "-o", bin}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("the probe did not compile: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("the probe failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "every fixed refusal carries a name") {
		t.Fatalf("the probe said %q", out)
	}
}
