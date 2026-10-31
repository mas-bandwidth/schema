// THE JAVA PORT RUNS THE SHARED HOSTILE CORPUS (docs/HOSTILE-CORPUS.md).
//
// One raw input per manifest line, the port's generated reader underneath, and
// the Go engine's committed verdict as the oracle. The corpus is read as files:
// no network, no clock, no outside service. The runner prints one PASS line per
// case it agrees with, one FIX-CARD line per carried disagreement, and fails on
// a disagreement no FIX-CARD names (or a FIX-CARD whose case has gone green).
//
// usage: java -cp <classes> Runner <corpus-dir> <known-findings.tsv>
//
// A carrier maps to the port's public reader (each unit is its own package,
// so the reader is resolved by name from the manifest's schema and table):
//   text              <Table>Variable.fromJson(byte[], TableVariable.Report)
//   wire              <Table>Variable.load(byte[], TableVariable.Report)
//   message           TableVocabulary.announce + announceRead, then
//                     <Table>Message.loadMessages into one value (capacity 1)
//   message_novocab   loadMessages with no vocabulary
//   announce          TableVocabulary.announceRead on a fresh vocabulary
//   announce_second   announceRead twice on one vocabulary
//   announce_bytes    announceRead with maxBytes 8
//   announce_entries  announceRead with maxEntries 1
//
// The canonical decoded value is the port's text writer (<Table>Variable.toJson)
// with the engine's one shape difference removed: the port ends its text with
// one newline and the engine's canonical text (internal/tabletext.Model.Write)
// does not.

import java.io.IOException;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

public final class Runner {
    private Runner() {}

    // The manifest names a unit by its schema path; the generated classes live
    // in the unit's schema package.
    private static final Map<String, String> PACKAGES =
            Map.of(
                    "tables/examples", "tabledemo",
                    "tables/messages", "messagedemo",
                    "tables/blobs", "blobdemo",
                    "tables/pointers", "graphdemo",
                    "tables/scalars", "scalardemo",
                    "tables/stream", "streamdemo",
                    "tables/maps", "mapdemo",
                    "tables/lists", "listdemo",
                    "schemas/probe.schema", "probe");

    // A verdict: "accept <digest>" or "refuse <reason>"; anything else (a
    // throw, a value the runner cannot canonicalise) is its own text and never
    // equals an expectation.
    private static String accept(String digest) {
        return "accept " + digest;
    }

    private static String refuse(String reason) {
        return "refuse " + reason;
    }

    private static String sha256(byte[] text) throws NoSuchAlgorithmException {
        byte[] sum = MessageDigest.getInstance("SHA-256").digest(text);
        StringBuilder b = new StringBuilder(64);
        for (byte x : sum) {
            b.append(Character.forDigit((x >> 4) & 0xf, 16)).append(Character.forDigit(x & 0xf, 16));
        }
        return b.toString();
    }

    private static String pkg(String schema) {
        String p = PACKAGES.get(schema);
        if (p == null) {
            throw new IllegalArgumentException("no Java unit for schema " + schema);
        }
        return p;
    }

    private static final class Variable {
        final Class<?> codec;
        final Class<?> reportType;
        final Class<?> valueType;

        Variable(String pkg, String table) throws ReflectiveOperationException {
            codec = Class.forName(pkg + "." + table + "Variable");
            reportType = Class.forName(pkg + ".TableVariable$Report");
            valueType = Class.forName(pkg + "." + table + "Variable$Value");
        }

        Object report() throws ReflectiveOperationException {
            return reportType.getConstructor().newInstance();
        }

        Object call(String name, byte[] data, Object report) throws ReflectiveOperationException {
            return codec.getMethod(name, byte[].class, reportType).invoke(null, data, report);
        }

        boolean flag(Object report, String field) throws ReflectiveOperationException {
            return reportType.getField(field).getBoolean(report);
        }

        String reason(Object report) throws ReflectiveOperationException {
            return (String) reportType.getField("reason").get(report);
        }

        String digest(Object value) throws ReflectiveOperationException, NoSuchAlgorithmException {
            byte[] text = (byte[]) codec.getMethod("toJson", valueType).invoke(null, value);
            if (text == null) {
                return "digest-error: toJson refused the decoded value";
            }
            int n = text.length;
            while (n > 0 && text[n - 1] == '\n') {
                n--;
            }
            return accept(sha256(Arrays.copyOf(text, n)));
        }

