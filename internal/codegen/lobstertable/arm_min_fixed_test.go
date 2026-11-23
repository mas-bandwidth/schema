package lobstertable_test

import (
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A COUNTED [A..B] HOLDS A INSIDE A UNION ARM (ruling R10): the fixed form's
// read refuses an arm payload whose count is below A as a refusal (the verdict is the contract), the
// text read refuses it, and the fixed writer and the text writer refuse a
// record whose arm holds fewer than A elements, in every build. The root is
// tabledemo MinArmFix (tables/examples/Tables.schema), its fixed images are
// the conformance rows armmin_fix_count_{0,1}.
func TestLobsterUnionArmCountedMinimum(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Lobster toolchain")
	schema, err := os.ReadFile("../../../tables/examples/Tables.schema")
	if err != nil {
		t.Fatal(err)
	}
	g := finiteGenerate(t, string(schema))
	inputs := map[string][]byte{}
	for _, n := range []string{"armmin_fix_count_0", "armmin_fix_count_1"} {
		b, err := os.ReadFile("../../../testdata/wire/tables/framing/" + n + ".bin")
		if err != nil {
			t.Fatal(err)
		}
		inputs[n] = b
	}
	out := finiteRun(t, g, `import .json
for(["armmin_fix_count_0", "armmin_fix_count_1"]) f:
    let rep = schema_rt.LoadReport {}
    let back = schema_rt.load("MinArmFix", read_file(f) or "", rep)
    print("read " + f + " " + (if back: "read" else: rep.reason))
for([1, 2, 4]) n:
    let rec = schema_rt.new_record(schema_rt.table_of("MinArmFix"))
    let u = fslot(rec, "pick")
    let un = schema_rt.lookup_union(u.fd.t)
    assert un
    u.cell.i = 1
    u.cell.arm = schema_rt.new_slot(un.arms[0])
    let a = u.cell.arm

    while length(a.elems) < n:
        push(a.elems, schema_rt.Value {})
    a.count = n
    let _w, why = schema_rt.save_reasoned(rec)
    print("save " + n + " " + (if why == "": "wrote" else: "refused"))
    let _js, jwhy = schema_rt.json_write(rec)
    print("json " + n + " " + (if jwhy == "": "wrote" else: "refused"))
`, inputs)
	got := strings.Join(out, "\n")
	for _, want := range []string{
		"read armmin_fix_count_0 out_of_range",
		"read armmin_fix_count_1 out_of_range",
		"save 1 refused", "json 1 refused",
		"save 2 wrote", "json 2 wrote",
		"save 4 wrote", "json 4 wrote",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}
