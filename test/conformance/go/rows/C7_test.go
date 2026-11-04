package main

import (
	"testing"

	"scalardemo"
)

func TestRowC7(t *testing.T) {
	// A ranged scalar past its declared min or max refuses the fixed read
	// (docs/FIXED-FORM-ALGORITHM.md §4.6, docs/SPEC-TABLES.md §4): the
	// SimState scalars table's fixed-point fields, each forged one way past
	// its bound, and the clean file reads.

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

	// the clean file reads
	var report scalardemo.TableReport
	values[0] = scalardemo.SimState{}
	nLoaded := scalardemo.SimStateFixedLoad(values[:], data, plan, &report)
	if nLoaded != 1 || report.Refused() || report.Verdict != scalardemo.TableOpenOk {
		t.Fatalf("valid: n=%d %+v", nLoaded, report)
	}

	// Fixed form layout: header(16) + layoutLen(4) + layout + records.
	// Each record: 8-byte hash + 243-byte body (SimStateFixedBodyBytes).
	layoutOverhead := int(scalardemo.SimStateFixedMeasure(0))
	bodyOff := layoutOverhead + 8

	// Field body offsets from SimStateFixedWriteBody (ScalarsTable.go:6220):
	//   Tilt   int8  at body[0]    raw max = 112
	//   Angle  int32 at body[1:5]  raw min = -11796480, raw max = 11796480
	//   Ticks  int32 at body[29:33] min = 0, max = 1000000

	t.Run("tilt_past_high", func(t *testing.T) {
		mut := make([]byte, len(data))
		copy(mut, data)
		mut[bodyOff] = 127 // Tilt max raw is 112
		var r1 scalardemo.TableReport
		values[0] = scalardemo.SimState{}
		if n := scalardemo.SimStateFixedLoad(values[:], mut, plan, &r1); n != -1 || r1.Err() == nil {
			t.Errorf("tilt high: read past the range: n=%d %+v", n, r1)
		}
	})

	t.Run("angle_past_low", func(t *testing.T) {
		mut := make([]byte, len(data))
		copy(mut, data)
		// Angle raw min = -11796480.  Write -20000000 as LE int32.
		mut[bodyOff+1] = 0x00
		mut[bodyOff+2] = 0x2c
		mut[bodyOff+3] = 0xbe
		mut[bodyOff+4] = 0xfe
		var r1 scalardemo.TableReport
		values[0] = scalardemo.SimState{}
		if n := scalardemo.SimStateFixedLoad(values[:], mut, plan, &r1); n != -1 || r1.Err() == nil {
			t.Errorf("angle low: read past the range: n=%d %+v", n, r1)
		}
	})

	t.Run("angle_past_high", func(t *testing.T) {
		mut := make([]byte, len(data))
		copy(mut, data)
		// Angle raw max = 11796480.  Write 20000000 as LE int32.
		mut[bodyOff+1] = 0x00
		mut[bodyOff+2] = 0x2d
		mut[bodyOff+3] = 0x31
		mut[bodyOff+4] = 0x01
		var r1 scalardemo.TableReport
		values[0] = scalardemo.SimState{}
		if n := scalardemo.SimStateFixedLoad(values[:], mut, plan, &r1); n != -1 || r1.Err() == nil {
			t.Errorf("angle high: read past the range: n=%d %+v", n, r1)
		}
	})

	t.Run("ticks_past_low", func(t *testing.T) {
		mut := make([]byte, len(data))
		copy(mut, data)
		// Ticks min raw = 0.  Write 0xFFFFFFFF (uint32=4294967295) which
		// lands as int32(-1), under the minimum.
		mut[bodyOff+29] = 0xff
		mut[bodyOff+30] = 0xff
		mut[bodyOff+31] = 0xff
		mut[bodyOff+32] = 0xff
		var r1 scalardemo.TableReport
		values[0] = scalardemo.SimState{}
		if n := scalardemo.SimStateFixedLoad(values[:], mut, plan, &r1); n != -1 || r1.Err() == nil {
			t.Errorf("ticks low: read past the range: n=%d %+v", n, r1)
		}
	})

	t.Run("ticks_past_high", func(t *testing.T) {
		mut := make([]byte, len(data))
		copy(mut, data)
		// Ticks max = 1000000 raw.  Write 2000000 as LE int32.
		mut[bodyOff+29] = 0x40
		mut[bodyOff+30] = 0x84
		mut[bodyOff+31] = 0x1e
		mut[bodyOff+32] = 0x00
		var r1 scalardemo.TableReport
		values[0] = scalardemo.SimState{}
		if n := scalardemo.SimStateFixedLoad(values[:], mut, plan, &r1); n != -1 || r1.Err() == nil {
			t.Errorf("ticks high: read past the range: n=%d %+v", n, r1)
		}
	})
}
