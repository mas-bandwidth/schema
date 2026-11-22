package pythontable

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// TestPythonWireFuzzRetainLoss preserves the oracle's reported loss rather
// than treating every unretained record as an exhausted caller buffer.
func TestPythonWireFuzzRetainLoss(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Python retained-loss verdict pipe")
	root, err := pyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(root, "test/conformance/python/wirefuzz.py"))
	if err != nil {
		t.Fatal(err)
	}
	anchor := "                value = cls.load_retain(data, retained, report)\n"
	if strings.Count(string(src), anchor) != 1 {
		t.Fatal("retain-loss control anchor changed")
	}
	u, err := loadManifestUnit(root, []string{"tables/pointers"})
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, filepath.Join(dir, "generated", "graphdemo"), files)
	good, bad := filepath.Join(dir, "good.py"), filepath.Join(dir, "bad.py")
	if err := os.WriteFile(good, src, 0644); err != nil {
		t.Fatal(err)
	}
	broken := anchor + "                if report.retain_lost: raise RuntimeError('retention loss hidden')\n"
	if err := os.WriteFile(bad, []byte(strings.Replace(string(src), anchor, broken, 1)), 0644); err != nil {
		t.Fatal(err)
	}
	encoded := "01010c0474726565020402000000031102041104051107060c4d06070e080401000000010c0161091103000707080402000000000a0d0b0c03746f700c11050d1106000a080b0c046c656674000a090b0c057269676874000e0d0f04030000000b0c036d69640000861b638ebaadbcc437ea08982cc662bb020c9a5fcc128f0a95b31e51efb8255be84db4487b6d5feeffffffffffffffffa99fa6a599c80ef6ea0ce83094fde47c28f025a0ba6c31e51d434c78a3907eb93d62cb8fecfcf739b01c04a2ad70b02405d8145753aaaa76c8c204b4a28a8b9d66fe00649160807a0f00000000000000"
	wire, err := hex.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	vector := filepath.Join(dir, "loss.bin")
	if err := os.WriteFile(vector, wire, 0644); err != nil {
		t.Fatal(err)
	}
	harness := filepath.Join(dir, "harness")
	build := testproc.Command(t, "go", "build", "-o", harness, "./test/conformance/harness")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build harness: %v: %s", err, out)
	}
	for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		for _, script := range []string{good, bad} {
			driver := fmt.Sprintf("%s -W error -I -B %s %s", python, script, filepath.Join(dir, "generated"))
			cmd := testproc.Command(t, harness, "wire-fuzz", "--retain", "--driver", driver, "--replay", vector, "--unit", "graphdemo", "--root", "Scene", "--failed", filepath.Join(dir, "failed.bin"))
			cmd.Dir = root
			out, err := cmd.CombinedOutput()
			if script == good && (err != nil || !strings.Contains(string(out), "0 divergences")) {
				t.Fatalf("retained-loss replay: %v\n%s", err, out)
			}
			if script == bad && (err == nil || !strings.Contains(string(out), "retention loss hidden")) {
				t.Fatalf("retained-loss fault escaped: %v\n%s", err, out)
			}
		}
	}
}

// TestPythonWireFuzzCIPartition requires each real make support target for
// every interpreter, execution mode and arm, without reducing its roster.
func TestPythonWireFuzzCIPartition(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "make wire-fuzz support partition")
	root, err := pyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	run := func(target string) []string {
		t.Helper()
		cmd := testproc.Command(t, "make", "-s", target)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("make %s: %v\n%s", target, err, out)
		}
		return strings.Fields(string(out))
	}
	rows, support := run("python-wirefuzz-ci-list"), run("python-ci-support-list")
	if issue := wireFuzzPartitionIssue(rows, support); issue != "" {
		t.Fatal(issue)
	}
}

// wireFuzzCIShards is the number of CI shards per interpreter, mode and arm
// (PYTHON_WIRE_FUZZ_CI_SHARDS in make/python-wirefuzz.mk).
const wireFuzzCIShards = 8

func wireFuzzPartitionIssue(rows, support []string) string {
	count := func(values []string, want string) int {
		n := 0
		for _, value := range values {
			if value == want {
				n++
			}
		}
		return n
	}
	if want := 12 * wireFuzzCIShards; len(rows) != want {
		return fmt.Sprintf("wire-fuzz partition has %d rows, want %d", len(rows), want)
	}
	for _, pin := range []string{"current", "floor"} {
		for _, mode := range []string{"normal", "dev"} {
			for _, arm := range []string{"plain", "mixed", "retain"} {
				for shard := range wireFuzzCIShards {
					row := fmt.Sprintf("python-wirefuzz-%s-%s-%s-shard-%d", pin, mode, arm, shard)
					if count(rows, row) != 1 || count(support, row) != 1 {
						return "missing or duplicate wire-fuzz support row " + row
					}
				}
			}
		}
	}
	return ""
}

