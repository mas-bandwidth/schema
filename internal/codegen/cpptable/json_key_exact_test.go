package cpptable

// A JSON object key names a declared name only when every decoded byte equals
// the name (docs/SPEC-TABLES.md §16.2): an escaped U+0000 inside a key is a
// byte of the key, never a terminator. The C++ FromJson and the engine must
// reach the same verdict, count the same unknowns and land the same value on
// one text, so every key lands where the engine lands it.

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const cppJsonKeyExactSchema = `package probe

enum Mode { Idle, Run }
flags Toggles { A, B }

fixed table Sample
{
    x float32
}

fixed table Root
{
    pose    Sample
    tilt    float32
    mode    Mode
    toggles Toggles
    keyed   [Mode]int32
}

table Node
{
    x float32
}

table GraphRoot
{
    head *Node
}
`

type cppJsonKeyCase struct {
	name    string
	root    string
	text    string
	control bool
}

var cppJsonKeyCases = []cppJsonKeyCase{
	{"plain-field", "Root", `{"tilt":1.5}`, true},
	{"field-key-zero", "Root", `{"tilt\u0000":1.5}`, false},
	{"nested-field-key-zero", "Root", `{"pose":{"x\u0000zz":0.25}}`, false},
	{"plain-enum", "Root", `{"mode":"Run"}`, true},
	{"enum-value-zero", "Root", `{"mode":"Run\u0000zz"}`, false},
	{"plain-flags", "Root", `{"toggles":["A"]}`, true},
	{"flag-name-zero", "Root", `{"toggles":["A\u0000zz"]}`, false},
	{"plain-keyed", "Root", `{"keyed":{"Run":3}}`, true},
	{"keyed-slot-key-zero", "Root", `{"keyed":{"Run\u0000zz":3}}`, false},
	{"pointer-first-key-zero", "GraphRoot", `{"head":{"&node\u0000zz":1,"x":0.5}}`, false},
	{"pointer-field-key-zero", "GraphRoot", `{"head":{"x\u0000zz":0.5}}`, false},
}

type cppJsonKeyAnswer struct {
	ok      bool
	unknown int64
	value   string
}

func cppJsonKeyProbeSource(header string) string {
	var fixed, graph strings.Builder
	for _, c := range cppJsonKeyCases {
		line := fmt.Sprintf("        { %q, %q },\n", c.name, c.text)
		if c.root == "Root" {
			fixed.WriteString(line)
		} else {
			graph.WriteString(line)
		}
	}
	return fmt.Sprintf(`#include <stdio.h>
#include <string.h>
#include "%s"

using namespace probe;

typedef struct Case
{
    const char * name;
    const char * text;
} Case;

static void Print( const char * name, bool ok, const TableReport & report, int64_t bytes, const char * out )
{
    printf( "RESULT %%s %%d %%lld ", name, ok ? 1 : 0, (long long) report.unknown );
    if ( bytes < 0 ) { printf( "-" ); }
    else
    {
        for ( int64_t j = 0; j < bytes; j++ ) { printf( "%%02x", (unsigned char) out[j] ); }
    }
    printf( "\n" );
}

int main( void )
{
    static const Case fixed_cases[] = {
%s    };
    static const Case graph_cases[] = {
%s    };
    static char out[1 << 16];

    for ( size_t i = 0; i < sizeof( fixed_cases ) / sizeof( fixed_cases[0] ); i++ )
    {
        Root value;
        RootReset( value );
        TableReport report;
        bool ok = RootFromJson( value, fixed_cases[i].text, (int64_t) strlen( fixed_cases[i].text ), &report );
        int64_t bytes = RootToJson( value, out, (int64_t) sizeof( out ) );
        Print( fixed_cases[i].name, ok, report, bytes, out );
    }

    for ( size_t i = 0; i < sizeof( graph_cases ) / sizeof( graph_cases[0] ); i++ )
    {
        GraphRootBuilder builder;
        TableReport report;
        bool ok = GraphRootFromJson( builder, graph_cases[i].text, (int64_t) strlen( graph_cases[i].text ), &report );
        int64_t bytes = GraphRootToJson( builder.GetRoot(), out, (int64_t) sizeof( out ) );
        Print( graph_cases[i].name, ok, report, bytes, out );
    }

    return 0;
}
`, header, fixed.String(), graph.String())
}

func TestCppJsonKeyMatchesWholeKey(t *testing.T) {
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

	u := t02CppUnit(t, cppJsonKeyExactSchema)
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
	if header == "" {
		t.Fatal("Generate emitted no table header for the probe unit")
	}
	probe := filepath.Join(src, "probe.cpp")
	if err := os.WriteFile(probe, []byte(cppJsonKeyProbeSource(header)), 0o600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(src, "probe")
	args := append([]string{"-std=c++17", "-O1", "-I" + src, probe}, sources...)
	args = append(args, "-o", exe)
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("the probe did not compile: %v\n%s", err, out)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	out, err := exec.CommandContext(ctx, exe).CombinedOutput()
	if err != nil {
		t.Fatalf("the C++ probe failed: %v\n%s", err, out)
	}

	got := map[string]cppJsonKeyAnswer{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		var name, value string
		var ok int
		var unknown int64
		if _, err := fmt.Sscanf(line, "RESULT %s %d %d %s", &name, &ok, &unknown, &value); err != nil {
			t.Fatalf("parse probe line %q: %v", line, err)
		}
		got[name] = cppJsonKeyAnswer{ok: ok != 0, unknown: unknown, value: value}
	}

	m := tabletext.NewModel(u)
	for _, c := range cppJsonKeyCases {
		def := m.Lookup(c.root)
		if def == nil {
			t.Fatalf("%s: the engine has no %s in the probe unit", c.name, c.root)
		}
		inst := m.New(def)
		var report tabletext.Report
		ok := m.Read(inst, []byte(c.text), &report)
		want := cppJsonKeyAnswer{ok: ok, unknown: int64(report.Unknown)}
		if text, err := m.Write(inst); err != nil {
			t.Fatalf("%s: engine write: %v", c.name, err)
		} else {
			want.value = hex.EncodeToString(append(text, '\n'))
		}

		answer, seen := got[c.name]
		if !seen {
			t.Errorf("%s: the C++ probe reported no answer", c.name)
			continue
		}
		if c.control {
			if !want.ok || want.unknown != 0 || !answer.ok || answer.unknown != 0 {
				t.Errorf("%s (control): a plain key must read clean: engine ok=%v unknown=%d, C++ ok=%v unknown=%d",
					c.name, want.ok, want.unknown, answer.ok, answer.unknown)
			}
		}
		if answer.ok != want.ok || answer.unknown != want.unknown || answer.value != want.value {
			t.Errorf("%s: C++ ok=%v unknown=%d value=%s, engine ok=%v unknown=%d value=%s",
				c.name, answer.ok, answer.unknown, answer.value, want.ok, want.unknown, want.value)
		}
	}
}
