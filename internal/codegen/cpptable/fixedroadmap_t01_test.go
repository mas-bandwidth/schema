package cpptable

// TestFixedRoadmapCppT01Framing is the cpp leg's card schema-cpp-t01-framing,
// rows file-envelope, batch-capacity and plan-selection of docs/roadmap.sexp's
// node `fixed-tables`. One subtest per task id, each driving the GENERATED C++
// reader (`<Struct>FixedLoad`) over the C++ reference's byte oracle.
// The laws are docs/FIXED-FORM-ALGORITHM.md §5.3 (the load's steps),
// §5.9 #7 (the report), §5.9 #47 (a runtime derives no hash) and
// docs/SPEC-TABLES.md §3.4.
//
// Tasks held by an existing test:
//   cpp/F7  TestFixedFormUnder20Bytes             (fixedform_under_20_bytes_test.go)
//   cpp/F8  TestFixedFormRaggedTail               (fixedform_ragged_tail_test.go)
//
// EVERY REFUSAL IS CHECKED AGAINST THE WHOLE REPORT: EXPECT_REFUSED checks
// refused set, the expected reason, layout_hash only on layout_newer/layout_unsupported,
// zero counters, malformed false, and the destination poisoned with 0x5A untouched.

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

var _ = ir.TableFixedRoots

const cppT01Macros = `
#define RESET_BACK() do { memset( back, 0x5A, sizeof( back ) ); memset( &r, 0, sizeof( r ) ); } while ( 0 )
#define CHECK_POISON( WHO ) do { const uint8_t * p_ = (const uint8_t *) back; int64_t i_; \
    for ( i_ = 0; i_ < (int64_t) sizeof( back ); ++i_ ) \
        if ( p_[i_] != 0x5A ) { printf( "%s: byte %lld is 0x%02x: a refusal wrote a destination byte\n", (WHO), (long long) i_, p_[i_] ); return 1; } } while ( 0 )
#define EXPECT_REFUSED( WHO, REASON, HASH ) do { TableReport e_; \
    memset( &e_, 0, sizeof( e_ ) ); e_.refused = true; e_.reason = (REASON); e_.layout_hash = (HASH); \
    if ( n != -1 ) { printf( "%s: n=%lld, not -1\n", (WHO), (long long) n ); return 1; } \
    if ( r.malformed ) { printf( "%s: malformed set beside a refusal by name\n", (WHO) ); return 1; } \
    if ( !r.refused || r.reason != (REASON) ) { printf( "%s: refused=%d reason=%d, not reason %d\n", (WHO), (int) r.refused, (int) r.reason, (int) (REASON) ); return 1; } \
    if ( memcmp( &r, &e_, sizeof( e_ ) ) != 0 ) { printf( "%s: the report is not exactly refused+reason(+layout_hash): u=%d km=%d c=%d d=%d w=%d layout_hash=%llx\n", (WHO), (int) r.unknown, (int) r.kind_mismatch, (int) r.clamped, (int) r.duplicate, (int) r.widened, (unsigned long long) r.layout_hash ); return 1; } \
    CHECK_POISON( WHO ); } while ( 0 )
#define EXPECT_MALFORMED( WHO ) do { TableReport e_; \
    memset( &e_, 0, sizeof( e_ ) ); e_.malformed = true; \
    if ( n != -1 ) { printf( "%s: n=%lld, not -1\n", (WHO), (long long) n ); return 1; } \
    if ( r.refused || r.reason != 0 ) { printf( "%s: refused=%d reason=%d beside malformed\n", (WHO), (int) r.refused, (int) r.reason ); return 1; } \
    if ( memcmp( &r, &e_, sizeof( e_ ) ) != 0 ) { printf( "%s: the report is not exactly malformed\n", (WHO) ); return 1; } \
    CHECK_POISON( WHO ); } while ( 0 )
`

