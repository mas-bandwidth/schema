package cpptable

// TestCppArmMinHolds pins R10 on the C++ leg: a counted [A..B] holds A inside
// a union arm and on a plain field, on every form the shared conformance rows
// cannot reach. A plain [A..B] is BORN at A (SPEC §4.6), elides at its birth,
// and a freshly reset value round-trips on the table wire and the message
// form; the message writer refuses a count it cannot spell (count - A), the
// guard a release build keeps when the debug assert is gone. The
// rows (testdata/conformance/tables/MANIFEST.txt, arm-min-fix-* and
// armmin_fix_count_*) hold the text read and the fixed read; this probe holds
// the TABLE-WIRE read, which must refuse an arm whose count is below A exactly
// as the Go oracle's read refuses it (internal/codegen/gotable/read.go), and
// the WRITE side, which on this assert leg is a debug check (docs/SPEC.md
// §5.1): the table wire, the message form, the fixed form, the cook and the
// text writer each assert the minimum before they write an arm below it.
//
// The probe defines schema_assert as a handler that names the failing
// expression and exits 3, so each write mode is run in its own process and
// must exit 3 on the declared minimum.
//
// The negative controls plant the old behaviour in the real artifact: the
// read check removed must let a count-1 arm read, and the arm asserts relaxed
// back to `>= 0` must let every write mode complete.

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

const cppArmMinSchema = `package armmin

union MinPick
{
    few   [2..4]uint32
    other int32
}

fixed table MinArmFix
{
    pick MinPick
}

fixed table MinPlainFix
{
    few [2..4]uint32
}
`

