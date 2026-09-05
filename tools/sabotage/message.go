// THE MESSAGE FORM'S SABOTAGES (docs/SPEC-TABLES.md §3.3): one per row of the
// page's test section, each removing exactly the rule the row's red clause
// names from a COPY of the compiler's engine, so the harness test that holds
// the row is proven to go red on it (make tables-message-form-negative-control).
package main

import "maps"

func init() {
	maps.Copy(sabotages, messageSabotages)
}

var messageSabotages = map[string][]edit{
	// THE TAIL IS UNCONDITIONAL: a unit with no pointer announces the
	// node-table id all the same. Take it out and every slot after it moves.
	"message-tail-without-node-table": {{
		old: "\tname(TableNodeWireId)\n",
		new: "\t// SABOTAGED: the tail's node-table id is gone\n",
	}},

	// THE ORDER IS THE COOK PROJECTION'S, and the committed announcement pins
	// it: reverse the record order and the derived vocabulary is another.
	"message-order-reversed": {{
		old: "return ProjectionMemberName(u, names[i]) < ProjectionMemberName(u, names[j])",
		new: "return ProjectionMemberName(u, names[i]) > ProjectionMemberName(u, names[j]) // SABOTAGED: the projection order reversed",
	}},

	// THE WRITER'S SLOT NUMBERS ARE COMPILE-TIME CONSTANTS, and a slot that
	// moved writes a legal body that names another field.
	"message-slot-off-by-one": {{
		old: "\t\tslots[e.Key()] = uint64(i + 1)\n",
		new: "\t\tslots[e.Key()] = uint64(i + 2) // SABOTAGED: every slot off by one\n",
	}},

	// THE COUNT IS EIGHT BITS, a wire constant: spend sixteen and every byte
	// figure of the page moves.
	"message-count-sixteen-bits": {{
		old: "\tw.put(uint64(len(insts)-1), 8) // the count, a ranged integer over [1, 256]\n",
		new: "\tw.put(uint64(len(insts)-1), 16) // SABOTAGED: a sixteen-bit count\n",
	}},

	// THE BODIES ARE ONE CONTINUOUS BIT STREAM with no alignment between them.
	"message-align-between-bodies": {{
		old: "\t\tif err := encodeBitBody(e, w, inst, true); err != nil {\n\t\t\treturn nil, err\n\t\t}\n",
		new: "\t\tif err := encodeBitBody(e, w, inst, true); err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\tw.align() // SABOTAGED: an align between bodies\n",
	}},

	// M ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME.
	"message-no-batch-bound": {{
		old: "\tif len(insts) > TableMessageBatchMax {\n",
		new: "\tif false { // SABOTAGED: the write side's bound is gone\n",
	}},

	// A STRING ALIGNS BEFORE ITS BYTES, and the batch's arithmetic counts it.
	"message-string-no-align": {{
		old: "\t\tw.align() // a string ALIGNS before its bytes, which buys a memcpy\n",
		new: "\t\t// SABOTAGED: the string's align dropped\n",
	}},

	// A POINTER INDEX IS bits_required(0, node count) WIDE on the write side.
	"message-index-fixed-width": {{
		old: "\te.indexBits = ir.TableMessageBitsRequired(0, int64(len(records))+1)\n",
		new: "\te.indexBits = 32 // SABOTAGED: a fixed index width\n",
	}},

	// A REFERENCE ABOVE E IS DAMAGE: resolve it to nothing instead and the
	// read carries on past it.
	"message-reference-past-entries": {{
		old: "\tif ref == 0 || ref > uint64(len(d.v.entries)) {\n\t\treturn ir.TableVocabularyEntry{}, false\n\t}\n",
		new: "\tif ref == 0 {\n\t\treturn ir.TableVocabularyEntry{}, false\n\t}\n\tif ref > uint64(len(d.v.entries)) {\n\t\treturn ir.TableVocabularyEntry{}, true // SABOTAGED: a reference past E resolves to nothing\n\t}\n",
	}},

	// DAMAGE IS TERMINAL FOR THE BATCH: read on after it instead.
	"message-read-on-after-damage": {{
		old: "\t\tif !d.root(insts[i]) {\n\t\t\treturn i, false, nil\n\t\t}\n",
		new: "\t\tif !d.root(insts[i]) {\n\t\t\tcontinue // SABOTAGED: the next body is read after damage\n\t\t}\n",
	}},

	// THE PAD IS VERIFIED ZERO AND NOTHING FOLLOWS IT.
	"message-no-pad-check": {{
		old: "\tif !r.align() || r.off != r.n {\n",
		new: "\tif false { // SABOTAGED: the pad and what follows it go unchecked\n",
	}},

	// M ABOVE THE CALLER'S CAPACITY IS A REFUSAL BY NAME with nothing
	// decoded: decode what fits instead.
	"message-no-capacity-refusal": {{
		old: "\tif count > len(insts) {\n\t\treport.Refused = true\n\t\treturn count, false, &MessageRefusal{Reason: ReasonBatchTooLarge, BuildVersion: v.buildVersion}\n\t}\n",
		new: "\tif count > len(insts) {\n\t\tcount = len(insts) // SABOTAGED: what fits is decoded and the rest dropped\n\t}\n",
	}},

	// A VARIANT REFERENCE NAMING AN ENTRY OF THE WRONG SORT IS MALFORMED.
	"message-any-sort-names": {{
		old: "\tif !named || reserved(entry.Id) || entry.Kind != 0 {\n",
		new: "\tif !named || reserved(entry.Id) { // SABOTAGED: any sort names\n",
	}},

	// AN ARM REFERENCE NAMING A KIND-0 ENTRY IS MALFORMED.
	"message-any-sort-arms": {{
		old: "\tif !named || reserved(entry.Id) || entry.Kind == 0 {\n",
		new: "\tif !named || reserved(entry.Id) { // SABOTAGED: any sort frames an arm\n",
	}},

	// A RANGED OFFSET ABOVE THE SENDER'S MAX RECONSTRUCTS AND CLAMPS, and is
	// not damage.
	"message-offset-past-max-is-damage": {{
		old: "\t\t\t} else if value > hi {\n\t\t\t\tvalue = hi\n\t\t\t\td.report.Clamped++\n\t\t\t}\n\t\t} else {\n\t\t\tu := uint64(value)\n",
		new: "\t\t\t} else if value > hi {\n\t\t\t\td.report.Malformed = true // SABOTAGED: a value past the bound is damage\n\t\t\t\treturn false\n\t\t\t}\n\t\t} else {\n\t\t\tu := uint64(value)\n",
	}},

	// AN OVER-LONG ARRAY CLAMPS BY WALKING THE SURPLUS: stop at the bound
	// instead and the next field lands on the wrong bit.
	"message-clamp-drops-surplus": {{
		old: "\tfor i := uint64(0); i < n; i++ {\n\t\tvar sink tabletext.Cell\n\t\tcell := &sink\n\t\tif f.Array == ir.ArrayList {\n",
		new: "\tfor i := uint64(0); i < kept; i++ { // SABOTAGED: the surplus is not walked\n\t\tvar sink tabletext.Cell\n\t\tcell := &sink\n\t\tif f.Array == ir.ArrayList {\n",
	}},

	// A SKIPPED STRING ALIGNS BEFORE ITS BYTES exactly as a read one does.
	"message-skip-string-no-align": {{
		old: "\tcase ir.TableKindString:\n\t\tn, ok := d.r.get(ir.TableMessageBitsRequired(0, shape.Max))\n\t\tif !ok || !d.r.align() {\n",
		new: "\tcase ir.TableKindString:\n\t\tn, ok := d.r.get(ir.TableMessageBitsRequired(0, shape.Max))\n\t\tif !ok { // SABOTAGED: the skip does not align\n",
	}},

	// A WIDE STRING IS SIXTEEN BITS A CODE UNIT, not SPEC.md §4.12's group.
	"message-wide-string-thirty-two": {{
		old: "\t\treturn d.r.skip(int(n) * 16)\n",
		new: "\t\treturn d.r.skip(int(n) * 32) // SABOTAGED: a thirty-two bit group a unit\n",
	}},

	// A RANGE THAT MOVED DECODES AT THE SENDER'S WIDTH, not the reader's own.
	"message-reader-own-width": {{
		old: "\twidth := int(ir.TableMessageValueBits(kind, shape))\n",
		new: "\twidth := int(ir.TableMessageValueBits(kind, ir.TableFieldShape(f))) // SABOTAGED: the reader's own width\n",
	}},

	// A POINTER INDEX IS bits_required(0, node count) WIDE on the read side.
	"message-index-fixed-width-read": {{
		old: "\td.indexBits = ir.TableMessageBitsRequired(0, int64(count)+1)\n",
		new: "\td.indexBits = 32 // SABOTAGED: a fixed index width\n",
	}},

	// THE VOCABULARY IS PER CONNECTION AND PER DIRECTION: a reader that
	// resolves against its own unit's is wrong whenever the peers differ.
	"message-reads-own-vocabulary": {{
		old: "\t\td := &bitDecoder{m: m, v: v, report: report, r: r, refBits: v.RefBits(), indexBits: ir.TableMessageBitsRequired(0, 1)}\n",
		new: "\t\town := &Vocabulary{entries: ir.TableVocabulary(m.Unit), announced: true} // SABOTAGED: the reader's own vocabulary\n\t\td := &bitDecoder{m: m, v: own, report: report, r: r, refBits: own.RefBits(), indexBits: ir.TableMessageBitsRequired(0, 1)}\n",
	}},

	// A SECOND ANNOUNCEMENT IS REFUSED BY NAME and sets nothing.
	"message-second-announcement-accepted": {{
		old: "\tif v.announced {\n",
		new: "\tif false { // SABOTAGED: a second announcement is accepted\n",
	}},

	// THE BUILD VERSION IS PRESENT EXACTLY ONCE, and so is the vocabulary.
	"message-strict-checks-relaxed": {{
		old: "seenVersion == 1 && seenVocabulary == 1",
		new: "seenVersion >= 1 && seenVocabulary >= 1 /* SABOTAGED: twice is fine */",
	}},

	// THE ENTRY BOUND IS CHECKED BEFORE AN ENTRY IS TOUCHED.
	"message-no-entry-bound": {{
		old: "\tif len(entries) > v.bound() {\n",
		new: "\tif false { // SABOTAGED: the entry bound is gone\n",
	}},

	// AND THE BYTE BOUND, off the field's own L.
	"message-no-byte-bound": {{
		old: "\t\t\tif int(length) > byteBound {\n",
		new: "\t\t\tif false { // SABOTAGED: the byte bound is gone\n",
	}},

	// THE RESERVED IDS WHERE THEY DO NOT BELONG: a vocabulary carrying the
	// announcement's own two is malformed whole.
	"message-reserved-in-vocabulary-accepted": {{
		old: "\t\tcase ir.TableBuildVersionWireId, ir.TableMessageVocabularyWireId:\n\t\t\treturn nil, false\n",
		new: "\t\t// SABOTAGED: the announcement's own two ids take slots\n",
	}},

	// A FLAGS MASK RIDES AT ITS DECLARED W BITS, not the file's sixty-four.
	"message-mask-sixty-four": {{
		old: "Bits: int64(fl.WireBits)",
		new: "Bits: 64 + 0*int64(fl.WireBits) /* SABOTAGED: the file's width */",
	}},

	// AN UNBOUNDED ARRAY AND A MAP ARE ANNOUNCED AS RANGED 0 TO 2^32 - 1.
	"message-list-count-sixteen": {{
		old: "const TableMessageListMax = int64(0xFFFFFFFF)\n",
		new: "const TableMessageListMax = int64(0xFFFF) // SABOTAGED: a sixteen-bit count\n",
	}},

	// A HOSTILE SHAPE IS A HOSTILE WIDTH: bits above 128 are refused.
	"message-bits-unbounded": {{
		old: "\t\t\tif !ok || v > 128 {\n",
		new: "\t\t\tif !ok { // SABOTAGED: any width\n",
	}},

	// A REFERENCE IS bits_required(0, E) WIDE, not a byte.
	"message-reference-eight-bits": {{
		old: "\treturn bits.Len64(uint64(entries))\n",
		new: "\treturn 8 // SABOTAGED: a reference is a byte\n",
	}},
}

