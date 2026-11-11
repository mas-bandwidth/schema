package tabletext_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const textWriterZeroSchema = `package probe

table Root
{
    t string(8)
    list [..4]string(8)
    m map[string(8)]int32
    caption *string
}
`

func textWriterZeroModel(t *testing.T) *tabletext.Model {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "probe.schema"), []byte(textWriterZeroSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return tabletext.NewModel(u)
}

// TestTextWriterRefusesZeroByte holds the writer to the reader's rule: text
// storage holding a zero byte is refused before a byte is written, in every
// place the text form places text (docs/SPEC-TABLES.md §16.3, §16.14 W0, W8;
// docs/SPEC.md §5.1 row 8). U+0001 and U+001F are still text and still write
// as their \u00XX escapes.
func TestTextWriterRefusesZeroByte(t *testing.T) {
	t.Parallel()
	zero := []byte("a\x00b")
	cases := []struct {
		name string
		text string
		set  func(*tabletext.Instance)
	}{
		{"string field", `{"t":"ab"}`, func(inst *tabletext.Instance) {
			field(t, inst, "t").Cell.Str = zero
		}},
		{"text list element", `{"list":["a","b"]}`, func(inst *tabletext.Instance) {
			field(t, inst, "list").Elems[1].Tab.Fields[0].Cell.Str = zero
		}},
		{"map key", `{"m":{"a":1}}`, func(inst *tabletext.Instance) {
			field(t, inst, "m").Entries[0].Tab.Fields[0].Cell.Str = zero
		}},
		{"byte buffer written as text", `{"caption":"ab"}`, func(inst *tabletext.Instance) {
			field(t, inst, "caption").Cell.Blob = &tabletext.Blob{Data: zero}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			m := textWriterZeroModel(t)
			inst := m.New(m.Lookup("Root"))
			var r tabletext.Report
			if !m.Read(inst, []byte(c.text), &r) || !r.Silent() {
				t.Fatalf("%s: the control did not read clean: %+v", c.text, r)
			}
			c.set(inst)
			text, err := m.Write(inst)
			if err == nil {
				t.Fatalf("the writer accepted a zero byte: %s", text)
			}
			if !strings.HasPrefix(err.Error(), "invalid_value:") {
				t.Fatalf("the writer refused by another name: %v", err)
			}
			if text != nil {
				t.Fatalf("the writer handed back a prefix: %q", text)
			}
		})
	}

	t.Run("controls still escape", func(t *testing.T) {
		t.Parallel()
		m := textWriterZeroModel(t)
		inst := m.New(m.Lookup("Root"))
		var r tabletext.Report
		if !m.Read(inst, []byte(`{"t":"ab"}`), &r) || !r.Silent() {
			t.Fatalf("the control did not read clean: %+v", r)
		}
		field(t, inst, "t").Cell.Str = []byte("a\x01b\x1fc")
		text, err := m.Write(inst)
		if err != nil {
			t.Fatalf("the writer refused U+0001/U+001F: %v", err)
		}
		if !bytes.Contains(text, []byte(`\u0001`)) || !bytes.Contains(text, []byte(`\u001f`)) {
			t.Fatalf("the controls are not \\u00XX escapes: %s", text)
		}
	})
}
