// Resource bounds for the GO GENERATED READERS (docs/SPEC-TABLES.md §21.7,
// the shared port). The engine's half lives in internal/ci; this file holds
// the emitted reader runtime to the same F and nesting cap. It compiles and
// runs a generated unit, so it is behind the slow gate and rides the nightly
// leg. The hostile shapes are the documented matrix: huge counts, deep
// nesting, recursion, giant strings, and many unknown fields.
package gotable

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const resourceBoundsGeneratedTest = `package probe

import "testing"

func TestGeneratedReaderCaps(t *testing.T) {
 t.Parallel()
 var rep TableReport
 // One body past the cap is refused by name, before a frame is entered.
 r := TableReader{Buffer: []byte{0}, Report: &rep, Depth: TableMaxNestingDepth}
 if _, ok := r.Body(); ok { t.Fatalf("input past the nesting cap was accepted") }
 if rep.Reason != "nesting_depth" || !rep.Malformed { t.Fatalf("refusal = %+v, want nesting_depth", rep) }
 // One under the cap still enters.
 var under TableReport
 r = TableReader{Buffer: []byte{0}, Report: &under, Depth: TableMaxNestingDepth - 1}
 if _, ok := r.Body(); !ok { t.Fatalf("input under the cap refused: %+v", under) }
}

func TestGeneratedReaderHugeLength(t *testing.T) {
 // A 10-byte LEB of max u64: a giant string, blob or count's length claim.
 data := []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01}
 var rep TableReport
 r := TableReader{Buffer: data, Report: &rep}
 if _, ok := r.Body(); ok { t.Fatalf("giant length was accepted") }
 n := testing.AllocsPerRun(20, func() {
  var q TableReport
  rr := TableReader{Buffer: data, Report: &q}
  rr.Body()
 })
 if n > 4*float64(len(data)) { t.Fatalf("giant-length read allocated %v objects over %d bytes, above F=4", n, len(data)) }
}

func TestGeneratedReaderManyUnknownFields(t *testing.T) {
 // 4096 unknown descriptors, each an id ref, kind u8 and one value byte.
 data := make([]byte, 0, 4096*3)
 for i := 0; i < 4096; i++ { data = append(data, 1, 6, byte(i)) }
 var rep TableReport
 r := TableReader{Buffer: data, Report: &rep}
 n := testing.AllocsPerRun(20, func() {
  q := r
  for q.Offset < int64(len(q.Buffer)) { if !q.Skip(6) { panic("skip") } }
 })
 if n > 4*float64(len(data)) { t.Fatalf("unknown-field walk allocated %v objects over %d bytes, above F=4", n, len(data)) }
}

func TestGeneratedReaderRecursion(t *testing.T) {
 t.Parallel()
 // A table body nested in itself is the recursion shape: the depth counter,
 // not a declared cycle, is what stops it.
 data := []byte{0}
 var rep TableReport
 r := TableReader{Buffer: data, Report: &rep, Depth: TableMaxNestingDepth}
 if _, ok := r.Body(); ok { t.Fatal("recursive body accepted") }
 if rep.Reason != "nesting_depth" { t.Fatalf("recursive refusal = %q", rep.Reason) }
}
`

// TestGeneratedReaderResourceBounds compiles the EMITTED reader runtime
// (tableRuntime + tableWireSource, the exact bytes a generated reader carries)
// with no engine or generated vocabulary, so the hostile-shape assertions run
// against the reader the generator ships, not a hand copy. It shells out to
// the Go toolchain, hence the slow gate and the nightly leg.
func TestGeneratedReaderResourceBounds(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles the emitted reader runtime)")
	dir := t.TempDir()
	runtime := "package probe\n\nimport (\n\t\"encoding/binary\"\n\t\"unicode/utf8\"\n\t\"unsafe\"\n)\n\ntype TableMessageReader struct{}\ntype TableMessageWriter struct{}\n\n" +
		tableRuntime() + "\nconst tableIdCapacity = 8\nconst tableIdBuckets = 16\n" + tableWireSource
	files := map[string]string{
		"go.mod":           "module probe\n\ngo 1.26\n",
		"runtime.go":       runtime,
		"resource_test.go": resourceBoundsGeneratedTest,
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-timeout", "600s", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("emitted reader runtime: %v\n%s", err, out)
	}
}
