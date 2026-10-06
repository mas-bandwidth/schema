package ctable

// TestFixedRoadmapT01Framing is the c leg's card fixed-c-t01-framing, rows
// file-envelope, batch-capacity and plan-selection of docs/roadmap.sexp's node
// `fixed-tables`. One subtest per task id, each driving the GENERATED C reader
// (`<snake>_fixed_load`) through cRunVersionProbe over the C++ reference's byte
// oracle. The laws are docs/FIXED-FORM-ALGORITHM.md §5.3 (the load's steps),
// §5.9 #7 (the report), §5.9 #47 (a runtime derives no hash) and
// docs/SPEC-TABLES.md §3.4.
//
// Tasks held by an existing test carry no subtest here:
//
//	c/F8   TestCFixedFormRaggedTail             (fixedform_ragged_tail_test.go)
//	c/F10  TestFixedVersioningRefuseWritesNothing (the forged per-record hash)
//
// EVERY REFUSAL IS CHECKED AGAINST THE WHOLE REPORT, not against a list of
// fields: expectRefused builds the one report the law allows — refused set, the
// reason, `layout_hash` only where §5.9 #7 names it — and memcmps it with what
// the reader returned, so a counter that moves, a `malformed` that rises beside
// a refusal, or a stray `layout_hash` is a byte of difference. The destination
// is poisoned 0x5A first and every byte must still be 0x5A afterwards.

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// cT01Macros is the shared C: the poison, the reset, and the two answers a
// load may give. §5.9 #7's table is the source of both — a REFUSAL BY NAME is
// `refused` + `reason` (+ `layout_hash` on layout_newer / layout_unsupported)
// and nothing else; a MALFORMED read is `malformed` and nothing else.
const cT01Macros = `
#define RESET_BACK() do { memset( back, 0x5A, sizeof( back ) ); memset( &r, 0, sizeof( r ) ); } while ( 0 )
#define CHECK_POISON( WHO ) do { const uint8_t * p_ = (const uint8_t *) back; int64_t i_; \
    for ( i_ = 0; i_ < (int64_t) sizeof( back ); ++i_ ) \
        if ( p_[i_] != 0x5A ) { printf( "%s: byte %lld is 0x%02x: a refusal wrote a destination byte\n", (WHO), (long long) i_, p_[i_] ); return 1; } } while ( 0 )
#define EXPECT_REFUSED( WHO, REASON, HASH ) do { TableReport e_; \
    memset( &e_, 0, sizeof( e_ ) ); e_.refused = 1; e_.reason = (REASON); e_.layout_hash = (HASH); \
    if ( n != -1 ) { printf( "%s: n=%lld, not -1\n", (WHO), (long long) n ); return 1; } \
    if ( r.malformed ) { printf( "%s: malformed set beside a refusal by name\n", (WHO) ); return 1; } \
    if ( r.refused != 1 || r.reason != (REASON) ) { printf( "%s: refused=%d reason=%d, not reason %d\n", (WHO), r.refused, r.reason, (int) (REASON) ); return 1; } \
    if ( memcmp( &r, &e_, sizeof( e_ ) ) != 0 ) { printf( "%s: the report is not exactly refused+reason(+layout_hash): u=%d km=%d c=%d d=%d w=%d layout_hash=%llx\n", (WHO), r.unknown, r.kind_mismatch, r.clamped, r.duplicate, r.widened, (unsigned long long) r.layout_hash ); return 1; } \
    CHECK_POISON( WHO ); } while ( 0 )
#define EXPECT_MALFORMED( WHO ) do { TableReport e_; \
    memset( &e_, 0, sizeof( e_ ) ); e_.malformed = 1; \
    if ( n != -1 ) { printf( "%s: n=%lld, not -1\n", (WHO), (long long) n ); return 1; } \
    if ( r.refused || r.reason != 0 ) { printf( "%s: refused=%d reason=%d beside malformed\n", (WHO), r.refused, r.reason ); return 1; } \
    if ( memcmp( &r, &e_, sizeof( e_ ) ) != 0 ) { printf( "%s: the report is not exactly malformed\n", (WHO) ); return 1; } \
    CHECK_POISON( WHO ); } while ( 0 )
`

// cT01Body expands the load symbol into a probe body.
func cT01Body(t *testing.T, reader, body string) string {
	t.Helper()
	snake := ir.RustSnake(cFixedRootName(t, cUnitOf(t, reader)))
	return strings.NewReplacer("@LOAD@", snake+"_fixed_load", "@SNAKE@", snake).Replace(cT01Macros + body)
}

