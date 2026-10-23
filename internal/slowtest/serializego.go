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

// gateSkipMarker is shared in meaning with tools/slowgatescan's transcript
// parser. Keep the literal in sync with its scanner test.
const gateSkipMarker = "SCHEMA_SLOW: this test shells out"

// SerializeGo returns the absolute path of the serialize.go runtime used by Go
// probes. An explicit absolute SERIALIZE_GO wins, then the sibling checkout,
// then the copy provisioned by make deps under build/serialize.go.
func SerializeGo(t testing.TB, root string) string {
	t.Helper()
	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("slowtest: resolving the repository root %q: %v", root, err)
	}
	configured := os.Getenv("SERIALIZE_GO")
	sibling := filepath.Join(absRoot, "..", "serialize.go")
	fetched := filepath.Join(absRoot, "build", "serialize.go")
	if filepath.IsAbs(configured) && isSerializeGoDir(configured) {
		return configured
	}
	if isSerializeGoDir(sibling) {
		return sibling
	}
	if isSerializeGoDir(fetched) {
		return fetched
	}
	msg := "the Go runtime serialize.go is absent: looked for absolute SERIALIZE_GO " + configured +
		", sibling " + sibling + ", and fetched copy " + fetched + "; run make deps"
	if Enabled() {
		t.Fatalf("slowtest: %s", msg)
		return ""
	}
	t.Skipf("%s: %s", gateSkipMarker, msg)
	return ""
}

// isSerializeGoDir reports whether dir is a serialize.go checkout: a directory
// holding a go.mod. A bare directory of the right name is not a runtime.
func isSerializeGoDir(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "go.mod"))
	return err == nil && !info.IsDir()
}
