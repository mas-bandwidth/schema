import os;
import .conformance_unit;

extern "schema_goose_wirefuzz_read" fn read_exact(out: u8[>..]&, n: i64) -> i64;

fn le(b: const u8[:], at: i64, w: i64) -> u64 {
    var v: u64 = 0;
    var i = w - 1;
    while i >= 0 {
        v = (v << 8) | b[at + i];
        i -= 1;
    }
    return v;
}

fn put_le(out: u8[>..]&, v: u64, w: i64) {
    for i in w { out.push((v >> (8 * i)) as! u8); }
}

fn put_i32(out: u8[>..]&, v: i64) { put_le(out, v as! u64, 4); }
fn flag(b: bool) -> u64 { if b { 1 } else { 0 } }

fn take(n: i64) -> u8[>..] {
    var b: u8[>..] = [];
    if read_exact(b, n) != n {
        write_stderr("messagefuzz: stream ended inside frame\n");
        exit(1);
    }
    return b;
}

fn main() {
    let argv = args();
    if argv.len != 3 || (argv[2] != "builder" && argv[2] != "fixed") {
        write_stderr("usage: wirefuzz <unit> builder|fixed\n");
        exit(1);
    }
    let unit = conformance_unit();
    let is_fixed_arm = argv[2] == "fixed";
    let head = take(4);
    let count = le(head, 0, 4) as! i64;
    var roots: i64[>..] = [];
    var reply: u8[>..] = [];
    for _ in count {
        let kn = take(2);
        let key = take(le(kn, 0, 2) as! i64);
        let rn = take(2);
        let root = take(le(rn, 0, 2) as! i64);
        let fr = take(2);
        var t: i64 = -1;
        if is_fixed_arm && key == argv[1] && fr[0] == 5 && fr[1] == 0 && conformance_message_root(root) {
            for d, i in unit.tables {
                if d.name == root { t = i; break; }
            }
        }
        roots.push(t);
        reply.push(if t >= 0 { 1 } else { 0 });
    }
    write_stdout(reply);
    flush_stdout();

    while true {
        var mhead: u8[>..] = [];
        let got = read_exact(mhead, 8);
        if got == 0 { return; }
        if got != 8 { exit(1); }
        let idx = le(mhead, 0, 4) as! i64;
        let size = le(mhead, 4, 4) as! i64;
        let frame = take(size);
        let name = unit.tables[roots[idx]].name;
        var mrep = schema_rt::MsgReport { .. };
        var msaved: u8[>..] = [];
        var mn: i64 = -2;
        if size < 4 {
            mrep.damaged = true;
        } else {
            let alen = le(frame, 0, 4) as! i64;
            if alen < 0 || 4 + alen > size {
                mrep.damaged = true;
            } else {
                let ann = frame[4..4 + alen];
                let batch = frame[4 + alen..size];
                mn = conformance_message_framed_fuzz(name, ann, alen > 0, batch, msaved, mrep);
            }
        }
        let accepted = mn >= 0 && !mrep.damaged && mrep.reason == 0;
        var mout: u8[>..] = [];
        mout.push(if accepted { 1 } else { 0 });
        put_i32(mout, mrep.unknown);
        put_i32(mout, mrep.mismatch);
        put_i32(mout, mrep.widened);
        put_i32(mout, mrep.oor);
        put_i32(mout, mrep.repeated);
        mout.push(flag(mrep.damaged) as! u8);
        mout.push(flag(!accepted) as! u8);
        put_le(mout, 0xFFFFFFFFFFFFFFFF, 8);
        put_i32(mout, 0);
        put_i32(mout, 0);
        if !accepted {
            put_le(mout, 0xFFFFFFFFFFFFFFFF, 8);
        } else {
            put_le(mout, msaved.len as! u64, 8);
            mout.append(msaved);
        }
        write_stdout(mout);
        flush_stdout();
    }
}
