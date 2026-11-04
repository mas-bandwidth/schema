package lobstertable_test

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// Decimal -> binary64 -> binary32 can double-round a shortest candidate.
// Compare values and canonical precision-loop spellings against Go's
// formatter and directly rounded binary32 parser (SPEC-TABLES W12).
func TestLobsterJSONFloat32WriterSingleRounding(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "Root.schema")
	if err := os.WriteFile(path, []byte("package floatwriterprobe\ntable Root { value float32 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	var program strings.Builder
	program.WriteString("import .json\n")
	bits := []uint32{
		0x15AE43FC, 0x15AE43FD, 0x15AE43FE, 0x15AE43FF,
		0x95AE43FC, 0x95AE43FD, 0x95AE43FE, 0x95AE43FF,
		0, 0x80000000, 1, 0x80000001, 0x007FFFFF, 0x00800000,
		0x7F7FFFFF, 0xFF7FFFFF, 0x3F800000, 0xBF800000,
	}
	canonical := func(b uint32) string {
		f := float64(math.Float32frombits(b))
		for precision := 6; precision <= 9; precision++ {
			text := strconv.FormatFloat(f, 'g', precision, 32)
			back, err := strconv.ParseFloat(text, 32)
			if err != nil {
				t.Fatal(err)
			}
			if precision == 9 || math.Float32bits(float32(back)) == b {
				return text
			}
		}
		panic("unreachable")
	}
	for _, b := range bits {
		fmt.Fprintf(&program, "print(schema_rt.j_float_text(schema_rt.j_f32frombits(0x%08x), true))\n", b)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program.String()), 0644); err != nil {
		t.Fatal(err)
	}
	mismatches := func() int {
		out := strings.Fields(string(lobsterrun.Run(t, g.dir, "main.lobster", nil)))
		if len(out) != len(bits) {
			t.Fatalf("got %d outputs: %v", len(out), out)
		}
		bad := 0
		for i, text := range out {
			f, err := strconv.ParseFloat(text, 32)
			if err != nil {
				t.Fatal(err)
			}
			got := math.Float32bits(float32(f))
			wantText := canonical(bits[i])
			if text != wantText {
				bad++
				t.Logf("%08x writes %s, canonical spelling is %s", bits[i], text, wantText)
			}
			if got != bits[i] {
				bad++
				t.Logf("%08x writes %s, reads %08x", bits[i], text, got)
			}
		}
		return bad
	}
	if n := mismatches(); n != 0 {
		t.Fatalf("writer produced %d float32 value or spelling mismatches", n)
	}
	runtime := filepath.Join(g.dir, "json.lobster")
	raw, err := os.ReadFile(runtime)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "if j_parse_f32(text) == x:"
	if strings.Count(string(raw), anchor) != 1 {
		t.Fatal("single-rounding anchor must occur once")
	}
	patched := strings.Replace(string(raw), anchor, "if fround(back) == x:", 1)
	if err := os.WriteFile(runtime, []byte(patched), 0644); err != nil {
		t.Fatal(err)
	}
	if mismatches() == 0 {
		t.Fatal("double-rounding control escaped value comparison")
	}
}
