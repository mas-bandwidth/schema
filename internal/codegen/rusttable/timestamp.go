package rusttable

// Strict RFC3339 timestamps use the shared specification's civil-day arithmetic.
// i128 holds the intermediate nanosecond count so both int64 endpoints are safe.
const timestampHelpersRust = `
#[allow(dead_code)]
pub fn table_timestamp_parse(s: &str) -> Option<i64> {
    let text = s.as_bytes();
    if text.len() < 20 || text[4] != b'-' || text[7] != b'-' ||
        !matches!(text[10], b'T' | b't') || text[13] != b':' || text[16] != b':' {
        return None;
    }
    let number = |p: usize, n: usize| -> Option<i64> {
        text.get(p..p.checked_add(n)?)?.iter().try_fold(0i64, |value, c| {
            if c.is_ascii_digit() { Some(value * 10 + i64::from(c - b'0')) } else { None }
        })
    };
    let year = number(0, 4)?;
    let month = number(5, 2)?;
    let day = number(8, 2)?;
    let hour = number(11, 2)?;
    let minute = number(14, 2)?;
    let second = number(17, 2)?;
    let leap = year % 4 == 0 && (year % 100 != 0 || year % 400 == 0);
    let month_days = match month {
        1 | 3 | 5 | 7 | 8 | 10 | 12 => 31,
        4 | 6 | 9 | 11 => 30,
        2 => if leap { 29 } else { 28 },
        _ => return None,
    };
    if day < 1 || day > month_days || hour > 23 || minute > 59 || second > 59 {
        return None;
    }
    let mut p = 19usize;
    let mut nanos = 0i64;
    if text.get(p) == Some(&b'.') {
        p += 1;
        let mut digits = 0;
        while p < text.len() && text[p].is_ascii_digit() {
            if digits < 9 { nanos = nanos * 10 + i64::from(text[p] - b'0'); }
            digits += 1;
            p += 1;
        }
        if digits == 0 { return None; }
        while digits < 9 { nanos *= 10; digits += 1; }
    }
    let offset = match *text.get(p)? {
        b'Z' | b'z' => {
            if p + 1 != text.len() { return None; }
            0
        }
        b'+' | b'-' => {
            let sign = if text[p] == b'-' { -1 } else { 1 };
            if text.len() - p != 6 || text[p + 3] != b':' { return None; }
            let h = number(p + 1, 2)?;
            let m = number(p + 4, 2)?;
            if h > 23 || m > 59 { return None; }
            sign * (h * 3600 + m * 60)
        }
        _ => return None,
    };
    let y = year - i64::from(month <= 2);
    let era = y.div_euclid(400);
    let year_of_era = y - era * 400;
    let month_of_year = if month > 2 { month - 3 } else { month + 9 };
    let day_of_year = (153 * month_of_year + 2) / 5 + day - 1;
    let day_of_era = year_of_era * 365 + year_of_era / 4 - year_of_era / 100 + day_of_year;
    let days = era * 146097 + day_of_era - 719468;
    let seconds = days * 86400 + hour * 3600 + minute * 60 + second - offset;
    i64::try_from(i128::from(seconds) * 1_000_000_000 + i128::from(nanos)).ok()
}

#[allow(dead_code)]
pub fn table_timestamp_text(value: i64) -> String {
    let seconds = value.div_euclid(1_000_000_000);
    let nanos = value.rem_euclid(1_000_000_000);
    let days = seconds.div_euclid(86400);
    let time = seconds.rem_euclid(86400);
    let civil = days + 719468;
    let era = civil.div_euclid(146097);
    let day_of_era = civil - era * 146097;
    let year_of_era = (day_of_era - day_of_era / 1460 + day_of_era / 36524 - day_of_era / 146096) / 365;
    let mut year = year_of_era + era * 400;
    let day_of_year = day_of_era - (365 * year_of_era + year_of_era / 4 - year_of_era / 100);
    let month_of_year = (5 * day_of_year + 2) / 153;
    let day = day_of_year - (153 * month_of_year + 2) / 5 + 1;
    let month = month_of_year + if month_of_year < 10 { 3 } else { -9 };
    year += i64::from(month <= 2);
    let mut out = format!("{year:04}-{month:02}-{day:02}T{:02}:{:02}:{:02}", time / 3600, time / 60 % 60, time % 60);
    if nanos != 0 {
        let fraction = format!("{nanos:09}");
        out.push('.');
        out.push_str(fraction.trim_end_matches('0'));
    }
    out.push('Z');
    out
}
`
