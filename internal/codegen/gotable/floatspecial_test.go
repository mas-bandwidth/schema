package gotable

// A PLAIN FLOAT'S SPECIAL VALUES, IN EVERY FORM (docs/SPEC-TABLES.md §3.3, "A
// PLAIN FLOAT'S SPECIAL VALUES TAKE ONE RULE"). A float32 or float64 with no
// declared range rides every binary form as its bit pattern (§3), and the rule
// settles the two places the forms used to disagree:
//
//   - -0 is a value: the default elision compares BIT PATTERNS, so a -0 rides
//     the variable and message forms instead of being elided and read back as
//     +0, and the text reads -0 as -0. Found by the schema Go fuzz campaign:
//     {"echo":-0} read from JSON wrote -0 as text and +0 through the wire.
//   - a NaN or an infinity rides the binary forms bit for bit, the tolerant
//     variable read names it non_finite in Reason and counts nothing, and the
//     STRICT read refuses it (TableRefuseNonFinite), because the JSON writer
//     has no spelling for one (§16.3). Found by the same campaign: ReadStrict
//     accepted a NaN that ToJsonMeasure then refused.
//
// So a value ReadStrict accepts is one Save, ToJson and SaveText all write.
// Every assertion is on the RAW BITS: -0 == +0 and NaN != NaN under ==.

import "testing"

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
	"strings"
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
	{"plus_inf", 0x7F800000, 0x7FF0000000000000, false},
	{"minus_inf", 0xFF800000, 0xFFF0000000000000, false},
	{"largest_subnormal", 0x007FFFFF, 0x000FFFFFFFFFFFFF, true},
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

// silent: a special value is an ordinary value to every counter.
func silent(t *testing.T, form string, c fsCase, r *TableReport) {
	t.Helper()
	if r.Clamped != 0 || r.KindMismatch != 0 || r.Unknown != 0 || r.Widened != 0 || r.Duplicate != 0 || r.Malformed {
		t.Errorf("%s %s: counted %+v, want every counter zero", form, c.name, *r)
	}
}

// named: the tolerant variable read names a non-finite float and nothing else.
func named(t *testing.T, form string, c fsCase, r *TableReport) {
	t.Helper()
	silent(t, form, c, r)
	if c.finite {
		if r.Reason != "" || r.Err() != nil {
			t.Errorf("%s %s: Reason %q Err %v, want a clean read", form, c.name, r.Reason, r.Err())
		}
		return
	}
	if !(strings.HasPrefix(r.Reason, "non_finite: Root.") || strings.HasPrefix(r.Reason, "non_finite: Var.")) || !errors.Is(r.Err(), TableRefuseNonFinite) {
		t.Errorf("%s %s: Reason %q Err %v, want non_finite named as <Table>.<field> and refused", form, c.name, r.Reason, r.Err())
	}
}

func strictRefused(t *testing.T, form string, c fsCase, err error) {
	t.Helper()
	if !errors.Is(err, TableRefuseNonFinite) || err.Error() != "non_finite" {
		t.Errorf("%s %s: strict read answered %v, want the refusal non_finite", form, c.name, err)
	}
}

