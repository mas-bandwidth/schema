package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestJavaWideTextCook(t *testing.T) {
	slowtest.Gate(t, "javac and java (it checks UTF-16 cook values)")
	t.Parallel()
	javac, javaBin := javaTools(t)
	schema := filepath.Join(t.TempDir(), "Caption.schema")
	if err := os.WriteFile(schema, []byte("package widecook\ntable Caption { text wstring(6)\n tail int32 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	probe := `import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.util.Arrays;

public final class WideCookProbe {
    public static void main(String[] args) {
        widecook.CaptionVariable.Value value = new widecook.CaptionVariable.Value();
        for (String text : new String[]{"", "Aé中😀Z", "😀"}) {
            value.text = text.toCharArray();
            long size = widecook.CaptionCook.cookMeasure(value);
            if (size < 64) throw new AssertionError("wide value refused");
            for (int order : new int[]{1, 2}) {
                byte[] out = new byte[(int)size];
                if (!widecook.CaptionCook.cookFrom(value, out, order)) throw new AssertionError("write");
                ByteBuffer bytes = ByteBuffer.wrap(out).order(order == 1 ? ByteOrder.LITTLE_ENDIAN : ByteOrder.BIG_ENDIAN);
                int at = widecook.CaptionRow.textAt(64);
                int lengthAt = Math.toIntExact(64 + widecook.CaptionRow.cookInfo().fields[0].countOffset);
                if (bytes.getInt(lengthAt) != text.length()) throw new AssertionError("unit count");
                for (int i = 0; i < text.length(); i++) {
                    if (bytes.getChar(at + 2 * i) != text.charAt(i)) throw new AssertionError("UTF-16 order");
                }
                if (bytes.getChar(at + 2 * text.length()) != 0) throw new AssertionError("terminator");
                if (order == 1) {
                    widecook.CaptionCook cook = widecook.CaptionCook.open(out);
                    if (cook == null || widecook.CaptionRow.textLength(out, cook.root()) != text.length()) throw new AssertionError("row accessor");
                }
                byte[] shortOut = new byte[out.length - 1];
                Arrays.fill(shortOut, (byte)0x5a);
                if (widecook.CaptionCook.cookFrom(value, shortOut, order)) throw new AssertionError("short output accepted");
                for (byte b : shortOut) if (b != (byte)0x5a) throw new AssertionError("short output changed");
            }
        }
        for (char[] bad : new char[][]{new char[]{0}, new char[]{0xd800}, new char[]{0xdc00}, "1234567".toCharArray()}) {
            value.text = bad;
            if (widecook.CaptionCook.cookMeasure(value) >= 0) throw new AssertionError("invalid text measured");
            byte[] out = new byte[256]; Arrays.fill(out, (byte)0x5a);
            if (widecook.CaptionCook.cookFrom(value, out, 1)) throw new AssertionError("invalid text written");
            for (byte b : out) if (b != (byte)0x5a) throw new AssertionError("invalid text changed output");
        }
    }
}
`
	dir := compileCookUnit(t, javac, cookUnitFiles(t, schema), "widecook", map[string]string{"WideCookProbe": probe})
	for _, mode := range []string{"-ea", "-da"} {
		if out, err := exec.Command(javaBin, mode, "-cp", dir, "WideCookProbe").CombinedOutput(); err != nil {
			t.Fatalf("wide cook %s: %v\n%s", mode, err, out)
		}
	}
}
