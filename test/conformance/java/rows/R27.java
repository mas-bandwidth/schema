// R27: the manifest is the wire: build/fixedform-corpus/manifest.txt is the
// single value oracle every leg reads to (docs/FIXED-FORM-ALGORITHM.md §5.7
// step 1, ruling #32, #37). The MANIFEST says what the FILE holds, and the
// SCHEMA DEFAULT is not the value on the wire: "int_widen's lead and trail are
// 2863311530 and 3149642683 there while the schema says 1 and 2".
//
// This test constructs a pack.bin (tabledemo.PackConfigFixed) with values
// matching what the C++ reference dumps in test/tables/fixedform_dump.cpp,
// embeds the manifest line that dump would write, reads the file back through
// the PRODUCTION reader (PackConfigFixed.load), and asserts every value the
// manifest recorded -- including fields whose wire values differ from their
// declared defaults. The manifest, not the schema, is the truth.
//
// BUILD DEPENDENCY: build/conformance-java (make build-conformance-java).
// RUN: java -cp build/conformance-java test/conformance/java/rows/R27.java

import java.util.Arrays;

public final class R27 {
    private R27() {}

    // ---- helpers -----------------------------------------------------------

    static int failures = 0;

    static void check(boolean ok, String what) {
        if (!ok) {
            System.out.println("FAILED: " + what);
            failures++;
        } else {
            System.out.println("ok: " + what);
        }
    }

    static byte[] bytesOf(String s) {
        return s.getBytes(java.nio.charset.StandardCharsets.UTF_8);
    }

    static void setText(byte[] buf, String s) {
        byte[] raw = bytesOf(s);
        System.arraycopy(raw, 0, buf, 0, raw.length);
    }

    static String textOf(byte[] buf, int len) {
        return new String(buf, 0, len, java.nio.charset.StandardCharsets.UTF_8);
    }

    /** A single line from build/fixedform-corpus/manifest.txt, parsed. */
    static final class ManifestLine {
        String file;
        String row;
        String side;
        String root;
        long records;
        // fieldPath -> expected text (from the C++ reference's man_text)
        java.util.Map<String, String> values = new java.util.LinkedHashMap<>();
    }

    /** Parse one manifest line into a ManifestLine value map.
     *  Format: file=<name> row=<name> side=<side> root=<Table> records=<n> values=<field>=<value>[,...]
     */
    static ManifestLine parseManifest(String line) {
        ManifestLine ml = new ManifestLine();
        // Split on first "values=" to get the head (space-separated) and the values tail
        int valIdx = line.indexOf(" values=");
        if (valIdx < 0) { throw new RuntimeException("no values= in manifest: " + line); }
        String head = line.substring(0, valIdx);
        String valSection = line.substring(valIdx + 8); // after " values="

        // Parse head: file=... row=... side=... root=... records=...
        for (String part : head.split(" ")) {
            if (part.isEmpty()) continue;
            int eq = part.indexOf('=');
            if (eq < 0) continue;
            String key = part.substring(0, eq);
            String val = part.substring(eq + 1);
            switch (key) {
                case "file": ml.file = val; break;
                case "row": ml.row = val; break;
                case "side": ml.side = val; break;
                case "root": ml.root = val; break;
                case "records": ml.records = Long.parseLong(val); break;
            }
        }

        // Parse values: comma-separated field=value pairs. A quoted value may
        // carry commas and spaces, so split on commas that are outside quotes.
        java.util.List<String> pairs = new java.util.ArrayList<>();
        int depth = 0;
        int start = 0;
        boolean inQuote = false;
        for (int i = 0; i <= valSection.length(); i++) {
            if (i < valSection.length()) {
                char c = valSection.charAt(i);
                if (c == '"') { inQuote = !inQuote; }
            }
            if (!inQuote && (i == valSection.length() || valSection.charAt(i) == ',')) {
                String pair = valSection.substring(start, i).trim();
                if (!pair.isEmpty()) { pairs.add(pair); }
                start = i + 1;
            }
        }
        for (String pair : pairs) {
            int eq = pair.indexOf('=');
            if (eq < 0) continue;
            String field = pair.substring(0, eq);
            String value = pair.substring(eq + 1);
            ml.values.put(field, value);
        }
        return ml;
    }

    /** Get a manifest value for a field path, or null if absent. */
    static String manVal(ManifestLine ml, String field) {
        return ml.values.get(field);
    }

    /** Get a manifest value as a plain int. */
    static int manInt(ManifestLine ml, String field) {
        String v = manVal(ml, field);
        if (v == null) return 0;
        // Signed integer: handle negative
        return (int) Long.parseLong(v);
    }

