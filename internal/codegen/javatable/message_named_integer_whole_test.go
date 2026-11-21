package javatable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The message form on the Java leg holds two of the cross-leg message rules
// (docs/SPEC-TABLES.md §3.3; review-owed/cross-leg/message-form rules 1 and
// 3):
//
//   - an integer read by name is base plus offset taken EXACTLY and held to
//     the ANNOUNCED kind's domain, never wrapped: 257 announced as a uint8 is
//     refused in a uint8 and in the uint16 it widens into, and a uint64 or
//     int64 base near its kind's edge plus an offset past it is refused, not
//     landed wrapped round;
//   - a union FIELD the reader skips (unknown to it) written with arm
//     reference 0 is refused, as the reference read refuses it; a lawful arm
//     there is skipped and counted unknown.
//
// The lawful twin of every case reads.
const javaNamedIntegerReader = `package probe

fixed table Msg
{
    a uint8
    b uint16
    c uint64
    d int64
}
`

// The writer under another build: ranges whose offset bits can spell a value
// past the announced kind (a, b over [250, 255]; c over [2^64 - 6, 2^64 - 1];
// d over [2^63 - 6, 2^63 - 1], each in three bits), and a union field u the
// reader does not declare.
const javaNamedIntegerWriter = `package probe

union U
{
    pid  uint32 | min = 0, max = 1000
    note uint8
}

fixed table Msg
{
    a uint8 = 250 | min = 250, max = 255
    b uint8 = 250 | min = 250, max = 255
    c uint64 = 18446744073709551610 | min = 18446744073709551610, max = 18446744073709551615
    d int64 = 9223372036854775802 | min = 9223372036854775802, max = 9223372036854775807
    u U
}
`

type javaNamedIntegerCase struct{ name, batch, want string }

func javaNamedIntegerCases(t *testing.T, writer *ir.Unit) []javaNamedIntegerCase {
	t.Helper()
	vocab := tablewire.MessageVocabulary(writer)
	refBits := ir.TableMessageRefBits(len(vocab))
	slot := func(e ir.TableVocabularyEntry) uint64 {
		for i, v := range vocab {
			if v.Key() == e.Key() {
				return uint64(i + 1)
			}
		}
		t.Fatalf("entry %016x outside the writer's vocabulary", e.Id)
		return 0
	}
	field := map[string]*ir.Field{}
	for _, f := range writer.Tables["Msg"].Fields {
		field[f.Name] = f
	}
	entry := func(name string) ir.TableVocabularyEntry { return ir.TableFieldEntry(field[name]) }
	un := field["u"].Type.Ref.(*ir.Union)
	arm := func(name string) ir.TableVocabularyEntry {
		for _, v := range un.Variants {
			if v.Name == name {
				return ir.TableArmEntry(v)
			}
		}
		t.Fatalf("no arm %s", name)
		return ir.TableVocabularyEntry{}
	}
	valueBits := func(e ir.TableVocabularyEntry) int {
		b := ir.TableMessageValueBits(e.Kind, e.Shape)
		if b < 0 {
			t.Fatalf("entry %016x has no fixed-width value", e.Id)
		}
		return int(b)
	}
	batch := func(fill ...func(w *repeatedFieldBits)) string {
		w := &repeatedFieldBits{}
		for _, f := range fill {
			f(w)
		}
		w.put(0, refBits) // the body's terminator
		return hexOf(w.wire())
	}
	ranged := func(name string, offset uint64) func(w *repeatedFieldBits) {
		return func(w *repeatedFieldBits) {
			e := entry(name)
			w.put(slot(e), refBits)
			w.put(offset, valueBits(e))
		}
	}
	unionField := func(armName string, value uint64) func(w *repeatedFieldBits) {
		return func(w *repeatedFieldBits) {
			w.put(slot(entry("u")), refBits)
			if armName == "" {
				w.put(0, refBits)
				return
			}
			e := arm(armName)
			w.put(slot(e), refBits)
			w.put(value, valueBits(e))
		}
	}
	return []javaNamedIntegerCase{
		{"a-lawful-255", batch(ranged("a", 5)), "a=255"},
		{"a-256-past-uint8", batch(ranged("a", 6)), "refused"},
		{"a-257-past-uint8", batch(ranged("a", 7)), "refused"},
		{"b-lawful-255-widened", batch(ranged("b", 5)), "b=255"},
		{"b-257-past-announced-uint8", batch(ranged("b", 7)), "refused"},
		{"c-lawful-max", batch(ranged("c", 5)), "c=18446744073709551615"},
		{"c-past-uint64-not-wrapped", batch(ranged("c", 7)), "refused"},
		{"d-lawful-max", batch(ranged("d", 5)), "d=9223372036854775807"},
		{"d-past-int64-not-wrapped", batch(ranged("d", 7)), "refused"},
		{"u-arm-pid", batch(unionField("pid", 5)), "unknown=1"},
		{"u-arm-zero-at-field", batch(unionField("", 0)), "refused"},
		{"twin-fields", batch(ranged("a", 0), ranged("b", 1)), "a=250 b=251"},
	}
}

