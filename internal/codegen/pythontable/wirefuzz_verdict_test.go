package pythontable

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestPythonWireFuzzFileDispatch(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned interpreters")
	u := loadUnit(t, "package fuzzdispatch\ntable Variable { x int32 }\nfixed table Fixed { x int32 }\n")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	root, err := pyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(filepath.Join(root, "test/conformance/python/wirefuzz.py"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, filepath.Join(dir, "generated", "probe"), files)
	if err := os.WriteFile(filepath.Join(dir, "wirefuzz.py"), script, 0644); err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(u)
	var wires [][]byte
	for _, name := range []string{"Variable", "Fixed"} {
		value := model.New(model.Lookup(name))
		var report tabletext.Report
		if !model.Read(value, []byte(`{"x":7}`), &report) {
			t.Fatalf("Go JSON: %+v", report)
		}
		wire, err := tablewire.Encode(model, value)
		if err != nil {
			t.Fatal(err)
		}
		wires = append(wires, wire)
	}
	if wires[0][0] != 1 || wires[1][0] != 3 {
		t.Fatalf("wrong oracle form bytes %d %d", wires[0][0], wires[1][0])
	}
	var input bytes.Buffer
	binary.Write(&input, binary.LittleEndian, uint32(2))
	for _, name := range []string{"Variable", "Fixed"} {
		for _, word := range []string{"probe", name} {
			binary.Write(&input, binary.LittleEndian, uint16(len(word)))
			input.WriteString(word)
		}
		input.Write([]byte{1, 0})
	}
	for index, wire := range wires {
		binary.Write(&input, binary.LittleEndian, uint32(index))
		binary.Write(&input, binary.LittleEndian, uint32(len(wire)))
		input.Write(wire)
	}
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.TryStdin(t, pin, dir, "wirefuzz.py", []string{filepath.Join(dir, "generated")}, input.String())
		if err != nil {
			t.Fatalf("driver: %v %s", err, stderr)
		}
		data := []byte(out)
		if len(data) < 2 || data[0] != 1 || data[1] != 1 {
			t.Fatalf("missing roster: %x", data)
		}
		data = data[2:]
		for i, wire := range wires {
			if len(data) < 47+len(wire) || data[0] != 1 || !bytes.Equal(data[47:47+len(wire)], wire) {
				t.Fatalf("file dispatch %d: %x", i, data)
			}
			data = data[47+len(wire):]
		}
		if len(data) != 0 {
			t.Fatalf("extra output: %x", data)
		}
	}
}

func TestPythonWireFuzzVerdictPipe(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Python wire fuzz verdict pipe")
	root, err := pyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(filepath.Join(root, "test/conformance/python/wirefuzz.py"))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, "package fuzzprobe\ntable Root { x int32 }\n")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, filepath.Join(dir, "generated", "probe"), files)
	wire := wideEngineWire(t, u, "Root", `{"x":7}`)
	var input bytes.Buffer
	binary.Write(&input, binary.LittleEndian, uint32(1))
	for _, s := range []string{"probe", "Root"} {
		binary.Write(&input, binary.LittleEndian, uint16(len(s)))
		input.WriteString(s)
	}
	input.Write([]byte{1, 0})
	for _, b := range [][]byte{wire, {}} {
		binary.Write(&input, binary.LittleEndian, uint32(0))
		binary.Write(&input, binary.LittleEndian, uint32(len(b)))
		input.Write(b)
	}
	for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		if err := os.WriteFile(filepath.Join(dir, "wirefuzz.py"), script, 0644); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, err := pyrun.TryStdin(t, python, dir, "wirefuzz.py", []string{filepath.Join(dir, "generated")}, input.String())
		if err != nil {
			t.Fatalf("driver: %v: %s", err, stderr)
		}
		out := []byte(stdout)
		if len(out) != 1+47+len(wire)+47 || out[0] != 1 || out[1] != 1 || !bytes.Equal(out[48:48+len(wire)], wire) || out[48+len(wire)] != 0 || out[48+len(wire)+22] == 0 {
			t.Fatalf("unexpected replies: %x", out)
		}
	}
	units, err := readManifestUnits(filepath.Join(root, "testdata/conformance/tables/MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	generated := filepath.Join(dir, "corpus")
	compiler := filepath.Join(dir, "schema")
	buildCompiler := exec.Command("go", "build", "-tags", "schema_leg_python", "-o", compiler, "./cmd/schema")
	buildCompiler.Dir = root
	if out, err := buildCompiler.CombinedOutput(); err != nil {
		t.Fatalf("build Python compiler: %v: %s", err, out)
	}
	for _, entry := range units {
		args := []string{"generate", "--lang", "python", "--out", filepath.Join(generated, entry.key)}
		for _, path := range entry.paths {
			args = append(args, filepath.Join(root, path))
		}
		cmd := exec.Command(compiler, args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("generate %s: %v\n%s", entry.key, err, out)
		}
	}

	harness := filepath.Join(dir, "harness")
	cmd := exec.Command("go", "build", "-o", harness, "./test/conformance/harness")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build harness: %v: %s", err, out)
	}
	for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		for _, arm := range []string{"plain", "mixed-builder", "retain"} {
			args := []string{"wire-fuzz", "--driver", fmt.Sprintf("%s -W error -I -B %s %s", python, filepath.Join(dir, "wirefuzz.py"), generated), "--seed", "1", "--n", "200", "--failed", filepath.Join(dir, "failed.bin")}
			if arm == "mixed-builder" {
				args = append(args, "--mixed-builder")
			} else if arm == "retain" {
				args = append(args, "--retain")
			}
			cmd := exec.Command(harness, args...)
			cmd.Dir = root
			out, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(out), "0 divergences") || strings.Contains(string(out), "seeds absent") {
				t.Fatalf("%s %s: %v\n%s", python, arm, err, out)
			}
		}
	}
}

func TestPythonWireFuzzVerdictPipeNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Python wire fuzz verdict pipe negative control")
	root, err := pyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(root, "test/conformance/python/wirefuzz.py"))
	if err != nil {
		t.Fatal(err)
	}
	anchor := "            refused = True\n"
	if strings.Count(string(src), anchor) != 1 {
		t.Fatal("negative control: refusal anchor missing or repeated")
	}
	unit, err := loadManifestUnit(root, []string{"tables/examples"})
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, filepath.Join(dir, "generated", "tabledemo"), files)
	bad := filepath.Join(dir, "bad.py")
	good := filepath.Join(dir, "good.py")
	if err := os.WriteFile(good, src, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte(strings.Replace(string(src), anchor, "            refused = False\n", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	mutant := filepath.Join(dir, "empty.bin")
	if err := os.WriteFile(mutant, nil, 0644); err != nil {
		t.Fatal(err)
	}
	harness := filepath.Join(dir, "harness")
	build := exec.Command("go", "build", "-o", harness, "./test/conformance/harness")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build harness: %v: %s", err, out)
	}
	for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		for _, script := range []string{good, bad} {
			driver := fmt.Sprintf("%s -W error -I -B %s %s", python, script, filepath.Join(dir, "generated"))
			cmd := exec.Command(harness, "wire-fuzz", "--driver", driver, "--replay", mutant, "--unit", "tabledemo", "--root", "RootConfig")
			cmd.Dir = root
			out, err := cmd.CombinedOutput()
			if script == good && err != nil {
				t.Fatalf("clean replay: %v\n%s", err, out)
			}
			if script == bad && (err == nil || !strings.Contains(string(out), "the leg's report does not name the refusal")) {
				t.Fatalf("negative control did not detect missing named refusal: %v\n%s", err, out)
			}
		}
	}
}
