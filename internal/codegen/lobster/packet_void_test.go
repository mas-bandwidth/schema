// Payload-free packet union arms carry exactly their tag bits (SPEC §4.8,
// #503), the Lobster row of make/checks/packet-void.mk. The reader inputs are
// test/packet-void/README.md's literal constants, calculated from the spec and
// never produced by the writer under test; the writer must match their bytes
// and exact bit counts independently. The probe also checks fresh payload
// defaults on repeated selection, invalid tags and a missing tag. Two controls
// add one bit to the Mixed union's void arm on just the write or just the read
// path and require exactly that direction's void rows to go red.
package lobster

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// voidCases are the README's table: the probe writes and reads each.
var voidCases = []string{"mixed-none", "mixed-ping", "mixed-payload", "frame", "batch", "tags-pong", "single-ack", "empty-none"}

func voidRows() map[string]string {
	rows := map[string]string{
		"refuse mixed-tag": "value_out_of_range",
		"refuse tags-tag":  "value_out_of_range",
		"refuse missing":   "past_end",
		"fresh payload":    "ok",
	}
	for _, c := range voidCases {
		rows["write "+c] = "ok"
		rows["read "+c] = "ok"
	}
	return rows
}

func runVoid(t *testing.T, patch func(map[string][]byte)) map[string]string {
	t.Helper()
	src := t.TempDir()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "packet-void", "Void.schema"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, src, "Void.schema", string(data))
	files, err := Generate(unitFromDir(t, src))
	if err != nil {
		t.Fatal(err)
	}
	if patch != nil {
		patch(files)
	}
	dir := t.TempDir()
	writeAll(t, dir, files)
	writeFile(t, dir, "main.lobster", packetVoidProbe)
	out := string(lobsterrun.Run(t, dir, "main.lobster", nil))
	rows := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		i := strings.LastIndexByte(line, ' ')
		if i < 0 {
			t.Fatalf("void probe printed %q:\n%s", line, out)
		}
		rows[line[:i]] = line[i+1:]
	}
	return rows
}

func voidRed(got map[string]string) []string {
	want := voidRows()
	var red []string
	for _, row := range slices.Sorted(maps.Keys(want)) {
		if got[row] != want[row] {
			red = append(red, row)
		}
	}
	for row := range got {
		if _, ok := want[row]; !ok {
			red = append(red, row)
		}
	}
	slices.Sort(red)
	return red
}

func TestLobsterPacketVoidArms(t *testing.T) {
	t.Parallel()
	if red := voidRed(runVoid(t, nil)); len(red) > 0 {
		t.Fatalf("packet-void rows red: %v", red)
	}
}

func TestLobsterPacketVoidArmsNegativeControls(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ dir, anchor, with string }{
		{"write", "    s.serialize_bits(v.Type, 2) // the union tag (SPEC §4.8)\n    if v.Type == MixedTypePayload:",
			"    s.serialize_bits(v.Type, 2) // the union tag (SPEC §4.8)\n    if v.Type == MixedTypePing: s.serialize_bits(0, 1)\n    if v.Type == MixedTypePayload:"},
		{"read", "    value.Type = MixedType(rv)\n",
			"    value.Type = MixedType(rv)\n    if value.Type == MixedTypePing:\n        rv, rok = stream.serialize_bits(rv, 1)\n"},
	} {
		t.Run(tc.dir, func(t *testing.T) {
			t.Parallel()
			got := runVoid(t, func(files map[string][]byte) {
				text := string(files["Void.lobster"])
				if strings.Count(text, tc.anchor) != 1 {
					t.Fatalf("the %s anchor matched %d times", tc.dir, strings.Count(text, tc.anchor))
				}
				files["Void.lobster"] = []byte(strings.Replace(text, tc.anchor, tc.with, 1))
			})
			want := []string{tc.dir + " batch", tc.dir + " frame", tc.dir + " mixed-ping"}
			if red := voidRed(got); !slices.Equal(red, want) {
				t.Fatalf("an extra void %s bit turned %v red, want exactly %v", tc.dir, red, want)
			}
		})
	}
}

