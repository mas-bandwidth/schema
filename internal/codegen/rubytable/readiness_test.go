package rubytable

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/ruby/rubyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

var rubyReadinessFiles = []string{
	".github/workflows/ci-full.yml",
	".github/workflows/leg.yml",
	".github/workflows/certify.yml",
	".github/workflows/schema-ruby-nightly-fuzz.yml",
	".github/workflows/schema-ruby-runs-hostile-corpus.yml",
	".github/workflows/schema-ruby-resource-bounds.yml",
	"test/conformance/ruby/ci.json",
	"test/conformance/nightly-readiness",
	"test/conformance/harness/implementation_ruby.go",
	"make/ruby-ci-release.mk",
	"make/wire-fuzz-release.mk",
}

func loadRubyReadinessFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, rel := range rubyReadinessFiles {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		out[rel] = string(data)
	}
	return out
}

func rubyReadinessFindings(files map[string]string) []string {
	var out []string
	add := func(f string) {
		if !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	cifull := files[".github/workflows/ci-full.yml"]
	if strings.Count(cifull, "- uses: ./.github/actions/ruby\n        if: matrix.ruby != ''\n") < 2 {
		add("ci-full-pins")
	}
	leg := files[".github/workflows/leg.yml"]
	if !strings.Contains(leg, "- uses: ./.github/actions/ruby\n        if: matrix.ruby != ''\n") {
		add("leg-pins")
	}
	certify := files[".github/workflows/certify.yml"]
	// certify.yml runs Ruby in three jobs: the sharded `test` leg (`startsWith(matrix.shard, 'ruby-')`),
	// `release-gates` (`tables-ruby-release` + `go test -race`), and
	// `negative-controls-nightly` (`matrix.ruby != ''`). (`inline-gate` runs only
	// the five compiled inline-budget legs.)
	if strings.Count(certify, "      - uses: ./.github/actions/ruby\n") < 3 ||
		!strings.Contains(certify, "      - uses: ./.github/actions/ruby\n        if: startsWith(matrix.shard, 'ruby-')\n") ||
		!strings.Contains(certify, "      - uses: ./.github/actions/ruby\n        if: matrix.ruby != ''\n") {
		add("certify-pins")
	}
	if !strings.Contains(files["test/conformance/ruby/ci.json"], `"ruby": "pin"`) {
		add("ci-json-pin")
	}
	nr := files["test/conformance/nightly-readiness"]
	if !strings.Contains(nr, `"goose", "ruby", "moo"`) ||
		!strings.Contains(nr, `harness = "./build/conformance-harness-ruby"`) ||
		!strings.Contains(nr, `return RUBY_DIST + " --disable-gems -w test/conformance/ruby/wirefuzz.rb build/conformance-ruby"`) ||
		strings.Contains(nr, `["ruby", "-v"]`) {
		add("readiness-entry")
	}
	if !strings.Contains(files["test/conformance/harness/implementation_ruby.go"], "//go:build schema_leg_ruby\n") ||
		!strings.Contains(files["make/ruby-ci-release.mk"], "go build -tags schema_leg_ruby -o $@ ./test/conformance/harness") ||
		!strings.Contains(files["make/ruby-ci-release.mk"], "tables-ruby-release:") {
		add("readiness-harness")
	}
	if !strings.Contains(files["make/wire-fuzz-release.mk"], "WFR_DRIVER_ruby") {
		add("wire-fuzz-release")
	}
	for _, wf := range []string{
		".github/workflows/schema-ruby-nightly-fuzz.yml",
		".github/workflows/schema-ruby-runs-hostile-corpus.yml",
		".github/workflows/schema-ruby-resource-bounds.yml",
	} {
		if !strings.Contains(files[wf], "uses: ./.github/actions/ruby") {
			add("workflow-pins")
		}
	}
	return out
}

func rubyBenchLegUnit(t *testing.T, root string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(root, "bench", "corpus", "BenchTable.schema"))
	if err != nil {
		t.Fatal(err)
	}
	variable := regexp.MustCompile(`(?m)^fixed table `).ReplaceAllString(string(src), "table ")
	if variable == string(src) {
		t.Fatal("the bench corpus declares no fixed table")
	}
	files, err := Generate(loadUnit(t, variable))
	if err != nil {
		t.Fatal(err)
	}
	unit := t.TempDir()
	writeTree(t, unit, files)
	return unit
}

func rubyBenchLegGate(t *testing.T, root, interp, unit, corpus string) (string, string, error) {
	t.Helper()
	wire := filepath.Join(root, "testdata", "wire")
	variants := filepath.Join(root, "bench", "corpus", "variants")
	if corpus != "" {
		wire, variants = corpus, corpus
	}
	return rubyrun.TryArgs(t, interp, root, filepath.Join(root, "bench", "tables", "ruby", "table_main.rb"),
		[]string{unit, "--gate", "--wire-dir", wire, "--variant-dir", variants}, nil, nil)
}

