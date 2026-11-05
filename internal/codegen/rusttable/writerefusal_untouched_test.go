package rusttable

// TestWriteRefusalLeavesBufferRust holds the Row writers to "a refusal writes
// nothing" (docs/SPEC.md §5.1): FixedSave and SaveMessages are handed a batch
// of two whose SECOND value is refused, into a buffer POISONED with 0xA5 and
// long enough for the whole batch, and the poison must be intact afterwards, so
// a writer that stored the first value (or the message form's two header
// bytes) before it met the second goes red. The probe runs twice: with debug
// assertions on, where the write contract's debug_assert! is the refusal (a
// panic caught here), and off, Rust's release semantics, where the refusals
// that hold in every build (a NaN, a count or a length past its storage, the
// message form's enum and tag, a buffer too short) are the ones left. A row
// the release build writes on purpose (a ranged integer off its range, a zero
// byte in fixed text) runs in the debug probe only.
//
// A FUNCTIONAL TEST: it compiles and runs Rust with rustc (messageBatchRustc).
// The Go half reads only the generator's output: no clock, no socket, no
// network.

import (
	"testing"
)

const untouchedSrc = `package untouched

enum Coin { Copper, Silver, Gold }

type Bonus
{
    amount int32
}

type Penalty
{
    amount int32
}

union Prize
{
    bonus   Bonus
    penalty Penalty
}

fixed table Order
{
    qty   int32 = 1 | min = 1, max = 99
    price float32
    sku   string(4)
    coin  Coin
    tags  [..3]int32
    prize Prize
}
`

const untouchedMain = `
    let debug = cfg!(debug_assertions);
    let mut good = OrderRow::default();
    good.qty = 5;
    good.price = 2.5;
    good.sku[..2].copy_from_slice(b"ab");
    good.sku_length = 2;
    good.coin = 2;
    good.tags_count = 1;
    good.tags[0] = 7;
    // A batch of two: the first value is good, the second is the one refused.
    let cases: Vec<(&str, bool, Box<dyn Fn(&mut OrderRow)>)> = vec![
        ("nan", true, Box::new(|v: &mut OrderRow| v.price = f32::NAN)),
        ("length", true, Box::new(|v: &mut OrderRow| v.sku_length = 5)),
        ("count", true, Box::new(|v: &mut OrderRow| v.tags_count = 4)),
        ("negative_count", true, Box::new(|v: &mut OrderRow| v.tags_count = -1)),
        ("range", false, Box::new(|v: &mut OrderRow| v.qty = 100)),
    ];
    let untouched = |id: String, len: usize, save: &dyn Fn(&mut [u8]) -> bool| {
        let mut buf = vec![0xA5u8; len];
        let previous = std::panic::take_hook();
        std::panic::set_hook(Box::new(|_| {}));
        let outcome = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| save(&mut buf)));
        std::panic::set_hook(previous);
        let refused = match outcome { Ok(wrote) => !wrote, Err(_) => true };
        let intact = buf.iter().all(|&b| b == 0xA5);
        say(&id, refused && intact, format!("refused={refused} intact={intact}"));
    };
    let fixed_len = order_fixed_measure(2);
    let message_len = 256;
    for (name, every_build, mutate) in cases.iter() {
        if !every_build && !debug {
            continue;
        }
        let mut bad = good;
        mutate(&mut bad);
        let batch = [good, bad];
        untouched(format!("fixed_{name}"), fixed_len, &|buf| order_fixed_save(&batch, buf).is_some());
        untouched(format!("message_{name}"), message_len, &|buf| order_save_messages(&batch, buf, &mut TableReport::default()) >= 0);
    }
    // the fixed writer's debug-only rules; the message form's every-build ones
    if debug {
        let mut bad = good;
        bad.sku[..3].copy_from_slice(b"a\0b");
        bad.sku_length = 3;
        let batch = [good, bad];
        untouched("fixed_zero".to_string(), fixed_len, &|buf| order_fixed_save(&batch, buf).is_some());
    }
    {
        let mut bad = good;
        bad.coin = 4;
        let batch = [good, bad];
        untouched("message_enum".to_string(), message_len, &|buf| order_save_messages(&batch, buf, &mut TableReport::default()) >= 0);
    }
    {
        let mut bad = good;
        bad.prize.tag = 3;
        let batch = [good, bad];
        untouched("message_tag".to_string(), message_len, &|buf| order_save_messages(&batch, buf, &mut TableReport::default()) >= 0);
    }
    {
        // a buffer one byte short of the batch is a -1 and never a partial write
        let batch = [good, good];
        let need = order_measure_messages(&batch, &mut TableReport::default());
        untouched("message_short".to_string(), need as usize - 1, &|buf| order_save_messages(&batch, buf, &mut TableReport::default()) >= 0);
        untouched("fixed_short".to_string(), fixed_len - 1, &|buf| order_fixed_save(&batch, buf).is_some());
    }
`

func TestWriteRefusalLeavesBufferRust(t *testing.T) {
	t.Parallel()
	messageBatchRustc(t)
	every := []string{"nan", "length", "count", "negative_count"}
	for _, build := range []struct {
		name, flag string
		debug      bool
	}{{"debug", "debug-assertions=on", true}, {"release", "debug-assertions=off", false}} {
		t.Run(build.name, func(t *testing.T) {
			t.Parallel()
			out := runMessageBatchProbe(t, unitFrom(t, untouchedSrc), untouchedMain, "-C", build.flag)
			var ids []string
			for _, c := range every {
				ids = append(ids, "fixed_"+c, "message_"+c)
			}
			ids = append(ids, "message_enum", "message_tag", "message_short", "fixed_short")
			if build.debug {
				ids = append(ids, "fixed_range", "message_range", "fixed_zero")
			}
			for _, id := range ids {
				if line := messageBatchLine(out, id); line != "BATCH "+id+" ok" {
					t.Errorf("%s build, %s: %s", build.name, id, line)
				}
			}
		})
	}
}
