package gotable

import (
	"encoding/binary"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
)

// fixedDeadWriter is the OLDER writer the lineage dead-byte probes read: one
// field of each class of slack. Its body, in declared order: marks count at 0
// and two int32 slots at 4..11; label length at 12 and four bytes at 16..19;
// note's present byte at 20 and its int32 at 21..24; pick's tag at 25 (wide 1,
// narrow 2, ping 3, amount 4) and the widest arm's eight bytes at 26..33.
const fixedDeadWriter = `package probe
union Pick
{
    wide   int64
    narrow int16
    ping
    amount int32
}
fixed table Root {
    marks [..2]int32
    label string(4)
    note  ?int32
    pick  Pick
}
`

const fixedDeadWriterBody = 34

// fixedDeadWriterFile is one record of fixedDeadWriter, written by hand from
// the layout the writer's build locks: the form byte, seven reserved zeros, the
// layout hash at 8, the layout behind its u32 length, then the record's hash
// and body. marks holds one element (7), label "hi", note is absent and pick
// takes the narrow arm with the value 0; every other byte is zero.
func fixedDeadWriterFile(t *testing.T) []byte {
	t.Helper()
	o := unitOf(t, fixedDeadWriter)
	e, ok := FixedLineageOf(o, "Root")
	if !ok {
		t.Fatal("no lineage entry for the writer's Root")
	}
	if e.Record != 8+fixedDeadWriterBody {
		t.Fatalf("the writer's record is %d bytes, so the offsets are stale", e.Record)
	}
	file := make([]byte, 16, 16+4+len(e.Layout)+int(e.Record))
	file[0] = 3
	binary.LittleEndian.PutUint64(file[8:], e.Wire)
	file = binary.LittleEndian.AppendUint32(file, uint32(len(e.Layout)))
	file = append(file, e.Layout...)
	file = binary.LittleEndian.AppendUint64(file, e.Wire)
	body := make([]byte, fixedDeadWriterBody)
	binary.LittleEndian.PutUint32(body[0:], 1)
	binary.LittleEndian.PutUint32(body[4:], 7)
	binary.LittleEndian.PutUint32(body[12:], 2)
	body[16], body[17] = 'h', 'i'
	body[25] = 2
	return append(file, body...)
}

// fixedDeadProbe is the probe's source: the clean file, a stained copy per
// case refused wire_damaged with nothing landed, and the live controls read.
func fixedDeadProbe(file []byte, refused map[string][2]int, live map[string][2]int) string {
	var b strings.Builder
	b.WriteString(`package probe
import ("testing"; "unsafe")

func TestLineageSlackIsZero(t *testing.T) {
	clean := []byte{`)
	for i, c := range file {
		if i%16 == 0 {
			b.WriteString("\n\t\t")
		}
		fmt.Fprintf(&b, "0x%02x, ", c)
	}
	fmt.Fprintf(&b, `
	}
	body := len(clean) - %d
	stain := func(at, v int) []byte {
		out := append([]byte(nil), clean...)
		out[body+at] = byte(v)
		return out
	}
	load := func(data []byte) (Root, TableReport, int64) {
		got := make([]Root, 1)
		dst := unsafe.Slice((*byte)(unsafe.Pointer(&got[0])), unsafe.Sizeof(got[0]))
		for i := range dst {
			dst[i] = 0x5A
		}
		var r TableReport
		plan := make([]TableFixedEntry, 4096)
		n := RootFixedLoad(got, data, plan, &r)
		if n < 0 {
			for i, c := range dst {
				if c != 0x5A {
					t.Fatalf("a refusal wrote destination byte %%d", i)
				}
			}
		}
		return got[0], r, n
	}
	if got, r, n := load(clean); n != 1 || r.Reason != "" || got.MarksCount != 1 || got.Marks[0] != 7 || got.LabelLength != 2 || got.NotePresent {
		t.Fatalf("the clean writer record: n=%%d %%+v %%+v", n, r, got)
	}
`, fixedDeadWriterBody)
	for _, name := range sortedKeys(refused) {
		c := refused[name]
		fmt.Fprintf(&b, "\tif _, r, n := load(stain(%d, %d)); n != -1 || r.Reason != \"wire_damaged\" {\n", c[0], c[1])
		fmt.Fprintf(&b, "\t\tt.Fatalf(\"%%s: n=%%d %%+v, want refused wire_damaged\", %q, n, r)\n\t}\n", name)
	}
	for _, name := range sortedKeys(live) {
		c := live[name]
		fmt.Fprintf(&b, "\tif _, r, n := load(stain(%d, %d)); n != 1 || r.Reason != \"\" {\n", c[0], c[1])
		fmt.Fprintf(&b, "\t\tt.Fatalf(\"%%s: n=%%d %%+v, want the record read\", %q, n, r)\n\t}\n", name)
	}
	b.WriteString("}\n")
	return b.String()
}

