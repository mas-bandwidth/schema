package compiler

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// I13 (docs/PORTING.md) — THE TEXT DIFFERENTIAL AGAINST A THIRD
// IMPLEMENTATION, for the C leg. The pinned corpus holds eighteen texts the
// compiler's own Go engine wrote; this holds the SPELLING RULE over instances
// nobody wrote down. For each random instance the Go engine writes the wire
// and the text; the generated C loads the wire and must spell the SAME text
// byte for byte — and the other direction, reading that text and saving the
// wire it came from.
//
// The rule eighteen instances cannot cover is the float tie: a float32 such as
// -266744.625 renders as an eight-digit TIE, both candidates round-trip to the
// same float32, so the writer's own tie-break decides the byte. The random
// draw below reaches ties and every other scalar spelling; the negative control
// restores the magnitude tie-break and requires the gate to notice.
const cJsonProbeSchema = `package probe

enum Color { Red, Green, Blue }
flags Caps { Jump, Crouch, Shoot }

fixed table Leaf
{
    value  int32
    weight float32
    tags   string(6)
}

fixed table Root
{
    f32     float32
    f64     float64
    tiny    int8
    small   uint16
    huge    int64
    count   uint64
    mask    bits(5)
    flag    bool
    name    string(8)
    blob    bytes(4)
    color   Color
    caps    Caps
    numbers [3]int32
    samples [..4]float32
    opt     ?uint32
    leaf    Leaf
}
`

// cJsonInterestingFloats are spellings a random draw rarely lands on and the
// pinned corpus has never reached — negative zero, the smallest subnormals,
// values at a %g exponent boundary, and the exact tie the JavaScript leg found.
var cJsonInterestingFloats = []float64{
	0, math.Copysign(0, -1),
	-266744.625, 266744.625,
	0.1, -0.1, 1e-5, 1.5e-7, 1e21, -1e21,
	float64(math.SmallestNonzeroFloat32), -float64(math.SmallestNonzeroFloat32),
	float64(math.MaxFloat32), -float64(math.MaxFloat32),
}

func TestCTableJsonDifferential(t *testing.T) {
	u := unitFromSource(t, cJsonProbeSchema)
	model := tabletext.NewModel(u)
	rng := rand.New(rand.NewPCG(0x419, 0x9098))

	const rounds = 48
	var wires, texts [][]byte
	for i := range rounds {
		value := model.New(u.Tables["Root"])
		fillCTableRandom(rng, model, value)
		wire, err := tablewire.Encode(model, value)
		if err != nil {
			t.Fatalf("round %d: encode: %v", i, err)
		}
		// The oracle is the Go engine READING THE WIRE, exactly as `schema
		// unpack` does — not this test's in-memory instance. A writer elides a
		// field the wire then defaults, and only the read-back text is the
		// text both implementations must agree on.
		decoded := model.New(u.Tables["Root"])
		var report tabletext.Report
		if ok, err := tablewire.Decode(model, decoded, wire, &report); err != nil || !ok || !report.Silent() {
			t.Fatalf("round %d: the engine cannot read its own wire: %v %v %+v", i, ok, err, report)
		}
		text, err := model.Write(decoded)
		if err != nil {
			t.Fatalf("round %d: write: %v", i, err)
		}
		wires = append(wires, wire)
		texts = append(texts, append(append([]byte(nil), text...), '\n'))
	}
	runCTableWireProbe(t, u, cJsonDifferentialSource(wires, texts))
}

func fillCTableRandom(r *rand.Rand, m *tabletext.Model, inst *tabletext.Instance) {
	for i := range inst.Fields {
		fillCTableField(r, m, &inst.Fields[i])
	}
}

func fillCTableField(r *rand.Rand, m *tabletext.Model, fv *tabletext.Field) {
	f := fv.Def
	switch f.Array {
	case ir.ArrayFixed, ir.ArrayCounted:
		if f.Array == ir.ArrayCounted {
			fv.Count = r.IntN(len(fv.Elems) + 1)
		} else {
			fv.Count = len(fv.Elems)
		}
		for j := range fv.Elems {
			if f.Array == ir.ArrayCounted && j >= fv.Count {
				continue
			}
			if r.IntN(5) == 0 {
				continue
			}
			fillCTableCell(r, m, &fv.Elems[j], f)
		}
		return
	}
	if f.Type.Optional {
		fv.Present = r.IntN(4) != 0
		if !fv.Present {
			return
		}
	}
	fillCTableCell(r, m, &fv.Cell, f)
}

