package gotable

import (
	"os"
	"testing"
)

func TestGoWireBoolPayloadNot0Or1(t *testing.T) {
	t.Parallel()
	schema, err := os.ReadFile("../../../tables/examples/Guarded.schema")
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, string(schema), `package tabledemo

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"
	"unsafe"
)

func aligned(t *testing.T, wire []byte) []byte {
	t.Helper()
	size := PatrolLoadMeasure(wire)
	if size < 0 {
		return nil
	}
	raw := make([]byte, size+64)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
	return raw[off : off+uintptr(size)]
}

func boolPayloadOffset(wire []byte, wantRef byte) int {
	i := 1
	for i < len(wire) {
		ref := wire[i]
		i++
		if ref == 0 || i >= len(wire) {
			return -1
		}
		kind := wire[i]
		i++
		if ref == wantRef && kind == 1 {
			return i
		}
		switch kind {
		case 1:
			i++
		case 10:
			i += 4
		case 4:
			i += 4
		default:
			return -1
		}
		if i > len(wire) {
			return -1
		}
	}
	return -1
}

func TestGoWireBoolPayloadNot0Or1(t *testing.T) {
	t.Parallel()
	repro, err := hex.DecodeString("010101fe020a0000c03f0301010404e8030000006f0c6f030b798065400e20a08a498122bdcfaa557f0e7f245050a215c09abcb70400000000000000")
	if err != nil {
		t.Fatal(err)
	}
	clean, err := hex.DecodeString("01010101020a0000c03f0301010404e8030000006f0c6f030b798065400e20a08a498122bdcfaa557f0e7f245050a215c09abcb70400000000000000")
	if err != nil {
		t.Fatal(err)
	}
	activeOffset := boolPayloadOffset(clean, 1)
	targetOffset := boolPayloadOffset(clean, 3)
	if activeOffset < 0 || targetOffset < 0 || clean[activeOffset] != 1 || clean[targetOffset] != 1 {
		t.Fatalf("fixture bool payloads changed: active=%d target=%d wire=%x", activeOffset, targetOffset, clean)
	}
	if activeOffset >= len(repro) || targetOffset >= len(repro) {
		t.Fatalf("payload offsets active=%d target=%d exceed wire length %d", activeOffset, targetOffset, len(repro))
	}
	activeRepro := append([]byte(nil), clean...)
	activeRepro[activeOffset] = 0xfe
	if !bytes.Equal(activeRepro, repro) {
		t.Fatal("Patrol fuzz repro does not match the clean wire with Active set to 0xfe")
	}

	assertMalformed := func(field string, offset int, bad []byte) {
		t.Helper()
		if offset < 0 || offset >= len(bad) || bad[offset] != 0xfe {
			t.Fatalf("bad %s payload not set at %d: %x", field, offset, bad)
		}
		buf := aligned(t, bad)
		if buf == nil {
			t.Fatal("malformed wire has no load region")
		}
		var report TableReport
		// a bool payload other than 0 or 1 is damage, and damage discards the
		// whole load (docs/SPEC-TABLES.md §4): the tolerant read hands back nothing
		if got := PatrolLoadTolerant(buf, bad, &report); got != nil {
			t.Fatalf("tolerant read of malformed %s handed back a value: %+v", field, report)
		}
		if !report.Malformed {
			t.Fatalf("%s did not set malformed", field)
		}
		if report.Reason != "malformed: "+field {
			t.Fatalf("Reason=%q want %q", report.Reason, "malformed: "+field)
		}
		if _, err := PatrolReadStrict(bad); !errors.Is(err, TableRefuseMalformed) {
			t.Fatalf("strict read did not refuse malformed %s: %v", field, err)
		}
	}

	assertMalformed("Active", activeOffset, repro)
	badTarget := append([]byte(nil), clean...)
	badTarget[targetOffset] = 0xfe
	assertMalformed("HasTarget", targetOffset, badTarget)

	buf := aligned(t, clean)
	if buf == nil {
		t.Fatal("clean wire has no load region")
	}
	var report TableReport
	got := PatrolLoadTolerant(buf, clean, &report)
	if got == nil || report != (TableReport{}) {
		t.Fatalf("0x01 wire did not read clean: value=%+v report=%+v", got, report)
	}
	if !got.Active || !got.HasTarget {
		t.Fatalf("0x01 bool payloads read false: %+v", got)
	}
	strict, err := PatrolReadStrict(clean)
	if err != nil {
		t.Fatalf("clean strict read: %v", err)
	}
	reencoded := make([]byte, PatrolMeasure(strict))
	if PatrolSave(strict, reencoded) != int64(len(reencoded)) || !bytes.Equal(reencoded, clean) {
		t.Fatal("clean 0x01 wire did not re-encode byte for byte")
	}

	for _, active := range []bool{false, true} {
		for _, hasTarget := range []bool{false, true} {
			if !active && hasTarget {
				continue
			}
			var source Patrol
			PatrolReset(&source)
			source.Active = active
			source.HasTarget = hasTarget
			if active {
				source.Speed = 1.5
			}
			wire := make([]byte, PatrolMeasure(&source))
			if len(wire) == 0 || PatrolSave(&source, wire) != int64(len(wire)) {
				t.Fatalf("save active=%t has_target=%t failed", active, hasTarget)
			}
			wantActive := byte(0)
			if active {
				wantActive = 1
			}
			if activeOffset >= len(wire) || wire[activeOffset] != wantActive {
				t.Fatalf("writer active=%t emitted wrong payload: wire=%x", active, wire)
			}
			if wire[activeOffset] > 1 {
				t.Fatalf("writer emitted invalid Active payload 0x%02x", wire[activeOffset])
			}
			if active {
				wireTargetOffset := boolPayloadOffset(wire, 3)
				if hasTarget && (wireTargetOffset < 0 || wire[wireTargetOffset] != 1) {
					t.Fatalf("writer has_target=true omitted or changed its payload: wire=%x", wire)
				}
				if !hasTarget && wireTargetOffset >= 0 && wire[wireTargetOffset] != 0 {
					t.Fatalf("writer has_target=false emitted payload 0x%02x", wire[wireTargetOffset])
				}
				if wireTargetOffset >= 0 && wire[wireTargetOffset] > 1 {
					t.Fatalf("writer emitted invalid HasTarget payload 0x%02x", wire[wireTargetOffset])
				}
			}

			buf := aligned(t, wire)
			if buf == nil {
				t.Fatalf("valid writer wire has no load region: %x", wire)
			}
			report := TableReport{}
			loaded := PatrolLoadTolerant(buf, wire, &report)
			if loaded == nil || report != (TableReport{}) {
				t.Fatalf("writer wire reads malformed: active=%t has_target=%t value=%+v report=%+v", active, hasTarget, loaded, report)
			}
			if loaded.Active != active || (active && loaded.HasTarget != hasTarget) {
				t.Fatalf("wrong bool values: active=%t has_target=%t loaded=%+v", active, hasTarget, loaded)
			}
			strict, err := PatrolReadStrict(wire)
			if err != nil || strict.Active != active || (active && strict.HasTarget != hasTarget) {
				t.Fatalf("strict read active=%t has_target=%t: value=%+v err=%v", active, hasTarget, strict, err)
			}
			out := make([]byte, PatrolMeasure(loaded))
			if PatrolSave(loaded, out) != int64(len(out)) || !bytes.Equal(out, wire) {
				t.Fatalf("writer wire did not re-encode byte for byte: active=%t has_target=%t", active, hasTarget)
			}
		}
	}
}
`)
}
