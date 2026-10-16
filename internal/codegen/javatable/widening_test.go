package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestWideningReasonsJava compiles two generations and reads the narrower
// writer's form-1 bytes with the newer reader. This exercises the emitted Java
// runtime rather than only checking generator text (§4).
func TestWideningReasonsJava(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaTools(t)
	oldFiles, err := Generate(unitFrom(t, "package oldv\ntable Sample { value int32 }\n"))
	if err != nil {
		t.Fatal(err)
	}
	newFiles, err := Generate(unitFrom(t, "package newv\ntable Sample { value int64 }\n"))
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
	if err := os.WriteFile(probe, []byte(`public final class Probe {
  public static void main(String[] args) {
    oldv.SampleVariable.Value old = new oldv.SampleVariable.Value();
    old.value = 37;
    byte[] wire = oldv.SampleVariable.save(old);
    newv.TableVariable.Report report = new newv.TableVariable.Report();
    newv.SampleVariable.Value widened = newv.SampleVariable.load(wire, report);
    if (widened == null || widened.value != 37L) throw new AssertionError("widened value");
    if (report.widened != 1 || !"widened".equals(report.reason)) throw new AssertionError("reason=" + report.reason + " widened=" + report.widened);
    newv.TableVariable.Report refused = new newv.TableVariable.Report();
    newv.TableVariable.open(new byte[] { 7, 0, 0, 0, 0, 0, 0, 0, 0 }, refused);
    if (!refused.refused || !"unknown_form".equals(refused.reason) || refused.malformed) throw new AssertionError("refusal=" + refused.reason);
    System.out.println("ok");
  }
}
`), 0o644); err != nil {
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
