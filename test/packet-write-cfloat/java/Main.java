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

    // The case being run and the failures before it: answer() prints its
    // "case <name> ok|FAILED: ..." line, the answer tools/casegate reads.
    static String pending;
    static int pendingFailed;

    static void answer() {
        if (pending != null) {
            System.out.println(
                    "case " + pending + (failed == pendingFailed ? " ok" : " FAILED: see the FAILED line above"));
        }
        pending = null;
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
            answer();
            pending = name;
            pendingFailed = failed;
            float value = parseValue(field[1]);
            boolean wantRefused = field[2].equals("refused");
            if (!wantRefused && !field[2].equals("written")) {
                fail(name + ": unknown verdict " + field[2]);
                continue;
            }
            // [reads-back <v>] [type RatioEndpoint|RatioPair] [wire <hex>] [bits <n>], in pairs
            float expect = value;
            boolean ratio = false;
            boolean pair = false; // both fields of a RatioPair take the value
            String wire = null;
            int wantBits = -1;
            boolean malformed = field.length % 2 != 1;
            for (int i = 3; i + 1 < field.length; i += 2) {
                switch (field[i]) {
                    case "reads-back":
                        expect = parseValue(field[i + 1]);
                        break;
                    case "type":
                        pair = field[i + 1].equals("RatioPair");
                        ratio = pair || field[i + 1].equals("RatioEndpoint");
                        malformed |= !ratio;
                        break;
                    case "wire":
                        wire = field[i + 1].toLowerCase(java.util.Locale.ROOT);
                        break;
                    case "bits":
                        try {
                            wantBits = Integer.parseInt(field[i + 1]);
                            malformed |= wantBits < 0;
                        } catch (NumberFormatException error) {
                            malformed = true;
                        }
                        break;
                    default:
                        malformed = true;
                }
            }
            if (malformed) {
                fail(name + ": malformed modifier in " + line);
                continue;
            }
            PacketCFloat.CFloat in = new PacketCFloat.CFloat();
            in.x = value;
            PacketCFloat.RatioEndpoint ratioIn = new PacketCFloat.RatioEndpoint();
            ratioIn.ratio = value;
            PacketCFloat.RatioPair pairIn = new PacketCFloat.RatioPair();
            pairIn.first = value;
            pairIn.second = value;
            byte[] data = new byte[pair ? PacketCFloat.RATIO_PAIR_MAX_BYTES
                    : ratio ? PacketCFloat.RATIO_ENDPOINT_MAX_BYTES : PacketCFloat.C_FLOAT_MAX_BYTES];
            boolean threw = false;
            int bits = 0;
            try {
                bits = pair
                        ? PacketCFloat.writeRatioPairBits(pairIn, data, 0)
                        : ratio
                                ? PacketCFloat.writeRatioEndpointBits(ratioIn, data, 0)
                                : PacketCFloat.writeCFloatBits(in, data, 0);
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
            // the bits the write emitted, before any padding to a byte
            if (wantBits >= 0 && bits != wantBits) {
                fail(name + ": wrote " + bits + " bits, want " + wantBits);
                continue;
            }
            if (wire != null) {
                StringBuilder got = new StringBuilder();
                for (int i = 0; i < (bits + 7) / 8; i++) {
                    got.append(String.format("%02x", data[i] & 0xff));
                }
                if (!got.toString().equals(wire)) {
                    fail(name + ": wrote " + got + ", want " + wire);
                    continue;
                }
            }
            float back;
            if (pair) {
                PacketCFloat.RatioPair out = new PacketCFloat.RatioPair();
                if (!PacketCFloat.readRatioPair(out, data, bits)) {
                    fail(name + ": the write did not read back");
                    continue;
                }
                if (Float.floatToRawIntBits(out.first) != Float.floatToRawIntBits(out.second)) {
                    fail(name + ": the second field read back " + out.second + ", the first " + out.first);
                    continue;
                }
                back = out.first;
            } else if (ratio) {
                PacketCFloat.RatioEndpoint out = new PacketCFloat.RatioEndpoint();
                if (!PacketCFloat.readRatioEndpoint(out, data, bits)) {
                    fail(name + ": the write did not read back");
                    continue;
                }
                back = out.ratio;
            } else {
                PacketCFloat.CFloat out = new PacketCFloat.CFloat();
                if (!PacketCFloat.readCFloat(out, data, bits)) {
                    fail(name + ": the write did not read back");
                    continue;
                }
                back = out.x;
            }
            if (Float.floatToRawIntBits(back) != Float.floatToRawIntBits(expect)) {
                fail(name
                        + ": read back "
                        + Float.floatToRawIntBits(back)
                        + ", want the quantized value "
                        + Float.floatToRawIntBits(expect));
                continue;
            }
            written++;
        }
        answer();
        if (cases == 0) {
            fail("no cases");
        }
        // a summary; tools/casegate holds every case of the file to an answer
        System.out.println("packet-write-cfloat Java (" + refused + " refused, " + written + " written): ran "
                + cases + ", failed " + failed);
        if (failed != 0) {
            System.exit(1);
        }
    }
}
