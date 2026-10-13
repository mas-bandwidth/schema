// The owning card facade (docs/SPEC-TABLES.md §2, §3, §6.2): the Java port of
// the Go reference leg tables/cards/cards.go, parse.go and render.go. It wraps
// the generated variable-class unit (cards.CardVariable) with a Record that
// keeps its own wire copy, the customer bounds MaxBodyBytes/MaxListEntries
// (cards.go), and the admitted brief text parsed once (parse.go) and rendered
// many times (render.go). Generated views never escape: a Record holds the
// resolved Value plus the caller's bytes copied.
package cards;

import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.HashSet;
import java.util.List;
import java.util.Set;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

public final class Cards {
    private Cards() {}

    // MaxBodyBytes is the customer ceiling on one card's opaque body; the schema
    // declares body *string with no bound (docs/SPEC-TABLES.md §2.5). cards.go.
    public static final int MaxBodyBytes = 64 << 10;
    // MaxListEntries is the customer ceiling on each collection field
    // (docs/SPEC-TABLES.md §2.9). cards.go.
    public static final int MaxListEntries = 512;

    // Refusal is one class of the closed list FORMAT.md fixes, never free text.
    public static final class Refusal extends Exception {
        public final String cls;
        public Refusal(String cls) { super(cls); this.cls = cls; }
    }

    // Record owns a private copy of the wire and the resolved variable value.
    public static final class Record {
        private final byte[] wire;
        final CardVariable.Value v;
        final TableVariable.Report report;

        Record(byte[] wire, CardVariable.Value v, TableVariable.Report report) {
            this.wire = wire;
            this.v = v;
            this.report = report;
        }

        // Wire returns a defensive copy of the record's wire bytes. cards.go.
        public byte[] wire() {
            return Arrays.copyOf(wire, wire.length);
        }

        // body returns the opaque body text, or null when absent.
        public String body() {
            return v.body;
        }
    }

    // Load reads wire into a Record through the generated variable load
    // (docs/SPEC-TABLES.md §6.2). Malformed wire is a refusal; the customer
    // bounds are checked before any Record is published. cards.go Load.
    public static Record load(byte[] wire) throws Refusal {
        TableVariable.Report report = new TableVariable.Report();
        CardVariable.Value v = CardVariable.load(wire, report);
        if (v == null || report.malformed) {
            throw new Refusal("malformed");
        }
        checkValue(v);
        return new Record(Arrays.copyOf(wire, wire.length), v, report);
    }

    // FromJSON builds a Record from JSON text through the generated fromJson and
    // save (docs/SPEC-TABLES.md §16), then the customer bounds; loss events are
    // refused, never truncated. cards.go FromJSON.
    public static Record fromJSON(byte[] text) throws Refusal {
        TableVariable.Report report = new TableVariable.Report();
        CardVariable.Value v = CardVariable.fromJson(normalizeEnumNone(text), report);
        if (report.malformed) {
            throw new Refusal("malformed");
        }
        if (report.unknown > 0 || report.kindMismatch > 0 || report.clamped > 0) {
            throw new Refusal("unknown-present");
        }
        checkValue(v);
        byte[] wire = CardVariable.save(v);
        if (wire == null) {
            throw new Refusal("malformed");
        }
        return new Record(wire, v, report);
    }

    // normalizeEnumNone drops the enum "None" spelling before the generated
    // fromJson reads the text. The generated reader (internal/codegen/javatable)
    // omits the None case, so "None" is counted unknown and the facade would
    // refuse a lossless read. None is the zero value (docs/SPEC-TABLES.md §16),
    // so removing the field is equivalent: the reader leaves it at its default.
    // cards.go FromJSON, which reads "None" through the Go enum's None const.
    static byte[] normalizeEnumNone(byte[] text) {
        return ENUM_NONE.matcher(new String(text, StandardCharsets.UTF_8))
            .replaceAll("").getBytes(StandardCharsets.UTF_8);
    }

    // ToJSON writes the record's known-field JSON text (docs/SPEC-TABLES.md §16).
    // cards.go ToJSON.
    public static byte[] toJSON(Record record) throws Refusal {
        if (record == null || record.v == null) {
            throw new Refusal("malformed");
        }
        byte[] json = CardVariable.toJson(record.v);
        if (json == null) {
            throw new Refusal("malformed");
        }
        return json;
    }

