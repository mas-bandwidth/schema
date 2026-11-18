package unitytable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// kindBool carries a lone `bool`: kind 1, one byte that is 0 or 1
// (docs/SPEC-TABLES.md §3). The wire check has already refused any other byte,
// so the read only has to hold the kind.
type kindBool struct{}

func init() { register(kindBool{}) }

func (kindBool) name() string    { return "bool" }
func (kindBool) codec() int      { return 1 }
func (kindBool) priority() int   { return 10 }
func (kindBool) carried() bool   { return true }
func (kindBool) runtime() string { return "" }

func (kindBool) claims(f *ir.Field) bool {
	return plainScalar(f) && f.Type.Kind == ir.TBool
}

func (kindBool) storage(*ir.Field) string { return "bool" }

func (kindBool) defaultValue(f *ir.Field) string {
	if f.HasDefault && f.DefBool {
		return "true"
	}
	return "false"
}

func (kindBool) defaultBits(f *ir.Field) uint64 {
	if f.HasDefault && f.DefBool {
		return 1
	}
	return 0
}

func (k kindBool) differs(f *ir.Field, v string) string {
	return fmt.Sprintf("%s != %s", v, k.defaultValue(f))
}

func (kindBool) width(*ir.Field) int { return 1 }

func (kindBool) emitAsserts(*strings.Builder, *ir.Field, string) {}

func (kindBool) emitPayload(b *strings.Builder, _ *ir.Field, v string) {
	fmt.Fprintf(b, "buffer[off++] = %s ? (byte)1 : (byte)0;", v)
}

func (kindBool) emitRead(b *strings.Builder, _ *ir.Field, v string) {
	fmt.Fprintf(b, "if (kind != %d) { reason = TableReport.KindMismatch; goto done; }\n", ir.TableKindBool)
	fmt.Fprintf(b, "%s = body[off++] != 0;\n", v)
}
