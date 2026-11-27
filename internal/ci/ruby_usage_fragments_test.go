package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/compiler"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/ruby/rubyrun"
)

var rubyUsageMarker = regexp.MustCompile(`<!-- ruby-usage ([A-Za-z0-9_./-]+) -->`)

type rubyUsageFragment struct {
	unit   string
	source string
}

func rubyUsageFragments(t *testing.T, page string) []rubyUsageFragment {
	t.Helper()
	var out []rubyUsageFragment
	rest := page
	for {
		loc := rubyUsageMarker.FindStringSubmatchIndex(rest)
		if loc == nil {
			break
		}
		unit := rest[loc[2]:loc[3]]
		block, after, ok := strings.Cut(rest[loc[1]:], "<!-- /ruby-usage -->")
		if !ok {
			t.Fatalf("the ruby-usage fragment for %s has no closing <!-- /ruby-usage --> marker", unit)
		}
		_, source, ok := strings.Cut(block, "```ruby\n")
		if !ok {
			t.Fatalf("the ruby-usage fragment for %s has no ```ruby fence", unit)
		}
		source, _, ok = strings.Cut(source, "```")
		if !ok {
			t.Fatalf("the ruby-usage fragment for %s has an unclosed fence", unit)
		}
		out = append(out, rubyUsageFragment{unit: unit, source: source})
		rest = after
	}
	return out
}

func rubyUsageUnits(t *testing.T, root string, fragments []rubyUsageFragment) map[string]map[string][]byte {
	t.Helper()
	c := compiler.New()
	units := map[string]map[string][]byte{}
	for _, f := range fragments {
		if _, ok := units[f.unit]; ok {
			continue
		}
		dir := filepath.Join(root, filepath.FromSlash(f.unit))
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("a ruby-usage fragment names %s, which is not a directory of the repository", f.unit)
		}
		paths, err := compiler.GatherPaths([]string{dir})
		if err != nil {
			t.Fatalf("%s: %v", f.unit, err)
		}
		u, err := c.Load(paths)
		if err != nil {
			t.Fatalf("%s does not compile: %v", f.unit, err)
		}
		files, err := c.Generate(u, "ruby", nil)
		if err != nil {
			t.Fatalf("%s: schema generate --lang ruby refused it: %v", f.unit, err)
		}
		units[f.unit] = files
	}
	return units
}

func rubyUsageTree(t *testing.T, files map[string][]byte, source string) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	prelude := "# frozen_string_literal: true\n\n"
	if err := os.WriteFile(filepath.Join(dir, "main.rb"), []byte(prelude+source), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func rubyUsageCorpus(t *testing.T) ([]rubyUsageFragment, map[string]map[string][]byte) {
	t.Helper()
	root := repoRoot(t)
	page, err := os.ReadFile(filepath.Join(root, "docs", "USAGE.md"))
	if err != nil {
		t.Fatal(err)
	}
	fragments := rubyUsageFragments(t, string(page))
	if len(fragments) == 0 {
		t.Fatal("docs/USAGE.md carries no <!-- ruby-usage <unit> --> fragment")
	}
	var packet, table bool
	for _, f := range fragments {
		packet = packet || strings.Contains(f.source, ".write")
		table = table || strings.Contains(f.source, ".save")
	}
	if !packet || !table {
		t.Fatalf("docs/USAGE.md's Ruby fragments must show a packet write and a table save; packet=%v table=%v", packet, table)
	}
	return fragments, rubyUsageUnits(t, root, fragments)
}

func TestRubyUsageFragmentsRun(t *testing.T) {
	t.Parallel()
	fragments, units := rubyUsageCorpus(t)
	for i, f := range fragments {
		for _, floor := range []bool{false, true} {
			pin := "current"
			if floor {
				pin = "floor"
			}
			t.Run(filepath.Base(f.unit)+"-"+pin, func(t *testing.T) {
				t.Parallel()
				dir := rubyUsageTree(t, units[f.unit], f.source)
				if floor {
					rubyrun.RunFloor(t, dir, "main.rb", nil)
				} else {
					rubyrun.Run(t, dir, "main.rb", nil)
				}
				t.Logf("docs/USAGE.md Ruby fragment %d (%s) ran", i+1, f.unit)
			})
		}
	}
}

func TestRubyUsageFragmentsRunNegativeControl(t *testing.T) {
	t.Parallel()
	fragments, units := rubyUsageCorpus(t)
	controls := []struct {
		name, file, from, to, want string
	}{
		{"packet-write-renamed", "/Types.rb", "    def write\n", "    def write_renamed\n", "write"},
		{"table-save-renamed", "/Graph.rb", "    def save\n", "    def save_renamed\n", "save"},
		{"table-load-renamed", "/Graph.rb", "    def self.load(data", "    def self.load_renamed(data", "load"},
		{"runtime-refused-renamed", "/_schema.rb", "class Refused < StandardError", "class Refusal < StandardError", "Refused"},
	}
	for _, c := range controls {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			planted := 0
			for _, f := range fragments {
				files := map[string][]byte{}
				hit := false
				for name, data := range units[f.unit] {
					if strings.HasSuffix(name, c.file) && strings.Contains(string(data), c.from) {
						data = []byte(strings.ReplaceAll(string(data), c.from, c.to))
						hit = true
					}
					files[name] = data
				}
				if !hit || !strings.Contains(f.source, c.want) {
					continue
				}
				planted++
				dir := rubyUsageTree(t, files, f.source)
				stdout, stderr, err := rubyrun.Try(t, rubyrun.Interpreter(t), dir, "main.rb", nil)
				if err == nil {
					t.Fatalf("NEGATIVE CONTROL FAILED: with %s planted the %s fragment still ran\nstdout:\n%s", c.name, f.unit, stdout)
				}
				if !strings.Contains(stderr+stdout, c.want) {
					t.Fatalf("with %s planted the %s fragment failed without naming %s:\nstderr:\n%s\nstdout:\n%s", c.name, f.unit, c.want, stderr, stdout)
				}
			}
			if planted == 0 {
				t.Fatalf("NEGATIVE CONTROL: %s planted into no fragment (%q in %s)", c.name, c.from, c.file)
			}
		})
	}
}
