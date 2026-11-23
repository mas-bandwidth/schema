package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// TestLobsterJsonShapesVerdicts holds the Lobster JSON read to every json-hostile
// row of the shapes unit (testdata/conformance/tables/MANIFEST.txt): among
// them a counted array below its Min (shapes-octets-below-min,
// shapes-recs-below-min, shapes-fixed-octets-below-min) refuses as one past
// its Max does (docs/SPEC.md §5.1 row 3), and the same arrays at their Min
// read. The verdict is the contract, so no reason is compared.
func TestLobsterJsonShapesVerdicts(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifest, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	type row struct{ name, root, verdict string }
	var rows []row
	inputs := map[string][]byte{}
	for line := range strings.SplitSeq(string(manifest), "\n") {
		f := strings.Fields(line)
		if len(f) != 6 || f[0] != "json-hostile" || f[2] != "shapes" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(repo, f[4], f[3]+".json"))
		if err != nil {
			t.Fatal(err)
		}
		inputs[f[1]+".json"] = body
		rows = append(rows, row{name: f[1], root: f[3], verdict: f[5]})
	}
	for _, must := range []string{"shapes-octets-below-min", "shapes-recs-below-min", "shapes-fixed-octets-below-min"} {
		if _, ok := inputs[must+".json"]; !ok {
			t.Fatalf("the manifest has no shapes json-hostile row %s", must)
		}
	}

	paths, err := filepath.Glob(filepath.Join(repo, "test", "tables", "shapes", "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("test/tables/shapes: %v, %d schemas", err, len(paths))
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatal(err)
	}
	var program strings.Builder
	for _, mod := range g.mods {
		fmt.Fprintf(&program, "import .%s\n", mod)
	}
	program.WriteString("import .json\n")
	for _, r := range rows {
		fmt.Fprintf(&program, `if true:
    let input = read_file(%q)
    assert input
    let report = schema_rt.LoadReport {}
    let value = schema_rt.json_read(%q, input, report)
    print(%q + (if value: " read" else: " refused"))
`, r.name+".json", r.root, r.name)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := lobsterrun.Run(t, g.dir, "main.lobster", inputs)
	got := make(map[string]string, len(rows))
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		name, verdict, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok {
			got[name] = verdict
		}
	}
	for _, r := range rows {
		if got[r.name] != r.verdict {
			t.Errorf("%s (root %s): the Lobster JSON read gave %q, want %q", r.name, r.root, got[r.name], r.verdict)
		}
	}
}
