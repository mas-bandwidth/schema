// THE JAVA LEG OF THE TOLERANT WIRE'S DIFFERENTIAL FUZZER (docs/PORTING.md I15;
// test/conformance/README.md, "The wire-fuzz driver"; docs/SPEC-TABLES.md §4.2).
//
// One process on a pipe for the whole run. The harness owns the mutants and the
// oracle (test/conformance/harness/wirefuzz.go, the compiler's own engine); this
// leg binds each roster entry to the generated codec on the conformance
// classpath and answers, per mutant, the load verdict, the report, what
// LoadMeasure asked for and the re-saved bytes. It decides nothing about what a
// mutant means.
//
// A roster entry is (unit, root, form, retain):
//   form 1, retain 0: <Root>Variable.load / loadMeasure / save.
//   form 1, retain 1: <Root>Retain.loadRetain / saveRetain over one caller-owned
//                     TableRetain with both capacities large (§6.6).
//   form 2:           <Root>Message.loadMessages / measureMessages / saveMessages
//                     against the vocabulary this unit's OWN announcement
//                     resolves to (§3.3), a mutant being a batch of one body.
// A root this classpath does not carry answers 0 in the roster and is never sent
// a mutant. The stream is little-endian throughout.
import java.io.BufferedInputStream;
import java.io.BufferedOutputStream;
import java.io.EOFException;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.lang.reflect.Array;
import java.lang.reflect.Field;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.nio.charset.StandardCharsets;

public final class TablesWireFuzz {
    private TablesWireFuzz() {}

    // Both capacities large (test/conformance/README.md: two engines at one
    // tight capacity would drop different records).
    private static final int RETAIN_BYTES = 1 << 20;
    private static final int RETAIN_IDS = (1 << 20) / 8;
    private static final int BATCH_CAP = 256;

    // The manifest names a unit by its key; the classes live under the unit's
    // schema package (the conformance Driver's variableUnitPackage).
    private static String unitPackage(String unit) {
        switch (unit) {
            case "scalars": return "scalardemo";
            case "widedemo": return "wide";
            case "tblscalars2": return "scalardemo2";
            default: return unit;
        }
    }

    /** The outcome of one mutant, in the reply's order. */
    private static final class Reply {
        boolean loaded;
        int unknown;
        int kindMismatch;
        int widened;
        int clamped;
        int duplicate;
        boolean malformed;
        boolean refused;
        long measure = -1;
        int retained;
        int retainLost;
        byte[] saved; // null: Measure or Save refused
    }

    private abstract static class Codec {
        abstract Reply run(byte[] wire) throws ReflectiveOperationException;
    }

    private static int count(Object report, Class<?> type, String field) throws ReflectiveOperationException {
        Field f;
        try {
            f = type.getField(field);
        } catch (NoSuchFieldException absent) {
            return 0;
        }
        return f.getInt(report);
    }

    private static void fill(Reply r, Object report, Class<?> type) throws ReflectiveOperationException {
        r.unknown = count(report, type, "unknown");
        r.kindMismatch = count(report, type, "kindMismatch");
        r.widened = count(report, type, "widened");
        r.clamped = count(report, type, "clamped");
        r.duplicate = count(report, type, "duplicate");
        r.retained = count(report, type, "retained");
        r.retainLost = count(report, type, "retainLost");
        r.malformed = type.getField("malformed").getBoolean(report);
        r.refused = type.getField("refused").getBoolean(report);
    }

    // form 1, the ordinary run: Load and Save with retention off.
    //
    // A FIXED root always loads (test/conformance/README.md): its read fills a
    // value from whatever decoded before the damage and reports, so the byte is
    // 1, the measure is -1, and the value is saved whatever the report says. The
    // class is declared (§2.2) and the emitter names it: a `fixed table` carries
    // <Root>Fixed, or <Root>Block where its record is past the fixed form's
    // ceiling (§3.4), and a plain `table` carries neither (§2.7 derives the
    // block form for the fixed class alone).
    private static final class FileCodec extends Codec {
        final boolean fixed;
        final Class<?> reportType;
        final Class<?> valueType;
        final Class<?> nodeTableType;
        final Method load;
        final Method open;
        final Method readFields;
        final Method loadMeasure;
        final Method save;

        FileCodec(String pkg, String root) throws ReflectiveOperationException {
            Class<?> codec = Class.forName(pkg + "." + root + "Variable");
            Class<?> variable = Class.forName(pkg + ".TableVariable");
            valueType = Class.forName(pkg + "." + root + "Variable$Value");
            reportType = Class.forName(pkg + ".TableVariable$Report");
            nodeTableType = Class.forName(pkg + ".TableVariable$NodeTable");
            Class<?> reader = Class.forName(pkg + ".TableVariable$Reader");
            fixed = carried(pkg + "." + root + "Fixed") || carried(pkg + "." + root + "Block");
            load = codec.getMethod("load", byte[].class, reportType);
            open = variable.getMethod("open", byte[].class, reportType);
            readFields = codec.getMethod("readFields", reader, valueType, nodeTableType, reportType);
            loadMeasure = codec.getMethod("loadMeasure", byte[].class);
            save = codec.getMethod("save", valueType);
        }

