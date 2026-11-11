package cpptable

// TestCppBigEndianFormThreeRead is the C++ big-endian form-3 (fixed form)
// read cell, the twin of the C leg's TestCBigEndianFormThreeRead. Every
// integer on the fixed-form wire is little-endian on every host
// (docs/FIXED-FORM-ALGORITHM.md), so a copy into native storage must land each
// element in this host's order. A host-order byte copy lands every multi-byte
// value byte-swapped on a big-endian host. Copy entries carry their element
// width (elem), and on a big-endian host each element is reversed as it lands;
// the widen, ordinal and const ops read their source through an explicit
// little-endian load and store their result in native order.
//
// No big-endian host is needed to hold it: the probe is compiled from the
// generated header with SCHEMA_TABLE_FIXED_FORCE_BIG_ENDIAN, which selects
// the big-endian copy path. In the forced build a copied field's native value
// is meaningless on this little-endian host, so the probe holds its BYTES: at
// each field's offsetof they must be the big-endian image of the saved value,
// which is exactly what a big-endian host's struct would hold. Fields the
// reader decodes by value (a text length, a bool) are held by value. The
// strict half is held too: a forged bool byte refuses the whole load and the
// record is reset.

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

const cppBigEndianForm3Schema = `package beprobe
fixed table Root {
    lead uint32
    a uint16
    b uint32
    c uint64
    d int32
    e float32
    f float64
    vals [3]uint32
    label string(8)
    ok bool
}
`

const cppBigEndianForm3Main = `
#define CHECK(x) do { if (!(x)) { std::printf( "FAIL: line %d: %s\n", __LINE__, #x ); return 1; } } while (0)
static bool be_at( const void * base, size_t off, const uint8_t * want, size_t n )
{
    return std::memcmp( (const uint8_t *) base + off, want, n ) == 0;
}
int main( void )
{
    static const uint8_t lead_be[4] = { 0x12, 0x34, 0x56, 0x78 };
    static const uint8_t a_be[2] = { 0x9A, 0xBC };
    static const uint8_t b_be[4] = { 0xDE, 0xAD, 0xBE, 0xEF };
    static const uint8_t c_be[8] = { 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF };
    static const uint8_t d_be[4] = { 0xFF, 0xFF, 0xFF, 0xFE };
    static const uint8_t vals_be[12] = { 0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3 };
    Root value;
    RootReset( value );
    value.lead = 0x12345678u;
    value.a = 0x9ABC;
    value.b = 0xDEADBEEFu;
    value.c = 0x0123456789ABCDEFull;
    value.d = -2;
    value.e = 1.5f;
    value.f = -2.5;
    value.vals[0] = 1; value.vals[1] = 2; value.vals[2] = 3;
    std::memcpy( value.label, "hi", 2 );
    value.label_length = 2;
    value.ok = true;
    static uint8_t file[1024];
    static TableFixedEntry plan[64];
    const int64_t need = RootFixedMeasure( 1 );
    CHECK( need > 0 && need <= (int64_t) sizeof( file ) );
    CHECK( RootFixedSave( &value, 1, file, (int64_t) sizeof( file ) ) == need );
    // THE FILE IS LITTLE-ENDIAN WHATEVER THE HOST: its first record body
    // opens with lead's little-endian bytes, not this build's order.
    const uint32_t layout_bytes = TableFixedGet32( file + kTableFixedHeaderBytes );
    uint8_t * body = file + kTableFixedHeaderBytes + 4 + layout_bytes + 8;
    CHECK( body[0] == 0x78 && body[1] == 0x56 && body[2] == 0x34 && body[3] == 0x12 );
    Root loaded;
    std::memset( (void *) &loaded, 0xAA, sizeof( loaded ) );
    TableReport report;
    int64_t n = RootFixedLoad( &loaded, 1, file, need, plan, 64, NULL, &report );
    CHECK( n == 1 );
    CHECK( !report.refused );
#if defined( SCHEMA_TABLE_FIXED_FORCE_BIG_ENDIAN )
    // COPIED FIELDS LAND AS THE BIG-ENDIAN IMAGE OF THE SAVED VALUE.
    CHECK( be_at( &loaded, offsetof( Root, lead ), lead_be, 4 ) );
    CHECK( be_at( &loaded, offsetof( Root, a ), a_be, 2 ) );
    CHECK( be_at( &loaded, offsetof( Root, b ), b_be, 4 ) );
    CHECK( be_at( &loaded, offsetof( Root, c ), c_be, 8 ) );
    CHECK( be_at( &loaded, offsetof( Root, d ), d_be, 4 ) );
    CHECK( be_at( &loaded, offsetof( Root, vals ), vals_be, 12 ) );
    // A FLOAT RIDES THE SAME COPY: its bits are the value's, byte-reversed.
    {
        const float ef = 1.5f;
        const double df = -2.5;
        uint8_t eb[4], db[8];
        std::memcpy( eb, &ef, 4 );
        std::memcpy( db, &df, 8 );
        for ( int i = 0; i < 4; ++i ) { CHECK( ( (const uint8_t *) &loaded )[offsetof( Root, e ) + i] == eb[3 - i] ); }
        for ( int i = 0; i < 8; ++i ) { CHECK( ( (const uint8_t *) &loaded )[offsetof( Root, f ) + i] == db[7 - i] ); }
    }
#else
    // THE NATIVE BUILD ROUND-TRIPS BY VALUE, so the forced build's byte
    // image is held against a reader that also works at home.
    (void) lead_be; (void) a_be; (void) b_be; (void) c_be; (void) d_be; (void) vals_be; (void) be_at;
    CHECK( loaded.lead == 0x12345678u && loaded.a == 0x9ABC && loaded.b == 0xDEADBEEFu );
    CHECK( loaded.c == 0x0123456789ABCDEFull && loaded.d == -2 && loaded.e == 1.5f && loaded.f == -2.5 );
    CHECK( loaded.vals[0] == 1 && loaded.vals[1] == 2 && loaded.vals[2] == 3 );
#endif
    // DECODED-BY-VALUE FIELDS AND TEXT LAND BY VALUE IN EITHER ORDER.
    CHECK( loaded.label_length == 2 && loaded.label[0] == 'h' && loaded.label[1] == 'i' && loaded.label[2] == 0 );
    CHECK( loaded.ok );
    // THE STRICT READ: a forged bool byte (the body's last byte) refuses the
    // WHOLE load, and the record it wrote is back at its defaults.
    body[RootFixedBodyBytes - 1] = 0x02;
    Root fresh;
    std::memset( (void *) &fresh, 0, sizeof( fresh ) );
    RootReset( fresh );
    std::memset( (void *) &loaded, 0, sizeof( loaded ) );
    n = RootFixedLoad( &loaded, 1, file, need, plan, 64, NULL, &report );
    CHECK( n == -1 );
    CHECK( report.refused && report.refuse_reason == wire_damaged );
    CHECK( std::memcmp( (const void *) &loaded, (const void *) &fresh, sizeof( fresh ) ) == 0 );
    return 0;
}
`

