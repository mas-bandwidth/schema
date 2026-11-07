package compiler

import (
	"fmt"
	"os"
	"testing"
)

// A framed pointer with trailing bytes is malformed before any pointee lookup.
// This corpus mutation used to add a second kind mismatch in both C readers.
// The damage discards the whole load (docs/SPEC-TABLES.md §4): both reads hand
// back no root, the retaining read's store stays empty, and the report still
// counts the one kind mismatch.
func TestCTablePointerArmFraming(t *testing.T) {
	u, err := New().Load([]string{"../tables/stream/Stream.schema"})
	if err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile("../testdata/wire/tables/fuzz-vectors/pointer_arm_trailing.bin")
	if err != nil {
		t.Fatal(err)
	}
	runCTableWireProbe(t, u, fmt.Sprintf(`#include "StreamTable.h"
#include <stdio.h>
static const uint8_t wire[]={%s};
#define CHECK(x) do {if(!(x)){fprintf(stderr,"line %%d: %%s\n",__LINE__,#x);return 1;}}while(0)
int main(void){TableReport report={0};TableRetain retain={0};TableRetainId ids[32];uint8_t store[4096];const Feed * root;int64_t need=feed_load_measure(wire,sizeof(wire));uint8_t *region;
CHECK(need==136);region=(uint8_t*)malloc((size_t)need);CHECK(region);
root=feed_load_tolerant(region,need,wire,sizeof(wire),&report);CHECK(!root&&report.malformed&&report.unknown==1&&report.kind_mismatch==1&&!report.refused);
report=(TableReport){0};retain.bytes=store;retain.capacity=sizeof(store);retain.ids=ids;retain.id_capacity=32;
root=feed_load_retain(region,need,wire,sizeof(wire),&retain,&report);CHECK(!root&&report.malformed&&report.unknown==1&&report.kind_mismatch==1&&!report.refused);CHECK(retain.used==0&&retain.count==0);free(region);return 0;}
`, cppWire(wire)))
}
