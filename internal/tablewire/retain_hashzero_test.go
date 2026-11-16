package tablewire_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The retained representation must distinguish a resolved hash-zero ID from
// an actual reference-zero None/terminator. Every case uses hash zero inside
// an unknown field, not merely as that field's outer identity.
func TestRetainNestedHashZero(t *testing.T) {
	m := retainModel(t, "RT1.schema")
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"enum", []byte{1, 30, 2, 0}},
		{"union", []byte{1, 15, 2, 6, 1, 7, 0}},
		{"body-field", []byte{1, 13, 4, 2, 6, 7, 0, 0}},
		{"keyed-slot", []byte{1, 16, 5, 6, 1, 2, 1, 7, 0}},
		{"enum-array-and-None", []byte{1, 14, 4, 30, 2, 2, 0, 0}},
		{"union-array-and-None", []byte{1, 14, 7, 15, 2, 2, 6, 1, 7, 0, 0}},
	} {
		for _, zero := range []bool{false, true} {
			name := tc.name + "/nonzero-control"
			id := ir.TableWireId("future_inner_hash")
			if zero {
				name = tc.name + "/hash-zero"
				id = 0
			}
			t.Run(name, func(t *testing.T) {
				wire := append([]byte{1}, tc.body...)
				wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId("future_outer_hash"))
				wire = binary.LittleEndian.AppendUint64(wire, id)
				wire = binary.LittleEndian.AppendUint64(wire, 2)
				for _, region := range []bool{false, true} {
					for pass := range 2 {
						inst := m.New(m.Lookup("Node"))
						rt := &tablewire.Retain{Capacity: 4096, IdCapacity: 32}
						var r tabletext.Report
						var ok bool
						var err error
						// A plain load must agree, without learning the unknown identity.
						if region {
							ok, err = tablewire.DecodeRegion(m, inst, wire, &r)
						} else {
							ok, err = tablewire.Decode(m, inst, wire, &r)
						}
						if !ok || err != nil || r.Unknown != 1 || r.Refused {
							t.Fatalf("plain region=%v: %v %v %+v", region, ok, err, r)
						}
						r = tabletext.Report{}
						if region {
							ok, err = tablewire.DecodeRegionRetain(m, inst, wire, rt, &r)
						} else {
							ok, err = tablewire.DecodeRetain(m, inst, wire, rt, &r)
						}
						if !ok || err != nil || r.Unknown != 1 || r.Retained != 1 || r.RetainLost != 0 || r.Refused {
							t.Fatalf("retain pass=%d region=%v: %v %v %+v", pass, region, ok, err, r)
						}
						var sr tabletext.Report
						saved, err := tablewire.EncodeRetain(m, inst, rt, &sr)
						if err != nil || sr.RetainLost != 0 || !bytes.Equal(saved, wire) {
							t.Fatalf("save pass=%d region=%v: %x want %x %v %+v", pass, region, saved, wire, err, sr)
						}
						// Re-read and retain the first save too; same bytes must be a fixed point.
						wire = saved
					}
				}
			})
		}
	}
}
