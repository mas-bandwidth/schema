import java.nio.file.Files;
import java.nio.file.Path;

public final class Main {
    static void check(boolean ok, String what) {
        if (!ok) {
            throw new AssertionError(what);
        }
    }

    public static void main(String[] args) throws Exception {
        Path dir = Path.of(args[0]);
        byte[] exact = Files.readAllBytes(dir.resolve("exact.bin"));
        byte[] trailing = Files.readAllBytes(dir.resolve("trailing.bin"));
        byte[] padding = Files.readAllBytes(dir.resolve("padding.bin"));

        packetend.PacketEnd.End value = new packetend.PacketEnd.End();
        check(packetend.PacketEnd.readEnd(value, exact, exact.length * 8),
                "FAILED: packet-end exact.bin refused");
        check(value.value == 3, "FAILED: packet-end exact.bin read " + value.value);

        check(!packetend.PacketEnd.readEnd(new packetend.PacketEnd.End(), trailing, trailing.length * 8),
                "FAILED: packet-end trailing.bin accepted");
        check(!packetend.PacketEnd.readEnd(new packetend.PacketEnd.End(), padding, padding.length * 8),
                "FAILED: packet-end padding.bin accepted");

        System.out.println("packet-end Java: exact accepted as 3, trailing and padding refused");
    }
}
