package rust

import (
	"fmt"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *gen) emitWriteWString(f *ir.Field, name, ind string) {
	bound := g.renderArg(f.Type.SizeExpr, big.NewInt(f.Type.Size), "i32")
	g.pf("%s{\n%s    let mut length = %s_length;\n", ind, ind, name)
	g.writeAssert(ind+"    ", fmt.Sprintf("length >= 0 && length <= %s", bound),
		fmt.Sprintf("%s_length out of range [0, %s]", assertLabel(name), bound))
	g.pf("%s    stream.serialize_int(&mut length, 0, %s)?;\n", ind, bound)
	g.pf("%s    for &unit in &%s[..length as usize] {\n", ind, name)
	g.writeAssert(ind+"        ", "unit != 0", fmt.Sprintf("%s carries an interior null within its used length", assertLabel(name)))
	g.pf("%s        let mut group = u32::from(unit);\n%s        stream.serialize_bits(&mut group, 32)?;\n%s    }\n%s}\n", ind, ind, ind, ind)
}

func (g *gen) emitReadWString(f *ir.Field, name, ind string) {
	bound := g.renderArg(f.Type.SizeExpr, big.NewInt(f.Type.Size), "i32")
	g.pf("%sstream.serialize_int(&mut %s_length, 0, %s)?;\n", ind, name, bound)
	g.pf("%s{\n%s    let mut expect_low = false;\n", ind, ind)
	g.pf("%s    for unit in &mut %s[..%s_length as usize] {\n", ind, name, name)
	g.pf("%s        let mut group = 0u32;\n%s        stream.serialize_bits(&mut group, 32)?;\n", ind, ind)
	g.pf("%s        if group == 0 || group > 0xFFFF { return Err(Error::Validation); }\n", ind)
	g.pf("%s        let high = (0xD800..=0xDBFF).contains(&group);\n", ind)
	g.pf("%s        let low = (0xDC00..=0xDFFF).contains(&group);\n", ind)
	g.pf("%s        if low != expect_low { return Err(Error::Validation); }\n", ind)
	g.pf("%s        expect_low = high;\n%s        *unit = group as u16;\n%s    }\n", ind, ind, ind)
	g.pf("%s    if expect_low { return Err(Error::Validation); }\n%s}\n", ind, ind)
}
