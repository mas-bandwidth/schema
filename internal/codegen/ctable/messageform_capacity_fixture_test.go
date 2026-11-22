package ctable

import (
	"fmt"
	"os"
	"testing"
)

// TestCMessageLoadCapacityFixture reads the committed capacity fixture,
// testdata/batch_capacity_two.bin (two LoginRequest bodies of
// tables/backend, client_build 40, written by the Go engine and pinned to it
// by test/conformance/harness TestBatchCapacityFixtureIsTheGoEnginesInEveryLeg),
// through the generated C's login_request_load_messages. Storage for one body
// whose client_build is the sentinel 17 owes "A refused batch returns a count
// of `0`" and "refuses before it decodes anything" (docs/SPEC-TABLES.md §3.3):
// a refusal, count 0, the sentinel untouched. Storage for two owes 2 and 40.
// A caller's capacity is a resource limit (§6.5), so this row runs here, in
// the leg's own tests, never in the shared suites.
func TestCMessageLoadCapacityFixture(t *testing.T) {
	t.Parallel()
	schema, err := os.ReadFile("../../../tables/backend/Backend.schema")
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("testdata/batch_capacity_two.bin")
	if err != nil {
		t.Fatal(err)
	}
	runCGenerated(t, string(schema), fmt.Sprintf(cCapacityFixtureProbe, cMessageBytes(fixture)))
}

const cCapacityFixtureProbe = `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if(!(x)){fprintf(stderr,"line %%d: %%s\n",__LINE__,#x);return 1;} }while(0)
static const uint8_t fixture[]={%s};
int main(void)
{
 TableMessageEntry entries[kTableMessageEntriesHere];
 TableVocabulary vocabulary=table_vocabulary(entries,kTableMessageEntriesHere);
 TableReport report={0};
 CHECK(announce_read(&vocabulary,kTableAnnounce,kTableAnnounceBytes,&report));
 LoginRequest one[1];
 login_request_reset(one);
 one[0].client_build=17;
 int64_t count=1;
 CHECK(!login_request_load_messages(one,&count,&vocabulary,fixture,sizeof(fixture),&report));
 CHECK(report.refused);
 if(count!=0){fprintf(stderr,"two bodies into storage for one: count=%%lld, and a refused batch returns 0\n",(long long)count);return 1;}
 CHECK(one[0].client_build==17);
 LoginRequest two[2];
 login_request_reset(two);
 login_request_reset(two+1);
 count=2;
 CHECK(login_request_load_messages(two,&count,&vocabulary,fixture,sizeof(fixture),&report));
 CHECK(count==2 && two[0].client_build==40 && two[1].client_build==40);
 return 0;
}
`
