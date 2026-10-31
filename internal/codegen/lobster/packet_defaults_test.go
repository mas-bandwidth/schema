// Packet value defaults on the Lobster type wire (SPEC §4.2, §4.8, §5;
// test/packet-defaults/README.md), the Lobster row of the packet-defaults
// gate. test/packet-defaults/Defaults.schema and its defaultless twin
// Plain.schema are generated side by side and checked against the eight C++
// pins under testdata/wire/packet-defaults: every constructor carries its
// string, bytes and flags defaults (explicit-empty and short literals with
// zero tails, the bit-63 and full 64-bit masks), the twin set to the same
// values writes the same bytes, every pin is written byte and bit exact and
// read back into REUSED storage (a direct read overlays only the received
// prefix; an untaken conditional clears to zero; every union selection
// constructs a fresh payload at its defaults, a repeated tag included), and
// each read ends exactly at the pin's bit count.
//
// A failed check prints "FAILED: <what>"; the negative control drops the
// constructor's byte copies and requires the "packet-default constructor
// bytes" marker.
package lobster

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

var packetDefaultPins = []string{
	"batch-defaults", "choice-sample", "conditional-off", "conditional-on",
	"sample-defaults", "sample-empty", "sample-short", "zero-count",
}

func generatePacketDefaults(t *testing.T) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	for _, name := range []string{"Defaults", "Plain"} {
		src := t.TempDir()
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "packet-defaults", name+".schema"))
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, src, name+".schema", string(data))
		out, err := Generate(unitFromDir(t, src))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		maps.Copy(files, out)
	}
	return files
}

func runPacketDefaults(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	writeAll(t, dir, files)
	inputs := map[string][]byte{}
	for _, pin := range packetDefaultPins {
		for _, ext := range []string{".bin", ".bits"} {
			data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", "packet-defaults", pin+ext))
			if err != nil {
				t.Fatal(err)
			}
			inputs[pin+ext] = data
		}
	}
	writeFile(t, dir, "main.lobster", packetDefaultsProbe)
	// a sabotaged constructor may go on to trip a write contract (a length
	// over zero bytes is a NUL); the marker is printed first either way
	out, _ := lobsterrun.RunExit(t, dir, "main.lobster", inputs)
	return string(out)
}

func TestLobsterPacketDefaults(t *testing.T) {
	t.Parallel()
	out := runPacketDefaults(t, generatePacketDefaults(t))
	if strings.TrimSpace(out) != "packet defaults Lobster: constructors, eight C++ pins and reused storage OK" {
		t.Fatalf("packet defaults:\n%s", out)
	}
}

// TestLobsterPacketDefaultsNegativeControl removes only the constructors'
// byte copies (the schema_rt.put lines lobster.go's ByteDefault emits) and
// requires the generated code to compile and fail the constructor-bytes
// marker.
func TestLobsterPacketDefaultsNegativeControl(t *testing.T) {
	t.Parallel()
	files := generatePacketDefaults(t)
	var kept []string
	removed := 0
	for line := range strings.SplitSeq(string(files["Defaults.lobster"]), "\n") {
		if strings.Contains(line, " = schema_rt.put(value.") {
			removed++
			continue
		}
		kept = append(kept, line)
	}
	if removed == 0 {
		t.Fatal("no constructor byte copy to remove in Defaults.lobster")
	}
	files["Defaults.lobster"] = []byte(strings.Join(kept, "\n"))
	out := runPacketDefaults(t, files)
	if !strings.Contains(out, "FAILED: packet-default constructor bytes\n") {
		t.Fatalf("removing %d constructor byte copies did not fail the constructor-bytes marker:\n%s", removed, out)
	}
}

