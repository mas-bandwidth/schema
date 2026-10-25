package javatable

// THE KEYED ACCESSOR (docs/SPEC-TABLES.md §2.4, docs/PORTING.md M5), Java side.
//
// An enum-keyed array holds one slot per NAMED variant: key k lives at slot
// k-1, None (key 0) names no slot, and neither does a key past E.Max. The
// accessor takes the KEY, refuses both ends by name in every build (one
// unsigned compare, never `assert`, which a -da run drops), and is the only
// place the shift is written. The JVM's own ArrayIndexOutOfBounds is not the
// refusal: it names an index rather than the key, and a row read over a wider
// byte[] does not throw at all.
//
// Two tests hold it:
//
//   - TestKeyedArrayIndexingJava is FUNCTIONAL: it generates a fixed table with
//     a three-variant key, compiles it under the consumer's -Xlint:all -Werror
//     and runs a driver under -da and -ea. Every check names its END (None or
//     Max+1), so the negative control in make/java.mk can require a sabotaged
//     bound to red its own end and only its own.
//   - TestKeyedFormWireCodeMatchesUnkeyedInTree holds that the accessor is
//     ADDITIVE, by behaviour and in the same tree: every keyed unit is
//     generated twice, keyed and with its keys lowered to `[E.Max]T`, both
//     are compiled, and one corpus must give byte-identical fixed bodies that
//     cross-decode, and identical saved-and-loaded message values; and each
//     form's message, and the keyed form's variable wire, must be the Go
//     codec's encoding of the same value byte for byte. There is no committed
//     pin, so a change that MEANS to move the wire moves both forms.

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const keyedProbeSchema = `package probe

enum Team { Red, Blue, Green }

fixed table TeamConfig
{
    spawn_count int32 = 4
}

fixed table Scores
{
    per_team [Team]int32
    teams    [Team]TeamConfig
    tag      int32
}
`

// keyedDriver prints one CHECK line per probe. A name's prefix is the END it
// holds: none/ is the lower bound (None, and a negative key below it), max/ is
// the upper (Max+1), and reach/ and place/ are the in-range half.
const keyedDriver = `package probe;

public final class Driver {
    private Driver() {}

    static int failed;

    static void check(String name, String why) {
        if (why == null) {
            System.out.println("CHECK " + name + " ok");
        } else {
            System.out.println("CHECK " + name + " FAIL: " + why);
            failed++;
        }
    }

    static String want(boolean ok, String why) { return ok ? null : why; }

    interface Access { Object run(); }

    // null when the access threw IllegalArgumentException naming the key, the
    // enum and its Max; otherwise what it did instead.
    static String refusal(Access a, int key) {
        try {
            Object got = a.run();
            return "key " + key + " was not refused: the access answered " + got;
        } catch (IllegalArgumentException e) {
            String m = String.valueOf(e.getMessage());
            for (String w : new String[] { "key " + key, "[Team]", "Team.Max", "3" }) {
                if (!m.contains(w)) {
                    return "the refusal of key " + key + " does not name " + w + ": " + m;
                }
            }
            return null;
        } catch (RuntimeException e) {
            return "key " + key + " was refused by " + e.getClass().getName() + " (" + e.getMessage() + "), which names an index and not the key";
        }
    }

    public static void main(String[] args) {
        final ScoresFixed.Value v = new ScoresFixed.Value();

        // the FIXED RECORD's accessors: get, set, and the element instance
        for (int key : new int[] { 0, -1 }) {
            check("none/fixed-get/" + key, refusal(() -> v.perTeam(key), key));
            check("none/fixed-set/" + key, refusal(() -> { v.perTeam(key, 7); return "stored"; }, key));
            check("none/fixed-element/" + key, refusal(() -> v.teams(key), key));
        }
        check("max/fixed-get", refusal(() -> v.perTeam(4), 4));
        check("max/fixed-set", refusal(() -> { v.perTeam(4, 7); return "stored"; }, 4));
        check("max/fixed-element", refusal(() -> v.teams(4), 4));

        v.perTeam(3, 33);
        check("reach/fixed-max", want(v.perTeam(3) == 33 && v.perTeam[2] == 33,
            "key Max wrote slot " + java.util.Arrays.toString(v.perTeam)));

        for (int k = 1; k <= 3; k++) {
            v.perTeam(k, 100 + k);
            v.teams(k).spawnCount = 10 + k;
        }
        for (int k = 1; k <= 3; k++) {
            check("place/fixed-slot/" + k, want(v.perTeam[k - 1] == 100 + k && v.perTeam(k) == 100 + k,
                "key " + k + " is not at slot " + (k - 1) + ": " + java.util.Arrays.toString(v.perTeam)));
            check("place/fixed-element/" + k, want(v.teams(k) == v.teams[k - 1],
                "key " + k + "'s element is not slot " + (k - 1) + "'s instance"));
        }

        // and the SAVED WIRE: slot k-1's bytes carry key k's value
        final byte[] buf = new byte[ScoresFixed.measure(1)];
        ScoresFixed.save(new ScoresFixed.Value[] { v }, 1, buf);
        final int body = ScoresFixed.headerBytes + 8;
        for (int k = 1; k <= 3; k++) {
            int scalar = TableFixed.get32(buf, body + (k - 1) * 4);
            check("place/wire-scalar/" + k, want(scalar == 100 + k,
                "the saved slot " + (k - 1) + " holds " + scalar + ", want key " + k + "'s " + (100 + k)));
            int nested = TableFixed.get32(buf, body + 3 * 4 + (k - 1) * TeamConfigFixed.bodyBytes);
            check("place/wire-element/" + k, want(nested == 10 + k,
                "the saved element slot " + (k - 1) + " holds " + nested + ", want key " + k + "'s " + (10 + k)));
        }

        // the ROW accessors (block and cook share them): the key form beside
        // the slot form, over a byte[] wider than the row so a missing bound
        // reads silently rather than throwing
        final int at = 8;
        final byte[] row = new byte[at + ScoresRow.size + 8];
        for (int k = 1; k <= 3; k++) {
            int off = at + ScoresRow.offsets[0] + (k - 1) * 4;
            java.nio.ByteBuffer.wrap(row).order(java.nio.ByteOrder.LITTLE_ENDIAN).putInt(off, 200 + k);
        }
        for (int key : new int[] { 0, -1 }) {
            check("none/row-get/" + key, refusal(() -> ScoresRow.perTeamByKey(row, at, key), key));
            check("none/row-element/" + key, refusal(() -> ScoresRow.teamsAtKey(at, key), key));
        }
        check("max/row-get", refusal(() -> ScoresRow.perTeamByKey(row, at, 4), 4));
        check("max/row-element", refusal(() -> ScoresRow.teamsAtKey(at, 4), 4));
        check("reach/row-max", want(ScoresRow.perTeamByKey(row, at, 3) == 203,
            "key Max read " + ScoresRow.perTeamByKey(row, at, 3)));
        for (int k = 1; k <= 3; k++) {
            check("place/row-slot/" + k, want(ScoresRow.perTeamByKey(row, at, k) == ScoresRow.perTeam(row, at, k - 1),
                "key " + k + " does not read slot " + (k - 1)));
            check("place/row-element/" + k, want(ScoresRow.teamsAtKey(at, k) == ScoresRow.teamsAt(at, k - 1),
                "key " + k + "'s element offset is not slot " + (k - 1) + "'s"));
        }

        System.out.println(failed == 0 ? "keyed: ok" : "keyed: " + failed + " failed");
        System.exit(failed == 0 ? 0 : 1);
    }
}
`

