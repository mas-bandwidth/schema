package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// THE TEXT READ HOLDS DEPRECATED VARIANTS (docs/SPEC-TABLES.md §2.15 READ, SF-27):
// a stored value naming a variant deprecated in place reads as the field
// default (an array element: None), never as the variant itself, as the JSON
// and binary reads already do. Every deprecated case has a live twin.

const textDeprecatedSchema = `package textdep

enum Grade
{
    Bronze
    Silver | deprecated
    Gold
}

table Probe
{
    g  Grade = Gold
    gs [..2]Grade
}
`

var textDeprecatedCases = []struct{ name, text, want string }{
	{"field_deprecated", `(:g "Silver")`, "g 3 gs 0 0"},
	{"field_live", `(:g "Bronze")`, "g 1 gs 0 0"},
	{"element_deprecated", `(:gs ("Silver" "Gold"))`, "g 3 gs 0 3"},
	{"element_live", `(:gs ("Bronze" "Gold"))`, "g 3 gs 1 3"},
}

func TestLobsterTextReadHoldsDeprecatedVariants(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(src, []byte(textDeprecatedSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{src})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\nimport .text_form\n")
	b.WriteString(`def show(name:string, text:string):
    let doc = schema_rt.text_load("Probe", text)
    if not doc:
        print(name + " refused")
        return
    let gs = schema_rt.slot_named(doc.rec, "gs")
    print(name + " g " + string(schema_rt.slot_named(doc.rec, "g").cell.i) + " gs " + string(gs.elems[0].i) + " " + string(gs.elems[1].i))
`)
	for _, c := range textDeprecatedCases {
		fmt.Fprintf(&b, "show(%q, %q)\n", c.name, c.text)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, code := lobsterrun.RunExit(t, g.dir, "main.lobster", nil)
	got := strings.Split(strings.TrimRight(string(stdout), "\n"), "\n")
	var bad []string
	for i, c := range textDeprecatedCases {
		want := c.name + " " + c.want
		line := "(no line: the program stopped)"
		if i < len(got) {
			line = got[i]
		}
		if line != want {
			bad = append(bad, fmt.Sprintf("%s: got %q, want %q", c.name, line, want))
		}
	}
	if code != 0 {
		bad = append(bad, fmt.Sprintf("the program exited %d", code))
	}
	if len(bad) > 0 {
		t.Fatalf("the text read does not hold deprecated variants:\n%s", strings.Join(bad, "\n"))
	}
}
