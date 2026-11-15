package elixirtable

// THE MESSAGE WRITER REFUSES A SCALAR ITS SHAPE CANNOT CARRY (docs/SPEC.md
// §5.1, the Elixir row: "SaveMessages: 1–6 and 8 every build";
// docs/SPEC-TABLES.md §3.3). A ranged scalar rides as `value - base` in its
// announced bits and an unranged integer at its storage width; a value outside
// [base, base + 2^bits - 1], or outside its width's signed or unsigned range,
// is refused by save_messages and never masked into the bits. The value at
// each boundary still writes and reads back equal. The rows are taken from the
// unit's own announced vocabulary, never a copied list.

import (
	"fmt"
	"math/big"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// scalarOffShapeRow is one entry under test: its reference, the values that
// must be refused, and the boundary values that must round-trip. An array
// row wraps each value as a one-element list.
type scalarOffShapeRow struct {
	what    string
	ref     int
	array   bool
	refused []*big.Int
	kept    []*big.Int
}

func TestElixirSaveMessagesRefusesScalarOffShape(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "an external toolchain")
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := messageLoadUnit(t, root, []string{"tables/examples"})
	vocab := tablewire.MessageVocabulary(u)

	signedKind := func(k uint8) bool { return k >= ir.TableKindI8 && k <= ir.TableKindI64 }
	intKind := func(k uint8) bool { return k >= ir.TableKindI8 && k <= ir.TableKindU64 }
	storageBits := func(k uint8) uint {
		switch k {
		case ir.TableKindI8, ir.TableKindU8:
			return 8
		case ir.TableKindI16, ir.TableKindU16:
			return 16
		case ir.TableKindI32, ir.TableKindU32:
			return 32
		}
		return 64
	}
	find := func(what string, match func(e ir.TableVocabularyEntry) bool) (int, ir.TableVocabularyEntry) {
		for i, e := range vocab {
			if match(e) {
				return i + 1, e
			}
		}
		t.Fatalf("the unit announces no %s", what)
		return 0, ir.TableVocabularyEntry{}
	}
	one := big.NewInt(1)
	ranged := func(what string, ref int, e ir.TableVocabularyEntry) scalarOffShapeRow {
		base := new(big.Int).Set(e.Shape.Base)
		top := new(big.Int).Add(base, new(big.Int).Sub(new(big.Int).Lsh(one, uint(e.Shape.Bits)), one))
		return scalarOffShapeRow{
			what:    what,
			ref:     ref,
			refused: []*big.Int{new(big.Int).Add(top, one), new(big.Int).Sub(base, one)},
			kept:    []*big.Int{base, top},
		}
	}
	signedRange := func(bits uint) (lo, hi *big.Int) {
		hi = new(big.Int).Sub(new(big.Int).Lsh(one, bits-1), one)
		lo = new(big.Int).Neg(new(big.Int).Lsh(one, bits-1))
		return lo, hi
	}

	// The write contract (docs/SPEC.md §5.1) also refuses a value off the
	// declared range, which can be narrower than the bits the shape announces
	// ([0, 100] rides in 7 bits). This test is about the SHAPE's bounds, so a
	// ranged row is taken only where the declaration admits the whole span
	// [base, base + 2^bits - 1]: there the shape check alone decides.
	declared := declaredIntSpans(u)
	spanDeclared := func(e ir.TableVocabularyEntry) bool {
		d, ok := declared[[2]uint64{e.Id, uint64(e.Kind)}]
		if !ok {
			return true
		}
		top := new(big.Int).Add(e.Shape.Base, new(big.Int).Sub(new(big.Int).Lsh(one, uint(e.Shape.Bits)), one))
		return d[0].Cmp(e.Shape.Base) <= 0 && d[1].Cmp(top) >= 0
	}

	var rows []scalarOffShapeRow

	ref, e := find("ranged integer at base 0", func(e ir.TableVocabularyEntry) bool {
		return intKind(e.Kind) && e.Shape.Packing == ir.TableMessageRanged && e.Shape.Base != nil &&
			e.Shape.Base.Sign() == 0 && e.Shape.Bits > 0 && e.Shape.Bits < 64 && spanDeclared(e)
	})
	rows = append(rows, ranged("ranged integer at base 0", ref, e))

	ref, e = find("ranged integer below zero", func(e ir.TableVocabularyEntry) bool {
		return signedKind(e.Kind) && e.Shape.Packing == ir.TableMessageRanged && e.Shape.Base != nil &&
			e.Shape.Base.Sign() < 0 && e.Shape.Bits > 0 && e.Shape.Bits < 64 && spanDeclared(e)
	})
	rows = append(rows, ranged("ranged integer below zero", ref, e))

	ref, e = find("unranged signed integer", func(e ir.TableVocabularyEntry) bool {
		return signedKind(e.Kind) && e.Shape.Packing != ir.TableMessageRanged
	})
	lo, hi := signedRange(storageBits(e.Kind))
	rows = append(rows, scalarOffShapeRow{
		what:    "unranged signed integer",
		ref:     ref,
		refused: []*big.Int{new(big.Int).Add(hi, one), new(big.Int).Sub(lo, one)},
		kept:    []*big.Int{lo, hi},
	})

	ref, e = find("array of signed integers", func(e ir.TableVocabularyEntry) bool {
		return e.Kind == ir.TableKindArray && signedKind(e.Shape.Elem) && e.Shape.Max >= 1 && e.Shape.Min <= 1
	})
	var elem scalarOffShapeRow
	if e.Shape.Inner != nil && e.Shape.Inner.Packing == ir.TableMessageRanged {
		elem = ranged("array element, ranged integer", ref, ir.TableVocabularyEntry{Kind: e.Shape.Elem, Shape: *e.Shape.Inner})
	} else {
		lo, hi = new(big.Int), new(big.Int).Sub(new(big.Int).Lsh(one, storageBits(e.Shape.Elem)), one)
		if signedKind(e.Shape.Elem) {
			lo, hi = signedRange(storageBits(e.Shape.Elem))
		}
		elem = scalarOffShapeRow{
			what:    "array element, unranged integer",
			ref:     ref,
			refused: []*big.Int{new(big.Int).Add(hi, one), new(big.Int).Sub(lo, one)},
			kept:    []*big.Int{lo, hi},
		}
	}
	elem.array = true
	rows = append(rows, elem)

	probeScalarOffShape(t, u, rows)

	// A SIGNED WIDE BASE IS TWO'S COMPLEMENT (docs/SPEC-TABLES.md §3.3): a
	// ranged int128 or signed fixed-point announces its base as sixteen bytes,
	// and a base below zero is the lower bound the writer and the reader hold
	// the value to, never a value near 2^128.
	u = messageLoadUnit(t, root, []string{"tables/scalars"})
	vocab = tablewire.MessageVocabulary(u)
	declared = declaredIntSpans(u)
	// The declared range of an int128 here ([-2^100, 2^100]) is narrower than
	// any power-of-two span, so the kept boundaries are the shape's held to the
	// declaration: the base below zero, and the top the write contract admits.
	// The shape-off values stay refused either way.
	clamp := func(row scalarOffShapeRow, e ir.TableVocabularyEntry) scalarOffShapeRow {
		d, ok := declared[[2]uint64{e.Id, uint64(e.Kind)}]
		if !ok {
			return row
		}
		var kept []*big.Int
		for _, v := range row.kept {
			if v.Cmp(d[0]) < 0 {
				v = d[0]
			}
			if v.Cmp(d[1]) > 0 {
				v = d[1]
			}
			kept = append(kept, v)
		}
		row.kept = kept
		return row
	}
	rows = nil
	for _, k := range []struct {
		what string
		kind func(k uint8) bool
	}{
		{"ranged signed fixed-point below zero", func(k uint8) bool { return k >= ir.TableKindFixed8 && k <= ir.TableKindFixed128 }},
		{"ranged int128 below zero", func(k uint8) bool { return k == ir.TableKindI128 }},
	} {
		n := len(rows)
		for i, e := range vocab {
			if k.kind(e.Kind) && e.Shape.Packing == ir.TableMessageRanged && e.Shape.Base != nil &&
				e.Shape.Base.Sign() < 0 && e.Shape.Bits > 0 {
				rows = append(rows, clamp(ranged(k.what, i+1, e), e))
			}
		}
		if len(rows) == n {
			t.Fatalf("the unit announces no %s", k.what)
		}
	}
	probeScalarOffShape(t, u, rows)
}

