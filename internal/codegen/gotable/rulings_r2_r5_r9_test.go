package gotable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// discardWireOK encodes the text as the root, undamaged.
func discardWireOK(t *testing.T, u *ir.Unit, root, text string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup(root))
	var r tabletext.Report
	if !m.Read(inst, []byte(text), &r) {
		t.Fatalf("%s: the text did not place", root)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

// THE GO LEG OF RULINGS R2, R5 AND R9 (2026-11-22). Source pins over the
// emitted runtime, shelling out to nothing, each of which goes red when the old
// behaviour is planted back. The behaviour itself is run by
// TestRulingR5LoadMeasurePartsRunGo and by internal/tablewire's
// TestFixedLineageOptionalIntoBareRefusesKindMismatch.

const rulingSource = `package probe

fixed table Cfg
{
    n ?int32
    k int32
}

table Graph
{
    a    int32 = 0
    name string(16)
    next *Graph
}
`

func rulingEmitted(t *testing.T) string {
	t.Helper()
	var all strings.Builder
	for _, data := range generate(t, rulingSource) {
		all.Write(data)
	}
	return all.String()
}

func TestRulingR2OptionalIntoBareRefusesKindMismatchGo(t *testing.T) {
	t.Parallel()
	text := rulingEmitted(t)
	for _, want := range []string{
		"c.unlawful = 3 - 2*int8(min(1, te.Kind^35))",
		"return -1 - int(c.unlawful)",
		"if made == -4 {", "out[i].Why = TableRefuseKindMismatch.Error()",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("R2: the Go fixed runtime lost %q", want)
		}
	}
}

func TestRulingR5LoadMeasurePartsEmittedGo(t *testing.T) {
	t.Parallel()
	text := rulingEmitted(t)
	if !strings.Contains(text, "func GraphLoadMeasureParts(wire []byte) (data, attribution int64)") {
		t.Errorf("R5: a variable root carries no LoadMeasureParts")
	}
}

func TestRulingR9MaxBytesZeroIsNotTheDefaultGo(t *testing.T) {
	t.Parallel()
	text := rulingEmitted(t)
	if !strings.Contains(text, "if int64(n) > v.MaxBytes {") {
		t.Errorf("R9: the announcement byte bound is not the caller's MaxBytes alone")
	}
	if strings.Contains(text, "limit = 64 * 1024") {
		t.Errorf("R9: a MaxBytes of 0 still maps to the 64 KiB default")
	}
}

// TestRulingR5LoadMeasurePartsRunGo runs the generated Go: the parts sum to
// LoadMeasure, the attribution is one 16-byte entry per node record plus the
// root, and a refusal answers -1, 0.
func TestRulingR5LoadMeasurePartsRunGo(t *testing.T) {
	t.Parallel()
	u := unitOf(t, rulingSource)
	wire := discardWireOK(t, u, "Graph", `{"a":5,"name":"hello","next":{"a":6,"name":"x"}}`)
	src := fmt.Sprintf(`package probe

import "testing"

func TestParts(t *testing.T) {
	wire := %s
	total := GraphLoadMeasure(wire)
	data, attribution := GraphLoadMeasureParts(wire)
	if total <= 0 || data <= 0 || attribution <= 0 || data+attribution != total {
		t.Fatalf("parts do not sum: total=%%d data=%%d attribution=%%d", total, data, attribution)
	}
	if attribution %% 16 != 0 || attribution < 32 {
		t.Fatalf("attribution is not 16 bytes a node plus the root: %%d", attribution)
	}
	if d, a := GraphLoadMeasureParts(wire[:3]); d != -1 || a != 0 {
		t.Fatalf("a refusal answers -1, 0: %%d %%d", d, a)
	}
}
`, goBytes(wire))
	out, err := runGeneratedUnit(t, u, src, nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
