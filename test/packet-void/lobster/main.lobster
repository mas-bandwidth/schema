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
// refuse case's pin must be refused by the public one-packet read. A select
// case reads its from= pin, selects the arm on the value it read, and writes
// the pin.
//
// A refuse-write case: the Lobster writer refuses by trapping (a failed
// assert of the schema's write contract, or the runtime's serialize_abort for
// a value its bit width cannot carry), and a trap ends the program. So the Go
// test runs this driver once per refuse-write case as `-- write <case>`,
// which builds the type's default value with the one named change and calls
// the writer, and hands each run's exit code and stdout back in as
// <case>.trap. The full run then requires that the write was reached and
// trapped with the writer's own refusal for that case. The pinned default
// JIT keeps asserts on, and that is the build this test runs.
import .Void
import .serialize

// one overload per type: write, the public one-packet read, the body read,
// dirtied storage, and field equality (unselected arms are not compared;
// a selected payload is compared with its whole backing storage)
def wr(w:serialize_write_stream, v:packetvoid.Mixed) -> bool: return packetvoid.WriteMixed(w, v)
def wr(w:serialize_write_stream, v:packetvoid.Late) -> bool: return packetvoid.WriteLate(w, v)
def wr(w:serialize_write_stream, v:packetvoid.Tags) -> bool: return packetvoid.WriteTags(w, v)
def wr(w:serialize_write_stream, v:packetvoid.Single) -> bool: return packetvoid.WriteSingle(w, v)
def wr(w:serialize_write_stream, v:packetvoid.Empty) -> bool: return packetvoid.WriteEmpty(w, v)
def wr(w:serialize_write_stream, v:packetvoid.Frame) -> bool: return packetvoid.WriteFrame(w, v)
def wr(w:serialize_write_stream, v:packetvoid.Batch) -> bool: return packetvoid.WriteBatch(w, v)

def rd(r:serialize_read_stream, v:packetvoid.Mixed) -> bool: return packetvoid.ReadMixed(r, v)
def rd(r:serialize_read_stream, v:packetvoid.Late) -> bool: return packetvoid.ReadLate(r, v)
def rd(r:serialize_read_stream, v:packetvoid.Tags) -> bool: return packetvoid.ReadTags(r, v)
def rd(r:serialize_read_stream, v:packetvoid.Single) -> bool: return packetvoid.ReadSingle(r, v)
def rd(r:serialize_read_stream, v:packetvoid.Empty) -> bool: return packetvoid.ReadEmpty(r, v)
def rd(r:serialize_read_stream, v:packetvoid.Frame) -> bool: return packetvoid.ReadFrame(r, v)
def rd(r:serialize_read_stream, v:packetvoid.Batch) -> bool: return packetvoid.ReadBatch(r, v)

def body(r:serialize_read_stream, v:packetvoid.Mixed) -> bool: return packetvoid.read_body_Mixed(r, v)
def body(r:serialize_read_stream, v:packetvoid.Late) -> bool: return packetvoid.read_body_Late(r, v)
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
def dirty(v:packetvoid.Late) -> packetvoid.Late:
    let l = packetvoid.NewLate()
    l.Type = packetvoid.LateTypePayload
    l.Payload.EntriesCount = 2
    l.Payload.Entries[0].Retries = 9
    l.Payload.Entries[1].Retries = 9
    l.Payload.Marker = 1
    return l
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
def same(a:packetvoid.Late, b:packetvoid.Late) -> bool:
    if a.Type != b.Type: return false
    if a.Type == packetvoid.LateTypePayload: return same_arm(a.Payload, b.Payload)
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
    if typ == "Late":
        let l = packetvoid.NewLate()
        // the payload arm is the type's declared defaults: entries empty, marker 5
        if name == "late-payload":
            l.Type = packetvoid.LateTypePayload
            return accept(pin, bits, l)
        if name == "late-pong":
            l.Type = packetvoid.LateTypePong
            return accept(pin, bits, l)
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
    elif typ == "Late": accepted = packetvoid.ReadLate(r, dirty(packetvoid.NewLate()))
    elif typ == "Tags": accepted = packetvoid.ReadTags(r, packetvoid.NewTags())
    elif typ == "Single": accepted = packetvoid.ReadSingle(r, packetvoid.NewSingle())
    elif typ == "Empty": accepted = packetvoid.ReadEmpty(r, packetvoid.NewEmpty())
    elif typ == "Frame": accepted = packetvoid.ReadFrame(r, packetvoid.NewFrame())
    elif typ == "Batch": accepted = packetvoid.ReadBatch(r, packetvoid.NewBatch())
    else: return "unknown type " + typ
    if accepted: return "read accepted"
    return ""

// select_case reads mixed-ping's pin with the public one-packet read, then
// selects the payload arm on that value the Lobster API's way: the union keeps
// its payload object, so selecting is setting the tag alone. The object must
// still carry its declared defaults after the void arm was read.
def select_case(name:string, pin:string, want_bits:int) -> string:
    if name != "mixed-ping-select-payload": return "unknown select case"
    let ping_pin = read_file("mixed-ping.bin")
    if not ping_pin: return "the pin mixed-ping.bin is missing"
    let m = packetvoid.NewMixed()
    let r = serialize_read_stream_padded(ping_pin)
    if not packetvoid.ReadMixed(r, m): return "read mixed-ping refused: " + r.refusal_name()
    if m.Type != packetvoid.MixedTypePing: return "read mixed-ping: tag " + string(m.Type)
    m.Type = packetvoid.MixedTypePayload
    let got, bits, wok = written(m)
    if not wok: return "write answered false"
    if got != pin: return "write " + hex(got) + ", pin " + hex(pin)
    if bits != want_bits: return "write has " + string(bits) + " bits, want " + string(want_bits)
    return ""

