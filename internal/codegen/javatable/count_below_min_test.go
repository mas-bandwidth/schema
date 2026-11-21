package javatable

import (
	"bytes"
	"encoding/binary"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A COUNTED ARRAY [Min..Max]T BELOW ITS MINIMUM IS REFUSED ON EVERY READ of
// the Java leg (docs/SPEC.md §5.1 row 3; docs/SPEC-TABLES.md §3.3, §3.4;
// review-owed/cross-leg/counted-array-min), count_over_bound as one past the
// bound is: the variable read (a scalar and a struct element), the JSON read,
// the fixed read and the message read. The bytes are the Go engine's, written
// under a [..4] declaration (or, for the fixed read, the reader's own layout
// with its count word rewritten); the lawful twin of each case reads.
//
// The two schemas and one row per case are the committed corpus
// testdata/count-below-min/{reader.schema,writer.schema,cases.tsv} (F-203).
const countMinDir = "testdata/count-below-min"

// countMinCovers is what the committed rows must hold between them: each form
// below its minimum and at it.
var countMinCovers = []string{
	"variable-items-1", "variable-items-2", "variable-inner-1",
	"json-items-1", "json-items-2", "fixed-1", "fixed-2", "message-1", "message-2",
}

func TestJavaCountBelowMinRefused(t *testing.T) {
	t.Parallel()
	casesPath := filepath.Join(countMinDir, "cases.tsv")
	rows := corpusRows(t, casesPath, 6)
	inCorpus := map[string]bool{}
	for _, r := range rows {
		inCorpus[r[0]] = true
	}
	for _, label := range countMinCovers {
		if !inCorpus[label] {
			t.Errorf("%s has no %s row", casesPath, label)
		}
	}
	reader := unitFrom(t, corpusSchema(t, countMinDir, "reader.schema"))
	writer := unitFrom(t, corpusSchema(t, countMinDir, "writer.schema"))
	files, err := Generate(reader)
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, name := range sortedNames(files) {
		all.Write(files[name])
	}
	squeeze := func(s string) string { return strings.Join(strings.Fields(s), "") }
	src := squeeze(all.String())
	for _, want := range []string{
		`if (v.items.size() < 2) { report.refuse("count_over_bound"); }`,
		`if (v.items.size() < 2) { in.rep.refuse("count_over_bound"); }`,
		`if (n < 2) {`,
	} {
		if !strings.Contains(src, squeeze(want)) {
			t.Errorf("the generated Java lacks %s", want)
		}
	}

	rm := tabletext.NewModel(reader)
	wm := tabletext.NewModel(writer)
	oracle := func(m *tabletext.Model, table, text string) *tabletext.Instance {
		inst := m.New(m.Lookup(table))
		var rep tabletext.Report
		if !m.Read(inst, []byte(text), &rep) || !rep.Silent() {
			t.Fatalf("oracle read %s: %+v", text, rep)
		}
		return inst
	}
	// payload is what the driver reads for one row: the JSON text itself, or
	// the hex of the bytes the row's form reads.
	payload := func(form, table, value, forge string) string {
		var w []byte
		var err error
		switch form {
		case "json":
			return value
		case "variable":
			w, err = tablewire.EncodeVariable(wm, oracle(wm, table, value))
		case "message":
			w, err = tablewire.EncodeMessages(wm, []*tabletext.Instance{oracle(wm, table, value)})
		case "fixed":
			w, err = tablewire.EncodeFixed(rm, oracle(rm, table, value))
		default:
			t.Fatalf("no form %s", form)
		}
		if err != nil {
			t.Fatalf("oracle encode %s %s: %v", form, value, err)
		}
		if forge != "-" {
			spelled, ok := strings.CutPrefix(forge, "count=")
			n, perr := strconv.ParseUint(spelled, 10, 32)
			if !ok || perr != nil || form != "fixed" {
				t.Fatalf("forge %s on the %s form: only count=N on the fixed form is spelled", forge, form)
			}
			// the count word lies just before the first element (287454020
			// is 0x11223344)
			at := bytes.Index(w, []byte{0x44, 0x33, 0x22, 0x11})
			if at < 4 || binary.LittleEndian.Uint32(w[at-4:]) != 2 {
				t.Fatal("no count word 2 before the first element in the fixed save")
			}
			w = bytes.Clone(w)
			binary.LittleEndian.PutUint32(w[at-4:], uint32(n))
		}
		return hexOf(w)
	}
	var plan strings.Builder
	for _, r := range rows {
		id, form, table, value, forge, want := r[0], r[1], r[2], r[3], r[4], r[5]
		if want != "refused" && want != "read" {
			t.Fatalf("%s: want %s, not refused or read", id, want)
		}
		plan.WriteString(id + "\t" + form + "\t" + payload(form, table, value, forge) + "\t" + want + "\n")
	}

	t.Run("jdk", func(t *testing.T) {
		slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
		javac, java := javaTools(t)
		packet, err := javacodegen.Generate(reader)
		if err != nil {
			t.Fatal(err)
		}
		for name, data := range packet {
			if _, ok := files[name]; !ok {
				files[name] = data
			}
		}
		files["Driver.java"] = []byte(countMinDriver)
		dir := t.TempDir()
		var sources []string
		for name, data := range files {
			p := filepath.Join(dir, name)
			if err := os.WriteFile(p, data, 0o644); err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(name, ".java") {
				sources = append(sources, p)
			}
		}
		planPath := filepath.Join(dir, "plan.tsv")
		if err := os.WriteFile(planPath, []byte(plan.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		sort.Strings(sources)
		args := append([]string{"--release", "17", "-nowarn", "-d", dir}, sources...)
		if out, err := testproc.Command(t, javac, args...).CombinedOutput(); err != nil {
			t.Fatalf("javac: %v\n%s", err, out)
		}
		out, err := testproc.Command(t, java, "-ea", "-cp", dir, reader.Package+".Driver", hexOf(tablewire.Announce(writer)), planPath).CombinedOutput()
		if err != nil {
			t.Fatalf("java: %v\n%s", err, out)
		}
		seen := map[string]bool{}
		for line := range strings.SplitSeq(string(out), "\n") {
			if rest, ok := strings.CutPrefix(line, "CHECK "); ok {
				label, verdict, _ := strings.Cut(rest, " ")
				seen[label] = true
				if verdict != "ok" {
					t.Errorf("%s: %s", label, verdict)
				}
			}
			if rest, ok := strings.CutPrefix(line, "NOTE "); ok {
				t.Log(rest)
			}
		}
		for _, r := range rows {
			if !seen[r[0]] {
				t.Errorf("the probe made no %s check:\n%s", r[0], out)
			}
		}
	})
}

const countMinDriver = `package probe;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;

public final class Driver {
    static byte[] fromHex(String s) {
        final byte[] out = new byte[s.length() / 2];
        for (int i = 0; i < out.length; i++) { out[i] = (byte) Integer.parseInt(s.substring(2 * i, 2 * i + 2), 16); }
        return out;
    }

    static void say(String label, boolean ok, String detail) {
        System.out.println("CHECK " + label + (ok ? " ok" : " FAIL " + detail));
    }

    static void variable(String label, String hex, boolean refuse) {
        TableVariable.Report r = new TableVariable.Report();
        ProbeVariable.Value v = ProbeVariable.load(fromHex(hex), r);
        boolean refused = v == null || r.refused;
        say(label, refuse ? (refused && "count_over_bound".equals(r.reason)) : !refused, "loaded=" + (v != null) + " refused=" + r.refused + " reason=" + r.reason);
    }

    static void json(String label, String text, boolean refuse) {
        TableVariable.Report r = new TableVariable.Report();
        ProbeVariable.Value v = ProbeVariable.fromJson(text.getBytes(StandardCharsets.UTF_8), r);
        boolean refused = v == null || r.refused;
        say(label, refuse ? (refused && "count_over_bound".equals(r.reason)) : !refused, "loaded=" + (v != null) + " refused=" + r.refused + " reason=" + r.reason);
    }

    static void fixed(String label, String hex, boolean refuse) {
        byte[] data = fromHex(hex);
        MsgFixed.Value[] back = { new MsgFixed.Value() };
        TableFixed.Report r = new TableFixed.Report();
        int n = MsgFixed.load(back, 1, data, TableFixed.plan(1 << 14), new short[1 << 14], MsgFixed.image(), r);
        boolean refused = n != 1 || r.refused;
        say(label, refuse ? (refused && r.reason == TableFixed.Reason.countOverBound) : !refused, "n=" + n + " refused=" + r.refused + " reason=" + r.reason);
    }

    static void message(String label, TableVocabulary vocab, String hex, boolean refuse) {
        MsgFixed.Value[] values = { new MsgFixed.Value() };
        TableVocabulary.Report r = new TableVocabulary.Report();
        int n = MsgMessage.loadMessages(values, 1, vocab, fromHex(hex), r);
        boolean refused = n != 1 || r.refused || r.damaged;
        say(label, refuse ? (refused && "count_over_bound".equals(r.reason)) : !refused, "n=" + n + " refused=" + r.refused + " damaged=" + r.damaged + " reason=" + r.reason);
    }

    public static void main(String[] args) throws Exception {
        TableVocabulary vocab = new TableVocabulary();
        vocab.reset(16);
        TableVocabulary.Report arep = new TableVocabulary.Report();
        if (!TableVocabulary.announceRead(vocab, fromHex(args[0]), arep)) { System.out.println("CHECK announce FAIL " + arep.reason); return; }
        for (String line : Files.readAllLines(Path.of(args[1]), StandardCharsets.UTF_8)) {
            if (line.isEmpty()) { continue; }
            String[] f = line.split("\t", -1);
            String label = f[0], form = f[1], payload = f[2];
            boolean refuse = f[3].equals("refused");
            switch (form) {
                case "variable": variable(label, payload, refuse); break;
                case "json": json(label, payload, refuse); break;
                case "fixed": fixed(label, payload, refuse); break;
                case "message": message(label, vocab, payload, refuse); break;
                default: System.out.println("CHECK " + label + " FAIL no form " + form);
            }
        }
        System.out.println("NOTE a fresh value holds " + new ProbeVariable.Value().items.size() + " items and " + new MsgFixed.Value().itemsCount + " fixed items");
    }
}
`