// THE FIXED CLASS, variable wire, strict read, text and message forms.
func TestRoot(t *testing.T) {
	for _, c := range fsCases {
		var v Root
		fillRoot(&v, c)
		want := c.want(true)
		// The variable wire carries every pattern: Save never refuses one.
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
		if !RootLoad(&got, wire, &rep) {
			t.Fatalf("root %s: load %+v", c.name, rep)
		}
		landed(t, "root wire", c, rootBits(&got), want)
		named(t, "root wire", c, &rep)
		strict, err := RootReadStrict(wire)
		text, textErr := RootSaveText(&v)
		if textErr != nil {
			t.Errorf("root %s: SaveText refused %v; the base64 carrier is the binary wire", c.name, textErr)
		}
		_, loadTextErr := RootLoadText(text, nil)
		if !c.finite {
			strictRefused(t, "root strict", c, err)
			strictRefused(t, "root LoadText", c, loadTextErr)
			if m := RootToJsonMeasure(&v); m != -1 {
				t.Errorf("root %s: ToJsonMeasure %d, want the refusal -1", c.name, m)
			}
			var sink [1024]byte
			if m := RootToJson(&v, sink[:]); m != -1 {
				t.Errorf("root %s: ToJson %d, want the refusal -1", c.name, m)
			}
		} else {
			if err != nil || loadTextErr != nil {
				t.Fatalf("root %s: strict read %v, LoadText %v", c.name, err, loadTextErr)
			}
			landed(t, "root strict", c, rootBits(strict), want)
			// Every writer writes what the strict read accepted.
			m := RootToJsonMeasure(strict)
			if m <= 0 || RootMeasure(strict) <= 0 {
				t.Fatalf("root %s: a strict-clean value refused by a writer (json %d)", c.name, m)
			}
			if _, err := RootSaveText(strict); err != nil {
				t.Errorf("root %s: SaveText of a strict-clean value: %v", c.name, err)
			}
			js := make([]byte, m)
			if RootToJson(strict, js) != m {
				t.Fatalf("root %s: ToJson", c.name)
			}
			// The text lands the same bits as the wire, and so does the text
			// read saved back to the wire: one value, every form.
			var fromText Root
			var trep TableReport
			if !RootFromJson(&fromText, js, &trep) {
				t.Fatalf("root %s: FromJson %+v of %s", c.name, trep, js)
			}
			landed(t, "root text", c, rootBits(&fromText), want)
			named(t, "root text", c, &trep)
			back := make([]byte, RootMeasure(&fromText))
			RootSave(&fromText, back)
			again, err := RootReadStrict(back)
			if err != nil {
				t.Fatalf("root %s: text -> wire -> strict: %v", c.name, err)
			}
			landed(t, "root text->wire", c, rootBits(again), want)
		}
		// THE MESSAGE FORM elides by the same bit compare and carries the pattern.
		values := []*Root{&v}
		var wrep TableReport
		buf := make([]byte, RootMeasureMessages(values, nil))
		if RootSaveMessages(values, buf, &wrep) != int64(len(buf)) {
			t.Fatalf("message %s: save", c.name)
		}
		if wrep.Clamped != 0 {
			t.Errorf("message %s: the writer counted %d clamps, want 0", c.name, wrep.Clamped)
		}
		ann := make([]byte, AnnounceMeasure())
		Announce(ann)
		var storage [64]TableMessageEntry
		var vocab TableVocabulary
		vocab.Init(storage[:])
		var arep TableReport
		if !AnnounceRead(&vocab, ann, &arep) {
			t.Fatalf("announce read: %+v", arep)
		}
		msgs := make([]Root, 1)
		var mrep TableReport
		if k, ok := RootLoadMessages(msgs, &vocab, buf, &mrep); k != 1 || !ok {
			t.Fatalf("message %s: load %d %v %+v", c.name, k, ok, mrep)
		}
		landed(t, "message", c, rootBits(&msgs[0]), want)
		named(t, "message", c, &mrep)
	}
}

