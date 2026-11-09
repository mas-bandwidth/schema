package rusttable

// THE WIRE-FUZZ VERDICT PIPE FOR RUST (docs/SPEC-TABLES.md §4.2, §6.5, §6.6;
// test/conformance/README.md, "The wire-fuzz driver"): every mutant sent over
// the verdict pipe must produce the same strict acceptance verdict in the Rust
// card leg and the Go oracle (internal/tablewire), and every accepted read must
// write back identical canonical bytes on the plain and retaining arms.
//
// TestRustWireFuzzVerdictPipe checks make/rust.mk's card wire-fuzz gate
// registrations, generates the card modules in-process through Generate,
// compiles test/conformance/rust/src/wire_fuzz.rs beside those real generated
// artifacts, and drives conformance-harness wire-fuzz over the Card root in
// plain and --retain modes.
// TestRustWireFuzzVerdictPipeNegativeControl plants faults in the real
// generated card_table.rs — the array-body tail check and the repeated-field-id
// check — and requires each plant to turn the verdict pipe red.
//
// THIS IS A FUNCTIONAL TEST: it compiles and runs the emitted Rust with rustc,
// so it takes a real toolchain and names it through slowtest.Gate.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func rustWireFuzzVerdictMakefileCheck(mk string) error {
	for _, want := range []string{
		"cards-rust-wire-fuzz: build/conformance-harness build/conformance-rust-card",
		"./build/conformance-harness wire-fuzz \\",
		"--manifest tables/cards/fuzz/MANIFEST.txt --json tables/cards/fuzz --vectors '' \\",
		`--driver 'build/conformance-rust-card wire-fuzz' --seed 24845619678 --n 100`,
		"cards-rust-retain-fuzz: build/conformance-harness build/conformance-rust-card",
		"--retain --seed 24845619678 --n 100",
		"test-rust: cards-rust-wire-fuzz cards-rust-retain-fuzz",
	} {
		if !strings.Contains(mk, want) {
			return fmt.Errorf("make/rust.mk is missing %q", want)
		}
	}
	return nil
}

