package ci

import (
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/ruby/rubyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func rubyStrictGateUnits(t *testing.T, root string) map[string]map[string][]byte {
	t.Helper()
	c := compiler.New()
	units := map[string]map[string][]byte{}

	loadGen := func(label string, paths []string) {
		u, err := c.Load(paths)
		if err != nil {
			t.Fatalf("%s: load failed: %v", label, err)
		}
		files, err := c.Generate(u, "ruby", nil)
		if err != nil {
			t.Fatalf("%s: generate --lang ruby failed: %v", label, err)
		}
		units[label] = files
	}

	for _, dir := range []string{
		"examples",
		"examples128",
		"tables/examples",
		"tables/messages",
		"tables/pointers",
		"tables/maps",
		"tables/blobs",
		"tables/block",
		"tables/widelist",
		"tables/cards",
	} {
		p := filepath.Join(root, filepath.FromSlash(dir))
		paths, err := compiler.GatherPaths([]string{p})
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		loadGen(dir, paths)
	}

	for _, single := range []string{
		"bench/corpus/Bench.schema",
		"bench/corpus/RealWorld.schema",
		"test/packet-text/Narrow.schema",
		"test/packet-finite/ruby/Finite.schema",
		"test/packet-void/ruby/Ruby.schema",
	} {
		loadGen(single, []string{filepath.Join(root, filepath.FromSlash(single))})
	}

	benchTableSrc, err := os.ReadFile(filepath.Join(root, "bench", "corpus", "BenchTable.schema"))
	if err != nil {
		t.Fatal(err)
	}
	benchVar := regexp.MustCompile(`(?m)^fixed table `).ReplaceAllString(string(benchTableSrc), "table ")
	tmpSchema := filepath.Join(t.TempDir(), "BenchTable.schema")
	if err := os.WriteFile(tmpSchema, []byte(benchVar), 0o600); err != nil {
		t.Fatal(err)
	}
	loadGen("bench/corpus/BenchTable.schema", []string{tmpSchema})

	manifest, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(manifest), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "unit" {
			continue
		}
		key := "manifest/" + fields[1]
		var schemaInputs []string
		for _, rel := range fields[2:] {
			schemaInputs = append(schemaInputs, filepath.Join(root, filepath.FromSlash(rel)))
		}
		paths, err := compiler.GatherPaths(schemaInputs)
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		loadGen(key, paths)
	}
	return units
}

func rubyStrictGateWriteAndPkg(t *testing.T, dir string, files map[string][]byte) string {
	t.Helper()
	pkg := ""
	for _, name := range slices.Sorted(maps.Keys(files)) {
		if path.Dir(name) == "." && strings.HasSuffix(name, ".rb") && !strings.HasPrefix(name, "_") {
			pkg = strings.TrimSuffix(name, ".rb")
		}
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, files[name], 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if pkg == "" {
		t.Fatal("generated Ruby unit has no top-level <pkg>.rb")
	}
	return pkg
}

func TestRubyGeneratedStrictGate(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Ruby interpreters and strict analyzers")
	root := repoRoot(t)
	units := rubyStrictGateUnits(t, root)
	if len(units) < 40 {
		t.Fatalf("strict gate covered only %d units, want >= 40", len(units))
	}

	allDir := t.TempDir()
	var allPkgs []string
	var reqLines strings.Builder
	reqLines.WriteString("# frozen_string_literal: true\n\n")

	_ = allPkgs
	_ = reqLines
	for _, label := range slices.Sorted(maps.Keys(units)) {
		files := units[label]
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			subDir := filepath.Join(allDir, filepath.FromSlash(strings.ReplaceAll(label, "/", "__")))
			if err := os.MkdirAll(subDir, 0o755); err != nil {
				t.Fatal(err)
			}
			pkg := rubyStrictGateWriteAndPkg(t, subDir, files)
			probe := map[string][]byte{
				"_probe.rb": []byte("# frozen_string_literal: true\n\nrequire_relative '" + pkg + "'\n"),
			}
			rubyrun.Run(t, subDir, "_probe.rb", probe)
			rubyrun.RunFloor(t, subDir, "_probe.rb", probe)
			rubyrun.Analyze(t, subDir, pkg)
		})
	}
}

