package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// jsonDepthCapSchema puts a table one level inside each kind of container the
// JSON read opens: a map's value, a union's body arm and a list's element.
const jsonDepthCapSchema = `package depthcap

type Inner
{
    n int32
}

union Arm
{
    body Inner
    idle
}

table Root
{
    arm  Arm
    tags map[string]Inner
    list [..4]Inner
}
`

// TestLobsterJsonDepthCap holds the JSON read to §16.2's depth cap, which
// counts every container, placed or skipped, the root at depth 0: a text
// whose deepest container is at depth 128 reads and one at 129 refuses. Each
// row reaches its table through a different container (the map object, the
// union object, the list) and nests arrays under an unknown key there, at
// depth 3, so a reader that counts the map or the union as no level reads the
// 129 row (testdata/conformance/tables MANIFEST.txt, depth-map-past-cap and
// depth-union-past-cap).
func TestLobsterJsonDepthCap(t *testing.T) {
	t.Parallel()
	nest := func(k int) string { return strings.Repeat("[", k) + strings.Repeat("]", k) }
	sites := []struct{ name, open, close string }{
		{"map", `{"tags": {"a": {"zz": `, `}}}`},
		{"union", `{"arm": {"body": {"zz": `, `}}}`},
		{"list", `{"list": [{"zz": `, `}]}`},
	}
	type row struct{ name, verdict string }
	var rows []row
	inputs := map[string][]byte{}
	for _, s := range sites {
		// the unknown key's value opens at depth 3, so k arrays reach 2 + k
		for _, c := range []struct {
			k       int
			verdict string
		}{{126, "read"}, {127, "refused"}} {
			name := fmt.Sprintf("%s-%d", s.name, 2+c.k)
			inputs[name] = []byte(s.open + nest(c.k) + s.close)
			rows = append(rows, row{name, c.verdict})
		}
	}

	g, err := generateLoadUnit(t, []string{writeSchema(t, "DepthCap", jsonDepthCapSchema)})
	if err != nil {
		t.Fatalf("the unit is refused: %v", err)
	}
	var p strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&p, "import .%s\n", m)
	}
	p.WriteString("import .load\nimport .json\n")
	p.WriteString(keyedRegistrations(g.u))
	for i, r := range rows {
		fmt.Fprintf(&p, "let t%d = read_file(%s)\nassert t%d\n", i, lobster.StringLit([]byte(r.name)), i)
		fmt.Fprintf(&p, "let v%d = schema_rt.json_read(\"Root\", t%d, schema_rt.LoadReport {})\n", i, i)
		fmt.Fprintf(&p, "print(%s + if v%d: \" read\" else: \" refused\")\n", lobster.StringLit([]byte(r.name)), i)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(p.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs))), "\n")
	if len(out) != len(rows) {
		t.Fatalf("the program printed %d lines for %d rows:\n%s", len(out), len(rows), strings.Join(out, "\n"))
	}
	for i, r := range rows {
		if want := r.name + " " + r.verdict; out[i] != want {
			t.Errorf("got %q, want %q", out[i], want)
		}
	}
}
