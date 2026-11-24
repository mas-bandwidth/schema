// The Lobster leg of test/packet-void (SPEC §4.8): payload-free union arms
// cost exactly their tag bits. Answers every case of cases.txt (copied beside
// this file with every pin by TestLobsterPacketVoid), one
// "case <name> ok|FAILED: <why>" line each; the Go test judges the lines with
// internal/casegate. The pins are the Go oracle's bytes, never this writer's.
//
// An accept case is built by name, written (bytes and bit count against the
// pin and the bits column), read from the pin with the public one-packet read
// into dirtied storage (so a payload not constructed fresh at its declared
// defaults shows), compared field by field, written again, and read once more
// with the composable body read, whose bit count must be the bits column. A
// refuse case's pin must be refused by the public one-packet read.
import .Void
import .serialize

// one overload per type: write, the public one-packet read, the body read,
// dirtied storage, and field equality (unselected arms are not compared;
// a selected payload is compared with its whole backing storage)
def wr(w:serialize_write_stream, v:packetvoid.Mixed) -> bool: return packetvoid.WriteMixed(w, v)
def wr(w:serialize_write_stream, v:packetvoid.Tags) -> bool: return packetvoid.WriteTags(w, v)
def wr(w:serialize_write_stream, v:packetvoid.Single) -> bool: return packetvoid.WriteSingle(w, v)
def wr(w:serialize_write_stream, v:packetvoid.Empty) -> bool: return packetvoid.WriteEmpty(w, v)
def wr(w:serialize_write_stream, v:packetvoid.Frame) -> bool: return packetvoid.WriteFrame(w, v)
def wr(w:serialize_write_stream, v:packetvoid.Batch) -> bool: return packetvoid.WriteBatch(w, v)

def rd(r:serialize_read_stream, v:packetvoid.Mixed) -> bool: return packetvoid.ReadMixed(r, v)
def rd(r:serialize_read_stream, v:packetvoid.Tags) -> bool: return packetvoid.ReadTags(r, v)
def rd(r:serialize_read_stream, v:packetvoid.Single) -> bool: return packetvoid.ReadSingle(r, v)
def rd(r:serialize_read_stream, v:packetvoid.Empty) -> bool: return packetvoid.ReadEmpty(r, v)
def rd(r:serialize_read_stream, v:packetvoid.Frame) -> bool: return packetvoid.ReadFrame(r, v)
def rd(r:serialize_read_stream, v:packetvoid.Batch) -> bool: return packetvoid.ReadBatch(r, v)

def body(r:serialize_read_stream, v:packetvoid.Mixed) -> bool: return packetvoid.read_body_Mixed(r, v)
def body(r:serialize_read_stream, v:packetvoid.Tags) -> bool: return packetvoid.read_body_Tags(r, v)
def body(r:serialize_read_stream, v:packetvoid.Single) -> bool: return packetvoid.read_body_Single(r, v)
def body(r:serialize_read_stream, v:packetvoid.Empty) -> bool: return packetvoid.read_body_Empty(r, v)
def body(r:serialize_read_stream, v:packetvoid.Frame) -> bool: return packetvoid.read_body_Frame(r, v)
def body(r:serialize_read_stream, v:packetvoid.Batch) -> bool: return packetvoid.read_body_Batch(r, v)

def dirty_mixed() -> packetvoid.Mixed:
    let m = packetvoid.NewMixed()
    m.Type = packetvoid.MixedTypePayload
    m.Payload.EntriesCount = 2
    m.Payload.Entries[0].Retries = 9
    m.Payload.Entries[1].Retries = 9
    m.Payload.Marker = 1
    return m

def dirty(v:packetvoid.Mixed) -> packetvoid.Mixed: return dirty_mixed()
def dirty(v:packetvoid.Tags) -> packetvoid.Tags:
    let t = packetvoid.NewTags()
    t.Type = packetvoid.TagsTypePong
    return t