// cppBigEndianForm3Run generates the probe unit, optionally sabotaging the
// generated header (the real artifact), compiles it with or without the
// forced big-endian path and runs it. It answers the probe's FAIL lines and
// its whole output.
func cppBigEndianForm3Run(t *testing.T, forceBigEndian bool, sabotage [2]string) ([]string, string) {
	t.Helper()
	u := cppUnitFrom(t, cppBigEndianForm3Schema)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if sabotage[0] != "" {
		patched := false
		for name, data := range files {
			if src := string(data); strings.Contains(src, sabotage[0]) {
				files[name] = []byte(strings.ReplaceAll(src, sabotage[0], sabotage[1]))
				patched = true
			}
		}
		if !patched {
			t.Fatalf("the sabotage patches nothing: no emitted file carries %q", sabotage[0])
		}
	}
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
	force := ""
	if forceBigEndian {
		force = "#define SCHEMA_TABLE_FIXED_FORCE_BIG_ENDIAN 1\n"
	}
	probe := force + `#include <cstdio>
#include <cstring>
#include <cstddef>
#include <cstdint>
#include "` + header + `"
using namespace beprobe;
` + cppBigEndianForm3Main
	if err := os.WriteFile(filepath.Join(dir, "probe.cpp"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	args := []string{"-std=c++17", "-O1", "-Wall", "-Wextra", "-Werror", "-Wno-invalid-offsetof", "-I" + dir, filepath.Join(dir, "probe.cpp")}
	args = append(args, sources...)
	args = append(args, "-o", filepath.Join(dir, "probe"))
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	output, runErr := exec.CommandContext(ctx, filepath.Join(dir, "probe")).CombinedOutput()
	var fails []string
	for line := range strings.SplitSeq(string(output), "\n") {
		if strings.HasPrefix(line, "FAIL: ") {
			fails = append(fails, line)
		}
	}
	if len(fails) == 0 && runErr != nil {
		fails = append(fails, "the probe did not exit 0: "+runErr.Error())
	}
	return fails, string(output)
}

func TestCppBigEndianFormThreeRead(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler (the leg's own toolchain)")
	t.Parallel()
	for _, force := range []bool{true, false} {
		if fails, out := cppBigEndianForm3Run(t, force, [2]string{}); len(fails) != 0 {
			t.Fatalf("the C++ big-endian form-3 read does not hold (forced big-endian %v):\n%s", force, out)
		}
	}
}

// TestCppBigEndianFormThreeReadNegativeControl plants the defect in the real
// artifact — the generated header's big-endian copy, put back to a
// host-order byte copy — and requires the forced big-endian probe to go red.
// On a little-endian host the same sabotage is invisible, which is the whole
// argument for forcing the big-endian path in the probe.
func TestCppBigEndianFormThreeReadNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler (the leg's own toolchain)")
	t.Parallel()
	sabotage := [2]string{
		"dst[i + j] = src[i + elem - 1 - j];",
		"dst[i + j] = src[i + j]; /* SABOTAGED: host order */",
	}
	fails, out := cppBigEndianForm3Run(t, true, sabotage)
	if len(fails) == 0 {
		t.Fatalf("NEGATIVE CONTROL FAILED: a host-order copy left the C++ big-endian form-3 probe green:\n%s", out)
	}
}