const cppArmMinMain = `
bool armmin_asserts_on = true;

// re-spell a saved count-2 array with count_byte elements under an L that
// covers exactly them, so the only thing wrong with it is the count
template <typename T, typename Save, typename Load>
static bool armmin_respelled( const T & v, Save save, Load load, uint8_t count_byte )
{
    static uint8_t wire[256];
    const int64_t n = save( v, wire, (int64_t) sizeof( wire ) );
    if ( n <= 0 ) { std::printf( "read: the count-2 value did not save\n" ); std::exit( 1 ); }
    int64_t at = -1;
    for ( int64_t i = 1; i + 2 < n; ++i )
    {
        if ( wire[i] == 8 && wire[i + 1] == 2 && wire[i + 2] == 0x44 && wire[i - 1] == 10 ) { at = i + 1; break; }
    }
    if ( at < 0 ) { std::printf( "read: no count byte in the saved wire\n" ); std::exit( 1 ); }
    static uint8_t spelled[256];
    int64_t m = 0;
    for ( int64_t i = 0; i < at - 2; ++i ) { spelled[m++] = wire[i]; }
    spelled[m++] = (uint8_t) ( 2 + 4 * count_byte ); // L
    spelled[m++] = 8;
    spelled[m++] = count_byte;
    for ( int64_t i = 0; i < 4 * (int64_t) count_byte; ++i ) { spelled[m++] = wire[at + 1 + i]; }
    for ( int64_t i = at + 9; i < n; ++i ) { spelled[m++] = wire[i]; }
    T back;
    TableReport r;
    const bool ok = load( back, spelled, m, &r );
    std::printf( "read count %d: ok=%d refused=%d damaged=%d\n", (int) count_byte, (int) ok, (int) r.refused, (int) r.damaged );
    return ok;
}

static bool plainmin_read( uint8_t count_byte )
{
    MinPlainFix v;
    MinPlainFixReset( v );
    v.few_count = 2;
    v.few[0] = 0x11223344u;
    v.few[1] = 0x55667788u;
    return armmin_respelled( v,
        []( const MinPlainFix & x, uint8_t * b, int64_t c ) { return MinPlainFixSave( x, b, c ); },
        []( MinPlainFix & x, const uint8_t * b, int64_t c, TableReport * r ) { return MinPlainFixLoad( x, b, c, r ); },
        count_byte );
}

static bool armmin_read( uint8_t count_byte )
{
    MinArmFix v;
    MinArmFixReset( v );
    v.pick.type = MinPickType::Few;
    v.pick.few.value_count = 2;
    v.pick.few.value[0] = 0x11223344u;
    v.pick.few.value[1] = 0x55667788u;
    return armmin_respelled( v,
        []( const MinArmFix & x, uint8_t * b, int64_t c ) { return MinArmFixSave( x, b, c ); },
        []( MinArmFix & x, const uint8_t * b, int64_t c, TableReport * r ) { return MinArmFixLoad( x, b, c, r ); },
        count_byte );
}

int main( int argc, char ** argv )
{
    if ( argc != 2 ) { return 1; }
    const std::string mode = argv[1];
    if ( mode == "read" )
    {
        if ( !armmin_read( 2 ) ) { std::printf( "read: the count-2 arm did not read\n" ); return 1; }
        if ( armmin_read( 1 ) ) { std::printf( "read: a count-1 arm under [2..4] read\n" ); return 1; }
        if ( armmin_read( 0 ) ) { std::printf( "read: a count-0 arm under [2..4] read\n" ); return 1; }
        return 0;
    }
    if ( mode == "plain-read" )
    {
        if ( !plainmin_read( 2 ) ) { std::printf( "plain-read: the count-2 field did not read\n" ); return 1; }
        if ( plainmin_read( 1 ) ) { std::printf( "plain-read: a count-1 field under [2..4] read\n" ); return 1; }
        if ( plainmin_read( 0 ) ) { std::printf( "plain-read: a count-0 field under [2..4] read\n" ); return 1; }
        return 0;
    }
    if ( mode == "plain-born" )
    {
        // a fresh value is LAWFUL: born at A, elided at its birth, and read
        // back as itself on the table wire and the message form
        MinPlainFix fresh;
        if ( fresh.few_count != 2 ) { std::printf( "plain-born: a constructed value's count is %d, want 2\n", (int) fresh.few_count ); return 1; }
        fresh.few_count = 3;
        MinPlainFixReset( fresh );
        if ( fresh.few_count != 2 ) { std::printf( "plain-born: a reset value's count is %d, want 2\n", (int) fresh.few_count ); return 1; }
        static uint8_t wire[256];
        const int64_t n = MinPlainFixSave( fresh, wire, (int64_t) sizeof( wire ) );
        MinPlainFix none;
        MinPlainFix back;
        back.few_count = 4;
        TableReport r;
        if ( n <= 0 || !MinPlainFixLoad( back, wire, n, &r ) || back.few_count != 2 ) { std::printf( "plain-born: the fresh value did not round-trip on the wire (n=%lld count=%d)\n", (long long) n, (int) back.few_count ); return 1; }
        none.few_count = 2;
        none.few[0] = 1;
        static uint8_t rides[256];
        const int64_t riding = MinPlainFixSave( none, rides, (int64_t) sizeof( rides ) );
        if ( riding <= n ) { std::printf( "plain-born: the birth did not elide (%lld bytes, a riding count-2 value %lld)\n", (long long) n, (long long) riding ); return 1; }
        static uint8_t msg[256];
        const int64_t mn = MinPlainFixSaveMessages( &fresh, 1, msg, (int64_t) sizeof( msg ), &r );
        if ( mn <= 0 ) { std::printf( "plain-born: the fresh value did not save as a message\n" ); return 1; }
        return 0;
    }
    if ( mode == "plain-message-guard" )
    {
        // the release guard: with the debug assert off, a count the message
        // form cannot spell is refused rather than wrapped
        armmin_asserts_on = false;
        MinPlainFix v;
        MinPlainFixReset( v );
        v.few_count = 1;
        v.few[0] = 7;
        static uint8_t msg[256];
        TableReport r;
        if ( MinPlainFixMeasureMessages( &v, 1, &r ) >= 0 ) { std::printf( "plain-message-guard: a count-1 value measured\n" ); return 1; }
        if ( MinPlainFixSaveMessages( &v, 1, msg, (int64_t) sizeof( msg ), &r ) >= 0 ) { std::printf( "plain-message-guard: a count-1 value saved\n" ); return 1; }
        return 0;
    }
    if ( mode.rfind( "plain-", 0 ) == 0 )
    {
        MinPlainFix p;
        MinPlainFixReset( p );
        p.few_count = 1;
        p.few[0] = 7;
        static uint8_t out[4096];
        TableReport pr;
        if ( mode == "plain-wire" ) { (void) MinPlainFixSave( p, out, (int64_t) sizeof( out ) ); }
        else if ( mode == "plain-message" ) { (void) MinPlainFixSaveMessages( &p, 1, out, (int64_t) sizeof( out ), &pr ); }
        else if ( mode == "plain-fixed" ) { (void) MinPlainFixFixedSave( &p, 1, out, (int64_t) sizeof( out ) ); }
        else if ( mode == "plain-cook" ) { (void) MinPlainFixCook( p, out, (uint64_t) sizeof( out ), TableByteOrder::Little ); }
        else { return 1; }
        std::printf( "%s: a count-1 field under [2..4] was written\n", mode.c_str() );
        return 0;
    }
    MinArmFix v;
    MinArmFixReset( v );
    v.pick.type = MinPickType::Few;
    v.pick.few.value_count = 1;
    v.pick.few.value[0] = 7;
    static uint8_t buffer[4096];
    TableReport r;
    if ( mode == "wire" ) { (void) MinArmFixSave( v, buffer, (int64_t) sizeof( buffer ) ); }
    else if ( mode == "message" ) { (void) MinArmFixSaveMessages( &v, 1, buffer, (int64_t) sizeof( buffer ), &r ); }
    else if ( mode == "fixed" ) { (void) MinArmFixFixedSave( &v, 1, buffer, (int64_t) sizeof( buffer ) ); }
    else if ( mode == "cook" ) { (void) MinArmFixCook( v, buffer, (uint64_t) sizeof( buffer ), TableByteOrder::Little ); }
    else if ( mode == "json" ) { (void) MinArmFixToJson( v, (char *) buffer, (int64_t) sizeof( buffer ) ); }
    else { return 1; }
    std::printf( "%s: a count-1 arm under [2..4] was written\n", mode.c_str() );
    return 0;
}
`