// not_representable names the refuse-write cases Lobster's types cannot
// hold, refused by its compiler: a tag is a typed enum field, so the text
// "ping" is "requires type `packetvoid.MixedType`, got `string`", and the
// payload a packetvoid.DefaultArm field, so the integer 1 is "requires type
// `packetvoid.DefaultArm`, got `int`". A tag of -1 or 3 is an enum cast,
// packetvoid.MixedType(-1), and is tested.
def not_representable(name:string) -> bool:
    return name == "mixed-write-tag-text" or name == "mixed-write-payload-type"

// write_trapped is the trap run's write: it says the write is reached, calls
// the writer, and says what the writer answered if it did not trap
def write_trapped(name:string, value):
    let w = serialize_write_stream(ensure_size("", 64, 0))
    print("writing " + name)
    let ok = wr(w, value)
    w.flush()
    print("write answered " + (if ok: "true" else: "false") + ": " + hex(substring(w.buffer, 0, w.bytes_processed())))

// trap_write builds each refuse-write case's value by name: the type's
// default value with the one change its value column names; a name it does
// not know writes nothing, and the full run answers it FAILED
def trap_write(name:string):
    if name == "mixed-write-tag-negative":
        let m = packetvoid.NewMixed()
        m.Type = packetvoid.MixedType(-1)
        write_trapped(name, m)
    elif name == "mixed-write-tag-3":
        let m = packetvoid.NewMixed()
        m.Type = packetvoid.MixedType(3)
        write_trapped(name, m)
    elif name == "mixed-write-marker-8":
        let m = packetvoid.NewMixed()
        m.Type = packetvoid.MixedTypePayload
        m.Payload.Marker = 8
        write_trapped(name, m)
    elif name == "tags-write-tag-3":
        let t = packetvoid.NewTags()
        t.Type = packetvoid.TagsType(3)
        write_trapped(name, t)
    elif name == "empty-write-tag-1":
        let e = packetvoid.NewEmpty()
        e.Type = packetvoid.EmptyType(1)
        write_trapped(name, e)
    else:
        print("unknown refuse-write case " + name)

// refusal is the trap each refuse-write case's writer must end the program
// with: its exit code and the text of the line it prints. A tag is the
// schema's write contract, an assert in the generated writer; a marker wider
// than bits(3) is the runtime's serialize_abort, which traps in every build.
def refusal(name:string) -> int, string:
    if name == "mixed-write-tag-negative" or name == "mixed-write-tag-3" or name == "tags-write-tag-3":
        return 1, "): assertion failed: v·.Type >= 0 and v·.Type <= 2["
    if name == "empty-write-tag-1": return 1, "): assertion failed: v·.Type >= 0 and v·.Type <= 0["
    if name == "mixed-write-marker-8": return 70, "serialize: value must fit in bits"
    return 0, ""

// refuse_write_case judges the case's trap run (<case>.trap: "exit <code>",
// then its stdout): the write was reached, and the writer trapped with its own
// refusal for this case, the only line after it
def refuse_write_case(name:string) -> string:
    let code, want = refusal(name)
    if want == "": return "unknown refuse-write case"
    let trap = read_file(name + ".trap")
    if not trap: return "no trap run: " + name + ".trap is missing"
    let lines = []::string
    var got = ""
    for(tokenize(trap, "\n", "\r")) l:
        if l != "":
            push(lines, l)
            got += (if got == "": "" else: " | ") + l
    if length(lines) < 2 or lines[1] != "writing " + name: return "the trap run did not reach the write: " + got
    for(lines) l:
        if find_string(l, "write answered ") == 0: return "write accepted: " + got
    if lines[0] != "exit " + string(code): return "the trap run ended " + lines[0] + ", want exit " + string(code) + ": " + got
    if length(lines) != 3: return "not exactly one refusal line after the write: " + got
    if code == 1 and find_string(lines[2], "VM error (Void.lobster:") != 0: return "not the generated writer assert: " + got
    if find_string(lines[2], want) < 0: return "not the writer refusal (" + want + "): " + got
    return ""

def answer(name:string, typ:string, verdict:string, bits_text:string) -> string:
    if not_representable(name):
        if verdict != "refuse-write": return "named not representable in Lobster, and its verdict is " + verdict
        return ""
    if verdict == "refuse-write": return refuse_write_case(name)
    let pin = read_file(name + ".bin")
    if not pin: return "the pin " + name + ".bin is missing"
    if verdict == "refuse": return refuse_case(typ, pin)
    if verdict != "accept" and verdict != "select": return "verdict " + verdict
    let bits, ok = string_to_int(bits_text)
    if not ok: return "bits column " + bits_text
    if verdict == "select": return select_case(name, pin, bits)
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

// `-- write <case>` is one refuse-write case's trap run; no argument is the
// full run over cases.txt
let args = command_line_arguments()
if length(args) == 2 and args[0] == "write":
    trap_write(args[1])
elif length(args) == 0:
    main()
else:
    print("usage: main.lobster [-- write <case>]")
    set_exit_code(2)
