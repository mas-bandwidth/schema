package ci

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/compiler"
)

const goBenchMakefile = "make/go.mk"

var goBenchRules = []string{
	"generated/bench/tables/go/.stamp",
	"generated/bench/paired/go/.stamp",
	"generated/bench/go/.stamp",
}

type goBenchCommand struct {
	out     string
	schemas []string
}

func goBenchRecipe(t *testing.T, root, rule string) []goBenchCommand {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, goBenchMakefile))
	if err != nil {
		t.Fatalf("read %s: %v", goBenchMakefile, err)
	}
	found := false
	var commands []goBenchCommand
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, rule+":") {
			found = true
			continue
		}
		if !found || line == "" {
			continue
		}
		if !strings.HasPrefix(line, "\t") {
			break
		}
		fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "\t")))
		if len(fields) == 0 {
			continue
		}
		fields[0] = strings.TrimPrefix(fields[0], "@")
		if fields[0] != "./bin/schema" || len(fields) < 7 ||
			fields[1] != "generate" || fields[2] != "--lang" || fields[3] != "go" || fields[4] != "--out" {
			continue
		}
		commands = append(commands, goBenchCommand{
			out:     fields[5],
			schemas: append([]string(nil), fields[6:]...),
		})
	}
	if !found {
		t.Fatalf("%s: no rule %s", goBenchMakefile, rule)
	}
	if len(commands) == 0 {
		t.Fatalf("%s: rule %s runs no Go generate command", goBenchMakefile, rule)
	}
	return commands
}

func goBenchFirstDiff(committed, emitted []byte) string {
	have := strings.Split(string(committed), "\n")
	want := strings.Split(string(emitted), "\n")
	for i := 0; i < len(have) && i < len(want); i++ {
		if have[i] != want[i] {
			return fmt.Sprintf("first differing line %d: committed %q, compiler emits %q", i+1, have[i], want[i])
		}
	}
	if len(have) != len(want) {
		return fmt.Sprintf("line counts differ: committed %d, compiler emits %d", len(have), len(want))
	}
	return "bytes differ with no differing line"
}

func TestGoBenchTreesAreCurrent(t *testing.T) {
	t.Parallel()
	start := time.Now()
	root := repoRoot(t)
	for _, rule := range goBenchRules {
		tree := strings.TrimSuffix(rule, "/.stamp")
		c := compiler.New()
		c.TablesBaseline = true
		c.SchemaLock = true
		emitted := map[string][]byte{}
		for _, command := range goBenchRecipe(t, root, rule) {
			paths := make([]string, 0, len(command.schemas))
			for _, schema := range command.schemas {
				paths = append(paths, filepath.Join(root, schema))
			}
			gathered, err := compiler.GatherPaths(paths)
			if err != nil {
				t.Fatalf("%s: %v", rule, err)
			}
			unit, err := c.Load(gathered)
			if err != nil {
				t.Fatalf("%s: %v", rule, err)
			}
			files, err := c.Generate(unit, "go", compiler.Options{"surface": ""})
			if err != nil {
				t.Fatalf("%s: %v", rule, err)
			}
			for name, data := range files {
				rel := filepath.Join(command.out, name)
				if rel != tree && !strings.HasPrefix(rel, tree+string(filepath.Separator)) {
					t.Fatalf("%s: command writes %s outside the tree", rule, rel)
				}
				if _, dup := emitted[rel]; dup {
					t.Fatalf("%s: emitted twice by the rule", rel)
				}
				emitted[rel] = data
			}
		}
		names := make([]string, 0, len(emitted))
		for name := range emitted {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			committed, err := os.ReadFile(filepath.Join(root, name))
			if err != nil {
				t.Errorf("%s: the compiler emits this file and it is not committed: %v", name, err)
				continue
			}
			if string(committed) != string(emitted[name]) {
				t.Errorf("%s: %s", name, goBenchFirstDiff(committed, emitted[name]))
			}
		}
		err := filepath.WalkDir(filepath.Join(root, tree), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if _, ok := emitted[rel]; !ok {
				t.Errorf("%s: committed .go file the compiler does not emit", rel)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("%s: %v", tree, err)
		}
	}
	t.Logf("three Go bench trees compared with the in-process compiler in %s", time.Since(start))
}
