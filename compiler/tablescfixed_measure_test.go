package compiler

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestCTableFixedMeasureCountAsserted holds the C fixed form's measure to the
// debug-time count checks: a negative count, and a count whose file size would
// pass int64_t, both stop before the multiply; a fitting count measures the
// formula exactly. The release build keeps measuring the fitting counts.
func TestCTableFixedMeasureCountAsserted(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, `package probe

fixed table Root
{
    id uint32
    weight int64
}
`)
	st := u.Tables["Root"]
	if st == nil {
		t.Fatal("the probe unit has no Root table")
	}
	body := ir.TableFixedTypeBytes(st)
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
	fixed := ir.TableFixedHeaderBytes + 4 + int64(len(layout))
	step := 8 + body
	max := (math.MaxInt64 - fixed) / step
	wantThree := fixed + 3*step
	wantMax := fixed + max*step

	const source = `#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include "ProbeTable.h"
int main( int argc, char ** argv )
{
    const int64_t fixed = kTableFixedHeaderBytes + 4 + root_fixed_layout_bytes;
    const int64_t step = 8 + root_fixed_body_bytes;
    const int64_t max = ( INT64_MAX - fixed ) / step;
    if ( argc != 2 ) { return 2; }
    if ( strcmp( argv[1], "fits" ) == 0 )
    {
        printf( "%lld %lld\n", (long long) root_fixed_measure( 3 ), (long long) root_fixed_measure( max ) );
        return 0;
    }
    if ( strcmp( argv[1], "past" ) == 0 ) { printf( "%lld\n", (long long) root_fixed_measure( max + 1 ) ); return 0; }
    if ( strcmp( argv[1], "negative" ) == 0 ) { printf( "%lld\n", (long long) root_fixed_measure( -1 ) ); return 0; }
    return 2;
}
`
	debug := buildCTableFixedMeasureProbe(t, u, source, false)

	out, err := runCTableFixedMeasureProbe(t, debug, "fits")
	if err != nil {
		t.Fatalf("fits exited abnormally: %v\n%s", err, out)
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		t.Fatalf("fits printed %q, want two sizes", out)
	}
	gotThree, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		t.Fatalf("fits' first size is not an integer: %q", fields[0])
	}
	gotMax, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		t.Fatalf("fits' second size is not an integer: %q", fields[1])
	}
	if gotThree != wantThree {
		t.Errorf("fixed_measure(3) = %d, want %d", gotThree, wantThree)
	}
	if gotMax != wantMax {
		t.Errorf("fixed_measure(%d) = %d, want %d", max, gotMax, wantMax)
	}

	for _, name := range []string{"past", "negative"} {
		out, err := runCTableFixedMeasureProbe(t, debug, name)
		if err == nil {
			t.Errorf("%s exited 0 with %q; the debug count check did not fire", name, out)
			continue
		}
		if !strings.Contains(out, "root_fixed_measure") {
			t.Errorf("%s stopped without naming the measure: %s", name, out)
		}
	}

	release := buildCTableFixedMeasureProbe(t, u, source, true)
	out, err = runCTableFixedMeasureProbe(t, release, "fits")
	if err != nil {
		t.Fatalf("release fits exited abnormally: %v\n%s", err, out)
	}
	if strings.TrimSpace(out) != strings.TrimSpace(strings.Join([]string{
		strconv.FormatInt(wantThree, 10),
		strconv.FormatInt(wantMax, 10),
	}, " ")) {
		t.Errorf("release fits printed %q, want %d %d", out, wantThree, wantMax)
	}
}

func buildCTableFixedMeasureProbe(t *testing.T, u *ir.Unit, source string, release bool) string {
	t.Helper()
	slowtest.Gate(t, "the C compiler (cc)")
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("generated C execution requires cc")
	}
	files, err := generateCUnqualified(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "probe")
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-Wshadow", "-O2", "-I", dir}
	if release {
		args = append(args, "-DNDEBUG")
	}
	args = append(args, filepath.Join(dir, "main.c"))
	for name := range files {
		if strings.HasSuffix(name, ".c") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	args = append(args, "-lm", "-o", binary)
	if output, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, output)
	}
	return binary
}

func runCTableFixedMeasureProbe(t *testing.T, binary, name string) (string, error) {
	t.Helper()
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, name).CombinedOutput()
	return string(out), err
}
