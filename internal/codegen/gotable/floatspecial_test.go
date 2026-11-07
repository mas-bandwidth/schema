package gotable

// A PLAIN FLOAT'S SPECIAL VALUES, IN EVERY FORM (docs/SPEC-TABLES.md §3.3, "A
// PLAIN FLOAT'S SPECIAL VALUES TAKE ONE RULE"; Glenn, 2026-10-27: "Tables go
// finite-only too, same rule as packets. NaN/inf refused on write and read,
// -0 and subnormals kept"). The rule, held on every Go form:
//
//   - -0 is a value: the default elision compares BIT PATTERNS, so a -0 rides
//     the variable and message forms instead of being elided and read back as
//     +0, and the text reads -0 as -0. Found by the schema Go fuzz campaign:
//     {"echo":-0} read from JSON wrote -0 as text and +0 through the wire.
//   - the subnormals are ordinary values and ride bit for bit.
//   - a NaN (any sign, any payload) or an infinity is refused by EVERY WRITER
//     (measure and save answer -1 in the variable, message and fixed forms,
//     ToJSON as before), and EVERY READ refuses one on its raw bits, the
//     tolerant read included: it answers false, lands nothing, and the report
//     names the field, non_finite_float: <Table>.<field>, with Err()
//     TableRefuseNonFiniteFloat. Since no writer emits one, the reads are
//     driven by hand-built wires: a finite payload flipped to the pattern, as
//     a damaged byte or a foreign writer would hand it.
//
// Every assertion is on the RAW BITS: -0 == +0 and NaN != NaN under ==.

import (
	"strings"
	"testing"
)

const fsSchema = `package probe

union Shape
{
    r float64
    n int32
}

fixed table Root
{
    a float32
    b float64
    xs [..3]float64
    ys [2]float64
    u Shape
}

table Var
{
    a float32
    b float64
    xs [..3]float64
    ys [2]float64
    u Shape
}
`

