package rusttable

// Wide text follows C++ TableJsonReadWide/WriteWide exactly, using native
// u128 arithmetic. Fixed-point fractions are exact dyadic values, never f64.
const wideRuntime = `
fn table_json_wide_signed(kind: u8) -> bool {
    kind == 18 || (20..=24).contains(&kind)
}
unsafe fn table_json_wide_load(storage: *const u8, f: &TableFieldInfo) -> u128 {
    unsafe {
        if f.elem_size == 16 {
            ptr::read_unaligned(storage as *const u128)
        } else if table_json_wide_signed(f.kind) {
            table_json_get_signed(storage, f.elem_size) as i128 as u128
        } else {
            table_json_get_raw(storage, f.elem_size) as u128
        }
    }
}
unsafe fn table_json_write_wide(out: &mut TableJsonOut, storage: *const u8, f: &TableFieldInfo) {
    let mut value = unsafe { table_json_wide_load(storage, f) };
    if table_json_wide_signed(f.kind) && (value as i128) < 0 {
        out.put(b'-');
        value = value.wrapping_neg();
    }
    let frac = f.frac_bits;
    let mut whole = value >> frac;
    let mut digits = [0u8; 40];
    let mut at = digits.len();
    loop {
        at -= 1;
        digits[at] = b'0' + (whole % 10) as u8;
        whole /= 10;
        if whole == 0 {
            break;
        }
    }
    out.raw(&digits[at..]);
    if f.kind < 20 {
        return;
    }
    out.put(b'.');
    let mask = (1u128 << frac) - 1;
    let mut fraction = value & mask;
    if fraction == 0 {
        out.put(b'0');
        return;
    }
    while fraction != 0 {
        let carry = (((fraction >> 64) * 10) + (((fraction as u64 as u128) * 10) >> 64)) >> 64;
        let product = fraction.wrapping_mul(10);
        let mut digit = product >> frac;
        if frac > 64 {
            digit |= carry << (128 - frac);
        }
        out.put(b'0' + digit as u8);
        fraction = product & mask;
    }
}
unsafe fn table_json_read_wide(
    token: &[u8],
    storage: *mut u8,
    f: &TableFieldInfo,
    report: &mut TableReport,
) -> bool {
    let signed = table_json_wide_signed(f.kind);
    let frac = f.frac_bits;
    let mut i = 0;
    let mut negative = false;
    if matches!(token.first(), Some(b'-' | b'+')) {
        negative = token[0] == b'-';
        i += 1;
    }
    let int_start = i;
    while i < token.len() && token[i].is_ascii_digit() {
        i += 1;
    }
    let int_len = i - int_start;
    let mut frac_start = i;
    let mut frac_len = 0;
    if i < token.len() && token[i] == b'.' {
        i += 1;
        frac_start = i;
        while i < token.len() && token[i].is_ascii_digit() {
            i += 1;
        }
        frac_len = i - frac_start;
    }
    let mut exponent = 0i64;
    if i < token.len() && matches!(token[i], b'e' | b'E') {
        i += 1;
        let mut neg = false;
        if i < token.len() && matches!(token[i], b'-' | b'+') {
            neg = token[i] == b'-';
            i += 1;
        }
        while i < token.len() && token[i].is_ascii_digit() {
            if exponent < 100000 {
                exponent = exponent * 10 + i64::from(token[i] - b'0');
            }
            i += 1;
        }
        if neg {
            exponent = -exponent;
        }
    }
    let digit = |k: usize| -> u8 {
        if k < int_len {
            token[int_start + k] - b'0'
        } else {
            token[frac_start + k - int_len] - b'0'
        }
    };
    let mut start = 0;
    let mut end = int_len + frac_len;
    let mut point = int_len as i64 + exponent;
    while start < end && digit(start) == 0 {
        start += 1;
        point -= 1;
    }
    while end > start && digit(end - 1) == 0 {
        end -= 1;
    }
    let mut raw = 0u128;
    let mut saturated = false;
    if start == end {
    } else if point > 40 {
        saturated = true;
        raw = if negative {
            if signed { 1u128 << 127 } else { 0 }
        } else if signed {
            i128::MAX as u128
        } else {
            u128::MAX
        };
    } else if point < -40 {
        report.kind_mismatch += 1;
        return true;
    } else {
        let mut digits = [0u8; TABLE_JSON_MAX_NUMBER + 48];
        let mut count = (-point).max(0) as usize;
        for k in start + point.max(0) as usize..end {
            digits[count] = digit(k);
            count += 1;
        }
        let mut fraction = 0u128;
        for _ in 0..frac {
            let mut carry = 0;
            for k in (0..count).rev() {
                let d = digits[k] * 2 + carry;
                digits[k] = d % 10;
                carry = d / 10;
            }
            fraction = (fraction << 1) | u128::from(carry);
        }
        if digits[..count].iter().any(|v| *v != 0) {
            report.kind_mismatch += 1;
            return true;
        }
        let mut whole = 0u128;
        for k in start..start + point.max(0) as usize {
            let d = if k < end { digit(k) } else { 0 };
            match whole
                .checked_mul(10)
                .and_then(|w| w.checked_add(u128::from(d)))
            {
                Some(w) => whole = w,
                None => {
                    saturated = true;
                    break;
                }
            }
        }
        if !saturated && frac > 0 && (whole >> (128 - frac)) != 0 {
            saturated = true;
        }
        if !saturated {
            raw = (whole << frac) | fraction;
        }
        if signed {
            if !saturated
                && ((!negative && raw > i128::MAX as u128) || (negative && raw > (1u128 << 127)))
            {
                saturated = true;
            }
            if saturated {
                raw = if negative {
                    1u128 << 127
                } else {
                    i128::MAX as u128
                };
            } else if negative {
                raw = raw.wrapping_neg();
            }
        } else {
            if saturated {
                raw = u128::MAX;
            }
            if negative && raw != 0 {
                raw = 0;
                saturated = true;
            }
        }
    }
    if saturated {
        report.clamped += 1;
    }
    if f.wide_range {
        if if signed {
            (raw as i128) < (f.wide_min as i128)
        } else {
            raw < f.wide_min
        } {
            raw = f.wide_min;
            report.clamped += 1;
        } else if if signed {
            (raw as i128) > (f.wide_max as i128)
        } else {
            raw > f.wide_max
        } {
            raw = f.wide_max;
            report.clamped += 1;
        }
    }
    unsafe {
        if f.elem_size == 16 {
            ptr::write_unaligned(storage as *mut u128, raw);
        } else {
            table_json_set_raw(storage, f.elem_size, raw as u64);
        }
    }
    true
}
`
