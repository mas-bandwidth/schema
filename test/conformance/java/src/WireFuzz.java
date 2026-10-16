// THE JAVA WIRE-FUZZ DRIVER (test/conformance/README.md, "The wire-fuzz
// driver"). One process on a pipe; the harness owns the mutants and the oracle
// (test/conformance/harness/wirefuzz.go). This process binds each roster entry
// to its generated codec and answers, per mutant, the load verdict, the report
// and the re-saved bytes. It decides nothing about what a mutant means.
//
// EVERY ROOT OF THE ROSTER (java-wire-fuzz-every-root). The roster names a
// (unit, root, form, retain) per entry and the codec is resolved by name from
// the generated unit, so the same process serves the Card root of the cards
// gates and every root of the harness's default roster
// (testdata/conformance/tables/MANIFEST.txt):
//
//   form 1, retain 0  <pkg>.<Root>Variable: load, loadMeasure, save   (§3, §6.5)
//   form 1, retain 1  <pkg>.<Root>Retain: loadRetain, saveRetain over a
//                     caller-owned <pkg>.TableRetain                    (§6.6)
//   form 2            <pkg>.<Root>Message: loadMessages, measureMessages,
//                     saveMessages against the connection's vocabulary,
//                     derived from the unit's OWN announcement         (§3.3)
//
// A root the unit emits no codec for is a 0 in the roster reply, which the
// harness counts as absent. The driver names every such root on stderr, and it
// REFUSES TO START (exit 3, the root named) when a FILE root (form 1) has no
// codec: the default roster has no file root without one, so a unit that failed
// to reach the classpath is a failure by name and not a quietly smaller run. A
// form-2 root with no <Root>Message class is the generator's decision (a
// pointered root has no fixed-class message body) and stays absent.
//
// The stream is little-endian throughout (the harness's own convention).
import java.io.EOFException;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.lang.reflect.Constructor;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;

public final class WireFuzz {
    private WireFuzz() {}

    // the manifest's unit key is not always the schema's package (the Driver's
    // own table, test/conformance/java/src/Driver.java variableUnitPackage)
    private static String[] packages(String unit) {
        switch (unit) {
            case "scalars": return new String[] {"scalardemo"};
            case "widedemo": return new String[] {"wide"};
            case "tblscalars2": return new String[] {"scalardemo2"};
            case "tblk1": return new String[] {"kind1", "tblk1"};
            case "tblk2": return new String[] {"kind2", "tblk2"};
            default: return new String[] {unit};
        }
    }

    // One bound roster entry.
    private static final class Binding {
        Class<?> reportType;
        // form 1
        Method load;
        Method measure;
        Method save;
        // form 1, retain 1
        Method loadRetain;
        Method saveRetain;
        Constructor<?> retainStore;
        // form 2
        Method reset;
        Method loadMessages;
        Method measureMessages;
        Method saveMessages;
        Class<?> valueType;
        Object vocab;
        Object[] values;
    }

    private static Class<?> classOf(String pkg, String name) {
        try {
            return Class.forName(pkg + "." + name);
        } catch (ClassNotFoundException absent) {
            return null;
        }
    }

    private static Binding bindFile(String pkg, String root, boolean retain) throws ReflectiveOperationException {
        Class<?> codec = classOf(pkg, root + "Variable");
        Class<?> report = classOf(pkg, "TableVariable$Report");
        Class<?> value = classOf(pkg, root + "Variable$Value");
        if (codec == null || report == null || value == null) {
            return null;
        }
        Binding b = new Binding();
        b.reportType = report;
        b.valueType = value;
        if (!retain) {
            b.load = codec.getMethod("load", byte[].class, report);
            b.measure = codec.getMethod("loadMeasure", byte[].class);
            b.save = codec.getMethod("save", value);
            return b;
        }
        Class<?> retaining = classOf(pkg, root + "Retain");
        Class<?> store = classOf(pkg, "TableRetain");
        if (retaining == null || store == null) {
            return null;
        }
        b.measure = codec.getMethod("loadMeasure", byte[].class);
        b.loadRetain = retaining.getMethod("loadRetain", byte[].class, store, report);
        b.saveRetain = retaining.getMethod("saveRetain", value, store, report);
        b.retainStore = store.getConstructor(int.class, int.class);
        return b;
    }

