package javatable

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
	"os"
	"path/filepath"
	"sort"
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
const (
	countMinReader = `package probe

type Inner { x int32 }

table Probe
{
    items [2..4]int32
    inner [2..4]Inner
}

fixed table Msg
{
    items [2..4]int32
}
`
	countMinWriter = `package probe

type Inner { x int32 }

table Probe
{
    items [..4]int32
    inner [..4]Inner
}

fixed table Msg
{
    items [..4]int32
}
`
)

func TestJavaCountBelowMinRefused(t *testing.T) {
	t.Parallel()
	reader := unitFrom(t, countMinReader)
	writer := unitFrom(t, countMinWriter)
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
	encode := func(m *tabletext.Model, table, text string, enc func(*tabletext.Model, *tabletext.Instance) ([]byte, error)) string {
		inst := m.New(m.Lookup(table))
		var rep tabletext.Report
		if !m.Read(inst, []byte(text), &rep) || !rep.Silent() {
			t.Fatalf("oracle read %s: %+v", text, rep)
		}
		w, err := enc(m, inst)
		if err != nil {
			t.Fatalf("oracle encode %s: %v", text, err)
		}
		return hexOf(w)
	}
	messages := func(m *tabletext.Model, inst *tabletext.Instance) ([]byte, error) {
		return tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
	}
	fixedLawful := func() []byte {
		inst := rm.New(rm.Lookup("Msg"))
		var rep tabletext.Report
		if !rm.Read(inst, []byte(`{"items":[287454020,1432778632]}`), &rep) || !rep.Silent() {
			t.Fatalf("oracle read: %+v", rep)
		}
		w, err := tablewire.EncodeFixed(rm, inst)
		if err != nil {
			t.Fatal(err)
		}
		return w
	}()
	// the count word lies just before the first element (287454020 is
	// 0x11223344): rewritten to 1
	fixedForged := bytes.Clone(fixedLawful)
	at := bytes.Index(fixedForged, []byte{0x44, 0x33, 0x22, 0x11})
	if at < 4 || binary.LittleEndian.Uint32(fixedForged[at-4:]) != 2 {
		t.Fatal("no count word 2 before the first element in the fixed save")
	}
	binary.LittleEndian.PutUint32(fixedForged[at-4:], 1)

	cases := map[string]string{
		"VAR_ITEMS_1": encode(wm, "Probe", `{"items":[77]}`, tablewire.EncodeVariable),
		"VAR_ITEMS_2": encode(wm, "Probe", `{"items":[77,78],"inner":[{},{}]}`, tablewire.EncodeVariable),
		"VAR_INNER_1": encode(wm, "Probe", `{"items":[77,78],"inner":[{"x":1}]}`, tablewire.EncodeVariable),
		"FIXED_2":     hexOf(fixedLawful),
		"FIXED_1":     hexOf(fixedForged),
		"MSG_1":       encode(wm, "Msg", `{"items":[77]}`, messages),
		"MSG_2":       encode(wm, "Msg", `{"items":[77,78]}`, messages),
		"ANNOUNCE":    hexOf(tablewire.Announce(writer)),
	}
	driver := countMinDriver
	for k, v := range cases {
		driver = strings.ReplaceAll(driver, k, v)
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
		files["Driver.java"] = []byte(driver)
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
		sort.Strings(sources)
		args := append([]string{"--release", "17", "-nowarn", "-d", dir}, sources...)
		if out, err := testproc.Command(t, javac, args...).CombinedOutput(); err != nil {
			t.Fatalf("javac: %v\n%s", err, out)
		}
		out, err := testproc.Command(t, java, "-ea", "-cp", dir, reader.Package+".Driver").CombinedOutput()
		if err != nil {
			t.Fatalf("java: %v\n%s", err, out)
		}
		n := 0
		for line := range strings.SplitSeq(string(out), "\n") {
			if rest, ok := strings.CutPrefix(line, "CHECK "); ok {
				n++
				if label, verdict, _ := strings.Cut(rest, " "); verdict != "ok" {
					t.Errorf("%s: %s", label, verdict)
				}
			}
			if rest, ok := strings.CutPrefix(line, "NOTE "); ok {
				t.Log(rest)
			}
		}
		if n != len(countMinLabels) {
			t.Errorf("the probe made %d checks, want %d:\n%s", n, len(countMinLabels), out)
		}
	})
}

var countMinLabels = []string{
	"variable-items-1", "variable-items-2", "variable-inner-1",
	"json-items-1", "json-items-2", "fixed-1", "fixed-2", "message-1", "message-2",
}

var countMinDriver = fmt.Sprintf(`package probe;

import java.nio.charset.StandardCharsets;

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

    public static void main(String[] args) {
        variable("variable-items-1", "VAR_ITEMS_1", true);
        variable("variable-items-2", "VAR_ITEMS_2", false);
        variable("variable-inner-1", "VAR_INNER_1", true);
        json("json-items-1", %q, true);
        json("json-items-2", %q, false);
        fixed("fixed-1", "FIXED_1", true);
        fixed("fixed-2", "FIXED_2", false);
        TableVocabulary vocab = new TableVocabulary();
        vocab.reset(16);
        TableVocabulary.Report arep = new TableVocabulary.Report();
        if (!TableVocabulary.announceRead(vocab, fromHex("ANNOUNCE"), arep)) { System.out.println("CHECK announce FAIL " + arep.reason); return; }
        message("message-1", vocab, "MSG_1", true);
        message("message-2", vocab, "MSG_2", false);
        System.out.println("NOTE a fresh value holds " + new ProbeVariable.Value().items.size() + " items and " + new MsgFixed.Value().itemsCount + " fixed items");
    }
}
`, `{"items":[77],"inner":[{},{}]}`, `{"items":[77,78],"inner":[{},{}]}`)
