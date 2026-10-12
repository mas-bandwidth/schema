package javatable

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
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
		// "A field at its declared default is not written ... and presence
		// rather than content decides ?T" (docs/SPEC-TABLES.md §3.3): a value
		// at every declared default is the terminator alone and reads back as
		// those defaults, and a zero that is not the default rides.
		defaults := generate(t, `package probe

enum Region { NA, EU }

fixed table Defaults
{
    count  uint8 = 7
    flag   bool = true
    ratio  float32 = 1.5
    tag    string(8) = "hi"
    region Region = EU
    slots  [3]uint8
    list   [..4]uint16
    extra  ?uint32
}
`)
		runMessageDriver(t, defaults, declaredDefaultsDriver)
		// "THE TWO FORMS ARE TWO ENCODINGS OF ONE VALUE, and the pin is a
		// ROUND TRIP" (§3.3): every manifest `message` row, one subtest each,
		// with no row skipped.
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

const declaredDefaultsDriver = batchDriverPrologue + `
    static DefaultsFixed.Value load(byte[] buf, TableVocabulary vocab) {
        final DefaultsFixed.Value[] got = { new DefaultsFixed.Value() };
        // storage holding other values first, so the read's reset is observed
        got[0].count = 9; got[0].flag = false; got[0].ratio = 0f; got[0].tagLength = 0;
        got[0].region = 0; got[0].slots[1] = 5; got[0].listCount = 2; got[0].extraPresent = true;
        final TableMessage.Report r = new TableMessage.Report();
        if (DefaultsMessage.loadMessages(got, buf, vocab, r) != 1 || r.refused || r.malformed) { fail("load " + r.reason); }
        return got[0];
    }

    static byte[] save(DefaultsFixed.Value v) {
        final DefaultsFixed.Value[] values = { v };
        final TableMessage.Report report = new TableMessage.Report();
        final long m = DefaultsMessage.measureMessages(values, report);
        if (m <= 0 || report.refused) { fail("measure " + m); }
        final byte[] buf = new byte[(int) m];
        if (DefaultsMessage.saveMessages(values, buf, report) != m) { fail("save"); }
        return buf;
    }

    public static void main(String[] args) {
        final TableVocabulary vocab = mkVocab();
        // "A field at its declared default is not written" (§3.3): the batch
        // of a value at its declared defaults is its terminator alone.
        final byte[] all = save(new DefaultsFixed.Value());
        if (all.length != 3 || all[0] != 2 || all[1] != 0 || all[2] != 0) { fail("all-default batch is " + all.length + " bytes"); }
        // ... so a body that names no field reads back every declared default.
        final DefaultsFixed.Value d = load(all, vocab);
        if (d.count != 7) { fail("count default " + d.count); }
        if (!d.flag) { fail("flag default"); }
        if (d.ratio != 1.5f) { fail("ratio default " + d.ratio); }
        if (d.tagLength != 2 || d.tag[0] != 'h' || d.tag[1] != 'i') { fail("tag default"); }
        if (d.region != 2) { fail("region default " + d.region); }
        if (d.slots[1] != 0) { fail("slots default"); }
        if (d.listCount != 0) { fail("list default"); }
        if (d.extraPresent) { fail("extra present"); }

        // A zero that is not the declared default rides, and a present ?T
        // rides whatever it holds ("presence rather than content", §3.3).
        final DefaultsFixed.Value z = new DefaultsFixed.Value();
        z.count = 0; z.flag = false; z.ratio = 0f; z.tagLength = 0; z.region = 0;
        z.slots[2] = 4; z.listCount = 1; z.list[0] = 9; z.extraPresent = true; z.extra = 0;
        final DefaultsFixed.Value back = load(save(z), vocab);
        if (back.count != 0 || back.flag || back.ratio != 0f || back.tagLength != 0 || back.region != 0) { fail("zero scalars"); }
        if (back.slots[0] != 0 || back.slots[2] != 4) { fail("slots"); }
        if (back.listCount != 1 || back.list[0] != 9) { fail("list"); }
        if (!back.extraPresent || back.extra != 0) { fail("present optional at zero"); }
        System.out.println("ok");
    }
}
`