func cppT01Forge(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func cppT01Split(t *testing.T, path string) (header, layout, records []byte) {
	t.Helper()
	d, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lb := int(binary.LittleEndian.Uint32(d[16:20]))
	return d[:16], d[20 : 20+lb], d[20+lb:]
}

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

func cppT01Probe(t *testing.T, readerPrefix, row, file, body string) {
	t.Helper()
	dir := t.TempDir()
	if readerPrefix == "VNEW_" {
		oldData, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", "VOLD_"+row+".schema"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "VOLD_"+row+".schema"), oldData, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rData, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", readerPrefix+row+".schema"))
	if err != nil {
		t.Fatal(err)
	}
	rPath := filepath.Join(dir, readerPrefix+row+".schema")
	if err := os.WriteFile(rPath, rData, 0o644); err != nil {
		t.Fatal(err)
	}

	u := loadUnit(t, rPath)
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

	root := cppFixedRootName(t, u)
	pkg := u.Package

	expandedBody := strings.NewReplacer(
		"@LOAD@", root+"FixedLoad",
		"@ROOT@", root,
		"@RESET@", root+"Reset",
		"@PKG@", pkg,
		"@SETFLOOR@", root+"FixedSetFloorForTest",
	).Replace(body)

	probe := fmt.Sprintf(`#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "%s"

using namespace %s;

%s

int main( void )
{
    static uint8_t data[1 << 20];
    int64_t len = 0;
    FILE * f = fopen( %q, "rb" );
    %s back[8];
    TableReport r;
    static TableFixedEntry plan[4096];
    TableFixedPlanCache cache;
    memset( &cache, 0, sizeof( cache ) );
    int64_t n = 0;
    uint64_t want = 0;
    if ( f != NULL )
    {
        len = (int64_t) fread( data, 1, sizeof( data ), f );
        fclose( f );
        if ( len >= 16 ) { memcpy( &want, data + 8, 8 ); }
    }
    RESET_BACK();
    (void) want;
    (void) cache;
%s
    return 0;
}
`, header, pkg, cppT01Macros, file, root, expandedBody)

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
	args := []string{"-std=c++17", "-O1", "-DSCHEMA_FIXED_FLOOR_TEST_HOOKS", "-I" + outDir, "-I" + serialize, filepath.Join(outDir, "probe.cpp"), "-o", filepath.Join(outDir, "probe")}
	build := exec.Command(cxx, args...)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the probe did not compile: %v\n%s", err, out)
	}
	out, err := exec.Command(filepath.Join(outDir, "probe")).CombinedOutput()
	if err != nil {
		t.Fatalf("the probe failed: %v\n%s", err, out)
	}
}

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
		{"cpp/F10", cppT01NoLayout},
		{"cpp/R7", cppT01IdentityLane},
		{"cpp/R8", cppT01LayoutNewer},
		{"cpp/R9/remaining-boundaries", cppT01KnownHashLie},
		{"cpp/R12", cppT01NoLayoutWritesNothing},
		{"cpp/R13", cppT01RefuseIsTotal},
	}
	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			t.Parallel()
			row.run(t, corpus)
		})
	}
}

// cpp/F4 — "layout_malformed, truncated". Algorithm §5.3 step 3:
// "L := LE(4, file+16) ; if 20 + L > len(file): REFUSE layout_malformed"
func cppT01LayoutMalformedTruncated(t *testing.T, corpus string) {
	newFile := filepath.Join(corpus, "new_field_append.bin")
	body := `
    {
        static uint8_t orig[1 << 20];
        const int64_t L = len;
        const int64_t lb = TableFixedGet32( data + kTableFixedHeaderBytes );
        memcpy( orig, data, (size_t) L );

        for ( int64_t k = kTableFixedHeaderBytes + 4; k < kTableFixedHeaderBytes + 4 + lb; ++k )
        {
            char who[64];
            snprintf( who, sizeof( who ), "cut at %lld of %lld", (long long) k, (long long) ( kTableFixedHeaderBytes + 4 + lb ) );
            RESET_BACK();
            n = @LOAD@( back, 8, data, k, plan, 4096, NULL, &r );
            EXPECT_REFUSED( who, layout_malformed, 0 );
        }

        RESET_BACK();
        n = @LOAD@( back, 8, data, kTableFixedHeaderBytes + 4 + lb, plan, 4096, NULL, &r );
        if ( n != 0 || r.refused || r.malformed || r.reason != 0 )
        {
            printf( "20 + lb with no records must read 0: n=%lld refused=%d malformed=%d\n", (long long) n, (int) r.refused, (int) r.malformed );
            return 1;
        }
    }
`
	cppT01Probe(t, "VNEW_", "field_append", newFile, body)
}