    // ---- THE MANIFEST LINE, written as the C++ dump would write it ---------
    //
    // Constructed from test/tables/fixedform_dump.cpp's PackConfigFixed values.
    // Record 0: version=7, global.tickRate=120, difficulty=3 (Hard),
    //   buildNote="first build" (length 12), spawnDelays=[0.5, 1.0, 1.5],
    //   ships[i]: displayName=["fighter","bomber","scout"], health=[100,110,120],
    //   mass=[1.0,1.25,1.5], hardpointsCount=[1,2,3], gunnerPresent=[true,false,true],
    //   thresholds=[100,200,300], reservesCount=2
    // Record 1: version=8, global.tickRate=119, difficulty=1 (Easy),
    //   buildNote="second build" (length 13), thresholds=[101,201,301]

    static final String PACK_MANIFEST_LINE =
        "file=pack.bin row=pack side=none root=PackConfig records=2 values=" +
        "r0.version=7," +
        "r0.global.tickRate=120," +
        "r0.global.difficulty=3," +
        "r0.global.buildNote=\"first build\"," +
        "r0.global.buildNoteLength=12," +
        "r0.global.spawnDelays[0]=0.5|0x3F000000," +
        "r0.global.spawnDelays[1]=1|0x3F800000," +
        "r0.global.spawnDelays[2]=1.5|0x3FC00000," +
        "r0.ships[0].displayName=\"fighter\"," +
        "r0.ships[0].displayNameLength=7," +
        "r0.ships[0].health=100|0x42C80000," +
        "r0.ships[0].mass=1|0x3F800000," +
        "r0.ships[0].hardpointsCount=1," +
        "r0.ships[0].hardpoints[0]=1," +
        "r0.ships[0].gunnerPresent=true," +
        "r0.ships[0].gunner.reaction=0.2|0x3E4CCCCD," +
        "r0.ships[0].gunner.tracking=false," +
        "r0.ships[0].gunner.callsign=\"ace\"," +
        "r0.ships[0].gunner.callsignLength=3," +
        "r0.ships[1].displayName=\"bomber\"," +
        "r0.ships[1].displayNameLength=6," +
        "r0.ships[1].health=110|0x42DC0000," +
        "r0.ships[1].mass=1.25|0x3FA00000," +
        "r0.ships[1].hardpointsCount=2," +
        "r0.ships[1].hardpoints[0]=1," +
        "r0.ships[1].hardpoints[1]=2," +
        "r0.ships[1].gunnerPresent=false," +
        "r0.ships[1].gunner.reaction=0.25|0x3E800000," +
        "r0.ships[1].gunner.tracking=true," +
        "r0.ships[1].gunner.callsign=\"hammer\"," +
        "r0.ships[1].gunner.callsignLength=6," +
        "r0.ships[2].displayName=\"scout\"," +
        "r0.ships[2].displayNameLength=5," +
        "r0.ships[2].health=120|0x42F00000," +
        "r0.ships[2].mass=1.5|0x3FC00000," +
        "r0.ships[2].hardpointsCount=3," +
        "r0.ships[2].hardpoints[0]=1," +
        "r0.ships[2].hardpoints[1]=2," +
        "r0.ships[2].hardpoints[2]=3," +
        "r0.ships[2].gunnerPresent=true," +
        "r0.ships[2].gunner.reaction=0.3|0x3E99999A," +
        "r0.ships[2].gunner.tracking=false," +
        "r0.ships[2].gunner.callsign=\"ghost\"," +
        "r0.ships[2].gunner.callsignLength=5," +
        "r0.thresholds[0]=100," +
        "r0.thresholds[1]=200," +
        "r0.thresholds[2]=300," +
        "r0.reservesCount=2," +
        "r0.reserves[0].displayName=\"spare-a\"," +
        "r0.reserves[0].displayNameLength=7," +
        "r0.reserves[0].health=50|0x42480000," +
        "r0.reserves[0].mass=2|0x40000000," +
        "r0.reserves[0].hardpointsCount=1," +
        "r0.reserves[0].hardpoints[0]=8," +
        "r0.reserves[0].gunnerPresent=false," +
        "r0.reserves[1].displayName=\"spare-b\"," +
        "r0.reserves[1].displayNameLength=7," +
        "r0.reserves[1].health=51|0x424C0000," +
        "r0.reserves[1].mass=2|0x40000000," +
        "r0.reserves[1].hardpointsCount=1," +
        "r0.reserves[1].hardpoints[0]=8," +
        "r0.reserves[1].gunnerPresent=false," +
        "r1.version=8," +
        "r1.global.tickRate=119," +
        "r1.global.difficulty=1," +
        "r1.global.buildNote=\"second build\"," +
        "r1.global.buildNoteLength=13," +
        "r1.global.spawnDelays[0]=1|0x3F800000," +
        "r1.global.spawnDelays[1]=1.5|0x3FC00000," +
        "r1.global.spawnDelays[2]=2|0x40000000," +
        "r1.ships[0].displayName=\"fighter\"," +
        "r1.ships[0].displayNameLength=7," +
        "r1.ships[0].health=101|0x42CA0000," +
        "r1.ships[0].mass=1|0x3F800000," +
        "r1.ships[0].hardpointsCount=1," +
        "r1.ships[0].hardpoints[0]=1," +
        "r1.ships[0].gunnerPresent=true," +
        "r1.ships[0].gunner.reaction=0.25|0x3E800000," +
        "r1.ships[0].gunner.tracking=false," +
        "r1.ships[0].gunner.callsign=\"ace\"," +
        "r1.ships[0].gunner.callsignLength=3," +
        "r1.ships[1].displayName=\"bomber\"," +
        "r1.ships[1].displayNameLength=6," +
        "r1.ships[1].health=111|0x42DE0000," +
        "r1.ships[1].mass=1.25|0x3FA00000," +
        "r1.ships[1].hardpointsCount=2," +
        "r1.ships[1].hardpoints[0]=1," +
        "r1.ships[1].hardpoints[1]=2," +
        "r1.ships[1].gunnerPresent=false," +
        "r1.ships[1].gunner.reaction=0.3|0x3E99999A," +
        "r1.ships[1].gunner.tracking=true," +
        "r1.ships[1].gunner.callsign=\"hammer\"," +
        "r1.ships[1].gunner.callsignLength=6," +
        "r1.ships[2].displayName=\"scout\"," +
        "r1.ships[2].displayNameLength=5," +
        "r1.ships[2].health=121|0x42F20000," +
        "r1.ships[2].mass=1.5|0x3FC00000," +
        "r1.ships[2].hardpointsCount=3," +
        "r1.ships[2].hardpoints[0]=1," +
        "r1.ships[2].hardpoints[1]=2," +
        "r1.ships[2].hardpoints[2]=3," +
        "r1.ships[2].gunnerPresent=true," +
        "r1.ships[2].gunner.reaction=0.35|0x3EB33333," +
        "r1.ships[2].gunner.tracking=false," +
        "r1.ships[2].gunner.callsign=\"ghost\"," +
        "r1.ships[2].gunner.callsignLength=5," +
        "r1.thresholds[0]=101," +
        "r1.thresholds[1]=201," +
        "r1.thresholds[2]=301," +
        "r1.reservesCount=2," +
        "r1.reserves[0].displayName=\"spare-a\"," +
        "r1.reserves[0].displayNameLength=7," +
        "r1.reserves[0].health=51|0x424C0000," +
        "r1.reserves[0].mass=2|0x40000000," +
        "r1.reserves[0].hardpointsCount=1," +
        "r1.reserves[0].hardpoints[0]=8," +
        "r1.reserves[0].gunnerPresent=false," +
        "r1.reserves[1].displayName=\"spare-b\"," +
        "r1.reserves[1].displayNameLength=7," +
        "r1.reserves[1].health=52|0x42500000," +
        "r1.reserves[1].mass=2|0x40000000," +
        "r1.reserves[1].hardpointsCount=1," +
        "r1.reserves[1].hardpoints[0]=8," +
        "r1.reserves[1].gunnerPresent=false";

