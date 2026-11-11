package cstable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A FIELD ID TWICE IN ONE BODY IS DAMAGE on the message form (docs/SPEC-TABLES.md
// §3.3), whether this reader can name the field or not: the read and the skip
// path hold a body to the same rule. The writer announces two int32 fields; the
// reader declares only the first, so the second is skipped. The engine's
// verdict on the same bytes is the oracle, and only verdicts are compared.

const messageDuplicateIdWriter = `package skiptext

fixed table Root {
	a int32
	b int32
}
`

// messageDuplicateIdBatch spells one body of (reference, int32) pairs and its
// end, with the count byte for a batch of one.
func messageDuplicateIdBatch(refBits int, refs []uint64) []byte {
	var bits []uint8
	put := func(val uint64, width int) {
		for b := range width {
			bits = append(bits, uint8((val>>b)&1))
		}
	}
	for i, ref := range refs {
		put(ref, refBits)
		put(uint64(100+i), 32)
	}
	put(0, refBits)
	out := make([]byte, 2+(len(bits)+7)/8)
	out[0] = ir.TableWireMessageForm
	for i, bit := range bits {
		if bit != 0 {
			out[2+(i>>3)] |= uint8(1 << (i & 7))
		}
	}
	return out
}

func messageDuplicateIdFixtures(t *testing.T) (*ir.Unit, []byte, []messageSkipTextCase) {
	t.Helper()
	writer := messageSkipTextUnit(t, messageDuplicateIdWriter)
	reader := messageSkipTextUnit(t, messageSkipTextReader)
	ann := tablewire.Announce(writer)
	var vocabulary tablewire.Vocabulary
	var report tabletext.Report
	if err := vocabulary.AnnounceRead(ann, &report); err != nil {
		t.Fatalf("the engine refused the writer's announcement: %v", err)
	}
	st := writer.Tables["Root"]
	ref := map[string]uint64{}
	for i, e := range vocabulary.Entries() {
		for _, f := range st.Fields {
			if e.Id == ir.TableFieldWireId(f) && e.Kind != 0 {
				ref[f.Name] = uint64(i + 1)
			}
		}
	}
	if ref["a"] == 0 || ref["b"] == 0 {
		t.Fatalf("the writer's vocabulary does not carry both fields: %v", ref)
	}
	bits := vocabulary.RefBits()
	cases := []messageSkipTextCase{
		{"each_once", messageDuplicateIdBatch(bits, []uint64{ref["a"], ref["b"]})},
		{"named_twice", messageDuplicateIdBatch(bits, []uint64{ref["a"], ref["b"], ref["a"]})},
		{"skipped_twice", messageDuplicateIdBatch(bits, []uint64{ref["b"], ref["a"], ref["b"]})},
	}
	return reader, ann, cases
}

func TestCSharpMessageDuplicateFieldId(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK")
	reader, ann, cases := messageDuplicateIdFixtures(t)
	engine := messageSkipTextEngine(t, reader, ann, cases)
	if !engine["each_once"] || engine["named_twice"] || engine["skipped_twice"] {
		t.Fatalf("the engine's verdicts are not the rule's: %v", engine)
	}
	out, err := runCSMessageSkipTextProbe(t, reader, ann, cases, nil)
	if err != nil {
		t.Fatalf("csharp message probe failed: %v\n%s", err, out)
	}
	leg := messageSkipTextVerdicts(t, out, cases)
	for _, c := range cases {
		if leg[c.name] != engine[c.name] {
			t.Errorf("%s: the C# leg reads=%t, the engine reads=%t\n%s", c.name, leg[c.name], engine[c.name], out)
		}
	}
}

// The control takes the repeated-id check out of the generated read and skip
// bodies and requires both repeated batches to read, so the test above is red
// on it.
func TestCSharpMessageDuplicateFieldIdNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK")
	reader, ann, cases := messageDuplicateIdFixtures(t)
	const seenCheck = " || MessageSeen("
	out, err := runCSMessageSkipTextProbe(t, reader, ann, cases, func(files map[string][]byte) {
		replaced := 0
		for name, data := range files {
			s := string(data)
			if n := strings.Count(s, seenCheck); n > 0 {
				files[name] = []byte(strings.ReplaceAll(s, seenCheck, " || false && MessageSeen("))
				replaced += n
			}
		}
		if replaced != 2 {
			t.Fatalf("expected the generated C# message read and skip bodies to check repeated ids, found %d", replaced)
		}
	})
	if err != nil {
		t.Fatalf("control without the repeated-id check failed to compile/run: %v\n%s", err, out)
	}
	leg := messageSkipTextVerdicts(t, out, cases)
	if !leg["each_once"] || !leg["named_twice"] || !leg["skipped_twice"] {
		t.Fatalf("control without the repeated-id check did not read the repeated batches:\n%s", out)
	}
}
