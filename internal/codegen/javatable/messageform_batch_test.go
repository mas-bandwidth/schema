package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMessageFormBatchJava pins the MESSAGE FORM's BATCH SURFACE
// (docs/SPEC-TABLES.md §3.3) on the Java leg: the batch verbs MeasureMessages,
// SaveMessages and LoadMessages over one announced vocabulary, their refusal
// reasons, and their damage rules. The three subtests are STEP 2's three
// clauses, one subtest each. A message is a bitpacked body under a vocabulary
// announced once, and the form is a fixed table's, so the probe is a
// `fixed table` root.
func TestMessageFormBatchJava(t *testing.T) {
	t.Parallel()

	src := `package probe

fixed table LoginRequest
{
    player_id     uint64
    session_token bytes(16)
    client_build  uint32
    name          string(8)
}
`
	files := generate(t, src)

	t.Run("surface", func(t *testing.T) {
		// "SaveMessages takes an array of bodies of ONE root and writes ONE
		// buffer. LoadMessages reads one buffer into the caller's storage and
		// allocates nothing. MeasureMessages sizes the buffer."
		// (docs/SPEC-TABLES.md §3.3, "The primitive is a BATCH")
		for _, want := range []string{"measureMessages", "saveMessages", "loadMessages"} {
			if !strings.Contains(string(files["LoginRequestMessage.java"]), want) {
				t.Fatalf("%q absent from LoginRequestMessage.java", want)
			}
		}
		runMessageDriver(t, files, messageRoundTripDriver)
	})

	t.Run("refusals", func(t *testing.T) {
		// "M ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME ... the reason
		// batch_too_large on it" and "M ABOVE THE CALLER'S CAPACITY ON THE READ
		// SIDE IS A REFUSAL BY NAME ... the reason batch_too_large ... the
		// reader sets the returned count to the wire's M" and "A BODY FROM A
		// PEER THAT NEVER ANNOUNCED IS REFUSED BY NAME ... no_vocabulary"
		// (docs/SPEC-TABLES.md §3.3).
		runMessageDriver(t, files, messageRefusalDriver)
	})

	t.Run("damage", func(t *testing.T) {
		// "DAMAGE INSIDE BODY k DELIVERS BODIES 1 TO k - 1 ... ONE malformed
		// counts", "BYTES AFTER THE PAD ARE MALFORMED", the form byte's own
		// refusal newer_form, and message_form_as_file from the FILE reader
		// (docs/SPEC-TABLES.md §3.3).
		runMessageDriver(t, files, messageDamageDriver)
	})
}

