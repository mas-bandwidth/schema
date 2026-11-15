// The bytes inside a JSON comment, line or block, are well-formed UTF-8 and
// hold no NUL (docs/SPEC-TABLES.md §16.2, docs/rulings.json json-comment-text).
//
// Compiling and running the probe makes this a functional test: it skips by
// name without SCHEMA_SLOW, and fails by name under SCHEMA_REQUIRE_CORPUS=1
// when no C compiler is found.
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

const jsonCommentRulesDir = "../../../testdata/wire/json-comment-text-rules"

type jsonCommentRow struct {
	name    string
	verdict string
	text    []byte
}

// readJsonCommentRows reads every vector the manifest names.
func readJsonCommentRows(t *testing.T) []jsonCommentRow {
	t.Helper()
	manifest, err := os.Open(filepath.Join(jsonCommentRulesDir, "manifest.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer manifest.Close()
	var rows []jsonCommentRow
	scanner := bufio.NewScanner(manifest)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.SplitN(line, "\t", 3)
		if len(cols) != 3 || (cols[1] != "read" && cols[1] != "refused") {
			t.Fatalf("bad manifest line %q", line)
		}
		text, err := os.ReadFile(filepath.Join(jsonCommentRulesDir, cols[0]))
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, jsonCommentRow{name: cols[0], verdict: cols[1], text: text})
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("the manifest names no vectors")
	}
	return rows
}

// cBytesLiteral spells data as a C string literal, every byte in octal.
func cBytesLiteral(data []byte) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range data {
		fmt.Fprintf(&b, "\\%03o", c)
	}
	b.WriteByte('"')
	return b.String()
}

func TestCJsonCommentIsWellFormedUTF8(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C compiler")
	cc, err := exec.LookPath("cc")
	if err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and cc is not found: %v", err)
		}
		t.Skipf("cc: %v", err)
	}

	u := zeroWidthUnit(t, "tables/examples")
	rows := readJsonCommentRows(t)
	// The same rule between a placed key and its value, and inside the value
	// under an unknown key.
	rows = append(rows,
		jsonCommentRow{"placed-line-comment-utf8", "read", []byte("{\"experience\": // caf\xc3\xa9\n 3}\n")},
		jsonCommentRow{"placed-block-comment-utf8", "read", []byte("{\"experience\": /* caf\xc3\xa9 */ 3}\n")},
		jsonCommentRow{"placed-line-comment-0x80", "refused", []byte("{\"experience\": // \x80\n 3}\n")},
		jsonCommentRow{"placed-block-comment-overlong", "refused", []byte("{\"experience\": /* \xc0\xaf */ 3}\n")},
		jsonCommentRow{"placed-block-comment-nul", "refused", []byte("{\"experience\": /* \x00 */ 3}\n")},
		jsonCommentRow{"unknown-value-line-comment-utf8", "read", []byte("{\"mystery\": [1, // caf\xc3\xa9\n 2]}\n")},
		jsonCommentRow{"unknown-value-line-comment-surrogate", "refused", []byte("{\"mystery\": [1, // \xed\xa0\x80\n 2]}\n")},
		jsonCommentRow{"unknown-value-block-comment-truncated", "refused", []byte("{\"mystery\": {\"a\": /* \xe2\x82 */ 2}}\n")},
		jsonCommentRow{"unknown-value-line-comment-nul", "refused", []byte("{\"mystery\": [1, // \x00\n 2]}\n")},
	)

	// The reference first: the engine gives the standard's verdict on every row.
	m := tabletext.NewModel(u)
	st := m.Lookup("ProfileConfig")
	if st == nil {
		t.Fatal("tables/examples has no table ProfileConfig")
	}
	engine := make([]string, len(rows))
	for i, row := range rows {
		var report tabletext.Report
		engine[i] = "refused"
		if m.Read(m.New(st), row.text, &report) && !report.Refused {
			engine[i] = "read"
		}
		if engine[i] != row.verdict {
			t.Fatalf("%s: the engine's verdict is %s, the standard's is %s", row.name, engine[i], row.verdict)
		}
	}

	var probe strings.Builder
	probe.WriteString(`#include <stdio.h>
#include <string.h>
#include "TablesTable.h"

typedef struct Row { const char * text; int64_t size; } Row;

int main( void )
{
    static const Row rows[] = {
`)
	for _, row := range rows {
		fmt.Fprintf(&probe, "        { %s, %d },\n", cBytesLiteral(row.text), len(row.text))
	}
	probe.WriteString(`    };
    size_t i;
    for ( i = 0; i < sizeof( rows ) / sizeof( rows[0] ); i++ )
    {
        ProfileConfig value;
        TableReport r;
        int ok;
        profile_config_reset( &value );
        memset( &r, 0, sizeof( r ) );
        ok = profile_config_from_json( &value, rows[i].text, rows[i].size, &r );
        printf( "%s\n", ( ok && !r.refused ) ? "read" : "refused" );
    }
    return 0;
}
`)

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
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	main := filepath.Join(dir, "main.c")
	if err := os.WriteFile(main, []byte(probe.String()), 0600); err != nil {
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
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
	got := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(got) != len(rows) {
		t.Fatalf("the probe printed %d verdicts for %d rows:\n%s", len(got), len(rows), out)
	}
	for i, row := range rows {
		if got[i] != engine[i] {
			t.Errorf("%s: C's verdict is %s, the engine's is %s", row.name, got[i], engine[i])
		}
	}
}
