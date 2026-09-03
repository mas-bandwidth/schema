// The TEXT form in JavaScript: JSON in and out of one table, driven by the
// reflection descriptors (docs/SPEC-TABLES.md §16).
//
// ONE generic walk, emitted ONCE PER UNIT — into the same module the rest of
// the unit's shared table runtime goes, <Package>Table.js — because an ES
// module is file-scoped and a second copy would be a second, unequal symbol.
// That is the block-home rule's shape, and it makes the same gate available:
// the walker's source is the SAME BYTES in every unit's generated output
// (`make tables-js-json-walk`).
//
// THE C++ BACKEND IS THE REFERENCE and this file mirrors it: the same
// classifier, the same shapes, the same clamps, the same report events, the
// same acceptance of a trailing comma and the same refusal of a comment. It is
// NOT JSON.parse and NOT JSON.stringify, and could not be: §16's clamping,
// counting, duplicate-key and trailing-comma rules are the form, and the
// output must be byte-identical to the goldens down to a float's `%.*g`
// spelling — so the C spelling is written out here rather than borrowed from a
// runtime that does not have it.
//
// Where JavaScript forces a different spelling the reason is stated at the
// site, and there are exactly six:
//
//   - The text crosses the boundary as a STRING — the language's currency for
//     text — encoded once on the way in and decoded once on the way out;
//     C++ takes a buffer and a length. A Uint8Array is taken too, for a text
//     read straight off a file, and walked as it is.
//   - Storage is reached through the descriptor's ACCESSORS rather than an
//     offset and a width: a JavaScript field has no address. C++'s `storage`
//     pointer becomes the triple (owner, field, index) throughout.
//   - The raw numeric currency is BigInt, which is the one JavaScript type
//     that holds sixty-four bits without loss; it is C++'s `uint64_t` and C#'s
//     `ulong` exactly, and it is why this path allocates.
//   - The number grammar's conversions go through Number(token) and
//     Math.fround, which are correctly rounded by specification and locale-free
//     by construction — JavaScript has no decimal separator to cross.
//   - A JSON string is written from TWO sources — a descriptor's key, which is
//     a JavaScript string of UTF-16 code units, and a string(N)'s storage,
//     which is bytes — where C++ has char* for both. The escaping rule is one
//     rule, spelled at each source.
//   - The walk lives inside one closure, `TableJson`, so its members claim
//     nothing at module scope (docs/SPEC-TABLES.md §11).
package jstable

import "github.com/mas-bandwidth/schema/v2/ir"

// emitJsonSurface emits one closure member's text-form surface: two thin
// wrappers over the generic walk, each naming a descriptor and nothing else.
//
// TEXT IS A STRING HERE, which is the language's currency for it: <Name>ToJson
// answers a string and <Name>FromJson takes one — or a Uint8Array, for a text
// read straight off a file — where C++ measures, takes a buffer and answers a
// count. The text form is the generic, tooling path and allocates by design
// (docs/SPEC-TABLES.md §16), so the string costs nothing the path was not
// already paying. FromJson hands the value back, as Load does.
func (g *tableGen) emitJsonSurface(st *ir.Struct) {
	g.needRuntime("TableJson")
	g.pf("// %s in and out of a JSON text — one instance, one text, the generic\n", st.Name)
	g.pf("// walk over this type's descriptors (docs/SPEC-TABLES.md §16).\n")
	g.pf("export function %sFromJson(text, report, value) {\n", st.Name)
	g.pf("  if (value === undefined || value === null) { value = new %s(); }\n", st.Name)
	g.pf("  TableJson.read(value, %sTableType(), text, report);\n", st.Name)
	g.pf("  return value;\n}\n\n")
	g.pf("export function %sToJson(value) {\n", st.Name)
	g.pf("  return TableJson.write(value, %sTableType());\n}\n\n", st.Name)
}

