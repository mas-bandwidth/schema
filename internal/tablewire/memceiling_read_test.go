package tablewire

import (
	"encoding/binary"
	"math/rand"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/memceiling"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE ONE READ ALLOCATES IN PROPORTION TO THE BYTES IT IS HANDED, never to a
// count a wire states (docs/SPEC-TABLES.md §6.5, §11): every count the wire
// check meets is walked element by element against the bytes that carry it,
// so a hostile count costs what its bytes cost. Each wire below states a count
// or a length near the int32 cap in a few bytes; the reads run under a heap
// ceiling far below what one such count would command, and the mutants of
// oneVerdictUnit run under it too.
func TestTheOneReadAllocatesNothingAWireCommands(t *testing.T) {
	m := covModel(t, oneVerdictUnit)
	root := m.Lookup("Root")
	field := func(name string) *ir.Field {
		for _, f := range root.Fields {
			if f.Name == name {
				return f
			}
		}
		t.Fatalf("no field %s", name)
		return nil
	}
	// one field under its own id: form, reference 1, kind, the payload, the
	// terminator, then the one-entry id table
	wire := func(f *ir.Field, kind byte, payload []byte) []byte {
		b := []byte{ir.TableWireForm, 1, kind}
		b = binary.AppendUvarint(b, uint64(len(payload)))
		b = append(b, payload...)
		b = append(b, 0)
		b = binary.LittleEndian.AppendUint64(b, ir.TableFieldWireId(f))
		return binary.LittleEndian.AppendUint64(b, 1)
	}
	header := func(ek byte, count uint64, rest ...byte) []byte {
		return append(binary.AppendUvarint([]byte{ek}, count), rest...)
	}
	hostile := map[string][]byte{
		"list count at the cap":         wire(field("shapes"), ir.TableKindArray, header(ir.TableKindUnion, 1<<31-1, 0)),
		"list count past the cap":       wire(field("shapes"), ir.TableKindArray, header(ir.TableKindUnion, 1<<40, 0)),
		"map count at the cap":          wire(field("named"), ir.TableKindArray, header(ir.TableKindTable, 1<<31-1, 1, 0)),
		"keyed count at the cap":        wire(field("by"), ir.TableKindKeyed, header(ir.TableKindI32, 1<<31-1, 1, 4, 0, 0, 0, 0)),
		"payload-free elements":         wire(field("shapes"), ir.TableKindArray, header(ir.TableKindNoPayload, 1<<31-1, 0, 0, 0)),
		"bounded array past its bound":  wire(field("counts"), ir.TableKindArray, header(ir.TableKindU8, 1<<31-1, 1, 2, 3)),
		"string length past the buffer": wire(field("words"), ir.TableKindArray, header(ir.TableKindString, 1, 0xFF, 0xFF, 0xFF, 0xFF, 0x07)),
	}
	stop := memceiling.Heap(64 << 20)
	for name, data := range hostile {
		var report tabletext.Report
		if ok, err := Decode(m, m.New(root), data, &report); ok || !ReadRefusal(err) {
			t.Errorf("%s: %v %v, want a refusal", name, ok, err)
		}
	}
	rng := rand.New(rand.NewSource(7))
	for _, seed := range oneVerdictSeeds {
		w, err := Encode(m, covPlace(t, m, "Root", seed))
		if err != nil {
			t.Fatal(err)
		}
		for _, data := range oneVerdictMutants(rng, w, 400) {
			var report tabletext.Report
			if _, err := Decode(m, m.New(root), data, &report); err != nil && !ReadRefusal(err) {
				t.Fatalf("a mutant answered an error that is not a refusal: %v", err)
			}
		}
	}
	if used := stop(); used > 64<<20 {
		t.Fatalf("the reads allocated %d bytes", used)
	}
}
