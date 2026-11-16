package pythontable_test

import (
	"bytes"
	"encoding/binary"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/pythontable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// These are the scalar value rows of the shared fixed-form oracle. Setup is
// independent of the C++ setup, which the oracle compiles from the same schema.
var pythonFixedValueSetups = map[string]string{
	"bool-values":          "rows[1].a = True\nrows[1].b = False\n",
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
	maps.Copy(pythonFixedSetups, pythonFixedValueSetups)
	for _, id := range []string{"scalar-bounds", "ordinals", "normalization"} {
		pythonFixedSetups[id] = ""
	}
}

var pythonFixedValueRows = []string{
	"bool-values", "signed-integers", "unsigned-integers", "signed-128", "unsigned-128",
	"ranged-integers", "bits-values", "float32-values", "float64-values", "compressed-floats",
	"signed-fixed-point", "unsigned-fixed-point", "flags-values", "enum-values", "scalar-defaults",
}

func TestPythonFixedProofValues(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "pinned Python and the C++ fixed oracle")
	fixedproof.RunValueRows(t, pythonFixedProofRunner{}, pythonFixedValueRows...)
}

func TestPythonFixedProofValuesNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "pinned Python and the C++ fixed oracle")
	fixedproof.RunNegativeControlRows(t, pythonFixedProofRunner{}, pythonFixedValueRows...)
}

func TestPythonFixedProofValuesTypes(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "pinned mypy")
	for _, tc := range fixedproof.Cases() {
		if _, ok := pythonFixedValueSetups[tc.ID]; !ok {
			continue
		}
		t.Run(tc.ID, func(t *testing.T) {
			t.Parallel()
			u := fixedproof.ParseCheckUnit(t, tc.Schema)
			files, err := pythontable.Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			dir := pythonFixedTree(t, files)
			if out, err := pyrun.Analyze(t, dir, "probe"); err != nil {
				t.Fatalf("mypy generated package: %s", out)
			}
			if out, err := pyrun.Analyze(t, dir, "probe/_schema"); err != nil {
				t.Fatalf("mypy runtime: %s", out)
			}
		})
	}
}

// TestPythonFixedProofValuesReadRules holds invalid scalar representations to
// the strict read and exercises each check's own mutated runtime copy.
func TestPythonFixedProofValuesReadRules(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "pinned Python and the C++ fixed oracle")
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
			[]string{"if value < spec.get(\"lo\", 0) or value > spec.get(\"hi\", len(spec.get(\"variants\", []))):"}},
		{"ordinals", "package probe\nenum Grade { Gold, Silver }\nunion Pick { a int32\n b int32\n}\nfixed table T { g Grade\n pick Pick\n}\n", 6,
			[]struct {
				offset int
				value  []byte
			}{{0, []byte{3}}, {1, []byte{3}}},
			[]string{"if value < spec.get(\"lo\", 0) or value > spec.get(\"hi\", len(spec.get(\"variants\", []))):", "if shape == \"enum\" and value > len(spec[\"variants\"]):", "if tag > len(arms):"}},
		{"normalization", "package probe\nfixed table T { a bool\n b ?int32\n}\n", 6,
			[]struct {
				offset int
				value  []byte
			}{{0, []byte{2}}, {0, []byte{0x5a}}, {0, []byte{0x7f}}, {0, []byte{0x80}}, {0, []byte{0xff}}, {1, []byte{2}}, {1, []byte{0x5a}}, {1, []byte{0x7f}}, {1, []byte{0x80}}, {1, []byte{0xff}}},
			[]string{"if value > 1:"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			row := fixedproof.RowCase{ID: tc.name, Schema: tc.schema, Root: "T", Body: tc.body, Count: 1}
			u := fixedproof.ParseCheckUnit(t, tc.schema)
			cpp, verify := fixedproof.CppOracle(t, row, u)
			files, err := pythontable.Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			dir := pythonFixedTree(t, files)
			if out, err := pyrun.Analyze(t, dir, "probe"); err != nil {
				t.Fatalf("mypy generated package: %s", out)
			}
			if out, err := pyrun.Analyze(t, dir, "probe/_schema"); err != nil {
				t.Fatalf("mypy runtime: %s", out)
			}
			setup := "from probe import T\nwith open('saved', 'wb') as f: f.write(T.fixed_save([T()]))\n"
			_, python := pythonFixedRun(t, files, setup, nil)
			if !bytes.Equal(python, cpp) {
				t.Fatal("default Python save differs from C++")
			}
			if err := verify(python); err != nil {
				t.Fatalf("C++ rejected Python: %v", err)
			}
			runner := pythonFixedProofRunner{}
			resaved, err := runner.LoadAndResave(t, row, u, cpp)
			if err != nil || !bytes.Equal(resaved, cpp) {
				t.Fatalf("Python load/resave differs: %v", err)
			}
			badWriter, err := runner.SaveMutated(t, row, u)
			if err == nil && (bytes.Equal(badWriter, cpp) || verify(badWriter) == nil) {
				t.Fatal("writer negative control did not change the reference verdict")
			}
			probe := `from probe import T, Refused
try:
    T.fixed_load(open("wire", "rb").read(), 1)
except Refused:
    print("refused")
except Exception as err:
    print("crash " + type(err).__name__)
else:
    print("accepted")
`
			mutated := make(map[string][]byte, len(files))
			for k, v := range files {
				mutated[k] = bytes.Clone(v)
			}
			const path = "probe/_schema/fixedform.py"
			src := string(mutated[path])
			for _, anchor := range tc.anchors {
				if strings.Count(src, anchor) != 1 {
					t.Fatalf("control anchor %q occurs %d times", anchor, strings.Count(src, anchor))
				}
				src = strings.Replace(src, anchor, "if False:", 1)
			}
			mutated[path] = []byte(src)
			// The writer's bounds and ordinal counts also govern an older file
			// read through a compiled plan, even when the reader admits more.
			if tc.name == "scalar-bounds" || tc.name == "ordinals" {
				t.Run("compiled-writer", func(t *testing.T) {
					newSchema := tc.schema
					if tc.name == "scalar-bounds" {
						newSchema = strings.Replace(newSchema, "min = -5, max = 5", "min = -50, max = 50", 1)
						newSchema = strings.Replace(newSchema, "min = -500, max = 500", "min = -1000, max = 1000", 1)
						newSchema = strings.Replace(newSchema, "min = 10, max = 1000", "min = 0, max = 2000", 1)
					} else {
						newSchema = strings.Replace(newSchema, "Gold, Silver }", "Gold, Silver, Bronze }", 1)
						newSchema = strings.Replace(newSchema, " b int32\n}", " b int32\n c int32\n}", 1)
					}
					newer := fixedproof.ParseCheckUnit(t, newSchema)
					entry, ok := fixedproof.LineageEntryOf(u, "T")
					if !ok {
						t.Fatal("old writer has no fixed lineage")
					}
					lineage := map[string][]fixedproof.LineageEntry{"T": {entry}}
					if n, reason, _, _, _ := runner.LoadLineage(t, newer, "T", lineage, cpp, 1); n != 1 {
						t.Fatalf("compiled plan refuses clean writer: %s", reason)
					}
					for _, c := range tc.corruptions {
						wire := bytes.Clone(cpp)
						copy(wire[len(wire)-int(tc.body)+c.offset:], c.value)
						if n, _, _, _, _ := runner.LoadLineage(t, newer, "T", lineage, wire, 1); n >= 0 {
							t.Errorf("compiled plan accepted invalid writer value at %d", c.offset)
						}
					}
				})
			}
			for _, c := range tc.corruptions {
				wire := bytes.Clone(cpp)
				at := len(wire) - int(tc.body) + c.offset
				copy(wire[at:], c.value)
				out, _ := pythonFixedRun(t, files, probe, map[string][]byte{"wire": wire})
				if strings.TrimSpace(out) != "refused" {
					t.Errorf("offset %d: invalid file was not refused: %q", c.offset, out)
				}
				control, _ := pythonFixedRun(t, mutated, probe, map[string][]byte{"wire": wire})
				if strings.TrimSpace(control) == "refused" {
					t.Errorf("offset %d: negative control still refuses", c.offset)
				}
			}
		})
	}
}

