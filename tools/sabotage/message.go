// THE MESSAGE FORM'S SABOTAGES (docs/SPEC-TABLES.md §3.3): one per row of the
// page's test section, each removing exactly the rule the row's red clause
// names from a COPY of the compiler's engine, so the harness test that holds
// the row is proven to go red on it (make tables-message-form-negative-control).
package main

import "maps"

func init() {
	maps.Copy(sabotages, messageSabotages)
}

// countOverBoundClamps takes the strict read's count_over_bound refusal out of
// the message array read and clamps to the bound in its place.
var countOverBoundClamps = []edit{{
	old: "\tif kept > bound {\n\t\td.refuse(ReasonCountOverBound)\n\t\treturn false\n\t}\n",
	new: "\tif kept > bound {\n\t\tkept = bound // SABOTAGED: an over-long array clamps to the bound\n\t}\n",
}}

var messageSabotages = map[string][]edit{
	// THE ANNOUNCEMENT CARRIES THE MESSAGE CLOSURE AND NOTHING ELSE: no body
	// of the message form can carry the reserved node-table id, so it takes
	// no slot. Keep it and the vocabulary grows a slot no body can name.
	"message-closure-announces-node-table": {{
		old: "\t\tif keep[e.Key()] || (names[e.Id] && e.Kind == 0) {\n",
		new: "\t\tif keep[e.Key()] || (names[e.Id] && e.Kind == 0) || e.Id == ir.TableNodeWireId { // SABOTAGED: the node-table id takes a slot\n",
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
		old: "\t\tif err := encodeBitBody(e, w, inst); err != nil {\n\t\t\treturn nil, err\n\t\t}\n",
		new: "\t\tif err := encodeBitBody(e, w, inst); err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\tw.align() // SABOTAGED: an align between bodies\n",
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

	// A REFERENCE ABOVE E IS DAMAGE: resolve it to nothing instead and the
	// read carries on past it.
	"message-reference-past-entries": {{
		old: "\tif ref == 0 || ref > uint64(len(d.v.entries)) {\n\t\treturn ir.TableVocabularyEntry{}, false\n\t}\n",
		new: "\tif ref == 0 {\n\t\treturn ir.TableVocabularyEntry{}, false\n\t}\n\tif ref > uint64(len(d.v.entries)) {\n\t\treturn ir.TableVocabularyEntry{}, true // SABOTAGED: a reference past E resolves to nothing\n\t}\n",
	}},

	// DAMAGE DISCARDS THE WHOLE BATCH (§4, §6.5): no body is delivered, the
	// ones before the damage included. The strict read replaced the rule
	// that stopped at the damage and kept the bodies read before it; keep
	// them again and the batch reads short instead of refusing.
	"message-damage-keeps-earlier-bodies": {{
		old: "\t\tif fail.err != nil {\n\t\t\treturn 0, fail.err\n\t\t}\n\t\tif !ok {\n\t\t\treturn 0, damage(\"a body that cannot be read\")\n\t\t}\n",
		new: "\t\tif fail.err != nil || !ok {\n\t\t\treturn i, nil // SABOTAGED: damage keeps the bodies before it\n\t\t}\n",
	}},

	// THE PAD IS VERIFIED ZERO AND NOTHING FOLLOWS IT.
	"message-no-pad-check": {{
		old: "\tif !r.align() || r.off != r.n {\n",
		new: "\tif false { // SABOTAGED: the pad and what follows it go unchecked\n",
	}},

	// M ABOVE THE CALLER'S CAPACITY IS A REFUSAL BY NAME with nothing
	// decoded: decode what fits instead.
	"message-no-capacity-refusal": {{
		old: "\tif count > len(insts) {\n\t\treturn count, &MessageRefusal{Reason: ReasonBatchTooLarge, BuildVersion: v.buildVersion}\n\t}\n",
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

	// A RANGED OFFSET ABOVE THE READER'S MAX REFUSES (§6.5): the strict read
	// never clamps. Clamp it to the bound and read on instead, which is the
	// rule this row held before the strict read replaced it.
	"message-offset-past-max-clamps": {{
		old: "\t\t\t} else if value > hi {\n\t\t\t\tvalue = hi\n\t\t\t\td.refuse(ReasonOutOfRange)\n\t\t\t}\n\t\t} else {\n\t\t\t// the bounds are read whole: an unsigned range reaches 2^64 - 1\n",
		new: "\t\t\t} else if value > hi {\n\t\t\t\tvalue = hi // SABOTAGED: a value past the bound clamps and reads\n\t\t\t}\n\t\t} else {\n\t\t\t// the bounds are read whole: an unsigned range reaches 2^64 - 1\n",
	}},

	// A COUNT PAST THE READER'S BOUND REFUSES count_over_bound before any
	// element is read (§6.5): the strict read replaced the clamp that walked
	// the surplus into a sink. Clamp to the bound and read on instead. Two
	// rows hold it: an over-long array of walked elements, and a zero-width
	// element under a count of 2^31 + 1, whose surplus is arithmetic.
	"message-count-over-bound-clamps":            countOverBoundClamps,
	"message-count-over-bound-clamps-zero-width": countOverBoundClamps,

	// A SKIPPED STRING ALIGNS BEFORE ITS BYTES exactly as a read one does.
	"message-skip-string-no-align": {{
		old: "\tcase ir.TableKindString:\n\t\tn, ok := d.r.get(ir.TableMessageBitsRequired(0, shape.Max))\n\t\tif !ok || !d.r.align() {\n",
		new: "\tcase ir.TableKindString:\n\t\tn, ok := d.r.get(ir.TableMessageBitsRequired(0, shape.Max))\n\t\tif !ok { // SABOTAGED: the skip does not align\n",
	}},

	// A RANGE THAT MOVED DECODES AT THE SENDER'S WIDTH, not the reader's own.
	"message-reader-own-width": {{
		old: "\twidth := int(ir.TableMessageValueBits(kind, shape))\n",
		new: "\twidth := int(ir.TableMessageValueBits(kind, ir.TableFieldShape(f))) // SABOTAGED: the reader's own width\n",
	}},

	// THE VOCABULARY IS PER CONNECTION AND PER DIRECTION: a reader that
	// resolves against its own unit's is wrong whenever the peers differ.
	"message-reads-own-vocabulary": {{
		old: "\t\td := &bitDecoder{m: m, v: v, report: report, r: r, refBits: v.RefBits(), fail: fail}\n",
		new: "\t\town := &Vocabulary{entries: MessageVocabulary(m.Unit), announced: true} // SABOTAGED: the reader's own vocabulary\n\t\td := &bitDecoder{m: m, v: own, report: report, r: r, refBits: own.RefBits(), fail: fail}\n",
	}},

	// A SECOND ANNOUNCEMENT IS REFUSED BY NAME and sets nothing.
	"message-second-announcement-accepted": {{
		old: "\tif v.announced || v.refused {\n",
		new: "\tif false { // SABOTAGED: a second announcement is accepted\n",
	}},

	// A FIELD ID TWICE IN ONE BODY IS DAMAGE, in an announcement as in any
	// file (§6.5): the announcement's body is checked whole before its two
	// fields are read, and that check is what refuses a build version or a
	// vocabulary present twice before the exactly-once count sees them. It
	// alone stands behind an UNKNOWN field present twice; take it out and
	// that announcement sets a vocabulary.
	"message-announcement-field-twice-accepted": {{
		old: "\t\tif seen[id] {\n\t\t\treturn c.damage(\"the same field id twice in one body\")\n\t\t}\n",
		new: "\t\t// SABOTAGED: the same field id twice in one body is accepted\n",
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
		old: "\t\t\tif !ok || v > uint64(TableMessageKindBits(kind)) {\n",
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
// under a moved slot, because the reader resolves whatever the writer named, so what
// goes red is the round trip against the corpus's own bytes.
var messageEmitterSabotages = map[string][]edit{
	// THE WRITER'S SLOT NUMBERS ARE COMPILE-TIME CONSTANTS: every emitted slot
	// off by one writes a legal body that names another field.
	"message-emitter-slot-off-by-one": {{
		old: "\treturn slot\n}\n\n// msgMeasureCall",
		new: "\treturn slot + 1 // SABOTAGED: every emitted slot off by one\n}\n\n// msgMeasureCall",
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

	// THE WRITER ROUNDS TO NEAREST, twice in float32: truncate instead and the
	// rounding tie at 0.005 falls to 0 where the packet wire writes 1.
	"message-quantize-truncates": {{
		old: "\tindex := uint32(math.Floor(float64(float32(scaled + 0.5))))\n",
		new: "\tindex := uint32(scaled) // SABOTAGED: truncated, never rounded\n",
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

	// A RANGED 128-BIT VALUE READS AT ITS ANNOUNCED WIDTH (M2): read the raw
	// sixteen bytes instead.
	"message-wide-reads-raw": {{
		old: "\t\tif shape.Packing == ir.TableMessageRanged {\n\t\t\toffset, ok := d.r.getBig(width)\n",
		new: "\t\tif false { // SABOTAGED: a ranged 128-bit value reads raw\n\t\t\toffset, ok := d.r.getBig(width)\n",
	}},

	// THE BOUND APPLIES WHILE THE VALUE IS WIDE (M6): narrow first and 260
	// read by a [0, 10] becomes 4, which reads where the wide value refuses.
	"message-narrow-before-clamp": {{
		old: "\t\t\tu, ulo, uhi := uint64(value), f.IntMin.Uint64(), f.IntMax.Uint64()\n",
		new: "\t\t\tu, ulo, uhi := uint64(value)&(uint64(1)<<uint(f.Type.Width)-1), f.IntMin.Uint64(), f.IntMax.Uint64() // SABOTAGED: narrowed before the clamp\n",
	}},
}

func init() {
	maps.Copy(sabotages, messageRoundTwoSabotages)
	maps.Copy(sabotages, messageColdReadSabotages)
}

// THE COLD READ'S ROWS (docs/SPEC-TABLES.md §3.3, schema#557): the rules a
// reader of the page against the engine found unenforced, each with the one
// rule its red clause names taken out.
var messageColdReadSabotages = map[string][]edit{
	// AN ANNOUNCED MAX IS BOUNDED BY WHAT THE KIND CAN HOLD: lift the ceiling
	// and a string length bound of 2^31 is accepted, which is a bound no
	// declaration this checker passes can produce.
	"message-max-above-int32": {{
		old: "\t\tif !ok || v > math.MaxInt32 {\n\t\t\treturn s, 0, false // A MAX ABOVE WHAT THE KIND CAN HOLD is a hostile width\n\t\t}\n",
		new: "\t\tif !ok { // SABOTAGED: an announced max is unbounded\n\t\t\treturn s, 0, false\n\t\t}\n",
	}},

	// A QUANTIZED INDEX ABOVE `count` IS REJECTED as the packet wire rejects
	// it: take the refusal out and the dequantize clamps it instead, which is
	// the ranged offset's rule reaching a kind it does not govern.
	"message-quantized-index-above-count": {{
		old: "\tcount, _, derived := ir.TableMessageQuantization(shape)\n\tif !derived || index > uint64(count) {\n\t\td.damage()\n\t\treturn 0, false\n\t}\n",
		new: "\t// SABOTAGED: an index above the step count is clamped, never rejected\n",
	}},

	// A FAILED STRICT CHECK REFUSES THE ANNOUNCEMENT and sets no vocabulary.
	// Which reason it names is not the contract (docs/rulings.json,
	// refusal-verdict-only), so the blade takes the refusal itself out: an
	// announcement missing its build version or its vocabulary is accepted.
	"message-strict-check-ignored": {{
		old: "\tif !read {\n\t\treturn damage(\"an announcement without its build version and vocabulary\")\n\t}\n",
		new: "\tif false && !read { // SABOTAGED: a failed strict check is ignored\n\t\treturn damage(\"an announcement without its build version and vocabulary\")\n\t}\n",
	}},

	// A TRIPLE ALREADY PLACED IS NEVER PLACED TWICE: drop the scan and a
	// vocabulary carrying one entry twice is accepted.
	"message-duplicate-entry-accepted": {{
		old: "\t\tfor _, seen := range out {\n\t\t\tif seen.Id == e.Id && seen.Kind == e.Kind && seen.Key() == e.Key() {\n\t\t\t\treturn nil, false\n\t\t\t}\n\t\t}\n",
		new: "\t\t// SABOTAGED: a triple may be placed twice\n",
	}},

	// AN ELEMENT KIND OF 12 IS A SHAPE LIKE ANY OTHER (§3.3): a text
	// list is a kind 14 array over kind 12, so refuse that element at the
	// announcement and a unit with a text list is refused its own vocabulary.
	"message-array-of-text-refused": {{
		old: "\t\t\treturn s, 0, false // an element kind outside the closed set\n\t\t}\n",
		new: "\t\t\treturn s, 0, false // an element kind outside the closed set\n\t\t}\n\t\tif s.Elem == TableKindString {\n\t\t\treturn s, 0, false // SABOTAGED: an array over element kind 12 is refused\n\t\t}\n",
	}},

	// A VARIANT REFERENCE IS RESOLVED EVEN ON THE SKIP PATH: step over its
	// bits instead and a reference above E rides through an unknown field.
	"message-skipped-variant-unresolved": {{
		old: "\t\tif ref == 0 {\n\t\t\treturn true // None: the reference is the whole payload\n\t\t}\n\t\t_, named := d.name(ref)\n\t\treturn named\n",
		new: "\t\t_ = ref // SABOTAGED: a skipped variant reference is not resolved\n\t\treturn true\n",
	}},

	// THE BOUND APPLIES WHILE THE COUNT IS WIDE, in the C++ emitter's array
	// read: narrow to int32 first and a count of 2^31 + 1 is negative, which
	// passes the signed test against the bound and lands in caller storage.
	"message-emitter-narrow-count-before-clamp": {{
		old: "\tg.pf(\"%s    int32_t kept%s = 0;\\n\", ind, sfx)\n\tg.pf(\"%[1]s    if ( n%[2]s > (uint64_t) %[3]d )\\n%[1]s    {\\n%[1]s        kept%[4]s = %[5]d;\\n%[1]s        report->refused = true;\\n%[1]s        report->refuse_reason = out_of_range;\\n%[1]s        } else { kept%[6]s = (int32_t) n%[7]s;\\n%[1]s    }\\n\", ind, sfx, f.ArrayBound, sfx, f.ArrayBound, sfx, sfx)\n",
		new: "\tg.pf(\"%s    int32_t kept%s = (int32_t) n%s;\\n\", ind, sfx, sfx) // SABOTAGED: narrowed before the bound\n\tg.pf(\"%[1]s    if ( kept%[2]s > %[3]d )\\n%[1]s    {\\n%[1]s        kept%[2]s = %[3]d;\\n%[1]s        report->refused = true;\\n%[1]s        report->refuse_reason = out_of_range;\\n%[1]s    }\\n\", ind, sfx, f.ArrayBound)\n",
	}},
}

func init() {
	maps.Copy(sabotages, messageTextSabotages)
}

// THE MESSAGE FORM'S ONE CONTENT RULE AND ITS CLAMP (docs/SPEC-TABLES.md §3.3,
// schema#620): a kind 12 payload that is not well-formed UTF-8, or that
// carries a zero byte, is DAMAGE and terminal for the batch, and a payload
// past the reader's bound refuses (the strict read replaced the clamp). The
// blades are the content rule and the bound in the Go oracle, and the content
// rule (and the retired clamp) in the C++ emitter. Each names the row
// TestTheMessageFormsTextContentRuleAndClamp and test/tables/message_text_main.cpp
// hold.
var messageTextSabotages = map[string][]edit{
	// ILL-FORMED TEXT IS DAMAGE HERE TOO: take the check out of the oracle's
	// message path and the variable form still refuses what the message form now
	// stores, which is the two forms parting on the one rule they share.
	"message-text-accepts-ill-formed": {{
		old: "\tif f.Type.Kind == ir.TString && !textValid(raw) {\n",
		new: "\tif false && !textValid(raw) { // SABOTAGED: the kind 12 content rule is gone\n",
	}},

	// A PAYLOAD PAST THE READER'S BOUND REFUSES length_over_bound and no
	// prefix of it is kept (§6.5): the strict read replaced the clamp that cut
	// at a code point boundary. Take the refusal out and the oracle keeps an
	// over-long payload whole.
	"message-text-over-bound-kept": {{
		old: "\tif len(raw) > int(f.Type.Size) {\n\t\td.refuse(ReasonLengthOverBound)\n\t\treturn false\n\t}\n",
		new: "\t// SABOTAGED: a payload past the bound is kept\n",
	}},

	// AND THE SAME TWO IN THE C++ EMITTER, which is the one the reference
	// reads with. The content rule first: the emitted reader stops calling
	// TableUtf8Valid and stores whatever arrived.
	"message-emitter-text-accepts-ill-formed": {{
		old: "\t\tg.pf(\"%[1]s    if ( !TableUtf8Valid( text%[2]s, n%[3]s ) )\\n%[1]s    {\\n%[1]s        report->damaged = true;\\n%[1]s        return false;\\n%[1]s    }\\n\", ind, sfx, sfx)\n",
		new: "\t\tg.pf(\"%s    // SABOTAGED: the kind 12 content rule is gone\\n\", ind)\n",
	}},

	// then the clamp: the emitted reader bounds the length itself instead of
	// asking TableUtf8Clamp where the last whole code point ends.
	"message-emitter-text-clamp-off-boundary": {{
		old: "\t\tg.pf(\"%s    const int32_t kept%s = (int32_t) TableUtf8Clamp( text%s, n%s, %d );\\n\", ind, sfx, sfx, sfx, f.Type.Size)\n",
		new: "\t\tg.pf(\"%s    const int32_t kept%s = n%s > (uint64_t) %d ? (int32_t) %d : (int32_t) n%s; // SABOTAGED: the clamp is off the boundary\\n\", ind, sfx, sfx, f.Type.Size, f.Type.Size, sfx)\n",
	}},
}
