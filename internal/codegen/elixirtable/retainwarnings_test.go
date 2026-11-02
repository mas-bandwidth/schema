package elixirtable

import (
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The generic retaining payload clause cannot receive enum/union branches:
// earlier clauses own them. Compile an actual emitted module with the project's
// strict warning gate so an impossible fallback result cannot recur unnoticed.
func TestRetainScalarFallbackCompilesWithoutWarningsElixir(t *testing.T) {
	slowtest.Gate(t, "Elixir strict retained payload compilation")
	elixir := elixirBinary(t)
	compiler := filepath.Join(filepath.Dir(elixir), "elixirc")
	if _, err := os.Stat(compiler); err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, `package warningprobe
 table Root { value int32 }
 `)
	dir := t.TempDir()
	names := writeModuleDir(t, dir, generateElixir(t, u))
	gate, err := filepath.Abs("../../../make/elixir-strict.sh")
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{gate, compiler, "-o", filepath.Join(dir, "ebin")}, names...)
	cmd := exec.Command("sh", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+otpBin(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("strict native compiler: %v\n%s", err, out)
	}
}