func TestJavaMessageNamedIntegerHeldWholeAndUnionFieldArmZero(t *testing.T) {
	t.Parallel()
	reader := unitFrom(t, javaNamedIntegerReader)
	writer := unitFrom(t, javaNamedIntegerWriter)
	files, err := Generate(reader)
	if err != nil {
		t.Fatal(err)
	}
	src := string(files["TableMessage.java"])
	for _, want := range []string{
		"static boolean rangedHolds(TableVocabulary.Shape s, long offLo, long offHi)",
		"public static boolean skipField(Reader r, TableVocabulary.Entry e)",
		"if (atField) {",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("TableMessage.java lacks %s", want)
		}
	}
	if !strings.Contains(string(files["MsgMessage.java"]), "TableMessage.skipField(r, entry)") {
		t.Errorf("an unknown body field must be skipped as a FIELD")
	}

	ann := hexOf(tablewire.Announce(writer))
	cases := javaNamedIntegerCases(t, writer)
	var body strings.Builder
	fmt.Fprintf(&body, "        final TableVocabulary vocab = new TableVocabulary();\n")
	fmt.Fprintf(&body, "        vocab.reset(16);\n")
	fmt.Fprintf(&body, "        final TableVocabulary.Report arep = new TableVocabulary.Report();\n")
	fmt.Fprintf(&body, "        if (!TableVocabulary.announceRead(vocab, fromHex(%q), arep)) { fail(\"announcement refused: \" + arep.reason); }\n", ann)
	for _, c := range cases {
		fmt.Fprintf(&body, `        {
            final MsgFixed.Value[] values = new MsgFixed.Value[] { new MsgFixed.Value() };
            final TableVocabulary.Report rep = new TableVocabulary.Report();
            final int n = MsgMessage.loadMessages(values, 1, vocab, fromHex(%q), rep);
            final boolean refused = n != 1 || rep.damaged || rep.refused;
            final MsgFixed.Value v = values[0];
            final String got = refused ? "refused" : "a=" + (v.a & 0xff) + " b=" + (v.b & 0xffff) + " c=" + Long.toUnsignedString(v.c) + " d=" + v.d + " unknown=" + rep.unknown;
            System.out.println("CASE %s " + got + " (n=" + n + " damaged=" + rep.damaged + " refused=" + rep.refused + " reason=" + rep.reason + ")");
        }
`, c.batch, c.name)
	}
	out := runMessageBatchProbe(t, reader, body.String())
	for _, c := range cases {
		line := ""
		for l := range strings.SplitSeq(out, "\n") {
			if strings.HasPrefix(l, "CASE "+c.name+" ") {
				line = strings.TrimPrefix(l, "CASE "+c.name+" ")
			}
		}
		if line == "" {
			t.Errorf("%s: no CASE line in:\n%s", c.name, out)
			continue
		}
		ok := true
		if c.want == "refused" {
			ok = strings.HasPrefix(line, "refused ")
		} else {
			for _, w := range strings.Fields(c.want) {
				if !strings.Contains(" "+line+" ", " "+w+" ") {
					ok = false
				}
			}
			ok = ok && !strings.HasPrefix(line, "refused ")
		}
		if !ok {
			t.Errorf("%s: got %s, want %s", c.name, line, c.want)
		}
	}
}
