package cpptable

// TestFixedRoadmapCppT01Framing is the cpp leg's card schema-cpp-t01-framing,
// the rows file-envelope, batch-capacity and plan-selection of docs/roadmap.sexp's
// node `fixed-tables` (ROADMAP.md "NEW Fixed Tables"). One subtest per task id
// this card proves, each driving the GENERATED C++ reader (`<Root>FixedLoad`)
// against the WRITER the same build lays down (`<Root>FixedSave`): the probe
// SAVES its own file and then forges it, so the row reads the same C++ reference
// bytes a corpus would carry without needing the sibling runtime the corpus's
// dump includes (docs/FIXED-FORM-ALGORITHM.md §5.3's steps, §5.9 #7's report and
// #47's "a runtime derives no hash"; docs/SPEC-TABLES.md §3.4).
//
// Tasks also held by an existing corpus test are still proved here, because the
// probe writes its own file: the byte-oracle corpus's sibling runtime is absent
// in this tree, so a corpus test skips where this one runs. The pre-existing
// tests are named beside each verdict for corroboration:
//
//	cpp/F7  TestFixedFormUnder20Bytes        (fixedform_under_20_bytes_test.go)
//	cpp/F8  TestFixedFormRaggedTail          (fixedform_ragged_tail_test.go)
//	cpp/F10 TestFixedVersioningRefuseWritesNothing (fixedversioning_refuse_writes_nothing_test.go)
//	cpp/R12 TestFixedVersioningRefuseWritesNothing (the per-record hash before the prefill)
//
// EVERY REFUSAL IS CHECKED AGAINST THE WHOLE REPORT, not against a list of
// fields: the macros below assert `refused`/`reason`/`layout_hash` and that
// every counter stays zero, and the destination is poisoned 0x5A first so a
// refusal that wrote one byte is a failure. MALFORMED is its converse:
// `malformed` alone, `refused` and `reason` untouched.

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
)

// cppT01Probe is the probe every subtest compiles: it includes the generated
// header, saves ONE clean record with the build's own writer, and hands the
// body a `data`/`len` file plus the `back`/`r`/`plan`/`n` names the macros and
// the bodies use. @@HEADER@@, @@PKG@@ and @@ROOT@@ are substituted; @@BODY@@ is
// the subtest's assertions.
const cppT01Probe = `#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "@@HEADER@@"

using namespace @@PKG@@;

static const uint64_t kT01OwnHash = @@ROOT@@FixedHash;
static const int64_t  kT01Lineage = @@ROOT@@FixedLineageCount;
static const TableFixedKnownLayout & kT01Old      = @@ROOT@@FixedKnown[0];
static const TableFixedKnownLayout & kT01Identity = @@ROOT@@FixedKnown[@@ROOT@@FixedLineageCount - 1];

#define T01_RESET() do { memset( back, 0x5A, sizeof( back ) ); memset( &r, 0, sizeof( r ) ); } while ( 0 )
#define T01_POISON( WHO ) do { const uint8_t * p_ = (const uint8_t *) back; int64_t i_; \
    for ( i_ = 0; i_ < (int64_t) sizeof( back ); ++i_ ) \
        if ( p_[i_] != 0x5A ) { printf( "%s: byte %lld is 0x%02x: a refusal wrote a destination byte\n", (WHO), (long long) i_, (unsigned) p_[i_] ); return 1; } } while ( 0 )
#define T01_REFUSED( WHO, REASON, HASH ) do { \
    if ( n != -1 ) { printf( "%s: n=%lld, not -1\n", (WHO), (long long) n ); return 1; } \
    if ( !r.refused ) { printf( "%s: refused is false\n", (WHO) ); return 1; } \
    if ( r.malformed ) { printf( "%s: malformed set beside a refusal by name\n", (WHO) ); return 1; } \
    if ( r.reason != (REASON) ) { printf( "%s: reason=%d, not %d\n", (WHO), (int) r.reason, (int) (REASON) ); return 1; } \
    if ( r.layout_hash != (uint64_t) (HASH) ) { printf( "%s: layout_hash=%llx, not %llx\n", (WHO), (unsigned long long) r.layout_hash, (unsigned long long) (HASH) ); return 1; } \
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.clamped != 0 || r.duplicate != 0 || r.widened != 0 || r.retained != 0 || r.retain_lost != 0 ) \
        { printf( "%s: a counter moved: u=%d km=%d c=%d d=%d w=%d r=%d rl=%d\n", (WHO), r.unknown, r.kind_mismatch, r.clamped, r.duplicate, r.widened, r.retained, r.retain_lost ); return 1; } \
    T01_POISON( WHO ); } while ( 0 )
#define T01_MALFORMED( WHO ) do { \
    if ( n != -1 ) { printf( "%s: n=%lld, not -1\n", (WHO), (long long) n ); return 1; } \
    if ( !r.malformed ) { printf( "%s: malformed is false\n", (WHO) ); return 1; } \
    if ( r.refused ) { printf( "%s: refused set beside malformed\n", (WHO) ); return 1; } \
    if ( r.reason != newer_form ) { printf( "%s: reason=%d beside malformed, not untouched\n", (WHO), (int) r.reason ); return 1; } \
    if ( r.layout_hash != 0 ) { printf( "%s: layout_hash set beside malformed\n", (WHO) ); return 1; } \
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.clamped != 0 || r.duplicate != 0 || r.widened != 0 || r.retained != 0 || r.retain_lost != 0 ) \
        { printf( "%s: a counter moved beside malformed\n", (WHO) ); return 1; } \
    T01_POISON( WHO ); } while ( 0 )

int main( void )
{
    static uint8_t data[1 << 20];
    int64_t len;
    static @@ROOT@@ values[8];
    static @@ROOT@@ back[8];
    TableReport r;
    static TableFixedEntry plan[4096];
    int64_t n = 0;
    (void) n;
    (void) kT01Lineage;

    memset( data, 0, sizeof( data ) );
    memset( values, 0, sizeof( values ) );
    for ( int k = 0; k < 8; ++k ) { @@ROOT@@Reset( values[k] ); }
    len = @@ROOT@@FixedSave( values, 1, data, (int64_t) sizeof( data ) );
    if ( len < 20 ) { printf( "the writer produced no file: len=%lld\n", (long long) len ); return 1; }

@@BODY@@
    return 0;
}
`

