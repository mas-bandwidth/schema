// Selecting a packet union arm constructs its payload, including the defaults
// in unused backing capacity (SPEC §4.8; examples/ArmDefaults.schema), the
// Lobster row of make/checks/packet-arm-defaults.mk. The probe reads the
// independent 7-bit vector 0x51 (tag 1 = first, count 0, marker 5) twice into
// a DefaultChoice, a DefaultChoicePacket and a DefaultBulkChoice, poisoning
// the selected arm's backing entries before each read; both unused entries
// must come back at ProbeConfig's defaults (retries -1, preferred Railgun).
// The control removes the fresh construction and must fail the assertion.
package lobster

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

const armDefaultsFailure = "FAILED: selected unused backing entry receives its defaults"

func runArmDefaults(t *testing.T, sabotage bool) string {
	t.Helper()
	files, err := Generate(unitFromDir(t, filepath.Join("..", "..", "..", "examples")))
	if err != nil {
		t.Fatal(err)
	}
	if sabotage {
		text := string(files["ArmDefaults.lobster"])
		var kept []string
		removed := 0
		for line := range strings.SplitSeq(text, "\n") {
			if strings.Contains(line, "// a fresh payload on every selection (SPEC §4.8)") {
				removed++
				continue
			}
			kept = append(kept, line)
		}
		if removed == 0 {
			t.Fatal("no fresh arm construction to remove in ArmDefaults.lobster")
		}
		files["ArmDefaults.lobster"] = []byte(strings.Join(kept, "\n"))
	}
	dir := t.TempDir()
	writeAll(t, dir, files)
	writeFile(t, dir, "main.lobster", armDefaultsProbe)
	return string(lobsterrun.Run(t, dir, "main.lobster", nil))
}

func TestLobsterPacketArmDefaults(t *testing.T) {
	t.Parallel()
	if out := runArmDefaults(t, false); strings.TrimSpace(out) != "packet arm defaults Lobster OK" {
		t.Fatalf("packet arm defaults:\n%s", out)
	}
}

func TestLobsterPacketArmDefaultsNegativeControl(t *testing.T) {
	t.Parallel()
	if out := runArmDefaults(t, true); !strings.Contains(out, armDefaultsFailure+"\n") {
		t.Fatalf("a reader that keeps the poisoned arm stayed green:\n%s", out)
	}
}

const armDefaultsProbe = `import .ArmDefaults
import .Wire
import .serialize

var bad = 0
def poison(a:example.DefaultArm):
    a.EntriesCount = 2
    for(a.Entries) e:
        e.Retries = 77
        e.Preferred = example.WeaponNone
    a.Marker = 0

def defaults(a:example.DefaultArm) -> bool:
    if a.EntriesCount != 0 or a.Marker != 5: return false
    for(a.Entries) e:
        if e.Retries != -1 or e.Preferred != example.WeaponRailgun: return false
    return true

def check(ok:bool):
    if not ok:
        bad += 1
        print("` + armDefaultsFailure + `")

let choice = example.NewDefaultChoice()
let packet = example.NewDefaultChoicePacket()
for(2):
    poison(choice.First)
    let r = serialize_read_stream_padded("\x51")
    check(example.ReadDefaultChoice(r, choice) and r.serialize_end() and choice.Type == example.DefaultChoiceTypeFirst and defaults(choice.First))
    poison(packet.Choice.First)
    let p = serialize_read_stream_padded("\x51")
    check(example.ReadDefaultChoicePacket(p, packet) and p.serialize_end() and defaults(packet.Choice.First))
let bulk = example.NewDefaultBulkChoice()
for(2):
    poison(bulk.First.Payload)
    // tag first, count 0, marker 5, then the bytes(2) length 0 and its align
    let b = serialize_read_stream_padded("\x51\x00")
    check(example.ReadDefaultBulkChoice(b, bulk) and b.serialize_end() and defaults(bulk.First.Payload))
if bad == 0:
    print("packet arm defaults Lobster OK")
`
