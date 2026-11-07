package cpptable

// I13 (docs/PORTING.md) is the text differential against a third
// implementation, here for the C++ leg: a C++ probe builds random instances
// with the generated storage structs and a splitmix64, then writes each as
// (wire, text) with the generated RootSave and RootToJson; the compiler's own
// engine (internal/tabletext, written from the spec and from neither backend)
// reads the same wire and writes its text, the two are byte-compared, then
// the engine packs the leg's text back to wire and that is byte-compared with
// the leg's wire. It is the port of the Go leg's cell
// (internal/codegen/gotable/textdifferential_test.go): the same schema, the
// same seed and count, the same draw order, finite floats only (both writers
// refuse a non-finite one), instance 0 the reset proof.
//
// The C++ float writer is emitted source over snprintf("%.*g") and strtof /
// strtod round trips; the engine's is internal/tabletext/write.go's formatG.
// Two implementations in two languages, neither written from the other.
//
// Compiling and running the probe makes this a functional test: it skips by
// name without SCHEMA_SLOW, and fails by name under SCHEMA_REQUIRE_CORPUS=1
// when no C++ compiler is found. It is reached from make
// tables-cpp-text-differential.

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
	"maps"
)

const cppTextDifferentialSchema = `package probe
type Sample {
 s float32
 d float64
}
fixed table Root {
 a float32
 b float64
 pair Sample
 many [..8]float32
 deep [..8]float64
}
`

const cppTextDifferentialProbe = `#include <stdio.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <math.h>
#include "%s"
using namespace probe;

static uint64_t state;

static uint64_t SplitMix64Next()
{
    state += 0x9E3779B97F4A7C15ull;
    uint64_t z = state;
    z = ( z ^ ( z >> 30 ) ) * 0xBF58476D1CE4E5B9ull;
    z = ( z ^ ( z >> 27 ) ) * 0x94D049BB133111EBull;
    return z ^ ( z >> 31 );
}

static float NextFloat32()
{
    for ( ;; )
    {
        uint32_t bits = (uint32_t) SplitMix64Next();
        float f;
        memcpy( &f, &bits, sizeof( f ) );
        if ( isfinite( f ) ) { return f; }
    }
}

static double NextFloat64()
{
    for ( ;; )
    {
        uint64_t bits = SplitMix64Next();
        double f;
        memcpy( &f, &bits, sizeof( f ) );
        if ( isfinite( f ) ) { return f; }
    }
}

static bool WriteFile( const char * dir, int i, const char * ext, const void * data, int64_t bytes )
{
    char path[1024];
    snprintf( path, sizeof( path ), "%%s/inst-%%d.%%s", dir, i, ext );
    FILE * f = fopen( path, "wb" );
    if ( f == NULL ) { return false; }
    bool ok = fwrite( data, 1, (size_t) bytes, f ) == (size_t) bytes;
    return fclose( f ) == 0 && ok;
}

int main( void )
{
    const char * dir = getenv( "SCHEMA_TEXT_DIFF_DIR" );
    const char * seed = getenv( "SCHEMA_TEXT_DIFF_SEED" );
    const char * count = getenv( "SCHEMA_TEXT_DIFF_N" );
    if ( dir == NULL || seed == NULL || count == NULL ) { fprintf( stderr, "SCHEMA_TEXT_DIFF_{DIR,SEED,N} are unset\n" ); return 1; }
    state = strtoull( seed, NULL, 10 );
    const int n = atoi( count );
    static uint8_t wire[1 << 16];
    static char text[1 << 16];
    for ( int i = 0; i < n; i++ )
    {
        Root v;
        RootReset( v );
        if ( i != 0 )
        {
            v.a = NextFloat32();
            v.b = NextFloat64();
            v.pair.s = NextFloat32();
            v.pair.d = NextFloat64();
            v.many_count = 8;
            for ( int j = 0; j < 8; j++ ) { v.many[j] = NextFloat32(); }
            v.deep_count = 8;
            for ( int j = 0; j < 8; j++ ) { v.deep[j] = NextFloat64(); }
        }
        int64_t size = RootMeasure( v );
        if ( size < 0 || size > (int64_t) sizeof( wire ) ) { fprintf( stderr, "instance %%d: RootMeasure refused (%%lld)\n", i, (long long) size ); return 1; }
        if ( RootSave( v, wire, (int64_t) sizeof( wire ) ) != size ) { fprintf( stderr, "instance %%d: RootSave wrote short\n", i ); return 1; }
        int64_t textSize = RootToJsonMeasure( v );
        if ( textSize < 0 || textSize > (int64_t) sizeof( text ) ) { fprintf( stderr, "instance %%d: RootToJsonMeasure refused (%%lld)\n", i, (long long) textSize ); return 1; }
        if ( RootToJson( v, text, (int64_t) sizeof( text ) ) != textSize ) { fprintf( stderr, "instance %%d: RootToJson wrote short\n", i ); return 1; }
        if ( !WriteFile( dir, i, "wire", wire, size ) || !WriteFile( dir, i, "json", text, textSize ) ) { fprintf( stderr, "instance %%d: write failed\n", i ); return 1; }
    }
    printf( "ok %%d\n", n );
    return 0;
}
`

