// THE JAVA PORT RUNS THE SHARED HOSTILE CORPUS (docs/HOSTILE-CORPUS.md).
//
// One raw input per manifest line, the port's generated reader underneath, and
// the Go engine's committed verdict as the oracle. The corpus is read as files:
// no network, no clock, no outside service. The runner prints one PASS line per
// case it agrees with, one FIX-CARD line per carried disagreement, and fails on
// a disagreement no FIX-CARD names (or a FIX-CARD whose case has gone green).
//
// usage: java -cp <classes> Runner <corpus-dir> <known-findings.tsv> [packet-dir]
//
// A carrier maps to the port's public reader (each unit is its own package,
// so the reader is resolved by name from the manifest's schema and table):
//   text              <Table>Variable.fromJson(byte[], TableVariable.Report)
//   wire              <Table>Variable.load(byte[], TableVariable.Report)
//   wire_retain       <Table>Retain.loadRetain(byte[], TableRetain, Report)
//   message           TableVocabulary.announce + announceRead, then
//                     <Table>Message.loadMessages into one value (capacity 1)
//   message_novocab   loadMessages with no vocabulary
//   announce          TableVocabulary.announceRead on a fresh vocabulary
//   announce_second   announceRead twice on one vocabulary
//   announce_bytes    announceRead with maxBytes 8
//   announce_entries  announceRead with maxEntries 1
//   packet            <File>.read<Type>(value, bytes, 8 * length), then
//                     <File>.write<Type> re-writes the decoded value; the
//                     digest is SHA-256 of those bytes. The packet units are
//                     the ones make/hostile-packet-units.sh writes under
//                     [packet-dir], each loaded by its own class loader.
//
// The canonical decoded value is the port's text writer (<Table>Variable.toJson)
// with the engine's one shape difference removed: the port ends its text with
// one newline and the engine's canonical text (internal/tabletext.Model.Write)
// does not.
//
// THE RUNNER RULES (docs/HOSTILE-CORPUS.md). Every case runs on its own thread
// with a fixed 512 KiB stack and a time budget (5 s, or HOSTILE_CASE_BUDGET_MS).
// A throw of any kind, a stack overflow included, is that case's answer and the
// run goes on; a case still running at its budget is over budget, and its
// thread is a daemon the run leaves behind. After an accepted wire,
// wire_retain, text or message case the runner also measures, saves and writes
// the text of the decoded value, and a throw there is the case's answer too (a
// clean refusal, a null or -1, is not). Cases run one at a time: the budget is
// wall-clock, and parallel cases sharing the bench would charge each other's
// time.

import java.io.IOException;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.net.MalformedURLException;
import java.net.URL;
import java.net.URLClassLoader;
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
import java.util.regex.Matcher;
import java.util.regex.Pattern;

public final class Runner {
    private Runner() {}

    private static final long STACK_BYTES = 512 * 1024;

    // The retaining load's two stores: roomy, so the verdict is the load's and
    // never the caller's capacity.
    private static final int RETAIN_BYTES = 1 << 20;
    private static final int RETAIN_IDS = RETAIN_BYTES / 8;

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

