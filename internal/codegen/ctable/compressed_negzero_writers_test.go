package ctable

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A COMPRESSED FLOAT'S -0 IS WRITTEN AS +0 BY EVERY GENERATED C WRITER
// (docs/SPEC-TABLES.md §3.3, the special values; docs/rulings.json
// fixed-compressed-negzero). The variable save and its retaining twin, the
// fixed save, the JSON writer and the cook stored the bits as held. The corpus
// is the Go leg's, testdata/conformance/feature-witnesses/compressed-negzero-go:
// each row loads its base, sets one cell to -0 IN THE NATIVE VALUE, and owes
// the base's own bytes (its +0 twin); the plain float `p` is the control,
// whose -0 rides bit-exact.

const cNegZeroCorpusDir = "../../../testdata/conformance/feature-witnesses/compressed-negzero-go"

// cNegZeroRow is one row of the corpus's cases.tsv.
type cNegZeroRow struct {
	name, form, root, base, path, expect string
}

func cNegZeroRows(t *testing.T) []cNegZeroRow {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(cNegZeroCorpusDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []cNegZeroRow
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("cases.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		rows = append(rows, cNegZeroRow{f[0], f[1], f[2], f[3], f[4], f[5]})
	}
	if len(rows) != 15 {
		t.Fatalf("cases.tsv holds %d rows, want 15", len(rows))
	}
	return rows
}

// cNegZeroCell is the C lvalue a corpus path names on the value `v`: `field`,
// `field[index]`, `field[Variant]` for an enum-keyed slot (the key k at index
// k-1; the Level enum is the corpus's one key), or `field.arm` for a union's
// set arm.
func cNegZeroCell(path string) string {
	if name, index, ok := strings.Cut(path, "["); ok {
		index = strings.TrimSuffix(index, "]")
		if index[0] < '0' || index[0] > '9' {
			index = "LEVEL_" + strings.ToUpper(index) + " - 1"
		}
		return "v." + name + "[" + index + "]"
	}
	if name, arm, ok := strings.Cut(path, "."); ok {
		return "v." + name + ".as." + arm
	}
	return "v." + path
}

func cByteArray(name string, data []byte) string {
	var b strings.Builder
	fmt.Fprintf(&b, "        static const uint8_t %s[%d] = {", name, len(data))
	for i, c := range data {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "%d", c)
	}
	b.WriteString("};\n")
	return b.String()
}

// cNegZeroWriters are the writers each root is held on, by the index the
// probe's functions switch on; the first is the variable form and, for Fix,
// the second is the fixed form.
var cNegZeroWriters = map[string][]string{
	"Var": {"save", "save_retain", "json", "cook"},
	"Fix": {"save", "fixed", "json", "message", "cook"},
}

const cNegZeroPrelude = `#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "ProbeTable.h"

#define CAP 4096

static int64_t var_bytes( int w, const Var * v, uint8_t * out )
{
    int64_t n = -1;
    int64_t m = -2;
    TableRetain retain;
    TableReport report;
    memset( &retain, 0, sizeof( retain ) );
    memset( &report, 0, sizeof( report ) );
    switch ( w )
    {
        case 0: n = var_measure( NULL, v ); if ( n > 0 && n <= CAP ) { m = var_save( NULL, v, out, n ); } break;
        case 1: n = var_measure_retain( v, &retain ); if ( n > 0 && n <= CAP ) { m = var_save_retain( v, &retain, out, n, &report ); } break;
        case 2: n = var_to_json_measure( v ); if ( n > 0 && n <= CAP ) { m = var_to_json( v, (char *) out, n ); } break;
        case 3: n = var_cook_measure( NULL, v ); if ( n > 0 && n <= CAP ) { m = var_cook( NULL, v, out, (uint64_t) n, TableByteOrder_Little ) ? n : -1; } break;
        default: break;
    }
    return n > 0 && m == n ? n : -1;
}

static int64_t fix_bytes( int w, const Fix * v, uint8_t * out )
{
    int64_t n = -1;
    int64_t m = -2;
    TableReport report;
    memset( &report, 0, sizeof( report ) );
    switch ( w )
    {
        case 0: n = fix_measure( v ); if ( n > 0 && n <= CAP ) { m = fix_save( v, out, n ); } break;
        case 1: n = fix_fixed_measure( 1 ); if ( n > 0 && n <= CAP ) { m = fix_fixed_save( v, 1, out, n ); } break;
        case 2: n = fix_to_json_measure( v ); if ( n > 0 && n <= CAP ) { m = fix_to_json( v, (char *) out, n ); } break;
        case 3: n = fix_measure_messages( v, 1, &report ); if ( n > 0 && n <= CAP ) { m = fix_save_messages( v, 1, out, n, &report ); } break;
        case 4: n = fix_cook_measure( v ); if ( n > 0 && n <= CAP ) { m = fix_cook( v, out, (uint64_t) n, TableByteOrder_Little ) ? n : -1; } break;
        default: break;
    }
    return n > 0 && m == n ? n : -1;
}

static void hex( const uint8_t * b, int64_t n )
{
    int64_t i;
    for ( i = 0; i < n; i++ ) { printf( "%02x", b[i] ); }
    printf( "\n" );
}

static int same( const uint8_t * a, int64_t na, const uint8_t * b, int64_t nb )
{
    return na >= 0 && na == nb && memcmp( a, b, (size_t) na ) == 0;
}

static uint8_t minus[CAP];
static uint8_t plus[CAP];

int main( void )
{
    float negzero = 0.0f;
    const uint32_t negzero_bits = 0x80000000u;
    int failed = 0;
    int64_t nm = 0;
    int64_t np = 0;
    memcpy( &negzero, &negzero_bits, 4 );
`

