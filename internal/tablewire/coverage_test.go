package tablewire

import (
	"math"
	"math/big"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestBitBigRoundTrip(t *testing.T) {
	t.Parallel()
	cases := []struct {
		v *big.Int
		n int
	}{
		{big.NewInt(0), 0},
		{big.NewInt(1), 1},
		{big.NewInt(0xDEADBEEF), 32},
		{big.NewInt(1234567890123456789), 63},
		{big.NewInt(0), 128},
	}
	for _, c := range cases {
		w := &bitWriter{}
		w.putBig(c.v, c.n)
		r := newBitReader(w.b)
		got, ok := r.getBig(c.n)
		if !ok || got.Cmp(c.v) != 0 {
			t.Fatalf("putBig/getBig(%v, %d) = %v, %v; want %v", c.v, c.n, got, ok, c.v)
		}
	}
	r := newBitReader([]byte{0xFF})
	if _, ok := r.getBig(16); ok {
		t.Fatal("getBig reading past the stream must fail")
	}
}

func TestWidenNarrowF32(t *testing.T) {
	t.Parallel()
	finites := []float64{0, 1, -1, 0.5, -3.25, math.MaxFloat32, math.SmallestNonzeroFloat32}
	for _, f := range finites {
		bits := math.Float32bits(float32(f))
		if got := WidenF32(bits); got != f {
			t.Fatalf("WidenF32(%x) = %v, want %v", bits, got, f)
		}
		if got := NarrowF32(f); got != bits {
			t.Fatalf("NarrowF32(%v) = %x, want %x", f, got, bits)
		}
	}
	nan := math.Float32frombits(0x7FC00000)
	b := math.Float32bits(nan)
	w := WidenF32(b)
	if !math.IsNaN(w) {
		t.Fatal("WidenF32 of a NaN must be a NaN")
	}
	if got := NarrowF32(w); got != b {
		t.Fatalf("NarrowF32 round trip = %x, want %x", got, b)
	}
	nan2 := math.Float32frombits(0xFF800123)
	b2 := math.Float32bits(nan2)
	w2 := WidenF32(b2)
	if got := NarrowF32(w2); got != b2 {
		t.Fatalf("NarrowF32 signed NaN round trip = %x, want %x", got, b2)
	}
	canonical := math.Float32bits(float32(math.Inf(1)))
	if got := NarrowF32(math.Inf(1)); got != canonical {
		t.Fatalf("NarrowF32(Inf) = %x, want %x", got, canonical)
	}
}

func TestTextHelpers(t *testing.T) {
	t.Parallel()
	if textBoundary([]byte("hello"), 5) != 5 {
		t.Fatal("textBoundary within bound")
	}
	if textBoundary([]byte("héllo"), 3) != 3 {
		t.Fatal("textBoundary should not split a multibyte rune")
	}
	if got := textBoundary([]byte("é"), 1); got != 0 {
		t.Fatalf("textBoundary at a continuation byte = %d, want 0", got)
	}
	units := wideUnits([]byte{0x41, 0x00, 0x42, 0x00})
	if units[0] != 0x41 || units[1] != 0x42 {
		t.Fatalf("wideUnits = %v", units)
	}
	back := wideBytes(units)
	if string(back) != string([]byte{0x41, 0x00, 0x42, 0x00}) {
		t.Fatalf("wideBytes = %v", back)
	}
	if !wideValid([]uint16{0x41, 0x42}) {
		t.Fatal("plain units must be valid")
	}
	if wideValid([]uint16{0x41, 0x00}) {
		t.Fatal("a zero unit must be invalid")
	}
	if wideValid([]uint16{0xD800}) {
		t.Fatal("an unpaired high surrogate must be invalid")
	}
	if wideValid([]uint16{0xD800, 0x0041}) {
		t.Fatal("a high surrogate before a non-low must be invalid")
	}
	if !wideValid([]uint16{0xD800, 0xDC00}) {
		t.Fatal("a paired surrogate must be valid")
	}
	if wideValid([]uint16{0xDC00, 0xD800}) {
		t.Fatal("a leading low surrogate must be invalid")
	}
	if wideBoundary([]uint16{0x41, 0x42}, 2) != 2 {
		t.Fatal("wideBoundary within bound")
	}
	if wideBoundary([]uint16{0xD800, 0xDC00}, 1) != 0 {
		t.Fatal("wideBoundary must drop an unpaired high surrogate")
	}
	if wideBoundary([]uint16{0x41, 0x42}, 1) != 1 {
		t.Fatal("wideBoundary should keep whole units")
	}
}

func TestMessageReasonString(t *testing.T) {
	t.Parallel()
	expect := map[MessageReason]string{
		ReasonNewerForm:        "newer_form",
		ReasonNoVocabulary:     "no_vocabulary",
		ReasonSecondAnnouncement: "second_announcement",
		ReasonVocabularyTooLarge: "vocabulary_too_large",
		ReasonMessageFormAsFile:  "message_form_as_file",
		ReasonBatchTooLarge:      "batch_too_large",
	}
	for r, want := range expect {
		if got := r.String(); got != want {
			t.Fatalf("MessageReason(%d).String() = %q, want %q", r, got, want)
		}
	}
	if got := MessageReason(99).String(); got != "MessageReason(99)" {
		t.Fatalf("unknown reason String() = %q", got)
	}
}

func TestMessageRefusalError(t *testing.T) {
	t.Parallel()
	e := &MessageRefusal{Reason: ReasonNoVocabulary}
	if got := e.Error(); got == "" {
		t.Fatal("MessageRefusal.Error must not be empty without a build version")
	}
	e2 := &MessageRefusal{Reason: ReasonNoVocabulary, BuildVersion: 0xABCD}
	if got := e2.Error(); got == "" {
		t.Fatal("MessageRefusal.Error must not be empty with a build version")
	}
}

func TestFormAndCountRefusalError(t *testing.T) {
	t.Parallel()
	f := &FormRefusal{Form: 9}
	if got := f.Error(); got == "" {
		t.Fatal("FormRefusal.Error must not be empty")
	}
	c := &CountRefusal{}
	if got := c.Error(); got == "" {
		t.Fatal("CountRefusal.Error must not be empty")
	}
}

func TestVocabularyGetters(t *testing.T) {
	t.Parallel()
	v := &Vocabulary{entries: []ir.TableVocabularyEntry{{Id: 1, Kind: 6}}, buildVersion: 7, announced: true}
	if got := v.Entries(); len(got) != 1 {
		t.Fatalf("Entries() = %v", got)
	}
	if got := v.BuildVersion(); got != 7 {
		t.Fatalf("BuildVersion() = %d", got)
	}
}

func TestMessageCount(t *testing.T) {
	t.Parallel()
	v := &Vocabulary{announced: true}
	if n, err := MessageCount([]byte{}, v); err != nil || n != 0 {
		t.Fatalf("MessageCount(empty) = %d, %v", n, err)
	}
	if _, err := MessageCount([]byte{0xAA}, v); err == nil {
		t.Fatal("MessageCount with a non-message form byte must refuse")
	}
	if _, err := MessageCount([]byte{ir.TableWireMessageForm}, nil); err == nil {
		t.Fatal("MessageCount without a vocabulary must refuse")
	}
	if n, err := MessageCount([]byte{ir.TableWireMessageForm, 0x02}, v); err != nil || n != 3 {
		t.Fatalf("MessageCount = %d, %v; want 3", n, err)
	}
}
