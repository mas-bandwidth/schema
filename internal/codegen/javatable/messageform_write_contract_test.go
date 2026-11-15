package javatable

// TestJavaSaveMessagesWriteContract holds the message-form writer to
// docs/SPEC.md §5.1's Java `SaveMessages` column over a fixed table carried by
// the message form: rows 1-2 and 8 refuse under `-ea` through the type's
// `checkWrite` predicate, and rows 3-7 keep the refusal they already carry.
// Every refused value has its lawful twin at the same boundary: the twin saves
// in both builds and loads back equal through loadMessages. A release build is
// asked only that rows 1-2 and 8 do not throw, because Java is an assert leg
// and the debug assert is the whole of the write-side mechanism.

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestJavaSaveMessagesWriteContract(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaToolsRequired(t)

	u := unitFrom(t, messageWriteContractSrc)
	ea, plain := runMessageWriteContract(t, u, javac, javaBin)
	factsEA := messageWriteFacts(t, ea)
	factsPlain := messageWriteFacts(t, plain)

	cases := []struct {
		id  string
		row int
	}{
		{"row1", 1},
		{"row2", 2},
		{"row3", 3},
		{"row4", 4},
		{"row5", 5},
		{"row6", 6},
		{"row7", 7},
		{"row8", 8},
	}
	for _, c := range cases {
		ea := factsEA[c.id]
		plain := factsPlain[c.id]
		switch c.row {
		case 1, 2, 8:
			if !ea.threw {
				t.Errorf("%s: under -ea the message writer did not throw AssertionError (result=%d)", c.id, ea.result)
			}
			if plain.threw {
				t.Errorf("%s: the release build threw, but the write check is the debug assert alone", c.id)
			}
		default:
			if !ea.threw && ea.result >= 0 {
				t.Errorf("%s: under -ea the message writer accepted a value the contract refuses (result=%d)", c.id, ea.result)
			}
		}
		if !ea.twin {
			t.Errorf("%s: the lawful twin did not save and load back equal under -ea", c.id)
		}
		if !plain.twin {
			t.Errorf("%s: the lawful twin did not save and load back equal in the release build", c.id)
		}
		if ea.twinThrew || plain.twinThrew {
			t.Errorf("%s: the lawful twin threw from saveMessages", c.id)
		}
	}
}

type messageWriteFact struct {
	threw     bool
	result    int
	twin      bool
	twinThrew bool
}

func messageWriteFacts(t *testing.T, out string) map[string]messageWriteFact {
	t.Helper()
	facts := map[string]messageWriteFact{}
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.Fields(line)
		if len(f) != 6 || f[0] != "WRITE" {
			continue
		}
		var fact messageWriteFact
		var err error
		if fact.threw, err = strconv.ParseBool(strings.TrimPrefix(f[2], "threw=")); err != nil {
			t.Fatalf("%s: parse threw: %v", line, err)
		}
		if fact.result, err = strconv.Atoi(strings.TrimPrefix(f[3], "result=")); err != nil {
			t.Fatalf("%s: parse result: %v", line, err)
		}
		if fact.twin, err = strconv.ParseBool(strings.TrimPrefix(f[4], "twin=")); err != nil {
			t.Fatalf("%s: parse twin: %v", line, err)
		}
		if fact.twinThrew, err = strconv.ParseBool(strings.TrimPrefix(f[5], "twinThrew=")); err != nil {
			t.Fatalf("%s: parse twinThrew: %v", line, err)
		}
		facts[f[1]] = fact
	}
	if len(facts) != 8 {
		t.Fatalf("the driver reported %d rows, want 8:\n%s", len(facts), out)
	}
	return facts
}