    // checkValue enforces the customer bounds before any write publishes: the
    // body byte ceiling and the collection-entry ceiling, in cards.go checkCard
    // order (body, paths, _new, shared, needs, extra, consumers).
    static void checkValue(CardVariable.Value v) throws Refusal {
        if (v.body != null && byteLen(v.body) > MaxBodyBytes) {
            throw new Refusal("body-bound " + MaxBodyBytes);
        }
        checkList(v.paths.size(), "paths");
        checkList(v.new_.size(), "_new");
        checkList(v.shared.size(), "shared");
        checkList(v.needs.size(), "needs");
        checkList(v.extra.size(), "extra");
        checkList(v.consumers.size(), "consumers");
    }

    private static void checkList(int n, String field) throws Refusal {
        if (n < 0 || n > MaxListEntries) {
            throw new Refusal("list-bound " + field + " " + MaxListEntries);
        }
    }

    private static int byteLen(String s) {
        return s.getBytes(StandardCharsets.UTF_8).length;
    }

    // ---------------------------------------------------------------------
    // parse.go — the admitted brief text, parsed once into a typed Record.
    // ---------------------------------------------------------------------

    // headerLineRE is the admitted header grammar (parse.go).
    private static final Pattern HEADER_LINE =
        Pattern.compile("^([A-Za-z][A-Za-z0-9-]*):\\s*(.*)$");
    // deadlineRE is the customer spelling DEADLINE accepts (parse.go).
    private static final Pattern DEADLINE =
        Pattern.compile("^finish within ([0-9]+) minutes\\.$");
    // enumNoneRE is the enum "None" spelling the generated fromJson omits
    // (internal/codegen/javatable); normalizeEnumNone drops it (cards.go FromJSON).
    private static final Pattern ENUM_NONE =
        Pattern.compile("\"(?:tier|priority|column)\"\\s*:\\s*\"None\"\\s*,?");

    // strandedKeys is the exact case-sensitive set refused once prose has begun
    // (parse.go, nova-sprint cardHeaderKeys).
    private static final Set<String> STRANDED = new HashSet<>(Arrays.asList(
        "KIND", "PATHS", "TEST", "LEGS", "SOURCE", "START", "STOP", "DEPENDS-ON",
        "PRIORITY", "NEW", "SHARED", "REPO", "BASE", "tier", "RESULT"));

    // reservedHeaderKeys is the mapped header vocabulary (parse.go).
    private static final Set<String> RESERVED = new HashSet<>(Arrays.asList(
        "RESULT", "tier", "REPO", "BASE", "KIND", "PRIORITY", "PATHS", "NEW",
        "SHARED", "DEPENDS-ON", "TEST", "DONE-WHEN", "DEADLINE", "SOURCE"));

    // Parse reads admitted brief text and publishes a Record only on complete
    // checked success (docs/SPEC-TABLES.md §2, §6.2). parse.go Parse.
    public static Record parse(byte[] text) throws Refusal {
        if (!utf8Valid(text) || indexOf(text, (byte) 0) >= 0) {
            throw new Refusal("malformed");
        }
        List<Line> lines = splitLines(text);
        if (lines.isEmpty()) {
            throw new Refusal("malformed");
        }
        String[] kv = matchHeader(lines.get(0).content);
        if (kv == null || !kv[0].equals("RESULT")) {
            throw new Refusal("malformed");
        }
        Parsed p = new Parsed();
        applyResult(p, kv[1]);
        int lastHeaderEnd = lines.get(0).termEnd;
        Set<String> seen = new HashSet<>();
        seen.add("RESULT");
        boolean prose = false;
        for (int i = 1; i < lines.size(); i++) {
            Line ln = lines.get(i);
            if (isBlank(ln.content)) {
                continue;
            }
            String[] h = matchHeader(ln.content);
            if (h == null) {
                prose = true;
                continue;
            }
            if (prose) {
                if (STRANDED.contains(h[0])) {
                    throw new Refusal("stranded-key " + h[0]);
                }
                continue;
            }
            applyHeader(p, h[0], h[1], seen);
            lastHeaderEnd = ln.termEnd;
        }
        byte[] body = Arrays.copyOfRange(text, lastHeaderEnd, text.length);
        return build(p, body);
    }

