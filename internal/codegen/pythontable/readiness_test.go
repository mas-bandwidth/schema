package pythontable

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE PYTHON LEG'S CI AND RELEASE TIER (docs/port-python/table-ci-release.md).
// pythonReadinessFindings reads the artifacts the tier is made of and names
// every rule one of them breaks:
//
//   - release-target: make/python-ci-release.mk carries tables-python-release,
//     which certify.yml's release-targets job derives by name, and its recipe
//     runs the release wire fuzz with its control, the bench leg's gate with
//     its control, and the measured bench pass;
//   - path-interpreter: no recipe of that file, and no line of the bench leg,
//     starts an interpreter by a name looked up on PATH;
//   - bench-leg: bench/tables/python/leg builds and runs the runner under a
//     pin it refuses to take as a bare name, and the runner names the
//     generated root once (its SHAPE-GATE.allow entry);
//   - ci-full-pins: ci-full.yml's conformance job installs the pins for a row
//     whose ci.json names `python`;
//   - certify-pins: certify.yml's release-gates job installs the pins every
//     tables-python-release run needs;
//   - leg-pins: leg.yml's legs job, which certify's schema-legs job calls,
//     installs the pins for a row whose ci.json names `python`;
//   - ci-json-pin: test/conformance/python/ci.json names `"python": "pin"`,
//     so both pin steps above run for the Python row;
//   - readiness-entry: test/conformance/nightly-readiness carries python in
//     its acceptance shape: the language, its two packages under the
//     schema_leg_python tag, the tagged harness built and run for
//     `implementation --lang python`, the pins as a prerequisite, the wire
//     fuzz under the current pin over build/conformance-python, its own
//     packet probes and retention controls, and no bare interpreter;
//   - readiness-harness: the harness admits `implementation --lang python`
//     in a schema_leg_python build only, and make/python-ci-release.mk builds
//     that harness with the tag.
//
// The files are passed in, so the negative control can hand it planted copies.
func pythonReadinessFindings(files map[string]string) []string {
	var findings []string
	add := func(name string) {
		if !slices.Contains(findings, name) {
			findings = append(findings, name)
		}
	}

	mk := files["make/python-ci-release.mk"]
	release := makeRecipe(mk, "tables-python-release")
	for _, step := range []string{
		"tables-wire-fuzz-release-python",
		"tables-python-wire-fuzz-negative-control",
		"tables-python-bench-gate",
		"tables-python-bench-gate-negative-control",
		"bench/tables/run.sh --only python --bare",
	} {
		if !strings.Contains(release, step) {
			add("release-target")
		}
	}
	for line := range strings.SplitSeq(mk, "\n") {
		if strings.HasPrefix(line, "\t") && bareInterpreter.MatchString(line) {
			add("path-interpreter")
		}
	}

	leg := files["bench/tables/python/leg"]
	for line := range strings.SplitSeq(leg, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") && bareInterpreter.MatchString(line) {
			add("path-interpreter")
		}
	}
	for _, rule := range []string{
		`: "${PYTHON:=dist/python-`,
		"*/*) ;;",
		`exec "$PYTHON" -W error -I -B bench/tables/python/table_main.py`,
		"./bin/schema generate --lang python",
	} {
		if !strings.Contains(leg, rule) {
			add("bench-leg")
		}
	}
	if strings.Count(files["bench/tables/python/table_main.py"], "TableMixed") != 1 ||
		!strings.Contains(files["bench/tables/python/SHAPE-GATE.allow"], "names bench/tables/python/table_main.py 1 ") {
		add("bench-leg")
	}

	if !workflowStep(workflowJob(files[".github/workflows/ci-full.yml"], "conformance"),
		"uses: ./.github/actions/python", "if: matrix.python != ''") {
		add("ci-full-pins")
	}
	if !workflowStep(workflowJob(files[".github/workflows/certify.yml"], "release-gates"),
		"uses: ./.github/actions/python", "") {
		add("certify-pins")
	}
	if !workflowStep(workflowJob(files[".github/workflows/leg.yml"], "legs"),
		"uses: ./.github/actions/python", "if: matrix.python != ''") {
		add("leg-pins")
	}

	var row map[string]string
	if err := json.Unmarshal([]byte(files["test/conformance/python/ci.json"]), &row); err != nil || row["python"] != "pin" {
		add("ci-json-pin")
	}

	readiness := files["test/conformance/nightly-readiness"]
	for _, rule := range []string{
		`"moo", "python")`,
		`"python": ("python", "pythontable"),`,
		`GO_TAGS = {"python": "schema_leg_python"}`,
		`tags = ["-tags", GO_TAGS[lang]] if lang in GO_TAGS else []`,
		`"go", "test", *tags, "-json", "-timeout=0"`,
		`PYTHON_DIST = "dist/python-` + pyrun.CurrentVersion + `/bin/python3.15"`,
		`return PYTHON_DIST + " -W error -I -B test/conformance/python/wirefuzz.py build/conformance-python"`,
		`prerequisites += " dist-python python-toolchain-check build/conformance-harness-python"`,
		`harness = "./build/conformance-harness-python"`,
		`required |= {"retain-python-" + name for name in PYTHON_RETENTION_CONTROLS}`,
		`run_python_packets(runner)`,
	} {
		if !strings.Contains(readiness, rule) {
			add("readiness-entry")
		}
	}
	for line := range strings.SplitSeq(readiness, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") && readinessBareInterpreter.MatchString(line) {
			add("readiness-entry")
		}
	}

	admit := files["test/conformance/harness/implementation_python.go"]
	if !strings.HasPrefix(admit, "//go:build schema_leg_python\n") ||
		!strings.Contains(admit, `taggedImplementationLanguages = append(taggedImplementationLanguages, "python")`) ||
		!strings.Contains(makeRecipe(mk, "build/conformance-harness-python"),
			"go build -tags schema_leg_python -o $@ ./test/conformance/harness") {
		add("readiness-harness")
	}
	return findings
}