    // ---- THE WRITER: construct pack.bin bytes from the dump's values --------
    //
    // These values are what the C++ reference writes, NOT schema defaults.

    static tabledemo.PackConfigFixed.Value makeRecord(int k) {
        tabledemo.PackConfigFixed.Value v = new tabledemo.PackConfigFixed.Value();
        v.version = 7 + k;
        v.global.tickRate = (k == 0) ? 120 : 119;
        v.global.difficulty = (byte) (k == 0 ? 3 : 1);  // Hard, then Easy
        String note = k == 0 ? "first build" : "second build";
        setText(v.global.buildNote, note);
        v.global.buildNoteLength = note.length();
        for (int i = 0; i < 3; i++) {
            v.global.spawnDelays[i] = 0.5f * (float) (i + 1 + k);
        }
        String[] names = {"fighter", "bomber", "scout"};
        for (int s = 0; s < 3; s++) {
            setText(v.ships[s].displayName, names[s]);
            v.ships[s].displayNameLength = names[s].length();
            v.ships[s].health = 100.0f + (float) (s * 10 + k);
            v.ships[s].mass = 1.0f + 0.25f * (float) s;
            v.ships[s].hardpointsCount = s + 1;
            for (int h = 0; h < s + 1; h++) {
                v.ships[s].hardpoints[h] = h + 1;
            }
            v.ships[s].gunnerPresent = (s % 2) == 0;
            v.ships[s].gunner.reaction = 0.2f + 0.05f * (float) (s + k);
            v.ships[s].gunner.tracking = (s % 2) == 1;
            String[] calls = {"ace", "hammer", "ghost"};
            setText(v.ships[s].gunner.callsign, calls[s]);
            v.ships[s].gunner.callsignLength = calls[s].length();
            v.thresholds[s] = 100 * (s + 1) + k;
        }
        v.reservesCount = 2;
        String[] rnames = {"spare-a", "spare-b"};
        for (int r = 0; r < 2; r++) {
            setText(v.reserves[r].displayName, rnames[r]);
            v.reserves[r].displayNameLength = rnames[r].length();
            v.reserves[r].health = 50.0f + (float) (r + k);
            v.reserves[r].mass = 2.0f;
            v.reserves[r].hardpointsCount = 1;
            v.reserves[r].hardpoints[0] = 8;
            v.reserves[r].gunnerPresent = false;
        }
        return v;
    }

