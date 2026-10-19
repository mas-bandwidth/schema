package main

import (
	"encoding/binary"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// §3, §2.11: the frame scanner the wire fuzz mutates through reads kind 36,
// the timestamp, as an eight-byte value, exactly as the Go reader and the
// oracle skip it, and the swap pass plants 36 at every kind spot. A scanner
// that called 36 framing damage stopped at the field and framed nothing after
// it, which is the disagreement tables-go-wire-fuzz found.
func TestTimestampKindFramesAtEightBytes(t *testing.T) {
	t.Parallel()
	if got := kindWidth(ir.TableKindTimestamp); got != 8 {
		t.Fatalf("kindWidth(36) = %d, want 8", got)
	}
	if wireKindLast < ir.TableKindTimestamp {
		t.Fatalf("the swap pass plants kinds up to %d and never plants 36", wireKindLast)
	}

	// form, field ref 1 kind 36 + 8 bytes, field ref 2 kind u32 + 4 bytes,
	// the zero reference, then the id table and its count
	data := []byte{ir.TableWireForm}
	data = append(data, 1, ir.TableKindTimestamp, 0, 1, 2, 3, 4, 5, 6, 7)
	data = append(data, 2, ir.TableKindU32, 9, 0, 0, 0)
	data = append(data, 0)
	data = binary.LittleEndian.AppendUint64(data, 0x1111111111111111)
	data = binary.LittleEndian.AppendUint64(data, 0x2222222222222222)
	data = binary.LittleEndian.AppendUint64(data, 2)

	f := frameWire(data)
	if len(f.fields) != 2 {
		t.Fatalf("framed %d fields, want 2: the scanner stopped at kind 36", len(f.fields))
	}
	if f.fields[0].start != 1 || f.fields[0].end != 11 {
		t.Fatalf("the kind-36 field framed as [%d,%d), want [1,11)", f.fields[0].start, f.fields[0].end)
	}
	if f.fields[1].start != 11 || f.fields[1].end != 17 {
		t.Fatalf("the field after it framed as [%d,%d), want [11,17)", f.fields[1].start, f.fields[1].end)
	}
}
