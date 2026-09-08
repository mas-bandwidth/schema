package cstable

// Exact decimal operations for the 128-bit and fixed-point domains. The
// reference is cpptable/json.go: representability is checked before saturation.
const tableWideJsonSource = `
    static void WriteChars(ref Out o, ReadOnlySpan<char> text)
    {
        o.Put((byte)'"');
        for (int i = 0; i < text.Length; i++)
        {
            int code = text[i];
            if (code >= 0xd800 && code <= 0xdbff && i + 1 < text.Length && text[i + 1] >= 0xdc00 && text[i + 1] <= 0xdfff)
            { code = 0x10000 + ((code - 0xd800) << 10) + text[++i] - 0xdc00; }
            else if (code >= 0xd800 && code <= 0xdfff) { code = 0xfffd; }
            if (!WriteEscape(ref o, code)) { WriteUtf8(ref o, code); }
        }
        o.Put((byte)'"');
    }
    static void WriteWide(ref Out o, UInt128 raw, TableFieldInfo f)
    {
        if (f.WideSigned && unchecked((Int128)raw) < 0) { o.Put((byte)'-'); raw = unchecked(0 - raw); }
        int frac = f.FracBits;
        UInt128 whole = raw >> frac;
        Span<char> digits = stackalloc char[40];
        whole.TryFormat(digits, out int used);
        for (int i = 0; i < used; i++) { o.Put((byte)digits[i]); }
        if (f.Kind < 20 || f.Kind > 29) { return; }
        o.Put((byte)'.');
        UInt128 mask = ((UInt128)1 << frac) - 1;
        UInt128 fraction = raw & mask;
        if (fraction == 0) { o.Put((byte)'0'); return; }
        while (fraction != 0)
        {
            UInt128 low = (UInt128)(ulong)fraction * 10;
            UInt128 high = (fraction >> 64) * 10 + (low >> 64);
            uint carry = (uint)(high >> 64);
            fraction = (high << 64) | (ulong)low;
            ulong digit = (ulong)(fraction >> frac);
            if (frac > 64) { digit |= (ulong)carry << (128 - frac); }
            o.Put((byte)('0' + (int)digit)); fraction &= mask;
        }
    }
    static bool ReadWide(ReadOnlySpan<char> token, ref In input, object owner, TableFieldInfo f, int index)
    {
        Span<byte> digits = stackalloc byte[MaxNumber];
        int pos = 0, n = 0, integer = 0;
        bool negative = token[0] == '-';
        if (negative) { pos++; }
        while (pos < token.Length && token[pos] >= '0' && token[pos] <= '9') { digits[n++] = (byte)(token[pos++] - '0'); integer++; }
        if (pos < token.Length && token[pos] == '.') { pos++; while (pos < token.Length && token[pos] >= '0' && token[pos] <= '9') { digits[n++] = (byte)(token[pos++] - '0'); } }
        int exponent = 0;
        if (pos < token.Length)
        {
            pos++; bool minus = pos < token.Length && token[pos] == '-';
            if (pos < token.Length && (token[pos] == '-' || token[pos] == '+')) { pos++; }
            while (pos < token.Length) { if (exponent < 100000) { exponent = exponent * 10 + token[pos] - '0'; } pos++; }
            if (minus) { exponent = -exponent; }
        }
        int start = 0, end = n, point = integer + exponent;
        while (start < end && digits[start] == 0) { start++; point--; }
        while (end > start && digits[end - 1] == 0) { end--; }
        UInt128 raw = 0, sign = (UInt128)1 << 127;
        bool saturated = false;
        if (start == end) { }
        else if (point > 40)
        {
            saturated = true;
            raw = negative ? (f.WideSigned ? sign : 0) : (f.WideSigned ? sign - 1 : UInt128.MaxValue);
        }
        else if (point < -40) { input.Report.KindMismatch++; return true; }
        else
        {
            Span<byte> fractionDigits = stackalloc byte[MaxNumber + 48];
            int fn = 0;
            for (int z = point; z < 0; z++) { fractionDigits[fn++] = 0; }
            for (int k = Math.Max(point, 0) + start; k < end; k++) { fractionDigits[fn++] = digits[k]; }
            UInt128 fraction = 0;
            for (int bit = 0; bit < f.FracBits; bit++)
            {
                int carry = 0;
                for (int k = fn - 1; k >= 0; k--) { int d = fractionDigits[k] * 2 + carry; fractionDigits[k] = (byte)(d % 10); carry = d / 10; }
                fraction = (fraction << 1) | (uint)carry;
            }
            for (int k = 0; k < fn; k++) { if (fractionDigits[k] != 0) { input.Report.KindMismatch++; return true; } }
            UInt128 whole = 0;
            for (int k = start; k < start + point && !saturated; k++)
            {
                uint digit = k < end ? digits[k] : 0u;
                if (whole > (UInt128.MaxValue - digit) / 10) { saturated = true; }
                else { whole = whole * 10 + digit; }
            }
            if (!saturated && f.FracBits > 0 && (whole >> (128 - f.FracBits)) != 0) { saturated = true; }
            if (!saturated) { raw = (whole << f.FracBits) | fraction; }
            if (f.WideSigned)
            {
                if (!saturated && ((!negative && raw >= sign) || (negative && raw > sign))) { saturated = true; }
                if (saturated) { raw = negative ? sign : sign - 1; }
                else if (negative) { raw = unchecked(0 - raw); }
            }
            else
            {
                if (saturated) { raw = UInt128.MaxValue; }
                if (negative && raw != 0) { raw = 0; saturated = true; }
            }
        }
        if (saturated) { input.Report.Clamped++; }
        if (f.ClampWide != null) { raw = f.ClampWide(raw, input.Report); }
        f.SetWide(owner, index, raw); return true;
    }
`