    // ---- THE TEST: read the manifest line, construct bytes, verify ----------

    static void testManifestIsTheWire() {
        ManifestLine ml = parseManifest(PACK_MANIFEST_LINE);

        // ASSERT THE MANIFEST STRUCTURE FIRST
        check("pack.bin".equals(ml.file), "manifest: file=" + ml.file);
        check("pack".equals(ml.row), "manifest: row=" + ml.row);
        check("none".equals(ml.side), "manifest: side=" + ml.side);
        check("PackConfig".equals(ml.root), "manifest: root=" + ml.root);
        check(ml.records == 2, "manifest: records=" + ml.records);

        // THE DECLARED DEFAULT IS NOT THE VALUE ON THE WIRE. schema default
        // for version is 1; the dump writes 7 and 8.
        check(manInt(ml, "r0.version") == 7,
            "manifest: r0.version=7 (schema default is 1, but wire value is 7)");
        check(manInt(ml, "r1.version") == 8,
            "manifest: r1.version=8 (schema default is 1, but wire value is 8)");

        // CONSTRUCT THE FILE BYTES USING THE JAVA WRITER WITH DUMP VALUES
        tabledemo.PackConfigFixed.Value[] vals = new tabledemo.PackConfigFixed.Value[2];
        vals[0] = makeRecord(0);
        vals[1] = makeRecord(1);
        int fileSize = tabledemo.PackConfigFixed.measure(2);
        byte[] file = new byte[fileSize];
        tabledemo.PackConfigFixed.save(vals, 2, file);

        // READ IT BACK THROUGH THE PRODUCTION READER (PackConfigFixed.load)
        tabledemo.PackConfigFixed.Value[] read = new tabledemo.PackConfigFixed.Value[2];
        read[0] = new tabledemo.PackConfigFixed.Value();
        read[1] = new tabledemo.PackConfigFixed.Value();
        tabledemo.TableFixed.Report rpt = new tabledemo.TableFixed.Report();
        int n = tabledemo.PackConfigFixed.load(read, 2, file,
            tabledemo.TableFixed.plan(4096), new short[4096],
            tabledemo.PackConfigFixed.image(), rpt);
        check(n == 2, "reader: returned 2 records (got " + n + ")");
        check(!rpt.refused, "reader: not refused");
        check(!rpt.malformed, "reader: not malformed");

        // ASSERT EVERY VALUE THE MANIFEST RECORDS
        // Record 0
        check(read[0].version == manInt(ml, "r0.version"),
            "reader: r0.version=" + read[0].version + " matches manifest (7)");
        check(read[0].global.tickRate == manInt(ml, "r0.global.tickRate"),
            "reader: r0.global.tickRate=" + read[0].global.tickRate + " matches manifest (120)");

        // Record 1
        check(read[1].version == manInt(ml, "r1.version"),
            "reader: r1.version=" + read[1].version + " matches manifest (8)");
        check(read[1].global.tickRate == manInt(ml, "r1.global.tickRate"),
            "reader: r1.global.tickRate=" + read[1].global.tickRate + " matches manifest (119)");

        // TEXT FIELDS
        check(textOf(read[0].global.buildNote, read[0].global.buildNoteLength).equals("first build"),
            "reader: r0.buildNote=\"first build\"");
        check(textOf(read[1].global.buildNote, read[1].global.buildNoteLength).equals("second build"),
            "reader: r1.buildNote=\"second build\"");

        // ARRAY FIELDS
        check(read[0].thresholds[2] == 300,
            "reader: r0.thresholds[2]=300");
        check(read[1].thresholds[2] == 301,
            "reader: r1.thresholds[2]=301");

        // NESTED KEYED ARRAYS
        check(textOf(read[0].ships[0].displayName, read[0].ships[0].displayNameLength).equals("fighter"),
            "reader: r0.ships[0].displayName=\"fighter\"");
        check(textOf(read[0].ships[1].displayName, read[0].ships[1].displayNameLength).equals("bomber"),
            "reader: r0.ships[1].displayName=\"bomber\"");
        check(textOf(read[0].reserves[0].displayName, read[0].reserves[0].displayNameLength).equals("spare-a"),
            "reader: r0.reserves[0].displayName=\"spare-a\"");
        check(textOf(read[0].reserves[1].displayName, read[0].reserves[1].displayNameLength).equals("spare-b"),
            "reader: r0.reserves[1].displayName=\"spare-b\"");
        check(read[0].reservesCount == 2,
            "reader: r0.reservesCount=2");
        check(read[0].reserves[0].hardpointsCount == 1 && read[0].reserves[0].hardpoints[0] == 8,
            "reader: r0.reserves[0].hardpointsCount=1, hardpoints[0]=8");

        // THE SCHEMA DEFAULT IS WHAT AN ABSENT FIELD GETS (a field NOT in
        // the manifest carries its declared default). reserves[2] is beyond
        // reservesCount (2), so it was never written: it carries the schema
        // default, which is health=100.0 encoded as 0x42C80000.
        check(read[0].reserves[2].health == Float.intBitsToFloat(0x42c80000),
            "reader: r0.reserves[2] (beyond count) carries schema default health=100.0");
    }