const messageRefusalDriver = batchDriverPrologue + `
    static LoginRequestFixed.Value[] values(int n) {
        final LoginRequestFixed.Value[] out = new LoginRequestFixed.Value[n];
        for (int i = 0; i < n; i++) out[i] = new LoginRequestFixed.Value();
        return out;
    }

    public static void main(String[] args) {
        // "M ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME. MeasureMessages
        // and SaveMessages both refuse and return -1 ... with its refusal
        // verdict set and the reason batch_too_large on it" (docs/SPEC-TABLES.md
        // §3.3). 256 is the wire's own bound, so 256 bodies are a batch.
        final TableMessage.Report atMax = new TableMessage.Report();
        if (LoginRequestMessage.measureMessages(values(256), atMax) <= 0 || atMax.refused) { fail("256 bodies refused"); }
        final LoginRequestFixed.Value[] many = values(257);
        final TableMessage.Report wm = new TableMessage.Report();
        if (LoginRequestMessage.measureMessages(many, wm) != -1 || !wm.refused || !"batch_too_large".equals(wm.reason)) {
            fail("measure over 256 bodies: reason=" + wm.reason);
        }
        final TableMessage.Report ws = new TableMessage.Report();
        if (LoginRequestMessage.saveMessages(many, new byte[65536], ws) != -1 || !ws.refused || !"batch_too_large".equals(ws.reason)) {
            fail("save over 256 bodies: reason=" + ws.reason);
        }

        final TableVocabulary vocab = mkVocab();

        // "M ABOVE THE CALLER'S CAPACITY ON THE READ SIDE IS A REFUSAL BY NAME
        // ... refuses before it decodes anything. Nothing is decoded, no counter
        // moves and malformed does not fire ... The reader sets the returned
        // count to the wire's M before it returns the refusal" (§3.3). A valid
        // batch of two bodies read into storage for one.
        final LoginRequestFixed.Value[] two = values(2);
        two[0].clientBuild = 5;
        two[1].clientBuild = 6;
        final byte[] twoWire = new byte[(int) LoginRequestMessage.measureMessages(two, new TableMessage.Report())];
        if (LoginRequestMessage.saveMessages(two, twoWire, new TableMessage.Report()) != twoWire.length) { fail("save two"); }
        final LoginRequestFixed.Value[] one = values(1);
        one[0].clientBuild = 77;
        final TableMessage.Report r2 = new TableMessage.Report();
        final long c = LoginRequestMessage.loadMessages(one, twoWire, vocab, r2);
        if (c != 2 || !r2.refused || !"batch_too_large".equals(r2.reason) || r2.malformed) {
            fail("read over capacity: count=" + c + " reason=" + r2.reason + " malformed=" + r2.malformed);
        }
        if (one[0].clientBuild != 77) { fail("read over capacity decoded a body"); }

        // "A BODY FROM A PEER THAT NEVER ANNOUNCED IS REFUSED BY NAME. Nothing
        // is decoded, no counter moves and malformed does not fire" (§3.3).
        final TableVocabulary nov = new TableVocabulary();
        nov.reset(64);
        final LoginRequestFixed.Value[] three = values(3);
        three[0].clientBuild = 77;
        final TableMessage.Report r3 = new TableMessage.Report();
        if (LoginRequestMessage.loadMessages(three, twoWire, nov, r3) != -1 || !r3.refused
                || !"no_vocabulary".equals(r3.reason) || r3.malformed) {
            fail("no_vocabulary: reason=" + r3.reason + " malformed=" + r3.malformed);
        }
        if (three[0].clientBuild != 77) { fail("no_vocabulary decoded a body"); }
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

        // "DAMAGE INSIDE BODY k DELIVERS BODIES 1 TO k - 1. The returned count
        // states k - 1, ONE malformed counts, and nothing at or after body k
        // is read" (docs/SPEC-TABLES.md §3.3). Three bodies: body 1 is its
        // terminator alone, body 2 opens with player_id's reference and its
        // 64 payload bits, body 3 is a terminator. The wire is cut eight bits
        // into body 2's payload, so the bits run out inside body 2 (k = 2).
        final LoginRequestFixed.Value mid = new LoginRequestFixed.Value();
        mid.playerId = 0x0123456789ABCDEFL;
        final LoginRequestFixed.Value[] three = { new LoginRequestFixed.Value(), mid, new LoginRequestFixed.Value() };
        final byte[] whole = new byte[(int) LoginRequestMessage.measureMessages(three, new TableMessage.Report())];
        if (LoginRequestMessage.saveMessages(three, whole, new TableMessage.Report()) != whole.length) { fail("save three"); }
        final int cutBits = 16 + TableMessage.refBits + TableMessage.refBits + 8;
        final byte[] cut2 = java.util.Arrays.copyOf(whole, (cutBits + 7) / 8);
        final LoginRequestFixed.Value[] into = { new LoginRequestFixed.Value(), new LoginRequestFixed.Value(), new LoginRequestFixed.Value() };
        into[2].clientBuild = 77;
        final TableMessage.Report r0 = new TableMessage.Report();
        final long n0 = LoginRequestMessage.loadMessages(into, cut2, vocab, r0);
        if (n0 != 1 || !r0.malformed || r0.refused) { fail("damage in body 2: count=" + n0 + " malformed=" + r0.malformed); }
        if (into[2].clientBuild != 77) { fail("a body after the damage was read"); }

        // The same rule at k = 1: a batch of one cut to its header alone
        // reads no body and counts one malformed (§3.3).
        final byte[] cut = new byte[] { 2, 0 };
        final TableMessage.Report r1 = new TableMessage.Report();
        final long n1 = LoginRequestMessage.loadMessages(got, cut, vocab, r1);
        if (n1 != 0 || !r1.malformed || r1.refused) { fail("damage n=" + n1 + " malformed=" + r1.malformed); }

        // "BYTES AFTER THE PAD ARE MALFORMED ... a buffer with bytes left
        // over describes no batch this reader can name" (§3.3).
        final byte[] trailing = java.util.Arrays.copyOf(buf, buf.length + 1);
        final TableMessage.Report r2 = new TableMessage.Report();
        LoginRequestMessage.loadMessages(got, trailing, vocab, r2);
        if (!r2.malformed || r2.refused) { fail("trailing byte not malformed"); }

        // "The FORM BYTE is a whole byte and is read FIRST ... a form a reader
        // does not know is a REFUSAL by name and never damage" (§3.3).
        final byte[] nf = new byte[] { 3, 0 };
        final TableMessage.Report r3 = new TableMessage.Report();
        if (LoginRequestMessage.loadMessages(got, nf, vocab, r3) != -1) { fail("newer_form not refused"); }
        if (!r3.refused || !"newer_form".equals(r3.reason) || r3.malformed) { fail("newer_form reason " + r3.reason); }

        // "a reader handed a form-2 wire where a file was expected refuses by
        // name" (§3.3): message_form_as_file, from the file reader.
        final byte[] mf = new byte[] { 2, 0 };
        final TableVocabulary fresh = new TableVocabulary();
        fresh.reset(64);
        final TableVocabulary.Report r4 = new TableVocabulary.Report();
        if (TableVocabulary.announceRead(fresh, mf, r4) || !r4.refused || !"message_form_as_file".equals(r4.reason) || r4.malformed) {
            fail("message_form_as_file reason " + r4.reason);
        }
        System.out.println("ok");
    }
}
`

