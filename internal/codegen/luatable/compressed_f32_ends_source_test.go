package luatable

import (
	"math"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaCompressedF32EndsSource pins A COMPRESSED FLOAT'S RANGE IS HELD AT
// FLOAT32 (docs/SPEC-TABLES.md §3.3) in the source, with no interpreter: the
// descriptor of a compressed float carries the float32 nearest each declared
// end (0.1f, -0.3f, not the doubles 0.1 and -0.3), and the runtime's one range
// test narrows the value to float32 before it compares. The control: a plain
// float32 beside it carries no range at all.
func TestLuaCompressedF32EndsSource(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "p.schema"), `package probe

table V
{
    r float32 | min = -0.3, max = 0.1, resolution = 0.01
}
`)
	u := luarun.LoadUnit(t, dir)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	src := string(files[ModuleName(u)+".lua"])
	if src == "" {
		t.Fatalf("no %s.lua among the generated files", ModuleName(u))
	}
	lo := "fmin = " + lua.FloatLiteral(float64(float32(-0.3))) + ","
	hi := "fmax = " + lua.FloatLiteral(float64(float32(0.1))) + ","
	if lua.FloatLiteral(float64(float32(0.1))) == lua.FloatLiteral(0.1) || math.Float32bits(float32(0.1)) != 0x3dcccccd {
		t.Fatal("the probe's ends are not distinct at float32")
	}
	for _, end := range []string{lo, hi} {
		key, _, _ := strings.Cut(end, " = ")
		if n, all := strings.Count(src, end), strings.Count(src, key+" = "); n == 0 || n != all {
			t.Errorf("the compressed float's descriptor does not carry the float32 end %q (%d of %d %s do)", end, n, all, key)
		}
	}
	// the control: a unit whose one float is plain carries no range at all
	plain := t.TempDir()
	writeFile(t, filepath.Join(plain, "p.schema"), "package probe\n\ntable V\n{\n    p float32\n}\n")
	pu := luarun.LoadUnit(t, plain)
	pfiles, err := Generate(pu)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if psrc := string(pfiles[ModuleName(pu)+".lua"]); psrc == "" || strings.Contains(psrc, "fmin") || strings.Contains(psrc, "fmax") {
		t.Errorf("a plain float's unit carries a range (or is missing)")
	}
	rt := string(files["descriptor.lua"])
	const narrow = `local x = string.unpack("<f", string.pack("<f", v))
  return x < d.fmin or x > d.fmax`
	if !strings.Contains(rt, narrow) {
		t.Errorf("descriptor.lua's D.float_off_range does not narrow v to float32 before the compare:\nwant %q", narrow)
	}
}
