// Package ci is the gate on this repo's own continuous integration: two
// invariants that live in .github/ rather than in Go, and that no reader can
// hold by hand across three workflow files. It rides `go test ./...` — so the
// go-test job, `make test` and certification all carry it — and ci.yml's lint
// job names it as its own step.
//
// TestEveryActionIsPinnedToACommit (issue #472) requires every `uses:` under
// .github/workflows to name a full 40-hex commit SHA, with the tag it was
// resolved from in a trailing comment, and refuses `@latest` anywhere. A tag
// and a branch are equally mutable: whoever can repoint `v7` or `stable` in a
// third-party repository runs their code in this repo's jobs, and cla.yml's job
// hands an org PAT to an action whose upstream is archived. Dependabot's weekly
// github-actions pass keeps the SHAs current — it bumps a pinned SHA and
// rewrites the comment — which is a bump policy ON TOP of immutability, not a
// substitute for it.
//
// TestEveryProjectTargetsThePinnedSDK (issue #470) requires the .NET SDK pin at
// .github/dotnet-version to be exactly the version every csproj's
// TargetFramework names, and refuses a literal `dotnet-version:` in a workflow.
// All three of certify.yml's certification jobs asked for 8.0 while every
// project targeted net10.0, so certification ran on whatever SDK the runner
// image happened to carry rather than on one the workflow installed. One pin,
// read by both files, is what makes that unrepresentable; this test is what
// keeps the pin and the projects the same number.
package ci

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	commitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sdkPin    = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)
	targetTFM = regexp.MustCompile(`<TargetFrameworks?>([^<]*)</TargetFrameworks?>`)
)

// dotnetPinFile is the ONE home of the .NET SDK version, relative to the repo
// root: every workflow that installs the SDK reads this file, and the C#
// conformance row names it rather than restating the version.
const dotnetPinFile = ".github/dotnet-version"

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// workflows reads every workflow file, keyed by its base name. An empty set is
// a failure rather than a pass: a gate over nothing proves nothing.
func workflows(t *testing.T, root string) map[string]string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no workflow under .github/workflows — this gate would pass over an empty set")
	}
	sort.Strings(paths)
	out := map[string]string{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		out[filepath.Base(path)] = string(data)
	}
	return out
}

// line is one workflow line with its comment stripped and its list dash
// removed, so a caller reads `uses: ...` the same whether it opened a step or
// continued one. Comment-only lines are dropped: prose may quote what these
// gates refuse.
type line struct {
	where   string // "ci.yml:403"
	full    string // the whole line, comment and all — what a `run:` step runs
	text    string // "uses: actions/checkout@<sha>"
	comment string // "v7.0.1"
}

func lines(name, data string) []line {
	var out []line
	for i, raw := range strings.Split(data, "\n") {
		full := strings.TrimSpace(raw)
		if full == "" || strings.HasPrefix(full, "#") {
			continue
		}
		text, comment := full, ""
		if before, after, found := strings.Cut(full, "#"); found {
			text, comment = strings.TrimSpace(before), strings.TrimSpace(after)
		}
		out = append(out, line{
			where:   fmt.Sprintf("%s:%d", name, i+1),
			full:    full,
			text:    strings.TrimPrefix(text, "- "),
			comment: comment,
		})
	}
	return out
}

func TestEveryActionIsPinnedToACommit(t *testing.T) {
	pinned := 0
	for name, data := range workflows(t, repoRoot(t)) {
		for _, l := range lines(name, data) {
			if strings.Contains(l.full, "@latest") {
				t.Errorf("%s: %q runs @latest — name the version, so no tool release can change a verdict with no commit here", l.where, l.full)
			}
			if !strings.HasPrefix(l.text, "uses:") {
				continue
			}
			action := strings.TrimSpace(strings.TrimPrefix(l.text, "uses:"))
			if strings.HasPrefix(action, "./") {
				pinned++ // an action in this tree is pinned by the commit under test
				continue
			}
			at := strings.LastIndex(action, "@")
			if at < 0 || !commitSHA.MatchString(action[at+1:]) {
				t.Errorf("%s: %s rides a mutable ref — pin the commit, which `gh api repos/<owner>/<repo>/git/ref/tags/<tag>` resolves (dereference an annotated tag to its commit)", l.where, action)
				continue
			}
			if l.comment == "" {
				t.Errorf("%s: %s carries no trailing comment — name the tag the SHA came from, for the next reader and for dependabot", l.where, action)
			}
			pinned++
		}
	}
	if pinned == 0 {
		t.Error("no `uses:` found in any workflow — this gate would pass over an empty set")
	}
}

