package gotable

// TestGoMessageFormFixedOnly holds the Go emitter to docs/SPEC-TABLES.md §3.3:
// "THE FORM IS A FIXED TABLE'S, and that is a declaration, not a derivation"
// — "`fixed table T` may ride here, a plain `table T` may not, and a
// message-form request naming one is REFUSED naming the table (§11) rather
// than quietly writing another form." The message form's batch verbs
// (MeasureMessages, SaveMessages, LoadMessages, LoadRetainMessages) are
// therefore emitted for a `fixed table` root and for no plain `table` root.

import (
	"strings"
	"testing"
)

func TestGoMessageFormFixedOnly(t *testing.T) {
	t.Parallel()
	const schema = `package probe
table Loose {
	n    int32
	rows []int16
}
fixed table Tight {
	n    int32
	rows [..3]int16
}
`
	// the emitter's own source: a fixed root carries the message form, a plain
	// root carries none of the four verbs, under the value form or the region
	// overloads.
	out := generate(t, schema)
	var source strings.Builder
	for _, data := range out {
		source.Write(data)
	}
	body := source.String()
	for _, verb := range []string{"MeasureMessages", "SaveMessages", "LoadMessages"} {
		if !strings.Contains(body, "func Tight"+verb+"(") {
			t.Errorf("fixed table Tight has no %s: docs/SPEC-TABLES.md §3.3", verb)
		}
	}
	for _, verb := range []string{"MeasureMessages", "SaveMessages", "LoadMessages", "LoadRetainMessages"} {
		if strings.Contains(body, "func Loose"+verb+"(") {
			t.Errorf("plain table Loose carries %s: §3.3 \"a plain `table T` may not\"", verb)
		}
	}

	// and it is a compilation fact: a caller of the fixed root's verbs builds
	runGenerated(t, schema, `package probe
import "testing"
func TestFixedRootCaller(t *testing.T) {
	var v Tight
	values := []*Tight{&v}
	buf := make([]byte, TightMeasureMessages(values, nil))
	var report TableReport
	if TightSaveMessages(values, buf, &report) != int64(len(buf)) {
		t.Fatalf("save: %+v", report)
	}
	var vocabulary TableVocabulary
	vocabulary.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	storage := make([]Tight, 1)
	if _, ok := TightLoadMessages(storage, &vocabulary, buf, &report); ok {
		t.Fatal("an unannounced vocabulary decoded a batch")
	}
}
`)

	// while a caller that asks the plain root for the same form does not build
	if _, err := runGeneratedResult(t, schema, `package probe
func TestPlainRootCaller() {
	var v Loose
	LooseMeasureMessages([]*Loose{&v}, nil)
}
`); err == nil {
		t.Fatal("a plain table compiled a message-form caller: §3.3 \"a plain `table T` may not\"")
	}
}
