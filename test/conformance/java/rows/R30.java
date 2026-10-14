// R30.java — guard for java/R30 (compressed float resolution in the 'Q' digest).
//
// SPEC: docs/FIXED-FORM-ALGORITHM.md:583
// "a FLOAT range's RESOLUTION | 'Q', then the step as an f64's IEEE-754 bits,
//  u64 LE, immediately after that range's 'R' and bounds."
//
// Without the 'Q' row, two compressed floats with the same range but different
// resolutions produce identical layout hashes. This test generates two schemas
// that differ ONLY in resolution (0.1 vs 0.01) and asserts the wire hashes
// differ. If they are equal, the digest is missing the resolution — the defect
// the red team found.
//
// Run:  java -cp build/conformance-java test/conformance/java/rows/R30.java
// Exit: 0 green, 1 red

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

public final class R30 {
    private R30() {}

    static final String SCHEMA_OLD =
        "package r30_old\n" +
        "\n" +
        "fixed table R30Test\n" +
        "{\n" +
        "    lead  uint32 = 1\n" +
        "    aim   float32 | min = -1, max = 1, resolution = 0.1\n" +
        "    trail uint32 = 2\n" +
        "}\n";

    static final String SCHEMA_NEW =
        "package r30_new\n" +
        "\n" +
        "fixed table R30Test\n" +
        "{\n" +
        "    lead  uint32 = 1\n" +
        "    aim   float32 | min = -1, max = 1, resolution = 0.01\n" +
        "    trail uint32 = 2\n" +
        "}\n";

    static int failures = 0;
    static int assertions = 0;

    static void assertEqual(String label, long want, long got) {
        assertions++;
        if (want != got) {
            failures++;
            System.out.println("FAIL " + label + ": want 0x" + Long.toHexString(want) + ", got 0x" + Long.toHexString(got));
        } else {
            System.out.println("OK   " + label + ": 0x" + Long.toHexString(got));
        }
    }

    static void assertTrue(String label, boolean cond, String detail) {
        assertions++;
        if (!cond) {
            failures++;
            System.out.println("FAIL " + label + ": " + detail);
        } else {
            System.out.println("OK   " + label);
        }
    }

    public static void main(String[] args) throws Exception {
        String repoRoot = System.getProperty("user.dir");
        Path work = Path.of(repoRoot, "build", "r30-work");
        Files.createDirectories(work);
        Path oldSrc = work.resolve("src-old");
        Path newSrc = work.resolve("src-new");
        Path classes = work.resolve("classes");
        Files.createDirectories(oldSrc);
        Files.createDirectories(newSrc);
        Files.createDirectories(classes);

        Path oldSchema = work.resolve("R30Old.schema");
        Path newSchema = work.resolve("R30New.schema");
        Files.writeString(oldSchema, SCHEMA_OLD);
        Files.writeString(newSchema, SCHEMA_NEW);

        String binSchema = repoRoot + "/bin/schema";

        run(binSchema, "generate", "--lang", "java", "--out", oldSrc.toString(), oldSchema.toString());
        run(binSchema, "generate", "--lang", "java", "--out", newSrc.toString(), newSchema.toString());

        Path oldFixed = oldSrc.resolve("R30TestFixed.java");
        Path newFixed = newSrc.resolve("R30TestFixed.java");
        assertTrue("old R30TestFixed.java generated", Files.exists(oldFixed), "file not found: " + oldFixed);
        assertTrue("new R30TestFixed.java generated", Files.exists(newFixed), "file not found: " + newFixed);

        long oldHash = readHashConstant(oldFixed);
        long newHash = readHashConstant(newFixed);

        assertEqual("R30Test hash (old, res=0.1)", oldHash, oldHash);
        assertEqual("R30Test hash (new, res=0.01)", newHash, newHash);

        // THE ASSERTION: the two hashes MUST differ, because the 'Q' row
        // (resolution) is part of the definitions digest which feeds into the
        // layout hash (docs/FIXED-FORM-ALGORITHM.md:568).
        assertTrue("resolution moves the layout hash ('Q' in the digest)",
            oldHash != newHash,
            "hashes are identical (0x" + Long.toHexString(oldHash) +
            ") despite different resolutions (0.1 vs 0.01) — the 'Q' row is missing from the digest");

        String javac = System.getenv("JAVAC");
        if (javac == null) {
            javac = "javac";
        }
        int exit;

        // Compile the old generation alone
        ProcessBuilder pb1 = new ProcessBuilder(
            javac, "--release", "17", "-nowarn", "-d", classes.toString(),
            oldSrc.resolve("R30TestFixed.java").toString(),
            oldSrc.resolve("TableFixed.java").toString()
        );
        pb1.inheritIO();
        exit = pb1.start().waitFor();
        assertTrue("old generation compiles under javac --release 17", exit == 0, "javac exit=" + exit);

        // Compile the new generation alone
        Path classesNew = work.resolve("classes-new");
        Files.createDirectories(classesNew);
        ProcessBuilder pb2 = new ProcessBuilder(
            javac, "--release", "17", "-nowarn", "-d", classesNew.toString(),
            newSrc.resolve("R30TestFixed.java").toString(),
            newSrc.resolve("TableFixed.java").toString()
        );
        pb2.inheritIO();
        exit = pb2.start().waitFor();
        assertTrue("new generation compiles under javac --release 17", exit == 0, "javac exit=" + exit);

        // Verify record size is unchanged (the float rides whole, SPEC 3.4)
        int oldRec = readRecordConstant(oldFixed);
        int newRec = readRecordConstant(newFixed);
        assertEqual("recordBytes (old)", oldRec, oldRec);
        assertEqual("recordBytes (new)", newRec, newRec);
        assertTrue("record size unchanged by resolution change (SPEC 3.4)",
            oldRec == newRec,
            "old=" + oldRec + ", new=" + newRec);

        System.out.println(assertions + " assertions, " + failures + " failures");
        if (failures > 0) {
            System.exit(1);
        }
    }

    static long readHashConstant(Path file) throws IOException {
        String text = Files.readString(file);
        Pattern p = Pattern.compile("public\\s+static\\s+final\\s+long\\s+hash\\s*=\\s*(0x[0-9a-fA-F]+)L");
        Matcher m = p.matcher(text);
        if (!m.find()) {
            throw new IOException("no hash constant found in " + file);
        }
        return Long.decode(m.group(1));
    }

    static int readRecordConstant(Path file) throws IOException {
        String text = Files.readString(file);
        Pattern p = Pattern.compile("public\\s+static\\s+final\\s+int\\s+recordBytes\\s*=\\s*(\\d+)");
        Matcher m = p.matcher(text);
        if (!m.find()) {
            throw new IOException("no recordBytes constant found in " + file);
        }
        return Integer.parseInt(m.group(1));
    }

    static void run(String... cmd) throws IOException, InterruptedException {
        ProcessBuilder pb = new ProcessBuilder(cmd);
        pb.inheritIO();
        int exit = pb.start().waitFor();
        if (exit != 0) {
            throw new IOException("command failed with exit " + exit + ": " + String.join(" ", cmd));
        }
    }
}
