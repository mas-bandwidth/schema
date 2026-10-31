package gotable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestGoMessageFormFixedOnly asserts, over one generated unit, that the four
// message verbs — MeasureMessages, SaveMessages, LoadMessages and
// LoadRetainMessages — exist for a `fixed table` root and are absent for a
// plain `table` root. docs/SPEC-TABLES.md §3.3: "THE FORM IS A FIXED TABLE'S,
// and that is a declaration, not a derivation ... `fixed table T` may ride
// here, a plain `table T` may not.".
func TestGoMessageFormFixedOnly(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	schema := `package probe

table Loose { n int32 }
fixed table Tight { n int32 }
`
	files := generate(t, schema)
	// the verbs are the MESSAGE form's, in <Base>TableMessage.go, and a verb
	// refused by name is a constant of the core <Base>Table.go
	// (docs/SPEC-TABLES.md, "GO's optional forms"), so the unit's table output is read whole
	var table string
	for name, data := range files {
		if strings.HasSuffix(name, "Table.go") || strings.HasSuffix(name, "TableMessage.go") {
			table += string(data)
		}
	}
	if table == "" {
		t.Fatal("the unit emitted no table module")
	}
	for _, violation := range fixedOnlyViolations(table) {
		t.Error(violation)
	}
	compileTightCaller(t, files)
}

// fixedOnlyViolations names every way the unit's table source breaks §3.3's
// fixed-only rule over the Loose (plain) and Tight (fixed) roots: a message
// verb on the plain root, or one missing from the fixed root.
func fixedOnlyViolations(table string) []string {
	var violations []string
	for _, verb := range []string{"MeasureMessages", "SaveMessages", "LoadMessages", "LoadRetainMessages"} {
		// "a plain `table T` may not" (docs/SPEC-TABLES.md §3.3).
		if strings.Contains(table, "Loose"+verb) {
			violations = append(violations, fmt.Sprintf("a plain table root carries Loose%s (docs/SPEC-TABLES.md §3.3: \"a plain `table T` may not\")", verb))
		}
	}
	for _, verb := range []string{"MeasureMessages", "SaveMessages", "LoadMessages", "LoadRetainMessages"} {
		// "`fixed table T` may ride here" (docs/SPEC-TABLES.md §3.3).
		if !strings.Contains(table, "Tight"+verb) {
			violations = append(violations, fmt.Sprintf("a fixed table root lacks Tight%s (docs/SPEC-TABLES.md §3.3: \"`fixed table T` may ride here\")", verb))
		}
	}
	return violations
}

// compileTightCaller compiles a caller that names the fixed root's four message
// verbs, proving they are real symbols and not mere source text.
func compileTightCaller(t *testing.T, files map[string][]byte) {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module probe\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixedonly_test.go"), []byte(tightCaller), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("a caller naming the fixed root's message verbs does not compile: %v\n%s", err, out)
	}
}

// tightCaller references all four verbs on the fixed root. LoadRetainMessages
// is a named refusal for a fixed root (§6.6), so it is a constant and exists as
// a symbol the caller may name.
const tightCaller = `package probe

import "testing"

func TestTightMessageVerbs(t *testing.T) {
	var v Tight
	v.N = 7
	values := []*Tight{&v}
	n := TightMeasureMessages(values, nil)
	if n < 2 {
		t.Fatalf("measure %d", n)
	}
	buf := make([]byte, n)
	var wr TableReport
	if TightSaveMessages(values, buf, &wr) != n || wr != (TableReport{}) {
		t.Fatalf("save %+v", wr)
	}
	vocab := new(TableVocabulary)
	vocab.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	ann := make([]byte, AnnounceMeasure())
	if Announce(ann) != int64(len(ann)) {
		t.Fatal("announce")
	}
	var ar TableReport
	if !AnnounceRead(vocab, ann, &ar) {
		t.Fatalf("announce read %+v", ar)
	}
	var got [1]Tight
	count, ok := TightLoadMessages(got[:], vocab, buf, &ar)
	if !ok || count != 1 || got[0].N != 7 {
		t.Fatalf("load %d %v %+v", count, ok, ar)
	}
	_ = TightLoadRetainMessages
}
`
