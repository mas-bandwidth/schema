package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The OLDER, NARROWER writer and the newer reader of one table: every field the
// later build widened (docs/SPEC-TABLES.md §4's rungs: a signed kind up its
// ladder, an unsigned kind up its own, f32 into f64), and a list whose element
// widened, which is §4.1's element case beside the field case. The same pair
// the JavaScript leg's TestWideningReasonsJavaScript reads.
const wideningOlderSchema = `package oldv

table Rec
{
    small  int16
    mid    int32
    count  uint8
    ratio  float32
    scores []int16
}
`

const wideningNewerSchema = `package newv

table Rec
{
    small  int32
    mid    int64
    count  uint32
    ratio  float64
    scores []int32
}
`

// TestWideningReasonsJava is the Java leg's direct widening test
// (docs/SPEC-TABLES.md §4, §4.1, §3, §6.5). It compiles both generations,
// writes with the older one and reads through the newer one, running the
// emitted Java runtime rather than checking generator text:
//
//   - an OLDER, NARROWER writer's bytes: five widenings, every value kept, and
//     the reason null, because a widening is a successful read and the
//     reference's TableReport.Reason (internal/codegen/gotable/gotable.go) is
//     empty for a file read;
//   - the REFUSALS, each named as the reference leg and the JavaScript leg name
//     it (gotable fixedform.go and message.go): form byte 2 is
//     `message_form_as_file`, form byte 3 (the fixed form, newer than this one)
//     and a byte no form defines are `newer_form`, each with refused set and no
//     counter moved;
//   - DAMAGE (a list count longer than the bytes left) moves the report and
//     names no reason, as Go's Load leaves it.
//
// The cook's and the block's TableRefuseReason names
// (internal/codegen/gotable/refusal.go: not_a_cook, foreign_order,
// wrong_build_version, reserved_not_zero, bad_alignment, truncated, bad_layout
// and unaligned_base, checked last) are judged by the cook-reason and
// block-reason conformance surfaces against the manifest's refusal rows.
func TestWideningReasonsJava(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaTools(t)
	oldFiles, err := Generate(unitFrom(t, wideningOlderSchema))
	if err != nil {
		t.Fatal(err)
	}
	newFiles, err := Generate(unitFrom(t, wideningNewerSchema))
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

const wideningProbe = `public final class Probe {
  static void check(boolean ok, String what) { if (!ok) throw new AssertionError(what); }

  static void refused(byte[] wire, String want) {
    newv.TableVariable.Report r = new newv.TableVariable.Report();
    newv.RecVariable.Value v = newv.RecVariable.load(wire, r);
    check(v == null, "form " + wire[0] + ": a refused read answered a value");
    check(r.refused && want.equals(r.reason), "form " + wire[0] + ": reason=" + r.reason + " refused=" + r.refused + " want " + want);
    check(!r.malformed && r.widened == 0 && r.unknown == 0 && r.kindMismatch == 0 && r.clamped == 0,
        "form " + wire[0] + ": a refusal moved a counter");
  }

  public static void main(String[] args) {
    oldv.RecVariable.Value old = new oldv.RecVariable.Value();
    old.small = (short) -300;
    old.mid = -70000;
    old.count = 200;
    old.ratio = 1.5f;
    old.scores.add(-2); old.scores.add(32767);
    byte[] wire = oldv.RecVariable.save(old);

    // An older, narrower writer through the newer reader: every value kept,
    // five widenings counted, and no reason (a widening is not a refusal).
    newv.TableVariable.Report report = new newv.TableVariable.Report();
    newv.RecVariable.Value v = newv.RecVariable.load(wire, report);
    check(v != null, "widened load returned null");
    check(v.small == -300, "small=" + v.small);
    check(v.mid == -70000L, "mid=" + v.mid);
    check(v.count == 200, "count=" + v.count);
    check(v.ratio == 1.5, "ratio=" + v.ratio);
    check(v.scores.size() == 2 && v.scores.get(0) == -2 && v.scores.get(1) == 32767, "scores=" + v.scores);
    check(report.widened == 5, "widened=" + report.widened);
    check(report.reason == null && !report.refused && !report.malformed, "widening reason=" + report.reason + " refused=" + report.refused + " malformed=" + report.malformed);
    check(report.unknown == 0 && report.kindMismatch == 0 && report.clamped == 0, "widening moved another counter");

    // The form byte refusals, by the reference's names (§3, §3.3).
    byte[] batch = wire.clone(); batch[0] = 2;
    refused(batch, "message_form_as_file");
    byte[] fixed = wire.clone(); fixed[0] = 3;
    refused(fixed, "newer_form");
    byte[] unassigned = wire.clone(); unassigned[0] = 7;
    refused(unassigned, "newer_form");

    // Damage names no reason: the list count (LEB 2, just before the two
    // int16 elements fe ff ff 7f) forged to 127 claims more than remains.
    int at = -1;
    for (int i = 1; i + 4 <= wire.length; i++) {
      if (wire[i] == (byte) 0xfe && wire[i+1] == (byte) 0xff && wire[i+2] == (byte) 0xff && wire[i+3] == 0x7f) { at = i; break; }
    }
    check(at > 0 && wire[at - 1] == 2, "list count not found at " + at);
    byte[] forged = wire.clone();
    forged[at - 1] = 0x7f;
    newv.TableVariable.Report damage = new newv.TableVariable.Report();
    newv.RecVariable.load(forged, damage);
    check(damage.reason == null, "damage reason=" + damage.reason);
    check(damage.refused || damage.malformed, "damage moved nothing");
    System.out.println("ok");
  }
}
`
