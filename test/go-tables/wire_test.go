package schematables

import (
	"encoding/binary"
	"hash/fnv"
	"testing"

	"tblv1"
)

func TestDamagedRepeatedUnionRefuses(t *testing.T) {
	// A complete ward, then a repeated effect whose arm length exceeds the
	// remaining root body: the read refuses the whole file (docs/SPEC-TABLES.md
	// §4, §6.5), and the same bytes without the repeat read.
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
	if tblv1.CfgLoad(&value, wire, &report) || report.Err() == nil {
		t.Fatalf("damaged repeat read: %+v", report)
	}
	// negative control: the same ward written by the generated save reads
	value = tblv1.Cfg{}
	value.Effect.Type = tblv1.EffectTypeWard
	value.Effect.Ward.Charge = 0.75
	buffer := make([]byte, 4096)
	n := tblv1.CfgSave(&value, buffer)
	var back tblv1.Cfg
	report = tblv1.TableReport{}
	if n <= 0 || !tblv1.CfgLoad(&back, buffer[:n], &report) || back.Effect.Type != tblv1.EffectTypeWard || back.Effect.Ward.Charge != 0.75 {
		t.Fatalf("the saved ward: %d %+v %+v", n, report, back.Effect)
	}
}
