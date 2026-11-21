package goosetable_test

// THE COOKED-ROW AND BLOCK SETTERS HOLD A COMPRESSED FLOAT the way every other
// Goose writer does (docs/SPEC-TABLES.md §3.3; docs/rulings.json
// fixed-compressed-negzero): <Row>RowSet<Field> and <Table>BlockSet<Field>
// write a compressed float's -0 as +0, and refuse a value outside its declared
// range (NaN and the infinities included) in every build, writing nothing and
// answering false. A plain float's setter stores the bits as held, -0 and all,
// and answers nothing (the control).
//
// TestGooseCompressedRowSettersAreGuarded is the fast half: it reads the
// generated source. TestGooseCompressedRowSetters is the functional half: it
// runs the setters over a cooked row view and a block projection.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// rowSetterSchema: Cell's c and xs are compressed, p and q plain; Frame is a
// block (a fixed table whose bounded array is of fixed rows), so its
// projection's c and p take BlockSet setters as well.
const rowSetterSchema = `package rsprobe

fixed table Cell
{
    c  float32 | min = -1, max = 1, resolution = 0.01
    xs [2]float32 | min = -1, max = 1, resolution = 0.01
    p  float32
    q  float64
}

fixed table Frame
{
    c    float32 | min = -1, max = 1, resolution = 0.01
    p    float32
    rows [..4]Cell
}
`

func TestGooseCompressedRowSettersAreGuarded(t *testing.T) {
	t.Parallel()
	files, err := goosetable.Generate(probeUnit(t, rowSetterSchema))
	if err != nil {
		t.Fatal(err)
	}
	setter := regexp.MustCompile(`^fn ((?:Cell|Frame)(?:Row|Block)Set(C|Xs|P|Q))\(([^)]*)\)(.*)$`)
	guarded := regexp.MustCompile(`^ -> bool \{ if !\(v >= \(?-1(\.0)?\)? && v <= 1(\.0)?\) \{ return false; \} schema_rt::accel_put\(b, [^;]*schema_rt::accel_cf32_bits\(v\), 4\); return true; \}$`)
	plain := regexp.MustCompile(`^ \{ schema_rt::accel_put\(b, [^;]*schema_rt::accel_f(32|64)_bits\(v\), (4|8)\); \}$`)
	seen := map[string]bool{}
	for name, data := range files {
		for i, line := range strings.Split(string(data), "\n") {
			m := setter.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			seen[m[1]] = true
			compressed := m[2] == "C" || m[2] == "Xs"
			switch {
			case compressed && !guarded.MatchString(m[4]):
				t.Errorf("%s:%d: a compressed float's setter neither refuses out of range nor stores -0 as +0: %s", name, i+1, line)
			case !compressed && !plain.MatchString(m[4]):
				t.Errorf("%s:%d: a plain float's setter must store the bits as held: %s", name, i+1, line)
			}
		}
	}
	for _, want := range []string{"CellRowSetC", "CellRowSetXs", "CellRowSetP", "CellRowSetQ", "FrameRowSetC", "FrameRowSetP",
		"CellBlockSetC", "CellBlockSetXs", "CellBlockSetP", "CellBlockSetQ", "FrameBlockSetC", "FrameBlockSetP"} {
		if !seen[want] {
			t.Errorf("no setter %s was emitted", want)
		}
	}
}