const fsProbe = `package probe

import (
	"bytes"
	"errors"
	"math"
	"testing"
)

type fsCase struct {
	name   string
	b32    uint32
	b64    uint64
	finite bool
}

var fsCases = []fsCase{
	{"plus_zero", 0x00000000, 0x0000000000000000, true},
	{"minus_zero", 0x80000000, 0x8000000000000000, true},
	{"nan_quiet", 0x7FC00000, 0x7FF8000000000000, false},
	{"nan_payload", 0xFFA5A5A5, 0x7FF00DEFACED0001, false}, // signalling, a rich payload
	{"nan_signalling", 0x7F800001, 0xFFF0000000000001, false},
	{"plus_inf", 0x7F800000, 0x7FF0000000000000, false},
	{"minus_inf", 0xFF800000, 0xFFF0000000000000, false},
	{"largest_subnormal", 0x007FFFFF, 0x000FFFFFFFFFFFFF, true},
	{"smallest_negative_subnormal", 0x80000001, 0x8000000000000001, true},
}

// the bits every float slot holds: a, b, xs[0..2], ys[0..1] and the union arm r.
type fsBits struct {
	a                        uint32
	b, x0, x1, x2, y0, y1, r uint64
	xn                       int32
}

func (c fsCase) want(arm bool) fsBits {
	w := fsBits{a: c.b32, b: c.b64, x0: c.b64, x1: c.b64, x2: c.b64, y0: c.b64, y1: c.b64, xn: 3}
	if arm {
		w.r = c.b64
	}
	return w
}

func f64(v float64) uint64 { return math.Float64bits(v) }

func rootBits(v *Root) fsBits {
	w := fsBits{a: math.Float32bits(v.A), b: f64(v.B), x0: f64(v.Xs[0]), x1: f64(v.Xs[1]), x2: f64(v.Xs[2]), y0: f64(v.Ys[0]), y1: f64(v.Ys[1]), xn: v.XsCount}
	if v.U.Type == ShapeTypeR {
		w.r = f64(v.U.R().Value)
	} else {
		w.r = 0xDEAD
	}
	return w
}

func varBits(v *Var) fsBits {
	w := fsBits{a: math.Float32bits(v.A), b: f64(v.B), x0: f64(v.Xs[0]), x1: f64(v.Xs[1]), x2: f64(v.Xs[2]), y0: f64(v.Ys[0]), y1: f64(v.Ys[1]), xn: v.XsCount}
	if v.U.Type == ShapeTypeR {
		w.r = f64(v.U.R().Value)
	} else {
		w.r = 0xDEAD
	}
	return w
}

func fillRoot(v *Root, c fsCase) {
	RootReset(v)
	f := math.Float64frombits(c.b64)
	v.A, v.B = math.Float32frombits(c.b32), f
	v.Xs, v.XsCount, v.Ys = [3]float64{f, f, f}, 3, [2]float64{f, f}
	v.U.Type = ShapeTypeR
	v.U.R().Value = f
}

func fillVar(v *Var, c fsCase) {
	VarReset(v)
	f := math.Float64frombits(c.b64)
	v.A, v.B = math.Float32frombits(c.b32), f
	v.Xs, v.XsCount, v.Ys = [3]float64{f, f, f}, 3, [2]float64{f, f}
	v.U.Type = ShapeTypeR
	v.U.R().Value = f
}

func landed(t *testing.T, form string, c fsCase, got, want fsBits) {
	t.Helper()
	if got != want {
		t.Errorf("%s %s: landed %#x, want %#x", form, c.name, got, want)
	}
}

// clean: a finite special value is an ordinary value to every counter.
func clean(t *testing.T, form string, c fsCase, r *TableReport) {
	t.Helper()
	if r.Clamped != 0 || r.KindMismatch != 0 || r.Unknown != 0 || r.Widened != 0 || r.Duplicate != 0 || r.Malformed || r.Reason != "" || r.NonFiniteFloat != "" || r.Err() != nil {
		t.Errorf("%s %s: counted %+v, want a clean read", form, c.name, *r)
	}
}

// refused: a read of a non-finite float answers false, names the field, and
// its typed reason is non_finite_float.
func refused(t *testing.T, form string, ok bool, r *TableReport, field string) {
	t.Helper()
	if ok || r.NonFiniteFloat != field || r.Reason != "non_finite_float: "+field || !errors.Is(r.Err(), TableRefuseNonFiniteFloat) {
		t.Errorf("%s: ok %v report %+v Err %v, want the refusal non_finite_float: %s", form, ok, *r, r.Err(), field)
	}
}

func strictRefused(t *testing.T, form string, err error) {
	t.Helper()
	if !errors.Is(err, TableRefuseNonFiniteFloat) || err.Error() != "non_finite_float" {
		t.Errorf("%s: strict read answered %v, want the refusal non_finite_float", form, err)
	}
}

// flipped is the wire with its one copy of from replaced by to.
func flipped(t *testing.T, wire, from, to []byte) []byte {
	t.Helper()
	i := bytes.Index(wire, from)
	if i < 0 || bytes.Index(wire[i+len(from):], from) >= 0 {
		t.Fatalf("want exactly one % x in % x", from, wire)
	}
	out := append([]byte(nil), wire...)
	copy(out[i:], to)
	return out
}

func le64(v uint64) []byte {
	var b [8]byte
	for i := range b {
		b[i] = byte(v >> (8 * i))
	}
	return b[:]
}

func le32(v uint32) []byte { return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)} }

// flippedBits is a bitpacked batch with its one width-bit copy of from (low
// bit first, as the bit writer puts it) replaced by to.
func flippedBits(t *testing.T, buf []byte, from, to uint64, width int) []byte {
	t.Helper()
	get := func(at int) uint64 {
		var v uint64
		for i := range width {
			if buf[(at+i)/8]>>uint((at+i)%8)&1 == 1 {
				v |= 1 << uint(i)
			}
		}
		return v
	}
	found := -1
	for at := 0; at+width <= len(buf)*8; at++ {
		if get(at) == from {
			if found >= 0 {
				t.Fatalf("the %d-bit pattern %#x rides twice", width, from)
			}
			found = at
		}
	}
	if found < 0 {
		t.Fatalf("the %d-bit pattern %#x does not ride", width, from)
	}
	out := append([]byte(nil), buf...)
	for i := range width {
		bit := byte(1) << uint((found+i)%8)
		if to>>uint(i)&1 == 1 {
			out[(found+i)/8] |= bit
		} else {
			out[(found+i)/8] &^= bit
		}
	}
	return out
}

func vocabulary(t *testing.T) *TableVocabulary {
	t.Helper()
	ann := make([]byte, AnnounceMeasure())
	Announce(ann)
	storage := make([]TableMessageEntry, 64)
	var vocab TableVocabulary
	vocab.Init(storage)
	var arep TableReport
	if !AnnounceRead(&vocab, ann, &arep) {
		t.Fatalf("announce read: %+v", arep)
	}
	return &vocab
}

// THE FIXED CLASS: the variable wire, the strict read, text and message forms.
// A finite special value rides every form; a non-finite one is refused by
// every writer.
func TestRoot(t *testing.T) {
	for _, c := range fsCases {
		var v Root
		fillRoot(&v, c)
		want := c.want(true)
		values := []*Root{&v}
		if !c.finite {
			sink := make([]byte, 4096)
			if RootMeasure(&v) != -1 || RootSave(&v, sink) != -1 {
				t.Errorf("root %s: the variable writer did not refuse", c.name)
			}
			if RootMeasureMessages(values, nil) != -1 || RootSaveMessages(values, sink, nil) != -1 {
				t.Errorf("root %s: the message writer did not refuse", c.name)
			}
			if RootToJSONMeasure(&v) != -1 || RootToJSON(&v, sink) != -1 {
				t.Errorf("root %s: the text writer did not refuse", c.name)
			}
			if _, err := RootSaveText(&v); err == nil {
				t.Errorf("root %s: SaveText wrote it; its carrier is the binary wire", c.name)
			}
			continue
		}
		n := RootMeasure(&v)
		if n <= 0 {
			t.Fatalf("root %s: measure %d", c.name, n)
		}
		wire := make([]byte, n)
		if RootSave(&v, wire) != n {
			t.Fatalf("root %s: save", c.name)
		}
		var got Root
		var rep TableReport
		if !RootLoadTolerant(&got, wire, &rep) {
			t.Fatalf("root %s: load %+v", c.name, rep)
		}
		landed(t, "root wire", c, rootBits(&got), want)
		clean(t, "root wire", c, &rep)
		strict, err := RootReadStrict(wire)
		if err != nil {
			t.Fatalf("root %s: strict read %v", c.name, err)
		}
		landed(t, "root strict", c, rootBits(strict), want)
		text, err := RootSaveText(&v)
		if err != nil {
			t.Fatalf("root %s: SaveText %v", c.name, err)
		}
		if _, err := RootLoadText(text, nil); err != nil {
			t.Fatalf("root %s: LoadText %v", c.name, err)
		}
		// Every writer writes what a read accepted, and the text lands the
		// same bits as the wire: one value, every form.
		m := RootToJSONMeasure(strict)
		if m <= 0 {
			t.Fatalf("root %s: ToJSONMeasure %d", c.name, m)
		}
		js := make([]byte, m)
		if RootToJSON(strict, js) != m {
			t.Fatalf("root %s: ToJSON", c.name)
		}
		var fromText Root
		var trep TableReport
		if !RootFromJSON(&fromText, js, &trep) {
			t.Fatalf("root %s: FromJSON %+v of %s", c.name, trep, js)
		}
		landed(t, "root text", c, rootBits(&fromText), want)
		clean(t, "root text", c, &trep)
		// THE MESSAGE FORM elides by the same bit compare and carries the pattern.
		var wrep TableReport
		buf := make([]byte, RootMeasureMessages(values, nil))
		if RootSaveMessages(values, buf, &wrep) != int64(len(buf)) || wrep.Clamped != 0 {
			t.Fatalf("message %s: save %+v", c.name, wrep)
		}
		msgs := make([]Root, 1)
		var mrep TableReport
		if k, ok := RootLoadMessagesTolerant(msgs, vocabulary(t), buf, &mrep); k != 1 || !ok {
			t.Fatalf("message %s: load %d %v %+v", c.name, k, ok, mrep)
		}
		landed(t, "message", c, rootBits(&msgs[0]), want)
		clean(t, "message", c, &mrep)
	}
}

// THE VARIABLE CLASS: the builder-backed read, the strict read and text.
func TestVar(t *testing.T) {
	for _, c := range fsCases {
		var v Var
		fillVar(&v, c)
		want := c.want(true)
		if !c.finite {
			sink := make([]byte, 4096)
			if VarMeasure(&v) != -1 || VarSave(&v, sink) != -1 {
				t.Errorf("var %s: the variable writer did not refuse", c.name)
			}
			if VarToJSONMeasure(&v) != -1 {
				t.Errorf("var %s: the text writer did not refuse", c.name)
			}
			continue
		}
		n := VarMeasure(&v)
		if n <= 0 {
			t.Fatalf("var %s: measure %d", c.name, n)
		}
		wire := make([]byte, n)
		if VarSave(&v, wire) != n {
			t.Fatalf("var %s: save", c.name)
		}
		var b VarBuilder
		if !b.Init() {
			t.Fatal("builder")
		}
		var rep TableReport
		if !VarLoadBuilderTolerant(&b, wire, &rep) {
			t.Fatalf("var %s: load %+v", c.name, rep)
		}
		landed(t, "var wire", c, varBits(b.GetRoot()), want)
		clean(t, "var wire", c, &rep)
		b.Shutdown()
		strict, err := VarReadStrict(wire)
		if err != nil {
			t.Fatalf("var %s: strict read %v", c.name, err)
		}
		landed(t, "var strict", c, varBits(strict), want)
		m := VarToJSONMeasure(strict)
		if m <= 0 {
			t.Fatalf("var %s: ToJSONMeasure %d", c.name, m)
		}
		js := make([]byte, m)
		if VarToJSON(strict, js) != m {
			t.Fatalf("var %s: ToJSON", c.name)
		}
		var tb VarBuilder
		tb.Init()
		var trep TableReport
		if !VarFromJSON(&tb, js, &trep) {
			t.Fatalf("var %s: FromJSON %+v of %s", c.name, trep, js)
		}
		landed(t, "var text", c, varBits(tb.GetRoot()), want)
		clean(t, "var text", c, &trep)
		tb.Shutdown()
	}
}

// A HAND-BUILT WIRE: one float payload flipped to a special value, in every
// float position of both classes — a float32 field, a float64 field, a
// counted array's element, a fixed array's element and a union arm. A finite
// value lands as its bits; a NaN or an infinity is refused by the tolerant
// read, the strict read and the builder read alike, naming its own field, and
// lands nothing.
func TestFlippedPayload(t *testing.T) {
	half64, half32 := le64(f64(0.5)), le32(math.Float32bits(0.5))
	sites := []struct {
		field string
		set   func(r *Root, v *Var)
		bits  func(r *Root) uint64
		wide  bool
	}{
		{"a", func(r *Root, v *Var) { r.A, v.A = 0.5, 0.5 }, func(r *Root) uint64 { return uint64(math.Float32bits(r.A)) }, false},
		{"b", func(r *Root, v *Var) { r.B, v.B = 0.5, 0.5 }, func(r *Root) uint64 { return f64(r.B) }, true},
		{"xs", func(r *Root, v *Var) { r.Xs[1], r.XsCount, v.Xs[1], v.XsCount = 0.5, 2, 0.5, 2 }, func(r *Root) uint64 { return f64(r.Xs[1]) }, true},
		{"ys", func(r *Root, v *Var) { r.Ys[0], v.Ys[0] = 0.5, 0.5 }, func(r *Root) uint64 { return f64(r.Ys[0]) }, true},
	}
	for _, c := range fsCases {
		for _, s := range sites {
			var r Root
			var v Var
			RootReset(&r)
			VarReset(&v)
			s.set(&r, &v)
			from, to := half64, le64(c.b64)
			if !s.wide {
				from, to = half32, le32(c.b32)
			}
			wire := make([]byte, RootMeasure(&r))
			RootSave(&r, wire)
			wire = flipped(t, wire, from, to)
			vwire := make([]byte, VarMeasure(&v))
			VarSave(&v, vwire)
			vwire = flipped(t, vwire, from, to)
			var got Root
			var rep TableReport
			ok := RootLoadTolerant(&got, wire, &rep)
			_, strictErr := RootReadStrict(wire)
			vstrict, vErr := VarReadStrict(vwire)
			var b VarBuilder
			b.Init()
			var brep TableReport
			bok := VarLoadBuilderTolerant(&b, vwire, &brep)
			b.Shutdown()
			if c.finite {
				want := uint64(c.b64)
				if !s.wide {
					want = uint64(c.b32)
				}
				if !ok || s.bits(&got) != want || strictErr != nil || vErr != nil || !bok {
					t.Errorf("flip %s %s: ok %v landed %#x strict %v var strict %v builder %v %+v", s.field, c.name, ok, s.bits(&got), strictErr, vErr, bok, brep)
				}
				_ = vstrict
				continue
			}
			refused(t, "root flip "+s.field+" "+c.name, ok, &rep, "Root."+s.field)
			var reset Root
			RootReset(&reset)
			if rootBits(&got) != rootBits(&reset) {
				t.Errorf("root flip %s %s: the refused read landed %#x", s.field, c.name, rootBits(&got))
			}
			strictRefused(t, "root flip "+s.field+" "+c.name, strictErr)
			strictRefused(t, "var flip "+s.field+" "+c.name, vErr)
			refused(t, "var builder flip "+s.field+" "+c.name, bok, &brep, "Var."+s.field)
		}
	}
	// a UNION ARM is named by the union that declares it
	var r Root
	RootReset(&r)
	r.U.Type = ShapeTypeR
	r.U.R().Value = 0.5
	wire := make([]byte, RootMeasure(&r))
	RootSave(&r, wire)
	var got Root
	var rep TableReport
	ok := RootLoadTolerant(&got, flipped(t, wire, half64, le64(0x7FF8000000000000)), &rep)
	refused(t, "union arm", ok, &rep, "Shape.r")
}

// THE MESSAGE FORM refuses on the raw bits too: a body holding a non-finite
// float refuses the batch, the tolerant read included, with a count of 0.
func TestMessageFlipped(t *testing.T) {
	var v Root
	RootReset(&v)
	v.Xs[2], v.XsCount = 0.5, 3
	values := []*Root{&v}
	buf := make([]byte, RootMeasureMessages(values, nil))
	if RootSaveMessages(values, buf, nil) != int64(len(buf)) {
		t.Fatal("message save")
	}
	for _, bits := range []uint64{0x7FF8000000000000, 0xFFF0000000000000, 0x7FF00DEFACED0001} {
		bad := flippedBits(t, buf, f64(0.5), bits, 64)
		msgs := make([]Root, 1)
		var mrep TableReport
		k, ok := RootLoadMessagesTolerant(msgs, vocabulary(t), bad, &mrep)
		if k != 0 {
			t.Errorf("message %#x: delivered %d bodies", bits, k)
		}
		refused(t, "message", ok, &mrep, "Root.xs")
		var strict TableReport
		if k, ok := RootLoadMessages(msgs, vocabulary(t), bad, &strict); k != 0 || ok || !errors.Is(strict.Err(), TableRefuseNonFiniteFloat) {
			t.Errorf("strict message %#x: %d %v %v", bits, k, ok, strict.Err())
		}
	}
}

// A TEXT SPELLING -0 BY HAND keeps the sign in every plain float slot.
func TestTextMinusZeroByHand(t *testing.T) {
	text := []byte("{\"a\": -0, \"b\": -0.0, \"xs\": [-0, -0e0, -0.0], \"ys\": [-0, -0]}\n")
	var got Root
	var rep TableReport
	if !RootFromJSON(&got, text, &rep) {
		t.Fatalf("text -0 by hand: %+v", rep)
	}
	w := fsCases[1].want(false)
	w.r = 0xDEAD
	landed(t, "text by hand", fsCases[1], rootBits(&got), w)
	clean(t, "text by hand", fsCases[1], &rep)
	wire := make([]byte, RootMeasure(&got))
	RootSave(&got, wire)
	s, err := RootReadStrict(wire)
	if err != nil {
		t.Fatal(err)
	}
	landed(t, "text by hand -> wire", fsCases[1], rootBits(s), w)
}
`

