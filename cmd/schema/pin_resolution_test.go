package main

import (
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/modulemove/pincorpus"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// TestPinDocSeparatesOfflineChecksFromGoResolution is F-183's fast half: the
// schema pin paragraph of docs/CLI.md must not present pin's offline checks as
// proof that Go resolves the version. pin checks a pseudo-version's spelling
// offline; Go verifies the commit and its timestamp when it resolves it.
func TestPinDocSeparatesOfflineChecksFromGoResolution(t *testing.T) {
	text := mustRead(t, filepath.Join("..", "..", "docs", "CLI.md"))
	start := strings.Index(text, "`schema pin <version>")
	if start < 0 {
		t.Fatalf("docs/CLI.md has no schema pin paragraph")
	}
	para, _, _ := strings.Cut(text[start:], "\n\n")
	para = strings.Join(strings.Fields(para), " ")
	if !strings.Contains(para, "offline") || !strings.Contains(para, "Go verifies") {
		t.Errorf("the schema pin paragraph must explain that pin checks offline and Go verifies the revision on resolution:\n%s", para)
	}
}

// TestPinCorpusGoResolution is F-183's resolver half. It compares a successful
// offline pin with cmd/go resolving that same version against a real local
// repository and an empty module cache: the corpus rows with git_time hold one
// version Go resolves and one it refuses, and pin accepts both.
func TestPinCorpusGoResolution(t *testing.T) {
	slowtest.Gate(t, "Go and Git resolve a pseudo-version against a local repository")
	bin := buildCLI(t)
	cases, err := pincorpus.Spellings()
	if err != nil {
		t.Fatal(err)
	}
	var accepted, refused int
	for _, c := range cases {
		if c.GitTime == "" {
			continue
		}
		if c.ResolveError == "" {
			accepted++
		} else {
			refused++
		}
		t.Run(c.ID, func(t *testing.T) { checkPinResolution(t, bin, c) })
	}
	if accepted == 0 || refused == 0 {
		t.Fatalf("pin resolution corpus has %d accepted and %d refused Go resolutions; want both", accepted, refused)
	}
}

func checkPinResolution(t *testing.T, bin string, c pincorpus.Spelling) {
	t.Helper()
	repo := t.TempDir()
	consumer := writePinPkg(t, t.TempDir(), "consumer", pinPkg1)
	module := pinModuleBase + c.Path
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module "+module+"\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commitTime, err := time.Parse(time.RFC3339, c.GitTime)
	if err != nil {
		t.Fatal(err)
	}
	// GitHub's well-known import path needs no discovery request. Git resolves
	// its remote to this directory, and file is the only permitted transport.
	origin := (&url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(repo), "/")}).String()
	env := append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_ALLOW_PROTOCOL=file",
		"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=url."+origin+".insteadOf",
		"GIT_CONFIG_VALUE_0=https://"+pinModuleBase, "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Schema Test", "GIT_COMMITTER_NAME=Schema Test",
		"GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_EMAIL=test@example.invalid",
		"GOPATH="+t.TempDir(), "GOMODCACHE="+t.TempDir(), "GOPROXY=direct",
		"GOVCS=*:git", "GOPRIVATE="+pinModuleBase, "GOSUMDB=off",
		"GOWORK=off", "GOENV=off", "GOTOOLCHAIN=local", "GO111MODULE=on", "GOFLAGS=")
	git := func(ts time.Time, args ...string) string {
		t.Helper()
		cmd := testproc.Command(t, "git", args...)
		cmd.Dir = repo
		stamp := ts.UTC().Format(time.RFC3339)
		cmd.Env = slices.Concat(env, []string{"GIT_AUTHOR_DATE=" + stamp, "GIT_COMMITTER_DATE=" + stamp})
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	before := commitTime.Add(-time.Minute)
	git(before, "init", "--quiet", "--template=")
	git(before, "add", "go.mod")
	git(before, "commit", "--quiet", "-m", "base")
	git(before, "tag", "v2.5.0")
	git(commitTime, "commit", "--quiet", "--allow-empty", "-m", "next")
	rev := git(commitTime, "rev-parse", "--short=12", "HEAD")
	pos := strings.LastIndexByte(c.Version, '-')
	if pos < 0 {
		t.Fatalf("fixture %s has no pseudo-version revision", c.Version)
	}
	version := c.Version[:pos+1] + rev
	run(t, bin, "pin", version, consumer)
	if got := mustRead(t, filepath.Join(consumer, "generate.go")); !strings.Contains(got, module+"/cmd/schema@"+version+" ") {
		t.Fatalf("pin did not write %s to the adopter's generate directive:\n%s", version, got)
	}
	cmd := testproc.Command(t, "go", "list", "-m", module+"@"+version)
	cmd.Dir = consumer
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if c.ResolveError == "" {
		if err != nil || strings.TrimSpace(string(out)) != module+" "+version {
			t.Fatalf("go list -m: %v\n%s", err, out)
		}
	} else if err == nil || !strings.Contains(string(out), c.ResolveError) {
		t.Fatalf("go list -m: %v\n%s\nwant rejection containing %q", err, out, c.ResolveError)
	}
}
