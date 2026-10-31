package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestOtherVariableConstructsAreRefusedByName retains its historical gate name.
// The map/optional/keyed/union/wide refusal cases move to the positive native
// matrix in TestMapsAndUnionArmsOnTheTableWireJava. The closure walk must also
// admit a carried map reached through a list, preserving the card regression.
func TestOtherVariableConstructsAreRefusedByName(t *testing.T) {
	t.Parallel()
	nested := `package probe
table Child { m map[int32]int32 }
table Card { items []Child }
`
	if err := refuseVariableForm(unitFrom(t, nested)); err != nil {
		t.Fatalf("a carried map nested in a list element is refused: %v", err)
	}

	// The card's own constructs are NOT refused: a variable table root with bool,
	// int32, int64 and enum fields, a string(N), the *string byte buffer and an
	// unbounded list of table elements (docs/SPEC-TABLES.md §2.5, §2.9).
	card := `package probe
enum Tier { flash, pro, heavy }
table Elem { s string(8) }
table Card
{
    id       string(64)
    tier     Tier
    count    int32
    total    int64
    ok       bool
    done_when *string
    source    *string
    items    []Elem
}
`
	// The boundary rule is asked directly here: the card's own constructs lie
	// inside the variable form, so the refusal names nothing. (The emitters for
	// the variable form itself are the follow-on work; this test pins the
	// boundary, not the codec.)
	if err := refuseVariableForm(unitFrom(t, card)); err != nil {
		t.Fatalf("the card's own constructs are refused: %v", err)
	}
}