    static final class Line {
        final byte[] content;
        final int termEnd;
        Line(byte[] content, int termEnd) { this.content = content; this.termEnd = termEnd; }
    }

    // splitLines returns every physical line with content and terminator end
    // offset; a trailing LF does not invent a final empty line (parse.go).
    static List<Line> splitLines(byte[] text) {
        List<Line> out = new ArrayList<>();
        int i = 0;
        while (i < text.length) {
            int start = i;
            int j = start;
            while (j < text.length && text[j] != '\n') {
                j++;
            }
            int lineEnd = j < text.length ? j : text.length;
            int termEnd = j < text.length ? lineEnd + 1 : text.length;
            int ce = lineEnd;
            if (ce > start && text[ce - 1] == '\r') {
                ce--;
            }
            out.add(new Line(Arrays.copyOfRange(text, start, ce), termEnd));
            i = termEnd;
        }
        return out;
    }

    // matchHeader recognizes a header key line; null for prose or a line with
    // leading whitespace (parse.go matchHeader).
    static String[] matchHeader(byte[] line) {
        Matcher m = HEADER_LINE.matcher(new String(line, StandardCharsets.UTF_8));
        if (!m.matches()) {
            return null;
        }
        return new String[] { m.group(1), m.group(2) };
    }

    static final class Parsed {
        String id = "";
        String sha = "";
        int tier = 0;
        String repo = "";
        String base = "";
        String kind = "";
        int priority = 0;
        String test = "";
        String doneWhen = "";
        String source = "";
        int deadline = 0;
        List<String> paths = new ArrayList<>();
        List<String> news = new ArrayList<>();
        List<String> shared = new ArrayList<>();
        List<String> needs = new ArrayList<>();
        List<Kv> extras = new ArrayList<>();
    }

    static final class Kv {
        final String key;
        final String value;
        Kv(String key, String value) { this.key = key; this.value = value; }
    }

    // applyResult parses "id sha=<sha> tier: <tier>" (parse.go applyResult).
    static void applyResult(Parsed p, String value) throws Refusal {
        int i = value.lastIndexOf(" tier:");
        if (i < 0) {
            throw new Refusal("malformed");
        }
        String tierStr = value.substring(i + 6).trim();
        String rest = value.substring(0, i);
        int j = rest.lastIndexOf(" sha=");
        if (j < 0) {
            throw new Refusal("malformed");
        }
        String idStr = rest.substring(0, j).trim();
        String shaStr = rest.substring(j + 5);
        if (idStr.length() > 64) {
            throw new Refusal("malformed");
        }
        if (!isHex(shaStr) || (shaStr.length() != 12 && shaStr.length() != 40)) {
            throw new Refusal("malformed");
        }
        p.id = idStr;
        p.sha = shaStr;
        p.tier = parseTier(tierStr);
    }

    // applyHeader dispatches one header line to its field, an agreeing tier or
    // an extra, refusing duplicates and CR/LF (parse.go applyHeader).
    static void applyHeader(Parsed p, String key, String value, Set<String> seen) throws Refusal {
        if (value.indexOf('\r') >= 0 || value.indexOf('\n') >= 0) {
            throw new Refusal("malformed");
        }
        if (key.equals("tier")) {
            if (seen.contains("tier")) {
                throw new Refusal("malformed");
            }
            seen.add("tier");
            int t = parseTier(value.trim());
            if (t != p.tier) {
                throw new Refusal("malformed");
            }
            return;
        }
        if (key.equals("RESULT")) {
            throw new Refusal("malformed");
        }
        if (RESERVED.contains(key)) {
            if (seen.contains(key)) {
                throw new Refusal("malformed");
            }
            seen.add(key);
            assignMapped(p, key, value);
            return;
        }
        if (key.length() > 32) {
            throw new Refusal("malformed");
        }
        p.extras.add(new Kv(key, value));
    }

