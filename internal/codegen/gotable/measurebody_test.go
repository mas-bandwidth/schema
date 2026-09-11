package gotable

import (
	"strings"
	"testing"
)

const arithMeasureSchema = `package probe
flags Caps { Jump, Fly }
enum Grade { Gold, Silver }
fixed table Leaf {
 n int32
 u uint8
 flag Caps
 grade Grade
 ok bool
 x float32
 wide uint128
 bitsy bits(8)
 maybe ?int32
}
fixed table Child {
 n uint32
}
fixed table Root {
 a uint32
 child Child
 note string(8)
}
fixed table Arr {
 xs [..3]int32
}
fixed table Fixedish {
 q fixed(4,4) | min = -8, max = 7
}
`

func tableGoSource(t *testing.T, src string) string {
	t.Helper()
	files := generate(t, src)
	var body string
	for name, data := range files {
		if strings.HasSuffix(name, "Table.go") {
			body += string(data)
		}
	}
	if body == "" {
		t.Fatal("no Table.go")
	}
	return body
}

func generatedFunc(src, name string) string {
	marker := "func " + name
	i := strings.Index(src, marker)
	if i < 0 {
		return ""
	}
	src = src[i:]
	if j := strings.Index(src[1:], "\nfunc "); j >= 0 {
		src = src[:j+1]
	}
	return src
}

func TestArithMeasureBodyShape(t *testing.T) {
	body := tableGoSource(t, arithMeasureSchema)
	leaf := generatedFunc(body, "LeafMeasureBody")
	if leaf == "" {
		t.Fatal("LeafMeasureBody missing")
	}
	for _, want := range []string{
		"bytes := int64(1)",
		"tableLebBytes(ids.refAtHit(",
		"+ 1 + 4",
		"+ 1 + 1",
		"+ 1 + 8",
		"+ 1 + 16",
		"vref = ids.refAtHit(",
		"tableLebBytes(ref) + 1 + tableLebBytes(vref)",
		"if ids.Overflow",
	} {
		if !strings.Contains(leaf, want) {
			t.Fatalf("LeafMeasureBody missing %q\n%s", want, leaf)
		}
	}
	for _, old := range []string{"Measuring", "LeafSaveBody", "TableWriter{", "w.Header(", "w.Ids.RefAt("} {
		if strings.Contains(leaf, old) {
			t.Fatalf("LeafMeasureBody still dry-runs via %q\n%s", old, leaf)
		}
	}
	child := generatedFunc(body, "ChildMeasureBody")
	if !strings.Contains(child, "tableLebBytes(ids.refAtHit(") || strings.Contains(child, "ChildSaveBody") {
		t.Fatalf("ChildMeasureBody is not arithmetic\n%s", child)
	}
	for _, name := range []string{"RootMeasureBody", "ArrMeasureBody", "FixedishMeasureBody"} {
		fn := generatedFunc(body, name)
		if !strings.Contains(fn, "Measuring") || !strings.Contains(fn, "SaveBody") {
			t.Fatalf("%s dropped the dry-run walk\n%s", name, fn)
		}
	}
	save := generatedFunc(body, "LeafSaveBody")
	if !strings.Contains(save, "w.Ids.refAtHit(") || !strings.Contains(save, "headerPair") {
		t.Fatal("LeafSaveBody lost the 795 write path")
	}
	rootSave := generatedFunc(body, "RootSaveBody")
	if !strings.Contains(rootSave, "ChildMeasureBody") || !strings.Contains(rootSave, "n > 1") {
		t.Fatal("RootSaveBody lost the 793 child-table reuse")
	}
	runGenerated(t, arithMeasureSchema, arithMeasureWireTest)
}

