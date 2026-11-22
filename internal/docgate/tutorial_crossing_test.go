package docgate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// The tutorial's Go crossing (Parts 12 and 13) is the one place a reader
// leaves C++ and builds a Go program against generated code, so it is the one
// place the page's go.mod has to be right: the consumer imports the generated
// package, the generated package imports the table runtime from the compiler's
// own module, and the packet wire's runtime is serialize.go. The parse-only
// hold TestExamplesCompile gives a Go example cannot see any of that; the
// page's go.mod once named a module path no build could resolve and the walk
// still passed. This test is the reader: it walks the two parts in page order,
// writes every "`name`, entire:" file the page gives, runs every `$` command
// the page shows with the CLI built from this tree, and holds each command's
// output to the lines the page prints under it.
const (
	crossingPage   = "docs/TUTORIAL.md"
	crossingFrom   = "## Part 12:"
	crossingUntil  = "### The block" // Part 13's last section, which runs nothing new
	crossingUnit   = "internal/docgate/testdata/units/tutorial/part12"
	crossingBefore = "internal/docgate/testdata/units/tutorial/part11" // the unit as Part 12 finds it
	crossingTree   = "internal/docgate/testdata/tutorial-tree"         // Part 9's tree as Part 13 cooks it
)

// crossingSkips are the commands on the two parts the test does not run, by
// the start of the command, each with the reason. Everything else the page
// shows, it runs.
var crossingSkips = []struct{ prefix, reason string }{
	{"echo $?", "the status of the command before it, which the test already requires to be 0"},
	{"for l in ", "it writes nine generated trees under /tmp, outside the test's directory; every target's generate is held by its own leg"},
	{"go mod tidy", "it downloads the two required modules; offline, the test points them at this tree and at the pinned serialize.go instead"},
	{"./cook ", "Part 11's program, compiled by TestExamplesCompile"},
}

func crossingSkip(command string) (string, bool) {
	for _, s := range crossingSkips {
		if strings.HasPrefix(command, s.prefix) {
			return s.reason, true
		}
	}
	return "", false
}

// crossingMustRun are the Go steps the walk must reach, so a page that drops
// one of them fails here rather than passing on fewer commands.
var crossingMustRun = []string{
	"schema generate --lang go --out gengo .",
	"go run consume.go",
	"go run readpacket.go",
	"go run opencook.go",
}

type crossingStep struct {
	line    int
	command string
	want    []string
}