        @Override
        Reply run(byte[] wire) throws ReflectiveOperationException {
            Reply r = new Reply();
            Object report = reportType.getConstructor().newInstance();
            Object v;
            if (fixed) {
                v = valueType.getConstructor().newInstance();
                Object reader = open.invoke(null, wire, report);
                if (reader != null) {
                    readFields.invoke(null, reader, v, null, report);
                }
                r.measure = -1;
            } else {
                r.measure = (long) loadMeasure.invoke(null, (Object) wire);
                v = load.invoke(null, wire, report);
            }
            fill(r, report, reportType);
            r.loaded = v != null;
            if (v != null) {
                // a save that refuses (a shared blob, §2.5) answers null
                r.saved = (byte[]) save.invoke(null, v);
            }
            return r;
        }
    }

    // form 1, the retention arm (§6.6): LoadRetain then SaveRetain over one
    // store, the report read after the save so the two counters accumulate.
    private static final class RetainCodec extends Codec {
        final Class<?> reportType;
        final Class<?> retainType;
        final Method loadRetain;
        final Method saveRetain;
        final Method loadMeasure;

        RetainCodec(String pkg, String root) throws ReflectiveOperationException {
            Class<?> codec = Class.forName(pkg + "." + root + "Retain");
            Class<?> variable = Class.forName(pkg + "." + root + "Variable");
            Class<?> value = Class.forName(pkg + "." + root + "Variable$Value");
            reportType = Class.forName(pkg + ".TableVariable$Report");
            retainType = Class.forName(pkg + ".TableRetain");
            loadRetain = codec.getMethod("loadRetain", byte[].class, retainType, reportType);
            saveRetain = codec.getMethod("saveRetain", value, retainType, reportType);
            loadMeasure = variable.getMethod("loadMeasure", byte[].class);
        }

        @Override
        Reply run(byte[] wire) throws ReflectiveOperationException {
            Reply r = new Reply();
            Object report = reportType.getConstructor().newInstance();
            Object store = retainType.getConstructor(int.class, int.class).newInstance(RETAIN_BYTES, RETAIN_IDS);
            r.measure = (long) loadMeasure.invoke(null, (Object) wire);
            Object v = loadRetain.invoke(null, wire, store, report);
            r.loaded = v != null;
            if (v != null) {
                r.saved = (byte[]) saveRetain.invoke(null, v, store, report);
            }
            fill(r, report, reportType);
            return r;
        }
    }

    // form 2: a batch of one body against this unit's own vocabulary (§3.3).
    private static final class MessageCodec extends Codec {
        final Class<?> vocabType;
        final Class<?> reportType;
        final Class<?> valueType;
        final Method loadMessages;
        final Method measureMessages;
        final Method saveMessages;
        final Object vocabulary;

        MessageCodec(String pkg, String root) throws ReflectiveOperationException {
            vocabType = Class.forName(pkg + ".TableVocabulary");
            reportType = Class.forName(pkg + ".TableVocabulary$Report");
            valueType = Class.forName(pkg + "." + root + "Fixed$Value");
            Class<?> values = Array.newInstance(valueType, 0).getClass();
            Class<?> msg = Class.forName(pkg + "." + root + "Message");
            loadMessages = msg.getMethod("loadMessages", values, int.class, vocabType, byte[].class, reportType);
            measureMessages = msg.getMethod("measureMessages", values, int.class, reportType);
            saveMessages = msg.getMethod("saveMessages", values, int.class, byte[].class, reportType);
            // the vocabulary is a pure function of the build version: the
            // compile-time announcement, read through the receiver's own path
            int size = (int) vocabType.getMethod("announceMeasure").invoke(null);
            byte[] announcement = new byte[size];
            if ((int) vocabType.getMethod("announce", byte[].class).invoke(null, (Object) announcement) != size) {
                throw new IllegalStateException(pkg + ": announce refused its own measure");
            }
            vocabulary = vocabType.getConstructor().newInstance();
            vocabType.getMethod("reset", int.class).invoke(vocabulary, 4096);
            Object report = reportType.getConstructor().newInstance();
            boolean ok = (boolean) vocabType.getMethod("announceRead", vocabType, byte[].class, reportType)
                    .invoke(null, vocabulary, announcement, report);
            if (!ok || reportType.getField("malformed").getBoolean(report) || reportType.getField("refused").getBoolean(report)) {
                throw new IllegalStateException(pkg + ": announceRead refused the unit's own announcement");
            }
        }

