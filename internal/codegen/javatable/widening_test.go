package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestWideningReasonsJava compiles two generations of one table and reads the
// older, narrower writer's form-1 bytes through the newer reader, running the
// emitted Java runtime rather than checking generator text (docs/SPEC-TABLES.md
// §4, the report; §3, the form byte and its refusal).
//
// It pins the report's semantics to the reference leg's TableReport
// (internal/codegen/gotable/gotable.go): a widening keeps the value and moves
// the Widened counter, and leaves Reason empty, because Reason names a refusal
// and a widening is not one. A refusal names its reason with the reference
// leg's spelling (TableRefuse*.Error() in internal/codegen/gotable/refusal.go):
//
//	unknown_form       (TableRefuseUnknownForm)      — a form byte this reader does not carry
//	count_over_length  (TableRefuseCountOverLength)  — a list count longer than the bytes left
func TestWideningReasonsJava(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaTools(t)
	oldFiles, err := Generate(unitFrom(t, "package oldv\ntable Sample {\n    value int32\n    items []int32\n}\n"))
	if err != nil {
		t.Fatal(err)
	}
	newFiles, err := Generate(unitFrom(t, "package newv\ntable Sample {\n    value int64\n    items []int64\n}\n"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(pkg string, files map[string][]byte) []string {
		t.Helper()
		pkgDir := filepath.Join(dir, "src", pkg)
		if err := os.MkdirAll(pkgDir, 0o755); err != nil {
			t.Fatal(err)
		}
		var paths []string
		for name, body := range files {
			if filepath.Ext(name) != ".java" {
				continue
			}
			path := filepath.Join(pkgDir, name)
			if err := os.WriteFile(path, body, 0o644); err != nil {
				t.Fatal(err)
			}
			paths = append(paths, path)
		}
		return paths
	}
	sources := append(write("oldv", oldFiles), write("newv", newFiles)...)
	probe := filepath.Join(dir, "Probe.java")
	if err := os.WriteFile(probe, []byte(wideningProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	args = append(args, probe)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "Probe").CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	if string(out) != "ok\n" {
		t.Fatalf("probe: %s", out)
	}
}

// wideningProbe writes with the old (int32) generation and reads with the new
// (int64) one: the scalar and the list each widen once, keeping every value;
// then it forges the two refusals and checks each reason by name.
const wideningProbe = `public final class Probe {
  static void check(boolean ok, String what) { if (!ok) throw new AssertionError(what); }

  public static void main(String[] args) {
    oldv.SampleVariable.Value old = new oldv.SampleVariable.Value();
    old.value = -37;
    old.items.add(0x11223344); old.items.add(0x11223344);
    byte[] wire = oldv.SampleVariable.save(old);

    // An older, narrower writer through the newer reader: values kept,
    // widened counted once per field, no reason (a widening is not a refusal).
    newv.TableVariable.Report report = new newv.TableVariable.Report();
    newv.SampleVariable.Value widened = newv.SampleVariable.load(wire, report);
    check(widened != null, "widened load returned null");
    check(widened.value == -37L, "widened value=" + widened.value);
    check(widened.items.size() == 2 && widened.items.get(0) == 0x11223344L && widened.items.get(1) == 0x11223344L, "widened items=" + widened.items);
    check(report.widened == 2, "widened=" + report.widened);
    check(report.reason == null && !report.refused && !report.malformed, "widening reason=" + report.reason + " refused=" + report.refused + " malformed=" + report.malformed);
    check(report.unknown == 0 && report.kindMismatch == 0 && report.clamped == 0, "widening moved another counter");

    // unknown_form: a form byte this reader does not carry is refused by name
    // and moves no counter (§3).
    newv.TableVariable.Report form = new newv.TableVariable.Report();
    newv.TableVariable.open(new byte[] { 7, 0, 0, 0, 0, 0, 0, 0, 0 }, form);
    check(form.refused && "unknown_form".equals(form.reason) && !form.malformed && form.widened == 0, "form reason=" + form.reason);

    // count_over_length: the list's count (LEB 2, just before the two
    // elements) forged to 127 claims more elements than bytes remain.
    int at = -1;
    for (int i = 1; i + 8 <= wire.length; i++) {
      if (wire[i] == 0x44 && wire[i+1] == 0x33 && wire[i+2] == 0x22 && wire[i+3] == 0x11
          && wire[i+4] == 0x44 && wire[i+5] == 0x33 && wire[i+6] == 0x22 && wire[i+7] == 0x11) { at = i; break; }
    }
    check(at > 0 && wire[at - 1] == 2, "list count not found at " + at);
    byte[] forged = wire.clone();
    forged[at - 1] = 0x7f;
    newv.TableVariable.Report count = new newv.TableVariable.Report();
    newv.SampleVariable.load(forged, count);
    check(count.refused && "count_over_length".equals(count.reason), "count reason=" + count.reason + " refused=" + count.refused);
    System.out.println("ok");
  }
}
`
