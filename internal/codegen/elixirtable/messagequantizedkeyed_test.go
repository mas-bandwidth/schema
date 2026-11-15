package elixirtable

// A MESSAGE BODY CARRIES A QUANTIZED FLOAT32 AND AN ENUM-KEYED ARRAY
// (docs/SPEC-TABLES.md §3.3): the float rides its step index in
// bits_required(0, count) bits (packing 2), and the keyed array rides the
// number of present slots then one (key reference, element) pair per slot
// (kind 16). A fixed table with both round-trips through the message form to
// the Go reference leg's bytes: save_messages writes the Go bytes, and
// load_messages decodes them back to the value save_messages took.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func elixirFloat(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

func TestMessageQuantizedFloatAndEnumKeyedArray(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe

enum Slot { Alpha, Beta, Gamma }

fixed table Root
{
    cf     float32 | min = 0.0, max = 10.0, resolution = 0.01
    tokens [Slot]int32
}
`)
	slots := tablewire.MessageVocabularySlots(u)

	var cfField, tokensField *ir.Field
	for _, f := range u.Tables["Root"].Fields {
		switch f.Name {
		case "cf":
			cfField = f
		case "tokens":
			tokensField = f
		}
	}
	if cfField == nil || tokensField == nil {
		t.Fatalf("Root declares no cf and tokens fields: %+v", u.Tables["Root"].Fields)
	}
	cfShape := ir.TableFieldEntry(cfField).Shape
	cfSlot := slots[ir.TableFieldEntry(cfField).Key()]
	tokensSlot := slots[ir.TableFieldEntry(tokensField).Key()]
	keySlot := func(name string) uint64 {
		e := ir.TableVocabularyEntry{Id: ir.TableWireId(tokensField.KeyEnumRef.VariantWireNameOf(name))}
		s, ok := slots[e.Key()]
		if !ok {
			t.Fatalf("the unit announces no slot for %s", name)
		}
		return s
	}
	alphaRef, gammaRef := keySlot("Alpha"), keySlot("Gamma")

	// the Go reference leg's bytes for one body carrying both fields
	model := tabletext.NewModel(u)
	inst := model.New(model.Lookup("Root"))
	var report tabletext.Report
	if !model.Read(inst, []byte(`{"cf":3.5,"tokens":{"Alpha":101,"Gamma":303}}`), &report) || !report.Silent() {
		t.Fatalf("source: %+v", report)
	}
	batch, err := tablewire.EncodeMessages(model, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}

	// the value the float's step index names
	want := float64(ir.TableMessageDequantize(cfShape, ir.TableMessageQuantize(cfShape, float32(3.5))))

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "batch.bin"), batch, 0o600); err != nil {
		t.Fatal(err)
	}
	ann := filepath.Join(dir, "announce.bin")

	input := fmt.Sprintf("[[{%d, %s}, {%d, [{%d, 101}, {%d, 303}]}]]",
		cfSlot, elixirFloat(3.5), tokensSlot, alphaRef, gammaRef)
	expected := fmt.Sprintf("[[{%d, %s}, {%d, [{%d, 101}, {%d, 303}]}]]",
		cfSlot, elixirFloat(want), tokensSlot, alphaRef, gammaRef)

	body := fmt.Sprintf(`    vocab =
      case M.announce_read(M.announce()) do
        {:ok, v} -> v
        other -> raise "announce_read refused: #{inspect(other)}"
      end

    batch = File.read!(golden("batch.bin"))
    input = %s
    expected = %s

    case M.save_messages(vocab, input) do
      {:ok, got} -> check(got == batch, "save_messages differs from the Go leg's bytes")
      other -> check(false, "save_messages refused: #{inspect(other)}")
    end

    case M.load_messages(vocab, batch) do
      {:ok, values} ->
        check(values == expected, "load_messages did not decode to the value save_messages took")

        case M.save_messages(vocab, values) do
          {:ok, again} -> check(again == batch, "save(load) did not reproduce the batch")
          other -> check(false, "save(load) refused: #{inspect(other)}")
        end

      other -> check(false, "load_messages refused: #{inspect(other)}")
    end
`, input, expected)

	out, err := runMessageProbe(t, u, ann, body)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
