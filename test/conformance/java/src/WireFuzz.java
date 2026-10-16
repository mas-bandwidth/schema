// THE JAVA WIRE-FUZZ DRIVER (test/conformance/README.md, "The wire-fuzz driver").
// The harness announces the complete default roster. This leg resolves each
// variable-form root from the generated package at runtime, so adding a root
// to the corpus cannot silently leave this driver card-only.
import java.io.EOFException;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;

public final class WireFuzz {
    private WireFuzz() {}

    private static final class Codec {
        final Class<?> reportType;
        final Class<?> valueType;
        final Method load;
        final Method measure;
        final Method save;

        Codec(String unit, String root) throws ReflectiveOperationException {
            String pkg = packageFor(unit);
            Class<?> type = Class.forName(pkg + "." + root + "Variable");
            reportType = Class.forName(pkg + ".TableVariable$Report");
            valueType = Class.forName(pkg + "." + root + "Variable$Value");
            load = type.getMethod("load", byte[].class, reportType);
            measure = type.getMethod("loadMeasure", byte[].class);
            save = type.getMethod("save", valueType);
        }
        Object report() throws ReflectiveOperationException { return reportType.getConstructor().newInstance(); }
        Object load(byte[] wire, Object report) throws ReflectiveOperationException { return invoke(load, null, wire, report); }
        long measure(byte[] wire) throws ReflectiveOperationException { return ((Number) invoke(measure, null, wire)).longValue(); }
        byte[] save(Object value) throws ReflectiveOperationException { return (byte[]) invoke(save, null, value); }
        int count(Object report, String name) throws ReflectiveOperationException { return reportType.getField(name).getInt(report); }
        boolean flag(Object report, String name) throws ReflectiveOperationException { return reportType.getField(name).getBoolean(report); }
        private static Object invoke(Method method, Object receiver, Object... args) throws ReflectiveOperationException {
            try { return method.invoke(receiver, args); }
            catch (InvocationTargetException failure) {
                Throwable cause = failure.getCause();
                if (cause instanceof RuntimeException runtime) throw runtime;
                throw failure;
            }
        }
    }

    private static String packageFor(String unit) {
        return switch (unit) {
            case "scalars" -> "scalardemo";
            case "widedemo" -> "wide";
            case "tblscalars2" -> "scalardemo2";
            case "tblk1" -> "kind1";
            case "tblk2" -> "kind2";
            default -> unit;
        };
    }

    private static Codec codec(String unit, String root) {
        try { return new Codec(unit, root); }
        catch (ClassNotFoundException | NoSuchMethodException absent) { return null; }
        catch (ReflectiveOperationException failure) {
            throw new IllegalStateException("cannot bind " + unit + "." + root, failure);
        }
    }