const packetVoidProbe = `import .Void
import .serialize

def wire(write) -> string, int:
    let w = serialize_write_stream(ensure_size("", 64, 0))
    write(w)
    let bits = w.bits_processed()
    w.flush()
    return substring(w.buffer, 0, w.bytes_processed()), bits

def row(name:string, ok:bool):
    print(name + (if ok: " ok" else: " BAD"))

// the writer must give the README's bytes and bits
def check_write(name:string, want:string, wbits:int, write):
    let got, bits = wire(write)
    row("write " + name, got == want and bits == wbits)

// the reader consumes exactly the README's bits from its constant
def check_read(name:string, data:string, wbits:int, read):
    let r = serialize_read_stream_padded(data)
    let ok = read(r)
    row("read " + name, ok and r.bits_processed() == wbits and r.serialize_end())

let none = packetvoid.NewMixed()
check_write("mixed-none", "\x00", 2) w: packetvoid.WriteMixed(w, none)
let ping = packetvoid.NewMixed()
ping.Type = packetvoid.MixedTypePing
check_write("mixed-ping", "\x01", 2) w: packetvoid.WriteMixed(w, ping)
let pay = packetvoid.NewMixed()
pay.Type = packetvoid.MixedTypePayload
pay.Payload.EntriesCount = 0
check_write("mixed-payload", "\x52", 7) w: packetvoid.WriteMixed(w, pay)
let frame = packetvoid.NewFrame()
frame.Lead = 5
frame.Choice.Type = packetvoid.MixedTypePing
frame.Tail = 6
check_write("frame", "\xcd", 8) w: packetvoid.WriteFrame(w, frame)
let batch = packetvoid.NewBatch()
batch.ValuesCount = 1
batch.Values[0].Type = packetvoid.MixedTypePing
check_write("batch", "\x05", 4) w: packetvoid.WriteBatch(w, batch)
let tags = packetvoid.NewTags()
tags.Type = packetvoid.TagsTypePong
check_write("tags-pong", "\x02", 2) w: packetvoid.WriteTags(w, tags)
let single = packetvoid.NewSingle()
single.Type = packetvoid.SingleTypeAck
check_write("single-ack", "\x01", 1) w: packetvoid.WriteSingle(w, single)
check_write("empty-none", "", 0) w: packetvoid.WriteEmpty(w, packetvoid.NewEmpty())

let m = packetvoid.NewMixed()
m.Type = packetvoid.MixedTypePayload
check_read("mixed-none", "\x00", 2) r: packetvoid.ReadMixed(r, m) and m.Type == packetvoid.MixedTypeNone
check_read("mixed-ping", "\x01", 2) r: packetvoid.ReadMixed(r, m) and m.Type == packetvoid.MixedTypePing
check_read("mixed-payload", "\x52", 7) r: packetvoid.ReadMixed(r, m) and m.Type == packetvoid.MixedTypePayload and m.Payload.EntriesCount == 0 and m.Payload.Marker == 5
let f = packetvoid.NewFrame()
check_read("frame", "\xcd", 8) r: packetvoid.ReadFrame(r, f) and f.Lead == 5 and f.Choice.Type == packetvoid.MixedTypePing and f.Tail == 6
let b = packetvoid.NewBatch()
check_read("batch", "\x05", 4) r: packetvoid.ReadBatch(r, b) and b.ValuesCount == 1 and b.Values[0].Type == packetvoid.MixedTypePing
let tg = packetvoid.NewTags()
check_read("tags-pong", "\x02", 2) r: packetvoid.ReadTags(r, tg) and tg.Type == packetvoid.TagsTypePong
let sg = packetvoid.NewSingle()
check_read("single-ack", "\x01", 1) r: packetvoid.ReadSingle(r, sg) and sg.Type == packetvoid.SingleTypeAck
let em = packetvoid.NewEmpty()
check_read("empty-none", "", 0) r: packetvoid.ReadEmpty(r, em) and em.Type == packetvoid.EmptyTypeNone

// every selection constructs a fresh payload at its defaults
var fresh = true
for(2):
    m.Payload.EntriesCount = 2
    m.Payload.Entries[0].Retries = 9
    m.Payload.Entries[1].Retries = 9
    m.Payload.Marker = 1
    let r = serialize_read_stream_padded("\x52")
    if not packetvoid.ReadMixed(r, m): fresh = false
    if m.Payload.EntriesCount != 0 or m.Payload.Entries[0].Retries != -1 or m.Payload.Entries[1].Retries != -1 or m.Payload.Marker != 5: fresh = false
row("fresh payload", fresh)

def refused(name:string, data:string, read):
    let r = serialize_read_stream_padded(data)
    if read(r):
        print("refuse " + name + " none")
    else:
        print("refuse " + name + " " + r.refusal_name())
refused("mixed-tag", "\x03") r: packetvoid.ReadMixed(r, packetvoid.NewMixed())
refused("tags-tag", "\x03") r: packetvoid.ReadTags(r, packetvoid.NewTags())
refused("missing", "") r: packetvoid.ReadMixed(r, packetvoid.NewMixed())
`
