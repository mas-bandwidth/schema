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
	"go-measure-wire-damage":      {{old: "reason:=TableRefuseWireDamaged;if verdict==TableOpenRefused", new: "reason:=TableRefuseTruncated /* SABOTAGED: borrow an accelerator clause */;if verdict==TableOpenRefused"}},
	"go-counted-tail-work":        {{old: "g.pf(\"%sfor i:=decoded;i<previous;i++{%sReset(&%s[i])}\\n\", i, f.Type.Name, expr)", new: "g.pf(\"%s_ = previous;for i:=decoded;i<%d;i++{/* SABOTAGED: reset unused capacity */%sReset(&%s[i])}\\n\", i, bound, f.Type.Name, expr)"}},
	"go-arm-reference-framing":    {{old: "if f.Type.Pointer || enumRef(f) != nil {", new: "if false { // SABOTAGED: resolve before validating an arm reference length"}},
	"go-message-measure-reserved": {{old: "e,ok:=v.Entry(ref);if !ok||!tableMessageSkip(r,v,indexBits,e,depth){return false}", new: "e,ok:=v.Entry(ref);if !ok||e.Id>=0xfffffffffffffffd||!tableMessageSkip(r,v,indexBits,e,depth){return false} /* SABOTAGED */"}},
	"go-counted-tail-reset":       {{old: "g.pf(\"%sfor i:=decoded;i<previous;i++{%s[i].Type=%sTypeNone}\\n\", i, expr, f.Type.Name)", new: "g.pf(\"%s_ = previous // SABOTAGED: retain union tails\\n\", i)"}},

	"go-measure-cycle-reason":      {{old: "n.reason=TableRefuseDataCycle;return false", new: "n.reason=TableRefuseInvalidValue;return false /* SABOTAGED */"}},
	"go-measure-count-reason":      {{old: "if count>math.MaxInt32{return TableRefuseCountOverExtentCap}", new: "if count>math.MaxInt32{return TableRefuseCountOverLength} /* SABOTAGED */"}},
	"go-retain-unknown-node":       {{old: "report.Unknown+=unknown;if retain!=nil{report.RetainLost+=unknown}", new: "report.Unknown+=unknown;if retain!=nil{/* SABOTAGED */}"}},
	"go-view-arm-offset":           {{old: "PayloadOffset:uint32(unsafe.Offsetof(%sRow{}.Payload)),", new: "PayloadOffset:uint32(unsafe.Offsetof(%sRow{}.Payload))+1,/* SABOTAGED */"}},
	"go-view-packet-offset":        {{old: "offset := fmt.Sprintf(\"uint32(unsafe.Offsetof(%s{}.%s))\", g.storageName(st.Name), name)", new: "offset := fmt.Sprintf(\"uint32(unsafe.Offsetof(%s{}.%s))\", g.storageName(st.Name), name)\n if g.viewPacket != nil {offset += \"+1\"} // SABOTAGED"}},
	"go-view-outside-identity":     {{old: "id = 0\n\t\tjson = \"\"", new: "id = 1 // SABOTAGED: grant an outside field wire identity\n\t\tjson = \"\""}},
	"reference-wide-union-include": {{old: "for name := range ir.TableClosureVocabulary(u) {", new: "for name := range map[string]bool{} { // SABOTAGED: miss union-only wide storage"}},
	"reference-wide-alignment":     {{old: "static const uint32_t kTableAlign       = ` + fmt.Sprint(align) + `;", new: "static const uint32_t kTableAlign       = 8; // SABOTAGED ` + fmt.Sprint(align) + `;"}},
	"reference-flags-widen":        {{old: "// decides widening, including flags elements (SPEC-TABLES §4).\n\treturn widenable(tableScalarKind(f))", new: "// decides widening, including flags elements (SPEC-TABLES §4).\n\tif f.Type.Ref != nil { return false } // SABOTAGED\n\treturn widenable(tableScalarKind(f))"}},
	"reference-map-widen-count":    {{old: "if !st.IsMapEntry() || f.Name != ir.MapKeyFieldName {", new: "if true { // SABOTAGED: count each widened key twice"}},
	"reference-list-widen-extent":  {{old: "        if ( !TableKindWidens( wire_kind, elem_kind ) ) { return true; }\n        // Load accepts the widening ladder.", new: "        if ( wire_kind != elem_kind ) { return true; } // SABOTAGED\n        // Load accepts the widening ladder."}},
	"go-builder-union-refusal":     {{old: "g.pf(\"%s%sLoadBody(&%s,&%s)\\n\", ind, v.Type, rdr, expr)\n\t\tg.emitCarveReturn(\"r\", rdr, ind)", new: "g.pf(\"%s%sLoadBody(&%s,&%s)\\n\", ind, v.Type, rdr, expr)\n\t\t// SABOTAGED: inspect payload length before propagating refusal"}},
	"map-builder-count-oracle":     {{old: "\t\tif count > uint64(math.MaxInt32) {\n\t\t\tr.off = end\n\t\t\treturn r.collectionCap()\n\t\t}\n\t\tsub := r.subTo(end)", new: "\t\tif false && count > uint64(math.MaxInt32) { // SABOTAGED\n\t\t\tr.off = end\n\t\t\treturn r.collectionCap()\n\t\t}\n\t\tsub := r.subTo(end)"}},
	// Go ownership, managed activation frames, and bounded retention.
	"go-allocator-original-slice": {
		{old: ";return b}", new: ";return b[:n] /* SABOTAGED */}"},
	},
	"go-allocator-pair": {
		{old: "if len(allocator)>0{n.allocator=allocator[0]}", new: "if len(allocator)>0{n.allocator=TableAllocator{}} /* SABOTAGED */"},
	},
	"go-allocator-frame": {
		{old: "type tableWireFrame struct", new: "var tableAllocationProbe []byte // SABOTAGED\ntype tableWireFrame struct"},
		{old: "frame:=tableWireFrame{};var ok bool", new: "frame:=tableWireFrame{};tableAllocationProbe=make([]byte,64);var ok bool"},
	},
	"go-retain-file-count-floor": {
		{old: "||count>uint64(max(int64(0),s.limit-s.w.Offset)/minimum)", new: "/* SABOTAGED */"},
	},
	// Packet defaults: remove only the constructor byte copy.
	"packet-defaults-c-constructor-bytes": {{
		old: "\t\t\tg.pf(\" };\\n        memcpy( value.%s, bytes, sizeof( bytes ) );\\n    }\\n\", f.Name)",
		new: "\t\t\tg.pf(\" };\\n        (void) bytes; /* SABOTAGED: value.%s bytes are not initialized */\\n    }\\n\", f.Name)",
	}},
	"packet-defaults-go-constructor-bytes": {{
		old: "\t\t\tg.pf(\"\\t%s = [%s]byte{\", name, g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size)))",
		new: "\t\t\tg.pf(\"\\t// SABOTAGED: %s = [%s]byte{\", name, g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size)))",
	}},

	// SPEC §4.8 / #503: a payload-free arm adds no bits after its tag.
	// Sabotage one direction at a time; the opposite direction remains an
	// independent check and no writer-produced bytes feed the reader oracle.
	"packet-void-write-cpp": {{
		old: `			g.pf("            write_bits( stream, %du, %d );\n", i+1, bits)
			if v.Void() {
				g.pf("            return true; // payload-free arm — the tag is the whole wire (SPEC §4.8)\n")`,
		new: `			g.pf("            write_bits( stream, %du, %d );\n", i+1, bits)
			if v.Void() {
				g.pf("            write_bits( stream, 0u, 1 ); // SABOTAGED: extra void payload bit\n")
				g.pf("            return true; // payload-free arm — the tag is the whole wire (SPEC §4.8)\n")`,
	}},
	"packet-void-read-cpp": {{
		old: `		if v.Void() {
			g.pf("            return true; // payload-free arm — the tag is the whole wire (SPEC §4.8)\n")
			continue
		}
		g.needsNew = true`,
		new: `		if v.Void() {
			g.pf("            { uint32_t extra = 0; read_bits( stream, extra, 1 ); (void) extra; } // SABOTAGED: extra void payload bit\n")
			g.pf("            return true; // payload-free arm — the tag is the whole wire (SPEC §4.8)\n")
			continue
		}
		g.needsNew = true`,
	}},
	"packet-void-write-c": {{
		old: `				g.pf("        case %d:\n            return 1; /* payload-free arm — the tag is the whole wire (SPEC §4.8) */\n", i+1)`,
		new: `				g.pf("        case %d:\n", i+1)
				g.call("            ", "serialize_write_bits( stream, 0, 1 )") // SABOTAGED: extra void payload bit
				g.pf("            return 1; /* payload-free arm — the tag is the whole wire (SPEC §4.8) */\n")`,
	}},
	"packet-void-read-c": {{
		old: `			g.pf("            return 1; /* payload-free arm — the tag is the whole wire (SPEC §4.8) */\n")`,
		new: `			g.pf("            { serialize_uint32_t extra = 0; if ( !serialize_read_bits( stream, &extra, 1 ) ) return 0; } /* SABOTAGED: extra void payload bit */\n")
			g.pf("            return 1; /* payload-free arm — the tag is the whole wire (SPEC §4.8) */\n")`,
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

	// SPEC-TABLES §7.5: Open is O(1) in the file's size, a header match and
	// nothing per node. Put a walk in: every word of the region is summed
	// before the base is returned, and the sum is kept live by a compare
	// no file can satisfy. Every check stays, so the forgery battery keeps
	// its answers and what goes red is the open-cost gate alone.
	"cook-open-walk-cpp": {{
		old: `    if ( ( (uintptr_t) base % (uintptr_t) alignment ) != 0 ) { return TableCookRefuse( reason, unaligned_base ); }
    return base;
`,
		new: `    if ( ( (uintptr_t) base % (uintptr_t) alignment ) != 0 ) { return TableCookRefuse( reason, unaligned_base ); }
    uint64_t walk = 0; // SABOTAGED: Open walks every word of the region
    for ( uint64_t i = 0; i < length / 8; i++ ) { walk += ( (const uint64_t *) raw )[i]; }
    if ( walk == UINT64_MAX ) { return NULL; }
    return base;
`,
	}},

	// SPEC-TABLES §2.6, §2.9, §3.1 (schema#565): a list of unions is a
	// by-value edge whose elements' set arms are the edges. Take the list out
	// of the walk, and a node reached only through a list element's pointer
	// arm is never numbered.
	"arms-list-union-edge": {{
		old: `		if un := listElementUnion(f); un != nil && g.unionHasEdge(un, map[*ir.Union]bool{}) {
			return edgeList
		}
		return edgeNone`,
		new: `		return edgeNone // SABOTAGED: a list of unions is no edge of the walk`,
	}},

	// SPEC-TABLES §2.6, §7.6: the cook's extent writer descends a table arm
	// exactly as the measure did. Make the COOK alone skip the arm, so the
	// layout reserves the arm's arrays and the writer lays nothing there: what
	// goes red is the extent check, which refuses the cook before a header.
	"arms-cook-skips-arm": {{
		old: `	armed.descend = func(table string, expr edgeExpr, indent string) {
		if ref := memberOf(g.unit, table); ref != nil && g.hasExtent(ref) {
			v.descend(table, expr, indent)
		}
	}`,
		new: `	armed.descend = func(table string, expr edgeExpr, indent string) {
		if ref := memberOf(g.unit, table); ref != nil && g.hasExtent(ref) {
			if ev.bytes != "" { g.pf("%s(void) ctx; (void) region; (void) extent; (void) at; (void) record; (void) value; (void) order; // SABOTAGED: the cook skips the arm\n", indent); return }
			v.descend(table, expr, indent)
		}
	}`,
	}},

	// SPEC-TABLES §7.6: the same skip with the extent check dropped. The cook
	// then loses the arm's list and REPORTS SUCCESS, and only the pinned
	// cook's byte compare catches it.
	"arms-cook-check-dropped": {{
		old: `	armed.descend = func(table string, expr edgeExpr, indent string) {
		if ref := memberOf(g.unit, table); ref != nil && g.hasExtent(ref) {
			v.descend(table, expr, indent)
		}
	}`,
		new: `	armed.descend = func(table string, expr edgeExpr, indent string) {
		if ref := memberOf(g.unit, table); ref != nil && g.hasExtent(ref) {
			if ev.bytes != "" { g.pf("%s(void) ctx; (void) region; (void) extent; (void) at; (void) record; (void) value; (void) order; // SABOTAGED: the cook skips the arm\n", indent); return }
			v.descend(table, expr, indent)
		}
	}`,
	}, {
		old: `	g.pf("    return extent_at == %sExtent( ctx, value ); // the extent written is the extent measured, or no header is written\n}\n\n", st.Name)`,
		new: `	g.pf("    (void) extent_at; return true; // SABOTAGED: the extent written is not held to the extent measured\n}\n\n")`,
	}},

	// SPEC-TABLES §2.6: an arm that is another union asks "does this arm reach
	// a container" one level in. Stop asking, and the leaf's list under two
	// tags leaves the extent: Lock copies the arena reference as it lies.
	"arms-nested-union-extent": {{
		old: `		if inner, ok := v.F.Type.Ref.(*ir.Union); ok && g.unionHasExtent(inner, seen) {
			return true
		}
	}
	return false`,
		new: `		_ = seen // SABOTAGED: a nested union's arms are not asked
	}
	return false`,
	}},

	// SPEC-TABLES §2.6, §6.5: the framing scan dispatches from the field's
	// actual shape. Frame every union field as ONE arm header, and an array of
	// unions' extent is read from its element kind and count as an arm id.
	"arms-array-of-unions-framing": {{
		old: `			if f.Array == ir.ArrayNone {
				g.pf("        if ( field_id == 0x%016xull && field_kind == %d ) // %s: a union arm that holds a list or a map\n        {\n", ir.TableFieldWireId(f), tkUnion, f.Name)`,
		new: `			if true { // SABOTAGED: every union field is framed as one arm header
				g.pf("        if ( field_id == 0x%016xull && field_kind == %d ) // %s: a union arm that holds a list or a map\n        {\n", ir.TableFieldWireId(f), tkUnion, f.Name)`,
	}},

	// SPEC-TABLES §2.6, §3.1: a POINTER ARM is a pointer edge, so its pointee
	// is in the set a root's load can place. Leave pointer arms out of the
	// reachable walk, and a node reached only through one is unknown to its
	// own writer's load.
	"arms-reachable-arm": {{
		old: `			for _, v := range ref.Variants {
				if v.F != nil {
					line(v.F)
				}
			}`,
		new: `			for _, v := range ref.Variants {
				if v.F != nil && !v.F.Type.Pointer { // SABOTAGED: a pointer arm names nothing
					line(v.F)
				}
			}`,
	}},

	// SPEC-TABLES §2.6, §3.1 (schema#578): a pointer arm's slot loop has its
	// own index, because under a list or an array of unions it runs inside
	// the element loop. Spell it `i` again, and slot k of element i reads
	// element k's slot while the bound test reads past the live count.
	"arms-slot-index-shadows": {{
		old: `		g.pf("    for ( int32_t k = 0; k < %s && k < %d; k++ ) // %s: [..%d]*%s\n    {\n", count, f.ArrayBound, f.Name, f.ArrayBound, f.Type.Name)
		body(base.index("k", 8))
		g.pf("    }\n")
	case ir.ArrayFixed:
		g.pf("    for ( int32_t k = 0; k < %d; k++ ) // %s: [%d]*%s\n    {\n", f.ArrayBound, f.Name, f.ArrayBound, f.Type.Name)
		body(base.index("k", 8))`,
		new: `		g.pf("    for ( int32_t i = 0; i < %s && i < %d; i++ ) // %s: [..%d]*%s\n    {\n", count, f.ArrayBound, f.Name, f.ArrayBound, f.Type.Name) // SABOTAGED: the slot loop reuses the element index
		body(base.index("i", 8))
		g.pf("    }\n")
	case ir.ArrayFixed:
		g.pf("    for ( int32_t i = 0; i < %d; i++ ) // %s: [%d]*%s\n    {\n", f.ArrayBound, f.Name, f.ArrayBound, f.Type.Name)
		body(base.index("i", 8))`,
	}},

	// the same walk on the C# leg's Open
	"cook-open-walk-cs": {{
		old: `	g.hf("        cook = new %sCook(at + dataOffset, (long)dataLength);\n", name)`,
		new: `	g.hf("        ulong walk = 0; // SABOTAGED: Open walks every word of the region\n")
	g.hf("        for (ulong i = 0; i < bytes / 8; i++) { walk += ((ulong*)at)[i]; }\n")
	g.hf("        if (walk == ulong.MaxValue) { return false; }\n")
	g.hf("        cook = new %sCook(at + dataOffset, (long)dataLength);\n", name)`,
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