// manifestMessageRow is one manifest `message` line: the connection's unit
// directory, the instance's pinned form-1 wire and the message's pinned batch
// wire (docs/SPEC-TABLES.md §3.3).
type manifestMessageRow struct {
	name, root, unitDir, filePath, batchPath string
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
					filePath:  fields[4],
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

// runManifestMessageRows asserts EVERY manifest `message` row, one subtest a
// row and none skipped (docs/SPEC-TABLES.md §3.3).
//
// A `fixed table` root: the row's FORM-1 wire (the manifest's fourth field) is
// decoded by the compiler's own file reader (internal/tablewire), and the Java
// LoadMessages of the row's batch must hold exactly that value, field by
// field; MeasureMessages equals the batch's length and SaveMessages gives the
// batch bytes back exactly. "THE TWO FORMS ARE TWO ENCODINGS OF ONE VALUE, and
// the pin is a ROUND TRIP ... loading either form and saving the other, which
// reproduces the other's pinned bytes" (§3.3).
//
// A plain `table` root: "`fixed table T` may ride here, a plain `table T` may
// not" (§3.3), so the row's assertion is that the leg emits NO batch surface
// for it — the Go, C#, JavaScript and Dart legs answer the same row ABSENT.
func runManifestMessageRows(t *testing.T) {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range manifestMessageRows(t) {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			u := loadUnitDir(t, root, row.unitDir)
			st, ok := u.Tables[row.root]
			if !ok {
				t.Fatalf("manifest message %q names root %q, which unit %s does not declare", row.name, row.root, row.unitDir)
			}
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			if !st.FixedDeclared {
				// "THE FORM IS A FIXED TABLE'S ... a plain `table T` may not"
				// (docs/SPEC-TABLES.md §3.3): no batch verb is emitted for it.
				if _, emitted := files[messageClass(row.root)+".java"]; emitted {
					t.Fatalf("%s is a plain table and the leg emits %s.java; §3.3: a plain `table T` may not ride the message form", row.root, messageClass(row.root))
				}
				return
			}
			batch, err := os.ReadFile(filepath.Join(root, row.batchPath))
			if err != nil {
				t.Fatal(err)
			}
			fileWire, err := os.ReadFile(filepath.Join(root, row.filePath))
			if err != nil {
				t.Fatal(err)
			}
			// The form-1 instance, decoded by the compiler's own file reader:
			// "a form-`1` wire is the three parts §3 describes and every rule
			// above holds over it unchanged" (§3.3).
			model := tabletext.NewModel(u)
			want := model.New(st)
			var report tabletext.Report
			if ok, err := tablewire.Decode(model, want, fileWire, &report); !ok || err != nil || !report.Silent() || report.Refused {
				t.Fatalf("the form-1 wire %s does not decode cleanly: ok=%v err=%v report=%+v", row.filePath, ok, err, report)
			}
			var checks strings.Builder
			if err := javaInstanceChecks(&checks, want, "got[0]"); err != nil {
				t.Fatalf("NOT DONE for row %s: %v", row.name, err)
			}
			runMessageDriverPkg(t, u.Package, messageBatchClosure(u, files, row.root),
				messageManifestDriver(u, row.root, hexOf(batch), checks.String()))
		})
	}
}

