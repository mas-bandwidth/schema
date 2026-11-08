package tablewire

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE ONE READ'S VERDICT DOES NOT DEPEND ON DETECTION ORDER
// (docs/SPEC-TABLES.md §6.5; tla/ReadOrder.tla, OneVerdict). Every input,
// valid or mutated, is read in several orders: the wire-alone conditions
// before the declaration's and after them, and the node records in index
// order, in reverse and shuffled. Every order answers the same verdict, and
// where it accepts, the same value, shown by byte-identical write-back.
//
// Beside it, the region measure's two implications (§6.5, "EVERY PATH AGREES
// ON THE OUTCOME"): a measure that refuses implies a read that refuses, and a
// read that accepts implies a measure that answers a size.

const oneVerdictUnit = `package oneverdict

enum Mode { Idle, Run, Fly }

table Leaf
{
    n     int32 | min = -100, max = 100
    label string(8)
    next  *Leaf
}

union Shape
{
    leaf  Leaf
    count int16
    name  string(6)
}

table Root
{
    flag   bool
    mode   Mode
    ratio  float32 | min = 0, max = 1, resolution = 0.01
    counts [..4]uint8
    by     [Mode]int32
    named  map[uint8]Leaf
    shape  Shape
    shapes []Shape
    head   *Leaf
    other  *Leaf
    blob   *bytes
    words  [..3]string(5)
}
`

var oneVerdictSeeds = []string{
	`{}`,
	`{"flag":true,"mode":"Run","ratio":0.25,"counts":[1,2,3],"by":{"Fly":7},"words":["ab","cde"]}`,
	`{"named":{"1":{"n":5,"label":"x"},"9":{"n":-5}},"shape":{"leaf":{"n":3}},"shapes":[{"count":4},{"name":"hi"},{}]}`,
	`{"head":{"&node":1,"n":1,"label":"a","next":{"n":2,"next":{"n":3}}},"other":{"&node":1},"blob":"AAECAw=="}`,
	`{"head":{"n":7,"next":{"n":8}},"other":{"n":9},"shape":{"name":"abc"},"mode":"Fly","by":{"Idle":1,"Run":2}}`,
}

// oneVerdictOrders is every order a read is held to.
func oneVerdictOrders(rng *rand.Rand) []readOrder {
	reverse := func(n int) []int {
		out := make([]int, n)
		for i := range out {
			out[i] = n - 1 - i
		}
		return out
	}
	shuffle := func(n int) []int { return rng.Perm(n) }
	return []readOrder{
		{},
		{wireLast: true},
		{records: reverse},
		{wireLast: true, records: shuffle},
		{records: shuffle},
	}
}

type oneVerdictAnswer struct {
	accepted bool
	value    []byte
}

func oneVerdictRead(t *testing.T, m *tabletext.Model, root *ir.Struct, data []byte, order readOrder) oneVerdictAnswer {
	t.Helper()
	inst := m.New(root)
	var report tabletext.Report
	if err := decodeWalk(m, inst, data, &report, nil, order); err != nil {
		if !ReadRefusal(err) {
			t.Fatalf("a read answered an error that is not a refusal: %v", err)
		}
		return oneVerdictAnswer{}
	}
	value, err := Encode(m, inst)
	if err != nil {
		t.Fatalf("an accepted value does not write back: %v", err)
	}
	return oneVerdictAnswer{accepted: true, value: value}
}

// oneVerdictMutants is the seed and a deterministic set of mutants of it: a
// byte flipped, set to 0 or 0xFF, inserted and deleted, across the body.
func oneVerdictMutants(rng *rand.Rand, wire []byte, n int) [][]byte {
	out := [][]byte{wire}
	for range n {
		b := append([]byte(nil), wire...)
		at := 1 + rng.Intn(len(b)-1)
		switch rng.Intn(5) {
		case 0:
			b[at] ^= byte(1 << rng.Intn(8))
		case 1:
			b[at] = 0
		case 2:
			b[at] = 0xFF
		case 3:
			b = append(b[:at], append([]byte{byte(rng.Intn(256))}, b[at:]...)...)
		default:
			b = append(b[:at], b[at+1:]...)
		}
		out = append(out, b)
	}
	return out
}