func fillCTableCell(r *rand.Rand, m *tabletext.Model, cell *tabletext.Cell, f *ir.Field) {
	switch f.Type.Kind {
	case ir.TBool:
		cell.B = r.IntN(2) == 1
	case ir.TInt:
		if f.Type.Width >= 64 {
			if f.Type.Signed {
				cell.I = r.Int64()
			} else {
				cell.I = int64(r.Uint64())
			}
			cell.U = uint64(cell.I)
			return
		}
		bits := uint(f.Type.Width)
		v := r.Uint64() & ((uint64(1) << bits) - 1)
		if f.Type.Signed {
			cell.I = signExtend(v, int(bits))
		} else {
			cell.I = int64(v)
		}
		cell.U = uint64(cell.I)
	case ir.TBits:
		bits := uint(f.Type.Width)
		if bits >= 64 {
			cell.U = r.Uint64()
			return
		}
		cell.U = r.Uint64() & ((uint64(1) << bits) - 1)
	case ir.TFloat32:
		cell.F = randomCTableFloat32(r)
	case ir.TFloat64:
		cell.F = randomCTableFloat64(r)
	case ir.TString:
		cell.Str = randomCTableText(r, int(f.Type.Size), true)
	case ir.TBytes:
		cell.Str = randomCTableText(r, int(f.Type.Size), false)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			cell.U = uint64(r.Int64N(ref.Max + 1))
		case *ir.Flags:
			bits := uint(ref.WireBits)
			if bits >= 64 {
				cell.U = r.Uint64()
				return
			}
			cell.U = r.Uint64() & ((uint64(1) << bits) - 1)
		case *ir.Struct:
			if cell.Tab == nil {
				cell.Tab = m.New(ref)
			}
			fillCTableRandom(r, m, cell.Tab)
		case *ir.Union:
			// leave the None tag: an arm needs a whole declared payload filled
		}
	}
}

func signExtend(v uint64, bits int) int64 {
	shift := uint(64 - bits)
	return int64(v<<shift) >> shift
}

func randomCTableText(r *rand.Rand, max int, printable bool) []byte {
	n := 0
	if max > 0 {
		n = r.IntN(max + 1)
	}
	out := make([]byte, n)
	for i := range out {
		if printable {
			out[i] = byte('a' + r.IntN(26))
		} else {
			out[i] = byte(r.IntN(256))
		}
	}
	return out
}

func randomCTableFloat32(r *rand.Rand) float64 {
	if r.IntN(4) == 0 {
		return cJsonInterestingFloats[r.IntN(len(cJsonInterestingFloats))]
	}
	for {
		v := float64(math.Float32frombits(r.Uint32()))
		if !math.IsNaN(v) && !math.IsInf(v, 0) {
			return v
		}
	}
}

func randomCTableFloat64(r *rand.Rand) float64 {
	if r.IntN(4) == 0 {
		return cJsonInterestingFloats[r.IntN(len(cJsonInterestingFloats))]
	}
	for {
		v := math.Float64frombits(r.Uint64())
		if !math.IsNaN(v) && !math.IsInf(v, 0) {
			return v
		}
	}
}

