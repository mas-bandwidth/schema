package rubytable

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablenames"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// rubyKeyedNamesDir is the committed corpus of F-083 and F-084: four units
// (declared, twowrappers, fixed, control) and cases.tsv, one Ruby probe per
// row.
const rubyKeyedNamesDir = "testdata/keyed-wrapper-names"

// TestRubyKeyedWrapperNames pins the Ruby table leg's keyed-array wrapper
// classes (docs/PORT-RUBY.md, Tables: [E]T) to names no declaration and no
// other wrapper can spell. The emitter used to invent <Table><Field>KeyedArray
// in the unit module, where a declared table QuestRankKeyedArray reopened
// Quest.rank's wrapper (F-083: Quest.new.rank[1] = 3 raised), and where
// Quest.rank_value and QuestRank.value both became QuestRankValueKeyedArray,
// one class sized by whichever came last (F-084: a two-key field took a third
// key). Every row runs on both pinned interpreters: the assignments, a
// variable, JSON and fixed-form round trip, the size of each wrapper, its
// refusal of the key past its enum, and the declared tables' own values,
// beside the control unit whose names never met the old spellings.
func TestRubyKeyedWrapperNames(t *testing.T) {
	t.Parallel()
	rows := rubyCorpusRows(t, filepath.Join(rubyKeyedNamesDir, "cases.tsv"), 4)
	bySchema := map[string][][]string{}
	var schemas []string
	for _, r := range rows {
		if !strings.HasPrefix(r[3], "held ") && !strings.HasPrefix(r[3], "raised ") {
			t.Fatalf("%s: expect %q", r[0], r[3])
		}
		if bySchema[r[1]] == nil {
			schemas = append(schemas, r[1])
		}
		bySchema[r[1]] = append(bySchema[r[1]], r)
	}
	for _, want := range []string{"declared", "twowrappers", "fixed", "control"} {
		if len(bySchema[want]) == 0 {
			t.Fatalf("cases.tsv: no rows for %s.schema", want)
		}
	}
	trees := map[string]map[string][]byte{}
	for _, name := range schemas {
		src, err := os.ReadFile(filepath.Join(rubyKeyedNamesDir, name+".schema"))
		if err != nil {
			t.Fatal(err)
		}
		files, err := Generate(loadUnit(t, string(src)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		trees[name] = files
	}

	t.Run("ruby", func(t *testing.T) {
		slowtest.Gate(t, "both pinned Ruby interpreters")
		for _, name := range schemas {
			var probe strings.Builder
			probe.WriteString(rubyKeyedNamesProbeHead)
			probe.WriteString("module League\n")
			for _, r := range bySchema[name] {
				fmt.Fprintf(&probe, "  step(%q) { %s }\n", r[0], r[2])
			}
			probe.WriteString("end\n")
			dir := t.TempDir()
			writeTree(t, dir, trees[name])
			out := runBothRubyPins(t, dir, "probe.rb", map[string][]byte{"probe.rb": []byte(probe.String())})
			got := map[string]string{}
			for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
				id, answer, _ := strings.Cut(line, " ")
				got[id] = answer
			}
			for _, r := range bySchema[name] {
				if got[r[0]] != r[3] {
					t.Errorf("%s (%s.schema: %s): answered %q, want %q", r[0], name, r[2], got[r[0]], r[3])
				}
			}
		}
	})
}

const rubyKeyedNamesProbeHead = `# frozen_string_literal: true

require_relative 'league'

def step(id)
  back = yield
  puts "#{id} held #{back.inspect}"
rescue StandardError => e
  puts "#{id} raised #{e.class}"
end

`

// rubyUnitConstant matches a class or module the generated unit module opens
// at its own level: two spaces in, inside "module <Unit>".
var rubyUnitConstant = regexp.MustCompile(`(?m)^  (?:class|module) ([A-Z][A-Za-z0-9_:]*)`)

// rubyUnitConstantsOpenedTwice scans the declaration files of one generated
// Ruby table unit (<package>/<Base>.rb, one per schema file: the files that
// hold the declarations and every class the emitter invents beside them) and
// answers each unit-level constant opened more than once, and each one that
// spells a name the Ruby table runtime defines (internal/tablenames, Ruby).
// The runtime's own files (_table/, _schema.rb, _fixed.rb, _view.rb, ...)
// reopen the runtime's modules on purpose and are not scanned.
func rubyUnitConstantsOpenedTwice(u *ir.Unit, files map[string][]byte) []string {
	runtime := map[string]bool{}
	for _, n := range tablenames.DefinedBy(tablenames.Ruby) {
		runtime[n] = true
	}
	opened := map[string]int{}
	for _, f := range u.Files {
		data, ok := files[filepath.ToSlash(filepath.Join(u.Package, f.Base+".rb"))]
		if !ok {
			continue
		}
		for _, m := range rubyUnitConstant.FindAllStringSubmatch(string(data), -1) {
			opened[m[1]]++
		}
	}
	var bad []string
	for name, n := range opened {
		if n > 1 {
			bad = append(bad, fmt.Sprintf("%s opened %d times", name, n))
		}
		if runtime[name] {
			bad = append(bad, name+" is a runtime name")
		}
	}
	sort.Strings(bad)
	return bad
}

// TestRubyGeneratedConstantsAreBoundOnce is the generated-output scan behind
// F-083 and F-084: no unit-level constant of a generated Ruby table unit is
// opened twice in its declaration files, and none spells a runtime name. A
// class the emitter invents that shares a spelling with a declaration or with
// another invented class is opened twice, and Ruby reopens it silently. It
// scans the keyed-wrapper corpus and every unit of the table conformance
// MANIFEST.txt the Ruby leg generates. A pure Go test: no interpreter, no
// clock.
func TestRubyGeneratedConstantsAreBoundOnce(t *testing.T) {
	t.Parallel()
	type unit struct {
		key   string
		paths []string
	}
	var units []unit
	for _, name := range []string{"declared", "twowrappers", "fixed", "control"} {
		units = append(units, unit{"keyed-wrapper-names/" + name, []string{filepath.Join(rubyKeyedNamesDir, name+".schema")}})
	}
	repo := filepath.Join("..", "..", "..")
	manifest, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(manifest), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[0] != "unit" {
			continue
		}
		var paths []string
		for _, p := range f[2:] {
			paths = append(paths, filepath.Join(repo, p))
		}
		units = append(units, unit{f[1], paths})
	}
	generated := 0
	for _, un := range units {
		u := loadUnitPaths(t, un.paths)
		files, err := Generate(u)
		if err != nil {
			continue // a unit the Ruby leg refuses by name is the refusal's test, not this one
		}
		generated++
		for _, bad := range rubyUnitConstantsOpenedTwice(u, files) {
			t.Errorf("%s: %s", un.key, bad)
		}
	}
	if generated < 10 {
		t.Fatalf("generated %d units; the scan owes the corpus and the manifest", generated)
	}
}

// loadUnitPaths checks one unit from schema files and directories of them.
func loadUnitPaths(t *testing.T, paths []string) *ir.Unit {
	t.Helper()
	var sources []check.SourceFile
	add := func(full string) {
		raw, err := os.ReadFile(full)
		if err != nil {
			t.Fatal(err)
		}
		ast, perrs := parser.Parse(full, raw)
		if len(perrs) > 0 {
			t.Fatalf("parse %s: %v", full, perrs[0])
		}
		name := filepath.Base(full)
		sources = append(sources, check.SourceFile{Path: full, Name: name, Base: strings.TrimSuffix(name, ".schema"), Bytes: raw, AST: ast})
	}
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if !st.IsDir() {
			add(p)
			continue
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".schema") {
				add(filepath.Join(p, e.Name()))
			}
		}
	}
	u, cerrs := check.Unit(sources)
	if len(cerrs) > 0 {
		t.Fatalf("check %v: %v", paths, cerrs[0])
	}
	return u
}
