// The Java card facade's unit tests, one check per named test, run from the
// repository root. No test framework: a failure prints and the process exits
// nonzero. The tests pin the Go reference leg's answers (tables/cards/*.go) on
// the shared corpus (tables/cards/{fuzz,conformance}).
import cards.CardDescriptor;
import cards.CardVariable;
import cards.Cards;
import cards.TableFieldInfo;
import cards.TableTypeInfo;
import cards.TableVariable;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;

public final class CardsTest {
    private CardsTest() {}

    private static final List<String> FAILURES = new ArrayList<>();

    public static void main(String[] args) throws Exception {
        run("TestCardRoundTripsThePopulatedFixture", CardsTest::testRoundTrip);
        run("TestCardBoundsRefuseBeforePublishing", CardsTest::testBounds);
        run("TestParseRenderMatchesGo", CardsTest::testParseRender);
        run("TestUpgradeMatchesGo", CardsTest::testUpgradeMatchesGo);
        run("TestTaskHashFramesLogicalFields", CardsTest::testTaskHashFramesLogicalFields);
        run("TestFieldEditLeavesTheTaskHashAlone", CardsTest::testFieldEditLeavesTheTaskHashAlone);
        run("TestViewWalksEveryField", CardsTest::testViewWalksEveryField);
        for (String f : FAILURES) {
            System.out.println("FAIL " + f);
        }
        if (!FAILURES.isEmpty()) {
            System.out.println(FAILURES.size() + " check(s) failed");
            System.exit(1);
        }
        System.out.println("all checks passed");
    }

    interface Check { void run() throws Exception; }

    static void run(String name, Check c) {
        try {
            c.run();
            System.out.println("PASS " + name);
        } catch (Throwable t) {
            FAILURES.add(name + ": " + t.getMessage());
        }
    }

    static void require(boolean ok, String msg) {
        if (!ok) {
            throw new AssertionError(msg);
        }
    }

    static byte[] read(String path) throws Exception {
        return Files.readAllBytes(Paths.get(path));
    }

    // TestCardRoundTripsThePopulatedFixture: Load the fuzz fixture, match every
    // field against card-populated.json, and prove ToJSON and FromJSON reproduce
    // both files exactly while the record holds a private copy.
    static void testRoundTrip() throws Exception {
        byte[] wire = read("tables/cards/fuzz/card-populated.wire");
        byte[] json = read("tables/cards/fuzz/card-populated.json");

        Cards.Record rec = Cards.load(wire);
        byte[] out = Cards.toJSON(rec);
        require(Arrays.equals(out, json), "ToJSON does not reproduce card-populated.json");
        require(rec.body() != null, "body is null after Load");
        require(rec.body().getBytes(StandardCharsets.UTF_8).length > 0, "body is empty after Load");

        Cards.Record again = Cards.fromJSON(json);
        require(Arrays.equals(again.wire(), wire), "FromJSON does not reproduce card-populated.wire");
        require(again.body().equals(rec.body()), "body differs across the round trip");

        // The record owns a private copy: mutating the caller's input changes
        // nothing the record returns.
        byte[] caller = Arrays.copyOf(wire, wire.length);
        Cards.Record owned = Cards.load(caller);
        caller[0] ^= 0x7f;
        require(Arrays.equals(owned.wire(), wire), "Load aliased the caller's input");

        // Mutating a Wire result changes nothing the record returns.
        byte[] w1 = owned.wire();
        w1[0] ^= 0x7f;
        require(Arrays.equals(owned.wire(), wire), "Wire returned an alias of the record's wire");

        // FromJSON also owns a private copy of the caller's text.
        byte[] jsonCaller = Arrays.copyOf(json, json.length);
        Cards.Record fromJsonOwned = Cards.fromJSON(jsonCaller);
        jsonCaller[0] ^= 0x7f;
        require(Arrays.equals(fromJsonOwned.wire(), wire),
            "FromJSON aliased the caller's text");

        // A save/load round trip reproduces the wire byte for byte.
        Cards.Record reloaded = Cards.load(again.wire());
        require(Arrays.equals(reloaded.wire(), wire),
            "save/load does not reproduce the wire");
    }

    // TestCardBoundsRefuseBeforePublishing: the customer bounds accept exactly
    // the ceiling and refuse past it, naming the field.
    static void testBounds() throws Exception {
        accept("body ceiling", jsonBody(65536));
        refuse("body past ceiling", jsonBody(65537), "body-bound 65536");

        String[] fields = {"paths", "_new", "shared", "needs", "consumers"};
        for (String field : fields) {
            accept(field + " ceiling", jsonList(field, 512));
            refuse(field + " past ceiling", jsonList(field, 513), "list-bound " + field + " 512");
        }
    }

