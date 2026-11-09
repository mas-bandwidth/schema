package gotable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// The STRICT-100 cells vt/text-form/go, vt/cook-write/go and
// vt/message-form/go over one declared fixed root: every read is the one
// strict read (the whole value, or a refusal that leaves the value at its
// defaults), through the entry point each form owns (docs/SPEC-TABLES.md
// §3.3, §3.4, §6.5, §16.2).

const vtTextCookMessageSchema = `package probe
fixed table Root {
 n int32
 label string(8)
 values [..3]int32
}
`

const vtTextCookMessageProbe = `package probe

import (
	"bytes"
	"testing"
	"unsafe"
)

func TestVTTextCookMessage(t *testing.T) {
	// text-form: a value round-trips through JSON, and a duplicate key —
	// known, unknown, or inside a skipped object, escaped or not — refuses
	// the whole read and leaves the value at its defaults.
	var v Root
	var report TableReport
	if !RootFromJSON(&v, []byte("{\"n\":7,\"label\":\"hi\",\"values\":[1,2]}"), &report) || report != (TableReport{}) {
		t.Fatalf("text read: %+v", report)
	}
	if v.N != 7 || string(v.Label[:v.LabelLength]) != "hi" || v.ValuesCount != 2 || v.Values[1] != 2 {
		t.Fatalf("text value: %+v", v)
	}
	text := make([]byte, RootToJSONMeasure(&v))
	if RootToJSON(&v, text) != int64(len(text)) || !bytes.Contains(text, []byte("\"label\": \"hi\"")) {
		t.Fatalf("text write: %s", text)
	}
	for _, dup := range []string{
		"{\"n\":1,\"n\":2}",
		"{\"zz\":1,\"zz\":2}",
		"{\"zz\":{\"a\":1,\"a\":2}}",
		"{\"zz\":{\"a\":1,\"\\u0061\":2}}",
	} {
		var got Root
		got.N = 99
		var r TableReport
		if RootFromJSON(&got, []byte(dup), &r) || !r.Refused() {
			t.Fatalf("duplicate key was accepted: %s: %+v %+v", dup, got, r)
		}
		var want Root
		RootReset(&want)
		if got != want {
			t.Fatalf("duplicate key left a partial value: %s: %+v want %+v", dup, got, want)
		}
	}

	// cook-write: the cook of the value opens back to the same value, and
	// a short buffer refuses without touching it.
	size := RootCookMeasure(&v)
	if size <= 0 {
		t.Fatalf("cook measure %d", size)
	}
	cooked := bytes.Repeat([]byte{0xa5}, int(size))
	if !RootCookFrom(&v, cooked, TableByteOrderLittle) {
		t.Fatal("cook refused")
	}
	short := bytes.Repeat([]byte{0xa5}, int(size))
	if RootCookFrom(&v, short[:len(short)-1], TableByteOrderLittle) || !bytes.Equal(short, bytes.Repeat([]byte{0xa5}, len(short))) {
		t.Fatal("short cook wrote or was accepted")
	}
	raw := make([]byte, len(cooked)+63)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
	copy(raw[off:], cooked)
	var cook RootCook
	if !RootOpen(&cook, unsafe.Pointer(&raw[off]), int64(len(cooked))) || cook.Root() == nil || cook.Root().N != v.N || cook.Root().ValuesCount != v.ValuesCount {
		t.Fatalf("cook open: %+v", cook.Root())
	}

	// message-form and the fixed file form: the fixed writer writes form
	// 3, the fixed reader reads it whole, a form-1 file is refused as
	// previous_form, and a message batch round-trips through a vocabulary.
	wire := make([]byte, RootFixedMeasure(1))
	if RootFixedSave([]Root{v}, wire) != int64(len(wire)) {
		t.Fatal("fixed save")
	}
	if wire[0] != TableFixedForm {
		t.Fatalf("fixed save form byte %d, want 3", wire[0])
	}
	fixed := make([]Root, 1)
	plan := make([]TableFixedEntry, 4096)
	var fr TableReport
	if RootFixedLoad(fixed, wire, plan, &fr) != 1 || fr != (TableReport{}) || fixed[0] != v {
		t.Fatalf("fixed load: %+v %+v", fixed, fr)
	}
	form1 := make([]byte, RootMeasure(&v))
	if RootSave(&v, form1) != int64(len(form1)) || len(form1) == 0 || form1[0] != 1 {
		t.Fatal("form-1 save for the previous_form refusal")
	}
	if RootFixedLoad(fixed, form1, plan, &fr) != -1 || fr.Reason != "previous_form" {
		t.Fatalf("form-1 file at the fixed reader: %+v", fr)
	}
	vocab := new(TableVocabulary)
	vocab.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	ann := make([]byte, AnnounceMeasure())
	if Announce(ann) != int64(len(ann)) {
		t.Fatal("announce")
	}
	var ar TableReport
	if !AnnounceRead(vocab, ann, &ar) {
		t.Fatalf("announce read: %+v", ar)
	}
	values := []*Root{&v}
	n := RootMeasureMessages(values, nil)
	if n < 2 {
		t.Fatalf("message measure %d", n)
	}
	batch := make([]byte, n)
	var wr TableReport
	if RootSaveMessages(values, batch, &wr) != n || wr != (TableReport{}) {
		t.Fatalf("message save: %+v", wr)
	}
	var got [1]Root
	count, ok := RootLoadMessages(got[:], vocab, batch, &ar)
	if !ok || count != 1 || got[0] != v {
		t.Fatalf("message load: %d %v %+v", count, ok, ar)
	}
	if _, ok := RootLoadMessages(got[:], vocab, batch[:len(batch)-1], &ar); ok {
		t.Fatal("a truncated message batch was accepted")
	}
}
`

func runVTTextCookMessage(t *testing.T, edit func(map[string][]byte)) ([]byte, error) {
	t.Helper()
	return runGeneratedEdited(t, vtTextCookMessageSchema, vtTextCookMessageProbe, edit)
}

func TestGoTextCookMessageCellsStrict(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	out, err := runVTTextCookMessage(t, nil)
	if err != nil {
		t.Fatalf("the text/cook/message cells do not hold: %v\n%s", err, out)
	}
}

// TestGoTextCookMessageCellsStrictNegativeControl plants each fault in a
// copy of the real generated artifact: the skipped-object duplicate
// tracking is blinded, and the fixed writer's form byte is planted back
// to 1. Both must turn the strict gate red on its own message.
func TestGoTextCookMessageCellsStrictNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	t.Run("duplicate_key", func(t *testing.T) {
		t.Parallel()
		out, err := runVTTextCookMessage(t, func(files map[string][]byte) {
			sabotageGenerated(t, files, "skipped-object duplicate tracking", [2]string{"keyLen[keyCount] = len(key)", "keyLen[keyCount] = -1"})
		})
		requireRed(t, "the skipped-object duplicate tracking planted out", out, err, "duplicate key")
	})
	t.Run("file_form", func(t *testing.T) {
		t.Parallel()
		out, err := runVTTextCookMessage(t, func(files map[string][]byte) {
			sabotageGenerated(t, files, "fixed form byte", [2]string{"buffer[0] = TableFixedForm", "buffer[0] = 1"})
		})
		requireRed(t, "the fixed writer's form byte planted to 1", out, err, "form byte")
	})
}
