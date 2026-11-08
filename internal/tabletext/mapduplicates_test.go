package tabletext_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const mapDuplicatesSchema = `package mapdup

table Maps
{
    names map[string(16)]string(4)
    ints map[int64]int32
    wide map[uint64]int32
}
`

func mapDuplicatesModel(t *testing.T) *tabletext.Model {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "MapDup.schema"), []byte(mapDuplicatesSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	_, u := load(t, dir)
	return tabletext.NewModel(u)
}

// TestMapRepeatedKeyRefuses pins the text form's rule for map keys
// (docs/SPEC-TABLES.md §2.8, §16.2, §6.5): a repeated key refuses the whole
// read, and distinct keys come out in ascending key order. A signed key of -1
// and an unsigned key of 2^64-1 share their bits and are still each one
// identity in their own map, so neither repeats in the control.
func TestMapRepeatedKeyRefuses(t *testing.T) {
	t.Parallel()
	m := mapDuplicatesModel(t)
	for _, text := range []string{
		`{"names":{"b":"1","a":"2","b":"3"}}`,
		`{"ints":{"-1":1,"5":2,"-1":3}}`,
		`{"wide":{"18446744073709551615":1,"0":2,"18446744073709551615":7}}`,
		`{"ints":{"-1":1,"-1.0e0":2}}`,
	} {
		var r tabletext.Report
		if m.Read(m.New(m.Lookup("Maps")), []byte(text), &r) || !r.Refused || !strings.HasPrefix(r.Reason, "malformed") {
			t.Fatalf("%s: want a refusal, got %+v", text, r)
		}
	}
	text := `{"names":{"b":"4","a":"2"},` +
		`"ints":{"5":2,"-1":3},` +
		`"wide":{"18446744073709551615":7,"0":2}}`
	inst := m.New(m.Lookup("Maps"))
	var r tabletext.Report
	if !m.Read(inst, []byte(text), &r) || !r.Silent() {
		t.Fatalf("read refused: %+v", r)
	}
	out, err := m.Write(inst)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"names":{"a":"2","b":"4"}`,
		`"ints":{"-1":3,"5":2}`,
		`"wide":{"0":2,"18446744073709551615":7}`,
	} {
		if !strings.Contains(strings.Join(strings.Fields(string(out)), ""), want) {
			t.Errorf("the written value lacks %s:\n%s", want, out)
		}
	}
}

// TestMapManyKeysIsNotQuadratic reads a map of 2^17 distinct keys, then the
// same keys with one repeated at the end. A repeat test that scans the entries
// placed so far makes some 2^33 key comparisons here, which does not finish
// inside the package's test timeout; the hash lookup makes one per key
// (security/text-form-map-duplicates-quadratic).
func TestMapManyKeysIsNotQuadratic(t *testing.T) {
	t.Parallel()
	m := mapDuplicatesModel(t)
	const n = 1 << 17
	var b strings.Builder
	b.WriteString(`{"wide":{`)
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"%d":1`, (i*7919)%n)
	}
	distinct := b.String() + `}}`
	repeated := b.String() + `,"0":2}}`
	var r tabletext.Report
	if m.Read(m.New(m.Lookup("Maps")), []byte(repeated), &r) || !r.Refused {
		t.Fatalf("a repeated key read: %+v", r)
	}
	inst := m.New(m.Lookup("Maps"))
	r = tabletext.Report{}
	if !m.Read(inst, []byte(distinct), &r) || !r.Silent() {
		t.Fatalf("read refused: %+v", r)
	}
	var wide *tabletext.Field
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == "wide" {
			wide = &inst.Fields[i]
		}
	}
	if wide == nil || len(wide.Entries) != n {
		t.Fatalf("the map does not hold %d entries", n)
	}
	for i, e := range wide.Entries {
		if e.Tab.Fields[0].Cell.U != uint64(i) || e.Tab.Fields[1].Cell.I != 1 {
			t.Fatalf("entry %d is key %d value %d, want key %d value 1", i, e.Tab.Fields[0].Cell.U, e.Tab.Fields[1].Cell.I, i)
		}
	}
}
