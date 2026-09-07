import base64test.BytesTable;
import base64test.TableReport;
import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.util.Arrays;
import java.util.HexFormat;

public class Main {
    static String hex(byte[] bytes) {
        return bytes.length == 0 ? "-" : HexFormat.of().formatHex(bytes);
    }
    public static void main(String[] args) throws Exception {
        BufferedReader input = new BufferedReader(new InputStreamReader(System.in));
        String line;
        while ((line = input.readLine()) != null) {
            BytesTable.Blob value = new BytesTable.Blob();
            TableReport report = new TableReport();
            BytesTable.blobFromJson(value, HexFormat.of().parseHex(line), report);
            if (report.malformed) { System.out.println("1 0 0 - -"); continue; }
            byte[] out = new byte[(int) BytesTable.blobToJsonMeasure(value)];
            if (BytesTable.blobToJson(value, out) != out.length) { throw new AssertionError("writer size"); }
            System.out.println("0 " + report.clamped + " " + report.kindMismatch + " " + hex(Arrays.copyOf(value.payload, value.payloadLength)) + " " + hex(out));
        }
    }
}
