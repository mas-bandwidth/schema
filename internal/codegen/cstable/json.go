// The TEXT form in C#: JSON in and out of one table, driven by the reflection
// descriptors (docs/SPEC-TABLES.md §16).
//
// ONE generic walk, emitted ONCE PER UNIT — into the same file the rest of the
// unit's shared table runtime goes, <Package>Table.cs — because a unit's C#
// files compile together into one assembly and a second copy would be a
// duplicate definition rather than C++'s harmless re-inclusion behind a guard.
// One home per unit makes the gate available: the walker's source is the SAME
// BYTES in every unit's generated output (`make tables-cs-json-walk`).
//
// THE C++ BACKEND IS THE REFERENCE and this file mirrors it: the same
// classifier, the same shapes, the same clamps, the same report events, the
// same acceptance of a trailing comma and the same refusal of a comment. Where
// C# forces a different spelling the reason is stated at the site, and there
// are exactly four:
//
//   - Storage is reached through the descriptor's ACCESSORS rather than an
//     offset and a width: a C# field has no address (see cstable.go's
//     TableFieldInfo). C++'s `storage` pointer becomes the triple
//     (owner, field, index) throughout.
//   - The number grammar's conversions go through the INVARIANT culture, so
//     the walker never consults a locale. C++ crosses the runtime's decimal
//     point in both directions; C# has a culture that does not have one.
//   - A JSON string is written from TWO sources — a descriptor's key, which is
//     a C# string, and a string(N)'s storage, which is bytes — where C++ has
//     char* for both. The escaping rule is one rule, spelled at each source.
//   - The walk lives inside a nested static class, `Schema.TableJson`, so its
//     members claim nothing at unit scope (docs/SPEC-TABLES.md §11).
package cstable

import "github.com/mas-bandwidth/schema/v2/ir"

// emitJsonSurface emits one closure member's text-form surface: three thin
// wrappers over the generic walk, each naming a descriptor and nothing else.
// FIXED-SIZE members only, which in this backend is every member a
// <Base>Table.cs exists for — a pointered unit gets no such file at all
// (docs/SPEC-TABLES.md §11), so the variable class's refusal is already made and
// this emission has no second one to make.
func (g *tableGen) emitJsonSurface(st *ir.Struct) {
	g.pf("// %s in and out of a JSON text — one instance, one text, the generic\n", st.Name)
	g.pf("// walk over this type's descriptors (docs/SPEC-TABLES.md §16).\n")
	g.pf("public static bool %sFromJson(%s value, ReadOnlySpan<byte> text, TableReport report)\n{\n", st.Name, st.Name)
	g.pf("    return TableJson.Read(value, %sTableType(), text, report);\n}\n\n", st.Name)
	g.pf("public static long %sToJsonMeasure(%s value)\n{\n", st.Name, st.Name)
	g.pf("    return TableJson.Write(value, %sTableType(), Span<byte>.Empty, true);\n}\n\n", st.Name)
	g.pf("public static long %sToJson(%s value, Span<byte> buffer)\n{\n", st.Name, st.Name)
	g.pf("    return TableJson.Write(value, %sTableType(), buffer, false);\n}\n\n", st.Name)
}