// THE FIXED FORM (§3.4) is emitted for a unit of fixed tables alone, so it is
// a unit of its own: one fixed-size table, its fixed form and its variable wire.
const fsFlatSchema = `package probe

fixed table Flat
{
    a float32
    b float64
    ys [2]float64
}
`

const fsFlatProbe = `package probe

import (
	"bytes"
	"errors"
	"math"
	"testing"
)

type fsCase struct {
	name   string
	b32    uint32
	b64    uint64
	finite bool
}

var fsCases = []fsCase{
	{"plus_zero", 0x00000000, 0x0000000000000000, true},
	{"minus_zero", 0x80000000, 0x8000000000000000, true},
	{"nan_quiet", 0x7FC00000, 0x7FF8000000000000, false},
	{"nan_payload", 0xFFA5A5A5, 0x7FF00DEFACED0001, false},
	{"plus_inf", 0x7F800000, 0x7FF0000000000000, false},
	{"minus_inf", 0xFF800000, 0xFFF0000000000000, false},
	{"largest_subnormal", 0x007FFFFF, 0x000FFFFFFFFFFFFF, true},
}

type fsBits struct{ a uint32; b, y0, y1 uint64 }

func f64(v float64) uint64 { return math.Float64bits(v) }

func flatBits(v *Flat) fsBits {
	return fsBits{a: math.Float32bits(v.A), b: f64(v.B), y0: f64(v.Ys[0]), y1: f64(v.Ys[1])}
}

func le64(v uint64) []byte {
	var b [8]byte
	for i := range b {
		b[i] = byte(v >> (8 * i))
	}
	return b[:]
}

// THE FIXED FORM carries the record whole: a finite pattern, -0 and the
// subnormals included, lands as written and nothing counts; the writer
// refuses a NaN or an infinity, and a record that carries one anyway is
// refused by the strict and the tolerant load alike, naming the field, with
// every value the call wrote back at its defaults.
func TestFixed(t *testing.T) {
	for _, c := range fsCases {
		f := math.Float64frombits(c.b64)
		var v Flat
		FlatReset(&v)
		v.A, v.B, v.Ys = math.Float32frombits(c.b32), f, [2]float64{f, f}
		buf := make([]byte, FlatFixedMeasure(1))
		if !c.finite {
			if FlatFixedSave([]Flat{v}, buf) != -1 {
				t.Errorf("fixed %s: the writer wrote a non-finite float", c.name)
			}
			if _, err := FlatFixedSaveReason([]Flat{v}, buf); !errors.Is(err, TableRefuseInvalidValue) {
				t.Errorf("fixed %s: the writer's reason %v, want invalid_value", c.name, err)
			}
			if FlatMeasure(&v) != -1 {
				t.Errorf("fixed %s: the variable writer did not refuse", c.name)
			}
			continue
		}
		if FlatFixedSave([]Flat{v}, buf) < 0 {
			t.Fatalf("fixed %s: the writer refused a finite float", c.name)
		}
		var got [1]Flat
		var plan [64]TableFixedEntry
		var rep TableReport
		if k := FlatFixedLoad(got[:], buf, plan[:], &rep); k != 1 || rep.Err() != nil {
			t.Fatalf("fixed %s: load %d %q", c.name, k, rep.Reason)
		}
		if flatBits(&got[0]) != (fsBits{c.b32, c.b64, c.b64, c.b64}) {
			t.Errorf("fixed %s: landed %#x", c.name, flatBits(&got[0]))
		}
	}
	// THE READ, over a record a foreign writer could hand it: two records, the
	// second one's float flipped, so the refusal is total over the batch.
	for _, site := range []struct {
		field string
		set   func(v *Flat)
		from  []byte
	}{
		{"a", func(v *Flat) { v.A = 0.5 }, []byte{0, 0, 0, 0x3F}},
		{"b", func(v *Flat) { v.B = 0.5 }, le64(f64(0.5))},
		{"ys", func(v *Flat) { v.Ys[1] = 0.5 }, le64(f64(0.5))},
	} {
		for _, c := range fsCases {
			if c.finite {
				continue
			}
			var one, two Flat
			FlatReset(&one)
			FlatReset(&two)
			one.B = 7
			site.set(&two)
			buf := make([]byte, FlatFixedMeasure(2))
			if FlatFixedSave([]Flat{one, two}, buf) < 0 {
				t.Fatal("fixed save")
			}
			to := le64(c.b64)
			if len(site.from) == 4 {
				to = []byte{byte(c.b32), byte(c.b32 >> 8), byte(c.b32 >> 16), byte(c.b32 >> 24)}
			}
			i := bytes.Index(buf, site.from)
			if i < 0 || bytes.Index(buf[i+len(site.from):], site.from) >= 0 {
				t.Fatalf("%s: want one % x in the file", site.field, site.from)
			}
			copy(buf[i:], to)
			for _, tolerant := range []bool{false, true} {
				got := make([]Flat, 2)
				var plan [64]TableFixedEntry
				var rep TableReport
				var k int64
				if tolerant {
					k = FlatFixedLoadTolerant(got, buf, plan[:], &rep)
				} else {
					k = FlatFixedLoad(got, buf, plan[:], &rep)
				}
				if k != -1 || rep.NonFiniteFloat != "Flat."+site.field || rep.Reason != "non_finite_float: Flat."+site.field || !errors.Is(rep.Err(), TableRefuseNonFiniteFloat) {
					t.Errorf("fixed %s %s tolerant=%v: %d %+v", site.field, c.name, tolerant, k, rep)
				}
				var reset Flat
				FlatReset(&reset)
				if flatBits(&got[0]) != flatBits(&reset) || flatBits(&got[1]) != flatBits(&reset) {
					t.Errorf("fixed %s %s: the refused batch kept %#x %#x", site.field, c.name, flatBits(&got[0]), flatBits(&got[1]))
				}
			}
		}
	}
}
`

