package cpptable

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A COMPRESSED FLOAT'S -0 IS WRITTEN AS +0 BY EVERY GENERATED C++ WRITER
// (docs/SPEC-TABLES.md §3.3, the special values; docs/rulings.json
// fixed-compressed-negzero). The variable save and its retaining twin, the
// fixed save, the JSON writer and the cook stored the bits as held. The corpus
// is the Go leg's, testdata/conformance/feature-witnesses/compressed-negzero-go:
// each row loads its base, sets one cell to -0 IN THE NATIVE VALUE, and owes
// the base's own bytes (its +0 twin); the plain float `p` is the control,
// whose -0 rides bit-exact.

const cppNegZeroCorpusDir = "../../../testdata/conformance/feature-witnesses/compressed-negzero-go"

// cppNegZeroRow is one row of the corpus's cases.tsv.
type cppNegZeroRow struct {
	name, form, root, base, path, expect string
}

func cppNegZeroRows(t *testing.T) []cppNegZeroRow {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(cppNegZeroCorpusDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []cppNegZeroRow
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("cases.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		rows = append(rows, cppNegZeroRow{f[0], f[1], f[2], f[3], f[4], f[5]})
	}
	if len(rows) != 15 {
		t.Fatalf("cases.tsv holds %d rows, want 15", len(rows))
	}
	return rows
}

// cppNegZeroCell is the C++ lvalue a corpus path names on the value `v`:
// `field`, `field[index]`, `field[Variant]` for an enum-keyed slot (the Level
// enum is the corpus's one key), or `field.arm` for a union's set arm.
func cppNegZeroCell(path string) string {
	if name, index, ok := strings.Cut(path, "["); ok {
		index = strings.TrimSuffix(index, "]")
		if index[0] < '0' || index[0] > '9' {
			index = "Level::" + index
		}
		return "v." + name + "[" + index + "]"
	}
	return "v." + path
}

func cppByteVector(name string, data []byte) string {
	var b strings.Builder
	fmt.Fprintf(&b, "        const std::vector<uint8_t> %s = {", name)
	for i, c := range data {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "%d", c)
	}
	b.WriteString("};\n")
	return b.String()
}

// cppNegZeroWriters are the writers each root is held on, by the index the
// probe's lambdas switch on; the first is the variable form and, for Fix, the
// second is the fixed form.
var cppNegZeroWriters = map[string][]string{
	"Var": {"save", "save_retain", "json", "cook"},
	"Fix": {"save", "fixed", "json", "message", "cook"},
}

const cppNegZeroPrelude = `
    float negzero = 0.0f;
    { const uint32_t bits = 0x80000000u; memcpy( &negzero, &bits, 4 ); }
    int failed = 0;
    auto var_bytes = []( int w, const Var & v ) -> std::vector<uint8_t>
    {
        std::vector<uint8_t> out;
        int64_t n = -1;
        int64_t m = -2;
        TableRetain retain;
        TableReport report;
        switch ( w )
        {
            case 0: n = VarMeasure( &v ); if ( n > 0 ) { out.resize( (size_t) n ); m = VarSave( &v, out.data(), n ); } break;
            case 1: n = VarMeasureRetain( &v, &retain ); if ( n > 0 ) { out.resize( (size_t) n ); m = VarSaveRetain( &v, &retain, out.data(), n, &report ); } break;
            case 2: n = VarToJsonMeasure( &v ); if ( n > 0 ) { out.resize( (size_t) n ); m = VarToJson( &v, (char *) out.data(), n ); } break;
            case 3: n = VarCookMeasure( &v ); if ( n > 0 ) { out.resize( (size_t) n ); m = VarCook( &v, out.data(), (uint64_t) n, TableByteOrder::Little ) ? n : -1; } break;
        }
        if ( n <= 0 || m != n ) { out.clear(); }
        return out;
    };
    auto fix_bytes = []( int w, const Fix & v ) -> std::vector<uint8_t>
    {
        std::vector<uint8_t> out;
        int64_t n = -1;
        int64_t m = -2;
        TableReport report;
        switch ( w )
        {
            case 0: n = FixMeasure( v ); if ( n > 0 ) { out.resize( (size_t) n ); m = FixSave( v, out.data(), n ); } break;
            case 1: n = FixFixedMeasure( 1 ); if ( n > 0 ) { out.resize( (size_t) n ); m = FixFixedSave( &v, 1, out.data(), n ); } break;
            case 2: n = FixToJsonMeasure( v ); if ( n > 0 ) { out.resize( (size_t) n ); m = FixToJson( v, (char *) out.data(), n ); } break;
            case 3: n = FixMeasureMessages( &v, 1, &report ); if ( n > 0 ) { out.resize( (size_t) n ); m = FixSaveMessages( &v, 1, out.data(), n, &report ); } break;
            case 4: n = FixCookMeasure( v ); if ( n > 0 ) { out.resize( (size_t) n ); m = FixCook( v, out.data(), (uint64_t) n, TableByteOrder::Little ) ? n : -1; } break;
        }
        if ( n <= 0 || m != n ) { out.clear(); }
        return out;
    };
    auto hex = []( const std::vector<uint8_t> & b )
    {
        for ( uint8_t c : b ) { printf( "%02x", c ); }
        printf( "\n" );
    };
    (void) hex;
`

