package cpptable

// The integrity check (docs/SPEC-TABLES.md §2.14, §3.4 THE FRAMING): a table
// that declares `integrity = "crc32c"` saves its file with a CRC-32C trailer,
// and a read checks it before decoding. This mirrors the Go oracle
// internal/codegen/gotable/integrity_test.go TestGoWireIntegrityCheck and
// integrityfixed_test.go TestGoWireIntegrityFixedForm: the trailer bytes for
// the same values, and the flipped-bit rows and their names. The C++ emitter's
// generated save is compiled and run; the Go hash/crc32 Castagnoli table is the
// oracle the trailer is held to.

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

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const integritySchema = `package probe
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
}`

// integrityPlainGolden and integrityLedgerGolden are transcribed from the Go
// oracle internal/codegen/gotable/integrity_test.go (integrityPlainGolden and
// integrityLedgerGolden): PlainSave of {n 1000, label "ok", count 7}, and the
// same file for the declaring Ledger plus 0x1d23b551, its CRC-32C.
const integrityPlainGolden = "010105e803000000000000020c026f6b030707000071f801864ce363af3d62cb8fecfcf73974a279448ee2e5b10300000000000000"

const integrityLedgerGolden = integrityPlainGolden + "51b5231d"

const integrityFixedSchema = `package probe
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

const integrityPointerSchema = `package probe
table Root | integrity = "crc32c"
{
    entries []int32
}`

// A variable root exercises the separate region and LoadMeasure emitters.
const pointerFormDriver = `#include <stdio.h>
#include <stdint.h>
#include "HEADER"
using namespace probe;
int main()
{
    uint8_t other[1] = { kTableWireMessageForm };
    TableRefuseReason why = ok;
    if ( RootLoadMeasure( other, 1, NULL, &why ) != -1 || why != unknown_form )
        { printf( "MEASURE form 2 was misnamed\n" ); return 1; }
    TableReport report;
    if ( RootLoad( NULL, 0, other, 1, &report ) != NULL ||
         !report.refused || report.reason != message_form_as_file || report.damage_reason != ok )
        { printf( "LOAD form 2 was misnamed\n" ); return 1; }
    other[0] = 0xff;
    why = ok;
    if ( RootLoadMeasure( other, 1, NULL, &why ) != -1 || why != unknown_form )
        { printf( "MEASURE unknown form was misnamed\n" ); return 1; }
    TableReport unknown;
    if ( RootLoad( NULL, 0, other, 1, &unknown ) != NULL ||
         !unknown.refused || unknown.reason != newer_form || unknown.damage_reason != ok )
        { printf( "LOAD unknown form was misnamed\n" ); return 1; }
    why = ok;
    if ( RootLoadMeasure( other, 0, NULL, &why ) != -1 || why != ok )
        { printf( "MEASURE empty input was misnamed\n" ); return 1; }
    TableReport empty;
    if ( RootLoad( NULL, 0, other, 0, &empty ) != NULL ||
         !empty.malformed || empty.refused || empty.damage_reason != ok )
        { printf( "LOAD empty input was misnamed\n" ); return 1; }
    return 0;
}
`

// The fixed-form expected files are transcribed from the Go oracle's
// internal/codegen/gotable/integrityfixed_test.go: integrityFixedPlainGolden
// (the keyless file) and integrityFixedGolden (the declaring file). They are
// checked byte for byte, not merely for a trailer, because the two tables'
// layout hashes differ.
const integrityFixedPlainGolden = "030000000000000064f3fa8daeaf844448000000040000000deeaf527dc1c0970d1200000003000000071702864cf563af040400000000000000541502864cf463af0302000000000000003d62cb8fecfcf7390c0c0000000000000064f3fa8daeaf8444e80300000000020000006f6b00000000000064f3fa8daeaf8444e9030000f9ff00000000000000000000000064f3fa8daeaf8444ea030000f2ff080000006569676874206368"

const integrityFixedGolden = "03000000000000006938893b0968270148000000040000007b8c6cd3fc19b9b80d1200000003000000071702864cf563af040400000000000000541502864cf463af0302000000000000003d62cb8fecfcf7390c0c000000000000006938893b09682701e80300000000020000006f6b0000000000006938893b09682701e9030000f9ff0000000000000000000000006938893b09682701ea030000f2ff0800000065696768742063682854cdb3"

func generateCPPHeaders(t *testing.T, u *ir.Unit) (map[string][]byte, string) {
	t.Helper()
	files, err := cpp.Generate(u)
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

func writeAndCompile(t *testing.T, files map[string][]byte, header, driver string) ([]byte, error) {
	t.Helper()
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("the generated save path is C++: no c++ on PATH")
	}
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	main := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(main, []byte(driver), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := []string{"-std=c++17", "-O2", "-DNDEBUG", "-Wall", "-Wextra", "-Werror", "-Wshadow", "-I", dir, main, "-o", bin}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	return exec.Command(bin).CombinedOutput()
}

// variableDriver ports the Go oracle's TestIntegrity and flipAll
// (internal/codegen/gotable/integrity_test.go): the golden bytes for the same
// values, every byte of the file flipped by every single bit and by 0xff, and
// the strict read, the measure, the builder read and the tolerant read held to
// a refusal by name — unknown_form for the form byte, which is read first, and
// integrity_mismatch for every other byte. Whole pins the by-value reader and
// the retain rows pin MeasureRetain/SaveRetain (the Go oracle's
// emitIntegrityRetain).
const variableDriver = `#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "HEADER"

