package golang

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// The Edge triple of security/compressed-float-top-step-inf: min = 3*2^103,
// max = the largest float32, resolution 1e36 (341 steps, 9 bits). Every
// operand is finite, and the top step, float32(1*delta) + min, rounds to +Inf.
const (
	edgeMin = 30423614405477505635920876929024.0
	edgeMax = 340282346638528859811704183484516925440.0
	edgeRes = 1e36
)

// generateEdgeGo checks a unit with a harmless triple on every `x`, then sets
// the Edge triple on the IR the checker produced, so the generator sees a
// declaration the checker refuses: the reader's own floor is what is tested.
func generateEdgeGo(t *testing.T, src string) map[string][]byte {
	t.Helper()
	f, perrs := parser.Parse("Edge.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Edge.schema", Name: "Edge.schema", Base: "Edge",
		Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	for _, s := range u.Structs {
		for _, fd := range s.Fields {
			if fd.Name == "x" {
				fd.FMin, fd.FMax, fd.Resolution = edgeMin, edgeMax, edgeRes
			}
		}
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestGoCompressedFloatReadIsFinite pins SPEC §4.3's finite rule on the
// generated compressed-float read: where the top step dequantizes past the
// largest float32, the per-field and the flat read refuse the non-finite value
// with serialize.ErrNonFiniteFloat, as serialize.go's own read does; where it
// cannot, no guard is emitted.
func TestGoCompressedFloatReadIsFinite(t *testing.T) {
	t.Parallel()
	const src = "package t\n\n" +
		"type Edge\n{\n    x float32 | min = 0.0, max = 1000.0, resolution = 1.0\n}\n\n" +
		"type FlatEdge\n{\n    a uint8\n    x float32 | min = 0.0, max = 1000.0, resolution = 1.0\n}\n\n" +
		"type Fine\n{\n    y float32 | min = 0.0, max = 1000.0, resolution = 1.0\n}\n"
	files := generateEdgeGo(t, src)
	for _, decl := range []string{"func ReadEdge(", "func ReadFlatEdge("} {
		body := funcBody(t, files, decl)
		if !strings.Contains(body, "return serialize.ErrNonFiniteFloat") {
			t.Errorf("%s does not refuse a non-finite dequantized value\n%s", decl, body)
		}
	}
	if body := funcBody(t, files, "func ReadFine("); strings.Contains(body, "ErrNonFiniteFloat") {
		t.Errorf("ReadFine guards a declaration whose every step is finite\n%s", body)
	}
	t.Run("probe", func(t *testing.T) {
		t.Parallel()
		runCompressedFiniteProbe(t, files)
	})
}

// runCompressedFiniteProbe compiles the generated Go and reads the top step:
// 0x55 0x01 is index 341 in 9 bits, the wire WriteEdge emits for x = max.
func runCompressedFiniteProbe(t *testing.T, files map[string][]byte) {
	t.Helper()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated reader)")
	const probe = `package t

import (
	"errors"
	"testing"

	"github.com/mas-bandwidth/serialize.go/v2"
)

func TestTopStepIsRefused(t *testing.T) {
	var e Edge
	if err := ReadEdge(serialize.NewReadStream([]byte{0x55, 0x01}), &e); !errors.Is(err, serialize.ErrNonFiniteFloat) {
		t.Errorf("ReadEdge of the top step: x = %v, err = %v, want ErrNonFiniteFloat", e.X, err)
	}
	var f FlatEdge
	if err := ReadFlatEdge(serialize.NewReadStream([]byte{0x07, 0x55, 0x01}), &f); !errors.Is(err, serialize.ErrNonFiniteFloat) {
		t.Errorf("ReadFlatEdge of the top step: x = %v, err = %v, want ErrNonFiniteFloat", f.X, err)
	}
	if err := ReadEdge(serialize.NewReadStream([]byte{0x54, 0x01}), &e); err != nil {
		t.Errorf("ReadEdge of the step below the top refused: %v", err)
	}
}
`
	runtime := slowtest.SerializeGo(t, "../../../")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	files["finite_test.go"] = []byte(probe)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the generated reader failed its probe: %v\n%s", err, out)
	}
}