// cpp/F12 — "second layout for a held hash". SPEC-TABLES §3.4: "A second layout
// for a hash already held is refused by name and changes nothing." The layout
// is named by its hash and the hash is taken as given (§5.3 step 4), so a
// second layout under a held hash is, at step 7, "a known hash whose bytes
// differ": layout_malformed, one name, not layout_newer.
func cppT01SecondLayout(t *testing.T, corpus string) {
	newFile := filepath.Join(corpus, "new_field_append.bin")
	oldFile := filepath.Join(corpus, "old_field_append.bin")
	nh, nl, nr := cppT01Split(t, newFile)
	oh, ol, or := cppT01Split(t, oldFile)
	if bytes.Equal(nl, ol) {
		t.Fatal("the premise fails: the two lock entries carry one layout")
	}
	malformed := `
    {
        RESET_BACK();
        n = @LOAD@( back, 8, data, len, plan, 4096, NULL, &r );
        EXPECT_REFUSED( "a second layout under a held hash", layout_malformed, 0 );
    }
`
	t.Run("other_entry_layout_under_own_hash", func(t *testing.T) {
		t.Parallel()
		cppT01Probe(t, "VNEW_", "field_append", cppT01Forge(t, "f.bin", cppT01Join(nh, ol, nr)), malformed)
	})
	t.Run("own_layout_under_older_hash", func(t *testing.T) {
		t.Parallel()
		cppT01Probe(t, "VNEW_", "field_append", cppT01Forge(t, "f.bin", cppT01Join(oh, nl, or)), malformed)
	})
	t.Run("every_layout_byte_bent", func(t *testing.T) {
		t.Parallel()
		cppT01Probe(t, "VNEW_", "field_append", newFile, `
    {
        static uint8_t orig[1 << 20];
        const int64_t lb = TableFixedGet32( data + kTableFixedHeaderBytes );
        int64_t b;
        char who[64];
        memcpy( orig, data, (size_t) len );
        for ( b = 0; b < lb; ++b )
        {
            memcpy( data, orig, (size_t) len );
            data[kTableFixedHeaderBytes + 4 + b] ^= 0x01;
            snprintf( who, sizeof( who ), "layout byte %lld bent", (long long) b );
            RESET_BACK();
            n = @LOAD@( back, 8, data, len, plan, 4096, NULL, &r );
            EXPECT_REFUSED( who, layout_malformed, 0 );
        }
    }
`)
	})
	t.Run("contrast_hash_held_by_no_entry", func(t *testing.T) {
		t.Parallel()
		cppT01Probe(t, "VNEW_", "field_append", newFile, `
    {
        const uint64_t stranger = want ^ 1ull;
        memcpy( data + kTableFixedHashAt, &stranger, 8 );
        RESET_BACK();
        n = @LOAD@( back, 8, data, len, plan, 4096, NULL, &r );
        EXPECT_REFUSED( "a hash in no entry", layout_newer, stranger );
    }
`)
	})
}

