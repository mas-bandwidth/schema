package main

import (
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
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

func TestPythonSurfaceCLIBehavior(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Probe.schema"), []byte("package probe\nfixed table Root { value int32 }\ntable Variable { value int32 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, selection := range []string{"wire", "fixed", ""} {
		t.Run(selection, func(t *testing.T) {
			out := t.TempDir()
			args := []string{"generate", "--lang", "python", "--out", out}
			if selection != "" {
				args = append(args, "--surface", selection)
			}
			args = append(args, dir)
			run(t, bin, args...)
			probe := pyrun.ImportPrelude + `from probe import Root,Variable,Refused,InvalidValue
v=Variable();v.value=4
wire=v.save();assert wire[0]==1 and Variable.load(wire)==v
r=Root();r.value=8
`
			if selection == "wire" {
				if _, err := os.Stat(filepath.Join(out, "probe", "_fixed.py")); !os.IsNotExist(err) {
					t.Fatal("wire-only fixed metadata")
				}
				probe += `assert not hasattr(Root,'fixed_save'), 'fixed batch surface leaked'
try:r.save()
except InvalidValue as e:assert 'fixed_surface_not_selected' in str(e)
else:raise AssertionError('fixed writer fell back to form 1')
try:Root.load(wire)
except Refused as e:assert e.reason=='fixed_surface_not_selected'
else:raise AssertionError('fixed reader accepted form 1')
`
			} else {
				probe += `wire=r.save();assert wire[0]==3 and Root.load(wire)==r and r.measure()==len(wire)
assert Root.fixed_save([r])==wire
`
			}
			probe += "print('cli-surface-ok')\n"
			for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				stdout, stderr, err := pyrun.Try(t, pin, out, "main.py", map[string][]byte{"main.py": []byte(probe)})
				if err != nil || string(stdout) != "cli-surface-ok\n" {
					t.Fatalf("%v\n%s\n%s", err, stdout, stderr)
				}
			}
		})
	}
}