func TestEveryProjectTargetsThePinnedSDK(t *testing.T) {
	root := repoRoot(t)

	raw, err := os.ReadFile(filepath.Join(root, dotnetPinFile))
	if err != nil {
		t.Fatalf("%s is the one home of the .NET SDK version: %v", dotnetPinFile, err)
	}
	pin := strings.TrimSpace(string(raw))
	if !sdkPin.MatchString(pin) {
		t.Fatalf("%s holds %q — it is the SDK version both workflows install, like 10.0", dotnetPinFile, pin)
	}
	want := "net" + pin

	// Every C# project in the tree, discovered rather than listed. Build
	// output is skipped so a local `make` cannot change the verdict.
	skip := map[string]bool{".git": true, "bin": true, "obj": true, "build": true, "dist": true, "node_modules": true, "target": true, "_build": true, "deps": true}
	projects := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skip[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".csproj" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		found := targetTFM.FindAllStringSubmatch(string(src), -1)
		if len(found) == 0 {
			t.Errorf("%s: no <TargetFramework> — the pin has nothing to agree with", rel)
			return nil
		}
		projects++
		for _, m := range found {
			if got := strings.TrimSpace(m[1]); got != want {
				t.Errorf("%s targets %s while %s pins %s: certification would run on an SDK no workflow installs (issue #470)", rel, got, dotnetPinFile, pin)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if projects == 0 {
		t.Fatal("no .csproj found — this gate would pass over an empty set")
	}

	// The C# conformance row names WHERE the pin lives; the workflow reads
	// that file. A row that restated the version would be a second home.
	rowPath := filepath.Join(root, "test", "conformance", "cs", "ci.json")
	rowData, err := os.ReadFile(rowPath)
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]string
	if err := json.Unmarshal(rowData, &row); err != nil {
		t.Fatal(err)
	}
	if row["dotnet"] != dotnetPinFile {
		t.Errorf("test/conformance/cs/ci.json names %q as its .NET pin; it must name %s, the file the workflow reads", row["dotnet"], dotnetPinFile)
	}

	// And no workflow keeps a version of its own.
	for name, data := range workflows(t, root) {
		if strings.Contains(data, "setup-dotnet@") && !strings.Contains(data, "cat "+dotnetPinFile) {
			t.Errorf("%s installs the .NET SDK without reading %s", name, dotnetPinFile)
		}
		for _, l := range lines(name, data) {
			if !strings.HasPrefix(l.text, "dotnet-version:") {
				continue
			}
			if v := strings.TrimSpace(strings.TrimPrefix(l.text, "dotnet-version:")); !strings.HasPrefix(v, "${{") {
				t.Errorf("%s: dotnet-version: %s is a second home for the pin — read %s instead (issue #470)", l.where, v, dotnetPinFile)
			}
		}
	}
}

// makeSources is every file the build itself is written in, relative to the
// repo root: the root Makefile and the per-language includes it pulls in.
var makeSources = []string{"Makefile", "make"}

// goPackageArg matches a repo-relative Go package path as it appears on a
// `go run` / `go build` / `go test` command line: a leading ./ and no wildcard.
var goPackageArg = regexp.MustCompile(`(?:^|\s)(\./[A-Za-z0-9_./-]+)`)

// goInvocation matches one go subcommand invocation, after line continuations
// have been joined.
var goInvocation = regexp.MustCompile(`\bgo\s+(?:run|build|test|vet)\b[^\n]*`)

// TestEveryPackageTheBuildRunsIsCommitted requires every repo-relative Go
// package the Makefile invokes to be TRACKED BY GIT, not merely present on the
// author's disk.
//
// The class it closes: .gitignore hid a source tree, `git add` skipped it
// without a word, the author's `make test` stayed green because the file was
// right there in the working copy, and the first fresh checkout to run the
// chain was main's own certification. tools/sabotage, which four wide-text
// negative controls run, landed that way in #556 and reddened every certify
// leg at `make test`. A working tree cannot see the difference between a file
// it owns and a file the repository owns. Git can, so the gate asks git.
func TestEveryPackageTheBuildRunsIsCommitted(t *testing.T) {
	root := repoRoot(t)

	var text strings.Builder
	for _, src := range makeSources {
		path := filepath.Join(root, src)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("read %s: %v", src, err)
		}
		files := []string{path}
		if info.IsDir() {
			files, err = filepath.Glob(filepath.Join(path, "*.mk"))
			if err != nil {
				t.Fatal(err)
			}
		}
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			// a recipe wraps across lines with a trailing backslash, and the
			// package path is often on the wrapped half
			text.WriteString(strings.ReplaceAll(string(data), "\\\n", " "))
			text.WriteString("\n")
		}
	}

	packages := map[string]bool{}
	for _, invocation := range goInvocation.FindAllString(text.String(), -1) {
		for _, match := range goPackageArg.FindAllStringSubmatch(invocation, -1) {
			pkg := match[1]
			if strings.Contains(pkg, "...") {
				continue
			}
			packages[strings.TrimSuffix(pkg, "/")] = true
		}
	}
	if len(packages) == 0 {
		t.Fatal("no repo-relative go packages found in the make sources: this gate is looking at the wrong text")
	}

	names := make([]string, 0, len(packages))
	for pkg := range packages {
		names = append(names, pkg)
	}
	sort.Strings(names)
	t.Logf("packages the make sources run: %s", strings.Join(names, " "))

	for _, pkg := range names {
		dir := strings.TrimPrefix(pkg, "./")
		tracked := gitLsFiles(t, root, dir)
		if len(tracked) == 0 {
			t.Errorf("the build runs `go ... %s` but git tracks no file under %s/. "+
				"The package exists on the author's disk and in no checkout. "+
				"Commit it, and check .gitignore is not swallowing the path.", pkg, dir)
		}
	}
}

// gitLsFiles returns the tracked paths under dir, relative to root.
func gitLsFiles(t *testing.T, root, dir string) []string {
	t.Helper()
	cmd := exec.Command("git", "ls-files", "--", dir)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files %s: %v", dir, err)
	}
	var paths []string
	for line := range strings.SplitSeq(string(out), "\n") {
		if line != "" {
			paths = append(paths, line)
		}
	}
	return paths
}
