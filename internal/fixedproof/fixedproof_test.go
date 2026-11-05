package fixedproof

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// stubEvoRunner is a target-neutral runner for the evolution suite: it speaks
// the Form 3 envelope and the lineage entries, and never runs the C++ oracle,
// a target interpreter or a clock.
type stubEvoRunner struct {
	ignoreCap bool
}

func (s stubEvoRunner) Name() string { return "stub" }

func (s stubEvoRunner) HasFixedRoot(_ *testing.T, u *ir.Unit, root string) bool {
	_, ok := LineageEntryOf(u, root)
	return ok
}

func (s stubEvoRunner) SaveCustom(_ *testing.T, u *ir.Unit, root string, scenario string) ([]byte, string) {
	eOwn, _ := LineageEntryOf(u, root)
	writingWant := []byte{1, 0, 0, 0, 0x22, 0x11, 0, 0, 0, 0, 1, 0, 0, 0, 'A', 0, 0, 0, 1, 0, 0, 0, 0x42, 0, 0, 0, 1, 7, 0, 0, 0, 0, 0, 0, 0}
	switch scenario {
	case "optional_absent_zeros":
		return make([]byte, 45), ""
	case "live_extents_base":
		body := make([]byte, 13)
		binary.LittleEndian.PutUint32(body[0:], 1)
		return MakeFixedFile(eOwn.Wire, eOwn.Layout, body), ""
	case "writing_slack_batch":
		return MakeFixedFile(eOwn.Wire, eOwn.Layout, make([]byte, 35), writingWant), ""
	case "writing_overcount":
		return nil, "leg_diagnostic_invalid_value"
	case "writing_slack_unzeroed":
		unzeroed := bytes.Clone(writingWant)
		unzeroed[6] = 0xaa
		return MakeFixedFile(eOwn.Wire, eOwn.Layout, make([]byte, 35), unzeroed), ""
	case "closure_small_42":
		body := make([]byte, 4)
		binary.LittleEndian.PutUint32(body, 42)
		return MakeFixedFile(eOwn.Wire, eOwn.Layout, body), ""
	case "outer_nested":
		return MakeFixedFile(eOwn.Wire, eOwn.Layout, make([]byte, 57)), ""
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
	if root == "Target" {
		return -1, "leg_diagnostic_record_too_large", nil, nil
	}

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
	if root == "Small" && recSize == 8+4 {
		fields["r0.x"] = "42"
	}
	if root == "Boundary" && recSize == 8+16 {
		fields["r0.name"] = `"edgecase"`
	}
	if root == "Outer" && recSize == 8+57 {
		fields["r0.head"] = "7"
		fields["r0.inner.a"] = "11"
		fields["r0.inner.b"] = `"one"`
		fields["r0.items[1].a"] = "33"
		fields["r0.items[1].b"] = `"six"`
		fields["r0.items[0].b"] = `"hi"`
		fields["r0.maybe_present"] = "true"
		fields["r0.maybe.a"] = "44"
		fields["r0.tail"] = "9"
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

// TestEvolutionSuiteVerdictAndCallerCapacity drives the shared evolution,
// layout, plan, lineage and file rows against a stub runner: no process, no
// socket, no clock, and no target interpreter.
func TestEvolutionSuiteVerdictAndCallerCapacity(t *testing.T) {
	t.Parallel()
	RunEvolutionSuite(t, "../..", stubEvoRunner{})
}
