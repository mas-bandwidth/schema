package gotable

// I13 (docs/PORTING.md:1278) is the text differential against a third
// implementation: the leg emits random instances as (wire, text), the
// compiler's own engine (schema unpack, internal/tabletext, written from the
// spec and from neither backend) reads the same wire and writes its text, the
// two are byte-compared, then the other direction. Every cell of I13's matrix
// row is open at this head, so this is the Go leg's cell.
//
// The engine and the emitted walker share NO code even though both are Go: the
// engine's float loop is internal/tabletext/write.go's writeFloat/formatG
// (C's %.*g spelled out over the double), while the leg's is
// tableJsonWriteFloat, emitted SOURCE (tableJsonWalkSource) and reaching for
// strconv.AppendFloat. Two implementations, one language, which is what makes
// this row cheap (no subprocess protocol) and NOT circular.
//
// The pinned corpus is 92 instances, and a pinned corpus never reaches a float
// tie, so the random source is built here. Non-finite floats have no JSON
// spelling and both writers REFUSE rather than lose one, so the generator
// redraws until a pattern is finite. Instance 0 is the fixture proof:
// RootReset and nothing else. docs/PORTING.md's cell is owed, not edited.

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const textDifferentialSchema = `package probe
type Sample {
 s float32
 d float64
}
fixed table Root {
 a float32
 b float64
 pair Sample
 many [..8]float32
 deep [..8]float64
}
`

// textDifferentialProbe is a probe module (module probe) that generates N
// random float-only instances with the generated setters and a splitmix64
// written into the body, then writes each instance's wire and text. The seed,
// the count and the directory arrive through the environment so the outer test
// owns them. Instance 0 is RootReset alone; instances 1..N-1 fill every float
// slot, redrawing any non-finite 32- or 64-bit pattern.
const textDifferentialProbe = `package probe

import (
	"math"
	"os"
	"strconv"
	"testing"
)

type splitmix64 struct{ state uint64 }

func (r *splitmix64) next() uint64 {
	r.state += 0x9E3779B97F4A7C15
	z := r.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

func nextFloat32(r *splitmix64) float32 {
	for {
		bits := uint32(r.next())
		f := math.Float32frombits(bits)
		if !math.IsNaN(float64(f)) && !math.IsInf(float64(f), 0) {
			return f
		}
	}
}

func nextFloat64(r *splitmix64) float64 {
	for {
		f := math.Float64frombits(r.next())
		if !math.IsNaN(f) && !math.IsInf(f, 0) {
			return f
		}
	}
}

func TestProbe(t *testing.T) {
	dir := os.Getenv("SCHEMA_TEXT_DIFF_DIR")
	if dir == "" {
		t.Fatal("SCHEMA_TEXT_DIFF_DIR is unset")
	}
	seed, err := strconv.ParseUint(os.Getenv("SCHEMA_TEXT_DIFF_SEED"), 10, 64)
	if err != nil {
		t.Fatalf("SCHEMA_TEXT_DIFF_SEED: %v", err)
	}
	n, err := strconv.Atoi(os.Getenv("SCHEMA_TEXT_DIFF_N"))
	if err != nil {
		t.Fatalf("SCHEMA_TEXT_DIFF_N: %v", err)
	}
	r := &splitmix64{state: seed}
	for i := 0; i < n; i++ {
		var v Root
		RootReset(&v)
		if i != 0 {
			v.A = nextFloat32(r)
			v.B = nextFloat64(r)
			v.Pair.S = nextFloat32(r)
			v.Pair.D = nextFloat64(r)
			v.ManyCount = 8
			for j := 0; j < 8; j++ {
				v.Many[j] = nextFloat32(r)
			}
			v.DeepCount = 8
			for j := 0; j < 8; j++ {
				v.Deep[j] = nextFloat64(r)
			}
		}
		size := RootMeasure(&v)
		if size < 0 {
			t.Fatalf("instance %d: RootMeasure refused (%d)", i, size)
		}
		wire := make([]byte, size)
		if RootSave(&v, wire) != int64(len(wire)) {
			t.Fatalf("instance %d: RootSave wrote short", i)
		}
		if err := os.WriteFile(dir+"/inst-"+strconv.Itoa(i)+".wire", wire, 0o600); err != nil {
			t.Fatal(err)
		}
		textSize := RootToJsonMeasure(&v)
		if textSize < 0 {
			t.Fatalf("instance %d: RootToJsonMeasure refused (%d)", i, textSize)
		}
		text := make([]byte, textSize)
		if RootToJson(&v, text) != int64(len(text)) {
			t.Fatalf("instance %d: RootToJson wrote short", i)
		}
		if err := os.WriteFile(dir+"/inst-"+strconv.Itoa(i)+".json", text, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
`

