package gotable

import (
	"strings"
	"testing"
)

// TestGoMessageFormFixedOnly enforces docs/SPEC-TABLES.md §3.3:
// "THE FORM IS A FIXED TABLE'S, and that is a declaration, not a derivation:
// fixed table T may ride here, a plain table T may not".
// Only fixed table roots emit message verbs; plain table roots carry none.
func TestGoMessageFormFixedOnly(t *testing.T) {
	t.Parallel()
	const schema = `package probe
table Loose { n int32 }
fixed table Tight { n int32 }
`
	files := generate(t, schema)
	var body string
	for name, data := range files {
		if strings.HasSuffix(name, "Table.go") {
			body += string(data) + "\n"
		}
	}
	if body == "" {
		t.Fatal("generate emitted no table module")
	}

	// Tight is a fixed table root: the four message verbs exist (§3.3). The
	// three batch verbs are functions; LoadRetainMessages is the §6.6 refusal
	// by name, because retention is a region round trip and a fixed table has
	// no region.
	for _, verb := range []string{"MeasureMessages", "SaveMessages", "LoadMessages", "LoadRetainMessages"} {
		if !strings.Contains(body, "Tight"+verb) {
			t.Errorf("fixed table Tight missing message verb Tight%s (docs/SPEC-TABLES.md §3.3)", verb)
		}
	}

	// Loose is a plain table root: all four message verbs are absent (§3.3).
	for _, verb := range []string{"MeasureMessages", "SaveMessages", "LoadMessages", "LoadRetainMessages"} {
		if strings.Contains(body, "Loose"+verb) {
			t.Errorf("plain table Loose carries message verb Loose%s (docs/SPEC-TABLES.md §3.3)", verb)
		}
	}

	t.Run("Caller", func(t *testing.T) {
		const caller = `package probe
import (
	"testing"
)

func TestCaller(t *testing.T) {
	var v Tight
	v.N = 42
	values := []*Tight{&v}
	var report TableReport
	if TightLoadRetainMessages == "" {
		t.Fatal("fixed table Tight has no LoadRetainMessages refusal (§6.6)")
	}
	n := TightMeasureMessages(values, &report)
	if n <= 0 {
		t.Fatalf("measure: %d", n)
	}
	buf := make([]byte, n)
	if TightSaveMessages(values, buf, &report) != n {
		t.Fatalf("save: %+v", report)
	}
	var storage [TableMessageEntriesHere]TableMessageEntry
	var vocab TableVocabulary
	vocab.Init(storage[:])
	ann := make([]byte, AnnounceMeasure())
	Announce(ann)
	if !AnnounceRead(&vocab, ann, &report) {
		t.Fatalf("announce: %+v", report)
	}
	var out [1]Tight
	count, ok := TightLoadMessages(out[:], &vocab, buf, &report)
	if !ok || count != 1 || out[0].N != 42 {
		t.Fatalf("load: count=%d ok=%v out=%+v", count, ok, out[0])
	}
}
`
		runGenerated(t, schema, caller)
	})
}
