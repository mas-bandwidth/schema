package pythonwriterefusalpacket

import (
	_ "embed"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	wrpacket "github.com/mas-bandwidth/schema/v2/test/conformance/write-refusal-packet"
)

//go:embed writerefusalpacket.py
var probe []byte

// exempt names the rows the Python leg cannot build. None: text_illformed is
// built as a lone surrogate, the str that is not UTF-8 (the probe's text_bytes).
var exempt = map[string]string{}

// TestWriteRefusalPacketPython holds docs/SPEC.md section 5.1's packet writer on
// the Python leg: every caller-holdable value the declaration does not admit is
// refused with InvalidValue in every build, and the lawful value at the same
// boundary is written. The negative control empties the generated range check
// (the check the const and reserved rows rest on alone: the stream writes no
// bits for them) and requires it to go red at const_wrong.
func TestWriteRefusalPacketPython(t *testing.T) {
	runWriteRefusalPacketPython(t)

	out, failed := slowtest.RunSabotaged(t, ".", "./test/conformance/python",
		"^TestWriteRefusalPacketPythonSurface$",
		slowtest.Sabotage{
			File: "internal/codegen/python/codec.go",
			From: `c.line("    validate_int(%s, %s, %s, %s)", v, pathExpr, min, max)`,
			To:   `c.line("    pass  # %s %s %s %s", v, pathExpr, min, max)`,
		})
	if !failed {
		t.Fatalf("negative control: the generated range check was emptied and the packet surface stayed green\n%s", out)
	}
	if !strings.Contains(out, "const_wrong") {
		t.Fatalf("negative control: the packet surface went red, but not at const_wrong\n%s", out)
	}
}

// TestWriteRefusalPacketPythonSurface is the surface alone, so the negative
// control can run it under an overlay without re-entering the control.
func TestWriteRefusalPacketPythonSurface(t *testing.T) {
	runWriteRefusalPacketPython(t)
}

func runWriteRefusalPacketPython(t *testing.T) {
	t.Helper()
	slowtest.Gate(t, "the pinned Python interpreter (it runs the generated packet writer)")
	t.Chdir("../../..")
	python := pyrun.Interpreter(t)
	rows := wrpacket.Rows(t, ".")
	dir := wrpacket.Generate(t, ".", "python", compiler.Options{})
	if err := os.WriteFile(filepath.Join(dir, "probe.py"), append([]byte(pyrun.ImportPrelude), probe...), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := pyrun.TryStdin(t, python, dir, "probe.py", []string{wrpacket.RowsPath(".")}, "")
	if err != nil {
		t.Fatalf("the probe crashed: %v\nstderr:\n%s\nstdout:\n%s", err, stderr, stdout)
	}
	wrpacket.Check(t, stdout, rows, exempt)
}