// cT01Forge writes a forged file and returns its path.
func cT01Forge(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// cT01Split is a corpus file's header, layout and records (§3.4).
func cT01Split(t *testing.T, path string) (header, layout, records []byte) {
	t.Helper()
	d, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lb := int(binary.LittleEndian.Uint32(d[16:20]))
	return d[:16], d[20 : 20+lb], d[20+lb:]
}

func cT01Join(header, layout, records []byte) []byte {
	var b bytes.Buffer
	b.Write(header)
	var n [4]byte
	binary.LittleEndian.PutUint32(n[:], uint32(len(layout)))
	b.Write(n[:])
	b.Write(layout)
	b.Write(records)
	return b.Bytes()
}

func cT01Probe(t *testing.T, reader string, older []string, retire int, file, body string) {
	t.Helper()
	out, err := cRunVersionProbe(t, reader, older, retire, file, cT01Body(t, reader, body), "")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestFixedRoadmapT01Framing(t *testing.T) {
	t.Parallel()
	corpus := cFixedCorpus(t)
	rows := []struct {
		id  string
		run func(t *testing.T, corpus string)
	}{
		{"c/F12", cT01SecondLayout},
	}
	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			t.Parallel()
			row.run(t, corpus)
		})
	}
}

// c/F12 — "second layout for a held hash". SPEC-TABLES §3.4: "A second layout
// for a hash already held is refused by name and changes nothing." The layout
// is named by its hash and the hash is taken as given (§5.3 step 4), so a
// second layout under a held hash is, at step 7, "a known hash whose bytes
// differ": layout_malformed, one name, not layout_newer. Three vectors: every
// byte of the held layout bent in turn; the OTHER lock entry's real layout
// under this build's hash; and this build's layout under the older entry's hash.
// The contrast is a hash held by no entry, which is layout_newer and carries
// the file's hash.
func cT01SecondLayout(t *testing.T, corpus string) {
	newer := cReadSchema(t, "VNEW_field_append")
	older := cReadSchema(t, "VOLD_field_append")
	newFile := filepath.Join(corpus, "new_field_append.bin")
	oldFile := filepath.Join(corpus, "old_field_append.bin")
	nh, nl, nr := cT01Split(t, newFile)
	oh, ol, or := cT01Split(t, oldFile)
	if bytes.Equal(nl, ol) {
		t.Fatal("the premise fails: the two lock entries carry one layout")
	}
	malformed := `
    {
        RESET_BACK();
        n = @LOAD@( back, 8, data, len, plan, 4096, NULL, &r );
        EXPECT_REFUSED( "a second layout under a held hash", SCHEMA_TABLE_LAYOUT_MALFORMED, 0 );
    }
`
	t.Run("other_entry_layout_under_own_hash", func(t *testing.T) {
		t.Parallel()
		cT01Probe(t, newer, []string{older}, 0, cT01Forge(t, "f.bin", cT01Join(nh, ol, nr)), malformed)
	})
	t.Run("own_layout_under_older_hash", func(t *testing.T) {
		t.Parallel()
		cT01Probe(t, newer, []string{older}, 0, cT01Forge(t, "f.bin", cT01Join(oh, nl, or)), malformed)
	})
	t.Run("every_layout_byte_bent", func(t *testing.T) {
		t.Parallel()
		cT01Probe(t, newer, []string{older}, 0, newFile, `
    {
        static uint8_t orig[1 << 20];
        const int64_t lb = table_fixed_get32( data + kTableFixedHeaderBytes );
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
            EXPECT_REFUSED( who, SCHEMA_TABLE_LAYOUT_MALFORMED, 0 );
        }
    }
`)
	})
	t.Run("contrast_hash_held_by_no_entry", func(t *testing.T) {
		t.Parallel()
		cT01Probe(t, newer, []string{older}, 0, newFile, `
    {
        const uint64_t stranger = want ^ 1ull;
        memcpy( data + kTableFixedHashAt, &stranger, 8 );
        RESET_BACK();
        n = @LOAD@( back, 8, data, len, plan, 4096, NULL, &r );
        EXPECT_REFUSED( "a hash in no entry", SCHEMA_TABLE_LAYOUT_NEWER, stranger );
    }
`)
	})
}