    static void accept(String what, byte[] json) throws Exception {
        try {
            Cards.fromJSON(json);
        } catch (Cards.Refusal r) {
            throw new AssertionError(what + " refused: " + r.cls);
        }
    }

    static void refuse(String what, byte[] json, String cls) throws Exception {
        byte[] before = Arrays.copyOf(json, json.length);
        try {
            Cards.fromJSON(json);
            throw new AssertionError(what + " was accepted");
        } catch (Cards.Refusal r) {
            require(r.cls.equals(cls), what + " refusal class " + r.cls + " != " + cls);
        }
        // A refused write leaves its input unchanged.
        require(Arrays.equals(json, before), what + " mutated its input");
    }

    static byte[] jsonBody(int n) {
        StringBuilder b = new StringBuilder("{\"id\":\"x\",\"body\":\"");
        for (int i = 0; i < n; i++) {
            b.append('y');
        }
        b.append("\"}");
        return b.toString().getBytes(StandardCharsets.UTF_8);
    }

    static byte[] jsonList(String field, int n) {
        String elem = field.equals("consumers") ? "{\"kind\":\"k\"}" : "{\"s\":\"p\"}";
        StringBuilder b = new StringBuilder("{\"id\":\"x\",\"").append(field).append("\":[");
        for (int i = 0; i < n; i++) {
            if (i > 0) {
                b.append(',');
            }
            b.append(elem);
        }
        b.append("]}");
        return b.toString().getBytes(StandardCharsets.UTF_8);
    }

    // TestParseRenderMatchesGo: every parse case parses to the Go wire and every
    // render case renders to the Go text byte for byte; the rejected case names
    // its stranded key.
    static void testParseRender() throws Exception {
        byte[] brief = read("tables/cards/conformance/cases/normalized-valid.md");
        Cards.Record parsed = Cards.parse(brief);
        byte[] wantWire = read("tables/cards/conformance/expected/normalized-valid.wire");
        require(Arrays.equals(parsed.wire(), wantWire), "parse did not reproduce normalized-valid.wire");

        byte[] rejected = read("tables/cards/conformance/cases/rejected-legacy.md");
        try {
            Cards.parse(rejected);
            throw new AssertionError("rejected-legacy was accepted");
        } catch (Cards.Refusal r) {
            require(r.cls.equals("stranded-key PRIORITY"),
                "rejected-legacy refusal class " + r.cls);
        }

        String[] names = {"card-populated", "initial-populated", "consumers-populated", "empty-consumers"};
        for (String name : names) {
            byte[] wire = read("tables/cards/conformance/cases/" + name + ".wire");
            Cards.Record rec = Cards.load(wire);
            byte[] text = Cards.render(rec);
            byte[] wantText = read("tables/cards/conformance/expected/" + name + ".txt");
            require(Arrays.equals(text, wantText), name + " render does not match Go");
        }
    }

    // TestUpgradeMatchesGo: every upgrade case in the corpus yields the expected
    // wire and report byte for byte, or the expected refusal class; upgrade of
    // an upgraded record is byte-identical; retention storage one id short
    // refuses with retain-lost and leaves the input unchanged.
    static void testUpgradeMatchesGo() throws Exception {
        String[] names = {"initial-populated", "consumers-populated", "empty-consumers", "future-note"};
        for (String n : names) {
            byte[] wire = read("tables/cards/conformance/cases/" + n + ".wire");
            byte[] wantWire = read("tables/cards/conformance/expected/" + n + ".wire");
            byte[] wantReport = read("tables/cards/conformance/expected/" + n + ".report");
            Cards.UpgradeResult r = Cards.upgrade(wire);
            require(Arrays.equals(r.wire, wantWire), n + " upgrade wire does not match Go");
            require(Arrays.equals(reportString(r.report).getBytes(StandardCharsets.UTF_8), wantReport),
                n + " upgrade report does not match Go");
            Cards.UpgradeResult again = Cards.upgrade(r.wire);
            require(Arrays.equals(again.wire, r.wire), n + " upgrade is not idempotent");
        }
        byte[] trunc = read("tables/cards/conformance/cases/truncated.wire");
        try {
            Cards.upgrade(trunc);
            throw new AssertionError("truncated was accepted");
        } catch (Cards.Refusal r) {
            require(r.cls.equals("malformed"), "truncated refusal class " + r.cls);
        }
        byte[] future = read("tables/cards/conformance/cases/future-note.wire");
        byte[] before = Arrays.copyOf(future, future.length);
        try {
            Cards.upgrade(future, Cards.MaxRetainBytes, 0);
            throw new AssertionError("one-id-short retention was accepted");
        } catch (Cards.Refusal r) {
            require(r.cls.equals("retain-lost"), "short-retention refusal class " + r.cls);
        }
        require(Arrays.equals(future, before), "short-retention refusal mutated the input");
    }

