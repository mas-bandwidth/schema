package javatable

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The nullness pass adds a guard only where a null is impossible on every
// valid input (§16), and each guard refuses by the path that already exists
// beside it. These pin that every guard is emitted with its existing refusal;
// the unchanged roundtrip suites (TestMessageFormBatchJava, the variable,
// retain and cook-write probes) pin that valid behaviour does not move.
func TestJavaNullGuardsRefuseByTheExistingPath(t *testing.T) {
	t.Parallel()
	guards := []struct {
		unit, file, why string
		re              *regexp.Regexp
	}{
		{"messages", "InsertTextMessage.java", "array element shape: malformed",
			regexp.MustCompile(`TableVocabulary\.Shape n\d+e = entry\.element;\s+if \(n\d+e == null\) \{\s+r\.report\.malformed = true;\s+return false;`)},
		{"messages", "EditMessage.java", "union arm array element shape: malformed",
			regexp.MustCompile(`TableVocabulary\.Shape n\d+e = arm\d+\.element;\s+if \(n\d+e == null\) \{\s+r\.report\.malformed = true;\s+return false;`)},
		{"examples", "TableMessage.java", "nested skip without an element: skip fails",
			regexp.MustCompile(`if \(elem == null \|\| !skipAt\(r, elem, null, depth \+ 1\)\) \{\s+return false;`)},
		{"examples", "TableRetain.java", "resolve without ids: every ref refused by Resolver.id",
			regexp.MustCompile(`new Resolver\(buf, ids == null \? new long\[0\] : ids, off, limit\)`)},
		{"examples", "TableCookWrite.java", "record descriptor: IllegalArgumentException",
			regexp.MustCompile(`if \(info == null\) \{\s+throw new IllegalArgumentException\("field record"\);`)},
		{"examples", "TableCookWrite.java", "storage descriptor: IllegalArgumentException",
			regexp.MustCompile(`if \(storage == null\) \{\s+throw new IllegalArgumentException\("field storage"\);`)},
		{"examples", "TableJson.java", "integer token: bad",
			regexp.MustCompile(`public long integer\(boolean signed, long min, long max\) \{\s+boolean\[\] integral = new boolean\[1\];\s+String tok = scanNumber\(integral\);\s+if \(bad \|\| tok == null\) \{\s+return 0;`)},
		{"maps", "CellsVariable.java", "map key: bad",
			regexp.MustCompile(`String mapKey = in\.scanString\(\);\s+if \(in\.bad \|\| mapKey == null\) \{\s+in\.bad = true;\s+return;`)},
		{"examples", "TableFixed.java", "refused plan carries empty arrays",
			regexp.MustCompile(`new Plan\(new Entry\[0\], 0, new short\[0\], 0, 0, Reason\.layoutMalformed\)`)},
	}
	units := map[string]map[string][]byte{}
	for _, g := range guards {
		files, ok := units[g.unit]
		if !ok {
			files = generateCorpusUnit(t, g.unit)
			units[g.unit] = files
		}
		if !g.re.Match(files[g.file]) {
			t.Errorf("%s/%s: guard missing or refusing by another path (%s)", g.unit, g.file, g.why)
		}
	}
}

// TestJavaMessageArrayElementGuardPlant plants the otherwise unreachable
// state, an array entry with no element shape, immediately before the guard.
// The unplanted batch round-trips cleanly; the planted one is refused as
// malformed by the existing report, never a NullPointerException.
func TestJavaMessageArrayElementGuardPlant(t *testing.T) {
	t.Parallel()
	javaTools(t)
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitDir(t, root, "tables/messages")
	m := tabletext.NewModel(u)
	var insts []*tabletext.Instance
	for _, name := range []string{"message_open", "message_transaction", "message_batch", "message_trace", "message_arms", "message_arm_edits"} {
		raw, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "json", name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		inst := m.New(m.Lookup("ToolMessage"))
		var rep tabletext.Report
		if !m.Read(inst, raw, &rep) || rep != (tabletext.Report{}) {
			t.Fatalf("%s: the oracle did not read the JSON cleanly: %+v", name, rep)
		}
		insts = append(insts, inst)
	}
	wire, err := tablewire.EncodeMessages(m, insts)
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`
        final byte[] ann = fromHex(%q);
        final byte[] msg = fromHex(%q);
        final TableVocabulary vocab = new TableVocabulary();
        vocab.reset(4096);
        final TableVocabulary.Report arep = new TableVocabulary.Report();
        final boolean announced = TableVocabulary.announceRead(vocab, ann, arep);
        final ToolMessageFixed.Value[] values = new ToolMessageFixed.Value[TableMessage.batchMax];
        for (int i = 0; i < values.length; i++) { values[i] = new ToolMessageFixed.Value(); }
        final TableVocabulary.Report lrep = new TableVocabulary.Report();
        final int count = ToolMessageMessage.loadMessages(values, values.length, vocab, msg, lrep);
        say("plant", announced, "announced=" + announced + " count=" + count);
        System.out.println("MALFORMED " + lrep.malformed + " REFUSED " + lrep.refused);
`, hexOf(ir.TableAnnouncement(u)), hexOf(wire))
	clean := runMessageBatchProbeWith(t, u, body, nil)
	if !strings.Contains(clean, "MALFORMED false REFUSED false") {
		t.Fatalf("the unplanted batch did not read cleanly:\n%s", clean)
	}
	planted := runMessageBatchProbeWith(t, u, body, func(name, src string) string {
		return regexp.MustCompile(`(TableVocabulary\.Shape n\d+e = )(?:entry|arm\d+)\.element;`).ReplaceAllString(src, "${1}null;")
	})
	if !strings.Contains(planted, "MALFORMED true REFUSED false") {
		t.Fatalf("the planted missing element shape was not refused as malformed:\n%s", planted)
	}
}
