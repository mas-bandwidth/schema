package goosetable_test

// THE WIRE-FUZZ VERDICT PIPE FOR GOOSE (docs/SPEC-TABLES.md §4.2, §6.6;
// test/conformance/README.md, "The wire-fuzz driver"): every mutant sent over
// the verdict pipe must produce the same strict acceptance verdict in the Goose
// leg (test/conformance/goose/wirefuzz.goose) and the Go oracle
// (internal/tablewire), and every accepted read must write back identical
// canonical bytes on both the plain (--builder) and retaining
// (--builder --retain) variable-table arms.
//
// TestGooseWireFuzzVerdictPipe checks make/goose.mk's gate registration and
// runs the real harness wire-fuzz verdict pipe over the Goose variable-table
// units in both modes. TestGooseWireFuzzVerdictPipeNegativeControl plants
// faults in make/goose.mk, in wire_load.goose (accepting a non-minimal LEB128
// the strict read refuses), and in test/conformance/goose/wirefuzz.goose
// (dropping retained unknown fields on the retaining write-back), and requires
// each plant to fail its check.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func gooseWireFuzzVerdictMakefileCheck(mk string) error {
	for _, want := range []string{
		"tables-goose-wire-fuzz-verdict:",
		"tables-goose-wire-fuzz-verdict-negative-control:",
		"TestGooseWireFuzzVerdictPipe$$",
		"TestGooseWireFuzzVerdictPipeNegativeControl$$",
		"test-goose: tables-goose-wire-fuzz-verdict tables-goose-wire-fuzz-verdict-negative-control",
	} {
		if !strings.Contains(mk, want) {
			return fmt.Errorf("make/goose.mk is missing %q", want)
		}
	}
	return nil
}

var (
	gooseWireFuzzHarnessOnce sync.Once
	gooseWireFuzzHarnessDir  string
	gooseWireFuzzHarnessBin  string
	gooseWireFuzzHarnessErr  error
)

func gooseWireFuzzHarness(t *testing.T) string {
	t.Helper()
	gooseWireFuzzHarnessOnce.Do(func() {
		gooseWireFuzzHarnessDir, gooseWireFuzzHarnessErr = os.MkdirTemp("", "goose-wirefuzz-harness-*")
		if gooseWireFuzzHarnessErr != nil {
			return
		}
		gooseWireFuzzHarnessBin = filepath.Join(gooseWireFuzzHarnessDir, "conformance-harness")
		cmd := exec.Command("go", "build", "-o", gooseWireFuzzHarnessBin, "./test/conformance/harness")
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			gooseWireFuzzHarnessErr = fmt.Errorf("build conformance-harness: %w\n%s", err, out)
		}
	})
	if gooseWireFuzzHarnessErr != nil {
		t.Fatal(gooseWireFuzzHarnessErr)
	}
	return gooseWireFuzzHarnessBin
}

func buildGooseWireFuzzLeg(t *testing.T, c *wireCorpus, key string, override map[string][]byte) string {
	t.Helper()
	files := wireUnitFiles(t, key, c.units[key], override)
	delete(files, "main.goose")
	files["conformance_unit.goose"] = append(files["conformance_unit.goose"], []byte(
		"fn conformance_message_root(root: const u8[:]) -> bool { return false; }\n"+
			"fn conformance_message_fuzz(root: const u8[:], data: const u8[:], out: u8[>..]&, rep: schema_rt::MsgReport&) -> i64 { return -1; }\n")...)
	for _, name := range []string{"wirefuzz.goose", "wirefuzz.build", "wirefuzz_io.c"} {
		if _, overridden := override[name]; overridden {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, "test/conformance/goose", name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = b
	}
	dir := t.TempDir()
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	goose, why := gooserun.Compiler()
	if goose == "" {
		t.Fatal(why)
	}
	leg := filepath.Join(dir, "wirefuzz")
	build := gooserun.Command(t, goose, "build", "-O2", "wirefuzz.goose", "-o", leg)
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build wirefuzz leg (%s): %v\n%s", key, err, out)
	}
	return leg
}

