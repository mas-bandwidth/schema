package tablewire_test

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// quantTriple is a quantized f32 shape's three announced facts as they ride:
// min, max and resolution, each a little-endian float32.
func quantTriple(min, max, res float32) []byte {
	out := binary.LittleEndian.AppendUint32(nil, math.Float32bits(min))
	out = binary.LittleEndian.AppendUint32(out, math.Float32bits(max))
	return binary.LittleEndian.AppendUint32(out, math.Float32bits(res))
}

// TestMessageQuantizedDecodeIsFinite is security/compressed-float-top-step-inf
// on the message form. A peer announces a quantized f32 whose facts are each
// finite (min = 3*2^103, max = the largest float32, resolution 1e36: 341
// steps, 9 bits) while its top step dequantizes past the largest float32. The
// checker refuses that declaration in a schema, so it reaches a reader only in
// a hostile announcement. The read refuses the non-finite value as
// non_finite_float, as the packet reader does, and never clamps it to max; the
// step below the top is an ordinary finite value.
func TestMessageQuantizedDecodeIsFinite(t *testing.T) {
	t.Parallel()
	m := listModel(t, "package edge\n\nfixed table Edge\n{\n    x float32 | min = 0.0, max = 1000.0, resolution = 1.0\n}\n")
	own := tablewire.Announce(m.Unit)
	declared := quantTriple(0, 1000, 1)
	if bytes.Count(own, declared) != 1 {
		t.Fatalf("the unit's announcement does not carry the declared triple once")
	}
	hostile := bytes.Replace(own, declared, quantTriple(3*(1<<103), math.MaxFloat32, 1e36), 1)
	var v tablewire.Vocabulary
	var ar tabletext.Report
	if err := v.AnnounceRead(hostile, &ar); err != nil || ar.Refused {
		t.Fatalf("the hostile announcement was refused: %v %+v", err, ar)
	}

	// the batch the audit recorded: one body whose x carries the top step
	var report tabletext.Report
	n, ok, err := tablewire.DecodeMessages(m, []*tabletext.Instance{m.New(m.Lookup("Edge"))}, []byte{0x02, 0x00, 0xa9, 0x0a}, &v, &report)
	if ok || n != 0 {
		t.Errorf("the top step delivered %d bodies", n)
	}
	refusedAs(t, "message Edge.x top step", err, report, "Edge.x")

	// the same body with a lower step: a finite value, and far past this
	// reader's own [0, 1000], so the read refuses out_of_range (§4)
	inst := m.New(m.Lookup("Edge"))
	report = tabletext.Report{}
	n, ok, err = tablewire.DecodeMessages(m, []*tabletext.Instance{inst}, []byte{0x02, 0x00, 0xa1, 0x0a}, &v, &report)
	if ok || n != 0 || tablewire.RefusalReason(err) != tablewire.ReasonOutOfRange {
		t.Fatalf("the step below the top answered %d %v %v %+v", n, ok, err, report)
	}
}
