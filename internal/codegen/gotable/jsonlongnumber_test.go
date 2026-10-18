package gotable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
)

// goModCache is the module cache the pinned serialize.go runtime lives in. The
// test must RUN by default, so it names the runtime the way the tabletext
// generated-walk tests do rather than skipping when no sibling checkout is
// staged.
func goModCache(t *testing.T) string {
	t.Helper()
	if cache := os.Getenv("GOMODCACHE"); cache != "" {
		return cache
	}
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatalf("go env GOMODCACHE: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// runLongNumberUnit is runGeneratedUnit without the slow-half gate: the card's
// own test command names this test, so it must RUN by default rather than
// skip. The unit is still generated and run in a t.TempDir(), the house
// pattern, and it is one short build.
func runLongNumberUnit(t *testing.T, schema, testSource string) {
	t.Helper()
	u := unitFrom(t, schema)
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	runtime := filepath.Join(goModCache(t), "github.com", "mas-bandwidth", "serialize.go@v1.15.1")
	if _, err := os.Stat(filepath.Join(runtime, "go.mod")); err != nil {
		t.Fatalf("the generated walk needs the pinned serialize.go runtime at %s: %v", runtime, err)
	}
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
	files["values_test.go"] = []byte(testSource)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-timeout", "600s", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated runtime: %v\n%s", err, out)
	}
}

// docs/SPEC-TABLES.md §16.2 Numbers: a token that IS a JSON number is read by
// its VALUE, and its length is never a reason to call it malformed. The
// document's own size is the only bound. Both engines — this generated walk
// and the tool's internal/tabletext — land the same bytes and counters, which
// is what makes the rule one rule (§16.2).
func TestJsonLongNumberTokenLands(t *testing.T) {
	t.Parallel()
	runLongNumberUnit(t, `package probe
fixed table Root {
	f float64
	i int32
	w uint128
	g fixed(16, 16) | min = -32768, max = 32767
}
`, `package probe
import("testing";"bytes";"strings")
func TestLongNumber(t *testing.T) {
	zeros:=strings.Repeat("0",600)
	text:=[]byte("{\"f\":1."+zeros+",\"i\":1"+zeros+",\"w\":1."+zeros+"1,\"g\":1."+zeros+"}")
	var v Root
	var r TableReport
	if !RootFromJsonTolerant(&v,text,&r) {
		t.Fatalf("a valid long number token was refused: %+v",r)
	}
	if v.F != 1.0 { t.Fatalf("float64 landed %v, want 1.0",v.F) }
	if v.I != 2147483647 { t.Fatalf("int32 landed %d, want its max",v.I) }
	if v.G != 1<<16 { t.Fatalf("fixed(16,16) landed raw %d, want 1<<16",v.G) }
	if want:=(TableReport{Clamped:2,KindMismatch:1}); r!=want { t.Fatalf("report %+v, want %+v",r,want) }
	out:=make([]byte,4096)
	n:=RootToJson(&v,out)
	if n<0 { t.Fatal("the writer refused the value it read") }
	want:=[]byte("{\n  \"f\": 1,\n  \"i\": 2147483647,\n  \"w\": 0,\n  \"g\": 1.0\n}\n")
	if !bytes.Equal(out[:n],want) { t.Fatalf("text %q, want %q",out[:n],want) }
}
`)
}
