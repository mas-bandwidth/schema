package ctable

import (
	"encoding/binary"
	"encoding/hex"
	"hash/crc32"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const cIntegritySchema = `package probe
table Ledger | integrity = "crc32c"
{
    n int64 | min = 0, max = 9007199254740993
    label string(16)
    count uint16
}
table Plain
{
    n int64 | min = 0, max = 9007199254740993
    label string(16)
    count uint16
}
fixed table Whole | integrity = "crc32c"
{
    n int64 | min = 0, max = 9007199254740993
}
fixed table FixedRow | integrity = "crc32c"
{
    value int32
}`

const cIntegrityPlainGolden = "010105e803000000000000020c026f6b030707000071f801864ce363af3d62cb8fecfcf73974a279448ee2e5b10300000000000000"
const cIntegrityLedgerGolden = cIntegrityPlainGolden + "51b5231d"

const cIntegrityFixedSchema = `package probe
fixed table Reading | integrity = "crc32c"
{
    x int32
    y int16
    label string(8)
}
fixed table PlainReading
{
    x int32
    y int16
    label string(8)
}`

const cIntegrityPointerSchema = `package probe
table Root | integrity = "crc32c"
{
    entries []int32
}`

const cPointerFormDriver = `#include <stdio.h>
#include <stdint.h>
#include "HEADER"

int main( void )
{
    uint8_t other[1] = { 2 };
    TableRefuseReason why = SCHEMA_TABLE_REFUSE_OK;
    TableReport report, unknown, empty;
    if ( root_load_measure_ex( other, 1, NULL, &why ) != -1 || why != SCHEMA_TABLE_REFUSE_UNKNOWN_FORM )
        { printf( "MEASURE form 2 was misnamed\n" ); return 1; }
    if ( root_load( NULL, 0, other, 1, &report ) != NULL ||
         !report.refused || report.reason != SCHEMA_TABLE_MESSAGE_FORM_AS_FILE || report.damage_reason != SCHEMA_TABLE_REFUSE_OK )
        { printf( "LOAD form 2 was misnamed\n" ); return 1; }
    other[0] = 0xff;
    why = SCHEMA_TABLE_REFUSE_OK;
    if ( root_load_measure_ex( other, 1, NULL, &why ) != -1 || why != SCHEMA_TABLE_REFUSE_UNKNOWN_FORM )
        { printf( "MEASURE unknown form was misnamed\n" ); return 1; }
    if ( root_load( NULL, 0, other, 1, &unknown ) != NULL ||
         !unknown.refused || unknown.reason != SCHEMA_TABLE_NEWER_FORM || unknown.damage_reason != SCHEMA_TABLE_REFUSE_OK )
        { printf( "LOAD unknown form was misnamed\n" ); return 1; }
    why = SCHEMA_TABLE_REFUSE_OK;
    if ( root_load_measure_ex( other, 0, NULL, &why ) != -1 || why != SCHEMA_TABLE_REFUSE_OK )
        { printf( "MEASURE empty input was misnamed\n" ); return 1; }
    if ( root_load( NULL, 0, other, 0, &empty ) != NULL ||
         !empty.malformed || empty.refused || empty.damage_reason != SCHEMA_TABLE_REFUSE_OK )
        { printf( "LOAD empty input was misnamed\n" ); return 1; }
    return 0;
}
`

const cIntegrityFixedPlainGolden = "030000000000000064f3fa8daeaf844448000000040000000deeaf527dc1c0970d1200000003000000071702864cf563af040400000000000000541502864cf463af0302000000000000003d62cb8fecfcf7390c0c0000000000000064f3fa8daeaf8444e80300000000020000006f6b00000000000064f3fa8daeaf8444e9030000f9ff00000000000000000000000064f3fa8daeaf8444ea030000f2ff080000006569676874206368"
const cIntegrityFixedGolden = "03000000000000006938893b0968270148000000040000007b8c6cd3fc19b9b80d1200000003000000071702864cf563af040400000000000000541502864cf463af0302000000000000003d62cb8fecfcf7390c0c000000000000006938893b09682701e80300000000020000006f6b0000000000006938893b09682701e9030000f9ff0000000000000000000000006938893b09682701ea030000f2ff0800000065696768742063682854cdb3"

const cVariableDriver = `#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "HEADER"

static void hex( const uint8_t * p, int64_t n, const char * name )
{
    int64_t i;
    printf( "%s ", name );
    for ( i = 0; i < n; ++i ) printf( "%02x", p[i] );
    printf( "\n" );
}

int main( void )
{
    static uint8_t region[ 1 << 18 ];
    static uint8_t rregion[ 1 << 18 ];
    static uint8_t lbuf[ 1 << 18 ];
    static uint8_t pbuf[ 1 << 18 ];
    static uint8_t rbuf[ 1 << 18 ];
    static uint8_t bad[ 1 << 18 ];
    static uint8_t store[ 1 << 18 ];
    static uint8_t rstore[ 1 << 18 ];
    static const uint8_t flips[9] = { 1, 2, 4, 8, 16, 32, 64, 128, 255 };

    LedgerBuilder lbb;
    Ledger * l;
    PlainBuilder pbb;
    Plain * p;
    TableCtx lctx = { 0 }, pctx = { 0 };
    int64_t ml, mp, sl, sp, i;
    int f;
    TableReport clean, brep, sreport, rlreport, empty, wr;
    const Ledger * back;
    const Ledger * rl;
    TableRefuseReason why = SCHEMA_TABLE_REFUSE_OK;
    LedgerBuilder bload;
    TableRetain retain, rback;
    TableRetainId ids[16], rids[16];
    int64_t mr, sr, wm, ws;
    Whole wv, wback;
    uint8_t wbuf[ 256 ], wbad[ 256 ];
    FixedRow fr, frback;
    uint8_t frbuf[ 256 ];
    int64_t frm, frs;

    if ( !ledger_builder_init( &lbb ) ) { return 1; }
    l = ledger_builder_root( &lbb );
    if ( l == NULL ) { return 1; }
    l->n = 1000; memcpy( l->label, "ok", 2 ); l->label_length = 2; l->count = 7;

    if ( !plain_builder_init( &pbb ) ) { return 1; }
    p = plain_builder_root( &pbb );
    if ( p == NULL ) { return 1; }
    p->n = 1000; memcpy( p->label, "ok", 2 ); p->label_length = 2; p->count = 7;

    lctx.arena = &lbb.arena;
    pctx.arena = &pbb.arena;
    ml = ledger_measure( &lctx, l );
    mp = plain_measure( &pctx, p );
    sl = ledger_save( &lctx, l, lbuf, (int64_t) sizeof( lbuf ) );
    sp = plain_save( &pctx, p, pbuf, (int64_t) sizeof( pbuf ) );
    ledger_builder_shutdown( &lbb );
    plain_builder_shutdown( &pbb );
    if ( ml != mp + 4 ) { printf( "MEASURE %lld keyless %lld\n", (long long) ml, (long long) mp ); return 1; }
    if ( sl != ml || sp != mp ) { printf( "SAVE %lld %lld MEASURE %lld %lld\n", (long long) sl, (long long) sp, (long long) ml, (long long) mp ); return 1; }
    hex( pbuf, sp, "PLAIN" );
    hex( lbuf, sl, "LEDGER" );

    back = ledger_load( region, (int64_t) sizeof( region ), lbuf, sl, &clean );
    if ( back == NULL || back->n != 1000 || back->label_length != 2 ||
         memcmp( back->label, "ok", 2 ) != 0 || back->count != 7 ||
         clean.malformed || clean.refused || clean.damage_reason != SCHEMA_TABLE_REFUSE_OK )
        { printf( "LOAD clean round trip\n" ); return 1; }
    if ( ledger_load_measure_ex( lbuf, sl, NULL, &why ) <= 0 || why != SCHEMA_TABLE_REFUSE_OK )
        { printf( "MEASURE clean file\n" ); return 1; }
    if ( !ledger_builder_init( &bload ) ) { return 1; }
    if ( !ledger_load_builder( &bload, lbuf, sl, &brep ) ||
         ledger_builder_root( &bload ) == NULL || ledger_builder_root( &bload )->n != 1000 )
        { printf( "LOADBUILDER clean file\n" ); return 1; }
    ledger_builder_shutdown( &bload );
    if ( brep.malformed || brep.refused || brep.damage_reason != SCHEMA_TABLE_REFUSE_OK )
        { printf( "LOADBUILDER clean report\n" ); return 1; }

    for ( i = 0; i < sl; ++i )
    {
        for ( f = 0; f < 9; ++f )
        {
            TableReport r, rb;
            const Ledger * v;
            TableRefuseReason rwhy = SCHEMA_TABLE_REFUSE_OK;
            int64_t n;
            LedgerBuilder bflip;
            int got;

            memcpy( bad, lbuf, (size_t) sl );
            bad[i] ^= flips[f];
            v = ledger_load( region, (int64_t) sizeof( region ), bad, sl, &r );
            if ( i == 0 )
            {
                if ( v != NULL || !r.refused || r.malformed || r.damage_reason != SCHEMA_TABLE_REFUSE_OK || r.reason != SCHEMA_TABLE_NEWER_FORM )
                    { printf( "LOAD form byte %lld ^ %u\n", (long long) i, (unsigned) flips[f] ); return 1; }
            }
            else if ( v != NULL || !r.malformed || r.refused || r.damage_reason != SCHEMA_TABLE_REFUSE_INTEGRITY_MISMATCH || r.refuse_reason != SCHEMA_TABLE_REFUSE_INTEGRITY_MISMATCH )
                { printf( "LOAD byte %lld ^ %u\n", (long long) i, (unsigned) flips[f] ); return 1; }

            n = ledger_load_measure_ex( bad, sl, NULL, &rwhy );
            if ( n != -1 || rwhy != ( i == 0 ? SCHEMA_TABLE_REFUSE_UNKNOWN_FORM : SCHEMA_TABLE_REFUSE_INTEGRITY_MISMATCH ) )
                { printf( "MEASURE byte %lld ^ %u = %lld %d\n", (long long) i, (unsigned) flips[f], (long long) n, (int) rwhy ); return 1; }

            if ( !ledger_builder_init( &bflip ) ) { return 1; }
            got = ledger_load_builder( &bflip, bad, sl, &rb );
            ledger_builder_shutdown( &bflip );
            if ( i == 0 )
            {
                if ( got || !rb.refused || rb.malformed || rb.damage_reason != SCHEMA_TABLE_REFUSE_OK )
                    { printf( "LOADBUILDER form byte %lld ^ %u\n", (long long) i, (unsigned) flips[f] ); return 1; }
            }
            else if ( got || !rb.malformed || rb.refused || rb.damage_reason != SCHEMA_TABLE_REFUSE_INTEGRITY_MISMATCH || rb.refuse_reason != SCHEMA_TABLE_REFUSE_INTEGRITY_MISMATCH )
                { printf( "LOADBUILDER byte %lld ^ %u\n", (long long) i, (unsigned) flips[f] ); return 1; }
        }
    }

    {
        const uint8_t tiny[4] = { 1, 0, 0, 0 };
        TableReport r, rb;
        TableRefuseReason rwhy = SCHEMA_TABLE_REFUSE_OK;
        LedgerBuilder bshort;
        int got;
        if ( ledger_load( region, (int64_t) sizeof( region ), tiny, 4, &r ) != NULL ||
             !r.malformed || r.refused || r.damage_reason != SCHEMA_TABLE_REFUSE_INTEGRITY_MISMATCH )
            { printf( "LOAD short file\n" ); return 1; }
        if ( ledger_load_measure_ex( tiny, 4, NULL, &rwhy ) != -1 || rwhy != SCHEMA_TABLE_REFUSE_INTEGRITY_MISMATCH )
            { printf( "MEASURE short file\n" ); return 1; }
        if ( !ledger_builder_init( &bshort ) ) { return 1; }
        got = ledger_load_builder( &bshort, tiny, 4, &rb );
        ledger_builder_shutdown( &bshort );
        if ( got || !rb.malformed || rb.refused || rb.damage_reason != SCHEMA_TABLE_REFUSE_INTEGRITY_MISMATCH )
            { printf( "LOADBUILDER short file\n" ); return 1; }
    }

    memset( &retain, 0, sizeof( retain ) );
    memset( store, 0, sizeof( store ) );
    retain.bytes = store; retain.capacity = (int64_t) sizeof( store );
    retain.ids = ids; retain.id_capacity = 16;
    memset( &sreport, 0, sizeof( sreport ) );
    mr = ledger_measure_retain( back, &retain );
    sr = ledger_save_retain( back, &retain, rbuf, (int64_t) sizeof( rbuf ), &sreport );
    if ( mr != sl || sr != sl )
        { printf( "RETAIN MEASURE %lld SAVE %lld WANT %lld\n", (long long) mr, (long long) sr, (long long) sl ); return 1; }
    if ( memcmp( rbuf, lbuf, (size_t) sl ) != 0 )
        { printf( "RETAIN SAVE bytes differ from the golden\n" ); return 1; }
    memset( &rback, 0, sizeof( rback ) );
    rback.bytes = rstore; rback.capacity = (int64_t) sizeof( rstore );
    rback.ids = rids; rback.id_capacity = 16;
    memset( &rlreport, 0, sizeof( rlreport ) );
    rl = ledger_load_retain( rregion, (int64_t) sizeof( rregion ), rbuf, sr, &rback, &rlreport );
    if ( rl == NULL || rl->n != 1000 || rl->count != 7 || rlreport.malformed || rlreport.refused || rlreport.damage_reason != SCHEMA_TABLE_REFUSE_OK )
        { printf( "LOADRETAIN round trip\n" ); return 1; }

    if ( ledger_load( region, (int64_t) sizeof( region ), NULL, 0, &empty ) != NULL ||
         !empty.malformed || empty.refused || empty.damage_reason != SCHEMA_TABLE_REFUSE_OK )
        { printf( "LOAD empty input was misnamed\n" ); return 1; }

    whole_reset( &wv ); wv.n = 1000;
    wm = whole_measure( &wv );
    ws = whole_save( &wv, wbuf, (int64_t) sizeof( wbuf ) );
    if ( ws != wm || wm < 5 ) { printf( "WHOLE SAVE %lld MEASURE %lld\n", (long long) ws, (long long) wm ); return 1; }
    memcpy( wbad, wbuf, (size_t) ws ); wbad[ws - 1] ^= 1;
    if ( whole_load( &wback, wbad, ws, &wr ) ||
         !wr.malformed || wr.refused || wr.damage_reason != SCHEMA_TABLE_REFUSE_INTEGRITY_MISMATCH || wr.refuse_reason != SCHEMA_TABLE_REFUSE_INTEGRITY_MISMATCH )
        { printf( "WHOLE LOAD corrupt trailer\n" ); return 1; }

    fixed_row_reset( &fr ); fr.value = 17;
    frm = fixed_row_measure( &fr );
    frs = fixed_row_save( &fr, frbuf, (int64_t) sizeof( frbuf ) );
    if ( frs != frm || frm != 28 ) { printf( "FIXEDROW SAVE %lld MEASURE %lld\n", (long long) frs, (long long) frm ); return 1; }
    hex( frbuf, frs, "FIXEDROW" );
    if ( !fixed_row_load( &frback, frbuf, frs, &wr ) || frback.value != 17 || wr.malformed || wr.refused )
        { printf( "FIXEDROW LOAD clean\n" ); return 1; }
    return 0;
}
`

const cFixedDriver = `#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "HEADER"

static void hex( const uint8_t * p, int64_t n, const char * name )
{
    int64_t i;
    printf( "%s ", name );
    for ( i = 0; i < n; ++i ) printf( "%02x", p[i] );
    printf( "\n" );
}

int main( void )
{
    static uint8_t kbuf[ 1 << 18 ];
    static uint8_t pbuf[ 1 << 18 ];
    static uint8_t bad[ 1 << 18 ];
    static uint8_t cut[ 1 << 18 ];
    Reading rv[3], back[3];
    PlainReading pv[3];
    static const char * labels[3] = { "ok", "", "eight ch" };
    static const int32_t label_lengths[3] = { 2, 0, 8 };
    int k;
    int64_t mk, mp, sk, sp;
    TableReport report, report2;

    for ( k = 0; k < 3; ++k )
    {
        reading_reset( &rv[k] ); rv[k].x = (int32_t)( 1000 + k ); rv[k].y = (int16_t)( -7 * k );
        memcpy( rv[k].label, labels[k], (size_t) label_lengths[k] ); rv[k].label_length = label_lengths[k];
        plain_reading_reset( &pv[k] ); pv[k].x = rv[k].x; pv[k].y = rv[k].y;
        memcpy( pv[k].label, labels[k], (size_t) label_lengths[k] ); pv[k].label_length = label_lengths[k];
    }
    mk = reading_fixed_measure( 3 );
    mp = plain_reading_fixed_measure( 3 );
    sk = reading_fixed_save( rv, 3, kbuf, (int64_t) sizeof( kbuf ) );
    sp = plain_reading_fixed_save( pv, 3, pbuf, (int64_t) sizeof( pbuf ) );
    if ( mk != mp + 4 ) { printf( "MEASURE %lld keyless %lld\n", (long long) mk, (long long) mp ); return 1; }
    if ( sk != mk || sp != mp ) { printf( "SAVE %lld %lld MEASURE %lld %lld\n", (long long) sk, (long long) sp, (long long) mk, (long long) mp ); return 1; }
    hex( pbuf, sp, "PLAIN" );
    hex( kbuf, sk, "KEYED" );

    memcpy( bad, kbuf, (size_t) sk ); bad[sk-1] ^= 0x01;
    if ( reading_fixed_load( back, 3, bad, sk, NULL, 0, NULL, &report ) >= 0 ||
         !report.malformed || report.refused || report.damage_reason != SCHEMA_TABLE_REFUSE_INTEGRITY_MISMATCH || report.refuse_reason != SCHEMA_TABLE_REFUSE_INTEGRITY_MISMATCH )
        { printf( "READ flipped trailer was not named damage\n" ); return 1; }
    memcpy( cut, kbuf, (size_t) ( sk - 1 ) );
    if ( reading_fixed_load( back, 3, cut, sk - 1, NULL, 0, NULL, &report2 ) >= 0 ||
         !report2.malformed || report2.refused || report2.damage_reason != SCHEMA_TABLE_REFUSE_INTEGRITY_MISMATCH || report2.refuse_reason != SCHEMA_TABLE_REFUSE_INTEGRITY_MISMATCH )
        { printf( "READ cut trailer was not named damage\n" ); return 1; }
    return 0;
}
`

func generateCIntegrityFiles(t *testing.T, u *ir.Unit) (map[string][]byte, string) {
	t.Helper()
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	var header string
	for name := range files {
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
	}
	if header == "" {
		t.Fatal("no Table.h generated")
	}
	return files, header
}

func writeAndCompileCIntegrity(t *testing.T, files map[string][]byte, driver string) ([]byte, error) {
	t.Helper()
	slowtest.Gate(t, "cc")
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("no cc on PATH")
	}
	dir := t.TempDir()
	var cFiles []string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".c") {
			cFiles = append(cFiles, p)
		}
	}
	main := filepath.Join(dir, "main.c")
	if err := os.WriteFile(main, []byte(driver), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := append([]string{"-std=c99", "-O2", "-DNDEBUG", "-Wall", "-Wextra", "-Werror", "-Wshadow", "-I", dir, main}, cFiles...)
	args = append(args, "-o", bin)
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	return exec.Command(bin).CombinedOutput()
}

