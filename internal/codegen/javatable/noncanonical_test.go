package javatable

// TestJavaReadsRefuseNonCanonical holds docs/SPEC-TABLES.md §3, §3.1, §3.3 and
// §4's one-encoding rule on the Java reads (docs/rulings.json
// reads-refuse-non-canonical): the shared vectors under
// testdata/wire/non-canonical name one canonical file and one file per
// non-canonical spelling, and the message-one-encoding fixture beside it names
// the three message-form spellings it owns. Every variable row is read through
// the plain load and the retaining load; every message row through the batch
// load. The region measure is the extent scan (§6.5): it sizes what a load would
// allocate and answers no body verdict, so it is not a read entry here. The
// canonical rows read and their values re-save to the same bytes; every other
// row is refused. The verdict alone is the contract, so no refusal reason is
// compared.

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
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

func javaRunDriver(t *testing.T, javac, javaBin string, files map[string][]byte, pkg, driver string) string {
	t.Helper()
	dir := compileCookUnit(t, javac, files, pkg, map[string]string{"NonCanonicalDriver": driver})
	out, err := exec.Command(javaBin, "-ea", "-cp", dir, "NonCanonicalDriver").CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	return string(out)
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

	var variableRows, messageRows, oneRows strings.Builder
	groups := map[string]bool{}
	for _, row := range rows {
		path := filepath.Join(vectors, row.file)
		switch row.form {
		case "variable":
			groups["variable"] = true
			fmt.Fprintf(&variableRows, "        {%q, %q, %q},\n", row.file, javaHexFile(t, path), row.verdict)
		case "announcement":
			groups["announcement"] = true
		case "message":
			if strings.HasPrefix(row.file, "../message-one-encoding/") {
				groups["one"] = true
				fmt.Fprintf(&oneRows, "        {%q, %q, %q},\n", row.file, javaHexFile(t, path), row.verdict)
			} else {
				groups["message"] = true
				fmt.Fprintf(&messageRows, "        {%q, %q, %q},\n", row.file, javaHexFile(t, path), row.verdict)
			}
		default:
			t.Fatalf("%s: form %q is not variable, message or announcement", row.file, row.form)
		}
	}
	for _, group := range []string{"variable", "message", "announcement", "one"} {
		if !groups[group] {
			t.Errorf("the manifest names no %s row, so the test did not reach it", group)
		}
	}

	oneVectors := filepath.Join(repo, "testdata", "wire", "message-one-encoding")
	oneAnnouncement := javaHexFile(t, filepath.Join(oneVectors, "announcement.bin"))

	javac, javaBin := javaTools(t)

	nonCanonicalDriver := strings.NewReplacer(
		"%%PKG%%", nonCanonical.Package,
		"%%ANNOUNCE%%", javaHexFile(t, filepath.Join(vectors, "announcement.bin")),
		"%%VARIABLE%%", variableRows.String(),
		"%%MESSAGE%%", messageRows.String(),
	).Replace(javaNonCanonicalDriver)
	files, err := Generate(nonCanonical)
	if err != nil {
		t.Fatalf("noncanonical: Generate: %v", err)
	}
	if out := javaRunDriver(t, javac, javaBin, files, nonCanonical.Package, nonCanonicalDriver); strings.TrimSpace(out) != "ok" {
		t.Fatalf("noncanonical driver printed %q, want ok", out)
	}

	oneDriver := strings.NewReplacer(
		"%%PKG%%", one.Package,
		"%%ANNOUNCE%%", oneAnnouncement,
		"%%MESSAGE%%", oneRows.String(),
	).Replace(javaOneEncodingDriver)
	files, err = Generate(one)
	if err != nil {
		t.Fatalf("message-one-encoding: Generate: %v", err)
	}
	if out := javaRunDriver(t, javac, javaBin, files, one.Package, oneDriver); strings.TrimSpace(out) != "ok" {
		t.Fatalf("message-one-encoding driver printed %q, want ok", out)
	}
}

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

    static void fail(String msg) {
        System.out.println("FAILED: " + msg);
        System.exit(1);
    }

    public static void main(String[] args) {
        byte[] announcement = hex("%%ANNOUNCE%%");
        TableVocabulary vocab = new TableVocabulary();
        vocab.reset(16);
        TableVocabulary.Report announced = new TableVocabulary.Report();
        if (!TableVocabulary.announceRead(vocab, announcement, announced) || !vocab.announced) {
            fail("the announcement was refused");
        }

        String[][] variables = {
%%VARIABLE%%        };
        for (String[] row : variables) {
            String name = row[0];
            byte[] wire = hex(row[1]);
            boolean wantRead = row[2].equals("read");

            TableVariable.Report report = new TableVariable.Report();
            RootVariable.Value value = RootVariable.load(wire, report);
            TableRetain retain = new TableRetain(1 << 16, 1 << 10);
            TableVariable.Report retainReport = new TableVariable.Report();
            RootVariable.Value retained = RootRetain.loadRetain(wire, retain, retainReport);

            if (wantRead) {
                if (value == null || report.damaged || report.refused) { fail(name + " plain load"); }
                if (retained == null || retainReport.damaged || retainReport.refused) { fail(name + " retaining load"); }
                if (!java.util.Arrays.equals(RootVariable.save(value), wire)) { fail(name + " plain re-save"); }
                if (!java.util.Arrays.equals(RootVariable.save(retained), wire)) { fail(name + " retaining re-save"); }
            } else {
                if (value != null || !(report.damaged || report.refused)) { fail(name + " plain load accepted"); }
                if (retained != null || !(retainReport.damaged || retainReport.refused)) { fail(name + " retaining load accepted"); }
            }
        }

        String[][] messages = {
%%MESSAGE%%        };
        for (String[] row : messages) {
            String name = row[0];
            byte[] wire = hex(row[1]);
            boolean wantRead = row[2].equals("read");
            MessageFixed.Value[] values = new MessageFixed.Value[1];
            TableVocabulary.Report report = new TableVocabulary.Report();
            int count = MessageMessage.loadMessages(values, 1, vocab, wire, report);
            if (wantRead) {
                if (count != 1 || report.damaged || report.refused || values[0] == null) { fail(name + " message load"); }
                byte[] out = new byte[wire.length * 2 + 64];
                TableVocabulary.Report savedReport = new TableVocabulary.Report();
                int saved = MessageMessage.saveMessages(values, 1, out, savedReport);
                if (saved < 0 || !java.util.Arrays.equals(java.util.Arrays.copyOf(out, saved), wire)) {
                    fail(name + " message re-save");
                }
            } else {
                if (count != 0 || !(report.damaged || report.refused)) { fail(name + " message load accepted"); }
            }
        }
        System.out.println("ok");
    }
}
`

const javaOneEncodingDriver = `import %%PKG%%.*;

