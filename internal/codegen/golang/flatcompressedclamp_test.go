package golang

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestGoFlatCompressedFloatClampsTopStep pins PACKET-WIRE-ALGORITHM §2.5's
// integer clamp on both generated write paths. A flat run and the per-field
// form must quantize a value at the declared max to step index `steps`: once
// the step count reaches 2^23 the float32 ulp is 1, so n*steps + 0.5 at n = 1
// rounds to steps+1, and when the step count fills its wire width the mask
// would carry that to zero. The generated-text half needs no toolchain; the
// probe half compiles the generated code against the serialize.go runtime.
func TestGoFlatCompressedFloatClampsTopStep(t *testing.T) {
	t.Parallel()
	const src = "package t\n\n" +
		"type SingleA\n{\n    x float32 | min = 0.0, max = 8388609.0, resolution = 1.0\n}\n\n" +
		"type FlatA\n{\n    a uint8\n    x float32 | min = 0.0, max = 8388609.0, resolution = 1.0\n}\n\n" +
		"type SingleB\n{\n    x float32 | min = 0.0, max = 16777215.0, resolution = 1.0\n}\n\n" +
		"type FlatB\n{\n    a uint8\n    x float32 | min = 0.0, max = 16777215.0, resolution = 1.0\n}\n"
	files := generateGo(t, "Clamp", src)

	shapes := []struct {
		name  string
		steps uint64
		decls []string
	}{
		{"8388609", 8388609, []string{"func WriteSingleA(", "func WriteFlatA("}},
		{"16777215", 16777215, []string{"func WriteSingleB(", "func WriteFlatB("}},
	}
	for _, shape := range shapes {
		clamp := fmt.Sprintf("if integerValue > %d { integerValue = %d }", shape.steps, shape.steps)
		for _, decl := range shape.decls {
			body := funcBody(t, files, decl)
			if !strings.Contains(strings.Join(strings.Fields(body), " "), clamp) {
				t.Errorf("%s does not clamp its quantized step to %d: want %q\n%s",
					strings.TrimSuffix(strings.TrimPrefix(decl, "func "), "("), shape.steps, clamp, body)
			}
		}
	}

	t.Run("probe", func(t *testing.T) {
		t.Parallel()
		runFlatCompressedClampProbe(t, files)
	})
}

// runFlatCompressedClampProbe compiles the generated Go and writes each type
// with its field at the declared max. The flat run and the per-field form must
// both carry step index `steps` on the wire, and the value must read back as
// max. Behind slowtest.Gate — it shells out to the Go toolchain.
func runFlatCompressedClampProbe(t *testing.T, files map[string][]byte) {
	t.Helper()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated writer)")

	const probe = `package t

import (
	"bytes"
	"testing"

	"github.com/mas-bandwidth/serialize.go/v2"
)

func encodeWire(t *testing.T, write func(*serialize.WriteStream) error) []byte {
	t.Helper()
	stream := serialize.NewWriteStream(make([]byte, 4096))
	if err := write(stream); err != nil {
		t.Fatalf("write: %v", err)
	}
	stream.Flush()
	if err := stream.Err(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	return stream.Data()
}

func TestFlatCompressedClampTopStep(t *testing.T) {
	aSteps := []byte{0x01, 0x00, 0x80} // 8388609 little-endian in 24 bits
	aSingle := encodeWire(t, func(s *serialize.WriteStream) error {
		return WriteSingleA(s, &SingleA{X: 8388609})
	})
	if !bytes.Equal(aSingle, aSteps) {
		t.Fatalf("WriteSingleA at max = % x, want % x", aSingle, aSteps)
	}
	aFlat := encodeWire(t, func(s *serialize.WriteStream) error {
		return WriteFlatA(s, &FlatA{A: 0xa5, X: 8388609})
	})
	if want := append([]byte{0xa5}, aSteps...); !bytes.Equal(aFlat, want) {
		t.Fatalf("WriteFlatA at max = % x, want % x", aFlat, want)
	}
	var as SingleA
	if err := ReadSingleA(serialize.NewReadStream(aSingle), &as); err != nil {
		t.Fatalf("ReadSingleA refused the top value: %v", err)
	}
	if as.X != 8388609 {
		t.Fatalf("ReadSingleA read back %v, want 8388609", as.X)
	}
	var af FlatA
	if err := ReadFlatA(serialize.NewReadStream(aFlat), &af); err != nil {
		t.Fatalf("ReadFlatA refused the top value: %v", err)
	}
	if af.X != 8388609 {
		t.Fatalf("ReadFlatA read back %v, want 8388609", af.X)
	}

	bSteps := []byte{0xff, 0xff, 0xff} // 2^24-1 little-endian in 24 bits
	bSingle := encodeWire(t, func(s *serialize.WriteStream) error {
		return WriteSingleB(s, &SingleB{X: 16777215})
	})
	if !bytes.Equal(bSingle, bSteps) {
		t.Fatalf("WriteSingleB at max = % x, want % x", bSingle, bSteps)
	}
	bFlat := encodeWire(t, func(s *serialize.WriteStream) error {
		return WriteFlatB(s, &FlatB{A: 0xa5, X: 16777215})
	})
	if want := append([]byte{0xa5}, bSteps...); !bytes.Equal(bFlat, want) {
		t.Fatalf("WriteFlatB at max = % x, want % x", bFlat, want)
	}
	var bs SingleB
	if err := ReadSingleB(serialize.NewReadStream(bSingle), &bs); err != nil {
		t.Fatalf("ReadSingleB refused the top value: %v", err)
	}
	if bs.X != 16777215 {
		t.Fatalf("ReadSingleB read back %v, want 16777215", bs.X)
	}
	var bf FlatB
	if err := ReadFlatB(serialize.NewReadStream(bFlat), &bf); err != nil {
		t.Fatalf("ReadFlatB refused the top value: %v", err)
	}
	if bf.X != 16777215 {
		t.Fatalf("ReadFlatB read back %v, want 16777215", bf.X)
	}
}
`
	runtime := slowtest.SerializeGo(t, "../../../")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	files["flatclamp_test.go"] = []byte(probe)

	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the generated writer failed its flat clamp probe: %v\n%s", err, out)
	}
}
