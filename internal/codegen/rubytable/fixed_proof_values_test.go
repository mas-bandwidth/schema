package rubytable_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"maps"
	"math"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/ruby/rubyrun"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/rubytable"
	"github.com/mas-bandwidth/schema/v3/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/ir"
)

var rubyFixedValueSetups = map[string]string{
	"bool-values":          "rows[1].a = true\nrows[1].b = false\n",
	"signed-integers":      "rows[1].a, rows[1].b, rows[1].c, rows[1].d = 127, 32767, 2147483647, 9223372036854775807\nrows[2].a, rows[2].b, rows[2].c, rows[2].d = -1, 0, 1, -2\n",
	"unsigned-integers":    "rows[1].a, rows[1].b, rows[1].c, rows[1].d = 0, 0, 0, 0\nrows[2].a, rows[2].b, rows[2].c, rows[2].d = 1, 256, 65536, 4294967296\n",
	"signed-128":           "rows[1].a = (1 << 127) - 1\nrows[2].a = 1\n",
	"unsigned-128":         "rows[1].a = 0\nrows[2].a = 1\n",
	"ranged-integers":      "rows[0].a, rows[0].b, rows[0].c = -5, -1000000000000, 0\nrows[1].a, rows[1].b, rows[1].c = 5, 1000000000000, 1000\n",
	"bits-values":          "rows[0].a, rows[0].b, rows[0].c, rows[0].d = 7, 4095, 1048575, 1099511627775\n",
	"float32-values":       "rows[1].a, rows[1].b = -1024.25, 2048.75\n",
	"float64-values":       "rows[1].a, rows[1].b = -987654321.125, 123456789.875\n",
	"compressed-floats":    "rows[0].a, rows[0].b = 0.25, -0.5\nrows[1].a, rows[1].b = 0.0, -1.0\nrows[2].a, rows[2].b = 1.0, 1.0\n",
	"signed-fixed-point":   "rows[0].a, rows[0].b, rows[0].c = -(8 << 16), -(100 << 16), -0x1000000\nrows[1].a, rows[1].b, rows[1].c = 8 << 16, 100 << 16, 0xfa0000\n",
	"unsigned-fixed-point": "rows[1].a, rows[1].b, rows[1].c = 16 << 16, 100 << 16, 1000 << 16\n",
	"flags-values":         "rows[0].caps, rows[1].caps, rows[2].caps = 0, 5, 7\n",
	"enum-values":          "rows[0].g, rows[1].g, rows[2].g = 0, 1, 3\n",
	"scalar-defaults":      "rows[1].x, rows[1].g, rows[1].f = -9, 3, -2.25\n",
}

func init() {
	maps.Copy(rubyFixedSetups, rubyFixedValueSetups)
	for _, id := range []string{"scalar-bounds", "ordinals", "normalization"} {
		rubyFixedSetups[id] = ""
	}
}

var rubyFixedValueRows = []string{
	"bool-values", "signed-integers", "unsigned-integers", "signed-128", "unsigned-128",
	"ranged-integers", "bits-values", "float32-values", "float64-values", "compressed-floats",
	"signed-fixed-point", "unsigned-fixed-point", "flags-values", "enum-values", "scalar-defaults",
}

type rubyScalarValueRunner struct{ rubyFixedProofRunner }

func (rubyScalarValueRunner) LoadAndResave(t *testing.T, tc fixedproof.RowCase, u *ir.Unit, wire []byte) ([]byte, error) {
	t.Helper()
	files, err := rubytable.Generate(u)
	if err != nil {
		return nil, err
	}
	script := fmt.Sprintf(`require_relative "probe"
rows = Array.new(%d) { Probe::T.new }
%s
loaded = Probe::T.fixed_load(File.binread("wire"), %d)
raise "scalar mismatch" unless loaded == rows
File.binwrite("saved", Probe::T.fixed_save(loaded))
`, tc.Count, rubyFixedValueSetups[tc.ID], tc.Count)
	_, saved := rubyFixedRun(t, files, script, map[string][]byte{"wire": wire})
	return saved, nil
}