public final class NonCanonicalDriver {
    private NonCanonicalDriver() {}

    static byte[] hex(String s) {
        byte[] b = new byte[s.length() / 2];
        for (int i = 0; i < b.length; i++) {
            b[i] = (byte) Integer.parseInt(s.substring(2 * i, 2 * i + 2), 16);
        }
        return b;
    }

    static void fail(String msg) {
        System.out.println("FAILED: " + msg);
        System.exit(1);
    }

    public static void main(String[] args) {
        byte[] announcement = hex("%%ANNOUNCE%%");
        TableVocabulary vocab = new TableVocabulary();
        vocab.reset(16);
        TableVocabulary.Report announced = new TableVocabulary.Report();
        if (!TableVocabulary.announceRead(vocab, announcement, announced) || !vocab.announced) {
            fail("the announcement was refused");
        }
        String[][] messages = {
%%MESSAGE%%        };
        for (String[] row : messages) {
            String name = row[0];
            byte[] wire = hex(row[1]);
            boolean wantRead = row[2].equals("read");
            OneFixed.Value[] values = new OneFixed.Value[1];
            TableVocabulary.Report report = new TableVocabulary.Report();
            int count = OneMessage.loadMessages(values, 1, vocab, wire, report);
            if (wantRead) {
                if (count != 1 || report.damaged || report.refused || values[0] == null) { fail(name + " message load"); }
                byte[] out = new byte[wire.length * 2 + 64];
                TableVocabulary.Report savedReport = new TableVocabulary.Report();
                int saved = OneMessage.saveMessages(values, 1, out, savedReport);
                if (saved < 0 || !java.util.Arrays.equals(java.util.Arrays.copyOf(out, saved), wire)) {
                    fail(name + " message re-save");
                }
            } else {
                if (count != 0 || !(report.damaged || report.refused)) { fail(name + " message load accepted"); }
            }
        }
        System.out.println("ok");
    }
}
`
