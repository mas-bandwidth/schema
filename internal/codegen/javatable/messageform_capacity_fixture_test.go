package javatable

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestJavaMessageLoadCapacityFixture reads the committed capacity fixture,
// testdata/batch_capacity_two.bin (two LoginRequest bodies of
// tables/backend, client_build 40, written by the Go engine and pinned to it
// by test/conformance/harness TestBatchCapacityFixtureIsTheGoEnginesInEveryLeg),
// through the generated Java's LoginRequestMessage.loadMessages. Storage for
// one body whose clientBuild is the sentinel 17 owes "A refused batch returns
// a count of `0`" and "refuses before it decodes anything"
// (docs/SPEC-TABLES.md §3.3): refused, count 0, and (ruling R4) clientBuild
// back at its declared default. Storage for two owes 2 and 40. Storage for
// three, all at the sentinel, owes count 0 and every slot at its declared
// default after a framing refusal (form byte changed) and after a body/close
// refusal (last byte dropped). A caller's capacity is a resource limit
// (§6.5), so this row runs here, in the leg's own tests, never in the shared
// suites. Red before F-178's fix: loadMessages returned the wire's 2.
func TestJavaMessageLoadCapacityFixture(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := filepath.Abs(filepath.Join("testdata", "batch_capacity_two.bin"))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitDir(t, root, "tables/backend")
	out := runMessageJava(t, u, strings.ReplaceAll(javaCapacityFixtureMain, "FIXTURE", fixture))
	if strings.TrimSpace(out) != "ok" {
		t.Fatalf("capacity fixture: %s", out)
	}
}

const javaCapacityFixtureMain = `    public static void main(String[] args) throws Exception {
        final byte[] wire = java.nio.file.Files.readAllBytes(java.nio.file.Paths.get("FIXTURE"));
        final byte[] ann = new byte[TableVocabulary.announceMeasure()];
        TableVocabulary.announce(ann);
        final TableVocabulary vocab = new TableVocabulary();
        vocab.reset(4096);
        if (!TableVocabulary.announceRead(vocab, ann, new TableVocabulary.Report())) { fail("announceRead"); }
        final LoginRequestFixed.Value[] one = new LoginRequestFixed.Value[] { new LoginRequestFixed.Value() };
        one[0].clientBuild = 17;
        final TableVocabulary.Report rep = new TableVocabulary.Report();
        final int count = LoginRequestMessage.loadMessages(one, 1, vocab, wire, rep);
        if (count != 0 || !rep.refused) { fail("two bodies into storage for one: count=" + count + " refused=" + rep.refused + ", and a refused batch returns 0"); }
        // RULING R4: the refusal leaves the value handed in at its declared default.
        final int dflt = new LoginRequestFixed.Value().clientBuild;
        if (one[0].clientBuild != dflt) { fail("two bodies into storage for one: clientBuild reads " + one[0].clientBuild + ", not its declared default " + dflt); }
        final LoginRequestFixed.Value[] two = new LoginRequestFixed.Value[] { new LoginRequestFixed.Value(), new LoginRequestFixed.Value() };
        final TableVocabulary.Report loaded = new TableVocabulary.Report();
        final int got = LoginRequestMessage.loadMessages(two, 2, vocab, wire, loaded);
        if (got != 2 || loaded.refused || loaded.damaged || two[0].clientBuild != 40 || two[1].clientBuild != 40) {
            fail("two bodies into storage for two: count=" + got + " clientBuild " + two[0].clientBuild + " " + two[1].clientBuild);
        }
        // R4 over every value handed in: storage for THREE, all at the sentinel.
        // (a) the form byte changed, a framing refusal before any body; (b) the
        // last byte dropped, a body/close refusal. Slot 2 is past the batch's
        // count of 2, so a 0..count reset would leave it at 17.
        final byte[] framing = wire.clone();
        framing[0] = 9;
        final byte[] shortWire = java.util.Arrays.copyOf(wire, wire.length - 1);
        final String[] names = { "framing", "body" };
        final byte[][] wires = { framing, shortWire };
        for (int c = 0; c < 2; c++) {
            final LoginRequestFixed.Value[] three = new LoginRequestFixed.Value[] { new LoginRequestFixed.Value(), new LoginRequestFixed.Value(), new LoginRequestFixed.Value() };
            for (LoginRequestFixed.Value v : three) { v.clientBuild = 17; }
            final TableVocabulary.Report r3 = new TableVocabulary.Report();
            final int n3 = LoginRequestMessage.loadMessages(three, 3, vocab, wires[c], r3);
            if (n3 != 0 || !(r3.refused || r3.damaged)) { fail(names[c] + ": count=" + n3 + " refused=" + r3.refused + " damaged=" + r3.damaged + ", want a refusal with count 0"); }
            for (int k = 0; k < 3; k++) {
                if (three[k].clientBuild != dflt) { fail(names[c] + ": slot " + k + " clientBuild reads " + three[k].clientBuild + ", not its declared default " + dflt); }
            }
        }
        System.out.println("ok");
    }
`