func runGooseWireFuzzVerdictPipe(t *testing.T, harness, leg, unit string, retain bool, n int) (string, error) {
	t.Helper()
	failed := filepath.Join(t.TempDir(), "failed.bin")
	args := []string{
		"wire-fuzz",
		"--builder",
	}
	if retain {
		args = append(args, "--retain")
	}
	args = append(args,
		"--driver", fmt.Sprintf("%s %s builder", leg, unit),
		"--seed", "1",
		"--n", fmt.Sprintf("%d", n),
		"--failed", failed,
	)
	cmd := exec.Command(harness, args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestGooseWireFuzzVerdictPipe(t *testing.T) {
	mkBytes, err := os.ReadFile(filepath.Join(root, "make", "goose.mk"))
	if err != nil {
		t.Fatal(err)
	}
	if err := gooseWireFuzzVerdictMakefileCheck(string(mkBytes)); err != nil {
		t.Fatal(err)
	}
	slowtest.Gate(t, "the Goose toolchain (it builds and runs the wire-fuzz verdict pipe)")
	harness := gooseWireFuzzHarness(t)
	c := readWireCorpus(t)
	units := []string{
		"blobdemo",
		"featurewitnesses",
		"listdemo",
		"mapdemo",
		"streamdemo",
		"tblg1",
		"tblp2",
		"tblw1",
		"tblw2",
		"widelist",
	}
	for _, unit := range units {
		t.Run(unit, func(t *testing.T) {
			t.Parallel()
			leg := buildGooseWireFuzzLeg(t, c, unit, nil)
			for _, retain := range []bool{false, true} {
				mode := "plain"
				if retain {
					mode = "retain"
				}
				out, err := runGooseWireFuzzVerdictPipe(t, harness, leg, unit, retain, 50)
				if err != nil {
					t.Fatalf("%s (%s): verdict pipe failed: %v\n%s", unit, mode, err, out)
				}
				if !strings.Contains(out, "0 divergences") {
					t.Fatalf("%s (%s): missing 0 divergences in output:\n%s", unit, mode, out)
				}
			}
		})
	}
}

func TestGooseWireFuzzVerdictPipeNegativeControl(t *testing.T) {
	mkBytes, err := os.ReadFile(filepath.Join(root, "make", "goose.mk"))
	if err != nil {
		t.Fatal(err)
	}
	mk := string(mkBytes)
	if err := gooseWireFuzzVerdictMakefileCheck(mk); err != nil {
		t.Fatalf("baseline make/goose.mk failed check: %v", err)
	}
	sabotagedMk := strings.ReplaceAll(mk, "tables-goose-wire-fuzz-verdict", "tables-goose-wire-fuzz-gone")
	if sabotagedMk == mk {
		t.Fatal("NEGATIVE CONTROL FAILED: make/goose.mk plant did not alter the text")
	}
	if err := gooseWireFuzzVerdictMakefileCheck(sabotagedMk); err == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: dropping tables-goose-wire-fuzz-verdict left the makefile check green")
	}

	slowtest.Gate(t, "the Goose toolchain (it builds and runs the wire-fuzz verdict pipe negative controls)")
	harness := gooseWireFuzzHarness(t)
	c := readWireCorpus(t)

	// Plant 1: remove the non-minimal LEB128 refusal from wire_load.goose so
	// the leg accepts a mutant that the strict oracle refuses.
	t.Run("strict-acceptance", func(t *testing.T) {
		t.Parallel()
		src := string(goosetable.Runtime()["wire_load.goose"])
		const anchor = "if i > 0 && b == 0 { return 0, o, false; }"
		if strings.Count(src, anchor) != 1 {
			t.Fatalf("wire_load.goose LEB128 anchor count = %d, want 1", strings.Count(src, anchor))
		}
		mutant := strings.Replace(src, anchor, "if false { return 0, o, false; }", 1)
		leg := buildGooseWireFuzzLeg(t, c, "tblw1", map[string][]byte{"wire_load.goose": []byte(mutant)})
		out, err := runGooseWireFuzzVerdictPipe(t, harness, leg, "tblw1", false, 50)
		if err == nil {
			t.Fatalf("NEGATIVE CONTROL FAILED: non-minimal LEB128 plant left tblw1 verdict pipe green:\n%s", out)
		}
		if !strings.Contains(out, "read acceptance differs: the leg says true, the oracle says false") {
			t.Fatalf("NEGATIVE CONTROL FAILED: tblw1 went red for unexpected reason:\n%s", out)
		}
	})

	// Plant 2: replace save_table_retain with save_table in test/conformance/
	// goose/wirefuzz.goose so the retaining arm drops unknown fields on
	// write-back.
	t.Run("retain-writeback", func(t *testing.T) {
		t.Parallel()
		wfBytes, err := os.ReadFile(filepath.Join(root, "test/conformance/goose/wirefuzz.goose"))
		if err != nil {
			t.Fatal(err)
		}
		wf := string(wfBytes)
		const anchor = "schema_rt::save_table_retain(unit, name, data, vals, nodes, root, store, report, saved)"
		if strings.Count(wf, anchor) != 1 {
			t.Fatalf("wirefuzz.goose save_table_retain anchor count = %d, want 1", strings.Count(wf, anchor))
		}
		mutant := strings.Replace(wf, anchor, "schema_rt::save_table(unit, name, data, vals, nodes, root, saved)", 1)
		leg := buildGooseWireFuzzLeg(t, c, "tblw1", map[string][]byte{"wirefuzz.goose": []byte(mutant)})
		out, err := runGooseWireFuzzVerdictPipe(t, harness, leg, "tblw1", true, 50)
		if err == nil {
			t.Fatalf("NEGATIVE CONTROL FAILED: dropping save_table_retain left tblw1 retaining verdict pipe green:\n%s", out)
		}
		if !strings.Contains(out, "the decoded value differs:") {
			t.Fatalf("NEGATIVE CONTROL FAILED: tblw1 retaining plant went red for unexpected reason:\n%s", out)
		}
	})
}
