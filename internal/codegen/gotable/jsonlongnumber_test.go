package gotable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// runLongNumberUnit generates and runs the probe in a t.TempDir(), the house
// pattern. The caller places this toolchain test behind the slow-half gate.
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
	runtime := slowtest.SerializeGo(t, "../../../")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
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
	slowtest.Gate(t, "the generated Go runtime")
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
	// a long token past an integer field's range, or one a fixed field cannot
	// hold exactly, refuses the read
	for _, bad := range []string{"{\"i\":1"+zeros+"}", "{\"w\":1."+zeros+"1}"} {
		var v Root
		var r TableReport
		if RootFromJSON(&v,[]byte(bad),&r) || r.Err() == nil { t.Fatalf("a long token past the field read: %+v", r) }
	}
	text:=[]byte("{\"f\":1."+zeros+",\"g\":1."+zeros+"}")
	var v Root
	var r TableReport
	if !RootFromJSON(&v,text,&r) || r != (TableReport{}) {
		t.Fatalf("a valid long number token was refused: %+v",r)
	}
	if v.F != 1.0 { t.Fatalf("float64 landed %v, want 1.0",v.F) }
	if v.G != 1<<16 { t.Fatalf("fixed(16,16) landed raw %d, want 1<<16",v.G) }
	out:=make([]byte,4096)
	n:=RootToJSON(&v,out)
	if n<0 { t.Fatal("the writer refused the value it read") }
	want:=[]byte("{\n  \"f\": 1,\n  \"i\": 0,\n  \"w\": 0,\n  \"g\": 1.0\n}\n")
	if !bytes.Equal(out[:n],want) { t.Fatalf("text %q, want %q",out[:n],want) }
}
`)
}
