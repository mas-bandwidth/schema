package ctable

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestCNestedUnionFramesAreWhole(t *testing.T) {
	slowtest.Gate(t, "the C compiler and sanitizers")
	u := unitFrom(t, `package probe
union Inner { number int64 }
union Middle { inner Inner }
union Outer { middle Middle }
table Root { value Outer }
`)
	var body strings.Builder
	body.WriteString(`
 static uint8_t region[65536] __attribute__((aligned(64)));
 static uint8_t retained[4096]; static TableRetainId retained_ids[64];
 #define CHECK(c,n) do { if(!(c)) { fprintf(stderr,"FAIL: %s\n",n); return 1; } } while(0)
 `)
	for _, source := range []struct {
		kind  byte
		width int
	}{{2, 1}, {3, 2}, {4, 4}, {5, 8}} {
		for tail := -1; tail < 3; tail++ {
			payload := make([]byte, source.width)
			payload[0] = 5
			if tail == 2 {
				payload = append(payload, 0xee)
			}
			inner := append([]byte{4, source.kind, byte(len(payload))}, payload...)
			if tail == 1 {
				inner = append(inner, 0xee)
			}
			middle := append([]byte{3, 15, byte(len(inner))}, inner...)
			if tail == 0 {
				middle = append(middle, 0xee)
			}
			outer := append([]byte{2, 15, byte(len(middle))}, middle...)
			wire := append([]byte{1, 1, 15}, outer...)
			wire = append(wire, 0)
			for _, id := range []string{"value", "middle", "inner", "number"} {
				wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId(id))
			}
			wire = binary.LittleEndian.AppendUint64(wire, 4)
			want := 0
			if tail < 0 {
				want = 1
			}
			name := fmt.Sprintf("kind%d/tail%d", source.kind, tail)
			fmt.Fprintf(&body, `
 { const uint8_t wire[]={%s}; TableReport report; TableRetain store; RootBuilder builder; const Root *value; int ok;
   memset(&report,0,sizeof(report));
   value=root_load(region,sizeof(region),wire,sizeof(wire),&report);
   CHECK((value!=NULL)==%d,"%s plain outcome");
   CHECK((report.refused==0)==%d,"%s plain refusal");
   CHECK(value==NULL || value->value.as.middle.as.inner.as.number==5,"decoded widened scalar");
   memset(&report,0,sizeof(report)); memset(&store,0,sizeof(store));
   store.bytes=retained; store.capacity=sizeof(retained); store.ids=retained_ids; store.id_capacity=64;
   value=root_load_retain(region,sizeof(region),wire,sizeof(wire),&store,&report);
   CHECK((value!=NULL)==%d,"%s retain outcome");
   CHECK((report.refused==0)==%d,"%s retain refusal");
   CHECK(value==NULL || value->value.as.middle.as.inner.as.number==5,"retained widened scalar");
   if(value!=NULL) CHECK(root_load_measure(wire,sizeof(wire))>0,"valid frame measure");
   CHECK(root_builder_init(&builder),"builder init"); memset(&report,0,sizeof(report));
   ok=root_load_builder(&builder,wire,sizeof(wire),&report);
   CHECK(ok==%d,"%s builder outcome");
   CHECK((report.refused==0)==%d,"%s builder refusal"); root_builder_shutdown(&builder);
   if(%d && root_load_measure(wire,sizeof(wire))>=0) fprintf(stderr,"MEASURE accepted %s\n");
 }
 `, cBytes(wire), want, name, want, name, want, name, want, name, want, name, want, name, 1-want, name)
		}
	}
	out, err := runCNodeRecordTerminatorProbe(t, u, body.String(), nil)
	if err != nil {
		t.Fatalf("nested union frames: %v\n%s", err, out)
	}
	if strings.Contains(out, "MEASURE") {
		t.Fatalf("nested union framing measure: %s", out)
	}
	// Removing only the prepass verdict leaves semantic loads protected by
	// their own arm-end check, but must expose the malformed measure.
	out, err = runCNodeRecordTerminatorProbe(t, u, body.String(), func(files map[string][]byte) error {
		const anchor = "if ( sub.next_ref != NULL ) { *sub.next_ref = UINT64_MAX; }"
		const header = "ProbeTable.h"
		source := string(files[header])
		if strings.Count(source, anchor) != 1 {
			return fmt.Errorf("framed prepass control anchor absent or repeated")
		}
		files[header] = []byte(strings.Replace(source, anchor, "/* SABOTAGED framing verdict */", 1))
		return nil
	})
	if err != nil || !strings.Contains(out, "MEASURE accepted kind2/tail0") {
		t.Fatalf("framed prepass negative control did not expose the measure: %v\n%s", err, out)
	}
}
