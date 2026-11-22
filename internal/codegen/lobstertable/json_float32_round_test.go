package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A FLOAT32 IS SPELLED FROM ITS FLOAT32 VALUE (docs/SPEC-TABLES.md §16.2; Go's
// writeFloat): the writer rounds the stored double to float32 first, so 0.1
// is "0.1" and a double just past float32's top that rounds to it is its
// spelling, and a double that rounds past float32's range is refused as
// non_finite_float, as the save refuses it, never spelled as a double the
// reader would refuse.
func TestLobsterJSONFloat32WriterRoundsFirst(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Lobster toolchain")
	path := filepath.Join(t.TempDir(), "Root.schema")
	if err := os.WriteFile(path, []byte("package floatroundprobe\ntable Root { value float32 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString(`import .json
def go(name:string, x:float):
    let rec = schema_rt.new_record(schema_rt.table_of("Root"))
    schema_rt.slot_named(rec, "value").cell.f = x
    let text, why = schema_rt.json_write(rec)
    print(name + " " + text + "|" + why)
go("one_e300", 1.0e300)
go("tenth", 0.1)
go("past_max_rounds_down", 3.40282357e38)
go("past_max_rounds_up", 3.5e38)
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	out := string(lobsterrun.Run(t, g.dir, "main.lobster", nil))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		name, rest, _ := strings.Cut(line, " ")
		got[name] = rest
	}
	for name, want := range map[string]string{
		"one_e300":             "|non_finite_float",
		"tenth":                `{"value":0.1}` + "\n|",
		"past_max_rounds_down": `{"value":3.4028235e+38}` + "\n|",
		"past_max_rounds_up":   "|non_finite_float",
	} {
		if got[name] != want {
			t.Errorf("%s: got %q, want %q", name, got[name], want)
		}
	}
	if t.Failed() {
		t.Logf("output:\n%s", out)
	}
}
