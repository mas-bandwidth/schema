package javatable

import (
	"fmt"
	"path/filepath"
	"strconv"
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
// The lawful twin of every case reads. The reader, the writer under another
// build (ranges whose offset bits can spell a value past the announced kind,
// and a union field u the reader does not declare) and one row per batch are
// the committed corpus
// testdata/message-named-integer/{reader.schema,writer.schema,cases.tsv}
// (F-203).
const javaNamedIntegerDir = "testdata/message-named-integer"

type javaNamedIntegerCase struct{ name, batch, want string }

// javaNamedIntegerCovers is what the committed rows must hold between them:
// each rule's refusals beside their lawful twins.
var javaNamedIntegerCovers = []string{
	"a-lawful-255", "a-256-past-uint8", "a-257-past-uint8",
	"b-lawful-255-widened", "b-257-past-announced-uint8",
	"c-lawful-max", "c-past-uint64-not-wrapped", "d-lawful-max", "d-past-int64-not-wrapped",
	"u-arm-pid", "u-arm-zero-at-field", "twin-fields",
}

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
	entry := func(name string) ir.TableVocabularyEntry {
		f, ok := field[name]
		if !ok {
			t.Fatalf("the writer's Msg has no field %s", name)
		}
		return ir.TableFieldEntry(f)
	}
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
	number := func(id, s string) uint64 {
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return n
	}
	// put writes one field of a batch as the corpus spells it: name:offset,
	// u:arm:value, or u:0 for arm reference 0.
	put := func(id, spelled string, w *repeatedFieldBits) {
		parts := strings.Split(spelled, ":")
		switch {
		case len(parts) == 2 && parts[0] == "u" && parts[1] == "0":
			w.put(slot(entry("u")), refBits)
			w.put(0, refBits)
		case len(parts) == 3 && parts[0] == "u":
			e := arm(parts[1])
			w.put(slot(entry("u")), refBits)
			w.put(slot(e), refBits)
			w.put(number(id, parts[2]), valueBits(e))
		case len(parts) == 2:
			e := entry(parts[0])
			w.put(slot(e), refBits)
			w.put(number(id, parts[1]), valueBits(e))
		default:
			t.Fatalf("%s: field %q is not name:offset, u:arm:value or u:0", id, spelled)
		}
	}
	var out []javaNamedIntegerCase
	for _, r := range corpusRows(t, filepath.Join(javaNamedIntegerDir, "cases.tsv"), 3) {
		w := &repeatedFieldBits{}
		for f := range strings.FieldsSeq(r[1]) {
			put(r[0], f, w)
		}
		w.put(0, refBits) // the body's terminator
		out = append(out, javaNamedIntegerCase{r[0], hexOf(w.wire()), r[2]})
	}
	inCorpus := map[string]bool{}
	for _, c := range out {
		inCorpus[c.name] = true
	}
	for _, name := range javaNamedIntegerCovers {
		if !inCorpus[name] {
			t.Errorf("%s/cases.tsv has no %s row", javaNamedIntegerDir, name)
		}
	}
	return out
}

func TestJavaMessageNamedIntegerHeldWholeAndUnionFieldArmZero(t *testing.T) {
	t.Parallel()
	reader := unitFrom(t, corpusSchema(t, javaNamedIntegerDir, "reader.schema"))
	writer := unitFrom(t, corpusSchema(t, javaNamedIntegerDir, "writer.schema"))
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
			if rest, ok := strings.CutPrefix(l, "CASE "+c.name+" "); ok {
				line = rest
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
			for w := range strings.FieldsSeq(c.want) {
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
