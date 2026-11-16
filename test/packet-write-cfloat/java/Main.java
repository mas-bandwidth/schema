// Packet Write of a compressed float (SPEC §5.1 row 10,
// docs/PACKET-WIRE-ALGORITHM.md §2.5). The shared cases live in the file
// this driver is given. A refused case is a debug assert (java -ea): a NaN,
// an infinity, or a finite value outside [min, max]. A written case is
// written and read back to its quantized value. A release build is not this
// driver's question: without -ea the assert is gone by design.

import cfloat.PacketCFloat;
import java.nio.file.Files;
import java.nio.file.Path;

public final class Main {
    private Main() {}

    static int failed;

    static void fail(String what) {
        System.out.println("FAILED: " + what);
        failed++;
    }

    static float parseValue(String token) {
        switch (token) {
            case "NaN":
                return Float.NaN;
            case "+Inf":
                return Float.POSITIVE_INFINITY;
            case "-Inf":
                return Float.NEGATIVE_INFINITY;
            default:
                return Float.parseFloat(token);
        }
    }

    static boolean assertsEnabled() {
        boolean on = false;
        assert on = true;
        return on;
    }

    public static void main(String[] args) throws Exception {
        if (args.length != 1) {
            fail("usage: Main <cases.txt>");
            System.exit(1);
        }
        if (!assertsEnabled()) {
            fail("packet-write-cfloat Java requires -ea; the range check is a debug assert");
            System.exit(1);
        }
        int refused = 0;
        int written = 0;
        int cases = 0;
        for (String raw : Files.readAllLines(Path.of(args[0]))) {
            String line = raw.trim();
            if (line.isEmpty() || line.startsWith("#")) {
                continue;
            }
            String[] field = line.split("\\s+");
            if (field.length < 3) {
                fail("bad case line: " + line);
                continue;
            }
            cases++;
            String name = field[0];
            float value = parseValue(field[1]);
            boolean wantRefused = field[2].equals("refused");
            if (!wantRefused && !field[2].equals("written")) {
                fail(name + ": unknown verdict " + field[2]);
                continue;
            }
            float expect = value;
            if (field.length >= 5 && field[3].equals("reads-back")) {
                expect = parseValue(field[4]);
            }
            PacketCFloat.CFloat in = new PacketCFloat.CFloat();
            in.x = value;
            byte[] data = new byte[PacketCFloat.C_FLOAT_MAX_BYTES];
            boolean threw = false;
            int bits = 0;
            try {
                bits = PacketCFloat.writeCFloatBits(in, data, 0);
            } catch (AssertionError error) {
                threw = true;
            }
            if (wantRefused) {
                if (!threw) {
                    fail(name + ": a compressed float the declaration does not admit was written");
                } else {
                    refused++;
                }
                continue;
            }
            if (threw) {
                fail(name + ": a lawful compressed float was refused");
                continue;
            }
            PacketCFloat.CFloat out = new PacketCFloat.CFloat();
            if (!PacketCFloat.readCFloat(out, data, bits)) {
                fail(name + ": the write did not read back");
                continue;
            }
            if (Float.floatToRawIntBits(out.x) != Float.floatToRawIntBits(expect)) {
                fail(name
                        + ": read back "
                        + Float.floatToRawIntBits(out.x)
                        + ", want the quantized value "
                        + Float.floatToRawIntBits(expect));
                continue;
            }
            written++;
        }
        if (cases == 0) {
            fail("no cases");
        }
        if (failed != 0) {
            System.exit(1);
        }
        System.out.println(
                "packet-write-cfloat Java: " + refused + " refused, " + written + " written");
    }
}
