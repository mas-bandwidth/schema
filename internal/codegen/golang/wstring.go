package golang

import (
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *gen) emitWriteWString(f *ir.Field, name, ind string) {
	g.emitWriteRangedFold32(name+"Length", "0", g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size)), ir.BitsRequired(big.NewInt(0), big.NewInt(f.Type.Size)), true, ind)
	g.pf("%sfor _, unit := range %s[:%sLength] {\n", ind, name, name)
	g.pf("%s\tif unit == 0 { return ErrValidation }\n", ind)
	g.pf("%s\tgroup := uint32(unit)\n%s\tstream.SerializeBits(&group, 32)\n%s}\n", ind, ind, ind)
}

func (g *gen) emitReadWString(f *ir.Field, name, ind string) {
	g.emitReadRangedFold32("0", ir.BitsRequired(big.NewInt(0), big.NewInt(f.Type.Size)), big.NewInt(f.Type.Size), true, ind, func(ai, expr string) {
		g.pf("%s%sLength = %s\n", ai, name, expr)
	})
	g.pf("%s{\n%s\texpectLow := false\n", ind, ind)
	g.pf("%s\tfor i := int32(0); i < %sLength; i++ {\n", ind, name)
	g.pf("%s\t\tvar group uint32\n%s\t\tstream.SerializeBits(&group, 32)\n", ind, ind)
	g.pf("%s\t\tif stream.Err() != nil { return stream.Err() }\n", ind)
	g.pf("%s\t\tif group == 0 || group > 0xFFFF { return ErrValidation }\n", ind)
	g.pf("%s\t\thigh := group >= 0xD800 && group <= 0xDBFF\n", ind)
	g.pf("%s\t\tlow := group >= 0xDC00 && group <= 0xDFFF\n", ind)
	g.pf("%s\t\tif low != expectLow { return ErrValidation }\n", ind)
	g.pf("%s\t\texpectLow = high\n%s\t\t%s[i] = uint16(group)\n%s\t}\n", ind, ind, name, ind)
	g.pf("%s\tif expectLow { return ErrValidation }\n%s}\n", ind, ind)
}
