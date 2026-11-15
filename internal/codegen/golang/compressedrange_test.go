package golang

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestGoPacketWriteRefusesOffRangeCompressedFloat pins SPEC §5.1 row 10 on the
// generated packet Write: a compressed float that is NaN, an infinity, or
// finite and outside [min, max] is refused before the fold, on the flat run and
// the per-field path alike. The shared cases in test/packet-write-cfloat hold
// the verdicts; the generated-text half needs no toolchain.
func TestGoPacketWriteRefusesOffRangeCompressedFloat(t *testing.T) {
	t.Parallel()
	field := cfloatField(t)
	src := "package t\n\n" +
		"type Single\n{\n    " + field + "\n}\n\n" +
		"type Flat\n{\n    a uint8\n    " + field + "\n}\n"
	files := generateGo(t, "CFloatRange", src)

	for _, decl := range []string{"func WriteSingle(", "func WriteFlat("} {
		body := funcBody(t, files, decl)
		before, _, ok := strings.Cut(body, "normalizedValue :=")
		if !ok {
			t.Errorf("%s does not fold the compressed float\n%s", decl, body)
			continue
		}
		guard := before
		for _, refuse := range []string{"return serialize.ErrNonFiniteFloat", "return serialize.ErrValueOutOfRange"} {
			if !strings.Contains(guard, refuse) {
				t.Errorf("%s does not refuse before the fold with %q\n%s", decl, refuse, body)
			}
		}
	}

	t.Run("probe", func(t *testing.T) {
		t.Parallel()
		runCompressedRangeProbe(t, files)
	})
}

// cfloatField returns the compressed-float field line from the shared fixture,
// so the in-test types carry the same declaration its cases describe.
func cfloatField(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../../test/packet-write-cfloat/CFloat.schema")
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.Contains(line, "float32") {
			return strings.TrimSpace(line)
		}
	}
	t.Fatal("test/packet-write-cfloat/CFloat.schema holds no float32 field")
	return ""
}

type cfloatCase struct {
	name, value string
	refused     bool
	wantBits    uint32
}

// cfloatCases reads the shared case list, so every verdict it names runs.
func cfloatCases(t *testing.T) []cfloatCase {
	t.Helper()
	data, err := os.ReadFile("../../../test/packet-write-cfloat/cases.txt")
	if err != nil {
		t.Fatal(err)
	}
	var out []cfloatCase
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 3 {
			t.Fatalf("cases.txt: %q is not <name> <value> <verdict>", line)
		}
		c := cfloatCase{name: f[0], value: f[1], refused: f[2] == "refused"}
		if c.refused {
			out = append(out, c)
			continue
		}
		readBack := f[1]
		if len(f) >= 5 && f[3] == "reads-back" {
			readBack = f[4]
		}
		v, err := strconv.ParseFloat(readBack, 64)
		if err != nil {
			t.Fatalf("cases.txt: %q: reads-back value %q: %v", line, readBack, err)
		}
		c.wantBits = math.Float32bits(float32(v))
		out = append(out, c)
	}
	if len(out) == 0 {
		t.Fatal("cases.txt holds no cases")
	}
	return out
}

func cfloatValueExpr(v string) string {
	switch v {
	case "NaN":
		return "float32(math.NaN())"
	case "+Inf":
		return "float32(math.Inf(1))"
	case "-Inf":
		return "float32(math.Inf(-1))"
	case "-0.0":
		return "float32(math.Copysign(0, -1))"
	}
	return "float32(" + v + ")"
}

// runCompressedRangeProbe compiles the generated Go against serialize.go and
// writes every shared case through both paths, requiring each verdict.
func runCompressedRangeProbe(t *testing.T, files map[string][]byte) {
	t.Helper()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated writer)")

	var cases strings.Builder
	for _, c := range cfloatCases(t) {
		fmt.Fprintf(&cases, "\t{%q, %s, %t, 0x%08x},\n",
			c.name, cfloatValueExpr(c.value), c.refused, c.wantBits)
	}
	probe := fmt.Sprintf(`package t

import (
	"math"
	"testing"

	"github.com/mas-bandwidth/serialize.go/v2"
)

type cfloatCase struct {
	name    string
	value   float32
	refused bool
	wantBits uint32
}

var cfloatCases = []cfloatCase{
%s}

func TestPacketWriteCompressedFloatRange(t *testing.T) {
	for _, c := range cfloatCases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Run("single", func(t *testing.T) {
				stream := serialize.NewWriteStream(make([]byte, 4096))
				v := Single{X: c.value}
				err := WriteSingle(stream, &v)
				stream.Flush()
				if c.refused {
					if err == nil {
						t.Fatalf("WriteSingle(%%v) succeeded, want a refusal", c.value)
					}
					return
				}
				if err != nil {
					t.Fatalf("WriteSingle(%%v) refused: %%v", c.value, err)
				}
				if err := stream.Err(); err != nil {
					t.Fatalf("WriteSingle(%%v): stream error %%v", c.value, err)
				}
				var got Single
				if err := ReadSingle(serialize.NewReadStream(stream.Data()), &got); err != nil {
					t.Fatalf("ReadSingle after WriteSingle(%%v): %%v", c.value, err)
				}
				if bits := math.Float32bits(got.X); bits != c.wantBits {
					t.Fatalf("WriteSingle(%%v) read back %%08x, want %%08x", c.value, bits, c.wantBits)
				}
			})
			t.Run("flat", func(t *testing.T) {
				stream := serialize.NewWriteStream(make([]byte, 4096))
				v := Flat{A: 0xa5, X: c.value}
				err := WriteFlat(stream, &v)
				stream.Flush()
				if c.refused {
					if err == nil {
						t.Fatalf("WriteFlat(%%v) succeeded, want a refusal", c.value)
					}
					return
				}
				if err != nil {
					t.Fatalf("WriteFlat(%%v) refused: %%v", c.value, err)
				}
				if err := stream.Err(); err != nil {
					t.Fatalf("WriteFlat(%%v): stream error %%v", c.value, err)
				}
				var got Flat
				if err := ReadFlat(serialize.NewReadStream(stream.Data()), &got); err != nil {
					t.Fatalf("ReadFlat after WriteFlat(%%v): %%v", c.value, err)
				}
				if bits := math.Float32bits(got.X); bits != c.wantBits {
					t.Fatalf("WriteFlat(%%v) read back %%08x, want %%08x", c.value, bits, c.wantBits)
				}
			})
		})
	}
}
`, cases.String())
	runtime := slowtest.SerializeGo(t, "../../../")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	files["compressedrange_test.go"] = []byte(probe)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the generated writer failed its compressed-range probe: %v\n%s", err, out)
	}
}
