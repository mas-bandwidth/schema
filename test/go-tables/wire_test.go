package schematables

import (
	"encoding/binary"
	"hash/fnv"
	"testing"

	"tblv1"
)

func TestMalformedRepeatedUnionKeepsPriorValue(t *testing.T) {
	// A complete ward, then a repeated effect whose arm length exceeds the
	// remaining root body. The reference stops before replacing the old arm.
	wire := []byte{1, 1, 15, 2, 13, 7, 3, 10, 0, 0, 0x40, 0x3f, 0,
		1, 15, 2, 13, 7, 3, 10, 0}
	for _, name := range []string{"effect", "ward", "charge"} {
		h := fnv.New64a()
		h.Write([]byte(name))
		wire = binary.LittleEndian.AppendUint64(wire, h.Sum64())
	}
	wire = binary.LittleEndian.AppendUint64(wire, 3)
	var value tblv1.Cfg
	var report tblv1.TableReport
	if tblv1.CfgLoad(&value, wire, &report) || report.Verdict != tblv1.TableOpenBodyStopped || !report.Malformed {
		t.Fatalf("expected a stopped body: %+v", report)
	}
	if value.Effect.Type != tblv1.EffectTypeWard || value.Effect.Ward.Charge != 0.75 {
		t.Fatalf("damaged repeat replaced the previous arm: %+v", value.Effect)
	}
}
