import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;
import java.util.HexFormat;
import packetwritetext.PacketText;
import packetwritetext.PacketText.Text;

public final class Main {
    private static int failures;

    private static void fail(String what) {
        System.out.println("FAILED: " + what);
        failures++;
    }

    private static byte[] parseHex(String text) {
        return text.equals("-") ? new byte[0] : HexFormat.of().parseHex(text.replace(" ", ""));
    }

    private static Text value(byte[] raw) {
        Text value = new Text();
        System.arraycopy(raw, 0, value.content, 0, raw.length);
        value.contentLength = raw.length;
        return value;
    }

    private static boolean same(Text value, byte[] raw) {
        return value.contentLength == raw.length
                && Arrays.equals(Arrays.copyOf(value.content, value.contentLength), raw);
    }

    public static void main(String[] args) throws Exception {
        for (String line : Files.readAllLines(Path.of(args[0]))) {
            line = line.trim();
            if (line.isEmpty() || line.startsWith("#")) {
                continue;
            }
            String[] parts = line.split("\\|");
            String name = parts[0].trim();
            byte[] raw = parseHex(parts[1].trim());
            String verdict = parts[2].trim();
            Text value = value(raw);
            if (verdict.equals("refused")) {
                try {
                    PacketText.writeText(value, new byte[PacketText.TEXT_MAX_BYTES]);
                    fail(name + " write accepted");
                } catch (AssertionError expected) {
                }
                continue;
            }
            byte[] wire = new byte[PacketText.TEXT_MAX_BYTES];
            int bits = PacketText.writeTextBits(value, wire, 0);
            Text back = new Text();
            if (PacketText.readTextBits(back, wire, 0, bits) < 0) {
                fail(name + " read refused");
                continue;
            }
            if (!same(back, raw)) {
                fail(name + " read back differs");
            }
        }
        if (failures != 0) {
            System.exit(1);
        }
    }
}
