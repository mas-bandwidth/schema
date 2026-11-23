package mootable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestMooJsonLabelRules holds docs/SPEC-TABLES.md §16.7's label rules in the
// moo JSON read, placed and skipped, against the conformance corpus's
// graphdemo rows (testdata/conformance/tables/MANIFEST.txt, json-hostile): a
// label is followed by a comma or the object's close, never the next key; a
// skipped `{"&node": N}` alone refers, so N must be defined earlier and
// closed; a skipped `&node` with fields defines, so N must be new to the
// whole text. The verdict is the contract, so no reason is compared.
func TestMooJsonLabelRules(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (it runs the emitted text form)")

	data, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	type row struct{ name, root, verdict string }
	var rows []row
	inputs := map[string][]byte{}
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) != 6 || f[0] != "json-hostile" || f[2] != "graphdemo" {
			continue
		}
		if !strings.HasPrefix(f[1], "label-") && !strings.HasPrefix(f[1], "skipped-") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, f[4], f[3]+".json"))
		if err != nil {
			t.Fatal(err)
		}
		inputs[f[1]] = body
		rows = append(rows, row{name: f[1], root: f[3], verdict: f[5]})
	}
	for _, must := range []string{"label-missing-comma", "skipped-reference-undefined", "skipped-label-defined-twice"} {
		if _, ok := inputs[must]; !ok {
			t.Fatalf("the manifest has no graphdemo json-hostile row %s", must)
		}
	}

	u := unitFromPath(t, filepath.Join("tables", "pointers"))
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	g := writeUnit(t, u, files)
	var b strings.Builder
	b.WriteString("--!strict\nlocal inputs = require(\"./inputs\")\n")
	b.WriteString("local function run(mod: any, root: string, name: string)\n")
	b.WriteString("\tlocal v = mod[\"New\" .. root]()\n")
	b.WriteString("\tlocal r: any = {}\n")
	b.WriteString("\tlocal ok = mod[root .. \"FromJson\"](v, inputs[name], r)\n")
	b.WriteString("\tprint(name .. \" \" .. (if ok then \"read\" else \"refused\"))\nend\n")
	mods := map[string]bool{}
	for _, r := range rows {
		mod := u.DeclFile[r.root] + "Table"
		if !mods[mod] {
			mods[mod] = true
			fmt.Fprintf(&b, "local %s: any = require(\"./%s\")\n", mod, mod)
		}
		fmt.Fprintf(&b, "run(%s, %q, %q)\n", mod, r.root, r.name)
	}

	out := strings.Split(g.run(t, b.String(), inputs), "\n")
	if len(out) != len(rows) {
		t.Fatalf("the probe printed %d lines for %d rows:\n%s", len(out), len(rows), strings.Join(out, "\n"))
	}
	for i, r := range rows {
		want := r.name + " " + r.verdict
		if out[i] != want {
			t.Errorf("%s (root %s): got %q, want %q", r.name, r.root, out[i], want)
		}
	}
}
