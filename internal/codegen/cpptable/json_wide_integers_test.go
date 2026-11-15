package cpptable

// The JSON form's two integer rules (docs/SPEC-TABLES.md §16.2, §16.14 W10;
// docs/rulings.json json-wide-integers-are-strings and json-integer-token-only)
// as the C++ leg holds them. An integer whose storage is wider than 32 bits is
// written as a string of decimal digits and reads as a string or a number; a
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
`
)

type cppJsonIntegersRow struct {
	name, file, verdict string
}

func cppJsonIntegersRows(t *testing.T) []cppJsonIntegersRow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(cppJsonIntegersDir, "MANIFEST.tsv"))
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
		rows = append(rows, cppJsonIntegersRow{fields[0], fields[2], fields[3]})
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

struct Row { const uint8_t * text; int64_t bytes; };

%s

static const Row kRows[] = {
%s
};

static const int kCases = (int) ( sizeof( kRows ) / sizeof( kRows[ 0 ] ) );

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
        const char status = ok ? '1' : '0';
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
	serialize, ok := cppSerializeDir(t)
	if !ok {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("the sibling runtime serialize is absent at %s: the generated header's 128-bit storage does not resolve", serialize)
		}
		t.Skipf("named as not run: the sibling serialize runtime is not staged at %s", serialize)
	}

	rows := cppJsonIntegersRows(t)
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
		entries = append(entries, fmt.Sprintf("\t{ text%d, (int64_t) sizeof( text%d ) }", i, i))
	}
	probe := filepath.Join(src, "probe.cpp")
	probeSrc := fmt.Sprintf(cppJsonIntegersProbe, header, strings.Join(defs, "\n"), strings.Join(entries, ",\n"))
	if err := os.WriteFile(probe, []byte(probeSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(src, "probe")
	args := append([]string{"-std=c++17", "-O1", "-fsanitize=undefined", "-fno-sanitize-recover=undefined", "-I" + src, "-I" + serialize, probe}, sources...)
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