// readinessBareInterpreter is a Python interpreter named with no directory in
// the readiness script's code: a command list whose first word is a bare
// `python...`, or `python3...` as a word of a string. The language key
// "python", an index such as GO_TAGS["python"], is neither; the script's own
// shebang is a comment.
var readinessBareInterpreter = regexp.MustCompile(`(?:^|[^\w\]])\[\s*["']python[0-9.]*["']\s*[,\]]|["'\s]python[3][0-9.]*["'\s]`)

// bareInterpreter is a command word, or a value handed to one, that names a
// Python interpreter with no directory: the shell would look it up on PATH.
// An argument such as `--lang python` is no command word.
var bareInterpreter = regexp.MustCompile("(^\\s*|[;&|(`'\"=]\\s*|\\bexec\\s+)python[0-9.]*(\\s|$)")

// makeRecipe is the recipe of target in a makefile's text: the tab-led lines
// after `target:`.
func makeRecipe(text, target string) string {
	var out []string
	in := false
	for line := range strings.SplitSeq(text, "\n") {
		switch {
		case strings.HasPrefix(line, target+":"):
			in = true
		case in && strings.HasPrefix(line, "\t"):
			out = append(out, line)
		default:
			in = false
		}
	}
	return strings.Join(out, "\n")
}

// workflowJob is the block of one job under a workflow's jobs: key.
func workflowJob(text, job string) string {
	_, rest, ok := strings.Cut(text, "\n  "+job+":\n")
	if !ok {
		return ""
	}
	var out []string
	for line := range strings.SplitSeq(rest, "\n") {
		if len(line) > 2 && line[0] == ' ' && line[1] == ' ' && line[2] != ' ' && line[2] != '#' {
			break
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// workflowStep is whether a job carries a step whose first line is uses and,
// when cond is not empty, whose next line is cond.
func workflowStep(job, uses, cond string) bool {
	lines := strings.Split(job, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "- "+uses {
			continue
		}
		if cond == "" {
			return true
		}
		if i+1 < len(lines) && strings.TrimSpace(lines[i+1]) == cond {
			return true
		}
	}
	return false
}

var pythonReadinessFiles = []string{
	"make/python-ci-release.mk",
	"bench/tables/python/leg",
	"bench/tables/python/table_main.py",
	"bench/tables/python/SHAPE-GATE.allow",
	".github/workflows/ci-full.yml",
	".github/workflows/certify.yml",
	".github/workflows/leg.yml",
	"test/conformance/python/ci.json",
	"test/conformance/nightly-readiness",
	"test/conformance/harness/implementation_python.go",
}

func readPythonReadinessFiles(t *testing.T) (string, map[string]string) {
	t.Helper()
	root, err := pyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, name := range pythonReadinessFiles {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(data)
	}
	return root, files
}

// TestPythonReadiness holds the tier's artifacts to the rules above, then,
// under SCHEMA_SLOW, runs the bench leg's runner in its gate mode under both
// pins over a unit generated from the bench corpus with its tables declared
// variable, as the leg builds it: variant 0 is the pinned golden and every
// one of the 64 records loads and saves back byte-identical.
func TestPythonReadiness(t *testing.T) {
	t.Parallel()
	root, files := readPythonReadinessFiles(t)
	if findings := pythonReadinessFindings(files); len(findings) != 0 {
		t.Fatalf("the Python CI and release tier breaks %v", findings)
	}
	if !slowtest.Enabled() {
		t.Log("SCHEMA_SLOW unset: the bench gate under the pins is not run")
		return
	}
	unit := benchLegUnit(t, root)
	for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		stdout, stderr, err := benchLegGate(t, root, python, unit, "")
		if err != nil || !strings.Contains(stderr, "\nOK (corpus_id ") {
			t.Fatalf("%s: the bench leg's gate did not pass: %v\nstderr:\n%s\nstdout:\n%s", filepath.Base(python), err, stderr, stdout)
		}
	}
}

// TestPythonReadinessNegativeControl plants one fault per rule in a copy of
// the real artifact the rule reads and requires the finding by its name;
// under SCHEMA_SLOW it also hands the bench leg's gate a copy of the corpus
// whose golden differs from variant 0, then one whose variant 3 does not
// round-trip, and requires each red by its own message.
func TestPythonReadinessNegativeControl(t *testing.T) {
	t.Parallel()
	root, files := readPythonReadinessFiles(t)
	for _, plant := range []struct {
		finding, file, before, after string
	}{
		{"release-target", "make/python-ci-release.mk", "\t$(MAKE) tables-python-bench-gate-negative-control\n", ""},
		{"release-target", "make/python-ci-release.mk", "bench/tables/run.sh --only python --bare", "true"},
		{"path-interpreter", "make/python-ci-release.mk", "PYTHON=$(PYTHON_FLOOR) bench/tables/python/leg run --gate", "PYTHON=python3.15 bench/tables/python/leg run --gate"},
		{"path-interpreter", "bench/tables/python/leg", `exec "$PYTHON" -W error`, `exec python3.15 -W error`},
		{"bench-leg", "bench/tables/python/leg", "*/*) ;;", "*) ;;"},
		{"bench-leg", "bench/tables/python/table_main.py", `bench_table(getattr(module, ROOT)`, `bench_table(getattr(module, "TableMixed")`},
		{"ci-full-pins", ".github/workflows/ci-full.yml", "      - uses: ./.github/actions/python\n        if: matrix.python != ''\n\n      # THE LOBSTER PIN", "\n      # THE LOBSTER PIN"},
		{"certify-pins", ".github/workflows/certify.yml", "      - uses: ./.github/actions/python\n\n      - uses: erlef/setup-beam@54075bcc5e249e4758d363f27d099f55d843f124 # v1.24.1\n        with:", "\n      - uses: erlef/setup-beam@54075bcc5e249e4758d363f27d099f55d843f124 # v1.24.1\n        with:"},
		{"leg-pins", ".github/workflows/leg.yml", "      - uses: ./.github/actions/python\n        if: matrix.python != ''\n", ""},
		{"leg-pins", ".github/workflows/leg.yml", "        if: matrix.python != ''\n", "        if: matrix.moo != ''\n"},
		{"ci-json-pin", "test/conformance/python/ci.json", `"python": "pin"`, `"python": ""`},
		{"readiness-entry", "test/conformance/nightly-readiness", `"moo", "python")`, `"moo")`},
		{"readiness-entry", "test/conformance/nightly-readiness", `GO_TAGS = {"python": "schema_leg_python"}`, `GO_TAGS = {}`},
		{"readiness-entry", "test/conformance/nightly-readiness", `harness = "./build/conformance-harness-python"`, `harness = "./build/conformance-harness"`},
		{"readiness-entry", "test/conformance/nightly-readiness", "def run_python_packets(runner):\n", "def run_python_packets(runner):\n    runner.run(\"probe\", [\"python3.15\", \"-V\"])\n"},
		{"readiness-entry", "test/conformance/nightly-readiness", `return PYTHON_DIST + " -W error`, `return "python3.15 -W error`},
		{"readiness-harness", "test/conformance/harness/implementation_python.go", "//go:build schema_leg_python\n", ""},
		{"readiness-harness", "make/python-ci-release.mk", "go build -tags schema_leg_python -o $@ ./test/conformance/harness", "go build -o $@ ./test/conformance/harness"},
	} {
		planted := maps.Clone(files)
		if strings.Count(planted[plant.file], plant.before) != 1 {
			t.Fatalf("NEGATIVE CONTROL: the %s plant no longer matches exactly once in %s", plant.finding, plant.file)
		}
		planted[plant.file] = strings.Replace(planted[plant.file], plant.before, plant.after, 1)
		if got := pythonReadinessFindings(planted); !slices.Contains(got, plant.finding) {
			t.Errorf("NEGATIVE CONTROL FAILED: a planted %s fault in %s was not found (findings %v)", plant.finding, plant.file, got)
		}
	}
	if !slowtest.Enabled() {
		t.Log("SCHEMA_SLOW unset: the bench gate's planted corpora are not run")
		return
	}
	unit := benchLegUnit(t, root)
	python := pyrun.Interpreter(t)
	golden, err := os.ReadFile(filepath.Join(root, "testdata/wire/bench_table.bin"))
	if err != nil {
		t.Fatal(err)
	}
	variants, err := os.ReadFile(filepath.Join(root, "bench/corpus/variants/bench_table.variants.bin"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, want string
		flip       func(golden, variants []byte)
	}{
		{"golden", "WIRE GOLDEN MISMATCH", func(golden, _ []byte) { golden[100] ^= 0xFF }},
		{"roundtrip", "FAILED: bench_table", func(_, variants []byte) { variants[len(variants)/64*3+100] ^= 0xFF }},
	} {
		g, v := slices.Clone(golden), slices.Clone(variants)
		c.flip(g, v)
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "bench_table.bin"), g, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "bench_table.variants.bin"), v, 0o644); err != nil {
			t.Fatal(err)
		}
		_, stderr, err := benchLegGate(t, root, python, unit, dir)
		if err == nil {
			t.Errorf("NEGATIVE CONTROL FAILED: a %s corpus passed the bench leg's gate", c.name)
			continue
		}
		if !strings.Contains(stderr, c.want) {
			t.Errorf("NEGATIVE CONTROL FAILED: the %s corpus went red, but not on %q:\n%s", c.name, c.want, stderr)
		}
	}
}