// TestPythonWireFuzzCIPartitionNegativeControl removes an arm from a copy of
// the actual make fragment and requires the partition check to name the gap.
func TestPythonWireFuzzCIPartitionNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "make wire-fuzz support partition control")
	root, err := pyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(root, "make/python-wirefuzz.mk"))
	if err != nil {
		t.Fatal(err)
	}
	anchor := "foreach arm,plain mixed retain,"
	if strings.Count(string(src), anchor) != 3 {
		t.Fatal("wire-fuzz partition control anchor changed")
	}
	planted := strings.ReplaceAll(string(src), anchor, "foreach arm,plain mixed,")
	planted += "\npython-ci-support-list:\n\t@printf '%s\\n' $(PYTHON_CI_SUPPORT_GROUPS)\n"
	file := filepath.Join(t.TempDir(), "Makefile")
	if err := os.WriteFile(file, []byte(planted), 0644); err != nil {
		t.Fatal(err)
	}
	run := func(target string) []string {
		t.Helper()
		cmd := testproc.Command(t, "make", "-s", "-f", file, target)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("planted make %s: %v\n%s", target, err, out)
		}
		return strings.Fields(string(out))
	}
	want := fmt.Sprintf("wire-fuzz partition has %d rows, want %d", 8*wireFuzzCIShards, 12*wireFuzzCIShards)
	issue := wireFuzzPartitionIssue(run("python-wirefuzz-ci-list"), run("python-ci-support-list"))
	if issue == "" {
		t.Fatal("NEGATIVE CONTROL FAILED: the partition check accepted a make fragment with the retain arm removed")
	}
	if issue != want {
		t.Fatalf("the partition check named the wrong gap: %q, want %q", issue, want)
	}
}

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
	runPythonWireFuzzVerdictArm(t, root, dir, script, "plain")
}

func TestPythonWireFuzzVerdictPipeMixedBuilder(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Python wire fuzz verdict pipe (mixed-builder arm)")
	root, err := pyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(filepath.Join(root, "test/conformance/python/wirefuzz.py"))
	if err != nil {
		t.Fatal(err)
	}
	runPythonWireFuzzVerdictArm(t, root, t.TempDir(), script, "mixed-builder")
}

func TestPythonWireFuzzVerdictPipeRetain(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Python wire fuzz verdict pipe (retain arm)")
	root, err := pyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(filepath.Join(root, "test/conformance/python/wirefuzz.py"))
	if err != nil {
		t.Fatal(err)
	}
	runPythonWireFuzzVerdictArm(t, root, t.TempDir(), script, "retain")
}