using namespace probe;

static void hex( const uint8_t * p, int64_t n, const char * name )
{
    printf( "%s ", name );
    for ( int64_t i = 0; i < n; ++i ) printf( "%02x", p[i] );
    printf( "\n" );
}

int main()
{
    static uint8_t region[ 1 << 18 ];
    static uint8_t rregion[ 1 << 18 ];
    static uint8_t lbuf[ 1 << 18 ];
    static uint8_t pbuf[ 1 << 18 ];
    static uint8_t rbuf[ 1 << 18 ];
    static uint8_t bad[ 1 << 18 ];
    static uint8_t store[ 1 << 18 ];
    static const uint8_t flips[9] = { 1, 2, 4, 8, 16, 32, 64, 128, 255 };

    LedgerBuilder lbb;
    Ledger * l = lbb.GetRoot();
    if ( l == NULL ) { printf( "Ledger builder root\n" ); return 1; }
    l->n = 1000; memcpy( l->label, "ok", 2 ); l->label_length = 2; l->count = 7;
    PlainBuilder pbb;
    Plain * p = pbb.GetRoot();
    if ( p == NULL ) { printf( "Plain builder root\n" ); return 1; }
    p->n = 1000; memcpy( p->label, "ok", 2 ); p->label_length = 2; p->count = 7;

    const int64_t ml = LedgerMeasure( lbb );
    const int64_t mp = PlainMeasure( pbb );
    const int64_t sl = LedgerSave( lbb, lbuf, (int64_t) sizeof( lbuf ) );
    const int64_t sp = PlainSave( pbb, pbuf, (int64_t) sizeof( pbuf ) );
    if ( ml != mp + 4 ) { printf( "MEASURE %lld keyless %lld\n", (long long) ml, (long long) mp ); return 1; }
    if ( sl != ml || sp != mp ) { printf( "SAVE %lld %lld MEASURE %lld %lld\n", (long long) sl, (long long) sp, (long long) ml, (long long) mp ); return 1; }
    hex( pbuf, sp, "PLAIN" );
    hex( lbuf, sl, "LEDGER" );

    TableReport clean;
    const Ledger * back = LedgerLoad( region, (int64_t) sizeof( region ), lbuf, sl, &clean );
    if ( back == NULL || back->n != 1000 || back->label_length != 2 ||
         memcmp( back->label, "ok", 2 ) != 0 || back->count != 7 ||
         clean.malformed || clean.refused || clean.damage_reason != ok )
        { printf( "LOAD clean round trip\n" ); return 1; }
    TableRefuseReason why = ok;
    if ( LedgerLoadMeasure( lbuf, sl, NULL, &why ) <= 0 || why != ok )
        { printf( "MEASURE clean file\n" ); return 1; }
    LedgerBuilder bload;
    TableReport brep;
    if ( !LedgerLoadBuilder( bload, lbuf, sl, &brep ) ||
         bload.GetRoot() == NULL || bload.GetRoot()->n != 1000 )
        { printf( "LOADBUILDER clean file\n" ); return 1; }
    if ( brep.malformed || brep.refused || brep.damage_reason != ok )
        { printf( "LOADBUILDER clean report\n" ); return 1; }

    // every byte by every single bit and by 0xff (Go integrity_test.go flipAll)
    for ( int64_t i = 0; i < sl; ++i )
    {
        for ( int f = 0; f < 9; ++f )
        {
            memcpy( bad, lbuf, (size_t) sl );
            bad[i] ^= flips[f];
            TableReport r;
            const Ledger * v = LedgerLoad( region, (int64_t) sizeof( region ), bad, sl, &r );
            if ( i == 0 )
            {
                if ( v != NULL || !r.refused || r.malformed || r.damage_reason != ok || r.reason != newer_form )
                    { printf( "LOAD form byte %lld ^ %u\n", (long long) i, (unsigned) flips[f] ); return 1; }
            }
            else if ( v != NULL || !r.malformed || r.refused || r.damage_reason != integrity_mismatch )
                { printf( "LOAD byte %lld ^ %u\n", (long long) i, (unsigned) flips[f] ); return 1; }
            TableRefuseReason rwhy = ok;
            const int64_t n = LedgerLoadMeasure( bad, sl, NULL, &rwhy );
            if ( n != -1 || rwhy != ( i == 0 ? unknown_form : integrity_mismatch ) )
                { printf( "MEASURE byte %lld ^ %u = %lld %d\n", (long long) i, (unsigned) flips[f], (long long) n, (int) rwhy ); return 1; }
            LedgerBuilder bflip;
            TableReport rb;
            const bool got = LedgerLoadBuilder( bflip, bad, sl, &rb );
            if ( i == 0 )
            {
                if ( got || !rb.refused || rb.malformed || rb.damage_reason != ok )
                    { printf( "LOADBUILDER form byte %lld ^ %u\n", (long long) i, (unsigned) flips[f] ); return 1; }
            }
            else if ( got || !rb.malformed || rb.refused || rb.damage_reason != integrity_mismatch )
                { printf( "LOADBUILDER byte %lld ^ %u\n", (long long) i, (unsigned) flips[f] ); return 1; }
        }
    }

    // a file too short to hold the trailer is integrity_mismatch (Go
    // integrity_test.go: LedgerReadStrict([]byte{1,0,0,0}))
    {
        const uint8_t tiny[4] = { 1, 0, 0, 0 };
        TableReport r;
        if ( LedgerLoad( region, (int64_t) sizeof( region ), tiny, 4, &r ) != NULL ||
             !r.malformed || r.refused || r.damage_reason != integrity_mismatch )
            { printf( "LOAD short file\n" ); return 1; }
        TableRefuseReason rwhy = ok;
        if ( LedgerLoadMeasure( tiny, 4, NULL, &rwhy ) != -1 || rwhy != integrity_mismatch )
            { printf( "MEASURE short file\n" ); return 1; }
        LedgerBuilder bshort;
        TableReport rb;
        if ( LedgerLoadBuilder( bshort, tiny, 4, &rb ) || !rb.malformed || rb.refused || rb.damage_reason != integrity_mismatch )
            { printf( "LOADBUILDER short file\n" ); return 1; }
    }

    // MeasureRetain and SaveRetain count and seal the trailer as the plain
    // pair does (the Go oracle's emitIntegrityRetain, gotable/integrity.go),
    // so SaveRetain's bytes are the golden file and LoadRetain reads them back.
    TableRetain retain;
    TableRetain::Id ids[16];
    memset( store, 0, sizeof( store ) );
    retain.bytes = store; retain.capacity = (int64_t) sizeof( store );
    retain.ids = ids; retain.id_capacity = 16;
    TableReport sreport;
    const int64_t mr = LedgerMeasureRetain( back, &retain );
    const int64_t sr = LedgerSaveRetain( back, &retain, rbuf, (int64_t) sizeof( rbuf ), &sreport );
    if ( mr != sl || sr != sl )
        { printf( "RETAIN MEASURE %lld SAVE %lld WANT %lld\n", (long long) mr, (long long) sr, (long long) sl ); return 1; }
    if ( memcmp( rbuf, lbuf, (size_t) sl ) != 0 )
        { printf( "RETAIN SAVE bytes differ from the golden\n" ); return 1; }
    TableRetain rback;
    TableRetain::Id rids[16];
    uint8_t rstore[ 1 << 18 ];
    rback.bytes = rstore; rback.capacity = (int64_t) sizeof( rstore );
    rback.ids = rids; rback.id_capacity = 16;
    TableReport rlreport;
    const Ledger * rl = LedgerLoadRetain( rregion, (int64_t) sizeof( rregion ), rbuf, sr, &rback, &rlreport );
    if ( rl == NULL || rl->n != 1000 || rl->count != 7 || rlreport.malformed || rlreport.refused || rlreport.damage_reason != ok )
        { printf( "LOADRETAIN round trip\n" ); return 1; }

    // a form-1 file too short for a trailer, and an empty file (damage with no
    // named cause), keep the form dispatch's answers (Go integrity_test.go).
    TableReport empty;
    if ( LedgerLoad( region, (int64_t) sizeof( region ), NULL, 0, &empty ) != NULL ||
         !empty.malformed || empty.refused || empty.damage_reason != ok )
        { printf( "LOAD empty input was misnamed\n" ); return 1; }

    // Whole is the by-value (non-variable) table: its save is sealed and its
    // strict read checks the trailer through the table reader.
    Whole wv; WholeReset( wv ); wv.n = 1000;
    uint8_t wbuf[ 256 ];
    uint8_t wbad[ 256 ];
    const int64_t wm = WholeMeasure( wv );
    const int64_t ws = WholeSave( wv, wbuf, (int64_t) sizeof( wbuf ) );
    if ( ws != wm || wm < 5 ) { printf( "WHOLE SAVE %lld MEASURE %lld\n", (long long) ws, (long long) wm ); return 1; }
    memcpy( wbad, wbuf, (size_t) ws ); wbad[ws - 1] ^= 1;
    Whole wback; TableReport wr;
    if ( WholeLoad( wback, wbad, ws, &wr ) ||
         !wr.malformed || wr.refused || wr.damage_reason != integrity_mismatch )
        { printf( "WHOLE LOAD corrupt trailer\n" ); return 1; }
    return 0;
}
`

const fixedDriver = `#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "HEADER"

