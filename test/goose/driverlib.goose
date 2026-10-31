// driverlib.goose — the hex line helpers the Goose packet drivers share
// (test/packet-text/goose, test/packet-wide/goose). Copied beside each
// driver, which imports it as `import .driverlib;`.

fn hex_nibble(c: u8) -> u8 {
    if c >= '0' && c <= '9' { return c - '0'; }
    if c >= 'a' && c <= 'f' { return c - 'a' + 10; }
    if c >= 'A' && c <= 'F' { return c - 'A' + 10; }
    abort("driver: a wire line is not hex");
}

// hex_decode appends the bytes of a hex line ("-" is no bytes) to out.
fn hex_decode(line: const u8[:], out: u8[>..]&) {
    if line.len == 1 && line[0] == '-' { return; }
    var i: i64 = 0;
    while i + 1 < line.len {
        out.push((hex_nibble(line[i]) << 4) | hex_nibble(line[i + 1]));
        i += 2;
    }
}

// hex_append appends b as lowercase hex to out, or "-" for no bytes.
fn hex_append(out: u8[>..]&, b: const u8[:]) {
    let digits = "0123456789abcdef";
    if b.len == 0 { out.push('-'); return; }
    for c in b { out.push(digits[c >> 4]); out.push(digits[c & 15]); }
}