// THE VARIABLE CLASS: the builder-backed read, strict read and text.
func TestVar(t *testing.T) {
	for _, c := range fsCases {
		var v Var
		fillVar(&v, c)
		want := c.want(true)
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
		if !VarLoadBuilder(&b, wire, &rep) {
			t.Fatalf("var %s: load %+v", c.name, rep)
		}
		landed(t, "var wire", c, varBits(b.GetRoot()), want)
		named(t, "var wire", c, &rep)
		b.Shutdown()
		strict, err := VarReadStrict(wire)
		text, textErr := VarSaveText(&v)
		if textErr != nil {
			t.Errorf("var %s: SaveText refused %v", c.name, textErr)
		}
		_, loadTextErr := VarLoadText(text, nil)
		if !c.finite {
			strictRefused(t, "var strict", c, err)
			strictRefused(t, "var LoadText", c, loadTextErr)
			if m := VarToJsonMeasure(&v); m != -1 {
				t.Errorf("var %s: ToJsonMeasure %d, want the refusal -1", c.name, m)
			}
			continue
		}
		if err != nil || loadTextErr != nil {
			t.Fatalf("var %s: strict read %v, LoadText %v", c.name, err, loadTextErr)
		}
		landed(t, "var strict", c, varBits(strict), want)
		m := VarToJsonMeasure(strict)
		if m <= 0 {
			t.Fatalf("var %s: ToJsonMeasure of a strict-clean value %d", c.name, m)
		}
		if _, err := VarSaveText(strict); err != nil {
			t.Errorf("var %s: SaveText of a strict-clean value: %v", c.name, err)
		}
		js := make([]byte, m)
		if VarToJson(strict, js) != m {
			t.Fatalf("var %s: ToJson", c.name)
		}
		var tb VarBuilder
		tb.Init()
		var trep TableReport
		if !VarFromJson(&tb, js, &trep) {
			t.Fatalf("var %s: FromJson %+v of %s", c.name, trep, js)
		}
		landed(t, "var text", c, varBits(tb.GetRoot()), want)
		named(t, "var text", c, &trep)
		back := make([]byte, VarMeasure(tb.GetRoot()))
		VarSave(tb.GetRoot(), back)
		again, err := VarReadStrict(back)
		if err != nil {
			t.Fatalf("var %s: text -> wire -> strict: %v", c.name, err)
		}
		landed(t, "var text->wire", c, varBits(again), want)
		tb.Shutdown()
	}
}

// A HAND-BUILT WIRE: one float payload flipped to a NaN or an infinity, as a
// damaged byte or a foreign writer would hand it. The tolerant read lands the
// pattern, the strict read refuses it by name.
func TestFlippedPayload(t *testing.T) {
	half := []byte{0, 0, 0, 0, 0, 0, 0xE0, 0x3F} // 0.5 as a float64, little-endian
	for _, c := range fsCases {
		var to [8]byte
		for i := range to {
			to[i] = byte(c.b64 >> (8 * i))
		}
		patch := func(wire []byte) []byte {
			i := bytes.Index(wire, half)
			if i < 0 || bytes.Index(wire[i+8:], half) >= 0 {
				t.Fatalf("%s: want exactly one 0.5 payload", c.name)
			}
			copy(wire[i:], to[:])
			return wire
		}
		var r Root
		RootReset(&r)
		r.B = 0.5
		wire := make([]byte, RootMeasure(&r))
		RootSave(&r, wire)
		wire = patch(wire)
		var got Root
		var rep TableReport
		if !RootLoad(&got, wire, &rep) || f64(got.B) != c.b64 {
			t.Errorf("root flip %s: landed %#x ok %+v", c.name, f64(got.B), rep)
		}
		named(t, "root flip", c, &rep)
		if _, err := RootReadStrict(wire); c.finite && err != nil {
			t.Errorf("root flip %s: strict %v", c.name, err)
		} else if !c.finite {
			strictRefused(t, "root flip", c, err)
		}
		var v Var
		VarReset(&v)
		v.B = 0.5
		vwire := make([]byte, VarMeasure(&v))
		VarSave(&v, vwire)
		vwire = patch(vwire)
		if s, err := VarReadStrict(vwire); c.finite && (err != nil || f64(s.B) != c.b64) {
			t.Errorf("var flip %s: strict %v", c.name, err)
		} else if !c.finite {
			strictRefused(t, "var flip", c, err)
		}
	}
}

