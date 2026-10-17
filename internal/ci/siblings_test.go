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

// TestMakeSiblingsClonesEachAbsentSiblingAtItsCIPin holds `make siblings`: a
// fresh clone has none of the serialize runtimes the compiler tests resolve
// beside the repository (../../serialize.cs from compiler/, and the rest), and
// every slow compiler test that compiles generated code failed on the benches
// naming the missing sibling. The verb must clone EVERY absent sibling at the
// tag ci-full.yml pins for it (read from the workflow, never restated), take the
// $NOVA_MIRRORS mirror first, leave a present sibling untouched, and
// `make siblings-check` must fail by name while any sibling is absent.
//
// It shells out to make and git, so it sits behind the slow gate. No network
// and no real remote: each sibling is a LOCAL bare repository under
// <tmp>/mirrors in the mirror layout, SIBLINGS_BASE names a path that does not
// exist (so only the mirror can satisfy a clone), and SIBLINGS_PARENT points
// the clones into the test's own temporary directory.
func TestMakeSiblingsClonesEachAbsentSiblingAtItsCIPin(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "make and git (local clones of the sibling runtimes)")
	for _, tool := range []string{"make", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("the siblings gate shells out to %s: %v", tool, err)
		}
	}

	root := repoRoot(t)
	wf, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci-full.yml"))
	if err != nil {
		t.Fatal(err)
	}
	siblings := map[string]string{
		"serialize":    "SERIALIZE_TAG",
		"serialize.c":  "SERIALIZE_C_TAG",
		"serialize.cs": "SERIALIZE_CS_TAG",
		"serialize.go": "SERIALIZE_GO_TAG",
		"serialize.js": "SERIALIZE_JS_TAG",
		"serialize.rs": "SERIALIZE_RS_TAG",
	}
	pins := map[string]string{}
	for name, variable := range siblings {
		m := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(variable) + `:\s*(\S+)`).FindStringSubmatch(string(wf))
		if m == nil {
			t.Fatalf("ci-full.yml names no %s; the siblings gate cannot pin %s", variable, name)
		}
		pins[name] = m[1]
	}

	tmp := t.TempDir()
	mirrors := filepath.Join(tmp, "mirrors")
	parent := filepath.Join(tmp, "parent")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, pin := range pins {
		makeBareRuntime(t, filepath.Join(mirrors, "mas-bandwidth", name+".git"), pin,
			map[string]string{"PIN": name + " " + pin + "\n"})
	}
	// A sibling already present is kept exactly as it is, never replaced.
	kept := filepath.Join(parent, "serialize.go")
	if err := os.MkdirAll(kept, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kept, "KEPT"), []byte("hand-kept\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	vars := []string{
		"NOVA_MIRRORS=" + mirrors,
		"SIBLINGS_BASE=/nonexistent",
		"SIBLINGS_PARENT=" + parent,
	}
	if out, err := runMakeFetch(t, root, vars, "siblings"); err != nil {
		t.Fatalf("make siblings: %v\n%s", err, out)
	}
	for name, pin := range pins {
		if name == "serialize.go" {
			continue
		}
		got, err := os.ReadFile(filepath.Join(parent, name, "PIN"))
		if err != nil {
			t.Errorf("make siblings left no %s clone: %v", name, err)
			continue
		}
		if want := name + " " + pin + "\n"; string(got) != want {
			t.Errorf("%s cloned at the wrong ref: PIN holds %q, want %q", name, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(kept, "KEPT")); err != nil {
		t.Errorf("make siblings replaced the present serialize.go checkout: %v", err)
	}
	if _, err := os.Stat(filepath.Join(kept, "PIN")); err == nil {
		t.Errorf("make siblings cloned over the present serialize.go checkout")
	}
	if out, err := runMakeFetch(t, root, vars, "siblings-check"); err != nil {
		t.Fatalf("make siblings-check with every sibling present: %v\n%s", err, out)
	}

	if err := os.RemoveAll(filepath.Join(parent, "serialize.cs")); err != nil {
		t.Fatal(err)
	}
	out, err := runMakeFetch(t, root, vars, "siblings-check")
	if err == nil {
		t.Fatalf("make siblings-check passed with serialize.cs absent:\n%s", out)
	}
	if !strings.Contains(out, "serialize.cs ABSENT") {
		t.Errorf("make siblings-check did not name the absent serialize.cs:\n%s", out)
	}
}