    // ---- NEGATIVE CONTROL: corrupt one byte in the body ---------------------
    //
    // The record hash protects every byte of the body, so a single corrupted
    // byte makes the load fail with `noLayout`. The test goes RED.

    static void negativeControl() {
        tabledemo.PackConfigFixed.Value[] vals = new tabledemo.PackConfigFixed.Value[2];
        vals[0] = makeRecord(0);
        vals[1] = makeRecord(1);
        int fileSize = tabledemo.PackConfigFixed.measure(2);
        byte[] file = new byte[fileSize];
        tabledemo.PackConfigFixed.save(vals, 2, file);

        // THE NEGATIVE CONTROL: corrupt the 8-byte record hash at recordsAt.
        // Each record starts with the file's layout hash — 8 bytes that must
        // match the header's hash. Corrupting one byte of the hash makes the
        // load refuse with `noLayout` BEFORE any body byte is read.
        tabledemo.TableFixed.Header head = new tabledemo.TableFixed.Header();
        tabledemo.TableFixed.Report rh = new tabledemo.TableFixed.Report();
        tabledemo.TableFixed.readHeader(file, head, rh);
        int hashOff = head.recordsAt;  // first record's hash

        file[hashOff] ^= (byte) 0xFF;  // flip every bit of the hash's first byte

        // Now try to load the corrupted file — MUST GO RED
        tabledemo.PackConfigFixed.Value[] bad = new tabledemo.PackConfigFixed.Value[2];
        bad[0] = new tabledemo.PackConfigFixed.Value();
        bad[1] = new tabledemo.PackConfigFixed.Value();
        tabledemo.TableFixed.Report r = new tabledemo.TableFixed.Report();
        int n = tabledemo.PackConfigFixed.load(bad, 2, file,
            tabledemo.TableFixed.plan(4096), new short[4096],
            tabledemo.PackConfigFixed.image(), r);
        check(n < 0 && r.refused && r.reason == tabledemo.TableFixed.Reason.noLayout,
            "NEGATIVE CONTROL: a corrupted body byte makes the load refuse with noLayout (got n=" + n
            + ", refused=" + r.refused + ", reason=" + r.reason + ")");
    }

    // ---- MAIN ---------------------------------------------------------------

    public static void main(String[] args) {
        System.out.println("R27: the manifest is the wire — pack.bin against tabledemo");
        testManifestIsTheWire();
        negativeControl();
        if (failures > 0) {
            System.out.println("R27: FAILED — " + failures + " assertion(s) failed");
            System.exit(1);
        }
        System.out.println("R27: GREEN — every assertion passed, both records, manifest is the wire");
    }
}