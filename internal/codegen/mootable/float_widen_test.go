package mootable_test

import (
	"encoding/binary"
	"fmt"
	"maps"
	"math"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A FLOAT CROSSES TWO WIDTHS BY BIT SURGERY (docs/PORTING.md M21;
// docs/SPEC-TABLES.md §3.3, §4's float rung). moo has no float: a float32 is
// its binary32 pattern held as a number and a float64 a Uint64 of its
// binary64 pattern (docs/PORT-MOO.md), so where kind 10 widens into kind 11
// the runtime moves the bits itself (table_values.moo V.f32to64), and there
// is no conversion to move a bit. The gate holds that surgery to the
// hardware's exact widening for every finite pattern the probe draws: every
// exponent of both signs with the mantissa corners, the subnormals at a
// stride, and pseudo-random patterns at a fixed seed. NaN and the infinities
// are refused on their raw bits before any rung (TestMooTableLoadMatchesTheReports'
// floats_nan rows), so they are not drawn.

const widenProbe = `local inputs = require("./inputs")
local V = require("./table_values")
local b = inputs.bits
local out = table.create(buffer.len(b) // 4)
for i = 0, buffer.len(b) - 4, 4 do
	local lo, hi = V.f32to64(buffer.readu32(b, i))
	out[#out + 1] = string.format("%08x%08x", hi, lo)
end
print(table.concat(out, "\n"))
`

func widenPatterns() []uint32 {
	var out []uint32
	for sign := uint32(0); sign < 2; sign++ {
		for exp := uint32(0); exp < 255; exp++ {
			for _, frac := range []uint32{0, 1, 2, 0x400000, 0x7ffffe, 0x7fffff, 0x2aaaaa, 0x555555} {
				out = append(out, sign<<31|exp<<23|frac)
			}
		}
		for frac := uint32(1); frac < 0x800000; frac += 0x1fff {
			out = append(out, sign<<31|frac)
		}
	}
	r := rand.New(rand.NewPCG(0x6d6f6f, 0x3231))
	for len(out) < 40000 {
		bits := r.Uint32()
		if bits>>23&0xff == 0xff {
			continue
		}
		out = append(out, bits)
	}
	return out
}

func widenMisses(t *testing.T, g *generatedUnit, patterns []uint32) []string {
	t.Helper()
	in := make([]byte, 4*len(patterns))
	for i, p := range patterns {
		binary.LittleEndian.PutUint32(in[4*i:], p)
	}
	lines := strings.Split(g.run(t, widenProbe, map[string][]byte{"bits": in}), "\n")
	if len(lines) != len(patterns) {
		t.Fatalf("the probe answered %d lines for %d patterns", len(lines), len(patterns))
	}
	var miss []string
	for i, p := range patterns {
		want := fmt.Sprintf("%016x", math.Float64bits(float64(math.Float32frombits(p))))
		if lines[i] != want {
			miss = append(miss, fmt.Sprintf("%08x widened to %s, want %s", p, lines[i], want))
		}
	}
	return miss
}

func TestMooFloat32WidensBitForBit(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_values.moo)")
	t.Parallel()
	patterns := widenPatterns()
	miss := widenMisses(t, keyedUnit(t), patterns)
	for i, m := range miss {
		if i == 10 {
			t.Errorf("... and %d more", len(miss)-10)
			break
		}
		t.Error(m)
	}
	t.Logf("%d finite binary32 patterns widened bit for bit", len(patterns))
}

// TestMooFloat32WidenNegativeControl plants one defect per step of the
// surgery into the emitted table_values.moo, each required to turn the gate:
// the fraction's low three bits dropped, and a subnormal normalized one
// exponent off.
func TestMooFloat32WidenNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_values.moo)")
	t.Parallel()
	g := keyedUnit(t)
	patterns := widenPatterns()
	for _, c := range []struct{ name, from, to string }{
		{"low fraction bits dropped", "\tlocal lo = (frac % 8) * 0x20000000\n", "\tlocal lo = 0\n"},
		{"subnormal exponent off by one", "\t\tlocal e = -126\n", "\t\tlocal e = -125\n"},
	} {
		src := string(g.files["table_values.moo"])
		if strings.Count(src, c.from) != 1 {
			t.Fatalf("%s: table_values.moo carries %q %d times, want 1: the control would be vacuous", c.name, c.from, strings.Count(src, c.from))
		}
		files := maps.Clone(g.files)
		files["table_values.moo"] = []byte(strings.Replace(src, c.from, c.to, 1))
		miss := widenMisses(t, writeUnit(t, g.unit, files), patterns)
		if len(miss) == 0 {
			t.Errorf("%s: every pattern still widens exactly: the gate cannot see the surgery", c.name)
			continue
		}
		t.Logf("%s: %d of %d patterns turn", c.name, len(miss), len(patterns))
	}
}
