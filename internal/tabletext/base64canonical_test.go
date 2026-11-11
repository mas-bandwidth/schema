package tabletext_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const canonicalBytesSchema = `package probe

table Blob {
    bounded bytes(4)
    pointer *bytes
}
`

func canonicalBytesModel(t *testing.T) (*tabletext.Model, *ir.Struct) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(path, []byte(canonicalBytesSchema), 0o600); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(u)
	st := model.Lookup("Blob")
	if st == nil {
		t.Fatal("no Blob table")
	}
	return model, st
}

func TestTextBytesReadCanonicalBase64Only(t *testing.T) {
	t.Parallel()
	model, st := canonicalBytesModel(t)
	cases := []struct {
		name string
		body string
		want []byte
		ok   bool
	}{
		{name: "canonical padded", body: "QUI=", want: []byte{'A', 'B'}, ok: true},
		{name: "unpadded", body: "QUI"},
		{name: "nonzero pad bits", body: "QUJ="},
		{name: "one pad where two belong", body: "QQ="},
		{name: "padding in the middle", body: "Q=Q="},
		{name: "empty", body: "", want: []byte{}, ok: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inst := model.New(st)
			text := []byte(`{"bounded":"` + c.body + `","pointer":"` + c.body + `"}`)
			var report tabletext.Report
			ok := model.Read(inst, text, &report)
			if ok != c.ok {
				t.Fatalf("read ok=%t, want %t; report=%+v", ok, c.ok, report)
			}
			if c.ok {
				for _, name := range []string{"bounded", "pointer"} {
					fv, found := inst.FieldByKey(name)
					if !found {
						t.Fatalf("no %s field", name)
					}
					got := fv.Cell.Str
					if name == "pointer" {
						if fv.Cell.Blob == nil {
							t.Fatalf("%s blob is nil", name)
						}
						got = fv.Cell.Blob.Data
					}
					if !bytes.Equal(got, c.want) {
						t.Errorf("%s bytes=%x, want %x", name, got, c.want)
					}
				}
			}
		})
	}
}
