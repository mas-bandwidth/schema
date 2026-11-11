package ci

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// SIBLINGS, at the repository root, lists the pinned sibling checkouts the
// build reads beside the worktree, as data a tool can stage without running
// the build. One line per sibling:
//
//	<path> <clone-url> <tag> <40-hex commit>
//
// Blank lines and # comments are allowed. The make files stay the authority:
// serialize-siblings stages the C++/C/CS/Rust/JS checkouts through
// stage-sibling and deps-go stages serialize.go. These tests hold the file to
// what make resolves, in both directions, and hold `make deps` to leaving a
// sibling that is already present alone.

// siblingPin is one sibling as make stages it. commit is empty when make pins
// only the tag.
type siblingPin struct {
	path, url, tag, commit string
}

// siblingLine is one parsed SIBLINGS line.
type siblingLine struct {
	n                   int
	path, url, tag, sha string
}

// siblingsGo names the variables deps-go stages serialize.go through: the
// path, the clone URL, the tag and the commit the tag must resolve to.
var siblingsGo = [4]string{"SERIALIZE_GO", "SERIALIZE_GO_REPO", "SERIALIZE_GO_PIN", "SERIALIZE_GO_COMMIT"}

// siblingsStageRE reads one stage-sibling call: PIN variable, mirror path,
// clone URL, sibling path, label.
var siblingsStageRE = regexp.MustCompile(`\$\(call stage-sibling,\$\(([A-Z0-9_]+)\),[^,]*,([^,]+),([^,]+),[^)]*\)`)

var siblingsSHARE = regexp.MustCompile(`^[0-9a-f]{40}$`)

// TestSiblingsFileMatchesMakePins holds every SIBLINGS line equal to the pin
// make resolves for that sibling (tag, clone URL and, where make pins one, the
// commit), and every sibling make stages present in the file. The values are
// read by running make, as TestMainRedsPinsMatchCIFull does, so a later
// assignment or an include that moves a pin turns this red. The subtest is the
// negative control: a drifted tag, a dropped line and an extra line each fail.
func TestSiblingsFileMatchesMakePins(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "SIBLINGS"))
	if err != nil {
		t.Fatalf("SIBLINGS is absent, so the pinned sibling checkouts the build reads are not data a tool can stage: %v", err)
	}
	lines, problems := parseSiblings(string(data))
	for _, p := range problems {
		t.Error(p)
	}

	want := siblingsFromMake(t, root)
	for _, m := range siblingsMismatches(lines, want) {
		t.Error(m)
	}

	t.Run("a drifted file is refused", func(t *testing.T) {
		t.Parallel()
		if len(want) < 2 {
			t.Fatalf("make stages %d siblings; the control needs two", len(want))
		}
		exact := make([]siblingLine, len(want))
		for i, p := range want {
			sha := p.commit
			if sha == "" {
				sha = strings.Repeat("0", 40)
			}
			exact[i] = siblingLine{n: i + 1, path: p.path, url: p.url, tag: p.tag, sha: sha}
		}
		if m := siblingsMismatches(exact, want); len(m) != 0 {
			t.Fatalf("the exact copy of make's pins is refused: %v", m)
		}
		drifted := slices.Clone(exact)
		drifted[0].tag = "v0.0.0"
		if len(siblingsMismatches(drifted, want)) == 0 {
			t.Errorf("a SIBLINGS tag that differs from make's pin passed")
		}
		if len(siblingsMismatches(exact[1:], want)) == 0 {
			t.Errorf("a SIBLINGS file missing a sibling make stages passed")
		}
		extra := append(slices.Clone(exact), siblingLine{n: 99, path: "../extra", url: "https://example.invalid/extra.git", tag: "v1.0.0", sha: strings.Repeat("1", 40)})
		if len(siblingsMismatches(extra, want)) == 0 {
			t.Errorf("a SIBLINGS line for a sibling make does not stage passed")
		}
		for i, p := range want {
			if p.commit == "" {
				continue
			}
			wrong := slices.Clone(exact)
			wrong[i].sha = strings.Repeat("f", 40)
			if len(siblingsMismatches(wrong, want)) == 0 {
				t.Errorf("a SIBLINGS commit that differs from make's pinned commit for %s passed", p.path)
			}
		}
	})
}

