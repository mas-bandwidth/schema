package unitytable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixedBool is a bool on the fixed form: one byte, 0 or 1 (§3). Any other
// byte is wire_damaged in the check pass, never normalized (§4.5). A bool has
// no ladder: an older writer's leaf under the same id is a bool, or the pair
// is not lawful.
type fixedBool struct{}

func init() { registerFixed("bool", fixedBool{}) }

func (fixedBool) store(b *strings.Builder, _ *ir.Field, v string, off int64) {
	fmt.Fprintf(b, "body[%d] = %s ? (byte)1 : (byte)0;\n", off, v)
}

func (fixedBool) decode(b *strings.Builder, _ *ir.Field, v string, off int64) {
	fmt.Fprintf(b, "%s = body[%d] != 0;\n", v, off)
}

func (fixedBool) pair(w *ir.Field, wkind int, wsize, src int64, _ *ir.Field, dst int64) ([]fixedOp, []fixedOp, bool) {
	if kindOf(w).name() != "bool" || wkind != ir.TableKindBool || wsize != 1 {
		return nil, nil, false
	}
	return []fixedOp{{code: opCopy, src: src, dst: dst, size: 1, width: 1}},
		[]fixedOp{{code: opBool, src: src, size: 1, width: 1}}, true
}

func (fixedBool) image(f *ir.Field) []byte {
	return []byte{byte(kindBool{}.defaultBits(f))}
}
