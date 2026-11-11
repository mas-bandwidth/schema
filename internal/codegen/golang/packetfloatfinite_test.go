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

// TestGoPacketWriteRefusesNonFiniteFloat pins SPEC §5.1 contract row 7 on the
// generated per-field write: a plain float32 or float64 that is not in a flat
// run is tested on its raw bits and refused with serialize.ErrNonFiniteFloat
// before the runtime's SerializeFloat32/64 is called, so a NaN or an infinity
// is an error in every build, as on the flat path; negative zero and the
// subnormals still ride bit exact.
func TestGoPacketWriteRefusesNonFiniteFloat(t *testing.T) {
	t.Parallel()
	const src = "package t\n\n" +
		"type Lone32\n{\n    x float32\n}\n\n" +
		"type Lone64\n{\n    x float64\n}\n"
	files := generateGo(t, "LoneFloat", src)
	for _, c := range []struct{ decl, call string }{
		{"func WriteLone32(", "stream.SerializeFloat32("},
		{"func WriteLone64(", "stream.SerializeFloat64("},
	} {
		body := funcBody(t, files, c.decl)
		call := strings.Index(body, c.call)
		if call < 0 {
			t.Errorf("%s does not write its float with %s\n%s", c.decl, c.call, body)
			continue
		}
		if !strings.Contains(body[:call], "return serialize.ErrNonFiniteFloat") {
			t.Errorf("%s reaches %s without refusing a non-finite value first\n%s", c.decl, c.call, body)
		}
	}
	t.Run("probe", func(t *testing.T) {
		t.Parallel()
		runPacketFloatFiniteProbe(t, files)
	})
}

// runPacketFloatFiniteProbe compiles the generated Go against serialize.go and
// writes each lone float: the non-finite values are refused with an error and
// no panic, and the edge finite values round trip bit exact.
func runPacketFloatFiniteProbe(t *testing.T, files map[string][]byte) {
	t.Helper()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated writer)")
	const probe = `package t

import (
	"math"
	"testing"

	"github.com/mas-bandwidth/serialize.go/v2"
)

func write32(t *testing.T, x float32) (out []byte, err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("WriteLone32(%08x) panicked: %v", math.Float32bits(x), r)
		}
	}()
	buf := make([]byte, 64)
	s := serialize.NewWriteStream(buf)
	v := Lone32{X: x}
	err = WriteLone32(s, &v)
	s.Flush()
	return s.Data(), err
}

func write64(t *testing.T, x float64) (out []byte, err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("WriteLone64(%016x) panicked: %v", math.Float64bits(x), r)
		}
	}()
	buf := make([]byte, 64)
	s := serialize.NewWriteStream(buf)
	v := Lone64{X: x}
	err = WriteLone64(s, &v)
	s.Flush()
	return s.Data(), err
}

func TestNonFiniteWriteIsRefused(t *testing.T) {
	for _, bits := range []uint32{0x7fc00000, 0x7f800000, 0xff800000} {
		if _, err := write32(t, math.Float32frombits(bits)); err == nil {
			t.Errorf("WriteLone32(%08x) succeeded, want a refusal", bits)
		}
	}
	for _, bits := range []uint64{0x7ff8000000000000, 0x7ff0000000000000, 0xfff0000000000000} {
		if _, err := write64(t, math.Float64frombits(bits)); err == nil {
			t.Errorf("WriteLone64(%016x) succeeded, want a refusal", bits)
		}
	}
}

func TestFiniteEdgesRoundTrip(t *testing.T) {
	for _, bits := range []uint32{0x80000000, 0x00000001, 0x7f7fffff} {
		data, err := write32(t, math.Float32frombits(bits))
		if err != nil {
			t.Errorf("WriteLone32(%08x) refused: %v", bits, err)
			continue
		}
		var v Lone32
		if err := ReadLone32(serialize.NewReadStream(data), &v); err != nil {
			t.Errorf("ReadLone32(%08x) refused: %v", bits, err)
		} else if got := math.Float32bits(v.X); got != bits {
			t.Errorf("Lone32 round trip: got %08x, want %08x", got, bits)
		}
	}
	for _, bits := range []uint64{0x8000000000000000, 0x0000000000000001, 0x7fefffffffffffff} {
		data, err := write64(t, math.Float64frombits(bits))
		if err != nil {
			t.Errorf("WriteLone64(%016x) refused: %v", bits, err)
			continue
		}
		var v Lone64
		if err := ReadLone64(serialize.NewReadStream(data), &v); err != nil {
			t.Errorf("ReadLone64(%016x) refused: %v", bits, err)
		} else if got := math.Float64bits(v.X); got != bits {
			t.Errorf("Lone64 round trip: got %016x, want %016x", got, bits)
		}
	}
}
`
	runtime := slowtest.SerializeGo(t, "../../../")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	files["packetfloat_test.go"] = []byte(probe)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the generated writer failed its probe: %v\n%s", err, out)
	}
}
