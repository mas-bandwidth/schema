package cpptable

// EVERY DEAD BYTE IN A KEYED ARRAY IS ZERO, SLOT BY SLOT
// (docs/FIXED-FORM-ALGORITHM.md §3.1, §4.5, §5.3; docs/SPEC-TABLES.md §3.4,
// §13.10): an enum-keyed array's element slots are fixed regions like any
// other array's, so a counted array's dead tail and an absent optional's dead
// payload in a LATER slot are held to zero too. The check pass reads each
// slot's own plan row, so a slot past the first carries its element width and
// payload span the same way the first one does.
//
// The verdict is the contract; the refusal's name is this leg's diagnostic.

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

const cppFixedDeadKeyedSchema = `package tbldeadkeyed

const Slots = 3

enum Slot
{
    Alpha
    Beta
    Gamma
}

fixed table Cell
{
    nums [..3]int16
    opt  ?int32
}

fixed table KeyedRoot
{
    cells [Slot]Cell
}
`

const cppFixedDeadKeyedProbe = `#include <cstdio>
#include <cstring>
#include <cstdint>
#include <vector>
#include "@HEADER@"

using namespace @PKG@;

int main( int argc, char ** argv )
{
    if ( argc < 2 )
    {
        std::printf( "usage: probe <clean|count|opt>\n" );
        return 2;
    }
    static uint8_t data[1 << 20];
    KeyedRoot v;
    KeyedRootReset( v );
    // Slot 1 is the LATER keyed slot: its counted array is short and its
    // optional is absent, so its dead tail and dead payload are both zero on
    // save. The marker locates the slot in the wire image.
    v.cells[Slot::Beta].nums_count = 1;
    v.cells[Slot::Beta].nums[0] = 0x1234;
    v.cells[Slot::Beta].nums[1] = 0;
    v.cells[Slot::Beta].nums[2] = 0;
    v.cells[Slot::Beta].opt_present = 0;
    const int64_t len = KeyedRootFixedSave( &v, 1, data, (int64_t) sizeof( data ) );
    if ( len <= 0 ) { std::printf( "save failed\n" ); return 1; }
    int64_t at = -1;
    for ( int64_t i = 0; i + 1 < len; ++i )
    {
        if ( data[i] == 0x34 && data[i + 1] == 0x12 ) { at = i; break; }
    }
    if ( at < 0 ) { std::printf( "the slot-1 marker is not in the save\n" ); return 1; }
    // Cell is count(4) + nums[3](6) + present(1) + payload(4). The marker is
    // nums[0] at Cell+4, so the dead tail nums[1] is marker+2 and the absent
    // optional's payload begins at marker+7.
    const bool want_refuse = std::strcmp( argv[1], "clean" ) != 0;
    if ( std::strcmp( argv[1], "count" ) == 0 ) { data[at + 2] = 0x7f; }
    else if ( std::strcmp( argv[1], "opt" ) == 0 ) { data[at + 7] = 0x7f; }
    else if ( !want_refuse ) { /* the clean control */ }
    else { std::printf( "unknown case %s\n", argv[1] ); return 2; }

    KeyedRoot storage[1];
    std::memset( (void *) storage, 0xA5, sizeof( storage ) );
    KeyedRoot before[1];
    std::memcpy( (void *) before, (const void *) storage, sizeof( storage ) );
    std::vector<TableFixedEntry> plan( 4096 );
    TableReport r;
    const int64_t n = KeyedRootFixedLoad( storage, 1, data, len, plan.data(), 4096, NULL, &r );
    if ( want_refuse )
    {
        if ( n != -1 )
        {
            std::printf( "%s: read %lld records, want the whole load refused\n", argv[1], (long long) n );
            return 1;
        }
        if ( std::memcmp( (const void *) storage, (const void *) before, sizeof( storage ) ) != 0 )
        {
            std::printf( "%s: a refused load wrote the caller's destination\n", argv[1] );
            return 1;
        }
        return 0;
    }
    if ( n != 1 || r.damaged || r.refused )
    {
        std::printf( "clean: n=%lld damaged=%d refused=%d, want one record read\n", (long long) n, (int) r.damaged, (int) r.refused );
        return 1;
    }
    std::vector<uint8_t> out( (size_t) KeyedRootFixedMeasure( 1 ) );
    const int64_t wrote = KeyedRootFixedSave( storage, 1, out.data(), (int64_t) out.size() );
    if ( wrote != (int64_t) out.size() || wrote != len || std::memcmp( out.data(), data, (size_t) len ) != 0 )
    {
        std::printf( "clean: the read did not save back byte for byte (wrote=%lld len=%lld)\n", (long long) wrote, (long long) len );
        return 1;
    }
    return 0;
}
`

func cppFixedDeadKeyedProbeBinary(t *testing.T) string {
	slowtest.Gate(t, "an external toolchain")
	t.Helper()
	u := loadUnitFromSource(t, cppFixedDeadKeyedSchema)
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
	var header string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
	}
	if header == "" {
		t.Fatal("no Table.h was generated")
	}
	probe := strings.ReplaceAll(strings.ReplaceAll(cppFixedDeadKeyedProbe, "@HEADER@", header), "@PKG@", u.Package)
	if err := os.WriteFile(filepath.Join(dir, "probe.cpp"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	serialize, _ := filepath.Abs("../../../../serialize")
	args := []string{"-std=c++17", "-O1", "-I" + dir, "-I" + serialize, filepath.Join(dir, "probe.cpp"), "-o", filepath.Join(dir, "probe")}
	build := exec.Command(cxx, args...)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the probe did not compile: %v\n%s", err, out)
	}
	return filepath.Join(dir, "probe")
}

func TestFixedDeadBytesKeyedSlots(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "a C++ compiler (it runs the generated reader over a keyed record)")
	probe := cppFixedDeadKeyedProbeBinary(t)
	for _, vec := range []struct {
		name string
		want string
	}{
		{name: "clean", want: "clean"},
		{name: "counted_tail", want: "count"},
		{name: "absent_payload", want: "opt"},
	} {
		vec := vec
		t.Run(vec.name, func(t *testing.T) {
			t.Parallel()
			out, err := exec.Command(probe, vec.want).CombinedOutput()
			if err != nil {
				t.Fatalf("%s: %v\n%s", vec.name, err, out)
			}
		})
	}
}
