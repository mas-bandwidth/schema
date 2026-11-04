package fixedproof

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

type stubEvoRunner struct {
	ignoreCap bool
}

func (s stubEvoRunner) Name() string { return "stub" }

func (s stubEvoRunner) HasFixedRoot(_ *testing.T, u *ir.Unit, root string) bool {
	_, ok := LineageEntryOf(u, root)
	return ok
}

func (s stubEvoRunner) SaveCustom(_ *testing.T, u *ir.Unit, root string, scenario string) ([]byte, string) {
	switch scenario {
	case "optional_absent_zeros":
		return make([]byte, 45), ""
	case "live_extents_base":
		eOwn, _ := LineageEntryOf(u, root)
		body := make([]byte, 13)
		binary.LittleEndian.PutUint32(body[0:], 1)
		return MakeFixedFile(eOwn.Wire, eOwn.Layout, body), ""
	case "writing_slack_zeros":
		return make([]byte, 64), ""
	default:
		return nil, "leg_diagnostic_unknown_scenario"
	}
}

func (s stubEvoRunner) LoadLineage(_ *testing.T, u *ir.Unit, root string, lineage map[string][]LineageEntry, wireBytes []byte, cap int) (int, string, map[string]string, []byte) {
	if len(wireBytes) < 20 || wireBytes[0] != 3 {
		return -1, "leg_diagnostic_envelope", nil, nil
	}
	for i := 1; i < 8; i++ {
		if wireBytes[i] != 0 {
			return -1, "leg_diagnostic_reserved", nil, nil
		}
	}
	hash := binary.LittleEndian.Uint64(wireBytes[8:16])
	layoutLen := int(binary.LittleEndian.Uint32(wireBytes[16:20]))
	if layoutLen < 4 || 20+layoutLen > len(wireBytes) {
		return -1, "leg_diagnostic_layout_len", nil, nil
	}
	layout := wireBytes[20 : 20+layoutLen]

	var entries []LineageEntry
	if lineage != nil {
		entries = append(entries, lineage[root]...)
	}
	if eOwn, ok := LineageEntryOf(u, root); ok {
		hasOwn := false
		for _, e := range entries {
			if e.Wire == eOwn.Wire {
				hasOwn = true
				break
			}
		}
		if !hasOwn {
			entries = append(entries, eOwn)
		}
	}

	floor := -1
	for i, e := range entries {
		if e.Retired {
			floor = i
		}
	}
	pick := -1
	for i, e := range entries {
		if e.Wire == hash {
			pick = i
			break
		}
	}
	if pick < 0 {
		return -1, "leg_diagnostic_unknown_hash", nil, nil
	}
	if pick <= floor {
		return -1, "leg_diagnostic_retired", nil, nil
	}
	entry := entries[pick]
	if !bytes.Equal(layout, entry.Layout) {
		return -1, "leg_diagnostic_layout_mismatch", nil, nil
	}
	if !stubValidLayout(layout) {
		return -1, "leg_diagnostic_bad_tree", nil, nil
	}

	recSize := int(entry.Record)
	rest := len(wireBytes) - 20 - layoutLen
	if recSize <= 8 || rest%recSize != 0 {
		return -1, "leg_diagnostic_ragged", nil, nil
	}
	nRec := rest / recSize
	if !s.ignoreCap && cap >= 0 && nRec > cap {
		return -1, "leg_diagnostic_caller_cap", nil, nil
	}
	for k := range nRec {
		at := 20 + layoutLen + k*recSize
		if binary.LittleEndian.Uint64(wireBytes[at:at+8]) != hash {
			return -1, "leg_diagnostic_rec_hash", nil, nil
		}
	}

	fields := map[string]string{
		"r0.w":           "77",
		"r0.a":           "7",
		"r0.b":           "10",
		"r0.c":           "99",
		"r1.a":           "8",
		"r1.b":           "30",
		"r0.opt_present": "true",
		"r0.opt.b":       "4",
		"r0.tail":        "77",
		"r0.u.nested.y":  "-7",
	}
	if root == "Lineage" && recSize == 8+8 && nRec == 2 {
		fields["r0.a"] = "100"
		fields["r0.b"] = "-20"
		fields["r0.c"] = "99"
		fields["r1.b"] = "30"
	}
	if root == "T" && recSize == 8+13 {
		fields["r0.opt_present"] = "false"
		fields["r0.tail"] = "77"
	}
	if root == "T" && recSize == 8+10 {
		fields["r0.tail"] = "42"
	}
	resaved := wireBytes
	if root == "T" && recSize == 8+18 {
		resaved = []byte("canonical-plan-execution-bytes")
	}
	return nRec, "leg_diagnostic_ok", fields, resaved
}

func stubValidLayout(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	count := int(binary.LittleEndian.Uint32(b[:4]))
	if count <= 0 || 4+count*17 != len(b) {
		return false
	}
	rootKind := b[4+8]
	rootSize := int64(binary.LittleEndian.Uint32(b[4+9 : 4+13]))
	if rootKind != ir.TableKindTable || rootSize <= 0 || rootSize > ir.TableFixedRecordMaxBytes {
		return false
	}
	for i := range count {
		at := 4 + i*17
		k := b[at+8]
		sz := int64(binary.LittleEndian.Uint32(b[at+9 : at+13]))
		validKind := (k >= 1 && k <= 16) || (k >= 18 && k <= 30) || k == 32 || k == 33 || k == 35 || k == 36
		if !validKind || sz < 0 || sz > ir.TableFixedRecordMaxBytes {
			return false
		}
	}
	return true
}

func TestEvolutionSuiteVerdictAndCallerCapacity(t *testing.T) {
	RunEvolutionSuite(t, "../..", stubEvoRunner{})
}
