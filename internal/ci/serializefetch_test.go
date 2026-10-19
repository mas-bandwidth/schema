package ci

import (
	"bytes"
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
// runs make's runtime-header target in the repository root. The Go-runtime
// subtests use copied Makefile inputs in temporary roots so a real sibling
// cannot bypass the dependency-fetch path.
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
		pin := makePin(t, makefile, "SERIALIZE_GO_PIN")
		tmp := t.TempDir()
		makeRoot := filepath.Join(tmp, "repo")
		copyMakeWorkspace(t, root, makeRoot)
		mirrors := filepath.Join(tmp, "mirrors")
		makeBareRuntimeWithBranchCollision(t, filepath.Join(mirrors, "mas-bandwidth", "serialize.go.git"), pin,
			map[string]string{"go.mod": "module github.com/mas-bandwidth/serialize.go\n"})
		siblingModule := filepath.Join(tmp, "serialize.go", "go.mod")
		tagCommit := gitOutput(t, "--git-dir", filepath.Join(mirrors, "mas-bandwidth", "serialize.go.git"), "rev-parse", "refs/tags/"+pin+"^{commit}")
		branchCommit := gitOutput(t, "--git-dir", filepath.Join(mirrors, "mas-bandwidth", "serialize.go.git"), "rev-parse", "refs/heads/"+pin)
		if tagCommit == branchCommit {
			t.Fatalf("the local mirror fixture did not create conflicting branch and tag commits")
		}

		out, err := runMakeFetch(t, makeRoot, []string{
			"NOVA_MIRRORS=" + mirrors,
			"SERIALIZE_GO_REPO=/nonexistent/serialize.go.git",
			"SERIALIZE_GO_COMMIT=" + tagCommit,
		}, "deps")
		if err != nil {
			t.Fatalf("make deps: %v\n%s", err, out)
		}
		if _, err := os.Stat(siblingModule); err != nil {
			t.Fatalf("make deps exited 0 but produced no sibling %s:\n%s", siblingModule, out)
		}
		checkedOut := gitOutput(t, "-C", filepath.Join(tmp, "serialize.go"), "rev-parse", "HEAD")
		if checkedOut != tagCommit {
			t.Fatalf("make deps checked out %s, want pinned tag commit %s (same-named branch is %s)", checkedOut, tagCommit, branchCommit)
		}
		if !strings.Contains(out, "mirror") {
			t.Errorf("the Go runtime fetch did not use the local mirror:\n%s", out)
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		wantLast := "make deps: serialize.go " + pin + " at ../serialize.go"
		if len(lines) == 0 || lines[len(lines)-1] != wantLast {
			t.Errorf("last make deps line = %q, want %q", lines[len(lines)-1], wantLast)
		}
	})

	t.Run("deps preserves invalid existing destinations", func(t *testing.T) {
		t.Parallel()
		for _, explicit := range []bool{false, true} {
			explicit := explicit
			name := "default sibling"
			if explicit {
				name = "explicit override"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				tmp := t.TempDir()
				makeRoot := filepath.Join(tmp, "repo")
				copyMakeWorkspace(t, root, makeRoot)
				destination := filepath.Join(tmp, "serialize.go")
				vars := []string{"SERIALIZE_GO_REPO=/nonexistent/serialize.go.git"}
				if explicit {
					destination = filepath.Join(tmp, "chosen-runtime")
					vars = append(vars, "SERIALIZE_GO="+destination)
				}
				if err := os.MkdirAll(destination, 0o755); err != nil {
					t.Fatal(err)
				}
				sentinel := filepath.Join(destination, "keep-me.txt")
				want := []byte("this pre-existing directory belongs to the caller\n")
				if err := os.WriteFile(sentinel, want, 0o600); err != nil {
					t.Fatal(err)
				}
				out, err := runMakeFetch(t, makeRoot, vars, "deps")
				if err == nil {
					t.Fatalf("make deps unexpectedly accepted invalid destination %s:\n%s", destination, out)
				}
				got, readErr := os.ReadFile(sentinel)
				if readErr != nil {
					t.Fatalf("make deps removed the sentinel in %s: %v\n%s", destination, readErr, out)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("make deps changed sentinel in %s: got %q, want %q", destination, got, want)
				}
				if !strings.Contains(out, "exists without go.mod") || strings.Contains(out, "fetched") {
					t.Fatalf("make deps did not refuse the existing path before fetching:\n%s", out)
				}
			})
		}
	})

	t.Run("deps refuses a destination that appears during placement", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		makeRoot := filepath.Join(tmp, "repo")
		copyMakeWorkspace(t, root, makeRoot)
		pin := makePin(t, makefile, "SERIALIZE_GO_PIN")
		mirrors := filepath.Join(tmp, "mirrors")
		makeBareRuntime(t, filepath.Join(mirrors, "mas-bandwidth", "serialize.go.git"), pin,
			map[string]string{"go.mod": "module github.com/mas-bandwidth/serialize.go\n"})
		destination := filepath.Join(tmp, "serialize.go")
		bin := filepath.Join(tmp, "bin")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		realMkdir, err := exec.LookPath("mkdir")
		if err != nil {
			t.Fatal(err)
		}
		wrapper := "#!/bin/sh\n" +
			"if [ \"$#\" -eq 1 ] && [ \"$1\" = \"$RACE_DEST\" ]; then\n" +
			"  \"$RACE_REAL_MKDIR\" -p \"$RACE_DEST\"\n" +
			"  printf 'concurrent destination\\n' > \"$RACE_DEST/keep-me.txt\"\n" +
			"fi\nexec \"$RACE_REAL_MKDIR\" \"$@\"\n"
		if err := os.WriteFile(filepath.Join(bin, "mkdir"), []byte(wrapper), 0o755); err != nil {
			t.Fatal(err)
		}
		raceDest, err := filepath.Rel(makeRoot, destination)
		if err != nil {
			t.Fatal(err)
		}
		vars := []string{
			"NOVA_MIRRORS=" + mirrors,
			"SERIALIZE_GO_COMMIT=" + gitOutput(t, "--git-dir", filepath.Join(mirrors, "mas-bandwidth", "serialize.go.git"), "rev-parse", "refs/tags/"+pin+"^{commit}"),
			"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
			"RACE_DEST=" + raceDest,
			"RACE_REAL_MKDIR=" + realMkdir,
		}
		out, err := runMakeFetch(t, makeRoot, vars, "deps")
		if err == nil || !strings.Contains(out, "appeared during placement") {
			t.Fatalf("make deps did not reject the deterministic placement race: err=%v\n%s", err, out)
		}
		sentinel, readErr := os.ReadFile(filepath.Join(destination, "keep-me.txt"))
		if readErr != nil || string(sentinel) != "concurrent destination\n" {
			t.Fatalf("concurrent destination was changed: bytes=%q err=%v\n%s", sentinel, readErr, out)
		}
		if _, statErr := os.Lstat(filepath.Join(destination, "serialize.go")); !os.IsNotExist(statErr) {
			t.Fatalf("staged repository was nested under concurrent destination: %v", statErr)
		}
	})

	t.Run("deps cleans failed staged clones", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		makeRoot := filepath.Join(tmp, "repo")
		copyMakeWorkspace(t, root, makeRoot)
		out, err := runMakeFetch(t, makeRoot, []string{
			"NOVA_MIRRORS=" + filepath.Join(tmp, "missing-mirrors"),
			"SERIALIZE_GO_REPO=" + filepath.Join(tmp, "missing-serialize.go.git"),
		}, "deps")
		if err == nil {
			t.Fatalf("make deps unexpectedly fetched from nonexistent sources:\n%s", out)
		}
		if _, statErr := os.Lstat(filepath.Join(tmp, "serialize.go")); !os.IsNotExist(statErr) {
			t.Fatalf("failed staged fetch left a sibling destination behind: %v", statErr)
		}
		entries, readErr := os.ReadDir(tmp)
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".serialize.go.deps.") {
				t.Fatalf("failed staged fetch left temporary directory %s\n%s", entry.Name(), out)
			}
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

