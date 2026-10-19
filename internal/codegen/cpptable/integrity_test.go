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
    count uint16
}
table Plain
{
    n int64 | min = 0, max = 9007199254740993
    count uint16
}`

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
    static uint8_t lbuf[ 1 << 18 ];
    static uint8_t pbuf[ 1 << 18 ];
    Ledger l; LedgerReset( l );
    l.n = 1000; l.count = 7;
    Plain p; PlainReset( p );
    p.n = 1000; p.count = 7;
    const int64_t ml = LedgerMeasure( &l );
    const int64_t mp = PlainMeasure( &p );
    const int64_t sl = LedgerSave( &l, lbuf, (int64_t) sizeof( lbuf ) );
    const int64_t sp = PlainSave( &p, pbuf, (int64_t) sizeof( pbuf ) );
    if ( ml != mp + 4 ) { printf( "MEASURE %lld keyless %lld\n", (long long) ml, (long long) mp ); return 1; }
    if ( sl != ml || sp != mp ) { printf( "SAVE %lld %lld MEASURE %lld %lld\n", (long long) sl, (long long) sp, (long long) ml, (long long) mp ); return 1; }
    if ( sl < 5 ) { printf( "SHORT %lld\n", (long long) sl ); return 1; }
    hex( pbuf, sp, "PLAIN" );
    hex( lbuf, sl, "LEDGER" );
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
		// the declaring file is the keyless file plus its CRC-32C trailer
		if len(led) != len(plain)+4 {
			t.Fatalf("Ledger file is %d bytes, keyless %d, want keyless + 4 trailer bytes", len(led), len(plain))
		}
		if string(led[:len(plain)]) != string(plain) {
			t.Fatalf("the declaring file's body differs from the keyless file")
		}
		want := crc32.Checksum(plain, crc32.MakeTable(crc32.Castagnoli))
		got := binary.LittleEndian.Uint32(led[len(plain):])
		if got != want {
			t.Fatalf("trailer 0x%08x, want the CRC-32C of the body 0x%08x (Go hash/crc32 Castagnoli oracle)", got, want)
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