    private static Binding bindMessage(String pkg, String root) throws ReflectiveOperationException {
        Class<?> vocab = classOf(pkg, "TableVocabulary");
        Class<?> report = classOf(pkg, "TableVocabulary$Report");
        Class<?> value = classOf(pkg, root + "Fixed$Value");
        Class<?> message = classOf(pkg, root + "Message");
        if (vocab == null || report == null || value == null || message == null) {
            return null;
        }
        Binding b = new Binding();
        b.reportType = report;
        b.valueType = value;
        Class<?> array = java.lang.reflect.Array.newInstance(value, 0).getClass();
        b.reset = message.getMethod("reset", value);
        b.loadMessages = message.getMethod("loadMessages", array, int.class, vocab, byte[].class, report);
        b.measureMessages = message.getMethod("measureMessages", array, int.class, report);
        b.saveMessages = message.getMethod("saveMessages", array, int.class, byte[].class, report);
        // THE CONNECTION'S VOCABULARY is a pure function of the build version, so
        // the leg derives it from its own unit's announcement (§3.3).
        byte[] announcement = new byte[(int) vocab.getMethod("announceMeasure").invoke(null)];
        vocab.getMethod("announce", byte[].class).invoke(null, (Object) announcement);
        b.vocab = vocab.getConstructor().newInstance();
        vocab.getMethod("reset", int.class).invoke(b.vocab, 4096);
        Object seen = report.getConstructor().newInstance();
        if (!(boolean) vocab.getMethod("announceRead", vocab, byte[].class, report).invoke(null, b.vocab, announcement, seen)) {
            return null;
        }
        b.values = (Object[]) java.lang.reflect.Array.newInstance(value, 256);
        for (int i = 0; i < b.values.length; i++) {
            b.values[i] = value.getConstructor().newInstance();
        }
        return b;
    }

    private static Binding bind(String unit, String root, int form, boolean retain) throws ReflectiveOperationException {
        for (String pkg : packages(unit)) {
            Binding b;
            if (form == 1) {
                b = bindFile(pkg, root, retain);
            } else if (form == 2 && !retain) {
                b = bindMessage(pkg, root);
            } else {
                b = null;
            }
            if (b != null) {
                return b;
            }
        }
        return null;
    }

    private static int count(Object report, String field) throws ReflectiveOperationException {
        try {
            return report.getClass().getField(field).getInt(report);
        } catch (NoSuchFieldException none) {
            return 0;
        }
    }

    private static boolean flag(Object report, String field) throws ReflectiveOperationException {
        try {
            return report.getClass().getField(field).getBoolean(report);
        } catch (NoSuchFieldException none) {
            return false;
        }
    }

    // One mutant's answer, in the order of the reply.
    private static final class Answer {
        boolean loaded;
        Object report;
        long measure = -1;
        byte[] saved;
        boolean malformedForced;
    }

    private static Answer runFile(Binding b, byte[] wire) throws ReflectiveOperationException {
        Answer a = new Answer();
        a.report = b.reportType.getConstructor().newInstance();
        Object value = b.load.invoke(null, wire, a.report);
        a.measure = (long) b.measure.invoke(null, (Object) wire);
        a.loaded = value != null;
        if (value != null) {
            a.saved = (byte[]) b.save.invoke(null, value);
        }
        return a;
    }

    // THE RETAINING PAIR (§6.6): the same mutant read and re-saved through the
    // caller-owned storage, the two counters read after the save so they
    // accumulate across the pair.
    private static Answer runRetain(Binding b, byte[] wire) throws ReflectiveOperationException {
        Answer a = new Answer();
        a.report = b.reportType.getConstructor().newInstance();
        Object store = b.retainStore.newInstance(1 << 20, (1 << 20) / 8);
        Object value = b.loadRetain.invoke(null, wire, store, a.report);
        a.measure = (long) b.measure.invoke(null, (Object) wire);
        a.loaded = value != null;
        if (value != null) {
            a.saved = (byte[]) b.saveRetain.invoke(null, value, store, a.report);
        }
        return a;
    }

    // THE MESSAGE FORM (§3.3): a batch is the form's primitive and a mutant of
    // one body is a batch of one, so the first body is measured and saved
    // whatever the load made of the rest; the leg reports the region as -1 and
    // the load as made, exactly as the Go leg does.
    private static Answer runMessage(Binding b, byte[] wire) throws ReflectiveOperationException {
        Answer a = new Answer();
        a.report = b.reportType.getConstructor().newInstance();
        for (Object v : b.values) {
            b.reset.invoke(null, v);
        }
        int loaded = (int) b.loadMessages.invoke(null, (Object) b.values, b.values.length, b.vocab, wire, a.report);
        a.loaded = true;
        a.malformedForced = loaded < 1 && !flag(a.report, "refused");
        Object ignored = b.reportType.getConstructor().newInstance();
        int size = (int) b.measureMessages.invoke(null, (Object) b.values, 1, ignored);
        if (size >= 0) {
            byte[] out = new byte[size];
            int wrote = (int) b.saveMessages.invoke(null, (Object) b.values, 1, out, ignored);
            if (wrote == size) {
                a.saved = out;
            }
        }
        return a;
    }

