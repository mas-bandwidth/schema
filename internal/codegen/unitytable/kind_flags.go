package unitytable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// kindFlags carries [Flags] enum : ulong fields (unsigned integer wire kind of
// the declared wire width).
type kindFlags struct{}

func init() { register(kindFlags{}) }

func (kindFlags) name() string    { return "flags" }
func (kindFlags) codec() int      { return 5 }
func (kindFlags) priority() int   { return 35 }
func (kindFlags) carried() bool   { return true }
func (kindFlags) runtime() string { return "" }

func (kindFlags) claims(f *ir.Field) bool {
	if !plainScalar(f) || f.Type.Kind != ir.TNamed {
		return false
	}
	_, ok := f.Type.Ref.(*ir.Flags)
	return ok
}

func (kindFlags) storage(f *ir.Field) string { return f.Type.Name }

func (kindFlags) width(f *ir.Field) int {
	return ir.TableKindWidth(ir.TableWireFieldKind(f))
}

func (kindFlags) defaultValue(f *ir.Field) string {
	fl, _ := f.Type.Ref.(*ir.Flags)
	if f.HasDefault && f.DefInt != nil && f.DefInt.Sign() != 0 {
		return flagsLitExpr(fl, f.DefInt)
	}
	return fmt.Sprintf("(%s)0", f.Type.Name)
}

func (kindFlags) defaultBits(f *ir.Field) uint64 {
	if f.HasDefault && f.DefInt != nil {
		return f.DefInt.Uint64()
	}
	return 0
}

func (k kindFlags) differs(f *ir.Field, v string) string {
	return fmt.Sprintf("(ulong)%s != 0x%016xUL", v, k.defaultBits(f))
}

func (kindFlags) emitAsserts(*strings.Builder, *ir.Field, string) {}

func (k kindFlags) emitPayload(b *strings.Builder, f *ir.Field, v string) {
	fmt.Fprintf(b, "TableWire.PutWidth(buffer, ref off, %d, (ulong)%s);", k.width(f), v)
}

func (kindFlags) emitRead(b *strings.Builder, f *ir.Field, v string) {
	fmt.Fprintf(b, "if (!TableWire.Int(body, ref off, kind, %d, ref report.Widened, out long flv)) { reason = TableReport.KindMismatch; goto done; }\n", ir.TableWireFieldKind(f))
	fmt.Fprintf(b, "%s = (%s)(ulong)flv;\n", v, f.Type.Name)
}