// cNegZeroProbe is the probe over every corpus row: the row's own form owes
// its expect file byte for byte, its base owes the base file (C writes the
// engine's bytes), and every writer of the root writes the -0 value as it
// writes the +0 base — or, for the plain control, differently.
func cNegZeroProbe(t *testing.T, rows []cNegZeroRow) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(cNegZeroPrelude)
	for _, r := range rows {
		writers := cNegZeroWriters[r.root]
		if writers == nil {
			t.Fatalf("%s: root %s", r.name, r.root)
		}
		form := 0
		if r.form == "fixed" {
			form = 1
		}
		if r.form != "variable" && (r.form != "fixed" || r.root != "Fix") {
			t.Fatalf("%s: form %s at %s", r.name, r.form, r.root)
		}
		read := func(stem string) []byte {
			data, err := os.ReadFile(filepath.Join(cNegZeroCorpusDir, stem))
			if err != nil {
				t.Fatal(err)
			}
			return data
		}
		json := strings.TrimSpace(string(read(r.base + ".json")))
		plain := r.path == "p"
		lower := strings.ToLower(r.root)
		fmt.Fprintf(&b, "    { /* %s */\n", r.name)
		b.WriteString(cByteArray("want_base", read(r.base+"."+r.form+".bin")))
		b.WriteString(cByteArray("want", read(r.expect+"."+r.form+".bin")))
		fmt.Fprintf(&b, "        const char * json = %q;\n        TableReport report;\n        %s base;\n        %s v;\n", json, r.root, r.root)
		if r.root == "Var" {
			b.WriteString("        VarBuilder builder;\n")
			b.WriteString("        if ( !var_builder_init( &builder ) ) { printf( \"" + r.name + ": no builder\\n\" ); return 1; }\n")
			b.WriteString("        if ( !var_from_json( &builder, json, (int64_t) strlen( json ), &report ) ) { printf( \"" + r.name + ": the base does not load\\n\" ); return 1; }\n")
			b.WriteString("        base = *var_builder_root( &builder );\n        var_builder_shutdown( &builder );\n")
		} else {
			b.WriteString("        fix_reset( &base );\n")
			b.WriteString("        if ( !fix_from_json( &base, json, (int64_t) strlen( json ), &report ) ) { printf( \"" + r.name + ": the base does not load\\n\" ); return 1; }\n")
		}
		fmt.Fprintf(&b, "        v = base;\n        %s = negzero;\n", cNegZeroCell(r.path))
		fmt.Fprintf(&b, "        np = %s_bytes( %d, &base, plus );\n", lower, form)
		fmt.Fprintf(&b, "        if ( !same( plus, np, want_base, (int64_t) sizeof( want_base ) ) ) { printf( \"%s: the %s save of the base is not %s.%s.bin: \" ); hex( plus, np ); failed = 1; }\n", r.name, r.form, r.base, r.form)
		fmt.Fprintf(&b, "        nm = %s_bytes( %d, &v, minus );\n", lower, form)
		fmt.Fprintf(&b, "        if ( !same( minus, nm, want, (int64_t) sizeof( want ) ) ) { printf( \"%s: the %s save of -0 at %s is not %s.%s.bin: \" ); hex( minus, nm ); failed = 1; }\n", r.name, r.form, r.path, r.expect, r.form)
		for w, name := range writers {
			fmt.Fprintf(&b, "        nm = %s_bytes( %d, &v, minus );\n        np = %s_bytes( %d, &base, plus );\n", lower, w, lower, w)
			fmt.Fprintf(&b, "        if ( nm < 0 || np < 0 ) { printf( \"%s: the %s writer refused\\n\" ); failed = 1; }\n", r.name, name)
			if plain {
				fmt.Fprintf(&b, "        else if ( same( minus, nm, plus, np ) ) { printf( \"%s: a plain float's -0 did not ride in the %s writer\\n\" ); failed = 1; }\n", r.name, name)
			} else {
				fmt.Fprintf(&b, "        else if ( !same( minus, nm, plus, np ) ) { printf( \"%s: the %s writer did not write a compressed -0 as +0: \" ); hex( minus, nm ); failed = 1; }\n", r.name, name)
			}
		}
		b.WriteString("    }\n")
	}
	fmt.Fprintf(&b, "    if ( failed ) { return 1; }\n    printf( \"%d rows: every writer stores a compressed -0 as +0\\n\" );\n    return 0;\n}\n", len(rows))
	return b.String()
}

