package javatable

// messageform_field_once — a MESSAGE-FORM body names each field id once and
// each keyed slot once (docs/SPEC-TABLES.md §4, §5): a repeated field id, known
// or unknown, and a keyed slot repeated inside one keyed value are DAMAGE, and
// the load refuses the whole batch. The Go message reader (internal/tablewire)
// is the oracle; every verdict below is compared with its decode of the same
// bytes.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const repeatedFieldOnceSchema = `package probe

enum Team { Red, Blue, Green }

fixed table Probe
{
    a        int32
    b        int32
    per_team [Team]int32
}

fixed table Other
{
    z int32
}
`

// repeatedFieldBits writes a message body low-bit-first, the order the form's
// bit stream uses.
type repeatedFieldBits struct {
	data []byte
	n    int
}

func (w *repeatedFieldBits) put(v uint64, bits int) {
	for i := range bits {
		if w.n/8 == len(w.data) {
			w.data = append(w.data, 0)
		}
		if (v>>uint(i))&1 != 0 {
			w.data[w.n/8] |= 1 << uint(w.n%8)
		}
		w.n++
	}
}

func (w *repeatedFieldBits) wire() []byte {
	return append([]byte{ir.TableWireMessageForm, 0}, w.data...)
}

func repeatedFieldByName(t *testing.T, st *ir.Struct, name string) *ir.Field {
	t.Helper()
	for _, f := range st.Fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("%s has no field %s", st.Name, name)
	return nil
}

func repeatedFieldSlot(t *testing.T, slots map[string]uint64, e ir.TableVocabularyEntry) uint64 {
	t.Helper()
	slot, ok := slots[e.Key()]
	if !ok {
		t.Fatalf("the vocabulary names no slot for id %016x kind %d", e.Id, e.Kind)
	}
	return slot
}

type repeatedFieldVerdict struct {
	refused bool
	count   int
	a       int64
	slot    int64
}

func repeatedFieldOracle(t *testing.T, u *ir.Unit, wire []byte) repeatedFieldVerdict {
	t.Helper()
	m := tabletext.NewModel(u)
	insts := []*tabletext.Instance{m.New(m.Lookup("Probe"))}
	v := &tablewire.Vocabulary{}
	if err := v.AnnounceRead(tablewire.Announce(u), &tabletext.Report{}); err != nil {
		t.Fatalf("oracle announcement: %v", err)
	}
	report := &tabletext.Report{}
	count, ok, err := tablewire.DecodeMessages(m, insts, wire, v, report)
	refused := !ok || err != nil || report.Refused
	out := repeatedFieldVerdict{refused: refused, count: count}
	if !refused {
		a, _ := insts[0].FieldByKey("a")
		slot, _ := insts[0].FieldByKey("per_team")
		out.a = a.Cell.I
		if len(slot.Elems) > 0 {
			out.slot = slot.Elems[0].I
		}
	}
	return out
}

func repeatedFieldCase(id string, announce, wire []byte) string {
	return fmt.Sprintf(`
        {
            final byte[] ann = fromHex(%q);
            final byte[] msg = fromHex(%q);
            final TableVocabulary vocab = new TableVocabulary();
            vocab.reset(4096);
            final TableVocabulary.Report arep = new TableVocabulary.Report();
            final boolean announced = TableVocabulary.announceRead(vocab, ann, arep);
            final ProbeFixed.Value[] values = new ProbeFixed.Value[TableMessage.batchMax];
            for (int i = 0; i < values.length; i++) { values[i] = new ProbeFixed.Value(); }
            final TableVocabulary.Report lrep = new TableVocabulary.Report();
            final int count = ProbeMessage.loadMessages(values, values.length, vocab, msg, lrep);
            if (!announced) { fail("announce failed"); }
            System.out.println("CASE %s count=" + count
                + " damaged=" + lrep.damaged + " refused=" + lrep.refused
                + " a=" + values[0].a + " slot=" + values[0].perTeam[0]);
        }