// THE SAME TWO IN THE C++ EMITTER, which is the one the reference reads and
// writes: the instrument is the pinned wire, because the golden still LOADS
// under a moved slot — the reader resolves whatever the writer named — so what
// goes red is the round trip against the corpus's own bytes.
var messageEmitterSabotages = map[string][]edit{
	// THE WRITER'S SLOT NUMBERS ARE COMPILE-TIME CONSTANTS: every emitted slot
	// off by one writes a legal body that names another field.
	"message-emitter-slot-off-by-one": {{
		old: "func (g *tableGen) msgSlot(entry ir.TableVocabularyEntry) uint64 { return g.slots[entry.Key()] }",
		new: "func (g *tableGen) msgSlot(entry ir.TableVocabularyEntry) uint64 { return g.slots[entry.Key()] + 1 } // SABOTAGED: every emitted slot off by one",
	}},
	// THE COUNT IS EIGHT BITS: spend sixteen and every pinned batch moves.
	"message-emitter-count-sixteen-bits": {{
		old: "    batch.w.put( (uint64_t) ( bodies - 1 ), 8 ); // a ranged integer over [1, 256]\n",
		new: "    batch.w.put( (uint64_t) ( bodies - 1 ), 16 ); // SABOTAGED: a sixteen-bit count\n",
	}},
}

