// A FLAGS ARRAY NAMES EACH KNOWN VARIANT ONCE (docs/SPEC-TABLES.md §16.2): a
// flags array that names one known variant twice refuses the read, as a
// repeated key does, because a conforming writer writes each set bit's name
// once. An unknown name keeps its treatment (skipped and counted), and a flags
// array inside a value the read only skips is held to the JSON grammar alone.
// The rows are the vectors under testdata/wire/json-flags-name-once; each is
// compared on the verdict alone with the Go engine's (internal/tabletext), and
// the engine is first held to the verdict the vectors state. Compiling the
// generated C makes this a functional test, gated by slowtest.
package ctable

import (
	"bufio"
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
)

const cJsonFlagsNameOnceDir = "../../../testdata/wire/json-flags-name-once"

type cJsonFlagsNameOnceRow struct {
	name, unit, root, text, verdict string
}

func cJsonFlagsNameOnceRows(t *testing.T) []cJsonFlagsNameOnceRow {
	t.Helper()
	f, err := os.Open(filepath.Join(cJsonFlagsNameOnceDir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []cJsonFlagsNameOnceRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		c := strings.Split(line, "\t")
		if len(c) != 6 {
			t.Fatalf("MANIFEST.tsv: %d columns, want 6: %q", len(c), line)
		}
		rows = append(rows, cJsonFlagsNameOnceRow{c[0], c[1], c[2], c[3], c[4]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no rows")
	}
	return rows
}

// cSnake spells a table name the way the generated C names its functions.
func cSnake(name string) string {
	var b strings.Builder
	for i, r := range name {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

func TestCJsonFlagsNameOnce(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C compiler")
	cc, err := exec.LookPath("cc")
	if err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and cc is absent: %v", err)
		}
		t.Skip("cc not found")
	}

	rows := cJsonFlagsNameOnceRows(t)
	unit, root := rows[0].unit, rows[0].root
	for _, row := range rows {
		if row.unit != unit || row.root != root {
			t.Fatalf("%s: unit %s root %s; this test reads one unit and root, %s %s", row.name, row.unit, row.root, unit, root)
		}
	}
	u := zeroWidthUnit(t, unit)

	// The reference first: the Go engine gives each text the verdict the
	// vectors state.
	model := tabletext.NewModel(u)
	def := model.Lookup(root)
	if def == nil {
		t.Fatalf("%s declares no %s", unit, root)
	}
	texts := make([][]byte, len(rows))
	want := make([]string, len(rows))
	for i, row := range rows {
		src, err := os.ReadFile(filepath.Join(cJsonFlagsNameOnceDir, row.text))
		if err != nil {
			t.Fatal(err)
		}
		texts[i] = src
		var rep tabletext.Report
		verdict := "refused"
		if model.Read(model.New(def), src, &rep) {
			verdict = "read"
		}
		if verdict != row.verdict {
			t.Fatalf("%s: the Go engine answers %s, the vectors state %s", row.name, verdict, row.verdict)
		}
		want[i] = verdict
	}

	// The generated C text read answers each row read or refused.
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatalf("cgen: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	maps.Copy(files, tables)
	header := ""
	fn := cSnake(root) + "_from_json("
	for name, src := range tables {
		if strings.HasSuffix(name, "Table.h") && strings.Contains(string(src), fn) {
			header = name
		}
	}
	if header == "" {
		t.Fatalf("Generate emitted no header declaring %s", fn)
	}

	var cases strings.Builder
	for i, row := range rows {
		hex := make([]string, len(texts[i]))
		for j, b := range texts[i] {
			hex[j] = fmt.Sprintf("0x%02x", b)
		}
		fmt.Fprintf(&cases, "    { %q, %d, { %s } },\n", row.name, len(texts[i]), strings.Join(hex, ", "))
	}
	snake := cSnake(root)
	probe := fmt.Sprintf(`#include <stdio.h>
#include <string.h>
#include "%s"

typedef struct Case {
    const char * name;
    int64_t length;
    char text[256];
} Case;

static const Case cases[] = {
%s};

int main( void )
{
    size_t i;
    for ( i = 0; i < sizeof( cases ) / sizeof( cases[0] ); i++ )
    {
        %s v;
        TableReport r;
        int ok;
        %s_reset( &v );
        memset( &r, 0, sizeof( r ) );
        ok = %s_from_json( &v, cases[i].text, cases[i].length, &r );
        printf( "%%s\n", ( ok && !r.refused ) ? "read" : "refused" );
    }
    return 0;
}
`, header, cases.String(), root, snake, snake)

	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	main := filepath.Join(dir, "main.c")
	if err := os.WriteFile(main, []byte(probe), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-I", dir, main}
	for file := range files {
		if strings.HasSuffix(file, ".c") {
			args = append(args, filepath.Join(dir, file))
		}
	}
	args = append(args, "-lm", "-o", bin)
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile failed: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
	got := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(got) != len(rows) {
		t.Fatalf("the probe answered %d lines for %d rows: %q", len(got), len(rows), out)
	}
	for i, row := range rows {
		if got[i] != want[i] {
			t.Errorf("%s: C answered %q, the Go engine %q", row.name, got[i], want[i])
		}
	}
}