def dirty(v:packetvoid.Single) -> packetvoid.Single:
    let s = packetvoid.NewSingle()
    s.Type = packetvoid.SingleTypeAck
    return s
def dirty(v:packetvoid.Empty) -> packetvoid.Empty: return packetvoid.NewEmpty()
def dirty(v:packetvoid.Frame) -> packetvoid.Frame:
    let f = packetvoid.NewFrame()
    f.Lead = 7
    f.Choice = dirty_mixed()
    f.Tail = 1
    return f
def dirty(v:packetvoid.Batch) -> packetvoid.Batch:
    let b = packetvoid.NewBatch()
    b.ValuesCount = 2
    b.Values[0] = dirty_mixed()
    b.Values[1] = dirty_mixed()
    return b

def same_arm(a:packetvoid.DefaultArm, b:packetvoid.DefaultArm) -> bool:
    if a.EntriesCount != b.EntriesCount or a.Marker != b.Marker or length(a.Entries) != length(b.Entries): return false
    for(a.Entries) e, i:
        if e.Retries != b.Entries[i].Retries: return false
    return true

def same(a:packetvoid.Mixed, b:packetvoid.Mixed) -> bool:
    if a.Type != b.Type: return false
    if a.Type == packetvoid.MixedTypePayload: return same_arm(a.Payload, b.Payload)
    return true
def same(a:packetvoid.Tags, b:packetvoid.Tags) -> bool: return a.Type == b.Type
def same(a:packetvoid.Single, b:packetvoid.Single) -> bool: return a.Type == b.Type
def same(a:packetvoid.Empty, b:packetvoid.Empty) -> bool: return a.Type == b.Type
def same(a:packetvoid.Frame, b:packetvoid.Frame) -> bool:
    return a.Lead == b.Lead and a.Tail == b.Tail and same(a.Choice, b.Choice)
def same(a:packetvoid.Batch, b:packetvoid.Batch) -> bool:
    if a.ValuesCount != b.ValuesCount: return false
    for(a.ValuesCount) i:
        if not same(a.Values[i], b.Values[i]): return false
    return true

def mixed(t:packetvoid.MixedType) -> packetvoid.Mixed:
    let m = packetvoid.NewMixed()
    m.Type = t
    return m

def hex(s:string) -> string:
    if s == "": return "-"
    let digits = "0123456789abcdef"
    var out = ""
    for(s) c:
        out += substring(digits, (c >> 4) & 15, 1) + substring(digits, c & 15, 1)
    return out

// written is the bytes and bit count the generated writer gives value
def written(value) -> string, int, bool:
    let w = serialize_write_stream(ensure_size("", 64, 0))
    let ok = wr(w, value)
    let bits = w.bits_processed()
    w.flush()
    return substring(w.buffer, 0, w.bytes_processed()), bits, ok

def accept(pin:string, want_bits:int, value) -> string:
    let got, bits, wok = written(value)
    if not wok: return "write answered false"
    if got != pin: return "write " + hex(got) + ", pin " + hex(pin)
    if bits != want_bits: return "write has " + string(bits) + " bits, want " + string(want_bits)
    let back = dirty(value)
    let r = serialize_read_stream_padded(pin)
    if not rd(r, back): return "read refused: " + r.refusal_name()
    if not same(back, value): return "read value differs"
    let again, _b, _ok = written(back)
    if again != pin: return "write what was read: " + hex(again) + ", pin " + hex(pin)
    let rb = serialize_read_stream_padded(pin)
    if not body(rb, dirty(value)): return "body read refused: " + rb.refusal_name()
    if rb.bits_processed() != want_bits: return "read has " + string(rb.bits_processed()) + " bits, want " + string(want_bits)
    return ""