const (
	textDifferentialSeed = 24845619678
	textDifferentialN    = 200
)

// runTextDifferential runs the probe (with an optional in-memory sabotage of
// the emitted files) and then the engine comparison in the main module. It
// returns the divergence messages and a non-nil error when any instance's leg
// text and engine text differ, so the negative control can assert on the same
// output the row itself produces.
func runTextDifferential(t *testing.T, edit func(map[string][]byte)) ([]byte, error) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SCHEMA_TEXT_DIFF_DIR", dir)
	t.Setenv("SCHEMA_TEXT_DIFF_SEED", strconv.Itoa(textDifferentialSeed))
	t.Setenv("SCHEMA_TEXT_DIFF_N", strconv.Itoa(textDifferentialN))
	out, err := runGeneratedEdited(t, textDifferentialSchema, textDifferentialProbe, edit)
	if err != nil {
		return out, err
	}
	u := unitFrom(t, textDifferentialSchema)
	m := tabletext.NewModel(u)
	msgs, err := compareTextDifferential(m, dir, textDifferentialN)
	if err != nil {
		return out, err
	}
	if len(msgs) > 0 {
		return []byte(strings.Join(msgs, "\n\n")), fmt.Errorf("%d instance(s) differ", len(msgs))
	}
	return out, nil
}

// TestTextDifferential is the row. It measures whether the emitted Go float
// writer and the engine's float writer agree, byte for byte, over 200 random
// float-only instances, in both directions.
func TestTextDifferential(t *testing.T) {
	out, err := runTextDifferential(t, nil)
	if err != nil {
		t.Fatalf("text differential: %v\n%s", err, out)
	}
}

// TestTextDifferentialNegativeControl caps a float32's text at SIX significant
// digits, so a float32 that needs seven, eight or nine renders short and only
// the cross-implementation comparison sees it. The gate must go red on the text
// comparison, not on a compile error.
func TestTextDifferentialNegativeControl(t *testing.T) {
	out, err := runTextDifferential(t, func(files map[string][]byte) {
		total := 0
		for _, data := range files {
			total += strings.Count(string(data), "bits, low, high = 32, 6, 9")
		}
		if total != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the sabotage anchor %q appeared %d times across the emitted files, want 1", "bits, low, high = 32, 6, 9", total)
		}
		patch(t, files,
			"bits, low, high = 32, 6, 9",
			"bits, low, high = 32, 6, 6 /* SABOTAGED */")
	})
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: a float32 writer capped at six significant digits left the gate GREEN\n%s", out)
	}
	if !bytes.Contains(out, []byte("leg text and engine text differ")) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the gate went red, but not on the text comparison\n%s", out)
	}
}