// tableJsonWalkSource is the walker. It reads and writes ONLY the columns every
// unit's descriptors carry, so its text never varies with the unit — which is
// what the generic-walk gate asserts.
const tableJsonWalkSource = `// ---- json walk: begin ----
//
// The TEXT form (docs/SPEC-TABLES.md §16): one table, one text, one walk over the
// reflection descriptors (§8). Reading fills ONE caller-owned instance and
// allocates nothing beyond it; writing targets a caller span with the wire's
// measure/write symmetry. Everything AROUND this — which file goes with which
// instance, what key an instance is filed under, how instances link into a
// root table's collections — is a packer's opinion and stays with the tool
// that holds it.
//
// The dialect: trailing commas are accepted on read (the authoring files this
// exists for carry them) and never written; comments are not JSON and are
// refused; unknown keys are skipped and counted; a duplicate key is last-wins
// and counted; a key present with the wrong JSON type is skipped and counted,
// never coerced. The canonical text ends with exactly ONE newline, which the
// writer emits and the reader accepts with or without.
//
// Everything below is a member of this nested class, so the walk claims not one
// name at unit scope (docs/SPEC-TABLES.md §11).
public static class TableJson
{
    public const int MaxDepth = 128;

    // A key longer than this cannot name a field, so it is skipped as unknown.
    public const int MaxKey = 256;

    // The longest numeric token the walk will convert. Anything longer is a
    // value no field can hold and counts as a kind mismatch.
    public const int MaxNumber = 512;

    const string Base64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

    // A vocabulary entry the descriptor could not spell. The generated name
    // functions answer "???" for a value outside the declared set, and that is
    // not a name — writing it would put a spelling in the text that the reader
    // then counts as unknown, turning a refusal into a silent loss.
    static bool Named(string name)
    {
        return name != null && !string.Equals(name, "???", StringComparison.Ordinal);
    }

    // finite: not a NaN, not an infinity. C++ spells the NaN test v == v; C#
    // refuses a comparison of a variable with itself (CS1718, an error here),
    // so the runtime's own predicate says it.
    static bool Finite(double v)
    {
        return !double.IsNaN(v) && v <= 1.7976931348623157e308 && v >= -1.7976931348623157e308;
    }

    // a counted field's companion: a string's length, a bytes' length, a
    // counted array's count. Bounded by the declared extent on the way out, so
    // a storage invariant a caller broke cannot walk off the end of the array.
    static int Count(object value, TableFieldInfo f)
    {
        if (!f.Counted) { return f.ArrayBound; }
        int count = f.GetCount(value);
        if (count < 0) { count = 0; }
        if (count > f.ArrayBound) { count = f.ArrayBound; }
        return count;
    }

    static void PutCount(object value, TableFieldInfo f, int count)
    {
        if (f.Counted) { f.SetCount(value, count); }
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
    static bool IsBytes(TableFieldInfo f)
    {
        return f.IsArray && f.Kind == 6 && string.Equals(f.TypeName, "bytes", StringComparison.Ordinal);
    }

    // An ENUM-KEYED array (docs/SPEC-TABLES.md §2.4): its JSON form is an OBJECT
    // keyed by variant name, not a positional array, because that is what the
    // storage is — one slot per variant, addressed by the variant.
    static bool IsKeyed(TableFieldInfo f)
    {
        return f.KeyName != null;
    }

    // THE KEY A STORAGE SLOT HOLDS (§2.4, §8): the storage shifts left, so slot
    // i holds the key i + 1 and nothing is stored for None. This is the ONE
    // place the walker spells the shift.
    static ulong KeyedSlotKey(int slot)
    {
        return (ulong)(slot + 1);
    }

    // A slot whose key names a variant of the keying enum. Every slot in
    // [0, ArrayBound) does, unless the enum carries max-headroom variants
    // outside a table closure, where a reserved value names nothing and its key
    // id is 0 — the reserved id no declared name can fold to (§5).
    static bool KeyedSlotValid(TableFieldInfo f, int slot)
    {
        return f.KeyId(KeyedSlotKey(slot)) != 0;
    }

    static bool IsFlags(TableFieldInfo f)
    {
        return f.EnumName != null && f.VariantId == null;
    }

    static bool IsEnum(TableFieldInfo f)
    {
        return f.VariantId != null && f.Arms == null;
    }

    static char Shape(TableFieldInfo f)
    {
        if (f.Kind == 12) { return 's'; }          // string
        if (IsBytes(f)) { return 's'; }            // bytes: base64
        if (IsKeyed(f)) { return 'o'; }            // an object keyed by variant NAME
        if (f.IsArray) { return 'a'; }
        if (f.Arms != null) { return 'o'; }        // union: an object with ONE key
        if (f.Kind == 13) { return 'o'; }          // nested table or type
        if (IsEnum(f)) { return 's'; }
        if (IsFlags(f)) { return 'a'; }
        if (f.Kind == 1) { return 'b'; }
        return 'n';
    }

    // the ELEMENT shape of an array field — the same classifier one level down
    static char ElementShape(TableFieldInfo f)
    {
        if (f.Kind == 13) { return 'o'; }
        if (IsEnum(f)) { return 's'; }
        if (IsFlags(f)) { return 'a'; }
        if (f.Kind == 1) { return 'b'; }
        return 'n';
    }

    // A guarded group rides only when its guard reads true — the wire's own
    // elision (§4), carried into the text so a text and a wire written from one
    // instance say the same thing. The guard is spelled as its branch condition
    // over bool fields of the SAME type ("at_rest", "!at_rest",
    // "active && has_target"), so evaluating it is a walk of the same
    // descriptor. Nothing is inferred in the other direction: reading places
    // every key it can name, and the guard is a plain bool key (§16.2).
    static bool GuardHolds(object value, TableTypeInfo info, string guard)
    {
        int p = 0;
        for (;;)
        {
            while (p < guard.Length && (guard[p] == ' ' || guard[p] == '&')) { p++; }
            if (p >= guard.Length) { return true; }
            bool want = true;
            if (guard[p] == '!') { want = false; p++; }
            int start = p;
            while (p < guard.Length && guard[p] != ' ' && guard[p] != '&') { p++; }
            int length = p - start;
            bool held = false;
            for (int i = 0; i < info.NumFields; i++)
            {
                TableFieldInfo f = info.Fields[i];
                if (f.Name.Length == length && string.CompareOrdinal(f.Name, 0, guard, start, length) == 0)
                {
                    held = f.GetRaw(value, 0) != 0;
                    break;
                }
            }
            if (held != want) { return false; }
        }
    }

    // ---- writing ----

    // The writer sink MEASURES when Measuring is set and WRITES when it is not,
    // over one code path — so measure and write agree byte for byte, the wire's
    // invariant (§9) carried across.
    public ref struct Out
    {
        public Span<byte> Buffer;
        public bool Measuring;
        public long Offset;
        public bool Overflow;

        public void Raw(ReadOnlySpan<byte> data)
        {
            if (!Measuring)
            {
                if (Offset + data.Length > Buffer.Length) { Overflow = true; return; }
                data.CopyTo(Buffer.Slice((int)Offset, data.Length));
            }
            Offset += data.Length;
        }

        public void Put(byte c)
        {
            if (!Measuring)
            {
                if (Offset + 1 > Buffer.Length) { Overflow = true; return; }
                Buffer[(int)Offset] = c;
            }
            Offset += 1;
        }

        // an ASCII literal of the walk's own — "true", ": ", "[]" — never a
        // value, so widening each char is the whole encoding
        public void Text(string s)
        {
            for (int i = 0; i < s.Length; i++) { Put((byte)s[i]); }
        }

        public void Line(int depth)
        {
            Put((byte)'\n');
            for (int i = 0; i < depth; i++) { Put((byte)' '); Put((byte)' '); }
        }
    }

    static void WriteBase64(ref Out o, ReadOnlySpan<byte> data)
    {
        o.Put((byte)'"');
        int i = 0;
        for (; i + 3 <= data.Length; i += 3)
        {
            uint triple = ((uint)data[i] << 16) | ((uint)data[i + 1] << 8) | (uint)data[i + 2];
            o.Put((byte)Base64Alphabet[(int)((triple >> 18) & 0x3f)]);
            o.Put((byte)Base64Alphabet[(int)((triple >> 12) & 0x3f)]);
            o.Put((byte)Base64Alphabet[(int)((triple >> 6) & 0x3f)]);
            o.Put((byte)Base64Alphabet[(int)(triple & 0x3f)]);
        }
        if (i < data.Length)
        {
            int left = data.Length - i;
            uint triple = (uint)data[i] << 16;
            if (left == 2) { triple |= (uint)data[i + 1] << 8; }
            o.Put((byte)Base64Alphabet[(int)((triple >> 18) & 0x3f)]);
            o.Put((byte)Base64Alphabet[(int)((triple >> 12) & 0x3f)]);
            o.Put(left == 2 ? (byte)Base64Alphabet[(int)((triple >> 6) & 0x3f)] : (byte)'=');
            o.Put((byte)'=');
        }
        o.Put((byte)'"');
    }

    // One UTF-8 sequence at s[at], or -1 when the bytes there are not one.
    // Rejects the lot: a stray continuation, an overlong form, a surrogate half,
    // and anything past U+10FFFF.
    static int Utf8(ReadOnlySpan<byte> s, int at, out int width)
    {
        width = 0;
        byte lead = s[at];
        int want;
        int code;
        if (lead < 0x80) { width = 1; return lead; }
        else if (lead >= 0xc2 && lead <= 0xdf) { want = 2; code = lead & 0x1f; }
        else if (lead >= 0xe0 && lead <= 0xef) { want = 3; code = lead & 0x0f; }
        else if (lead >= 0xf0 && lead <= 0xf4) { want = 4; code = lead & 0x07; }
        else { return -1; }
        if (s.Length - at < want) { return -1; }
        for (int i = 1; i < want; i++)
        {
            byte next = s[at + i];
            if ((next & 0xc0) != 0x80) { return -1; }
            code = (code << 6) | (next & 0x3f);
        }
        if (want == 3 && code < 0x800) { return -1; }          // overlong
        if (want == 4 && code < 0x10000) { return -1; }        // overlong
        if (code >= 0xd800 && code <= 0xdfff) { return -1; }   // a surrogate half
        if (code > 0x10ffff) { return -1; }
        width = want;
        return code;
    }

    // The escape rule, spelled once and reached from both string sources: a
    // code point the JSON grammar names, a control character as \u00XX, and
    // anything else as itself. Returns false when the caller must emit the
    // code point's own bytes instead.
    static bool WriteEscape(ref Out o, int code)
    {
        switch (code)
        {
            case '"': o.Text("\\\""); return true;
            case '\\': o.Text("\\\\"); return true;
            case '\b': o.Text("\\b"); return true;
            case '\f': o.Text("\\f"); return true;
            case '\n': o.Text("\\n"); return true;
            case '\r': o.Text("\\r"); return true;
            case '\t': o.Text("\\t"); return true;
        }
        if (code < 0x20)
        {
            const string hex = "0123456789abcdef";
            o.Text("\\u00");
            o.Put((byte)hex[code >> 4]);
            o.Put((byte)hex[code & 0xf]);
            return true;
        }
        return false;
    }

    static void WriteUtf8(ref Out o, int code)
    {
        if (code < 0x80) { o.Put((byte)code); return; }
        if (code < 0x800)
        {
            o.Put((byte)(0xc0 | (code >> 6)));
            o.Put((byte)(0x80 | (code & 0x3f)));
            return;
        }
        if (code < 0x10000)
        {
            o.Put((byte)(0xe0 | (code >> 12)));
            o.Put((byte)(0x80 | ((code >> 6) & 0x3f)));
            o.Put((byte)(0x80 | (code & 0x3f)));
            return;
        }
        o.Put((byte)(0xf0 | (code >> 18)));
        o.Put((byte)(0x80 | ((code >> 12) & 0x3f)));
        o.Put((byte)(0x80 | ((code >> 6) & 0x3f)));
        o.Put((byte)(0x80 | (code & 0x3f)));
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
    static void WriteString(ref Out o, ReadOnlySpan<byte> s)
    {
        o.Put((byte)'"');
        for (int i = 0; i < s.Length; i++)
        {
            byte c = s[i];
            if (WriteEscape(ref o, c)) { continue; }
            if (c < 0x80) { o.Put(c); continue; }
            int width;
            if (Utf8(s, i, out width) < 0)
            {
                WriteUtf8(ref o, 0xfffd); // one per bad byte
            }
            else
            {
                o.Raw(s.Slice(i, width));
                i += width - 1;
            }
        }
        o.Put((byte)'"');
    }

    // The same rule at the OTHER source: a descriptor's key or vocabulary name,
    // which C# holds as a string. C++ has one writer because both of its
    // sources are char*; here the chars are UTF-16, so a surrogate pair is
    // recombined and a lone half — which a schema identifier cannot produce and
    // a json = "key" attribute could not carry either — reads as U+FFFD, the
    // same answer the byte path gives an ill-formed sequence.
    static void WriteName(ref Out o, string s)
    {
        o.Put((byte)'"');
        for (int i = 0; i < s.Length; i++)
        {
            int code = s[i];
            if (code >= 0xd800 && code <= 0xdbff && i + 1 < s.Length &&
                s[i + 1] >= 0xdc00 && s[i + 1] <= 0xdfff)
            {
                code = 0x10000 + ((code - 0xd800) << 10) + (s[i + 1] - 0xdc00);
                i++;
            }
            else if (code >= 0xd800 && code <= 0xdfff)
            {
                code = 0xfffd;
            }
            if (WriteEscape(ref o, code)) { continue; }
            WriteUtf8(ref o, code);
        }
        o.Put((byte)'"');
    }

    static void WriteUnsigned(ref Out o, ulong value)
    {
        Span<byte> digits = stackalloc byte[24];
        int n = 0;
        do
        {
            digits[n++] = (byte)('0' + (int)(value % 10));
            value /= 10;
        } while (value != 0);
        for (int i = n - 1; i >= 0; i--) { o.Put(digits[i]); }
    }

    static void WriteSigned(ref Out o, long value)
    {
        if (value < 0)
        {
            o.Put((byte)'-');
            WriteUnsigned(ref o, 0ul - (ulong)value);
            return;
        }
        WriteUnsigned(ref o, (ulong)value);
    }

    // C's "%.*g" for a finite double, spelled out into chars: the exponent form
    // when the decimal exponent is below -4 or at least the precision, the
    // plain form otherwise, trailing zeros and a trailing point removed, and an
    // exponent of at least two digits. The C++ walker reaches this through
    // snprintf; a port spells it out, because the two must agree byte for byte.
    // Every conversion here is INVARIANT-CULTURE, so nothing in the walk
    // consults a locale — the one corner where C++ has to.
    static int FormatG(double value, int prec, Span<char> text)
    {
        if (prec < 1) { prec = 1; }
        Span<char> pattern = stackalloc char[MaxNumber];
        Span<char> sci = stackalloc char[MaxNumber];
        int patternLength = Pattern(pattern, prec - 1, true);
        int sciLength;
        if (!value.TryFormat(sci, out sciLength, pattern.Slice(0, patternLength), System.Globalization.CultureInfo.InvariantCulture))
        {
            return -1;
        }
        int at = sci.Slice(0, sciLength).IndexOf('e');
        if (at < 0) { return -1; }
        int exp = 0;
        bool negativeExp = sci[at + 1] == '-';
        for (int i = at + 2; i < sciLength; i++) { exp = exp * 10 + (sci[i] - '0'); }
        if (negativeExp) { exp = -exp; }
        if (exp < -4 || exp >= prec)
        {
            int mantissa = TrimZeros(sci.Slice(0, at));
            if (mantissa > text.Length) { return -1; }
            sci.Slice(0, mantissa).CopyTo(text);
            int n = mantissa;
            if (n + 4 > text.Length) { return -1; }
            text[n++] = 'e';
            text[n++] = negativeExp ? '-' : '+';
            int magnitude = exp < 0 ? -exp : exp;
            if (magnitude < 10)
            {
                text[n++] = '0';
                text[n++] = (char)('0' + magnitude);
            }
            else
            {
                int digits = 0;
                Span<char> scratch = stackalloc char[8];
                while (magnitude != 0) { scratch[digits++] = (char)('0' + magnitude % 10); magnitude /= 10; }
                if (n + digits > text.Length) { return -1; }
                for (int i = digits - 1; i >= 0; i--) { text[n++] = scratch[i]; }
            }
            return n;
        }
        patternLength = Pattern(pattern, prec - 1 - exp, false);
        int plainLength;
        if (!value.TryFormat(text, out plainLength, pattern.Slice(0, patternLength), System.Globalization.CultureInfo.InvariantCulture))
        {
            return -1;
        }
        return TrimZeros(text.Slice(0, plainLength));
    }

    // the custom pattern "%.*g" needs: "0." and decimals zeros, with "e+00"
    // appended for the exponent form — which is what fixes the exponent at two
    // digits minimum and always signed, the one shape C's %g writes
    static int Pattern(Span<char> pattern, int decimals, bool exponent)
    {
        if (decimals < 0) { decimals = 0; }
        int n = 0;
        pattern[n++] = '0';
        if (decimals > 0)
        {
            if (n + 1 + decimals + 4 > pattern.Length) { decimals = pattern.Length - n - 5; }
            pattern[n++] = '.';
            for (int i = 0; i < decimals; i++) { pattern[n++] = '0'; }
        }
        if (exponent)
        {
            pattern[n++] = 'e';
            pattern[n++] = '+';
            pattern[n++] = '0';
            pattern[n++] = '0';
        }
        return n;
    }

    static int TrimZeros(Span<char> s)
    {
        int n = s.Length;
        bool point = false;
        for (int i = 0; i < n; i++) { if (s[i] == '.') { point = true; break; } }
        if (!point) { return n; }
        while (n > 0 && s[n - 1] == '0') { n--; }
        if (n > 0 && s[n - 1] == '.') { n--; }
        return n;
    }

    // A float writes at the SHORTEST precision that reads back as the same
    // value at the field's own width, so a round trip is exact and a text stays
    // readable. Non-finite values have no JSON spelling at all, and the writer
    // REFUSES rather than losing one silently — the same rule measure and save
    // already apply to an enum value no variant names (§5).
    static bool WriteFloat(ref Out o, double value, bool single)
    {
        if (!Finite(value)) { return false; }
        Span<char> text = stackalloc char[MaxNumber];
        int low = single ? 6 : 15;
        int high = single ? 9 : 17;
        int length = 0;
        for (int digits = low; ; digits++)
        {
            length = FormatG(value, digits, text);
            if (length <= 0) { return false; }
            if (digits >= high) { break; }
            double back;
            if (!double.TryParse(text.Slice(0, length), System.Globalization.NumberStyles.Float,
                    System.Globalization.CultureInfo.InvariantCulture, out back))
            {
                continue;
            }
            if (single)
            {
                if ((double)(float)back == value) { break; }
            }
            else if (back == value) { break; }
        }
        for (int i = 0; i < length; i++) { o.Put((byte)text[i]); }
        return true;
    }

    // one scalar, at one storage slot: a nested object, a union, a vocabulary,
    // or a number. C++ takes the storage's ADDRESS here; the C# triple
    // (owner, field, index) is the same thing said in this language.
    static bool WriteScalar(ref Out o, object owner, TableFieldInfo f, int index, int depth)
    {
        if (f.Arms != null)
        {
            // a union is an object with ONE key, the arm's name; None is {}
            object union = f.GetChild(owner, index);
            ulong tag = f.Arms.GetTag(union);
            if (tag == 0)
            {
                o.Text("{}");
                return true;
            }
            if ((long)tag > f.EnumMax)
            {
                return false; // a tag no arm names, exactly as measure refuses it
            }
            string arm = f.EnumName(tag);
            // and refuse on the NAME, not merely on the bound: §16.2 says a
            // value no variant NAMES is refused, so the check is the name.
            // Writing whatever came back would emit "???", a spelling the
            // reader counts as unknown — a silent round-trip loss in place of a
            // refusal.
            if (!Named(arm)) { return false; }
            o.Put((byte)'{');
            o.Line(depth + 1);
            WriteName(ref o, arm);
            o.Text(": ");
            if (!WriteValue(ref o, f.Arms.Arms[(int)tag].Payload(union), f.Arms.Arms[(int)tag].Table, depth + 1))
            {
                return false;
            }
            o.Line(depth);
            o.Put((byte)'}');
            return true;
        }
        if (f.Kind == 13)
        {
            return WriteValue(ref o, f.GetChild(owner, index), f.Table, depth);
        }
        if (IsEnum(f))
        {
            ulong value = f.GetRaw(owner, index);
            // a value no variant names has no text spelling, exactly as it has
            // no wire identity: the writer REFUSES rather than writing None
            // over it, the rule measure and save already apply (§5)
            if ((long)value > f.EnumMax) { return false; }
            if (value != 0 && f.VariantId(value) == 0) { return false; }
            string name = f.EnumName(value);
            if (!Named(name)) { return false; }
            WriteName(ref o, name);
            return true;
        }
        if (IsFlags(f))
        {
            ulong bits = f.GetRaw(owner, index);
            if (bits == 0)
            {
                o.Text("[]");
                return true;
            }
            o.Put((byte)'[');
            bool first = true;
            for (int bit = 0; bit < 64; bit++)
            {
                if ((bits & (1ul << bit)) == 0) { continue; }
                if (bit > f.EnumMax)
                {
                    return false; // a bit no variant names has no text spelling
                }
                string name = f.EnumName((ulong)bit);
                if (!Named(name)) { return false; }
                if (!first) { o.Put((byte)','); }
                first = false;
                o.Line(depth + 1);
                WriteName(ref o, name);
            }
            o.Line(depth);
            o.Put((byte)']');
            return true;
        }
        switch (f.Kind)
        {
            case 1:
                o.Text(f.GetRaw(owner, index) != 0 ? "true" : "false");
                return true;
            case 10:
                return WriteFloat(ref o, TableBitsToFloat(unchecked((uint)f.GetRaw(owner, index))), true);
            case 11:
                return WriteFloat(ref o, TableBitsToDouble(f.GetRaw(owner, index)), false);
            case 2: case 3: case 4: case 5:
                WriteSigned(ref o, (long)f.GetRaw(owner, index));
                return true;
            default:
                WriteUnsigned(ref o, f.GetRaw(owner, index));
                return true;
        }
    }

    static bool WriteField(ref Out o, object value, TableFieldInfo f, int depth)
    {
        if (f.Kind == 12)
        {
            WriteString(ref o, new ReadOnlySpan<byte>(f.GetBuffer(value), 0, Count(value, f)));
            return true;
        }
        if (IsBytes(f))
        {
            WriteBase64(ref o, new ReadOnlySpan<byte>(f.GetBuffer(value), 0, Count(value, f)));
            return true;
        }
        if (IsKeyed(f))
        {
            // one entry per SLOT, keyed by the variant that owns it, so
            // inserting a variant next season moves nothing in the text either.
            // Slot i holds the key i + 1: nothing is stored for None, so
            // nothing is written for it.
            o.Put((byte)'{');
            bool first = true;
            for (int slot = 0; slot < f.ArrayBound; slot++)
            {
                if (!KeyedSlotValid(f, slot)) { continue; }
                if (!first) { o.Put((byte)','); }
                first = false;
                o.Line(depth + 1);
                WriteName(ref o, f.KeyName(KeyedSlotKey(slot)));
                o.Text(": ");
                if (!WriteScalar(ref o, value, f, slot, depth + 1))
                {
                    return false;
                }
            }
            if (first) { o.Put((byte)'}'); return true; }
            o.Line(depth);
            o.Put((byte)'}');
            return true;
        }
        if (f.IsArray)
        {
            int count = Count(value, f);
            if (count == 0)
            {
                o.Text("[]");
                return true;
            }
            o.Put((byte)'[');
            for (int i = 0; i < count; i++)
            {
                if (i > 0) { o.Put((byte)','); }
                o.Line(depth + 1);
                if (!WriteScalar(ref o, value, f, i, depth + 1))
                {
                    return false;
                }
            }
            o.Line(depth);
            o.Put((byte)']');
            return true;
        }
        return WriteScalar(ref o, value, f, 0, depth);
    }

    // One instance, every field, in DECLARATION ORDER, defaults included — a
    // text is for people and tools, and a text that elides is a text a reader
    // has to know the schema to complete.
    static bool WriteValue(ref Out o, object value, TableTypeInfo info, int depth)
    {
        bool any = false;
        for (int i = 0; i < info.NumFields; i++)
        {
            TableFieldInfo f = info.Fields[i];
            if (f.Guard.Length != 0 && !GuardHolds(value, info, f.Guard)) { continue; }
            // an ABSENT optional writes no key: presence of the key IS the
            // presence (§16.2), so an absent field is an absent key and nothing
            // else would read back as absent
            if (f.Optional && !f.GetPresent(value)) { continue; }
            if (!any) { o.Put((byte)'{'); }
            else { o.Put((byte)','); }
            any = true;
            o.Line(depth + 1);
            WriteName(ref o, f.Json);
            o.Text(": ");
            if (!WriteField(ref o, value, f, depth + 1)) { return false; }
        }
        if (!any)
        {
            o.Text("{}");
            return true;
        }
        o.Line(depth);
        o.Put((byte)'}');
        return true;
    }

    // ---- reading ----

    // THE READER, and C# forces its shape. The reference holds the text, the
    // cursor and the report in ONE ref struct over the span. C# refuses to hand
    // a stackalloc'd buffer to a method that also takes a ref struct BY REF
    // (CS8350: the callee could store the buffer in it), and this walk needs a
    // scratch buffer for every key it compares — one per frame in C++, which is
    // a char[256] there. So the reader is SPLIT: a plain struct carries the
    // cursor, the report and the malformed flag, and the TEXT rides beside it
    // as its own ReadOnlySpan parameter. Nothing is copied and nothing is
    // allocated; the span stays the caller's bytes the whole way down.
    public struct In
    {
        public int Pos;
        public TableReport Report;
        public bool Bad; // the text is not JSON: the walk stops and keeps what it placed
    }

    static void Space(ReadOnlySpan<byte> text, ref In input)
    {
        while (input.Pos < text.Length)
        {
            byte c = text[input.Pos];
            if (c == ' ' || c == '\t' || c == '\n' || c == '\r') { input.Pos++; continue; }
            // comments are not JSON, and a walk that guessed at one would be
            // reading a dialect nobody wrote down
            if (c == '/') { input.Bad = true; }
            return;
        }
    }

    static byte Peek(ReadOnlySpan<byte> text, ref In input)
    {
        Space(text, ref input);
        return input.Pos < text.Length ? text[input.Pos] : (byte)0;
    }

    // the shape of the value sitting at the cursor, without consuming it
    static char ValueShape(ReadOnlySpan<byte> text, ref In input)
    {
        byte c = Peek(text, ref input);
        switch (c)
        {
            case (byte)'{': return 'o';
            case (byte)'[': return 'a';
            case (byte)'"': return 's';
            case (byte)'t': case (byte)'f': return 'b';
            case (byte)'n': return 'z';
            case 0: return (char)0;
            default: return 'n';
        }
    }

    static bool Literal(ReadOnlySpan<byte> text, ref In input, string word)
    {
        if (input.Pos + word.Length > text.Length) { input.Bad = true; return false; }
        for (int i = 0; i < word.Length; i++)
        {
            if (text[input.Pos + i] != (byte)word[i]) { input.Bad = true; return false; }
        }
        input.Pos += word.Length;
        return true;
    }

    // one \uXXXX escape body; -1 when the four hex digits are not there
    static int Hex4(ReadOnlySpan<byte> text, ref In input)
    {
        if (input.Pos + 4 > text.Length) { return -1; }
        int value = 0;
        for (int i = 0; i < 4; i++)
        {
            byte c = text[input.Pos + i];
            int digit;
            if (c >= '0' && c <= '9') { digit = c - '0'; }
            else if (c >= 'a' && c <= 'f') { digit = c - 'a' + 10; }
            else if (c >= 'A' && c <= 'F') { digit = c - 'A' + 10; }
            else { return -1; }
            value = (value << 4) | digit;
        }
        input.Pos += 4;
        return value;
    }

    static int EncodeUtf8(uint code, Span<byte> unit)
    {
        if (code < 0x80) { unit[0] = (byte)code; return 1; }
        if (code < 0x800)
        {
            unit[0] = (byte)(0xc0 | (code >> 6));
            unit[1] = (byte)(0x80 | (code & 0x3f));
            return 2;
        }
        if (code < 0x10000)
        {
            unit[0] = (byte)(0xe0 | (code >> 12));
            unit[1] = (byte)(0x80 | ((code >> 6) & 0x3f));
            unit[2] = (byte)(0x80 | (code & 0x3f));
            return 3;
        }
        unit[0] = (byte)(0xf0 | (code >> 18));
        unit[1] = (byte)(0x80 | ((code >> 12) & 0x3f));
        unit[2] = (byte)(0x80 | ((code >> 6) & 0x3f));
        unit[3] = (byte)(0x80 | (code & 0x3f));
        return 4;
    }

    // Scan one JSON string into the caller's span. Bytes are appended ONE CODE
    // POINT AT A TIME — an escape's encoding, or a UTF-8 sequence read whole —
    // so a string longer than the field is clamped AT A CODE POINT BOUNDARY and
    // never cut through a multi-byte character. Clamping is counted, never
    // fatal, exactly as it is on the wire (§4). keep false scans past a
    // string without keeping it, and counts no clamp for what it dropped —
    // C++'s NULL destination.
    static bool ScanString(ReadOnlySpan<byte> text, ref In input, Span<byte> destination, bool keep, out int length)
    {
        length = 0;
        if (Peek(text, ref input) != '"') { input.Bad = true; return false; }
        input.Pos++;
        int placed = 0;
        bool clamped = false;
        Span<byte> unit = stackalloc byte[4];
        for (;;)
        {
            if (input.Pos >= text.Length) { input.Bad = true; return false; }
            byte c = text[input.Pos];
            if (c == '"') { input.Pos++; break; }
            int unitLength = 0;
            if (c == '\\')
            {
                input.Pos++;
                if (input.Pos >= text.Length) { input.Bad = true; return false; }
                byte escape = text[input.Pos++];
                switch (escape)
                {
                    case (byte)'"': unit[0] = (byte)'"'; unitLength = 1; break;
                    case (byte)'\\': unit[0] = (byte)'\\'; unitLength = 1; break;
                    case (byte)'/': unit[0] = (byte)'/'; unitLength = 1; break;
                    case (byte)'b': unit[0] = (byte)'\b'; unitLength = 1; break;
                    case (byte)'f': unit[0] = (byte)'\f'; unitLength = 1; break;
                    case (byte)'n': unit[0] = (byte)'\n'; unitLength = 1; break;
                    case (byte)'r': unit[0] = (byte)'\r'; unitLength = 1; break;
                    case (byte)'t': unit[0] = (byte)'\t'; unitLength = 1; break;
                    case (byte)'u':
                    {
                        int high = Hex4(text, ref input);
                        if (high < 0) { input.Bad = true; return false; }
                        uint code = (uint)high;
                        if (high >= 0xd800 && high <= 0xdbff && input.Pos + 2 <= text.Length &&
                            text[input.Pos] == '\\' && text[input.Pos + 1] == 'u')
                        {
                            int mark = input.Pos;
                            input.Pos += 2;
                            int low = Hex4(text, ref input);
                            if (low >= 0xdc00 && low <= 0xdfff)
                            {
                                code = (uint)(0x10000 + (((uint)high - 0xd800) << 10) + ((uint)low - 0xdc00));
                            }
                            else
                            {
                                input.Pos = mark; // a lone lead surrogate rides as itself
                            }
                        }
                        // a surrogate half that never found its partner has no
                        // UTF-8 encoding: encoding it anyway would manufacture
                        // CESU-8 — invalid UTF-8 — out of input that was valid
                        // JSON, so it reads as the replacement character
                        if (code >= 0xd800 && code <= 0xdfff) { code = 0xfffd; }
                        unitLength = EncodeUtf8(code, unit);
                        break;
                    }
                    default: input.Bad = true; return false;
                }
            }
            else if (c < 0x20)
            {
                input.Bad = true; // a raw control character is not a JSON string body
                return false;
            }
            else
            {
                // a UTF-8 sequence read WHOLE, so the clamp below can only land
                // between code points. Only bytes that ACTUALLY look like
                // continuations are taken: the wire imposes no encoding (§3),
                // so a string may legitimately hold a stray lead byte, and one
                // at the end of a text must not swallow the closing quote.
                int want = 1;
                if ((c & 0xe0) == 0xc0) { want = 2; }
                else if ((c & 0xf0) == 0xe0) { want = 3; }
                else if ((c & 0xf8) == 0xf0) { want = 4; }
                unit[0] = c;
                input.Pos++;
                unitLength = 1;
                while (unitLength < want && input.Pos < text.Length &&
                       (text[input.Pos] & 0xc0) == 0x80)
                {
                    unit[unitLength++] = text[input.Pos++];
                }
            }
            if (keep)
            {
                if (placed + unitLength <= destination.Length)
                {
                    unit.Slice(0, unitLength).CopyTo(destination.Slice(placed, unitLength));
                    placed += unitLength;
                }
                else
                {
                    clamped = true;
                }
            }
        }
        if (clamped) { input.Report.Clamped++; }
        length = placed;
        return true;
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
    static bool WalkNumber(ReadOnlySpan<byte> text, ref In input, out bool integral)
    {
        Space(text, ref input);
        integral = true;
        if (input.Pos < text.Length && text[input.Pos] == '-') { input.Pos++; }
        // int: a lone zero, or a non-zero digit and any digits after it
        if (input.Pos >= text.Length) { return false; }
        if (text[input.Pos] == '0')
        {
            input.Pos++;
        }
        else if (text[input.Pos] >= '1' && text[input.Pos] <= '9')
        {
            while (input.Pos < text.Length && text[input.Pos] >= '0' && text[input.Pos] <= '9') { input.Pos++; }
        }
        else
        {
            return false;
        }
        // frac
        if (input.Pos < text.Length && text[input.Pos] == '.')
        {
            input.Pos++;
            if (input.Pos >= text.Length || text[input.Pos] < '0' || text[input.Pos] > '9') { return false; }
            while (input.Pos < text.Length && text[input.Pos] >= '0' && text[input.Pos] <= '9') { input.Pos++; }
            integral = false;
        }
        // exp
        if (input.Pos < text.Length && (text[input.Pos] == 'e' || text[input.Pos] == 'E'))
        {
            input.Pos++;
            if (input.Pos < text.Length && (text[input.Pos] == '-' || text[input.Pos] == '+')) { input.Pos++; }
            if (input.Pos >= text.Length || text[input.Pos] < '0' || text[input.Pos] > '9') { return false; }
            while (input.Pos < text.Length && text[input.Pos] >= '0' && text[input.Pos] <= '9') { input.Pos++; }
            integral = false;
        }
        return true;
    }

    // the same production, with the token kept for conversion. The token is
    // ASCII by that grammar, so it widens into chars a digit at a time and the
    // conversions below need no encoder.
    static bool ScanNumber(ReadOnlySpan<byte> text, ref In input, Span<char> token, out int length, out bool integral)
    {
        length = 0;
        Space(text, ref input);
        int start = input.Pos;
        if (!WalkNumber(text, ref input, out integral)) { return false; }
        int count = input.Pos - start;
        if (count <= 0 || count >= token.Length) { return false; }
        for (int i = 0; i < count; i++) { token[i] = (char)text[start + i]; }
        length = count;
        return true;
    }

    // the token's exact double, through the runtime's own converter — under the
    // INVARIANT culture, so the decimal point is always '.' and the walk never
    // crosses a locale's own
    static double TokenDouble(ReadOnlySpan<char> token, bool single)
    {
        if (single)
        {
            float narrow;
            if (!float.TryParse(token, System.Globalization.NumberStyles.Float,
                    System.Globalization.CultureInfo.InvariantCulture, out narrow))
            {
                return double.NaN;
            }
            return narrow;
        }
        double value;
        if (!double.TryParse(token, System.Globalization.NumberStyles.Float,
                System.Globalization.CultureInfo.InvariantCulture, out value))
        {
            return double.NaN;
        }
        return value;
    }

    // the token's exact integer, parsed digit by digit so no width and no
    // locale can move it. Saturation is reported as a clamp, the wire's rule
    // for a value outside what the reader can hold (§4).
    static long TokenInteger(ReadOnlySpan<char> token, bool isSigned, out bool saturated)
    {
        int i = 0;
        bool negative = false;
        if (i < token.Length && (token[i] == '-' || token[i] == '+'))
        {
            negative = token[i] == '-';
            i++;
        }
        ulong magnitude = 0;
        bool over = false;
        for (; i < token.Length; i++)
        {
            ulong digit = (ulong)(token[i] - '0');
            if (magnitude > (ulong.MaxValue - digit) / 10) { over = true; break; }
            magnitude = magnitude * 10 + digit;
        }
        if (!isSigned)
        {
            // -0 IS zero, and clamping it would report an event that did not
            // happen; only a real negative magnitude is out of range here
            if (negative) { saturated = magnitude != 0; return 0; }
            if (over) { saturated = true; return unchecked((long)ulong.MaxValue); }
            saturated = false;
            return unchecked((long)magnitude);
        }
        if (negative)
        {
            if (over || magnitude > (1ul << 63)) { saturated = true; return long.MinValue; }
            saturated = false;
            if (magnitude == (1ul << 63)) { return long.MinValue; }
            return -(long)magnitude;
        }
        if (over || magnitude > long.MaxValue) { saturated = true; return long.MaxValue; }
        saturated = false;
        return (long)magnitude;
    }

    static bool SkipContainer(ReadOnlySpan<byte> text, ref In input, byte close, int depth)
    {
        if (depth > MaxDepth) { input.Bad = true; return false; }
        input.Pos++; // the opening bracket
        for (;;)
        {
            byte c = Peek(text, ref input);
            if (c == close) { input.Pos++; return true; }
            if (c == 0) { input.Bad = true; return false; }
            if (close == '}')
            {
                int ignored;
                if (!ScanString(text, ref input, Span<byte>.Empty, false, out ignored)) { return false; }
                if (Peek(text, ref input) != ':') { input.Bad = true; return false; }
                input.Pos++;
            }
            if (!SkipValue(text, ref input, depth + 1)) { return false; }
            c = Peek(text, ref input);
            if (c == ',') { input.Pos++; continue; }   // a trailing comma is accepted
            if (c == close) { input.Pos++; return true; }
            input.Bad = true;
            return false;
        }
    }

    static bool SkipValue(ReadOnlySpan<byte> text, ref In input, int depth)
    {
        byte c = Peek(text, ref input);
        switch (c)
        {
            case (byte)'{': return SkipContainer(text, ref input, (byte)'}', depth);
            case (byte)'[': return SkipContainer(text, ref input, (byte)']', depth);
            case (byte)'"':
            {
                int ignored;
                return ScanString(text, ref input, Span<byte>.Empty, false, out ignored);
            }
            case (byte)'t': return Literal(text, ref input, "true");
            case (byte)'f': return Literal(text, ref input, "false");
            case (byte)'n': return Literal(text, ref input, "null");
            case 0: input.Bad = true; return false;
            default:
            {
                // consumed, never converted: skipping needs no buffer, and this
                // is the one walk a hostile text drives to the depth cap. It is
                // the SAME production the value path scans, so an unknown key
                // cannot smuggle past a number a named key would refuse.
                bool integral;
                if (!WalkNumber(text, ref input, out integral)) { input.Bad = true; return false; }
                return true;
            }
        }
    }

    // compare a scanned UTF-8 key against a descriptor's string. Schema
    // identifiers are ASCII, so the common case is a byte walk; a json = "key"
    // that is not falls to the encoder. C++ compares two char* and needs
    // neither branch.
    static bool Same(ReadOnlySpan<byte> text, string name)
    {
        if (name == null) { return false; }
        for (int i = 0; i < name.Length; i++)
        {
            char c = name[i];
            if (c >= 0x80) { return SameEncoded(text, name); }
            if (i >= text.Length || text[i] != (byte)c) { return false; }
        }
        return name.Length == text.Length;
    }

    static bool SameEncoded(ReadOnlySpan<byte> text, string name)
    {
        Span<byte> encoded = stackalloc byte[MaxKey];
        int n = System.Text.Encoding.UTF8.GetByteCount(name);
        if (n > encoded.Length) { return false; }
        System.Text.Encoding.UTF8.GetBytes(name.AsSpan(), encoded);
        return text.SequenceEqual(encoded.Slice(0, n));
    }

    // place one scalar at one storage slot
    static bool ReadScalar(ReadOnlySpan<byte> text, ref In input, object owner, TableFieldInfo f, int index, int depth)
    {
        if (f.Arms != null)
        {
            // a union is an object with ONE key, the arm's name; {} is None,
            // and two keys is a text this walk will not guess at
            object union = f.GetChild(owner, index);
            if (Peek(text, ref input) != '{') { input.Bad = true; return false; }
            input.Pos++;
            f.Arms.SetTag(union, 0);
            if (Peek(text, ref input) == '}') { input.Pos++; return true; }
            Span<byte> key = stackalloc byte[MaxKey];
            int keyLength;
            if (!ScanString(text, ref input, key, true, out keyLength)) { return false; }
            if (Peek(text, ref input) != ':') { input.Bad = true; return false; }
            input.Pos++;
            int tag = 0;
            for (int t = 1; t <= f.EnumMax; t++)
            {
                if (Same(key.Slice(0, keyLength), f.EnumName((ulong)t))) { tag = t; break; }
            }
            if (tag == 0)
            {
                input.Report.Unknown++;
                if (!SkipValue(text, ref input, depth + 1)) { return false; }
            }
            else
            {
                object payload = f.Arms.Arms[tag].Payload(union);
                TableTypeInfo arm = f.Arms.Arms[tag].Table;
                arm.Reset(payload);
                if (!ReadTable(text, ref input, payload, arm, depth + 1)) { return false; }
                f.Arms.SetTag(union, (ulong)tag);
            }
            byte c = Peek(text, ref input);
            if (c == ',') { input.Pos++; c = Peek(text, ref input); }
            if (c == '}') { input.Pos++; return true; }
            input.Bad = true; // a second key: a one-of with two arms is not a value
            return false;
        }
        if (f.Kind == 13)
        {
            object child = f.GetChild(owner, index);
            f.Table.Reset(child);
            return ReadTable(text, ref input, child, f.Table, depth + 1);
        }
        if (IsEnum(f))
        {
            Span<byte> name = stackalloc byte[MaxKey];
            int nameLength;
            if (!ScanString(text, ref input, name, true, out nameLength)) { return false; }
            for (int v = 0; v <= f.EnumMax; v++)
            {
                if (Same(name.Slice(0, nameLength), f.EnumName((ulong)v)))
                {
                    f.SetRaw(owner, index, (ulong)v);
                    return true;
                }
            }
            // a name this build cannot name reads as None and counts as
            // unknown, exactly as an unknown variant id does on the wire (§4)
            f.SetRaw(owner, index, 0);
            input.Report.Unknown++;
            return true;
        }
        if (IsFlags(f))
        {
            if (Peek(text, ref input) != '[') { input.Bad = true; return false; }
            input.Pos++;
            ulong bits = 0;
            for (;;)
            {
                byte c = Peek(text, ref input);
                if (c == ']') { input.Pos++; break; }
                if (c == 0) { input.Bad = true; return false; }
                if (c != '"')
                {
                    input.Report.KindMismatch++;
                    if (!SkipValue(text, ref input, depth + 1)) { return false; }
                }
                else
                {
                    Span<byte> name = stackalloc byte[MaxKey];
                    int nameLength;
                    if (!ScanString(text, ref input, name, true, out nameLength)) { return false; }
                    bool found = false;
                    for (int bit = 0; bit <= f.EnumMax; bit++)
                    {
                        if (Same(name.Slice(0, nameLength), f.EnumName((ulong)bit)))
                        {
                            bits |= 1ul << bit;
                            found = true;
                            break;
                        }
                    }
                    if (!found) { input.Report.Unknown++; }
                }
                c = Peek(text, ref input);
                if (c == ',') { input.Pos++; continue; }
                if (c == ']') { input.Pos++; break; }
                input.Bad = true;
                return false;
            }
            f.SetRaw(owner, index, bits);
            return true;
        }
        if (f.Kind == 1)
        {
            byte b = Peek(text, ref input);
            if (b == 't') { if (!Literal(text, ref input, "true")) { return false; } f.SetRaw(owner, index, 1); return true; }
            if (!Literal(text, ref input, "false")) { return false; }
            f.SetRaw(owner, index, 0);
            return true;
        }
        Span<char> token = stackalloc char[MaxNumber];
        int length;
        bool tokenIntegral;
        if (!ScanNumber(text, ref input, token, out length, out tokenIntegral))
        {
            input.Bad = true;
            return false;
        }
        if (f.Kind == 10 || f.Kind == 11)
        {
            bool single = f.Kind == 10;
            double value = TokenDouble(token.Slice(0, length), single);
            // A magnitude the field's format cannot hold is the WRONG SHAPE for
            // the kind, and it never reaches storage: 1e400 is not a float64
            // and 1e300 is not a float32. Storing the infinity the conversion
            // produced would leave an instance this walk called CLEAN that
            // ToJsonMeasure then refuses forever (a non-finite float has no
            // JSON spelling), and §16.1's one invariant is that a text which
            // reads clean writes back.
            if (!Finite(value))
            {
                input.Report.KindMismatch++;
                return true;
            }
            if (f.HasRange)
            {
                if (value < f.RangeMin) { value = f.RangeMin; input.Report.Clamped++; }
                else if (value > f.RangeMax) { value = f.RangeMax; input.Report.Clamped++; }
            }
            if (single)
            {
                float narrow = (float)value;
                if (!Finite(narrow))
                {
                    input.Report.KindMismatch++;
                    return true;
                }
                f.SetRaw(owner, index, TableFloatToBits(narrow));
            }
            else
            {
                f.SetRaw(owner, index, TableDoubleToBits(value));
            }
            return true;
        }
        // JSON HAS ONE NUMBER TYPE. 2.0 IS the integer 2 and 1e3 IS 1000, and a
        // library that round-trips numbers through a double emits them that
        // way — this walker's own float writer emits 1e+21. So an integer field
        // takes any number whose VALUE is integral, however it was spelled;
        // only a genuinely fractional value is the wrong shape for it.
        bool isSigned = f.Kind >= 2 && f.Kind <= 5;
        bool saturated = false;
        long value2 = 0;
        if (tokenIntegral)
        {
            value2 = TokenInteger(token.Slice(0, length), isSigned, out saturated);
        }
        else
        {
            double d = TokenDouble(token.Slice(0, length), false);
            if (!Finite(d))
            {
                input.Report.KindMismatch++;
                return true;
            }
            if (isSigned)
            {
                if (d >= 9223372036854775808.0) { value2 = long.MaxValue; saturated = true; }
                else if (d < -9223372036854775808.0) { value2 = long.MinValue; saturated = true; }
                else if (d != (double)(long)d) { input.Report.KindMismatch++; return true; }
                else { value2 = (long)d; }
            }
            else
            {
                if (d < 0.0)
                {
                    // a negative for an unsigned field clamps to zero, as the
                    // exact digit path already does
                    if (d != (double)(long)d) { input.Report.KindMismatch++; return true; }
                    value2 = 0;
                    saturated = true;
                }
                else if (d >= 18446744073709551616.0) { value2 = unchecked((long)ulong.MaxValue); saturated = true; }
                else if (d != (double)(ulong)d) { input.Report.KindMismatch++; return true; }
                else { value2 = unchecked((long)(ulong)d); }
            }
        }
        if (saturated) { input.Report.Clamped++; }
        if (f.HasRange)
        {
            if ((double)value2 < f.RangeMin) { value2 = (long)f.RangeMin; input.Report.Clamped++; }
            else if ((double)value2 > f.RangeMax) { value2 = (long)f.RangeMax; input.Report.Clamped++; }
        }
        // the field's own storage width is the last bound: a value past it
        // clamps rather than wrapping, which is what the wire does too
        if (f.ElemWidth < 8)
        {
            if (isSigned)
            {
                long high = (1L << (f.ElemWidth * 8 - 1)) - 1;
                long low = -high - 1;
                if (value2 > high) { value2 = high; input.Report.Clamped++; }
                else if (value2 < low) { value2 = low; input.Report.Clamped++; }
            }
            else
            {
                ulong high = (1ul << (f.ElemWidth * 8)) - 1;
                if (value2 < 0) { value2 = 0; input.Report.Clamped++; }
                else if ((ulong)value2 > high) { value2 = (long)high; input.Report.Clamped++; }
            }
        }
        // at eight bytes the storage IS the parser's width, and an unsigned
        // value past long.MaxValue rides here as a negative long by design —
        // the token parser already turned a NEGATIVE token for an unsigned
        // field into a clamped zero, so there is nothing left to bound.
        f.SetRaw(owner, index, unchecked((ulong)value2));
        return true;
    }

    // put one array field's every slot back at its declared defaults. A table
    // element's defaults are its own (the reset hook); every other element
    // kind's storage default is zero, which is what the generated array
    // declares. There is no union arm here because an ARRAY OF UNIONS is
    // refused by name (docs/SPEC-TABLES.md §11).
    static void ResetSlots(object value, TableFieldInfo f)
    {
        for (int i = 0; i < f.ArrayBound; i++)
        {
            if (f.Kind == 13) { f.Table.Reset(f.GetChild(value, i)); }
            else { f.SetRaw(value, i, 0); }
        }
    }

    static bool ReadField(ReadOnlySpan<byte> text, ref In input, object value, TableFieldInfo f, int depth)
    {
        if (f.Kind == 12)
        {
            byte[] storage = f.GetBuffer(value);
            int length;
            if (!ScanString(text, ref input, new Span<byte>(storage, 0, f.ArrayBound), true, out length)) { return false; }
            PutCount(value, f, length);
            return true;
        }
        if (IsBytes(f))
        {
            // base64 decodes STRAIGHT INTO the field's storage, six bits at a
            // time — no window, no temporary, so a bytes(N) of any declared
            // extent reads the same way. A base64 body carries no escapes, so a
            // backslash in one is simply not an alphabet character.
            if (Peek(text, ref input) != '"') { input.Bad = true; return false; }
            input.Pos++;
            byte[] storage = f.GetBuffer(value);
            Array.Clear(storage, 0, f.ArrayBound);
            PutCount(value, f, 0);
            int placed = 0;
            uint accumulator = 0;
            int held = 0;
            bool clamped = false;
            bool malformed = false;
            for (;;)
            {
                if (input.Pos >= text.Length) { input.Bad = true; return false; }
                byte c = text[input.Pos++];
                if (c == '"') { break; }
                if (c == '=' || malformed) { continue; }
                int at = Base64Alphabet.IndexOf((char)c);
                if (at < 0) { malformed = true; continue; }
                accumulator = (accumulator << 6) | (uint)at;
                held += 6;
                if (held >= 8)
                {
                    held -= 8;
                    if (placed < f.ArrayBound)
                    {
                        storage[placed++] = (byte)((accumulator >> held) & 0xff);
                    }
                    else
                    {
                        clamped = true;
                    }
                }
            }
            if (malformed)
            {
                // a body that is not base64 is the wrong shape for the kind:
                // the field keeps its default and the event is counted
                input.Report.KindMismatch++;
                return true;
            }
            if (clamped) { input.Report.Clamped++; }
            PutCount(value, f, placed);
            return true;
        }
        if (IsKeyed(f))
        {
            if (Peek(text, ref input) != '{') { input.Bad = true; return false; }
            input.Pos++;
            // every slot back to its declared defaults first, so a key the text
            // omits keeps them and a repeated field key cannot leave an earlier
            // occurrence's slots standing
            ResetSlots(value, f);
            char shape = ElementShape(f);
            // A KEYED OBJECT'S KEYS ARE KEYS: a variant named twice is a
            // duplicate key like any other, last-wins and counted (§16.2).
            // Tracked the way a table's own field keys are — a bounded,
            // allocation-free bitmask; a vocabulary wider than this still
            // reads, its repeats simply stop being counted.
            Span<ulong> seen = stackalloc ulong[8];
            seen.Clear();
            for (;;)
            {
                byte c = Peek(text, ref input);
                if (c == '}') { input.Pos++; break; }
                if (c == 0) { input.Bad = true; return false; }
                Span<byte> key = stackalloc byte[MaxKey];
                int keyLength;
                if (!ScanString(text, ref input, key, true, out keyLength)) { return false; }
                if (Peek(text, ref input) != ':') { input.Bad = true; return false; }
                input.Pos++;
                int slot = -1;
                for (int v = 0; v < f.ArrayBound; v++)
                {
                    // nothing is stored for None, so "None" finds no slot and
                    // is an unknown key like any other name this reader cannot
                    // place
                    if (!KeyedSlotValid(f, v)) { continue; }
                    if (Same(key.Slice(0, keyLength), f.KeyName(KeyedSlotKey(v)))) { slot = v; break; }
                }
                if (slot >= 0 && slot < 512)
                {
                    ulong bit = 1ul << (slot & 63);
                    if ((seen[slot >> 6] & bit) != 0) { input.Report.Duplicate++; }
                    seen[slot >> 6] |= bit;
                }
                if (slot < 0)
                {
                    input.Report.Unknown++;
                    if (!SkipValue(text, ref input, depth + 1)) { return false; }
                }
                else if (ValueShape(text, ref input) != shape)
                {
                    input.Report.KindMismatch++;
                    if (!SkipValue(text, ref input, depth + 1)) { return false; }
                }
                else if (!ReadScalar(text, ref input, value, f, slot, depth + 1))
                {
                    return false;
                }
                c = Peek(text, ref input);
                if (c == ',') { input.Pos++; continue; } // a trailing comma is accepted
                if (c == '}') { input.Pos++; break; }
                input.Bad = true;
                return false;
            }
            return true;
        }
        if (f.IsArray)
        {
            if (Peek(text, ref input) != '[') { input.Bad = true; return false; }
            input.Pos++;
            // LAST WINS has to be true of a repeated ARRAY key too, and it is
            // wire-visible: a fixed array writes every slot, so a second,
            // shorter occurrence overlaying a prefix would leave the first
            // occurrence's tail standing. The field goes back to its declared
            // defaults before this occurrence's elements are placed — the
            // re-establishment a nested table and a union arm already get.
            ResetSlots(value, f);
            PutCount(value, f, 0);
            int placed = 0;
            char shape = ElementShape(f);
            for (;;)
            {
                byte c = Peek(text, ref input);
                if (c == ']') { input.Pos++; break; }
                if (c == 0) { input.Bad = true; return false; }
                if (placed >= f.ArrayBound)
                {
                    // more elements than the reader's bound: the bounded prefix
                    // is kept and the excess counts, the wire's rule (§4)
                    input.Report.Clamped++;
                    if (!SkipValue(text, ref input, depth + 1)) { return false; }
                }
                else if (ValueShape(text, ref input) != shape)
                {
                    input.Report.KindMismatch++;
                    if (!SkipValue(text, ref input, depth + 1)) { return false; }
                    placed++;
                }
                else
                {
                    if (!ReadScalar(text, ref input, value, f, placed, depth + 1)) { return false; }
                    placed++;
                }
                c = Peek(text, ref input);
                if (c == ',') { input.Pos++; continue; }
                if (c == ']') { input.Pos++; break; }
                input.Bad = true;
                return false;
            }
            // a fixed array's tail keeps the defaults the prefill left there,
            // exactly as a short wire count does
            PutCount(value, f, placed);
            return true;
        }
        return ReadScalar(text, ref input, value, f, 0, depth);
    }

    // ONE table object: keys are field keys, unknown ones are skipped and
    // counted, a repeated key is last-wins and counted. The instance is already
    // at its declared defaults when this is entered, so a key the text never
    // mentions keeps the default an absent field takes on the wire (§4).
    static bool ReadTable(ReadOnlySpan<byte> text, ref In input, object value, TableTypeInfo info, int depth)
    {
        if (depth > MaxDepth) { input.Bad = true; return false; }
        if (Peek(text, ref input) != '{') { input.Bad = true; return false; }
        input.Pos++;
        // duplicate tracking, bounded and allocation-free: a table with more
        // fields than this still reads, its repeats simply stop being counted
        Span<ulong> seen = stackalloc ulong[8];
        seen.Clear();
        for (;;)
        {
            byte c = Peek(text, ref input);
            if (c == '}') { input.Pos++; return true; }
            if (c == 0) { input.Bad = true; return false; }
            Span<byte> key = stackalloc byte[MaxKey];
            int keyLength;
            if (!ScanString(text, ref input, key, true, out keyLength)) { return false; }
            if (Peek(text, ref input) != ':') { input.Bad = true; return false; }
            input.Pos++;
            int index = -1;
            for (int i = 0; i < info.NumFields; i++)
            {
                if (Same(key.Slice(0, keyLength), info.Fields[i].Json)) { index = i; break; }
            }
            if (index < 0)
            {
                input.Report.Unknown++;
                if (!SkipValue(text, ref input, depth + 1)) { return false; }
            }
            else
            {
                TableFieldInfo f = info.Fields[index];
                if (index < 512)
                {
                    ulong bit = 1ul << (index & 63);
                    if ((seen[index >> 6] & bit) != 0) { input.Report.Duplicate++; }
                    seen[index >> 6] |= bit;
                }
                // PRESENCE OF THE KEY IS THE PRESENCE (§16.2): reaching this
                // line is the key being present, so an optional is set present
                // whatever its value — with one exception the page names: a
                // JSON null, which reads as ABSENT rather than as a value.
                char got = ValueShape(text, ref input);
                if (f.Optional && got == 'z')
                {
                    if (!Literal(text, ref input, "null")) { return false; }
                    // absent, and back at its defaults: a repeated key whose
                    // last occurrence is null must not leave an earlier value
                    // standing
                    if (f.Table != null) { f.Table.Reset(f.GetChild(value, 0)); }
                    else { f.SetRaw(value, 0, 0); }
                    f.SetPresent(value, false);
                }
                else
                {
                    if (got != Shape(f))
                    {
                        // the wrong JSON type for the kind: skipped, never
                        // coerced
                        input.Report.KindMismatch++;
                        if (!SkipValue(text, ref input, depth + 1)) { return false; }
                    }
                    else if (!ReadField(text, ref input, value, f, depth))
                    {
                        return false;
                    }
                    if (f.Optional)
                    {
                        f.SetPresent(value, true);
                    }
                }
            }
            c = Peek(text, ref input);
            if (c == ',') { input.Pos++; continue; } // a trailing comma is accepted
            if (c == '}') { input.Pos++; return true; }
            input.Bad = true;
            return false;
        }
    }

    // ---- the two entry points the per-table wrappers name ----

    public static bool Read(object value, TableTypeInfo info, ReadOnlySpan<byte> text, TableReport report)
    {
        In input = new In();
        input.Pos = 0;
        // a caller with no report is off the read path this section prices: it
        // gets one rather than a branch on every counter
        input.Report = report != null ? report : new TableReport();
        input.Bad = false;
        info.Reset(value);
        // C++ refuses a null pointer and a negative length here before it walks.
        // A span is neither: an empty one is a text with no object in it, which
        // the walk below already calls malformed, so there is nothing to check
        // that the walk does not answer.
        bool ok = ReadTable(text, ref input, value, info, 0);
        if (ok)
        {
            // the canonical text ends with ONE newline and a text without one
            // is the same text: whitespace after the object is skipped either
            // way, and anything else is trailing rubbish rather than one text
            Space(text, ref input);
            if (input.Pos != text.Length) { input.Bad = true; }
        }
        if (input.Bad || !ok)
        {
            input.Report.Malformed = true;
            return false;
        }
        return true;
    }

    public static long Write(object value, TableTypeInfo info, Span<byte> buffer, bool measuring)
    {
        Out o = new Out();
        o.Buffer = buffer;
        o.Measuring = measuring;
        o.Offset = 0;
        o.Overflow = false;
        if (!WriteValue(ref o, value, info, 0)) { return -1; }
        // THE CANONICAL TEXT ENDS WITH EXACTLY ONE NEWLINE (§16.1). Every
        // writer emits it — this one, the C++ walk and schema unpack — and
        // every reader accepts a text with or without.
        o.Put((byte)'\n');
        if (o.Overflow) { return -1; }
        return o.Offset;
    }
}
// ---- json walk: end ----
`
