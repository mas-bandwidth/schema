package cpptable

// A COUNTED ARRAY'S DEAD SLOTS ARE ZERO IN A COOK (docs/SPEC-TABLES.md §7.2;
// docs/rulings.json fixed-dead-bytes-are-zero): the writer writes the live
// slots and leaves every slot past the count the zero the extent was cleared
// to. The recipe's own values are cooked through the generated C++ SaveCook,
// in both byte orders, and held to the committed cook-write fixtures.

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const cppCookZeroProbeMain = `#include <cstdio>
#include <cstring>
#include <cstdint>
#include <vector>
#include "TablesTable.h"
#include "NestedTable.h"

using namespace tabledemo;

static bool slurp( const char * path, std::vector<uint8_t> & out )
{
    FILE * f = std::fopen( path, "rb" );
    if ( f == NULL ) { return false; }
    std::fseek( f, 0, SEEK_END );
    const long n = std::ftell( f );
    std::fseek( f, 0, SEEK_SET );
    if ( n < 0 ) { std::fclose( f ); return false; }
    out.resize( (size_t) n );
    if ( n > 0 && std::fread( out.data(), 1, (size_t) n, f ) != (size_t) n ) { std::fclose( f ); return false; }
    std::fclose( f );
    return true;
}

template <typename Root>
static int cook_row( const char * row, const char * json_dir, const char * fixture_dir,
                     void ( *reset )( Root & ),
                     bool ( *from_json )( Root &, const char *, int64_t, TableReport * ),
                     int64_t ( *measure )( const Root & ),
                     bool ( *cook )( const Root &, void *, uint64_t, TableByteOrder ) )
{
    char json_path[4096];
    std::snprintf( json_path, sizeof( json_path ), "%s/%s.json", json_dir, row );
    std::vector<uint8_t> text;
    if ( !slurp( json_path, text ) ) { std::printf( "%s: cannot read %s\n", row, json_path ); return 2; }

    Root value;
    reset( value );
    TableReport report;
    if ( !from_json( value, (const char *) text.data(), (int64_t) text.size(), &report ) || report.refused )
    {
        std::printf( "%s: the JSON input did not read\n", row );
        return 1;
    }

    const int64_t need = measure( value );
    if ( need <= 0 ) { std::printf( "%s: CookMeasure is %lld\n", row, (long long) need ); return 1; }

    for ( int big = 0; big < 2; big++ )
    {
        std::vector<uint8_t> got( (size_t) need );
        if ( !cook( value, got.data(), (uint64_t) need, big ? TableByteOrder::Big : TableByteOrder::Little ) )
        {
            std::printf( "%s%s: Cook returned false\n", row, big ? "-be" : "" );
            return 1;
        }
        char path[4096];
        std::snprintf( path, sizeof( path ), "%s/%s%s.cook", fixture_dir, row, big ? "-be" : "" );
        std::vector<uint8_t> want;
        if ( !slurp( path, want ) ) { std::printf( "%s: cannot read %s\n", row, path ); return 2; }
        if ( got.size() != want.size() || std::memcmp( got.data(), want.data(), want.size() ) != 0 )
        {
            std::printf( "%s%s: the cooked bytes are not the fixture (%zu vs %zu)\n",
                         row, big ? "-be" : "", got.size(), want.size() );
            return 1;
        }
    }
    return 0;
}

int main( int argc, char ** argv )
{
    if ( argc != 3 ) { std::printf( "usage: probe <cook-write dir> <json dir>\n" ); return 2; }
    const char * fixture_dir = argv[1];
    const char * json_dir = argv[2];

    int rc = cook_row<RootConfig>( "root_full", json_dir, fixture_dir,
                                   RootConfigReset, RootConfigFromJson, RootConfigCookMeasure, RootConfigCook );
    if ( rc != 0 ) { return rc; }
    rc = cook_row<ArchiveConfig>( "archive", json_dir, fixture_dir,
                                  ArchiveConfigReset, ArchiveConfigFromJson, ArchiveConfigCookMeasure, ArchiveConfigCook );
    if ( rc != 0 ) { return rc; }
    return 0;
}
`

// TestCppCookWriteZeroesDeadSlots cooks root_full and archive (a counted array
// below its capacity) through the generated C++ SaveCook and requires the bytes
// to equal the committed fixtures, in both byte orders.
func TestCppCookWriteZeroesDeadSlots(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C++ compiler (the leg's own toolchain)")

	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	u := loadHeldUnitPaths(t, root, []string{"tables/examples"})
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)

	dir := t.TempDir()
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.cpp"), []byte(cppCookZeroProbeMain), 0o600); err != nil {
		t.Fatal(err)
	}

	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	serialize, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "serialize"))
	if err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(dir, "probe")
	args := []string{
		"-std=c++17", "-Wall", "-Wextra", "-Werror", "-Wshadow", "-ffp-contract=off",
		"-O1", "-I" + dir, "-I" + serialize,
		filepath.Join(dir, "probe.cpp"),
		filepath.Join(dir, "TablesTable.cpp"),
		filepath.Join(dir, "NestedTable.cpp"),
		"-o", probe,
	}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("the cook probe did not compile: %v\n%s", err, out)
	}

	fixtureDir := filepath.Join(root, "testdata", "conformance", "tables", "cook-write")
	jsonDir := filepath.Join(root, "testdata", "conformance", "tables", "json")
	if out, err := exec.Command(probe, fixtureDir, jsonDir).CombinedOutput(); err != nil {
		t.Fatalf("the dead counted slots are not zero: %v\n%s", err, out)
	}
}