const packetDefaultsProbe = `import .Defaults
import .Plain
import .serialize
import .schema_runtime

var failures = 0
def ck(ok:bool, what:string):
    if not ok:
        failures += 1
        print("FAILED: " + what)

def hexs(b:string) -> string:
    var s = ""
    for(b) c:
        s += number_to_string(c, 16, 2)
    return lowercase(s)

def fill(n:int, c:int) -> string:
    return ensure_size("", n, c)

def show(s:packetdefaults.Sample) -> string:
    return hexs(s.Name) + "/" + string(s.NameLength) + "/" + hexs(s.Token) + "/" + string(s.TokenLength) + "/" + string(s.Caps) + "/" + hexs(s.EmptyName) + "/" + string(s.EmptyNameLength) + "/" + hexs(s.EmptyToken) + "/" + string(s.EmptyTokenLength) + "/" + string(s.EmptyCaps)

def set(s:packetdefaults.Sample, name:string, nl:int, token:string, tl:int, caps:int, en:string, enl:int, et:string, etl:int, ec:int) -> packetdefaults.Sample:
    write_substring(s.Name, 0, name, false)
    s.NameLength = nl
    write_substring(s.Token, 0, token, false)
    s.TokenLength = tl
    s.Caps = caps
    write_substring(s.EmptyName, 0, en, false)
    s.EmptyNameLength = enl
    write_substring(s.EmptyToken, 0, et, false)
    s.EmptyTokenLength = etl
    s.EmptyCaps = ec
    return s

def zero() -> packetdefaults.Sample:
    return set(packetdefaults.NewSample(), fill(6, 0), 0, fill(4, 0), 0, 0, fill(3, 0), 0, fill(2, 0), 0, 0)
def sample() -> packetdefaults.Sample:
    return set(zero(), "\xc3\xa9\xf0\x90\x80\x80", 6, "\x5cn\x5ct", 4, 5, fill(3, 0), 0, fill(2, 0), 0, 0)
def short() -> packetdefaults.Sample:
    return set(zero(), "A\x00\x00\x00\x00\x00", 1, "\x00\xff\x00\x00", 2, 2, fill(3, 0), 0, fill(2, 0), 0, 0)
def dirty() -> packetdefaults.Sample:
    return set(zero(), fill(6, 100), 6, fill(4, 161), 4, 7, fill(3, 111), 3, fill(2, 177), 2, 7)
// a direct read overlays the received prefix and keeps the unused tails
def overlay(into:packetdefaults.Sample, sent:packetdefaults.Sample) -> packetdefaults.Sample:
    write_substring(into.Name, 0, substring(sent.Name, 0, sent.NameLength), false)
    into.NameLength = sent.NameLength
    write_substring(into.Token, 0, substring(sent.Token, 0, sent.TokenLength), false)
    into.TokenLength = sent.TokenLength
    into.Caps = sent.Caps
    into.EmptyNameLength = 0
    into.EmptyTokenLength = 0
    into.EmptyCaps = 0
    return into

def show_batch(b:packetdefaults.Batch) -> string:
    var s = show(b.Head)
    for(b.Items) x: s += "|" + show(x)
    for(b.Counted) x: s += "|" + show(x)
    return s + "|" + string(b.CountedCount)
def show_zc(z:packetdefaults.ZeroCount) -> string:
    var s = string(z.ItemsCount)
    for(z.Items) x: s += "|" + show(x)
    return s
def show_cond(c:packetdefaults.Conditional) -> string:
    return string(c.Enabled) + "|" + show(c.Value)
def show_choice(c:packetdefaults.Choice) -> string:
    if c.Type == packetdefaults.ChoiceTypeSample: return "sample|" + show(c.Sample)
    if c.Type == packetdefaults.ChoiceTypeConditional: return "conditional|" + show_cond(c.Conditional)
    return "none"

def wire(write) -> string, int:
    let w = serialize_write_stream(ensure_size("", 4096, 0))
    write(w)
    let bits = w.bits_processed()
    w.flush()
    return substring(w.buffer, 0, w.bytes_processed()), bits

def pinned(name:string) -> string, int:
    let data = read_file(name + ".bin")
    let text = read_file(name + ".bits")
    if not data or not text:
        return "", -1
    let n, _ = string_to_int(substring(text, 0, find_string(text, "\n")))
    return data, n

// a pin: the write is the C++ bytes and bits; a read into reused storage
// consumes exactly those bits and ends there
def pin(name:string, write, read):
    let want, wbits = pinned(name)
    let got, bits = wire(write)
    ck(got == want and bits == wbits, "C++ pin " + name + " write")
    let r = serialize_read_stream_padded(want)
    ck(read(r), "C++ pin " + name + " read")
    ck(r.bits_processed() == wbits, "C++ pin " + name + " read consumed bits")
    ck(r.serialize_end(), "C++ pin " + name + " read end")

// ---- constructors
ck(show(packetdefaults.NewSample()) == show(sample()), "packet-default constructor bytes")
let empty = packetdefaults.NewEmptyOnly()
ck(empty.NameLength == 0 and empty.TokenLength == 0 and empty.Caps == 0 and hexs(empty.Name) == "0000" and hexs(empty.Token) == "00", "explicit empty defaults")
let prefix = packetdefaults.NewPrefix()
ck(hexs(prefix.Name) == "c3a9000000" and prefix.NameLength == 2 and hexs(prefix.Token) == "5c6e000000" and prefix.TokenLength == 2, "short literal defaults keep zero tails")
let wide = packetdefaults.NewWideMask()
ck(wide.High == 0x8000000000000000 and wide.All == -1, "bit-63 and full 64-bit masks")
let wb, wbits = wire() w: packetdefaults.WriteWideMask(w, wide)
ck(hexs(wb) == "0000000000000080ffffffffffffffff" and wbits == 128, "WideMask wire")
let split = packetdefaults.NewSplitMask()
split.Lead = 5
split.Mask = 1 << 32
split.Tail = 2
let sb, sbits = wire() w: packetdefaults.WriteSplitMask(w, split)
ck(hexs(sb) == "0500000028" and sbits == 38, "SplitMask wire")
let wideBack = packetdefaults.NewWideMask()
wideBack.High = 0
wideBack.All = 0
ck(packetdefaults.ReadWideMask(serialize_read_stream_padded(wb), wideBack) and wideBack.High == wide.High and wideBack.All == -1, "WideMask read")
let splitBack = packetdefaults.NewSplitMask()
ck(packetdefaults.ReadSplitMask(serialize_read_stream_padded(sb), splitBack) and splitBack.Lead == 5 and splitBack.Mask == 1 << 32 and splitBack.Tail == 2, "SplitMask read")

// ---- the defaultless twin writes the same bytes
let plain = packetplain.NewSample()
write_substring(plain.Name, 0, "\xc3\xa9\xf0\x90\x80\x80", false)
plain.NameLength = 6
write_substring(plain.Token, 0, "\x5cn\x5ct", false)
plain.TokenLength = 4
plain.Caps = 5
let pb, pbits = wire() w: packetplain.WriteSample(w, plain)
let db, dbits = wire() w: packetdefaults.WriteSample(w, packetdefaults.NewSample())
ck(pb == db and pbits == dbits, "defaultless twin")

// ---- the eight pins
let s0 = zero()
pin("sample-defaults", fn(w): packetdefaults.WriteSample(w, packetdefaults.NewSample())) r: packetdefaults.ReadSample(r, s0)
ck(show(s0) == show(sample()), "sample-defaults values")

let batch = packetdefaults.NewBatch()
ck(batch.CountedCount == 1 and show(batch.Head) == show(sample()), "Batch born")
for(batch.Items) x: ck(show(x) == show(sample()), "Batch items born at defaults")
for(batch.Counted) x: ck(show(x) == show(sample()), "Batch counted born at defaults")
def reused_batch() -> packetdefaults.Batch:
    let b = packetdefaults.NewBatch()
    b.Counted[1] = dirty()
    b.Counted[2] = short()
    return b
let b0 = reused_batch()
pin("batch-defaults", fn(w): packetdefaults.WriteBatch(w, batch)) r: packetdefaults.ReadBatch(r, b0)
ck(show_batch(b0) == show_batch(reused_batch()), "batch-defaults values and backing")

let zc = packetdefaults.NewZeroCount()
ck(zc.ItemsCount == 0 and show(zc.Items[0]) == show(sample()) and show(zc.Items[1]) == show(sample()), "ZeroCount born")
let z0 = packetdefaults.NewZeroCount()
z0.Items[0] = dirty()
z0.Items[1] = short()
z0.ItemsCount = 2
pin("zero-count", fn(w): packetdefaults.WriteZeroCount(w, zc)) r: packetdefaults.ReadZeroCount(r, z0)
ck(z0.ItemsCount == 0 and show(z0.Items[0]) == show(dirty()) and show(z0.Items[1]) == show(short()), "zero-count keeps backing")

ck(packetdefaults.NewConditional().Enabled and show(packetdefaults.NewConditional().Value) == show(sample()), "Conditional born")
let c0 = packetdefaults.NewConditional()
c0.Enabled = false
c0.Value = zero()
pin("conditional-on", fn(w): packetdefaults.WriteConditional(w, packetdefaults.NewConditional())) r: packetdefaults.ReadConditional(r, c0)
ck(show_cond(c0) == show_cond(packetdefaults.NewConditional()), "conditional-on values")
let off = packetdefaults.NewConditional()
off.Enabled = false
let c1 = packetdefaults.NewConditional()
c1.Value = dirty()
pin("conditional-off", fn(w): packetdefaults.WriteConditional(w, off)) r: packetdefaults.ReadConditional(r, c1)
ck(not c1.Enabled and show(c1.Value) == show(zero()), "conditional-off clears the untaken side to zero")

let choice = packetdefaults.NewChoice()
choice.Type = packetdefaults.ChoiceTypeSample
let ch0 = packetdefaults.NewChoice()
ch0.Type = packetdefaults.ChoiceTypeConditional
pin("choice-sample", fn(w): packetdefaults.WriteChoice(w, choice)) r: packetdefaults.ReadChoice(r, ch0)
ck(show_choice(ch0) == "sample|" + show(sample()), "choice-sample values")

let sh0 = dirty()
pin("sample-short", fn(w): packetdefaults.WriteSample(w, short())) r: packetdefaults.ReadSample(r, sh0)
ck(show(sh0) == show(overlay(dirty(), short())), "sample-short overlays the prefix")
let se0 = dirty()
pin("sample-empty", fn(w): packetdefaults.WriteSample(w, zero())) r: packetdefaults.ReadSample(r, se0)
ck(show(se0) == show(overlay(dirty(), zero())), "sample-empty overlays the prefix")

// ---- every selection is a fresh payload, a repeated tag included
for([short(), zero()]) sent:
    let value = packetdefaults.NewChoice()
    value.Type = packetdefaults.ChoiceTypeSample
    value.Sample = sent
    let cw, cbits = wire() w: packetdefaults.WriteChoice(w, value)
    let output = packetdefaults.NewChoice()
    output.Type = packetdefaults.ChoiceTypeConditional
    for(2):
        output.Sample = dirty()
        let r = serialize_read_stream_padded(cw)
        ck(packetdefaults.ReadChoice(r, output) and r.bits_processed() == cbits, "repeated union read")
        ck(show_choice(output) == "sample|" + show(overlay(sample(), sent)), "repeated union selection is fresh")
let offChoice = packetdefaults.NewChoice()
offChoice.Type = packetdefaults.ChoiceTypeConditional
offChoice.Conditional.Enabled = false
let ow, _ = wire() w: packetdefaults.WriteChoice(w, offChoice)
let outC = packetdefaults.NewChoice()
outC.Type = packetdefaults.ChoiceTypeSample
for(2):
    outC.Conditional.Enabled = true
    outC.Conditional.Value = dirty()
    ck(packetdefaults.ReadChoice(serialize_read_stream_padded(ow), outC), "conditional arm read")
    ck(show_choice(outC) == "conditional|false|" + show(zero()), "conditional arm fresh, untaken side zero")

if failures == 0:
    print("packet defaults Lobster: constructors, eight C++ pins and reused storage OK")
`
