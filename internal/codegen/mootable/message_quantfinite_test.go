package mootable_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"math/bits"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE DEQUANTIZED FLOAT IS A FLOAT READ LIKE ANY OTHER (docs/SPEC-TABLES.md
// §3.3): every message-form read refuses a non-finite float, whatever packing
// the peer's announcement claims. A quantized f32 triple whose facts are each
// finite can still put its top step past the largest float32; the read
// refuses that value, into a float32 field and widened into a float64 one,
// and never delivers it. moo's verdict is held to the Go engine's
// (docs/rulings.json refusal-verdict-only: the verdict, never a reason).

// quantFiniteWriter is the peer's declaration: one quantized float32.
const quantFiniteWriter = "package edge\n\nfixed table Edge\n{\n    x float32 | min = 0.0, max = 1000.0, resolution = 1.0\n}\n"

// quantFiniteReaders are the builds that read the peer's batch: the same
// declaration, and one that widens the float32 into a float64.
var quantFiniteReaders = []struct{ name, src string }{
	{"float32", quantFiniteWriter},
	{"float64", "package edge\n\nfixed table Edge\n{\n    x float64\n}\n"},
}

// quantFiniteTriple is one announced quantized f32 triple, as it rides.
type quantFiniteTriple struct {
	name          string
	min, max, res float32
}

var quantFiniteTriples = []quantFiniteTriple{
	// every fact finite, 341 steps, the top step past the largest float32
	{"top_step_overflows", 3 * (1 << 103), math.MaxFloat32, 1e36},
	// max - min overflows float32
	{"delta_overflows", -3e38, 3e38, 1},
	{"infinite_range", float32(math.Inf(-1)), float32(math.Inf(1)), 1},
}

func quantFiniteBytes(min, max, res float32) []byte {
	out := binary.LittleEndian.AppendUint32(nil, math.Float32bits(min))
	out = binary.LittleEndian.AppendUint32(out, math.Float32bits(max))
	return binary.LittleEndian.AppendUint32(out, math.Float32bits(res))
}

// quantFiniteSteps is the triple's step count in float32, as the message form
// derives it (ir.TableMessageQuantization), or 0 for a non-conforming triple.
func quantFiniteSteps(min, max, res float32) uint64 {
	if !(min < max) || !(res > 0) {
		return 0
	}
	delta := max - min
	values := delta / res
	if math.IsInf(float64(delta), 0) || math.IsInf(float64(values), 0) || values != values {
		return 0
	}
	if values < 1 {
		values = 1
	}
	return uint64(math.Ceil(float64(values)))
}

// quantFiniteBatch is a batch of one body per step, each body the field's
// reference (the announcement's only entry, one bit), the step at the
// announced width, and the end reference.
func quantFiniteBatch(width int, steps ...uint64) []byte {
	out := []byte{0x02, byte(len(steps) - 1)}
	var acc uint64
	n := 0
	put := func(v uint64, w int) {
		for i := 0; i < w; i++ {
			acc |= ((v >> i) & 1) << n
			n++
			if n == 8 {
				out = append(out, byte(acc))
				acc, n = 0, 0
			}
		}
	}
	for _, s := range steps {
		put(1, 1)
		put(s, width)
		put(0, 1)
	}
	if n > 0 {
		out = append(out, byte(acc))
	}
	return out
}

// quantFiniteGo is the Go engine's verdict reading the batch into the reader
// build against the announcement: "refuse", or "accept" and the reader's file
// form of the first value.
func quantFiniteGo(t *testing.T, ub *ir.Unit, ann, msg []byte) string {
	t.Helper()
	var vocab tablewire.Vocabulary
	var rv tabletext.Report
	if err := vocab.AnnounceRead(ann, &rv); err != nil || rv.Refused {
		return "refuse"
	}
	mb := tabletext.NewModel(ub)
	var insts []*tabletext.Instance
	for i := 0; i < int(msg[1])+1; i++ {
		insts = append(insts, mb.New(ub.Tables["Edge"]))
	}
	var rb tabletext.Report
	n, ok, err := tablewire.DecodeMessages(mb, insts, msg, &vocab, &rb)
	if err != nil || !ok || rb.Refused {
		if n != 0 {
			t.Fatalf("the Go engine refused and delivered %d bodies", n)
		}
		return "refuse"
	}
	fileB, err := tablewire.Encode(mb, insts[0])
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("accept %x", fileB)
}

type quantFiniteRow struct {
	name   string
	reader *ir.Unit
	ann    []byte
	msg    []byte
	refuse bool
}

// quantFiniteRows are the hostile rows (each refused) and the control rows
// (an ordinary triple, an in-range step: each loads its value).
func quantFiniteRows(t *testing.T) []quantFiniteRow {
	t.Helper()
	own := tablewire.Announce(unitFromSource(t, quantFiniteWriter))
	declared := quantFiniteBytes(0, 1000, 1)
	if bytes.Count(own, declared) != 1 {
		t.Fatal("the unit's announcement does not carry the declared triple once")
	}
	var rows []quantFiniteRow
	for _, r := range quantFiniteReaders {
		ub := unitFromSource(t, r.src)
		for _, q := range quantFiniteTriples {
			ann := bytes.Replace(own, declared, quantFiniteBytes(q.min, q.max, q.res), 1)
			steps := quantFiniteSteps(q.min, q.max, q.res)
			width := 9
			if steps != 0 {
				width = bits.Len64(steps)
			}
			rows = append(rows,
				quantFiniteRow{r.name + "/" + q.name + "/top", ub, ann, quantFiniteBatch(width, steps), true},
				quantFiniteRow{r.name + "/" + q.name + "/bottom_then_top", ub, ann, quantFiniteBatch(width, 0, steps), true})
		}
		rows = append(rows, quantFiniteRow{r.name + "/control", ub, own, quantFiniteBatch(bits.Len64(1000), 500), false})
	}
	return rows
}

// quantFiniteMoo is moo's verdict for one row, in quantFiniteGo's terms.
func quantFiniteMoo(t *testing.T, row quantFiniteRow, files map[string][]byte) string {
	t.Helper()
	got := runEvolve(t, row.reader, files, "Edge", row.ann, row.msg)
	if strings.HasPrefix(got, "ANNOUNCE ") {
		return "refuse"
	}
	return got
}

func TestMooMessageQuantizedFloatIsFinite(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	for _, row := range quantFiniteRows(t) {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			want := quantFiniteGo(t, row.reader, row.ann, row.msg)
			if (want == "refuse") != row.refuse {
				t.Fatalf("the Go engine's verdict is %s, the row says refuse=%v", want, row.refuse)
			}
			files, err := generate(row.reader)
			if err != nil {
				t.Fatal(err)
			}
			if got := quantFiniteMoo(t, row, files); got != want {
				t.Fatalf("moo reading the batch:\nmoo: %s\ngo:  %s", got, want)
			}
		})
	}
}

// TestMooMessageQuantizedFloatIsFiniteNegativeControl plants the finite test
// out of the emitted runtime and requires the top step past the largest
// float32 to stop refusing.
func TestMooMessageQuantizedFloatIsFiniteNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_message.moo)")
	t.Parallel()
	const anchor = "\tif not V.finite32(bits) then\n"
	for _, row := range quantFiniteRows(t) {
		if row.name != "float64/top_step_overflows/top" {
			continue
		}
		files, err := generate(row.reader)
		if err != nil {
			t.Fatal(err)
		}
		src := string(files["table_message.moo"])
		if strings.Count(src, anchor) != 1 {
			t.Fatal("the finite anchor is not in table_message.moo exactly once")
		}
		files["table_message.moo"] = []byte(strings.Replace(src, anchor, "\tif false then\n", 1))
		if got := quantFiniteMoo(t, row, files); got == "refuse" {
			t.Fatal("the reader with the finite test planted out still refuses the top step")
		}
		return
	}
	t.Fatal("the top_step_overflows row is missing")
}