// THE ENUM-KEYED SLOT elides by the same bit compare (write.go's keyed
// elision): a -0 in one slot rides and a +0 is elided; the writer refuses a
// non-finite slot, and a read refuses one by name.
const fsKeyedSchema = `package probe

enum Level { Low, High }

fixed table Keyed
{
    ks [Level]float64
}
`

const fsKeyedProbe = `package probe

import (
	"bytes"
	"errors"
	"math"
	"testing"
)

func TestKeyed(t *testing.T) {
	var empty Keyed
	KeyedReset(&empty)
	base := KeyedMeasure(&empty)
	for _, c := range []struct {
		name   string
		b64    uint64
		finite bool
	}{
		{"plus_zero", 0, true},
		{"minus_zero", 0x8000000000000000, true},
		{"nan_payload", 0x7FF00DEFACED0001, false},
		{"minus_inf", 0xFFF0000000000000, false},
	} {
		var v Keyed
		KeyedReset(&v)
		v.Ks[1] = math.Float64frombits(c.b64)
		if !c.finite {
			if KeyedMeasure(&v) != -1 {
				t.Errorf("keyed %s: the writer did not refuse", c.name)
			}
			// the read, over a wire whose slot was flipped after the write
			v.Ks[1] = 0.5
			wire := make([]byte, KeyedMeasure(&v))
			KeyedSave(&v, wire)
			half := []byte{0, 0, 0, 0, 0, 0, 0xE0, 0x3F}
			i := bytes.Index(wire, half)
			if i < 0 {
				t.Fatal("no 0.5 in the keyed wire")
			}
			for k := range 8 {
				wire[i+k] = byte(c.b64 >> (8 * k))
			}
			var got Keyed
			var rep TableReport
			if KeyedLoadTolerant(&got, wire, &rep) || rep.Reason != "non_finite_float: Keyed.ks" || math.Float64bits(got.Ks[1]) != 0 {
				t.Errorf("keyed %s: tolerant %+v landed %#x", c.name, rep, math.Float64bits(got.Ks[1]))
			}
			if _, err := KeyedReadStrict(wire); !errors.Is(err, TableRefuseNonFiniteFloat) {
				t.Errorf("keyed %s: strict %v", c.name, err)
			}
			continue
		}
		wire := make([]byte, KeyedMeasure(&v))
		KeyedSave(&v, wire)
		if rides := int64(len(wire)) > base; rides != (c.b64 != 0) {
			t.Errorf("keyed %s: %d bytes against the empty %d, want the slot to ride exactly when its bits are not +0", c.name, len(wire), base)
		}
		var got Keyed
		var rep TableReport
		if !KeyedLoadTolerant(&got, wire, &rep) || math.Float64bits(got.Ks[1]) != c.b64 || math.Float64bits(got.Ks[0]) != 0 {
			t.Errorf("keyed %s: landed %#x %#x %+v", c.name, math.Float64bits(got.Ks[0]), math.Float64bits(got.Ks[1]), rep)
		}
		if _, err := KeyedReadStrict(wire); err != nil {
			t.Errorf("keyed %s: strict %v", c.name, err)
		}
	}
}
`