// cpp/F9 — "batch_too_large". Algorithm §5.3 step 10:
// "n := rest / record_bytes ; if n > capacity: REFUSE batch_too_large"
func cppT01BatchTooLarge(t *testing.T, corpus string) {
	body := `
    {
        RESET_BACK();
        n = @LOAD@( back, 8, data, len, plan, 4096, NULL, &r );
        const int64_t count = n;
        int64_t c;
        char who[64];
        if ( count < 1 || count > 8 ) { printf( "the corpus file carries no record: n=%lld\n", (long long) count ); return 1; }
        for ( c = 0; c < count; ++c )
        {
            snprintf( who, sizeof( who ), "batch_too_large capacity=%lld of %lld", (long long) c, (long long) count );
            RESET_BACK();
            n = @LOAD@( back, c, data, len, plan, 4096, NULL, &r );
            EXPECT_REFUSED( who, batch_too_large, 0 );
        }
        RESET_BACK();
        n = @LOAD@( back, count, data, len, plan, 4096, NULL, &r );
        if ( n != count || r.refused || r.malformed || r.reason != 0 )
        {
            printf( "capacity == count must read: n=%lld refused=%d reason=%d\n", (long long) n, (int) r.refused, (int) r.reason );
            return 1;
        }
    }
`
	for _, lane := range []struct{ name, file string }{
		{"identity", "new_int_widen.bin"}, {"lineage", "old_int_widen.bin"},
	} {
		t.Run(lane.name, func(t *testing.T) {
			t.Parallel()
			cppT01Probe(t, "VNEW_", "int_widen", filepath.Join(corpus, lane.file), body)
		})
	}
}

// cpp/F10 — "no_layout". Algorithm §5.3 step 11:
// "per record, at at: if LE(8, at) != h: REFUSE no_layout -- BEFORE any byte is landed"
func cppT01NoLayout(t *testing.T, corpus string) {
	body := `
    {
        const int64_t lb = TableFixedGet32( data + kTableFixedHeaderBytes );
        data[kTableFixedHeaderBytes + 4 + lb] ^= 0xFF;
        RESET_BACK();
        n = @LOAD@( back, 8, data, len, plan, 4096, NULL, &r );
        EXPECT_REFUSED( "forged record hash", no_layout, 0 );
    }
`
	for _, lane := range []struct{ name, file string }{
		{"identity", "new_field_append.bin"}, {"lineage", "old_field_append.bin"},
	} {
		t.Run(lane.name, func(t *testing.T) {
			t.Parallel()
			cppT01Probe(t, "VNEW_", "field_append", filepath.Join(corpus, lane.file), body)
		})
	}
}

// cpp/R7 — "the identity lane is an index comparison, never a recomputed hash".
// Algorithm §5.3 step 8 and §5.9 #47: "a runtime NEVER computes a hash from
// layout bytes it holds — not for the IDENTITY LANE".
func cppT01IdentityLane(t *testing.T, corpus string) {
	newFile := filepath.Join(corpus, "new_cfloat_res_refine.bin")
	oldFile := filepath.Join(corpus, "old_cfloat_res_refine.bin")

	t.Run("no_hash_computation_in_the_unit", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		oldData, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", "VOLD_cfloat_res_refine.schema"))
		if err != nil {
			t.Fatal(err)
		}
		newData, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", "VNEW_cfloat_res_refine.schema"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "VOLD_cfloat_res_refine.schema"), oldData, 0o644); err != nil {
			t.Fatal(err)
		}
		newPath := filepath.Join(dir, "VNEW_cfloat_res_refine.schema")
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
		seen := 0
		for _, data := range files {
			seen += strings.Count(string(data), "TableFixedHashOf")
		}
		if seen != 1 {
			t.Fatalf("TableFixedHashOf appears %d times in the generated unit, want 1 (its definition): a runtime derives no hash (§5.9 #47)", seen)
		}
	})

	_, nl, _ := cppT01Split(t, newFile)
	_, ol, _ := cppT01Split(t, oldFile)
	nd, _ := os.ReadFile(newFile)
	od, _ := os.ReadFile(oldFile)
	if !bytes.Equal(nl, ol) || bytes.Equal(nd[8:16], od[8:16]) {
		t.Fatal("the premise fails: cfloat_res_refine must carry equal layout bytes under two hashes")
	}

	t.Run("own_hash_takes_the_identity_lane", func(t *testing.T) {
		t.Parallel()
		cppT01Probe(t, "VNEW_", "cfloat_res_refine", newFile, `
    RESET_BACK();
    n = @LOAD@( back, 8, data, len, plan, 4096, &cache, &r );
    if ( n != 1 || r.refused || r.malformed ) { printf( "own file: n=%lld refused=%d malformed=%d\n", (long long) n, (int) r.refused, (int) r.malformed ); return 1; }
    if ( cache.compiles != 0 ) { printf( "the identity lane compiled a plan: the entry was not recognised as this build's own\n" ); return 1; }
`)
	})

	t.Run("equal_bytes_older_hash_takes_the_entrys_lane", func(t *testing.T) {
		t.Parallel()
		cppT01Probe(t, "VNEW_", "cfloat_res_refine", oldFile, `
    RESET_BACK();
    n = @LOAD@( back, 8, data, len, plan, 4096, &cache, &r );
    if ( n != 1 || r.refused || r.malformed ) { printf( "older file: n=%lld refused=%d malformed=%d\n", (long long) n, (int) r.refused, (int) r.malformed ); return 1; }
    if ( cache.compiles == 0 ) { printf( "equal layout bytes under a different hash took the identity lane: the reader matched itself by its bytes\n" ); return 1; }
`)
	})
}

