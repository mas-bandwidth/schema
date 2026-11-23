package javatable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestJavaReportResetDamageReasonAndCookCycle holds three Java ports to Go:
//
//   - load RESETS the Report at the start of every read, so a stale refused
//     flag in a reused Report cannot refuse a valid wire (SPEC-TABLES.md §3.3).
//   - framing damage names itself: reason wire_damaged, as Go's report does.
//   - cookMeasure and cookFrom REFUSE a cyclic pointer graph as data_cycle in
//     every build (SPEC-TABLES.md §6.5): a read-of-input check, not an assert.
func TestJavaReportResetDamageReasonAndCookCycle(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	t.Parallel()
	const schema = `package probe

table Var {
    a int32
    b int32
}

fixed table Fx {
    a int32
}

table Node {
    a    int32
    next *Node
}
`
	const driver = `import java.util.Arrays;
import probe.*;

public final class Main {
    private static void check(boolean cond, String msg) {
        if (!cond) throw new AssertionError(msg);
    }

    public static void main(String[] args) {
        // a stale refusal in a reused Report must not refuse a valid variable wire
        VarVariable.Value v = new VarVariable.Value();
        v.a = 7;
        v.b = 9;
        byte[] wire = VarVariable.save(v);
        check(wire != null, "save");
        TableVariable.Report rep = new TableVariable.Report();
        rep.refused = true;
        rep.reason = "stale";
        rep.unknown = 5;
        VarVariable.Value got = VarVariable.load(wire, rep);
        check(got != null && !rep.refused && rep.reason == null && rep.unknown == 0 && got.a == 7 && got.b == 9, "variable load reset");

        // the same for the fixed form
        FxFixed.Value fv = new FxFixed.Value();
        fv.a = 3;
        byte[] fw = new byte[FxFixed.measure(1)];
        check(FxFixed.save(new FxFixed.Value[]{fv}, 1, fw) == fw.length, "fixed save");
        FxFixed.Value[] back = {new FxFixed.Value()};
        TableFixed.Report fr = new TableFixed.Report();
        fr.refused = true;
        fr.unknown = 4;
        check(FxFixed.load(back, 1, fw, TableFixed.plan(1 << 10), new short[1 << 10], FxFixed.image(), fr) == 1, "fixed load reset: refused stays");
        check(!fr.refused && fr.unknown == 0 && back[0].a == 3, "fixed load report");

        // damage names itself wire_damaged
        TableVariable.Report dr = new TableVariable.Report();
        byte[] cut = Arrays.copyOf(wire, wire.length - 2);
        VarVariable.Value bad = VarVariable.load(cut, dr);
        check(bad == null && dr.damaged && "wire_damaged".equals(dr.reason), "load damage reason: " + dr.reason + " damaged=" + dr.damaged);
        TableVariable.Report jr = new TableVariable.Report();
        check(VarVariable.fromJson("{\"a\":1 \"b\"".getBytes(java.nio.charset.StandardCharsets.UTF_8), jr) == null
            && jr.damaged && "wire_damaged".equals(jr.reason), "fromJson damage reason: " + jr.reason);

        // a cyclic pointer graph is refused by cookMeasure and cookFrom, in every build
        NodeVariable.Value n = new NodeVariable.Value();
        n.a = 1;
        n.next = n;
        check(NodeCook.cookMeasure(n) == -1L, "cookMeasure cycle");
        NodeCook.MeasureReason mr = NodeCook.cookMeasureReason(n);
        check(mr.bytes() == -1L && "data_cycle".equals(mr.reason()), "cycle reason: " + mr.reason());
        check(!NodeCook.cookFrom(n, new byte[4096], TableCookWrite.LITTLE), "cookFrom cycle");
        NodeVariable.Value x = new NodeVariable.Value();
        NodeVariable.Value y = new NodeVariable.Value();
        x.next = y;
        y.next = x;
        check("data_cycle".equals(NodeCook.cookMeasureReason(x).reason()), "two-node cycle");
        // a shared (diamond) node is not a cycle
        NodeVariable.Value leaf = new NodeVariable.Value();
        NodeVariable.Value top = new NodeVariable.Value();
        top.next = leaf;
        check(NodeCook.cookMeasure(top) > 0, "chain measures");
        System.out.println("OK");
    }
}
`
	_, out := compileAndRunJavaProbe(t, schema, "probe", driver)
	if !strings.HasSuffix(out, "OK") {
		t.Fatalf("probe did not finish: %s", out)
	}
}
