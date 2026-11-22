import java.nio.file.DirectoryStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;

public final class Main {
    // one counted unit per vector: main prints "ran N, failed M" and
    // build/mustrun holds N to the count the recipe registers. A failed check
    // throws, so a run that reaches the count line failed nothing.
    static int ran = 0;

    static final List<String> KNOWN = List.of("exact.bin", "trailing.bin", "padding.bin");

    // The directory is held to the vectors this driver reads: a .bin it does
    // not read is red, so a vector added to the shared corpus cannot ride
    // unread. A named vector that is gone throws at its read.
    static void holdDirectory(Path dir) throws Exception {
        try (DirectoryStream<Path> listing = Files.newDirectoryStream(dir, "*.bin")) {
            for (Path path : listing) {
                String name = path.getFileName().toString();
                check(KNOWN.contains(name),
                        "FAILED: packet-end vector " + name + " is in " + dir + " and this driver does not read it");
            }
        }
    }

    static void check(boolean ok, String what) {
        if (!ok) {
            throw new AssertionError(what);
        }
    }

    public static void main(String[] args) throws Exception {
        Path dir = Path.of(args[0]);
        holdDirectory(dir);
        byte[] exact = Files.readAllBytes(dir.resolve("exact.bin"));
        byte[] trailing = Files.readAllBytes(dir.resolve("trailing.bin"));
        byte[] padding = Files.readAllBytes(dir.resolve("padding.bin"));

        packetend.PacketEnd.End value = new packetend.PacketEnd.End();
        check(packetend.PacketEnd.readEnd(value, exact, exact.length * 8),
                "FAILED: packet-end exact.bin refused");
        check(value.value == 3, "FAILED: packet-end exact.bin read " + value.value);
        ran++;

        check(!packetend.PacketEnd.readEnd(new packetend.PacketEnd.End(), trailing, trailing.length * 8),
                "FAILED: packet-end trailing.bin accepted");
        ran++;
        check(!packetend.PacketEnd.readEnd(new packetend.PacketEnd.End(), padding, padding.length * 8),
                "FAILED: packet-end padding.bin accepted");
        ran++;

        System.out.println("packet-end Java: exact accepted as 3, trailing and padding refused");
        System.out.println("packet-end-java: ran " + ran + ", failed 0");
    }
}
