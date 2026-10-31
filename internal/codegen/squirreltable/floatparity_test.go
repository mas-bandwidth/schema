package squirreltable_test

// THREE FLOAT RULES THE SQUIRREL TABLE LEG HOLDS BIT FOR BIT AGAINST THE GO
// ENGINE (docs/SPEC-TABLES.md §3.3, §4, §7.6; SPEC.md §4.3):
//
//   - TestSquirrelCompressedFloatClampsLikeGo: the variable form's read of a
//     COMPRESSED float32 clamps a finite value past an end to that end and
//     counts clamped, and lands a -0 as +0, exactly as tablewire's scalarAt.
//   - TestSquirrelMessageQuantizerIsGos: the message form's quantizer and
//     dequantizer are ir.TableMessageQuantize / TableMessageDequantize in
//     float32, fuzzed over random values, step midpoints and their
//     neighbours, the range edges and far outside them: the batch the leg
//     writes is the oracle's byte for byte, and the floats it reads from the
//     oracle's batch are the oracle's bit for bit, clamped and counted the
//     same; an index above the step count is damage.
//   - TestSquirrelCookMeasureRefusesLikeCook: CookMeasure refuses a NaN or an
//     infinity exactly as Cook does, CookReason
//     non_finite_float: <Owner>.<field>, the error tablecook.Cook answers.
//
// Each has a negative control that puts the old behaviour back in memory and
// must red exactly the rows the Go engine says the old behaviour gets wrong.
// The fuzz is seeded: no real time, no randomness between runs.

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const fpSchema = `package fp

type Inner
{
    c float32 = 1.0
}

table Root
{
    a float32 = 1.0
    b float64
    n Inner
    xs [3]float32
}

table V
{
    q float32 | min = -1.5, max = 2.3, resolution = 0.01
    r float32 | min = 0, max = 1, resolution = 0.001
}

fixed table Q
{
    qa float32 | min = -10, max = 10, resolution = 0.01
    qb float32 | min = 0, max = 1, resolution = 0.001
    qc float32 | min = -1.5, max = 2.3, resolution = 0.0007
    qd float32 = 100 | min = 100, max = 100000, resolution = 0.37
}
`

// fpRow is one probe statement, its expected line, and whether the control
// of its test must red it.
type fpRow struct {
	key, code, want string
	red             bool
}

func fpUnit(t *testing.T) (*ir.Unit, *tabletext.Model) {
	t.Helper()
	u, model, _ := nfLoadNamed(t, fpSchema)
	return u, model
}

// fpRun generates the unit, runs the rows and answers each row's line.
func fpRun(t *testing.T, u *ir.Unit, rows []fpRow, inputs map[string][]byte, patch func(map[string][]byte)) map[string]string {
	t.Helper()
	dir := t.TempDir()
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	if patch != nil {
		patch(tables)
	}
	out := filepath.Join(dir, "fp")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}
	var b strings.Builder
	b.WriteString(`local I = dofile("inputs.nut", true);
local schema = dofile("fp/schema_module.nut", true);
local T = schema.require("` + squirreltable.ModuleName(u) + `");
local L = schema.require("schema_table_load");
local hexInt = function(h) {
  local n = 0;
  foreach (c in h) n = (n << 4) | (c <= '9' ? c - '0' : c - 'a' + 10);
  return n;
};
local f64 = function(h) { return casti2f(hexInt(h)); };
local f32bits = function(x) {
  local b = blob(4);
  b.writen(x, 'f');
  b.seek(0);
  return format("%08x", b.readn('i') & 0xffffffff);
};
local hex = function(b, n) {
  local s = "";
  for (local i = 0; i < n; i++) s += format("%02x", b[i] & 0xff);
  return s;
};
local nn = function(x) { return x == null ? "-" : x; };
local voc = {};
local ar = {};
if (!T.AnnounceRead(voc, T.TableMessageAnnouncement, ar)) throw "announce: " + ar.reason;
`)
	for _, r := range rows {
		b.WriteString(r.code + "\n")
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(b.String()))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", inputs)), "\n"), "\n") {
		key, _, _ := strings.Cut(line, " ")
		got[key] = line
	}
	return got
}

