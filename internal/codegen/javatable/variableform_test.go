package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestVariableFormRuntimeCompilesAndRoundTrips proves the variable-form runtime
// (TableVariable.java, docs/SPEC-TABLES.md §3) is valid Java and that its
// LEB128 reader/writer and id-table trailer round-trip: a probe writes a form-1
// wire (a field, the terminator, the trailer) and reads it back to the same
// values. This is the foundation the per-table load/save emitters build on; it
// pins the runtime before any codec names it.
func TestVariableFormRuntimeCompilesAndRoundTrips(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaTools(t)
	files, err := Generate(unitFrom(t, `package probe
table Card { id string(64) }
`))
	if err != nil {
		t.Fatal(err)
	}
	runtime, ok := files["TableVariable.java"]
	if !ok {
		t.Fatal("TableVariable.java not emitted for a variable-table unit")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(filepath.Join(src, "probe"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "probe", "TableVariable.java"), runtime, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "probe", "Probe.java"), []byte(variableRuntimeProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(javac, "--release", "17", "-d", classes,
		filepath.Join(src, "probe", "TableVariable.java"),
		filepath.Join(src, "probe", "Probe.java"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "probe.Probe").CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("probe: %s", out)
	}
}

// variableRuntimeProbe writes a form-1 wire by hand and reads it back through
// the runtime: one i32 field (kind 4) under reference 1, the zero terminator,
// then the id-table trailer. It also proves a non-canonical LEB128 is malformed
// and an unknown form byte is a refusal, not damage.
const variableRuntimeProbe = `package probe;

public final class Probe {
    public static void main(String[] args) {
        // Write: form byte 1, ref 1, kind 4 (i32), value 0x01020304, terminator.
        byte[] buf = new byte[64];
        TableVariable.Writer w = new TableVariable.Writer(buf, new TableVariable.Ids(16));
        w.put8(1);
        w.putLeb(w.ref(0x1122334455667788L));
        w.put8(4);
        w.put8(0x04); w.put8(0x03); w.put8(0x02); w.put8(0x01);
        w.put8(0);
        w.trailer();
        byte[] wire = java.util.Arrays.copyOf(buf, w.off);

        TableVariable.Report r = new TableVariable.Report();
        TableVariable.Reader rd = TableVariable.open(wire, r);
        if (r.refused || r.malformed) { throw new AssertionError("open refused/malformed"); }
        long ref = rd.leb();
        if (ref != 1) { throw new AssertionError("ref " + ref); }
        long id = rd.resolve(ref);
        if (id != 0x1122334455667788L) { throw new AssertionError("id " + Long.toHexString(id)); }
        int kind = rd.get8();
        if (kind != 4) { throw new AssertionError("kind " + kind); }
        int v = rd.get8() | rd.get8() << 8 | rd.get8() << 16 | rd.get8() << 24;
        if (v != 0x01020304) { throw new AssertionError("value " + Integer.toHexString(v)); }
        if (rd.leb() != 0) { throw new AssertionError("terminator"); }

        // A non-canonical LEB128 (0x80 0x00) is malformed.
        byte[] bad = new byte[] { 1, (byte) 0x80, 0x00, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0 };
        TableVariable.Report rb = new TableVariable.Report();
        TableVariable.Reader rdb = TableVariable.open(bad, rb);
        if (rdb == null) { throw new AssertionError("bad open"); }
        rdb.leb();
        if (!rb.malformed) { throw new AssertionError("non-canonical LEB not malformed"); }

        // An unknown form byte is a refusal, not damage.
        byte[] unknown = new byte[] { 7, 0, 0, 0, 0, 0, 0, 0, 0 };
        TableVariable.Report ru = new TableVariable.Report();
        TableVariable.open(unknown, ru);
        if (!ru.refused || ru.malformed) { throw new AssertionError("unknown form not a refusal"); }

        // A hand-built node table (§3.1): one *string record under the reserved
        // string type id — declared 1, type reference 2, size 3, body "abc".
        long[] nodeIds = new long[] { 0xffffffffffffffffL, 0x704be0d8faaffc58L };
        byte[] nodePay = new byte[] { 1, 2, 3, 'a', 'b', 'c' };
        TableVariable.Report rn = new TableVariable.Report();
        TableVariable.Reader rnr = new TableVariable.Reader(nodePay, nodeIds, rn);
        TableVariable.NodeRecord[] recs = TableVariable.nodes(rnr, false);
        if (rn.malformed || recs == null || recs.length != 1) { throw new AssertionError("nodes"); }
        if (recs[0].typeId != 0x704be0d8faaffc58L) { throw new AssertionError("node typeId"); }
        if (!java.util.Arrays.equals(recs[0].body, new byte[] { 'a', 'b', 'c' })) { throw new AssertionError("node body"); }
        if (TableVariable.node(recs, 2) != recs[0]) { throw new AssertionError("node resolve 2"); }
        if (TableVariable.node(recs, 0) != null || TableVariable.node(recs, 1) != null
                || TableVariable.node(recs, 3) != null) { throw new AssertionError("node resolve nulls"); }

        System.out.println("ok");
    }
}
`
