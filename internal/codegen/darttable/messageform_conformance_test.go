package darttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// THE MESSAGE FORM ON THE DART LEG (docs/SPEC-TABLES.md §3.3), pinned through
// the conformance driver (test/conformance/README.md). The driver answers the
// `message` surface by naming it in `list` and running, per the README's
// surface table: "AnnounceRead the connection's announcement, LoadMessages the
// message-form batch against it, SaveMessages, the bytes". A message row the
// driver leaves ABSENT is the port's missing form, and this test pins that the
// driver answers every manifest message row rather than printing ABSENT.
//
// The two clauses:
//   - the SOURCE PIN: the driver's `list` names `message`. §3.3's verbs are
//     plural because the primitive is a batch — "SaveMessages takes an array of
//     bodies of ONE root and writes ONE buffer. LoadMessages reads one buffer
//     into the caller's storage and allocates nothing." — so a `message` case
//     whose batch wire holds one body still drives LoadMessages and
//     SaveMessages with a count of one;
//   - the HARNESS ROWS: the harness's `message` surface passes over the Dart
//     leg, skipped by name when the leg's toolchain is not present.
//
// The source pin is red today because the Dart leg carries no message-form
// codec — the id-table wire (schema#514) and the form-2 batch are unlanded —
// so the driver cannot answer `message` and its `list` omits it.
func TestMessageFormConformanceDart(t *testing.T) {
	t.Parallel()

	// The source pin: `list` names the surfaces the driver answers, and a
	// message row is ABSENT exactly when `list` omits `message`.
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "conformance", "dart", "main.dart"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "'message\n'") {
		t.Error("the Dart driver's `list` does not name `message`: every manifest message row is ABSENT (docs/SPEC-TABLES.md §3.3)")
	}

	// The harness's message rows, skipped by name when the leg's toolchain is
	// not present.
	dartBin := dartBinary(t)
	if err := dartMessageRows(t, dartBin); err != nil {
		t.Error(err)
	}
}

// dartMessageRows runs the harness's `message` surface over the Dart leg and
// requires every message row to pass, not print ABSENT (test/conformance/README.md).
func dartMessageRows(t *testing.T, dartBin string) error {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		return err
	}
	work := filepath.Join(t.TempDir(), "conformance")
	cmd := exec.Command("go", "run", "./test/conformance/harness", "run", "--only", "dart", "--work", work)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "DART="+dartBin, "NOVA_TEST_NO_HOST=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("harness run failed: %v\n%s", err, out)
	}
	cell, ok := messageCell(string(out))
	if !ok {
		return fmt.Errorf("the harness printed no `message` row for the dart leg:\n%s", out)
	}
	if cell == "absent" || strings.HasPrefix(cell, "FAIL") {
		return fmt.Errorf("the dart leg's `message` surface is %q, not a pass (docs/SPEC-TABLES.md §3.3)", cell)
	}
	if !strings.HasPrefix(cell, "pass ") {
		return fmt.Errorf("the dart leg's `message` surface reads %q, not a pass (docs/SPEC-TABLES.md §3.3)", cell)
	}
	return nil
}

// messageCell returns the dart column's cell on the harness matrix's `message`
// row. The matrix prints surfaces one per line and one column per language, so
// the cell is the first non-empty field after the surface name.
func messageCell(matrix string) (string, bool) {
	for _, line := range strings.Split(matrix, "\n") {
		if !strings.HasPrefix(line, "message ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return "", false
		}
		return fields[1], true
	}
	return "", false
}
