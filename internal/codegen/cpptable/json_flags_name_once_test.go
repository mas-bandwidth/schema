package cpptable

// A FLAGS ARRAY NAMES EACH KNOWN VARIANT ONCE (docs/SPEC-TABLES.md §16.2): a
// flags array that names one known variant twice refuses the read, as a
// repeated key does, because a conforming writer writes each set bit's name
// once. An unknown name keeps its treatment (skipped and counted), and a flags
// array inside a value the read only skips is held to the JSON grammar alone.
// The rows are the vectors under testdata/wire/json-flags-name-once; each is
// compared on the verdict alone with the Go engine's (internal/tabletext), and
// the engine is first held to the verdict the vectors state. Building the
// generated C++ with c++ makes this a functional test, gated by slowtest.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const cppJsonFlagsNameOnceDir = "../../../testdata/wire/json-flags-name-once"

type cppJsonFlagsNameOnceRow struct {
	name, unit, root, text, verdict string
}

func cppJsonFlagsNameOnceRows(t *testing.T) []cppJsonFlagsNameOnceRow {
	t.Helper()
	f, err := os.Open(filepath.Join(cppJsonFlagsNameOnceDir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []cppJsonFlagsNameOnceRow
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
		rows = append(rows, cppJsonFlagsNameOnceRow{c[0], c[1], c[2], c[3], c[4]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no rows")
	}
	return rows
}

// cppJsonFlagsNameOnceUnitFile names the one source in the unit that declares
// root, so the probe generates that root and its closure rather than every
// root the directory holds.
func cppJsonFlagsNameOnceUnitFile(t *testing.T, unit, root string) string {
	t.Helper()
	dir := filepath.Join("../../..", unit)
	decl := regexp.MustCompile(`(?m)^(?:fixed\s+)?table\s+` + regexp.QuoteMeta(root) + `\b`)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".schema") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if decl.Match(data) {
			return filepath.Join(unit, e.Name())
		}
	}
	t.Fatalf("%s declares no %s", unit, root)
	return ""
}

func TestCPPJsonFlagsNameOnce(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C++ compiler (c++)")

	rows := cppJsonFlagsNameOnceRows(t)
	unit, root := rows[0].unit, rows[0].root
	for _, row := range rows {
		if row.unit != unit || row.root != root {
			t.Fatalf("%s: unit %s root %s; this test reads one unit and root, %s %s", row.name, row.unit, row.root, unit, root)
		}
	}
	u := loadHeldUnitPaths(t, "../../..", []string{cppJsonFlagsNameOnceUnitFile(t, unit, root)})
	model := tabletext.NewModel(u)
	def := model.Lookup(root)
	if def == nil {
		t.Fatalf("%s: the unit declares no %s", unit, root)
	}

	var body strings.Builder
	body.WriteString(`
    int failures = 0;
    #define CHECK(ok, name) do { if (!(ok)) { fprintf(stderr, "FAIL: %s (line %d)\n", (name), __LINE__); failures++; } } while (0)
`)
	for _, row := range rows {
		src, err := os.ReadFile(filepath.Join(cppJsonFlagsNameOnceDir, row.text))
		if err != nil {
			t.Fatal(err)
		}
		var rep tabletext.Report
		verdict := "refused"
		if model.Read(model.New(def), src, &rep) {
			verdict = "read"
		}
		if verdict != row.verdict {
			t.Fatalf("%s: the Go engine answers %s, the vectors state %s", row.name, verdict, row.verdict)
		}
		want := "false"
		if verdict == "read" {
			want = "true"
		}
		fmt.Fprintf(&body, `
    {
        static const uint8_t text[] = { %s };
        ProfileConfig value;
        TableReport report;
        bool ok = ProfileConfigFromJson( value, (const char *) text, (int64_t) sizeof(text), &report );
        printf("FLAGS_CASE %s: %%s\n", ok ? "read" : "refused");
        CHECK(ok == %s, "%s verdict");
    }
`, cppBytes(src), row.name, want, row.name)
	}
	body.WriteString("    if ( failures != 0 ) return 1;\n")
	out, err := runCPPCountRefusalProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("json flags name-once probe: %v\n%s", err, out)
	}
}