const (
	// The Go cell's constants (internal/codegen/gotable/textdifferential_test.go),
	// so the legs draw the same float patterns in the same order.
	cppTextDifferentialSeed = 24845619678
	cppTextDifferentialN    = 200
)

// runCppTextDifferential generates the C++ unit for the probe schema, applies
// an optional in-memory edit to the emitted files, compiles and runs the
// probe, then runs the engine comparison. It returns the divergence messages
// and a non-nil error when any instance differs.
func runCppTextDifferential(t *testing.T, edit func(map[string][]byte)) ([]byte, error) {
	t.Helper()
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
	u := t02CppUnit(t, cppTextDifferentialSchema)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatalf("cpp.Generate: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	maps.Copy(files, tables)
	if edit != nil {
		edit(files)
	}
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
	if header == "" || len(sources) != 1 {
		t.Fatalf("Generate emitted header %q and %d table sources for the probe unit", header, len(sources))
	}
	probe := filepath.Join(src, "probe.cpp")
	if err := os.WriteFile(probe, []byte(fmt.Sprintf(cppTextDifferentialProbe, header)), 0o600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(src, "probe")
	args := append([]string{"-std=c++17", "-O1", "-I" + src, probe}, sources...)
	args = append(args, "-o", exe)
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		return out, fmt.Errorf("the probe did not compile: %w", err)
	}
	outDir := filepath.Join(src, "out")
	if err := os.Mkdir(outDir, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe)
	cmd.Env = append(os.Environ(),
		"SCHEMA_TEXT_DIFF_DIR="+outDir,
		"SCHEMA_TEXT_DIFF_SEED="+strconv.FormatUint(cppTextDifferentialSeed, 10),
		"SCHEMA_TEXT_DIFF_N="+strconv.Itoa(cppTextDifferentialN),
	)
	probeOut, err := cmd.CombinedOutput()
	if err != nil {
		return probeOut, fmt.Errorf("the C++ probe failed: %w", err)
	}
	m := tabletext.NewModel(u)
	msgs, err := compareCppTextDifferential(m, outDir, cppTextDifferentialN)
	if err != nil {
		return probeOut, err
	}
	if len(msgs) > 0 {
		return []byte(strings.Join(msgs, "\n\n")), fmt.Errorf("%d instance(s) differ", len(msgs))
	}
	return probeOut, nil
}

// TestCppTextDifferentialAgainstTheGoEngine is the row: the generated C++ text
// writer and the engine's float writer (internal/tabletext/write.go,
// writeFloat/formatG; docs/SPEC-TABLES.md §16.2) agree byte for byte over 200
// random float-only instances, in both directions.
func TestCppTextDifferentialAgainstTheGoEngine(t *testing.T) {
	out, err := runCppTextDifferential(t, nil)
	if err != nil {
		t.Fatalf("C++ text differential: %v\n%s", err, out)
	}
}

const cppTextDifferentialAnchor = "int high = single ? 9 : 17;"

// TestCppTextDifferentialNegativeControl caps a float32's text at SIX
// significant digits in the emitted C++ (in memory), so a float32 that needs
// seven, eight or nine renders short and only the cross-implementation
// comparison sees it. The row must go red on the text comparison, not on a
// compile error or a probe failure.
func TestCppTextDifferentialNegativeControl(t *testing.T) {
	out, err := runCppTextDifferential(t, func(files map[string][]byte) {
		total := 0
		for _, data := range files {
			total += bytes.Count(data, []byte(cppTextDifferentialAnchor))
		}
		if total != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the sabotage anchor %q appeared %d times across the emitted files, want 1", cppTextDifferentialAnchor, total)
		}
		for name, data := range files {
			files[name] = bytes.Replace(data, []byte(cppTextDifferentialAnchor), []byte("int high = single ? 6 : 17; /* SABOTAGED */"), 1)
		}
	})
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: a C++ float32 writer capped at six significant digits left the row GREEN\n%s", out)
	}
	if !bytes.Contains(out, []byte("leg text and engine text differ")) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the row went red, but not on the text comparison: %v\n%s", err, out)
	}
}