// fpCheck compares every row; under a control it requires exactly the rows
// marked red to differ.
func fpCheck(t *testing.T, rows []fpRow, got map[string]string, control bool) {
	t.Helper()
	red, wantRed := 0, 0
	for _, r := range rows {
		same := got[r.key] == r.want
		switch {
		case !control && !same:
			t.Errorf("%s: %q, want %q", r.key, got[r.key], r.want)
		case control && r.red:
			wantRed++
			if same {
				t.Errorf("%s stayed green under the control", r.key)
			} else {
				red++
			}
		case control && !same:
			t.Errorf("%s went red under the control: %q, want %q", r.key, got[r.key], r.want)
		}
	}
	if control {
		if red != wantRed || red == 0 {
			t.Fatalf("the control red %d of its %d rows", red, wantRed)
		}
		t.Logf("the control reds exactly its %d rows of %d", red, len(rows))
	} else {
		t.Logf("compared %d rows against the Go engine", len(rows))
	}
}

// fpPatch applies one in-memory sabotage, each anchor matched exactly once.
func fpPatch(t *testing.T, file string, edits [][2]string) func(map[string][]byte) {
	return func(tables map[string][]byte) {
		src := string(tables[file])
		for _, e := range edits {
			if n := strings.Count(src, e[0]); n != 1 {
				t.Fatalf("the control's anchor %q appears %d times in %s, want exactly once", e[0], n, file)
			}
			src = strings.Replace(src, e[0], e[1], 1)
		}
		tables[file] = []byte(src)
	}
}

// fpValues is the fuzz population for one compressed range: random values in
// and around the range, the step midpoints of the quantizer and one float32
// either side of each, the edges and their neighbours, -0, and far outside.
func fpValues(rng *rand.Rand, f *ir.Field, n int) []float32 {
	shape := ir.TableFieldShape(f)
	count, delta, _ := ir.TableMessageQuantization(shape)
	lo, hi := float32(f.FMin), float32(f.FMax)
	out := []float32{lo, hi, float32(math.Copysign(0, -1)), 0, 1e30, -1e30,
		math.Nextafter32(lo, -1e38), math.Nextafter32(lo, 1e38), math.Nextafter32(hi, -1e38), math.Nextafter32(hi, 1e38)}
	for len(out) < n {
		switch rng.IntN(3) {
		case 0:
			span := float64(hi - lo)
			out = append(out, float32(float64(lo)-span*0.1+rng.Float64()*span*1.2))
		default:
			// THE MIDPOINT of step k, as the quantizer sees it, and a float32
			// either side: where one rounding in double and two in float32
			// part ways
			k := rng.Uint32N(count)
			mid := float32(float32(float32(float32(k)+0.5)/float32(count))*delta) + shape.QMin
			out = append(out, mid, math.Nextafter32(mid, -1e38), math.Nextafter32(mid, 1e38))
		}
	}
	return out[:n]
}

// fpOldQuantize is the leg's former quantizer, in Go: the subtraction and the
// division in one double, rounded once, and the count unrounded. The control
// puts it back, and it must red exactly the values where it parts from Go's.
func fpOldQuantize(s ir.TableMessageShape, value float32) uint32 {
	count, delta, ok := ir.TableMessageQuantization(s)
	if !ok {
		return 0
	}
	norm := float64(float32((float64(value) - float64(s.QMin)) / float64(delta)))
	if !(norm >= 0) {
		norm = 0
	} else if !(norm <= 1) {
		norm = 1
	}
	scaled := float64(float32(norm * float64(count)))
	index := uint32(math.Floor(float64(float32(scaled + 0.5))))
	return min(index, count)
}

func fpField(t *testing.T, st *ir.Struct, name string) *ir.Field {
	t.Helper()
	for _, f := range st.Fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("%s declares no field %s", st.Name, name)
	return nil
}

// ---- 1. the variable form's compressed clamp --------------------------------

