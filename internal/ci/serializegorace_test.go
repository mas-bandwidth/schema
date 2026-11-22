package ci

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestSerializeGoRuntimeWhenTheSiblingArrivesMidRun holds the Go runtime rule
// ($(SERIALIZE_GO_RUNTIME)/go.mod) to the case CI's verify-leg (cs|unity, bench)
// jobs hit. make/go.mk resolves SERIALIZE_GO_RUNTIME once, when the Makefile is
// read: with no ../serialize.go it is build/serialize.go. The same run's
// deps-go then fetches and publishes ../serialize.go, and the runtime rule,
// seeing the sibling, used to do nothing — so build/serialize.go/go.mod, the
// file it is the rule for, never existed and the rule refused ("the fetch
// produced no build/serialize.go/go.mod"). The rule must provide its own
// target from the sibling that arrived.
//
// The other half: a sibling present when the Makefile is read is the runtime,
// and nothing is fetched or copied into build/serialize.go.
//
// It shells out to make and git, so it is a functional test behind the slow
// gate. No network: the runtime comes from a local bare mirror fixture, and the
// network repository is named as a path that does not exist.
func TestSerializeGoRuntimeWhenTheSiblingArrivesMidRun(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "make and git (a local fetch of the Go runtime)")
	for _, tool := range []string{"make", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("the runtime rule gate shells out to %s: %v", tool, err)
		}
	}
	root := repoRoot(t)
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	pin := makePin(t, string(makefile), "SERIALIZE_GO_PIN")
	const module = "module github.com/mas-bandwidth/serialize.go/v2\n"

	// probe names the runtime the way every consumer does: through
	// $(SERIALIZE_GO_RUNTIME), resolved when the Makefile is read.
	const probe = "probe: $(SERIALIZE_GO_RUNTIME)/go.mod\n\t@echo runtime=$(SERIALIZE_GO_RUNTIME)\n"

	run := func(t *testing.T, makeRoot string, vars []string, goals ...string) (string, error) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(makeRoot, "probe.mk"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		args := append([]string{"--no-print-directory", "-j1", "-f", "Makefile", "-f", "probe.mk"}, vars...)
		args = append(args, goals...)
		cmd := exec.Command("make", args...)
		cmd.Dir = makeRoot
		cmd.Env = append(stripEnv(os.Environ(),
			"SERIALIZE", "SERIALIZE_GO", "SERIALIZE_REPO", "SERIALIZE_GO_REPO",
			"SERIALIZE_GO_PIN", "SERIALIZE_GO_COMMIT",
			"SERIALIZE_FETCHED", "SERIALIZE_GO_FETCHED", "NOVA_MIRRORS", "MAKEFLAGS"), vars...)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	t.Run("the sibling arrives during the run", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		makeRoot := filepath.Join(tmp, "repo")
		copyMakeWorkspace(t, root, makeRoot)
		mirrors := filepath.Join(tmp, "mirrors")
		bare := filepath.Join(mirrors, "mas-bandwidth", "serialize.go.git")
		makeBareRuntime(t, bare, pin, map[string]string{"go.mod": module})
		commit := gitOutput(t, "--git-dir", bare, "rev-parse", "refs/tags/"+pin+"^{commit}")

		// deps-go runs first (-j1, goals in order) and publishes ../serialize.go
		// after the Makefile resolved the runtime to build/serialize.go.
		out, err := run(t, makeRoot, []string{
			"NOVA_MIRRORS=" + mirrors,
			"SERIALIZE_GO_REPO=/nonexistent/serialize.go.git",
			"SERIALIZE_GO_COMMIT=" + commit,
		}, "deps-go", "probe")
		if err != nil {
			t.Fatalf("make deps-go probe: %v\n%s", err, out)
		}
		if !strings.Contains(out, "runtime=build/serialize.go\n") {
			t.Fatalf("the runtime did not resolve to build/serialize.go at parse time, so this is not the race:\n%s", out)
		}
		sibling, err := os.ReadFile(filepath.Join(tmp, "serialize.go", "go.mod"))
		if err != nil {
			t.Fatalf("deps-go published no sibling: %v\n%s", err, out)
		}
		got, err := os.ReadFile(filepath.Join(makeRoot, "build", "serialize.go", "go.mod"))
		if err != nil {
			t.Fatalf("make exited 0 but build/serialize.go/go.mod is absent: %v\n%s", err, out)
		}
		if !bytes.Equal(got, sibling) {
			t.Errorf("build/serialize.go/go.mod = %q, want the sibling's %q", got, sibling)
		}
	})

	t.Run("the sibling is present when the Makefile is read", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		makeRoot := filepath.Join(tmp, "repo")
		copyMakeWorkspace(t, root, makeRoot)
		if err := os.MkdirAll(filepath.Join(tmp, "serialize.go"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmp, "serialize.go", "go.mod"), []byte(module), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := run(t, makeRoot, []string{"SERIALIZE_GO_REPO=/nonexistent/serialize.go.git"}, "probe")
		if err != nil {
			t.Fatalf("make probe: %v\n%s", err, out)
		}
		if !strings.Contains(out, "runtime=../serialize.go\n") {
			t.Errorf("the runtime did not resolve to the sibling:\n%s", out)
		}
		if _, err := os.Stat(filepath.Join(makeRoot, "build", "serialize.go")); err == nil {
			t.Errorf("a present sibling still produced build/serialize.go:\n%s", out)
		}
	})
}
