package tablewire

import (
	"bytes"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE WRITER'S ELISION TEST JUDGES TEXT BY ITS BYTES (docs/SPEC-TABLES.md §3,
// §3.3: a field is elided only where it holds its declared default). A text
// cell holds its value in Str, so a text element (a text list's element record
// field) or field at its default is default and one holding any other bytes
// is not. No schema reaches
// cellIsDefault with a text cell today (a text list takes no lower bound and
// no fixed extent, §2.13.1, and a scalar text field elides by messageElides
// and elides, not here); the test holds the function to the rule directly so
// a future caller inherits the right answer.
//
// The cases are the committed corpus testdata/celldefault-text: the schema and
// one row per held value, each default beside the unequal values that must
// not pass for it.
func TestCellIsDefaultJudgesTextByItsBytes(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("testdata/celldefault-text/Root.schema")
	if err != nil {
		t.Fatal(err)
	}
	m := covModel(t, string(src))
	st := m.Unit.Tables["Root"]
	field := func(name string) *ir.Field {
		for _, f := range st.Fields {
			if f.Name == name {
				// a text list rides as an array of its element record, so
				// its text cell is that record's one field (ir.TextListElem)
				if elem := f.TextListElem(); elem != nil {
					return elem.Fields[0]
				}
				return f
			}
		}
		t.Fatalf("no field %s", name)
		return nil
	}
	unhex := func(s string) []byte {
		if s == "-" {
			return nil
		}
		b, err := hex.DecodeString(s)
		if err != nil {
			t.Fatalf("cases.tsv: %q: %v", s, err)
		}
		return b
	}
	data, err := os.ReadFile("testdata/celldefault-text/cases.tsv")
	if err != nil {
		t.Fatal(err)
	}
	e := &encoder{m: m}
	seen := map[string]int{}
	for n, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			t.Fatalf("cases.tsv line %d: %d fields, want 5", n+1, len(f))
		}
		name, fld, held, want := f[0], field(f[1]), unhex(f[3]), f[4] == "true"
		if f[4] != "true" && f[4] != "false" {
			t.Fatalf("cases.tsv line %d: is_default %q", n+1, f[4])
		}
		seen[f[1]+"/"+f[4]]++
		if def := m.FieldDefaultCell(fld).Str; !bytes.Equal(def, unhex(f[2])) {
			t.Errorf("%s: the schema declares default %x for %s, the corpus says %s", name, def, f[1], f[2])
		}
		cell := tabletext.Cell{Str: held}
		if got := cellIsDefault(e, fld, &cell); got != want {
			t.Errorf("%s: %s holding %x: cellIsDefault = %v, want %v", name, f[1], held, got, want)
		}
	}
	// every field is judged at its default AND at a value that is not it
	for _, fld := range []string{"names", "title", "blob"} {
		for _, verdict := range []string{"true", "false"} {
			if seen[fld+"/"+verdict] == 0 {
				t.Errorf("the corpus has no is_default=%s row for %s", verdict, fld)
			}
		}
	}
}