// compareTextDifferential runs both directions of the engine comparison for
// every instance the probe wrote. Direction one: the engine reads the wire and
// writes its text, byte-compared against the leg's text. Direction two: the
// engine packs the leg's text back to wire, byte-compared against the leg's
// wire. Divergences are collected as messages, not failures, so the negative
// control can assert on them.
func compareTextDifferential(m *tabletext.Model, dir string, n int) ([]string, error) {
	var msgs []string
	for i := range n {
		wire, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.wire", i)))
		if err != nil {
			return msgs, fmt.Errorf("read inst-%d.wire: %w", i, err)
		}
		legJSON, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.json", i)))
		if err != nil {
			return msgs, fmt.Errorf("read inst-%d.json: %w", i, err)
		}

		d1, err := os.MkdirTemp(dir, "d1-")
		if err != nil {
			return msgs, err
		}
		report, err := tablepack.UnpackOneFile(m, "Root", wire, d1)
		if err != nil {
			os.RemoveAll(d1)
			return msgs, fmt.Errorf("instance %d: UnpackOneFile: %w", i, err)
		}
		if report != (tabletext.Report{}) {
			os.RemoveAll(d1)
			return msgs, fmt.Errorf("instance %d: UnpackOneFile report not clean: %+v", i, report)
		}
		engineJSON, err := os.ReadFile(filepath.Join(d1, "Root.json"))
		os.RemoveAll(d1)
		if err != nil {
			return msgs, fmt.Errorf("instance %d: read engine Root.json: %w", i, err)
		}
		if !bytes.Equal(engineJSON, legJSON) {
			off, win := textWindow(legJSON, engineJSON)
			msgs = append(msgs, fmt.Sprintf("instance %d: leg text and engine text differ at byte %d\n%s\nfloats: %s", i, off, win, floatBitsOf(m, wire)))
			continue
		}

		d2, err := os.MkdirTemp(dir, "d2-")
		if err != nil {
			return msgs, err
		}
		if err := os.WriteFile(filepath.Join(d2, "Root.json"), legJSON, 0o600); err != nil {
			os.RemoveAll(d2)
			return msgs, err
		}
		wireBack, _, report2, err := tablepack.Pack(m, "Root", d2)
		os.RemoveAll(d2)
		if err != nil {
			return msgs, fmt.Errorf("instance %d: Pack: %w", i, err)
		}
		if report2 != (tabletext.Report{}) {
			return msgs, fmt.Errorf("instance %d: Pack report not clean: %+v", i, report2)
		}
		if !bytes.Equal(wireBack, wire) {
			msgs = append(msgs, fmt.Sprintf("instance %d: engine wire packed from leg text differs from leg wire: engine=%x leg=%x", i, wireBack, wire))
		}
	}
	return msgs, nil
}

// floatBitsOf decodes one wire with the engine and spells every float field's
// bits, so a divergence replays from the bits alone. Non-finite values cannot
// appear here: the probe redraws them and both writers refuse them.
func floatBitsOf(m *tabletext.Model, wire []byte) string {
	inst := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if _, err := tablewire.Decode(m, inst, wire, &report); err != nil {
		return fmt.Sprintf("<decode error: %v>", err)
	}
	var b strings.Builder
	var walk func(fields []tabletext.Field, prefix string)
	walk = func(fields []tabletext.Field, prefix string) {
		for i := range fields {
			f := &fields[i]
			name := prefix + f.Def.Name
			emit := func(c tabletext.Cell, slot string) {
				switch f.Def.Type.Kind {
				case ir.TFloat32:
					fmt.Fprintf(&b, "%s=0x%08x(f32) ", slot, math.Float32bits(float32(c.F)))
				case ir.TFloat64:
					fmt.Fprintf(&b, "%s=0x%016x(f64) ", slot, math.Float64bits(c.F))
				}
			}
			if len(f.Elems) > 0 {
				for j := 0; j < f.Count && j < len(f.Elems); j++ {
					c := f.Elems[j]
					if c.Tab != nil {
						walk(c.Tab.Fields, fmt.Sprintf("%s[%d].", name, j))
					} else {
						emit(c, fmt.Sprintf("%s[%d]", name, j))
					}
				}
				continue
			}
			if f.Cell.Tab != nil {
				walk(f.Cell.Tab.Fields, name+".")
				continue
			}
			emit(f.Cell, name)
		}
	}
	walk(inst.Fields, "")
	return strings.TrimSpace(b.String())
}

// textWindow returns the first byte offset where two texts differ and a window
// of each around it.
func textWindow(a, b []byte) (int, string) {
	n := min(len(b), len(a))
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	lo := max(i-32, 0)
	hiA := min(i+32, len(a))
	hiB := min(i+32, len(b))
	return i, fmt.Sprintf("leg[%d:%d]=%q engine[%d:%d]=%q", lo, hiA, a[lo:hiA], lo, hiB, b[lo:hiB])
}
