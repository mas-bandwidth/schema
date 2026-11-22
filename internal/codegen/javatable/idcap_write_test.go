package javatable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE WRITER REFUSES AN ID TABLE ITS READERS REFUSE (docs/SPEC-TABLES.md §3,
// ruling R8), for Java: the save-side id table used to grow to 65536. The unit
// is tablewire.IdCapSchema, so the three values need 4095, 4096 and 4097 ids:
// the first two write (the count word says so and the strict load accepts
// them), and the third answers null.
func TestJavaWriterRefusesMoreThan4096Ids(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	t.Parallel()
	const driver = `import idcap.*;

public final class Main {
    private static void check(boolean ok, String msg) {
        if (!ok) throw new AssertionError(msg);
    }

    private static RootVariable.Value value(int x, int y) {
        RootVariable.Value v = new RootVariable.Value();
        for (int i = 0; i < v.keys.length; i++) v.keys[i] = 1;
        v.x = (byte) x;
        v.y = (byte) y;
        return v;
    }

    private static int count(byte[] b) {
        int n = 0;
        for (int i = 0; i < 8; i++) n |= (b[b.length - 8 + i] & 0xff) << (8 * i);
        return n;
    }

    public static void main(String[] args) {
        int[][] rows = {{0, 0, 4095}, {1, 0, 4096}};
        for (int[] row : rows) {
            byte[] wire = RootVariable.save(value(row[0], row[1]));
            check(wire != null, row[2] + " ids: save refused");
            check(count(wire) == row[2], "id count " + count(wire) + ", want " + row[2]);
            TableVariable.Report rep = new TableVariable.Report();
            check(RootVariable.load(wire, rep) != null && !rep.damaged && !rep.refused, row[2] + " ids: load refused the file");
        }
        check(RootVariable.save(value(1, 1)) == null, "4097 ids: save wrote a file");
    }
}
`
	compileAndRunJavaProbe(t, tablewire.IdCapSchema(), "idcap", driver)
}
