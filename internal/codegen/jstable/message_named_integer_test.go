package jstable

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The reader: a bare uint8, a bare uint16, and a bits(9).
const jsMessageReaderSchema = `package probe

fixed table Msg
{
    a uint8
    b uint16
    c bits(9)
}
`

// The writer under another build: a and b ranged over [250, 255] in three
// bits (offsets 6 and 7 spell 256 and 257, which no conforming writer emits;
// b widens into the reader's uint16), and a union field u the reader does
// not declare.
const jsMessageWriterSchema = `package probe

union U
{
    pid  uint32 | min = 0, max = 1000
    note uint8
}

fixed table Msg
{
    a uint8 = 250 | min = 250, max = 255
    b uint8 = 250 | min = 250, max = 255
    u U
}
`

// TestJsMessageNamedIntegerHeldWhole pins the message form's named integer
// read and its union skip to SPEC-TABLES §3.3 (rulings message-one-encoding,
// skipped-value-is-validated, uncertain-read-refused). An integer read by
// name is reconstructed whole and refused out_of_range when it lands outside
// the announced kind or the reader's own kind: it is never masked to the
// reader's width (base 250 + offset 7 = 257 would land as 1 in a uint8) and
// never let past the announced kind because the reader is wider (257 in a
// uint16 announced as a uint8). A union FIELD the reader skips, unknown to
// it, is refused when written with arm reference 0, as the named read
// refuses it; the lawful twins of every case read. The pure half reads the
// emitted code; the gated half runs the batches under node.
func TestJsMessageNamedIntegerHeldWhole(t *testing.T) {
	t.Parallel()
	reader := unitFrom(t, jsMessageReaderSchema)
	files, err := Generate(reader)
	if err != nil {
		t.Fatal(err)
	}
	src := string(files["ProbeTable.js"])
	body := jsFn(src, "MsgLoadMessageBody")
	for _, banned := range []string{"BigInt.asUintN(8, x)", "BigInt.asUintN(16, x)", "BigInt.asUintN(9, x)"} {
		if strings.Contains(body, banned) {
			t.Errorf("the named read masks the value to the reader's width (%q) before any test:\n%s", banned, body)
		}
	}
	for _, want := range []string{
		"tableMessageReadInteger(r.Bits, entry.Shape)",
		"if (!holds || x < 0n || x > 255n) { TableMessageRefuse(r.Report, \"out_of_range\"); return false; }",
		"if (!holds || x < 0n || x > 65535n) { TableMessageRefuse(r.Report, \"out_of_range\"); return false; }",
		"if (!holds || x < 0n || x > 511n) { TableMessageRefuse(r.Report, \"out_of_range\"); return false; }",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the named read lacks %q:\n%s", want, body)
		}
	}
	if !strings.Contains(body, "tableMessageSkip(r.Bits, r.Vocabulary, r.IndexBits, entry, 0, true)") {
		t.Errorf("an unknown body field must be skipped as a FIELD (atField):\n%s", body)
	}
	skip := jsFn(src, "tableMessageSkip")
	if !strings.Contains(skip, "if (!ok || ref === 0n) { return ok && !atField; }") {
		t.Errorf("the skip must refuse arm reference 0 at a union field:\n%s", skip)
	}
	if !strings.Contains(jsFn(src, "tableMessageSkipBody"), "tableMessageSkip(r, v, indexBits, e, depth, true)") {
		t.Errorf("a skipped body's fields must be skipped as FIELDS")
	}

	t.Run("node", func(t *testing.T) {
		slowtest.Gate(t, "node (it runs the generated message read)")
		node := jsNode(t)
		dir := t.TempDir()
		base, err := js.Generate(reader)
		if err != nil {
			t.Fatal(err)
		}
		for _, set := range []map[string][]byte{base, files} {
			for name, data := range set {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
		}
		writer := unitFrom(t, jsMessageWriterSchema)
		cases := jsMessageNamedIntegerCases(t, writer)
		var lines []string
		for _, c := range cases {
			lines = append(lines, c.name+" "+c.batch+" "+c.want)
		}
		input := hex.EncodeToString(tablewire.Announce(writer)) + "\n" + strings.Join(lines, "\n") + "\n"
		if err := os.WriteFile(filepath.Join(dir, "cases.txt"), []byte(input), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(jsMessageNamedIntegerProbe), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(node, "probe.mjs", "cases.txt")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("node probe: %v\n%s", err, out)
		}
		if strings.Contains(string(out), "FAILED") || !strings.Contains(string(out), "OK") {
			t.Fatalf("probe output:\n%s", out)
		}
	})
}

