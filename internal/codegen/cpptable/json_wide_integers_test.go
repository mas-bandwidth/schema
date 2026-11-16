package cpptable

// The JSON form's two integer rules (docs/SPEC-TABLES.md §16.2, §16.14 W10;
// docs/rulings.json json-wide-integers-are-strings and json-integer-token-only)
// as the C++ leg holds them. An integer whose storage is wider than 32 bits is
// written and read as a string of decimal digits; a
// kind of 32 bits and under stays a number. Every integer field takes an
// integer token only — no fraction, no exponent, no -0 — and its range is
// tested on the exact value. The rows are the vectors under
// testdata/wire/json-integers, and building the generated C++ makes this a
// functional test behind slowtest.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const (
	cppJsonIntegersDir = "../../../testdata/wire/json-integers"
	// cppJsonIntegersSerialize is the one thing the generated header takes from
	// serialize.h for a 128-bit field: the storage type. The JSON writer and
	// reader are self-contained, so the unit names no serialize stream and this
	// shim lets the probe compile without a sibling runtime checkout.
	cppJsonIntegersSerialize = `#pragma once

#include <cstdint>

namespace serialize
{
    using int128_t = __int128;
    using uint128_t = unsigned __int128;
}
`
	cppJsonIntegersSrc = `package jsonintegers

table Integers
{
    small   int32
    wide    int64
    uwide   uint64
    wide128 int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    ranged  int64 | min = 0, max = 9007199254740993
    frac    fixed(48, 16) | min = -10, max = 10
    counts  map[uint64]int32
}

enum Slot { Alpha, Beta }
union Arm { wide int64 }
table Sites {
    plain int64
    array [..3]int64
    opt ?uint64
    arm Arm
    vals map[uint64]uint64
    keyed [Slot]int64
    u128 uint128
    b33 bits(33)
    b64 bits(64)
    f128 fixed(64, 64) | min = -10, max = 10
    narrow int32
    fixed32 fixed(16, 16) | min = -10, max = 10
}
`
)

type cppJsonIntegersRow struct {
	name, file, verdict string
	numberToken         bool
}

func cppJsonIntegersRows(t *testing.T, manifest string, numberToken bool) []cppJsonIntegersRow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(cppJsonIntegersDir, manifest))
	if err != nil {
		t.Fatal(err)
	}
	var rows []cppJsonIntegersRow
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("%s/MANIFEST.tsv:%d: want name, root, json, verdict, description; got %d columns", cppJsonIntegersDir, i+1, len(fields))
		}
		if fields[3] != "read" && fields[3] != "refused" {
			t.Fatalf("%s/MANIFEST.tsv:%d: verdict %q, want read or refused", cppJsonIntegersDir, i+1, fields[3])
		}
		rows = append(rows, cppJsonIntegersRow{fields[0], fields[2], fields[3], numberToken})
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no rows")
	}
	return rows
}

