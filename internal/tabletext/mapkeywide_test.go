package tabletext_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// A bounded string map key is judged against its OWN declared byte bound, and
// the 255-byte cap on schema names never reaches it (docs/SPEC-TABLES.md
// §16.2, §2.13.1): EdgeRow.names is map[string(300)]Item, so a 256-byte and a
// 300-byte key read and a 301-byte key refuses length_over_bound.
func TestTextMapKeyAboveNameCapReadsUnderItsOwnBound(t *testing.T) {
	t.Parallel()
	m, _ := load(t, "../../tables/maps")
	st := m.Lookup("EdgeRow")
	if st == nil {
		t.Fatal("no EdgeRow table")
	}
	for _, n := range []int{255, 256, 300} {
		var r tabletext.Report
		text := `{"names":{"` + strings.Repeat("a", n) + `":{"count":1}}}`
		if !m.Read(m.New(st), []byte(text), &r) || r.Refused {
			t.Fatalf("a %d-byte key under string(300) refused: %+v", n, r)
		}
	}
	var r tabletext.Report
	text := `{"names":{"` + strings.Repeat("a", 301) + `":{"count":1}}}`
	if m.Read(m.New(st), []byte(text), &r) || !r.Refused {
		t.Fatalf("a 301-byte key under string(300) read: %+v", r)
	}
}
