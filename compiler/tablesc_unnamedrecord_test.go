package compiler

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A node record whose type this reader does not name is an unknown payload
// (docs/SPEC-TABLES.md §3.1, §4): the C graph load reads its body as strictly
// as any other unknown field, so damage there refuses the load and the C
// verdict matches the compiler's engine for every case.
func TestCTableUnnamedRecordBodyChecked(t *testing.T) {
	t.Parallel()
	const older = `package probe
table Root {
 value uint32
}
`
	const newer = `package probe
table Extra {
 items []uint8
 name string(8)
 flag bool
}
table Root {
 value uint32
 extra *Extra
}
`
	writer := unitFromSource(t, newer)
	wm := tabletext.NewModel(writer)
	value := wm.New(wm.Lookup("Root"))
	var report tabletext.Report
	fixture := `{"value":7,"extra":{"items":[201],"name":"QZQ","flag":true}}`
	if !wm.Read(value, []byte(fixture), &report) || !report.Silent() {
		t.Fatalf("fixture: %+v", report)
	}
	clean, err := tablewire.Encode(wm, value)
	if err != nil {
		t.Fatal(err)
	}
	if records, whole := tablewire.FileNodeRecords(clean); !whole || len(records) != 1 {
		t.Fatalf("the fixture must carry one node record: %v %v", records, whole)
	}
	// Each damage rewrites bytes in place inside the Extra record's body, so
	// every frame length around it stays true.
	damage := func(name string, from, to []byte) []byte {
		if bytes.Count(clean, from) != 1 || len(from) != len(to) {
			t.Fatalf("%s: the marker %x must occur once in %x", name, from, clean)
		}
		wire := bytes.Clone(clean)
		at := bytes.Index(wire, from)
		copy(wire[at:], to)
		return wire
	}
	cases := []struct {
		name string
		wire []byte
	}{
		{"clean", clean},
		{"count with no elements", damage("count", []byte{0x01, 201}, []byte{0x03, 201})},
		{"ill-formed UTF-8", damage("utf8", []byte("QZQ"), []byte{0xff, 0xfe, 'Q'})},
		{"bool byte 2", damage("bool", []byte{0x01, 0x01, 0x00}, []byte{0x01, 0x02, 0x00})},
	}
	reader := unitFromSource(t, older)
	rm := tabletext.NewModel(reader)
	var wires, expects strings.Builder
	for i, tc := range cases {
		var r tabletext.Report
		ok, _ := tablewire.Decode(rm, rm.New(rm.Lookup("Root")), tc.wire, &r)
		if i == 0 && (!ok || r.Unknown == 0) {
			t.Fatalf("the clean file must load with the record unknown: %v %+v", ok, r)
		}
		if i > 0 && ok {
			t.Fatalf("%s: the engine must refuse the damage", tc.name)
		}
		fmt.Fprintf(&wires, "static const uint8_t wire%d[]={", i)
		for _, b := range tc.wire {
			fmt.Fprintf(&wires, "0x%02x,", b)
		}
		wires.WriteString("};\n")
		verdict := 0
		if ok {
			verdict = 1
		}
		fmt.Fprintf(&expects, " EXPECT(%q,verdict(wire%d,sizeof(wire%d)),%d);\n", tc.name, i, i, verdict)
	}
	source := fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#include <stdlib.h>
static int failed=0;
#define EXPECT(name,got,want) do { int g_=(got); if(g_!=(want)) { fprintf(stderr,"%%s: C verdict %%d, engine verdict %%d\n",(name),g_,(want)); failed=1; } } while(0)
%s
static int verdict(const uint8_t * wire,int64_t bytes)
{
 TableReport report; const Root * root; RootBuilder builder; int loaded, built; uint8_t * region;
 int64_t need=root_load_measure(wire,bytes);
 region=(uint8_t *)malloc(need>0 ? (size_t)need : 16u); if(region==NULL) { return -1; }
 memset(&report,0,sizeof(report));
 root=root_load(region,need>0 ? need : 0,wire,bytes,&report);
 loaded=root!=NULL && !report.refused;
 if((root!=NULL)!=(!report.refused)) { free(region); return -2; }
 free(region);
 if(!root_builder_init(&builder)) { return -1; }
 memset(&report,0,sizeof(report));
 built=root_load_builder(&builder,wire,bytes,&report) && !report.refused;
 root_builder_shutdown(&builder);
 if(built!=loaded) { return -3; }
 return loaded;
}
int main(void)
{
%s return failed;
}
`, wires.String(), expects.String())
	runCTableWireProbe(t, reader, source)
}