using namespace probe;

static void hex( const uint8_t * p, int64_t n, const char * name )
{
    printf( "%s ", name );
    for ( int64_t i = 0; i < n; ++i ) printf( "%02x", p[i] );
    printf( "\n" );
}

int main()
{
    static uint8_t kbuf[ 1 << 18 ];
    static uint8_t pbuf[ 1 << 18 ];
    Reading rv[3];
    PlainReading pv[3];
    static const char * labels[3] = { "ok", "", "eight ch" };
    static const int32_t label_lengths[3] = { 2, 0, 8 };
    for ( int k = 0; k < 3; ++k )
    {
        ReadingReset( rv[k] ); rv[k].x = (int32_t)( 1000 + k ); rv[k].y = (int16_t)( -7 * k );
        memcpy( rv[k].label, labels[k], (size_t) label_lengths[k] ); rv[k].label_length = label_lengths[k];
        PlainReadingReset( pv[k] ); pv[k].x = rv[k].x; pv[k].y = rv[k].y;
        memcpy( pv[k].label, labels[k], (size_t) label_lengths[k] ); pv[k].label_length = label_lengths[k];
    }
    const int64_t mk = ReadingFixedMeasure( 3 );
    const int64_t mp = PlainReadingFixedMeasure( 3 );
    const int64_t sk = ReadingFixedSave( rv, 3, kbuf, (int64_t) sizeof( kbuf ) );
    const int64_t sp = PlainReadingFixedSave( pv, 3, pbuf, (int64_t) sizeof( pbuf ) );
    if ( mk != mp + 4 ) { printf( "MEASURE %lld keyless %lld\n", (long long) mk, (long long) mp ); return 1; }
    if ( sk != mk || sp != mp ) { printf( "SAVE %lld %lld MEASURE %lld %lld\n", (long long) sk, (long long) sp, (long long) mk, (long long) mp ); return 1; }
    hex( pbuf, sp, "PLAIN" );
    hex( kbuf, sk, "KEYED" );
    // THE READ (§2.14): the trailer's own last byte flipped, and the trailer cut
    // off, are both damage and land no record (the Go oracle's flipped-bit and
    // cut-trailer rows, integrityfixed_test.go).
    Reading back[3];
    TableReport report;
    uint8_t bad[1 << 18];
    memcpy( bad, kbuf, (size_t) sk ); bad[sk-1] ^= 0x01;
    if ( ReadingFixedLoad( back, 3, bad, sk, NULL, 0, NULL, &report ) >= 0 ||
         !report.malformed || report.refused || report.damage_reason != integrity_mismatch )
        { printf( "READ flipped trailer was not named damage\n" ); return 1; }
    TableReport report2;
    uint8_t cut[1 << 18];
    memcpy( cut, kbuf, (size_t) ( sk - 1 ) );
    if ( ReadingFixedLoad( back, 3, cut, sk - 1, NULL, 0, NULL, &report2 ) >= 0 ||
         !report2.malformed || report2.refused || report2.damage_reason != integrity_mismatch )
        { printf( "READ cut trailer was not named damage\n" ); return 1; }
    return 0;
}
`

func parseHexLine(out []byte, prefix string) ([]byte, bool) {
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, prefix) {
			body := strings.TrimSpace(strings.TrimPrefix(line, prefix))
			b, err := hex.DecodeString(body)
			if err != nil {
				return nil, false
			}
			return b, true
		}
	}
	return nil, false
}

func TestCPPWireIntegrityCheck(t *testing.T) {
	t.Parallel()

	t.Run("variable", func(t *testing.T) {
		t.Parallel()
		u := loadUnitFromSource(t, integritySchema)
		files, header := generateCPPHeaders(t, u)
		out, err := writeAndCompile(t, files, header, strings.Replace(variableDriver, "HEADER", header, 1))
		if err != nil {
			t.Fatalf("run: %v\n%s", err, out)
		}
		plain, ok := parseHexLine(out, "PLAIN")
		if !ok {
			t.Fatalf("no PLAIN line:\n%s", out)
		}
		led, ok := parseHexLine(out, "LEDGER")
		if !ok {
			t.Fatalf("no LEDGER line:\n%s", out)
		}
		// the Go oracle's golden files, byte for byte (integrity_test.go)
		if got := hex.EncodeToString(plain); got != integrityPlainGolden {
			t.Fatalf("Plain moved from the Go oracle's BASE: %s", got)
		}
		if got := hex.EncodeToString(led); got != integrityLedgerGolden {
			t.Fatalf("Ledger golden: %s", got)
		}
		if len(led) != len(plain)+4 {
			t.Fatalf("Ledger file is %d bytes, keyless %d, want keyless + 4 trailer bytes", len(led), len(plain))
		}
		want := crc32.Checksum(plain, crc32.MakeTable(crc32.Castagnoli))
		got := binary.LittleEndian.Uint32(led[len(plain):])
		if got != want {
			t.Fatalf("trailer 0x%08x, want the CRC-32C of the body 0x%08x (Go hash/crc32 Castagnoli oracle)", got, want)
		}
	})

	t.Run("variable form dispatch", func(t *testing.T) {
		t.Parallel()
		u := loadUnitFromSource(t, integrityPointerSchema)
		files, header := generateCPPHeaders(t, u)
		out, err := writeAndCompile(t, files, header, strings.Replace(pointerFormDriver, "HEADER", header, 1))
		if err != nil {
			t.Fatalf("run: %v\n%s", err, out)
		}
	})

	t.Run("fixed", func(t *testing.T) {
		t.Parallel()
		u := loadUnitFromSource(t, integrityFixedSchema)
		files, header := generateCPPHeaders(t, u)
		out, err := writeAndCompile(t, files, header, strings.Replace(fixedDriver, "HEADER", header, 1))
		if err != nil {
			t.Fatalf("run: %v\n%s", err, out)
		}
		plain, ok := parseHexLine(out, "PLAIN")
		if !ok {
			t.Fatalf("no PLAIN line:\n%s", out)
		}
		keyed, ok := parseHexLine(out, "KEYED")
		if !ok {
			t.Fatalf("no KEYED line:\n%s", out)
		}
		if len(keyed) != len(plain)+4 {
			t.Fatalf("Reading file is %d bytes, keyless %d, want keyless + 4 trailer bytes", len(keyed), len(plain))
		}
		if got := hex.EncodeToString(plain); got != integrityFixedPlainGolden {
			t.Fatalf("PlainReading moved from BASE: %s", got)
		}
		if got := hex.EncodeToString(keyed); got != integrityFixedGolden {
			t.Fatalf("Reading golden: %s", got)
		}
		want := crc32.Checksum(keyed[:len(keyed)-4], crc32.MakeTable(crc32.Castagnoli))
		got := binary.LittleEndian.Uint32(keyed[len(keyed)-4:])
		if got != want {
			t.Fatalf("trailer 0x%08x, want the CRC-32C of the body 0x%08x (Go hash/crc32 Castagnoli oracle)", got, want)
		}
	})
}

func loadUnitFromSource(t *testing.T, src string) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "probe.schema")
	if err := os.WriteFile(path, []byte(src), 0600); err != nil {
		t.Fatal(err)
	}
	return loadUnit(t, path)
}