// javaInstanceChecks writes one Java statement a value slot, each failing the
// driver when the loaded batch value differs from the form-1 instance's value
// at that slot (docs/SPEC-TABLES.md §3.3: "What is claimed is the VALUE").
// A construct this comparison does not spell is an error, never a skipped
// field, so a row that holds one goes red rather than green.
func javaInstanceChecks(b *strings.Builder, inst *tabletext.Instance, expr string) error {
	for i := range inst.Fields {
		fv := &inst.Fields[i]
		f := fv.Def
		at := expr + "." + member(f)
		if f.Type.Optional {
			fmt.Fprintf(b, "        if (%sPresent != %t) fail(%q);\n", at, fv.Present, at+"Present")
			if !fv.Present {
				continue
			}
		}
		switch {
		case f.Type.Pointer || f.IsMap() || f.KeyEnum != "":
			return fmt.Errorf("field %s: a pointer, map or enum-keyed array is outside this comparison", f.Name)
		case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
			fmt.Fprintf(b, "        if (%sLength != %d) fail(%q);\n", at, fv.Count, at+"Length")
			for j := 0; j < fv.Count; j++ {
				fmt.Fprintf(b, "        if (%s[%d] != (byte) 0x%02x) fail(%q);\n", at, j, fv.Cell.Str[j], fmt.Sprintf("%s[%d]", at, j))
			}
		case f.Array != ir.ArrayNone:
			n := int(f.ArrayBound)
			if f.Array == ir.ArrayCounted {
				fmt.Fprintf(b, "        if (%sCount != %d) fail(%q);\n", at, fv.Count, at+"Count")
				n = fv.Count
			}
			el := messageElementField(f)
			for j := 0; j < n; j++ {
				if err := javaCellCheck(b, el, fv.Elems[j], fmt.Sprintf("%s[%d]", at, j)); err != nil {
					return err
				}
			}
		default:
			if err := javaCellCheck(b, f, fv.Cell, at); err != nil {
				return err
			}
		}
	}
	return nil
}

