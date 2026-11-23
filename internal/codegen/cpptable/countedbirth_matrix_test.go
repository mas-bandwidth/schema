// THE BIRTH VALUE OF A COUNTED ARRAY IN C++, ACROSS ELEMENT KINDS
// (docs/SPEC-TABLES.md §3.3 and the §22.2 [M..N]T row; the Go engine's
// encodeArray and messageElides): a plain counted field holding exactly Min
// elements, each at its default (a float by its bits, a table by a body that
// is its terminator alone), stays off the wire on the variable save and on the
// message form, and rides at any other count or with one live element. The
// text form never elides: its JSON carries the elements. TestCPPCountedArrayBirthValue
// holds the int32 case to fixed hex; this holds an int32, a table element and a
// float64 (with its -0) to the bytes the engine writes, case by case.
package cpptable

import (
	"bytes"
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
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

const cppBirthMatrixSchema = `package probe

type Rec
{
    n int32 = 55
    on bool
}

fixed table FixI32 { few [2..4]int32 }
fixed table FixRec { few [2..4]Rec }
fixed table FixF64 { few [2..4]float64 }
table VarI32 { few [2..4]int32 }
table VarRec { few [2..4]Rec }
table VarF64 { few [2..4]float64 }
`

// cppBirthCase: the engine's text for the value, and the C++ statements that
// place it on `v`, a pointer to the fixed or the variable root.
type cppBirthCase struct {
	name, root, text, place string
}

func cppBirthCases() []cppBirthCase {
	return []cppBirthCase{
		{"fresh", "I32", "", ""},
		{"min_default", "I32", `{"few":[0,0]}`, "v->few[0] = 0; v->few[1] = 0;"},
		{"min_one", "I32", `{"few":[0,7]}`, "v->few[1] = 7;"},
		{"min_plus1_default", "I32", `{"few":[0,0,0]}`, "v->few_count = 3;"},
		{"fresh", "Rec", "", ""},
		{"min_default", "Rec", `{"few":[{},{}]}`, "v->few[0].n = 55; v->few[1].n = 55;"},
		{"min_one", "Rec", `{"few":[{},{"n":56}]}`, "v->few[1].n = 56;"},
		{"min_plus1_default", "Rec", `{"few":[{},{},{}]}`, "v->few_count = 3;"},
		{"fresh", "F64", "", ""},
		{"min_default", "F64", `{"few":[0,0]}`, "v->few[0] = 0.0; v->few[1] = 0.0;"},
		{"min_negzero", "F64", `{"few":[0,-0.0]}`, "v->few[1] = -0.0;"},
	}
}

const cppBirthMatrixHead = `#include <csignal>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <string>
#include "ProbeTable.h"
using namespace probe;

static std::string hex( const uint8_t * p, int64_t n )
{
    static const char digits[] = "0123456789abcdef";
    std::string s;
    for ( int64_t i = 0; i < n; i++ ) { s += digits[p[i] >> 4]; s += digits[p[i] & 15]; }
    return s;
}

template <typename Fixed> static void message( const char * key, const Fixed & value, int64_t (*measure)( const Fixed *, int64_t, TableReport * ), int64_t (*save)( const Fixed *, int64_t, uint8_t *, int64_t, TableReport * ) )
{
    TableReport report;
    uint8_t out[256];
    const int64_t n = measure( &value, 1, &report );
    if ( n < 0 || n > (int64_t) sizeof( out ) || save( &value, 1, out, n, &report ) != n ) { std::printf( "%s msg refused\n", key ); return; }
    std::printf( "%s msg %s\n", key, hex( out, n ).c_str() );
}

template <typename Var, typename Builder> static int variable( const char * key, Builder & b, int64_t (*measure)( const Var *, TableAllocator ), int64_t (*save)( const Var *, uint8_t *, int64_t, TableAllocator ), int64_t (*json)( const Var *, char *, int64_t, TableAllocator ) )
{
    if ( !b.Lock() ) { return 5; }
    uint8_t out[256];
    char text[512];
    int64_t n = measure( b.AsConst(), TableDefaultAllocator() );
    if ( n < 0 || n > (int64_t) sizeof( out ) || save( b.AsConst(), out, n, TableDefaultAllocator() ) != n ) { std::printf( "%s var refused\n", key ); }
    else { std::printf( "%s var %s\n", key, hex( out, n ).c_str() ); }
    n = json( b.AsConst(), text, (int64_t) sizeof( text ), TableDefaultAllocator() );
    if ( n < 0 || n > (int64_t) sizeof( text ) ) { std::printf( "%s json refused\n", key ); }
    else { std::printf( "%s json %s\n", key, hex( (const uint8_t *) text, n ).c_str() ); }
    return 0;
}

extern "C" void on_abort( int ) { std::_Exit( 3 ); }

int main()
{
    std::signal( SIGABRT, on_abort );
`

func TestCPPCountedArrayBornAcrossElementKinds(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C++ compiler (c++)")
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	if _, err := exec.LookPath(cxx); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and %s is not found: %v", cxx, err)
		}
		t.Skipf("generated C++ execution requires %s: %v", cxx, err)
	}
	u := cppUnitFrom(t, cppBirthMatrixSchema)
	model := tabletext.NewModel(u)
	place := func(root, text string) *tabletext.Instance {
		inst := model.New(model.Lookup(root))
		if text != "" {
			var rep tabletext.Report
			if !model.Read(inst, []byte(text), &rep) {
				t.Fatalf("%s: the engine refused %s: %+v", root, text, rep)
			}
		}
		return inst
	}
	var p strings.Builder
	p.WriteString(cppBirthMatrixHead)
	want := map[string]string{}
	for _, c := range cppBirthCases() {
		key := "Fix" + c.root + "_" + c.name
		vkey := "Var" + c.root + "_" + c.name
		fmt.Fprintf(&p, "    { Fix%[1]s fx; Fix%[1]s * v = &fx; (void) v; %[2]s message<Fix%[1]s>( %[3]q, fx, Fix%[1]sMeasureMessages, Fix%[1]sSaveMessages ); }\n", c.root, c.place, key)
		fmt.Fprintf(&p, "    { Var%[1]sBuilder b; Var%[1]s * v = b.GetRoot(); (void) v; %[2]s if ( int rc = variable<Var%[1]s>( %[3]q, b, Var%[1]sMeasure, Var%[1]sSave, Var%[1]sToJson ) ) { return rc; } }\n", c.root, c.place, vkey)
		msg, err := tablewire.EncodeMessages(model, []*tabletext.Instance{place("Fix"+c.root, c.text)})
		if err != nil {
			t.Fatal(err)
		}
		variable, err := tablewire.EncodeVariable(model, place("Var"+c.root, c.text))
		if err != nil {
			t.Fatal(err)
		}
		text, err := model.Write(place("Var"+c.root, c.text))
		if err != nil {
			t.Fatal(err)
		}
		want[key+" msg"] = hex.EncodeToString(msg)
		want[vkey+" var"] = hex.EncodeToString(variable)
		want[vkey+" json"] = hex.EncodeToString(bytes.TrimRight(text, "\n"))
	}
	p.WriteString("    return 0;\n}\n")
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".cpp") {
			sources = append(sources, filepath.Join(dir, name))
		}
	}
	mainPath := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(mainPath, []byte(p.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "probe")
	args := append([]string{"-std=c++17", "-Wall", "-Wextra", "-Werror", "-O1", "-I", dir, mainPath}, sources...)
	if out, err := testproc.Command(t, cxx, append(args, "-o", exe)...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	out, err := testproc.Command(t, exe).CombinedOutput()
	if err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) == 3 {
			got[f[0]+" "+f[1]] = f[2]
			if f[1] == "json" {
				got[f[0]+" "+f[1]] = strings.TrimSuffix(f[2], "0a")
			}
		}
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: C++ wrote %q, the engine wrote %q", k, got[k], w)
		}
	}
}
