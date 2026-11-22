package lobstertable_test

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A FLOAT32 IS SPELLED FROM ITS FLOAT32 VALUE (docs/SPEC-TABLES.md §16.2; Go's
// writeFloat): the writer rounds the stored double to float32 first, so a
// double that is not a float32 takes the float32's shortest spelling, and one
// that rounds past float32's range is refused as non_finite_float, as the save
// refuses it, never spelled as a double the reader would refuse.
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
	canonical := func(f float32) string {
		for precision := 6; precision <= 9; precision++ {
			text := strconv.FormatFloat(float64(f), 'g', precision, 32)
			back, err := strconv.ParseFloat(text, 32)
			if err != nil {
				t.Fatal(err)
			}
			if precision == 9 || float32(back) == f {
				return text
			}
		}
		panic("unreachable")
	}
	// doubles that are no float32: more digits than a float32 holds, one just
	// under the rounding edge of float32's top, and two past the range
	xs := []float64{0.123456789012, 1.0000000001, 16777217.0, 3.40282356e38, 3.40282357e38, 3.5e38, 1.0e300, -1.0e300}
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString(`import .json
def go(x:float):
    let rec = schema_rt.new_record(schema_rt.table_of("Root"))
    schema_rt.slot_named(rec, "value").cell.f = x
    let text, why = schema_rt.json_write(rec)
    print("@" + text + "|" + why)
`)
	for _, x := range xs {
		fmt.Fprintf(&b, "go(%s)\n", strconv.FormatFloat(x, 'e', 17, 64))
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(string(lobsterrun.Run(t, g.dir, "main.lobster", nil)), "@")[1:]
	for i := range out {
		out[i] = strings.Join(strings.Fields(out[i]), " ")
	}
	if len(out) != len(xs) {
		t.Fatalf("got %d lines, want %d: %q", len(out), len(xs), out)
	}
	for i, x := range xs {
		f := float32(x)
		want := "|non_finite_float"
		if !math.IsInf(float64(f), 0) {
			want = fmt.Sprintf(`{ "value": %s }|`, canonical(f))
		}
		if out[i] != want {
			t.Errorf("%v: got %q, want %q", x, out[i], want)
		}
	}
}
