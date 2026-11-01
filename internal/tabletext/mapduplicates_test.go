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

// TestMapDuplicateKeysLastWins pins the text form's duplicate rule for map
// keys (docs/SPEC-TABLES.md §2.8, §16.2): a repeated key counts `duplicate`
// once per repeat, the LAST occurrence wins, and the entries come out in
// ascending key order. A signed key of -1 and an unsigned key of 2^64-1 share
// their bits and are still each one identity in their own map.
func TestMapDuplicateKeysLastWins(t *testing.T) {
	t.Parallel()
	m := mapDuplicatesModel(t)
	text := `{"names":{"b":"1","a":"2","b":"3","b":"4"},` +
		`"ints":{"-1":1,"5":2,"-1":3},` +
		`"wide":{"18446744073709551615":1,"0":2,"18446744073709551615":7}}`
	inst := m.New(m.Lookup("Maps"))
	var r tabletext.Report
	if !m.Read(inst, []byte(text), &r) {
		t.Fatalf("read refused: %+v", r)
	}
	if r.Duplicate != 4 {
		t.Fatalf("duplicate = %d, want 4 (%+v)", r.Duplicate, r)
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

// TestMapManyKeysIsNotQuadratic reads a map of 2^17 distinct keys and then the
// same keys repeated. A duplicate test that scans the entries placed so far
// makes some 2^33 key comparisons here, which does not finish inside the
// package's test timeout; the hash lookup makes one per key
// (security/text-form-map-duplicates-quadratic).
func TestMapManyKeysIsNotQuadratic(t *testing.T) {
	t.Parallel()
	m := mapDuplicatesModel(t)
	const n = 1 << 17
	var b strings.Builder
	b.WriteString(`{"wide":{`)
	for pass := 0; pass < 2; pass++ {
		for i := 0; i < n; i++ {
			if pass+i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `"%d":%d`, (i*7919)%n, pass)
		}
	}
	b.WriteString(`}}`)
	inst := m.New(m.Lookup("Maps"))
	var r tabletext.Report
	if !m.Read(inst, []byte(b.String()), &r) {
		t.Fatalf("read refused: %+v", r)
	}
	if r.Duplicate != n {
		t.Fatalf("duplicate = %d, want %d", r.Duplicate, n)
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
