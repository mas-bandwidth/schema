package darttable

import (
	"github.com/mas-bandwidth/schema/v2/ir"
	"strconv"
	"strings"
)

func flagsRef(f *ir.Field) *ir.Flags {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	v, _ := f.Type.Ref.(*ir.Flags)
	return v
}
func flagsNames(f *ir.Field) string {
	names := flagsRef(f).Variants
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = strconv.Quote(n)
	}
	return "<String>[" + strings.Join(parts, ",") + "]"
}