func sortedKeys(m map[string][2]int) []string {
	return slices.Sorted(maps.Keys(m))
}

// TestFixedFormLineageSlackIsZero holds the owner's ruling of 2026-11-10
// ("Every dead byte", "Be strict"; docs/rulings.json fixed-dead-bytes-zero,
// docs/SPEC-TABLES.md §3.4) on a COMPILED LINEAGE PLAN: a newer reader reads an
// older writer's record through the lock, and the slack is the WRITER's — past
// the writer's count and length, under the writer's absent flag, behind the
// writer's arm (an arm this reader cannot name included), and the writer's
// slots and units past this reader's own bound. One stained byte of each
// refuses wire_damaged with nothing landed; the clean record and a stained live
// byte read.
func TestFixedFormLineageSlackIsZero(t *testing.T) {
	t.Parallel()
	file := fixedDeadWriterFile(t)
	every := map[string][2]int{
		"a counted array past the writer's live count": {8, 1},
		"string(4) past the writer's used length":      {18, 'x'},
		"an absent optional's payload":                 {22, 1},
		"the arm bytes behind the writer's narrow arm": {28, 1},
		"the last arm byte behind the narrow arm":      {33, 1},
	}
	live := map[string][2]int{
		"the narrow arm's high byte": {27, 1},
	}

	// THE READER GREW A FIELD (an appended field: the writer's plan is the
	// reader's own with a hole).
	grown := strings.Replace(fixedDeadWriter, "    pick  Pick\n}", "    pick  Pick\n    tail  int32 = 7\n}", 1)
	if out, err := runVersionProbe(t, grown, []string{fixedDeadWriter}, fixedDeadProbe(file, every, live)); err != nil {
		t.Fatalf("a grown reader: %v\n%s", err, out)
	}

	// UNDER THE WRITER'S TAG 0 AND ITS PAYLOAD-FREE ARM every arm byte is slack.
	none := append([]byte(nil), file...)
	none[len(none)-fixedDeadWriterBody+25] = 0
	ping := append([]byte(nil), file...)
	ping[len(ping)-fixedDeadWriterBody+25] = 3
	for name, image := range map[string][]byte{"tag 0": none, "the payload-free arm": ping} {
		cases := map[string][2]int{
			name + ": the first arm byte": {26, 1},
			name + ": the last arm byte":  {33, 1},
		}
		if out, err := runVersionProbe(t, grown, []string{fixedDeadWriter}, fixedDeadProbe(image, cases, nil)); err != nil {
			t.Fatalf("%s: %v\n%s", name, err, out)
		}
	}

	// THE READER CANNOT NAME THE WRITER'S ARM: narrow is gone, the record reads
	// its union as None, and the bytes behind the writer's narrow arm are still
	// the writer's slack.
	dropped := strings.Replace(grown, "    narrow int16\n", "", 1)
	if out, err := runVersionProbe(t, dropped, []string{fixedDeadWriter}, fixedDeadProbe(file, every, live)); err != nil {
		t.Fatalf("a reader without the writer's arm: %v\n%s", err, out)
	}

	// THE READER'S BOUNDS ARE NARROWER THAN THE WRITER'S: the writer's slot and
	// units past this reader's bound are slack on every record the plan admits,
	// since the count and the length are held to the reader's bound.
	narrowed := strings.Replace(strings.Replace(grown, "marks [..2]int32", "marks [..1]int32", 1), "label string(4)", "label string(2)", 1)
	if out, err := runVersionProbe(t, narrowed, []string{fixedDeadWriter}, fixedDeadProbe(file, every, live)); err != nil {
		t.Fatalf("a reader with narrower bounds: %v\n%s", err, out)
	}
}