// fsNegSchema declares a -0 default (docs/SPEC-TABLES.md §3.3, "-0 IS A VALUE,
// AND IT RIDES"): Reset stores -0's own bits, a -0 under it is elided, and a
// +0 under it rides, in a fixed and a variable table; a compressed float's
// declared -0 default is +0 (the compressed rule of §3.3).
const fsNegSchema = `package probe

fixed table Neg
{
    a float32 = -0.0
    b float64 = -0.0
}

table NegV
{
    a float32 = -0.0
    b float64 = -0.0
}

table NegC
{
    c float32 = -0.0 | min = -1, max = 1, resolution = 0.01
}
`

const fsNegProbe = `package probe

import (
	"math"
	"testing"
)

func TestNegativeZeroDefault(t *testing.T) {
	var r Neg
	NegReset(&r)
	if math.Float32bits(r.A) != 0x80000000 || math.Float64bits(r.B) != 0x8000000000000000 {
		t.Fatalf("fixed reset: %#x %#x, want the -0 default's own bits", math.Float32bits(r.A), math.Float64bits(r.B))
	}
	var w NegV
	NegVReset(&w)
	if math.Float32bits(w.A) != 0x80000000 || math.Float64bits(w.B) != 0x8000000000000000 {
		t.Fatalf("variable reset: %#x %#x, want the -0 default's own bits", math.Float32bits(w.A), math.Float64bits(w.B))
	}
	// a COMPRESSED float keeps the compressed rule of 3.3, -0 IS +0: its
	// declared -0 default resets to +0, and a +0 under it reads back +0
	var cz NegC
	NegCReset(&cz)
	if math.Float32bits(cz.C) != 0 {
		t.Errorf("compressed reset: %#x, want +0 (a compressed -0 is +0)", math.Float32bits(cz.C))
	}
	czWire := make([]byte, NegCMeasure(&cz))
	NegCSave(&cz, czWire)
	if gotC, err := NegCReadStrict(czWire); err != nil || math.Float32bits(gotC.C) != 0 {
		t.Errorf("compressed default: strict read %v", err)
	}
	empty := int64(NegMeasure(&r))
	emptyV := int64(NegVMeasure(&w))
	for _, c := range []struct {
		name string
		b32  uint32
		b64  uint64
	}{
		{"minus_zero", 0x80000000, 0x8000000000000000},
		{"plus_zero", 0, 0},
	} {
		rides := c.b64 == 0
		var v Neg
		NegReset(&v)
		v.A, v.B = math.Float32frombits(c.b32), math.Float64frombits(c.b64)
		wire := make([]byte, NegMeasure(&v))
		NegSave(&v, wire)
		if got := int64(len(wire)) > empty; got != rides {
			t.Errorf("fixed %s: %d bytes against the default's %d, want it to ride exactly when its bits differ from -0", c.name, len(wire), empty)
		}
		var got Neg
		var rep TableReport
		if !NegLoad(&got, wire, &rep) || math.Float32bits(got.A) != c.b32 || math.Float64bits(got.B) != c.b64 || rep.Err() != nil {
			t.Errorf("fixed %s: landed %#x %#x %+v", c.name, math.Float32bits(got.A), math.Float64bits(got.B), rep)
		}
		var vv NegV
		NegVReset(&vv)
		vv.A, vv.B = math.Float32frombits(c.b32), math.Float64frombits(c.b64)
		wireV := make([]byte, NegVMeasure(&vv))
		NegVSave(&vv, wireV)
		if got := int64(len(wireV)) > emptyV; got != rides {
			t.Errorf("variable %s: %d bytes against the default's %d, want it to ride exactly when its bits differ from -0", c.name, len(wireV), emptyV)
		}
		gotV, err := NegVReadStrict(wireV)
		if err != nil || math.Float32bits(gotV.A) != c.b32 || math.Float64bits(gotV.B) != c.b64 {
			t.Errorf("variable %s: strict read %v", c.name, err)
		}
	}
}
`