func fpClampRows(t *testing.T, model *tabletext.Model) ([]fpRow, map[string][]byte) {
	st := model.Lookup("V")
	q, r := fpField(t, st, "q"), fpField(t, st, "r")
	rng := rand.New(rand.NewPCG(1, 2))
	qs, rs := fpValues(rng, q, 400), fpValues(rng, r, 400)
	inputs := map[string][]byte{}
	var rows []fpRow
	for i := range qs {
		inst := model.New(st)
		nfField(t, inst, "q").Cell.F = float64(qs[i])
		nfField(t, inst, "r").Cell.F = float64(rs[i])
		wire, err := tablewire.Encode(model, inst)
		if err != nil {
			t.Fatalf("clamp row %d: the oracle refused %v, %v: %v", i, qs[i], rs[i], err)
		}
		back := model.New(st)
		var rep tabletext.Report
		if ok, err := tablewire.Decode(model, back, wire, &rep); !ok || err != nil {
			t.Fatalf("clamp row %d: the oracle's reader answered %v %v", i, ok, err)
		}
		gq := math.Float32bits(float32(nfField(t, back, "q").Cell.F))
		gr := math.Float32bits(float32(nfField(t, back, "r").Cell.F))
		key := fmt.Sprintf("clamp:%d", i)
		inputs[key] = wire
		rows = append(rows, fpRow{
			key:  key,
			code: fmt.Sprintf(`{ local rep = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0, malformed = false, refused = false }; local v = L.loadTolerant(T, "V", I[%q], rep); print(%q + " " + rep.clamped + " " + f32bits(v.Q) + "," + f32bits(v.R) + "\n"); }`, key, key),
			want: fmt.Sprintf("%s %d %08x,%08x", key, rep.Clamped, gq, gr),
			// the control lands every value as written: the rows the engine
			// clamped go red, and only those (a -0 is elided by the writer, as
			// +0 is, so it never reaches the read)
			red: rep.Clamped > 0,
		})
	}
	return rows, inputs
}

var fpClampSabotage = [][2]string{{
	"    if (x < fd.fmin) { x = narrowToF32(fd.fmin); report.clamped = report.clamped + 1; }\n" +
		"    else if (x > fd.fmax) { x = narrowToF32(fd.fmax); report.clamped = report.clamped + 1; }\n" +
		"    else if (x == 0.0) x = 0.0;\n",
	"",
}}

func TestSquirrelCompressedFloatClampsLikeGo(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)
	u, model := fpUnit(t)
	rows, inputs := fpClampRows(t, model)
	fpCheck(t, rows, fpRun(t, u, rows, inputs, nil), false)
}

func TestSquirrelCompressedFloatClampNegativeControl(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)
	u, model := fpUnit(t)
	rows, inputs := fpClampRows(t, model)
	fpCheck(t, rows, fpRun(t, u, rows, inputs, fpPatch(t, "schema_table_load.nut", fpClampSabotage)), true)
}

// ---- 2. the message quantizer -----------------------------------------------

