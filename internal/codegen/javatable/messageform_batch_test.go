package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
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
		// "an all-default nested table is not written" (docs/SPEC-TABLES.md
		// §3.3): a nested table at its declared defaults costs nothing, so the
		// batch of an all-default Outer is its terminator alone and
		// SaveMessages gives the three bytes back exactly.
		nested := generate(t, `package probe

fixed table Outer
{
    a     uint32
    inner Inner
}

type Inner
{
    x uint32
    y uint32
}
`)
		runMessageDriver(t, nested, nestedElisionDriver)
		// "SaveMessages of them gives the batch bytes back exactly" (§3.3):
		// the manifest's `message` rows are the C++ reference's bytes, so a
		// fixed root's batch reproduces its pinned wire byte for byte.
		runManifestMessageRows(t)
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
	runMessageDriverPkg(t, "probe", files, driver)
}

func runMessageDriverPkg(t *testing.T, pkg string, files map[string][]byte, driver string) {
	t.Helper()
	javac, javaBin := javaTools(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(filepath.Join(src, pkg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(src, pkg, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(src, pkg, "Driver.java"), []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	pkgSrc, err := filepath.Glob(filepath.Join(src, pkg, "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, pkgSrc...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, pkg+".Driver").CombinedOutput()
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

const nestedElisionDriver = batchDriverPrologue + `
    public static void main(String[] args) {
        final OuterFixed.Value v = new OuterFixed.Value();
        final OuterFixed.Value[] values = { v };
        final TableMessage.Report report = new TableMessage.Report();
        // an all-default nested table is not written (§3.3): the batch is its
        // terminator alone, three bytes, no reference to ` + "`inner`" + `.
        final long m = OuterMessage.measureMessages(values, report);
        if (m != 3 || report.refused) { fail("measure " + m); }
        final byte[] buf = new byte[3];
        if (OuterMessage.saveMessages(values, buf, report) != 3) { fail("save"); }
        if (buf[0] != 2 || buf[1] != 0 || buf[2] != 0) { fail("all-default bytes"); }
        // a nested table at a non-default value rides and reads back (§3.3).
        v.inner.x = 5;
        final long m2 = OuterMessage.measureMessages(values, report);
        if (m2 <= 3 || report.refused) { fail("nested measure " + m2); }
        final byte[] buf2 = new byte[(int) m2];
        if (OuterMessage.saveMessages(values, buf2, report) != m2) { fail("nested save"); }
        final TableVocabulary vocab = mkVocab();
        final OuterFixed.Value[] got = { new OuterFixed.Value() };
        final TableMessage.Report r2 = new TableMessage.Report();
        if (OuterMessage.loadMessages(got, buf2, vocab, r2) != 1 || r2.refused || r2.malformed) { fail("nested load"); }
        if (got[0].inner.x != 5 || got[0].inner.y != 0) { fail("nested value"); }
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

// manifestMessageRow is one manifest `message` line: the connection's unit
// directory and the message's pinned batch wire (docs/SPEC-TABLES.md §3.3).
type manifestMessageRow struct {
	name, root, unitDir, batchPath string
}

// manifestMessageRows reads the `message` rows of the cross-language manifest
// and resolves each to its unit directory and batch wire. The manifest is read
// here rather than copied: a new row is a new assertion with no edit.
func manifestMessageRows(t *testing.T) []manifestMessageRow {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	units := map[string]string{}
	conns := map[string]string{}
	var rows []manifestMessageRow
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		switch fields[0] {
		case "unit":
			if len(fields) >= 3 {
				units[fields[1]] = fields[2]
			}
		case "connection":
			if len(fields) >= 3 {
				conns[fields[1]] = fields[2]
			}
		case "message":
			if len(fields) >= 6 {
				rows = append(rows, manifestMessageRow{
					name:      fields[1],
					root:      fields[3],
					unitDir:   units[conns[fields[2]]],
					batchPath: fields[5],
				})
			}
		}
	}
	if len(rows) == 0 {
		t.Fatalf("%s carries no message rows", "MANIFEST.txt")
	}
	return rows
}

// runManifestMessageRows pins every manifest `message` row whose root is a
// fixed table: MeasureMessages equals the batch's length and SaveMessages gives
// the pinned bytes back exactly (§3.3). A root that is a plain `table` rather
// than a `fixed table` is not a message-form root at all ("the form is a fixed
// table's", §3.3), so its row asserts nothing, exactly as the Go leg's answer
// does (gotable/messageform_goldens_test.go's VariableTables skip). The clause's
// "the values the form-1 instance decodes to" half is answered NOT DONE rather
// than here: the Java leg emits no form-1 reader (messageManifestDriver's note).
func runManifestMessageRows(t *testing.T) {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range manifestMessageRows(t) {
		u := loadUnitDir(t, root, row.unitDir)
		st, ok := u.Tables[row.root]
		if !ok || !st.FixedDeclared {
			continue
		}
		batch, err := os.ReadFile(filepath.Join(root, row.batchPath))
		if err != nil {
			t.Fatal(err)
		}
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		runMessageDriverPkg(t, u.Package, messageBatchClosure(u, files, row.root),
			messageManifestDriver(u, row.root, hexOf(batch)))
	}
}

// messageBatchClosure keeps only the files one root's batch needs: the shared
// runtimes and the root's fixed-form and message-form classes. The sibling
// roots' classes are dropped, because a sibling whose reader the carried work
// does not compile must not stop this root's pin.
func messageBatchClosure(u *ir.Unit, files map[string][]byte, root string) map[string][]byte {
	st := u.Tables[root]
	var order []*ir.Struct
	fixedCollectTypes(st, map[string]bool{}, &order)
	out := map[string][]byte{
		"TableMessage.java":    files["TableMessage.java"],
		"TableVocabulary.java": files["TableVocabulary.java"],
		"TableFixed.java":      files["TableFixed.java"],
	}
	for _, t := range order {
		out[messageClass(t.Name)+".java"] = files[messageClass(t.Name)+".java"]
		out[fixedClass(t.Name)+".java"] = files[fixedClass(t.Name)+".java"]
	}
	return out
}

// messageManifestDriver compiles one manifest row's root and asserts the two
// halves of §3.3 this leg can pin: MeasureMessages equals the batch's length
// and SaveMessages of the batch's value gives the pinned bytes back exactly.
// The third half — "LoadMessages of the batch gives the values the form-1
// instance decodes to" — is answered NOT DONE rather than here: the Java leg
// emits no form-1 reader (form 1 is not emitted for Java, javatable.go), so
// the manifest's form-1 file is unreadable and the batch's value cannot be
// compared against the instance it decodes.
func messageManifestDriver(u *ir.Unit, root, batchHex string) string {
	const tmpl = `package @PKG@;

public final class Driver {
    private Driver() {}
    static void fail(String w) { System.out.println("FAILED: " + w); System.exit(1); }
    static String hex(byte[] b) { final StringBuilder o = new StringBuilder(); for (final byte v : b) o.append(String.format("%02x", v)); return o.toString(); }
    static byte[] fromHex(String s) { final byte[] o = new byte[s.length()/2]; for (int i = 0; i < o.length; i++) o[i] = (byte) Integer.parseInt(s.substring(2*i, 2*i+2), 16); return o; }
    static TableVocabulary mkVocab() {
        final TableVocabulary v = new TableVocabulary();
        v.reset(4096);
        final TableVocabulary.Report r = new TableVocabulary.Report();
        final byte[] ann = new byte[TableVocabulary.announceMeasure()];
        if (TableVocabulary.announce(ann) != ann.length) fail("announce");
        if (!TableVocabulary.announceRead(v, ann, r)) fail("announceRead");
        return v;
    }
    public static void main(String[] args) {
        final byte[] batch = fromHex("@HEX@");
        final TableVocabulary vocab = mkVocab();
        final @VAL@.Value[] got = { new @VAL@.Value() };
        final TableMessage.Report r = new TableMessage.Report();
        final long c = @MSG@.loadMessages(got, batch, vocab, r);
        if (c != 1 || r.refused || r.malformed) fail("load " + c + " " + r.reason);

        // "MeasureMessages sizes the buffer" (§3.3), so it equals the batch's
        // length, and "SaveMessages of them gives the batch bytes back exactly"
        // (§3.3).
        final long m = @MSG@.measureMessages(got, r);
        if (m != batch.length) fail("measure " + m + " want " + batch.length);
        final byte[] out = new byte[(int) m];
        if (@MSG@.saveMessages(got, out, r) != m) fail("save");
        if (!hex(out).equals("@HEX@")) fail("bytes " + hex(out) + " want " + "@HEX@");
        System.out.println("ok");
    }
}
`
	return strings.NewReplacer(
		"@PKG@", u.Package,
		"@HEX@", batchHex,
		"@VAL@", fixedClass(root),
		"@MSG@", messageClass(root),
	).Replace(tmpl)
}