    static void assignMapped(Parsed p, String key, String value) throws Refusal {
        switch (key) {
            case "REPO":
                p.repo = bound(value, 96); break;
            case "BASE":
                p.base = bound(value, 96); break;
            case "KIND":
                p.kind = bound(value, 24); break;
            case "PRIORITY":
                p.priority = parsePriority(value.trim()); break;
            case "PATHS":
                p.paths = splitListChecked(value, 192, true); break;
            case "NEW":
                p.news = splitListChecked(value, 192, false); break;
            case "SHARED":
                p.shared = splitListChecked(value, 192, false); break;
            case "DEPENDS-ON":
                p.needs = splitListChecked(value, 64, false); break;
            case "TEST":
                p.test = bound(value, 192); break;
            case "DONE-WHEN":
                p.doneWhen = value; break;
            case "DEADLINE":
                p.deadline = parseDeadline(value); break;
            case "SOURCE":
                p.source = value; break;
            default:
                break;
        }
    }

    static String bound(String value, int max) throws Refusal {
        if (value.length() > max) {
            throw new Refusal("malformed");
        }
        return value;
    }

    // splitListChecked splits a comma list, trims elements, drops empty ones,
    // and enforces the element bound and the entry ceiling (parse.go).
    static List<String> splitListChecked(String value, int bound, boolean noneSentinel) throws Refusal {
        String v = value.trim();
        List<String> out = new ArrayList<>();
        if (v.isEmpty() || v.equals("-")) {
            return out;
        }
        if (noneSentinel && v.equals("none")) {
            return out;
        }
        for (String part : v.split(",", -1)) {
            String t = part.trim();
            if (t.isEmpty()) {
                continue;
            }
            if (t.length() > bound) {
                throw new Refusal("malformed");
            }
            out.add(t);
        }
        if (out.size() > MaxListEntries) {
            throw new Refusal("malformed");
        }
        return out;
    }

    static int parseTier(String s) throws Refusal {
        switch (s) {
            case "flash": return 1;
            case "pro": return 2;
            case "heavy": return 3;
            case "frontier": return 4;
            default: throw new Refusal("malformed");
        }
    }

    static int parsePriority(String s) throws Refusal {
        switch (s) {
            case "normal": return 1;
            case "high": return 2;
            case "critical": return 3;
            case "blocker": return 4;
            default: throw new Refusal("malformed");
        }
    }

    static int parseDeadline(String s) throws Refusal {
        Matcher m = DEADLINE.matcher(s.trim());
        if (!m.matches()) {
            throw new Refusal("malformed");
        }
        long n = Long.parseLong(m.group(1));
        if (n < 0 || n > 100000) {
            throw new Refusal("malformed");
        }
        return (int) n;
    }

