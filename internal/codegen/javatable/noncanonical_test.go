package javatable

// TestJavaReadsRefuseNonCanonical holds docs/SPEC-TABLES.md §3, §3.1, §3.3 and
// §4's one-encoding rule on the Java reads (docs/rulings.json
// reads-refuse-non-canonical) over every row of
// testdata/wire/non-canonical/MANIFEST.tsv. Every variable row is read through
// the plain load, the retaining load and the load measure; a read row re-saves
// through each load's writer to the canonical bytes (the vector itself, or the
// canonical rewrite pinned beside it for a lawful spelling no writer produces),
// and a refused row is refused by both loads. Every message row is read through
// the batch load twice: under the announcement as written, where the
// manifest's verdict holds and a read re-saves to the same bytes, and under
// another build version (§3.3), where a field at its default and fields out of
// order are lawful evolution and read, re-saving to a batch that reads back
// canonically, while a union field written with arm reference 0 (§3.3 (c)) is
// refused at every build. Three controls stand beside the manifest: a node
// table out of its place under an id table in first-use order is refused, a
// body naming a field its record does not declare is refused at the reader's
// own build and read at another, and a None ELEMENT of an array of unions reads
// at both. The verdict alone is the contract, so no refusal reason is compared.

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

type nonCanonicalRow struct {
	file     string
	form     string
	spelling string
	verdict  string
}

func javaNonCanonicalRows(t *testing.T, path string) []nonCanonicalRow {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows []nonCanonicalRow
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields, want file, form, case, verdict", i+1, len(f))
		}
		if f[0] == "file" {
			continue
		}
		if f[3] != "read" && f[3] != "refused" {
			t.Fatalf("MANIFEST.tsv:%d: verdict %q, want read or refused", i+1, f[3])
		}
		rows = append(rows, nonCanonicalRow{file: f[0], form: f[1], spelling: f[2], verdict: f[3]})
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no rows")
	}
	return rows
}

func javaHexFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(data)
}

// javaNonCanonicalExpected is the canonical file a read variable row re-saves
// to: the vector itself, or the canonical rewrite pinned beside it.
func javaNonCanonicalExpected(t *testing.T, vectors string, row nonCanonicalRow) string {
	t.Helper()
	path := filepath.Join(vectors, row.file)
	rewrite := filepath.Join(vectors, "canonical", filepath.Base(row.file))
	if _, err := os.Stat(rewrite); err == nil {
		path = rewrite
	} else if row.file == "variable-fields-out-of-order.bin" {
		path = filepath.Join(vectors, "variable-canonical.bin")
	}
	return javaHexFile(t, path)
}

// javaRunVerdicts compiles one generated unit with a driver and answers the
// driver's "<row> <entry> <answer>" lines as a map.
func javaRunVerdicts(t *testing.T, javac, javaBin string, files map[string][]byte, pkg, driver string) map[string]string {
	t.Helper()
	dir := compileCookUnit(t, javac, files, pkg, map[string]string{"NonCanonicalDriver": driver})
	out, err := testproc.Command(t, javaBin, "-ea", "-cp", dir, "NonCanonicalDriver").CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	answers := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 {
			t.Fatalf("driver line %q, want <row> <entry> <answer>", line)
		}
		answers[f[0]+" "+f[1]] = f[2]
	}
	return answers
}

func javaHold(t *testing.T, answers map[string]string, row, entry, want string) {
	t.Helper()
	got, ok := answers[row+" "+entry]
	if !ok {
		t.Errorf("%s: %s did not answer, so the test did not reach it", row, entry)
		return
	}
	if got != want {
		t.Errorf("%s: %s answered %s, want %s", row, entry, got, want)
	}
}

// javaUndeclaredFieldBatch is a batch of one Message whose body names Leaf's
// field `tag`, an entry the announcement carries (Leaf rides as Pick's arm)
// that Message does not declare, then the body's zero reference.
func javaUndeclaredFieldBatch(t *testing.T, u *ir.Unit, announcement []byte) []byte {
	t.Helper()
	var voc tablewire.Vocabulary
	if err := voc.AnnounceRead(announcement, &tabletext.Report{}); err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	leaf := m.Lookup("Leaf")
	if leaf == nil || len(leaf.Fields) == 0 {
		t.Fatal("NonCanonical.schema: no Leaf.tag")
	}
	slot, ok := tablewire.MessageVocabularySlots(u)[ir.TableFieldEntry(leaf.Fields[0]).Key()]
	if !ok {
		t.Fatal("the announcement carries no Leaf.tag entry")
	}
	refBits := voc.RefBits()
	var out []byte
	n := 0
	put := func(v uint64, bits int) {
		for i := range bits {
			if n%8 == 0 {
				out = append(out, 0)
			}
			if v>>uint(i)&1 == 1 {
				out[n/8] |= 1 << uint(n%8)
			}
			n++
		}
	}
	put(0, 8) // a batch of one
	put(slot, refBits)
	put(3, 8)
	put(0, refBits)
	batch := append([]byte{ir.TableWireMessageForm}, out...)

	// The oracle holds the same verdicts: refused at its own build, read at
	// another, so the batch is spelled as the wire spells it.
	model := tabletext.NewModel(u)
	v := voc
	if ok, err := tablewire.DecodeMessage(model, model.New(model.Lookup("Message")), batch, &v, &tabletext.Report{}); ok || err == nil {
		t.Fatalf("oracle read the undeclared-field batch at its own build: %x", batch)
	}
	return batch
}

