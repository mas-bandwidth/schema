package main

import (
	"testing"

	"scalardemo"
)

func TestRowC7(t *testing.T) {
	// Ranged scalar clamp (docs/FIXED-FORM-ALGORITHM.md §4.6, line 351):
	// "A RANGED SCALAR clamps to its declared min and max, COUNT clamped."
	//
	// Test with the SimState scalars table: fixed-point fields with
	// declared min/max bounds.  The fixed-form bounds pass
	// (SimStateFixedClampBody) clamps out-of-range raw storage values
	// after the plan-based copy.  The Clamped counter is incremented
	// for each field that fires.

	val := scalardemo.SimState{
		Angle: 0,
		Ticks: 500,
	}
	buf := make([]byte, scalardemo.SimStateFixedMeasure(1))
	n := scalardemo.SimStateFixedSave([]scalardemo.SimState{val}, buf)
	if n < 0 {
		t.Fatal("SimStateFixedSave failed")
	}
	data := buf[:n]
	plan := make([]scalardemo.TableFixedEntry, 64)
	var values [1]scalardemo.SimState

	// Verify valid data loads cleanly with no clamping.
	var report scalardemo.TableReport
	values[0] = scalardemo.SimState{}
	nLoaded := scalardemo.SimStateFixedLoad(values[:], data, plan, &report)
	if nLoaded != 1 || report.Malformed || report.Verdict != scalardemo.TableOpenOk {
		t.Fatalf("valid: n=%d malformed=%v verdict=%v clamped=%d",
			nLoaded, report.Malformed, report.Verdict, report.Clamped)
	}
	if report.Clamped != 0 {
		t.Fatalf("valid: expected 0 clamped, got %d", report.Clamped)
	}

	// Fixed form layout: header(16) + layoutLen(4) + layout + records.
	// Each record: 8-byte hash + 243-byte body (SimStateFixedBodyBytes).
	layoutOverhead := int(scalardemo.SimStateFixedMeasure(0))
	bodyOff := layoutOverhead + 8

	// Field body offsets from SimStateFixedWriteBody (ScalarsTable.go:6220):
	//   Tilt   int8  at body[0]    raw max clamp = 112
	//   Angle  int32 at body[1:5]  raw min = -11796480, raw max = 11796480
	//   Ticks  int32 at body[29:33] min = 0, max = 1000000

	t.Run("tilt_clamp_high", func(t *testing.T) {
		mut := make([]byte, len(data))
		copy(mut, data)
		mut[bodyOff] = 127 // Tilt max raw clamp is 112
		var r1 scalardemo.TableReport
		values[0] = scalardemo.SimState{}
		_ = scalardemo.SimStateFixedLoad(values[:], mut, plan, &r1)
		if r1.Clamped == 0 {
			t.Errorf("tilt high: Clamped=0")
		}
		if values[0].Tilt != 112 {
			t.Errorf("tilt high: got %d, want 112", values[0].Tilt)
		}
	})

	t.Run("angle_clamp_low", func(t *testing.T) {
		mut := make([]byte, len(data))
		copy(mut, data)
		// Angle raw min = -11796480.  Write -20000000 as LE int32.
		mut[bodyOff+1] = 0x00
		mut[bodyOff+2] = 0x2c
		mut[bodyOff+3] = 0xbe
		mut[bodyOff+4] = 0xfe
		var r1 scalardemo.TableReport
		values[0] = scalardemo.SimState{}
		_ = scalardemo.SimStateFixedLoad(values[:], mut, plan, &r1)
		if r1.Clamped == 0 {
			t.Errorf("angle low: Clamped=0")
		}
		if values[0].Angle != -11796480 {
			t.Errorf("angle low: got %d, want -11796480", values[0].Angle)
		}
	})

	t.Run("angle_clamp_high", func(t *testing.T) {
		mut := make([]byte, len(data))
		copy(mut, data)
		// Angle raw max = 11796480.  Write 20000000 as LE int32.
		mut[bodyOff+1] = 0x00
		mut[bodyOff+2] = 0x2d
		mut[bodyOff+3] = 0x31
		mut[bodyOff+4] = 0x01
		var r1 scalardemo.TableReport
		values[0] = scalardemo.SimState{}
		_ = scalardemo.SimStateFixedLoad(values[:], mut, plan, &r1)
		if r1.Clamped == 0 {
			t.Errorf("angle high: Clamped=0")
		}
		if values[0].Angle != 11796480 {
			t.Errorf("angle high: got %d, want 11796480", values[0].Angle)
		}
	})

	t.Run("ticks_clamp_low", func(t *testing.T) {
		mut := make([]byte, len(data))
		copy(mut, data)
		// Ticks min raw = 0.  Write 0xFFFFFFFF (uint32=4294967295) which
		// lands as int32(-1) in the struct, triggering the <0 clamp to 0.
		mut[bodyOff+29] = 0xff
		mut[bodyOff+30] = 0xff
		mut[bodyOff+31] = 0xff
		mut[bodyOff+32] = 0xff
		var r1 scalardemo.TableReport
		values[0] = scalardemo.SimState{}
		_ = scalardemo.SimStateFixedLoad(values[:], mut, plan, &r1)
		if r1.Clamped == 0 {
			t.Errorf("ticks low: Clamped=0")
		}
		if values[0].Ticks != 0 {
			t.Errorf("ticks low: got %d, want 0 (clamped to min)", values[0].Ticks)
		}
	})

	t.Run("ticks_clamp_high", func(t *testing.T) {
		mut := make([]byte, len(data))
		copy(mut, data)
		// Ticks max = 1000000 raw.  Write 2000000 as LE int32.
		mut[bodyOff+29] = 0x40
		mut[bodyOff+30] = 0x84
		mut[bodyOff+31] = 0x1e
		mut[bodyOff+32] = 0x00
		var r1 scalardemo.TableReport
		values[0] = scalardemo.SimState{}
		_ = scalardemo.SimStateFixedLoad(values[:], mut, plan, &r1)
		if r1.Clamped == 0 {
			t.Errorf("ticks high: Clamped=0")
		}
		if values[0].Ticks != 1000000 {
			t.Errorf("ticks high: got %d, want 1000000", values[0].Ticks)
		}
	})
}