        // A read's verdict off its report: a named refusal is its reason, a
        // malformed read (no name) is the corpus's `damaged`, and a clean read
        // is accepted with the digest of its canonical text.
        String verdict(Object value, Object report) throws ReflectiveOperationException, NoSuchAlgorithmException {
            if (flag(report, "refused") || value == null) {
                String r = reason(report);
                return refuse(r == null || r.isEmpty() ? "damaged" : r);
            }
            if (flag(report, "malformed")) {
                return refuse("damaged");
            }
            return digest(value);
        }
    }

    private static final class Vocabulary {
        final Class<?> vocabType;
        final Class<?> reportType;
        final Method announceRead;

        Vocabulary(String pkg) throws ReflectiveOperationException {
            vocabType = Class.forName(pkg + ".TableVocabulary");
            reportType = Class.forName(pkg + ".TableVocabulary$Report");
            announceRead = vocabType.getMethod("announceRead", vocabType, byte[].class, reportType);
        }

        Object fresh() throws ReflectiveOperationException {
            Object v = vocabType.getConstructor().newInstance();
            vocabType.getMethod("reset", int.class).invoke(v, 4096);
            return v;
        }

        Object report() throws ReflectiveOperationException {
            return reportType.getConstructor().newInstance();
        }

        boolean read(Object vocab, byte[] data, Object report) throws ReflectiveOperationException {
            return (boolean) announceRead.invoke(null, vocab, data, report);
        }

        byte[] own() throws ReflectiveOperationException {
            int n = (int) vocabType.getMethod("announceMeasure").invoke(null);
            byte[] b = new byte[n];
            vocabType.getMethod("announce", byte[].class).invoke(null, (Object) b);
            return b;
        }

        boolean flag(Object report, String field) throws ReflectiveOperationException {
            return reportType.getField(field).getBoolean(report);
        }

        String refusal(Object report) throws ReflectiveOperationException {
            String r = (String) reportType.getField("reason").get(report);
            return refuse(r == null || r.isEmpty() ? "damaged" : r);
        }

        String announceVerdict(Object vocab, byte[] data) throws ReflectiveOperationException {
            Object report = report();
            boolean ok = read(vocab, data, report);
            if (!ok || flag(report, "refused")) {
                return refusal(report);
            }
            if (flag(report, "malformed")) {
                return refuse("damaged");
            }
            return accept("-");
        }
    }

    private static String message(String pkg, String table, byte[] payload, boolean announced)
            throws ReflectiveOperationException, NoSuchAlgorithmException {
        Vocabulary vocabulary = new Vocabulary(pkg);
        Object vocab = null;
        if (announced) {
            vocab = vocabulary.fresh();
            Object ar = vocabulary.report();
            if (!vocabulary.read(vocab, vocabulary.own(), ar) || vocabulary.flag(ar, "malformed")) {
                return "error: the unit's own announcement was refused";
            }
        }
        Class<?> fixedValue = Class.forName(pkg + "." + table + "Fixed$Value");
        Class<?> msg = Class.forName(pkg + "." + table + "Message");
        Object values = java.lang.reflect.Array.newInstance(fixedValue, 1);
        Method load =
                msg.getMethod(
                        "loadMessages",
                        values.getClass(),
                        int.class,
                        vocabulary.vocabType,
                        byte[].class,
                        vocabulary.reportType);
        Object report = vocabulary.report();
        int n = (int) load.invoke(null, values, 1, vocab, payload, report);
        if (vocabulary.flag(report, "refused")) {
            return vocabulary.refusal(report);
        }
        if (vocabulary.flag(report, "malformed") || n != 1) {
            return refuse("damaged");
        }
        // The batch's one body is a fixed-class value; the port's text writer
        // takes the variable value. A body that sets no field decodes to the
        // table's defaults, and fromJson("{}") is where the port states them:
        // the runner compares the decoded record's bytes with a reset value's.
        Object got = java.lang.reflect.Array.get(values, 0);
        Object blank = fixedValue.getConstructor().newInstance();
        msg.getMethod("reset", fixedValue).invoke(null, blank);
        Class<?> fixed = Class.forName(pkg + "." + table + "Fixed");
        int size = fixed.getField("bodyBytes").getInt(null);
        Method writeBody = fixed.getMethod("writeBody", byte[].class, int.class, fixedValue);
        byte[] a = new byte[size];
        byte[] b = new byte[size];
        writeBody.invoke(null, a, 0, got);
        writeBody.invoke(null, b, 0, blank);
        if (!Arrays.equals(a, b)) {
            return "unhandled decoded value: the body sets a field";
        }
        Variable variable = new Variable(pkg, table);
        Object vr = variable.report();
        Object defaults = variable.call("fromJson", "{}".getBytes(StandardCharsets.UTF_8), vr);
        return variable.digest(defaults);
    }

