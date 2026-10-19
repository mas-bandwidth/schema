package example

import (
	"testing"

	"github.com/mas-bandwidth/serialize.go"
)

// TestProbeHeaderRoundTrip gives the -race run a real generated-code test: the
// packet corpus has no _test.go of its own, so without this the race detector
// has nothing to execute and the gate's -race line proves only that the package
// compiles.
func TestProbeHeaderRoundTrip(t *testing.T) {
	t.Parallel()
	in := ProbeHeader{Version: 3, ProbeId: 0x1122334455667788}
	stream := serialize.NewWriteStream(make([]byte, 64))
	if err := WriteProbeHeader(stream, &in); err != nil {
		t.Fatalf("write: %v", err)
	}
	stream.Flush()
	if err := stream.Err(); err != nil {
		t.Fatalf("write stream: %v", err)
	}
	var out ProbeHeader
	if err := ReadProbeHeader(serialize.NewReadStream(stream.Data()), &out); err != nil {
		t.Fatalf("read: %v", err)
	}
	if out != in {
		t.Fatalf("read back %+v, want %+v", out, in)
	}
}
