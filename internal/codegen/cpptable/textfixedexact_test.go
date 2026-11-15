package cpptable

// The hand-editable text form decides a fixed-point token on the token's own
// decimal digits, never through a double: a value is exact for its declared
// fraction, or it is a kind mismatch (docs/SPEC-TABLES.md §16.9.2). The C++
// leg and the engine (internal/tabletext) read the same tokens, so every
// verdict and every landed value agrees, and the run is free of undefined
// behaviour for every fraction width the checker admits.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const cppTextFixedExactSchema = `package fixedexact

fixed table Root
{
    big fixed(112, 16) | min = -9000000000000000000, max = 9000000000000000000
    wide ufixed(8, 120) | min = 0, max = 255
}
`

type cppTextFixedExactCase struct {
	name  string
	field string
	token string
}

var cppTextFixedExactCases = []cppTextFixedExactCase{
	{"fixed-max", "big", "9000000000000000000"},
	{"fixed-min", "big", "-9000000000000000000"},
	{"fixed-finer-near-max", "big", "8000000000000000000.00000762939453125"},
	{"fixed-finer-near-min", "big", "-8000000000000000000.00000762939453125"},
	{"fixed-finer-small", "big", "0.00000762939453125"},
	{"ufixed-zero", "wide", "0"},
	{"ufixed-one", "wide", "1"},
	{"ufixed-neighbour", "wide", "2"},
	{"ufixed-max", "wide", "255"},
	{"ufixed-over", "wide", "256"},
	{"ufixed-finer", "wide", "0.0000000000000000000000000000000000000001"},
}

const cppTextFixedExactProbe = `#include <cstdio>
#include <cstring>
#include <cstdint>
#include <cstdlib>
#include "%s"
using namespace fixedexact;

static const char * kText[] = {
%s
};

static const int kCases = (int) ( sizeof( kText ) / sizeof( kText[ 0 ] ) );

int main( void )
{
    const char * dir = getenv( "SCHEMA_FIXED_EXACT_DIR" );
    if ( dir == NULL ) { std::fprintf( stderr, "SCHEMA_FIXED_EXACT_DIR is unset\n" ); return 1; }
    static char text[ 1 << 16 ];
    for ( int i = 0; i < kCases; i++ )
    {
        Root v;
        RootReset( v );
        const char * refusal = NULL;
        char path[ 1024 ];
        std::snprintf( path, sizeof( path ), "%%s/case-%%d", dir, i );
        const bool ok = RootLoadTextForm( kText[ i ], (int64_t) std::strlen( kText[ i ] ), v, &refusal );
        FILE * f = std::fopen( path, "wb" );
        if ( f == NULL ) { std::fprintf( stderr, "case %%d: open failed\n", i ); return 1; }
        const char status = ok ? '1' : '0';
        bool wrote = std::fwrite( &status, 1, 1, f ) == 1;
        if ( ok )
        {
            const int64_t size = RootToJsonMeasure( v );
            if ( size < 0 || size > (int64_t) sizeof( text ) ) { std::fprintf( stderr, "case %%d: RootToJsonMeasure refused (%%lld)\n", i, (long long) size ); return 1; }
            if ( RootToJson( v, text, (int64_t) sizeof( text ) ) != size ) { std::fprintf( stderr, "case %%d: RootToJson wrote short\n", i ); return 1; }
            wrote = wrote && std::fwrite( text, 1, (size_t) size, f ) == (size_t) size;
        }
        if ( std::fclose( f ) != 0 || !wrote ) { std::fprintf( stderr, "case %%d: write failed\n", i ); return 1; }
    }
    std::printf( "ok %%d\n", kCases );
    return 0;
}
`

// TestCppTextFixedPointTokenExact holds the C++ hand-editable text form to the
// engine over a fixed(112, 16) and a ufixed(8, 120): an in-range whole number
// reads, a fraction finer than 2^-F refuses, and the verdict and the landed
// value agree with internal/tabletext for the same token.
func TestCppTextFixedPointTokenExact(t *testing.T) {
	t.Parallel()
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
	serialize, ok := cppSerializeDir(t)
	if !ok {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("the sibling runtime serialize is absent at %s: the generated header's 128-bit storage does not resolve", serialize)
		}
		t.Skipf("named as not run: the sibling serialize runtime is not staged at %s", serialize)
	}
	u := t02CppUnit(t, cppTextFixedExactSchema)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatalf("cpp.Generate: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	maps.Copy(files, tables)
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
	if header == "" || len(sources) == 0 {
		t.Fatalf("Generate emitted header %q and %d table sources for the probe unit", header, len(sources))
	}

	literals := make([]string, 0, len(cppTextFixedExactCases))
	for _, c := range cppTextFixedExactCases {
		literals = append(literals, strconv.Quote(fmt.Sprintf("(:%s %s)", c.field, c.token)))
	}
	probe := filepath.Join(src, "probe.cpp")
	if err := os.WriteFile(probe, []byte(fmt.Sprintf(cppTextFixedExactProbe, header, strings.Join(literals, ",\n"))), 0o600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(src, "probe")
	args := append([]string{"-std=c++17", "-O1", "-fsanitize=undefined", "-fno-sanitize-recover=undefined", "-I" + src, "-I" + serialize, probe}, sources...)
	args = append(args, "-o", exe)
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("the probe did not compile: %v\n%s", err, out)
	}
	outDir := filepath.Join(src, "out")
	if err := os.Mkdir(outDir, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe)
	cmd.Env = append(os.Environ(), "SCHEMA_FIXED_EXACT_DIR="+outDir)
	probeOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the C++ probe failed: %v\n%s", err, probeOut)
	}
	if strings.Contains(string(probeOut), "runtime error:") {
		t.Fatalf("the C++ probe reported undefined behaviour:\n%s", probeOut)
	}

	m := tabletext.NewModel(u)
	for i, c := range cppTextFixedExactCases {
		raw, err := os.ReadFile(filepath.Join(outDir, fmt.Sprintf("case-%d", i)))
		if err != nil {
			t.Fatalf("%s: read probe result: %v", c.name, err)
		}
		cppOK := len(raw) > 0 && raw[0] == '1'
		cppJSON := raw[1:]

		jsonText := []byte(fmt.Sprintf("{%q: %s}", c.field, c.token))
		inst := m.New(m.Lookup("Root"))
		var report tabletext.Report
		engineOK := m.Read(inst, jsonText, &report)
		var engineJSON []byte
		if engineOK {
			engineJSON, err = m.Write(inst)
			if err != nil {
				t.Fatalf("%s: engine write: %v", c.name, err)
			}
		}
		if cppOK != engineOK {
			t.Errorf("%s: C++ %s %q, engine %s %q", c.name, cppVerdict(cppOK), cppJSON, cppVerdict(engineOK), engineJSON)
			continue
		}
		if cppOK {
			// The text form's writer closes its document with one newline; the
			// engine's Write answers the value alone. The bytes between are the
			// value both landed, so they must match exactly.
			text := string(cppJSON)
			if !strings.HasSuffix(text, "\n") {
				t.Errorf("%s: the C++ text form did not close its document with a newline: %q", c.name, text)
				continue
			}
			if strings.TrimSuffix(text, "\n") != string(engineJSON) {
				t.Errorf("%s: C++ text %q, engine text %q", c.name, text, engineJSON)
			}
		}
	}
}

func cppVerdict(ok bool) string {
	if ok {
		return "read"
	}
	return "refused"
}
