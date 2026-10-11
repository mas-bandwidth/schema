package javatable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
	javapkt "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
)

// TestMessageFormBatchJava pins the MESSAGE FORM (form byte 2, a batch of
// bitpacked bodies under one announced vocabulary, docs/SPEC-TABLES.md §3.3)
// on the Java leg's generated surface. One subtest per clause of the card.
//
// The surface this test names is the language's own spelling of the Go leg's
// one (internal/codegen/gotable/message.go): `measureMessages`,
// `saveMessages`, `loadMessages` on the root's generated message class, and
// the unit's announcement (`announceMeasure`/`announce`/`announceRead`) on the
// shared `TableMessage` runtime. A body's value is the packet emitter's class
// for the root, so the test emits both halves and compiles them together.
func TestMessageFormBatchJava(t *testing.T) {
	t.Parallel()

	u := schemaUnit(t, "M1.schema")

	t.Run("manifest_message_rows", func(t *testing.T) {
		t.Parallel()
		assertMessageSurface(t, u)
		expected := m1OpenBatch(t, u)
		driver := fmt.Sprintf(msgManifestDriver, javaByteArray(expected), len(expected))
		if out := compileAndRunMessage(t, u, driver); !strings.Contains(out, "manifest ok") {
			t.Fatalf("message rows: %s", out)
		}
	})

	t.Run("refusals", func(t *testing.T) {
		t.Parallel()
		assertMessageSurface(t, u)
		if out := compileAndRunMessage(t, u, msgRefusalDriver); !strings.Contains(out, "refusals ok") {
			t.Fatalf("refusals: %s", out)
		}
	})

	t.Run("damage_and_forms", func(t *testing.T) {
		t.Parallel()
		assertMessageSurface(t, u)
		if out := compileAndRunMessage(t, u, msgDamageDriver); !strings.Contains(out, "damage ok") {
			t.Fatalf("damage and forms: %s", out)
		}
	})
}

// assertMessageSurface is the go-level half of every clause: the generated unit
// must carry the message surface at all. It runs without a JDK, so the clauses
// are RED in an environment that has none, and it pins the names the drivers
// below call (docs/SPEC-TABLES.md §3.3).
func assertMessageSurface(t *testing.T, u *ir.Unit) {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, data := range files {
		all.Write(data)
	}
	src := all.String()
	for _, want := range []string{
		"measureMessages", "saveMessages", "loadMessages",
		"announceMeasure", "announceRead", "batch_too_large", "no_vocabulary",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("the generated Java carries no message form: %q is absent (docs/SPEC-TABLES.md §3.3)", want)
		}
	}
}

// m1OpenBatch is the form-2 batch of two copies of the manifest's m1_open
// instance: the bytes `SaveMessages` must return exactly, and the length
// `MeasureMessages` must equal (docs/SPEC-TABLES.md §3.3).
func m1OpenBatch(t *testing.T, u *ir.Unit) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	v := m.New(m.Lookup("Msg"))
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "conformance", "tables", "json", "m1_open.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report tabletext.Report
	if !m.Read(v, raw, &report) || !report.Silent() {
		t.Fatalf("text %+v", report)
	}
	expected, err := tablewire.EncodeMessages(m, []*tabletext.Instance{v, v})
	if err != nil {
		t.Fatal(err)
	}
	return expected
}

func javaByteArray(b []byte) string {
	if len(b) == 0 {
		return "0"
	}
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprintf("(byte)0x%02x", v)
	}
	return strings.Join(parts, ", ")
}

