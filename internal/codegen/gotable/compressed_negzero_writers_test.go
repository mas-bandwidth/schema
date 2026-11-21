package gotable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A COMPRESSED FLOAT'S -0 IS WRITTEN AS +0 BY EVERY GENERATED GO WRITER
// (docs/SPEC-TABLES.md §3.3, the special values; docs/rulings.json
// fixed-compressed-negzero). The fixed save already held it
// (TestFixedCompressedNegZeroWrite); the variable save, its retaining twin and
// the text writer stored the bits as held. Each case sets one cell to -0 in
// the native value and owes the bytes its +0 twin writes; the plain float `p`
// is the control, whose -0 rides bit-exact.

const negZeroWritersSchema = `package probe

enum Level { Low, High }

union Pick
{
    v float32 | min = 0, max = 0.1, resolution = 0.01
    n uint8
}

table Var
{
    a    float32 = 0.05 | min = 0, max = 0.1, resolution = 0.01
    z    float32 | min = 0, max = 0.1, resolution = 0.01
    xs   [2]float32 | min = 0, max = 0.1, resolution = 0.01
    ys   [..3]float32 | min = 0, max = 0.1, resolution = 0.01
    ks   [Level]float32 | min = 0, max = 0.1, resolution = 0.01
    pick Pick
    p    float32
}

fixed table Fix
{
    a    float32 = 0.05 | min = 0, max = 0.1, resolution = 0.01
    z    float32 | min = 0, max = 0.1, resolution = 0.01
    xs   [2]float32 | min = 0, max = 0.1, resolution = 0.01
    ys   [..3]float32 | min = 0, max = 0.1, resolution = 0.01
    ks   [Level]float32 | min = 0, max = 0.1, resolution = 0.01
    pick Pick
    p    float32
}
`

const negZeroWritersProbe = `package probe

import (
	"bytes"
	"math"
	"testing"
)

var negZero = float32(math.Copysign(0, -1))

// set is one case: it writes zero (the twin's +0, or -0) into one cell of a
// value whose other cells force the bytes around it to be written.
type set struct {
	name  string
	plain bool
	v     func(v *Var, zero float32)
	f     func(v *Fix, zero float32)
}

var cases = []set{
	{"leaf_nonzero_default", false, func(v *Var, z float32) { v.A = z }, func(v *Fix, z float32) { v.A = z }},
	{"leaf_zero_default", false, func(v *Var, z float32) { v.Z = z }, func(v *Fix, z float32) { v.Z = z }},
	{"array_element", false, func(v *Var, z float32) { v.Xs[0], v.Xs[1] = z, 0.05 }, func(v *Fix, z float32) { v.Xs[0], v.Xs[1] = z, 0.05 }},
	{"counted_element", false, func(v *Var, z float32) { v.Ys[0], v.Ys[1], v.YsCount = 0.05, z, 2 }, func(v *Fix, z float32) { v.Ys[0], v.Ys[1], v.YsCount = 0.05, z, 2 }},
	{"keyed_slot", false, func(v *Var, z float32) { v.Ks[0], v.Ks[1] = z, 0.05 }, func(v *Fix, z float32) { v.Ks[0], v.Ks[1] = z, 0.05 }},
	{"union_arm", false, func(v *Var, z float32) { v.Pick.Type = PickTypeV; v.Pick.V().Value = z }, func(v *Fix, z float32) { v.Pick.Type = PickTypeV; v.Pick.V().Value = z }},
	{"plain_control", true, func(v *Var, z float32) { v.P = z }, func(v *Fix, z float32) { v.P = z }},
}

func varBytes(t *testing.T, writer string, v *Var) []byte {
	t.Helper()
	switch writer {
	case "save":
		out := make([]byte, VarMeasure(v))
		if VarSave(v, out) != int64(len(out)) {
			t.Fatal("VarSave refused")
		}
		return out
	case "save_retain":
		var retain TableRetain
		var report TableReport
		out := make([]byte, VarMeasureRetain(v, &retain))
		if VarSaveRetain(v, &retain, out, &report) != int64(len(out)) {
			t.Fatalf("VarSaveRetain refused: %+v", report)
		}
		return out
	case "json":
		out := make([]byte, VarToJSONMeasure(v))
		if VarToJSON(v, out) != int64(len(out)) {
			t.Fatal("VarToJSON refused")
		}
		return out
	case "cook":
		out := make([]byte, VarCookMeasure(v))
		if !VarCookFrom(v, out, TableByteOrderLittle) {
			t.Fatal("VarCookFrom refused")
		}
		return out
	}
	t.Fatalf("no writer %s", writer)
	return nil
}

func fixBytes(t *testing.T, writer string, v *Fix) []byte {
	t.Helper()
	switch writer {
	case "save":
		out := make([]byte, FixFixedMeasure(1))
		if FixFixedSave([]Fix{*v}, out) != int64(len(out)) {
			t.Fatal("FixFixedSave refused")
		}
		return out
	case "json":
		out := make([]byte, FixToJSONMeasure(v))
		if FixToJSON(v, out) != int64(len(out)) {
			t.Fatal("FixToJSON refused")
		}
		return out
	case "message":
		var report TableReport
		out := make([]byte, FixMeasureMessages([]*Fix{v}, &report))
		if FixSaveMessages([]*Fix{v}, out, &report) != int64(len(out)) {
			t.Fatalf("FixSaveMessages refused: %+v", report)
		}
		return out
	case "cook":
		out := make([]byte, FixCookMeasure(v))
		if !FixCookFrom(v, out, TableByteOrderLittle) {
			t.Fatal("FixCookFrom refused")
		}
		return out
	}
	t.Fatalf("no writer %s", writer)
	return nil
}

func TestCompressedNegZeroWriters(t *testing.T) {
	for _, c := range cases {
		for _, writer := range []string{"save", "save_retain", "json", "cook"} {
			t.Run("Var/"+writer+"/"+c.name, func(t *testing.T) {
				var plus, minus Var
				VarReset(&plus)
				VarReset(&minus)
				c.v(&plus, 0)
				c.v(&minus, negZero)
				check(t, c.plain, varBytes(t, writer, &minus), varBytes(t, writer, &plus))
			})
		}
		for _, writer := range []string{"save", "json", "message", "cook"} {
			t.Run("Fix/"+writer+"/"+c.name, func(t *testing.T) {
				var plus, minus Fix
				FixReset(&plus)
				FixReset(&minus)
				c.f(&plus, 0)
				c.f(&minus, negZero)
				check(t, c.plain, fixBytes(t, writer, &minus), fixBytes(t, writer, &plus))
			})
		}
	}
}

func check(t *testing.T, plain bool, minus, plus []byte) {
	t.Helper()
	if plain {
		if bytes.Equal(minus, plus) {
			t.Errorf("a plain float's -0 did not ride: % x", minus)
		}
		return
	}
	if !bytes.Equal(minus, plus) {
		t.Errorf("a compressed float's -0 was not written as +0:\n -0 % x\n +0 % x", minus, plus)
	}
}
`

