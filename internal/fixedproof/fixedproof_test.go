package fixedproof

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"path/filepath"
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

func (s stubEvoRunner) SaveCustom(t *testing.T, u *ir.Unit, root string, scenario string) ([]byte, string) {
	eOwn, _ := LineageEntryOf(u, root)
	writingWant := []byte{1, 0, 0, 0, 0x22, 0x11, 0, 0, 0, 0, 1, 0, 0, 0, 'A', 0, 0, 0, 1, 0, 0, 0, 0x42, 0, 0, 0, 1, 7, 0, 0, 0, 0, 0, 0, 0}
	switch scenario {
	case "optional_absent_zeros":
		body := make([]byte, 17)
		binary.LittleEndian.PutUint32(body[0:], 11)
		binary.LittleEndian.PutUint32(body[4:], 22)
		return MakeFixedFile(eOwn.Wire, eOwn.Layout, body), ""
	case "live_extents_base":
		body := make([]byte, 13)
		binary.LittleEndian.PutUint32(body[0:], 1)
		body[4] = 1
		binary.LittleEndian.PutUint32(body[9:], 77)
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
	case "w14_writer_offsets":
		st := RootStruct(t, u, root)
		off := fieldOffsets(st)
		body := make([]byte, ir.TableFixedTypeBytes(st))
		body[off["a"]] = 0xA1
		binary.LittleEndian.PutUint32(body[off["b"]:], 0x0B0B0B0B)
		binary.LittleEndian.PutUint32(body[off["inner"]:], 0x1E1E1E1E)
		binary.LittleEndian.PutUint16(body[off["inner"]+4:], 0x2E2E)
		body[off["opt"]] = 1
		binary.LittleEndian.PutUint32(body[off["opt"]+1:], 0x0D0D0D0D)
		binary.LittleEndian.PutUint32(body[off["arr"]:], 2)
		binary.LittleEndian.PutUint16(body[off["arr"]+4:], 0x3E3E)
		binary.LittleEndian.PutUint16(body[off["arr"]+6:], 0x4E4E)
		binary.LittleEndian.PutUint32(body[off["s"]:], 5)
		copy(body[off["s"]+4:], "seven")
		binary.LittleEndian.PutUint32(body[off["tail"]:], 0x7A7A7A7A)
		return MakeFixedFile(eOwn.Wire, eOwn.Layout, body), ""
	default:
		return nil, "leg_diagnostic_unknown_scenario"
	}
}

func (s stubEvoRunner) LoadLineage(t *testing.T, u *ir.Unit, root string, lineage map[string][]LineageEntry, wireBytes []byte, cap int) (int, string, map[string]string, []byte, LoadCounters) {
	n, reason, fields, resaved := s.loadLineage(t, u, root, lineage, wireBytes, cap)
	var counters LoadCounters
	if n == 2 && root == "Lineage" {
		counters.Widened = 2
	}
	if n == 2 && root == "T" {
		if _, ok := fields["r0.outer.type"]; ok {
			counters.Widened = 1
		}
	}
	return n, reason, fields, resaved, counters
}

func (s stubEvoRunner) CheckLineage(_ *testing.T, u *ir.Unit, root string, lineage map[string][]LineageEntry) string {
	entries := append([]LineageEntry(nil), lineage[root]...)
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
	for i, e := range entries {
		if len(e.Layout) < 4 {
			return fmt.Sprintf("entry %d has no layout", i)
		}
		if count := int(binary.LittleEndian.Uint32(e.Layout[:4])); count > ir.TableFixedLeafCap {
			return fmt.Sprintf("entry %d is past the plan bound", i)
		}
		if !stubValidLayout(e.Layout) {
			return fmt.Sprintf("entry %d layout is malformed", i)
		}
		if e.Record <= 8 || e.Record > ir.TableFixedRecordMaxBytes+8 {
			return fmt.Sprintf("entry %d record is out of bounds", i)
		}
	}
	return ""
}

