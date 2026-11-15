package tablewire

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Kinds 20–29 announced RAW ride at the kind's storage width, two's complement
// for the signed kinds (docs/SPEC-TABLES.md §3.3). The checker requires bounds
// on a fixed field; they are cleared so the announcement is packing RAW, which
// is the row under test. A one-field vocabulary makes the reference one bit,
// which is the framing of the canonical bodies below.
var rawFixedKinds = []struct {
	kind int
	decl string
	lo   string
	hi   string
	body string // canonical batch of raw -16; empty when the kind is unsigned
}{
	{ir.TableKindFixed8, "fixed(8, 0)", "-128", "127", "0200e101"},
	{ir.TableKindFixed16, "fixed(16, 0)", "-32768", "32767", "0200e1ff01"},
	{ir.TableKindFixed32, "fixed(32, 0)", "-1000", "1000", "0200e1ffffff01"},
	{ir.TableKindFixed64, "fixed(64, 0)", "-1000", "1000", "0200e1ffffffffffffff01"},
	{ir.TableKindFixed128, "fixed(128, 0)", "-1000", "1000", "0200e1ffffffffffffffffffffffffffffff01"},
	{ir.TableKindUFixed8, "ufixed(8, 0)", "0", "255", ""},
	{ir.TableKindUFixed16, "ufixed(16, 0)", "0", "65535", ""},
	{ir.TableKindUFixed32, "ufixed(32, 0)", "0", "1000", ""},
	{ir.TableKindUFixed64, "ufixed(64, 0)", "0", "1000", ""},
	{ir.TableKindUFixed128, "ufixed(128, 0)", "0", "1000", ""},
}

func TestMessageRawFixedReadsAtStorageWidth(t *testing.T) {
	t.Parallel()
	for _, tc := range rawFixedKinds {
		t.Run(fmt.Sprintf("kind-%d", tc.kind), func(t *testing.T) {
			t.Parallel()
			m, v := rawFixedVocabulary(t, tc.decl, tc.lo, tc.hi, tc.kind)
			if tc.body != "" {
				t.Run("canonical", func(t *testing.T) {
					raw, err := hex.DecodeString(tc.body)
					if err != nil {
						t.Fatal(err)
					}
					rawFixedEqual(t, rawFixedLoad(t, m, v, raw), -16)
				})
			}
			values := []int64{16}
			if ir.TableKindSigned(tc.kind) {
				values = append([]int64{-16}, values...)
			}
			for _, value := range values {
				t.Run(fmt.Sprintf("%d", value), func(t *testing.T) {
					wire := rawFixedSave(t, m, value)
					if value == -16 && tc.body != "" && hex.EncodeToString(wire) != tc.body {
						t.Fatalf("kind %d wrote %s, want %s", tc.kind, hex.EncodeToString(wire), tc.body)
					}
					rawFixedEqual(t, rawFixedLoad(t, m, v, wire), value)
				})
			}
		})
	}
}

func rawFixedVocabulary(t *testing.T, decl, lo, hi string, kind int) (*tabletext.Model, *Vocabulary) {
	t.Helper()
	src := fmt.Sprintf("package rawfixed\n\nfixed table Probe\n{\n    v %s | min = %s, max = %s\n}\n", decl, lo, hi)
	m := covModel(t, src)
	st := m.Lookup("Probe")
	if st == nil || len(st.Fields) != 1 {
		t.Fatalf("Probe: %v", st)
	}
	f := st.Fields[0]
	// Bounds are part of the declaration the checker accepts. Clearing them
	// announces packing RAW: the storage width, and nothing beside it.
	f.HasIntRange = false
	f.IntMin = nil
	f.IntMax = nil
	entry := ir.TableFieldEntry(f)
	vocab := MessageVocabulary(m.Unit)
	if int(entry.Kind) != kind || entry.Shape.Packing != ir.TableMessageRaw || len(vocab) != 1 {
		t.Fatalf("kind %d announced kind %d packing %d over %d entries", kind, entry.Kind, entry.Shape.Packing, len(vocab))
	}
	v := &Vocabulary{entries: vocab, announced: true}
	if v.RefBits() != 1 {
		t.Fatalf("ref bits %d, want 1", v.RefBits())
	}
	return m, v
}

func rawFixedSave(t *testing.T, m *tabletext.Model, value int64) []byte {
	t.Helper()
	inst := m.New(m.Lookup("Probe"))
	inst.Fields[0].Cell.Wide = big.NewInt(value)
	wire, err := EncodeMessage(m, inst)
	if err != nil {
		t.Fatalf("save %d: %v", value, err)
	}
	return wire
}

func rawFixedLoad(t *testing.T, m *tabletext.Model, v *Vocabulary, wire []byte) *big.Int {
	t.Helper()
	inst := m.New(m.Lookup("Probe"))
	var report tabletext.Report
	ok, err := DecodeMessage(m, inst, wire, v, &report)
	if !ok || err != nil || report.Refused {
		t.Fatalf("load refused: ok=%v err=%v report=%+v wire=%x", ok, err, report, wire)
	}
	got := inst.Fields[0].Cell.Wide
	if got == nil {
		return new(big.Int)
	}
	return got
}

func rawFixedEqual(t *testing.T, got *big.Int, want int64) {
	t.Helper()
	if got.Cmp(big.NewInt(want)) != 0 {
		t.Fatalf("got %s, want %d", got, want)
	}
}