// TestGoCompressedFloatStoresAreGuarded is the fast twin of the probe below:
// it reads the generated source rather than running it. Every variable-form
// store of a compressed float sits in the else of a zero test that stores +0,
// and the plain float's store does not (the control). The JSON and cook
// runtimes carry the same test.
func TestGoCompressedFloatStoresAreGuarded(t *testing.T) {
	t.Parallel()
	files, err := Generate(unitFrom(t, negZeroWritersSchema))
	if err != nil {
		t.Fatal(err)
	}
	var guarded, plain int
	var all strings.Builder
	for name, data := range files {
		all.Write(data)
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if !strings.Contains(line, ".Put32(math.Float32bits(") {
				continue
			}
			isPlain := strings.Contains(line, "(value.P))")
			isGuarded := i > 0 && strings.TrimSpace(lines[i-1]) == "} else {" && i > 2 &&
				strings.HasSuffix(strings.TrimSpace(lines[i-2]), ".Put32(0)")
			switch {
			case isPlain && isGuarded:
				t.Errorf("%s:%d: the plain float's -0 is normalized: %s", name, i+1, strings.TrimSpace(line))
			case isPlain:
				plain++
			case !isGuarded:
				t.Errorf("%s:%d: a compressed float is stored as held: %s", name, i+1, strings.TrimSpace(line))
			default:
				guarded++
			}
		}
	}
	if guarded == 0 || plain == 0 {
		t.Fatalf("the source has %d guarded compressed stores and %d plain ones; want both", guarded, plain)
	}
	if want := "if f.HasRange && value == 0 {"; strings.Count(all.String(), want) != 2 {
		t.Errorf("the generated JSON writer does not carry %q for float32 and float64", want)
	}
	if want := "if f.HasRange&&(f.Kind==10&&value==0x80000000||f.Kind==11&&value==0x8000000000000000){value=0}"; !strings.Contains(goCookWriteRuntime, want) {
		t.Errorf("the cook writer's runtime does not carry %q", want)
	}
}

func TestGoCompressedNegZeroWriters(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "go test (a generated probe package)")
	runGenerated(t, negZeroWritersSchema, strings.TrimSpace(negZeroWritersProbe)+"\n")
}
