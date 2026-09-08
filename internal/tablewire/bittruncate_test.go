package tablewire

import (
	"bytes"
	"testing"
)

func TestBitWriterTruncate(t *testing.T) {
	for prefix := 0; prefix < 17; prefix++ {
		var got, want bitWriter
		got.put(0x51b7, prefix)
		want.put(0x51b7, prefix)
		got.put(0xffffffff, 32)
		got.truncate(prefix)
		got.put(0x308, 13)
		want.put(0x308, 13)
		if got.n != want.n || !bytes.Equal(got.b, want.b) {
			t.Fatalf("prefix %d: got %x want %x", prefix, got.b, want.b)
		}
	}
}
