package tabletext_test

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const base64PaddingSchema = `package probe

fixed table Blob {
    data bytes(16)
}
`

func base64PaddingUnit(t *testing.T) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(path, []byte(base64PaddingSchema), 0o600); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// TestTextBase64PaddingIsTheWalks pins docs/SPEC-TABLES.md §16.2's bytes(N)
// padding grammar: tabletext and the generated walk are two readers of one
// rule, so every body gets the same verdict (§6.5), and an accepted body lands
// the same bytes. The generated walk's verdict is its read with no refusing
// condition counted.
func TestTextBase64PaddingIsTheWalks(t *testing.T) {
	t.Parallel()
	u := base64PaddingUnit(t)
	model := tabletext.NewModel(u)
	st := model.Lookup("Blob")
	if st == nil {
		t.Fatal("no Blob table")
	}
	bodies := []string{"AQ==", "AQ", "A=Q=", "AQ===", "AQ=", "A", "AQID", "=AQI"}
	type want struct {
		body string
		text string
		ok   bool
		data []byte
	}
	wants := make([]want, 0, len(bodies))
	accepted := 0
	for _, body := range bodies {
		inst := model.New(st)
		text := `{"data":` + strconv.Quote(body) + `}`
		var report tabletext.Report
		ok := model.Read(inst, []byte(text), &report)
		if ok == report.Refused {
			t.Fatalf("%q: ok=%t disagrees with the report %+v", body, ok, report)
		}
		fv, found := inst.FieldByKey("data")
		if !found {
			t.Fatal("no data field")
		}
		var data []byte
		if ok {
			accepted++
			data = append([]byte(nil), fv.Cell.Str...)
		}
		wants = append(wants, want{body: body, text: text, ok: ok, data: data})
	}
	// both verdicts are present, so the cross-check has an accepted twin for
	// every refusal it compares
	if accepted == 0 || accepted == len(bodies) {
		t.Fatalf("the bodies carry one verdict only: %d of %d accepted", accepted, len(bodies))
	}

	var src strings.Builder
	src.WriteString("package probe\n\nimport (\n\t\"bytes\"\n\t\"testing\"\n)\n\n")
	src.WriteString("func TestWalkBase64Padding(t *testing.T) {\n")
	src.WriteString("\tcases := []struct {\n\t\tbody, text string\n\t\tok         bool\n\t\tdata       []byte\n\t}{\n")
	for _, w := range wants {
		data := "nil"
		if w.data != nil {
			parts := make([]string, len(w.data))
			for i, b := range w.data {
				parts[i] = fmt.Sprintf("0x%02x", b)
			}
			data = "[]byte{" + strings.Join(parts, ", ") + "}"
		}
		fmt.Fprintf(&src, "\t\t{%s, %s, %t, %s},\n",
			strconv.Quote(w.body), strconv.Quote(w.text), w.ok, data)
	}
	src.WriteString(`	}
	for _, c := range cases {
		var v Blob
		BlobReset(&v)
		var r TableReport
		ok := BlobFromJSON(&v, []byte(c.text), &r)
		if ok != c.ok {
			t.Fatalf("%q: walk ok=%t report=%+v; text ok=%t", c.body, ok, r, c.ok)
		}
		if got := append([]byte(nil), v.Data[:v.DataLength]...); ok && !bytes.Equal(got, c.data) {
			t.Fatalf("%q: walk data=%x; text data=%x", c.body, got, c.data)
		}
	}
}
`)

	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	runtime := slowtest.SerializeGo(t, "../..")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	files["values_test.go"] = []byte(src.String())
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-timeout", "600s", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly -p=4")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated walk: %v\n%s", err, out)
	}
}
