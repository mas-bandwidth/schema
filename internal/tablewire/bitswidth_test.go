package tablewire

import "testing"

// put and get refuse a width outside [0,64]: a wider put would silently write
// zeros past bit 63 (Go shifts by >=64 yield 0), and a wider get would read
// them, so the writer names the width and panics instead of corrupting the
// wire. Widths 0, 1, 63 and 64 round-trip.
func TestBitWriterPutRefusesOverWidth(t *testing.T) {
	t.Parallel()

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("put(^uint64(0), 65) did not panic")
			}
		}()
		var w bitWriter
		w.put(^uint64(0), 65)
	}()

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("get(65) did not panic")
			}
		}()
		r := newBitReader(make([]byte, 9))
		r.get(65)
	}()

	for _, n := range []int{0, 1, 63, 64} {
		var w bitWriter
		w.put(^uint64(0), n)
		r := newBitReader(w.b)
		got, ok := r.get(n)
		if !ok {
			t.Fatalf("width %d: get returned not-ok", n)
		}
		want := ^uint64(0)
		if n < 64 {
			want = (uint64(1) << uint(n)) - 1
		}
		if got != want {
			t.Fatalf("width %d: got %#x want %#x", n, got, want)
		}
	}
}