var cppArmMinWriteModes = []string{"wire", "message", "fixed", "cook", "json", "plain-wire", "plain-message", "plain-fixed", "plain-cook"}

// the modes that must pass (exit 0) on the fixed code and go red when planted
var cppArmMinGreenModes = []string{"read", "plain-read", "plain-born", "plain-message-guard"}

// cppArmMinBuild generates the unit, lets undermine plant a fault in the real
// artifact, and builds the probe; it returns the probe's path.
func cppArmMinBuild(t *testing.T, undermine func(map[string][]byte)) string {
	t.Helper()
	u := cppUnitFrom(t, cppArmMinSchema)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if undermine != nil {
		undermine(files)
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
	// the handler every translation unit sees: the generated header takes a
	// schema_assert the includer defines (cpptable.go, `#ifndef schema_assert`)
	hook := `#include <cstdio>
#include <cstdlib>
extern bool armmin_asserts_on;
#define schema_assert( c ) do { if ( armmin_asserts_on && !( c ) ) { std::printf( "assert: %s\n", #c ); std::fflush( stdout ); std::exit( 3 ); } } while ( 0 )
`
	if err := os.WriteFile(filepath.Join(dir, "armmin_hook.h"), []byte(hook), 0o600); err != nil {
		t.Fatal(err)
	}
	probe := `#include <cstdio>
#include <cstdlib>
#include <cstdint>
#include <string>
#include "` + header + `"
using namespace armmin;
` + cppArmMinMain
	if err := os.WriteFile(filepath.Join(dir, "probe.cpp"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	args := []string{"-std=c++17", "-O1", "-UNDEBUG", "-include", filepath.Join(dir, "armmin_hook.h"), "-I" + dir, filepath.Join(dir, "probe.cpp")}
	args = append(args, sources...)
	args = append(args, "-o", filepath.Join(dir, "probe"))
	if out, err := testproc.Command(t, cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return filepath.Join(dir, "probe")
}

// cppArmMinExit runs one probe mode and returns its exit code and output.
func cppArmMinExit(t *testing.T, probe, mode string) (int, string) {
	t.Helper()
	out, err := testproc.Command(t, probe, mode).CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	type exitCoder interface{ ExitCode() int }
	if e, ok := err.(exitCoder); ok {
		return e.ExitCode(), string(out)
	}
	t.Fatalf("%s: %v\n%s", mode, err, out)
	return -1, ""
}

func TestCppArmMinHolds(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler (the leg's own toolchain)")
	t.Parallel()
	probe := cppArmMinBuild(t, nil)
	for _, mode := range cppArmMinGreenModes {
		if code, out := cppArmMinExit(t, probe, mode); code != 0 {
			t.Errorf("%s: exit %d\n%s", mode, code, out)
		}
	}
	for _, mode := range cppArmMinWriteModes {
		code, out := cppArmMinExit(t, probe, mode)
		if code != 3 || !strings.Contains(out, ">= 2") && !strings.Contains(out, "TableJsonCountMeetsMin") {
			t.Errorf("%s: a count-1 arm under [2..4] was not stopped by the minimum's debug assert: exit %d\n%s", mode, code, out)
		}
	}
}

func TestCppArmMinHoldsNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler (the leg's own toolchain)")
	t.Parallel()
	undermine := func(files map[string][]byte) {
		planted := 0
		for name, data := range files {
			text := string(data)
			old := text
			// the reads: the minimum checks become checks nothing meets, arm
			// and plain field alike
			text = strings.ReplaceAll(text, "_count < 2 )\n", "_count < 0 )\n")
			text = strings.ReplaceAll(text, "if ( count < 2 )\n", "if ( count < 0 )\n")
			// the writers: every assert back to the storage invariant
			text = strings.ReplaceAll(text, "_count >= 2 &&", "_count >= 0 &&")
			// the birth: back to count zero, and the zero-count elision
			text = strings.ReplaceAll(text, "few_count = 2;", "few_count = 0;")
			// the message guard: the old unguarded subtraction
			text = strings.ReplaceAll(text, ") < 2 ) { return -1; }", ") < 0 ) { return -1; }")
			text = strings.ReplaceAll(text, ") < 2 ) { return false; }", ") < 0 ) { return false; }")
			text = strings.ReplaceAll(text, "schema_assert( TableJsonCountMeetsMin( storage, arms->arms[tag].field ) );", "")
			if text != old {
				files[name] = []byte(text)
				planted++
			}
		}
		if planted == 0 {
			t.Fatal("no generated file carried the arm minimum to plant out")
		}
	}
	probe := cppArmMinBuild(t, undermine)
	for _, mode := range cppArmMinGreenModes {
		if code, out := cppArmMinExit(t, probe, mode); code == 0 {
			t.Errorf("%s: with the minimum planted out, the probe still passed\n%s", mode, out)
		}
	}
	for _, mode := range cppArmMinWriteModes {
		if code, out := cppArmMinExit(t, probe, mode); code == 3 {
			t.Errorf("%s: with the arm asserts planted back to >= 0, the write was still stopped\n%s", mode, out)
		}
	}
}