// benchLegUnit generates the bench corpus as bench/tables/python/leg does:
// its fixed tables declared variable, so the generated classes read form 1.
func benchLegUnit(t *testing.T, root string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(root, "bench/corpus/BenchTable.schema"))
	if err != nil {
		t.Fatal(err)
	}
	variable := regexp.MustCompile(`(?m)^fixed table `).ReplaceAllString(string(src), "table ")
	if variable == string(src) {
		t.Fatal("the bench corpus declares no fixed table: the leg's variable copy changes nothing")
	}
	files, err := Generate(loadUnit(t, variable))
	if err != nil {
		t.Fatal(err)
	}
	unit := t.TempDir()
	writeTree(t, unit, files)
	return unit
}

// benchLegGate runs the runner's gate mode; corpus, when set, is a directory
// holding both the golden and the variant corpus in place of the committed ones.
func benchLegGate(t *testing.T, root, python, unit, corpus string) (string, string, error) {
	t.Helper()
	wire, variants := filepath.Join(root, "testdata/wire"), filepath.Join(root, "bench/corpus/variants")
	if corpus != "" {
		wire, variants = corpus, corpus
	}
	return pyrun.TryStdin(t, python, root, filepath.Join(root, "bench/tables/python/table_main.py"),
		[]string{unit, "--gate", "--wire-dir", wire, "--variant-dir", variants}, "")
}
