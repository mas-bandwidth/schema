package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestSerializeFetchTriesTheMirrorFirst holds the fetch's SOURCE ORDER: since
// schema#1881 `make tables-fixedform-corpus` fetches the pinned serialize
// runtime when no sibling checkout is present, and the fleet's benches keep
// bare mirrors of the repositories they work on under $NOVA_MIRRORS, laid out
// as <owner>/<repo>.git (for example $NOVA_MIRRORS/mas-bandwidth/serialize.git).
// A bench with no network — or one the forge has rate-limited — could not build
// the corpus before this order existed, and every SCHEMA_REQUIRE_CORPUS gate
// reported the same absence. The fetch must try that mirror first and fall back
// to the network repository, without changing either pin.
//
// The test shells out to make and git, so it is a functional test behind the
// slow gate and a bare `go test ./...` skips it by name. It uses no network and
// no real remote: it builds a LOCAL bare repository under <tmp>/mirrors in the
// mirror layout, tags it with the pin it READS from the Makefile, names a
// nonexistent SERIALIZE_REPO (so only the mirror can satisfy the fetch), and
// runs make's runtime-header target in the repository root. A sibling checkout
// would be used before any fetch, so when one resolves SERIALIZE the test skips
// by name — the fetch is simply not reached.
func TestSerializeFetchTriesTheMirrorFirst(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "make and git (a local fetch of the runtime)")

	if _, err := exec.LookPath("make"); err != nil {
		t.Skipf("the fetch gate shells out to make: %v", err)
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("the fetch gate shells out to git: %v", err)
	}

	root := repoRoot(t)
	makefileBytes, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	makefile := string(makefileBytes)

	t.Run("the C++ runtime", func(t *testing.T) {
		t.Parallel()
		sibling := filepath.Join(root, "..", "serialize", "serialize.h")
		if _, err := os.Stat(sibling); err == nil {
			t.Skipf("a sibling C++ serialize checkout at %s resolves SERIALIZE, so the fetch is not reached", sibling)
		}
		pin := makePin(t, makefile, "SERIALIZE_PIN")
		tmp := t.TempDir()
		mirrors := filepath.Join(tmp, "mirrors")
		makeBareRuntime(t, filepath.Join(mirrors, "mas-bandwidth", "serialize.git"), pin,
			map[string]string{"serialize.h": "// the pinned serialize runtime\n"})
		fetched := filepath.Join(tmp, "out", "serialize")
		header := filepath.Join(fetched, "serialize.h")

		out, err := runMakeFetch(t, root, []string{
			"NOVA_MIRRORS=" + mirrors,
			"SERIALIZE_REPO=/nonexistent/serialize.git",
			"SERIALIZE_FETCHED=" + fetched,
		}, header)
		if err != nil {
			t.Fatalf("make %s: %v\n%s", header, err, out)
		}
		if _, err := os.Stat(header); err != nil {
			t.Fatalf("make exited 0 but the mirror fetch produced no %s:\n%s", header, out)
		}
		if !strings.Contains(out, "mirror") {
			t.Errorf("the fetch did not say it used the mirror:\n%s", out)
		}
	})

	t.Run("the Go runtime", func(t *testing.T) {
		t.Parallel()
		sibling := filepath.Join(root, "..", "serialize.go", "go.mod")
		if _, err := os.Stat(sibling); err == nil {
			t.Skipf("a sibling Go serialize.go checkout at %s resolves SERIALIZE_GO, so the fetch is not reached", sibling)
		}
		pin := makePin(t, makefile, "SERIALIZE_GO_PIN")
		tmp := t.TempDir()
		mirrors := filepath.Join(tmp, "mirrors")
		makeBareRuntime(t, filepath.Join(mirrors, "mas-bandwidth", "serialize.go.git"), pin,
			map[string]string{"go.mod": "module github.com/mas-bandwidth/serialize.go\n"})
		fetched := filepath.Join(tmp, "out", "serialize.go")
		gomod := filepath.Join(fetched, "go.mod")

		out, err := runMakeFetch(t, root, []string{
			"NOVA_MIRRORS=" + mirrors,
			"SERIALIZE_GO_REPO=/nonexistent/serialize.go.git",
			"SERIALIZE_GO_FETCHED=" + fetched,
		}, gomod)
		if err != nil {
			t.Fatalf("make %s: %v\n%s", gomod, err, out)
		}
		if _, err := os.Stat(gomod); err != nil {
			t.Fatalf("make exited 0 but the mirror fetch produced no %s:\n%s", gomod, out)
		}
		if !strings.Contains(out, "mirror") {
			t.Errorf("the fetch did not say it used the mirror:\n%s", out)
		}
	})
}

// makePin reads a `NAME ?= value` assignment out of the Makefile text so the
// local mirror carries the same tag the recipe will ask for. The pin is read,
// never restated: a card that bumps it keeps this gate honest for free.
func makePin(t *testing.T, makefile, name string) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `\s*\?=\s*(\S+)`)
	m := re.FindStringSubmatch(makefile)
	if m == nil {
		t.Fatalf("the Makefile names no %s; the mirror fetch gate cannot pin its tag", name)
	}
	return m[1]
}

// makeBareRuntime builds a local bare repository in the $NOVA_MIRRORS layout
// (<mirrors>/<owner>/<repo>.git) holding one commit with the given files and a
// tag equal to pin. No network, no real remote, no shared state.
func makeBareRuntime(t *testing.T, bare, pin string, files map[string]string) {
	t.Helper()
	src := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = src
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=serialize-fetch-gate",
			"GIT_AUTHOR_EMAIL=serialize-fetch-gate@example.invalid",
			"GIT_COMMITTER_NAME=serialize-fetch-gate",
			"GIT_COMMITTER_EMAIL=serialize-fetch-gate@example.invalid",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("add", "-A")
	git("commit", "-qm", "the pinned runtime")
	git("tag", pin)
	if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
		t.Fatal(err)
	}
	git("clone", "-q", "--bare", src, bare)
}

// runMakeFetch runs the runtime fetch target in the repository root with the
// given make VARIABLE=value assignments on the command line. SERIALIZE,
// SERIALIZE_GO and the fetch variables are stripped from the child environment:
// an inherited SERIALIZE would resolve as an environment origin and the recipe
// would refuse by name before reaching the fetch this gate means to exercise.
func runMakeFetch(t *testing.T, root string, vars []string, target string) (string, error) {
	t.Helper()
	args := append([]string{"--no-print-directory"}, vars...)
	args = append(args, target)
	cmd := exec.Command("make", args...)
	cmd.Dir = root
	cmd.Env = stripEnv(os.Environ(),
		"SERIALIZE", "SERIALIZE_GO", "SERIALIZE_REPO", "SERIALIZE_GO_REPO",
		"SERIALIZE_FETCHED", "SERIALIZE_GO_FETCHED", "NOVA_MIRRORS")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// stripEnv returns env without the named variables.
func stripEnv(env []string, names ...string) []string {
	drop := make(map[string]bool, len(names))
	for _, name := range names {
		drop[name] = true
	}
	out := env[:0:0]
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if !drop[name] {
			out = append(out, kv)
		}
	}
	return out
}