// javaNodeTableNotLast respells variable-canonical.bin with its node table
// moved ahead of the root's `pick` field and the id table renumbered to the
// order a reader first meets each id in that spelling, so the only fault left
// is the node table's place: it is not the root body's last field (§3.1).
func javaNodeTableNotLast(t *testing.T, canonical []byte) []byte {
	t.Helper()
	// form 1; count = 5; head -> node 2; pick = leaf{tag = 4}; the node table
	// (one Node{value = 3}); the terminator
	const body = "01" + "010605" + "021102" + "030f040d04050604" + "00" + "060c0701070408060300" + "00"
	want, err := hex.DecodeString(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(canonical) < len(want)+8 || string(canonical[:len(want)]) != string(want) {
		t.Fatalf("variable-canonical.bin's body is no longer %s: %x", body, canonical)
	}
	ids := canonical[len(want) : len(canonical)-8]
	if len(ids) != 8*8 {
		t.Fatalf("variable-canonical.bin carries %d id bytes, want 64", len(ids))
	}
	id := func(ref int) []byte { return ids[8*(ref-1) : 8*ref] }
	moved, err := hex.DecodeString("01" + "010605" + "021102" + "030c07010704080603" + "00" + "040f050d04060604" + "00" + "00")
	if err != nil {
		t.Fatal(err)
	}
	// first met: count, head, the node table, pick, leaf, tag, then the
	// record's Node and value
	for _, ref := range []int{1, 2, 6, 3, 4, 5, 7, 8} {
		moved = append(moved, id(ref)...)
	}
	return append(moved, canonical[len(canonical)-8:]...)
}

func TestJavaReadsRefuseNonCanonical(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "javac and java (it builds and runs the generated Java)")

	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	vectors := filepath.Join(repo, "testdata", "wire", "non-canonical")
	rows := javaNonCanonicalRows(t, filepath.Join(vectors, "MANIFEST.tsv"))

	nonCanonical := unitFromDir(t, filepath.Join(repo, "test", "non-canonical", "NonCanonical.schema"))
	one := unitFromDir(t, filepath.Join(repo, "test", "message-one-encoding", "One.schema"))
	announcement, err := os.ReadFile(filepath.Join(vectors, "announcement.bin"))
	if err != nil {
		t.Fatal(err)
	}

	var variableRows, messageRows, oneRows, announceRows strings.Builder
	for _, row := range rows {
		path := filepath.Join(vectors, row.file)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("MANIFEST.tsv names %s which is not on disk: %v", row.file, err)
		}
		other := "read"
		if row.spelling == "union-arm-zero" {
			other = "refused"
		}
		switch row.form {
		case "variable":
			fmt.Fprintf(&variableRows, "        {%q, %q, %q, %q},\n", row.file, javaHexFile(t, path), javaNonCanonicalExpected(t, vectors, row), row.verdict)
		case "announcement":
			fmt.Fprintf(&announceRows, "        {%q, %q, %q},\n", row.file, javaHexFile(t, path), row.verdict)
		case "message":
			line := fmt.Sprintf("        {%q, %q, %q, %q},\n", row.file, javaHexFile(t, path), row.verdict, other)
			if strings.HasPrefix(row.file, "../message-one-encoding/") {
				oneRows.WriteString(line)
			} else {
				messageRows.WriteString(line)
			}
		default:
			t.Fatalf("%s: form %q is not variable, message or announcement", row.file, row.form)
		}
	}
	canonicalFile, err := os.ReadFile(filepath.Join(vectors, "variable-canonical.bin"))
	if err != nil {
		t.Fatal(err)
	}
	notLast := javaNodeTableNotLast(t, canonicalFile)
	oracle := tabletext.NewModel(nonCanonical)
	if ok, err := tablewire.Decode(oracle, oracle.New(oracle.Lookup("Root")), notLast, &tabletext.Report{}); ok && err == nil {
		t.Fatalf("oracle read the node table out of its place: %x", notLast)
	}
	fmt.Fprintf(&variableRows, "        {%q, %q, %q, %q},\n", "node-table-not-last", hex.EncodeToString(notLast), hex.EncodeToString(notLast), "refused")

	undeclared := javaUndeclaredFieldBatch(t, nonCanonical, announcement)
	fmt.Fprintf(&messageRows, "        {%q, %q, %q, %q},\n", "undeclared-field", hex.EncodeToString(undeclared), "refused", "read")

	javac, javaBin := javaTools(t)

	files, err := Generate(nonCanonical)
	if err != nil {
		t.Fatalf("noncanonical: Generate: %v", err)
	}
	answers := javaRunVerdicts(t, javac, javaBin, files, nonCanonical.Package, strings.NewReplacer(
		"%%PKG%%", nonCanonical.Package,
		"%%VALUE%%", "MessageFixed.Value",
		"%%MESSAGE_CLASS%%", "MessageMessage",
		"%%ANNOUNCE%%", hex.EncodeToString(announcement),
		"%%ANNOUNCEMENTS%%", announceRows.String(),
		"%%VARIABLE%%", variableRows.String(),
		"%%MESSAGE%%", messageRows.String(),
		"%%VARIABLE_BODY%%", javaNonCanonicalVariableBody,
	).Replace(javaNonCanonicalDriver))

	oneFiles, err := Generate(one)
	if err != nil {
		t.Fatalf("message-one-encoding: Generate: %v", err)
	}
	oneAnswers := javaRunVerdicts(t, javac, javaBin, oneFiles, one.Package, strings.NewReplacer(
		"%%PKG%%", one.Package,
		"%%VALUE%%", "OneFixed.Value",
		"%%MESSAGE_CLASS%%", "OneMessage",
		"%%ANNOUNCE%%", javaHexFile(t, filepath.Join(repo, "testdata", "wire", "message-one-encoding", "announcement.bin")),
		"%%ANNOUNCEMENTS%%", "",
		"%%VARIABLE%%", "",
		"%%MESSAGE%%", oneRows.String(),
		"%%VARIABLE_BODY%%", "",
	).Replace(javaNonCanonicalDriver))

	for _, row := range rows {
		switch row.form {
		case "variable":
			javaHold(t, answers, row.file, "plain", row.verdict)
			javaHold(t, answers, row.file, "retain", row.verdict)
			if row.verdict == "read" {
				javaHold(t, answers, row.file, "measure", "read")
			}
		case "announcement":
			javaHold(t, answers, row.file, "announce", row.verdict)
		case "message":
			got := answers
			if strings.HasPrefix(row.file, "../message-one-encoding/") {
				got = oneAnswers
			}
			other := "read"
			if row.spelling == "union-arm-zero" {
				other = "refused"
			}
			javaHold(t, got, row.file, "same-build", row.verdict)
			javaHold(t, got, row.file, "other-build", other)
		}
	}
	javaHold(t, answers, "node-table-not-last", "plain", "refused")
	javaHold(t, answers, "node-table-not-last", "retain", "refused")
	javaHold(t, answers, "undeclared-field", "same-build", "refused")
	javaHold(t, answers, "undeclared-field", "other-build", "read")

	t.Run("union None element", testJavaUnionNoneElementReads)
}