    static boolean isHex(String s) {
        if (s.isEmpty()) {
            return false;
        }
        for (int i = 0; i < s.length(); i++) {
            char c = s.charAt(i);
            if (!((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F'))) {
                return false;
            }
        }
        return true;
    }

    static boolean isBlank(byte[] line) {
        for (byte b : line) {
            if (b != ' ' && b != '\t' && b != '\r') {
                return false;
            }
        }
        return true;
    }

    // build publishes the parsed card through the generated save (parse.go build).
    static Record build(Parsed p, byte[] body) throws Refusal {
        CardVariable.Value v = new CardVariable.Value();
        v.id = p.id;
        v.sha = p.sha;
        v.tier = p.tier;
        v.repo = p.repo;
        v.base = p.base;
        v.kind = p.kind;
        v.priority = p.priority;
        v.test = p.test;
        if (!p.doneWhen.isEmpty()) {
            v.done = p.doneWhen;
        }
        v.deadlineMinutes = p.deadline;
        if (!p.source.isEmpty()) {
            v.source = p.source;
        }
        for (String e : p.paths) {
            PathTextVariable.Value row = new PathTextVariable.Value();
            row.s = e;
            v.paths.add(row);
        }
        for (String e : p.news) {
            PathTextVariable.Value row = new PathTextVariable.Value();
            row.s = e;
            v.new_.add(row);
        }
        for (String e : p.shared) {
            PathTextVariable.Value row = new PathTextVariable.Value();
            row.s = e;
            v.shared.add(row);
        }
        for (String e : p.needs) {
            CardIDVariable.Value row = new CardIDVariable.Value();
            row.s = e;
            v.needs.add(row);
        }
        for (Kv e : p.extras) {
            KeyValueVariable.Value row = new KeyValueVariable.Value();
            row.key = e.key;
            row.value = e.value;
            v.extra.add(row);
        }
        if (body.length > 0) {
            v.body = new String(body, StandardCharsets.UTF_8);
        }
        checkValue(v);
        byte[] wire = CardVariable.save(v);
        if (wire == null) {
            throw new Refusal("malformed");
        }
        return new Record(wire, v, new TableVariable.Report());
    }

    // ---------------------------------------------------------------------
    // render.go — the informational text view of a typed card.
    // ---------------------------------------------------------------------

    // Render returns the record's informational text (docs/SPEC-TABLES.md §6.2,
    // §16). render.go Render.
    public static byte[] render(Record record) throws Refusal {
        if (record == null || record.v == null) {
            throw new Refusal("malformed");
        }
        CardVariable.Value v = record.v;
        StringBuilder out = new StringBuilder();
        out.append("RESULT: ").append(v.id).append(" sha=").append(v.sha)
           .append(" tier: ").append(tierString(v.tier)).append('\n');
        emitStr(out, "REPO", v.repo);
        emitStr(out, "BASE", v.base);
        emitStr(out, "KIND", v.kind);
        String prio = priorityString(v.priority);
        if (!prio.isEmpty()) {
            out.append("PRIORITY: ").append(prio).append('\n');
        }
        emitList(out, "PATHS", collectPaths(v.paths));
        emitList(out, "NEW", collectPaths(v.new_));
        emitList(out, "SHARED", collectPaths(v.shared));
        emitList(out, "DEPENDS-ON", collectIDs(v.needs));
        emitStr(out, "TEST", v.test);
        if (v.done != null) {
            out.append("DONE-WHEN: ").append(v.done).append('\n');
        }
        if (v.deadlineMinutes != 0) {
            out.append("DEADLINE: finish within ").append(v.deadlineMinutes).append(" minutes.\n");
        }
        if (v.source != null) {
            out.append("SOURCE: ").append(v.source).append('\n');
        }
        for (KeyValueVariable.Value e : v.extra) {
            out.append(e.key).append(": ");
            if (e.value != null) {
                out.append(e.value);
            }
            out.append('\n');
        }
        if (v.body != null) {
            out.append(v.body);
        }
        return out.toString().getBytes(StandardCharsets.UTF_8);
    }

    static String tierString(int t) throws Refusal {
        switch (t) {
            case 1: return "flash";
            case 2: return "pro";
            case 3: return "heavy";
            case 4: return "frontier";
            default: throw new Refusal("malformed");
        }
    }

    static String priorityString(int p) throws Refusal {
        switch (p) {
            case 0: return "";
            case 1: return "normal";
            case 2: return "high";
            case 3: return "critical";
            case 4: return "blocker";
            default: throw new Refusal("malformed");
        }
    }

    static void emitStr(StringBuilder out, String key, String value) {
        if (value == null || value.isEmpty()) {
            return;
        }
        out.append(key).append(": ").append(value).append('\n');
    }

    static void emitList(StringBuilder out, String key, List<String> elems) {
        if (elems.isEmpty()) {
            return;
        }
        out.append(key).append(": ");
        for (int i = 0; i < elems.size(); i++) {
            if (i > 0) {
                out.append(", ");
            }
            out.append(elems.get(i));
        }
        out.append('\n');
    }

    static List<String> collectPaths(List<PathTextVariable.Value> list) {
        List<String> out = new ArrayList<>(list.size());
        for (PathTextVariable.Value e : list) {
            out.add(e.s);
        }
        return out;
    }

    static List<String> collectIDs(List<CardIDVariable.Value> list) {
        List<String> out = new ArrayList<>(list.size());
        for (CardIDVariable.Value e : list) {
            out.add(e.s);
        }
        return out;
    }

    static boolean utf8Valid(byte[] b) {
        try {
            java.nio.charset.CharsetDecoder dec =
                StandardCharsets.UTF_8.newDecoder();
            dec.onMalformedInput(java.nio.charset.CodingErrorAction.REPORT);
            dec.onUnmappableCharacter(java.nio.charset.CodingErrorAction.REPORT);
            dec.decode(java.nio.ByteBuffer.wrap(b));
            return true;
        } catch (java.nio.charset.CharacterCodingException e) {
            return false;
        }
    }

    static int indexOf(byte[] b, byte needle) {
        for (int i = 0; i < b.length; i++) {
            if (b[i] == needle) {
                return i;
            }
        }
        return -1;
    }
}