// fpQuantRows: write rows (the leg's batch against EncodeMessage), read rows
// (the leg's floats from the oracle's batch against DecodeMessages), and
// over-count rows (an index above the step count is damage). redWrite marks
// the write rows the old quantizer gets wrong; redRead the over-count rows.
func fpQuantRows(t *testing.T, u *ir.Unit, model *tabletext.Model, control string) ([]fpRow, map[string][]byte) {
	st := model.Lookup("Q")
	names := []string{"qa", "qb", "qc", "qd"}
	fields := make([]*ir.Field, len(names))
	values := make([][]float32, len(names))
	rng := rand.New(rand.NewPCG(3, 4))
	for i, n := range names {
		fields[i] = fpField(t, st, n)
		values[i] = fpValues(rng, fields[i], 500)
	}
	var vocabulary tablewire.Vocabulary
	var announced tabletext.Report
	if err := vocabulary.AnnounceRead(tablewire.Announce(u), &announced); err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	var rows []fpRow
	for i := range values[0] {
		inst := model.New(st)
		old := false
		var set strings.Builder
		for k, n := range names {
			x := values[k][i]
			nfField(t, inst, n).Cell.F = float64(x)
			fmt.Fprintf(&set, " v.%s = f64(%q);", "Q"+strings.ToLower(n[1:]), fmt.Sprintf("%016x", math.Float64bits(float64(x))))
			shape := ir.TableFieldShape(fields[k])
			if fpOldQuantize(shape, x) != ir.TableMessageQuantize(shape, x) {
				old = true
			}
		}
		batch, err := tablewire.EncodeMessage(model, inst)
		if err != nil {
			t.Fatalf("quant row %d: the oracle refused: %v", i, err)
		}
		key := fmt.Sprintf("qwrite:%d", i)
		rows = append(rows, fpRow{
			key:  key,
			code: fmt.Sprintf(`{ local v = T.Q();%s local b = blob(64); local sr = {}; local n = T.QSaveMessages([v], b, sr); print(%q + " " + (n < 0 ? "refused" : hex(b, n)) + "\n"); }`, set.String(), key),
			want: key + " " + hex.EncodeToString(batch),
			red:  control == "write" && old,
		})
		back := model.New(st)
		var rep tabletext.Report
		if n, ok, err := tablewire.DecodeMessages(model, []*tabletext.Instance{back}, batch, &vocabulary, &rep); n != 1 || !ok || err != nil {
			t.Fatalf("quant row %d: the oracle's reader answered %d %v %v", i, n, ok, err)
		}
		var bits []string
		for _, n := range names {
			bits = append(bits, fmt.Sprintf("%08x", math.Float32bits(float32(nfField(t, back, n).Cell.F))))
		}
		rkey := fmt.Sprintf("qread:%d", i)
		inputs[rkey] = batch
		rows = append(rows, fpRow{
			key:  rkey,
			code: fmt.Sprintf(`{ local out = [T.Q()]; local rr = {}; local n = T.QLoadMessages(out, voc, I[%q], rr); local w = out[0]; print(%q + " " + n + " " + rr.clamped + " " + f32bits(w.Qa) + "," + f32bits(w.Qb) + "," + f32bits(w.Qc) + "," + f32bits(w.Qd) + "\n"); }`, rkey, rkey),
			want: fmt.Sprintf("%s 1 %d %s", rkey, rep.Clamped, strings.Join(bits, ",")),
			// a row whose write the old quantizer moves reads the same:
			// the read rows take the oracle's own batch
		})
	}
	// AN INDEX ABOVE THE STEP COUNT IS DAMAGE (§3.3): qa's count is 2000 in 11
	// bits, so 2001..2047 are spellable; the oracle refuses each as malformed
	shapeA := ir.TableFieldShape(fields[0])
	count, _, _ := ir.TableMessageQuantization(shapeA)
	// a carried (non-default) value of a, and one whose index differs in the
	// lowest bit: the one spot the two batches differ is a's index
	zeroIndex := ir.TableMessageQuantize(shapeA, 3)
	baseInst := model.New(st)
	nfField(t, baseInst, "qa").Cell.F = float64(ir.TableMessageDequantize(shapeA, zeroIndex))
	base, err := tablewire.EncodeMessage(model, baseInst)
	if err != nil {
		t.Fatal(err)
	}
	for _, idx := range []uint64{uint64(count) + 1, uint64(count) + 7, 1<<shapeA.Bits - 1} {
		inst := model.New(st)
		nfField(t, inst, "qa").Cell.F = float64(ir.TableMessageDequantize(shapeA, zeroIndex^1))
		batch, err := tablewire.EncodeMessage(model, inst)
		if err != nil {
			t.Fatal(err)
		}
		// find a's index bits: the one spot where this batch and the base differ
		at := -1
		for p := 0; p+int(shapeA.Bits) <= len(base)*8; p++ {
			if nfBitAt(base, p, int(shapeA.Bits)) == uint64(zeroIndex) && nfBitAt(batch, p, int(shapeA.Bits)) == uint64(zeroIndex^1) {
				at = p
				break
			}
		}
		if at < 0 {
			t.Fatal("a's index bits are not where the batches differ")
		}
		bad := append([]byte(nil), base...)
		for i := range int(shapeA.Bits) {
			p := at + i
			bad[p>>3] &^= 1 << (p & 7)
			bad[p>>3] |= byte(idx>>i&1) << (p & 7)
		}
		var rep tabletext.Report
		n, _, _ := tablewire.DecodeMessages(model, []*tabletext.Instance{model.New(st)}, bad, &vocabulary, &rep)
		if n != 0 || !rep.Malformed {
			t.Fatalf("index %d: the oracle answered %d bodies, malformed %v; want damage", idx, n, rep.Malformed)
		}
		key := fmt.Sprintf("qover:%d", idx)
		inputs[key] = bad
		rows = append(rows, fpRow{
			key:  key,
			code: fmt.Sprintf(`{ local out = [T.Q()]; local rr = {}; local n = T.QLoadMessages(out, voc, I[%q], rr); print(%q + " " + n + " " + ("malformed" in rr ? rr.malformed : false) + "\n"); }`, key, key),
			want: key + " 0 true",
			red:  control == "read",
		})
	}
	return rows, inputs
}

var fpQuantWriteSabotage = [][2]string{{
	"      local v=f32(value), norm=f32(f32(v-fd.qmin)/fd.qdelta);\n",
	"      local v=f32(value), norm=f32((v-fd.qmin)/fd.qdelta);\n",
}, {
	"      local scaled=f32(norm*f32(fd.qcount.tofloat()));\n",
	"      local scaled=f32(norm*fd.qcount);\n",
}}

