package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPythonSurfaceCLI(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Probe.schema"), []byte("package probe\nfixed table Root { value int32 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out")
	run(t, bin, "generate", "--lang", "python", "--surface", "wire", "--out", out, dir)
	data, err := os.ReadFile(filepath.Join(out, "probe", "Probe.py"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Root.block = None") {
		t.Fatal("CLI did not select wire surface")
	}
	cmd := exec.Command(bin, "generate", "--lang", "python", "--surface", "bad", "--out", out, dir)
	result, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(result), "unknown surface") {
		t.Fatalf("bad surface: %v\n%s", err, result)
	}
	forms := t.TempDir()
	if err := os.WriteFile(filepath.Join(forms, "Probe.schema"), []byte("package probe\ntable Root | forms = \"wire,json\" { value int32 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(bin, "generate", "--lang", "python", "--surface", "wire,cook", "--out", out, forms)
	result, err = cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(result), "Root") || !strings.Contains(string(result), "cook") || !strings.Contains(string(result), "wire,json") {
		t.Fatalf("forms conflict: %v\n%s", err, result)
	}

}