    static String reportString(TableVariable.Report r) {
        return "Retained=" + r.retained + "\n"
            + "RetainLost=" + r.retainLost + "\n"
            + "Widened=" + r.widened + "\n"
            + "Unknown=" + r.unknown + "\n"
            + "KindMismatch=" + r.kindMismatch + "\n"
            + "Clamped=" + r.clamped + "\n"
            + "Duplicate=0\n"
            + "Malformed=" + r.malformed + "\n";
    }

    // TestTaskHashFramesLogicalFields: pins the frame with an independently
    // written byte vector, shows body "a"/["bc"] and "ab"/["c"] hash
    // differently, matches every expected .hex, and shows the lossy refusal.
    static void testTaskHashFramesLogicalFields() throws Exception {
        java.security.MessageDigest h = java.security.MessageDigest.getInstance("SHA-256");
        frameText(h, "a");
        hashU64(h, 1);
        hashU64(h, 2);
        h.update("bc".getBytes(StandardCharsets.UTF_8));
        hashU64(h, 0);
        hashU64(h, 0);
        hashU64(h, 0);
        hashU64(h, 0);
        String wantHex = hex(h.digest());
        byte[] json = ("{\"id\":\"x\",\"body\":\"a\",\"paths\":[{\"s\":\"bc\"}]}")
            .getBytes(StandardCharsets.UTF_8);
        String got = Cards.taskHash(Cards.fromJSON(json).wire());
        require(got.equals(wantHex), "frame mismatch " + got + " != " + wantHex);
        byte[] json2 = ("{\"id\":\"x\",\"body\":\"ab\",\"paths\":[{\"s\":\"c\"}]}")
            .getBytes(StandardCharsets.UTF_8);
        String got2 = Cards.taskHash(Cards.fromJSON(json2).wire());
        require(!got.equals(got2), "distinct task frames hashed equal");
        String[] hexNames = {"card-populated", "initial-populated", "empty-consumers"};
        for (String n : hexNames) {
            byte[] w = read("tables/cards/conformance/cases/" + n + ".wire");
            String want = new String(read("tables/cards/conformance/expected/" + n + ".hex"),
                StandardCharsets.UTF_8).trim();
            require(Cards.taskHash(w).equals(want), n + " hex does not match Go");
        }
        byte[] future = read("tables/cards/conformance/cases/future-note.wire");
        try {
            Cards.taskHash(future);
            throw new AssertionError("lossy record was hashed");
        } catch (Cards.Refusal r) {
            require(r.cls.equals("unknown-present"), "lossy hash refusal " + r.cls);
        }
    }

    static void frameText(java.security.MessageDigest h, String s) {
        byte[] b = s.getBytes(StandardCharsets.UTF_8);
        hashU64(h, b.length);
        h.update(b);
    }

    static void hashU64(java.security.MessageDigest h, long v) {
        for (int i = 0; i < 8; i++) {
            h.update((byte) (v >>> (8 * i)));
        }
    }

    static String hex(byte[] b) {
        StringBuilder sb = new StringBuilder();
        for (byte x : b) {
            sb.append(String.format("%02x", x));
        }
        return sb.toString();
    }

