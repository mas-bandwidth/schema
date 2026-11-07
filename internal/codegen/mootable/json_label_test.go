package mootable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestMooJsonNodeLabelNegativeControl is the reversed witness of the `&node`
// label (docs/PORTING.md M14; docs/SPEC-TABLES.md §16.7), held by
// TestMooJsonWriteAndReadGates over the graphdemo, listdemo and mapdemo
// instances that share a node. Two plants in json.moo, each required to turn
// the gate on its own side and on a shared-node instance:
//
//	writer  a node two pointers name is written in full at each appearance,
//	        never labeled: json-write red on graph_shared
//	reader  a label's reference resolves to null instead of the node it
//	        defined: json-read red on graph_shared
func TestMooJsonNodeLabelNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs a sabotaged json.moo)")
	t.Parallel()
	src := string(Runtime()["json.moo"])
	for _, c := range []struct {
		side, from, to string
	}{
		{"writer", "\tif (w.count[node] or 0) <= 1 then\n", "\tif true then\n"},
		{"reader", "\t\t\tholder[key] = e.node\n", "\t\t\tholder[key] = null\n"},
	} {
		if strings.Count(src, c.from) != 1 {
			t.Fatalf("%s: json.moo carries %q %d times, want 1: the control would be vacuous", c.side, c.from, strings.Count(src, c.from))
		}
		broken := strings.Replace(src, c.from, c.to, 1)
		writeBad, readBad, total := jsonGates(t, map[string][]byte{"json.moo": []byte(broken)})
		bad := writeBad
		if c.side == "reader" {
			bad = readBad
		}
		hit := false
		for _, b := range bad {
			if strings.HasPrefix(b, "graph_shared ") {
				hit = true
			}
		}
		if !hit {
			t.Errorf("%s plant: graph_shared stayed green on the %s gate over %d instances: the gate cannot see the label", c.side, c.side, total)
			continue
		}
		t.Logf("%s plant: %d of %d instances red on the %s gate, graph_shared among them", c.side, len(bad), total, c.side)
	}
}