func parseCHexLine(out []byte, prefix string) ([]byte, bool) {
	for line := range strings.SplitSeq(string(out), "\n") {
		if after, ok := strings.CutPrefix(line, prefix); ok {
			body := strings.TrimSpace(after)
			b, err := hex.DecodeString(body)
			if err != nil {
				return nil, false
			}
			return b, true
		}
	}
	return nil, false
}

const cIntegrityMessageSchema = `package probe
fixed table Ledger | integrity = "crc32c"
{
    n int32
    count uint16
}
fixed table Plain
{
    n int32
    count uint16
}
`

const cIntegrityMessagePlainGolden = "0200813e000020070000"
const cIntegrityMessageLedgerGolden = "0200813e00002007000078f2be34"

const cMessageDriver = `#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "HEADER"

static void hex( const uint8_t * p, int64_t n, const char * name )
{
    int64_t i;
    printf( "%s ", name );
    for ( i = 0; i < n; ++i ) printf( "%02x", p[i] );
    printf( "\n" );
}

int main( void )
{
    static const uint8_t flips[9] = { 1, 2, 4, 8, 16, 32, 64, 128, 255 };
    TableVocabulary vocab;
    TableMessageEntry entries[ kTableMessageEntriesHere ];
    uint8_t ann[ 4096 ], lb[ 256 ], pb[ 256 ], bad[ 256 ];
    int64_t an, ln, pn, count, i;
    int f;
    TableReport r;
    Ledger l, out[1];
    Plain p, pout[1];

    vocab = table_vocabulary( entries, kTableMessageEntriesHere );
    an = announce_measure();
    if ( an <= 0 || announce( ann, (int64_t) sizeof( ann ) ) != an ) { return 1; }
    memset( &r, 0, sizeof( r ) );
    if ( !announce_read( &vocab, ann, an, &r ) ) { return 1; }

    ledger_reset( &l ); l.n = 1000; l.count = 7;
    plain_reset( &p ); p.n = 1000; p.count = 7;
    ln = ledger_measure_messages( &l, 1, NULL );
    pn = plain_measure_messages( &p, 1, NULL );
    if ( ln < 0 || pn < 0 || ln != pn + 4 ) { return 1; }
    if ( ledger_save_messages( &l, 1, lb, ln, NULL ) != ln ) { return 1; }
    if ( plain_save_messages( &p, 1, pb, pn, NULL ) != pn ) { return 1; }
    if ( ledger_save_messages( &l, 1, lb, ln - 1, NULL ) >= 0 ) { return 1; }
    hex( pb, pn, "PLAIN" );
    hex( lb, ln, "LEDGER" );

    count = 1;
    if ( !ledger_load_messages( out, &count, &vocab, lb, ln, &r ) || count != 1 || out[0].n != 1000 || out[0].count != 7 ) { return 1; }
    count = 1;
    if ( !plain_load_messages( pout, &count, &vocab, pb, pn, &r ) || count != 1 || pout[0].n != 1000 || pout[0].count != 7 ) { return 1; }

    for ( i = 0; i < ln; ++i )
    {
        for ( f = 0; f < 9; ++f )
        {
            memcpy( bad, lb, (size_t) ln );
            bad[i] ^= flips[f];
            count = 1;
            if ( ledger_load_messages( out, &count, &vocab, bad, ln, &r ) || count != 0 ) { return 1; }
            if ( i == 0 )
            {
                if ( !r.refused || r.malformed || r.reason != SCHEMA_TABLE_NEWER_FORM || r.refuse_reason != SCHEMA_TABLE_REFUSE_UNKNOWN_FORM ) { return 1; }
            }
            else
            {
                if ( !r.malformed || r.refused || r.damage_reason != SCHEMA_TABLE_REFUSE_INTEGRITY_MISMATCH || r.refuse_reason != SCHEMA_TABLE_REFUSE_INTEGRITY_MISMATCH ) { return 1; }
            }
        }
    }
    return 0;
}
`