// declaredIntSpans is each integer field's declared write domain keyed by its
// vocabulary {id, kind}, over the message closure's fields and union arms the
// way messageWriteRuleRows walks them; declarations sharing a key merge to the
// looser span.
func declaredIntSpans(u *ir.Unit) map[[2]uint64][2]*big.Int {
	spans := map[[2]uint64][2]*big.Int{}
	add := func(e ir.TableVocabularyEntry, f *ir.Field) {
		lo, hi, ok := messageIntBounds(f)
		if !ok {
			return
		}
		k := [2]uint64{e.Id, uint64(e.Kind)}
		if old, seen := spans[k]; seen {
			if old[0].Cmp(lo) < 0 {
				lo = old[0]
			}
			if old[1].Cmp(hi) > 0 {
				hi = old[1]
			}
		}
		spans[k] = [2]*big.Int{lo, hi}
	}
	for name := range tablewire.MessageClosure(u) {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			add(ir.TableFieldEntry(f), f)
			if un, ok := f.Type.Ref.(*ir.Union); ok && f.Type.Kind == ir.TNamed {
				for _, v := range un.Variants {
					if v.F != nil {
						add(ir.TableArmEntry(v), v.F)
					}
				}
			}
		}
	}
	return spans
}

// probeScalarOffShape runs each row against the unit's generated
// TableMessage: every refused value is refused by save_messages, and every
// kept value writes and reads back equal.
func probeScalarOffShape(t *testing.T, u *ir.Unit, rows []scalarOffShapeRow) {
	t.Helper()
	var body strings.Builder
	body.WriteString(`    vocab =
      case M.announce_read(M.announce()) do
        {:ok, v} -> v
        other -> raise "announce_read refused the unit's own announcement: #{inspect(other)}"
      end
`)
	for _, row := range rows {
		for _, v := range row.refused {
			value := v.String()
			if row.array {
				value = "[" + value + "]"
			}
			fmt.Fprintf(&body, "\n    check(match?({:error, _}, M.save_messages(vocab, [[{%d, %s}]])), %q)\n",
				row.ref, value, fmt.Sprintf("%s: save_messages wrote %s", row.what, value))
		}
		for _, v := range row.kept {
			value := v.String()
			if row.array {
				value = "[" + value + "]"
			}
			fmt.Fprintf(&body, `
    case M.save_messages(vocab, [[{%[1]d, %[2]s}]]) do
      {:ok, bytes} ->
        check(M.load_messages(vocab, bytes) == {:ok, [[{%[1]d, %[2]s}]]}, %[3]q)

      _ ->
        check(false, %[4]q)
    end
`, row.ref, value,
				fmt.Sprintf("%s: %s did not read back equal", row.what, value),
				fmt.Sprintf("%s: save_messages refused the boundary %s", row.what, value))
		}
	}

	ann := filepath.Join(t.TempDir(), "announce.bin")
	out, err := runMessageProbe(t, u, ann, body.String())
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
