package ctable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const cNodeTableLastValidSchema = `package validnodes
 table Item { id int32 }
 table Crews {
  members map[uint32][..2]*Item
  past int32
 }
`

// Ordinary encoder output exercises an absent numbering, null slots, an
// all-default node, and live arrays at their declared boundary. No input is
// forged or replayed: both region and builder APIs hold their canonical bytes.
func TestCNodeTableLastValidRoundTrips(t *testing.T) {
	slowtest.Gate(t, "the C compiler: valid root node-table boundary")
	u := unitFrom(t, cNodeTableLastValidSchema)
	m := tabletext.NewModel(u)
	var body strings.Builder
	for _, tc := range []struct {
		name, text string
		nodes      int
	}{
		{"empty", `{}`, 0},
		{"null-slot", `{"members":{"3":[null]},"past":11}`, 0},
		{"default-node", `{"members":{"3":[{"id":0}]},"past":11}`, 1},
		{"map-and-array-boundary", `{"members":{"3":[{"id":7},{"id":8}],"9":[null,{"id":10}]},"past":12}`, 3},
	} {
		value := m.New(m.Lookup("Crews"))
		var report tabletext.Report
		if !m.Read(value, []byte(tc.text), &report) {
			t.Fatalf("ordinary value %s: %+v", tc.name, report)
		}
		wire, err := tablewire.Encode(m, value)
		if err != nil {
			t.Fatal(err)
		}
		var decoded tabletext.Report
		if ok, err := tablewire.Decode(m, m.New(m.Lookup("Crews")), wire, &decoded); !ok || err != nil {
			t.Fatalf("valid oracle roundtrip %s: %v %+v", tc.name, err, decoded)
		}
		fmt.Fprintf(&body, `{
    const uint8_t wire[] = { %s };
    TableReport report; TableReader reader; TableNodeScan scan; TableReader record; uint64_t type_id, records=0;
    int64_t need=crews_load_measure(wire,sizeof(wire));
    if(need<1){printf("measure %s failed\n");return 1;}
    memset(&report,0,sizeof(report));
    if(!table_wire_open(&reader,wire,sizeof(wire),&report)){printf("open %s failed\n");return 1;}
    scan=table_node_scan_begin(&reader);
    while(table_node_scan_next(&scan,&type_id,&record)){records++;}
    if(!table_node_scan_whole(&scan)||records!=%d||scan.present!=%d){printf("scan %s failed\n");return 1;}
    uint8_t *region=(uint8_t*)calloc(1,(size_t)need);
    if(region==NULL){return 1;}
    const Crews *got=crews_load(region,need,wire,sizeof(wire),&report);
    if(got==NULL||report.refused||report.unknown||report.widened){free(region);printf("region %s failed\n");return 1;}
    uint8_t saved[4096]; int64_t written=crews_save(NULL,got,saved,sizeof(saved));
    if(written!=(int64_t)sizeof(wire)||memcmp(saved,wire,sizeof(wire))){free(region);printf("region save %s changed bytes\n");return 1;}
    free(region);
    CrewsBuilder builder;
    if(!crews_builder_init(&builder)){return 1;}
    memset(&report,0,sizeof(report));
    if(!crews_load_builder(&builder,wire,sizeof(wire),&report)||report.refused||report.unknown||report.widened){crews_builder_shutdown(&builder);printf("builder %s failed\n");return 1;}
    TableCtx ctx;memset(&ctx,0,sizeof(ctx));ctx.arena=&builder.arena;
    written=crews_save(&ctx,crews_builder_root(&builder),saved,sizeof(saved));
    crews_builder_shutdown(&builder);
    if(written!=(int64_t)sizeof(wire)||memcmp(saved,wire,sizeof(wire))){printf("builder save %s changed bytes\n");return 1;}
}
`, cLastValidBytes(wire), tc.name, tc.name, tc.nodes, boolInt(tc.nodes > 0), tc.name, tc.name, tc.name, tc.name, tc.name)
	}
	out, err := runCNodeRecordTerminatorProbe(t, u, body.String(), nil)
	if err != nil {
		t.Fatalf("ordinary node-table roundtrips: %v\n%s", err, out)
	}
}

func cLastValidBytes(wire []byte) string {
	parts := make([]string, len(wire))
	for i, b := range wire {
		parts[i] = fmt.Sprintf("0x%02x", b)
	}
	return strings.Join(parts, ",")
}
