package javatable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestJavaDriverChecksEveryIntegerAssignment holds the Java conformance
// driver's write-refusal mutation to the declared kind on every path a row can
// take to an integer: a scalar field, an array element, a channel-array
// element, a map entry's key and value, and a nested record's field
// (testdata/conformance/tables/FORMAT.md, `write-refusal`). The rows and their
// answers are the committed corpus testdata/conformance/write-refusal-int
// (F-098, F-099).
//
// Java spells intN and uintN with one type, so the driver decides from the
// declaration what its storage holds. A value the declared kind cannot hold in
// that storage answers `unrepresentable`, as the Go reference driver answers
// the same row (test/conformance/go/writerefusal.go): one past a signed
// maximum, a negative in an unsigned field, one past an unsigned maximum. The
// value at each end is stored and written. A value the storage holds and the
// declaration does not (256 in a uint8, which Java stores in an int) reaches
// the writer, and the writer refuses it.
func TestJavaDriverChecksEveryIntegerAssignment(t *testing.T) {
	slowtest.Gate(t, "the JDK and the conformance corpus")
	root := javaConformanceRoot(t)
	corpus := filepath.Join(root, "testdata", "conformance", "write-refusal-int")
	manifest, err := os.ReadFile(filepath.Join(corpus, "manifest.txt"))
	if err != nil {
		t.Fatal(err)
	}
	answers, err := os.ReadFile(filepath.Join(corpus, "answers.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for line := range strings.SplitSeq(string(answers), "\n") {
		if f := strings.Fields(line); len(f) == 2 && !strings.HasPrefix(f[0], "#") {
			want[f[0]] = f[1] + "\n"
		}
	}
	var rows []string
	for line := range strings.SplitSeq(string(manifest), "\n") {
		if f := strings.Fields(line); len(f) == 8 && f[0] == "write-refusal" {
			rows = append(rows, f[1])
			if _, ok := want[f[1]]; !ok {
				t.Errorf("%s: answers.txt owes no answer", f[1])
			}
		}
	}
	if len(rows) != len(want) {
		t.Fatalf("manifest.txt has %d rows, answers.txt %d answers", len(rows), len(want))
	}

	javaConformanceBuild(t, root)
	outDir := javaDriverSurface(t, root, "write-refusal", string(manifest))
	for _, name := range rows {
		got, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			t.Errorf("%s: the driver wrote no answer: %v", name, err)
			continue
		}
		if string(got) != want[name] {
			t.Errorf("%s: the driver answered %q, want %q", name, got, want[name])
		}
	}
}
