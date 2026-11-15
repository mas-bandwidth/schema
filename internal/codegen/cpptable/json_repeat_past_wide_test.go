package cpptable

// The text form refuses a key repeated in ONE object whatever the index of the
// field it names (docs/SPEC-TABLES.md §16.2): the walk tracks every field the
// table declares, not only the first window of them. The engine
// (internal/tabletext) is the oracle; this compiles the leg's own C++ and
// compares the two verdicts on a table wider than one 64-bit-word window.

import (
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

const cppWideRepeatFields = 4100

// cppWideRepeatSchema is a table whose last field sits past the old fixed
// tracking window, so a repeat of it is the rule under test.
func cppWideRepeatSchema() string {
	var b strings.Builder
	b.WriteString("package probe\n\ntable Root\n{\n")
	for i := range cppWideRepeatFields {
		fmt.Fprintf(&b, "    f%d int32\n", i)
	}
	b.WriteString("}\n")
	return b.String()
}

type cppRepeatCase struct {
	name string
	text string
	ok   bool
}

var cppRepeatCases = []cppRepeatCase{
	{"once-past-window", `{"f4099":7}`, true},
	{"repeat-past-window", `{"f4099":7,"f4099":8}`, false},
	{"repeat-in-window", `{"f3":7,"f3":8}`, false},
}

func TestCppJsonRepeatRefusedPastWideTable(t *testing.T) {
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

	schema := cppWideRepeatSchema()
	u := t02CppUnit(t, schema)
	engine := tabletext.NewModel(u)
	want := make(map[string]bool, len(cppRepeatCases))
	for _, tc := range cppRepeatCases {
		inst := engine.New(engine.Lookup("Root"))
		var report tabletext.Report
		got := engine.Read(inst, []byte(tc.text), &report)
		if got != tc.ok {
			t.Fatalf("engine verdict for %s = %v, want %v (report %+v)", tc.name, got, tc.ok, report)
		}
		want[tc.name] = got
	}

	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatalf("cpp.Generate: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	maps.Copy(files, tables)
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
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
		if strings.HasSuffix(name, "Table.cpp") {
			sources = append(sources, filepath.Join(dir, name))
		}
	}
	if header == "" || len(sources) == 0 {
		t.Fatalf("Generate emitted header %q and %d table sources for the probe unit", header, len(sources))
	}

	var calls strings.Builder
	for _, tc := range cppRepeatCases {
		fmt.Fprintf(&calls, "    Check( %q, %q );\n", tc.name, tc.text)
	}
	probe := fmt.Sprintf(`#include <cstdio>
#include <cstdint>
#include <cstring>
#include "%s"
using namespace probe;

static void Check( const char * name, const char * text )
{
    RootBuilder builder;
    TableReport report;
    memset( &report, 0, sizeof( report ) );
    const bool ok = RootFromJson( builder, text, (int64_t) strlen( text ), &report );
    printf( "%%s %%s\n", name, ok ? "ok" : "refused" );
    fflush( stdout );
}

int main( void )
{
%s    return 0;
}
`, header, calls.String())
	probePath := filepath.Join(dir, "probe.cpp")
	if err := os.WriteFile(probePath, []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "probe")
	args := append([]string{"-std=c++17", "-O1", "-I" + dir, probePath}, sources...)
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

	got := make(map[string]bool, len(cppRepeatCases))
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("the C++ probe printed %q, want `<case> ok|refused`", line)
		}
		switch fields[1] {
		case "ok":
			got[fields[0]] = true
		case "refused":
			got[fields[0]] = false
		default:
			t.Fatalf("the C++ probe printed %q, want `<case> ok|refused`", line)
		}
	}
	for _, tc := range cppRepeatCases {
		leg, ok := got[tc.name]
		if !ok {
			t.Errorf("%s: the C++ probe printed no verdict", tc.name)
			continue
		}
		if leg != want[tc.name] {
			t.Errorf("%s: the C++ read = %v, the engine = %v", tc.name, leg, want[tc.name])
		}
	}
}
