package cpptable

// docs/SPEC-TABLES.md §16.2: the bytes inside a JSON comment, line or block,
// are well-formed UTF-8 and hold no NUL. A comment that holds an ill-formed
// sequence or a NUL refuses the read, wherever whitespace is legal and inside
// a skipped value under an unknown key. The engine (internal/tabletext) is the
// reference: every vector in the comment-text-rules corpus must answer the
// verdict the landed standard states there, and the emitted C++ reads it the
// same way.

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

func TestCPPJsonCommentIsWellFormedUTF8(t *testing.T) {
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
		t.Skipf("the generated C++ reader is built with %s: %v", cxx, err)
	}

	u := loadUnit(t, filepath.Join("..", "..", "..", "tables", "examples", "Tables.schema"))
	model := tabletext.NewModel(u)
	st := model.Lookup("ProfileConfig")
	if st == nil {
		t.Fatal("no ProfileConfig table")
	}

	dir := filepath.Join("..", "..", "..", "testdata", "wire", "json-comment-text-rules")
	manifest, err := os.Open(filepath.Join(dir, "manifest.tsv"))
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
		text, err := os.ReadFile(filepath.Join(dir, name))
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
        static const uint8_t text[] = { %s };
        ProfileConfig value; TableReport report;
        bool ok = ProfileConfigFromJson(value, (const char *)text, (int64_t)sizeof(text), &report);
        printf("ROW %s: %%s\n", ok ? "read" : "refused");
    }
`, cppBytes(r.text), r.name)
	}
	out, err := runCPPCountRefusalProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("comment-utf8 probe: %v\n%s", err, out)
	}

	got := map[string]bool{}
	for line := range strings.SplitSeq(string(out), "\n") {
		name, verdict, ok := strings.Cut(strings.TrimPrefix(line, "ROW "), ": ")
		if !ok || !strings.HasPrefix(line, "ROW ") {
			continue
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
		read, seen := got[r.name]
		if !seen {
			t.Errorf("%s: the probe printed no verdict", r.name)
			continue
		}
		if read != r.read {
			t.Errorf("%s: the emitted reader answers %v where the engine answers %v", r.name, read, r.read)
		}
	}
}
