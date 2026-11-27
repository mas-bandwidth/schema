// Shared write-refusal mutations for the writebounds conformance unit.
// Included by both the full conformance driver and the single-unit
// writebounds driver so the two share one source of truth.

pub enum WriteBoundsMutation {
    Done,
    Unrepresentable,
}

fn wref_int<T: TryFrom<i128>>(value: &str, slot: &mut T) -> Result<WriteBoundsMutation, String> {
    let Ok(n) = value.parse::<i128>() else {
        return Err(format!("write-refusal: int value {value}"));
    };
    match T::try_from(n) {
        Ok(v) => {
            *slot = v;
            Ok(WriteBoundsMutation::Done)
        }
        Err(_) => Ok(WriteBoundsMutation::Unrepresentable),
    }
}

fn wref_usize(value: &str) -> Result<usize, String> {
    value
        .parse::<usize>()
        .map_err(|_| format!("write-refusal: count or length {value}"))
}

pub fn mutate(v: &mut writebounds::TortureVarWref, path: &str, kind: &str, value: &str) -> Result<WriteBoundsMutation, String> {
    use writebounds::WrefArm;
    match (path, kind) {
        ("opt_arr", "count") => {
            let n = wref_usize(value)?;
            let array = v.opt_arr.get_or_insert_with(Vec::new);
            let first = array.first().copied().unwrap_or(0);
            array.resize(n, first);
            Ok(WriteBoundsMutation::Done)
        }
        ("arm_bits.a_bits", "int") => {
            let mut bits = 0u16;
            let result = wref_int(value, &mut bits)?;
            v.arm_bits = WrefArm::ABits(bits);
            Ok(result)
        }
        ("arm_range.a_range", "int") => {
            let mut n = 0i32;
            let result = wref_int(value, &mut n)?;
            v.arm_range = WrefArm::ARange(n);
            Ok(result)
        }
        ("arm_str.a_str", "length") => {
            v.arm_str = WrefArm::AStr("a".repeat(wref_usize(value)?));
            Ok(WriteBoundsMutation::Done)
        }
        ("chan_lim[1]", "int") => wref_int(value, &mut v.chan_lim[1]),
        _ => Err(format!("write-refusal: TortureVarWref names no {kind} path {path}")),
    }
}