func init() {
	maps.Copy(sabotages, messageEmitterSabotages)
}

// THE SECOND ROUND'S ROWS (docs/SPEC-TABLES.md §3.3, schema#571): the base's
// two encodings, the quantized index, the terminal refusal, and the six
// findings, each with the one rule its red clause names taken out.
var messageRoundTwoSabotages = map[string][]edit{
	// A RANGED BASE IS ENCODED BY ITS KIND'S SIGNEDNESS: zigzag an unsigned
	// base and the domain's high half stops spelling.
	"message-base-zigzag-unsigned": {{
		old: "\t\t\t\tout = appendLebBytes(out, tableMessageUnsignedBase(s.Base))\n",
		new: "\t\t\t\tout = appendLebBytes(out, tableMessageZigzag(s.Base)) // SABOTAGED: every base zigzags\n",
	}},

	// THE WRITER'S RULE IS IN FLOAT32: normalize in float64 and the rounding
	// tie at 0.005 falls to 0 where the packet wire writes 1.
	"message-quantize-float64": {{
		old: "\tnormalized := float32((value - s.QMin) / delta)\n",
		new: "\tnormalized := float32((float64(value) - float64(s.QMin)) / float64(delta)) // SABOTAGED: the writer normalizes in float64\n",
	}},

	// THE READER ROUNDS TWICE: fold the product and the add into one
	// rounding and 6666 over [-100, 100] decodes to C2055C29.
	"message-dequantize-round-once": {{
		old: "\tscaled := float32(normalized * delta)\n\treturn float32(scaled + s.QMin)\n",
		new: "\treturn float32(float64(normalized)*float64(delta) + float64(s.QMin)) // SABOTAGED: one rounding\n",
	}},

	// REFUSAL IS TERMINAL: a refused first announcement leaves the connection
	// open to a second resolve.
	"message-refusal-not-terminal": {{
		old: "\tif v.announced || v.refused {\n",
		new: "\tif v.announced { // SABOTAGED: a refused first announcement is not terminal\n",
	}},

	// A WIDTH ABOVE THE KIND'S OWN DOMAIN IS A HOSTILE WIDTH (M3): bound every
	// kind at 128 instead and a uint8 announced at nine bits is accepted.
	"message-width-above-kind": {{
		old: "\t\t\tif !ok || v > uint64(TableMessageKindBits(kind)) {\n",
		new: "\t\t\tif !ok || v > 128 { // SABOTAGED: every kind may announce 128 bits\n",
	}},

	// THE COUNT RIDES AS ITS OFFSET FROM THE MINIMUM (M4): write the count
	// itself and the pinned vector moves.
	"message-count-not-offset": {{
		old: "\t\tw.put(uint64(count)-uint64(shape.Min), bits)\n",
		new: "\t\tw.put(uint64(count), bits) // SABOTAGED: the count, not its offset\n",
	}},

	// A DISCARDED SURPLUS ELEMENT NEVER ACQUIRES A LIVE DESTINATION (M1): land
	// it on element zero instead.
	"message-surplus-lands-on-zero": {{
		old: "\t\tvar sink tabletext.Cell\n\t\tcell := &sink\n",
		new: "\t\tcell := &fv.Elems[0] // SABOTAGED: a surplus element overwrites element zero\n",
	}},

	// A RANGED 128-BIT VALUE READS AT ITS ANNOUNCED WIDTH (M2): read the raw
	// sixteen bytes instead.
	"message-wide-reads-raw": {{
		old: "\t\tif shape.Packing == ir.TableMessageRanged {\n\t\t\toffset, ok := d.r.getBig(width)\n",
		new: "\t\tif false { // SABOTAGED: a ranged 128-bit value reads raw\n\t\t\toffset, ok := d.r.getBig(width)\n",
	}},

	// THE BOUND APPLIES WHILE THE VALUE IS WIDE (M6): narrow first and 263
	// becomes 7, which clamps to 200 rather than 250.
	"message-narrow-before-clamp": {{
		old: "\t\t\tu, ulo, uhi := uint64(value), f.IntMin.Uint64(), f.IntMax.Uint64()\n",
		new: "\t\t\tu, ulo, uhi := uint64(value)&(uint64(1)<<uint(f.Type.Width)-1), f.IntMin.Uint64(), f.IntMax.Uint64() // SABOTAGED: narrowed before the clamp\n",
	}},
}

func init() {
	maps.Copy(sabotages, messageRoundTwoSabotages)
}