// cppNegZeroProbe is the probe body over every corpus row: the row's own form
// owes its expect file byte for byte, its base owes the base file (C++ writes
// the engine's bytes), and every writer of the root writes the -0 value as it
// writes the +0 base — or, for the plain control, differently.
func cppNegZeroProbe(t *testing.T, rows []cppNegZeroRow) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(cppNegZeroPrelude)
	for i, r := range rows {
		writers := cppNegZeroWriters[r.root]
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
			data, err := os.ReadFile(filepath.Join(cppNegZeroCorpusDir, stem))
			if err != nil {
				t.Fatal(err)
			}
			return data
		}
		json := strings.TrimSpace(string(read(r.base + ".json")))
		plain := r.path == "p"
		fmt.Fprintf(&b, "    { // %s\n", r.name)
		b.WriteString(cppByteVector("want_base", read(r.base+"."+r.form+".bin")))
		b.WriteString(cppByteVector("want", read(r.expect+"."+r.form+".bin")))
		fmt.Fprintf(&b, "        const char * json = %q;\n        TableReport read;\n", json)
		lower := strings.ToLower(r.root)
		if r.root == "Var" {
			b.WriteString("        VarBuilder builder;\n")
			b.WriteString("        if ( !VarFromJson( builder, json, (int64_t) strlen( json ), &read ) ) { printf( \"" + r.name + ": the base does not load\\n\" ); return 1; }\n")
			b.WriteString("        const Var base = *builder.GetRoot();\n")
		} else {
			b.WriteString("        Fix base;\n        FixReset( base );\n")
			b.WriteString("        if ( !FixFromJson( base, json, (int64_t) strlen( json ), &read ) ) { printf( \"" + r.name + ": the base does not load\\n\" ); return 1; }\n")
		}
		fmt.Fprintf(&b, "        %s v = base;\n        %s = negzero;\n", r.root, cppNegZeroCell(r.path))
		fmt.Fprintf(&b, "        if ( %s_bytes( %d, base ) != want_base ) { printf( \"%s: the %s save of the base is not %s.%s.bin: \" ); hex( %s_bytes( %d, base ) ); failed = 1; }\n",
			lower, form, r.name, r.form, r.base, r.form, lower, form)
		fmt.Fprintf(&b, "        if ( %s_bytes( %d, v ) != want ) { printf( \"%s: the %s save of -0 at %s is not %s.%s.bin: \" ); hex( %s_bytes( %d, v ) ); failed = 1; }\n",
			lower, form, r.name, r.form, r.path, r.expect, r.form, lower, form)
		for w, name := range writers {
			fmt.Fprintf(&b, "        {\n            const std::vector<uint8_t> minus = %s_bytes( %d, v );\n            const std::vector<uint8_t> plus = %s_bytes( %d, base );\n", lower, w, lower, w)
			fmt.Fprintf(&b, "            if ( minus.empty() || plus.empty() ) { printf( \"%s: the %s writer refused\\n\" ); failed = 1; }\n", r.name, name)
			if plain {
				fmt.Fprintf(&b, "            else if ( minus == plus ) { printf( \"%s: a plain float's -0 did not ride in the %s writer\\n\" ); failed = 1; }\n", r.name, name)
			} else {
				fmt.Fprintf(&b, "            else if ( minus != plus ) { printf( \"%s: the %s writer did not write a compressed -0 as +0: \" ); hex( minus ); failed = 1; }\n", r.name, name)
			}
			b.WriteString("        }\n")
		}
		b.WriteString("    }\n")
		_ = i
	}
	fmt.Fprintf(&b, "    if ( failed ) { return 1; }\n    printf( \"%d rows: every writer stores a compressed -0 as +0\\n\" );\n", len(rows))
	return b.String()
}

// TestCppCompressedNegZeroCorpus runs the compressed -0 writer corpus on the
// generated C++: every row's form owes its expect file, every base owes the
// engine's bytes, and every writer — variable, retaining, fixed, JSON,
// message, cook — writes a compressed -0 as its +0 twin and a plain -0 as -0.
func TestCppCompressedNegZeroCorpus(t *testing.T) {
	t.Parallel()
	rows := cppNegZeroRows(t)
	src, err := os.ReadFile(filepath.Join(cppNegZeroCorpusDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitFromSource(t, string(src))
	body := cppNegZeroProbe(t, rows)
	out, err := cppHeldRun(t, u, body)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	t.Logf("%s", out)
}

// cppPlainFloatOperand finds the plain control `p` as a store's operand, and
// not `pick`.
var cppPlainFloatOperand = regexp.MustCompile(`(\.|->)p\b`)

// TestCppCompressedFloatStoresAreGuarded is the fast twin of the corpus run:
// it reads the generated source rather than compiling it. Every float store of
// the variable writer, the fixed writer and the cook goes through
// TableStoredFloatBits for a compressed float and stores the bits as held for
// the plain control `p`; the JSON writer's runtime carries the same test.
func TestCppCompressedFloatStoresAreGuarded(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join(cppNegZeroCorpusDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := GenerateLineage(loadUnitFromSource(t, string(src)), nil)
	if err != nil {
		t.Fatal(err)
	}
	raw := map[string]string{
		"variable": "w.put32( table_float_to_bits(",
		"fixed":    "TableFixedPutF32( b",
		"cook":     "memcpy( &bits, &",
	}
	guarded := map[string]string{
		"variable": "w.put32( TableStoredFloatBits(",
		"fixed":    "TableFixedPut32( b",
		"cook":     "table_cook_put(",
	}
	counts := map[string][2]int{}
	var all strings.Builder
	for name, data := range files {
		all.Write(data)
		for i, line := range strings.Split(string(data), "\n") {
			for writer, spelling := range raw {
				isGuarded := strings.Contains(line, guarded[writer]) && strings.Contains(line, "TableStoredFloatBits(")
				isRaw := strings.Contains(line, spelling) && (writer != "cook" || strings.Contains(line, "table_cook_put("))
				if !isGuarded && !isRaw {
					continue
				}
				plain := cppPlainFloatOperand.MatchString(line)
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
