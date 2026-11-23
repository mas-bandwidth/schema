// THE BIRTH VALUE OF A COUNTED ARRAY IN C, ACROSS ELEMENT KINDS
// (docs/SPEC-TABLES.md §3.3 and the §22.2 [M..N]T row; the Go engine's
// encodeArray and messageElides): a plain counted field holding exactly Min
// elements, each at its default (a float by its bits, a table by a body that
// is its terminator alone), stays off the wire on the variable save and on the
// message form, and rides at any other count or with one live element. The
// text form never elides: its JSON carries the elements. TestCCountedArrayBirthValue
// holds the int32 case to fixed hex; this holds an int32, a table element and a
// float64 (with its -0) to the bytes the engine writes, case by case.
package ctable

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

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

const cBirthMatrixSchema = `package probe

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

// cBirthCase: the engine's text for the value, and the C statements that place
// it on `v`, a pointer to the fixed or the variable root.
type cBirthCase struct {
	name, root, text, place string
}

func cBirthCases() []cBirthCase {
	return []cBirthCase{
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

const cBirthMatrixHead = `#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#define schema_assert( x ) do { if ( !( x ) ) { fprintf( stderr, "write check: %s\n", #x ); exit( 3 ); } } while ( 0 )
#include "ProbeTable.h"

static void phex( const uint8_t * p, int64_t n )
{
    int64_t i;
    for ( i = 0; i < n; i++ ) { printf( "%02x", p[i] ); }
    printf( "\n" );
}

#define MSG( key, snake ) do { \
    TableReport rep; uint8_t out[ 256 ]; int64_t n; \
    memset( &rep, 0, sizeof( rep ) ); \
    n = snake##_measure_messages( v, 1, &rep ); \
    if ( n < 0 || n > (int64_t) sizeof( out ) || snake##_save_messages( v, 1, out, n, &rep ) != n ) { printf( "%s msg refused\n", key ); } \
    else { printf( "%s msg ", key ); phex( out, n ); } \
} while ( 0 )

#define VAR( key, snake, type ) do { \
    uint8_t out[ 256 ]; char text[ 512 ]; int64_t n; \
    n = snake##_measure( NULL, (const type *)(const void *) b.region ); \
    if ( n < 0 || n > (int64_t) sizeof( out ) || snake##_save( NULL, (const type *)(const void *) b.region, out, n ) != n ) { printf( "%s var refused\n", key ); } \
    else { printf( "%s var ", key ); phex( out, n ); } \
    n = snake##_to_json( (const type *)(const void *) b.region, text, (int64_t) sizeof( text ) ); \
    if ( n < 0 || n > (int64_t) sizeof( text ) ) { printf( "%s json refused\n", key ); } \
    else { printf( "%s json ", key ); phex( (const uint8_t *) text, n ); } \
} while ( 0 )

int main( void )
{
`

func TestCCountedArrayBornAcrossElementKinds(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C compiler")
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("no C compiler on PATH")
	}
	u := unitFrom(t, cBirthMatrixSchema)
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
	p.WriteString(cBirthMatrixHead)
	want := map[string]string{}
	snake := map[string]string{"I32": "i32", "Rec": "rec", "F64": "f64"}
	for _, c := range cBirthCases() {
		key := "Fix" + c.root + "_" + c.name
		vkey := "Var" + c.root + "_" + c.name
		s := snake[c.root]
		fmt.Fprintf(&p, "    { Fix%s fx; Fix%s * v = &fx; memset( &fx, 0xff, sizeof( fx ) ); fix_%s_reset( v ); %s MSG( %q, fix_%s ); }\n", c.root, c.root, s, c.place, key, s)
		fmt.Fprintf(&p, "    { Var%sBuilder b; Var%s * v; if ( !var_%s_builder_init( &b ) ) { return 4; } v = var_%s_builder_root( &b ); (void) v; %s if ( !var_%s_builder_lock( &b ) ) { return 5; } VAR( %q, var_%s, Var%s ); var_%s_builder_shutdown( &b ); }\n", c.root, c.root, s, s, c.place, s, vkey, s, c.root, s)
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
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-I", dir}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".c") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	mainPath := filepath.Join(dir, "main.c")
	if err := os.WriteFile(mainPath, []byte(p.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args = append(args, mainPath, "-lm", "-o", bin)
	if out, err := testproc.Command(t, cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile failed: %v\n%s", err, out)
	}
	out, err := testproc.Command(t, bin).CombinedOutput()
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
			t.Errorf("%s: C wrote %q, the engine wrote %q", k, got[k], w)
		}
	}
}