// TestSiblingsPresentMakeDepsFetchesNothing holds `make deps` to using the
// siblings SIBLINGS names when they are already present beside the worktree,
// with no fetch. It copies the make inputs into a temporary root, lays each
// sibling out beside it as a git checkout at its tag, and runs make deps with a
// git on PATH that refuses clone, fetch, pull and ls-remote. The subtest is the
// negative control: with one sibling removed, the refusal fires.
func TestSiblingsPresentMakeDepsFetchesNothing(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "make and git (make deps over present siblings)")

	if _, err := exec.LookPath("make"); err != nil {
		t.Skipf("this gate shells out to make: %v", err)
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("this gate shells out to git: %v", err)
	}

	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "SIBLINGS"))
	if err != nil {
		t.Fatal(err)
	}
	lines, problems := parseSiblings(string(data))
	if len(problems) != 0 {
		t.Fatalf("SIBLINGS does not parse: %v", problems)
	}
	goPin := ""
	for _, p := range siblingsFromMake(t, root) {
		if strings.HasSuffix(p.path, "serialize.go") {
			goPin = p.tag
		}
	}

	run := func(t *testing.T, missing string) (string, string, error) {
		t.Helper()
		tmp := t.TempDir()
		makeRoot := filepath.Join(tmp, "repo")
		copyMakeWorkspace(t, root, makeRoot)
		for _, l := range lines {
			if l.path == missing {
				continue
			}
			siblingsCheckout(t, filepath.Join(makeRoot, l.path), l.tag, strings.HasSuffix(l.path, "serialize.go"))
		}
		bin := filepath.Join(tmp, "bin")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		netLog := filepath.Join(tmp, "network.log")
		wrapper := "#!/bin/sh\n" +
			"for a in \"$@\"; do\n" +
			"  case \"$a\" in clone|fetch|pull|ls-remote) printf '%s\\n' \"$*\" >> \"$SIBLINGS_NET_LOG\"; exit 97;; esac\n" +
			"done\n" +
			"exec \"$SIBLINGS_REAL_GIT\" \"$@\"\n"
		if err := os.WriteFile(filepath.Join(bin, "git"), []byte(wrapper), 0o755); err != nil {
			t.Fatal(err)
		}
		out, err := runMakeFetch(t, makeRoot, []string{
			"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
			"SIBLINGS_REAL_GIT=" + realGit,
			"SIBLINGS_NET_LOG=" + netLog,
			"GIT_ALLOW_PROTOCOL=file",
			"SERIALIZE_REPO=/nonexistent/serialize.git",
			"SERIALIZE_GO_REPO=/nonexistent/serialize.go.git",
		}, "deps")
		logged, _ := os.ReadFile(netLog)
		return out, string(logged), err
	}

	out, logged, err := run(t, "")
	if err != nil {
		t.Fatalf("make deps with every sibling present failed: %v\n%s", err, out)
	}
	if logged != "" {
		t.Fatalf("make deps with every sibling present reached for the network:\n%s\n%s", logged, out)
	}
	if strings.Contains(out, "fetch") || strings.Contains(out, "WARNING") {
		t.Errorf("make deps with every sibling present fetched or warned:\n%s", out)
	}
	trimmed := strings.Split(strings.TrimSpace(out), "\n")
	wantLast := "make deps: serialize.go " + goPin + " at ../serialize.go"
	if got := trimmed[len(trimmed)-1]; got != wantLast {
		t.Errorf("last make deps line = %q, want %q", got, wantLast)
	}

	t.Run("a missing sibling is fetched, and the refusal sees it", func(t *testing.T) {
		t.Parallel()
		missing := ""
		for _, l := range lines {
			if !strings.HasSuffix(l.path, "serialize.go") {
				missing = l.path
				break
			}
		}
		if missing == "" {
			t.Fatal("SIBLINGS names no sibling besides serialize.go")
		}
		out, logged, err := run(t, missing)
		if err == nil && logged == "" {
			t.Fatalf("make deps with %s absent fetched nothing, so the network refusal has no blade:\n%s", missing, out)
		}
		if !strings.Contains(logged, "clone") {
			t.Errorf("make deps with %s absent did not try to clone it:\n%s\n%s", missing, logged, out)
		}
	})
}

// parseSiblings reads SIBLINGS text. A problem is a line that is not four
// fields, a commit that is not 40 lowercase hex digits, or a repeated path.
func parseSiblings(text string) ([]siblingLine, []string) {
	var lines []siblingLine
	var problems []string
	seen := map[string]int{}
	for i, raw := range strings.Split(text, "\n") {
		n := i + 1
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 4 {
			problems = append(problems, fmt.Sprintf("SIBLINGS:%d has %d fields, want <path> <clone-url> <tag> <commit>: %q", n, len(f), line))
			continue
		}
		if !siblingsSHARE.MatchString(f[3]) {
			problems = append(problems, fmt.Sprintf("SIBLINGS:%d commit %q is not 40 lowercase hex digits", n, f[3]))
		}
		if prev, ok := seen[f[0]]; ok {
			problems = append(problems, fmt.Sprintf("SIBLINGS:%d repeats %s from line %d", n, f[0], prev))
			continue
		}
		seen[f[0]] = n
		lines = append(lines, siblingLine{n: n, path: f[0], url: f[1], tag: f[2], sha: f[3]})
	}
	return lines, problems
}

