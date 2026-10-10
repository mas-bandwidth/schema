package cpptable

// TestFixedRoadmapCppT01Framing is the cpp leg's card
// schema-cpp-t01-framing.w1, the rows file-envelope, batch-capacity and
// plan-selection of docs/roadmap.sexp's node `fixed-tables`. One subtest per
// task id, each driving the GENERATED C++ reader (`<Root>FixedLoad`) over the
// C++ reference's byte oracle in build/fixedform-corpus. The law is
// docs/FIXED-FORM-ALGORITHM.md §5.3 (LOAD: select by hash, never parse a
// stranger), its report table (the two answers, asserted jointly), §5.9 #47 (a
// runtime never derives a hash) and docs/SPEC-TABLES.md §3.4.
//
// Tasks this leg already holds carry no subtest here, and are named by the test
// that proves them:
//
//	cpp/F7   TestFixedFormUnder20Bytes             (fixedform_under_20_bytes_test.go)
//	cpp/F8   TestFixedFormRaggedTail               (fixedform_ragged_tail_test.go)
//	cpp/F10  TestFixedVersioningRefuseWritesNothing (the forged per-record hash)
//	cpp/R12  TestFixedVersioningRefuseWritesNothing (the poison survives the prefill)
//
// cpp/R9's `seven-corruptions` child is the reference's own witness
// (test/tables/versioning_numbers.cpp, PR #1002); this card proves the
// remaining boundaries, the known-hash length and byte boundaries.
//
// EVERY REFUSAL IS CHECKED AGAINST THE WHOLE REPORT, not against a list of
// fields: T01_REFUSED builds the one report §5.3's table allows — refused set,
// the reason, `layout_hash` only where the law names it — and memcmps it with
// what the reader returned, so a counter that moved, a `malformed` that rose
// beside a refusal by name, or a stray `layout_hash` is a byte of difference.
// The destination is poisoned 0x5A first and every byte must still be 0x5A
// afterwards, which is §5.3's "not one destination byte is written, the prefill
// included".

