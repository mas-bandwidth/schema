// The Java card facade's unit tests, one check per named test, run from the
// repository root. No test framework: a failure prints and the process exits
// nonzero. The tests pin the Go reference leg's answers (tables/cards/*.go) on
// the shared corpus (tables/cards/{fuzz,conformance}).
import cards.CardVariable;
import cards.Cards;
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
}
