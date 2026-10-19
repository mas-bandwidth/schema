package cstable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// A signed field's raw descriptor value is the two's-complement BIT PATTERN.
// Under /checked+ the sign-extension to ulong must be unchecked, or a negative
// profile timestamp raises OverflowException on every read, write and clamp.
func TestSignedRawGetIsUnchecked(t *testing.T) {
	t.Parallel()
	for _, width := range []int{8, 16, 32, 64} {
		got := csRawGet("v", ir.FieldType{Kind: ir.TInt, Signed: true, Width: width})
		if !strings.HasPrefix(got, "unchecked((ulong)(long)") || !strings.HasSuffix(got, ")") {
			t.Errorf("signed int%d raw get = %q; want an unchecked sign-extending cast", width, got)
		}
	}
	if got := csRawGet("v", ir.FieldType{Kind: ir.TInt, Signed: false, Width: 32}); strings.Contains(got, "unchecked") {
		t.Errorf("unsigned raw get = %q; want a plain widening cast", got)
	}
}

// The region runtime writes one stored word a byte at a time. Each lane's
// narrowing to byte truncates by design, so it must be unchecked: /checked+
// otherwise raises OverflowException on any word whose low lane is not the
// whole value, which is every multi-byte field in the corpus.
func TestNativePutTruncatesEachByteUnchecked(t *testing.T) {
	t.Parallel()
	want := "unchecked((byte)(value >> (i * 8)))"
	if !strings.Contains(tableRegionSource, want) {
		t.Errorf("tableRegionSource does not emit %q for the per-byte store", want)
	}
	if strings.Contains(tableRegionSource, "= (byte)(value >> (i * 8))") {
		t.Error("tableRegionSource still emits a checked narrowing for the per-byte store")
	}
}

// Every other emitted store that narrows a UInt128 or ulong to a lane, and the
// one signed read from a raw ulong, is a bit-pattern operation rather than an
// arithmetic one. /checked+ must not turn any of them into an exception.
func TestEmittedBitPatternNarrowingsAreUnchecked(t *testing.T) {
	t.Parallel()
	checked := []struct {
		name string
		src  string
		bad  string
	}{
		{"native writer", tableNativeSource, "= (byte)(raw >> (i * 8))"},
		{"region cook writer", tableRegionCookSource, "= (byte)(raw >> (i * 8))"},
		{"wire wide write", tableWireSource, "w.Fixed((ulong)raw, Math.Min(width, 8))"},
		{"region wide write", tableRegionWriteSource, "w.Fixed((ulong)raw, Math.Min(width, 8))"},
		{"retain message wide write", tableRetainMessageCaptureSource, "output.Fixed((ulong)raw, Math.Min(width, 8))"},
		{"json signed read", tableJsonWalkSource, "WriteSigned(ref o, (long)f.GetRaw(owner, index))"},
		{"retain put32", tableRetainSource, "p[i] = (byte)(v >> (8 * i))"},
		{"retain put64 low", tableRetainSource, "RetainPut32(p, (uint)v)"},
		{"retain put64 high", tableRetainSource, "RetainPut32(p + 4, (uint)(v >> 32))"},
		{"wire fixed byte", tableWireSource, "Byte((byte)v)"},
		{"json signed negation", tableJsonWalkSource, "WriteUnsigned(ref o, 0ul -"},
		{"json signed widening", tableJsonWalkSource, "WriteUnsigned(ref o, (ulong)value);"},
		{"builder load offset", tableBuilderLoadSource, "long offset = (long)NativeWord"},
		{"region message load offset", tableRegionMessageLoadSource, "long offset = (long)NativeWord"},
		{"retain load offset", tableRetainSource, ": (long)NativeWord(retain->Directory"},
		{"region value offset", tableRegionSource, "long offset = (long)NativeWord(row"},
		{"region raw word", tableRegionSource, "ulong raw = (ulong)NativeWord"},
		{"region tag word", tableRegionSource, "return (ulong)NativeWord(Base + At"},
		{"region union tag", tableRegionSource, "ulong tag = (ulong)NativeWord"},
		{"region builder offset", tableRegionSource, "long offset = (long)NativeWord(directory"},
		{"region pointer delta", tableRegionSource, "long delta = (long)Raw(f, index)"},
		{"retain input fixed", tableRetainSource, "Output[At + i] = (byte)(value"},
		{"retain input var", tableRetainSource, "Fixed((byte)(value | 128)"},
		{"region blob pointer", tableRegionGraphSource, "NativeWord(child.Base + at, 4)"},
		{"region node blob pointer", tableRegionGraphSource, "byte* node = Value.Base + Value.At;"},
		{"variable json blob byte", tableVariableJsonSource, "bytes[count++] = (byte)(accumulator >> held)"},
	}
	for _, c := range checked {
		if strings.Contains(c.src, c.bad) {
			t.Errorf("%s still emits the checked narrowing %q", c.name, c.bad)
		}
	}
}