// cpp/R8 — "a hash in no lineage entry → layout_newer, reporting the file's hash
// AND NOTHING ELSE". Algorithm §5.3 step 5, §5.9 #7.
func cppT01LayoutNewer(t *testing.T, corpus string) {
	newFile := filepath.Join(corpus, "new_field_append.bin")
	h, l, _ := cppT01Split(t, newFile)

	t.Run("corpus_file", func(t *testing.T) {
		t.Parallel()
		cppT01Probe(t, "VOLD_", "field_append", newFile, `
    RESET_BACK();
    n = @LOAD@( back, 8, data, len, plan, 4096, NULL, &r );
    EXPECT_REFUSED( "old reader, new writer's file", layout_newer, want );
    if ( want == 0 ) { printf( "the corpus hash is zero: the test cannot tell the file's hash from nothing\n" ); return 1; }
`)
	})

	t.Run("stranger_hashes", func(t *testing.T) {
		t.Parallel()
		cppT01Probe(t, "VOLD_", "field_append", cppT01Forge(t, "f.bin", cppT01Join(h, l, nil)), `
    {
        const uint64_t strangers[] = { 1ull, 0xDEADBEEFCAFEF00Dull, ~0ull, want ^ ( 1ull << 63 ) };
        int c;
        char who[64];
        for ( c = 0; c < 4; ++c )
        {
            memcpy( data + kTableFixedHashAt, &strangers[c], 8 );
            snprintf( who, sizeof( who ), "stranger hash %d", c );
            RESET_BACK();
            n = @LOAD@( back, 8, data, len, plan, 4096, NULL, &r );
            EXPECT_REFUSED( who, layout_newer, strangers[c] );
        }
    }
`)
	})
}