func TestOneVerdictInEveryDetectionOrder(t *testing.T) {
	t.Parallel()
	m := covModel(t, oneVerdictUnit)
	root := m.Lookup("Root")
	rng := rand.New(rand.NewSource(20261103))
	accepted, refused := 0, 0
	for _, seed := range oneVerdictSeeds {
		wire, err := Encode(m, covPlace(t, m, "Root", seed))
		if err != nil {
			t.Fatalf("%s: %v", seed, err)
		}
		for _, data := range oneVerdictMutants(rng, wire, 600) {
			var first oneVerdictAnswer
			for i, order := range oneVerdictOrders(rng) {
				got := oneVerdictRead(t, m, root, data, order)
				if i == 0 {
					first = got
					continue
				}
				if got.accepted != first.accepted || !bytes.Equal(got.value, first.value) {
					t.Fatalf("order %d answers %v %x, the read's own order %v %x, on %x", i, got.accepted, got.value, first.accepted, first.value, data)
				}
			}
			if first.accepted {
				accepted++
			} else {
				refused++
			}
			// THE MEASURE'S TWO IMPLICATIONS
			size, whole, measureRefused := FileRegionMeasure(m.Unit, root, data)
			if (measureRefused || !whole) && first.accepted {
				t.Fatalf("the measure refused (%d %v %v) a wire the read accepts: %x", size, whole, measureRefused, data)
			}
			if first.accepted && size < 0 {
				t.Fatalf("an accepted read has no region size: %x", data)
			}
		}
	}
	t.Logf("accepted %d, refused %d", accepted, refused)
	// the property is not vacuous: both verdicts occur
	if accepted == 0 || refused == 0 {
		t.Fatalf("accepted %d, refused %d: the mutants exercise one verdict only", accepted, refused)
	}
}

// THE NEGATIVE CONTROL: a reader that drops the wire-alone phase, checking
// only what its declaration reaches (tla/ReadOrder.tla's KnownSlotsOnly
// witness), answers a different verdict on some input, so the property above
// can see a reader that is order-dependent in the way the model forbids.
func TestOneVerdictNegativeControl(t *testing.T) {
	t.Parallel()
	m := covModel(t, oneVerdictUnit)
	root := m.Lookup("Root")
	// an unknown field carrying a bool byte of 2: damage the wire alone shows,
	// in bytes no declaration of this reader reaches
	wire, err := Encode(m, covPlace(t, m, "Root", `{"mode":"Run"}`))
	if err != nil {
		t.Fatal(err)
	}
	body, ids, ok := trailer(wire)
	if !ok {
		t.Fatal("trailer")
	}
	unknown := ir.TableWireId("not_a_field")
	ids = append(ids, unknown)
	damaged := append([]byte{ir.TableWireForm}, body[:len(body)-1]...)
	damaged = append(damaged, byte(len(ids)), ir.TableKindBool, 2, 0)
	for _, id := range ids {
		damaged = appendU64(damaged, id)
	}
	damaged = appendU64(damaged, uint64(len(ids)))

	if got := oneVerdictRead(t, m, root, damaged, readOrder{}); got.accepted {
		t.Fatal("the read accepted damage inside an unknown field")
	}
	inst := m.New(root)
	var report tabletext.Report
	if err := decodeValue(m, inst, damaged[1:len(damaged)-8*(len(ids)+1)], ids, &report, nil, readOrder{}); err != nil {
		t.Fatalf("the declaration-only reader refused too, so the control shows nothing: %v", err)
	}
}

func appendU64(b []byte, v uint64) []byte {
	for i := range 8 {
		b = append(b, byte(v>>(8*i)))
	}
	return b
}