// TestPythonFixedProofValuesFloatBounds checks compressed float range and
// non-finite wire values, including a control that removes both validations.
func TestPythonFixedProofValuesFloatBounds(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "pinned Python and the C++ fixed oracle")
	var row fixedproof.RowCase
	for _, tc := range fixedproof.Cases() {
		if tc.ID == "compressed-floats" {
			row = tc
			break
		}
	}
	if row.ID == "" {
		t.Fatal("compressed-floats proof row is missing")
	}
	u := fixedproof.ParseCheckUnit(t, row.Schema)
	cpp, _ := fixedproof.CppOracle(t, row, u)
	files, err := pythontable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	const path = "probe/_schema/fixedform.py"
	const finite = "if raw & exponent == exponent:"
	const bounded = "if spec.get(\"ranged\", False) and not spec[\"lo\"] <= value <= spec[\"hi\"]:"
	src := string(files[path])
	if strings.Count(src, finite) != 1 || strings.Count(src, bounded) != 1 {
		t.Fatal("float control anchors changed")
	}
	src = strings.Replace(src, finite, "if False:", 1)
	src = strings.Replace(src, bounded, "if False:", 1)
	altered := make(map[string][]byte, len(files))
	for k, v := range files {
		altered[k] = bytes.Clone(v)
	}
	altered[path] = []byte(src)
	probe := `from probe import T, Refused
try:
    with open("wire", "rb") as f:
        T.fixed_load(f.read(), 3)
except Refused:
    print("refused")
except Exception as err:
    print("crash " + type(err).__name__)
else:
    print("accepted")
`
	for _, bits := range []uint32{0x40000000, 0x7f800000, 0x7fc00000} {
		wire := bytes.Clone(cpp)
		binary.LittleEndian.PutUint32(wire[len(wire)-int(row.Body):], bits)
		out, _ := pythonFixedRun(t, files, probe, map[string][]byte{"wire": wire})
		if strings.TrimSpace(out) != "refused" {
			t.Errorf("float bits %08x: wanted refusal, got %q", bits, out)
		}
		control, _ := pythonFixedRun(t, altered, probe, map[string][]byte{"wire": wire})
		if strings.TrimSpace(control) == "refused" {
			t.Errorf("float bits %08x: control still refuses", bits)
		}
	}
}
