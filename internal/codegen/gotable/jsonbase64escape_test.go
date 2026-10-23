package gotable

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestJsonBase64BodyDecodesEscapesFirst pins docs/SPEC-TABLES.md §16.2: a
// bytes field is a JSON string whose decoded value is base64, so JSON string
// escapes (such as \/ and \uXXXX) are undone before base64 decoding in both
// the generated Go reader and internal/tabletext, while an escape that decodes
// to a character outside the base64 alphabet stays kind_mismatch.
func TestJsonBase64BodyDecodesEscapesFirst(t *testing.T) {
	t.Parallel()

	slowtest.Gate(t, "the generated Go runtime")

	files, err := Generate(goUnit(t, jsonBase64EscapeSchema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	files["go.mod"] = []byte("module probe\n\ngo 1.26\n")
	files["values_test.go"] = []byte(jsonBase64EscapeProbe)
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

	// Verify both engines agree on the same schema and inputs.
	m := tabletext.NewModel(goUnit(t, jsonBase64EscapeSchema))
	st := m.Lookup("Root")

	checkEngine := func(text string, want []byte, wantBlob []byte, wantMismatch int) {
		t.Helper()
		inst := m.New(st)
		var rep tabletext.Report
		if !m.Read(inst, []byte(text), &rep) || rep.Malformed {
			t.Fatalf("%s: engine refused (%+v)", text, rep)
		}
		if rep.KindMismatch != wantMismatch {
			t.Fatalf("%s: engine KindMismatch = %d, want %d (%+v)", text, rep.KindMismatch, wantMismatch, rep)
		}
		fb, _ := inst.FieldByKey("b")
		if !bytes.Equal(fb.Cell.Str, want) {
			t.Fatalf("%s: engine b = %x, want %x", text, fb.Cell.Str, want)
		}
		fp, _ := inst.FieldByKey("p")
		if wantBlob == nil {
			if fp.Cell.Blob != nil {
				t.Fatalf("%s: engine p = %x, want nil", text, fp.Cell.Blob.Data)
			}
		} else if fp.Cell.Blob == nil || !bytes.Equal(fp.Cell.Blob.Data, wantBlob) {
			t.Fatalf("%s: engine p = %+v, want %x", text, fp.Cell.Blob, wantBlob)
		}
	}

	checkEngine(`{"b":"\/w==","p":"\/w=="}`, []byte{0xff}, []byte{0xff}, 0)
	checkEngine(`{"b":"\u0041Q==","p":"\u0041Q=="}`, []byte{0x01}, []byte{0x01}, 0)
	checkEngine(`{"b":"\n","p":"\n"}`, nil, nil, 2)
}

const jsonBase64EscapeSchema = `package probe

table Root {
	b bytes(8)
	p *bytes
}
`

const jsonBase64EscapeProbe = `package probe

import (
	"bytes"
	"testing"
)

func TestBase64Escapes(t *testing.T) {
	cases := []struct {
		name         string
		text         string
		want         []byte
		wantBlob     []byte
		wantMismatch int32
	}{
		{"escaped solidus", ` + "`" + `{"b":"\/w==","p":"\/w=="}` + "`" + `, []byte{0xff}, []byte{0xff}, 0},
		{"unicode escape in alphabet", ` + "`" + `{"b":"\u0041Q==","p":"\u0041Q=="}` + "`" + `, []byte{0x01}, []byte{0x01}, 0},
		{"escaped newline outside alphabet", ` + "`" + `{"b":"\n","p":"\n"}` + "`" + `, nil, nil, 2},
	}
	for _, tc := range cases {
		var b RootBuilder
		if !b.Init() {
			t.Fatal("Init")
		}
		var rep TableReport
		if !RootFromJsonTolerant(&b, []byte(tc.text), &rep) || rep.Malformed {
			t.Fatalf("%s: RootFromJsonTolerant refused (%+v)", tc.name, rep)
		}
		if rep.KindMismatch != tc.wantMismatch {
			t.Fatalf("%s: KindMismatch = %d, want %d (%+v)", tc.name, rep.KindMismatch, tc.wantMismatch, rep)
		}
		r := b.GetRoot()
		got := r.B[:r.BLength]
		if !bytes.Equal(got, tc.want) {
			t.Fatalf("%s: B = %x, want %x", tc.name, got, tc.want)
		}
		live := TableBytesAt(&r.P, &b.Arena)
		if tc.wantBlob == nil {
			if r.P != 0 || live.Length != 0 {
				t.Fatalf("%s: P = %x (len=%d), want nil", tc.name, live.Data, live.Length)
			}
		} else {
			if r.P == 0 || !bytes.Equal(live.Data[:live.Length], tc.wantBlob) {
				t.Fatalf("%s: P = %x (ref=%d), want %x", tc.name, live.Data, r.P, tc.wantBlob)
			}
		}
		b.Shutdown()
	}
}
`