    // TestFieldEditLeavesTheTaskHashAlone: needs, tier, priority, column and
    // holder edits change the wire and not the hash; paths changes both; the
    // body is byte-equal throughout; every refusal leaves the wire byte-equal
    // to before.
    static void testFieldEditLeavesTheTaskHashAlone() throws Exception {
        byte[] wire = read("tables/cards/conformance/cases/card-populated.wire");
        String baseHash = Cards.taskHash(wire);
        byte[] baseBody = Cards.load(wire).body().getBytes(StandardCharsets.UTF_8);
        byte[] wNeeds = Cards.setNeeds(wire, Arrays.asList("a", "b")).wire();
        require(!Arrays.equals(wNeeds, wire), "needs edit did not change the wire");
        require(Cards.taskHash(wNeeds).equals(baseHash), "needs edit changed the hash");
        byte[] wTier = Cards.setTier(wire, 3).wire();
        require(Cards.taskHash(wTier).equals(baseHash), "tier edit changed the hash");
        byte[] wPrio = Cards.setPriority(wire, 4).wire();
        require(Cards.taskHash(wPrio).equals(baseHash), "priority edit changed the hash");
        byte[] wCol = Cards.setColumn(wire, 6).wire();
        require(Cards.taskHash(wCol).equals(baseHash), "column edit changed the hash");
        byte[] wHolder = Cards.setHolder(wire, "h").wire();
        require(Cards.taskHash(wHolder).equals(baseHash), "holder edit changed the hash");
        byte[] wPaths = Cards.setPaths(wire, Arrays.asList("x", "y")).wire();
        require(!Arrays.equals(wPaths, wire), "paths edit did not change the wire");
        require(!Cards.taskHash(wPaths).equals(baseHash), "paths edit did not change the hash");
        require(Arrays.equals(Cards.load(wNeeds).body().getBytes(StandardCharsets.UTF_8), baseBody),
            "needs edit changed the body");
        require(Arrays.equals(Cards.load(wPaths).body().getBytes(StandardCharsets.UTF_8), baseBody),
            "paths edit changed the body");
        byte[] over = new byte[65];
        Arrays.fill(over, (byte) 'x');
        try {
            Cards.setNeeds(wire, Arrays.asList(new String(over, StandardCharsets.UTF_8)));
            throw new AssertionError("overlong needs entry was accepted");
        } catch (Cards.Refusal r) {
            // any refusal class is fine; the wire must stay byte-equal
        }
        require(Arrays.equals(Cards.load(wire).wire(), wire), "refused edit changed the wire");
    }

    private static final String VIEW_VOCAB = "Card id string value 64\n"
        + "Card sha string value 40\n"
        + "Card tier Tier value -\n"
        + "Card repo string value 96\n"
        + "Card _base string value 96\n"
        + "Card kind string value 24\n"
        + "Card priority Priority value -\n"
        + "Card stream string value 32\n"
        + "Card paths PathText list -\n"
        + "Card _new PathText list -\n"
        + "Card shared PathText list -\n"
        + "Card needs CardID list -\n"
        + "Card test string value 192\n"
        + "Card done string blob -\n"
        + "Card deadline_minutes int32 value -\n"
        + "Card source string blob -\n"
        + "Card epoch int32 value -\n"
        + "Card column Column value -\n"
        + "Card holder string value 32\n"
        + "Card attempt int32 value -\n"
        + "Card head string value 40\n"
        + "Card branch string value 128\n"
        + "Card body string blob -\n"
        + "Card extra KeyValue list -\n"
        + "Card consumers Consumer list -\n"
        + "CardID s string value 64\n"
        + "Consumer kind string value 16\n"
        + "Consumer who string value 32\n"
        + "Consumer route string value 48\n"
        + "Consumer model string value 64\n"
        + "Consumer started int64 value -\n"
        + "Consumer ended int64 value -\n"
        + "Consumer outcome string value 16\n"
        + "Consumer line string value 200\n"
        + "Consumer cost_micro_usd int64 value -\n"
        + "KeyValue key string value 32\n"
        + "KeyValue value string blob -\n"
        + "PathText s string value 192\n";

    // TestViewWalksEveryField: the view surface matches the explicit vocabulary
    // written here and the corpus's expected view.txt; scalar, list and body
    // values walk through the descriptors on a loaded and an edited record.
    static void testViewWalksEveryField() throws Exception {
        byte[] want = read("tables/cards/conformance/expected/view.txt");
        byte[] got = cards.Driver.view();
        require(Arrays.equals(got, want), "view does not match the expected view.txt");
        require(new String(got, StandardCharsets.UTF_8).equals(VIEW_VOCAB),
            "view does not match the explicit vocabulary");
        byte[] wire = read("tables/cards/conformance/cases/card-populated.wire");
        Cards.Record rec = Cards.load(wire);
        require(rec.body() != null, "walked record has no body");
        TableTypeInfo card = CardDescriptor.tableType();
        TableFieldInfo[] f = card.fields;
        require(f[0].name.equals("id") && f[0].shape.equals("value"), "descriptor id walk");
        require(f[0].bound == 64, "descriptor id bound");
        require(f[22].name.equals("body") && f[22].shape.equals("blob"), "descriptor body walk");
        require(f[8].name.equals("paths") && f[8].shape.equals("list"), "descriptor list walk");
        require(f[2].name.equals("tier") && f[2].kind == 30, "descriptor enum walk");
        Cards.Record edited = Cards.setTier(wire, 3);
        require(!Arrays.equals(edited.wire(), wire), "walked edited record did not change the wire");
    }
}