// javaCellCheck spells one non-array slot's comparison. An integer compares
// at its declared width, because the Java storage is the same-width SIGNED
// type (fixedform.go, fixedScalarType); an enum compares its value, 0 for
// None and the declared variants from 1, which is the ordinal the reader
// stores for a resolved variant name (§3.3: "an ENUM value | a REFERENCE
// naming the VARIANT's name").
func javaCellCheck(b *strings.Builder, f *ir.Field, c tabletext.Cell, at string) error {
	switch {
	case f.Type.Kind == ir.TBool:
		fmt.Fprintf(b, "        if (%s != %t) fail(%q);\n", at, c.B, at)
	case f.Type.Kind == ir.TFloat32:
		fmt.Fprintf(b, "        if (java.lang.Float.floatToRawIntBits(%s) != 0x%08x) fail(%q);\n", at, math.Float32bits(float32(c.F)), at)
	case f.Type.Kind == ir.TFloat64:
		fmt.Fprintf(b, "        if (java.lang.Double.doubleToRawLongBits(%s) != 0x%016xL) fail(%q);\n", at, math.Float64bits(c.F), at)
	case (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TBits) && f.Type.Width <= 64:
		mask := ^uint64(0)
		if f.Type.Width < 64 {
			mask = uint64(1)<<uint(f.Type.Width) - 1
		}
		fmt.Fprintf(b, "        if ((((long) %s) & 0x%016xL) != 0x%016xL) fail(%q);\n", at, mask, c.U&mask, at)
	case enumRef(f) != nil:
		fmt.Fprintf(b, "        if (((long) %s) != %dL) fail(%q);\n", at, c.U, at)
	case isStructRef(f.Type):
		if c.Tab == nil {
			return fmt.Errorf("field %s: the form-1 instance holds no nested value", f.Name)
		}
		return javaInstanceChecks(b, c.Tab, at)
	default:
		return fmt.Errorf("field %s: kind %v is outside this comparison", f.Name, f.Type.Kind)
	}
	return nil
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

// messageManifestDriver compiles one manifest row's root and asserts the three
// halves of STEP 2.1 (docs/SPEC-TABLES.md §3.3): LoadMessages of the batch
// holds the value the form-1 instance decodes to (`checks`, written by
// javaInstanceChecks), MeasureMessages equals the batch's length, and
// SaveMessages of that value gives the pinned batch bytes back exactly.
func messageManifestDriver(u *ir.Unit, root, batchHex, checks string) string {
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

        // "THE TWO FORMS ARE TWO ENCODINGS OF ONE VALUE" (§3.3): the batch's
        // value is the form-1 instance's value, slot by slot.
@CHECKS@
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
		"@CHECKS@", checks,
	).Replace(tmpl)
}
