import java.nio.file.Files;
import java.nio.file.Path;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.Set;
import java.util.function.Consumer;
import packetrefusal.RefusalPacket;
import packetrefusal.RefusalPacket.Grade;
import packetrefusal.RefusalPacket.PacketWriteRefusal;
import packetrefusal.RefusalPacket.ShotType;

// write-refusal-packet on the Java leg (docs/SPEC.md §5.1): every row of the
// one shared list, test/conformance/write-refusal-packet/rows.txt
// (<name> <verdict> <op> <arg>, keyed here by name), starts from the default
// value, changes ONE field, and runs the packet writer under -ea. An
// AssertionError is the refusal; the lawful twin at the same boundary must be
// written. A row this driver has no setter for fails, and so does a setter it
// runs that the file does not name.
public final class Main {
    private static int failures;

    private static void fail(String what) {
        System.out.println("FAILED: write-refusal-packet " + what);
        failures++;
    }

    private static final Map<String, Consumer<PacketWriteRefusal>> SETTERS = new LinkedHashMap<>();

    static {
        // write-refusal-packet setters: begin
        SETTERS.put("scalar_high", v -> v.scalar = 11);
        SETTERS.put("scalar_low", v -> v.scalar = -1);
        SETTERS.put("scalar_at_max", v -> v.scalar = 10);
        SETTERS.put("scalar_at_min", v -> v.scalar = 0);
        SETTERS.put("bits_wide", v -> v.narrow = 200);
        SETTERS.put("bits_just_wide", v -> v.narrow = 128);
        SETTERS.put("bits_at_max", v -> v.narrow = 127);
        SETTERS.put("enum_high", v -> v.grade = 4);
        SETTERS.put("enum_at_max", v -> v.grade = Grade.GOLD);
        SETTERS.put("union_tag_high", v -> v.shot.type = 3);
        SETTERS.put("union_tag_at_max", v -> v.shot.type = ShotType.MAX);
        SETTERS.put("count_high", v -> v.windowCount = 9);
        SETTERS.put("count_low", v -> v.windowCount = 1);
        SETTERS.put("count_at_max", v -> v.windowCount = 8);
        SETTERS.put("count_at_min", v -> v.windowCount = 2);
        SETTERS.put("text_long", v -> v.textLength = 9);
        SETTERS.put("text_illformed", v -> {
            v.text[0] = (byte) 0xff;
            v.textLength = 1;
        });
        SETTERS.put("text_zero", v -> {
            v.text[0] = 0;
            v.textLength = 1;
        });
        SETTERS.put("text_at_max", v -> {
            java.util.Arrays.fill(v.text, (byte) 'a');
            v.textLength = 8;
        });
        SETTERS.put("const_wrong", v -> v.fixedConstant = 6);
        SETTERS.put("const_at", v -> v.fixedConstant = 5);
        SETTERS.put("reserved_wrong", v -> v.reservedZero = 1);
        SETTERS.put("reserved_at", v -> v.reservedZero = 0);
        SETTERS.put("flags_wide", v -> v.perks = 1L << 3);
        SETTERS.put("flags_wide_high", v -> v.perks = 1L << 40);
        SETTERS.put("flags_at_max", v -> v.perks = 7);
        // write-refusal-packet setters: end
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
            String[] parts = line.split("\\s+");
            if (parts.length != 4) {
                fail(": a row is not <name> <verdict> <op> <arg>: " + line);
                continue;
            }
            String name = parts[0];
            String verdict = parts[1];
            if (!listed.add(name)) {
                fail(name + ": named twice in rows.txt");
                continue;
            }
            Consumer<PacketWriteRefusal> set = SETTERS.get(name);
            if (set == null) {
                fail(name + ": a row in rows.txt this driver has no setter for");
                continue;
            }
            PacketWriteRefusal value = new PacketWriteRefusal();
            set.accept(value);
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
        for (String known : SETTERS.keySet()) {
            if (!listed.contains(known)) {
                fail(known + ": a setter this driver runs that rows.txt does not name");
            }
        }
        System.out.println("write-refusal-packet java: " + listed.size() + " rows, " + refused + " refused, " + written + " written");
        if (failures != 0) {
            System.exit(1);
        }
    }
}
