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
//   so both ends are shifted by F first; a bits(N) clamps to 2^N - 1.

func TestRowC8(t *testing.T) {
	// ================================================================
	// PART 1: bits(N) clamps to 2^N - 1
	// ================================================================
	// RangedWidths has bits(12) (max 2^12-1 = 4095) and bits(48)
	// (max 2^48-1 = 281474976710655). The variable-form save writes raw
	// storage; load clamps past the bit width to the declared maximum.

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
		if back.B12 != 3000 || report.Clamped != 0 {
			t.Fatalf("bits(N): value=4095 was clean; got B12=%d clamped=%d", back.B12, report.Clamped)
		}

		// bits(12): set past 4095, save, load — must clamp.
		val.B12 = 4096 // one past 2^12 - 1
		buf2 := make([]byte, tabledemo.RangedWidthsMeasure(&val))
		n2 := tabledemo.RangedWidthsSave(&val, buf2)
		if n2 < 0 {
			t.Fatal("RangedWidthsSave b12 overload failed")
		}

		var back2 tabledemo.RangedWidths
		var report2 tabledemo.TableReport
		ok2 := tabledemo.RangedWidthsLoad(&back2, buf2[:n2], &report2)
		if !ok2 {
			t.Fatal("RangedWidthsLoad b12 overload failed")
		}
		if back2.B12 != 4095 {
			t.Fatalf("bits(N): 4096 -> want 4095 (2^12-1), got %d", back2.B12)
		}
		if report2.Clamped != 1 {
			t.Fatalf("bits(N): want 1 clamped, got %d", report2.Clamped)
		}

		// bits(48): max 2^48-1 = 281474976710655
		val.B12 = 0
		val.B48 = 300000000000000 // past 2^48-1
		buf3 := make([]byte, tabledemo.RangedWidthsMeasure(&val))
		n3 := tabledemo.RangedWidthsSave(&val, buf3)
		if n3 < 0 {
			t.Fatal("RangedWidthsSave b48 overload failed")
		}

		var back3 tabledemo.RangedWidths
		var report3 tabledemo.TableReport
		ok3 := tabledemo.RangedWidthsLoad(&back3, buf3[:n3], &report3)
		if !ok3 {
			t.Fatal("RangedWidthsLoad b48 overload failed")
		}
		if back3.B48 != 281474976710655 {
			t.Fatalf("bits(N): 300000000000000 -> want 281474976710655 (2^48-1), got %d", back3.B48)
		}
		if report3.Clamped != 1 {
			t.Fatalf("bits(N): b48 want 1 clamped, got %d", report3.Clamped)
		}

		// bits(32) fills its lane exactly — no clamp fires (negative control)
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
		if back4.B32 != 0xFFFFFFFF || report4.Clamped != 0 {
			t.Fatalf("bits(N): bits(32) filling its lane clamps nothing; B32=%d clamped=%d",
				back4.B32, report4.Clamped)
		}
	}

	// ================================================================
	// PART 2: fixed-point F-shift
	// ================================================================
	// SimState.tilt is fixed(4,4) | min = -8, max = 7. F = 4, so the
	// shifted bounds are [-8*16, 7*16] = [-128, 112].
	//
	// The law says "both ends are shifted by F first". To prove it:
	//   - raw value 8 is past the value-unit max (7) but INSIDE the
	//     shifted max (112) → must NOT clamp
	//   - raw value 113 is past the shifted max → clamps to 112

	{
		var sim scalardemo.SimState
		sim.Tilt = 8 // past value-unit max 7, inside shifted max 112
		var clamped int32
		scalardemo.SimStateFixedClampBody(&sim, &clamped)
		if sim.Tilt != 8 || clamped != 0 {
			t.Fatalf("F-shift: raw 8 (past value-unit max) survives; tilt=%d clamped=%d",
				sim.Tilt, clamped)
		}

		sim = scalardemo.SimState{}
		sim.Tilt = 113 // one past shifted max
		scalardemo.SimStateFixedClampBody(&sim, &clamped)
		if sim.Tilt != 112 || clamped != 1 {
			t.Fatalf("F-shift: 113 -> want 112 (shifted max), got tilt=%d clamped=%d",
				sim.Tilt, clamped)
		}

		// value at exactly the shifted max: clean
		sim = scalardemo.SimState{}
		sim.Tilt = 112
		clamped = 0
		scalardemo.SimStateFixedClampBody(&sim, &clamped)
		if sim.Tilt != 112 || clamped != 0 {
			t.Fatalf("F-shift: raw 112 (exactly shifted max) clamps nothing; tilt=%d clamped=%d",
				sim.Tilt, clamped)
		}

		// angle: fixed(16,16) | min=-180, max=180, F=16
		// shifted bounds: [-11796480, 11796480]
		sim = scalardemo.SimState{}
		sim.Angle = 11796481 // one past shifted max
		clamped = 0
		scalardemo.SimStateFixedClampBody(&sim, &clamped)
		if sim.Angle != 11796480 || clamped != 1 {
			t.Fatalf("F-shift: angle 11796481 -> want 11796480; angle=%d clamped=%d",
				sim.Angle, clamped)
		}

		// ufixed(16,16) ping-style field: speed is ufixed(16,16)
		// min=0, max=1000, F=16 → raw bounds [0, 65536000]
		sim = scalardemo.SimState{}
		sim.Speed = 65536001 // one past shifted max
		clamped = 0
		scalardemo.SimStateFixedClampBody(&sim, &clamped)
		if sim.Speed != 65536000 || clamped != 1 {
			t.Fatalf("F-shift: speed 65536001 -> want 65536000; speed=%d clamped=%d",
				sim.Speed, clamped)
		}
	}
}
