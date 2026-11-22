package docgate

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// exampleTool is how the slow gate holds one target's compile examples.
// COMPILE runs the target's own compiler front end over the example against
// the unit's generated output, so a name the generated code does not carry is
// an error. PARSE is the weaker hold for a target whose examples the gate
// cannot type against generated output from here: the example is well-formed
// in its language, and the leg's own usage gate is what binds names.
type exampleTool struct {
	how  string // "compile" or "parse"
	tool string // the program, for the missing-toolchain message
	env  string // the environment variable that overrides it
	// args is the command line: the example's file, the generated output,
	// the unit's own directory (the hand headers a page keeps beside its
	// schema files, the tutorial's `-I .`), and the repository root.
	args func(file, gen, unit, root string) []string
}

var exampleTools = map[string]exampleTool{
	"cpp": {"compile", "c++", "CXX", func(file, gen, unit, root string) []string {
		return []string{"-std=c++17", "-fsyntax-only", "-Wall", "-Wextra", "-Werror", "-I", gen, "-I", unit, "-I", runtimeDir(root, "SERIALIZE", "serialize"), file}
	}},
	"c": {"compile", "cc", "CC", func(file, gen, unit, root string) []string {
		return []string{"-std=c11", "-fsyntax-only", "-Wall", "-Wextra", "-Werror", "-I", gen, "-I", unit, "-I", runtimeDir(root, "SERIALIZE_C", "serialize.c"), file}
	}},
	"js":     {"parse", "node", "NODE", func(file, _, _, _ string) []string { return []string{"--check", file} }},
	"python": {"parse", "python3", "PYTHON", func(file, _, _, _ string) []string { return []string{"-m", "py_compile", file} }},
	"ruby":   {"parse", "ruby", "RUBY", func(file, _, _, _ string) []string { return []string{"-c", file} }},
	"lua":    {"parse", "luac", "LUAC", func(file, _, _, _ string) []string { return []string{"-p", file} }},
}

// runtimeDir is where a packet example's runtime header lives: the checkout
// the environment names, else the sibling checkout the Makefile builds
// against, else the pinned copy `make deps` fetches into build/. A table
// example includes no runtime, so an absent one costs it nothing.
func runtimeDir(root, env, name string) string {
	if dir := os.Getenv(env); dir != "" {
		return dir
	}
	sibling := filepath.Join(root, "..", name)
	if _, err := os.Stat(sibling); err == nil {
		return sibling
	}
	return filepath.Join(root, "build", name)
}

// exampleGenerated generates a unit for a language once and hands back the
// directory holding the output. A unit under generated/ is checked-in output
// and is used where it sits.
type exampleGenerated struct {
	mu   sync.Mutex
	dirs map[string]string
	errs map[string]error
}

func (g *exampleGenerated) dir(t *testing.T, root, base, unit, lang string) (string, error) {
	if strings.HasPrefix(unit, "generated/") {
		return filepath.Join(root, filepath.FromSlash(unit)), nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	key := lang + " " + unit
	if dir, ok := g.dirs[key]; ok {
		return dir, g.errs[key]
	}
	dir := filepath.Join(base, lang, strings.ReplaceAll(unit, "/", "_"))
	err := func() error {
		paths, err := compiler.GatherPaths([]string{filepath.Join(root, filepath.FromSlash(unit))})
		if err != nil {
			return err
		}
		c := compiler.New()
		u, err := c.Load(paths)
		if err != nil {
			return fmt.Errorf("unit %s does not compile: %w", unit, err)
		}
		files, err := c.Generate(u, lang, nil)
		if err != nil {
			return fmt.Errorf("unit %s: generate --lang %s refused it: %w", unit, lang, err)
		}
		for name, data := range files {
			path := filepath.Join(dir, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				return err
			}
		}
		return nil
	}()
	g.dirs[key], g.errs[key] = dir, err
	return dir, err
}

// TestExamplesCompile is the slow gate (review finding F-173): every compile
// example of every page is extracted, spliced into its harness where it names
// one, and handed to its target's compiler against the generated output of
// the unit it names. The tutorial's report example printed two members the
// generated TableReport does not have; this is the test that goes red on
// that, by page and line.
//
// SCHEMA_DOCGATE_PAGES, a comma-separated list of repository-relative pages,
// narrows a run to those pages while a page is being worked on.
func TestExamplesCompile(t *testing.T) {
	slowtest.Gate(t, "the target compilers (it compiles the pages' code examples)")
	root, err := FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	examples, _, err := Examples(root)
	if err != nil {
		t.Fatal(err)
	}
	only := map[string]bool{}
	for page := range strings.SplitSeq(os.Getenv("SCHEMA_DOCGATE_PAGES"), ",") {
		if page != "" {
			only[page] = true
		}
	}
	base := t.TempDir()
	generated := &exampleGenerated{dirs: map[string]string{}, errs: map[string]error{}}
	ran := 0
	for _, e := range examples {
		if e.Class != ClassCompile || (len(only) > 0 && !only[e.Page]) {
			continue
		}
		ran++
		t.Run(fmt.Sprintf("%s:%d", e.Page, e.Line), func(t *testing.T) {
			t.Parallel()
			source, err := e.Source(root)
			if err != nil {
				t.Fatal(err)
			}
			if e.Lang == "go" {
				// a Go example is a whole file, held to the parser here; the
				// Go leg's own usage gates build and run the marked ones
				if _, err := parser.ParseFile(token.NewFileSet(), "example.go", source, parser.AllErrors); err != nil {
					t.Fatalf("%s:%d does not parse as Go: %v", e.Page, e.Line, err)
				}
				return
			}
			tool, ok := exampleTools[e.Lang]
			if !ok {
				t.Fatalf("%s:%d is marked compile, and the gate has no compiler step for %s: its examples are a leg's usage example or a fragment", e.Page, e.Line, e.Lang)
			}
			program := tool.tool
			if override := os.Getenv(tool.env); override != "" {
				program = override
			}
			if _, err := exec.LookPath(program); err != nil {
				t.Fatalf("%s is not on PATH, so %s:%d cannot be held (%s=<path> names it): %v", program, e.Page, e.Line, tool.env, err)
			}
			gen := ""
			if tool.how == "compile" {
				if e.Unit == "" {
					t.Fatalf("%s:%d is a %s example and names no unit to compile against", e.Page, e.Line, e.Lang)
				}
				gen, err = generated.dir(t, root, base, e.Unit, e.Lang)
				if err != nil {
					t.Fatalf("%s:%d: %v", e.Page, e.Line, err)
				}
			}
			dir := t.TempDir()
			file := filepath.Join(dir, "example"+exampleExt[e.Lang])
			if err := os.WriteFile(file, []byte(source), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(program, tool.args(file, gen, filepath.Join(root, filepath.FromSlash(e.Unit)), root)...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s:%d does not %s against %s: %v\n%s", e.Page, e.Line, tool.how, e.Unit, err, out)
			}
		})
	}
	if ran == 0 && len(only) == 0 {
		t.Fatal("no compile example was found on any page")
	}
}
