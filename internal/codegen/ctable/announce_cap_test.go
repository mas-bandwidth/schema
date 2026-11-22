package ctable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE ANNOUNCEMENT WRITER REFUSES A VOCABULARY ITS READERS REFUSE
// (docs/SPEC-TABLES.md §3.3, R8 follow-up), for C: announce and
// announce_measure refuse a unit with 4097 vocabulary entries and write the one
// with exactly 4096, which announce_read accepts. Red when the refusal is
// removed.
func TestCAnnouncementRefusesMoreThan4096Entries(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		entries int
		refused bool
	}{{4096, false}, {4097, true}} {
		src := tablewire.AnnounceSchema(row.entries)
		ast, diags := parser.Parse("Idcap.schema", []byte(src))
		if len(diags) > 0 {
			t.Fatal(diags)
		}
		unit, diags := check.Unit([]check.SourceFile{{Path: "Idcap.schema", Name: "Idcap.schema", Base: "Idcap", Bytes: []byte(src), AST: ast}})
		if len(diags) > 0 {
			t.Fatal(diags)
		}
		refused := "0"
		if row.refused {
			refused = "1"
		}
		probe := `#include "IdcapTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if(!(x)){ fprintf(stderr,"line %d: %s\n",__LINE__,#x); return 1; } } while(0)
static uint8_t ann[1 << 20];
static TableMessageEntry entries[kTableMessageEntriesHere];
int main(void)
{
    int64_t n = announce_measure(), w = announce(ann, (int64_t)sizeof(ann));
    if (` + refused + `) { CHECK(n < 0 && w < 0); return 0; }
    CHECK(n > 0 && w == n);
    TableVocabulary v = table_vocabulary(entries, kTableMessageEntriesHere);
    TableReport report; memset(&report, 0, sizeof(report));
    CHECK(announce_read(&v, ann, w, &report));
    return 0;
}
`
		runCMessageUnit(t, newCMessageUnitBuilds(t).of(t, "idcap", unit), probe)
	}
}