func gitOutput(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// makeBareRuntime builds a local bare repository in the $NOVA_MIRRORS layout
// (<mirrors>/<owner>/<repo>.git) with the given files and a tag equal to pin.
// No network, no real remote, no shared state.
func makeBareRuntime(t *testing.T, bare, pin string, files map[string]string) {
	t.Helper()
	makeBareRuntimeFixture(t, bare, pin, files, false)
}

func makeBareRuntimeWithBranchCollision(t *testing.T, bare, pin string, files map[string]string) {
	t.Helper()
	makeBareRuntimeFixture(t, bare, pin, files, true)
}

func makeBareRuntimeFixture(t *testing.T, bare, pin string, files map[string]string, branchCollision bool) {
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
	if branchCollision {
		git("checkout", "-qb", pin)
		if err := os.WriteFile(filepath.Join(src, "branch-only.txt"), []byte("not the tag\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		git("add", "branch-only.txt")
		git("commit", "-qm", "same-named branch, different commit")
	}
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
	cmd.Env = append(stripEnv(os.Environ(),
		"SERIALIZE", "SERIALIZE_GO", "SERIALIZE_REPO", "SERIALIZE_GO_REPO",
		"SERIALIZE_GO_PIN", "SERIALIZE_GO_COMMIT",
		"SERIALIZE_FETCHED", "SERIALIZE_GO_FETCHED", "NOVA_MIRRORS"), vars...)
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

func copyMakeWorkspace(t *testing.T, source, destination string) {
	t.Helper()
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	copyFile := func(from, to string, mode os.FileMode) {
		t.Helper()
		data, err := os.ReadFile(from)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(to, data, mode); err != nil {
			t.Fatal(err)
		}
	}
	copyFile(filepath.Join(source, "Makefile"), filepath.Join(destination, "Makefile"), 0o644)
	makeSource := filepath.Join(source, "make")
	returnErr := filepath.WalkDir(makeSource, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(makeSource, path)
		if err != nil {
			return err
		}
		to := filepath.Join(destination, "make", rel)
		if entry.IsDir() {
			return os.MkdirAll(to, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(to, data, info.Mode().Perm())
	})
	if returnErr != nil {
		t.Fatalf("copying make inputs: %v", returnErr)
	}
}