        @Override
        Reply run(byte[] wire) throws ReflectiveOperationException {
            Reply r = new Reply();
            Object report = reportType.getConstructor().newInstance();
            Object values = Array.newInstance(valueType, BATCH_CAP);
            Array.set(values, 0, valueType.getConstructor().newInstance());
            loadMessages.invoke(null, values, BATCH_CAP, vocabulary, wire, report);
            fill(r, report, reportType);
            // no message LoadMeasure: the root came back, and measure is -1
            r.loaded = true;
            if (Array.get(values, 0) == null) {
                Array.set(values, 0, valueType.getConstructor().newInstance());
            }
            Object one = Array.newInstance(valueType, 1);
            Array.set(one, 0, Array.get(values, 0));
            Object scratch = reportType.getConstructor().newInstance();
            int size = (int) measureMessages.invoke(null, one, 1, scratch);
            if (size >= 0) {
                byte[] buf = new byte[size];
                int wrote = (int) saveMessages.invoke(null, one, 1, buf, scratch);
                r.saved = wrote == size ? buf : null;
            }
            return r;
        }
    }

    private static boolean carried(String name) {
        try {
            Class.forName(name);
            return true;
        } catch (ClassNotFoundException absent) {
            return false;
        }
    }

    private static Codec bind(String unit, String root, int form, boolean retain) {
        String pkg = unitPackage(unit);
        try {
            if (form == 2) {
                return retain ? null : new MessageCodec(pkg, root);
            }
            if (form != 1) {
                return null;
            }
            return retain ? new RetainCodec(pkg, root) : new FileCodec(pkg, root);
        } catch (ClassNotFoundException | NoSuchMethodException absent) {
            return null;
        } catch (ReflectiveOperationException failed) {
            throw new IllegalStateException(unit + "." + root + ": " + failed, failed);
        }
    }

    public static void main(String[] args) throws IOException, ReflectiveOperationException {
        InputStream in = new BufferedInputStream(System.in, 1 << 16);
        OutputStream out = new BufferedOutputStream(System.out, 1 << 16);

        long n = readU32(in);
        if (n > 1 << 16) {
            System.err.println("wire-fuzz: a roster of " + n + " entries");
            System.exit(1);
        }
        Codec[] roster = new Codec[(int) n];
        for (int i = 0; i < n; i++) {
            String unit = readName(in);
            String root = readName(in);
            int form = readByte(in);
            int retain = readByte(in);
            roster[i] = bind(unit, root, form, retain != 0);
            out.write(roster[i] != null ? 1 : 0);
        }
        out.flush();

        byte[] head = new byte[1 + 5 * 4 + 2 + 8 + 2 * 4 + 8];
        while (true) {
            long index;
            try {
                index = readU32(in);
            } catch (EOFException done) {
                break;
            }
            long size = readU32(in);
            byte[] wire = new byte[(int) size];
            readFully(in, wire);
            if (index >= roster.length || roster[(int) index] == null) {
                System.err.println("wire-fuzz: a mutant for roster entry " + index + ", which this leg answered absent");
                System.exit(1);
            }
            Reply r;
            try {
                r = roster[(int) index].run(wire);
            } catch (InvocationTargetException thrown) {
                // a generated codec that throws on a mutant is a crash, attributed
                // to the mutant the harness just sent
                System.err.println("wire-fuzz: the codec threw on roster entry " + index + ": " + thrown.getCause());
                thrown.getCause().printStackTrace();
                System.exit(2);
                return;
            }
            int at = 0;
            head[at++] = (byte) (r.loaded ? 1 : 0);
            at = putI32(head, at, r.unknown);
            at = putI32(head, at, r.kindMismatch);
            at = putI32(head, at, r.widened);
            at = putI32(head, at, r.clamped);
            at = putI32(head, at, r.duplicate);
            head[at++] = (byte) (r.malformed ? 1 : 0);
            head[at++] = (byte) (r.refused ? 1 : 0);
            at = putI64(head, at, r.measure);
            at = putI32(head, at, r.retained);
            at = putI32(head, at, r.retainLost);
            at = putI64(head, at, r.saved == null ? -1 : r.saved.length);
            out.write(head, 0, at);
            if (r.saved != null && r.saved.length > 0) {
                out.write(r.saved);
            }
            out.flush();
        }
        out.flush();
    }

    private static int readByte(InputStream in) throws IOException {
        int b = in.read();
        if (b < 0) {
            throw new EOFException();
        }
        return b;
    }

    private static void readFully(InputStream in, byte[] b) throws IOException {
        int got = 0;
        while (got < b.length) {
            int r = in.read(b, got, b.length - got);
            if (r < 0) {
                throw new EOFException();
            }
            got += r;
        }
    }

    private static long readU32(InputStream in) throws IOException {
        byte[] b = new byte[4];
        int first = in.read();
        if (first < 0) {
            throw new EOFException();
        }
        b[0] = (byte) first;
        int got = 1;
        while (got < 4) {
            int r = in.read(b, got, 4 - got);
            if (r < 0) {
                throw new EOFException();
            }
            got += r;
        }
        return (b[0] & 0xffL) | (b[1] & 0xffL) << 8 | (b[2] & 0xffL) << 16 | (b[3] & 0xffL) << 24;
    }

    private static String readName(InputStream in) throws IOException {
        int n = readByte(in) | readByte(in) << 8;
        byte[] b = new byte[n];
        readFully(in, b);
        return new String(b, StandardCharsets.UTF_8);
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
