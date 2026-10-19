import java.lang.reflect.Array;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.MessageDigest;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.List;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/** Runs the generated Java table reader against every shared hostile-corpus row. */
public final class HostileCorpus {
    private static final Pattern PACKAGE = Pattern.compile("(?m)^\\s*package\\s+([A-Za-z_][A-Za-z0-9_]*)\\s*$");
    private static final Pattern TABLE = Pattern.compile("(?m)^\\s*(?:fixed\\s+)?table\\s+([A-Za-z_][A-Za-z0-9_]*)\\b");

    private record Case(String name, String carrier, String schema, String table,
                        String input, String verdict, String reason, String digest) {}
    private record Result(boolean accepted, String reason, byte[] canonical) {}

    private HostileCorpus() {}

    public static void main(String[] args) throws Exception {
        if (args.length != 2) {
            throw new IllegalArgumentException("usage: HostileCorpus REPOSITORY_ROOT CORPUS_DIR");
        }
        Path root = Path.of(args[0]);
        Path corpus = Path.of(args[1]);
        List<Case> cases = readManifest(corpus.resolve("manifest.tsv"));
        int failures = 0;
        for (Case c : cases) {
            Result got;
            try {
                got = run(root, corpus, c);
            } catch (ReflectiveOperationException e) {
                throw new AssertionError(c.name + ": generated Java surface is incomplete", e);
            }
            String problem = compare(c, got);
            if (problem == null) {
                System.out.println("PASS " + c.name + "\t" + c.verdict + "\t" + c.reason + "\t" + c.digest);
            } else {
                failures++;
                System.err.println("FAIL " + c.name + ": " + problem);
            }
        }
        if (failures != 0) {
            throw new AssertionError(failures + " of " + cases.size() + " hostile-corpus cases disagreed");
        }
        System.out.println(cases.size() + " cases: the Java port agrees with the committed manifest");
    }

    private static List<Case> readManifest(Path path) throws Exception {
        List<Case> out = new ArrayList<>();
        for (String line : Files.readAllLines(path, StandardCharsets.UTF_8)) {
            if (line.isBlank() || line.startsWith("#")) continue;
            String[] f = line.split("\\t", -1);
            if (f.length != 9) throw new IllegalArgumentException("manifest row has " + f.length + " fields: " + line);
            out.add(new Case(f[0], f[1], f[2], f[3], f[4], f[6], f[7], f[8]));
        }
        if (out.isEmpty()) throw new IllegalArgumentException("empty hostile-corpus manifest");
        return out;
    }

    private static Result run(Path root, Path corpus, Case c) throws Exception {
        String pkg = schemaPackage(root.resolve(c.schema), c.table);
        byte[] input = Files.readAllBytes(corpus.resolve(c.input));
        return switch (c.carrier) {
            case "text" -> readValue(pkg, c.table, "fromJson", input);
            case "wire" -> readValue(pkg, c.table, "load", input);
            case "message", "message_novocab" -> readMessages(pkg, c.table, input, c.carrier.equals("message"));
            case "announce", "announce_second", "announce_bytes", "announce_entries" ->
                    readAnnouncement(pkg, input, c.carrier);
            default -> throw new IllegalArgumentException(c.name + ": unknown carrier " + c.carrier);
        };
    }

    private static Result readValue(String pkg, String table, String method, byte[] input) throws Exception {
        Class<?> surface = Class.forName(pkg + "." + table + "Variable");
        Class<?> reportType = Class.forName(pkg + ".TableVariable$Report");
        Object report = reportType.getConstructor().newInstance();
        Object value = surface.getMethod(method, byte[].class, reportType).invoke(null, input, report);
        Result status = reportResult(report, value != null);
        if (!status.accepted) return status;
        byte[] canonical = (byte[]) surface.getMethod("toJson", value.getClass()).invoke(null, value);
        if (canonical == null) return new Result(false, "damaged", null);
        return new Result(true, "-", canonical);
    }