import (
	"bytes"
	"encoding/binary"
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

// cppT01Macros is the shared C++: the poison, and the two answers a load may
// give. §5.3's report table is the source of both — a REFUSAL BY NAME is
// `refused` + `reason` (+ `layout_hash` on layout_newer / layout_unsupported)
// and nothing else; a MALFORMED read is `malformed` and nothing else.
const cppT01Macros = `
#define T01_RESET() do { memset( back, 0x5A, sizeof( back ) ); memset( &r, 0, sizeof( r ) ); } while ( 0 )
#define T01_POISON( WHO ) do { const uint8_t * p_ = (const uint8_t *) back; int64_t i_; \
    for ( i_ = 0; i_ < (int64_t) sizeof( back ); ++i_ ) \
        if ( p_[i_] != 0x5A ) { printf( "%s: byte %lld is 0x%02x: a refusal wrote a destination byte\n", (WHO), (long long) i_, p_[i_] ); return 1; } } while ( 0 )
#define T01_REFUSED( WHO, REASON, HASH ) do { TableReport e_; \
    memset( &e_, 0, sizeof( e_ ) ); e_.refused = 1; e_.reason = (REASON); e_.layout_hash = (HASH); \
    if ( n != -1 ) { printf( "%s: n=%lld, not -1\n", (WHO), (long long) n ); return 1; } \
    if ( r.malformed ) { printf( "%s: malformed set beside a refusal by name\n", (WHO) ); return 1; } \
    if ( r.refused != 1 || r.reason != (REASON) ) { printf( "%s: refused=%d reason=%d, not reason %d\n", (WHO), r.refused, (int) r.reason, (int) (REASON) ); return 1; } \
    if ( memcmp( &r, &e_, sizeof( e_ ) ) != 0 ) { printf( "%s: the report is not exactly refused+reason(+layout_hash): u=%d km=%d c=%d d=%d w=%d layout_hash=%llx\n", (WHO), r.unknown, r.kind_mismatch, r.clamped, r.duplicate, r.widened, (unsigned long long) r.layout_hash ); return 1; } \
    T01_POISON( WHO ); } while ( 0 )
#define T01_MALFORMED( WHO ) do { TableReport e_; \
    memset( &e_, 0, sizeof( e_ ) ); e_.malformed = 1; \
    if ( n != -1 ) { printf( "%s: n=%lld, not -1\n", (WHO), (long long) n ); return 1; } \
    if ( r.refused || r.reason != 0 ) { printf( "%s: refused=%d reason=%d beside malformed\n", (WHO), r.refused, (int) r.reason ); return 1; } \
    if ( memcmp( &r, &e_, sizeof( e_ ) ) != 0 ) { printf( "%s: the report is not exactly malformed\n", (WHO) ); return 1; } \
    T01_POISON( WHO ); } while ( 0 )
`

// cppT01Generate copies the reader's schema and its older peers into one temp
// dir (the sibling-filename convention the nil lock reads), generates the
// packet and table headers and returns them with the unit.
func cppT01Generate(t *testing.T, reader string, older []string) (map[string][]byte, *ir.Unit) {
	t.Helper()
	dir := t.TempDir()
	read := func(base string) string {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", base+".schema"))
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, base+".schema")
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	readerPath := read(reader)
	for _, o := range older {
		read(o)
	}
	u := loadUnit(t, readerPath)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	return files, u
}

// cppT01Header is the one generated `<Base>Table.h` the probe includes.
func cppT01Header(t *testing.T, files map[string][]byte) string {
	t.Helper()
	for name := range files {
		if !strings.Contains(name, "/") && strings.HasSuffix(name, "Table.h") {
			return name
		}
	}
	t.Fatal("no Table.h was generated")
	return ""
}

// cppT01Run compiles a probe against the reader's generated header and runs it
// on `file`. `forge` is C++ that damages the bytes before the load; `body` is
// the assertions after it. The probe declares a poisoned `back[8]`, a zeroed
// report, a plan and a plan cache, and hands `want` (the file's own header hash)
// to the body.
func cppT01Run(t *testing.T, reader string, older []string, file, body, forge string) ([]byte, error) {
	t.Helper()
	files, u := cppT01Generate(t, reader, older)
	header := cppT01Header(t, files)
	if len(older) > 0 {
		text := string(files[header])
		if !strings.Contains(text, "FixedLineage[]") || strings.Count(text, "ull, //") <= 1 {
			t.Fatalf("the nil-lock convention picked up no older peer; the row cannot be probed:\n%s", text)
		}
	}
	root := cppFixedRootName(t, u)
	pkg := u.Package
	body = strings.NewReplacer("@LOAD@", root+"FixedLoad", "@RESET@", root+"Reset").Replace(body)

	outDir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(outDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	probe := fmt.Sprintf(`#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "%s"

using namespace %s;

%s

int main( void )
{
    static uint8_t data[1 << 20];
    int64_t len;
    FILE * f = fopen( %q, "rb" );
    %s back[8];
    TableReport r;
    uint64_t want;
    static TableFixedEntry plan[4096];
    static TableFixedEntry cache_storage[32768];
    TableFixedPlanCache cache;
    int64_t n;
    if ( f == NULL ) { printf( "the corpus file is missing\n" ); return 1; }
    len = (int64_t) fread( data, 1, sizeof( data ), f );
    fclose( f );
    if ( len < 20 ) { printf( "the corpus file is short\n" ); return 1; }
    memcpy( &want, data + kTableFixedHashAt, 8 );
%s
    memset( back, 0, sizeof( back ) );
    for ( int k = 0; k < 8; ++k ) { %sReset( back[k] ); }
    TableFixedPlanCacheInit( cache, cache_storage, 512 );
    memset( &r, 0, sizeof( r ) );
    n = %sFixedLoad( back, 8, data, len, plan, 4096, &cache, &r );
    (void) want;
%s
    return 0;
}
`, header, pkg, cppT01Macros, file, root, forge, root, root, body)

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

// cppT01Probe runs a probe whose failure is the test's failure.
func cppT01Probe(t *testing.T, reader string, older []string, file, body, forge string) {
	t.Helper()
	out, err := cppT01Run(t, reader, older, file, body, forge)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// cppT01Split is a corpus file's header, layout and records (§3.4).
func cppT01Split(t *testing.T, path string) (header, layout, records []byte) {
	t.Helper()
	d, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lb := int(binary.LittleEndian.Uint32(d[16:20]))
	return d[:16], d[20 : 20+lb], d[20+lb:]
}

// cppT01Join writes header, a fresh u32 length and layout, then records.
func cppT01Join(header, layout, records []byte) []byte {
	var b bytes.Buffer
	b.Write(header)
	var n [4]byte
	binary.LittleEndian.PutUint32(n[:], uint32(len(layout)))
	b.Write(n[:])
	b.Write(layout)
	b.Write(records)
	return b.Bytes()
}

// cppT01Forge writes a forged file and returns its path.
func cppT01Forge(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestFixedRoadmapCppT01Framing drives one subtest per task this card proves.
func TestFixedRoadmapCppT01Framing(t *testing.T) {
	t.Parallel()
	corpus := cppFixedCorpus(t)
	rows := []struct {
		id  string
		run func(t *testing.T, corpus string)
	}{
		{"cpp/F4", cppT01LayoutMalformedTruncated},
		{"cpp/F12", cppT01SecondLayout},
		{"cpp/F9", cppT01BatchTooLarge},
	}
	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			t.Parallel()
			row.run(t, corpus)
		})
	}
}

// cpp/F4 — "layout_malformed, truncated". Algorithm §5.3 step 3: "L :=
// LE(4, file+16) ; if 20 + L > len(file): REFUSE layout_malformed". The
// declared layout announcement is truncated two ways: the file is cut inside
// the announcement, and the announcement is written past the file's end. Both
// are refusals BY NAME and not `malformed` — the law's table puts `20 + L` past
// the file under layout_malformed, and a malformed read is a different answer.
func cppT01LayoutMalformedTruncated(t *testing.T, corpus string) {
	body := `
{
    static uint8_t orig[1 << 20];
    memcpy( orig, data, (size_t) len );
    /* (1) THE FILE IS CUT INSIDE ITS OWN LAYOUT ANNOUNCEMENT: 20 + L > bytes,
       and bytes is still >= 20 so step 2 has already passed. */
    T01_RESET();
    n = @LOAD@( back, 8, data, kTableFixedHeaderBytes + 4, plan, 4096, &cache, &r );
    T01_REFUSED( "a file cut inside its layout announcement", layout_malformed, 0 );
    /* (2) THE DECLARED LENGTH RUNS PAST THE FILE: the announcement is a lie
       about its own length, and the law refuses it by name. */
    TableFixedPut32( data + kTableFixedHeaderBytes, (uint32_t) len );
    T01_RESET();
    n = @LOAD@( back, 8, data, len, plan, 4096, &cache, &r );
    T01_REFUSED( "a declared layout length past the file", layout_malformed, 0 );
    memcpy( data, orig, (size_t) len );
}
`
	for _, lane := range []struct{ name, file string }{
		{"identity", "new_field_append.bin"}, {"lineage", "old_field_append.bin"},
	} {
		t.Run(lane.name, func(t *testing.T) {
			t.Parallel()
			cppT01Probe(t, "VNEW_field_append", []string{"VOLD_field_append"}, filepath.Join(corpus, lane.file), body, "")
		})
	}
}

// cpp/F12 — "second layout for a held hash". SPEC-TABLES §3.4: "a layout is
// NAMED BY ITS HASH and a second layout for a second hash amends nothing. A
// second layout for a hash already held is refused by name and changes
// nothing." The hash is taken as given (§5.3 step 4) and the bytes are compared
// against the held entry (§5.3 step 7), so a second layout under a held hash is
// layout_malformed — one name, not a re-announcement. Three vectors: the OTHER
// held entry's layout under this build's hash, this build's layout under the
// older entry's hash, and every byte of the held layout bent in turn.
func cppT01SecondLayout(t *testing.T, corpus string) {
	newFile := filepath.Join(corpus, "new_field_append.bin")
	oldFile := filepath.Join(corpus, "old_field_append.bin")
	nh, nl, nr := cppT01Split(t, newFile)
	oh, ol, or := cppT01Split(t, oldFile)
	if bytes.Equal(nl, ol) {
		t.Fatal("the premise fails: the two lock entries carry one layout")
	}
	malformed := `
    T01_RESET();
    n = @LOAD@( back, 8, data, len, plan, 4096, &cache, &r );
    T01_REFUSED( "a second layout under a held hash", layout_malformed, 0 );
`
	t.Run("other_entry_layout_under_own_hash", func(t *testing.T) {
		t.Parallel()
		cppT01Probe(t, "VNEW_field_append", []string{"VOLD_field_append"},
			cppT01Forge(t, "other_layout_under_own_hash.bin", cppT01Join(nh, ol, nr)), malformed, "")
	})
	t.Run("own_layout_under_older_hash", func(t *testing.T) {
		t.Parallel()
		cppT01Probe(t, "VNEW_field_append", []string{"VOLD_field_append"},
			cppT01Forge(t, "own_layout_under_older_hash.bin", cppT01Join(oh, nl, or)), malformed, "")
	})
	t.Run("every_layout_byte_bent", func(t *testing.T) {
		t.Parallel()
		cppT01Probe(t, "VNEW_field_append", []string{"VOLD_field_append"}, newFile, `
{
    static uint8_t orig[1 << 20];
    const uint32_t L = TableFixedGet32( data + kTableFixedHeaderBytes );
    uint32_t b;
    char who[80];
    memcpy( orig, data, (size_t) len );
    for ( b = 0; b < L; ++b )
    {
        memcpy( data, orig, (size_t) len );
        data[kTableFixedHeaderBytes + 4 + b] ^= 0x01;
        snprintf( who, sizeof( who ), "held layout byte %u bent", b );
        T01_RESET();
        n = @LOAD@( back, 8, data, len, plan, 4096, &cache, &r );
        T01_REFUSED( who, layout_malformed, 0 );
    }
    memcpy( data, orig, (size_t) len );
}
`, "")
	})
}

// cpp/F9 — "batch_too_large". Algorithm §5.3 step 10: "n := rest /
// record_bytes ; if n > capacity: REFUSE batch_too_large". old_int_widen.bin
// carries THREE records and new_int_widen.bin ONE, so capacity 0 (both lanes),
// 1 and 2 (the lineage lane) are each short and the file's own count is the
// boundary that reads. The refusal comes before any record is decoded on either
// lane: the report is exactly refused + reason, nothing is written, and the
// counters stay zero.
func cppT01BatchTooLarge(t *testing.T, corpus string) {
	body := `
{
    const int64_t count = n; /* the probe's own read, capacity 8: the entry's record size is the lock's */
    int64_t c;
    char who[96];
    if ( count < 1 || count > 8 ) { printf( "the corpus file carries no record\n" ); return 1; }
    for ( c = 0; c < count; ++c )
    {
        snprintf( who, sizeof( who ), "batch_too_large capacity=%lld of %lld", (long long) c, (long long) count );
        T01_RESET();
        n = @LOAD@( back, c, data, len, plan, 4096, &cache, &r );
        T01_REFUSED( who, batch_too_large, 0 );
    }
    T01_RESET();
    n = @LOAD@( back, count, data, len, plan, 4096, &cache, &r );
    if ( n != count || r.refused || r.malformed || r.reason != 0 ) { printf( "capacity == count must read: n=%lld refused=%d reason=%d\n", (long long) n, r.refused, (int) r.reason ); return 1; }
}
`
	for _, lane := range []struct{ name, file string }{
		{"identity", "new_int_widen.bin"}, {"lineage", "old_int_widen.bin"},
	} {
		t.Run(lane.name, func(t *testing.T) {
			t.Parallel()
			cppT01Probe(t, "VNEW_int_widen", []string{"VOLD_int_widen"}, filepath.Join(corpus, lane.file), body, "")
		})
	}
}