func rustWireFuzzRepo(t *testing.T) string {
	t.Helper()
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

// rustWireFuzzCardModules generates the real card modules in-process, the same
// emitter output the make target writes under build/tables-generated-rust.
func rustWireFuzzCardModules(t *testing.T, repo string) map[string][]byte {
	t.Helper()
	modules, err := Generate(generateUnit(t, repo, []string{"tables/cards"}))
	if err != nil {
		t.Fatalf("generate the card modules: %v", err)
	}
	for _, name := range []string{"variable_runtime.rs", "card_table.rs", "card_variable.rs", "table_enums.rs"} {
		if _, ok := modules[name]; !ok {
			t.Fatalf("the card generator emitted no %s; got %v", name, keys(modules))
		}
	}
	return modules
}

// rustWireFuzzLeg writes the generated modules beside the hand-written driver
// and builds it with rustc. overrides replace a generated module's bytes, which
// is how the negative control plants its fault in the real artifact.
func rustWireFuzzLeg(t *testing.T, repo string, modules map[string][]byte, overrides map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range modules {
		if !strings.HasSuffix(name, ".rs") {
			continue
		}
		if replacement, ok := overrides[name]; ok {
			data = replacement
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	driver, err := os.ReadFile(filepath.Join(repo, "test/conformance/rust/src/wire_fuzz.rs"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), driver, 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "wire-fuzz")
	cmd := exec.Command("rustc", "--edition", "2024", "-O", "-o", bin, "main.rs")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the generated card codec did not build: %v\n%s", err, out)
	}
	return bin
}

func rustWireFuzzHarness(t *testing.T, repo string) string {
	t.Helper()
	harness := filepath.Join(t.TempDir(), "conformance-harness")
	cmd := exec.Command("go", "build", "-o", harness, "./test/conformance/harness")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build conformance-harness: %v\n%s", err, out)
	}
	return harness
}

func runRustWireFuzzVerdictPipe(t *testing.T, repo, harness, leg string, retain bool) (string, error) {
	t.Helper()
	args := []string{
		"wire-fuzz",
		"--manifest", "tables/cards/fuzz/MANIFEST.txt",
		"--json", "tables/cards/fuzz",
		"--vectors", "",
		"--driver", leg + " wire-fuzz",
		"--seed", "24845619678",
		"--n", "100",
		"--failed", filepath.Join(t.TempDir(), "failed.bin"),
	}
	if retain {
		args = append(args, "--retain")
	}
	cmd := exec.Command(harness, args...)
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestRustWireFuzzVerdictPipe(t *testing.T) {
	repo := rustWireFuzzRepo(t)
	mk, err := os.ReadFile(filepath.Join(repo, "make", "rust.mk"))
	if err != nil {
		t.Fatal(err)
	}
	if err := rustWireFuzzVerdictMakefileCheck(string(mk)); err != nil {
		t.Fatal(err)
	}
	slowtest.Gate(t, "rustc (it builds and runs the generated card codec over the wire-fuzz verdict pipe)")
	harness := rustWireFuzzHarness(t, repo)
	leg := rustWireFuzzLeg(t, repo, rustWireFuzzCardModules(t, repo), nil)
	for _, retain := range []bool{false, true} {
		mode := "plain"
		if retain {
			mode = "retain"
		}
		out, err := runRustWireFuzzVerdictPipe(t, repo, harness, leg, retain)
		if err != nil {
			t.Fatalf("%s: verdict pipe failed: %v\n%s", mode, err, out)
		}
		if !strings.Contains(out, "0 divergences") {
			t.Fatalf("%s: missing 0 divergences in output:\n%s", mode, out)
		}
	}
}

func TestRustWireFuzzVerdictPipeNegativeControl(t *testing.T) {
	repo := rustWireFuzzRepo(t)
	mk, err := os.ReadFile(filepath.Join(repo, "make", "rust.mk"))
	if err != nil {
		t.Fatal(err)
	}
	if err := rustWireFuzzVerdictMakefileCheck(string(mk)); err != nil {
		t.Fatalf("baseline make/rust.mk failed check: %v", err)
	}
	sabotagedMk := strings.ReplaceAll(string(mk), "cards-rust-wire-fuzz", "cards-rust-wire-fuzz-gone")
	if sabotagedMk == string(mk) {
		t.Fatal("NEGATIVE CONTROL FAILED: make/rust.mk plant did not alter the text")
	}
	if err := rustWireFuzzVerdictMakefileCheck(sabotagedMk); err == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: dropping cards-rust-wire-fuzz left the makefile check green")
	}

	slowtest.Gate(t, "rustc (it builds and runs the generated card codec over the wire-fuzz verdict pipe negative controls)")
	harness := rustWireFuzzHarness(t, repo)
	modules := rustWireFuzzCardModules(t, repo)

	control := func(name, from, to string) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			src, ok := modules["card_table.rs"]
			if !ok {
				t.Fatal("no generated card_table.rs")
			}
			text := string(src)
			if strings.Count(text, from) == 0 {
				t.Fatalf("negative control: %q occurs nowhere in the generated card_table.rs", from)
			}
			mutant := strings.ReplaceAll(text, from, to)
			if mutant == text {
				t.Fatal("NEGATIVE CONTROL FAILED: the plant did not alter the generated artifact")
			}
			leg := rustWireFuzzLeg(t, repo, modules, map[string][]byte{"card_table.rs": []byte(mutant)})
			out, err := runRustWireFuzzVerdictPipe(t, repo, harness, leg, false)
			if err == nil {
				t.Fatalf("NEGATIVE CONTROL FAILED: the %s plant left the verdict pipe green:\n%s", name, out)
			}
			if !strings.Contains(out, "read acceptance differs") {
				t.Fatalf("NEGATIVE CONTROL FAILED: the %s plant went red for an unexpected reason:\n%s", name, out)
			}
		})
	}

	// Plant 1: an array's L is spent whole. Without the tail check the leg
	// accepts a count smaller than the elements' bytes, which the oracle
	// refuses as damage.
	control("array-tail", "if *off != sub { report.damaged = true; }", "if false { report.damaged = true; }")

	// Plant 2: a field id appears at most once in a body. Without the check the
	// leg accepts a repeated known field id, which the oracle refuses.
	control("repeated-field", "if seen[", "if false && seen[")
}