func runMessageWriteContract(t *testing.T, u *ir.Unit, javac, javaBin string) (string, string) {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	pkt, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatalf("packet generate: %v", err)
	}
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src", u.Package)
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	args := []string{"--release", "17", "-nowarn", "-d", classes}
	var names []string
	for name := range files {
		keep := name == "TableFixed.java" ||
			name == "TableVocabulary.java" ||
			name == "TableMessage.java" ||
			strings.HasSuffix(name, "Fixed.java") ||
			strings.HasSuffix(name, "Message.java")
		if keep {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		p := filepath.Join(srcDir, name)
		if err := os.WriteFile(p, files[name], 0o644); err != nil {
			t.Fatal(err)
		}
		args = append(args, p)
	}
	for _, name := range []string{"Int128.java", "UInt128.java"} {
		if data, ok := pkt[name]; ok {
			p := filepath.Join(srcDir, name)
			if err := os.WriteFile(p, data, 0o644); err != nil {
				t.Fatal(err)
			}
			args = append(args, p)
		}
	}
	driver := "package " + u.Package + ";\n\n" + messageWriteContractDriver
	driverPath := filepath.Join(srcDir, "Driver.java")
	if err := os.WriteFile(driverPath, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	args = append(args, driverPath)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	run := func(enableAssertions bool) string {
		t.Helper()
		javaArgs := []string{}
		if enableAssertions {
			javaArgs = append(javaArgs, "-ea")
		}
		javaArgs = append(javaArgs, "-cp", classes, u.Package+".Driver")
		out, err := exec.Command(javaBin, javaArgs...).CombinedOutput()
		if err != nil {
			t.Fatalf("java (ea=%t): %v\n%s", enableAssertions, err, out)
		}
		var lines []string
		for l := range strings.SplitSeq(string(out), "\n") {
			if strings.HasPrefix(l, "Picked up JAVA_TOOL_OPTIONS") {
				continue
			}
			lines = append(lines, l)
		}
		return strings.Join(lines, "\n")
	}
	return run(true), run(false)
}

const messageWriteContractSrc = `package writecheck

enum Mood { Calm, Angry, Sleepy }

union Choice
{
    amount uint16 | min = 0, max = 100
    note   string(8)
}

fixed table Root
{
    ranged  int16 | min = 0, max = 100
    bitsv   bits(6)
    counted [..4]uint16
    text    string(8)
    mood    Mood
    choice  Choice
    ratio   float32
}
`

const messageWriteContractDriver = `public final class Driver {
    private Driver() {}

    static final class Outcome {
        boolean threw;
        int result;
    }

    static Outcome save(RootFixed.Value v) {
        Outcome o = new Outcome();
        try {
            o.result = RootMessage.saveMessages(new RootFixed.Value[] { v }, 1, new byte[4096], new TableVocabulary.Report());
        } catch (AssertionError e) {
            o.threw = true;
            o.result = Integer.MIN_VALUE;
        }
        return o;
    }

    static boolean roundTrip(RootFixed.Value v) {
        try {
            byte[] buf = new byte[4096];
            int n = RootMessage.saveMessages(new RootFixed.Value[] { v }, 1, buf, new TableVocabulary.Report());
            if (n < 0) { return false; }
            TableVocabulary vocab = new TableVocabulary();
            vocab.reset(4096);
            byte[] ann = new byte[TableVocabulary.announceMeasure()];
            if (TableVocabulary.announce(ann) != ann.length) { return false; }
            if (!TableVocabulary.announceRead(vocab, ann, new TableVocabulary.Report())) { return false; }
            RootFixed.Value[] out = new RootFixed.Value[] { new RootFixed.Value() };
            TableVocabulary.Report rep = new TableVocabulary.Report();
            int count = RootMessage.loadMessages(out, 1, vocab, java.util.Arrays.copyOf(buf, n), rep);
            return count == 1 && !rep.damaged && !rep.refused && same(v, out[0]);
        } catch (AssertionError e) {
            return false;
        }
    }

    static boolean same(RootFixed.Value a, RootFixed.Value b) {
        if (a.ranged != b.ranged || a.bitsv != b.bitsv) { return false; }
        if (a.countedCount != b.countedCount) { return false; }
        for (int i = 0; i < a.countedCount; i++) {
            if (a.counted[i] != b.counted[i]) { return false; }
        }
        if (a.textLength != b.textLength) { return false; }
        for (int i = 0; i < a.textLength; i++) {
            if (a.text[i] != b.text[i]) { return false; }
        }
        if (a.mood != b.mood || a.choice.type != b.choice.type) { return false; }
        if (a.choice.type == 1 && a.choice.amount != b.choice.amount) { return false; }
        if (a.choice.type == 2) {
            if (a.choice.noteLength != b.choice.noteLength) { return false; }
            for (int i = 0; i < a.choice.noteLength; i++) {
                if (a.choice.note[i] != b.choice.note[i]) { return false; }
            }
        }
        return Float.floatToRawIntBits(a.ratio) == Float.floatToRawIntBits(b.ratio);
    }

    static void setText(RootFixed.Value v, byte[] b) {
        v.textLength = b.length;
        System.arraycopy(b, 0, v.text, 0, b.length);
    }

    static void report(String id, Outcome invalid, RootFixed.Value twinValue) {
        Outcome twin = save(twinValue);
        boolean twinOk = !twin.threw && twin.result >= 0 && roundTrip(twinValue);
        System.out.println("WRITE " + id + " threw=" + invalid.threw + " result=" + invalid.result
                + " twin=" + twinOk + " twinThrew=" + twin.threw);
    }

    public static void main(String[] args) {
        {
            RootFixed.Value bad = new RootFixed.Value();
            bad.ranged = 101;
            RootFixed.Value good = new RootFixed.Value();
            good.ranged = 100;
            report("row1", save(bad), good);
        }
        {
            RootFixed.Value bad = new RootFixed.Value();
            bad.bitsv = 64;
            RootFixed.Value good = new RootFixed.Value();
            good.bitsv = 63;
            report("row2", save(bad), good);
        }
        {
            RootFixed.Value bad = new RootFixed.Value();
            bad.countedCount = 5;
            RootFixed.Value good = new RootFixed.Value();
            good.countedCount = 4;
            good.counted[0] = 1;
            good.counted[1] = 2;
            good.counted[2] = 3;
            good.counted[3] = 4;
            report("row3", save(bad), good);
        }
        {
            RootFixed.Value bad = new RootFixed.Value();
            bad.textLength = 9;
            RootFixed.Value good = new RootFixed.Value();
            setText(good, new byte[] { 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h' });
            report("row4", save(bad), good);
        }
        {
            RootFixed.Value bad = new RootFixed.Value();
            bad.mood = 4;
            RootFixed.Value good = new RootFixed.Value();
            good.mood = 3;
            report("row5", save(bad), good);
        }
        {
            RootFixed.Value bad = new RootFixed.Value();
            bad.choice.type = 3;
            RootFixed.Value good = new RootFixed.Value();
            good.choice.type = 1;
            good.choice.amount = 100;
            report("row6", save(bad), good);
        }
        {
            RootFixed.Value bad = new RootFixed.Value();
            bad.ratio = Float.NaN;
            RootFixed.Value good = new RootFixed.Value();
            good.ratio = 1.5f;
            report("row7", save(bad), good);
        }
        {
            RootFixed.Value bad = new RootFixed.Value();
            setText(bad, new byte[] { 'a', (byte) 0xff, 'b' });
            RootFixed.Value good = new RootFixed.Value();
            setText(good, new byte[] { 'h', (byte) 0xc3, (byte) 0xa9, 'l', 'l', 'o' });
            report("row8", save(bad), good);
        }
    }
}
`