`, hexOf(announce), hexOf(wire), id)
}

func repeatedFieldJava(t *testing.T, u *ir.Unit, id string, announce, wire []byte) string {
	t.Helper()
	out := runMessageBatchProbe(t, u, repeatedFieldCase(id, announce, wire))
	var line string
	for l := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(l, "CASE "+id+" ") {
			if line != "" {
				t.Fatalf("%s: more than one CASE line in:\n%s", id, out)
			}
			line = l
		}
	}
	if line == "" {
		t.Fatalf("%s: no CASE line in:\n%s", id, out)
	}
	return line
}

func TestJavaMessageBodyRepeatedFieldRefuses(t *testing.T) {
	t.Parallel()
	javaToolsRequired(t)

	u := unitFrom(t, repeatedFieldOnceSchema)
	probe := u.Tables["Probe"]
	other := u.Tables["Other"]

	aField := repeatedFieldByName(t, probe, "a")
	keyedField := repeatedFieldByName(t, probe, "per_team")
	a := ir.TableFieldEntry(aField)
	keyed := ir.TableFieldEntry(keyedField)
	unknown := ir.TableFieldEntry(repeatedFieldByName(t, other, "z"))
	announce := tablewire.Announce(u)

	slots := tablewire.MessageVocabularySlots(u)
	refBits := ir.TableMessageRefBits(len(tablewire.MessageVocabulary(u)))
	aSlot := repeatedFieldSlot(t, slots, a)
	keyedSlot := repeatedFieldSlot(t, slots, keyed)
	unknownSlot := repeatedFieldSlot(t, slots, unknown)

	aBits := int(ir.TableMessageValueBits(a.Kind, a.Shape))
	unknownBits := int(ir.TableMessageValueBits(unknown.Kind, unknown.Shape))
	countBits := ir.TableMessageCountBits(keyed.Shape)
	elem := keyed.Shape.Inner
	elemBits := int(ir.TableMessageValueBits(keyed.Shape.Elem, *elem))

	key := ir.TableVocabularyEntry{Id: ir.TableWireId(keyedField.KeyEnumRef.VariantWireName(0))}
	keySlot := repeatedFieldSlot(t, slots, key)

	var once repeatedFieldBits
	once.put(aSlot, refBits)
	once.put(7, aBits)
	once.put(0, refBits)

	var twice repeatedFieldBits
	twice.put(aSlot, refBits)
	twice.put(7, aBits)
	twice.put(aSlot, refBits)
	twice.put(9, aBits)
	twice.put(0, refBits)

	var unknownTwice repeatedFieldBits
	unknownTwice.put(unknownSlot, refBits)
	unknownTwice.put(5, unknownBits)
	unknownTwice.put(unknownSlot, refBits)
	unknownTwice.put(6, unknownBits)
	unknownTwice.put(0, refBits)

	var keyedTwice repeatedFieldBits
	keyedTwice.put(keyedSlot, refBits)
	keyedTwice.put(2, countBits)
	keyedTwice.put(keySlot, refBits)
	keyedTwice.put(11, elemBits)
	keyedTwice.put(keySlot, refBits)
	keyedTwice.put(12, elemBits)
	keyedTwice.put(0, refBits)

	cases := []struct {
		id     string
		wire   []byte
		refuse bool
	}{
		{id: "once", wire: once.wire()},
		{id: "twice", wire: twice.wire(), refuse: true},
		{id: "unknown_twice", wire: unknownTwice.wire(), refuse: true},
		{id: "keyed_twice", wire: keyedTwice.wire(), refuse: true},
	}

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			oracle := repeatedFieldOracle(t, u, tc.wire)
			if oracle.refused != tc.refuse {
				t.Fatalf("%s: the oracle refused=%t, want %t", tc.id, oracle.refused, tc.refuse)
			}
			if !tc.refuse {
				if oracle.count != 1 || oracle.a != 7 {
					t.Fatalf("%s: the oracle delivered count=%d a=%d, want count=1 a=7", tc.id, oracle.count, oracle.a)
				}
			}
			line := repeatedFieldJava(t, u, tc.id, announce, tc.wire)
			javaRefused := strings.Contains(line, " count=0 ") &&
				(strings.Contains(line, " damaged=true ") || strings.Contains(line, " refused=true "))
			if javaRefused != oracle.refused {
				t.Errorf("%s: Java refused=%t, the oracle refused=%t: %s", tc.id, javaRefused, oracle.refused, line)
			}
			if !oracle.refused {
				want := fmt.Sprintf("count=%d damaged=false refused=false a=%d slot=%d", oracle.count, oracle.a, oracle.slot)
				if !strings.Contains(line, " "+want) {
					t.Errorf("%s: Java did not load the oracle's value, want %q: %s", tc.id, want, line)
				}
			}
		})
	}
}