type jsMessageCase struct{ name, batch, want string }

// jsMessageBits is the batch body's bit writer: least significant bit first.
type jsMessageBits struct {
	out []byte
	n   int
}

func (w *jsMessageBits) put(v uint64, bits int) {
	for i := range bits {
		if w.n%8 == 0 {
			w.out = append(w.out, 0)
		}
		if v>>uint(i)&1 != 0 {
			w.out[len(w.out)-1] |= 1 << uint(w.n%8)
		}
		w.n++
	}
}

func jsMessageNamedIntegerCases(t *testing.T, writer *ir.Unit) []jsMessageCase {
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
	msg := writer.Tables["Msg"]
	field := map[string]*ir.Field{}
	for _, f := range msg.Fields {
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
	batch := func(fill func(w *jsMessageBits)) string {
		w := &jsMessageBits{}
		fill(w)
		w.put(0, refBits) // the body's terminator
		return hex.EncodeToString(append([]byte{2, 0}, w.out...))
	}
	ranged := func(name string, offset uint64) func(w *jsMessageBits) {
		return func(w *jsMessageBits) {
			e := entry(name)
			w.put(slot(e), refBits)
			w.put(offset, valueBits(e))
		}
	}
	unionField := func(armName string, value uint64) func(w *jsMessageBits) {
		return func(w *jsMessageBits) {
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
	return []jsMessageCase{
		{"a-lawful-255", batch(ranged("a", 5)), "a=255"},
		{"a-256-past-uint8", batch(ranged("a", 6)), "refused"},
		{"a-257-past-uint8", batch(ranged("a", 7)), "refused"},
		{"b-lawful-255-widened", batch(ranged("b", 5)), "b=255"},
		{"b-257-past-announced-uint8", batch(ranged("b", 7)), "refused"},
		{"u-arm-pid", batch(unionField("pid", 5)), "unknown=1"},
		{"u-arm-zero-at-field", batch(unionField("", 0)), "refused"},
		{"twin-fields", batch(func(w *jsMessageBits) { ranged("a", 0)(w); ranged("b", 1)(w) }), fmt.Sprintf("a=%d b=%d", 250, 251)},
	}
}

const jsMessageNamedIntegerProbe = `import { readFileSync } from "node:fs";
import { Msg, TableVocabulary, AnnounceRead, MsgLoadMessages } from "./ProbeTable.js";
const lines = readFileSync(process.argv[2], "utf8").trim().split("\n");
const v = new TableVocabulary();
v.Init(new Array(16));
const arep = {};
if (!AnnounceRead(v, Uint8Array.from(Buffer.from(lines[0], "hex")), arep)) {
  console.log("FAILED: the announcement was refused " + JSON.stringify(arep));
  process.exit(1);
}
let failed = false;
for (const line of lines.slice(1)) {
  const [name, hexBatch, ...want] = line.split(" ");
  const out = [new Msg()];
  const rep = {};
  const n = MsgLoadMessages(out, v, Uint8Array.from(Buffer.from(hexBatch, "hex")), rep);
  const refused = n !== 1 || rep.Damaged || (rep.Oor | 0) > 0 || (rep.Mismatch | 0) > 0 || Boolean(rep.Reason);
  let got = "refused";
  if (!refused && want[0] === "refused") {
    got = "read";
  } else if (!refused) {
    const parts = [];
    for (const w of want) {
      const [k] = w.split("=");
      parts.push(k === "unknown" ? "unknown=" + rep.Unknown : k + "=" + out[0][k]);
    }
    got = parts.join(" ");
  }
  const expect = want.join(" ");
  if (got !== expect) {
    failed = true;
    console.log("FAILED: " + name + ": got " + got + " (n=" + n + " " + JSON.stringify(rep) + " a=" + out[0].a + " b=" + out[0].b + "), want " + expect);
  }
}
if (!failed) { console.log("OK"); }
`