// A HAND-BUILT FLOAT32 WIRE: the same flip on a four-byte payload.
func TestFlippedFloat32Payload(t *testing.T) {
	half := []byte{0, 0, 0, 0x3F} // 0.5 as a float32, little-endian
	for _, c := range fsCases {
		to := []byte{byte(c.b32), byte(c.b32 >> 8), byte(c.b32 >> 16), byte(c.b32 >> 24)}
		var r Root
		RootReset(&r)
		r.A = 0.5
		wire := make([]byte, RootMeasure(&r))
		RootSave(&r, wire)
		i := bytes.Index(wire, half)
		if i < 0 || bytes.Index(wire[i+4:], half) >= 0 {
			t.Fatalf("%s: want exactly one float32 0.5 payload in % x", c.name, wire)
		}
		copy(wire[i:], to)
		var got Root
		var rep TableReport
		if !RootLoad(&got, wire, &rep) || math.Float32bits(got.A) != c.b32 {
			t.Errorf("root flip32 %s: landed %#x ok %+v", c.name, math.Float32bits(got.A), rep)
		}
		named(t, "root flip32", c, &rep)
		if _, err := RootReadStrict(wire); c.finite && err != nil {
			t.Errorf("root flip32 %s: strict %v", c.name, err)
		} else if !c.finite {
			strictRefused(t, "root flip32", c, err)
		}
	}
}

// AN ELEMENT is named by the table that declares its array, in the variable
// wire and in the message form alike (§3.3: non_finite: <Table>.<field>).
func TestElementNamedByItsTable(t *testing.T) {
	var v Root
	RootReset(&v)
	v.Xs, v.XsCount = [3]float64{0, math.NaN(), 0}, 3
	wire := make([]byte, RootMeasure(&v))
	RootSave(&v, wire)
	var got Root
	var rep TableReport
	RootLoad(&got, wire, &rep)
	if rep.Reason != "non_finite: Root.xs" {
		t.Errorf("wire: Reason %q, want non_finite: Root.xs", rep.Reason)
	}
	values := []*Root{&v}
	var wrep TableReport
	buf := make([]byte, RootMeasureMessages(values, nil))
	RootSaveMessages(values, buf, &wrep)
	ann := make([]byte, AnnounceMeasure())
	Announce(ann)
	var storage [64]TableMessageEntry
	var vocab TableVocabulary
	vocab.Init(storage[:])
	var arep TableReport
	if !AnnounceRead(&vocab, ann, &arep) {
		t.Fatalf("announce read: %+v", arep)
	}
	msgs := make([]Root, 1)
	var mrep TableReport
	RootLoadMessages(msgs, &vocab, buf, &mrep)
	if mrep.Reason != "non_finite: Root.xs" || !errors.Is(mrep.Err(), TableRefuseNonFinite) {
		t.Errorf("message: Reason %q Err %v, want non_finite: Root.xs refused", mrep.Reason, mrep.Err())
	}
}