func runMessageDriver(t *testing.T, files map[string][]byte, driver string) {
	t.Helper()
	javac, javaBin := javaTools(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(filepath.Join(src, "probe"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(src, "probe", name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(src, "probe", "Driver.java"), []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	pkgSrc, err := filepath.Glob(filepath.Join(src, "probe", "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, pkgSrc...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "probe.Driver").CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("driver: %s", out)
	}
}

// batchDriverPrologue builds the announced vocabulary the batch verbs read with and
// the helpers every driver shares.
const batchDriverPrologue = `package probe;

public final class Driver {
    private Driver() {}

    static void fail(String what) {
        System.out.println("FAILED: " + what);
        System.exit(1);
    }

    static void setText(byte[] buf, String s) {
        final byte[] raw = s.getBytes(java.nio.charset.StandardCharsets.UTF_8);
        System.arraycopy(raw, 0, buf, 0, raw.length);
    }

    static String textOf(byte[] buf, int length) {
        return new String(buf, 0, length, java.nio.charset.StandardCharsets.UTF_8);
    }

    static TableVocabulary mkVocab() {
        final TableVocabulary v = new TableVocabulary();
        v.reset(64);
        final TableVocabulary.Report r = new TableVocabulary.Report();
        final byte[] ann = new byte[TableVocabulary.announceMeasure()];
        if (TableVocabulary.announce(ann) != ann.length) { fail("announce"); }
        if (!TableVocabulary.announceRead(v, ann, r)) { fail("announceRead"); }
        return v;
    }
`

const messageRoundTripDriver = batchDriverPrologue + `
    public static void main(String[] args) {
        final LoginRequestFixed.Value v = new LoginRequestFixed.Value();
        v.playerId = 0xDEADBEEFCAFEBABEL;
        v.clientBuild = 1234;
        v.sessionTokenLength = 2;
        v.sessionToken[0] = (byte) 0xAA;
        v.sessionToken[1] = (byte) 0xBB;
        v.nameLength = 3;
        setText(v.name, "abc");

        final LoginRequestFixed.Value[] values = { v };
        final TableMessage.Report report = new TableMessage.Report();
        final long m = LoginRequestMessage.measureMessages(values, report);
        if (m <= 0 || report.refused) { fail("measure " + m); }
        final byte[] buf = new byte[(int) m];
        final long n = LoginRequestMessage.saveMessages(values, buf, report);
        if (n != m || report.refused) { fail("save " + n + " want " + m); }

        final TableVocabulary vocab = mkVocab();
        final LoginRequestFixed.Value[] got = { new LoginRequestFixed.Value() };
        final TableMessage.Report r2 = new TableMessage.Report();
        final long loaded = LoginRequestMessage.loadMessages(got, buf, vocab, r2);
        if (loaded != 1 || r2.refused || r2.malformed) { fail("load " + loaded + " " + r2.reason); }
        if (got[0].playerId != v.playerId) { fail("player"); }
        if (got[0].clientBuild != v.clientBuild) { fail("build"); }
        if (got[0].sessionTokenLength != 2 || got[0].sessionToken[0] != (byte) 0xAA
                || got[0].sessionToken[1] != (byte) 0xBB) { fail("token"); }
        if (got[0].nameLength != 3 || !textOf(got[0].name, 3).equals("abc")) { fail("name"); }
        System.out.println("ok");
    }
}
`

const messageRefusalDriver = batchDriverPrologue + `
    public static void main(String[] args) {
        // M above 256 on the write side (docs/SPEC-TABLES.md §3.3).
        final LoginRequestFixed.Value[] many = new LoginRequestFixed.Value[300];
        final TableMessage.Report r = new TableMessage.Report();
        if (LoginRequestMessage.measureMessages(many, r) != -1) { fail("write batch_too_large not refused"); }
        if (!r.refused || !"batch_too_large".equals(r.reason)) { fail("write reason " + r.reason); }

        final TableVocabulary vocab = mkVocab();

        // M above the caller's capacity on the read side, count set to the
        // wire's M before the refusal (docs/SPEC-TABLES.md §3.3).
        final byte[] big = new byte[] { 2, 5 };
        final LoginRequestFixed.Value[] three = { new LoginRequestFixed.Value(),
                new LoginRequestFixed.Value(), new LoginRequestFixed.Value() };
        final TableMessage.Report r2 = new TableMessage.Report();
        final long c = LoginRequestMessage.loadMessages(three, big, vocab, r2);
        if (c != 6 || !r2.refused || !"batch_too_large".equals(r2.reason)) {
            fail("read batch_too_large count=" + c + " reason=" + r2.reason);
        }

        // A body from a peer that never announced (docs/SPEC-TABLES.md §3.3).
        final TableVocabulary nov = new TableVocabulary();
        final TableMessage.Report r3 = new TableMessage.Report();
        if (LoginRequestMessage.loadMessages(three, big, nov, r3) != -1) { fail("no_vocabulary not refused"); }
        if (!r3.refused || !"no_vocabulary".equals(r3.reason)) { fail("no_vocabulary reason " + r3.reason); }
        System.out.println("ok");
    }
}
`

const messageDamageDriver = batchDriverPrologue + `
    public static void main(String[] args) {
        final LoginRequestFixed.Value v = new LoginRequestFixed.Value();
        v.playerId = 0x123456789ABCDEFL;
        final LoginRequestFixed.Value[] values = { v };
        final TableMessage.Report report = new TableMessage.Report();
        final byte[] buf = new byte[(int) LoginRequestMessage.measureMessages(values, report)];
        if (LoginRequestMessage.saveMessages(values, buf, report) != buf.length) { fail("save"); }

        final TableVocabulary vocab = mkVocab();
        final LoginRequestFixed.Value[] got = { new LoginRequestFixed.Value() };

        // DAMAGE INSIDE BODY k DELIVERS BODIES 1 TO k - 1: a batch of one cut
        // to its header alone reads no body, counts one malformed, and returns
        // k - 1 = 0 (docs/SPEC-TABLES.md §3.3).
        final byte[] cut = new byte[] { 2, 0 };
        final TableMessage.Report r1 = new TableMessage.Report();
        final long n1 = LoginRequestMessage.loadMessages(got, cut, vocab, r1);
        if (n1 != 0 || !r1.malformed) { fail("damage n=" + n1 + " malformed=" + r1.malformed); }

        // BYTES AFTER THE PAD ARE MALFORMED (docs/SPEC-TABLES.md §3.3).
        final byte[] trailing = java.util.Arrays.copyOf(buf, buf.length + 1);
        final TableMessage.Report r2 = new TableMessage.Report();
        LoginRequestMessage.loadMessages(got, trailing, vocab, r2);
        if (!r2.malformed) { fail("trailing byte not malformed"); }

        // A form byte this reader does not know is a refusal by name (§3.3).
        final byte[] nf = new byte[] { 3, 0 };
        final TableMessage.Report r3 = new TableMessage.Report();
        if (LoginRequestMessage.loadMessages(got, nf, vocab, r3) != -1) { fail("newer_form not refused"); }
        if (!r3.refused || !"newer_form".equals(r3.reason)) { fail("newer_form reason " + r3.reason); }

        // A message handed to the FILE reader is refused message_form_as_file
        // (docs/SPEC-TABLES.md §3.3).
        final byte[] mf = new byte[] { 2, 0 };
        final TableVocabulary fresh = new TableVocabulary();
        fresh.reset(64);
        final TableVocabulary.Report r4 = new TableVocabulary.Report();
        if (TableVocabulary.announceRead(fresh, mf, r4) || !r4.refused || !"message_form_as_file".equals(r4.reason)) {
            fail("message_form_as_file reason " + r4.reason);
        }
        System.out.println("ok");
    }
}
`