// tableJsonWalkSource is the walker. It reads and writes ONLY the columns every
// unit's descriptors carry, so its text never varies with the unit — which is
// what the generic-walk gate asserts.
const tableJsonWalkSource = `// ---- json walk: begin ----
//
// The TEXT form (docs/SPEC-TABLES.md §16): one table, one text, one walk over the
// reflection descriptors (§8). Reading fills ONE caller-owned instance;
// writing targets a caller buffer with the wire's measure/write symmetry.
// Everything AROUND this — which file goes with which instance, what key an
// instance is filed under, how instances link into a root table's collections
// — is a packer's opinion and stays with the tool that holds it.
//
// The dialect: trailing commas are accepted on read (the authoring files this
// exists for carry them) and never written; comments are not JSON and are
// refused; unknown keys are skipped and counted; a duplicate key is last-wins
// and counted; a key present with the wrong JSON type is skipped and counted,
// never coerced. The canonical text ends with exactly ONE newline, which the
// writer emits and the reader accepts with or without.
//
// Everything below is inside this closure, so the walk claims not one name at
// module scope (docs/SPEC-TABLES.md §11).
export const TableJson = (() => {
  const MaxDepth = 128;

  // A key longer than this cannot name a field, so it is skipped as unknown.
  const MaxKey = 256;

  // The longest numeric token the walk will convert. Anything longer is a
  // value no field can hold and counts as a kind mismatch.
  const MaxNumber = 512;

  const Base64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

  // the boundary between the language's text and the form's bytes: a string in
  // is encoded once, and the bytes the walk wrote are decoded once on the way out
  const Utf8Encoder = new TextEncoder();
  const Utf8Decoder = new TextDecoder("utf-8");

  // The number token is ASCII by JSON's own grammar, so a byte-per-character
  // decode is exact and the walk never meets a multi-byte sequence here.
  const AsciiDecoder = new TextDecoder("latin1");

  // ONE scratch for every scanned key and vocabulary name. It is safe shared
  // because no scan is live across a recursion: a key is scanned, matched, and
  // finished with before the value under it is read.
  const ScanKey = new Uint8Array(MaxKey);

  // Duplicate tracking, bounded and allocation-free: 512 bits per frame,
  // indexed by depth, so a nested read never clears its parent's. A table with
  // more fields than this still reads; its repeats simply stop being counted.
  // The two pools are separate because a table frame and the keyed field
  // inside it are live at the same depth at the same time.
  const SeenWords = 16;
  const SeenTable = new Uint32Array((MaxDepth + 2) * SeenWords);
  const SeenKeyed = new Uint32Array((MaxDepth + 2) * SeenWords);

  function seenClear(pool, depth) {
    pool.fill(0, depth * SeenWords, depth * SeenWords + SeenWords);
  }

  // returns true when the index was already marked
  function seenMark(pool, depth, index) {
    if (index < 0 || index >= SeenWords * 32) { return false; }
    const at = depth * SeenWords + (index >>> 5);
    const bit = 1 << (index & 31);
    const had = (pool[at] & bit) !== 0;
    pool[at] |= bit;
    return had;
  }

  // A vocabulary entry the descriptor could not spell. The generated name
  // functions answer "???" for a value outside the declared set, and that is
  // not a name — writing it would put a spelling in the text that the reader
  // then counts as unknown, turning a refusal into a silent loss.
  function named(name) {
    return typeof name === "string" && name !== "???";
  }

  // finite: not a NaN, not an infinity.
  function finite(v) {
    return Number.isFinite(v);
  }

  // a counted field's companion: a string's length, a bytes' length, a counted
  // array's count. Bounded by the declared extent on the way out, so a storage
  // invariant a caller broke cannot walk off the end of the array.
  function count(value, f) {
    if (!f.Counted) { return f.ArrayBound; }
    let n = f.GetCount(value);
    if (!(n >= 0)) { n = 0; }
    if (n > f.ArrayBound) { n = f.ArrayBound; }
    return n;
  }

  function putCount(value, f, n) {
    if (f.Counted) { f.SetCount(value, n); }
  }

  function tableOf(f) {
    return f.TableRef === null ? null : f.TableRef();
  }

  function armTableOf(arm) {
    return arm.TableRef === null ? null : arm.TableRef();
  }

  // ---- what a field's kind expects to see in the text ----
  //
  // One classifier, consulted by both directions, so a reader and a writer
  // can never disagree about a kind's JSON form. 'o' object, 'a' array, 's'
  // string, 'n' number, 'b' boolean.
  //
  // A vocabulary field is spelled by NAME: an enum is one name, a flags mask
  // is the array of the names of its set bits. The two are told apart by the
  // id column — an enum variant rides under a wire id, a flags BIT never
  // does (docs/SPEC-TABLES.md §4), so a name function with no id function is
  // flags.
  //
  // bytes(N) is the one kind whose element kind does not decide its form: it
  // shares u8 with a plain array of u8, and rides as base64. The schema type
  // name settles it, and "bytes" is a keyword no declaration can claim.
  function isBytes(f) {
    return f.IsArray && f.Kind === 6 && f.TypeName === "bytes";
  }

  // An ENUM-KEYED array (docs/SPEC-TABLES.md §2.4): its JSON form is an OBJECT
  // keyed by variant name, not a positional array, because that is what the
  // storage is — one slot per variant, addressed by the variant.
  function isKeyed(f) {
    return f.KeyName !== null;
  }

  // THE KEY A STORAGE SLOT HOLDS (§2.4, §8): the storage shifts left, so slot
  // i holds the key i + 1 and nothing is stored for None. This is the ONE
  // place the walker spells the shift.
  function keyedSlotKey(slot) {
    return slot + 1;
  }

  // A slot whose key names a variant of the keying enum. Every slot in
  // [0, arrayBound) does, unless the enum carries max-headroom variants
  // outside a table closure, where a reserved value names nothing and its key
  // id is 0 — the reserved id no declared name can fold to (§5).
  function keyedSlotValid(f, slot) {
    return f.KeyId(keyedSlotKey(slot)) !== 0;
  }

  function isFlags(f) {
    return f.EnumName !== null && f.VariantId === null;
  }

  function isEnum(f) {
    return f.VariantId !== null && f.Arms === null;
  }

  function shape(f) {
    if (f.Kind === 12) { return "s"; }          // string
    if (isBytes(f)) { return "s"; }             // bytes: base64
    if (isKeyed(f)) { return "o"; }             // an object keyed by variant NAME
    if (f.IsArray) { return "a"; }
    if (f.Arms !== null) { return "o"; }        // union: an object with ONE key
    if (f.Kind === 13) { return "o"; }          // nested table or type
    if (isEnum(f)) { return "s"; }
    if (isFlags(f)) { return "a"; }
    if (f.Kind === 1) { return "b"; }
    return "n";
  }

  // the ELEMENT shape of an array field — the same classifier one level down
  function elementShape(f) {
    if (f.Kind === 13) { return "o"; }
    if (isEnum(f)) { return "s"; }
    if (isFlags(f)) { return "a"; }
    if (f.Kind === 1) { return "b"; }
    return "n";
  }

  // A guarded group rides only when its guard reads true — the wire's own
  // elision (§4), carried into the text so a text and a wire written from one
  // instance say the same thing. The guard is spelled as its branch condition
  // over bool fields of the SAME type ("at_rest", "!at_rest",
  // "active && has_target"), so evaluating it is a walk of the same
  // descriptor. Nothing is inferred in the other direction: reading places
  // every key it can name, and the guard is a plain bool key (§16.2).
  function guardHolds(value, info, guard) {
    let p = 0;
    for (;;) {
      while (p < guard.length && (guard[p] === " " || guard[p] === "&")) { p++; }
      if (p >= guard.length) { return true; }
      let want = true;
      if (guard[p] === "!") { want = false; p++; }
      const start = p;
      while (p < guard.length && guard[p] !== " " && guard[p] !== "&") { p++; }
      const name = guard.slice(start, p);
      let held = false;
      for (let i = 0; i < info.NumFields; i++) {
        const f = info.Fields[i];
        if (f.Name === name) {
          held = f.GetRaw(value, 0) !== 0n;
          break;
        }
      }
      if (held !== want) { return false; }
    }
  }

  // ---- writing ----

  // The writer sink MEASURES when measuring is set and WRITES when it is not,
  // over one code path — so measure and write agree byte for byte, the wire's
  // invariant (§9) carried across.
  class Out {
    constructor(buffer, measuring) {
      this.Bytes = buffer;
      this.measuring = measuring;
      this.Offset = 0;
      this.Overflow = false;
    }

    raw(source, from, length) {
      if (!this.measuring) {
        if (this.Offset + length > this.Bytes.length) { this.Overflow = true; return; }
        this.Bytes.set(source.subarray(from, from + length), this.Offset);
      }
      this.Offset += length;
    }

    put(c) {
      if (!this.measuring) {
        if (this.Offset + 1 > this.Bytes.length) { this.Overflow = true; return; }
        this.Bytes[this.Offset] = c;
      }
      this.Offset += 1;
    }

    // an ASCII literal of the walk's own — "true", ": ", "[]" — never a
    // value, so widening each character is the whole encoding
    text(s) {
      for (let i = 0; i < s.length; i++) { this.put(s.charCodeAt(i)); }
    }

    line(depth) {
      this.put(0x0a);
      for (let i = 0; i < depth; i++) { this.put(0x20); this.put(0x20); }
    }
  }

  function writeBase64(o, data, length) {
    o.put(0x22);
    let i = 0;
    for (; i + 3 <= length; i += 3) {
      const triple = (data[i] << 16) | (data[i + 1] << 8) | data[i + 2];
      o.put(Base64Alphabet.charCodeAt((triple >> 18) & 0x3f));
      o.put(Base64Alphabet.charCodeAt((triple >> 12) & 0x3f));
      o.put(Base64Alphabet.charCodeAt((triple >> 6) & 0x3f));
      o.put(Base64Alphabet.charCodeAt(triple & 0x3f));
    }
    if (i < length) {
      const left = length - i;
      let triple = data[i] << 16;
      if (left === 2) { triple |= data[i + 1] << 8; }
      o.put(Base64Alphabet.charCodeAt((triple >> 18) & 0x3f));
      o.put(Base64Alphabet.charCodeAt((triple >> 12) & 0x3f));
      o.put(left === 2 ? Base64Alphabet.charCodeAt((triple >> 6) & 0x3f) : 0x3d);
      o.put(0x3d);
    }
    o.put(0x22);
  }

  // One UTF-8 sequence at s[at], or -1 when the bytes there are not one.
  // Rejects the lot: a stray continuation, an overlong form, a surrogate half,
  // and anything past U+10FFFF. The sequence's width comes back in Utf8Width,
  // which is consumed in the same expression that fills it.
  const Utf8Width = { value: 0 };

  function utf8(s, length, at) {
    Utf8Width.value = 0;
    const lead = s[at];
    let want;
    let code;
    if (lead < 0x80) { Utf8Width.value = 1; return lead; }
    else if (lead >= 0xc2 && lead <= 0xdf) { want = 2; code = lead & 0x1f; }
    else if (lead >= 0xe0 && lead <= 0xef) { want = 3; code = lead & 0x0f; }
    else if (lead >= 0xf0 && lead <= 0xf4) { want = 4; code = lead & 0x07; }
    else { return -1; }
    if (length - at < want) { return -1; }
    for (let i = 1; i < want; i++) {
      const next = s[at + i];
      if ((next & 0xc0) !== 0x80) { return -1; }
      code = (code << 6) | (next & 0x3f);
    }
    if (want === 3 && code < 0x800) { return -1; }        // overlong
    if (want === 4 && code < 0x10000) { return -1; }      // overlong
    if (code >= 0xd800 && code <= 0xdfff) { return -1; }  // a surrogate half
    if (code > 0x10ffff) { return -1; }
    Utf8Width.value = want;
    return code;
  }

  // The escape rule, spelled once and reached from both string sources: a
  // code point the JSON grammar names, a control character as \u00XX, and
  // anything else as itself. Returns false when the caller must emit the
  // code point's own bytes instead.
  function writeEscape(o, code) {
    switch (code) {
      case 0x22: o.text("\\\""); return true;
      case 0x5c: o.text("\\\\"); return true;
      case 0x08: o.text("\\b"); return true;
      case 0x0c: o.text("\\f"); return true;
      case 0x0a: o.text("\\n"); return true;
      case 0x0d: o.text("\\r"); return true;
      case 0x09: o.text("\\t"); return true;
    }
    if (code < 0x20) {
      const hex = "0123456789abcdef";
      o.text("\\u00");
      o.put(hex.charCodeAt(code >> 4));
      o.put(hex.charCodeAt(code & 0xf));
      return true;
    }
    return false;
  }

  function writeUtf8(o, code) {
    if (code < 0x80) { o.put(code); return; }
    if (code < 0x800) {
      o.put(0xc0 | (code >> 6));
      o.put(0x80 | (code & 0x3f));
      return;
    }
    if (code < 0x10000) {
      o.put(0xe0 | (code >> 12));
      o.put(0x80 | ((code >> 6) & 0x3f));
      o.put(0x80 | (code & 0x3f));
      return;
    }
    o.put(0xf0 | (code >> 18));
    o.put(0x80 | ((code >> 12) & 0x3f));
    o.put(0x80 | ((code >> 6) & 0x3f));
    o.put(0x80 | (code & 0x3f));
  }

  // A JSON text MUST be valid UTF-8 (RFC 8259 §8.1). The read path is
  // byte-transparent — the wire imposes no encoding (§3) and a string may
  // hold anything — so the WRITER is where that obligation is met: a byte
  // that is not part of a well-formed sequence is written as U+FFFD, one per
  // bad byte, and never raw. A text this walk writes is therefore readable by
  // any conforming parser, which a raw byte would not be. The cost is stated
  // plainly: for a string holding invalid UTF-8, the round trip is NOT
  // byte-identical, because the alternative is emitting a text that is not
  // JSON.
  function writeString(o, s, length) {
    o.put(0x22);
    for (let i = 0; i < length; i++) {
      const c = s[i];
      if (writeEscape(o, c)) { continue; }
      if (c < 0x80) { o.put(c); continue; }
      if (utf8(s, length, i) < 0) {
        writeUtf8(o, 0xfffd); // one per bad byte
      } else {
        const width = Utf8Width.value;
        o.raw(s, i, width);
        i += width - 1;
      }
    }
    o.put(0x22);
  }

  // The same rule at the OTHER source: a descriptor's key or vocabulary name,
  // which JavaScript holds as a string of UTF-16 code units. C++ has one
  // writer because both of its sources are char*; here a surrogate pair is
  // recombined and a lone half — which a schema identifier cannot produce and
  // a json = "key" attribute could not carry either — reads as U+FFFD, the
  // same answer the byte path gives an ill-formed sequence.
  function writeName(o, s) {
    o.put(0x22);
    for (let i = 0; i < s.length; i++) {
      let code = s.charCodeAt(i);
      if (code >= 0xd800 && code <= 0xdbff && i + 1 < s.length &&
          s.charCodeAt(i + 1) >= 0xdc00 && s.charCodeAt(i + 1) <= 0xdfff) {
        code = 0x10000 + ((code - 0xd800) << 10) + (s.charCodeAt(i + 1) - 0xdc00);
        i++;
      } else if (code >= 0xd800 && code <= 0xdfff) {
        code = 0xfffd;
      }
      if (writeEscape(o, code)) { continue; }
      writeUtf8(o, code);
    }
    o.put(0x22);
  }

  const DigitScratch = new Uint8Array(24);

  function writeUnsigned(o, value) {
    let v = value;
    let n = 0;
    do {
      DigitScratch[n++] = 0x30 + Number(v % 10n);
      v /= 10n;
    } while (v !== 0n);
    for (let i = n - 1; i >= 0; i--) { o.put(DigitScratch[i]); }
  }

  function writeSigned(o, value) {
    if (value < 0n) {
      o.put(0x2d);
      writeUnsigned(o, 0n - value);
      return;
    }
    writeUnsigned(o, value);
  }

  function trimZeros(s) {
    if (s.indexOf(".") < 0) { return s; }
    let n = s.length;
    while (n > 0 && s[n - 1] === "0") { n--; }
    if (n > 0 && s[n - 1] === ".") { n--; }
    return s.slice(0, n);
  }

  // THE EXACT DECIMAL OF A DOUBLE. Every double is a dyadic rational, so its
  // decimal expansion terminates: value = ±digits x 10^exp10, exactly, with no
  // rounding anywhere in the derivation.
  function decimalOf(value) {
    TableBitsScratch.setFloat64(0, value, true);
    const bits = TableBitsScratch.getBigUint64(0, true);
    const negative = (bits >> 63n) === 1n;
    let biased = Number((bits >> 52n) & 0x7ffn);
    let mantissa = bits & 0xfffffffffffffn;
    if (biased === 0) { biased = 1; } else { mantissa |= 0x10000000000000n; }
    const e = biased - 1075; // value = mantissa x 2^e
    if (mantissa === 0n) { return { negative, digits: 0n, exp10: 0 }; }
    if (e >= 0) { return { negative, digits: mantissa << BigInt(e), exp10: 0 }; }
    // m / 2^k = m x 5^k / 10^k, and both are exact
    return { negative, digits: mantissa * 5n ** BigInt(-e), exp10: e };
  }

  // C's "%.*g" for a finite double, spelled out: round to the given number of significant
  // digits, then the exponent form when the decimal exponent is below -4 or at
  // least the precision and the plain form otherwise, trailing zeros and a
  // trailing point removed, and an exponent of at least two digits, always
  // signed.
  //
  // THE ROUNDING IS HALF-TO-EVEN, DONE HERE, and that is the whole reason this
  // does its own arithmetic rather than calling toExponential and toFixed.
  // Those two are correctly rounded but they break a TIE by magnitude, where C
  // breaks it to even — and the tie is reachable: a float32 near 2^18 holds
  // -266744.625 exactly, whose eight-digit rendering is a tie, and both
  // candidates round-trip back to the same float32, so the search below cannot
  // step past it. C writes -266744.62 and a magnitude tie-break writes
  // -266744.63. The compiler's own Go engine is the third implementation that
  // found it.
  //
  // A NEGATIVE ZERO KEEPS ITS SIGN for the same reason: C prints "-0" and
  // JavaScript's own formatters drop the sign.
  function formatG(value, prec) {
    if (prec < 1) { prec = 1; }
    const decimal = decimalOf(value);
    const sign = decimal.negative ? "-" : "";
    if (decimal.digits === 0n) { return sign + "0"; }
    let digits = decimal.digits;
    let exp10 = decimal.exp10;
    let text = digits.toString();
    if (text.length > prec) {
      const drop = text.length - prec;
      const scale = 10n ** BigInt(drop);
      let quotient = digits / scale;
      const remainder = digits % scale;
      const half = scale / 2n;
      if (remainder > half || (remainder === half && (quotient & 1n) === 1n)) { quotient += 1n; }
      digits = quotient;
      exp10 += drop;
      text = digits.toString();
      if (text.length > prec) { // the round carried into a new digit: 999 -> 1000
        digits /= 10n;
        exp10 += 1;
        text = digits.toString();
      }
    }
    // the decimal exponent %g decides the form by: the power of ten the
    // leading digit sits at
    const exponent = text.length - 1 + exp10;
    if (exponent < -4 || exponent >= prec) {
      const mantissa = text.length === 1 ? text : trimZeros(text[0] + "." + text.slice(1));
      const magnitude = exponent < 0 ? -exponent : exponent;
      const spelled = magnitude < 10 ? "0" + magnitude : String(magnitude);
      return sign + mantissa + "e" + (exponent < 0 ? "-" : "+") + spelled;
    }
    if (exponent >= text.length - 1) {
      return sign + text + "0".repeat(exponent - (text.length - 1));
    }
    if (exponent >= 0) {
      return sign + trimZeros(text.slice(0, exponent + 1) + "." + text.slice(exponent + 1));
    }
    return sign + trimZeros("0." + "0".repeat(-exponent - 1) + text);
  }

  // DECIMAL -> FLOAT32, CORRECTLY ROUNDED, and it has to be spelled out.
  //
  // The reference converts a float32 token with strtof, which rounds the
  // decimal to binary32 ONCE. JavaScript's only parser answers a double, and
  // Math.fround over it rounds a second time — which agrees with strtof
  // everywhere except where the double lands exactly on a float32 midpoint,
  // and there the two disagree at the widest possible value: the decimal just
  // below 2^128 - 2^103 is FLT_MAX to strtof and Infinity to a double rounded
  // twice. The hostile corpus has that number in it by name, so the conversion
  // is done exactly, in the one JavaScript type that can hold the digits.
  //
  // value = sign * mantissa * 10^exp10, rounded to twenty-four significant
  // bits with ties to even, floored at the subnormal step 2^-149.
  function bitLength(x) {
    return x === 0n ? 0 : x.toString(2).length;
  }

  function toFloat32(token) {
    let i = 0;
    let negative = false;
    if (token[0] === "-") { negative = true; i = 1; }
    let digits = "";
    while (i < token.length && token[i] >= "0" && token[i] <= "9") { digits += token[i++]; }
    let exp10 = 0;
    if (i < token.length && token[i] === ".") {
      i++;
      while (i < token.length && token[i] >= "0" && token[i] <= "9") { digits += token[i++]; exp10--; }
    }
    if (i < token.length && (token[i] === "e" || token[i] === "E")) {
      i++;
      let sign = 1;
      if (token[i] === "+") { i++; } else if (token[i] === "-") { sign = -1; i++; }
      let e = 0;
      while (i < token.length && e < 1000000) { e = e * 10 + (token.charCodeAt(i++) - 48); }
      exp10 += sign * e;
    }
    let mantissa = digits.length === 0 ? 0n : BigInt(digits);
    if (mantissa === 0n) { return negative ? -0 : 0; }
    // strip the zeros the digits carry, so the exact arithmetic below stays
    // small for a token spelled with a long tail
    while (mantissa % 10n === 0n) { mantissa /= 10n; exp10++; }
    const magnitude = exp10 + mantissa.toString().length;
    // far outside binary32's range the answer needs no arithmetic: FLT_MAX is
    // below 10^39 and the smallest subnormal is above 10^-46
    if (magnitude > 45) { return negative ? -Infinity : Infinity; }
    if (magnitude < -55) { return negative ? -0 : 0; }
    let num = mantissa;
    let den = 1n;
    if (exp10 >= 0) { num *= 10n ** BigInt(exp10); } else { den = 10n ** BigInt(-exp10); }
    // the binary exponent of the leading bit: bit lengths bracket it, and one
    // comparison settles which of the two it is
    const shift = bitLength(num) - bitLength(den);
    const atLeast = shift >= 0 ? num >= (den << BigInt(shift)) : (num << BigInt(-shift)) >= den;
    let k = (atLeast ? shift : shift - 1) - 23;
    if (k < -149) { k = -149; } // the subnormal floor: every step is a multiple of 2^-149
    const n = k >= 0 ? num : num << BigInt(-k);
    const d = k >= 0 ? den << BigInt(k) : den;
    let q = n / d;
    const twice = (n % d) * 2n;
    if (twice > d || (twice === d && (q & 1n) === 1n)) { q += 1n; }
    if (q === 1n << 24n) { q = 1n << 23n; k += 1; } // the round carried into a new binade
    const value = Number(q) * Math.pow(2, k);
    if (value > 3.4028234663852886e38) { return negative ? -Infinity : Infinity; }
    return negative ? -value : value;
  }

  // A float writes at the SHORTEST precision that reads back as the same
  // value at the field's own width, so a round trip is exact and a text stays
  // readable. Non-finite values have no JSON spelling at all, and the writer
  // REFUSES rather than losing one silently — the same rule measure and save
  // already apply to an enum value no variant names (§5).
  function writeFloat(o, value, single) {
    if (!finite(value)) { return false; }
    const low = single ? 6 : 15;
    const high = single ? 9 : 17;
    let text = null;
    for (let digits = low; ; digits++) {
      text = formatG(value, digits);
      if (text === null || text.length === 0) { return false; }
      if (digits >= high) { break; }
      if (single) {
        // the reference compares against strtof's answer, so this does too
        if (toFloat32(text) === value) { break; }
      } else {
        const back = Number(text);
        if (Number.isNaN(back)) { continue; }
        if (back === value) { break; }
      }
    }
    o.text(text);
    return true;
  }

  // one scalar, at one storage slot: a nested object, a union, a vocabulary,
  // or a number. C++ takes the storage's ADDRESS here; the JavaScript triple
  // (owner, field, index) is the same thing said in this language.
  function writeScalar(o, owner, f, index, depth) {
    if (f.Arms !== null) {
      // a union is an object with ONE key, the arm's name; None is {}
      const union = f.GetChild(owner, index);
      const tag = f.Arms.GetTag(union);
      if (tag === 0) {
        o.text("{}");
        return true;
      }
      if (tag > f.EnumMax || tag < 0) {
        return false; // a tag no arm names, exactly as measure refuses it
      }
      const arm = f.EnumName(tag);
      // and refuse on the NAME, not merely on the bound: §16.2 says a value no
      // variant NAMES is refused, so the check is the name. Writing whatever
      // came back would emit "???", a spelling the reader counts as unknown —
      // a silent round-trip loss in place of a refusal.
      if (!named(arm)) { return false; }
      o.put(0x7b);
      o.line(depth + 1);
      writeName(o, arm);
      o.text(": ");
      if (!writeValue(o, f.Arms.Arms[tag].Payload(union), armTableOf(f.Arms.Arms[tag]), depth + 1)) {
        return false;
      }
      o.line(depth);
      o.put(0x7d);
      return true;
    }
    if (f.Kind === 13) {
      return writeValue(o, f.GetChild(owner, index), tableOf(f), depth);
    }
    if (isEnum(f)) {
      const raw = BigInt.asIntN(64, f.GetRaw(owner, index));
      // a value no variant names has no text spelling, exactly as it has no
      // wire identity: the writer REFUSES rather than writing None over it,
      // the rule measure and save already apply (§5)
      if (raw > BigInt(f.EnumMax) || raw < 0n) { return false; }
      const value = Number(raw);
      if (value !== 0 && f.VariantId(value) === 0) { return false; }
      const name = f.EnumName(value);
      if (!named(name)) { return false; }
      writeName(o, name);
      return true;
    }
    if (isFlags(f)) {
      const bits = f.GetRaw(owner, index);
      if (bits === 0n) {
        o.text("[]");
        return true;
      }
      o.put(0x5b);
      let first = true;
      for (let bit = 0; bit < 64; bit++) {
        if ((bits & (1n << BigInt(bit))) === 0n) { continue; }
        if (bit > f.EnumMax) {
          return false; // a bit no variant names has no text spelling
        }
        const name = f.EnumName(bit);
        if (!named(name)) { return false; }
        if (!first) { o.put(0x2c); }
        first = false;
        o.line(depth + 1);
        writeName(o, name);
      }
      o.line(depth);
      o.put(0x5d);
      return true;
    }
    switch (f.Kind) {
      case 1:
        o.text(f.GetRaw(owner, index) !== 0n ? "true" : "false");
        return true;
      case 10:
        return writeFloat(o, TableBitsToFloat(Number(BigInt.asUintN(32, f.GetRaw(owner, index)))), true);
      case 11:
        return writeFloat(o, TableBitsToDouble(f.GetRaw(owner, index)), false);
      case 2: case 3: case 4: case 5:
        writeSigned(o, BigInt.asIntN(64, f.GetRaw(owner, index)));
        return true;
      default:
        writeUnsigned(o, f.GetRaw(owner, index));
        return true;
    }
  }

  function writeField(o, value, f, depth) {
    if (f.Kind === 12) {
      writeString(o, f.GetBuffer(value), count(value, f));
      return true;
    }
    if (isBytes(f)) {
      writeBase64(o, f.GetBuffer(value), count(value, f));
      return true;
    }
    if (isKeyed(f)) {
      // one entry per SLOT, keyed by the variant that owns it, so inserting a
      // variant next season moves nothing in the text either. Slot i holds the
      // key i + 1: nothing is stored for None, so nothing is written for it.
      o.put(0x7b);
      let first = true;
      for (let slot = 0; slot < f.ArrayBound; slot++) {
        if (!keyedSlotValid(f, slot)) { continue; }
        if (!first) { o.put(0x2c); }
        first = false;
        o.line(depth + 1);
        writeName(o, f.KeyName(keyedSlotKey(slot)));
        o.text(": ");
        if (!writeScalar(o, value, f, slot, depth + 1)) {
          return false;
        }
      }
      if (first) { o.put(0x7d); return true; }
      o.line(depth);
      o.put(0x7d);
      return true;
    }
    if (f.IsArray) {
      const n = count(value, f);
      if (n === 0) {
        o.text("[]");
        return true;
      }
      o.put(0x5b);
      for (let i = 0; i < n; i++) {
        if (i > 0) { o.put(0x2c); }
        o.line(depth + 1);
        if (!writeScalar(o, value, f, i, depth + 1)) {
          return false;
        }
      }
      o.line(depth);
      o.put(0x5d);
      return true;
    }
    return writeScalar(o, value, f, 0, depth);
  }

  // One instance, every field, in DECLARATION ORDER, defaults included — a
  // text is for people and tools, and a text that elides is a text a reader
  // has to know the schema to complete.
  function writeValue(o, value, info, depth) {
    let any = false;
    for (let i = 0; i < info.NumFields; i++) {
      const f = info.Fields[i];
      if (f.Guard.length !== 0 && !guardHolds(value, info, f.Guard)) { continue; }
      // an ABSENT optional writes no key: presence of the key IS the presence
      // (§16.2), so an absent field is an absent key and nothing else would
      // read back as absent
      if (f.Optional && !f.GetPresent(value)) { continue; }
      if (!any) { o.put(0x7b); } else { o.put(0x2c); }
      any = true;
      o.line(depth + 1);
      writeName(o, f.Json);
      o.text(": ");
      if (!writeField(o, value, f, depth + 1)) { return false; }
    }
    if (!any) {
      o.text("{}");
      return true;
    }
    o.line(depth);
    o.put(0x7d);
    return true;
  }

  // ---- reading ----

  // The reader's state: the cursor, the report and the malformed flag. The
  // TEXT rides beside it as its own Uint8Array parameter, so nothing is copied
  // and the bytes stay the caller's the whole way down.
  function space(text, input) {
    while (input.pos < text.length) {
      const c = text[input.pos];
      if (c === 0x20 || c === 0x09 || c === 0x0a || c === 0x0d) { input.pos++; continue; }
      // comments are not JSON, and a walk that guessed at one would be
      // reading a dialect nobody wrote down
      if (c === 0x2f) { input.bad = true; }
      return;
    }
  }

  function peek(text, input) {
    space(text, input);
    return input.pos < text.length ? text[input.pos] : 0;
  }

  // the shape of the value sitting at the cursor, without consuming it
  function valueShape(text, input) {
    switch (peek(text, input)) {
      case 0x7b: return "o";
      case 0x5b: return "a";
      case 0x22: return "s";
      case 0x74: case 0x66: return "b";
      case 0x6e: return "z";
      case 0: return ""; // no value at the cursor: a shape nothing matches
      default: return "n";
    }
  }

  function literal(text, input, word) {
    if (input.pos + word.length > text.length) { input.bad = true; return false; }
    for (let i = 0; i < word.length; i++) {
      if (text[input.pos + i] !== word.charCodeAt(i)) { input.bad = true; return false; }
    }
    input.pos += word.length;
    return true;
  }

  // one \uXXXX escape body; -1 when the four hex digits are not there
  function hex4(text, input) {
    if (input.pos + 4 > text.length) { return -1; }
    let value = 0;
    for (let i = 0; i < 4; i++) {
      const c = text[input.pos + i];
      let digit;
      if (c >= 0x30 && c <= 0x39) { digit = c - 0x30; }
      else if (c >= 0x61 && c <= 0x66) { digit = c - 0x61 + 10; }
      else if (c >= 0x41 && c <= 0x46) { digit = c - 0x41 + 10; }
      else { return -1; }
      value = (value << 4) | digit;
    }
    input.pos += 4;
    return value;
  }

  const CodeUnit = new Uint8Array(4);

  function encodeUtf8(code) {
    if (code < 0x80) { CodeUnit[0] = code; return 1; }
    if (code < 0x800) {
      CodeUnit[0] = 0xc0 | (code >> 6);
      CodeUnit[1] = 0x80 | (code & 0x3f);
      return 2;
    }
    if (code < 0x10000) {
      CodeUnit[0] = 0xe0 | (code >> 12);
      CodeUnit[1] = 0x80 | ((code >> 6) & 0x3f);
      CodeUnit[2] = 0x80 | (code & 0x3f);
      return 3;
    }
    CodeUnit[0] = 0xf0 | (code >> 18);
    CodeUnit[1] = 0x80 | ((code >> 12) & 0x3f);
    CodeUnit[2] = 0x80 | ((code >> 6) & 0x3f);
    CodeUnit[3] = 0x80 | (code & 0x3f);
    return 4;
  }

  // Scan one JSON string into the caller's buffer. Bytes are appended ONE CODE
  // POINT AT A TIME — an escape's encoding, or a UTF-8 sequence read whole —
  // so a string longer than the field is clamped AT A CODE POINT BOUNDARY and
  // never cut through a multi-byte character. Clamping is counted, never
  // fatal, exactly as it is on the wire (§4). A null destination scans past a
  // string without keeping it, and counts no clamp for what it dropped.
  // Returns the bytes placed, or -1 when the text is not a string.
  function scanString(text, input, destination, limit) {
    if (peek(text, input) !== 0x22) { input.bad = true; return -1; }
    input.pos++;
    let placed = 0;
    let clamped = false;
    for (;;) {
      if (input.pos >= text.length) { input.bad = true; return -1; }
      const c = text[input.pos];
      if (c === 0x22) { input.pos++; break; }
      let unitLength = 0;
      if (c === 0x5c) {
        input.pos++;
        if (input.pos >= text.length) { input.bad = true; return -1; }
        const escape = text[input.pos++];
        switch (escape) {
          case 0x22: CodeUnit[0] = 0x22; unitLength = 1; break;
          case 0x5c: CodeUnit[0] = 0x5c; unitLength = 1; break;
          case 0x2f: CodeUnit[0] = 0x2f; unitLength = 1; break;
          case 0x62: CodeUnit[0] = 0x08; unitLength = 1; break;
          case 0x66: CodeUnit[0] = 0x0c; unitLength = 1; break;
          case 0x6e: CodeUnit[0] = 0x0a; unitLength = 1; break;
          case 0x72: CodeUnit[0] = 0x0d; unitLength = 1; break;
          case 0x74: CodeUnit[0] = 0x09; unitLength = 1; break;
          case 0x75: {
            const high = hex4(text, input);
            if (high < 0) { input.bad = true; return -1; }
            let code = high;
            if (high >= 0xd800 && high <= 0xdbff && input.pos + 2 <= text.length &&
                text[input.pos] === 0x5c && text[input.pos + 1] === 0x75) {
              const mark = input.pos;
              input.pos += 2;
              const low = hex4(text, input);
              if (low >= 0xdc00 && low <= 0xdfff) {
                code = 0x10000 + ((high - 0xd800) << 10) + (low - 0xdc00);
              } else {
                input.pos = mark; // a lone lead surrogate rides as itself
              }
            }
            // a surrogate half that never found its partner has no UTF-8
            // encoding: encoding it anyway would manufacture CESU-8 — invalid
            // UTF-8 — out of input that was valid JSON, so it reads as the
            // replacement character
            if (code >= 0xd800 && code <= 0xdfff) { code = 0xfffd; }
            unitLength = encodeUtf8(code);
            break;
          }
          default: input.bad = true; return -1;
        }
      } else if (c < 0x20) {
        input.bad = true; // a raw control character is not a JSON string body
        return -1;
      } else {
        // a UTF-8 sequence read WHOLE, so the clamp below can only land
        // between code points. Only bytes that ACTUALLY look like
        // continuations are taken: the wire imposes no encoding (§3), so a
        // string may legitimately hold a stray lead byte, and one at the end
        // of a text must not swallow the closing quote.
        let want = 1;
        if ((c & 0xe0) === 0xc0) { want = 2; }
        else if ((c & 0xf0) === 0xe0) { want = 3; }
        else if ((c & 0xf8) === 0xf0) { want = 4; }
        CodeUnit[0] = c;
        input.pos++;
        unitLength = 1;
        while (unitLength < want && input.pos < text.length &&
               (text[input.pos] & 0xc0) === 0x80) {
          CodeUnit[unitLength++] = text[input.pos++];
        }
      }
      if (destination !== null) {
        if (placed + unitLength <= limit) {
          for (let i = 0; i < unitLength; i++) { destination[placed + i] = CodeUnit[i]; }
          placed += unitLength;
        } else {
          clamped = true;
        }
      }
    }
    if (clamped) { input.Report.Clamped++; }
    return placed;
  }

  // Scan one number, to JSON's OWN grammar (RFC 8259 §6) and not to a run of
  // number-ish characters:
  //
  //     number = [ "-" ] int [ frac ] [ exp ]
  //     int    = "0" / ( digit1-9 *digit )
  //     frac   = "." 1*digit
  //     exp    = ( "e" / "E" ) [ "-" / "+" ] 1*digit
  //
  // Scanning the production is what makes a typo in an authoring file a
  // DIAGNOSTIC rather than a value: "1-2" scans as 1 and leaves "-2" where
  // the object expects a comma, so the text is malformed — which is what
  // §16.2 already promises. A permissive scan would hand "1-2" to a digit
  // loop and report a clamp, and a config pipeline would never hear about it.
  // Leading "+", leading zeros, ".5" and "3." are not JSON either.
  const NumberIntegral = { value: true };

  function walkNumber(text, input) {
    space(text, input);
    NumberIntegral.value = true;
    if (input.pos < text.length && text[input.pos] === 0x2d) { input.pos++; }
    // int: a lone zero, or a non-zero digit and any digits after it
    if (input.pos >= text.length) { return false; }
    if (text[input.pos] === 0x30) {
      input.pos++;
    } else if (text[input.pos] >= 0x31 && text[input.pos] <= 0x39) {
      while (input.pos < text.length && text[input.pos] >= 0x30 && text[input.pos] <= 0x39) { input.pos++; }
    } else {
      return false;
    }
    // frac
    if (input.pos < text.length && text[input.pos] === 0x2e) {
      input.pos++;
      if (input.pos >= text.length || text[input.pos] < 0x30 || text[input.pos] > 0x39) { return false; }
      while (input.pos < text.length && text[input.pos] >= 0x30 && text[input.pos] <= 0x39) { input.pos++; }
      NumberIntegral.value = false;
    }
    // exp
    if (input.pos < text.length && (text[input.pos] === 0x65 || text[input.pos] === 0x45)) {
      input.pos++;
      if (input.pos < text.length && (text[input.pos] === 0x2d || text[input.pos] === 0x2b)) { input.pos++; }
      if (input.pos >= text.length || text[input.pos] < 0x30 || text[input.pos] > 0x39) { return false; }
      while (input.pos < text.length && text[input.pos] >= 0x30 && text[input.pos] <= 0x39) { input.pos++; }
      NumberIntegral.value = false;
    }
    return true;
  }

  // the same production, with the token kept for conversion; null when the
  // text at the cursor is not a number, or is longer than any field can hold
  function scanNumber(text, input) {
    space(text, input);
    const start = input.pos;
    if (!walkNumber(text, input)) { return null; }
    const n = input.pos - start;
    if (n <= 0 || n >= MaxNumber) { return null; }
    return AsciiDecoder.decode(text.subarray(start, input.pos));
  }

  // the token's exact integer, in the sixty-four-bit domain the reference
  // uses: the answer is the SIGNED long C++ and C# carry, so an unsigned value
  // past 2^63 - 1 rides here as a negative and the storage set below restores
  // it. Saturation is reported as a clamp, the wire's rule for a value outside
  // what the reader can hold (§4).
  const Saturated = { value: false };
  const U64Max = (1n << 64n) - 1n;
  const I64Max = (1n << 63n) - 1n;
  const I64Min = -(1n << 63n);

  function tokenInteger(token, isSigned) {
    let i = 0;
    let negative = false;
    if (i < token.length && (token[i] === "-" || token[i] === "+")) {
      negative = token[i] === "-";
      i++;
    }
    const digits = token.slice(i);
    const magnitude = digits.length === 0 ? 0n : BigInt(digits);
    Saturated.value = false;
    if (!isSigned) {
      // -0 IS zero, and clamping it would report an event that did not
      // happen; only a real negative magnitude is out of range here
      if (negative) { Saturated.value = magnitude !== 0n; return 0n; }
      if (magnitude > U64Max) { Saturated.value = true; return BigInt.asIntN(64, U64Max); }
      return BigInt.asIntN(64, magnitude);
    }
    if (negative) {
      if (magnitude > (1n << 63n)) { Saturated.value = true; return I64Min; }
      if (magnitude === (1n << 63n)) { return I64Min; }
      return -magnitude;
    }
    if (magnitude > I64Max) { Saturated.value = true; return I64Max; }
    return magnitude;
  }

  function skipContainer(text, input, close, depth) {
    if (depth > MaxDepth) { input.bad = true; return false; }
    input.pos++; // the opening bracket
    for (;;) {
      let c = peek(text, input);
      if (c === close) { input.pos++; return true; }
      if (c === 0) { input.bad = true; return false; }
      if (close === 0x7d) {
        if (scanString(text, input, null, 0) < 0) { return false; }
        if (peek(text, input) !== 0x3a) { input.bad = true; return false; }
        input.pos++;
      }
      if (!skipValue(text, input, depth + 1)) { return false; }
      c = peek(text, input);
      if (c === 0x2c) { input.pos++; continue; }   // a trailing comma is accepted
      if (c === close) { input.pos++; return true; }
      input.bad = true;
      return false;
    }
  }

  function skipValue(text, input, depth) {
    const c = peek(text, input);
    switch (c) {
      case 0x7b: return skipContainer(text, input, 0x7d, depth);
      case 0x5b: return skipContainer(text, input, 0x5d, depth);
      case 0x22: return scanString(text, input, null, 0) >= 0;
      case 0x74: return literal(text, input, "true");
      case 0x66: return literal(text, input, "false");
      case 0x6e: return literal(text, input, "null");
      case 0: input.bad = true; return false;
      default:
        // consumed, never converted: skipping needs no buffer, and this is the
        // one walk a hostile text drives to the depth cap. It is the SAME
        // production the value path scans, so an unknown key cannot smuggle
        // past a number a named key would refuse.
        if (!walkNumber(text, input)) { input.bad = true; return false; }
        return true;
    }
  }

  // compare a scanned UTF-8 key against a descriptor's string. Schema
  // identifiers are ASCII, so the common case is a byte walk; a json = "key"
  // that is not falls to the encoder. C++ compares two char* and needs neither
  // branch.
  const KeyEncoder = new TextEncoder();

  function same(bytes, length, name) {
    if (typeof name !== "string") { return false; }
    for (let i = 0; i < name.length; i++) {
      const c = name.charCodeAt(i);
      if (c >= 0x80) { return sameEncoded(bytes, length, name); }
      if (i >= length || bytes[i] !== c) { return false; }
    }
    return name.length === length;
  }

  function sameEncoded(bytes, length, name) {
    const encoded = KeyEncoder.encode(name);
    if (encoded.length !== length) { return false; }
    for (let i = 0; i < length; i++) {
      if (encoded[i] !== bytes[i]) { return false; }
    }
    return true;
  }

  // place one scalar at one storage slot
  function readScalar(text, input, owner, f, index, depth) {
    if (f.Arms !== null) {
      // a union is an object with ONE key, the arm's name; {} is None, and two
      // keys is a text this walk will not guess at
      const union = f.GetChild(owner, index);
      if (peek(text, input) !== 0x7b) { input.bad = true; return false; }
      input.pos++;
      f.Arms.SetTag(union, 0n);
      if (peek(text, input) === 0x7d) { input.pos++; return true; }
      const keyLength = scanString(text, input, ScanKey, MaxKey);
      if (keyLength < 0) { return false; }
      if (peek(text, input) !== 0x3a) { input.bad = true; return false; }
      input.pos++;
      let tag = 0;
      for (let t = 1; t <= f.EnumMax; t++) {
        if (same(ScanKey, keyLength, f.EnumName(t))) { tag = t; break; }
      }
      if (tag === 0) {
        input.Report.Unknown++;
        if (!skipValue(text, input, depth + 1)) { return false; }
      } else {
        const payload = f.Arms.Arms[tag].Payload(union);
        const arm = armTableOf(f.Arms.Arms[tag]);
        arm.Reset(payload);
        if (!readTable(text, input, payload, arm, depth + 1)) { return false; }
        f.Arms.SetTag(union, BigInt(tag));
      }
      let c = peek(text, input);
      if (c === 0x2c) { input.pos++; c = peek(text, input); }
      if (c === 0x7d) { input.pos++; return true; }
      input.bad = true; // a second key: a one-of with two arms is not a value
      return false;
    }
    if (f.Kind === 13) {
      const child = f.GetChild(owner, index);
      const info = tableOf(f);
      info.Reset(child);
      return readTable(text, input, child, info, depth + 1);
    }
    if (isEnum(f)) {
      const nameLength = scanString(text, input, ScanKey, MaxKey);
      if (nameLength < 0) { return false; }
      for (let v = 0; v <= f.EnumMax; v++) {
        if (same(ScanKey, nameLength, f.EnumName(v))) {
          f.SetRaw(owner, index, BigInt(v));
          return true;
        }
      }
      // a name this build cannot name reads as None and counts as unknown,
      // exactly as an unknown variant id does on the wire (§4)
      f.SetRaw(owner, index, 0n);
      input.Report.Unknown++;
      return true;
    }
    if (isFlags(f)) {
      if (peek(text, input) !== 0x5b) { input.bad = true; return false; }
      input.pos++;
      let bits = 0n;
      for (;;) {
        let c = peek(text, input);
        if (c === 0x5d) { input.pos++; break; }
        if (c === 0) { input.bad = true; return false; }
        if (c !== 0x22) {
          input.Report.KindMismatch++;
          if (!skipValue(text, input, depth + 1)) { return false; }
        } else {
          const nameLength = scanString(text, input, ScanKey, MaxKey);
          if (nameLength < 0) { return false; }
          let found = false;
          for (let bit = 0; bit <= f.EnumMax; bit++) {
            if (same(ScanKey, nameLength, f.EnumName(bit))) {
              bits |= 1n << BigInt(bit);
              found = true;
              break;
            }
          }
          if (!found) { input.Report.Unknown++; }
        }
        c = peek(text, input);
        if (c === 0x2c) { input.pos++; continue; }
        if (c === 0x5d) { input.pos++; break; }
        input.bad = true;
        return false;
      }
      f.SetRaw(owner, index, bits);
      return true;
    }
    if (f.Kind === 1) {
      const b = peek(text, input);
      if (b === 0x74) {
        if (!literal(text, input, "true")) { return false; }
        f.SetRaw(owner, index, 1n);
        return true;
      }
      if (!literal(text, input, "false")) { return false; }
      f.SetRaw(owner, index, 0n);
      return true;
    }
    const token = scanNumber(text, input);
    if (token === null) {
      input.bad = true;
      return false;
    }
    if (f.Kind === 10 || f.Kind === 11) {
      const single = f.Kind === 10;
      // a float32 field converts the decimal ONCE, exactly as strtof does; a
      // float64 field takes the runtime's own parser, which the language
      // already requires to be correctly rounded
      let value = single ? toFloat32(token) : Number(token);
      // A magnitude the field's format cannot hold is the WRONG SHAPE for the
      // kind, and it never reaches storage: 1e400 is not a float64 and 1e300
      // is not a float32. Storing the infinity the conversion produced would
      // leave an instance this walk called CLEAN that ToJsonMeasure then
      // refuses forever (a non-finite float has no JSON spelling), and §16.1's
      // one invariant is that a text which reads clean writes back.
      if (!finite(value)) {
        input.Report.KindMismatch++;
        return true;
      }
      if (f.HasRange) {
        if (value < f.RangeMin) { value = f.RangeMin; input.Report.Clamped++; }
        else if (value > f.RangeMax) { value = f.RangeMax; input.Report.Clamped++; }
      }
      if (single) {
        // the clamp above may have moved the value to a declared bound, which
        // is a float64 literal in the descriptor, so the narrowing stands
        const narrow = Math.fround(value);
        if (!finite(narrow)) {
          input.Report.KindMismatch++;
          return true;
        }
        f.SetRaw(owner, index, BigInt(TableFloatToBits(narrow)));
      } else {
        f.SetRaw(owner, index, TableDoubleToBits(value));
      }
      return true;
    }
    // JSON HAS ONE NUMBER TYPE. 2.0 IS the integer 2 and 1e3 IS 1000, and a
    // library that round-trips numbers through a double emits them that way —
    // this walker's own float writer emits 1e+21. So an integer field takes
    // any number whose VALUE is integral, however it was spelled; only a
    // genuinely fractional value is the wrong shape for it.
    const isSigned = f.Kind >= 2 && f.Kind <= 5;
    let saturated = false;
    let value = 0n;
    if (NumberIntegral.value) {
      value = tokenInteger(token, isSigned);
      saturated = Saturated.value;
    } else {
      const d = Number(token);
      if (!finite(d)) {
        input.Report.KindMismatch++;
        return true;
      }
      const truncated = Math.trunc(d);
      if (isSigned) {
        if (d >= 9223372036854775808.0) { value = I64Max; saturated = true; }
        else if (d < -9223372036854775808.0) { value = I64Min; saturated = true; }
        else if (d !== truncated) { input.Report.KindMismatch++; return true; }
        else { value = BigInt(truncated); }
      } else {
        if (d < 0.0) {
          // a negative for an unsigned field clamps to zero, as the exact
          // digit path already does
          if (d !== truncated) { input.Report.KindMismatch++; return true; }
          value = 0n;
          saturated = true;
        } else if (d >= 18446744073709551616.0) { value = BigInt.asIntN(64, U64Max); saturated = true; }
        else if (d !== truncated) { input.Report.KindMismatch++; return true; }
        else { value = BigInt.asIntN(64, BigInt(truncated)); }
      }
    }
    if (saturated) { input.Report.Clamped++; }
    if (f.HasRange) {
      if (Number(value) < f.RangeMin) { value = BigInt.asIntN(64, BigInt(Math.trunc(f.RangeMin))); input.Report.Clamped++; }
      else if (Number(value) > f.RangeMax) { value = BigInt.asIntN(64, BigInt(Math.trunc(f.RangeMax))); input.Report.Clamped++; }
    }
    // the field's own storage width is the last bound: a value past it clamps
    // rather than wrapping, which is what the wire does too
    if (f.ElemWidth < 8) {
      if (isSigned) {
        const high = (1n << BigInt(f.ElemWidth * 8 - 1)) - 1n;
        const low = -high - 1n;
        if (value > high) { value = high; input.Report.Clamped++; }
        else if (value < low) { value = low; input.Report.Clamped++; }
      } else {
        const high = (1n << BigInt(f.ElemWidth * 8)) - 1n;
        if (value < 0n) { value = 0n; input.Report.Clamped++; }
        else if (value > high) { value = high; input.Report.Clamped++; }
      }
    }
    // at eight bytes the storage IS the parser's width, and an unsigned value
    // past 2^63 - 1 rides here as a negative by design — the token parser
    // already turned a NEGATIVE token for an unsigned field into a clamped
    // zero, so there is nothing left to bound.
    f.SetRaw(owner, index, BigInt.asUintN(64, value));
    return true;
  }

  // put one array field's every slot back at its declared defaults. A table
  // element's defaults are its own (the reset hook); every other element
  // kind's storage default is zero, which is what the generated array
  // declares. There is no union arm here because an ARRAY OF UNIONS is
  // refused by name (docs/SPEC-TABLES.md §11).
  function resetSlots(value, f) {
    for (let i = 0; i < f.ArrayBound; i++) {
      if (f.Kind === 13) { tableOf(f).Reset(f.GetChild(value, i)); }
      else { f.SetRaw(value, i, 0n); }
    }
  }

  function readField(text, input, value, f, depth) {
    if (f.Kind === 12) {
      const storage = f.GetBuffer(value);
      const length = scanString(text, input, storage, f.ArrayBound);
      if (length < 0) { return false; }
      putCount(value, f, length);
      return true;
    }
    if (isBytes(f)) {
      // base64 decodes STRAIGHT INTO the field's storage, six bits at a time —
      // no window, no temporary, so a bytes(N) of any declared extent reads
      // the same way. A base64 body carries no escapes, so a backslash in one
      // is simply not an alphabet character.
      if (peek(text, input) !== 0x22) { input.bad = true; return false; }
      input.pos++;
      const storage = f.GetBuffer(value);
      storage.fill(0, 0, f.ArrayBound);
      putCount(value, f, 0);
      let placed = 0;
      let accumulator = 0;
      let held = 0;
      let clamped = false;
      let malformed = false;
      for (;;) {
        if (input.pos >= text.length) { input.bad = true; return false; }
        const c = text[input.pos++];
        if (c === 0x22) { break; }
        if (c === 0x3d || malformed) { continue; }
        const at = Base64Alphabet.indexOf(String.fromCharCode(c));
        if (at < 0) { malformed = true; continue; }
        accumulator = ((accumulator << 6) | at) >>> 0;
        held += 6;
        if (held >= 8) {
          held -= 8;
          if (placed < f.ArrayBound) {
            storage[placed++] = (accumulator >>> held) & 0xff;
          } else {
            clamped = true;
          }
        }
      }
      if (malformed) {
        // a body that is not base64 is the wrong shape for the kind: the field
        // keeps its default and the event is counted
        input.Report.KindMismatch++;
        return true;
      }
      if (clamped) { input.Report.Clamped++; }
      putCount(value, f, placed);
      return true;
    }
    if (isKeyed(f)) {
      if (peek(text, input) !== 0x7b) { input.bad = true; return false; }
      input.pos++;
      // every slot back to its declared defaults first, so a key the text
      // omits keeps them and a repeated field key cannot leave an earlier
      // occurrence's slots standing
      resetSlots(value, f);
      const want = elementShape(f);
      // A KEYED OBJECT'S KEYS ARE KEYS: a variant named twice is a duplicate
      // key like any other, last-wins and counted (§16.2).
      seenClear(SeenKeyed, depth);
      for (;;) {
        let c = peek(text, input);
        if (c === 0x7d) { input.pos++; break; }
        if (c === 0) { input.bad = true; return false; }
        const keyLength = scanString(text, input, ScanKey, MaxKey);
        if (keyLength < 0) { return false; }
        if (peek(text, input) !== 0x3a) { input.bad = true; return false; }
        input.pos++;
        let slot = -1;
        for (let v = 0; v < f.ArrayBound; v++) {
          // nothing is stored for None, so "None" finds no slot and is an
          // unknown key like any other name this reader cannot place
          if (!keyedSlotValid(f, v)) { continue; }
          if (same(ScanKey, keyLength, f.KeyName(keyedSlotKey(v)))) { slot = v; break; }
        }
        if (slot >= 0 && seenMark(SeenKeyed, depth, slot)) { input.Report.Duplicate++; }
        if (slot < 0) {
          input.Report.Unknown++;
          if (!skipValue(text, input, depth + 1)) { return false; }
        } else if (valueShape(text, input) !== want) {
          input.Report.KindMismatch++;
          if (!skipValue(text, input, depth + 1)) { return false; }
        } else if (!readScalar(text, input, value, f, slot, depth + 1)) {
          return false;
        }
        c = peek(text, input);
        if (c === 0x2c) { input.pos++; continue; } // a trailing comma is accepted
        if (c === 0x7d) { input.pos++; break; }
        input.bad = true;
        return false;
      }
      return true;
    }
    if (f.IsArray) {
      if (peek(text, input) !== 0x5b) { input.bad = true; return false; }
      input.pos++;
      // LAST WINS has to be true of a repeated ARRAY key too, and it is
      // wire-visible: a fixed array writes every slot, so a second, shorter
      // occurrence overlaying a prefix would leave the first occurrence's tail
      // standing. The field goes back to its declared defaults before this
      // occurrence's elements are placed — the re-establishment a nested table
      // and a union arm already get.
      resetSlots(value, f);
      putCount(value, f, 0);
      let placed = 0;
      const want = elementShape(f);
      for (;;) {
        let c = peek(text, input);
        if (c === 0x5d) { input.pos++; break; }
        if (c === 0) { input.bad = true; return false; }
        if (placed >= f.ArrayBound) {
          // more elements than the reader's bound: the bounded prefix is kept
          // and the excess counts, the wire's rule (§4)
          input.Report.Clamped++;
          if (!skipValue(text, input, depth + 1)) { return false; }
        } else if (valueShape(text, input) !== want) {
          input.Report.KindMismatch++;
          if (!skipValue(text, input, depth + 1)) { return false; }
          placed++;
        } else {
          if (!readScalar(text, input, value, f, placed, depth + 1)) { return false; }
          placed++;
        }
        c = peek(text, input);
        if (c === 0x2c) { input.pos++; continue; }
        if (c === 0x5d) { input.pos++; break; }
        input.bad = true;
        return false;
      }
      // a fixed array's tail keeps the defaults the prefill left there,
      // exactly as a short wire count does
      putCount(value, f, placed);
      return true;
    }
    return readScalar(text, input, value, f, 0, depth);
  }

  // ONE table object: keys are field keys, unknown ones are skipped and
  // counted, a repeated key is last-wins and counted. The instance is already
  // at its declared defaults when this is entered, so a key the text never
  // mentions keeps the default an absent field takes on the wire (§4).
  function readTable(text, input, value, info, depth) {
    if (depth > MaxDepth) { input.bad = true; return false; }
    if (peek(text, input) !== 0x7b) { input.bad = true; return false; }
    input.pos++;
    seenClear(SeenTable, depth);
    for (;;) {
      let c = peek(text, input);
      if (c === 0x7d) { input.pos++; return true; }
      if (c === 0) { input.bad = true; return false; }
      const keyLength = scanString(text, input, ScanKey, MaxKey);
      if (keyLength < 0) { return false; }
      if (peek(text, input) !== 0x3a) { input.bad = true; return false; }
      input.pos++;
      let index = -1;
      for (let i = 0; i < info.NumFields; i++) {
        if (same(ScanKey, keyLength, info.Fields[i].Json)) { index = i; break; }
      }
      if (keyLength > 0 && ScanKey[0] === 0x26) {
        // THE AMPERSAND PREFIX IS RESERVED TO THE FORM (docs/SPEC-TABLES.md
        // 16.7): never a field this build lacks, always a construct it
        // cannot honor. MALFORMED and refused, never counted as unknown.
        input.Report.Malformed = true;
        input.bad = true;
        return false;
      }
      if (index < 0) {
        input.Report.Unknown++;
        if (!skipValue(text, input, depth + 1)) { return false; }
      } else {
        const f = info.Fields[index];
        if (seenMark(SeenTable, depth, index)) { input.Report.Duplicate++; }
        // PRESENCE OF THE KEY IS THE PRESENCE (§16.2): reaching this line is
        // the key being present, so an optional is set present whatever its
        // value — with one exception the page names: a JSON null, which reads
        // as ABSENT rather than as a value.
        const got = valueShape(text, input);
        if (f.Optional && got === "z") {
          if (!literal(text, input, "null")) { return false; }
          // absent, and back at its defaults: a repeated key whose last
          // occurrence is null must not leave an earlier value standing
          if (f.TableRef !== null) { tableOf(f).Reset(f.GetChild(value, 0)); }
          else { f.SetRaw(value, 0, 0n); }
          f.SetPresent(value, false);
        } else {
          if (got !== shape(f)) {
            // the wrong JSON type for the kind: skipped, never coerced
            input.Report.KindMismatch++;
            if (!skipValue(text, input, depth + 1)) { return false; }
          } else if (!readField(text, input, value, f, depth)) {
            return false;
          }
          if (f.Optional) {
            f.SetPresent(value, true);
          }
        }
      }
      c = peek(text, input);
      if (c === 0x2c) { input.pos++; continue; } // a trailing comma is accepted
      if (c === 0x7d) { input.pos++; return true; }
      input.bad = true;
      return false;
    }
  }

  // ---- the two entry points the per-table wrappers name ----

  function read(value, info, text, report) {
    // THE TEXT IS A STRING OR THE BYTES OF ONE. A string is the language's
    // currency for text and is encoded here, once, at the boundary; a
    // Uint8Array is a text read straight off a file and is walked as it is.
    // Anything else is the CALLER's error, not malformed data: a number, an
    // object or a missing argument is a type the text form does not take.
    if (typeof text === "string") {
      text = Utf8Encoder.encode(text);
    } else if (!(text instanceof Uint8Array)) {
      throw new TypeError("FromJson takes the text as a string or a Uint8Array, not " +
        (text === null ? "null" : typeof text));
    }
    // a caller with no report is off the read path this section prices: it
    // gets one rather than a branch on every counter
    const input = {
      pos: 0,
      Report: report !== null && report !== undefined ? report : new TableReport(),
      bad: false,
    };
    info.Reset(value);
    // C++ refuses a null pointer and a negative length here before it walks. A
    // Uint8Array is neither: an empty one is a text with no object in it,
    // which the walk below already calls malformed, so there is nothing to
    // check that the walk does not answer.
    const ok = readTable(text, input, value, info, 0);
    if (ok) {
      // the canonical text ends with ONE newline and a text without one is the
      // same text: whitespace after the object is skipped either way, and
      // anything else is trailing rubbish rather than one text
      space(text, input);
      if (input.pos !== text.length) { input.bad = true; }
    }
    if (input.bad || !ok) {
      input.Report.Malformed = true;
      return false;
    }
    return true;
  }

  // TWO PASSES OVER ONE CODE PATH: the walk measures first, then writes into
  // a buffer of exactly that size, so measure and write agree byte for byte —
  // the wire's invariant (§9) carried across — and the answer is a string,
  // decoded once from bytes the walk itself spelled as UTF-8. A value the text
  // form cannot spell — a non-finite float, an enum value or union tag no
  // variant names, a count past its bound — is the CALLER's error and throws.
  function write(value, info) {
    const measure = new Out(null, true);
    if (!writeValue(measure, value, info, 0)) {
      throw new RangeError(info.Name + "ToJson: the value holds something the text form cannot spell — " +
        "a non-finite float, an enum value or union tag no variant names, or a count past its bound");
    }
    // THE CANONICAL TEXT ENDS WITH EXACTLY ONE NEWLINE (§16.1). Every writer
    // emits it — this one, the C++ walk and schema unpack — and every reader
    // accepts a text with or without.
    measure.put(0x0a);
    const bytes = new Uint8Array(measure.Offset);
    const o = new Out(bytes, false);
    writeValue(o, value, info, 0);
    o.put(0x0a);
    return Utf8Decoder.decode(bytes);
  }

  return Object.freeze({ read, write, MaxDepth, MaxKey, MaxNumber });
})();
// ---- json walk: end ----

`