const arithMeasureWireTest = `package probe
import (
	"encoding/binary"
	"testing"
)

func trailerIds(t *testing.T, wire []byte) []uint64 {
	t.Helper()
	if len(wire) < 8 {
		t.Fatalf("short wire %d", len(wire))
	}
	count := int(binary.LittleEndian.Uint64(wire[len(wire)-8:]))
	if count < 0 || 8+count*8 > len(wire) {
		t.Fatalf("trailer count %d in %d bytes", count, len(wire))
	}
	ids := make([]uint64, count)
	off := len(wire) - 8 - count*8
	for i := range ids {
		ids[i] = binary.LittleEndian.Uint64(wire[off+i*8:])
	}
	return ids
}

func TestLeafMeasureEqualsSave(t *testing.T) {
	var empty Leaf
	if n := LeafMeasure(&empty); n != 10 {
		t.Fatalf("elided file %d", n)
	}
	buf := make([]byte, 10)
	if LeafSave(&empty, buf) != 10 {
		t.Fatal("elided save")
	}
	if got := trailerIds(t, buf); len(got) != 0 {
		t.Fatalf("elided ids %v", got)
	}

	empty.MaybePresent = true
	if n := LeafMeasure(&empty); n <= 10 {
		t.Fatalf("optional default elided: %d", n)
	}

	var v Leaf
	v.N = 7
	v.U = 3
	v.Flag = CapsJump | CapsFly
	v.Grade = GradeGold
	v.Ok = true
	v.X = 1.5
	v.Wide.Lo = 9
	v.Bitsy = 4
	v.Maybe = 0
	v.MaybePresent = true

	var ids TableIds
	body := LeafMeasureBody(&v, &ids)
	if body < 0 || ids.Overflow {
		t.Fatal("measure body")
	}
	size := LeafMeasure(&v)
	if size != 1+body+int64(ids.Count)*8+8 {
		t.Fatalf("file size %d body %d ids %d", size, body, ids.Count)
	}
	wire := make([]byte, size)
	if LeafSave(&v, wire) != size {
		t.Fatal("save")
	}
	got := trailerIds(t, wire)
	if len(got) != ids.Count {
		t.Fatalf("id count measure %d save %d", ids.Count, len(got))
	}
	for i, id := range got {
		if id != ids.Values[i] {
			t.Fatalf("first-use id %d: measure 0x%x save 0x%x", i, ids.Values[i], id)
		}
	}
	if LeafSave(&v, make([]byte, size-1)) != -1 {
		t.Fatal("short buffer accepted")
	}
	v.Grade = Grade(99)
	if LeafMeasure(&v) != -1 || LeafSave(&v, make([]byte, 256)) != -1 {
		t.Fatal("invalid enum measured or saved")
	}

	var full TableIds
	full.Count = len(full.Values)
	v.Grade = GradeGold
	body = LeafMeasureBody(&v, &full)
	if body != -1 {
		t.Fatalf("MeasureBody on overflow returned %d, want -1", body)
	}
	if !full.Overflow {
		t.Fatal("Overflow unset")
	}
}

func TestChildMeasureOnParentWalk(t *testing.T) {
	var v Root
	v.A = 1
	v.Child.N = 5
	copy(v.Note[:], []byte("hi"))
	v.NoteLength = 2
	n := RootMeasure(&v)
	if n < 0 {
		t.Fatal("measure")
	}
	wire := make([]byte, n)
	if RootSave(&v, wire) != n {
		t.Fatal("save")
	}
	if RootSave(&v, make([]byte, n-1)) != -1 {
		t.Fatal("short buffer accepted")
	}
	var loaded Root
	var report TableReport
	if !RootLoad(&loaded, wire, &report) || report != (TableReport{}) || loaded.A != 1 || loaded.Child.N != 5 || loaded.NoteLength != 2 {
		t.Fatalf("round trip %+v %+v", loaded, report)
	}

	var z Root
	if n := RootMeasure(&z); n != 10 {
		t.Fatalf("elided root %d", n)
	}
}
`

const unionArmMeasureSchema = `package probe
fixed table Hit {
 n int32
}
fixed table Chat {
 note string(8)
}
union Event {
 hit Hit
 chat Chat
 ping
 note string(8)
}
fixed table Root {
 choice Event
}
`

func TestUnionTableArmMeasureBodyShape(t *testing.T) {
	body := tableGoSource(t, unionArmMeasureSchema)
	save := generatedFunc(body, "RootSaveBody")
	if save == "" {
		t.Fatal("RootSaveBody missing")
	}
	for _, want := range []string{
		"HitMeasureBody(&",
		"ChatMeasureBody(&",
		"HitSaveBody(",
		"ChatSaveBody(",
		"Ids.Truncate(mark)",
	} {
		if !strings.Contains(save, want) {
			t.Fatalf("RootSaveBody missing %q\n%s", want, save)
		}
	}
	if n := strings.Count(save, "TableWriter{Measuring: true"); n != 1 {
		t.Fatalf("RootSaveBody nested measuring writers = %d, want 1 (string arm only)\n%s", n, save)
	}
	if strings.Contains(save, "HitSaveBody(&") {
		t.Fatalf("Hit arm still dry-runs SaveBody into a measuring writer\n%s", save)
	}
	runGenerated(t, unionArmMeasureSchema, unionArmMeasureWireTest)
}