func TestGoPlainFloatSpecialValuesAgreeAcrossForms(t *testing.T) {
	t.Parallel()
	runGenerated(t, fsSchema, fsProbe)
	runGenerated(t, fsFlatSchema, fsFlatProbe)
	runGenerated(t, fsKeyedSchema, fsKeyedProbe)
	runGenerated(t, fsNegSchema, fsNegProbe)
}

// TestGoFiniteFloatNegativeControl is the finite rule's reversed witness: each
// of its three halves is sabotaged in the generated unit, one at a time, and
// the probe above must go red on exactly that half — the raw-bits test every
// variable and message read runs, the fixed form's finite pass, and the
// writer's clause. A control that cannot go red is not a gate.
func TestGoFiniteFloatNegativeControl(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, schema, probe string
		sabotage            [][2]string
		want                string
	}{
		{"read", fsSchema, fsProbe, [][2]string{
			{"b&0x7f800000 == 0x7f800000", "false && b&0x7f800000 == 0x7f800000 /* SABOTAGED */"},
			{"b&0x7ff0000000000000 == 0x7ff0000000000000", "false && b&0x7ff0000000000000 == 0x7ff0000000000000 /* SABOTAGED */"},
		}, "want the refusal non_finite_float"},
		{"fixed", fsFlatSchema, fsFlatProbe, [][2]string{
			{`FlatFixedFiniteBody(&values[k]); why != ""`, `FlatFixedFiniteBody(&values[k]); false && why != "" /* SABOTAGED */`},
		}, "tolerant="},
		{"writer", fsKeyedSchema, fsKeyedProbe, [][2]string{
			{"value.Ks[i]-value.Ks[i] != 0", "false && value.Ks[i]-value.Ks[i] != 0 /* SABOTAGED */"},
		}, "the writer did not refuse"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, err := runGeneratedEdited(t, c.schema, c.probe, func(files map[string][]byte) {
				for _, s := range c.sabotage {
					patch(t, files, s[0], s[1])
				}
			})
			if err == nil {
				t.Fatalf("NEGATIVE CONTROL FAILED: the %s half of the finite rule is gone and the probe stayed GREEN\n%s", c.name, out)
			}
			if !strings.Contains(string(out), c.want) {
				t.Fatalf("NEGATIVE CONTROL FAILED: the probe went red, but not on the %s half\n%s", c.name, out)
			}
		})
	}
}
