package mootable_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
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

// TestMooJsonDepthCap holds the JSON read to §16.2's depth cap, which counts
// every container, placed or skipped, the root at depth 0: a text whose
// deepest container is at depth 128 reads and one at 129 refuses. Each row
// reaches its table through a different container (the map object, the union
// object, the list) and nests arrays under an unknown key there, at depth 3,
// so a reader that counts the map or the union as no level reads the 129 row
// (testdata/conformance/tables MANIFEST.txt, depth-map-past-cap and
// depth-union-past-cap).
func TestMooJsonDepthCap(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (it runs the emitted text form)")

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

	u := unitFromSource(t, jsonDepthCapSchema)
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	g := writeUnit(t, u, files)
	mod := u.DeclFile["Root"] + "Table"
	var b strings.Builder
	b.WriteString("--!strict\nlocal inputs = require(\"./inputs\")\n")
	fmt.Fprintf(&b, "local M: any = require(\"./%s\")\n", mod)
	b.WriteString("local function run(name: string)\n")
	b.WriteString("\tlocal v = M.NewRoot()\n\tlocal r: any = {}\n")
	b.WriteString("\tlocal ok = M.RootFromJson(v, inputs[name], r)\n")
	b.WriteString("\tprint(name .. \" \" .. (if ok then \"read\" else \"refused\"))\nend\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "run(%q)\n", r.name)
	}
	out := strings.Split(g.run(t, b.String(), inputs), "\n")
	if len(out) != len(rows) {
		t.Fatalf("the probe printed %d lines for %d rows:\n%s", len(out), len(rows), strings.Join(out, "\n"))
	}
	for i, r := range rows {
		if want := r.name + " " + r.verdict; out[i] != want {
			t.Errorf("got %q, want %q", out[i], want)
		}
	}
}