func TestRubyFixedProofValues(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "pinned Ruby and the C++ fixed oracle")
	fixedproof.RunValueRows(t, rubyScalarValueRunner{}, rubyFixedValueRows...)
}

func TestRubyFixedProofValuesNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "pinned Ruby and the C++ fixed oracle")
	fixedproof.RunNegativeControlRows(t, rubyFixedProofRunner{}, rubyFixedValueRows...)
}

func TestRubyFixedProofValuesTypes(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "pinned RuboCop and Steep")
	for _, tc := range fixedproof.Cases() {
		if _, ok := rubyFixedValueSetups[tc.ID]; !ok {
			continue
		}
		t.Run(tc.ID, func(t *testing.T) {
			t.Parallel()
			u := fixedproof.ParseCheckUnit(t, tc.Schema)
			files, err := rubytable.Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			dir := rubyFixedTree(t, files)
			rubyrun.Analyze(t, dir, "probe")
		})
	}
}

func TestRubyFixedProofValuesReadRules(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "pinned Ruby and the C++ fixed oracle")
	cases := []struct {
		name, schema string
		body         int64
		corruptions  []struct {
			offset int
			value  []byte
		}
		anchors []string
	}{
		{"scalar-bounds", "package probe\nfixed table T { x int32 = 0 | min = -5, max = 5\n s int128 = 0 | min = -500, max = 500\n u uint16 = 10 | min = 10, max = 1000\n}\n", 22,
			[]struct {
				offset int
				value  []byte
			}{{0, []byte{6, 0, 0, 0}}, {0, []byte{0xfa, 0xff, 0xff, 0xff}}, {4, []byte{0xf5, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}}, {4, []byte{0x0b, 0xfe, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}}, {20, []byte{9, 0}}, {20, []byte{0xe9, 3}}},
			[]string{"if value < lo || value > hi\n        raise Refused.new(\"out_of_range\")"}},
		{"ordinals", "package probe\nenum Grade { Gold, Silver }\nunion Pick { a int32\n b int32\n}\nfixed table T { g Grade\n pick Pick\n}\n", 6,
			[]struct {
				offset int
				value  []byte
			}{{0, []byte{3}}, {1, []byte{3}}},
			[]string{"if value < lo || value > hi\n        raise Refused.new(\"out_of_range\")", `if shape == "enum" && value > spec["variants"].length`, `if tag > arms.length`}},
		{"normalization", "package probe\nfixed table T { a bool\n b ?int32\n}\n", 6,
			[]struct {
				offset int
				value  []byte
			}{{0, []byte{2}}, {0, []byte{0x5a}}, {0, []byte{0x7f}}, {0, []byte{0x80}}, {0, []byte{0xff}}, {1, []byte{2}}, {1, []byte{0x5a}}, {1, []byte{0x7f}}, {1, []byte{0x80}}, {1, []byte{0xff}}},
			[]string{`if value > 1`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			row := fixedproof.RowCase{ID: tc.name, Schema: tc.schema, Root: "T", Body: tc.body, Count: 1}
			u := fixedproof.ParseCheckUnit(t, tc.schema)
			cpp, verify := fixedproof.CppOracle(t, row, u)
			files, err := rubytable.Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			dir := rubyFixedTree(t, files)
			rubyrun.Analyze(t, dir, "probe")
			setup := `require_relative "probe"
File.binwrite("saved", Probe::T.fixed_save([Probe::T.new]))
`
			_, rbWire := rubyFixedRun(t, files, setup, nil)
			if !bytes.Equal(rbWire, cpp) {
				t.Fatal("default Ruby save differs from C++")
			}
			if err := verify(rbWire); err != nil {
				t.Fatalf("C++ rejected Ruby: %v", err)
			}
			runner := rubyFixedProofRunner{}
			resaved, err := runner.LoadAndResave(t, row, u, cpp)
			if err != nil || !bytes.Equal(resaved, cpp) {
				t.Fatalf("Ruby load/resave differs: %v", err)
			}
			badWriter, err := runner.SaveMutated(t, row, u)
			if err == nil && (bytes.Equal(badWriter, cpp) || verify(badWriter) == nil) {
				t.Fatal("writer negative control did not change the reference equality verdict")
			}
			probe := `require_relative "probe"
begin
  Probe::T.fixed_load(File.binread("wire"), 1)
rescue Probe::Refused
  puts "refused"
rescue StandardError => err
  puts "crash #{err.class}"
else
  puts "accepted"
end
`
			mutated := maps.Clone(files)
			const rpath = "probe/_fixed.rb"
			src := string(mutated[rpath])
			for _, anchor := range tc.anchors {
				if strings.Count(src, anchor) != 1 {
					t.Fatalf("control anchor %q occurs %d times", anchor, strings.Count(src, anchor))
				}
				first, rest, hasNL := strings.Cut(anchor, "\n")
				cond := strings.TrimPrefix(first, "if ")
				repl := "if false && (" + cond + ")"
				if hasNL {
					repl += "\n" + rest
				}
				src = strings.Replace(src, anchor, repl, 1)
			}
			mutated[rpath] = []byte(src)
			start := len(cpp) - int(tc.body)
			for _, c := range tc.corruptions {
				bad := bytes.Clone(cpp)
				copy(bad[start+c.offset:], c.value)
				out, _ := rubyFixedRun(t, files, probe, map[string][]byte{"wire": bad})
				if strings.TrimSpace(out) != "refused" {
					t.Fatalf("clean runtime did not refuse corruption at +%d: %s", c.offset, out)
				}
				outMut, _ := rubyFixedRun(t, mutated, probe, map[string][]byte{"wire": bad})
				if strings.TrimSpace(outMut) == "refused" {
					t.Fatalf("mutated runtime still refused corruption at +%d: %s", c.offset, outMut)
				}
			}
		})
	}
}