func cJsonDifferentialSource(wires, texts [][]byte) string {
	var b strings.Builder
	b.WriteString(`#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "instance %d line %d: %s\n", i, __LINE__, #x); return 1; } } while (0)
`)
	for i, wire := range wires {
		fmt.Fprintf(&b, "static const uint8_t w%d[] = {%s};\n", i, cByteList(wire))
		fmt.Fprintf(&b, "static const uint8_t t%d[] = {%s};\n", i, cByteList(texts[i]))
	}
	b.WriteString("static const uint8_t *wires[] = {")
	for i := range wires {
		fmt.Fprintf(&b, "w%d,", i)
	}
	b.WriteString("};\nstatic const int64_t wsizes[] = {")
	for _, wire := range wires {
		fmt.Fprintf(&b, "%d,", len(wire))
	}
	b.WriteString("};\nstatic const uint8_t *texts[] = {")
	for i := range texts {
		fmt.Fprintf(&b, "t%d,", i)
	}
	b.WriteString("};\nstatic const int64_t tsizes[] = {")
	for _, text := range texts {
		fmt.Fprintf(&b, "%d,", len(text))
	}
	fmt.Fprintf(&b, "};\n#define N %d\n", len(wires))
	b.WriteString(`int main(void)
{
    int i;
    for (i = 0; i < N; i++)
    {
        Root value, parsed;
        TableReport report;
        char out[65536];
        uint8_t saved[65536];
        int64_t n, sn;
        memset(&report, 0, sizeof(report));
        CHECK(root_load(&value, wires[i], wsizes[i], &report));
        CHECK(!report.malformed && !report.refused && !report.unknown && !report.kind_mismatch && !report.clamped);
        n = root_to_json(&value, out, (int64_t)sizeof(out));
        CHECK(n == tsizes[i]);
        CHECK(memcmp(out, texts[i], (size_t)n) == 0);
        memset(&report, 0, sizeof(report));
        CHECK(root_from_json(&parsed, (const char *)texts[i], tsizes[i], &report));
        CHECK(!report.malformed && !report.refused && !report.unknown && !report.kind_mismatch && !report.clamped);
        sn = root_save(&parsed, saved, (int64_t)sizeof(saved));
        CHECK(sn == wsizes[i]);
        CHECK(memcmp(saved, wires[i], (size_t)sn) == 0);
    }
    return 0;
}
`)
	return b.String()
}

func cByteList(data []byte) string {
	var b strings.Builder
	for i, x := range data {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "0x%02x", x)
	}
	return b.String()
}

// TestCTableJsonDifferentialCatchesMagnitudeTieBreak is the differential's
// NEGATIVE CONTROL, the tie-break half. The value -266744.625 is an exact
// float32 whose eight-digit rendering is a TIE: both -266744.62 and -266744.63
// round-trip to it. C breaks the tie to EVEN and the engine agrees, so the
// honest spelling passes; restore the MAGNITUDE tie-break a port's own
// formatter reaches for, and the gate must go red. This is the exact defect the
// JavaScript leg found on its first run.
func TestCTableJsonDifferentialCatchesMagnitudeTieBreak(t *testing.T) {
	u := unitFromSource(t, cJsonProbeSchema)
	model := tabletext.NewModel(u)
	inst := model.New(u.Tables["Root"])
	inst.Fields[0].Cell.F = -266744.625
	wire, err := tablewire.Encode(model, inst)
	if err != nil {
		t.Fatal(err)
	}
	decoded := model.New(u.Tables["Root"])
	var report tabletext.Report
	if ok, err := tablewire.Decode(model, decoded, wire, &report); err != nil || !ok || !report.Silent() {
		t.Fatalf("the engine cannot read its own wire: %v %v %+v", ok, err, report)
	}
	engine, err := model.Write(decoded)
	if err != nil {
		t.Fatal(err)
	}
	engine = append(engine, '\n')
	if !bytes.Contains(engine, []byte("-266744.62")) {
		t.Fatalf("the corpus lost its tie — the value spells %q", engine)
	}
	magnitude := bytes.Replace(engine, []byte("-266744.62"), []byte("-266744.63"), 1)

	if err := runCTableProbeOutcome(t, u, cJsonDifferentialSource([][]byte{wire}, [][]byte{engine})); err != nil {
		t.Fatalf("the honest tie-break must pass the gate: %v", err)
	}
	if err := runCTableProbeOutcome(t, u, cJsonDifferentialSource([][]byte{wire}, [][]byte{magnitude})); err == nil {
		t.Fatal("a magnitude tie-break left the differential green — the gate is watching nothing")
	} else {
		t.Logf("the restored magnitude tie-break turns the gate RED: %v", err)
	}
}

// runCTableProbeOutcome is runCTableWireProbe's non-fatal form: it compiles and
// runs the probe and HANDS BACK what happened, so a negative control can require
// the run to fail.
func runCTableProbeOutcome(t *testing.T, u *ir.Unit, source string) error {
	t.Helper()
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("generated C execution requires cc")
	}
	files, err := New().Generate(u, "c", Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-Wshadow", "-O2", "-I", dir, filepath.Join(dir, "main.c")}
	for name := range files {
		if strings.HasSuffix(name, ".c") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	args = append(args, "-lm", "-o", filepath.Join(dir, "probe"))
	if output, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, output)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, filepath.Join(dir, "probe")).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v\n%s", err, output)
	}
	return nil
}