// A TEXT SPELLING -0 BY HAND keeps the sign in every plain float slot.
func TestTextMinusZeroByHand(t *testing.T) {
	text := []byte("{\"a\": -0, \"b\": -0.0, \"xs\": [-0, -0e0, -0.0], \"ys\": [-0, -0]}\n")
	var got Root
	var rep TableReport
	if !RootFromJson(&got, text, &rep) {
		t.Fatalf("text -0 by hand: %+v", rep)
	}
	w := fsCases[1].want(false)
	w.r = 0xDEAD
	landed(t, "text by hand", fsCases[1], rootBits(&got), w)
	silent(t, "text by hand", fsCases[1], &rep)
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
	"errors"
	"math"
	"strings"
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

func landed(t *testing.T, form string, c fsCase, got, want fsBits) {
	t.Helper()
	if got != want {
		t.Errorf("%s %s: landed %#x, want %#x", form, c.name, got, want)
	}
}

func silent(t *testing.T, form string, c fsCase, r *TableReport) {
	t.Helper()
	if r.Clamped != 0 || r.KindMismatch != 0 || r.Unknown != 0 || r.Widened != 0 || r.Duplicate != 0 || r.Malformed {
		t.Errorf("%s %s: counted %+v, want every counter zero", form, c.name, *r)
	}
}

func named(t *testing.T, form string, c fsCase, r *TableReport) {
	t.Helper()
	silent(t, form, c, r)
	if c.finite {
		if r.Reason != "" || r.Err() != nil {
			t.Errorf("%s %s: Reason %q Err %v, want a clean read", form, c.name, r.Reason, r.Err())
		}
		return
	}
	if !strings.HasPrefix(r.Reason, "non_finite: ") || !errors.Is(r.Err(), TableRefuseNonFinite) {
		t.Errorf("%s %s: Reason %q Err %v, want non_finite named and refused", form, c.name, r.Reason, r.Err())
	}
}

// THE FIXED FORM carries the record whole: every pattern lands as written,
// nothing counts, and the writer refuses no plain float.
func TestFixed(t *testing.T) {
	for _, c := range fsCases {
		f := math.Float64frombits(c.b64)
		var v Flat
		FlatReset(&v)
		v.A, v.B, v.Ys = math.Float32frombits(c.b32), f, [2]float64{f, f}
		buf := make([]byte, FlatFixedMeasure(1))
		if FlatFixedSave([]Flat{v}, buf) < 0 {
			t.Fatalf("fixed %s: the writer refused a plain float", c.name)
		}
		var got [1]Flat
		var plan [64]TableFixedEntry
		var rep TableReport
		if k := FlatFixedLoad(got[:], buf, plan[:], &rep); k != 1 {
			t.Fatalf("fixed %s: load %d %q", c.name, k, rep.Reason)
		}
		landed(t, "fixed", c, flatBits(&got[0]), fsBits{c.b32, c.b64, c.b64, c.b64})
		silent(t, "fixed", c, &rep)
		// The variable wire of the same flat value agrees with it.
		wire := make([]byte, FlatMeasure(&v))
		FlatSave(&v, wire)
		var back Flat
		var wrep TableReport
		FlatLoad(&back, wire, &wrep)
		landed(t, "flat wire", c, flatBits(&back), fsBits{c.b32, c.b64, c.b64, c.b64})
		named(t, "flat wire", c, &wrep)
	}
}
`

// THE ENUM-KEYED SLOT elides by the same bit compare (write.go's keyed
// elision): a -0 in one slot rides, a +0 is elided, and the strict read refuses
// a non-finite slot by name.
const fsKeyedSchema = `package probe

enum Level { Low, High }

fixed table Keyed
{
    ks [Level]float64
}
`

const fsKeyedProbe = `package probe

import (
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
		wire := make([]byte, KeyedMeasure(&v))
		KeyedSave(&v, wire)
		if rides := int64(len(wire)) > base; rides != (c.b64 != 0) {
			t.Errorf("keyed %s: %d bytes against the empty %d, want the slot to ride exactly when its bits are not +0", c.name, len(wire), base)
		}
		var got Keyed
		var rep TableReport
		if !KeyedLoad(&got, wire, &rep) || math.Float64bits(got.Ks[1]) != c.b64 || math.Float64bits(got.Ks[0]) != 0 {
			t.Errorf("keyed %s: landed %#x %#x %+v", c.name, math.Float64bits(got.Ks[0]), math.Float64bits(got.Ks[1]), rep)
		}
		_, err := KeyedReadStrict(wire)
		if c.finite && err != nil {
			t.Errorf("keyed %s: strict %v", c.name, err)
		}
		if !c.finite && (!errors.Is(err, TableRefuseNonFinite) || rep.Reason != "non_finite: Keyed.ks") {
			t.Errorf("keyed %s: strict %v, Reason %q, want non_finite: Keyed.ks", c.name, err, rep.Reason)
		}
	}
}
`

func TestGoPlainFloatSpecialValuesAgreeAcrossForms(t *testing.T) {
	t.Parallel()
	runGenerated(t, fsSchema, fsProbe)
	runGenerated(t, fsFlatSchema, fsFlatProbe)
	runGenerated(t, fsKeyedSchema, fsKeyedProbe)
}
