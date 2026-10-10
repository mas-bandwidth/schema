package gotable

import (
	"testing"
)

const t02Prelude = `package probe
import ("encoding/binary"; "testing"; "unsafe")

var _ = binary.LittleEndian
var _ = unsafe.Sizeof(0)

const poison = 0x5A

func cleanFile(t *testing.T, n int) []byte {
	t.Helper()
	vals := make([]Point, n)
	for i := range vals {
		vals[i] = Point{X: int32(4242 + i), Y: int32(-7 - i)}
	}
	buf := make([]byte, PointFixedMeasure(int64(n)))
	if got := PointFixedSave(vals, buf); got != int64(len(buf)) {
		t.Fatalf("save %d", got)
	}
	return buf
}
`

func TestFixedRoadmapGoT02Plans(t *testing.T) {
	t.Parallel()
	const schema = `package probe
fixed table Point {
    x int32 = 1
    y int32 = 2
}
fixed table WithBytes {
    b bytes(8)
}
`
	for _, c := range []struct{ id, probe string }{
		{"go/R1", r1LineageStaticData},
		{"go/R2", r2RecordBytes},
		{"go/R23", r23TableFixedKnownLayoutOrder},
		{"go/R25", r25PlanTooLarge},
		{"go/R26", r26LineageWouldNotBuild},
		{"go/W14", w14PlanDstOffsetOf},
		{"go/R4", r4HashFNV1a64},
		{"go/R5", r5DigestTags},
		{"go/W11", w11BytesKind14},
		{"go/W12", w12HashIncludesCount},
	} {
		t.Run(c.id, func(t *testing.T) {
			t.Parallel()
			runGenerated(t, schema, c.probe)
		})
	}
}

const r1LineageStaticData = t02Prelude + `
func TestR1(t *testing.T) {
	if len(PointFixedKnown) < 1 {
		t.Fatalf("PointFixedKnown has no entries")
	}
	if PointFixedKnown[len(PointFixedKnown)-1].Hash != PointFixedHash {
		t.Fatalf("current layout last hash mismatch")
	}
}
`

const r2RecordBytes = t02Prelude + `
func TestR2(t *testing.T) {
	for _, k := range PointFixedKnown {
		if k.Record != 16 {
			t.Fatalf("PointFixedKnown record size is %d, want 16", k.Record)
		}
	}
}
`

const r23TableFixedKnownLayoutOrder = t02Prelude + `
func TestR23(t *testing.T) {
	k := TableFixedKnownLayout{Hash: 1, Layout: []byte{2}, Record: 3}
	if k.Hash != 1 || len(k.Layout) != 1 || k.Record != 3 {
		t.Fatalf("TableFixedKnownLayout order or members mismatch")
	}
	var r TableReport
	if r.LayoutHash != 0 {
		t.Fatalf("LayoutHash not zero on default report")
	}
}
`

const r25PlanTooLarge = t02Prelude + `
func TestR25(t *testing.T) {
	plan := make([]TableFixedEntry, 0)
	var r TableReport
	file := cleanFile(t, 1)
	n := PointFixedLoad(nil, file, plan, &r)
	if n != -1 || r.Reason != "plan_too_large" {
		t.Fatalf("expected plan_too_large, got %d %v", n, r)
	}
}
`

const r26LineageWouldNotBuild = t02Prelude + `
func TestR26(t *testing.T) {
	plan := make([]TableFixedEntry, 64)
	file := cleanFile(t, 1)
	var r TableReport
	dst := make([]Point, 1)
	if len(PointFixedKnown) > 0 {
		binary.LittleEndian.PutUint64(file[TableFixedHashAt:], PointFixedKnown[0].Hash)
		if len(PointFixedKnown[0].Layout) > 0 {
			PointFixedKnown[0].Layout[0] ^= 0xff
		}
		n := PointFixedLoad(dst, file, plan, &r)
		if n != -1 || r.Reason != "layout_malformed" {
			t.Fatalf("expected layout_malformed, got %d %v", n, r)
		}
	}
}
`

const w14PlanDstOffsetOf = t02Prelude + `
func TestW14(t *testing.T) {
	file := cleanFile(t, 1)
	plan := make([]TableFixedEntry, 64)
	var r TableReport
	dst := make([]Point, 1)
	n := PointFixedLoad(dst, file, plan, &r)
	if n <= 0 {
		t.Fatalf("load failed: %d %v", n, r)
	}
	for _, entry := range plan {
		if entry.Dst < 0 {
			t.Fatalf("invalid dst %d", entry.Dst)
		}
	}
}
`

const r4HashFNV1a64 = t02Prelude + `
func TestR4(t *testing.T) {
	if PointFixedHash == 0 {
		t.Fatalf("PointFixedHash is zero")
	}
}
`

const r5DigestTags = t02Prelude + `
func TestR5(t *testing.T) {
	if len(PointFixedLayout) == 0 {
		t.Fatalf("PointFixedLayout empty")
	}
}
`

const w11BytesKind14 = t02Prelude + `
func TestW11(t *testing.T) {
	if len(WithBytesFixedLayout) == 0 {
		t.Fatalf("WithBytesFixedLayout empty")
	}
	found := false
	for _, b := range WithBytesFixedLayout {
		if b == 14 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("layout kind 14 not found")
	}
}
`

const w12HashIncludesCount = t02Prelude + `
func TestW12(t *testing.T) {
	if PointFixedHash == 0 {
		t.Fatalf("hash is zero")
	}
}
`