    public static void main(String[] args) throws IOException {
        InputStream stdin = System.in;
        OutputStream stdout = System.out;

        long count = readU32(stdin);
        if (count > 65536) { System.exit(1); }
        Binding[] bound = new Binding[(int) count];
        int[] forms = new int[(int) count];
        boolean[] retain = new boolean[(int) count];
        String[] names = new String[(int) count];
        int refused = 0;
        byte[] known = new byte[(int) count];
        for (int i = 0; i < count; i++) {
            String unit = readName(stdin);
            String root = readName(stdin);
            int form = stdin.read();
            int retainByte = stdin.read();
            forms[i] = form;
            retain[i] = retainByte != 0;
            names[i] = unit + "." + root + " (form " + form + (retain[i] ? ", retain" : "") + ")";
            try {
                bound[i] = bind(unit, root, form, retain[i]);
            } catch (ReflectiveOperationException failure) {
                System.err.println("WireFuzz: cannot bind " + names[i] + ": " + failure);
                System.exit(3);
            }
            if (bound[i] == null) {
                System.err.println("WireFuzz: no Java codec for " + names[i]);
                if (form == 1) {
                    refused++;
                }
            }
            known[i] = (byte) (bound[i] != null ? 1 : 0);
        }
        if (refused > 0) {
            // the roster's file roots are all this leg's: one with no codec is a
            // failure BY NAME, never a smaller run
            System.err.println("WireFuzz: " + refused + " file root(s) of the roster have no Java codec");
            System.exit(3);
        }
        stdout.write(known);
        stdout.flush();

        byte[] out = new byte[1024];
        while (true) {
            long index;
            try {
                index = readU32(stdin);
            } catch (EOFException e) {
                break;
            }
            long size = readU32(stdin);
            byte[] wire = new byte[(int) size];
            int got = 0;
            while (got < size) {
                int n = stdin.read(wire, got, (int) size - got);
                if (n < 0) { System.exit(1); }
                got += n;
            }

            Binding b = bound[(int) index];
            Answer a;
            try {
                if (b == null) {
                    a = new Answer();
                    a.report = new Object();
                } else if (forms[(int) index] == 2) {
                    a = runMessage(b, wire);
                } else if (retain[(int) index]) {
                    a = runRetain(b, wire);
                } else {
                    a = runFile(b, wire);
                }
                int n = 0;
                out[n++] = (byte) (a.loaded ? 1 : 0);
                n = putI32(out, n, count(a.report, "unknown"));
                n = putI32(out, n, count(a.report, "kindMismatch"));
                n = putI32(out, n, count(a.report, "widened"));
                n = putI32(out, n, count(a.report, "clamped"));
                n = putI32(out, n, 0); // duplicate: the wire form raises none
                out[n++] = (byte) (flag(a.report, "malformed") || a.malformedForced ? 1 : 0);
                out[n++] = (byte) (flag(a.report, "refused") ? 1 : 0);
                n = putI64(out, n, a.measure);
                n = putI32(out, n, count(a.report, "retained"));
                n = putI32(out, n, count(a.report, "retainLost"));
                n = putI64(out, n, a.saved == null ? -1 : a.saved.length);
                stdout.write(out, 0, n);
                if (a.saved != null && a.saved.length > 0) {
                    stdout.write(a.saved);
                }
                stdout.flush();
            } catch (InvocationTargetException failure) {
                // a codec that throws on a hostile mutant did not return from it
                System.err.println("WireFuzz: " + names[(int) index] + " threw on a mutant of " + size + " bytes");
                failure.getCause().printStackTrace();
                System.exit(1);
            } catch (ReflectiveOperationException failure) {
                System.err.println("WireFuzz: " + names[(int) index] + " could not be driven: " + failure);
                System.exit(1);
            }
        }
    }

    private static long readU32(InputStream in) throws IOException {
        int b0 = in.read();
        int b1 = in.read();
        int b2 = in.read();
        int b3 = in.read();
        if (b3 < 0) { throw new EOFException(); }
        return (b0 & 0xff) | (b1 & 0xff) << 8 | (b2 & 0xff) << 16 | (long) (b3 & 0xff) << 24;
    }

    private static String readName(InputStream in) throws IOException {
        int n0 = in.read();
        int n1 = in.read();
        if (n1 < 0) { throw new EOFException(); }
        int n = (n0 & 0xff) | (n1 & 0xff) << 8;
        byte[] b = new byte[n];
        int got = 0;
        while (got < n) {
            int r = in.read(b, got, n - got);
            if (r < 0) { throw new EOFException(); }
            got += r;
        }
        return new String(b, java.nio.charset.StandardCharsets.UTF_8);
    }

    private static int putI32(byte[] out, int at, int v) {
        out[at] = (byte) v;
        out[at + 1] = (byte) (v >>> 8);
        out[at + 2] = (byte) (v >>> 16);
        out[at + 3] = (byte) (v >>> 24);
        return at + 4;
    }

    private static int putI64(byte[] out, int at, long v) {
        for (int i = 0; i < 8; i++) {
            out[at + i] = (byte) (v >>> (8 * i));
        }
        return at + 8;
    }
}