    private static boolean accepted(String verdict) {
        return verdict.startsWith("accept ");
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

    // A throw inside a step after an accept is the case's answer: it names the
    // step and what was thrown.
    private interface Step {
        void run() throws ReflectiveOperationException;
    }

    private static String after(String verdict, String name, Step step) throws ReflectiveOperationException {
        if (!accepted(verdict)) {
            return verdict;
        }
        try {
            step.run();
        } catch (InvocationTargetException thrown) {
            return "after accept, " + name + " raised " + thrown.getCause();
        }
        return verdict;
    }

    private static final class Variable {
        final String pkg;
        final String table;
        final Class<?> codec;
        final Class<?> reportType;
        final Class<?> valueType;

        Variable(String pkg, String table) throws ReflectiveOperationException {
            this.pkg = pkg;
            this.table = table;
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

        byte[] toJson(Object value) throws ReflectiveOperationException {
            return (byte[]) codec.getMethod("toJson", valueType).invoke(null, value);
        }

        String digest(Object value) throws ReflectiveOperationException, NoSuchAlgorithmException {
            byte[] text = toJson(value);
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

        long loadMeasure(byte[] data) throws ReflectiveOperationException {
            return (long) codec.getMethod("loadMeasure", byte[].class).invoke(null, (Object) data);
        }

        byte[] save(Object value) throws ReflectiveOperationException {
            return (byte[]) codec.getMethod("save", valueType).invoke(null, value);
        }

        // Measure, Save and the text writer on an accepted value: the save's
        // bytes are measured again, and a null save (a value the writer
        // refuses) is a clean refusal.
        void exercise(Object value, byte[] input) throws ReflectiveOperationException {
            if (input != null) {
                loadMeasure(input);
            }
            byte[] saved = save(value);
            if (saved != null) {
                loadMeasure(saved);
            }
            toJson(value);
        }

        Object retain() throws ReflectiveOperationException {
            return Class.forName(pkg + ".TableRetain")
                    .getConstructor(int.class, int.class)
                    .newInstance(RETAIN_BYTES, RETAIN_IDS);
        }

        Class<?> retainCodec() throws ReflectiveOperationException {
            return Class.forName(pkg + "." + table + "Retain");
        }

        Object loadRetain(byte[] data, Object retain, Object report) throws ReflectiveOperationException {
            return retainCodec()
                    .getMethod("loadRetain", byte[].class, retain.getClass(), reportType)
                    .invoke(null, data, retain, report);
        }

        void exerciseRetain(Object value, Object retain) throws ReflectiveOperationException {
            Class<?> rc = retainCodec();
            rc.getMethod("measureRetain", valueType, retain.getClass()).invoke(null, value, retain);
            rc.getMethod("saveRetain", valueType, retain.getClass(), reportType).invoke(null, value, retain, report());
            toJson(value);
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
        String verdict = variable.digest(defaults);
        // Measure and Save the decoded batch back through the message writer.
        return after(verdict, "measureMessages/saveMessages", () -> {
            Object mr = vocabulary.report();
            int need = (int) msg.getMethod("measureMessages", values.getClass(), int.class, vocabulary.reportType)
                    .invoke(null, values, 1, mr);
            byte[] buf = new byte[Math.max(need, 0) + 64];
            msg.getMethod("saveMessages", values.getClass(), int.class, byte[].class, vocabulary.reportType)
                    .invoke(null, values, 1, buf, vocabulary.report());
        });
    }

    // ---- the packet carrier ----

    private static final Pattern PACKAGE_LINE = Pattern.compile("(?m)^\\s*package\\s+([A-Za-z_][A-Za-z0-9_]*)");

    private static final Map<String, ClassLoader> LOADERS = new HashMap<>();
    private static Map<String, String> packetUnits;

    private static synchronized ClassLoader packetLoader(Path packetDir, String schema)
            throws IOException {
        if (packetUnits == null) {
            packetUnits = new HashMap<>();
            Path units = packetDir.resolve("units.tsv");
            if (Files.exists(units)) {
                for (String[] f : rows(units, 2)) {
                    packetUnits.put(f[0], f[1]);
                }
            }
        }
        String unit = packetUnits.get(schema);
        if (unit == null) {
            throw new IllegalArgumentException("no Java packet unit for schema " + schema);
        }
        ClassLoader cl = LOADERS.get(unit);
        if (cl == null) {
            try {
                URL url = packetDir.resolve(unit).resolve("classes").toUri().toURL();
                cl = new URLClassLoader(new URL[] {url}, Runner.class.getClassLoader());
            } catch (MalformedURLException e) {
                throw new IOException(e);
            }
            LOADERS.put(unit, cl);
        }
        return cl;
    }

    private static String packet(Path packetDir, String schema, String type, byte[] payload)
            throws ReflectiveOperationException, NoSuchAlgorithmException, IOException {
        ClassLoader cl = packetLoader(packetDir, schema);
        Matcher m = PACKAGE_LINE.matcher(Files.readString(Path.of(schema), StandardCharsets.UTF_8));
        if (!m.find()) {
            throw new IllegalArgumentException(schema + " names no package");
        }
        String base = Path.of(schema).getFileName().toString().replaceFirst("\\.schema$", "");
        Class<?> file = Class.forName(m.group(1) + "." + base, true, cl);
        Class<?> valueType = Class.forName(m.group(1) + "." + base + "$" + type, true, cl);
        Object value = valueType.getConstructor().newInstance();
        try {
            file.getMethod("init" + type, valueType).invoke(null, value);
        } catch (NoSuchMethodException none) {
            // a type with no specified defaults constructs to its zero form
        }
        boolean ok = (boolean) file.getMethod("read" + type, valueType, byte[].class, int.class)
                .invoke(null, value, payload, payload.length * 8);
        if (!ok) {
            return refuse("packet_refused");
        }
        int maxBytes = -1;
        String want = type.toUpperCase(java.util.Locale.ROOT) + "MAXBYTES";
        for (java.lang.reflect.Field f : file.getFields()) {
            if (f.getName().endsWith("_MAX_BYTES") && f.getName().replace("_", "").equals(want)) {
                maxBytes = f.getInt(null);
            }
        }
        if (maxBytes < 0) {
            throw new IllegalArgumentException(base + " carries no MAX_BYTES for " + type);
        }
        byte[] out = new byte[((maxBytes + 7) / 8) * 8 + 8];
        int n = (int) file.getMethod("write" + type, valueType, byte[].class).invoke(null, value, out);
        return accept(sha256(Arrays.copyOf(out, n)));
    }

    private static String run(Path packetDir, String carrier, String schema, String table, byte[] payload)
            throws ReflectiveOperationException, NoSuchAlgorithmException, IOException {
        if (carrier.equals("packet")) {
            return packet(packetDir, schema, table, payload);
        }
        String pkg = pkg(schema);
        switch (carrier) {
            case "text":
            case "wire": {
                Variable codec = new Variable(pkg, table);
                Object report = codec.report();
                boolean text = carrier.equals("text");
                Object value = codec.call(text ? "fromJson" : "load", payload, report);
                String verdict = codec.verdict(value, report);
                return after(verdict, "loadMeasure/save/toJson", () -> codec.exercise(value, text ? null : payload));
            }
            case "wire_retain": {
                Variable codec = new Variable(pkg, table);
                Object report = codec.report();
                Object retain = codec.retain();
                Object value = codec.loadRetain(payload, retain, report);
                String verdict = codec.verdict(value, report);
                return after(verdict, "measureRetain/saveRetain/toJson", () -> codec.exerciseRetain(value, retain));
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

    // One case on its own thread: a fixed 512 KiB stack, the budget, and every
    // throw (a StackOverflowError or an OutOfMemoryError among them) caught as
    // the case's answer.
    private static String isolated(Path packetDir, String name, String carrier, String schema, String table,
            byte[] payload, long budgetMs) throws InterruptedException {
        String[] answer = new String[1];
        Runnable body = () -> {
            String got;
            try {
                got = run(packetDir, carrier, schema, table, payload);
            } catch (InvocationTargetException thrown) {
                got = "raised " + thrown.getCause();
            } catch (Throwable failure) {
                got = "runner error " + failure;
            }
            synchronized (answer) {
                answer[0] = got;
            }
        };
        Thread t = new Thread(null, body, "hostile-" + name, STACK_BYTES);
        t.setDaemon(true);
        t.start();
        t.join(budgetMs);
        synchronized (answer) {
            if (answer[0] != null) {
                return answer[0];
            }
        }
        return "over budget: still running after " + budgetMs + " ms";
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

    public static void main(String[] args) throws IOException, InterruptedException {
        if (args.length != 2 && args.length != 3) {
            System.err.println("usage: Runner <corpus-dir> <known-findings.tsv> [packet-dir]");
            System.exit(2);
        }
        Path corpus = Path.of(args[0]);
        Path findingsPath = Path.of(args[1]);
        Path packetDir = Path.of(args.length == 3 ? args[2] : "build/java-hostile/packet");
        long budgetMs = 5000;
        String budgetEnv = System.getenv("HOSTILE_CASE_BUDGET_MS");
        if (budgetEnv != null && !budgetEnv.isEmpty()) {
            budgetMs = Long.parseLong(budgetEnv);
        }

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
        java.util.Set<String> seen = new java.util.HashSet<>();
        for (String[] f : manifest) {
            String name = f[0];
            String carrier = f[1];
            seen.add(name);
            byte[] payload = Files.readAllBytes(corpus.resolve(f[4]));
            String expected = f[6].equals("accept") ? accept(f[8]) : refuse(f[7]);
            String got = isolated(packetDir, name, carrier, f[2], f[3], payload, budgetMs);
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
        for (String name : findings.keySet()) {
            if (!seen.contains(name)) {
                System.out.println("STALE " + name + ": FIX-CARD names no case the manifest carries");
                stale.add(name);
            }
        }
        System.out.println(
                manifest.size() + " cases: " + passed + " pass, " + carried + " carried as FIX-CARD, "
                        + bad.size() + " unexplained, " + stale.size() + " stale");
        System.exit(!bad.isEmpty() || !stale.isEmpty() ? 1 : 0);
    }
}