// TestKeyedArrayIndexingJava holds §2.4's accessor at both ends, in the
// release shape (-da) and the checked one (-ea).
func TestKeyedArrayIndexingJava(t *testing.T) {
	t.Parallel()
	var javac, javaBin string
	t.Run("jdk", func(t *testing.T) { javac, javaBin = javaTools(t) })
	if javac == "" {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatal("SCHEMA_REQUIRE_CORPUS is set and TestKeyedArrayIndexingJava found no JDK: set JAVAC and JAVA (make tables-java-keyed passes them)")
		}
		t.Skip("TestKeyedArrayIndexingJava: no JDK (set JAVAC/JAVA)")
	}
	files, err := Generate(unitFrom(t, keyedProbeSchema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "probe")
	classes := filepath.Join(dir, "classes")
	for _, d := range []string{src, classes} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{"--release", "17", "-Xlint:all", "-Werror", "-d", classes}
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		p := filepath.Join(src, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		args = append(args, p)
	}
	driver := filepath.Join(src, "Driver.java")
	if err := os.WriteFile(driver, []byte(keyedDriver), 0o644); err != nil {
		t.Fatal(err)
	}
	args = append(args, driver)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: the generated keyed accessor does not compile beside the driver: %v\n%s", err, out)
	}
	for _, mode := range []string{"-da", "-ea"} {
		out, err := exec.Command(javaBin, mode, "-cp", classes, "probe.Driver").CombinedOutput()
		checks := 0
		sc := bufio.NewScanner(strings.NewReader(string(out)))
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "CHECK ") {
				continue
			}
			checks++
			name, rest, _ := strings.Cut(strings.TrimPrefix(line, "CHECK "), " ")
			if rest == "ok" {
				continue
			}
			end := "in-range"
			switch {
			case strings.HasPrefix(name, "none/"):
				end = "None end"
			case strings.HasPrefix(name, "max/"):
				end = "Max+1 end"
			}
			t.Errorf("java %s: keyed %s: %s: %s", mode, end, name, strings.TrimPrefix(rest, "FAIL: "))
		}
		if checks == 0 || (err != nil && !t.Failed()) {
			t.Fatalf("java %s: the driver ran %d checks and exited %v\n%s", mode, checks, err, out)
		}
	}
}