func TestRubyGeneratedStrictGatePlantedPattern(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Ruby interpreters and strict analyzers")
	root := repoRoot(t)
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, "tables", "pointers")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	baseFiles, err := compiler.New().Generate(u, "ruby", nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("ruby-warning", func(t *testing.T) {
		t.Parallel()
		files := maps.Clone(baseFiles)
		dir := t.TempDir()
		pkg := rubyStrictGateWriteAndPkg(t, dir, files)
		p := filepath.Join(dir, pkg+".rb")
		raw, rerr := os.ReadFile(p)
		if rerr != nil {
			t.Fatal(rerr)
		}
		raw = append(raw, []byte("\ndef _s_planted_warn\n  unused_planted_local = 1\n  nil\nend\n")...)
		if werr := os.WriteFile(p, raw, 0o644); werr != nil {
			t.Fatal(werr)
		}
		probe := map[string][]byte{"_probe.rb": []byte("# frozen_string_literal: true\n\nrequire_relative '" + pkg + "'\n")}
		for _, interp := range []string{rubyrun.Interpreter(t), rubyrun.Floor(t)} {
			// a warning does not fail the interpreter, so stderr is the verdict;
			// the run's error only says why stderr may be missing
			_, stderr, runErr := rubyrun.Try(t, interp, dir, "_probe.rb", probe)
			if !strings.Contains(stderr, "unused_planted_local") {
				t.Fatalf("%s: planted ruby -w warning was not caught on stderr: %q (run error: %v)", filepath.Base(interp), stderr, runErr)
			}
		}
	})

	t.Run("rubocop-offense", func(t *testing.T) {
		t.Parallel()
		files := maps.Clone(baseFiles)
		dir := t.TempDir()
		pkg := rubyStrictGateWriteAndPkg(t, dir, files)
		p := filepath.Join(dir, pkg+".rb")
		raw, rerr := os.ReadFile(p)
		if rerr != nil {
			t.Fatal(rerr)
		}
		raw = append(raw, []byte("\n# trailing whitespace offense   \nx=1+2\n")...)
		if werr := os.WriteFile(p, raw, 0o644); werr != nil {
			t.Fatal(werr)
		}
		step, out, aerr := rubyrun.TryAnalyze(t, dir, pkg)
		if aerr == nil || step != "rubocop" {
			t.Fatalf("expected rubocop failure, got step=%q err=%v out=%s", step, aerr, out)
		}
	})

	t.Run("steep-type-error", func(t *testing.T) {
		t.Parallel()
		files := maps.Clone(baseFiles)
		dir := t.TempDir()
		pkg := rubyStrictGateWriteAndPkg(t, dir, files)
		var targetFile string
		for name := range files {
			if strings.HasPrefix(name, pkg+"/") && strings.HasSuffix(name, ".rb") && !strings.Contains(name, "/_") && !strings.Contains(name, "serialize") {
				targetFile = filepath.Join(dir, filepath.FromSlash(name))
				break
			}
		}
		if targetFile == "" {
			t.Fatal("found no generated schema .rb file")
		}
		raw, rerr := os.ReadFile(targetFile)
		if rerr != nil {
			t.Fatal(rerr)
		}
		patched := strings.Replace(string(raw), "def save\n", "def save\n      1.no_such_schema_method\n", 1)
		if patched == string(raw) {
			t.Fatal("could not plant steep fault in save method")
		}
		if werr := os.WriteFile(targetFile, []byte(patched), 0o644); werr != nil {
			t.Fatal(werr)
		}
		step, out, aerr := rubyrun.TryAnalyze(t, dir, pkg)
		if aerr == nil || step != "steep" || !strings.Contains(out, "NoMethod") {
			t.Fatalf("expected steep NoMethod failure, got step=%q err=%v out=%s", step, aerr, out)
		}
	})
}

// TestRubyGeneratedStrictGateFast is the strict gate's PULL-REQUEST FORM
// (docs/CI-TIERS.md, ci-fast.yml nightly-fast row ruby-strict). The whole gate
// above runs both interpreters, RuboCop, RBS and Steep once per unit, about
// five minutes on four cores, so it stayed nightly, and a RuboCop offense in
// the generated Ruby reached main (2026-11-22). This form keeps the two
// halves that answer in seconds over the same units: every unit loads under
// ruby -w on both pins with an empty stderr, and ONE RuboCop process lints
// every unit at once instead of one process per unit. RBS validation and Steep
// strict mode stay in the nightly gate.
func TestRubyGeneratedStrictGateFast(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Ruby interpreters and RuboCop")
	root := repoRoot(t)
	units := rubyStrictGateUnits(t, root)
	if len(units) < 40 {
		t.Fatalf("strict gate fast form covered only %d units, want >= 40", len(units))
	}
	allDir := t.TempDir()
	// THE RUNTIME IS LINTED ONCE. Every unit carries the same Ruby runtime
	// (_table/, serialize/, _fixed.rb ...) under its own module name, which is
	// most of its lines. A file whose text, with the module name of its unit
	// replaced, equals one already queued is the same RuboCop input, so the
	// runtime is linted once and the schema files of every unit each time.
	var rcPaths []string
	queued := map[string]bool{}
	modLine := regexp.MustCompile("(?m)^module (\\w+)")
	type unit struct{ label, dir, pkg string }
	var all []unit
	for _, label := range slices.Sorted(maps.Keys(units)) {
		sub := strings.ReplaceAll(label, "/", "__")
		dir := filepath.Join(allDir, sub)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		pkg := rubyStrictGateWriteAndPkg(t, dir, units[label])
		m := modLine.FindSubmatch(units[label][pkg+".rb"])
		if m == nil {
			t.Fatalf("%s: %s.rb names no module", label, pkg)
		}
		for _, name := range slices.Sorted(maps.Keys(units[label])) {
			if !strings.HasSuffix(name, ".rb") {
				continue
			}
			key := strings.ReplaceAll(string(units[label][name]), string(m[1]), "SchemaUnit")
			if queued[key] {
				continue
			}
			queued[key] = true
			rcPaths = append(rcPaths, path.Join(sub, name))
		}
		all = append(all, unit{label, dir, pkg})
	}
	t.Run("ruby-w", func(t *testing.T) {
		t.Parallel()
		for _, u := range all {
			t.Run(u.label, func(t *testing.T) {
				t.Parallel()
				probe := map[string][]byte{
					"_probe.rb": []byte("# frozen_string_literal: true\n\nrequire_relative '" + u.pkg + "'\n"),
				}
				rubyrun.Run(t, u.dir, "_probe.rb", probe)
				rubyrun.RunFloor(t, u.dir, "_probe.rb", probe)
			})
		}
	})
	t.Run("rubocop", func(t *testing.T) {
		t.Parallel()
		out, err := rubyrun.RuboCopAll(t, allDir, rcPaths...)
		if err != nil {
			t.Fatalf("RuboCop over all %d generated Ruby units failed (%v):\n%s", len(all), err, out)
		}
		if !strings.Contains(out, "no offenses detected") {
			t.Fatalf("RuboCop over all %d units did not report its verdict:\n%s", len(all), out)
		}
	})
}