// testJavaUnionNoneElementReads is the control for §3.3 (c): the refusal is of
// a union FIELD written with arm reference 0, never of a None element of an
// array of unions, which is positional. The writer's batch carrying one reads
// under this reader's build version and under another.
func testJavaUnionNoneElementReads(t *testing.T) {
	t.Parallel()
	const schema = `package messagenone

union Choice
{
    item uint8
}

fixed table Root
{
    tick    uint8
    choice  Choice
    choices [..2]Choice
}
`
	u := unitFrom(t, schema)
	m := tabletext.NewModel(u)
	value := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(value, []byte(`{"tick": 1, "choices": [{}, {"item": 3}]}`), &report) {
		t.Fatalf("value: %+v", report)
	}
	batch, err := tablewire.EncodeMessage(m, value)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	javac, javaBin := javaTools(t)
	answers := javaRunVerdicts(t, javac, javaBin, files, u.Package, strings.NewReplacer(
		"%%PKG%%", u.Package,
		"%%VALUE%%", "RootFixed.Value",
		"%%MESSAGE_CLASS%%", "RootMessage",
		"%%ANNOUNCE%%", hex.EncodeToString(tablewire.Announce(u)),
		"%%ANNOUNCEMENTS%%", "",
		"%%VARIABLE%%", "",
		"%%MESSAGE%%", fmt.Sprintf("        {%q, %q, %q, %q},\n", "none-element", hex.EncodeToString(batch), "read", "read"),
		"%%VARIABLE_BODY%%", "",
	).Replace(javaNonCanonicalDriver))
	javaHold(t, answers, "none-element", "same-build", "read")
	javaHold(t, answers, "none-element", "other-build", "read")
}

