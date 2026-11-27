package rubytable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/ruby/rubyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"

	"github.com/mas-bandwidth/schema/v3/internal/testproc"
)

func generateWireFuzzUnit(t *testing.T, root, relDir string) map[string][]byte {
	t.Helper()
	u := loadUnitDir(t, filepath.Join(root, relDir))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestRubyWireFuzzVerdictPipeNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Ruby wire fuzz verdict pipe negative control")
	root, err := rubyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(root, "test/conformance/ruby/wirefuzz.rb"))
	if err != nil {
		t.Fatal(err)
	}
	anchor := "        refused = true\n"
	if strings.Count(string(src), anchor) != 1 {
		t.Fatal("negative control: refusal anchor missing or repeated")
	}
	files := generateWireFuzzUnit(t, root, "tables/pointers")
	dir := t.TempDir()
	writeTree(t, filepath.Join(dir, "generated", "graphdemo"), files)
	good := filepath.Join(dir, "good.rb")
	bad := filepath.Join(dir, "bad.rb")
	if err := os.WriteFile(good, src, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte(strings.Replace(string(src), anchor, "        refused = false\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	mutant := filepath.Join(dir, "empty.bin")
	if err := os.WriteFile(mutant, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	harness := filepath.Join(dir, "harness")
	build := testproc.Command(t, "go", "build", "-o", harness, "./test/conformance/harness")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build harness: %v: %s", err, out)
	}
	for _, rubyBin := range []string{rubyrun.Interpreter(t), rubyrun.Floor(t)} {
		for _, script := range []string{good, bad} {
			driver := fmt.Sprintf("%s --disable-gems -w %s %s", rubyBin, script, filepath.Join(dir, "generated"))
			cmd := testproc.Command(t, harness, "wire-fuzz", "--driver", driver, "--replay", mutant, "--unit", "graphdemo", "--root", "Scene")
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

func TestRubyWireFuzzRetainNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Ruby retaining verdict pipe negative control")
	root, err := rubyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(root, "test/conformance/ruby/wirefuzz.rb"))
	if err != nil {
		t.Fatal(err)
	}
	anchor := "value.save_retain(retained, report)"
	if strings.Count(string(src), anchor) != 1 {
		t.Fatal("retain control anchor missing or repeated")
	}
	files := generateWireFuzzUnit(t, root, "tables/pointers")
	dir := t.TempDir()
	writeTree(t, filepath.Join(dir, "generated", "graphdemo"), files)
	good, bad := filepath.Join(dir, "good.rb"), filepath.Join(dir, "bad.rb")
	if err := os.WriteFile(good, src, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte(strings.Replace(string(src), anchor, "value.save", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	harness := filepath.Join(dir, "harness")
	build := testproc.Command(t, "go", "build", "-o", harness, "./test/conformance/harness")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build harness: %v: %s", err, out)
	}
	vector := filepath.Join(root, "testdata/wire/tables/fuzz-vectors/retain_declared_default.bin")
	for _, rubyBin := range []string{rubyrun.Interpreter(t), rubyrun.Floor(t)} {
		for _, script := range []string{good, bad} {
			driver := fmt.Sprintf("%s --disable-gems -w %s %s", rubyBin, script, filepath.Join(dir, "generated"))
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

func TestRubyWireFuzzRetainLoss(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Ruby retained-loss verdict pipe")
	root, err := rubyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(root, "test/conformance/ruby/wirefuzz.rb"))
	if err != nil {
		t.Fatal(err)
	}
	anchor := "          value = cls.load_retain(data, retained, report)\n"
	if strings.Count(string(src), anchor) != 1 {
		t.Fatal("retain-loss control anchor changed")
	}
	files := generateWireFuzzUnit(t, root, "tables/pointers")
	dir := t.TempDir()
	writeTree(t, filepath.Join(dir, "generated", "graphdemo"), files)
	good, bad := filepath.Join(dir, "good.rb"), filepath.Join(dir, "bad.rb")
	if err := os.WriteFile(good, src, 0o644); err != nil {
		t.Fatal(err)
	}
	broken := anchor + "          raise 'retention loss hidden' if report.retain_lost.positive?\n"
	if err := os.WriteFile(bad, []byte(strings.Replace(string(src), anchor, broken, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	encoded := "01010c0474726565020402000000031102041104051107060c4d06070e080401000000010c0161091103000707080402000000000a0d0b0c03746f700c11050d1106000a080b0c046c656674000a090b0c057269676874000e0d0f04030000000b0c036d69640000861b638ebaadbcc437ea08982cc662bb020c9a5fcc128f0a95b31e51efb8255be84db4487b6d5feeffffffffffffffffa99fa6a599c80ef6ea0ce83094fde47c28f025a0ba6c31e51d434c78a3907eb93d62cb8fecfcf739b01c04a2ad70b02405d8145753aaaa76c8c204b4a28a8b9d66fe00649160807a0f00000000000000"
	wire, err := hex.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	vector := filepath.Join(dir, "loss.bin")
	if err := os.WriteFile(vector, wire, 0o644); err != nil {
		t.Fatal(err)
	}
	harness := filepath.Join(dir, "harness")
	build := testproc.Command(t, "go", "build", "-o", harness, "./test/conformance/harness")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build harness: %v: %s", err, out)
	}
	for _, rubyBin := range []string{rubyrun.Interpreter(t), rubyrun.Floor(t)} {
		for _, script := range []string{good, bad} {
			driver := fmt.Sprintf("%s --disable-gems -w %s %s", rubyBin, script, filepath.Join(dir, "generated"))
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