    public static void main(String[] args) throws IOException {
        InputStream stdin = System.in;
        OutputStream stdout = System.out;
        long count = readU32(stdin);
        if (count > 4096) System.exit(1);
        Codec[] codecs = new Codec[(int) count];
        boolean[] available = new boolean[(int) count];
        boolean[] retain = new boolean[(int) count];
        String[] units = new String[(int) count];
        String[] roots = new String[(int) count];
        for (int i = 0; i < count; i++) {
            String unit = readName(stdin);
            String root = readName(stdin);
            units[i] = unit;
            roots[i] = root;
            int form = stdin.read();
            retain[i] = stdin.read() != 0;
            if (form == 1) {
                codecs[i] = codec(unit, root);
                available[i] = codecs[i] != null || (retain[i] && "cards".equals(unit) && "Card".equals(root));
                if (!available[i]) {
                    System.err.println("wire-fuzz: cannot open variable root " + unit + "." + root);
                    System.exit(1);
                }
            } else {
                System.err.println("wire-fuzz: absent form-" + form + " root " + unit + "." + root);
            }
            stdout.write(available[i] ? 1 : 0);
        }
        stdout.flush();

        byte[] out = new byte[64];
        while (true) {
            long index;
            try { index = readU32(stdin); }
            catch (EOFException done) { break; }
            long size = readU32(stdin);
            if (index >= codecs.length || size > Integer.MAX_VALUE) System.exit(1);
            byte[] wire = new byte[(int) size];
            readFully(stdin, wire);

            int loaded = 0;
            long measured = -1, saved = -1;
            byte[] savedBytes = null;
            int unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, retained = 0, retainLost = 0;
            boolean malformed = false, refused = false;
            if (available[(int) index]) {
                try {
                    if (retain[(int) index] && "cards".equals(units[(int) index]) && "Card".equals(roots[(int) index])) {
                        cards.TableVariable.Report report = new cards.TableVariable.Report();
                        cards.TableRetain store = new cards.TableRetain(1 << 20, (1 << 20) / 8);
                        cards.CardVariable.Value value = cards.CardRetain.loadRetain(wire, store, report);
                        measured = cards.CardVariable.loadMeasure(wire);
                        loaded = value == null ? 0 : 1;
                        unknown = report.unknown;
                        kindMismatch = report.kindMismatch;
                        widened = report.widened;
                        clamped = report.clamped;
                        malformed = report.malformed;
                        refused = report.refused;
                        if (value != null) {
                            savedBytes = cards.CardRetain.saveRetain(value, store, report);
                            saved = savedBytes == null ? -1 : savedBytes.length;
                        }
                        retained = report.retained;
                        retainLost = report.retainLost;
                    } else {
                        Codec c = codecs[(int) index];
                        Object report = c.report();
                        Object value = c.load(wire, report);
                        measured = c.measure(wire);
                        loaded = value == null ? 0 : 1;
                        unknown = c.count(report, "unknown");
                        kindMismatch = c.count(report, "kindMismatch");
                        widened = c.count(report, "widened");
                        clamped = c.count(report, "clamped");
                        malformed = c.flag(report, "malformed");
                        refused = c.flag(report, "refused");
                        if (value != null) {
                            savedBytes = c.save(value);
                            saved = savedBytes.length;
                        }
                    }
                } catch (ReflectiveOperationException failure) {
                    System.exit(1);
                }
            }

            int n = 0;
            out[n++] = (byte) loaded;
            n = putI32(out, n, unknown);
            n = putI32(out, n, kindMismatch);
            n = putI32(out, n, widened);
            n = putI32(out, n, clamped);
            n = putI32(out, n, 0);
            out[n++] = (byte) (malformed ? 1 : 0);
            out[n++] = (byte) (refused ? 1 : 0);
            n = putI64(out, n, measured);
            n = putI32(out, n, retained);
            n = putI32(out, n, retainLost);
            n = putI64(out, n, saved);
            stdout.write(out, 0, n);
            if (saved > 0) stdout.write(savedBytes);
            stdout.flush();
        }
    }

    private static long readU32(InputStream in) throws IOException {
        int b0 = in.read(), b1 = in.read(), b2 = in.read(), b3 = in.read();
        if (b3 < 0) throw new EOFException();
        return (b0 & 0xffL) | (b1 & 0xffL) << 8 | (b2 & 0xffL) << 16 | (b3 & 0xffL) << 24;
    }
    private static String readName(InputStream in) throws IOException {
        int n0 = in.read(), n1 = in.read();
        if (n1 < 0) throw new EOFException();
        int n = (n0 & 0xff) | (n1 & 0xff) << 8;
        byte[] bytes = new byte[n];
        readFully(in, bytes);
        return new String(bytes, java.nio.charset.StandardCharsets.UTF_8);
    }
    private static void readFully(InputStream in, byte[] bytes) throws IOException {
        int at = 0;
        while (at < bytes.length) {
            int got = in.read(bytes, at, bytes.length - at);
            if (got < 0) throw new EOFException();
            at += got;
        }
    }
    private static int putI32(byte[] out, int at, int value) {
        out[at] = (byte) value; out[at + 1] = (byte) (value >>> 8);
        out[at + 2] = (byte) (value >>> 16); out[at + 3] = (byte) (value >>> 24);
        return at + 4;
    }
    private static int putI64(byte[] out, int at, long value) {
        for (int i = 0; i < 8; i++) out[at + i] = (byte) (value >>> (8 * i));
        return at + 8;
    }
}