const unionArmMeasureWireTest = `package probe
import (
	"encoding/binary"
	"testing"
)

func trailerIds(t *testing.T, wire []byte) []uint64 {
	t.Helper()
	if len(wire) < 8 {
		t.Fatalf("short wire %d", len(wire))
	}
	count := int(binary.LittleEndian.Uint64(wire[len(wire)-8:]))
	if count < 0 || 8+count*8 > len(wire) {
		t.Fatalf("trailer count %d in %d bytes", count, len(wire))
	}
	ids := make([]uint64, count)
	off := len(wire) - 8 - count*8
	for i := range ids {
		ids[i] = binary.LittleEndian.Uint64(wire[off+i*8:])
	}
	return ids
}

func roundTrip(t *testing.T, v *Root) []byte {
	t.Helper()
	n := RootMeasure(v)
	if n < 0 {
		t.Fatal("measure")
	}
	wire := make([]byte, n)
	if RootSave(v, wire) != n {
		t.Fatal("save")
	}
	if RootSave(v, make([]byte, n-1)) != -1 {
		t.Fatal("short buffer accepted")
	}
	var loaded Root
	var report TableReport
	if !RootLoad(&loaded, wire, &report) || report != (TableReport{}) {
		t.Fatalf("load %+v", report)
	}
	if loaded.Choice.Type != v.Choice.Type || loaded.Choice.Hit.N != v.Choice.Hit.N ||
		loaded.Choice.Chat.NoteLength != v.Choice.Chat.NoteLength ||
		string(loaded.Choice.Chat.Note[:loaded.Choice.Chat.NoteLength]) != string(v.Choice.Chat.Note[:v.Choice.Chat.NoteLength]) ||
		loaded.Choice.NoteLength != v.Choice.NoteLength ||
		string(loaded.Choice.Note[:loaded.Choice.NoteLength]) != string(v.Choice.Note[:v.Choice.NoteLength]) {
		t.Fatalf("round trip %+v want %+v", loaded, *v)
	}
	return wire
}

func TestUnionTableArmMeasureEqualsSave(t *testing.T) {
	var empty Root
	if n := RootMeasure(&empty); n != 10 {
		t.Fatalf("elided file %d", n)
	}

	var hit Root
	hit.Choice.Type = EventTypeHit
	hit.Choice.Hit.N = 7
	wire := roundTrip(t, &hit)
	got := trailerIds(t, wire)
	var ids TableIds
	body := HitMeasureBody(&hit.Choice.Hit, &ids)
	if body < 0 || ids.Overflow {
		t.Fatal("hit measure body")
	}
	if len(got) < ids.Count {
		t.Fatalf("trailer lost arm ids: measure %d save %d", ids.Count, len(got))
	}
	found := 0
	for _, want := range ids.Values[:ids.Count] {
		for _, id := range got {
			if id == want {
				found++
				break
			}
		}
	}
	if found != ids.Count {
		t.Fatalf("arm field ids %v not in trailer %v", ids.Values[:ids.Count], got)
	}

	var chat Root
	chat.Choice.Type = EventTypeChat
	copy(chat.Choice.Chat.Note[:], []byte("hi"))
	chat.Choice.Chat.NoteLength = 2
	roundTrip(t, &chat)

	var ping Root
	ping.Choice.Type = EventTypePing
	roundTrip(t, &ping)

	var note Root
	note.Choice.Type = EventTypeNote
	copy(note.Choice.Note[:], []byte("ab"))
	note.Choice.NoteLength = 2
	roundTrip(t, &note)

	var full TableIds
	full.Count = len(full.Values)
	if HitMeasureBody(&hit.Choice.Hit, &full) != -1 {
		t.Fatal("HitMeasureBody on overflow")
	}
	if RootMeasure(&hit) < 0 {
		t.Fatal("fresh RootMeasure overflowed")
	}
}
`
