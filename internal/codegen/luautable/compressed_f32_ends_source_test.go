package luautable_test

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
)

// TestLuauCompressedF32EndsSource pins A COMPRESSED FLOAT'S RANGE IS HELD AT
// FLOAT32 (docs/SPEC-TABLES.md §3.3) in the source, with no interpreter: the
// descriptor of a compressed float carries the float32 nearest each declared
// end (0.1f and -0.3f, not the doubles 0.1 and -0.3), and the runtime's write
// and JSON range tests narrow the value to float32 before they compare. The
// control: a plain float32 carries no range at all.
func TestLuauCompressedF32EndsSource(t *testing.T) {
	t.Parallel()
	if math.Float32bits(float32(0.1)) != 0x3dcccccd || luau.FormatFloat32(0.1) == luau.FormatFloat(0.1) {
		t.Fatal("the probe's ends are not distinct at float32")
	}
	descriptors := func(schema string) string {
		t.Helper()
		files, err := luautable.Generate(review1121Unit(t, writeSchema(t, schema)))
		if err != nil {
			t.Fatal(err)
		}
		var floats []string
		for name, data := range files {
			if strings.HasPrefix(name, "schema_") {
				continue
			}
			for line := range strings.SplitSeq(string(data), "\n") {
				if strings.Contains(line, `s = "float"`) {
					floats = append(floats, line)
				}
			}
		}
		if len(floats) == 0 {
			t.Fatal("no float descriptor among the generated modules")
		}
		return strings.Join(floats, "\n")
	}
	ends := "lo = " + luau.FormatFloat32(-0.3) + ", hi = " + luau.FormatFloat32(0.1) + ","
	got := descriptors("package probe\n\ntable V\n{\n    r float32 | min = -0.3, max = 0.1, resolution = 0.01\n}\n")
	if !strings.Contains(got, ends) || strings.Count(got, "lo = ") != strings.Count(got, ends) {
		t.Errorf("the compressed float's descriptor does not carry the float32 ends %q:\n%s", ends, got)
	}
	if plain := descriptors("package probe\n\ntable V\n{\n    p float32\n}\n"); strings.Contains(plain, "lo = ") || strings.Contains(plain, "hi = ") {
		t.Errorf("a plain float's descriptor carries a range:\n%s", plain)
	}
	files, err := luautable.Generate(review1121Unit(t, writeSchema(t, "package probe\n\ntable V\n{\n    p float32\n}\n")))
	if err != nil {
		t.Fatal(err)
	}
	for _, pin := range []struct{ file, narrow string }{
		// the writer's valid_scalar compares held_float's narrowed value
		{"schema_save.luau", "buffer.writef32(scratch, 0, v)\n\t\tv = buffer.readf32(scratch, 0)"},
		// the JSON writer's float_ok narrows before it compares
		{"schema_json.luau", "if fd.f32 and x == x then buffer.writef32(narrowScratch, 0, x); x = buffer.readf32(narrowScratch, 0) end"},
		// the message writer compares the float32 the value narrows to
		{"schema_message.luau", "local held = if e.f32 then asF32(val) else val"},
	} {
		if !strings.Contains(string(files[pin.file]), pin.narrow) {
			t.Errorf("%s no longer narrows a float32 before its range test:\nwant %q", pin.file, pin.narrow)
		}
	}
}

// writeSchema writes one schema file into a fresh directory and returns its path.
func writeSchema(t *testing.T, schema string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "p.schema")
	if err := os.WriteFile(p, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