func (s stubEvoRunner) loadLineage(t *testing.T, u *ir.Unit, root string, lineage map[string][]LineageEntry, wireBytes []byte, cap int) (int, string, map[string]string, []byte) {
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
	if int(binary.LittleEndian.Uint32(layout[:4])) > ir.TableFixedLeafCap {
		return -1, "leg_diagnostic_plan_too_large", nil, nil
	}
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
	if root == "T" && recSize == 8+17 {
		fields["r0.plain.a"] = "11"
		fields["r0.plain.b"] = "22"
		fields["r0.opt_present"] = "false"
	}
	if root == "T" && recSize == 8+13 {
		fields["r0.arr_count"] = "1"
		fields["r0.arr[0]"] = "1"
		fields["r0.opt_present"] = "false"
		fields["r0.tail"] = "77"
	}
	if root == "T" && recSize == 8+10 {
		fields["r0.outer.type"] = "1"
		fields["r0.outer.inner.type"] = "1"
		fields["r0.outer.inner.x"] = "-7"
		fields["r0.tail"] = "100"
		fields["r1.outer.type"] = "2"
		fields["r1.outer.plain"] = "16909064"
		fields["r1.tail"] = "200"
	}
	if root == "T" && recSize == 8+24 {
		fields["r0.head"] = "42"
		fields["r0.opt"] = "-99"
		fields["r1.opt"] = "-99"
		fields["r0.arr_count"] = "1"
		fields["r0.arr[0]"] = "2"
		fields["r0.ch.type"] = "2"
		fields["r0.ch.s"] = `"hi"`
		fields["r1.ch.s"] = `"hi"`
	}
	if root == "T" && recSize == 8+4 {
		fields["r0.x"] = "5"
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
	if _, err := os.Stat("../../build/fixedform-corpus/manifest.txt"); err == nil {
		manifest := readFixedManifest(t, "../../build/fixedform-corpus")
		for file, ml := range manifest {
			if data, err := os.ReadFile(filepath.Join("../../build/fixedform-corpus", file)); err == nil && bytes.Equal(data, wireBytes) {
				maps.Copy(fields, ml.values)
				if file == "old_rename_without_was.bin" {
					fields["r0.b"] = ml.values["r0.a"]
				}
				break
			}
		}
	}
	resaved := wireBytes
	if eOwn, ok := LineageEntryOf(u, root); ok && hash != eOwn.Wire {
		bodies := make([][]byte, 0, nRec)
		for k := range nRec {
			at := 20 + layoutLen + k*recSize
			bodies = append(bodies, bytes.Clone(wireBytes[at+8:at+recSize]))
		}
		resaved = MakeFixedFile(eOwn.Wire, eOwn.Layout, bodies...)
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

// checkMemoryCeilingOverlap proves that an inner ceiling scope keeps the
// process bounded after an overlapping outer scope releases, and that the
// original limit returns only when the last scope exits.
func checkMemoryCeilingOverlap(t *testing.T) {
	t.Helper()
	before, ok := addressSpaceLimit()
	if !ok {
		return
	}
	releaseOuter := acquireMemoryCeiling()
	releaseInner := acquireMemoryCeiling()
	releaseOuter()
	active, ok := addressSpaceLimit()
	if !ok {
		releaseInner()
		t.Fatal("cannot read RLIMIT_AS with an inner ceiling active")
	}
	if active > uint64(HardMemoryLimitBytes) {
		releaseInner()
		t.Fatalf("overlapping ceiling: after outer release with inner active RLIMIT_AS=%d, want <= %d", active, uint64(HardMemoryLimitBytes))
	}
	releaseInner()
	restored, ok := addressSpaceLimit()
	if !ok {
		t.Fatal("cannot read RLIMIT_AS after the ceiling released")
	}
	if restored != before {
		t.Fatalf("ceiling restored RLIMIT_AS to %d, want original %d", restored, before)
	}
}

// TestEvolutionSuiteVerdictAndCallerCapacity drives the shared evolution,
// layout, plan, lineage and file rows against a stub runner: no process, no
// socket, no clock, and no target interpreter.
func TestEvolutionSuiteVerdictAndCallerCapacity(t *testing.T) {
	t.Parallel()
	checkMemoryCeilingOverlap(t)
	RunEvolutionSuite(t, "../..", stubEvoRunner{})
}