func TestTutorialGoCrossing(t *testing.T) {
	slowtest.Gate(t, "go, c++ and the CLI it builds (it walks the tutorial's Go crossing)")
	root, err := FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"go", "c++"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("%s is not on PATH, so the tutorial's Go crossing cannot be walked: %v", tool, err)
		}
	}
	serializeGo := slowtest.SerializeGo(t, root)
	serializeCpp := runtimeDir(root, "SERIALIZE", "serialize")
	if _, err := os.Stat(filepath.Join(serializeCpp, "serialize.h")); err != nil {
		t.Fatalf("the C++ runtime serialize is absent at %s (SERIALIZE=<dir> names it; make deps fetches it): %v", serializeCpp, err)
	}

	page, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(crossingPage)))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(page), "\n")
	from, until := -1, -1
	for i, l := range lines {
		if from < 0 && strings.HasPrefix(l, crossingFrom) {
			from = i
		} else if from >= 0 && until < 0 && strings.HasPrefix(l, crossingUntil) {
			until = i
		}
	}
	if from < 0 || until < 0 {
		t.Fatalf("%s: the crossing's span (%q to %q) is not on the page", crossingPage, crossingFrom, crossingUntil)
	}

	// The reader's layout: the unit's directory, with the runtimes beside it.
	base := t.TempDir()
	bin := filepath.Join(base, "bin")
	work := filepath.Join(base, "starlight")
	copyTree(t, filepath.Join(root, filepath.FromSlash(crossingUnit)), work)
	copyTree(t, filepath.Join(root, filepath.FromSlash(crossingTree)), filepath.Join(work, "tree"))
	if err := os.Symlink(serializeCpp, filepath.Join(base, "serialize")); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", filepath.Join(bin, "schema"), "./cmd/schema")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the CLI: %v\n%s", err, out)
	}
	env := append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GOFLAGS=-mod=mod", "GOPROXY=off", "GOWORK=off", "GOTOOLCHAIN=local")
	run := func(command string) (string, error) {
		cmd := exec.Command("sh", "-c", command)
		cmd.Dir = work
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return strings.TrimRight(string(out), "\n"), err
	}
	// What the earlier parts left: the save-game baseline Part 10 committed,
	// taken before Part 12 added its tables ("Adding three tables passes the
	// baseline"), and gen/, which Part 12 builds against.
	before := filepath.Join(base, "before")
	copyTree(t, filepath.Join(root, filepath.FromSlash(crossingBefore)), before)
	baseline := exec.Command(filepath.Join(bin, "schema"), "tables-baseline", "--update", "--reason", "first baseline", ".")
	baseline.Dir = before
	if out, err := baseline.CombinedOutput(); err != nil {
		t.Fatalf("the baseline before Part 12: %v\n%s", err, out)
	}
	copyTree(t, filepath.Join(before, "tables.baseline"), filepath.Join(work, "tables.baseline"))
	if out, err := run("schema generate --lang cpp --out gen . 2>/dev/null"); err != nil {
		t.Fatalf("generating gen/: %v\n%s", err, out)
	}

	ran := map[string]bool{}
	var restore func()
	pending := "" // the name an "entire:" label gave the next fenced block
	for i := from; i < until; i++ {
		l := lines[i]
		if strings.HasPrefix(l, "`") && strings.HasSuffix(l, "`, entire:") {
			pending = strings.TrimSuffix(strings.TrimPrefix(l, "`"), "`, entire:")
			continue
		}
		if !strings.HasPrefix(l, "```") {
			continue
		}
		end := i + 1
		for end < until && !strings.HasPrefix(lines[end], "```") {
			end++
		}
		body := lines[i+1 : end]
		lineNo := i + 1
		i = end
		switch {
		case pending != "":
			text := strings.Join(body, "\n") + "\n"
			if pending == "go.mod" {
				text += offlineReplaces(text, root, serializeGo)
			}
			writeFile(t, filepath.Join(work, pending), text)
			pending = ""
		case len(body) > 0 && strings.HasPrefix(body[0], "$ "):
			for _, s := range consoleSteps(lineNo, body) {
				if reason, skip := crossingSkip(s.command); skip {
					t.Logf("%s:%d: not run: %s (%s)", crossingPage, s.line, s.command, reason)
					continue
				}
				got, err := run(s.command)
				if err != nil {
					t.Fatalf("%s:%d: `%s` failed: %v\n%s", crossingPage, s.line, s.command, err, got)
				}
				if want := strings.Join(s.want, "\n"); got != want {
					t.Errorf("%s:%d: `%s` printed\n%s\nand the page shows\n%s", crossingPage, s.line, s.command, got, want)
				}
				ran[s.command] = true
			}
			if restore != nil {
				restore()
				restore = nil
			}
		case len(body) > 0 && strings.HasPrefix(body[0], "fixed table "):
			// a declaration edited in place: it holds for the next console
			// block only, and the page takes it back out after that
			restore = patchDeclaration(t, work, body)
		}
	}
	for _, c := range crossingMustRun {
		if !ran[c] {
			t.Errorf("%s: the Go crossing never ran `%s`; the page lost a step", crossingPage, c)
		}
	}
}

// consoleSteps splits a console block into its commands, each with the output
// lines the page prints under it.
func consoleSteps(fence int, body []string) []crossingStep {
	var steps []crossingStep
	for j, l := range body {
		if c, ok := strings.CutPrefix(l, "$ "); ok {
			steps = append(steps, crossingStep{line: fence + 1 + j, command: c})
		} else if len(steps) > 0 {
			steps[len(steps)-1].want = append(steps[len(steps)-1].want, l)
		}
	}
	return steps
}

// offlineReplaces points the page's two requirements at what this test has on
// disk: the compiler's module is this tree, and serialize.go is the pinned
// checkout. They stand in for `go mod tidy`'s download and change nothing the
// page wrote: a module the page already replaces keeps the page's replace, and
// a requirement the page lost still fails the build.
func offlineReplaces(gomod, root, serializeGo string) string {
	var b strings.Builder
	for _, r := range [][2]string{
		{"github.com/mas-bandwidth/schema/v2", root},
		{"github.com/mas-bandwidth/serialize.go/v2", serializeGo},
	} {
		if !strings.Contains(gomod, "replace "+r[0]+" ") {
			fmt.Fprintf(&b, "\nreplace %s => %s\n", r[0], r[1])
		}
	}
	return b.String()
}

// patchDeclaration replaces the declaration the block names in the unit's
// files with the block, and returns the function that puts it back.
func patchDeclaration(t *testing.T, work string, block []string) func() {
	t.Helper()
	head := block[0]
	files, err := filepath.Glob(filepath.Join(work, "*.schema"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		start := strings.Index(text, head+"\n")
		if start < 0 {
			continue
		}
		brace := strings.Index(text[start:], "\n}\n")
		if brace < 0 {
			t.Fatalf("%s: %q has no closing brace", f, head)
		}
		patched := text[:start] + strings.Join(block, "\n") + text[start+brace+2:]
		writeFile(t, f, patched)
		return func() { writeFile(t, f, text) }
	}
	t.Fatalf("no file of the unit declares %q", head)
	return nil
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		writeFile(t, filepath.Join(to, rel), string(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
