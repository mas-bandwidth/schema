package elixirwriterefusalpacket

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	_ "embed"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
	wrpacket "github.com/mas-bandwidth/schema/v2/test/conformance/write-refusal-packet"
)

//go:embed writerefusalpacket.exs
var probe []byte

// TestWriteRefusalPacketElixir holds docs/SPEC.md 5.1's packet writer on the
// Elixir leg: every caller-holdable value the declaration does not admit is
// refused with ArgumentError, and the lawful value at the same boundary is
// written. The negative control changes the emitter's upper-bound comparison
// and requires the boundary case to go red.
func TestWriteRefusalPacketElixir(t *testing.T) {
	runSurface(t)

	out, failed := slowtest.RunSabotaged(t, ".", "./test/conformance/elixir",
		"^TestWriteRefusalPacketElixirSurface$",
		slowtest.Sabotage{
			File: "internal/codegen/elixir/functions.go",
			From: `fmt.Sprintf("%s > %s", expr, intLit(max))`,
			To:   `fmt.Sprintf("%s >= %s", expr, intLit(max))`,
		})
	if !failed {
		t.Fatalf("negative control: the upper-bound comparison moved and the packet surface stayed green\n%s", out)
	}
	if !strings.Contains(out, "scalar_at_max") {
		t.Fatalf("negative control: the packet surface went red, but not at the boundary case\n%s", out)
	}
}

// TestWriteRefusalPacketElixirSurface is the surface alone, so the negative
// control can run it under an overlay without re-entering the control.
func TestWriteRefusalPacketElixirSurface(t *testing.T) {
	runSurface(t)
}

// exempt names rows the Elixir leg cannot build. None: every row is a value an
// Elixir caller can hold.
var exempt = map[string]string{}

func runSurface(t *testing.T) {
	t.Helper()
	slowtest.Gate(t, "the Elixir toolchain (it compiles and runs the generated packet writer)")
	elixirBin := slowtest.Tool(t, "elixir")
	elixircBin := slowtest.Tool(t, "elixirc")
	t.Chdir("../../..")

	root := "."
	rows := wrpacket.Rows(t, root)
	dir := wrpacket.Generate(t, root, "elixir", compiler.Options{})
	ebin := filepath.Join(dir, "ebin")
	if err := os.MkdirAll(ebin, 0o700); err != nil {
		t.Fatal(err)
	}
	var names []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".ex") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	env := append(os.Environ(), "PATH="+filepath.Dir(elixirBin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd := testproc.Command(t, elixircBin, append([]string{"-o", ebin}, names...)...)
	cmd.Dir = dir
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile the generated packet writer: %v\n%s", err, out)
	}

	probePath := filepath.Join(dir, "probe.exs")
	if err := os.WriteFile(probePath, probe, 0o600); err != nil {
		t.Fatal(err)
	}
	rowsPath, err := filepath.Abs(wrpacket.RowsPath(root))
	if err != nil {
		t.Fatal(err)
	}
	cmd = testproc.Command(t, elixirBin, "-pa", ebin, probePath, rowsPath)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the probe crashed (only ArgumentError counts as a refusal): %v\n%s", err, out)
	}
	wrpacket.Check(t, string(out), rows, exempt)
}
