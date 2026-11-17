package gotable

import "testing"

// TestFixedCountBelowMinRefuses holds the generated Go fixed read and write to
// docs/SPEC.md §5.1 row 3: a counted array's count below its Min is refused on
// read, and a save refuses it in every build. The record stores two of
// [2..3]uint16; the count is forged to one and the slot it leaves dead is
// zeroed, so only the low bound can decide the read.
func TestFixedCountBelowMinRefuses(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
fixed table Root {
    ca [2..3]uint16
}
`, `package probe
import ("bytes";"encoding/binary";"testing")

func TestFixedCountBelowMin(t *testing.T) {
	t.Parallel()
	var one Root
	RootReset(&one)
	one.CaCount = 2
	one.Ca[0] = 7
	one.Ca[1] = 65535
	need := RootFixedMeasure(1)
	buf := make([]byte, need)
	if n := RootFixedSave([]Root{one}, buf); n != need {
		t.Fatalf("save %d of %d", n, need)
	}
	needle := []byte{2, 0, 0, 0, 7, 0, 0xff, 0xff}
	at := bytes.Index(buf, needle)
	if at < 0 {
		t.Fatal("the count of two with its two elements is not in the record")
	}
	forged := append([]byte(nil), buf...)
	binary.LittleEndian.PutUint32(forged[at:], 1)
	binary.LittleEndian.PutUint16(forged[at+6:], 0)
	got := make([]Root, 1)
	plan := make([]TableFixedEntry, 256)
	var r TableReport
	if n := RootFixedLoad(got, forged, plan, &r); n >= 0 || !r.Refused() {
		t.Fatalf("a count of one below [2..3]: n=%d reason=%q, want a refusal", n, r.Reason)
	}

	var below Root
	RootReset(&below)
	below.CaCount = 1
	below.Ca[0] = 7
	if n := RootFixedSave([]Root{below}, make([]byte, need)); n >= 0 {
		t.Fatalf("a save of a count of one below [2..3]: n=%d, want -1", n)
	}
}
`)
}
