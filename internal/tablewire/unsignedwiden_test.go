package tablewire_test

import (
	"encoding/binary"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The range belongs to the reader's declaration, not the incoming width.
// Re-masking after the clamp used to truncate this value back to u8/u16/u32.
func TestUnsignedWideningClampsAtDeclaredWidth(t *testing.T) {
	const source = `package widen
    table Root { value uint64 = 1099511627776 | min = 1099511627776, max = 2199023255552
    }
    `
	ast, parseErrs := parser.Parse("Wide.schema", []byte(source))
	if len(parseErrs) != 0 {
		t.Fatal(parseErrs)
	}
	unit, checkErrs := check.Unit([]check.SourceFile{{Path: "Wide.schema", Name: "Wide.schema", Base: "Wide", Bytes: []byte(source), AST: ast}})
	if len(checkErrs) != 0 {
		t.Fatal(checkErrs)
	}
	model := tabletext.NewModel(unit)
	for _, kind := range []int{ir.TableKindU8, ir.TableKindU16, ir.TableKindU32} {
		value := model.New(unit.Tables["Root"])
		wire := []byte{1, 1, byte(kind)}
		for i := 0; i < ir.TableKindWidth(kind); i++ {
			wire = append(wire, 255)
		}
		wire = append(wire, 0)
		wire = binary.LittleEndian.AppendUint64(wire, ir.TableFieldWireId(value.Fields[0].Def))
		wire = binary.LittleEndian.AppendUint64(wire, 1)
		var report tabletext.Report
		if ok, err := tablewire.Decode(model, value, wire, &report); !ok || err != nil {
			t.Fatalf("kind %d: decode %v, %v", kind, ok, err)
		}
		if got := value.Fields[0].Cell.U; got != 1<<40 {
			t.Fatalf("kind %d: clamped to %d, want %d", kind, got, uint64(1)<<40)
		}
		if report.Widened != 1 || report.Clamped != 1 || report.Malformed {
			t.Fatalf("kind %d: report %+v", kind, report)
		}
	}
}