func TestRubyReadiness(t *testing.T) {
	t.Parallel()
	root, err := rubyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	files := loadRubyReadinessFiles(t, root)
	if got := rubyReadinessFindings(files); len(got) != 0 {
		t.Fatalf("ruby readiness findings: %v", got)
	}
	if !slowtest.Enabled() {
		return
	}
	unit := rubyBenchLegUnit(t, root)
	for _, interp := range []string{rubyrun.Interpreter(t), rubyrun.Floor(t)} {
		stdout, stderr, gerr := rubyBenchLegGate(t, root, interp, unit, "")
		if gerr != nil || stderr != "" {
			t.Fatalf("%s: ruby bench gate failed: %v\nstderr:\n%s\nstdout:\n%s", filepath.Base(interp), gerr, stderr, stdout)
		}
	}
}

func TestRubyReadinessNegativeControl(t *testing.T) {
	t.Parallel()
	root, err := rubyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	files := loadRubyReadinessFiles(t, root)
	if got := rubyReadinessFindings(files); len(got) != 0 {
		t.Fatalf("clean files already have readiness findings: %v", got)
	}
	for _, plant := range []struct {
		finding, file, before, after string
	}{
		{"ci-full-pins", ".github/workflows/ci-full.yml", "      - uses: ./.github/actions/ruby\n        if: matrix.ruby != ''\n", ""},
		{"leg-pins", ".github/workflows/leg.yml", "      - uses: ./.github/actions/ruby\n        if: matrix.ruby != ''\n", ""},
		{"certify-pins", ".github/workflows/certify.yml", "      - uses: ./.github/actions/ruby\n        if: startsWith(matrix.shard, 'ruby-')\n", ""},
		{"certify-pins", ".github/workflows/certify.yml", "      - uses: ./.github/actions/ruby\n      # The moo pin", "      # The moo pin"},
		{"certify-pins", ".github/workflows/certify.yml", "      - uses: ./.github/actions/ruby\n        if: matrix.ruby != ''\n", ""},
		{"ci-json-pin", "test/conformance/ruby/ci.json", `"ruby": "pin"`, `"ruby": ""`},
		{"readiness-entry", "test/conformance/nightly-readiness", `"goose", "ruby", "moo"`, `"goose", "moo"`},
		{"readiness-entry", "test/conformance/nightly-readiness", `harness = "./build/conformance-harness-ruby"`, `harness = "./build/conformance-harness"`},
		{"readiness-entry", "test/conformance/nightly-readiness", `return RUBY_DIST + " --disable-gems -w test/conformance/ruby/wirefuzz.rb build/conformance-ruby"`, `return "unpinned-interp"`},
		{"readiness-harness", "test/conformance/harness/implementation_ruby.go", "//go:build schema_leg_ruby\n", ""},
		{"readiness-harness", "make/ruby-ci-release.mk", "go build -tags schema_leg_ruby -o $@ ./test/conformance/harness", "go build -o $@ ./test/conformance/harness"},
		{"wire-fuzz-release", "make/wire-fuzz-release.mk", "WFR_DRIVER_ruby", "WFR_OMITTED_ruby"},
	} {
		planted := maps.Clone(files)
		if strings.Count(planted[plant.file], plant.before) < 1 {
			t.Fatalf("NEGATIVE CONTROL: plant %s missing in %s", plant.finding, plant.file)
		}
		planted[plant.file] = strings.Replace(planted[plant.file], plant.before, plant.after, 1)
		if got := rubyReadinessFindings(planted); !slices.Contains(got, plant.finding) {
			t.Errorf("NEGATIVE CONTROL FAILED: planted %s in %s not caught (got %v)", plant.finding, plant.file, got)
		}
	}
	if !slowtest.Enabled() {
		return
	}
	unit := rubyBenchLegUnit(t, root)
	interp := rubyrun.Interpreter(t)
	golden, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "bench_table.bin"))
	if err != nil {
		t.Fatal(err)
	}
	variants, err := os.ReadFile(filepath.Join(root, "bench", "corpus", "variants", "bench_table.variants.bin"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, want string
		flip       func(golden, variants []byte)
	}{
		{"golden", "WIRE GOLDEN MISMATCH", func(g, _ []byte) { g[100] ^= 0xFF }},
		{"roundtrip", "FAILED: bench_table", func(_, v []byte) { v[len(v)/64*3+100] ^= 0xFF }},
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
		_, stderr, gerr := rubyBenchLegGate(t, root, interp, unit, dir)
		if gerr == nil {
			t.Errorf("NEGATIVE CONTROL FAILED: %s corpus passed the bench gate", c.name)
			continue
		}
		if !strings.Contains(stderr, c.want) {
			t.Errorf("NEGATIVE CONTROL FAILED: %s corpus went red without %q:\n%s", c.name, c.want, stderr)
		}
	}
}
