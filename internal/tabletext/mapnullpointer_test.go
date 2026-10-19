package tabletext_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestMapNullPointerValuesRoundTrip holds the map and pointer rows of
// docs/SPEC-TABLES.md §16.2 together: a null map value of pointer kind is a
// null reference, while a plain value still refuses null and an optional
// value still reads null as absent.
func TestMapNullPointerValuesRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const source = `package mapnull

fixed table Node
{
    n int32
}

table Holder
{
    node_s map[string(8)]*Node
    node_i map[int32]*Node
    bytes_s map[string(8)]*bytes
    bytes_i map[int32]*bytes
    string_s map[string(8)]*string
    string_i map[int32]*string
    plain_int map[string(8)]int32
    plain_string map[int32]string(8)
    optional_int map[int32]?int32
}
`
	if err := os.WriteFile(filepath.Join(dir, "MapNull.schema"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	_, u := load(t, dir)

	for _, tc := range []struct {
		name, field, kind string
		stringKey         bool
	}{
		{"table/string-key", "node_s", "table", true},
		{"table/integer-key", "node_i", "table", false},
		{"bytes/string-key", "bytes_s", "bytes", true},
		{"bytes/integer-key", "bytes_i", "bytes", false},
		{"string/string-key", "string_s", "string", true},
		{"string/integer-key", "string_i", "string", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := tabletext.NewModel(u)
			inst := m.New(m.Lookup("Holder"))
			fv := field(t, inst, tc.field)
			nullEntry := m.NewMapEntry(fv.Def)
			setEntry := m.NewMapEntry(fv.Def)
			nullKey, setKey := "1", "2"
			if tc.stringKey {
				nullKey, setKey = "a", "z"
				nullEntry.Fields[0].Cell.Str = []byte(nullKey)
				setEntry.Fields[0].Cell.Str = []byte(setKey)
			} else {
				nullEntry.Fields[0].Cell.I = 1
				setEntry.Fields[0].Cell.I = 2
			}
			switch tc.kind {
			case "table":
				node := m.New(m.Lookup("Node"))
				node.Fields[0].Cell.I = 17
				setEntry.Fields[1].Cell.Node = node
			case "bytes", "string":
				setEntry.Fields[1].Cell.Blob = &tabletext.Blob{Data: []byte("seed")}
			}
			fv.Entries = []tabletext.Cell{{Tab: nullEntry}, {Tab: setEntry}}
			first, err := m.Write(inst)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(first, []byte(`"`+nullKey+`": null`)) {
				t.Fatalf("writer omitted the null map value: %s", first)
			}
			back := m.New(m.Lookup("Holder"))
			var report tabletext.Report
			if !m.Read(back, first, &report) || !report.Silent() {
				t.Fatalf("reader refused its writer's null pointer value: %+v\n%s", report, first)
			}
			entries := field(t, back, tc.field).Entries
			if len(entries) != 2 {
				t.Fatalf("read %d entries, want two", len(entries))
			}
			if tc.stringKey {
				if string(entries[0].Tab.Fields[0].Cell.Str) != nullKey || string(entries[1].Tab.Fields[0].Cell.Str) != setKey {
					t.Fatalf("string keys did not survive: %+v", entries)
				}
			} else if entries[0].Tab.Fields[0].Cell.I != 1 || entries[1].Tab.Fields[0].Cell.I != 2 {
				t.Fatalf("integer keys did not survive: %+v", entries)
			}
			gotNull := entries[0].Tab.Fields[1].Cell
			gotSet := entries[1].Tab.Fields[1].Cell
			switch tc.kind {
			case "table":
				if gotNull.Node != nil || gotSet.Node == nil || gotSet.Node.Fields[0].Cell.I != 17 {
					t.Fatalf("table pointer values changed: null=%+v set=%+v", gotNull, gotSet)
				}
			case "bytes", "string":
				if gotNull.Blob != nil || gotSet.Blob == nil || !bytes.Equal(gotSet.Blob.Data, []byte("seed")) {
					t.Fatalf("blob pointer values changed: null=%+v set=%+v", gotNull, gotSet)
				}
			}
			second, err := m.Write(back)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(second, first) {
				t.Fatalf("write/read/write changed bytes:\nfirst %s\nsecond %s", first, second)
			}
		})
	}

	for _, tc := range []struct {
		name, field, text string
	}{
		{"plain-int", "plain_int", `{"plain_int":{"a":null}}`},
		{"plain-string", "plain_string", `{"plain_string":{"1":null}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := tabletext.NewModel(u)
			inst := m.New(m.Lookup("Holder"))
			var report tabletext.Report
			if m.Read(inst, []byte(tc.text), &report) {
				t.Fatalf("non-pointer map value accepted null: %s", tc.text)
			}
			if report != (tabletext.Report{Malformed: true}) {
				t.Fatalf("null changed the BASE refusal report: %+v", report)
			}
			if len(field(t, inst, tc.field).Entries) != 0 {
				t.Fatal("refused map value was inserted")
			}
		})
	}

	t.Run("optional-int", func(t *testing.T) {
		t.Parallel()
		m := tabletext.NewModel(u)
		inst := m.New(m.Lookup("Holder"))
		text := []byte(`{"optional_int":{"1":null,"2":7}}`)
		var report tabletext.Report
		if !m.Read(inst, text, &report) || !report.Silent() {
			t.Fatalf("optional null no longer reads as absent: %+v", report)
		}
		entries := field(t, inst, "optional_int").Entries
		if len(entries) != 2 || entries[0].Tab.Fields[1].Present || !entries[1].Tab.Fields[1].Present || entries[1].Tab.Fields[1].Cell.I != 7 {
			t.Fatalf("optional value presence changed: %+v", entries)
		}
		out, err := m.Write(inst)
		if err != nil || !strings.Contains(string(out), `"1": null`) {
			t.Fatalf("optional absent value did not write null: %v\n%s", err, out)
		}
	})

}
