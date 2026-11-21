package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// root is the repository this test runs in: two levels above the package.
func root(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Makefile")); err != nil {
		t.Fatalf("no Makefile at %s: %v", dir, err)
	}
	return dir
}

// recipes is the text of everything that can run a sabotage: the Makefile,
// its fragments, and the scripts under test/.
func recipes(t *testing.T, root string) string {
	t.Helper()
	var all strings.Builder
	n := 0
	read := func(path string) {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		all.Write(b)
		all.WriteByte('\n')
		n++
	}
	read(filepath.Join(root, "Makefile"))
	for _, dir := range []string{"make", "test"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() {
				return err
			}
			if info, err := e.Info(); err != nil || info.Size() > 1<<20 {
				return err
			}
			switch filepath.Ext(path) {
			case ".mk", ".sh", ".go", ".py", "":
				read(path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if n < 50 {
		t.Fatalf("read %d recipe files; the scan is looking in the wrong place", n)
	}
	return all.String()
}

func nameByte(c byte) bool {
	return c == '-' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z')
}

// named reports whether text holds name as a whole word.
func named(text, name string) bool {
	for from := 0; ; {
		i := strings.Index(text[from:], name)
		if i < 0 {
			return false
		}
		i += from
		end := i + len(name)
		if (i == 0 || !nameByte(text[i-1])) && (end == len(text) || !nameByte(text[end])) {
			return true
		}
		from = end
	}
}

var composedName = regexp.MustCompile(`(?:^|[^a-z0-9$)/_-])([a-z][a-z0-9-]*(?:\$[({]?[A-Za-z0-9_]+[)}]?[a-z0-9-]*)+)`)
var makeVariable = regexp.MustCompile(`\$\$?(\([^)]*\)|\{[^}]*\}|[A-Za-z0-9_]+)`)

// composed is the recipes' `-name` arguments that are built from a variable,
// each as a pattern: `packet-finite-go-$(1)` names both directions. An
// argument that is a variable and nothing else names nothing by itself.
func composed(text string) []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, m := range composedName.FindAllStringSubmatch(text, -1) {
		literal := makeVariable.ReplaceAllString(m[1], "")
		if len(literal) < 6 {
			continue
		}
		parts := makeVariable.Split(m[1], -1)
		for i := range parts {
			parts[i] = regexp.QuoteMeta(parts[i])
		}
		out = append(out, regexp.MustCompile("^"+strings.Join(parts, "[a-z0-9-]+")+"$"))
	}
	return out
}

// A sabotage no recipe names is a control that cannot fail: the rule it
// removes is taken out of nothing, on no lane, and the table entry reads as
// if a gate stood behind it.
func TestEverySabotageIsNamedByARecipe(t *testing.T) {
	text := recipes(t, root(t))
	patterns := composed(text)
	if len(patterns) == 0 {
		t.Fatal("no composed -name argument found; the recipes changed shape and this test reads nothing")
	}
	var orphans []string
	for name := range sabotages {
		used := named(text, name)
		for _, p := range patterns {
			used = used || p.MatchString(name)
		}
		if !used {
			orphans = append(orphans, name)
		}
	}
	sort.Strings(orphans)
	if len(sabotages) < 200 {
		t.Fatalf("%d sabotages registered; the table did not load", len(sabotages))
	}
	for _, name := range orphans {
		t.Errorf("sabotage %q is run by no recipe in the Makefile, make/ or test/: wire it into a negative control or delete it", name)
	}
}