// compileAndRunMessage emits the packet and table Java for the unit, compiles
// them with the driver, runs the driver, and returns its combined output. A
// compile failure is the red: the message surface is absent today.
func compileAndRunMessage(t *testing.T, u *ir.Unit, driver string) string {
	t.Helper()
	javac, javaBin := javaTools(t)
	packet, err := javapkt.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	table, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	pkg := u.Package
	if err := os.MkdirAll(filepath.Join(src, pkg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range packet {
		if err := os.WriteFile(filepath.Join(src, pkg, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range table {
		if err := os.WriteFile(filepath.Join(src, pkg, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(src, pkg, "MessageDriver.java"), []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(src, pkg, "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-d", classes}, files...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		return fmt.Sprintf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, pkg+".MessageDriver").CombinedOutput()
	if err != nil {
		return fmt.Sprintf("java: %v\n%s", err, out)
	}
	return string(out)
}

// 2.1: every manifest `message` row — LoadMessages of the batch gives the
// values the form-1 instance decodes to, SaveMessages gives the batch bytes
// back exactly, and MeasureMessages equals the length (docs/SPEC-TABLES.md
// §3.3).
const msgManifestDriver = `package tblm1;

import java.nio.charset.StandardCharsets;

public final class MessageDriver {
    private MessageDriver() {}

    static void fail(String what) {
        System.out.println("FAIL " + what);
        System.exit(1);
    }

    static byte[] text(String s) {
        return s.getBytes(StandardCharsets.UTF_8);
    }

    public static void main(String[] args) {
        final Msg m = new Msg();
        m.seq = 1;
        m.body.type = BodyType.open;
        final byte[] p = text("a.md");
        System.arraycopy(p, 0, m.body.open.path, 0, p.length);
        m.body.open.pathLength = p.length;

        final Msg[] values = { m, m };
        final TableReport measure = new TableReport();
        final int len = MsgMessage.measureMessages(values, measure);
        if (len != %[2]d) { fail("measure " + len); }
        final byte[] expected = { %[1]s };
        final byte[] buf = new byte[len];
        final TableReport wrote = new TableReport();
        if (MsgMessage.saveMessages(values, buf, wrote) != len) { fail("save"); }
        for (int i = 0; i < buf.length; i++) {
            if (buf[i] != expected[i]) { fail("byte " + i); }
        }

        final TableMessage.Vocabulary vocab = ownVocabulary();
        final Msg[] decoded = { new Msg(), new Msg() };
        final TableReport read = new TableReport();
        final int n = MsgMessage.loadMessages(decoded, vocab, buf, read);
        if (n != 2) { fail("load " + n); }
        if (decoded[0].seq != 1) { fail("seq"); }
        final String got = new String(decoded[0].body.open.path, 0,
                decoded[0].body.open.pathLength, StandardCharsets.UTF_8);
        if (!got.equals("a.md")) { fail("text " + got); }
        System.out.println("manifest ok");
    }

    static TableMessage.Vocabulary ownVocabulary() {
        final byte[] ann = new byte[TableMessage.announceMeasure()];
        TableMessage.announce(ann);
        final TableMessage.Vocabulary vocab = new TableMessage.Vocabulary();
        final TableReport report = new TableReport();
        if (!TableMessage.announceRead(vocab, ann, report)) { fail("announce"); }
        return vocab;
    }
}
`

// 2.2: no_vocabulary; batch_too_large on the write side (M above 256, -1 with
// the reason) and the read side (M above capacity, refused before decoding,
// count set to M) (docs/SPEC-TABLES.md §3.3).
const msgRefusalDriver = `package tblm1;

public final class MessageDriver {
    private MessageDriver() {}

    static void fail(String what) {
        System.out.println("FAIL " + what);
        System.exit(1);
    }

    public static void main(String[] args) {
        // A body from a peer that never announced is refused by name.
        final TableMessage.Vocabulary unannounced = new TableMessage.Vocabulary();
        final Msg[] one = { new Msg() };
        final TableReport noVocab = new TableReport();
        final int refused = MsgMessage.loadMessages(one, unannounced, new byte[] { 2, 0, 0, 0 }, noVocab);
        if (refused != 0 || !"no_vocabulary".equals(noVocab.reason)) {
            fail("no_vocabulary " + refused + " " + noVocab.reason);
        }

        // M above 256 on the write side.
        final Msg[] huge = new Msg[TableMessage.max + 1];
        for (int i = 0; i < huge.length; i++) { huge[i] = new Msg(); }
        final TableReport measure = new TableReport();
        if (MsgMessage.measureMessages(huge, measure) != -1 || !"batch_too_large".equals(measure.reason)) {
            fail("measure bound");
        }
        final TableReport save = new TableReport();
        if (MsgMessage.saveMessages(huge, new byte[8], save) != -1 || !"batch_too_large".equals(save.reason)) {
            fail("save bound");
        }

        // M above the caller's capacity on the read side: refused before
        // decoding, the count set to the wire's M.
        final TableMessage.Vocabulary vocab = ownVocabulary();
        final Msg[] two = { new Msg(), new Msg() };
        final int len = MsgMessage.measureMessages(two, new TableReport());
        final byte[] buf = new byte[len];
        MsgMessage.saveMessages(two, buf, new TableReport());
        final Msg[] storage = { new Msg() };
        final TableReport read = new TableReport();
        final int count = MsgMessage.loadMessages(storage, vocab, buf, read);
        if (count != 2 || !"batch_too_large".equals(read.reason) || read.malformed) {
            fail("read bound " + count + " " + read.reason);
        }
        System.out.println("refusals ok");
    }

    static TableMessage.Vocabulary ownVocabulary() {
        final byte[] ann = new byte[TableMessage.announceMeasure()];
        TableMessage.announce(ann);
        final TableMessage.Vocabulary vocab = new TableMessage.Vocabulary();
        final TableReport report = new TableReport();
        if (!TableMessage.announceRead(vocab, ann, report)) { fail("announce"); }
        return vocab;
    }
}
`

// 2.3: damage inside body k delivers k-1 with one `malformed`; bytes after the
// pad are `malformed`; newer_form, and message_form_as_file from the file
// reader (docs/SPEC-TABLES.md §3.3).
const msgDamageDriver = `package tblm1;

public final class MessageDriver {
    private MessageDriver() {}

    static void fail(String what) {
        System.out.println("FAIL " + what);
        System.exit(1);
    }

    public static void main(String[] args) {
        final TableMessage.Vocabulary vocab = ownVocabulary();

        // Damage inside body 2: M = 3, body 2's first reference is 0xF, above
        // the announced entry count; a reference is four bits, low bit first.
        final byte[] damaged = { 2, 2, (byte) 0xF0, 0x00 };
        final Msg[] three = { new Msg(), new Msg(), new Msg() };
        final TableReport damage = new TableReport();
        final int k = MsgMessage.loadMessages(three, vocab, damaged, damage);
        if (k != 1 || !damage.malformed || damage.reason != null) {
            fail("damage " + k + " " + damage.reason);
        }

        // Bytes after the pad are malformed.
        final Msg[] empty = { new Msg() };
        final int size = MsgMessage.measureMessages(empty, new TableReport());
        final byte[] buf = new byte[size + 1];
        MsgMessage.saveMessages(empty, buf, new TableReport());
        final Msg[] storage = { new Msg() };
        final TableReport padded = new TableReport();
        final int n = MsgMessage.loadMessages(storage, vocab, buf, padded);
        if (n != 1 || !padded.malformed) { fail("bytes after pad " + n); }

        // A form a reader does not know is a refusal by name.
        final TableReport newer = new TableReport();
        final int form = MsgMessage.loadMessages(storage, vocab, new byte[] { 1 }, newer);
        if (form != 0 || !"newer_form".equals(newer.reason)) { fail("newer_form"); }

        // A form-2 batch handed to the file reader refuses by name.
        final TableMessage.Vocabulary file = new TableMessage.Vocabulary();
        final TableReport asFile = new TableReport();
        if (TableMessage.announceRead(file, new byte[] { 2, 0, 0, 0 }, asFile)
                || !"message_form_as_file".equals(asFile.reason)) {
            fail("message_form_as_file");
        }
        System.out.println("damage ok");
    }

    static TableMessage.Vocabulary ownVocabulary() {
        final byte[] ann = new byte[TableMessage.announceMeasure()];
        TableMessage.announce(ann);
        final TableMessage.Vocabulary vocab = new TableMessage.Vocabulary();
        final TableReport report = new TableReport();
        if (!TableMessage.announceRead(vocab, ann, report)) { fail("announce"); }
        return vocab;
    }
}
`
