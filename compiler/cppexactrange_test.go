// THE C++ DESCRIPTOR'S INTEGER RANGE BOUNDS ARE EXACT (docs/SPEC-TABLES.md
// §8.1, §4). The double range pair carries a float range; an integer field's
// declared bounds ride exact int64/uint64 columns so a bound past 2^53 is not
// degraded through a double.
//
// THE ANSWERS ARE THE GO ORACLE'S. Every value below is the one
// internal/codegen/gotable/exactrange_test.go's TestGoExactIntegerRangeBounds
// asserts for this same schema text and these same input bytes — the `fixed
// table Big` with n, m, u and w, and the clamp at each exact edge:
//
//   - n's max 9007199254740993 (2^53 + 1) is exact on the descriptor, and the
//     text's 9007199254740994 clamps to it;
//   - m's min -9223372036854775807 is exact, and the text's
//     -9223372036854775808 clamps to it;
//   - u's max 18446744073709551614 is exact, and the text's
//     18446744073709551615 clamps to it;
//   - w (int128) carries no int64 pair: its bounds are the wide column's.
//
// The w row is held to the emitted descriptor's text because compiling an
// int128 storage needs the serialize sibling runtime, which this unit test
// does not fetch (the runtime is a make-target prerequisite, not a test
// dependency). The n/m/u clamp probe is compiled and run against the exact
// same declarations and input bytes the Go test uses.
package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const cppExactRangeSource = `package probe
fixed table Big {
	n int64 | min = 0, max = 9007199254740993
	m int64 | min = -9223372036854775807, max = 7
	u uint64 | min = 0, max = 18446744073709551614
	w int128 | min = -1267650600228229401496703205376, max = 1267650600228229401496703205376
}
`

func TestCPPExactIntegerRangeBounds(t *testing.T) {
	t.Parallel()

	// THE WIDE ROW: an int128 field carries no int64 pair; its exact bounds
	// stay in the wide column (the Go test's `w` assertion).
	files, err := New().Generate(unitFromSource(t, cppExactRangeSource), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	header := string(files["ProbeTable.h"])
	if !strings.Contains(header, "range_int_max") {
		t.Fatal("the descriptor carries no exact integer-range column (range_int_max)")
	}
	var wideRow string
	for _, line := range strings.Split(header, "\n") {
		if strings.Contains(line, `{ "w", "w", "int128"`) {
			wideRow = line
			break
		}
	}
	if wideRow == "" {
		t.Fatal("the generated descriptor has no w row")
	}
	if !strings.Contains(wideRow, "&Big_w_wide") {
		t.Fatalf("the w row lost its exact wide range: %s", wideRow)
	}
	for _, pair := range []string{", 0, 0, 0, 0, 0, &Big_w_wide,"} {
		if !strings.Contains(wideRow, pair) {
			t.Fatalf("the int128 w row must carry no int64 pair and no scale, want %q in:\n%s", pair, wideRow)
		}
	}

	// THE NARROW ROWS AND THE CLAMP: the same n/m/u declarations, compiled and
	// run. The full Big source is the oracle's text; w is omitted only from
	// this probe's schema because its storage type is external to the unit.
	narrow := strings.Replace(cppExactRangeSource, "\tw int128 | min = -1267650600228229401496703205376, max = 1267650600228229401496703205376\n", "", 1)
	if narrow == cppExactRangeSource {
		t.Fatal("the narrow probe source dropped no w declaration")
	}
	files, err = New().Generate(unitFromSource(t, narrow), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate narrow: %v", err)
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
	probe := `#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "ProbeTable.h"

using namespace probe;

int main( void )
{
    const TableTypeInfo * info = BigTableType();

    // descriptor: the exact int64/uint64 columns beside the float pair
    // (internal/codegen/gotable/exactrange_test.go TestGoExactIntegerRangeBounds)
    if ( info->fields[0].range_int_max != 9007199254740993LL )
    { printf( "n range_int_max = %lld\n", (long long) info->fields[0].range_int_max ); return 1; }
    if ( info->fields[1].range_int_min != -9223372036854775807LL )
    { printf( "m range_int_min = %lld\n", (long long) info->fields[1].range_int_min ); return 2; }
    if ( info->fields[2].range_uint_max != 18446744073709551614ULL )
    { printf( "u range_uint_max = %llu\n", (unsigned long long) info->fields[2].range_uint_max ); return 3; }

    // the JSON clamp reads the exact columns
    Big v;
    TableReport r{};
    const char * text = "{\"n\":9007199254740994,\"m\":-9223372036854775808,\"u\":18446744073709551615}";
    if ( !BigFromJson( v, text, (int64_t) strlen( text ), &r ) )
    { printf( "FromJson failed\n" ); return 5; }
    if ( v.n != 9007199254740993LL || v.m != -9223372036854775807LL || v.u != 18446744073709551614ULL )
    { printf( "clamp read n=%lld m=%lld u=%llu\n",
             (long long) v.n, (long long) v.m, (unsigned long long) v.u ); return 6; }

    return 0;
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.cpp"), []byte(probe), 0600); err != nil {
		t.Fatal(err)
	}
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("the generated JSON path is C++: no c++ on PATH")
	}
	args := []string{"-std=c++17", "-O2", "-Wall", "-Wextra", "-Werror", "-Wshadow", "-ffp-contract=off", "-I", dir, filepath.Join(dir, "probe.cpp")}
	names, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range names {
		if strings.HasSuffix(e.Name(), ".cpp") && e.Name() != "probe.cpp" {
			args = append(args, filepath.Join(dir, e.Name()))
		}
	}
	bin := filepath.Join(dir, "probe")
	args = append(args, "-o", bin)
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, strings.TrimSpace(string(out)))
	}
}