const cppJsonIntegersProbe = `#include <cstdio>
#include <cstring>
#include <cstdint>
#include <cstdlib>
#include <vector>
#include "%s"
using namespace jsonintegers;

struct Row { const uint8_t * text; int64_t bytes; bool number_token; };

%s

static const Row kRows[] = {
%s
};

static const int kCases = (int) ( sizeof( kRows ) / sizeof( kRows[ 0 ] ) );

struct SiteCase { const char *name; const char *text; bool read; };
static const SiteCase kSites[] = {
    { "plain number", "{\"plain\":5}", 0 },
    { "plain string", "{\"plain\":\"5\"}", 1 },
    { "array number", "{\"array\":[\"1\",2]}", 0 },
    { "array string", "{\"array\":[\"1\",\"2\"]}", 1 },
    { "optional number", "{\"opt\":7}", 0 },
    { "optional string", "{\"opt\":\"7\"}", 1 },
    { "union number", "{\"arm\":{\"wide\":9}}", 0 },
    { "union string", "{\"arm\":{\"wide\":\"9\"}}", 1 },
    { "map value number", "{\"vals\":{\"18446744073709551615\":5}}", 0 },
    { "map value string", "{\"vals\":{\"18446744073709551615\":\"5\"}}", 1 },
    { "keyed number", "{\"keyed\":{\"Alpha\":5}}", 0 },
    { "keyed string", "{\"keyed\":{\"Alpha\":\"5\"}}", 1 },
    { "uint128 number", "{\"u128\":340282366920938463463374607431768211455}", 0 },
    { "uint128 string", "{\"u128\":\"340282366920938463463374607431768211455\"}", 1 },
    { "bits33 number", "{\"b33\":8589934591}", 0 },
    { "bits33 string", "{\"b33\":\"8589934591\"}", 1 },
    { "bits64 number", "{\"b64\":18446744073709551615}", 0 },
    { "bits64 string", "{\"b64\":\"18446744073709551615\"}", 1 },
    { "fixed128 number", "{\"f128\":0.5}", 0 },
    { "fixed128 string", "{\"f128\":\"0.5\"}", 1 },
    { "int32 number", "{\"narrow\":-7}", 1 },
    { "fixed32 exponent", "{\"fixed32\":15e-1}", 1 },
};

static int check_sites()
{
    int failures = 0;
    for ( const auto & c : kSites )
    {
        SitesBuilder builder;
        TableReport report;
        const bool ok = SitesFromJson( builder, c.text, (int64_t) std::strlen( c.text ), &report );
        if ( c.read ? ( !ok || report.refused ) : ( ok || !report.refused || report.refuse_reason != kind_mismatch ) )
        {
            std::fprintf( stderr, "FAIL site %%s: ok=%%d refused=%%d reason=%%d\n", c.name, (int) ok, (int) report.refused, (int) report.refuse_reason );
            failures++;
        }
    }
    return failures;
}

int main( void )
{
    const char * dir = getenv( "SCHEMA_JSON_INTEGERS_DIR" );
    if ( dir == NULL ) { std::fprintf( stderr, "SCHEMA_JSON_INTEGERS_DIR is unset\n" ); return 1; }
    static char text[ 1 << 16 ];
    for ( int i = 0; i < kCases; i++ )
    {
        IntegersBuilder builder;
        TableReport report;
        const bool ok = IntegersFromJson( builder, (const char *) kRows[ i ].text, kRows[ i ].bytes, &report );
        char path[ 1024 ];
        std::snprintf( path, sizeof( path ), "%%s/row-%%d", dir, i );
        FILE * f = std::fopen( path, "wb" );
        if ( f == NULL ) { std::fprintf( stderr, "case %%d: open failed\n", i ); return 1; }
        const char status = ok ? '1' : ( kRows[ i ].number_token && report.refuse_reason != kind_mismatch ? '2' : '0' );
        bool wrote = std::fwrite( &status, 1, 1, f ) == 1;
        if ( ok )
        {
            // ToJson writes a REGION's const root (§16.1), so the builder's
            // value goes over the wire and comes back as one before the writer
            // sees it — the same path a loaded file takes.
            const int64_t wire_bytes = IntegersMeasure( builder );
            if ( wire_bytes < 0 ) { std::fprintf( stderr, "case %%d: Measure refused\n", i ); return 1; }
            std::vector<uint8_t> wire( (size_t) wire_bytes );
            if ( IntegersSave( builder, wire.data(), (int64_t) wire.size() ) != wire_bytes ) { std::fprintf( stderr, "case %%d: Save wrote short\n", i ); return 1; }
            const int64_t region_bytes = IntegersLoadMeasure( wire.data(), (int64_t) wire.size() );
            if ( region_bytes < 0 ) { std::fprintf( stderr, "case %%d: LoadMeasure refused\n", i ); return 1; }
            std::vector<uint8_t> region( (size_t) region_bytes );
            TableReport load_report;
            const Integers * value = IntegersLoad( region.data(), (int64_t) region.size(), wire.data(), (int64_t) wire.size(), &load_report );
            if ( value == NULL ) { std::fprintf( stderr, "case %%d: Load refused a value the read accepted\n", i ); return 1; }
            const int64_t size = IntegersToJsonMeasure( value );
            if ( size < 0 || size > (int64_t) sizeof( text ) ) { std::fprintf( stderr, "case %%d: ToJsonMeasure refused (%%lld)\n", i, (long long) size ); return 1; }
            if ( IntegersToJson( value, text, (int64_t) sizeof( text ) ) != size ) { std::fprintf( stderr, "case %%d: ToJson wrote short\n", i ); return 1; }
            wrote = wrote && std::fwrite( text, 1, (size_t) size, f ) == (size_t) size;
        }
        if ( std::fclose( f ) != 0 || !wrote ) { std::fprintf( stderr, "case %%d: write failed\n", i ); return 1; }
    }
    if ( check_sites() ) { return 1; }
    std::printf( "ok %%d\n", kCases );
    return 0;
}
`