    private static Result readMessages(String pkg, String table, byte[] input, boolean announce) throws Exception {
        Class<?> vocabType = Class.forName(pkg + ".TableVocabulary");
        Class<?> reportType = Class.forName(pkg + ".TableVocabulary$Report");
        Object vocab = vocabType.getConstructor().newInstance();
        vocabType.getMethod("reset", int.class).invoke(vocab, 4096);
        if (announce) {
            int n = (int) vocabType.getMethod("announceMeasure").invoke(null);
            byte[] bytes = new byte[n];
            vocabType.getMethod("announce", byte[].class).invoke(null, (Object) bytes);
            Object announceReport = reportType.getConstructor().newInstance();
            boolean ok = (boolean) vocabType.getMethod("announceRead", vocabType, byte[].class, reportType)
                    .invoke(null, vocab, bytes, announceReport);
            if (!ok) return reportResult(announceReport, false);
        }
        Class<?> message = Class.forName(pkg + "." + table + "Message");
        Class<?> value = Class.forName(pkg + "." + table + "Variable$Value");
        Object values = Array.newInstance(value, 1);
        Object report = reportType.getConstructor().newInstance();
        int count = (int) message.getMethod("loadMessages", values.getClass(), int.class,
                vocabType, byte[].class, reportType).invoke(null, values, 1, vocab, input, report);
        Result status = reportResult(report, count == 1 && Array.get(values, 0) != null);
        if (!status.accepted) return status;
        Object decoded = Array.get(values, 0);
        Class<?> variable = Class.forName(pkg + "." + table + "Variable");
        byte[] canonical = (byte[]) variable.getMethod("toJson", value).invoke(null, decoded);
        if (canonical == null) return new Result(false, "damaged", null);
        return new Result(true, "-", canonical);
    }

    private static Result readAnnouncement(String pkg, byte[] input, String carrier) throws Exception {
        Class<?> vocabType = Class.forName(pkg + ".TableVocabulary");
        Class<?> reportType = Class.forName(pkg + ".TableVocabulary$Report");
        Object vocab = vocabType.getConstructor().newInstance();
        int capacity = carrier.equals("announce_entries") ? 1 : 4096;
        vocabType.getMethod("reset", int.class).invoke(vocab, capacity);
        if (carrier.equals("announce_bytes")) field(vocabType, "maxBytes").setInt(vocab, 8);
        Method read = vocabType.getMethod("announceRead", vocabType, byte[].class, reportType);
        Object first = reportType.getConstructor().newInstance();
        boolean ok = (boolean) read.invoke(null, vocab, input, first);
        if (carrier.equals("announce_second")) {
            if (!ok) return reportResult(first, false);
            Object second = reportType.getConstructor().newInstance();
            read.invoke(null, vocab, input, second);
            return reportResult(second, false);
        }
        return reportResult(first, ok);
    }

    private static Result reportResult(Object report, boolean hasValue) throws Exception {
        Class<?> type = report.getClass();
        if (field(type, "refused").getBoolean(report)) {
            Object reason = field(type, "reason").get(report);
            return new Result(false, reason == null ? "damaged" : reason.toString(), null);
        }
        boolean damaged = field(type, "malformed").getBoolean(report);
        return damaged || !hasValue ? new Result(false, "damaged", null) : new Result(true, "-", null);
    }

    private static Field field(Class<?> type, String name) throws NoSuchFieldException {
        return type.getField(name);
    }

    private static String compare(Case c, Result got) throws Exception {
        if (c.verdict.equals("accept")) {
            if (!got.accepted) return "refused " + got.reason + ", want accept";
            if (c.digest.equals("-")) return null;
            String digest = hex(MessageDigest.getInstance("SHA-256").digest(got.canonical));
            return digest.equals(c.digest) ? null : "digest " + digest + ", want " + c.digest;
        }
        if (got.accepted) return "accepted, want refusal " + c.reason;
        return got.reason.equals(c.reason) ? null : "refused " + got.reason + ", want " + c.reason;
    }

    private static String schemaPackage(Path schema, String table) throws Exception {
        if (schema.endsWith("schemas/probe.schema")) return "probe";
        List<Path> candidates;
        if (Files.isDirectory(schema)) {
            try (var paths = Files.list(schema)) {
                candidates = paths.filter(p -> p.getFileName().toString().endsWith(".schema"))
                        .sorted(Comparator.comparing(Path::toString)).toList();
            }
        } else {
            candidates = List.of(schema);
        }
        String fallback = null;
        for (Path path : candidates) {
            String text = Files.readString(path, StandardCharsets.UTF_8);
            Matcher pm = PACKAGE.matcher(text);
            if (!pm.find()) continue;
            if (fallback == null) fallback = pm.group(1);
            Matcher tm = TABLE.matcher(text);
            while (tm.find()) if (tm.group(1).equals(table)) return pm.group(1);
        }
        if (fallback != null) return fallback;
        throw new IllegalArgumentException("cannot find package for " + table + " in " + schema);
    }

    private static String hex(byte[] bytes) {
        StringBuilder out = new StringBuilder(bytes.length * 2);
        for (byte b : bytes) out.append(String.format("%02x", b & 0xff));
        return out.toString();
    }
}
