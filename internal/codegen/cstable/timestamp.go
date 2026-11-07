package cstable

// Timestamp is an int64 count of nanoseconds, but its JSON spelling is strict
// RFC3339. The common unescaped text is read directly from the input span;
// fractions are scanned in linear time and only nine digits are accumulated.
const tableTimestampJsonSource = `
    static bool TimestampDigits(ReadOnlySpan<byte> s, int start, int count, out int value)
    {
        value = 0;
        if (start > s.Length - count) { return false; }
        for (int i = 0; i < count; i++)
        {
            int digit = s[start + i] - '0';
            if ((uint)digit > 9) { return false; }
            value = value * 10 + digit;
        }
        return true;
    }

    static bool ParseTimestamp(ReadOnlySpan<byte> s, out long nanos)
    {
        nanos = 0;
        if (s.Length < 20 || s[4] != '-' || s[7] != '-' || (s[10] != 'T' && s[10] != 't') || s[13] != ':' || s[16] != ':') { return false; }
        if (!TimestampDigits(s, 0, 4, out int year) || !TimestampDigits(s, 5, 2, out int month) ||
            !TimestampDigits(s, 8, 2, out int day) || !TimestampDigits(s, 11, 2, out int hour) ||
            !TimestampDigits(s, 14, 2, out int minute) || !TimestampDigits(s, 17, 2, out int second)) { return false; }
        if (year < 1 || month < 1 || month > 12 || day < 1 || day > DateTime.DaysInMonth(year, month) || hour > 23 || minute > 59 || second > 59) { return false; }
        int at = 19, digits = 0;
        long fraction = 0;
        if (s[at] == '.')
        {
            at++;
            int first = at;
            while (at < s.Length && s[at] >= '0' && s[at] <= '9')
            {
                if (digits < 9) { fraction = fraction * 10 + s[at] - '0'; digits++; }
                at++;
            }
            if (at == first) { return false; }
        }
        while (digits < 9) { fraction *= 10; digits++; }
        long offset = 0;
        if (at == s.Length - 1 && (s[at] == 'Z' || s[at] == 'z')) { at++; }
        else if (at == s.Length - 6 && (s[at] == '+' || s[at] == '-') && s[at + 3] == ':')
        {
            if (!TimestampDigits(s, at + 1, 2, out int oh) || !TimestampDigits(s, at + 4, 2, out int om) || oh > 23 || om > 59) { return false; }
            offset = (oh * 3600L + om * 60L) * (s[at] == '-' ? -1 : 1);
            at += 6;
        }
        else { return false; }
        if (at != s.Length) { return false; }
        long seconds = (new DateTime(year, month, day, hour, minute, second, DateTimeKind.Utc).Ticks - DateTime.UnixEpoch.Ticks) / TimeSpan.TicksPerSecond - offset;
        Int128 total = (Int128)seconds * 1000000000 + fraction;
        if (total < long.MinValue || total > long.MaxValue) { return false; }
        nanos = (long)total;
        return true;
    }

    static bool ReadTimestamp(ReadOnlySpan<byte> text, ref In input, object owner, TableFieldInfo f, int index)
    {
        if (!DecodedStringBody(text, ref input, out ReadOnlySpan<byte> body)) { return false; }
        if (!ParseTimestamp(body, out long nanos))
        {
            input.Report.Reason ??= "timestamp_not_rfc3339: " + f.Name;
            input.Bad = true;
            return false;
        }
        f.SetRaw(owner, index, unchecked((ulong)nanos));
        return true;
    }

    static bool WriteTimestamp(ref Out output, long nanos)
    {
        long seconds = nanos / 1000000000;
        long fraction = nanos % 1000000000;
        if (fraction < 0) { fraction += 1000000000; seconds--; }
        DateTime date = DateTime.UnixEpoch.AddTicks(seconds * TimeSpan.TicksPerSecond);
        Span<char> text = stackalloc char[19];
        if (!date.TryFormat(text, out int written, "yyyy-MM-dd'T'HH:mm:ss", System.Globalization.CultureInfo.InvariantCulture) || written != 19) { return false; }
        output.Put((byte)'"');
        foreach (char c in text) { output.Put((byte)c); }
        if (fraction != 0)
        {
            Span<byte> digits = stackalloc byte[9];
            for (int i = 8; i >= 0; i--) { digits[i] = (byte)('0' + fraction % 10); fraction /= 10; }
            int count = 9;
            while (digits[count - 1] == '0') { count--; }
            output.Put((byte)'.');
            for (int i = 0; i < count; i++) { output.Put(digits[i]); }
        }
        output.Put((byte)'Z'); output.Put((byte)'"');
        return true;
    }
`
