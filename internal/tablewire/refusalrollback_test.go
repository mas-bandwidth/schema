package tablewire_test

import (
	"math"
	"reflect"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A late refusal must not publish earlier fields or retained unknowns.
// Defaults are deliberately nonzero so clearing bytes cannot satisfy the test.
func TestNonFiniteRefusalDiscardsDecodedState(t *testing.T) {
	writer := listModel(t, "package rollback\ntable Root\n{\n x int32 = 9\n extra int32\n f float64\n}\n")
	reader := listModel(t, "package rollback\ntable Root\n{\n x int32 = 9\n f float64\n}\n")
	source := place(t, writer, "Root", `{"x":7,"extra":11,"f":1.25}`)
	wire, err := tablewire.Encode(writer, source)
	if err != nil {
		t.Fatal(err)
	}
	bad := replaced(t, wire, le64(math.Float64bits(1.25)), le64(0x7ff0000000000000))
	for _, mode := range []string{"plain", "region", "retain", "region-retain"} {
		t.Run(mode, func(t *testing.T) {
			inst := reader.New(reader.Lookup("Root"))
			retained := tablewire.Retain{Capacity: 8192, IdCapacity: 128}
			decode := func(data []byte, report *tabletext.Report) (bool, error) {
				switch mode {
				case "region":
					return tablewire.DecodeRegion(reader, inst, data, report)
				case "retain":
					return tablewire.DecodeRetain(reader, inst, data, &retained, report)
				case "region-retain":
					return tablewire.DecodeRegionRetain(reader, inst, data, &retained, report)
				default:
					return tablewire.Decode(reader, inst, data, report)
				}
			}
			var good tabletext.Report
			if ok, err := decode(wire, &good); !ok || err != nil || good.Malformed || oracleField(t, inst, "x").Cell.I != 7 {
				t.Fatalf("valid control: ok=%v err=%v report=%+v", ok, err, good)
			}
			if (mode == "retain" || mode == "region-retain") && retained.Used() == 0 {
				t.Fatal("valid control retained nothing")
			}
			// An existing report flag must neither hide this refusal nor be erased.
			report := tabletext.Report{Malformed: true}
			ok, err := decode(bad, &report)
			if ok {
				t.Fatal("accepted a nonfinite float")
			}
			refusedAs(t, mode, err, report, "Root.f")
			if !report.Malformed {
				t.Fatal("erased the previous read's malformed flag")
			}
			if !reflect.DeepEqual(inst, reader.New(inst.Def)) {
				t.Error("refusal left decoded fields instead of declared defaults")
			}
			if retained.Used() != 0 || len(retained.Ids()) != 0 {
				t.Errorf("refusal left retained state: used=%d ids=%d", retained.Used(), len(retained.Ids()))
			}
			var saved tabletext.Report
			got, err := tablewire.EncodeRetain(reader, inst, &retained, &saved)
			want, wantErr := tablewire.Encode(reader, reader.New(inst.Def))
			if err != nil || wantErr != nil || !reflect.DeepEqual(got, want) {
				t.Errorf("refused state re-saved decoded or retained data: err=%v wantErr=%v", err, wantErr)
			}
		})
	}
}

func TestNonFiniteBatchRefusalDiscardsEarlierBodies(t *testing.T) {
	m := listModel(t, "package rollback\nfixed table Root\n{\n x int32 = 9\n f float64\n}\n")
	first := place(t, m, "Root", `{"x":7,"f":1.25}`)
	second := place(t, m, "Root", `{"x":8,"f":2.5}`)
	wire, err := tablewire.EncodeMessages(m, []*tabletext.Instance{first, second})
	if err != nil {
		t.Fatal(err)
	}
	var vocabulary tablewire.Vocabulary
	var announcement tabletext.Report
	if err := vocabulary.AnnounceRead(tablewire.Announce(m.Unit), &announcement); err != nil {
		t.Fatal(err)
	}
	out := []*tabletext.Instance{m.New(first.Def), m.New(first.Def)}
	var good tabletext.Report
	if n, ok, err := tablewire.DecodeMessages(m, out, wire, &vocabulary, &good); n != 2 || !ok || err != nil || !reflect.DeepEqual(out, []*tabletext.Instance{first, second}) {
		t.Fatalf("valid batch: n=%d ok=%v err=%v", n, ok, err)
	}
	bad := replacedBits(t, wire, math.Float64bits(2.5), 0x7ff0000000000000, 64)
	var report tabletext.Report
	n, ok, err := tablewire.DecodeMessages(m, out, bad, &vocabulary, &report)
	if n != 0 || ok {
		t.Fatalf("refused batch delivered %d bodies, ok=%v", n, ok)
	}
	refusedAs(t, "batch", err, report, "Root.f")
	for i, inst := range out {
		if !reflect.DeepEqual(inst, m.New(inst.Def)) {
			t.Errorf("refusal left body %d decoded instead of its declared defaults", i)
		}
	}
}