func TestRubyFixedProofValuesFloatBounds(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "pinned Ruby and the C++ fixed oracle")
	const src = `package probe
fixed table T {
    f32 float32
    f64 float64
    cf float32 | min = -1, max = 1, resolution = 0.25
}
`
	u := fixedproof.ParseCheckUnit(t, src)
	row := fixedproof.RowCase{ID: "float-bounds", Schema: src, Root: "T", Body: 16, Count: 1}
	cpp, _ := fixedproof.CppOracle(t, row, u)
	files, err := rubytable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	start := len(cpp) - 16
	inputs := map[string][]byte{"clean": cpp}
	for name, bits := range map[string]uint32{
		"f32_nan": 0x7fc00000,
		"f32_inf": 0x7f800000,
		"cf_high": math.Float32bits(1.5),
		"cf_low":  math.Float32bits(-1.5),
	} {
		b := bytes.Clone(cpp)
		off := start
		if strings.HasPrefix(name, "cf_") {
			off = start + 12
		}
		binary.LittleEndian.PutUint32(b[off:], bits)
		inputs[name] = b
	}
	b64 := bytes.Clone(cpp)
	binary.LittleEndian.PutUint64(b64[start+4:], 0x7ff0000000000000)
	inputs["f64_inf"] = b64

	probe := `require_relative "probe"
%w[f32_nan f32_inf f64_inf cf_high cf_low].each do |name|
  begin
    Probe::T.fixed_load(File.binread(name))
  rescue Probe::Refused
    # expected
  else
    raise "accepted #{name}"
  end
end
[Float::NAN, Float::INFINITY, -Float::INFINITY, 1.0e39].each do |bad|
  v = Probe::T.new
  v.f32 = bad
  begin
    Probe::T.fixed_save([v])
  rescue Probe::InvalidValue
    # expected
  else
    raise "wrote bad f32 #{bad}"
  end
end
v = Probe::T.new
v.cf = 1.5
begin
  Probe::T.fixed_save([v])
rescue Probe::InvalidValue
  puts "ok"
else
  raise "wrote out-of-range cf"
end
`
	out, _ := rubyFixedRun(t, files, probe, inputs)
	if strings.TrimSpace(out) != "ok" {
		t.Fatalf("float bounds: %s", out)
	}
}