// TestCardConstructsRoundTripTheVariableForm is the wire half of this card:
// a Java program reads tables/cards/fuzz/card-populated.wire to the values in
// card-populated.json and re-saves form 1 byte for byte.
func TestCardConstructsRoundTripTheVariableForm(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaTools(t)
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "tables", "cards", "Card.schema"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(unitFrom(t, string(src)))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["CardVariable.java"]; !ok {
		t.Fatal("CardVariable.java not emitted for tables/cards")
	}
	dir := t.TempDir()
	classes := filepath.Join(dir, "classes")
	srcdir := filepath.Join(dir, "src", "cards")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(srcdir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if strings.HasSuffix(name, ".java") {
			if err := os.WriteFile(filepath.Join(srcdir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	wire, err := os.ReadFile(filepath.Join("..", "..", "..", "tables", "cards", "fuzz", "card-populated.wire"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "card-populated.wire"), wire, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Driver.java"), []byte(cardRoundTripDriver), 0o644); err != nil {
		t.Fatal(err)
	}
	srcs, err := filepath.Glob(filepath.Join(srcdir, "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-d", classes}, srcs...)
	args = append(args, filepath.Join(dir, "Driver.java"))
	cmd := exec.Command(javac, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "Driver", filepath.Join(dir, "card-populated.wire")).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("driver: %s", out)
	}

	// The two-declaration pair (internal/codegen/javatable/testdata/cards): the
	// newer build renames `old_name` to `renamed` with `was` and adds `extra`;
	// the older build reads the newer wire and finds the rename, skips and
	// counts the unknown `extra`, and defaults its own absent field (§4, §5).
	writeVarPkg := func(pkg, schemaName string) {
		s, err := os.ReadFile(filepath.Join("testdata", "cards", schemaName))
		if err != nil {
			t.Fatal(err)
		}
		files, err := Generate(unitFrom(t, string(s)))
		if err != nil {
			t.Fatal(err)
		}
		pdir := filepath.Join(srcdir, "..", pkg)
		if err := os.MkdirAll(pdir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if strings.HasSuffix(name, ".java") {
				if err := os.WriteFile(filepath.Join(pdir, name), data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	writeVarPkg("newpkg", "newer.schema")
	writeVarPkg("oldpkg", "older.schema")
	if err := os.WriteFile(filepath.Join(dir, "Cross.java"), []byte(cardCrossReadDriver), 0o644); err != nil {
		t.Fatal(err)
	}
	allSrcs, err := filepath.Glob(filepath.Join(dir, "src", "*", "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	allSrcs = append(allSrcs, filepath.Join(dir, "Cross.java"))
	cargs := append([]string{"--release", "17", "-d", classes}, allSrcs...)
	if out, err := exec.Command(javac, cargs...).CombinedOutput(); err != nil {
		t.Fatalf("javac (cross): %v\n%s", err, out)
	}
	if out, err := exec.Command(javaBin, "-ea", "-cp", classes, "Cross").CombinedOutput(); err != nil {
		t.Fatalf("java (cross): %v\n%s", err, out)
	} else if !strings.Contains(string(out), "ok") {
		t.Fatalf("cross driver: %s", out)
	}
}

// cardCrossReadDriver saves a newer-declaration wire and reads it with the older
// declaration: the `was` rename is found, the unknown field is skipped and
// counted, and the absent field reads its default.
const cardCrossReadDriver = `public final class Cross {
    private Cross() {}

    static void fail(String what) {
        System.out.println("FAILED: " + what);
        System.exit(1);
    }

    public static void main(String[] args) {
        newpkg.CardVariable.Value v = new newpkg.CardVariable.Value();
        v.id = "hi";
        v.renamed = 42;
        v.extra = "x";
        byte[] wire = newpkg.CardVariable.save(v);

        oldpkg.TableVariable.Report report = new oldpkg.TableVariable.Report();
        oldpkg.CardVariable.Value o = oldpkg.CardVariable.load(wire, report);
        if (o == null) { fail("load returned null"); }
        if (!"hi".equals(o.id)) { fail("id=" + o.id); }
        if (o.oldName != 42) { fail("oldName=" + o.oldName); }
        if (!"".equals(o.absent)) { fail("absent=" + o.absent); }
        if (report.unknown != 1) { fail("unknown=" + report.unknown); }
        System.out.println("ok");
    }
}
`

// cardRoundTripDriver loads the committed wire, checks the populated values, and
// re-saves byte for byte.
const cardRoundTripDriver = `import java.nio.file.Files;
import java.nio.file.Paths;

public final class Driver {
    private Driver() {}

    static void fail(String what) {
        System.out.println("FAILED: " + what);
        System.exit(1);
    }

    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Paths.get(args[0]));
        cards.TableVariable.Report report = new cards.TableVariable.Report();
        cards.CardVariable.Value v = cards.CardVariable.load(wire, report);
        if (v == null) { fail("load returned null"); }
        if (report.malformed || report.refused) { fail("load reported malformed/refused"); }
        if (!"populated".equals(v.id)) { fail("id=" + v.id); }
        if (!"0123456789012345678901234567890123456789".equals(v.sha)) { fail("sha"); }
        if (v.tier != 1) { fail("tier=" + v.tier); }
        if (!"repo".equals(v.repo)) { fail("repo"); }
        if (v.priority != 3) { fail("priority=" + v.priority); }
        if (!"stream".equals(v.stream)) { fail("stream"); }
        if (v.paths.size() != 2) { fail("paths=" + v.paths.size()); }
        if (!"path/one".equals(v.paths.get(0).s)) { fail("paths[0]"); }
        if (!"path/two".equals(v.paths.get(1).s)) { fail("paths[1]"); }
        if (v.new_.size() != 1 || !"new/path".equals(v.new_.get(0).s)) { fail("_new"); }
        if (v.needs.size() != 2 || !"need-two".equals(v.needs.get(1).s)) { fail("needs"); }
        if (!"fixture test".equals(v.test)) { fail("test"); }
        if (!"done when".equals(v.done)) { fail("done_when=" + v.done); }
        if (v.deadlineMinutes != 45) { fail("deadline_minutes=" + v.deadlineMinutes); }
        if (!"source résumé→世界".equals(v.source)) { fail("source=" + v.source); }
        if (v.epoch != 7) { fail("epoch=" + v.epoch); }
        if (v.column != 4) { fail("column=" + v.column); }
        if (!"holder".equals(v.holder)) { fail("holder"); }
        if (v.attempt != 1) { fail("attempt"); }
        if (!"branch/name".equals(v.branch)) { fail("branch"); }
        if (v.body == null || !v.body.contains("body with multibyte")) { fail("body"); }

        byte[] saved = cards.CardVariable.save(v);
        if (!java.util.Arrays.equals(wire, saved)) {
            int d = 0;
            while (d < wire.length && d < saved.length && wire[d] == saved[d]) { d++; }
            fail("re-save differs: wire " + wire.length + " bytes, saved " + saved.length
                    + " bytes, first at " + d + " wire=" + hex(wire, d) + " saved=" + hex(saved, d));
        }
        System.out.println("ok");
    }

    static String hex(byte[] b, int at) {
        StringBuilder s = new StringBuilder();
        for (int i = at; i < b.length && i < at + 24; i++) { s.append(String.format("%02x ", b[i])); }
        return s.toString();
    }
}
`

// TestCardTextFormMatchesGo is the text half of this card (docs/SPEC-TABLES.md
// §16): the JSON of card-populated.wire equals tables/cards/fuzz/card-populated.json
// byte for byte, that JSON read back saves to card-populated.wire byte for byte,
// and an unknown property, an out-of-range enum and an over-bound string are
// refused (counted, never truncated).
func TestCardTextFormMatchesGo(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaTools(t)
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "tables", "cards", "Card.schema"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(unitFrom(t, string(src)))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	classes := filepath.Join(dir, "classes")
	srcdir := filepath.Join(dir, "src", "cards")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(srcdir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if strings.HasSuffix(name, ".java") {
			if err := os.WriteFile(filepath.Join(srcdir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	wire, err := os.ReadFile(filepath.Join("..", "..", "..", "tables", "cards", "fuzz", "card-populated.wire"))
	if err != nil {
		t.Fatal(err)
	}
	jsonText, err := os.ReadFile(filepath.Join("..", "..", "..", "tables", "cards", "fuzz", "card-populated.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "card-populated.wire"), wire, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "card-populated.json"), jsonText, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Driver.java"), []byte(cardTextDriver), 0o644); err != nil {
		t.Fatal(err)
	}
	srcs, err := filepath.Glob(filepath.Join(srcdir, "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-d", classes}, srcs...)
	args = append(args, filepath.Join(dir, "Driver.java"))
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "Driver",
		filepath.Join(dir, "card-populated.wire"), filepath.Join(dir, "card-populated.json")).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("driver: %s", out)
	}
}

// cardTextDriver loads the committed wire, writes its JSON and compares it byte
// for byte with the committed JSON; reads that JSON back and compares the
// re-saved wire byte for byte; and proves an unknown property, an out-of-range
// enum and an over-bound string are counted and never silently truncated
// (docs/SPEC-TABLES.md §16.2, §16.3).
const cardTextDriver = `import java.nio.file.Files;
import java.nio.file.Paths;
import java.nio.charset.StandardCharsets;

public final class Driver {
    private Driver() {}

    static void fail(String what) {
        System.out.println("FAILED: " + what);
        System.exit(1);
    }

    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Paths.get(args[0]));
        byte[] golden = Files.readAllBytes(Paths.get(args[1]));

        cards.TableVariable.Report report = new cards.TableVariable.Report();
        cards.CardVariable.Value v = cards.CardVariable.load(wire, report);
        if (v == null) { fail("load returned null"); }
        byte[] text = cards.CardVariable.toJson(v);
        if (!java.util.Arrays.equals(golden, text)) {
            int d = 0;
            while (d < golden.length && d < text.length && golden[d] == text[d]) { d++; }
            fail("ToJson differs from the Go leg's JSON at byte " + d
                    + "\n  golden: " + new String(golden, Math.max(0, d - 20), Math.min(golden.length, d + 20) - Math.max(0, d - 20), StandardCharsets.UTF_8)
                    + "\n  java:   " + new String(text, Math.max(0, d - 20), Math.min(text.length, d + 20) - Math.max(0, d - 20), StandardCharsets.UTF_8));
        }

        cards.TableVariable.Report read = new cards.TableVariable.Report();
        cards.CardVariable.Value from = cards.CardVariable.fromJson(golden, read);
        if (read.malformed || read.unknown != 0 || read.kindMismatch != 0 || read.clamped != 0) {
            fail("FromJson of the Go leg's JSON reports loss (unknown=" + read.unknown
                    + " kindMismatch=" + read.kindMismatch + " clamped=" + read.clamped + ")");
        }
        byte[] saved = cards.CardVariable.save(from);
        if (!java.util.Arrays.equals(wire, saved)) {
            int d = 0;
            while (d < wire.length && d < saved.length && wire[d] == saved[d]) { d++; }
            fail("FromJson then Save differs from the wire at byte " + d);
        }

        // An UNKNOWN property is skipped and counted, never silently read (§16.2).
        byte[] unknownText = ("{ \"id\": \"x\", \"bogus\": 123 }").getBytes(StandardCharsets.UTF_8);
        cards.TableVariable.Report ur = new cards.TableVariable.Report();
        cards.CardVariable.Value uv = cards.CardVariable.fromJson(unknownText, ur);
        if (ur.malformed) { fail("an unknown property is not malformed"); }
        if (ur.unknown != 1) { fail("unknown property: unknown=" + ur.unknown); }
        if (!"x".equals(uv.id)) { fail("unknown property lost id=" + uv.id); }

        // An OUT-OF-RANGE enum name reads None and is counted, never truncated (§16.2).
        byte[] enumText = ("{ \"tier\": \"nonsense\" }").getBytes(StandardCharsets.UTF_8);
        cards.TableVariable.Report er = new cards.TableVariable.Report();
        cards.CardVariable.Value ev = cards.CardVariable.fromJson(enumText, er);
        if (er.unknown != 1) { fail("out-of-range enum: unknown=" + er.unknown); }
        if (ev.tier != 0) { fail("out-of-range enum did not read None"); }

        // An OVER-BOUND string is clamped and counted, never silently truncated (§16.2).
        StringBuilder longId = new StringBuilder();
        for (int i = 0; i < 70; i++) { longId.append('a'); }
        byte[] stringText = ("{ \"id\": \"" + longId + "\" }").getBytes(StandardCharsets.UTF_8);
        cards.TableVariable.Report sr = new cards.TableVariable.Report();
        cards.CardVariable.Value sv = cards.CardVariable.fromJson(stringText, sr);
        if (sr.clamped != 1) { fail("over-bound string: clamped=" + sr.clamped); }
        if (sv.id.length() != 64) { fail("over-bound string kept " + sv.id.length() + " code points"); }

        System.out.println("ok");
    }
}
`
