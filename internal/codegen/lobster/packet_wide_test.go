package lobster

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// The C++ fixtures exercise both halves of raw/ranged 128-bit values,
// signed wide fixed point, defaults and zeroing an untaken wide field.
func TestLobsterPacketWideGoldens(t *testing.T) {
	t.Parallel()
	runPacketWideGoldens(t, false)
}

func TestLobsterPacketWideGoldensNegativeControl(t *testing.T) {
	t.Parallel()
	runPacketWideGoldens(t, true)
}

func runPacketWideGoldens(t *testing.T, sabotage bool) {
	t.Helper()
	files := generateUfixed(t)
	if sabotage {
		const anchor = "serialize_uint128 { lo: v.EntityId.lo, hi: v.EntityId.hi }"
		src := string(files["Ludicrous.lobster"])
		if strings.Count(src, anchor) != 1 {
			t.Fatal("wide writer lane anchor must occur once")
		}
		files["Ludicrous.lobster"] = []byte(strings.Replace(src, anchor, "serialize_uint128 { lo: v.EntityId.hi, hi: v.EntityId.lo }", 1))
	}
	dir := t.TempDir()
	writeAll(t, dir, files)
	inputs := map[string][]byte{}
	for _, name := range []string{"ludicrous_state", "ludicrous_state_untargeted", "degenerate_probe"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		inputs[name] = b
	}
	writeFile(t, dir, "main.lobster", packetWideProbe)
	stdout, code := lobsterrun.RunExit(t, dir, "main.lobster", inputs)
	out := strings.TrimSpace(string(stdout))
	if sabotage {
		// The same probe must reject the mutated writer at the wire comparison.
		// Compilation failures and earlier decoded-value failures do not count.
		if code != 70 || out != "wide golden mismatch" {
			t.Fatalf("mutated writer: exit %d, output %q; want wire comparison refusal", code, out)
		}
		return
	}
	if code != 0 || out != "wide goldens passed" {
		t.Fatalf("exit %d: %s", code, out)
	}
}

const packetWideProbe = `import .Ludicrous
import .serialize_stream

def check_state(v:ludicrous.LudicrousState, targeted:bool):
    assert v.Mode == ludicrous.DriveModeLudicrous
    assert v.Probe.Angle == 2981888 and v.Probe.Position == -809119744
    assert v.Probe.Reach == schema_rt.Int128 { lo: 65535999999, hi: 0 }
    assert v.Probe.Ticks == 777777 and v.Probe.Samples[0] == -524288 and v.Probe.Samples[1] == 524288
    assert v.Wide.EntityId == schema_rt.Int128 { lo: 0xfedcba9876543210, hi: 0x0123456789abcdef }
    assert v.Wide.Energy == schema_rt.Int128 { lo: 4999999999, hi: 0 }
    assert v.Wide.Flux == schema_rt.Int128 { lo: 7, hi: 0x800000000 }
    assert v.Wide.Bias == schema_rt.Int128 { lo: -250, hi: -1 }
    assert v.Wide.Seed == schema_rt.Int128 { lo: 0, hi: 2 }
    assert v.KeysCount == 2
    assert v.Keys[0] == schema_rt.Int128 { lo: 1, hi: 0 }
    assert v.Keys[1] == schema_rt.Int128 { lo: 0, hi: 0x8000000000000000 }
    assert v.HasTarget == targeted
    assert v.TargetId == schema_rt.Int128 { lo: (if targeted: 42 else: 0), hi: 0 }

def state_case(name:string, targeted:bool):
    let golden = read_file(name)
    assert golden
    let v = ludicrous.NewLudicrousState()
    v.TargetId = schema_rt.Int128 { lo: 0xdead, hi: 0xbeef }
    let r = serialize_read_stream_padded(golden)
    assert ludicrous.ReadLudicrousState(r, v) and r.serialize_end()
    check_state(v, targeted)
    let w = serialize_write_stream(schema_rt.zeros(1024))
    assert ludicrous.WriteLudicrousState(w, v)
    w.flush()
    if substring(w.buffer, 0, w.bytes_processed()) != golden:
        serialize_abort("wide golden mismatch")

state_case("ludicrous_state", true)
state_case("ludicrous_state_untargeted", false)
let degenerate = read_file("degenerate_probe")
assert degenerate
let d = ludicrous.NewDegenerateProbe()
let dr = serialize_read_stream_padded(degenerate)
assert ludicrous.ReadDegenerateProbe(dr, d) and dr.serialize_end()
assert d.LockedFixed == -196608 and d.LockedInt == 7 and d.Tail == 0xa5
assert d.LockedWide == schema_rt.Int128 { lo: -12345678901234, hi: -1 }
let dw = serialize_write_stream(schema_rt.zeros(16))
assert ludicrous.WriteDegenerateProbe(dw, d)
dw.flush()
assert dw.bytes_processed() == 1 and substring(dw.buffer, 0, 1) == degenerate
// Oversized offsets must refuse; use a fresh fixture for each mutation.
for([[2, 25], [286, 34]]) span:
    let bad = read_file("ludicrous_state")
    assert bad
    for(span[1]) j:
        let bit = span[0] + j
        write_int8_le(bad, bit / 8, bad[bit / 8] | (1 << (bit % 8)))
    let r = serialize_read_stream_padded(bad)
    assert not ludicrous.ReadLudicrousState(r, ludicrous.NewLudicrousState())
let full = read_file("ludicrous_state")
assert full
let short = serialize_read_stream_padded(substring(full, 0, 4))
assert not ludicrous.ReadLudicrousState(short, ludicrous.NewLudicrousState())
print("wide goldens passed")
`
