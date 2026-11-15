package mootable

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A number token has no length limit in the verdict (docs/SPEC-TABLES.md
// §16.12): placing one costs time linear in its length, and its length never
// changes the verdict or the value. These rows are a million digits long, and
// the whole probe holds a deadline with more than ten times the margin either
// way.

const longNumberSchema = "package probe\ntable Root\n{\n    d float64\n    u uint32\n}\n"

// longNumberDeadline is the probe's wall-clock bound: the arithmetic is
// linear in the token, so a million digits land in a small fraction of it.
const longNumberDeadline = 30 * time.Second

type longNumberCase struct {
	name, text string
}

// halfwayAtMillion is the exact midpoint between 1 and its next double, then
// padded with zeros and a non-zero digit at the millionth fractional place:
// the tie is broken by a digit far past any binary64's expansion, so it rounds
// up to 1.0000000000000002.
func halfwayAtMillion() string {
	x := 1.0
	next := math.Nextafter(x, math.Inf(1))
	mid := new(big.Rat).Add(new(big.Rat).SetFloat64(x), new(big.Rat).SetFloat64(next))
	mid.Quo(mid, big.NewRat(2, 1))
	intPart, frac, _ := strings.Cut(mid.FloatString(1100), ".")
	frac = strings.TrimRight(frac, "0")
	frac += strings.Repeat("0", 1_000_000-len(frac)-1) + "1"
	return intPart + "." + frac
}

func longNumberUnit(t *testing.T) *ir.Unit {
	t.Helper()
	tree, errs := parser.Parse("Probe.schema", []byte(longNumberSchema))
	if len(errs) != 0 {
		t.Fatalf("parse: %v", errs[0])
	}
	u, errs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(longNumberSchema), AST: tree,
	}})
	if len(errs) != 0 {
		t.Fatalf("check: %v", errs[0])
	}
	return u
}

func longNumberCases() []longNumberCase {
	return []longNumberCase{
		{"ones", `{"d": 0.` + strings.Repeat("1", 1_000_000) + `}`},
		{"halfway", `{"d": ` + halfwayAtMillion() + `}`},
		// value ~1.1e19: out of uint32's domain, so the engine's verdict is
		// out_of_range; the exponent is what makes the digits matter at all.
		{"uint32", `{"u": 1` + strings.Repeat("1", 999_999) + `e-999980}`},
	}
}

// runLongNumberProbe runs main under the pinned moo with a deadline and returns
// its hex lines. A process still running at the deadline is a failure: the
// number token's cost must be linear in its length.
func runLongNumberProbe(t *testing.T, dir, main string, inputs map[string][]byte) []string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "inputs.moo"), moorun.InputsModule(inputs), 0o644); err != nil {
		t.Fatal(err)
	}
	moo, why := moorun.Interpreter()
	if moo == "" {
		t.Fatalf("the moo interpreter does not resolve: %s", why)
	}
	ctx, cancel := context.WithTimeout(context.Background(), longNumberDeadline)
	defer cancel()
	cmd := exec.CommandContext(ctx, moo, main)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("moo did not place the long number tokens within %v", longNumberDeadline)
	}
	if err != nil {
		t.Fatalf("moo %s: %v\n%s", main, err, out)
	}
	return decodeHexLines(t, out)
}

// TestMooJsonLongNumberIsLinearFunctional places the engine's verdict and the
// value of three million-digit tokens, one at a time, under a deadline on the
// whole moo process.
func TestMooJsonLongNumberIsLinearFunctional(t *testing.T) {
	slowtest.Gate(t, "moo (it runs json.moo's number reader)")
	t.Parallel()
	u := longNumberUnit(t)
	dir := generateUnitDir(t, u, nil)
	cases := longNumberCases()

	var b strings.Builder
	b.WriteString(probePrelude)
	fmt.Fprintf(&b, "local M: any = require(\"./%s\")\n", tableModule(u, "Root"))
	b.WriteString(`local function probe(name: string)
	local value = M.NewRoot()
	local report: any = {}
	local ok = M.RootFromJson(value, inputs[name], report)
	hexs(if ok then "read" else "refused")
	if ok then
		hexs(string.format("%08x%08x %d", value.D[2], value.D[1], value.U))
	else
		hexs("-")
	end
end
`)
	for _, c := range cases {
		fmt.Fprintf(&b, "probe(%q)\n", c.name)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.moo"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	for _, c := range cases {
		inputs[c.name] = []byte(c.text)
	}
	start := time.Now()
	lines := runLongNumberProbe(t, dir, "main.moo", inputs)
	elapsed := time.Since(start)
	if len(lines) != 2*len(cases) {
		t.Fatalf("the probe printed %d lines for %d cases:\n%s", len(lines), len(cases), strings.Join(lines, "\n"))
	}

	m := oracleModel(t, longNumberSchema)
	st := oracleRoot(t, m)
	for i, c := range cases {
		inst := m.New(st)
		var report tabletext.Report
		ok := m.Read(inst, []byte(c.text), &report)
		wantVerdict := "refused"
		wantValue := "-"
		if ok {
			wantVerdict = "read"
			wantValue = engineLongNumberValue(t, inst)
		}
		if lines[2*i] != wantVerdict {
			t.Errorf("%s: moo says %q, the engine %q (%s)", c.name, lines[2*i], wantVerdict, report.Reason)
			continue
		}
		if ok && lines[2*i+1] != wantValue {
			t.Errorf("%s: moo read %q, the engine %q", c.name, lines[2*i+1], wantValue)
		}
	}
	t.Logf("three million-digit rows placed in %v", elapsed)
}

// engineLongNumberValue is the engine's cell values in the probe's spelling.
func engineLongNumberValue(t *testing.T, inst *tabletext.Instance) string {
	t.Helper()
	var d uint64
	var u uint64
	for _, f := range inst.Fields {
		switch f.Def.Name {
		case "d":
			d = math.Float64bits(f.Cell.F)
		case "u":
			u = f.Cell.U
		}
	}
	return fmt.Sprintf("%08x%08x %d", uint32(d>>32), uint32(d), u)
}