// keyedUnits is every directory under tables/ that holds a .schema unit.
func keyedUnits(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..", "..", "tables")
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".schema") {
			seen[filepath.Dir(p)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for d := range seen {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	return dirs
}

// lowerKeyedFields spells every enum-keyed field as the positional `[E.Max]T`
// array its storage already is (§2.4), on a SECOND parse of the same unit: it
// is how the test generates the UNKEYED form in the same tree. It reports
// whether the unit had any keyed field at all.
func lowerKeyedFields(u *ir.Unit) bool {
	seen := map[*ir.Struct]bool{}
	lowered := false
	var walk func(*ir.Struct)
	walk = func(st *ir.Struct) {
		if st == nil || seen[st] {
			return
		}
		seen[st] = true
		for _, f := range st.Fields {
			if f.KeyEnum != "" && f.KeyEnumRef != nil {
				f.Array = ir.ArrayFixed
				f.ArrayBound = f.KeyEnumRef.Max
				f.KeyEnum = ""
				f.KeyEnumRef = nil
				lowered = true
			}
			if f.MapEntry != nil {
				walk(f.MapEntry)
			}
			if s, ok := f.Type.Ref.(*ir.Struct); ok {
				walk(s)
			}
		}
	}
	for _, st := range u.Tables {
		walk(st)
	}
	for _, st := range u.Structs {
		walk(st)
	}
	return lowered
}

// keyedWireCorpus is how many values each root encodes: value 0 is the
// default, and every other is the default walked by seeded random leaf
// mutations, each kept only while the form still saves it.
const keyedWireCorpus = 24

// keyedWireDriver is one generic driver, compiled into BOTH forms' class
// trees. It reaches every record by reflection, so the same source, the same
// seeds and the same storage shape (the key form is additive: the storage of
// `[E]T` is the storage of `[E.Max]T`) give both forms the same corpus.
//
// gen ROOT... prints, per root and value:
//
//	VAL   the value, dumped field by field (the corpus itself)
//	BODY  the fixed record body writeBody stores, in hex (the fixed wire)
//	RBODY that body scattered back by this form's reader, dumped
//	MSG   the message body saved and loaded back by this form, dumped
//
// cross FILE reads the OTHER form's BODY lines and prints XBODY: those bytes
// scattered by THIS form's reader, dumped.
const keyedWireDriver = `package %s;

import java.lang.reflect.Array;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Comparator;
import java.util.List;
import java.util.Random;

public final class KeyedWireDriver {
    private KeyedWireDriver() {}

    static final String PKG = "%s";

    static final class Root {
        final String name;
        final Class<?> value;
        final Method writeBody, scatter, save, load, saveMessages;
        final int bodyBytes;

        Root(String name) throws Exception {
            this.name = name;
            Class<?> fixed = Class.forName(PKG + "." + name + "Fixed");
            this.value = Class.forName(PKG + "." + name + "Fixed$Value");
            this.writeBody = fixed.getMethod("writeBody", byte[].class, int.class, value);
            this.scatter = fixed.getMethod("scatter", byte[].class, int.class, value, TableFixed.Report.class);
            this.bodyBytes = fixed.getField("bodyBytes").getInt(null);
            Method s = null, l = null, sm = null;
            try {
                Class<?> msg = Class.forName(PKG + "." + name + "Message");
                s = msg.getMethod("saveMessageBody", TableMessage.Writer.class, value);
                l = msg.getMethod("loadMessageBody", TableMessage.Reader.class, value);
                sm = msg.getMethod("saveMessages", Array.newInstance(value, 0).getClass(), int.class, byte[].class, TableVocabulary.Report.class);
            } catch (ClassNotFoundException | NoSuchMethodException e) {
                s = null;
                l = null;
                sm = null;
            }
            this.save = s;
            this.load = l;
            this.saveMessages = sm;
        }

        Object fresh() throws Exception { return value.getConstructor().newInstance(); }
    }

    static List<Field> fields(Class<?> c) {
        List<Field> out = new ArrayList<>();
        for (Field f : c.getFields()) {
            int m = f.getModifiers();
            if (Modifier.isStatic(m)) { continue; }
            if (Modifier.isFinal(m) && f.getType().isPrimitive()) { continue; }
            out.add(f);
        }
        out.sort(Comparator.comparing(Field::getName));
        return out;
    }

    // A LEAF is one primitive the corpus can move: a field, or an element.
    static final class Leaf {
        final Object owner; final Field field; final int index; final Class<?> type;
        Leaf(Object owner, Field field, int index, Class<?> type) {
            this.owner = owner; this.field = field; this.index = index; this.type = type;
        }
        Object get() throws Exception { return field != null ? field.get(owner) : Array.get(owner, index); }
        void set(Object v) throws Exception { if (field != null) { field.set(owner, v); } else { Array.set(owner, index, v); } }
    }

    static void leaves(Object o, List<Leaf> out, int depth) throws Exception {
        if (o == null || depth > 12) { return; }
        for (Field f : fields(o.getClass())) {
            Class<?> t = f.getType();
            if (t.isPrimitive()) { out.add(new Leaf(o, f, -1, t)); continue; }
            Object v = f.get(o);
            if (v == null) { continue; }
            if (t.isArray()) {
                Class<?> e = t.getComponentType();
                int n = Array.getLength(v);
                for (int i = 0; i < n; i++) {
                    if (e.isPrimitive()) { out.add(new Leaf(v, null, i, e)); } else { leaves(Array.get(v, i), out, depth + 1); }
                }
                continue;
            }
            if (t.getName().startsWith("java.")) { continue; }
            leaves(v, out, depth + 1);
        }
    }

    static Object pick(Random r, Class<?> t) {
        long n;
        switch (r.nextInt(6)) {
            case 0: n = 0; break;
            case 1: n = 1; break;
            case 2: n = 2; break;
            case 3: n = 3; break;
            case 4: n = r.nextInt(100); break;
            default: n = r.nextLong(); break;
        }
        if (t == boolean.class) { return (n & 1) != 0; }
        if (t == byte.class) { return (byte) n; }
        if (t == short.class) { return (short) n; }
        if (t == char.class) { return (char) n; }
        if (t == int.class) { return (int) n; }
        if (t == long.class) { return n; }
        double d;
        switch (r.nextInt(4)) {
            case 0: d = 0.5; break;
            case 1: d = -2.25; break;
            case 2: d = r.nextInt(200) - 100; break;
            default: d = r.nextInt(1 << 20) / 64.0; break;
        }
        if (t == float.class) { return (float) d; }
        return d;
    }

    static void dump(Object o, StringBuilder b, int depth) throws Exception {
        if (o == null) { b.append("null"); return; }
        Class<?> c = o.getClass();
        if (c == Float.class) { b.append(Integer.toHexString(Float.floatToRawIntBits((Float) o))); return; }
        if (c == Double.class) { b.append(Long.toHexString(Double.doubleToRawLongBits((Double) o))); return; }
        if (c.isPrimitive() || o instanceof Number || o instanceof Boolean || o instanceof Character) { b.append(o); return; }
        if (o instanceof String) { b.append('"').append(o).append('"'); return; }
        if (c.isArray()) {
            b.append('[');
            for (int i = 0; i < Array.getLength(o); i++) {
                if (i > 0) { b.append(','); }
                dump(Array.get(o, i), b, depth + 1);
            }
            b.append(']');
            return;
        }
        if (depth > 12 || c.getName().startsWith("java.")) { b.append(String.valueOf(o)); return; }
        b.append('{');
        boolean first = true;
        for (Field f : fields(c)) {
            if (!first) { b.append(' '); }
            first = false;
            b.append(f.getName()).append('=');
            dump(f.get(o), b, depth + 1);
        }
        b.append('}');
    }

    static String dump(Object o) throws Exception {
        StringBuilder b = new StringBuilder();
        dump(o, b, 0);
        return b.toString();
    }

    static String hex(byte[] a) {
        StringBuilder b = new StringBuilder();
        for (byte x : a) { b.append(Character.forDigit((x >> 4) & 15, 16)).append(Character.forDigit(x & 15, 16)); }
        return b.toString();
    }

    static byte[] unhex(String s) {
        byte[] a = new byte[s.length() / 2];
        for (int i = 0; i < a.length; i++) { a[i] = (byte) Integer.parseInt(s.substring(2 * i, 2 * i + 2), 16); }
        return a;
    }

    static String why(Throwable e) {
        Throwable c = e;
        while (c.getCause() != null) { c = c.getCause(); }
        return "EXC " + c.getClass().getSimpleName();
    }

    static byte[] body(Root r, Object v) throws Exception {
        byte[] b = new byte[r.bodyBytes];
        r.writeBody.invoke(null, b, 0, v);
        return b;
    }

    // the message body as this form saves it, or null when the save refuses
    static byte[] saveMessage(Root r, Object v) throws Exception {
        TableMessage.Writer m = new TableMessage.Writer(null, 0, true);
        if (!(Boolean) r.save.invoke(null, m, v)) { return null; }
        m.align();
        if (m.overflow) { return null; }
        // SLACK PAST THE MEASURE: a nested body the writer stores and then
        // truncates away may pass the measured end on its way (seen on
        // PackConfig's default), which is a buffer-size question and not the
        // keyed wire's, so the body is written roomy and cut to the measure.
        int n = m.bytesWritten();
        byte[] buf = new byte[n + 4096];
        TableMessage.Writer w = new TableMessage.Writer(buf, 0, false);
        if (!(Boolean) r.save.invoke(null, w, v)) { return null; }
        w.align();
        if (w.overflow || w.bytesWritten() != n) { return null; }
        return Arrays.copyOf(buf, n);
    }

    // the WHOLE message, a batch of one, as this form's saveMessages writes
    // it: the bytes the Go codec's encoding of the same value is held to
    static byte[] wholeMessage(Root r, Object v) throws Exception {
        byte[] body = saveMessage(r, v);
        if (body == null) { return null; }
        // the same slack past the measure as saveMessage, for the same reason
        byte[] buf = new byte[body.length + 2 + 4096];
        Object values = Array.newInstance(r.value, 1);
        Array.set(values, 0, v);
        int n = (Integer) r.saveMessages.invoke(null, values, 1, buf, new TableVocabulary.Report());
        return n < 0 ? null : Arrays.copyOf(buf, n);
    }

    static TableVocabulary vocab;

    // A value is in the corpus only while this form saves it AND loads it
    // back cleanly to the same value: a random leaf can leave the declared
    // domain (a used length past its extent, a value past its range), and a
    // value the form itself clamps or calls malformed carries no wire to hold.
    static boolean accepts(Root r, Object v) {
        try {
            body(r, v);
            if (r.save == null) { return true; }
            byte[] m = saveMessage(r, v);
            if (m == null) { return false; }
            Object back = r.fresh();
            TableVocabulary.Report rep = new TableVocabulary.Report();
            boolean ok = (Boolean) r.load.invoke(null, new TableMessage.Reader(m, 0, vocab, rep), back);
            return ok && !rep.malformed && !rep.refused && rep.unknown == 0 && rep.kindMismatch == 0 &&
                rep.widened == 0 && rep.clamped == 0 && dump(back).equals(dump(v));
        } catch (Throwable e) {
            return false;
        }
    }

    static long seed(String root, int i) {
        long h = 0xcbf29ce484222325L;
        for (char ch : root.toCharArray()) { h = (h ^ ch) * 0x100000001b3L; }
        return h ^ (i * 0x9e3779b97f4a7c15L);
    }

    static Object corpus(Root r, int i) throws Exception {
        Object v = r.fresh();
        if (i == 0) { return v; }
        Random rnd = new Random(seed(r.name, i));
        for (int step = 0; step < 96; step++) {
            List<Leaf> ls = new ArrayList<>();
            leaves(v, ls, 0);
            if (ls.isEmpty()) { break; }
            Leaf l = ls.get(rnd.nextInt(ls.size()));
            Object old = l.get();
            l.set(pick(rnd, l.type));
            if (!accepts(r, v)) { l.set(old); }
        }
        return v;
    }

    public static void main(String[] args) throws Exception {
        vocab = new TableVocabulary();
        vocab.reset(4096);
        byte[] ann = new byte[TableVocabulary.announceMeasure()];
        TableVocabulary.announce(ann);
        if (!TableVocabulary.announceRead(vocab, ann, new TableVocabulary.Report())) {
            throw new IllegalStateException("this form's own announcement was refused");
        }
        if (args[0].equals("vjson")) {
            // VJSON root i HEX: the Go oracle's text of a value, read by this
            // form's variable codec and saved again on the variable wire
            for (String line : java.nio.file.Files.readAllLines(java.nio.file.Paths.get(args[1]))) {
                String[] p = line.split(" ", 4);
                if (!p[0].equals("VJSON")) { continue; }
                String out;
                try {
                    Class<?> var = Class.forName(PKG + "." + p[1] + "Variable");
                    TableVariable.Report rep = new TableVariable.Report();
                    Object v = var.getMethod("fromJson", byte[].class, TableVariable.Report.class).invoke(null, unhex(p[3]), rep);
                    if (v == null || rep.malformed || rep.refused || rep.unknown != 0 || rep.clamped != 0 || rep.duplicate != 0) {
                        out = "fromJson malformed=" + rep.malformed + " refused=" + rep.refused + " unknown=" + rep.unknown +
                            " clamped=" + rep.clamped + " duplicate=" + rep.duplicate + " value=" + (v != null);
                    } else {
                        Class<?> c = Class.forName(PKG + "." + p[1] + "Variable$Value");
                        out = hex((byte[]) var.getMethod("save", c).invoke(null, v));
                    }
                } catch (Throwable e) {
                    out = why(e);
                }
                System.out.println("VWIRE " + p[1] + " " + p[2] + " " + out);
            }
            return;
        }
        if (args[0].equals("cross")) {
            for (String line : java.nio.file.Files.readAllLines(java.nio.file.Paths.get(args[1]))) {
                String[] p = line.split(" ", 4);
                if (!p[0].equals("BODY") || p[3].startsWith("EXC")) { continue; }
                Root r = new Root(p[1]);
                Object v = r.fresh();
                String out;
                try {
                    r.scatter.invoke(null, unhex(p[3]), 0, v, new TableFixed.Report());
                    out = dump(v);
                } catch (Throwable e) {
                    out = why(e);
                }
                System.out.println("XBODY " + p[1] + " " + p[2] + " " + out);
            }
            return;
        }
        for (int a = 1; a < args.length; a++) {
            Root r = new Root(args[a]);
            for (int i = 0; i < %d; i++) {
                String tag = r.name + " " + i + " ";
                Object v;
                try {
                    v = corpus(r, i);
                } catch (Throwable e) {
                    System.out.println("VAL " + tag + why(e));
                    continue;
                }
                System.out.println("VAL " + tag + dump(v));
                try {
                    byte[] b = body(r, v);
                    System.out.println("BODY " + tag + hex(b));
                    Object back = r.fresh();
                    r.scatter.invoke(null, b, 0, back, new TableFixed.Report());
                    System.out.println("RBODY " + tag + dump(back));
                } catch (Throwable e) {
                    System.out.println("BODY " + tag + why(e));
                }
                if (r.save == null) { continue; }
                try {
                    byte[] m = saveMessage(r, v);
                    if (m == null) {
                        System.out.println("MSG " + tag + "refused");
                        continue;
                    }
                    Object back = r.fresh();
                    TableVocabulary.Report rep = new TableVocabulary.Report();
                    boolean ok = (Boolean) r.load.invoke(null, new TableMessage.Reader(m, 0, vocab, rep), back);
                    System.out.println("MSG " + tag + "load=" + ok + " malformed=" + rep.malformed + " unknown=" + rep.unknown +
                        " kindMismatch=" + rep.kindMismatch + " " + dump(back));
                    byte[] whole = wholeMessage(r, v);
                    System.out.println("WIRE " + tag + (whole == null ? "refused" : hex(whole)));
                } catch (Throwable e) {
                    System.out.println("MSG " + tag + why(e));
                }
            }
        }
    }
}
`

// keyedFormJava compiles one form's generated Java beside the driver and
// answers its class directory.
func keyedFormJava(t *testing.T, javac, dir string, u *ir.Unit, files map[string][]byte) string {
	t.Helper()
	packet, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	for _, d := range []string{src, classes} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	all := map[string][]byte{}
	maps.Copy(all, files)
	for _, name := range []string{"Int128.java", "UInt128.java"} {
		if data := packet[name]; data != nil && all[name] == nil {
			all[name] = data
		}
	}
	all["KeyedWireDriver.java"] = []byte(fmt.Sprintf(keyedWireDriver, u.Package, u.Package, keyedWireCorpus))
	args := []string{"--release", "17", "-nowarn", "-d", classes}
	for name, data := range all {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		p := filepath.Join(src, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		args = append(args, p)
	}
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac %s: %v\n%s", dir, err, out)
	}
	return classes
}

// keyedRun runs the driver and answers its lines keyed by "KIND root i".
func keyedRun(t *testing.T, javaBin, classes, pkg string, args ...string) (map[string]string, string) {
	t.Helper()
	cmd := exec.Command(javaBin, append([]string{"-ea", "-cp", classes, pkg + ".KeyedWireDriver"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("java %s %v: %v\n%s", classes, args, err, out)
	}
	lines := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for sc.Scan() {
		p := strings.SplitN(sc.Text(), " ", 4)
		if len(p) == 4 {
			lines[p[0]+" "+p[1]+" "+p[2]] = p[3]
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return lines, string(out)
}

// keyedRebind pairs every declaration of the LOWERED parse with the same
// declaration of the keyed parse, so an instance the Go oracle decoded under
// the unkeyed unit can be encoded under the keyed one. The lowering changes
// nothing but a keyed field's spelling, and a keyed array's storage is the
// positional array's (every slot exists, len == E.Max, §2.4), so moving the
// declarations is the whole of the move: no value is touched.
type keyedRebind struct {
	structs map[*ir.Struct]*ir.Struct
	fields  map[*ir.Field]*ir.Field
}

func newKeyedRebind(t *testing.T, from, to *ir.Unit) *keyedRebind {
	t.Helper()
	r := &keyedRebind{structs: map[*ir.Struct]*ir.Struct{}, fields: map[*ir.Field]*ir.Field{}}
	var pairStruct func(a, b *ir.Struct)
	pairField := func(a, b *ir.Field) {
		if a == nil || b == nil {
			return
		}
		r.fields[a] = b
		if a.MapEntry != nil && b.MapEntry != nil {
			pairStruct(a.MapEntry, b.MapEntry)
		}
	}
	pairStruct = func(a, b *ir.Struct) {
		if b == nil || len(a.Fields) != len(b.Fields) {
			t.Fatalf("the keyed parse has no twin of %s", a.Name)
		}
		if _, done := r.structs[a]; done {
			return
		}
		r.structs[a] = b
		for i := range a.Fields {
			pairField(a.Fields[i], b.Fields[i])
		}
	}
	for name, a := range from.Tables {
		pairStruct(a, to.Tables[name])
	}
	for name, a := range from.Structs {
		pairStruct(a, to.Structs[name])
	}
	for _, unions := range [][2]map[string]*ir.Union{{from.Unions, to.Unions}, {from.TableUnions, to.TableUnions}} {
		for name, a := range unions[0] {
			b := unions[1][name]
			if b == nil || len(a.Variants) != len(b.Variants) {
				t.Fatalf("the keyed parse has no twin of union %s", name)
			}
			for i := range a.Variants {
				pairField(a.Variants[i].F, b.Variants[i].F)
			}
		}
	}
	return r
}

func (r *keyedRebind) instance(t *testing.T, in *tabletext.Instance, seen map[*tabletext.Instance]bool) {
	t.Helper()
	if in == nil || seen[in] {
		return
	}
	seen[in] = true
	to, ok := r.structs[in.Def]
	if !ok {
		t.Fatalf("no keyed twin of %s", in.Def.Name)
	}
	in.Def = to
	for i := range in.Fields {
		r.field(t, &in.Fields[i], seen)
	}
}

func (r *keyedRebind) field(t *testing.T, fv *tabletext.Field, seen map[*tabletext.Instance]bool) {
	t.Helper()
	to, ok := r.fields[fv.Def]
	if !ok {
		t.Fatalf("no keyed twin of field %s", fv.Def.Name)
	}
	fv.Def = to
	cell := func(c *tabletext.Cell) {
		r.instance(t, c.Tab, seen)
		r.instance(t, c.Node, seen)
		if c.Arm != nil {
			r.field(t, c.Arm, seen)
		}
	}
	cell(&fv.Cell)
	for i := range fv.Elems {
		cell(&fv.Elems[i])
	}
	for i := range fv.Entries {
		cell(&fv.Entries[i])
	}
}

func keyedClip(s string) string {
	if len(s) > 400 {
		return s[:400] + "..."
	}
	return s
}

// TestKeyedFormWireCodeMatchesUnkeyedInTree holds §2.4's additive rule BY
// BEHAVIOUR, with no committed pin and no testdata: for every unit with an
// enum-keyed field, the Java is generated twice in the same tree, the keyed
// form and the same unit with every key lowered to the positional
// `[E.Max]T` its storage already is. Both are compiled, and one corpus (each
// root's default, then seeded random values) is run through each:
//
//   - the FIXED record body must be byte-identical, and each form's reader
//     must decode the other's bytes to what it decodes from its own;
//   - the MESSAGE body cannot be: a keyed field rides as kind 16, slot by key
//     NAME, and the positional one as kind 14 (§3.2), so the two forms must
//     instead agree on the saved-and-loaded value of every corpus entry,
//     refusals included. A keyed pair written under the wrong name slot, at
//     the wrong count width, or from the wrong element loads a different
//     value than the positional form does;
//   - and since a round trip cannot see a SYMMETRIC fault (a pair saved
//     under the next variant's name and loaded back by the same mapping),
//     each form's whole message, and the keyed form's variable wire, is held
//     byte for byte to the Go codec's (internal/tablewire) encoding of the
//     same value, the value crossing by the unkeyed form's message.
func TestKeyedFormWireCodeMatchesUnkeyedInTree(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaTools(t)
	root := filepath.Join("..", "..", "..")
	ran := 0
	for _, dir := range keyedUnits(t) {
		rel, _ := filepath.Rel(root, dir)
		rel = filepath.ToSlash(rel)
		unkeyedUnit := unitFromDir(t, dir)
		if !lowerKeyedFields(unkeyedUnit) {
			continue
		}
		ran++
		dir := dir
		t.Run(strings.ReplaceAll(rel, "/", "_"), func(t *testing.T) {
			t.Parallel()
			keyedUnit := unitFromDir(t, dir)
			keyed, err := Generate(keyedUnit)
			if err != nil {
				t.Fatalf("%s: generating the keyed form: %v", rel, err)
			}
			unkeyed, err := Generate(unkeyedUnit)
			if err != nil {
				t.Fatalf("%s: generating the unkeyed form: %v", rel, err)
			}
			var roots []string
			for name, data := range keyed {
				if r, ok := strings.CutSuffix(name, "Fixed.java"); ok && strings.Contains(string(data), "public static void writeBody(") {
					roots = append(roots, r)
				}
			}
			sort.Strings(roots)
			if len(roots) == 0 {
				t.Fatalf("%s: no fixed record to encode", rel)
			}
			work := t.TempDir()
			kc := keyedFormJava(t, javac, filepath.Join(work, "keyed"), keyedUnit, keyed)
			uc := keyedFormJava(t, javac, filepath.Join(work, "unkeyed"), unkeyedUnit, unkeyed)
			pkg := keyedUnit.Package
			k, kout := keyedRun(t, javaBin, kc, pkg, append([]string{"gen"}, roots...)...)
			un, uout := keyedRun(t, javaBin, uc, pkg, append([]string{"gen"}, roots...)...)
			kb := filepath.Join(work, "keyed.out")
			ub := filepath.Join(work, "unkeyed.out")
			if err := os.WriteFile(kb, []byte(kout), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(ub, []byte(uout), 0o644); err != nil {
				t.Fatal(err)
			}
			kx, _ := keyedRun(t, javaBin, kc, pkg, "cross", ub)
			ux, _ := keyedRun(t, javaBin, uc, pkg, "cross", kb)

			keys := make([]string, 0, len(k)+len(un))
			seen := map[string]bool{}
			for _, m := range []map[string]string{k, un} {
				for key := range m {
					if !seen[key] {
						seen[key] = true
						keys = append(keys, key)
					}
				}
			}
			sort.Strings(keys)
			bad := 0
			report := func(format string, args ...any) {
				if bad++; bad <= 12 {
					t.Errorf(format, args...)
				}
			}
			moved, bodies, messages := 0, 0, 0
			for _, key := range keys {
				if strings.HasPrefix(key, "WIRE ") {
					continue // the forms' message wires differ by kind (§3.2); the Go oracle holds each below
				}
				kv, kok := k[key]
				uv, uok := un[key]
				if !kok || !uok || kv != uv {
					report("%s %s: the keyed form differs from the in-tree unkeyed form (docs/SPEC-TABLES.md §2.4)\n  keyed:   %s\n  unkeyed: %s",
						rel, key, keyedClip(kv), keyedClip(uv))
					continue
				}
				kind, rest, _ := strings.Cut(key, " ")
				switch kind {
				case "VAL":
					if _, i, _ := strings.Cut(rest, " "); i != "0" && kv != k["VAL "+strings.TrimSuffix(rest, " "+i)+" 0"] {
						moved++
					}
				case "BODY":
					if !strings.HasPrefix(kv, "EXC") {
						bodies++
					}
					// CROSS-DECODE: each reader on the other form's bytes
					// decodes what the other decodes from its own.
					if !strings.HasPrefix(kv, "EXC") {
						if got, want := kx["XBODY "+rest], un["RBODY "+rest]; got != want {
							report("%s %s: the keyed reader decodes the unkeyed body differently (docs/SPEC-TABLES.md §2.4)\n  keyed:   %s\n  unkeyed: %s", rel, rest, keyedClip(got), keyedClip(want))
						}
						if got, want := ux["XBODY "+rest], k["RBODY "+rest]; got != want {
							report("%s %s: the unkeyed reader decodes the keyed body differently (docs/SPEC-TABLES.md §2.4)\n  unkeyed: %s\n  keyed:   %s", rel, rest, keyedClip(got), keyedClip(want))
						}
					}
				case "MSG":
					if strings.HasPrefix(kv, "load=true malformed=false") {
						messages++
					}
				}
			}
			// THE GO ORACLE. A round trip through one form cannot see a
			// symmetric fault (a pair saved under the next variant's NAME and
			// loaded back by the same mapping), so each form's bytes are held
			// to the Go codec (internal/tablewire), written from the spec and
			// from neither backend. The VALUE crosses by the unkeyed form,
			// whose message carries no keyed pair: the Go codec decodes it and
			// must re-encode it byte for byte, and the same instance, moved
			// onto the keyed declarations, must encode to the keyed form's
			// message byte for byte. Its text is then read by the keyed form's
			// VARIABLE codec, whose save must be the Go codec's variable wire
			// byte for byte.
			um := tabletext.NewModel(unkeyedUnit)
			km := tabletext.NewModel(keyedUnit)
			vocab := &tablewire.Vocabulary{}
			var arep tabletext.Report
			if err := vocab.AnnounceRead(ir.TableAnnouncement(unkeyedUnit), &arep); err != nil {
				t.Fatalf("%s: the Go oracle refused the unkeyed announcement: %v", rel, err)
			}
			rb := newKeyedRebind(t, unkeyedUnit, keyedUnit)
			var vjson strings.Builder
			wantVariable := map[string]string{}
			oracled := 0
			for _, key := range keys {
				rest, ok := strings.CutPrefix(key, "WIRE ")
				if !ok {
					continue
				}
				kw, uw := k[key], un[key]
				if kw == "refused" || uw == "refused" {
					if kw != uw {
						report("%s %s: one form refuses a message the other saves\n  keyed:   %s\n  unkeyed: %s", rel, rest, keyedClip(kw), keyedClip(uw))
					}
					continue
				}
				root, _, _ := strings.Cut(rest, " ")
				st := um.Lookup(root)
				if st == nil {
					t.Fatalf("%s: the Go oracle has no root %s", rel, root)
				}
				uwire, err := hex.DecodeString(uw)
				if err != nil {
					t.Fatalf("%s %s: %v", rel, rest, err)
				}
				inst := um.New(st)
				var rep tabletext.Report
				if ok, err := tablewire.DecodeMessage(um, inst, uwire, vocab, &rep); err != nil || !ok || rep != (tabletext.Report{}) {
					report("%s %s: the Go oracle does not read the unkeyed form's message cleanly: ok=%v err=%v %+v\n  wire: %s", rel, rest, ok, err, rep, keyedClip(uw))
					continue
				}
				if again, err := tablewire.EncodeMessage(um, inst); err != nil || hex.EncodeToString(again) != uw {
					report("%s %s: the unkeyed form's message is not the Go codec's encoding of the value it carries (err %v)\n  java: %s\n  go:   %s", rel, rest, err, keyedClip(uw), keyedClip(hex.EncodeToString(again)))
					continue
				}
				rb.instance(t, inst, map[*tabletext.Instance]bool{})
				want, err := tablewire.EncodeMessage(km, inst)
				if err != nil {
					t.Fatalf("%s %s: the Go oracle does not encode the keyed value: %v", rel, rest, err)
				}
				if got := hex.EncodeToString(want); got != kw {
					report("%s %s: the keyed form's message is not the Go codec's encoding of the same value (docs/SPEC-TABLES.md §2.4, §3.2)\n  java: %s\n  go:   %s", rel, rest, keyedClip(kw), keyedClip(got))
					continue
				}
				oracled++
				text, err := km.Write(inst)
				if err != nil {
					t.Fatalf("%s %s: the Go oracle does not write the keyed value's text: %v", rel, rest, err)
				}
				vwire, err := tablewire.Encode(km, inst)
				if err != nil {
					t.Fatalf("%s %s: the Go oracle does not encode the keyed variable wire: %v", rel, rest, err)
				}
				wantVariable[rest] = hex.EncodeToString(vwire)
				fmt.Fprintf(&vjson, "VJSON %s %x\n", rest, text)
			}
			variables := 0
			if len(wantVariable) > 0 {
				vf := filepath.Join(work, "vjson.txt")
				if err := os.WriteFile(vf, []byte(vjson.String()), 0o644); err != nil {
					t.Fatal(err)
				}
				kv, _ := keyedRun(t, javaBin, kc, pkg, "vjson", vf)
				rests := make([]string, 0, len(wantVariable))
				for rest := range wantVariable {
					rests = append(rests, rest)
				}
				sort.Strings(rests)
				for _, rest := range rests {
					if got, want := kv["VWIRE "+rest], wantVariable[rest]; got != want {
						report("%s %s: the keyed form's variable save of the Go oracle's text is not the Go codec's variable wire (docs/SPEC-TABLES.md §2.4, §3.2)\n  java: %s\n  go:   %s", rel, rest, keyedClip(got), keyedClip(want))
						continue
					}
					variables++
				}
			}
			t.Logf("%s: %d values moved, %d fixed bodies, %d messages loaded; %d messages and %d variable wires held to the Go oracle", rel, moved, bodies, messages, oracled, variables)
			if bad > 12 {
				t.Errorf("%s: %d differences in all", rel, bad)
			}
			// the corpus is not vacuous: values moved off the default, both
			// wires carried them, and the Go oracle held them
			if moved == 0 || bodies == 0 || messages == 0 || oracled == 0 || variables == 0 {
				t.Errorf("%s: the corpus held nothing: %d moved values, %d bodies, %d loaded messages, %d messages and %d variable wires held to the Go oracle, over roots %v",
					rel, moved, bodies, messages, oracled, variables, roots)
			}
		})
	}
	if ran == 0 {
		t.Fatal("no unit under tables/ has an enum-keyed field: the test holds nothing")
	}
}
