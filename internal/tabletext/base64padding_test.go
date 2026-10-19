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
// rule, so every body lands the same bytes and raises the same counters.
// Red at BASE: tabletext skipped any '=' anywhere and accepted A=Q=, AQ===,
// AQ= and =AQI where the walk's strict read refuses them base64_not_canonical.
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
		body   string
		text   string
		ok     bool
		data   []byte
		km, cl int
		mal    bool
	}
	wants := make([]want, 0, len(bodies))
	for _, body := range bodies {
		inst := model.New(st)
		text := `{"data":` + strconv.Quote(body) + `}`
		var report tabletext.Report
		ok := model.Read(inst, []byte(text), &report)
		fv, found := inst.FieldByKey("data")
		if !found {
			t.Fatal("no data field")
		}
		wants = append(wants, want{
			body: body,
			text: text,
			ok:   ok,
			data: append([]byte(nil), fv.Cell.Str...),
			km:   report.KindMismatch,
			cl:   report.Clamped,
			mal:  report.Malformed,
		})
	}

	var src strings.Builder
	src.WriteString("package probe\n\nimport (\n\t\"bytes\"\n\t\"testing\"\n)\n\n")
	src.WriteString("func TestWalkBase64Padding(t *testing.T) {\n")
	src.WriteString("\tcases := []struct {\n\t\tbody, text string\n\t\tok         bool\n\t\tdata       []byte\n\t\tkm, cl     int32\n\t\tmal        bool\n\t}{\n")
	for _, w := range wants {
		data := "nil"
		if w.data != nil {
			parts := make([]string, len(w.data))
			for i, b := range w.data {
				parts[i] = fmt.Sprintf("0x%02x", b)
			}
			data = "[]byte{" + strings.Join(parts, ", ") + "}"
		}
		fmt.Fprintf(&src, "\t\t{%s, %s, %t, %s, %d, %d, %t},\n",
			strconv.Quote(w.body), strconv.Quote(w.text), w.ok, data, w.km, w.cl, w.mal)
	}
	src.WriteString(`	}
	for _, c := range cases {
		var v Blob
		BlobReset(&v)
		var r TableReport
		ok := BlobFromJsonTolerant(&v, []byte(c.text), &r)
		got := append([]byte(nil), v.Data[:v.DataLength]...)
		if ok != c.ok || !bytes.Equal(got, c.data) || r.KindMismatch != c.km || r.Clamped != c.cl || r.Malformed != c.mal {
			t.Fatalf("%q: walk ok=%t data=%x km=%d cl=%d mal=%t; text ok=%t data=%x km=%d cl=%d mal=%t",
				c.body, ok, got, r.KindMismatch, r.Clamped, r.Malformed, c.ok, c.data, c.km, c.cl, c.mal)
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
	runtime := filepath.Join(goModCache(t), "github.com", "mas-bandwidth", "serialize.go@v1.15.1")
	if _, err := os.Stat(filepath.Join(runtime, "go.mod")); err != nil {
		t.Fatalf("the generated walk needs the pinned serialize.go runtime at %s: %v", runtime, err)
	}
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\n\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
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
