// sabotage writes a deliberately broken COPY of a compiler source file, for
// the negative controls to build against through a Go overlay.
//
// Why a tool rather than the sed the older controls use: these sabotages
// INSERT emitter lines as well as remove them, and an inserted line carries
// the two characters `\n` inside a Go string literal, which sed's replacement
// side spells differently on the BSD and GNU implementations. Exact string
// replacement in Go spells it once. The exactly-once match is also the
// control's own guard: an emitter that drifts away from an anchor fails here,
// loudly, instead of producing an unsabotaged copy that passes and quietly
// retires the control.
//
// The tool NEVER writes the file it reads. Its output is a copy under build/.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// an edit is one exact replacement, which must match exactly once.
type edit struct{ old, new string }

// sabotages maps a control's name to what it breaks. Each entry names the
// rule it removes, so a reader of a red control knows what was taken away.
var sabotages = map[string][]edit{
	// SPEC §4.12: `wstring(N)` performs NO alignment. serialize.modern's
	// wstring_ inserts an align between the length and the code units, and
	// that align is the one thing schema does not do. Put it back on both
	// paths and every byte after the length field moves.
	"wstring-align": {{
		old: `		g.emitWriteRangedFold32(name+"_length", "0", g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size)),
			bitsRequired(big.NewInt(0), big.NewInt(f.Type.Size)), true, ind)
		g.pf("%sfor ( int32_t i = 0; i < %s_length; i++ )\n%s{\n", ind, name, ind)
		g.pf("%s    write_bits( stream, uint32_t( %s[i] ), 32 );\n%s}\n", ind, name, ind)`,
		new: `		g.emitWriteRangedFold32(name+"_length", "0", g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size)),
			bitsRequired(big.NewInt(0), big.NewInt(f.Type.Size)), true, ind)
		g.pf("%swrite_align( stream );\n", ind) // SABOTAGED: an align between the length and the code units
		g.pf("%sfor ( int32_t i = 0; i < %s_length; i++ )\n%s{\n", ind, name, ind)
		g.pf("%s    write_bits( stream, uint32_t( %s[i] ), 32 );\n%s}\n", ind, name, ind)`,
	}, {
		old: `		g.pf("%s{\n%s    bool expect_low_surrogate = false;\n", ind, ind)`,
		new: `		g.pf("%sread_align( stream );\n", ind) // SABOTAGED: an align between the length and the code units
		g.pf("%s{\n%s    bool expect_low_surrogate = false;\n", ind, ind)`,
	}},

	// SPEC §4.12: a high surrogate not immediately followed by a low one
	// fails, a low not immediately preceded by a high fails, and a high as
	// the final transmitted group fails. Take the whole pairing rule out.
	// The group-value and zero-group refusals stay, so what goes red is the
	// pairing and nothing else.
	"wstring-accept-unpaired-surrogate": {{
		old: `		g.pf("%s{\n%s    bool expect_low_surrogate = false;\n", ind, ind)
		g.pf("%s    for ( int32_t i = 0; i < %s_length; i++ )\n%s    {\n", ind, name, ind)
		g.pf("%s        uint32_t group = 0;\n", ind)
		g.pf("%s        read_bits( stream, group, 32 );\n", ind)
		g.pf("%s        if ( group == 0 || group > 0xFFFF )\n%s        {\n", ind, ind)
		g.pf("%s            return false; // a zero group and a group above 0xFFFF are content the read refuses (SPEC §4.12)\n%s        }\n", ind, ind)
		g.pf("%s        const bool high_surrogate = group >= 0xD800 && group <= 0xDBFF;\n", ind)
		g.pf("%s        const bool low_surrogate = group >= 0xDC00 && group <= 0xDFFF;\n", ind)
		g.pf("%s        if ( low_surrogate != expect_low_surrogate )\n%s        {\n", ind, ind)
		g.pf("%s            return false; // an unpaired surrogate is content the read refuses (SPEC §4.12)\n%s        }\n", ind, ind)
		g.pf("%s        expect_low_surrogate = high_surrogate;\n", ind)
		g.pf("%s        %s[i] = char16_t( group );\n%s    }\n", ind, name, ind)
		g.pf("%s    if ( expect_low_surrogate )\n%s    {\n", ind, ind)
		g.pf("%s        return false; // a high surrogate as the final group is unpaired (SPEC §4.12)\n%s    }\n%s}\n", ind, ind, ind)`,
		new: `		g.pf("%s{\n", ind) // SABOTAGED: the surrogate pairing rule removed
		g.pf("%s    for ( int32_t i = 0; i < %s_length; i++ )\n%s    {\n", ind, name, ind)
		g.pf("%s        uint32_t group = 0;\n", ind)
		g.pf("%s        read_bits( stream, group, 32 );\n", ind)
		g.pf("%s        if ( group == 0 || group > 0xFFFF )\n%s        {\n", ind, ind)
		g.pf("%s            return false; // a zero group and a group above 0xFFFF are content the read refuses (SPEC §4.12)\n%s        }\n", ind, ind)
		g.pf("%s        %s[i] = char16_t( group );\n%s    }\n%s}\n", ind, name, ind, ind)`,
	}},

	// SPEC §4.12: in C and C++ a successful read writes the zero unit at
	// index length, always. Drop the store. The buffer the harness reads
	// into is poisoned first, so what this reddens is the STORE rather than
	// the storage's own default initialization.
	"wstring-drop-terminator": {{
		old: `		// the terminating zero UNIT, always — §5's one stated tail exception
		g.pf("%s%s[%s_length] = 0;\n", ind, name, name)`,
		new: `		// SABOTAGED: the terminating zero unit at index length dropped`,
	}},

	// SPEC §4.7 as amended (schema#519): the UTF-8 validator runs on the READ
	// path in every build mode. Put it back where it was, a write-side debug
	// assert and nothing on read, which is the stance the amendment replaced.
	"string-write-only-utf8": {{
		old: `			// a payload that is not well-formed UTF-8 fails the READ, in every
			// build mode (SPEC §4.7). The refusal is terminal: nothing after it
			// has a defined position.
			g.pf("%sif ( !schema_utf8_valid( reinterpret_cast<const uint8_t *>( %s ), %s_length ) )\n%s{\n", ind, name, name, ind)
			g.pf("%s    return false; // malformed UTF-8 is content the read refuses (SPEC §4.7)\n%s}\n", ind, ind)
`,
		new: `			// SABOTAGED: the read-side UTF-8 refusal removed
`,
	}, {
		old: `			g.pf("%sfor ( int32_t i = 0; i < %s_length; i++ )\n%s{\n", ind, name, ind)
			g.pf("%s    serialize_assert( %s[i] != 0 );\n%s}\n", ind, name, ind)
		}`,
		new: `			g.pf("%sfor ( int32_t i = 0; i < %s_length; i++ )\n%s{\n", ind, name, ind)
			g.pf("%s    serialize_assert( %s[i] != 0 );\n%s}\n", ind, name, ind)
			g.pf("%sserialize_assert( schema_utf8_valid( reinterpret_cast<const uint8_t *>( %s ), %s_length ) );\n", ind, name, name) // SABOTAGED: the write-only stance restored
		}`,
	}},
}

func main() {
	name := flag.String("name", "", "the sabotage to apply")
	out := flag.String("out", "", "where to write the sabotaged copy")
	flag.Parse()
	if flag.NArg() != 1 || *name == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "usage: sabotage -name <name> -out <copy> <source>")
		os.Exit(2)
	}
	edits, ok := sabotages[*name]
	if !ok {
		fmt.Fprintf(os.Stderr, "sabotage: no sabotage named %q\n", *name)
		os.Exit(2)
	}
	b, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "sabotage:", err)
		os.Exit(1)
	}
	s := string(b)
	for i, e := range edits {
		if got := strings.Count(s, e.old); got != 1 {
			fmt.Fprintf(os.Stderr, "sabotage %s: edit %d matched %d times, want exactly 1. The emitter moved away from the anchor, so this negative control no longer breaks what it names. Re-anchor it or retire it.\n", *name, i, got)
			os.Exit(1)
		}
		s = strings.Replace(s, e.old, e.new, 1)
	}
	if !strings.Contains(s, "SABOTAGED") {
		fmt.Fprintf(os.Stderr, "sabotage %s: the copy carries no SABOTAGED marker\n", *name)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, []byte(s), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "sabotage:", err)
		os.Exit(1)
	}
}