func runPythonWireFuzzVerdictArm(t *testing.T, root, dir string, script []byte, arm string) {
	t.Helper()
	driverScript := filepath.Join(dir, "wirefuzz.py")
	if err := os.WriteFile(driverScript, script, 0o644); err != nil {
		t.Fatal(err)
	}
	units, err := readManifestUnits(filepath.Join(root, "testdata/conformance/tables/MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	generated := filepath.Join(dir, "corpus")
	compiler := filepath.Join(dir, "schema")
	harness := filepath.Join(dir, "harness")
	var buildWG sync.WaitGroup
	var compilerErr, harnessErr string
	buildWG.Go(func() {
		cmd := testproc.Command(t, "go", "build", "-o", compiler, "./cmd/schema")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			compilerErr = fmt.Sprintf("build Python compiler: %v: %s", err, out)
		}
	})
	buildWG.Go(func() {
		cmd := testproc.Command(t, "go", "build", "-o", harness, "./test/conformance/harness")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			harnessErr = fmt.Sprintf("build harness: %v: %s", err, out)
		}
	})
	buildWG.Wait()
	if compilerErr != "" {
		t.Fatal(compilerErr)
	}
	if harnessErr != "" {
		t.Fatal(harnessErr)
	}

	workers := max(1, runtime.GOMAXPROCS(0))
	sem := make(chan struct{}, workers)
	var genWG sync.WaitGroup
	var genMu sync.Mutex
	var genErr string
	for _, entry := range units {
		genWG.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			args := []string{"generate", "--lang", "python", "--out", filepath.Join(generated, entry.key)}
			for _, path := range entry.paths {
				args = append(args, filepath.Join(root, path))
			}
			cmd := testproc.Command(t, compiler, args...)
			cmd.Dir = root
			if out, err := cmd.CombinedOutput(); err != nil {
				genMu.Lock()
				if genErr == "" {
					genErr = fmt.Sprintf("generate %s: %v\n%s", entry.key, err, out)
				}
				genMu.Unlock()
			}
		})
	}
	genWG.Wait()
	if genErr != "" {
		t.Fatal(genErr)
	}

	pythons := []string{pyrun.Interpreter(t), pyrun.Floor(t)}
	// Precompile .pyc bytecode once per interpreter inside isolated dir (t.TempDir())
	// so the 8 shard subprocesses do not re-parse all 31 generated Python packages 8x.
	for _, python := range pythons {
		c := testproc.Command(t, python, "-W", "error", "-I", "-m", "compileall", "-q", "-j", strconv.Itoa(workers), generated, driverScript)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("compileall (%s): %v\n%s", filepath.Base(python), err, out)
		}
	}

	var runWG sync.WaitGroup
	var runMu sync.Mutex
	var runErr string
	for pi, python := range pythons {
		for shard := range wireFuzzCIShards {
			runWG.Go(func() {
				sem <- struct{}{}
				defer func() { <-sem }()
				failed := filepath.Join(dir, fmt.Sprintf("failed-%d-%s-%d.bin", pi, arm, shard))
				args := []string{
					"wire-fuzz",
					"--driver", fmt.Sprintf("%s -W error -I %s %s", python, driverScript, generated),
					"--seed", "1", "--n", "200",
					"--shard", strconv.Itoa(shard), "--shards", strconv.Itoa(wireFuzzCIShards),
					"--failed", failed,
				}
				switch arm {
				case "mixed-builder":
					args = append(args, "--mixed-builder")
				case "retain":
					args = append(args, "--retain")
				}
				cmd := testproc.Command(t, harness, args...)
				cmd.Dir = root
				out, err := cmd.CombinedOutput()
				if err != nil || !strings.Contains(string(out), "0 divergences") || strings.Contains(string(out), "seeds absent") {
					runMu.Lock()
					if runErr == "" {
						runErr = fmt.Sprintf("%s %s shard %d/%d: %v\n%s", python, arm, shard, wireFuzzCIShards, err, out)
					}
					runMu.Unlock()
				}
			})
		}
	}
	runWG.Wait()
	if runErr != "" {
		t.Fatal(runErr)
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
	build := testproc.Command(t, "go", "build", "-o", harness, "./test/conformance/harness")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build harness: %v: %s", err, out)
	}
	for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		for _, script := range []string{good, bad} {
			driver := fmt.Sprintf("%s -W error -I -B %s %s", python, script, filepath.Join(dir, "generated"))
			cmd := testproc.Command(t, harness, "wire-fuzz", "--driver", driver, "--replay", mutant, "--unit", "tabledemo", "--root", "RootConfig")
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

// TestPythonWireFuzzRetainNegativeControl plants a real retaining-save fault
// in the driver and requires the shared oracle to observe the lost record.
func TestPythonWireFuzzRetainNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Python retaining verdict pipe negative control")
	root, err := pyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(root, "test/conformance/python/wirefuzz.py"))
	if err != nil {
		t.Fatal(err)
	}
	anchor := "saved = value.save_retain(retained, report)"
	if strings.Count(string(src), anchor) != 1 {
		t.Fatal("retain control anchor missing or repeated")
	}
	u, err := loadManifestUnit(root, []string{"tables/pointers"})
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, filepath.Join(dir, "generated", "graphdemo"), files)
	good, bad := filepath.Join(dir, "good.py"), filepath.Join(dir, "bad.py")
	if err := os.WriteFile(good, src, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte(strings.Replace(string(src), anchor, "saved = value.save()", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	harness := filepath.Join(dir, "harness")
	build := testproc.Command(t, "go", "build", "-o", harness, "./test/conformance/harness")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build harness: %v: %s", err, out)
	}
	vector := filepath.Join(root, "testdata/wire/tables/fuzz-vectors/retain_declared_default.bin")
	for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		for _, script := range []string{good, bad} {
			driver := fmt.Sprintf("%s -W error -I -B %s %s", python, script, filepath.Join(dir, "generated"))
			cmd := testproc.Command(t, harness, "wire-fuzz", "--retain", "--driver", driver, "--replay", vector, "--unit", "graphdemo", "--root", "Scene", "--failed", filepath.Join(dir, "failed.bin"))
			cmd.Dir = root
			out, err := cmd.CombinedOutput()
			if script == good && err != nil {
				t.Fatalf("clean retaining replay: %v\n%s", err, out)
			}
			if script == bad && (err == nil || !strings.Contains(string(out), "the decoded value differs")) {
				t.Fatalf("retaining-save fault escaped: %v\n%s", err, out)
			}
		}
	}
}
