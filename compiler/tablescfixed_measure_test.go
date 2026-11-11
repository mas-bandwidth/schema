package compiler

import (
	"fmt"
	"math"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// fixedMeasureReturn is the generated C fixed_measure's size formula: the
// header bytes, the u32 layout length, the layout, and one hash plus one body
// per record.
var fixedMeasureReturn = regexp.MustCompile(`root_fixed_measure\( int64_t count \)\n\{\n(?:[^\n]*\n)*?    return (\w+) \+ 4 \+ \(int64_t\) sizeof\( (\w+) \) \+ count \* \( 8 \+ (\d+) \);`)

// TestCTableFixedMeasureCountAsserted runs the generated C fixed_measure in a
// debug build: a count from zero to the largest whose file size fits in
// int64_t measures exactly by the formula, and a negative count or one past
// that largest stops on the measure's assert. The same probe built with
// NDEBUG measures the fitting counts unchanged.
func TestCTableFixedMeasureCountAsserted(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C compiler (cc)")
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("generated C execution requires cc")
	}
	u := unitFromSource(t, "package probe\nfixed table Root {\nx uint32\ny float64\n}\n")
	files, err := generateCUnqualified(u)
	if err != nil {
		t.Fatal(err)
	}
	header, ok := files["ProbeTable.h"]
	if !ok {
		t.Fatal("no ProbeTable.h generated")
	}
	m := fixedMeasureReturn.FindSubmatch(header)
	if m == nil {
		t.Fatal("generated root_fixed_measure has no recognisable size formula")
	}
	headerBytes, layout := string(m[1]), string(m[2])
	body, err := strconv.ParseInt(string(m[3]), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	source := fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
int main(int argc, char ** argv)
{
    const int64_t fixed = %[1]s + 4 + (int64_t) sizeof( %[2]s );
    const int64_t largest = ( INT64_MAX - fixed ) / ( 8 + %[3]d );
    if ( argc != 2 ) { return 2; }
    if ( strcmp( argv[1], "fits" ) == 0 )
    {
        printf( "%%lld %%lld %%lld %%lld\n", (long long) fixed, (long long) largest,
                (long long) root_fixed_measure( 3 ), (long long) root_fixed_measure( largest ) );
        return 0;
    }
    if ( strcmp( argv[1], "past" ) == 0 )
    {
        printf( "%%lld\n", (long long) root_fixed_measure( largest + 1 ) );
        return 0;
    }
    if ( strcmp( argv[1], "negative" ) == 0 )
    {
        printf( "%%lld\n", (long long) root_fixed_measure( -1 ) );
        return 0;
    }
    return 2;
}
`, headerBytes, layout, body)
	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	build := func(out string, extra ...string) string {
		args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-Wshadow", "-O2"}
		args = append(args, extra...)
		args = append(args, "-I", dir, filepath.Join(dir, "main.c"))
		for name := range files {
			if strings.HasSuffix(name, ".c") {
				args = append(args, filepath.Join(dir, name))
			}
		}
		path := filepath.Join(dir, out)
		args = append(args, "-lm", "-o", path)
		if output, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
			t.Fatalf("compile %s: %v\n%s", out, err, output)
		}
		return path
	}
	run := func(probe, name string) (string, error) {
		ctx, cancel := slowtest.ProbeContext(t)
		defer cancel()
		output, err := exec.CommandContext(ctx, probe, name).CombinedOutput()
		return string(output), err
	}
	checkFits := func(probe string) {
		t.Helper()
		output, err := run(probe, "fits")
		if err != nil {
			t.Fatalf("%s fits: %v\n%s", filepath.Base(probe), err, output)
		}
		var fixed, largest, three, most int64
		if _, err := fmt.Sscan(output, &fixed, &largest, &three, &most); err != nil {
			t.Fatalf("%s fits: %v\n%s", filepath.Base(probe), err, output)
		}
		record := 8 + body
		want := (math.MaxInt64 - fixed) / record
		if largest != want {
			t.Fatalf("%s: largest fitting count %d, want %d", filepath.Base(probe), largest, want)
		}
		limit := big.NewInt(math.MaxInt64)
		past := new(big.Int).Add(big.NewInt(fixed), new(big.Int).Mul(big.NewInt(largest+1), big.NewInt(record)))
		if past.Cmp(limit) <= 0 {
			t.Fatalf("%s: count %d still fits, so %d is not the largest", filepath.Base(probe), largest+1, largest)
		}
		if three != fixed+3*record {
			t.Fatalf("%s: fixed_measure(3) = %d, want %d", filepath.Base(probe), three, fixed+3*record)
		}
		if most != fixed+largest*record {
			t.Fatalf("%s: fixed_measure(%d) = %d, want %d", filepath.Base(probe), largest, most, fixed+largest*record)
		}
	}

	debug := build("probe_debug")
	checkFits(debug)
	for _, name := range []string{"past", "negative"} {
		output, err := run(debug, name)
		if err == nil {
			t.Errorf("debug %s: fixed_measure returned (%s) instead of stopping on its assert", name, strings.TrimSpace(output))
			continue
		}
		if !strings.Contains(output, "fixed_measure") {
			t.Errorf("debug %s: ended (%v) without the measure's assert message:\n%s", name, err, output)
		}
	}

	release := build("probe_release", "-DNDEBUG")
	checkFits(release)
}
