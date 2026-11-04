package main

import (
	"testing"

	scalardemo "scalardemo"
	"tabledemo"
)

// TestRowC8 — fixed-point F-shift / bits(N) (docs/FIXED-FORM-ALGORITHM.md:351-352).
//
// THE LAW:
//   A fixed-point field's bounds are in VALUE UNITS and its storage is raw,
//   so both ends are shifted by F first; a bits(N) past 2^N - 1 refuses.

func TestRowC8(t *testing.T) {
	// ================================================================
	// PART 1: bits(N) past 2^N - 1 refuses
	// ================================================================
	// RangedWidths has bits(12) (max 2^12-1 = 4095) and bits(48)
	// (max 2^48-1 = 281474976710655). The variable-form save writes raw
	// storage; a load past the bit width refuses (docs/SPEC-TABLES.md §4).

	{
		val := tabledemo.RangedWidths{
			B12: 3000,
			B48: 0x123456789ABC,
		}
		buf := make([]byte, tabledemo.RangedWidthsMeasure(&val))
		n := tabledemo.RangedWidthsSave(&val, buf)
		if n < 0 {
			t.Fatal("RangedWidthsSave failed")
		}

		var back tabledemo.RangedWidths
		var report tabledemo.TableReport
		ok := tabledemo.RangedWidthsLoad(&back, buf[:n], &report)
		if !ok {
			t.Fatal("RangedWidthsLoad failed")
		}
		if back.B12 != 3000 {
			t.Fatalf("bits(N): value=3000 was clean; got B12=%d %+v", back.B12, report)
		}

		// bits(12): the writer REFUSES 4096 (the writer-refusal test below
		// pins that half of the rule), so hand-forge a wire that reads 4096
		// and check the load refuses it. The B12 lane is a
		// little-endian 16-bit field; save it at its in-range maximum (4095
		// = 0x0FFF, bytes ff 0f) and overwrite those two bytes with 4096
		// = 0x1000 (bytes 00 10).
		v12 := tabledemo.RangedWidths{B12: 4095} // in-range maximum
		buf2 := make([]byte, tabledemo.RangedWidthsMeasure(&v12))
		n2 := tabledemo.RangedWidthsSave(&v12, buf2)
		if n2 < 0 {
			t.Fatal("RangedWidthsSave b12 max failed")
		}
		off12 := findBytes(buf2[:n2], []byte{0xFF, 0x0F})
		if off12 < 0 {
			t.Fatal("b12 value bytes not found")
		}
		buf2[off12] = 0x00
		buf2[off12+1] = 0x10

		var back2 tabledemo.RangedWidths
		var report2 tabledemo.TableReport
		if tabledemo.RangedWidthsLoad(&back2, buf2[:n2], &report2) || report2.Err() == nil {
			t.Fatalf("bits(N): forged 4096 read: %+v", report2)
		}

		// bits(48): max 2^48-1 = 281474976710655; same hand-forge for a
		// wire that reads 300000000000000. The B48 lane is a little-endian
		// 64-bit field; save it at its in-range maximum (281474976710655 =
		// 0x0000FFFFFFFFFFFF, bytes ff ff ff ff ff ff 00 00) and overwrite
		// those eight bytes with 300000000000000 = 0x000110D9316EC000
		// (bytes 00 c0 6e 31 d9 10 01 00, LE).
		v48 := tabledemo.RangedWidths{B48: 281474976710655} // in-range maximum
		buf3 := make([]byte, tabledemo.RangedWidthsMeasure(&v48))
		n3 := tabledemo.RangedWidthsSave(&v48, buf3)
		if n3 < 0 {
			t.Fatal("RangedWidthsSave b48 max failed")
		}
		off48 := findBytes(buf3[:n3], []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0x00})
		if off48 < 0 {
			t.Fatal("b48 value bytes not found")
		}
		over := []byte{0x00, 0xC0, 0x6E, 0x31, 0xD9, 0x10, 0x01, 0x00}
		for i, b := range over {
			buf3[off48+i] = b
		}

		var back3 tabledemo.RangedWidths
		var report3 tabledemo.TableReport
		if tabledemo.RangedWidthsLoad(&back3, buf3[:n3], &report3) || report3.Err() == nil {
			t.Fatalf("bits(N): forged 300000000000000 read: %+v", report3)
		}

		// bits(32) fills its lane exactly and reads (negative control)
		val.B48 = 0
		val.B12 = 0
		val.B32 = 0xFFFFFFFF
		buf4 := make([]byte, tabledemo.RangedWidthsMeasure(&val))
		n4 := tabledemo.RangedWidthsSave(&val, buf4)
		if n4 < 0 {
			t.Fatal("RangedWidthsSave b32 max failed")
		}

		var back4 tabledemo.RangedWidths
		var report4 tabledemo.TableReport
		ok4 := tabledemo.RangedWidthsLoad(&back4, buf4[:n4], &report4)
		if !ok4 {
			t.Fatal("RangedWidthsLoad b32 max failed")
		}
		if back4.B32 != 0xFFFFFFFF {
			t.Fatalf("bits(N): bits(32) filling its lane; B32=%d %+v", back4.B32, report4)
		}
	}

	// ================================================================
	// PART 2: fixed-point F-shift
	// ================================================================
	// SimState.tilt is fixed(4,4) | min = -8, max = 7. F = 4, so the
	// shifted bounds are [-8*16, 7*16] = [-128, 112].
	//
	// The law says "both ends are shifted by F first". To prove it, raw
	// storage is forged into a saved fixed file and read:
	//   - raw 8 is past the value-unit max (7) but INSIDE the shifted max
	//     (112), and reads
	//   - raw 112, exactly the shifted max, reads
	//   - raw 113 is past the shifted max, and refuses (docs/SPEC-TABLES.md §4)

	{
		zero := make([]byte, scalardemo.SimStateFixedMeasure(1))
		if scalardemo.SimStateFixedSave([]scalardemo.SimState{{}}, zero) < 0 {
			t.Fatal("SimStateFixedSave failed")
		}
		plan := make([]scalardemo.TableFixedEntry, 64)
		// read forges raw into the field set marks (set stores 1, so the first
		// byte that differs from the zero file is the field's first byte)
		read := func(set func(*scalardemo.SimState), width int, raw int64) (scalardemo.SimState, int64, scalardemo.TableReport) {
			var probe scalardemo.SimState
			set(&probe)
			wire := make([]byte, len(zero))
			if scalardemo.SimStateFixedSave([]scalardemo.SimState{probe}, wire) < 0 {
				t.Fatal("SimStateFixedSave probe failed")
			}
			at := -1
			for i := range wire {
				if wire[i] != zero[i] {
					at = i
					break
				}
			}
			if at < 0 {
				t.Fatal("probe field not isolated")
			}
			for k := 0; k < width; k++ {
				wire[at+k] = byte(raw >> (8 * k))
			}
			var got [1]scalardemo.SimState
			var report scalardemo.TableReport
			n := scalardemo.SimStateFixedLoad(got[:], wire, plan, &report)
			return got[0], n, report
		}
		tilt := func(v *scalardemo.SimState) { v.Tilt = 1 }
		angle := func(v *scalardemo.SimState) { v.Angle = 1 }
		speed := func(v *scalardemo.SimState) { v.Speed = 1 }

		if v, n, r := read(tilt, 1, 8); n != 1 || r.Err() != nil || v.Tilt != 8 {
			t.Fatalf("F-shift: raw 8 (past value-unit max) reads; n=%d tilt=%d %+v", n, v.Tilt, r)
		}
		if v, n, r := read(tilt, 1, 112); n != 1 || r.Err() != nil || v.Tilt != 112 {
			t.Fatalf("F-shift: raw 112 (exactly shifted max) reads; n=%d tilt=%d %+v", n, v.Tilt, r)
		}
		if _, n, r := read(tilt, 1, 113); n != -1 || r.Err() == nil {
			t.Fatalf("F-shift: raw 113 (past shifted max) read: n=%d %+v", n, r)
		}

		// angle: fixed(16,16) | min=-180, max=180, F=16
		// shifted bounds: [-11796480, 11796480]
		if v, n, r := read(angle, 4, 11796480); n != 1 || r.Err() != nil || v.Angle != 11796480 {
			t.Fatalf("F-shift: angle 11796480 reads; n=%d angle=%d %+v", n, v.Angle, r)
		}
		if _, n, r := read(angle, 4, 11796481); n != -1 || r.Err() == nil {
			t.Fatalf("F-shift: angle 11796481 read: n=%d %+v", n, r)
		}

		// ufixed(16,16) ping-style field: speed is ufixed(16,16)
		// min=0, max=1000, F=16 → raw bounds [0, 65536000]
		if v, n, r := read(speed, 4, 65536000); n != 1 || r.Err() != nil || v.Speed != 65536000 {
			t.Fatalf("F-shift: speed 65536000 reads; n=%d speed=%d %+v", n, v.Speed, r)
		}
		if _, n, r := read(speed, 4, 65536001); n != -1 || r.Err() == nil {
			t.Fatalf("F-shift: speed 65536001 read: n=%d %+v", n, r)
		}
	}
}

// TestRowC8WriterRefusesOutOfRange — the other half of the LAW: the writer
// refuses a value its reader would refuse (docs/FIXED-FORM-ALGORITHM.md §3.1),
// so Measure returns a negative size for an out-of-range value. TestRowC8
// hand-forges the wire for exactly these values; this pins that the generator
// refuses them, so the row never needs to ask the writer to write them.
func TestRowC8WriterRefusesOutOfRange(t *testing.T) {
	t.Parallel()

	b12 := tabledemo.RangedWidths{B12: 4096} // one past 2^12-1
	if m := tabledemo.RangedWidthsMeasure(&b12); m >= 0 {
		t.Fatalf("bits(12): writer accepted 4096 (measure=%d)", m)
	}

	b48 := tabledemo.RangedWidths{B48: 300000000000000} // one past 2^48-1
	if m := tabledemo.RangedWidthsMeasure(&b48); m >= 0 {
		t.Fatalf("bits(48): writer accepted 300000000000000 (measure=%d)", m)
	}
}

// findBytes returns the index of the first occurrence of pat in wire, or -1.
func findBytes(wire, pat []byte) int {
	for i := 0; i+len(pat) <= len(wire); i++ {
		match := true
		for j := range pat {
			if wire[i+j] != pat[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