// accept_case builds each accept case's value by name (its value column);
// an accept name it does not know is FAILED, never some other case's value
def accept_case(name:string, typ:string, pin:string, bits:int) -> string:
    if typ == "Mixed":
        if name == "mixed-none": return accept(pin, bits, mixed(packetvoid.MixedTypeNone))
        if name == "mixed-ping": return accept(pin, bits, mixed(packetvoid.MixedTypePing))
        // the payload arm is the type's declared defaults: entries empty, marker 5
        if name == "mixed-payload": return accept(pin, bits, mixed(packetvoid.MixedTypePayload))
        return "unknown accept case"
    if typ == "Tags":
        let t = packetvoid.NewTags()
        if name == "tags-none": return accept(pin, bits, t)
        if name == "tags-ping":
            t.Type = packetvoid.TagsTypePing
            return accept(pin, bits, t)
        if name == "tags-pong":
            t.Type = packetvoid.TagsTypePong
            return accept(pin, bits, t)
        return "unknown accept case"
    if typ == "Single":
        let s = packetvoid.NewSingle()
        if name == "single-none": return accept(pin, bits, s)
        if name == "single-ack":
            s.Type = packetvoid.SingleTypeAck
            return accept(pin, bits, s)
        return "unknown accept case"
    if typ == "Empty":
        if name == "empty-none": return accept(pin, bits, packetvoid.NewEmpty())
        return "unknown accept case"
    if typ == "Frame":
        if name == "frame-ping":
            let f = packetvoid.NewFrame()
            f.Lead = 5
            f.Choice = mixed(packetvoid.MixedTypePing)
            f.Tail = 6
            return accept(pin, bits, f)
        return "unknown accept case"
    if typ == "Batch":
        let b = packetvoid.NewBatch()
        if name == "batch-empty": return accept(pin, bits, b)
        if name == "batch-ping":
            b.ValuesCount = 1
            b.Values[0] = mixed(packetvoid.MixedTypePing)
            return accept(pin, bits, b)
        if name == "batch-ping-payload":
            b.ValuesCount = 2
            b.Values[0] = mixed(packetvoid.MixedTypePing)
            b.Values[1] = mixed(packetvoid.MixedTypePayload)
            return accept(pin, bits, b)
        return "unknown accept case"
    return "unknown type " + typ

// refuse_case: the public one-packet read of the pin must refuse
def refuse_case(typ:string, pin:string) -> string:
    let r = serialize_read_stream_padded(pin)
    var accepted = false
    if typ == "Mixed": accepted = packetvoid.ReadMixed(r, dirty_mixed())
    elif typ == "Tags": accepted = packetvoid.ReadTags(r, packetvoid.NewTags())
    elif typ == "Single": accepted = packetvoid.ReadSingle(r, packetvoid.NewSingle())
    elif typ == "Empty": accepted = packetvoid.ReadEmpty(r, packetvoid.NewEmpty())
    elif typ == "Frame": accepted = packetvoid.ReadFrame(r, packetvoid.NewFrame())
    elif typ == "Batch": accepted = packetvoid.ReadBatch(r, packetvoid.NewBatch())
    else: return "unknown type " + typ
    if accepted: return "read accepted"
    return ""

def answer(name:string, typ:string, verdict:string, bits_text:string) -> string:
    let pin = read_file(name + ".bin")
    if not pin: return "the pin " + name + ".bin is missing"
    if verdict == "refuse": return refuse_case(typ, pin)
    if verdict != "accept": return "verdict " + verdict
    let bits, ok = string_to_int(bits_text)
    if not ok: return "bits column " + bits_text
    return accept_case(name, typ, pin, bits)

def main():
    let text = read_file("cases.txt")
    if not text:
        // no case line at all, which casegate refuses
        print("packet-void lobster: cases.txt is missing")
        return
    for(tokenize(text, "\n", "\r")) line:
        let f = []::string
        for(tokenize(line, " ", " \t\r")) w:
            if w != "": push(f, w)
        if length(f) > 0 and f[0][0] != '#':
            if length(f) < 4:
                print("case " + f[0] + " FAILED: a case line needs <name> <type> <verdict> <bits>")
            else:
                let why = answer(f[0], f[1], f[2], f[3])
                print("case " + f[0] + (if why == "": " ok" else: " FAILED: " + why))

main()
