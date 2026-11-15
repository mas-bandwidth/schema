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

// generateBitsWidthGo checks and generates a unit for the bits(N) write tests.
func generateBitsWidthGo(t *testing.T, src string) map[string][]byte {
	t.Helper()
	f, perrs := parser.Parse("Wide.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Wide.schema", Name: "Wide.schema", Base: "Wide",
		Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestGoPacketWriteRefusesWideBits pins SPEC §5.1 contract row 2 on the Go
// packet writer: a bits(N) value wider than N bits is refused with an error
// before any bit is written, on the flat-run path and the per-field path
// alike, and a bits(N) field whose storage holds exactly N bits carries no
// test, since every stored value fits.
func TestGoPacketWriteRefusesWideBits(t *testing.T) {
	t.Parallel()
	const src = "package t\n\n" +
		"type FlatBits\n{\n    a uint8\n    b bits(7)\n}\n\n" +
		"type OneBits\n{\n    b bits(7)\n}\n\n" +
		"type FullBits\n{\n    a uint8\n    b bits(32)\n}\n"
	files := generateBitsWidthGo(t, src)
	for _, decl := range []string{"func WriteFlatBits(", "func WriteOneBits("} {
		body := funcBody(t, files, decl)
		guard := strings.Index(body, "if value.B > 127 {")
		refuse := strings.Index(body, "return serialize.ErrValueOutOfRange")
		write := strings.Index(body, "stream.Serialize")
		if guard < 0 || refuse < guard || write < 0 || write < refuse {
			t.Errorf("%s does not refuse a bits(7) value above 0x7f before writing\n%s", decl, body)
		}
	}
	if body := funcBody(t, files, "func WriteFullBits("); strings.Contains(body, "ErrValueOutOfRange") {
		t.Errorf("WriteFullBits guards a bits(32) field whose every stored value fits\n%s", body)
	}
	t.Run("probe", func(t *testing.T) {
		t.Parallel()
		runBitsWidthProbe(t, files)
	})
}

// runBitsWidthProbe compiles the generated Go and writes 200 and 127 into each
// bits(7) field: 200 is refused with an error and no panic, 127 round trips.
func runBitsWidthProbe(t *testing.T, files map[string][]byte) {
	t.Helper()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated writer)")
	const probe = `package t

import (
	"testing"

	"github.com/mas-bandwidth/serialize.go/v2"
)

func TestWideBitsAreRefused(t *testing.T) {
	buf := make([]byte, 64)
	if err := WriteFlatBits(serialize.NewWriteStream(buf), &FlatBits{A: 1, B: 200}); err == nil {
		t.Errorf("WriteFlatBits of b = 200 in bits(7) returned nil")
	}
	if err := WriteOneBits(serialize.NewWriteStream(buf), &OneBits{B: 200}); err == nil {
		t.Errorf("WriteOneBits of b = 200 in bits(7) returned nil")
	}

	w := serialize.NewWriteStream(buf)
	if err := WriteFlatBits(w, &FlatBits{A: 1, B: 127}); err != nil {
		t.Fatalf("WriteFlatBits of b = 127 refused: %v", err)
	}
	w.Flush()
	var f FlatBits
	if err := ReadFlatBits(serialize.NewReadStream(w.Data()), &f); err != nil || f.B != 127 {
		t.Errorf("ReadFlatBits: b = %d, err = %v, want 127", f.B, err)
	}

	w = serialize.NewWriteStream(buf)
	if err := WriteOneBits(w, &OneBits{B: 127}); err != nil {
		t.Fatalf("WriteOneBits of b = 127 refused: %v", err)
	}
	w.Flush()
	var o OneBits
	if err := ReadOneBits(serialize.NewReadStream(w.Data()), &o); err != nil || o.B != 127 {
		t.Errorf("ReadOneBits: b = %d, err = %v, want 127", o.B, err)
	}
}
`
	runtime := slowtest.SerializeGo(t, "../../../")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	files["bitswidth_test.go"] = []byte(probe)
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