// TestCCompressedNegZeroCorpus runs the compressed -0 writer corpus on the
// generated C: every row's form owes its expect file, every base owes the
// engine's bytes, and every writer — variable, retaining, fixed, JSON,
// message, cook — writes a compressed -0 as its +0 twin and a plain -0 as -0.
func TestCCompressedNegZeroCorpus(t *testing.T) {
	t.Parallel()
	rows := cNegZeroRows(t)
	src, err := os.ReadFile(filepath.Join(cNegZeroCorpusDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	runCGenerated(t, string(src), cNegZeroProbe(t, rows))
}

// cPlainFloatOperand finds the plain control `p` as a store's operand, and
// not `pick`.
var cPlainFloatOperand = regexp.MustCompile(`(\.|->)p\b`)

// TestCCompressedFloatStoresAreGuarded is the fast twin of the corpus run: it
// reads the generated source rather than compiling it. Every float store of
// the variable writer, the fixed writer and the cook goes through
// table_stored_float_bits for a compressed float and stores the bits as held
// for the plain control `p`; the JSON writer's runtime carries the same test.
func TestCCompressedFloatStoresAreGuarded(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join(cNegZeroCorpusDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(unitFrom(t, string(src)))
	if err != nil {
		t.Fatal(err)
	}
	raw := map[string]string{
		"variable": "table_writer_put32( w, table_float_to_bits(",
		"fixed":    "table_fixed_putf32( b",
		"cook":     "memcpy(&bits,&",
	}
	guarded := map[string]string{
		"variable": "table_writer_put32( w, table_stored_float_bits(",
		"fixed":    "table_fixed_put32( b",
		"cook":     "table_cook_put(",
	}
	counts := map[string][2]int{}
	var all strings.Builder
	for name, data := range files {
		all.Write(data)
		for i, line := range strings.Split(string(data), "\n") {
			for writer, spelling := range raw {
				isGuarded := strings.Contains(line, guarded[writer]) && strings.Contains(line, "table_stored_float_bits(")
				isRaw := strings.Contains(line, spelling)
				if !isGuarded && !isRaw {
					continue
				}
				plain := cPlainFloatOperand.MatchString(line)
				c := counts[writer]
				switch {
				case isRaw && !plain:
					t.Errorf("%s:%d: the %s writer stores a compressed float as held: %s", name, i+1, writer, strings.TrimSpace(line))
				case isGuarded && plain:
					t.Errorf("%s:%d: the %s writer normalizes the plain float's -0: %s", name, i+1, writer, strings.TrimSpace(line))
				case isGuarded:
					c[0]++
				default:
					c[1]++
				}
				counts[writer] = c
			}
		}
	}
	for writer := range raw {
		if c := counts[writer]; c[0] == 0 || c[1] == 0 {
			t.Errorf("the %s writer has %d guarded compressed stores and %d plain ones; want both", writer, c[0], c[1])
		}
	}
	for _, want := range []string{
		"if ( f->has_range && bits == 0x80000000u ) { v = 0.0f; }",
		"if ( f->has_range && bits == 0x8000000000000000ull ) { v = 0.0; }",
	} {
		if !strings.Contains(all.String(), want) {
			t.Errorf("the generated JSON writer does not carry %q", want)
		}
	}
}
