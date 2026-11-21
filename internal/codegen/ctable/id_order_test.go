package ctable

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestCWireIdOrder(t *testing.T) {
	slowtest.Gate(t, "the C compiler and sanitizers")
	u := unitFrom(t, "package probe\ntable Root { n int32 }\n")
	file := func(body []byte, ids ...uint64) []byte {
		wire := append([]byte{1}, body...)
		for _, id := range ids {
			wire = binary.LittleEndian.AppendUint64(wire, id)
		}
		return binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
	}
	field := ir.TableWireId("future")
	variant := ir.TableWireId("Choice")
	other := ir.TableWireId("unused")
	node := []byte{1, 2, 7, 3, 4, 7, 0, 0, 0, 0}
	cases := []struct {
		name string
		wire []byte
		okay bool
	}{
		{"empty", file([]byte{0}), true},
		{"unused-empty", file([]byte{0}, other), false},
		{"unknown-field", file([]byte{1, 4, 7, 0, 0, 0, 0}, field), true},
		{"unused-after-field", file([]byte{1, 4, 7, 0, 0, 0, 0}, field, other), false},
		{"unknown-enum", file([]byte{1, 30, 2, 0}, field, variant), true},
		{"unused-after-enum", file([]byte{1, 30, 2, 0}, field, variant, other), false},
		{"unknown-union", file([]byte{1, 15, 2, 4, 4, 7, 0, 0, 0, 0}, field, variant), true},
		{"unused-after-union", file([]byte{1, 15, 2, 4, 4, 7, 0, 0, 0, 0}, field, variant, other), false},
		{"unknown-keyed", file([]byte{1, 16, 8, 4, 1, 2, 4, 7, 0, 0, 0, 0}, field, variant), true},
		{"unused-after-keyed", file([]byte{1, 16, 8, 4, 1, 2, 4, 7, 0, 0, 0, 0}, field, variant, other), false},
		{"node-type-and-field", file(append(append([]byte{1, 12, byte(len(node))}, node...), 0), ir.TableNodeWireId, variant, field), true},
		{"unused-after-node", file(append(append([]byte{1, 12, byte(len(node))}, node...), 0), ir.TableNodeWireId, variant, field, other), false},
		{"out-of-order-field", file([]byte{2, 4, 7, 0, 0, 0, 1, 4, 8, 0, 0, 0, 0}, field, other), false},
		{"out-of-order-enum", file([]byte{1, 30, 3, 0}, field, other, variant), false},
	}
	var body strings.Builder
	body.WriteString("static uint8_t region[4096] __attribute__((aligned(64)));\n")
	for _, tc := range cases {
		want := 0
		if tc.okay {
			want = 1
		}
		fmt.Fprintf(&body, "{ const uint8_t wire[]={%s}; TableReader r; TableReport report; memset(&report,0,sizeof(report)); if(table_wire_open(&r,wire,sizeof(wire),&report)!=%d) { fprintf(stderr,\"id-order %s\\n\"); return 1; }", cBytes(tc.wire), want, tc.name)
		if !tc.okay {
			fmt.Fprintf(&body, "{ memset(&report,0,sizeof(report)); if(root_load(region,sizeof(region),wire,sizeof(wire),&report)!=NULL || !report.refused) { fprintf(stderr,\"public id-order %s\\n\"); return 1; } }", tc.name)
		}
		body.WriteString("}\n")
	}
	// Probe the exact wire-only canonicality check shared by plain, graph,
	// builder and retaining entry points, with redzones at both ends of each wire.
	out, err := runCNodeRecordTerminatorProbe(t, u, body.String(), nil)
	if err != nil {
		t.Fatalf("healthy identity walk: %v\n%s", err, out)
	}
	const anchor = "if ( next_ref == UINT64_MAX || (end == order.size && next_ref - 1 != r->id_count) )"
	out, err = runCNodeRecordTerminatorProbe(t, u, body.String(), func(files map[string][]byte) error {
		const name = "ProbeTable.h"
		s := string(files[name])
		if strings.Count(s, anchor) != 1 {
			return fmt.Errorf("id-order control anchor absent or repeated")
		}
		files[name] = []byte(strings.Replace(s, anchor, "if ( end < -1 )", 1))
		return nil
	})
	if err == nil || !cCarries(out, "id-order unused-empty") {
		t.Fatalf("identity control did not fail by name: %v\n%s", err, out)
	}
}
