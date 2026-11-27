package wirealone

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const root = "../.."

// TestEveryRowIsARefusedFramingRow: every row the file lists is a framing
// row of MANIFEST.txt whose verdict is refused, so a port's control derived
// from it can never ask a row to read that the corpus does not refuse.
func TestEveryRowIsARefusedFramingRow(t *testing.T) {
	t.Parallel()
	rows, err := Read(root)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	verdict := map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		w := strings.Fields(sc.Text())
		if len(w) == 7 && w[0] == "framing" {
			verdict[w[1]] = w[5]
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(verdict) == 0 {
		t.Fatal("MANIFEST.txt holds no framing row: the check below would hold nothing")
	}
	for name, class := range rows {
		switch v, ok := verdict[name]; {
		case !ok:
			t.Errorf("%s (%s) is no framing row of MANIFEST.txt", name, class)
		case v != "refused":
			t.Errorf("%s (%s): MANIFEST.txt pins %q, and a wire-alone row is refused", name, class, v)
		}
	}
	if !t.Failed() {
		t.Logf("%d wire-alone rows, each a refused framing row", len(rows))
	}
}

// TestOwnedRefusesABadException: an exception naming no row, or one with no
// reason, is refused; a good one leaves exactly that row out.
func TestOwnedRefusesABadException(t *testing.T) {
	t.Parallel()
	all, err := Owned(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Owned(root, map[string]string{"no_such_framing_row": "why"}); err == nil {
		t.Error("an exception naming no row was accepted")
	}
	if _, err := Owned(root, map[string]string{all[0]: " "}); err == nil {
		t.Error("an exception with no reason was accepted")
	}
	got, err := Owned(root, map[string]string{all[0]: "a reason"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(all)-1 || got[0] == all[0] {
		t.Errorf("one exception left %d of %d rows, first %s", len(got), len(all), got[0])
	}
}
