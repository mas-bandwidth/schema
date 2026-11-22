import java.nio.file.Files;
import java.nio.file.Path;
import java.util.HashSet;
import java.util.Set;
import packetwriterefusal.RefusalPacket;
import packetwriterefusal.RefusalPacket.Grade;
import packetwriterefusal.RefusalPacket.PacketWriteRefusal;
import packetwriterefusal.RefusalPacket.ShotType;

// write-refusal-packet on the Java leg (docs/SPEC.md §5.1): every case starts
// from the default value, changes ONE field, and runs the packet writer under
// -ea. An AssertionError is the refusal; the lawful twin at the same boundary
// must be written. A case in cases.txt this driver does not set fails, so
// does a case it sets that the file does not list.
public final class Main {
    private static int failures;

    private static void fail(String what) {
        System.out.println("FAILED: write-refusal-packet " + what);
        failures++;
    }

    // set applies the case's one change, or returns false for a name it does not know.
    private static boolean set(String name, PacketWriteRefusal v) {
        switch (name) {
            case "scalar_high" -> v.scalar = 11;
            case "scalar_low" -> v.scalar = -1;
            case "scalar_at_max" -> v.scalar = 10;
            case "scalar_at_min" -> v.scalar = 0;
            case "bits_wide" -> v.narrow = 200;
            case "bits_at_max" -> v.narrow = 127;
            case "enum_high" -> v.grade = 4;
            case "enum_at_max" -> v.grade = Grade.GOLD;
            case "union_tag_high" -> v.shot.type = 3;
            case "union_tag_at_max" -> v.shot.type = ShotType.MAX;
            case "count_high" -> v.windowCount = 9;
            case "count_low" -> v.windowCount = 1;
            case "count_at_max" -> v.windowCount = 8;
            case "count_at_min" -> v.windowCount = 2;
            case "text_long" -> v.textLength = 9;
            case "text_illformed" -> {
                v.text[0] = (byte) 0xff;
                v.textLength = 1;
            }
            case "text_zero" -> {
                v.text[0] = 0;
                v.textLength = 1;
            }
            case "text_at_max" -> {
                java.util.Arrays.fill(v.text, (byte) 'a');
                v.textLength = 8;
            }
            case "const_wrong" -> v.fixedConstant = 6;
            case "const_at" -> v.fixedConstant = 5;
            case "reserved_wrong" -> v.reservedZero = 1;
            case "reserved_at" -> v.reservedZero = 0;
            default -> {
                return false;
            }
        }
        return true;
    }

    public static void main(String[] args) throws Exception {
        boolean enabled = false;
        assert enabled = true;
        if (!enabled) {
            System.out.println("FAILED: write-refusal-packet: run with -ea, the write checks are debug asserts");
            System.exit(1);
        }
        Set<String> listed = new HashSet<>();
        int refused = 0;
        int written = 0;
        for (String line : Files.readAllLines(Path.of(args[0]))) {
            line = line.trim();
            if (line.isEmpty() || line.startsWith("#")) {
                continue;
            }
            String[] parts = line.split("\\|");
            String name = parts[0].trim();
            String verdict = parts[1].trim();
            listed.add(name);
            PacketWriteRefusal value = new PacketWriteRefusal();
            if (!set(name, value)) {
                fail(name + ": neither run nor named not representable in Java");
                continue;
            }
            byte[] data = new byte[RefusalPacket.PACKET_WRITE_REFUSAL_MAX_BYTES];
            if (verdict.equals("refused")) {
                try {
                    RefusalPacket.writePacketWriteRefusal(value, data);
                    fail(name + ": out-of-contract value was written");
                } catch (AssertionError expected) {
                    refused++;
                }
            } else if (verdict.equals("wrote")) {
                try {
                    RefusalPacket.writePacketWriteRefusal(value, data);
                    written++;
                } catch (AssertionError e) {
                    fail(name + ": the lawful value at the boundary was refused");
                }
            } else {
                fail(name + ": unknown verdict " + verdict);
            }
        }
        for (String known : new String[] {"scalar_high", "bits_wide", "enum_high", "union_tag_high", "count_high",
                "text_long", "text_illformed", "text_zero", "const_wrong", "reserved_wrong"}) {
            if (!listed.contains(known)) {
                fail(known + ": missing from cases.txt");
            }
        }
        System.out.println("write-refusal-packet java: " + refused + " refused, " + written + " written");
        if (failures != 0) {
            System.exit(1);
        }
    }
}
