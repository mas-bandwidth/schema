package gotable

// TestFixedRoadmapGoT14CompressedFloats is go/R30: a compressed float rides as
// the float on this leg (docs/SPEC.md Floating-point discipline,
// docs/FIXED-FORM-ALGORITHM.md §5.1, docs/SPEC-TABLES.md §3.4).
//
// The fixture where math.FMA differs from the unfused product-then-add is
// dequantize of index 2 under min=-1, max=2, resolution=0.1 (count=30,
// delta=float32(3)): unfused bits 0xbf4ccccc, fused bits 0xbf4ccccd.

import (
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const t14Old = `package cfold

fixed table Reading
{
    lead uint32 = 1
    aim float32 | min = -1, max = 2, resolution = 0.1
    trail uint32 = 2
}
`

const t14Fine = `package cfold

fixed table Reading
{
    lead uint32 = 1
    aim float32 | min = -1, max = 2, resolution = 0.01
    trail uint32 = 2
}
`

const t14Dropped = `package cfold

fixed table Reading
{
    lead uint32 = 1
    aim float32
    trail uint32 = 2
}
`

func TestFixedRoadmapGoT14CompressedFloats(t *testing.T) {
	t.Parallel()
	t.Run("go/R30", func(t *testing.T) {
		t.Parallel()
		slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
		t14CiteCoarser(t)
		src := tableFile(generate(t, t14Old))
		write := funcSource(src, "func ReadingFixedWriteBody(")
		if !strings.Contains(write, "tableFixedPutF32(") {
			t.Fatal("the fixed write does not store the float32 with tableFixedPutF32")
		}
		if strings.Contains(write, "tableMessageWriteScalar") {
			t.Fatal("the fixed write quantizes the float instead of storing it")
		}
		dir := t.TempDir()
		file := t14Write(t, dir, t14Old)
		t14Read(t, t14Old, nil, file)
		t14Read(t, t14Fine, []string{t14Old}, file)
		t14Read(t, t14Dropped, []string{t14Old}, file)
		t14Contract(t)
	})
}

// t14CiteCoarser cites the lock's refusal rather than re-proving it. The
// phrase and the test live in internal/lockfile, outside this card's PATHS.
func t14CiteCoarser(t *testing.T) {
	t.Helper()
	b, err := os.ReadFile("../../lockfile/cfloatrange_test.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, needle := range []string{
		"func TestLockCFloatResolutionCoarsenedRefuses",
		"func TestLockCFloatResolutionRefinedAllowed",
		"func TestLockCFloatRangeDroppedAllowed",
		"resolution coarsened",
	} {
		if !strings.Contains(s, needle) {
			t.Fatalf("the lockfile citation is gone: %s", needle)
		}
	}
}

func t14Write(t *testing.T, dir, schema string) string {
	t.Helper()
	out := dir + "/old.bin"
	body := `
	var r Reading
	ReadingReset(&r)
	r.Lead = 1
	r.Aim = math.Float32frombits(0x3E99999A)
	r.Trail = 2
	buf := make([]byte, ReadingFixedMeasure(1))
	if ReadingFixedSave([]Reading{r}, buf) < 0 {
		t.Fatal("save")
	}
	raw := []byte{0x9a, 0x99, 0x99, 0x3e}
	if !bytes.Contains(buf, raw) {
		t.Fatalf("the file does not carry the float32 bits %x", buf)
	}
	if err := os.WriteFile(out, buf, 0o600); err != nil {
		t.Fatal(err)
	}`
	src := "package probe\n\nimport (\"bytes\"; \"math\"; \"os\"; \"testing\")\n\nfunc TestWrite(t *testing.T) {\n\tout := " + quote(out) + "\n\t_ = out\n" + body + "\n}\n"
	if o, err := runVersionProbe(t, schema, nil, src); err != nil {
		t.Fatalf("writer: %v\n%s", err, o)
	}
	return out
}

func t14Read(t *testing.T, schema string, older []string, file string) {
	t.Helper()
	body := `
	data, err := os.ReadFile(` + quote(file) + `)
	if err != nil {
		t.Fatal(err)
	}
	var got [1]Reading
	var plan [64]TableFixedEntry
	var report TableReport
	if n := ReadingFixedLoad(got[:], data, plan[:], &report); n != 1 {
		t.Fatalf("reader refused a lawful old file: n=%d why=%q", n, report.Reason)
	}
	if b := math.Float32bits(got[0].Aim); b != 0x3E99999A {
		t.Fatalf("aim moved: %#x", b)
	}
	if got[0].Lead != 1 || got[0].Trail != 2 {
		t.Fatalf("a neighbour moved: %+v", got[0])
	}
	if report.Clamped != 0 || report.Malformed || report.Reason != "" {
		t.Fatalf("a clean widen is not a refusal: %+v", report)
	}`
	src := "package probe\n\nimport (\"math\"; \"os\"; \"testing\")\n\nfunc TestRead(t *testing.T) {\n" + body + "\n}\n"
	if o, err := runVersionProbe(t, schema, older, src); err != nil {
		t.Fatalf("reader: %v\n%s", err, o)
	}
}

// t14Contract runs the emitted dequantize (tableMessageReadScalar, packing 2)
// on the fixture and insists the bits are the unfused product-then-add, not
// math.FMA's fused result.
func t14Contract(t *testing.T) {
	t.Helper()
	src := `package probe

import ("encoding/binary"; "testing")

func TestContract(t *testing.T) {
	// min=-1 max=2 res=0.1 count=30 index=2: unfused 0xbf4ccccc, fused 0xbf4ccccd.
	buf := make([]byte, 8)
	w := TableBitWriter{Buffer: buf}
	w.Put(2, 5)
	r := TableBitReader{Buffer: buf}
	s := TableMessageShape{Kind: 10, Packing: 2, Bits: 5, QMin: -1, QDelta: 3, QCount: 30}
	var out [16]byte
	if !tableMessageReadScalar(&r, s, &out) {
		t.Fatal("read")
	}
	bits := binary.LittleEndian.Uint32(out[:4])
	if bits != 0xbf4ccccc {
		t.Fatalf("dequantize bits %#x, want unfused 0xbf4ccccc (fused would be 0xbf4ccccd)", bits)
	}
}
`
	if o, err := runVersionProbe(t, t14Old, nil, src); err != nil {
		t.Fatalf("fp-contract: %v\n%s", err, o)
	}
}

func quote(s string) string {
	return "`" + s + "`"
}