// cpp/R9/remaining-boundaries — "Select known layouts and refuse unsupported input".
// Algorithm §5.3 step 7, §1.1's table.
func cppT01KnownHashLie(t *testing.T, corpus string) {
	body := `
    {
        static uint8_t orig[1 << 20];
        const int64_t L = len;
        const int64_t lb = TableFixedGet32( data + kTableFixedHeaderBytes );
        uint8_t * lay;
        int64_t flen;
        int c, k;
        char who[64];
        const char * names[] = {
            "rule 1 count mismatch", "rule 2 kind unknown", "rule 3 size mismatch", "rule 4 kind invalid",
            "rule 5 tree unclosed", "rule 6 record too large", "rule 7 too deep", "length under the header",
            "length zero", "length one entry short", "length one entry long"
        };
        memcpy( orig, data, (size_t) L );
        for ( c = 0; c < 11; ++c )
        {
            memcpy( data, orig, (size_t) L );
            lay = data + kTableFixedHeaderBytes + 4;
            flen = L;
            switch ( c )
            {
            case 0: TableFixedPut32( lay, TableFixedGet32( lay ) + 1 ); break;
            case 1: lay[4 + 17 * 1 + 8] = 31; break;
            case 2: TableFixedPut32( lay + 4 + 17 * 1 + 9, TableFixedGet32( lay + 4 + 17 * 1 + 9 ) + 3 ); break;
            case 3: lay[4 + 8] = 14; break;
            case 4: TableFixedPut32( lay + 4 + 13, TableFixedGet32( lay + 4 + 13 ) + 1 ); break;
            case 5: TableFixedPut32( lay + 4 + 9, 65537 ); break;
            case 6:
                TableFixedPut32( data + kTableFixedHeaderBytes, 4 + 17 * 66 );
                TableFixedPut32( lay, 66 );
                for ( k = 0; k < 66; ++k )
                {
                    uint8_t * e = lay + 4 + 17 * k;
                    memset( e, 0, 17 );
                    e[8] = ( k == 65 ) ? 6 : 13;
                    TableFixedPut32( e + 9, 1 );
                    TableFixedPut32( e + 13, ( k == 65 ) ? 0 : 1 );
                }
                flen = kTableFixedHeaderBytes + 4 + 4 + 17 * 66 + 16;
                memset( lay + 4 + 17 * 66, 0, 16 );
                break;
            case 7: TableFixedPut32( data + kTableFixedHeaderBytes, 3 ); break;
            case 8: TableFixedPut32( data + kTableFixedHeaderBytes, 0 ); break;
            case 9: TableFixedPut32( data + kTableFixedHeaderBytes, (uint32_t) ( lb - 17 ) ); break;
            case 10: TableFixedPut32( data + kTableFixedHeaderBytes, (uint32_t) ( lb + 17 ) ); memset( data + L, 0, 17 ); flen = L + 17; break;
            }
            snprintf( who, sizeof( who ), "%s", names[c] );
            RESET_BACK();
            n = @LOAD@( back, 8, data, flen, plan, 4096, NULL, &r );
            EXPECT_REFUSED( who, layout_malformed, 0 );
        }
    }
`
	for _, lane := range []struct{ name, file string }{
		{"identity", "new_field_append.bin"}, {"lineage", "old_field_append.bin"},
	} {
		t.Run(lane.name, func(t *testing.T) {
			t.Parallel()
			cppT01Probe(t, "VNEW_", "field_append", filepath.Join(corpus, lane.file), body)
		})
	}
}

// cpp/R12 — "the per-record hash check is before the prefill: no_layout writes nothing".
// Algorithm §5.3 step 11.
func cppT01NoLayoutWritesNothing(t *testing.T, corpus string) {
	oldFile := filepath.Join(corpus, "old_nested_append.bin")
	body := `
    {
        uint32_t lb = TableFixedGet32( data + kTableFixedHeaderBytes );
        int64_t off = kTableFixedHeaderBytes + 4 + (int64_t) lb;
        if ( len < off + 8 ) { printf( "no_layout_writes_nothing: record 0 out of bounds: len=%lld off=%lld\n", (long long) len, (long long) off ); return 1; }
        data[off] ^= 0xFF;
        RESET_BACK();
        n = @LOAD@( back, 8, data, len, plan, 4096, NULL, &r );
        EXPECT_REFUSED( "no_layout writes nothing", no_layout, 0 );
    }
`
	cppT01Probe(t, "VNEW_", "nested_append", oldFile, body)
}

