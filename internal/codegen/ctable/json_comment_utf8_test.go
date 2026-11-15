// A JSON COMMENT IS TEXT (docs/SPEC-TABLES.md §16.2, §16.3): the bytes inside a
// line or block comment are well-formed UTF-8 and hold no NUL, so a comment
// that holds an ill-formed sequence or a zero byte refuses the read wherever
// whitespace is legal and inside a value skipped under an unknown key.
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

// runCProbeCapture is runCProbe with the probe's output. A test that compiles C
// is a functional test: it is gated on SCHEMA_SLOW, and a missing cc fails by
// name when SCHEMA_REQUIRE_CORPUS says the corpus is required.
func runCProbeCapture(t *testing.T, schema, probe string) string {
	t.Helper()
	slowtest.Gate(t, "the C compiler")
	cc, err := exec.LookPath("cc")
	if err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and cc is not found: %v", err)
		}
		t.Skipf("the generated C reader is built with cc: %v", err)
	}
	u := unitFrom(t, schema)
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
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	main := filepath.Join(dir, "main.c")
	if err := os.WriteFile(main, []byte(probe), 0o600); err != nil {
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
	return string(out)
}

// TestCJsonCommentIsWellFormedUTF8 reads every vector under
// testdata/wire/json-comment-text-rules at tables/examples' root ProfileConfig.
// The compiler's own engine is the reference: it must answer the verdict the
// landed standard states on every row, and the generated C must answer the
// same verdict on the same text.
func TestCJsonCommentIsWellFormedUTF8(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(filepath.Join("..", "..", "..", "tables", "examples", "Tables.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(src))
	model := tabletext.NewModel(u)
	st := model.Lookup("ProfileConfig")
	if st == nil {
		t.Fatal("no ProfileConfig table")
	}

	const vectorDir = "../../../testdata/wire/json-comment-text-rules"
	manifest, err := os.Open(filepath.Join(vectorDir, "manifest.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer manifest.Close()

	type row struct {
		name string
		text []byte
		read bool
	}
	var rows []row
	scanner := bufio.NewScanner(manifest)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.SplitN(line, "\t", 3)
		if len(cols) != 3 {
			t.Fatalf("bad manifest line %q", line)
		}
		name, verdict := cols[0], cols[1]
		if verdict != "read" && verdict != "refused" {
			t.Fatalf("bad manifest verdict %q for %s", verdict, name)
		}
		text, err := os.ReadFile(filepath.Join(vectorDir, name))
		if err != nil {
			t.Fatal(err)
		}
		var report tabletext.Report
		ok := model.Read(model.New(st), text, &report)
		engineRead := ok && !report.Refused
		if want := verdict == "read"; engineRead != want {
			t.Fatalf("%s: the engine answers %v where the landed standard reads %q (report %+v)", name, engineRead, verdict, report)
		}
		rows = append(rows, row{name: name, text: text, read: engineRead})
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("manifest contains no comment-rule vectors")
	}

	var body strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&body, `
    {
        static const unsigned char text[] = { %s };
        ProfileConfig value;
        TableReport report;
        int ok;
        memset( &value, 0, sizeof( value ) );
        memset( &report, 0, sizeof( report ) );
        ok = profile_config_from_json( &value, (const char *) text, (int64_t) sizeof( text ), &report );
        printf( "ROW %s: %%s\n", ( ok && !report.refused ) ? "read" : "refused" );
    }
`, cBytes(r.text), r.name)
	}
	probe := `#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include "ProbeTable.h"

int main( void )
{
` + body.String() + `
    return 0;
}
`
	out := runCProbeCapture(t, string(src), probe)

	got := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "ROW ") {
			continue
		}
		name, verdict, ok := strings.Cut(strings.TrimPrefix(line, "ROW "), ": ")
		if !ok {
			t.Fatalf("bad probe answer %q", line)
		}
		switch verdict {
		case "read":
			got[name] = true
		case "refused":
			got[name] = false
		default:
			t.Fatalf("probe answered an unknown verdict %q for %s", verdict, name)
		}
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			read, seen := got[r.name]
			if !seen {
				t.Fatalf("the probe printed no verdict")
			}
			if read != r.read {
				t.Errorf("the emitted reader answers %v where the engine answers %v", read, r.read)
			}
		})
	}
}