// siblingsMismatches compares the parsed file to make's siblings.
func siblingsMismatches(got []siblingLine, want []siblingPin) []string {
	var out []string
	byPath := map[string]siblingLine{}
	for _, l := range got {
		byPath[l.path] = l
	}
	staged := map[string]bool{}
	for _, p := range want {
		staged[p.path] = true
		l, ok := byPath[p.path]
		if !ok {
			out = append(out, fmt.Sprintf("SIBLINGS has no line for %s, which make stages from %s at %s", p.path, p.url, p.tag))
			continue
		}
		if l.url != p.url {
			out = append(out, fmt.Sprintf("SIBLINGS:%d %s clone URL %s != make's %s", l.n, p.path, l.url, p.url))
		}
		if l.tag != p.tag {
			out = append(out, fmt.Sprintf("SIBLINGS:%d %s tag %s != make's pin %s", l.n, p.path, l.tag, p.tag))
		}
		if p.commit != "" && l.sha != p.commit {
			out = append(out, fmt.Sprintf("SIBLINGS:%d %s commit %s != make's pinned commit %s", l.n, p.path, l.sha, p.commit))
		}
	}
	for _, l := range got {
		if !staged[l.path] {
			out = append(out, fmt.Sprintf("SIBLINGS:%d lists %s, which make does not stage", l.n, l.path))
		}
	}
	return out
}

// siblingsFromMake returns every sibling make stages: each stage-sibling call
// in the make files, and serialize.go through deps-go. Each value is what make
// expands it to, read by running make.
func siblingsFromMake(t *testing.T, root string) []siblingPin {
	t.Helper()
	makeText := mainRedsMakeText(t, root)
	if !slices.Contains(mainRedsPrerequisites(makeText, "serialize-siblings"), "deps-go") {
		t.Fatalf("serialize-siblings no longer depends on deps-go, so this test's reading of the Go sibling is stale")
	}
	calls := siblingsStageRE.FindAllStringSubmatch(makeText, -1)
	if len(calls) == 0 {
		t.Fatal("the make files call stage-sibling nowhere; this test's reading of the staged siblings is stale")
	}
	var exprs []string
	for _, c := range calls {
		exprs = append(exprs, c[3], c[2], "$("+c[1]+")", "")
	}
	exprs = append(exprs, "$("+siblingsGo[0]+")", "$("+siblingsGo[1]+")", "$("+siblingsGo[2]+")", "$("+siblingsGo[3]+")")
	values := siblingsMakeExpand(t, root, exprs)
	var out []siblingPin
	for i := 0; i+3 < len(values); i += 4 {
		p := siblingPin{path: values[i], url: values[i+1], tag: values[i+2], commit: values[i+3]}
		if p.path == "" || p.url == "" || p.tag == "" {
			t.Fatalf("make expands a staged sibling to an empty field: %+v (from %v)", p, exprs[i:i+4])
		}
		out = append(out, p)
	}
	return out
}

// siblingsMakeExpand asks make for each expression's value after every make
// file is read, in order, with the sibling variables dropped from the
// environment so a developer's shell cannot change what is compared.
func siblingsMakeExpand(t *testing.T, root string, exprs []string) []string {
	t.Helper()
	makeBin, err := exec.LookPath("make")
	if err != nil {
		t.Fatalf("this gate resolves the pins by running make: %v", err)
	}
	probe := filepath.Join(t.TempDir(), "siblings.mk")
	var text strings.Builder
	text.WriteString(".PHONY: __siblings_print\n__siblings_print:\n\t@printf '%s\\n'")
	for i, e := range exprs {
		fmt.Fprintf(&text, " '%d=%s'", i, e)
	}
	text.WriteString("\n")
	if err := os.WriteFile(probe, []byte(text.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(makeBin, "-s", "--no-print-directory", "-f", "Makefile", "-f", probe, "__siblings_print")
	cmd.Dir = root
	cmd.Env = stripEnv(mainRedsCleanEnv(),
		"SERIALIZE", "SERIALIZE_GO", "SERIALIZE_REPO", "SERIALIZE_GO_REPO",
		"SERIALIZE_GO_COMMIT")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make: %v\n%s", err, output)
	}
	got := strings.Split(strings.TrimRight(string(output), "\n"), "\n")
	if len(got) != len(exprs) {
		t.Fatalf("make printed %d lines for %d expressions:\n%s", len(got), len(exprs), output)
	}
	values := make([]string, len(exprs))
	for i, line := range got {
		prefix := fmt.Sprintf("%d=", i)
		if !strings.HasPrefix(line, prefix) {
			t.Fatalf("make printed %q at line %d, want prefix %q", line, i, prefix)
		}
		values[i] = strings.TrimPrefix(line, prefix)
	}
	return values
}

// siblingsCheckout lays out a git checkout at dir with one commit tagged tag;
// withGoMod adds the go.mod deps-go requires of serialize.go.
func siblingsCheckout(t *testing.T, dir, tag string, withGoMod bool) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("a present sibling\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if withGoMod {
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/mas-bandwidth/serialize.go/v2\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=siblings-gate",
			"GIT_AUTHOR_EMAIL=siblings-gate@example.invalid",
			"GIT_COMMITTER_NAME=siblings-gate",
			"GIT_COMMITTER_EMAIL=siblings-gate@example.invalid",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("add", "-A")
	git("commit", "-qm", "a present sibling")
	git("tag", tag)
}