// cppT01Generate generates the NEW reader's headers for a VOLD_/VNEW_ row (the
// nil lock reads the sibling by the filename convention, §5.8 row 1's interim)
// and answers the file map, the Table header's name, the package and the root.
func cppT01Generate(t *testing.T, row string) (files map[string][]byte, header, pkg, root string) {
	t.Helper()
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
	for name := range files {
		if !strings.Contains(name, "/") && strings.HasSuffix(name, "Table.h") {
			header = name
		}
	}
	if header == "" {
		t.Fatal("no Table.h was generated")
	}
	return files, header, u.Package, cppFixedRootName(t, u)
}

// cppT01Run compiles the probe with the generated headers and runs it. The
// floor test hooks are enabled so a subtest may retire a lineage entry by hand
// (docs/roadmap.sexp's plan-selection floor lane).
func cppT01Run(t *testing.T, row, body string) ([]byte, error) {
	t.Helper()
	files, header, pkg, root := cppT01Generate(t, row)
	outDir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(outDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	probe := strings.Replace(cppT01Probe, "@@BODY@@", body, 1)
	probe = strings.NewReplacer(
		"@@HEADER@@", header,
		"@@PKG@@", pkg,
		"@@ROOT@@", root,
	).Replace(probe)
	if err := os.WriteFile(filepath.Join(outDir, "probe.cpp"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	args := []string{"-std=c++17", "-O1", "-DSCHEMA_FIXED_FLOOR_TEST_HOOKS", "-I" + outDir, filepath.Join(outDir, "probe.cpp"), "-o", filepath.Join(outDir, "probe")}
	build := exec.Command(cxx, args...)
	if out, err := build.CombinedOutput(); err != nil {
		return out, err
	}
	return exec.Command(filepath.Join(outDir, "probe")).CombinedOutput()
}

func cppT01ProbeRow(t *testing.T, row, body string) {
	t.Helper()
	out, err := cppT01Run(t, row, body)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestFixedRoadmapCppT01Framing(t *testing.T) {
	t.Parallel()
	rows := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"cpp/F4", cppT01TruncatedLayout},
		{"cpp/F7", cppT01Under20},
		{"cpp/F8", cppT01RaggedTail},
		{"cpp/F12", cppT01SecondLayout},
		{"cpp/F9", cppT01BatchTooLarge},
	}
	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			t.Parallel()
			row.run(t)
		})
	}
}

