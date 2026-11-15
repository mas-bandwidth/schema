package javatable

// message_integer_kind_domain — a MESSAGE-FORM integer rebuilt as the sender's
// offset plus the announced range base is held to THIS reader's own bound
// (docs/SPEC-TABLES.md §3.3). Where the field declares no range that bound is
// the domain of the reader's declared kind, and a value outside it refuses the
// batch rather than landing narrowed in the cell.

import (
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const messageIntegerKindDomainUnit = `package intkinddomain

fixed table Probe
{
    a int8
    b int32
    c uint8
}
`

type messageIntegerKindDomainCase struct {
	id     string
	field  *ir.Field
	kind   uint8
	shape  ir.TableMessageShape
	raw    uint64
	refuse bool
	want   int64
	widens bool
}

// messageIntegerKindDomainAnnouncement announces one entry as a form-1 file,
// the announcement a sender on the other end would deliver.
func messageIntegerKindDomainAnnouncement(e ir.TableVocabularyEntry) []byte {
	vocab := e.Encode(nil)
	return announcement(append(versionField(0x1122334455667788), vocabField(ir.TableKindU8, vocab)...))
}

// messageIntegerKindDomainBatch writes one body: a reference to the announced
// slot, the value at the sender's width, then the body's zero terminator.
func messageIntegerKindDomainBatch(kind uint8, shape ir.TableMessageShape, raw uint64) []byte {
	refBits := ir.TableMessageRefBits(1)
	width := int(ir.TableMessageValueBits(kind, shape))
	var w repeatedFieldBits
	w.put(1, refBits)
	w.put(raw, width)
	w.put(0, refBits)
	return w.wire()
}

// messageIntegerKindDomainOracle is the engine's verdict on the same bytes.
func messageIntegerKindDomainOracle(t *testing.T, u *ir.Unit, ann, wire []byte) (bool, int, map[string]int64) {
	t.Helper()
	v := &tablewire.Vocabulary{}
	var arep tabletext.Report
	if err := v.AnnounceRead(ann, &arep); err != nil {
		t.Fatalf("the engine refused the announcement: %v %+v", err, arep)
	}
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Probe"))
	var rep tabletext.Report
	count, ok, err := tablewire.DecodeMessages(m, []*tabletext.Instance{inst}, wire, v, &rep)
	refused := !ok || err != nil || rep.Refused
	values := map[string]int64{}
	if !refused {
		for _, name := range []string{"a", "b", "c"} {
			cell, _ := inst.FieldByKey(name)
			values[name] = cell.Cell.I
		}
	}
	return refused, count, values
}

func messageIntegerKindDomainJavaCase(c messageIntegerKindDomainCase, ann, msg []byte) string {
	return fmt.Sprintf(`
        {
            final byte[] ann = fromHex(%q);
            final byte[] msg = fromHex(%q);
            final TableVocabulary vocab = new TableVocabulary();
            vocab.reset(4);
            final TableVocabulary.Report arep = new TableVocabulary.Report();
            final boolean announced = TableVocabulary.announceRead(vocab, ann, arep);
            if (!announced) { fail("announcement refused"); }
            final ProbeFixed.Value[] values = new ProbeFixed.Value[1];
            values[0] = new ProbeFixed.Value();
            final TableVocabulary.Report lrep = new TableVocabulary.Report();
            final int count = ProbeMessage.loadMessages(values, 1, vocab, msg, lrep);
            System.out.println("CASE %s count=" + count
                + " damaged=" + lrep.damaged + " refused=" + lrep.refused
                + " widened=" + lrep.widened
                + " a=" + (int) values[0].a
                + " b=" + values[0].b
                + " c=" + ((int) values[0].c & 0xff));
        }
`, hexOf(ann), hexOf(msg), c.id)
}

func TestJavaMessageIntegerHeldToReaderKind(t *testing.T) {
	t.Parallel()
	javaToolsRequired(t)

	u := unitFrom(t, messageIntegerKindDomainUnit)
	st := u.Tables["Probe"]
	byName := map[string]*ir.Field{}
	for _, f := range st.Fields {
		byName[f.Name] = f
	}

	a, b, c := byName["a"], byName["b"], byName["c"]
	if a == nil || b == nil || c == nil {
		t.Fatal("the probe unit lost a field")
	}

	hostile := func(name string, f *ir.Field, base int64, bits int64, raw uint64) messageIntegerKindDomainCase {
		return messageIntegerKindDomainCase{
			id:     name,
			field:  f,
			kind:   uint8(ir.TableWireScalarKind(f)),
			shape:  ir.TableMessageShape{Packing: ir.TableMessageRanged, Bits: bits, Base: big.NewInt(base)},
			raw:    raw,
			refuse: true,
		}
	}
	raw := func(name string, f *ir.Field, raw uint64, want int64) messageIntegerKindDomainCase {
		return messageIntegerKindDomainCase{
			id:    name,
			field: f,
			kind:  uint8(ir.TableWireScalarKind(f)),
			raw:   raw,
			want:  want,
		}
	}

	cases := []messageIntegerKindDomainCase{
		hostile("int8 above its domain", a, 100, 8, 255),
		hostile("uint8 above its domain", c, 250, 8, 255),
		hostile("int32 above its domain", b, 1<<31-1, 32, 1<<32-1),
		hostile("int32 base 2^40, bits 8, offset 5", b, 1<<40, 8, 5),
		raw("int8 minimum", a, 1<<7, -128),
		raw("int8 maximum", a, 1<<7-1, 127),
		raw("int32 minimum", b, 1<<31, -1<<31),
		raw("int32 maximum", b, 1<<31-1, 1<<31-1),
		raw("uint8 minimum", c, 0, 0),
		raw("uint8 maximum", c, 255, 255),
		{
			id:     "a lawful widening",
			field:  b,
			kind:   ir.TableKindI8,
			raw:    1 << 7,
			want:   -128,
			widens: true,
		},
	}

	var body strings.Builder
	for _, tc := range cases {
		ann := messageIntegerKindDomainAnnouncement(ir.TableVocabularyEntry{
			Id:    ir.TableFieldWireId(tc.field),
			Kind:  tc.kind,
			Shape: tc.shape,
		})
		msg := messageIntegerKindDomainBatch(tc.kind, tc.shape, tc.raw)
		body.WriteString(messageIntegerKindDomainJavaCase(tc, ann, msg))
	}

	out := runMessageBatchProbe(t, u, body.String())
	got := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		if !strings.HasPrefix(line, "CASE ") {
			continue
		}
		fields := strings.Fields(line)
		id := strings.Join(fields[1:len(fields)-7], " ")
		got[id] = strings.Join(fields[len(fields)-7:], " ")
	}

	for _, tc := range cases {
		ann := messageIntegerKindDomainAnnouncement(ir.TableVocabularyEntry{
			Id:    ir.TableFieldWireId(tc.field),
			Kind:  tc.kind,
			Shape: tc.shape,
		})
		msg := messageIntegerKindDomainBatch(tc.kind, tc.shape, tc.raw)
		oracleRefused, oracleCount, oracleValues := messageIntegerKindDomainOracle(t, u, ann, msg)
		if oracleRefused != tc.refuse {
			t.Fatalf("%s: the engine refused=%t, want %t", tc.id, oracleRefused, tc.refuse)
		}
		if !oracleRefused && oracleCount != 1 {
			t.Fatalf("%s: the engine delivered %d bodies, want 1", tc.id, oracleCount)
		}
		if !oracleRefused && oracleValues[tc.field.Name] != tc.want {
			t.Fatalf("%s: the engine read %d, want %d", tc.id, oracleValues[tc.field.Name], tc.want)
		}

		line := got[tc.id]
		if line == "" {
			t.Fatalf("%s: no CASE line in:\n%s", tc.id, out)
		}
		javaRefused := strings.HasPrefix(line, "count=0 ") &&
			(strings.Contains(line, "damaged=true") || strings.Contains(line, "refused=true"))
		if javaRefused != tc.refuse {
			t.Errorf("%s: Java refused=%t, want %t: %s", tc.id, javaRefused, tc.refuse, line)
			continue
		}
		if tc.refuse {
			continue
		}
		widened := "0"
		if tc.widens {
			widened = "1"
		}
		want := fmt.Sprintf("count=1 damaged=false refused=false widened=%s a=%d b=%d c=%d",
			widened, int64(oracleValues["a"]), int64(oracleValues["b"]), int64(oracleValues["c"]))
		if line != want {
			t.Errorf("%s: Java read %q, want %q", tc.id, line, want)
		}
	}
}
