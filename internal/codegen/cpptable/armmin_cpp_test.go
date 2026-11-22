package cpptable

// TestCppArmMinHolds pins R10 on the C++ leg: a counted [A..B] holds A inside
// a union arm, on every form the shared conformance rows cannot reach. The
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
`

const cppArmMinMain = `
static bool armmin_read( uint8_t count_byte )
{
    MinArmFix v;
    MinArmFixReset( v );
    v.pick.type = MinPickType::Few;
    v.pick.few.value_count = 2;
    v.pick.few.value[0] = 0x11223344u;
    v.pick.few.value[1] = 0x55667788u;
    static uint8_t wire[256];
    const int64_t n = MinArmFixSave( v, wire, (int64_t) sizeof( wire ) );
    if ( n <= 0 ) { std::printf( "read: the count-2 arm did not save\n" ); std::exit( 1 ); }
    // the arm's array payload: L, element kind 8 (uint32), the count, the
    // elements. The re-spelled arm keeps its first count_byte elements and an
    // L that covers exactly them, so the only thing wrong with it is the count.
    int64_t at = -1;
    for ( int64_t i = 1; i + 2 < n; ++i )
    {
        if ( wire[i] == 8 && wire[i + 1] == 2 && wire[i + 2] == 0x44 && wire[i - 1] == 10 ) { at = i + 1; break; }
    }
    if ( at < 0 ) { std::printf( "read: no arm count byte in the saved wire\n" ); std::exit( 1 ); }
    static uint8_t spelled[256];
    int64_t m = 0;
    for ( int64_t i = 0; i < at - 2; ++i ) { spelled[m++] = wire[i]; }
    spelled[m++] = (uint8_t) ( 2 + 4 * count_byte ); // L
    spelled[m++] = 8;
    spelled[m++] = count_byte;
    for ( int64_t i = 0; i < 4 * (int64_t) count_byte; ++i ) { spelled[m++] = wire[at + 1 + i]; }
    for ( int64_t i = at + 9; i < n; ++i ) { spelled[m++] = wire[i]; }
    MinArmFix back;
    TableReport r;
    const bool ok = MinArmFixLoad( back, spelled, m, &r );
    std::printf( "read count %d: ok=%d refused=%d damaged=%d\n", (int) count_byte, (int) ok, (int) r.refused, (int) r.damaged );
    return ok;
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

var cppArmMinWriteModes = []string{"wire", "message", "fixed", "cook", "json"}

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
#define schema_assert( c ) do { if ( !( c ) ) { std::printf( "assert: %s\n", #c ); std::fflush( stdout ); std::exit( 3 ); } } while ( 0 )
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
	if code, out := cppArmMinExit(t, probe, "read"); code != 0 {
		t.Errorf("table-wire read: exit %d\n%s", code, out)
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
			// the read: the arm's minimum check becomes a check nothing meets
			text = strings.ReplaceAll(text, "_count < 2 )\n", "_count < 0 )\n")
			// the writers: every arm assert back to the storage invariant
			text = strings.ReplaceAll(text, "value_count >= 2 &&", "value_count >= 0 &&")
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
	if code, out := cppArmMinExit(t, probe, "read"); code == 0 {
		t.Errorf("with the arm minimum's read check planted out, the table-wire probe still passed\n%s", out)
	}
	for _, mode := range cppArmMinWriteModes {
		if code, out := cppArmMinExit(t, probe, mode); code == 3 {
			t.Errorf("%s: with the arm asserts planted back to >= 0, the write was still stopped\n%s", mode, out)
		}
	}
}