// javaNonCanonicalVariableBody runs the variable rows through the plain load,
// the retaining load and the load measure of NonCanonical.schema's Root.
const javaNonCanonicalVariableBody = `
        for (String[] row : variables) {
            String name = row[0];
            byte[] wire = hex(row[1]);
            byte[] expected = hex(row[2]);

            TableVariable.Report report = new TableVariable.Report();
            RootVariable.Value value = RootVariable.load(wire, report);
            String plain = value == null || report.damaged || report.refused ? "refused" : "read";
            if (plain.equals("read") && !java.util.Arrays.equals(RootVariable.save(value), expected)) { plain = "resave-differs"; }
            System.out.println(name + " plain " + plain);

            TableRetain retain = new TableRetain(1 << 16, 1 << 10);
            TableVariable.Report retainReport = new TableVariable.Report();
            RootVariable.Value retained = RootRetain.loadRetain(wire, retain, retainReport);
            String kept = retained == null || retainReport.damaged || retainReport.refused ? "refused" : "read";
            if (kept.equals("read")) {
                TableVariable.Report saveReport = new TableVariable.Report();
                byte[] saved = RootRetain.saveRetain(retained, retain, saveReport);
                if (saved == null || !java.util.Arrays.equals(saved, expected)) { kept = "resave-differs"; }
            }
            System.out.println(name + " retain " + kept);

            System.out.println(name + " measure " + (RootVariable.loadMeasure(wire) >= 0 ? "read" : "refused"));
        }
`

const javaNonCanonicalDriver = `import %%PKG%%.*;

public final class NonCanonicalDriver {
    private NonCanonicalDriver() {}

    static byte[] hex(String s) {
        byte[] b = new byte[s.length() / 2];
        for (int i = 0; i < b.length; i++) {
            b[i] = (byte) Integer.parseInt(s.substring(2 * i, 2 * i + 2), 16);
        }
        return b;
    }

    // load answers one batch's verdict under vocab, and for a read the batch
    // its value re-saves to (null when the save is refused).
    static byte[][] load(TableVocabulary vocab, byte[] wire) {
        %%VALUE%%[] values = new %%VALUE%%[1];
        TableVocabulary.Report report = new TableVocabulary.Report();
        int count = %%MESSAGE_CLASS%%.loadMessages(values, 1, vocab, wire, report);
        if (count != 1 || report.damaged || report.refused || values[0] == null) {
            return null;
        }
        byte[] out = new byte[wire.length * 2 + 64];
        int saved = %%MESSAGE_CLASS%%.saveMessages(values, 1, out, new TableVocabulary.Report());
        return new byte[][] { saved < 0 ? null : java.util.Arrays.copyOf(out, saved) };
    }

    public static void main(String[] args) {
        TableVocabulary vocab = new TableVocabulary();
        vocab.reset(16);
        if (!TableVocabulary.announceRead(vocab, hex("%%ANNOUNCE%%"), new TableVocabulary.Report()) || !vocab.announced) {
            System.out.println("announcement - refused");
            return;
        }

        String[][] announcements = {
%%ANNOUNCEMENTS%%        };
        for (String[] row : announcements) {
            byte[] want = hex(row[1]);
            byte[] out = new byte[TableVocabulary.announceMeasure()];
            int n = TableVocabulary.announce(out);
            TableVocabulary fresh = new TableVocabulary();
            fresh.reset(16);
            boolean read = n == want.length && java.util.Arrays.equals(out, want)
                    && TableVocabulary.announceRead(fresh, want, new TableVocabulary.Report()) && fresh.announced;
            System.out.println(row[0] + " announce " + (read ? "read" : "refused"));
        }

        String[][] variables = {
%%VARIABLE%%        };
%%VARIABLE_BODY%%
        String[][] messages = {
%%MESSAGE%%        };
        for (String[] row : messages) {
            String name = row[0];
            byte[] wire = hex(row[1]);

            byte[][] same = load(vocab, wire);
            String sameAnswer = same == null ? "refused" : same[0] != null && java.util.Arrays.equals(same[0], wire) ? "read" : "resave-differs";
            System.out.println(name + " same-build " + sameAnswer);

            vocab.buildVersion ^= 1;
            byte[][] other = load(vocab, wire);
            vocab.buildVersion ^= 1;
            String otherAnswer = "refused";
            if (other != null) {
                // the rewrite reads back at this reader's own build and is canonical
                byte[][] again = other[0] == null ? null : load(vocab, other[0]);
                otherAnswer = again != null && again[0] != null && java.util.Arrays.equals(again[0], other[0]) ? "read" : "rewrite-not-canonical";
            }
            System.out.println(name + " other-build " + otherAnswer);
        }
    }
}
`
