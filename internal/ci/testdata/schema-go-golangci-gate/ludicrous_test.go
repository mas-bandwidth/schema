package ludicrous

import (
	"testing"

	"github.com/mas-bandwidth/serialize.go"
)

// TestDegenerateProbeRoundTrip gives the -race run a real generated-code test:
// the 128-bit corpus has no _test.go of its own, so without this the race
// detector has nothing to execute and the gate's -race line proves only that
// the package compiles.
func TestDegenerateProbeRoundTrip(t *testing.T) {
	t.Parallel()
	in := NewDegenerateProbe()
	in.Tail = 9
	stream := serialize.NewWriteStream(make([]byte, 64))
	if err := WriteDegenerateProbe(stream, &in); err != nil {
		t.Fatalf("write: %v", err)
	}
	stream.Flush()
	if err := stream.Err(); err != nil {
		t.Fatalf("write stream: %v", err)
	}
	var out DegenerateProbe
	if err := ReadDegenerateProbe(serialize.NewReadStream(stream.Data()), &out); err != nil {
		t.Fatalf("read: %v", err)
	}
	if out != in {
		t.Fatalf("read back %+v, want %+v", out, in)
	}
}
