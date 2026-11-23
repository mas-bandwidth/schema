package squirreltable_test

// A counted [A..B] holds A inside a union arm (ruling R10; docs/SPEC.md §5.1
// row 3): the fixed read refuses an arm payload whose count is below A, and
// the fixed writer and the text writer refuse a record whose arm holds fewer,
// in every build. The root is tabledemo MinArmFix (tables/examples/
// Tables.schema), its images the conformance rows armmin_fix_count_{0,1}.

import (
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
)

func TestSquirrelUnionArmCountedMinimum(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	src, err := os.ReadFile("../../../tables/examples/Tables.schema")
	if err != nil {
		t.Fatal(err)
	}
	u := fixedSynthetic(t, string(src))
	g := sqEvoGenerate(t, u, nil)
	for _, n := range []string{"armmin_fix_count_0", "armmin_fix_count_1"} {
		bin, err := os.ReadFile("../../../testdata/wire/tables/framing/" + n + ".bin")
		if err != nil {
			t.Fatal(err)
		}
		if got := g.load(t, "MinArmFix", bin); !strings.HasPrefix(got.head, "refused ") {
			t.Errorf("%s: the fixed read answers %s, want refused", n, got.head)
		}
	}
	// a writer: the arm below its minimum refuses, at the minimum and the
	// maximum it writes
	probe := func(few string) string {
		out, err := g.exec(t, `local T = dofile("schema_module.nut", true).require("`+g.mod+`");
local v = T.MinArmFix();
v.Pick.Type = 1;
v.Pick.Few = `+few+`;
local out = blob(1024);
local n = T.MinArmFixFixedSave(out, [v]);
print("fixed " + (n < 0 ? "refused" : "wrote") + "\n");
local js = T.MinArmFixToJson(v);
print("json " + (js == null ? "refused" : "wrote") + "\n");
`, nil)
		if err != nil {
			t.Fatalf("sq: %v\n%s", err, out)
		}
		return out
	}
	for few, want := range map[string]string{
		"[7]":          "fixed refused\njson refused",
		"[]":           "fixed refused\njson refused",
		"[7, 9]":       "fixed wrote\njson wrote",
		"[1, 2, 3, 4]": "fixed wrote\njson wrote",
	} {
		if got := strings.TrimSpace(probe(few)); got != want {
			t.Errorf("writer, few %s: %q, want %q", few, got, want)
		}
	}
}
