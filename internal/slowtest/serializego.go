// THE GO VERSIONING PROBES FIND THEIR OWN RUNTIME. fixedversioning_test.go:1126
// wrote the probe's go.mod with a replace of github.com/mas-bandwidth/serialize.go
// to a directory named serialize.go BESIDE the repository checkout — four levels
// up from the gotable package — and nothing staged that sibling (schema#1881
// fetched only the C++ runtime), so a cold read on a bench with no sibling died
// with "replacement directory ... does not exist" for exactly the rows whose
// generated unit imports serialize.go (the union arms). SerializeGo is where
// that probe asks for the runtime: the sibling when it is a checkout, else the
// fetched build/serialize.go the Makefile's Go fetch leaves behind.
package slowtest

import (
	"os"
	"path/filepath"
	"testing"
)

// SerializeGo returns the absolute path of the serialize.go runtime the Go
// probes compile against. `root` is the repository root relative to the calling
// package's directory (gotable passes `../../..`). It looks first for the
// sibling checkout `../serialize.go` beside the root — the default the bench
// tooling at bench/paired assumes — and then for `build/serialize.go` under the
// root, the copy `make tables-fixedform-corpus` fetches when there is no
// sibling. A directory only counts as a runtime when it holds a go.mod.
//
// When neither exists SerializeGo refuses by name under SCHEMA_REQUIRE_CORPUS
// (the gate promised a corpus and must not go green having run nothing) and
// skips by name otherwise. It never fetches and never touches the network; the
// Makefile fetches, this only looks.
func SerializeGo(t testing.TB, root string) string {
	t.Helper()
	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("slowtest: resolving the repository root %q: %v", root, err)
	}
	sibling := filepath.Join(absRoot, "..", "serialize.go")
	fetched := filepath.Join(absRoot, "build", "serialize.go")
	if isSerializeGoDir(sibling) {
		return sibling
	}
	if isSerializeGoDir(fetched) {
		return fetched
	}
	msg := "slowtest: the Go runtime serialize.go is absent: looked for a sibling at " + sibling +
		" and a fetched copy at " + fetched + "; `make tables-fixedform-corpus` fetches the runtime"
	if Enabled() {
		t.Fatalf("%s", msg)
	}
	t.Skipf("%s", msg)
	return ""
}

// isSerializeGoDir reports whether dir is a serialize.go checkout: a directory
// holding a go.mod. A bare directory of the right name is not a runtime.
func isSerializeGoDir(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "go.mod"))
	return err == nil && !info.IsDir()
}