// TestCppJsonWideIntegersAreStrings holds the C++ JSON writer and reader to the
// two rules over every manifest row: each accept row reads and writes back the
// committed canonical text byte for byte, and each refuse row is refused.
func TestCppJsonWideIntegersAreStrings(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C++ compiler (it compiles and runs the generated unit)")
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	if _, err := exec.LookPath(cxx); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and %s is not found: %v", cxx, err)
		}
		t.Skipf("%s: %v", cxx, err)
	}
	rows := cppJsonIntegersRows(t, "MANIFEST.tsv", false)
	rows = append(rows, cppJsonIntegersRows(t, "NUMBER-TOKEN.tsv", true)...)
	canonical, err := os.ReadFile(filepath.Join(cppJsonIntegersDir, "canonical.read.json"))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitFromSource(t, cppJsonIntegersSrc)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatalf("cpp.Generate: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	maps.Copy(files, tables)
	src := t.TempDir()
	shim := filepath.Join(src, "shim")
	if err := os.Mkdir(shim, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shim, "serialize.h"), []byte(cppJsonIntegersSerialize), 0o600); err != nil {
		t.Fatal(err)
	}
	var header string
	var sources []string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(src, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
		if strings.HasSuffix(name, "Table.cpp") {
			sources = append(sources, filepath.Join(src, name))
		}
	}
	if header == "" || len(sources) == 0 {
		t.Fatalf("Generate emitted header %q and %d table sources for the probe unit", header, len(sources))
	}

	defs := make([]string, 0, len(rows))
	entries := make([]string, 0, len(rows))
	for i, row := range rows {
		body, err := os.ReadFile(filepath.Join(cppJsonIntegersDir, row.file))
		if err != nil {
			t.Fatal(err)
		}
		defs = append(defs, fmt.Sprintf("static const uint8_t text%d[] = { %s };", i, cppBytes(body)))
		entries = append(entries, fmt.Sprintf("\t{ text%d, (int64_t) sizeof( text%d ), %t }", i, i, row.numberToken))
	}
	probe := filepath.Join(src, "probe.cpp")
	probeSrc := fmt.Sprintf(cppJsonIntegersProbe, header, strings.Join(defs, "\n"), strings.Join(entries, ",\n"))
	if err := os.WriteFile(probe, []byte(probeSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(src, "probe")
	args := append([]string{"-std=c++17", "-O1", "-fsanitize=undefined", "-fno-sanitize-recover=undefined", "-I" + src, "-I" + shim, probe}, sources...)
	args = append(args, "-o", exe)
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("the probe did not compile: %v\n%s", err, out)
	}
	outDir := filepath.Join(src, "out")
	if err := os.Mkdir(outDir, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe)
	cmd.Env = append(os.Environ(), "SCHEMA_JSON_INTEGERS_DIR="+outDir)
	probeOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the C++ probe failed: %v\n%s", err, probeOut)
	}
	if strings.Contains(string(probeOut), "runtime error:") {
		t.Fatalf("the C++ probe reported undefined behaviour:\n%s", probeOut)
	}

	for i, row := range rows {
		raw, err := os.ReadFile(filepath.Join(outDir, fmt.Sprintf("row-%d", i)))
		if err != nil {
			t.Fatalf("%s: read probe result: %v", row.name, err)
		}
		if len(raw) == 0 {
			t.Fatalf("%s: the probe wrote no verdict", row.name)
		}
		if raw[0] == '2' {
			t.Errorf("%s: number token refused with a reason other than kind_mismatch", row.name)
			continue
		}
		read := raw[0] == '1'
		if row.verdict == "read" && !read {
			t.Errorf("%s: the C++ reader refused an accept row", row.name)
			continue
		}
		if row.verdict == "refused" && read {
			t.Errorf("%s: the C++ reader accepted a refuse row", row.name)
			continue
		}
		// The vector file is the value alone; ToJson writes the canonical DOCUMENT,
		// which ends with exactly one newline (§16.1).
		want := string(canonical) + "\n"
		if read && string(raw[1:]) != want {
			t.Errorf("%s: the C++ writer did not spell the canonical text\nwrote:\n%s\nwant:\n%s", row.name, raw[1:], want)
		}
	}
}
