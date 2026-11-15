package cpptable

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const cppSkippedValueProbe = `
#include <cstdio>
#include <cstdint>
#include <cstring>
#include <string>
#include <vector>
#include "ReaderTable.h"
using namespace skippedreader;

static bool slurp( const char * path, std::vector<uint8_t> & out )
{
    FILE * f = fopen( path, "rb" );
    if ( f == NULL ) { return false; }
    if ( fseek( f, 0, SEEK_END ) != 0 ) { fclose( f ); return false; }
    const long n = ftell( f );
    if ( n < 0 ) { fclose( f ); return false; }
    rewind( f );
    out.resize( (size_t) n );
    const bool ok = n == 0 || fread( out.data(), 1, (size_t) n, f ) == (size_t) n;
    fclose( f );
    return ok;
}

int main( int argc, char ** argv )
{
    if ( argc < 2 ) { printf( "missing fixture directory\n" ); return 1; }
    const std::string dir = argv[1];

    std::vector<uint8_t> announcement;
    if ( !slurp( ( dir + "/announcement.bin" ).c_str(), announcement ) ) { printf( "missing announcement\n" ); return 1; }
    static TableMessageEntry entries[4096];
    TableVocabulary vocabulary( entries, 4096 );
    TableReport announce_report; memset( &announce_report, 0, sizeof( announce_report ) );
    const bool announced = AnnounceRead( vocabulary, announcement.data(), (int64_t) announcement.size(), &announce_report );
    printf( "ROW\tannouncement.bin\t%s\n", announced ? "read" : "refused" );

    const char * messages[] = { "canonical.bin", "string-zero.bin", "string-illformed.bin", "float-nan.bin",
        "quant-index-over.bin", "array-count-over.bin", "keyed-repeat.bin", "keyed-payload-entry.bin", "nested-id-twice.bin" };
    for ( const char * file : messages )
    {
        std::vector<uint8_t> data;
        if ( !slurp( ( dir + "/" + file ).c_str(), data ) ) { printf( "MISSING\t%s\n", file ); continue; }
        Root value; RootReset( value );
        int64_t count = 1;
        TableReport report; memset( &report, 0, sizeof( report ) );
        const bool ok = RootLoadMessages( &value, &count, vocabulary, data.data(), (int64_t) data.size(), &report );
        printf( "ROW\t%s\t%s\n", file, ok && count == 1 ? "read" : "refused" );
    }

    const char * jsons[] = { "unknown-string-zero.json", "unknown-string-control.json",
        "skipped-object-key-zero.json", "skipped-object-key-control.json" };
    for ( const char * file : jsons )
    {
        std::vector<uint8_t> raw;
        if ( !slurp( ( dir + "/" + file ).c_str(), raw ) ) { printf( "MISSING\t%s\n", file ); continue; }
        Root value; RootReset( value );
        TableReport report; memset( &report, 0, sizeof( report ) );
        const bool ok = RootFromJson( value, (const char *) raw.data(), (int64_t) raw.size(), &report );
        printf( "ROW\t%s\t%s\n", file, ok ? "read" : "refused" );
    }
    return 0;
}
`

func cppSkippedValueRun(t *testing.T, fixtures string) ([]byte, error) {
	t.Helper()
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
	u := loadUnit(t, filepath.Join("..", "..", "..", "test", "skipped-value", "reader", "Reader.schema"))
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	var header string
	var sources []string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		switch {
		case strings.HasSuffix(name, "Table.h"):
			header = name
		case strings.HasSuffix(name, ".cpp"):
			sources = append(sources, filepath.Join(dir, name))
		}
	}
	if header == "" {
		t.Fatal("no Table.h was generated")
	}
	probe := `#include <cstdio>
#include <cstdint>
#include <cstring>
#include <string>
#include <vector>
#include "` + header + `"
` + cppSkippedValueProbe
	if err := os.WriteFile(filepath.Join(dir, "probe.cpp"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c++17", "-O1", "-DNDEBUG", "-I" + dir, filepath.Join(dir, "probe.cpp")}
	args = append(args, sources...)
	args = append(args, "-o", filepath.Join(dir, "probe"))
	build := exec.Command(cxx, args...)
	if out, err := build.CombinedOutput(); err != nil {
		return out, err
	}
	return exec.Command(filepath.Join(dir, "probe"), fixtures).CombinedOutput()
}

func TestCppSkippedValueIsValidated(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler (it compiles and runs the generated skipped-value probe)")
	t.Parallel()

	const manifestPath = "../../../testdata/wire/skipped-value/MANIFEST.tsv"
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	type row struct{ file, form, verdict string }
	var rows []row
	for i, line := range strings.Split(string(manifest), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			t.Fatalf("%s:%d: a vector line is <file> TAB <form> TAB <verdict>, got %q", manifestPath, i+1, line)
		}
		if f[1] != "json" && f[1] != "message" {
			t.Fatalf("%s:%d: form %q, want json or message", manifestPath, i+1, f[1])
		}
		if f[2] != "read" && f[2] != "refused" {
			t.Fatalf("%s:%d: verdict %q, want read or refused", manifestPath, i+1, f[2])
		}
		rows = append(rows, row{f[0], f[1], f[2]})
	}
	if len(rows) == 0 {
		t.Fatalf("%s names no vectors", manifestPath)
	}
	fixtures, err := filepath.Abs(filepath.Dir(manifestPath))
	if err != nil {
		t.Fatal(err)
	}
	out, err := cppSkippedValueRun(t, fixtures)
	if err != nil {
		t.Fatalf("the skipped-value probe: %v\n%s", err, out)
	}
	got := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(line, "\t")
		if len(f) == 3 && f[0] == "ROW" {
			got[f[1]] = f[2]
		}
	}
	for _, r := range rows {
		verdict, ok := got[r.file]
		if !ok {
			t.Errorf("%s: the probe reported no verdict", r.file)
			continue
		}
		if verdict != r.verdict {
			t.Errorf("%s: verdict %s, want %s", r.file, verdict, r.verdict)
		}
	}
}
