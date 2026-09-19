package c

import (
	"fmt"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *gen) emitWriteWString(f *ir.Field, ind string) {
	name := "value->" + f.Name
	bound := g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size))
	// the used length and an interior null are writer misuse, not content:
	// debug asserts that compile out under NDEBUG, the same two guards the
	// C++ backend folds in (SPEC §4.12, §5). Surrogate pairing is the
	// READER's refusal, below.
	//
	// Both asserts compile out under NDEBUG, so the RELEASE path is obliged to
	// be total: an out-of-contract length would walk off a buffer of N + 1
	// units. Clamp the used length into [0, N] once and write THAT length —
	// deterministic bytes, never a read past the end (SPEC §5).
	g.pf("%sserialize_assert( %s_length >= 0 && %s_length <= %s );\n", ind, name, name, bound)
	g.pf("%s{\n%s    int32_t i;\n", ind, ind)
	g.pf("%s    const int32_t clamped_length = %s_length < 0 ? 0 : ( %s_length > ( %s ) ? ( %s ) : %s_length ); /* release: an out-of-contract length writes the clamped length — never a trap (SPEC §5) */\n",
		ind, name, name, bound, bound, name)
	g.call(ind+"    ", fmt.Sprintf("serialize_write_int( stream, clamped_length, 0, %s )", bound))
	g.pf("%s    for ( i = 0; i < clamped_length; i++ )\n%s    {\n", ind, ind)
	g.pf("%s        serialize_assert( %s[i] != 0 ); /* interior null on write (SPEC §4.12) */\n", ind, name)
	g.call(ind+"        ", fmt.Sprintf("serialize_write_bits( stream, (serialize_uint32_t) %s[i], 32 )", name))
	g.pf("%s    }\n%s}\n", ind, ind)
}

func (g *gen) emitReadWString(f *ir.Field, ind string) {
	name := "value->" + f.Name
	g.call(ind, fmt.Sprintf("serialize_read_int( stream, &%s_length, 0, %s )", name, g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size))))
	g.pf("%s{\n%s    int32_t i;\n%s    int expect_low = 0;\n", ind, ind, ind)
	g.pf("%s    for ( i = 0; i < %s_length; i++ )\n%s    {\n", ind, name, ind)
	g.pf("%s        serialize_uint32_t group = 0;\n%s        int high, low;\n", ind, ind)
	g.call(ind+"        ", "serialize_read_bits( stream, &group, 32 )")
	g.pf("%s        if ( group == 0 || group > 0xFFFF ) { return 0; }\n", ind)
	g.pf("%s        high = group >= 0xD800 && group <= 0xDBFF;\n", ind)
	g.pf("%s        low = group >= 0xDC00 && group <= 0xDFFF;\n", ind)
	g.pf("%s        if ( low != expect_low ) { return 0; } /* unpaired surrogate (SPEC §4.12) */\n", ind)
	g.pf("%s        expect_low = high;\n%s        %s[i] = (uint16_t) group;\n%s    }\n", ind, ind, name, ind)
	g.pf("%s    if ( expect_low ) { return 0; } /* final high surrogate */\n%s}\n", ind, ind)
	g.pf("%s%s[%s_length] = 0;\n", ind, name, name)
}