// compareCppTextDifferential runs both directions of the engine comparison for
// every instance the probe wrote. Direction one: the engine reads the leg's
// wire and writes its text, byte-compared against the leg's text. Direction
// two: the engine packs the leg's text back to wire, byte-compared against the
// leg's wire. Divergences are collected as messages, not failures, so the
// negative control can assert on them.
func compareCppTextDifferential(m *tabletext.Model, dir string, n int) ([]string, error) {
	var msgs []string
	for i := range n {
		wire, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.wire", i)))
		if err != nil {
			return msgs, fmt.Errorf("read inst-%d.wire: %w", i, err)
		}
		legJSON, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.json", i)))
		if err != nil {
			return msgs, fmt.Errorf("read inst-%d.json: %w", i, err)
		}

		d1, err := os.MkdirTemp(dir, "d1-")
		if err != nil {
			return msgs, err
		}
		report, err := tablepack.UnpackOneFile(m, "Root", wire, d1)
		if err != nil {
			os.RemoveAll(d1)
			return msgs, fmt.Errorf("instance %d: UnpackOneFile: %w", i, err)
		}
		if report != (tabletext.Report{}) {
			os.RemoveAll(d1)
			return msgs, fmt.Errorf("instance %d: UnpackOneFile report not clean: %+v", i, report)
		}
		engineJSON, err := os.ReadFile(filepath.Join(d1, "Root.json"))
		os.RemoveAll(d1)
		if err != nil {
			return msgs, fmt.Errorf("instance %d: read engine Root.json: %w", i, err)
		}
		if !bytes.Equal(engineJSON, legJSON) {
			off, win := cppTextWindow(legJSON, engineJSON)
			msgs = append(msgs, fmt.Sprintf("instance %d: leg text and engine text differ at byte %d\n%s\nfloats: %s", i, off, win, cppFloatBitsOf(m, wire)))
			continue
		}

		d2, err := os.MkdirTemp(dir, "d2-")
		if err != nil {
			return msgs, err
		}
		if err := os.WriteFile(filepath.Join(d2, "Root.json"), legJSON, 0o600); err != nil {
			os.RemoveAll(d2)
			return msgs, err
		}
		wireBack, _, report2, err := tablepack.Pack(m, "Root", d2)
		os.RemoveAll(d2)
		if err != nil {
			return msgs, fmt.Errorf("instance %d: Pack: %w", i, err)
		}
		if report2 != (tabletext.Report{}) {
			return msgs, fmt.Errorf("instance %d: Pack report not clean: %+v", i, report2)
		}
		if !bytes.Equal(wireBack, wire) {
			msgs = append(msgs, fmt.Sprintf("instance %d: engine wire packed from leg text differs from leg wire: engine=%x leg=%x", i, wireBack, wire))
		}
	}
	return msgs, nil
}

// cppFloatBitsOf decodes one wire with the engine and spells every float
// field's bits, so a divergence replays from the bits alone.
func cppFloatBitsOf(m *tabletext.Model, wire []byte) string {
	inst := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if _, err := tablewire.Decode(m, inst, wire, &report); err != nil {
		return fmt.Sprintf("<decode error: %v>", err)
	}
	var b strings.Builder
	var walk func(fields []tabletext.Field, prefix string)
	walk = func(fields []tabletext.Field, prefix string) {
		for i := range fields {
			f := &fields[i]
			name := prefix + f.Def.Name
			emit := func(c tabletext.Cell, slot string) {
				switch f.Def.Type.Kind {
				case ir.TFloat32:
					fmt.Fprintf(&b, "%s=0x%08x(f32) ", slot, math.Float32bits(float32(c.F)))
				case ir.TFloat64:
					fmt.Fprintf(&b, "%s=0x%016x(f64) ", slot, math.Float64bits(c.F))
				}
			}
			if len(f.Elems) > 0 {
				for j := 0; j < f.Count && j < len(f.Elems); j++ {
					c := f.Elems[j]
					if c.Tab != nil {
						walk(c.Tab.Fields, fmt.Sprintf("%s[%d].", name, j))
					} else {
						emit(c, fmt.Sprintf("%s[%d]", name, j))
					}
				}
				continue
			}
			if f.Cell.Tab != nil {
				walk(f.Cell.Tab.Fields, name+".")
				continue
			}
			emit(f.Cell, name)
		}
	}
	walk(inst.Fields, "")
	return strings.TrimSpace(b.String())
}

// cppTextWindow returns the first byte offset where two texts differ and a
// window of each around it.
func cppTextWindow(a, b []byte) (int, string) {
	n := min(len(b), len(a))
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	lo := max(i-32, 0)
	hiA := min(i+32, len(a))
	hiB := min(i+32, len(b))
	return i, fmt.Sprintf("leg[%d:%d]=%q engine[%d:%d]=%q", lo, hiA, a[lo:hiA], lo, hiB, b[lo:hiB])
}