// rowSetterMain drives every setter over a zeroed view: Frame's row at 0,
// its first Cell row inside it, and a Frame block's projection. -0 into a
// compressed slot (+0's bits land), 2.0, -1.5, 1.01 and NaN into one
// (refused, the view unchanged), 0.5 and the bound -1 (stored), and -0 into
// the plain slots (its bits land). Each line is `<label> <answer> <bits>`,
// the bits read back through the getter, or `<label> <answer> <same>` for a
// refusal, 1 when the view is unchanged.
const rowSetterMain = `
fn same(a: const u8[:], b: const u8[:]) -> i64 {
    if a.len != b.len { return 0; }
    for i in a.len { if a[i] != b[i] { return 0; } }
    return 1;
}

fn f32bits(x: f32) -> u64 { return schema_rt::accel_f32_bits(x); }

fn main() {
    var b: u8[>..] = [];
    for i in rsprobe::FrameRowSize { b.push(0); }
    let neg: f32 = -0.0;
    let zero: f32 = 0.0;
    let nan: f32 = zero / zero;
    let root: i64 = 0;
    let cell = rsprobe::FrameRowRowsAt(root, 0);
    var before: u8[>..] = [];
    for x in b { before.push(x); }
    print("row_c_out ", rsprobe::CellRowSetC(b, cell, 2.0), " ", same(b, before));
    print("row_c_nan ", rsprobe::CellRowSetC(b, cell, nan), " ", same(b, before));
    print("row_xs_out ", rsprobe::CellRowSetXs(b, cell, 1, -1.5), " ", same(b, before));
    print("frame_c_out ", rsprobe::FrameRowSetC(b, root, 1.01), " ", same(b, before));
    print("row_c_neg ", rsprobe::CellRowSetC(b, cell, neg), " ", f32bits(rsprobe::CellRowC(b, cell)));
    print("row_xs_neg ", rsprobe::CellRowSetXs(b, cell, 1, neg), " ", f32bits(rsprobe::CellRowXs(b, cell, 1)));
    print("frame_c_neg ", rsprobe::FrameRowSetC(b, root, neg), " ", f32bits(rsprobe::FrameRowC(b, root)));
    print("row_c_in ", rsprobe::CellRowSetC(b, cell, 0.5), " ", f32bits(rsprobe::CellRowC(b, cell)));
    print("row_c_edge ", rsprobe::CellRowSetC(b, cell, -1.0), " ", f32bits(rsprobe::CellRowC(b, cell)));
    rsprobe::CellRowSetP(b, cell, neg);
    print("row_p_neg 1 ", f32bits(rsprobe::CellRowP(b, cell)));
    rsprobe::CellRowSetQ(b, cell, -0.0);
    print("row_q_neg 1 ", schema_rt::accel_f64_bits(rsprobe::CellRowQ(b, cell)));

    var blk: u8[>..] = [];
    for i in rsprobe::FrameBlockMaxBytes { blk.push(0); }
    var bbefore: u8[>..] = [];
    for x in blk { bbefore.push(x); }
    print("block_c_out ", rsprobe::FrameBlockSetC(blk, -2.0), " ", same(blk, bbefore));
    print("block_c_neg ", rsprobe::FrameBlockSetC(blk, neg), " ", f32bits(rsprobe::FrameBlockC(blk)));
    print("block_c_in ", rsprobe::FrameBlockSetC(blk, 0.5), " ", f32bits(rsprobe::FrameBlockC(blk)));
    rsprobe::FrameBlockSetP(blk, neg);
    print("block_p_neg 1 ", f32bits(rsprobe::FrameBlockP(blk)));
}
`

func TestGooseCompressedRowSetters(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Goose toolchain (it runs the cooked-row and block setters)")
	files := probeFiles(t, probeUnit(t, rowSetterSchema), nil)
	files["main.goose"] = []byte(probePrelude + rowSetterMain)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := probeLines(gooserun.Run(t, dir, "main.goose", nil))
	want := map[string]string{
		// out of range and NaN: refused, nothing written
		"row_c_out": "false 1", "row_c_nan": "false 1", "row_xs_out": "false 1", "frame_c_out": "false 1", "block_c_out": "false 1",
		// a compressed -0 lands as +0's bits
		"row_c_neg": "true 0", "row_xs_neg": "true 0", "frame_c_neg": "true 0", "block_c_neg": "true 0",
		// in range, the bound included: stored
		"row_c_in": "true 1056964608", "row_c_edge": "true 3212836864", "block_c_in": "true 1056964608",
		// the plain controls keep -0's bits
		"row_p_neg": "1 2147483648", "row_q_neg": "1 9223372036854775808", "block_p_neg": "1 2147483648",
	}
	for label, w := range want {
		g, ok := got[label]
		if !ok {
			t.Errorf("%s: the probe printed nothing", label)
			continue
		}
		if s := strings.Join(g, " "); s != w {
			t.Errorf("%s: %s, want %s", label, s, w)
		}
	}
	if len(got) != len(want) {
		t.Errorf("the probe printed %d lines, want %d: %v", len(got), len(want), fmt.Sprint(got))
	}
}
