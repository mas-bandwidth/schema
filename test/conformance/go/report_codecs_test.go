package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestReportCodecsReadCompleteHistoricalValues(t *testing.T) {
	for _, tc := range []struct{ unit, root, wire, text, bad string }{
		{"reportmessages", "ToolMessage", "batch_unknown_arm", `{"history":[{}]}`, "batch_elem_kind"},
		{"reportarms", "Root", "a1_arm_moved_width", `{"id":11,"value":{"a":7},"tail":22}`, "a1_arm_as_string"},
		{"reportscalars", "SimState", "field_scalar_widen", `{"angle":9}`, "scalars_full"},
		{"reportkeyed1", "Root", "", `{"grade":"Gold","raw":3}`, "k2_as_k1"},
		{"reportkeyed2", "Root", "", `{"grade":3,"raw":"Gold"}`, "k1_as_k2"},
	} {
		t.Run(tc.unit, func(t *testing.T) {
			c := findCodec(tc.unit, tc.root)
			if c == nil {
				t.Fatal("report codec missing")
			}
			save := func(value any) []byte {
				t.Helper()
				n := c.measure(value)
				if n <= 0 {
					t.Fatalf("accepted value measure=%d", n)
				}
				wire := make([]byte, n)
				if got := c.save(value, wire); got != n {
					t.Fatalf("accepted value save=%d, measured=%d", got, n)
				}
				return wire
			}
			want := c.fresh()
			if !c.fromJson(want, []byte(tc.text), &report{}) {
				t.Fatal("valid fixture JSON refused")
			}
			wantWire := save(want)
			wire := wantWire
			var err error
			if tc.wire != "" {
				wire, err = os.ReadFile(filepath.Join("../../../testdata/wire/tables", tc.wire+".bin"))
				if err != nil {
					t.Fatal(err)
				}
			}
			got := c.fresh()
			if !c.load(got, wire, &report{}) {
				t.Fatal("valid historical body refused")
			}
			if actual := save(got); !bytes.Equal(actual, wantWire) {
				t.Fatalf("accepted body lost values: got %x, want %x", actual, wantWire)
			}
			bad, err := os.ReadFile(filepath.Join("../../../testdata/wire/tables", tc.bad+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			if c.load(got, bad, &report{}) {
				t.Fatal("invalid body accepted")
			}
		})
	}
}