var fpQuantReadSabotage = [][2]string{{
	"if(index>shape.QCount){r.bad=true;return 0.0;}",
	"if(index>shape.QCount)index=shape.QCount;",
}}

func TestSquirrelMessageQuantizerIsGos(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)
	u, model := fpUnit(t)
	rows, inputs := fpQuantRows(t, u, model, "")
	fpCheck(t, rows, fpRun(t, u, rows, inputs, nil), false)
}

func TestSquirrelMessageQuantizerNegativeControl(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)
	u, model := fpUnit(t)
	t.Run("write", func(t *testing.T) {
		rows, inputs := fpQuantRows(t, u, model, "write")
		fpCheck(t, rows, fpRun(t, u, rows, inputs, fpPatch(t, "schema_table_message.nut", fpQuantWriteSabotage)), true)
	})
	t.Run("read", func(t *testing.T) {
		rows, inputs := fpQuantRows(t, u, model, "read")
		fpCheck(t, rows, fpRun(t, u, rows, inputs, fpPatch(t, "schema_table_message.nut", fpQuantReadSabotage)), true)
	})
}

// ---- 3. CookMeasure refuses as Cook does ------------------------------------

func fpCookRows(t *testing.T, model *tabletext.Model) []fpRow {
	st := model.Lookup("Root")
	type loc struct {
		name, path string
		set        func(*tabletext.Instance, float64)
	}
	locs := []loc{
		{"a", "v.A", func(i *tabletext.Instance, x float64) { nfField(t, i, "a").Cell.F = x }},
		{"b", "v.B", func(i *tabletext.Instance, x float64) { nfField(t, i, "b").Cell.F = x }},
		{"c", "v.N.C", func(i *tabletext.Instance, x float64) { nfField(t, nfField(t, i, "n").Cell.Tab, "c").Cell.F = x }},
		{"xs", "v.Xs[1]", func(i *tabletext.Instance, x float64) { nfField(t, i, "xs").Elems[1].F = x }},
	}
	var rows []fpRow
	for _, c := range nfCases {
		for _, l := range locs {
			// a float32 slot takes the float32 of the case's class: a double
			// past float32's range is not a value a Go float32 cell can hold
			x := math.Float64frombits(c.b64)
			hexBits := fmt.Sprintf("%016x", c.b64)
			if l.name != "b" && c.finite {
				x = float64(math.Float32frombits(c.b32))
				hexBits = fmt.Sprintf("%016x", math.Float64bits(x))
			}
			inst := model.New(st)
			l.set(inst, x)
			cooked, err := tablecook.Cook(model, inst, tablecook.Options{})
			want := ""
			var refusal *tablewire.NonFiniteRefusal
			switch {
			case c.finite && err == nil:
				want = fmt.Sprintf("%d true -", len(cooked))
			case !c.finite && errors.As(err, &refusal):
				want = fmt.Sprintf("-1 false %s", err.Error())
			default:
				t.Fatalf("cook %s at %s: the oracle answered %v", c.name, l.name, err)
			}
			key := fmt.Sprintf("cook:%s/%s", c.name, l.name)
			rows = append(rows, fpRow{
				key: key,
				code: fmt.Sprintf(`{ local v = T.Root(); %s = f64(%q); local m = T.CookMeasure("Root", v); local mr = T.CookReason; local ok = T.Cook("Root", v, blob(m < 0 ? 4096 : m), 1); print(%q + " " + m + " " + ok + " " + nn(m < 0 ? mr : T.CookReason) + "\n"); }`,
					l.path, hexBits, key),
				want: key + " " + want,
				red:  !c.finite,
			})
		}
	}
	return rows
}

var fpCookSabotage = [][2]string{{
	"  if (!_M.Cook(T, root, value, blob(total), 1)) return -1;\n",
	"",
}}

func TestSquirrelCookMeasureRefusesLikeCook(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)
	u, model := fpUnit(t)
	rows := fpCookRows(t, model)
	fpCheck(t, rows, fpRun(t, u, rows, nil, nil), false)
}

func TestSquirrelCookMeasureNegativeControl(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)
	u, model := fpUnit(t)
	rows := fpCookRows(t, model)
	fpCheck(t, rows, fpRun(t, u, rows, nil, fpPatch(t, "schema_table_cookwrite.nut", fpCookSabotage)), true)
}