// cpp/F4 — "layout_malformed, truncated". Algorithm §5.3 step 3: "L := LE(4,
// file+16) ; if 20 + L > len(file): REFUSE layout_malformed". The file's own
// u32 layout length is left alone and the file is cut inside the layout, so
// the declared layout runs past the end: a REFUSAL BY NAME, never `malformed`
// (the layout is present and a known hash names it). The whole layout with no
// record behind it is the boundary and reads clean as n == 0.
func cppT01TruncatedLayout(t *testing.T) {
	cppT01ProbeRow(t, "field_append", `
    {
        const int64_t L = @@ROOT@@FixedLayoutBytes;
        int64_t cut;
        char who[64];
        if ( len < kTableFixedHeaderBytes + 4 + L ) { printf( "the writer's file is shorter than its own layout\n" ); return 1; }
        for ( cut = 0; cut < L; ++cut )
        {
            snprintf( who, sizeof( who ), "truncated at 20+%lld of 20+%lld", (long long) cut, (long long) L );
            T01_RESET();
            n = @@ROOT@@FixedLoad( back, 8, data, kTableFixedHeaderBytes + 4 + cut, plan, 4096, NULL, &r );
            T01_REFUSED( who, layout_malformed, 0 );
        }
        T01_RESET();
        n = @@ROOT@@FixedLoad( back, 8, data, kTableFixedHeaderBytes + 4 + L, plan, 4096, NULL, &r );
        if ( n != 0 || r.refused || r.malformed ) { printf( "a whole layout with no record is a clean n==0 read: n=%lld refused=%d malformed=%d\n", (long long) n, (int) r.refused, (int) r.malformed ); return 1; }
    }
`)
}

// cpp/F7 — "malformed, under 20 bytes". Algorithm §5.3 steps 1 and 2: no first
// byte at all is `malformed`, and a file shorter than the twenty-byte fixed
// header (form byte plus seven reserved bytes plus the u32 layout length) is
// `malformed` too. It is the residue of a truncation and never a refusal by
// name: `reason` stays untouched. (Also TestFixedFormUnder20Bytes.)
func cppT01Under20(t *testing.T) {
	cppT01ProbeRow(t, "field_append", `
    {
        int64_t k;
        char who[64];
        T01_RESET();
        n = @@ROOT@@FixedLoad( back, 8, data, 0, plan, 4096, NULL, &r );
        T01_MALFORMED( "zero bytes" );
        for ( k = 1; k < kTableFixedHeaderBytes + 4; ++k )
        {
            snprintf( who, sizeof( who ), "under twenty bytes at %lld", (long long) k );
            T01_RESET();
            n = @@ROOT@@FixedLoad( back, 8, data, k, plan, 4096, NULL, &r );
            T01_MALFORMED( who );
        }
    }
`)
}

// cpp/F8 — "malformed, ragged tail". Algorithm §5.3 step 9: "if record_bytes <=
// 8 or rest mod record_bytes != 0: report.malformed := true ; return -1". A
// record region that is not a whole number of records is thin air and is
// `malformed`, never a refusal by name. One more whole record is not ragged:
// the loop lands record 0 and then refuses the next record's hash no_layout.
// (Also TestFixedFormRaggedTail.)
func cppT01RaggedTail(t *testing.T) {
	cppT01ProbeRow(t, "field_append", `
    {
        const int64_t record_bytes = @@ROOT@@FixedRecordBytes;
        int64_t extra;
        char who[64];
        for ( extra = 1; extra < record_bytes; ++extra )
        {
            snprintf( who, sizeof( who ), "ragged tail extra=%lld", (long long) extra );
            T01_RESET();
            n = @@ROOT@@FixedLoad( back, 8, data, len + extra, plan, 4096, NULL, &r );
            T01_MALFORMED( who );
        }
        T01_RESET();
        n = @@ROOT@@FixedLoad( back, 8, data, len + record_bytes, plan, 4096, NULL, &r );
        if ( n != -1 || !r.refused || r.malformed || r.reason != no_layout )
            { printf( "one more whole record walks its hash: n=%lld refused=%d reason=%d malformed=%d\n", (long long) n, (int) r.refused, (int) r.reason, (int) r.malformed ); return 1; }
        if ( r.unknown != 0 || r.kind_mismatch != 0 || r.clamped != 0 || r.duplicate != 0 || r.widened != 0 )
            { printf( "one more whole record moved a counter\n" ); return 1; }
    }
`)
}

