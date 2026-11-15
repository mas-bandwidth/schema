// A JSON number token is read by its value however many bytes it spans
// (docs/SPEC-TABLES.md §16.2 Numbers): its length is never why a read refuses.
package ctable

import (
	"fmt"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const cJsonNumberAnyLengthSchema = `package probe

fixed table Probe
{
    experience  uint32 = 0
    precision   float64
    g           fixed(16, 16) | min = -32768, max = 32767
}
`

// one row: the text is head, then fill repeated count times, then tail
type cJsonNumberRow struct {
	name  string
	head  string
	fill  byte
	count int
	tail  string
	want  string // what the engine answers: "refused", or <field>=<value> with any unknown count zero but for "unknown=1"
}

func (r cJsonNumberRow) text() string {
	return r.head + strings.Repeat(string(r.fill), r.count) + r.tail
}

// TestCJsonNumberAnyLength reads each row with the generated C walk and with
// the compiler's own engine (internal/tabletext), and requires the same
// verdict and, for a read, the same values and unknown count. Compiling and
// running the probe makes this a functional test.
func TestCJsonNumberAnyLength(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C compiler (it compiles and runs the generated unit)")
	cc, err := exec.LookPath("cc")
	if err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and cc is not found: %v", err)
		}
		t.Skipf("cc: %v", err)
	}
	rows := []cJsonNumberRow{
		{"experience-1.509-zeros", `{"experience": 1.`, '0', 509, `}`, "experience=1"},
		{"experience-1.510-zeros", `{"experience": 1.`, '0', 510, `}`, "experience=1"},
		{"experience-1.511-zeros", `{"experience": 1.`, '0', 511, `}`, "experience=1"},
		{"experience-1.600-zeros", `{"experience": 1.`, '0', 600, `}`, "experience=1"},
		{"precision-1.600-zeros", `{"precision": 1.`, '0', 600, `}`, "precision=1"},
		{"unknown-5000-digits", `{"nope": 1`, '0', 4999, `}`, "unknown=1"},
		{"experience-600-digit-integer", `{"experience": 1`, '0', 599, `}`, "refused"},
		{"experience-exponent-folds", `{"experience": 100`, '0', 600, `e-600}`, "experience=100"},
		{"precision-leading-zeros-fold", `{"precision": 0.`, '0', 1000, `15e1001}`, "precision=1.5"},
		{"precision-tail-past-halfway", `{"precision": 9007199254740993.`, '0', 1000, `1}`, "precision=9007199254740994"},
		{"precision-halfway-ties-even", `{"precision": 9007199254740993.`, '0', 1000, `}`, "precision=9007199254740992"},
		{"precision-negative-1.1000-zeros", `{"precision": -1.`, '0', 1000, `}`, "precision=-1"},
		{"g-1.600-zeros", `{"g": 1.`, '0', 600, `}`, "g=65536"},
		{"g-inexact-long-fraction", `{"g": 1.`, '0', 600, `1}`, "refused"},
	}
	u := unitFrom(t, cJsonNumberAnyLengthSchema)
	model := tabletext.NewModel(u)
	st := model.Lookup("Probe")
	if st == nil {
		t.Fatal("no table Probe")
	}
	var body strings.Builder
	for _, row := range rows {
		var rep tabletext.Report
		inst := model.New(st)
		ok := model.Read(inst, []byte(row.text()), &rep)
		if ok == rep.Refused {
			t.Fatalf("%s: the engine answered ok=%v with %+v", row.name, ok, rep)
		}
		var experience uint64
		var precision float64
		var g int64
		for i := range inst.Fields {
			f := &inst.Fields[i]
			switch f.Def.Name {
			case "experience":
				experience = f.Cell.U
			case "precision":
				precision = f.Cell.F
			case "g":
				g = tabletext.WideValue(&f.Cell).Int64()
			}
		}
		got := "refused"
		if ok {
			name, _, _ := strings.Cut(row.want, "=")
			switch name {
			case "experience":
				got = fmt.Sprintf("experience=%d", experience)
			case "precision":
				got = "precision=" + strconv.FormatFloat(precision, 'f', -1, 64)
			case "g":
				got = fmt.Sprintf("g=%d", g)
			case "unknown":
				got = fmt.Sprintf("unknown=%d", rep.Unknown)
			}
			if name != "unknown" && rep.Unknown != 0 {
				got += fmt.Sprintf(" unknown=%d", rep.Unknown)
			}
		}
		if got != row.want {
			t.Fatalf("%s: the engine answered %s, want %s", row.name, got, row.want)
		}
		fmt.Fprintf(&body, "    { %s, %s, '%c', %d, %s, %d, %d, %du, 0x%016xull, %d },\n",
			strconv.Quote(row.name), strconv.Quote(row.head), row.fill, row.count, strconv.Quote(row.tail),
			boolInt(ok), rep.Unknown, experience, math.Float64bits(precision), g)
	}
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatalf("cgen: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
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
		if strings.HasSuffix(name, ".c") {
			sources = append(sources, filepath.Join(dir, name))
		}
	}
	main := filepath.Join(dir, "main.c")
	if err := os.WriteFile(main, []byte(strings.Replace(cJsonNumberAnyLengthProbe, "ROWS", body.String(), 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-I", dir, main}
	args = append(args, sources...)
	args = append(args, "-lm", "-o", bin)
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile failed: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("the C walk and the engine disagree: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

const cJsonNumberAnyLengthProbe = `#include <stdio.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include "ProbeTable.h"

typedef struct Row {
    const char * name;
    const char * head;
    char fill;
    int count;
    const char * tail;
    int ok;
    int32_t unknown;
    uint32_t experience;
    uint64_t precision;
    int64_t g;
} Row;

static const Row rows[] = {
ROWS};

int main( void )
{
    int failures = 0;
    size_t i;
    for ( i = 0; i < sizeof( rows ) / sizeof( rows[0] ); i++ )
    {
        const Row * row = &rows[i];
        size_t head = strlen( row->head );
        size_t tail = strlen( row->tail );
        size_t length = head + (size_t) row->count + tail;
        char * text = (char *) malloc( length );
        Probe v;
        TableReport r;
        int ok;
        uint64_t precision;
        if ( text == NULL ) { fprintf( stderr, "%s: out of memory\n", row->name ); return 1; }
        memcpy( text, row->head, head );
        memset( text + head, row->fill, (size_t) row->count );
        memcpy( text + head + (size_t) row->count, row->tail, tail );
        probe_reset( &v );
        memset( &r, 0, sizeof( r ) );
        ok = probe_from_json( &v, text, (int64_t) length, &r ) && !r.refused;
        free( text );
        memcpy( &precision, &v.precision, sizeof( precision ) );
        if ( ok != row->ok )
        {
            fprintf( stderr, "FAIL %s: %zu-byte text: C %s, the engine %s\n", row->name, length, ok ? "read" : "refused", row->ok ? "read" : "refused" );
            failures++;
            continue;
        }
        if ( ok && ( r.unknown != row->unknown || v.experience != row->experience || precision != row->precision || (int64_t) v.g != row->g ) )
        {
            fprintf( stderr, "FAIL %s: C read unknown=%d experience=%u precision=0x%016llx g=%lld, the engine unknown=%d experience=%u precision=0x%016llx g=%lld\n",
                row->name, (int) r.unknown, (unsigned) v.experience, (unsigned long long) precision, (long long) v.g,
                (int) row->unknown, (unsigned) row->experience, (unsigned long long) row->precision, (long long) row->g );
            failures++;
            continue;
        }
        printf( "ok %s: %s\n", row->name, ok ? "read" : "refused" );
    }
    return failures == 0 ? 0 : 1;
}
`