func TestCWireIntegrityCheck(t *testing.T) {
	t.Parallel()

	t.Run("variable", func(t *testing.T) {
		t.Parallel()
		u := cUnitOf(t, cIntegritySchema)
		files, header := generateCIntegrityFiles(t, u)
		out, err := writeAndCompileCIntegrity(t, files, strings.Replace(cVariableDriver, "HEADER", header, 1))
		if err != nil {
			t.Fatalf("run: %v\n%s", err, out)
		}
		plain, ok := parseCHexLine(out, "PLAIN")
		if !ok {
			t.Fatalf("no PLAIN line:\n%s", out)
		}
		led, ok := parseCHexLine(out, "LEDGER")
		if !ok {
			t.Fatalf("no LEDGER line:\n%s", out)
		}
		if got := hex.EncodeToString(plain); got != cIntegrityPlainGolden {
			t.Fatalf("Plain moved from the Go oracle's BASE: %s", got)
		}
		if got := hex.EncodeToString(led); got != cIntegrityLedgerGolden {
			t.Fatalf("Ledger golden: %s", got)
		}
		if len(led) != len(plain)+4 {
			t.Fatalf("Ledger file is %d bytes, keyless %d, want keyless + 4 trailer bytes", len(led), len(plain))
		}
		want := crc32.Checksum(plain, crc32.MakeTable(crc32.Castagnoli))
		got := binary.LittleEndian.Uint32(led[len(plain):])
		if got != want {
			t.Fatalf("trailer 0x%08x, want the CRC-32C of the body 0x%08x", got, want)
		}
		frow, ok := parseCHexLine(out, "FIXEDROW")
		if !ok {
			t.Fatalf("no FIXEDROW line:\n%s", out)
		}
		const wantFixedRowHex = "0101041100000000ea0ce83094fde47c0100000000000000db895d2b"
		if gotHex := hex.EncodeToString(frow); gotHex != wantFixedRowHex {
			t.Fatalf("FixedRow variable wire golden = %s, want %s", gotHex, wantFixedRowHex)
		}
	})

	t.Run("variable form dispatch", func(t *testing.T) {
		t.Parallel()
		u := cUnitOf(t, cIntegrityPointerSchema)
		files, header := generateCIntegrityFiles(t, u)
		out, err := writeAndCompileCIntegrity(t, files, strings.Replace(cPointerFormDriver, "HEADER", header, 1))
		if err != nil {
			t.Fatalf("run: %v\n%s", err, out)
		}
	})

	t.Run("fixed", func(t *testing.T) {
		t.Parallel()
		u := cUnitOf(t, cIntegrityFixedSchema)
		files, header := generateCIntegrityFiles(t, u)
		out, err := writeAndCompileCIntegrity(t, files, strings.Replace(cFixedDriver, "HEADER", header, 1))
		if err != nil {
			t.Fatalf("run: %v\n%s", err, out)
		}
		plain, ok := parseCHexLine(out, "PLAIN")
		if !ok {
			t.Fatalf("no PLAIN line:\n%s", out)
		}
		keyed, ok := parseCHexLine(out, "KEYED")
		if !ok {
			t.Fatalf("no KEYED line:\n%s", out)
		}
		if len(keyed) != len(plain)+4 {
			t.Fatalf("Reading file is %d bytes, keyless %d, want keyless + 4 trailer bytes", len(keyed), len(plain))
		}
		if got := hex.EncodeToString(plain); got != cIntegrityFixedPlainGolden {
			t.Fatalf("PlainReading moved from BASE: %s", got)
		}
		if got := hex.EncodeToString(keyed); got != cIntegrityFixedGolden {
			t.Fatalf("Reading golden: %s", got)
		}
		want := crc32.Checksum(keyed[:len(keyed)-4], crc32.MakeTable(crc32.Castagnoli))
		got := binary.LittleEndian.Uint32(keyed[len(keyed)-4:])
		if got != want {
			t.Fatalf("trailer 0x%08x, want the CRC-32C of the body 0x%08x", got, want)
		}
	})

	t.Run("message", func(t *testing.T) {
		t.Parallel()
		u := cUnitOf(t, cIntegrityMessageSchema)
		files, header := generateCIntegrityFiles(t, u)
		out, err := writeAndCompileCIntegrity(t, files, strings.Replace(cMessageDriver, "HEADER", header, 1))
		if err != nil {
			t.Fatalf("run: %v\n%s", err, out)
		}
		plain, ok := parseCHexLine(out, "PLAIN")
		if !ok {
			t.Fatalf("no PLAIN line:\n%s", out)
		}
		led, ok := parseCHexLine(out, "LEDGER")
		if !ok {
			t.Fatalf("no LEDGER line:\n%s", out)
		}
		if got := hex.EncodeToString(plain); got != cIntegrityMessagePlainGolden {
			t.Fatalf("Plain batch moved from BASE: %s", got)
		}
		if got := hex.EncodeToString(led); got != cIntegrityMessageLedgerGolden {
			t.Fatalf("Ledger batch golden: %s", got)
		}
	})
}
