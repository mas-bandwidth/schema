package compiler

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// A count past the int32 storage cap sends a variable load down the builder
// fallback. Its refusal is still a refusal: the load returns NULL and the
// report says so, on the plain and the retaining read alike. A clean file is
// the control.
func TestCTableFallbackRefusalSetsRefused(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, `package probe
table Root { values []int32 }
`)
	wire := func(count uint64) []byte {
		list := binary.AppendUvarint([]byte{byte(ir.TableKindI32)}, count)
		for i := uint64(0); i < count && i < 4; i++ {
			list = binary.LittleEndian.AppendUint32(list, uint32(i+1))
		}
		body := []byte{1, byte(ir.TableKindArray)}
		body = binary.AppendUvarint(body, uint64(len(list)))
		body = append(body, list...)
		body = append(body, 0)
		file := append([]byte{1}, body...)
		file = binary.LittleEndian.AppendUint64(file, ir.TableWireId("values"))
		return binary.LittleEndian.AppendUint64(file, 1)
	}
	octets := func(data []byte) string {
		var s strings.Builder
		for _, b := range data {
			fmt.Fprintf(&s, "0x%02x,", b)
		}
		return s.String()
	}
	clean := wire(2)
	over := wire(uint64(1) << 31)
	runCTableWireProbe(t, u, fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do { if(!(x)) { fprintf(stderr,"line %%d: %%s\n",__LINE__,#x); return 1; } } while(0)
static const uint8_t clean[]={%s}, over[]={%s};
int main(void) {
    uint8_t region[65536] __attribute__((aligned(64)));
    uint8_t retain_bytes[4096];
    TableRetainId retain_ids[64];
    TableRetain store;
    TableReport report;
    const Root * loaded;

    memset(&store,0,sizeof(store));
    store.bytes=retain_bytes; store.capacity=(int64_t)sizeof(retain_bytes);
    store.ids=retain_ids; store.id_capacity=64;
    memset(&report,0,sizeof(report));
    loaded=root_load(region,(int64_t)sizeof(region),clean,(int64_t)sizeof(clean),&report);
    CHECK(loaded!=NULL && !report.refused);
    memset(&report,0,sizeof(report));
    loaded=root_load_retain(region,(int64_t)sizeof(region),clean,(int64_t)sizeof(clean),&store,&report);
    CHECK(loaded!=NULL && !report.refused);

    memset(&report,0,sizeof(report));
    loaded=root_load(region,(int64_t)sizeof(region),over,(int64_t)sizeof(over),&report);
    CHECK(loaded==NULL && report.refused);

    memset(&report,0,sizeof(report));
    memset(&store,0,sizeof(store));
    store.bytes=retain_bytes; store.capacity=(int64_t)sizeof(retain_bytes);
    store.ids=retain_ids; store.id_capacity=64;
    loaded=root_load_retain(region,(int64_t)sizeof(region),over,(int64_t)sizeof(over),&store,&report);
    CHECK(loaded==NULL && report.refused);
    return 0;
}
`, octets(clean), octets(over)))
}