// cpp/R13 — "REFUSE is total: refused+reason and malformed are never both set,
// every counter stays zero, and not one destination byte is written".
func cppT01RefuseIsTotal(t *testing.T, corpus string) {
	newFile := filepath.Join(corpus, "new_field_append.bin")
	oldFile := filepath.Join(corpus, "old_field_append.bin")

	t.Run("by_name_and_malformed", func(t *testing.T) {
		t.Parallel()
		cppT01Probe(t, "VNEW_", "field_append", newFile, `
    {
        static uint8_t orig[1 << 20];
        const int64_t L = len;
        const int64_t lb = TableFixedGet32( data + kTableFixedHeaderBytes );
        memcpy( orig, data, (size_t) L );

        data[0] = 1; RESET_BACK(); n = @LOAD@( back, 8, data, L, plan, 4096, NULL, &r );
        EXPECT_REFUSED( "previous_form", previous_form, 0 );
        memcpy( data, orig, (size_t) L );

        data[0] = 2; RESET_BACK(); n = @LOAD@( back, 8, data, L, plan, 4096, NULL, &r );
        EXPECT_REFUSED( "message_form_as_file", message_form_as_file, 0 );
        memcpy( data, orig, (size_t) L );

        data[0] = 9; RESET_BACK(); n = @LOAD@( back, 8, data, L, plan, 4096, NULL, &r );
        EXPECT_REFUSED( "newer_form", newer_form, 0 );
        memcpy( data, orig, (size_t) L );

        { const uint64_t stranger = want ^ 1ull; memcpy( data + kTableFixedHashAt, &stranger, 8 );
          RESET_BACK(); n = @LOAD@( back, 8, data, L, plan, 4096, NULL, &r );
          EXPECT_REFUSED( "layout_newer", layout_newer, stranger ); }
        memcpy( data, orig, (size_t) L );

        data[kTableFixedHeaderBytes + 4] ^= 0x01; RESET_BACK(); n = @LOAD@( back, 8, data, L, plan, 4096, NULL, &r );
        EXPECT_REFUSED( "layout_malformed", layout_malformed, 0 );
        memcpy( data, orig, (size_t) L );

        RESET_BACK(); n = @LOAD@( back, 0, data, L, plan, 4096, NULL, &r );
        EXPECT_REFUSED( "batch_too_large", batch_too_large, 0 );

        data[kTableFixedHeaderBytes + 4 + lb] ^= 0xFF; RESET_BACK(); n = @LOAD@( back, 8, data, L, plan, 4096, NULL, &r );
        EXPECT_REFUSED( "no_layout", no_layout, 0 );
        memcpy( data, orig, (size_t) L );

        RESET_BACK(); n = @LOAD@( back, 8, data, L + 1, plan, 4096, NULL, &r );
        EXPECT_MALFORMED( "ragged tail" );
        RESET_BACK(); n = @LOAD@( back, 8, data, kTableFixedHeaderBytes + 3, plan, 4096, NULL, &r );
        EXPECT_MALFORMED( "under twenty bytes" );
        data[3] = 1; RESET_BACK(); n = @LOAD@( back, 8, data, L, plan, 4096, NULL, &r );
        EXPECT_MALFORMED( "nonzero reserved byte" );
        memcpy( data, orig, (size_t) L );
    }
`)
	})

	t.Run("layout_unsupported", func(t *testing.T) {
		t.Parallel()
		cppT01Probe(t, "VNEW_", "field_append", oldFile, `
    @SETFLOOR@( 1 );
    RESET_BACK();
    n = @LOAD@( back, 8, data, len, plan, 4096, NULL, &r );
    EXPECT_REFUSED( "layout_unsupported", layout_unsupported, want );
`)
	})

	t.Run("plan_too_large", func(t *testing.T) {
		t.Parallel()
		cppT01Probe(t, "VNEW_", "field_append", oldFile, `
    RESET_BACK();
    n = @LOAD@( back, 8, data, len, plan, 0, NULL, &r );
    EXPECT_REFUSED( "plan_too_large", plan_too_large, 0 );
`)
	})
}