    private static String run(String carrier, String schema, String table, byte[] payload)
            throws ReflectiveOperationException, NoSuchAlgorithmException {
        String pkg = pkg(schema);
        switch (carrier) {
            case "text":
            case "wire": {
                Variable codec = new Variable(pkg, table);
                Object report = codec.report();
                Object value = codec.call(carrier.equals("text") ? "fromJson" : "load", payload, report);
                return codec.verdict(value, report);
            }
            case "message":
                return message(pkg, table, payload, true);
            case "message_novocab":
                return message(pkg, table, payload, false);
            case "announce":
            case "announce_bytes":
            case "announce_entries": {
                Vocabulary vocabulary = new Vocabulary(pkg);
                Object vocab = vocabulary.fresh();
                if (carrier.equals("announce_bytes")) {
                    vocabulary.vocabType.getField("maxBytes").setInt(vocab, 8);
                } else if (carrier.equals("announce_entries")) {
                    vocabulary.vocabType.getField("maxEntries").setInt(vocab, 1);
                }
                return vocabulary.announceVerdict(vocab, payload);
            }
            case "announce_second": {
                Vocabulary vocabulary = new Vocabulary(pkg);
                Object vocab = vocabulary.fresh();
                if (!vocabulary.read(vocab, payload, vocabulary.report())) {
                    return "error: the first announcement was refused";
                }
                return vocabulary.announceVerdict(vocab, payload);
            }
            default:
                throw new IllegalArgumentException("unknown carrier " + carrier);
        }
    }

    private static List<String[]> rows(Path path, int fields) throws IOException {
        List<String[]> out = new ArrayList<>();
        for (String line : Files.readAllLines(path, StandardCharsets.UTF_8)) {
            if (line.isEmpty() || line.startsWith("#")) {
                continue;
            }
            String[] f = line.split("\t", fields);
            if (f.length != fields) {
                throw new IOException(path + ": line has " + f.length + " fields, want " + fields + ": " + line);
            }
            out.add(f);
        }
        return out;
    }

    public static void main(String[] args) throws IOException {
        if (args.length != 2) {
            System.err.println("usage: Runner <corpus-dir> <known-findings.tsv>");
            System.exit(2);
        }
        Path corpus = Path.of(args[0]);
        Path findingsPath = Path.of(args[1]);

        // known findings: name<TAB>slug<TAB>input<TAB>one line
        Map<String, String[]> findings = new HashMap<>();
        if (Files.exists(findingsPath)) {
            for (String[] f : rows(findingsPath, 4)) {
                findings.put(f[0], f);
            }
        }

        List<String[]> manifest = rows(corpus.resolve("manifest.tsv"), 9);
        int passed = 0;
        int carried = 0;
        List<String> bad = new ArrayList<>();
        List<String> stale = new ArrayList<>();
        for (String[] f : manifest) {
            String name = f[0];
            String carrier = f[1];
            byte[] payload = Files.readAllBytes(corpus.resolve(f[4]));
            String expected = f[6].equals("accept") ? accept(f[8]) : refuse(f[7]);
            String got;
            try {
                got = run(carrier, f[2], f[3], payload);
            } catch (InvocationTargetException thrown) {
                got = "raised " + thrown.getCause();
            } catch (ReflectiveOperationException | NoSuchAlgorithmException | RuntimeException failure) {
                got = "runner error " + failure;
            }
            String[] finding = findings.get(name);
            if (got.equals(expected)) {
                if (finding != null) {
                    System.out.println("STALE " + name + ": FIX-CARD " + finding[1] + " no longer reproduces");
                    stale.add(name);
                } else {
                    System.out.println("PASS " + name);
                    passed++;
                }
            } else if (finding != null) {
                System.out.println("FIX-CARD: " + finding[1] + ": " + finding[2] + " " + finding[3]);
                carried++;
            } else {
                System.out.println("FAIL " + name + " carrier=" + carrier + " got=" + got + " want=" + expected);
                bad.add(name);
            }
        }
        System.out.println(
                manifest.size() + " cases: " + passed + " pass, " + carried + " carried as FIX-CARD, "
                        + bad.size() + " unexplained, " + stale.size() + " stale");
        if (!bad.isEmpty() || !stale.isEmpty()) {
            System.exit(1);
        }
    }
}
