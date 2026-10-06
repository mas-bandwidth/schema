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
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
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
		{"c/F9", cT01BatchTooLarge},
		{"c/F12", cT01SecondLayout},
		{"c/R7", cT01IdentityLane},
		{"c/R8", cT01LayoutNewer},
		{"c/R9", cT01KnownHashLie},
		{"c/R13", cT01RefuseIsTotal},
	}
	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			t.Parallel()
			row.run(t, corpus)
		})
	}
}

// c/F9 — "batch_too_large". Algorithm §5.3 step 6: "If `rest / record_bytes`
// passes the caller's capacity, `REFUSE batch_too_large`". new_int_widen.bin
// carries THREE records, so capacity 0, 1 and 2 are each short and capacity 3
// is the boundary that reads. The refusal comes before any record is decoded
// (nothing written, report exactly refused+reason), on the identity lane and on
// an older lineage entry's lane alike.
func cT01BatchTooLarge(t *testing.T, corpus string) {
	newer := cReadSchema(t, "VNEW_int_widen")
	older := cReadSchema(t, "VOLD_int_widen")
	body := `
    {
        const int64_t count = n; /* the probe's own read, capacity 8: the entry's record size is the lock's, not this build's */
        int64_t c;
        char who[64];
        if ( count < 1 || count > 8 ) { printf( "the corpus file carries no record\n" ); return 1; }
        for ( c = 0; c < count; ++c )
        {
            snprintf( who, sizeof( who ), "batch_too_large capacity=%lld of %lld", (long long) c, (long long) count );
            RESET_BACK();
            n = @LOAD@( back, c, data, len, plan, 4096, NULL, &r );
            EXPECT_REFUSED( who, SCHEMA_TABLE_BATCH_TOO_LARGE, 0 );
        }
        RESET_BACK();
        n = @LOAD@( back, count, data, len, plan, 4096, NULL, &r );
        if ( n != count || r.refused || r.malformed || r.reason != 0 ) { printf( "capacity == count must read: n=%lld refused=%d reason=%d\n", (long long) n, r.refused, r.reason ); return 1; }
    }
`
	for _, lane := range []struct{ name, file string }{
		{"identity", "new_int_widen.bin"}, {"lineage", "old_int_widen.bin"},
	} {
		t.Run(lane.name, func(t *testing.T) {
			t.Parallel()
			cT01Probe(t, newer, []string{older}, 0, filepath.Join(corpus, lane.file), body)
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

// c/R7 — "the identity lane is an index comparison, never a recomputed hash".
// Algorithm §5.3 step 8 and §5.9 #47: "a runtime NEVER computes a hash from
// layout bytes it holds — not for the IDENTITY LANE". Two proofs. (1) The
// generated unit calls `table_fixed_hash_of` nowhere: its one occurrence is
// the definition. (2) cfloat_res_refine's two entries have IDENTICAL layout
// bytes and DIFFERENT hashes (the resolution lives in the digest only), the
// case a reader that hashed its own bytes would conflate: reading this build's
// own file builds no lineage plan, and reading the older entry's file takes
// that entry's lane and builds it.
func cT01IdentityLane(t *testing.T, corpus string) {
	newer := cReadSchema(t, "VNEW_cfloat_res_refine")
	older := cReadSchema(t, "VOLD_cfloat_res_refine")
	newFile := filepath.Join(corpus, "new_cfloat_res_refine.bin")
	oldFile := filepath.Join(corpus, "old_cfloat_res_refine.bin")

	t.Run("no_hash_computation_in_the_unit", func(t *testing.T) {
		t.Parallel()
		u := cUnitOf(t, newer)
		o := cUnitOf(t, older)
		lineage := map[string][]FixedLineageEntry{}
		for _, st := range ir.TableFixedRoots(o) {
			e, ok := FixedLineageOf(o, st.Name)
			if !ok {
				t.Fatalf("no lineage entry for %s", st.Name)
			}
			lineage[st.Name] = append(lineage[st.Name], e)
		}
		files, err := cgen.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		tables, err := GenerateLineage(u, lineage)
		if err != nil {
			t.Fatal(err)
		}
		maps.Copy(files, tables)
		seen := 0
		for _, data := range files {
			seen += strings.Count(string(data), "table_fixed_hash_of")
		}
		if seen != 1 {
			t.Fatalf("table_fixed_hash_of appears %d times in the generated unit, want 1 (its definition): a runtime derives no hash (§5.9 #47)", seen)
		}
	})

	_, nl, _ := cT01Split(t, newFile)
	_, ol, _ := cT01Split(t, oldFile)
	nd, _ := os.ReadFile(newFile)
	od, _ := os.ReadFile(oldFile)
	if !bytes.Equal(nl, ol) || bytes.Equal(nd[8:16], od[8:16]) {
		t.Fatal("the premise fails: cfloat_res_refine must carry equal layout bytes under two hashes")
	}
	t.Run("own_hash_takes_the_identity_lane", func(t *testing.T) {
		t.Parallel()
		cT01Probe(t, newer, []string{older}, 0, newFile, `
    if ( n != 1 || r.refused || r.malformed ) { printf( "own file: n=%lld refused=%d malformed=%d\n", (long long) n, r.refused, r.malformed ); return 1; }
    if ( @SNAKE@_fixed_lineage_ready != SCHEMA_TABLE_FIXED_ONCE_UNBUILT ) { printf( "the identity lane built the lineage: the entry was not recognised as this build's own\n" ); return 1; }
`)
	})
	t.Run("equal_bytes_older_hash_takes_the_entrys_lane", func(t *testing.T) {
		t.Parallel()
		cT01Probe(t, newer, []string{older}, 0, oldFile, `
    if ( n != 1 || r.refused || r.malformed ) { printf( "older file: n=%lld refused=%d malformed=%d\n", (long long) n, r.refused, r.malformed ); return 1; }
    if ( @SNAKE@_fixed_lineage_ready == SCHEMA_TABLE_FIXED_ONCE_UNBUILT || @SNAKE@_fixed_lineage[0].entries == NULL )
        { printf( "equal layout bytes under a different hash took the identity lane: the reader matched itself by its bytes\n" ); return 1; }
`)
	})
}

// c/R8 — "a hash in no lineage entry → layout_newer, reporting the file's hash
// AND NOTHING ELSE" (algorithm §5.3 step 5, §5.9 #7). The OLD reader reads the
// widened writer's file; the report must equal refused + layout_newer + the
// FILE's hash, byte for byte, and not one destination byte may change. The hash
// alone decides: stranger hashes stamped over a file whose layout bytes are the
// lock's own, and a file cut after its layout (no record at all), answer the
// same.
func cT01LayoutNewer(t *testing.T, corpus string) {
	older := cReadSchema(t, "VOLD_field_append")
	newFile := filepath.Join(corpus, "new_field_append.bin")
	h, l, _ := cT01Split(t, newFile)
	t.Run("corpus_file", func(t *testing.T) {
		t.Parallel()
		cT01Probe(t, older, nil, 0, newFile, `
    RESET_BACK();
    n = @LOAD@( back, 8, data, len, plan, 4096, NULL, &r );
    EXPECT_REFUSED( "old reader, new writer's file", SCHEMA_TABLE_LAYOUT_NEWER, want );
    if ( want == 0 ) { printf( "the corpus hash is zero: the test cannot tell the file's hash from nothing\n" ); return 1; }
`)
	})
	t.Run("stranger_hashes", func(t *testing.T) {
		t.Parallel()
		cT01Probe(t, older, nil, 0, cT01Forge(t, "f.bin", cT01Join(h, l, nil)), `
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
            EXPECT_REFUSED( who, SCHEMA_TABLE_LAYOUT_NEWER, strangers[c] );
        }
    }
`)
	})
}

// c/R9 — "a known hash with a different layout length or bytes →
// layout_malformed; the seven §1.1 malformations under a known hash all come
// back as this one name" (algorithm §5.3 step 7, §1.1's table). Each of the
// seven rules is broken over the held hash, one at a time, plus four lengths:
// shorter than the 4-byte header, zero, one entry short, one entry long. Every
// answer is the one name, `layout_malformed`, as a refusal and not as
// `malformed`, and none of §1.1's seven refusal names appears. Both lanes: the
// reader's own hash and an older lineage entry's.
func cT01KnownHashLie(t *testing.T, corpus string) {
	newer := cReadSchema(t, "VNEW_field_append")
	older := cReadSchema(t, "VOLD_field_append")
	body := `
    {
        static uint8_t orig[1 << 20];
        const int64_t L = len;
        const int64_t lb = table_fixed_get32( data + kTableFixedHeaderBytes );
        uint8_t * lay;
        int64_t flen;
        int c, k;
        char who[64];
        const char * names[] = { "rule 1 count mismatch", "rule 2 kind unknown", "rule 3 size mismatch", "rule 4 kind invalid",
            "rule 5 tree unclosed", "rule 6 record too large", "rule 7 too deep", "length under the header", "length zero",
            "length one entry short", "length one entry long" };
        memcpy( orig, data, (size_t) L );
        for ( c = 0; c < 11; ++c )
        {
            memcpy( data, orig, (size_t) L );
            lay = data + kTableFixedHeaderBytes + 4;
            flen = L;
            switch ( c )
            {
            case 0: table_fixed_put32( lay, table_fixed_get32( lay ) + 1 ); break;
            case 1: lay[4 + 17 * 1 + 8] = 31; break;
            case 2: table_fixed_put32( lay + 4 + 17 * 1 + 9, table_fixed_get32( lay + 4 + 17 * 1 + 9 ) + 3 ); break;
            case 3: lay[4 + 8] = 14; break;
            case 4: table_fixed_put32( lay + 4 + 13, table_fixed_get32( lay + 4 + 13 ) + 1 ); break;
            case 5: table_fixed_put32( lay + 4 + 9, 65537 ); break;
            case 6:
                /* a chain of 66 entries: 65 single-child tables over one u8, deeper than the walk bound of 64 */
                table_fixed_put32( data + kTableFixedHeaderBytes, 4 + 17 * 66 );
                table_fixed_put32( lay, 66 );
                for ( k = 0; k < 66; ++k )
                {
                    uint8_t * e = lay + 4 + 17 * k;
                    memset( e, 0, 17 );
                    e[8] = ( k == 65 ) ? 6 : 13;
                    table_fixed_put32( e + 9, 1 );
                    table_fixed_put32( e + 13, ( k == 65 ) ? 0 : 1 );
                }
                flen = kTableFixedHeaderBytes + 4 + 4 + 17 * 66 + 16;
                memset( lay + 4 + 17 * 66, 0, 16 );
                break;
            case 7: table_fixed_put32( data + kTableFixedHeaderBytes, 3 ); break;
            case 8: table_fixed_put32( data + kTableFixedHeaderBytes, 0 ); break;
            case 9: table_fixed_put32( data + kTableFixedHeaderBytes, (uint32_t) ( lb - 17 ) ); break;
            case 10: table_fixed_put32( data + kTableFixedHeaderBytes, (uint32_t) ( lb + 17 ) ); memset( data + L, 0, 17 ); flen = L + 17; break;
            }
            snprintf( who, sizeof( who ), "%s", names[c] );
            RESET_BACK();
            n = @LOAD@( back, 8, data, flen, plan, 4096, NULL, &r );
            EXPECT_REFUSED( who, SCHEMA_TABLE_LAYOUT_MALFORMED, 0 );
        }
    }
`
	for _, lane := range []struct{ name, file string }{
		{"identity", "new_field_append.bin"}, {"lineage", "old_field_append.bin"},
	} {
		t.Run(lane.name, func(t *testing.T) {
			t.Parallel()
			cT01Probe(t, newer, []string{older}, 0, filepath.Join(corpus, lane.file), body)
		})
	}
}

// c/R13 — "REFUSE is total: refused+reason and malformed are never both set,
// every counter stays zero, and not one destination byte is written"
// (algorithm §5.9 #7's table). Every refusal name a file load can answer —
// previous_form, newer_form, message_form_as_file, layout_newer,
// layout_unsupported, layout_malformed, plan_too_large, batch_too_large,
// no_layout — is checked against the whole report, and the malformed answers
// (a ragged tail, under twenty bytes, a nonzero reserved byte) against the
// converse: `malformed` alone, `refused` and `reason` zero.
func cT01RefuseIsTotal(t *testing.T, corpus string) {
	newer := cReadSchema(t, "VNEW_field_append")
	older := cReadSchema(t, "VOLD_field_append")
	newFile := filepath.Join(corpus, "new_field_append.bin")
	oldFile := filepath.Join(corpus, "old_field_append.bin")
	t.Run("by_name_and_malformed", func(t *testing.T) {
		t.Parallel()
		cT01Probe(t, newer, []string{older}, 0, newFile, `
    {
        static uint8_t orig[1 << 20];
        const int64_t L = len;
        const int64_t lb = table_fixed_get32( data + kTableFixedHeaderBytes );
        memcpy( orig, data, (size_t) L );

        data[0] = 1; RESET_BACK(); n = @LOAD@( back, 8, data, L, plan, 4096, NULL, &r );
        EXPECT_REFUSED( "previous_form", SCHEMA_TABLE_PREVIOUS_FORM, 0 );
        memcpy( data, orig, (size_t) L );
        data[0] = 2; RESET_BACK(); n = @LOAD@( back, 8, data, L, plan, 4096, NULL, &r );
        EXPECT_REFUSED( "message_form_as_file", SCHEMA_TABLE_MESSAGE_FORM_AS_FILE, 0 );
        memcpy( data, orig, (size_t) L );
        data[0] = 9; RESET_BACK(); n = @LOAD@( back, 8, data, L, plan, 4096, NULL, &r );
        EXPECT_REFUSED( "newer_form", SCHEMA_TABLE_NEWER_FORM, 0 );
        memcpy( data, orig, (size_t) L );

        { const uint64_t stranger = want ^ 1ull; memcpy( data + kTableFixedHashAt, &stranger, 8 );
          RESET_BACK(); n = @LOAD@( back, 8, data, L, plan, 4096, NULL, &r );
          EXPECT_REFUSED( "layout_newer", SCHEMA_TABLE_LAYOUT_NEWER, stranger ); }
        memcpy( data, orig, (size_t) L );

        data[kTableFixedHeaderBytes + 4] ^= 0x01; RESET_BACK(); n = @LOAD@( back, 8, data, L, plan, 4096, NULL, &r );
        EXPECT_REFUSED( "layout_malformed", SCHEMA_TABLE_LAYOUT_MALFORMED, 0 );
        memcpy( data, orig, (size_t) L );

        RESET_BACK(); n = @LOAD@( back, 0, data, L, plan, 4096, NULL, &r );
        EXPECT_REFUSED( "batch_too_large", SCHEMA_TABLE_BATCH_TOO_LARGE, 0 );

        data[kTableFixedHeaderBytes + 4 + lb] ^= 0xFF; RESET_BACK(); n = @LOAD@( back, 8, data, L, plan, 4096, NULL, &r );
        EXPECT_REFUSED( "no_layout", SCHEMA_TABLE_NO_LAYOUT, 0 );
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
		cT01Probe(t, newer, []string{older}, 1, oldFile, `
    RESET_BACK();
    n = @LOAD@( back, 8, data, len, plan, 4096, NULL, &r );
    EXPECT_REFUSED( "layout_unsupported", SCHEMA_TABLE_LAYOUT_UNSUPPORTED, want );
`)
	})
	t.Run("plan_too_large", func(t *testing.T) {
		t.Parallel()
		cT01Probe(t, newer, []string{older}, 0, oldFile, `
    RESET_BACK();
    n = @LOAD@( back, 8, data, len, plan, 0, NULL, &r );
    EXPECT_REFUSED( "plan_too_large", SCHEMA_TABLE_PLAN_TOO_LARGE, 0 );
`)
	})
}
