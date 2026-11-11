package gotable

// A LIST INSIDE A LIST ELEMENT, THROUGH THE MESSAGE FORM (docs/SPEC-TABLES.md
// §3.3): an array of a union whose arms are themselves arrays and a text is
// written INLINE inside the element loop, so the arm's loop and the
// element's loop are live in one body. The enum-keyed array of integers
// adds the keyed count loop and the keyed element loop, with one slot per
// sample left empty. Every sample puts every bounded array at its declared
// maximum, and rotates the arms so each arm kind sits at every element
// position.
//
// The Go engine (internal/tablewire) writes each sample as a one-body batch,
// the generated <Root>LoadMessages reads it, and the generated
// <Root>SaveMessages writes it back: the generated batch must equal the
// engine's, byte for byte.

import (
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// nestedArmProbe is the schema: a counted and a fixed array of a union whose
// arms are a float array, a text and an int array, an enum-keyed array of
// integers, and a counted array of a record holding an array. A keyed array
// of unions, and a keyed array as an arm, are refused by the checker, so a
// keyed loop is never nested in these bodies.
const nestedArmProbe = `package probe

enum Slot { Red, Blue, Green }

union Arm
{
    vals  [..2]float32
    label string(8)
    nums  [..3]int32
}

fixed table Row
{
    xs [..2]int16
}

fixed table Box
{
    arms  [..4]Arm
    pair  [2]Arm
    keyed [Slot]int16
    rows  [..3]Row
}
`

// nestedArmSamples answers one sample per rotation of the arms: element e of
// arms (e = 0..3) and of pair (e = 4..5) holds arm (e + k) % 3, every arm
// array at its bound, and every value distinct by element and position, so
// each element's bytes are its own. One keyed slot per sample is left at
// zero, so the slots that ride differ by sample.
func nestedArmSamples() map[string]string {
	out := map[string]string{}
	for k := range 3 {
		arm := func(e int) string {
			switch (e + k) % 3 {
			case 0:
				return fmt.Sprintf(`{"vals":[%d.5,%d.25]}`, 10*e+1, 10*e+2)
			case 1:
				label := make([]byte, 8)
				for j := range label {
					label[j] = byte('a' + e + 3*j)
				}
				return fmt.Sprintf(`{"label":%q}`, label)
			default:
				return fmt.Sprintf(`{"nums":[%d,%d,%d]}`, 100*e+1, 100*e+2, 100*e+3)
			}
		}
		var b strings.Builder
		b.WriteString(`{"arms":[`)
		for e := range 4 {
			if e > 0 {
				b.WriteString(",")
			}
			b.WriteString(arm(e))
		}
		b.WriteString(`],"pair":[`)
		for e := 4; e < 6; e++ {
			if e > 4 {
				b.WriteString(",")
			}
			b.WriteString(arm(e))
		}
		b.WriteString(`],"keyed":{`)
		for j, name := range []string{"Red", "Blue", "Green"} {
			if j > 0 {
				b.WriteString(",")
			}
			slot := 0
			if j != k {
				slot = 50 + 10*j + k
			}
			fmt.Fprintf(&b, "%q:%d", name, slot)
		}
		b.WriteString(`},"rows":[`)
		for e := range 3 {
			if e > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `{"xs":[%d,%d]}`, 10*e+k+1, 10*e+k+2)
		}
		b.WriteString(`]}`)
		out[fmt.Sprintf("rotate%d", k)] = b.String()
	}
	return out
}

func TestGoMessageFormNestedArmLoops(t *testing.T) {
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	t.Parallel()
	u := unitFrom(t, nestedArmProbe)
	m := tabletext.NewModel(u)
	st := m.Lookup("Box")
	if st == nil {
		t.Fatal("no root Box")
	}
	samples := nestedArmSamples()
	var src strings.Builder
	src.WriteString(`package probe

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestNestedArmLoops(t *testing.T) {
	vocab := new(TableVocabulary)
	vocab.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	ann := make([]byte, AnnounceMeasure())
	if Announce(ann) != int64(len(ann)) {
		t.Fatal("announce")
	}
	var ar TableReport
	if !AnnounceRead(vocab, ann, &ar) {
		t.Fatalf("announce read %+v", ar)
	}
	for _, c := range []struct{ name, batch string }{
`)
	for _, name := range slices.Sorted(maps.Keys(samples)) {
		inst := m.New(st)
		var rep tabletext.Report
		if !m.Read(inst, []byte(samples[name]), &rep) || rep != (tabletext.Report{}) {
			t.Fatalf("%s: the engine did not read the sample cleanly: %+v", name, rep)
		}
		batch, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
		if err != nil {
			t.Fatalf("%s: the engine did not write the batch: %v", name, err)
		}
		fmt.Fprintf(&src, "\t\t{%q, %q},\n", name, hex.EncodeToString(batch))
	}
	src.WriteString(`	} {
		batch, _ := hex.DecodeString(c.batch)
		var got [1]Box
		var lr TableReport
		count, ok := BoxLoadMessages(got[:], vocab, batch, &lr)
		if !ok || count != 1 || lr != (TableReport{}) {
			t.Errorf("%s: load %d %v %+v", c.name, count, ok, lr)
			continue
		}
		values := []*Box{&got[0]}
		n := BoxMeasureMessages(values, nil)
		if n < 0 {
			t.Errorf("%s: measure %d", c.name, n)
			continue
		}
		out := make([]byte, n)
		var wr TableReport
		if w := BoxSaveMessages(values, out, &wr); w != n || wr != (TableReport{}) {
			t.Errorf("%s: save %d of %d %+v", c.name, w, n, wr)
			continue
		}
		if !bytes.Equal(out, batch) {
			t.Errorf("%s: the saved batch differs from the engine's (%d bytes against %d)", c.name, len(out), len(batch))
		}
	}
}
`)
	if out, err := runGoGoldensEdited(t, u, src.String(), nil); err != nil {
		t.Fatalf("the generated round trip failed: %v\n%s", err, out)
	}
}