// cpp/F12 — "second layout for a held hash". SPEC-TABLES §3.4: "A second layout
// for a hash already held is refused by name and changes nothing." The layout
// is named by its hash and the hash is taken as given (§5.3 step 4), so a
// second layout under a held hash is, at step 7, "a known hash whose bytes
// differ": layout_malformed, one name. Three vectors — the other lock entry's
// real layout under this build's hash, this build's layout under the older
// entry's hash, and every byte of the held layout bent in turn — plus the
// contrast of a hash held by no entry, which is layout_newer carrying the hash.
func cppT01SecondLayout(t *testing.T) {
	cppT01ProbeRow(t, "field_append", `
    {
        static uint8_t buf[1 << 20];
        static uint8_t orig[1 << 20];
        const int64_t ownL = kT01Identity.layout_bytes;
        const int64_t oldL = kT01Old.layout_bytes;
        int64_t blen, b;
        char who[64];

        blen = @@ROOT@@FixedSave( values, 1, buf, (int64_t) sizeof( buf ) );
        memcpy( buf + kTableFixedHeaderBytes + 4, kT01Old.layout, (size_t) oldL );
        TableFixedPut32( buf + kTableFixedHeaderBytes, (uint32_t) oldL );
        T01_RESET();
        n = @@ROOT@@FixedLoad( back, 8, buf, blen, plan, 4096, NULL, &r );
        T01_REFUSED( "own hash, the older entry's layout", layout_malformed, 0 );

        blen = @@ROOT@@FixedSave( values, 1, buf, (int64_t) sizeof( buf ) );
        TableFixedPut64( buf + kTableFixedHashAt, kT01Old.hash );
        T01_RESET();
        n = @@ROOT@@FixedLoad( back, 8, buf, blen, plan, 4096, NULL, &r );
        T01_REFUSED( "the older hash, this build's layout", layout_malformed, 0 );

        blen = @@ROOT@@FixedSave( values, 1, orig, (int64_t) sizeof( orig ) );
        for ( b = 0; b < ownL; ++b )
        {
            memcpy( buf, orig, (size_t) blen );
            buf[kTableFixedHeaderBytes + 4 + b] ^= 0x01;
            snprintf( who, sizeof( who ), "layout byte %lld bent", (long long) b );
            T01_RESET();
            n = @@ROOT@@FixedLoad( back, 8, buf, blen, plan, 4096, NULL, &r );
            T01_REFUSED( who, layout_malformed, 0 );
        }

        {
            const uint64_t stranger = kT01OwnHash ^ 1ull;
            blen = @@ROOT@@FixedSave( values, 1, buf, (int64_t) sizeof( buf ) );
            TableFixedPut64( buf + kTableFixedHashAt, stranger );
            T01_RESET();
            n = @@ROOT@@FixedLoad( back, 8, buf, blen, plan, 4096, NULL, &r );
            T01_REFUSED( "a hash in no entry", layout_newer, stranger );
        }
    }
`)
}

// cpp/F9 — "batch_too_large". Algorithm §5.3 step 10: "n := rest / record_bytes
// ; if n > capacity: REFUSE batch_too_large". A three-record file makes capacity
// 0, 1 and 2 each short and capacity 3 the boundary that reads. The refusal
// comes before any record is decoded, on the identity lane and on an older
// lineage entry's lane alike.
func cppT01BatchTooLarge(t *testing.T) {
	cppT01ProbeRow(t, "int_widen", `
    {
        const int64_t count = 3;
        int64_t c;
        char who[64];
        len = @@ROOT@@FixedSave( values, count, data, (int64_t) sizeof( data ) );
        if ( len < 20 ) { printf( "the writer produced no multi-record file\n" ); return 1; }
        for ( c = 0; c < count; ++c )
        {
            snprintf( who, sizeof( who ), "batch_too_large capacity=%lld of %lld", (long long) c, (long long) count );
            T01_RESET();
            n = @@ROOT@@FixedLoad( back, c, data, len, plan, 4096, NULL, &r );
            T01_REFUSED( who, batch_too_large, 0 );
        }
        T01_RESET();
        n = @@ROOT@@FixedLoad( back, count, data, len, plan, 4096, NULL, &r );
        if ( n != count || r.refused || r.malformed ) { printf( "capacity == count must read: n=%lld refused=%d malformed=%d\n", (long long) n, (int) r.refused, (int) r.malformed ); return 1; }

        {
            static uint8_t old[1 << 16];
            int64_t olen = kTableFixedHeaderBytes + 4 + kT01Old.layout_bytes + count * kT01Old.record_bytes;
            uint8_t * at;
            memset( old, 0, (size_t) olen );
            old[0] = kTableFixedForm;
            TableFixedPut64( old + kTableFixedHashAt, kT01Old.hash );
            TableFixedPut32( old + kTableFixedHeaderBytes, (uint32_t) kT01Old.layout_bytes );
            memcpy( old + kTableFixedHeaderBytes + 4, kT01Old.layout, (size_t) kT01Old.layout_bytes );
            at = old + kTableFixedHeaderBytes + 4 + kT01Old.layout_bytes;
            for ( c = 0; c < count; ++c ) { TableFixedPut64( at + c * kT01Old.record_bytes, kT01Old.hash ); }
            for ( c = 0; c < count; ++c )
            {
                snprintf( who, sizeof( who ), "batch_too_large (lineage) capacity=%lld of %lld", (long long) c, (long long) count );
                T01_RESET();
                n = @@ROOT@@FixedLoad( back, c, old, olen, plan, 4096, NULL, &r );
                T01_REFUSED( who, batch_too_large, 0 );
            }
        }
    }
`)
}
