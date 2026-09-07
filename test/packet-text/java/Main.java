import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.util.Arrays;
import java.util.HexFormat;
import packettext.Text;

public final class Main {
    private static String hex(byte[] bytes) {
        return bytes.length == 0 ? "-" : HexFormat.of().formatHex(bytes);
    }
    public static void main(String[] args) throws Exception {
        BufferedReader input = new BufferedReader(new InputStreamReader(System.in));
        String line;
        while ((line = input.readLine()) != null) {
            byte[] raw = line.equals("-") ? new byte[0] : HexFormat.of().parseHex(line);
            byte[] wire = Arrays.copyOf(raw, (raw.length + 7) & ~7);
            Text.Narrow value = new Text.Narrow();
            if (!Text.readNarrow(value, wire, raw.length * 8)) { System.out.println("REFUSE"); continue; }
            int bits = Text.measureNarrow(value);
            if (!Text.readNarrow(new Text.Narrow(), wire, bits)) throw new AssertionError("exact-bit read refused");
            if (Text.readNarrow(new Text.Narrow(), wire, bits - 1)) throw new AssertionError("one-bit-short read accepted");
            byte[] encoded = new byte[256];
            int written = Text.writeNarrow(value, encoded);
            if (written != (bits + 7) / 8) throw new AssertionError("writer byte count");
            System.out.printf("OK %d %s %d %s%n", bits, hex(Arrays.copyOf(value.text, value.textLength)), bits, hex(Arrays.copyOf(encoded, written)));
        }
    }
}
