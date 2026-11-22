package squirreltable_test

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// TestSquirrelCompressedF32EndsSource pins A COMPRESSED FLOAT'S RANGE IS HELD
// AT FLOAT32 (docs/SPEC-TABLES.md §3.3) in the source, with no interpreter:
// the descriptor every variable, JSON and message path compares against
// carries the float32 nearest each declared end (0.1f and -0.3f, not the
// doubles 0.1 and -0.3), the fixed form's emitted guard compares at those same
// ends, and the runtime's variable writer narrows the value before it
// compares. The control: a plain float32 carries no range and no guard.
func TestSquirrelCompressedF32EndsSource(t *testing.T) {
	t.Parallel()
	if math.Float32bits(float32(0.1)) != 0x3dcccccd || squirrel.Float32Lit(0.1) == squirrel.Float64Lit(0.1) {
		t.Fatal("the probe's ends are not distinct at float32")
	}
	module := func(decl string) (string, map[string][]byte) {
		t.Helper()
		files, err := squirreltable.Generate(unitFromSchema(t, "package probe\n\ntable V\n{\n    "+decl+"\n}\n\nfixed table F\n{\n    "+decl+"\n}\n"))
		if err != nil {
			t.Fatal(err)
		}
		var src strings.Builder
		for name, data := range files {
			if !strings.HasPrefix(name, "schema_") {
				src.Write(data)
			}
		}
		return src.String(), files
	}
	src, files := module("r float32 | min = -0.3, max = 0.1, resolution = 0.01")
	fixedEnd := func(v float64) string { return strconv.FormatFloat(float64(float32(v)), 'e', -1, 64) }
	for _, end := range []struct{ key, lit string }{
		{".fmin <- ", squirrel.Float32Lit(-0.3) + ";"},
		{".fmax <- ", squirrel.Float32Lit(0.1) + ";"},
	} {
		if n, all := strings.Count(src, end.key+end.lit), strings.Count(src, end.key); n == 0 || n != all {
			t.Errorf("the descriptors do not carry the float32 end %q (%d of %d %s do)", end.key+end.lit, n, all, strings.TrimSpace(end.key))
		}
	}
	guard := "< " + fixedEnd(-0.3) + " || "
	if !strings.Contains(src, guard) || !strings.Contains(src, "> "+fixedEnd(0.1)) {
		t.Errorf("the fixed form's range guard does not compare at the float32 ends %s and %s", fixedEnd(-0.3), fixedEnd(0.1))
	}
	if plain, _ := module("p float32"); strings.Contains(plain, ".fmin <- ") || strings.Contains(plain, ".fmax <- ") || strings.Contains(plain, "out_of_range") {
		t.Errorf("a plain float carries a range")
	}
	const narrow = `local f = d.f32 ? f32Value(val.tofloat()) : val.tofloat();
      if (f < d.fmin || f > d.fmax) return refuseValue(w);`
	if !strings.Contains(string(files["schema_table_save.nut"]), narrow) {
		t.Errorf("schema_table_save.nut no longer narrows a float32 before its range test:\nwant %q", narrow)
	}
}
