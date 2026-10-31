package gotable

import (
	"fmt"
	"testing"
)

// forgedFixedSource is the generated source of <T>ForgeFixed(values, buffer),
// the fixed form's save WITHOUT the write-side contract: the header, then each
// record's hash and an unchecked <T>FixedWriteBody. The Go writer refuses a
// count past Max, a value off its range and ill-formed text in every build
// (docs/FIXED-FORM-ALGORITHM.md §3.1), so a test of the READ side's clamps and
// refusals cannot make its hostile image with <T>FixedSave any more. It makes
// the image a FOREIGN writer would, which is what the reader's defence is for.
func forgedFixedSource(typ string) string {
	return fmt.Sprintf(`
func %[1]sForgeFixed(values []%[1]s, buffer []byte) int64 {
	need := %[1]sFixedMeasure(int64(len(values)))
	if int64(len(buffer)) < need {
		return -1
	}
	clear(buffer[:TableFixedHeaderBytes])
	buffer[0] = TableFixedForm
	tableFixedPut64(buffer[TableFixedHashAt:], %[1]sFixedHash)
	tableFixedPut32(buffer[TableFixedHeaderBytes:], uint32(len(%[1]sFixedLayout)))
	copy(buffer[TableFixedHeaderBytes+4:], %[1]sFixedLayout)
	at := buffer[TableFixedHeaderBytes+4+len(%[1]sFixedLayout):]
	for k := range values {
		tableFixedPut64(at, %[1]sFixedHash)
		clear(at[8 : 8+%[1]sFixedBodyBytes])
		%[1]sFixedWriteBody(at[8:], &values[k])
		at = at[%[1]sFixedRecordBytes:]
	}
	return need
}
`, typ)
}

// TestGoTableWritersRefuseWhatTheirReadersRefuse pins the rule of
// docs/USAGE.md "Writes are the caller's responsibility" (the Go row) and
// docs/FIXED-FORM-ALGORITHM.md §3.1: a Go table save, variable and fixed,
// refuses by name what its own reader would clamp or refuse. Out-of-contract
// values are a value off its min/max, a count past Max, a length past N,
// ill-formed UTF-8 and a NUL in text. A refused save returns -1 and its Reason
// twin returns TableRefuseInvalidValue. Every save it ACCEPTS reloads with no
// clamp and no refusal, so the writer and the reader hold one contract (the
// not-vacuous control).
func TestGoTableWritersRefuseWhatTheirReadersRefuse(t *testing.T) {
	t.Parallel()
	t.Run("variable", func(t *testing.T) {
		t.Parallel()
		runGenerated(t, `package probe
table Rec {
    health int32 | min = 0, max = 1000
    name   string(8)
}
`, `package probe
import ("testing")

func good() Rec {
	var v Rec
	RecReset(&v)
	v.Health = 1000
	v.NameLength = 3
	copy(v.Name[:], "abc")
	return v
}

func TestVariableWriterRefuses(t *testing.T) {
	// THE NOT-VACUOUS CONTROL: lawful saves round-trip with no clamp, no refusal.
	for _, name := range []string{"abc", "\u00e9\u00e9", "12345678", ""} {
		v := good()
		v.NameLength = int32(len(name))
		copy(v.Name[:], name)
		b := make([]byte, 4096)
		n := RecSave(&v, b)
		if n <= 0 {
			t.Fatalf("lawful save %q refused: %d", name, n)
		}
		if size, err := RecMeasureReason(&v); err != nil || size != n {
			t.Fatalf("lawful measure %q: %d %v, save wrote %d", name, size, err, n)
		}
		var r TableReport
		got := RecLoad(make([]byte, 4096), b[:n], &r)
		if got == nil || r != (TableReport{}) || got.Health != 1000 || string(got.Name[:got.NameLength]) != name {
			t.Fatalf("lawful %q did not reload clean: %+v %+v", name, got, r)
		}
	}
	for _, edge := range []int32{0, 1000} {
		v := good()
		v.Health = edge
		if n := RecSave(&v, make([]byte, 4096)); n <= 0 {
			t.Fatalf("health %d is on the bound and must save, got %d", edge, n)
		}
	}
	cases := []struct {
		what string
		edit func(*Rec)
	}{
		{"health over max", func(v *Rec) { v.Health = 2000 }},
		{"health under min", func(v *Rec) { v.Health = -1 }},
		{"length past N", func(v *Rec) { v.NameLength = 12 }},
		{"negative length", func(v *Rec) { v.NameLength = -1 }},
		{"ill-formed UTF-8", func(v *Rec) { v.NameLength = 2; v.Name[0], v.Name[1] = 0xff, 0xfe }},
		{"truncated UTF-8", func(v *Rec) { v.NameLength = 1; v.Name[0] = 0xc3 }},
		{"a NUL in text", func(v *Rec) { v.NameLength = 3; copy(v.Name[:], "a\x00b") }},
	}
	for _, c := range cases {
		v := good()
		c.edit(&v)
		if n := RecSave(&v, make([]byte, 4096)); n != -1 {
			t.Fatalf("variable save accepted %s (returned %d); its reader clamps or refuses it", c.what, n)
		}
		if size, err := RecMeasureReason(&v); size != -1 || err != TableRefuseInvalidValue {
			t.Fatalf("variable measure of %s must be refused by name: %d %v", c.what, size, err)
		}
	}
}
`)
	})
	t.Run("counted-unbounded-struct", func(t *testing.T) {
		t.Parallel()
		runGenerated(t, `package probe
fixed table Item { n int32 }
fixed table Only { items [..2]Item }
fixed table WithBound {
    items [..2]Item
    limit int32 | min = 0, max = 10
}
`, `package probe
import "testing"

func TestCountedUnboundedStruct(t *testing.T) {
	t.Parallel()
	var only Only
	OnlyReset(&only)
	only.ItemsCount = 2
	only.Items[0].N, only.Items[1].N = 3, 4
	buffer := make([]byte, OnlyFixedMeasure(1))
	if n := OnlyFixedSave([]Only{only}, buffer); n != int64(len(buffer)) {
		t.Fatalf("lawful unbounded struct array refused: %d", n)
	}
	got := make([]Only, 1)
	var report TableReport
	if n := OnlyFixedLoad(got, buffer, make([]TableFixedEntry, 256), &report); n != 1 || report != (TableReport{}) || got[0].ItemsCount != 2 || got[0].Items[1].N != 4 {
		t.Fatalf("lawful count did not reload cleanly: n=%d got=%+v report=%+v", n, got[0], report)
	}
	for _, count := range []int32{-1, 3, 9} {
		only.ItemsCount = count
		clear(buffer)
		if n := OnlyFixedSave([]Only{only}, buffer); n != -1 {
			t.Fatalf("OnlyFixedSave accepted count %d past Max 2: %d", count, n)
		}
		if n, err := OnlyFixedSaveReason([]Only{only}, buffer); n != -1 || err != TableRefuseInvalidValue {
			t.Fatalf("OnlyFixedSaveReason(%d) = %d, %v", count, n, err)
		}
		for i, b := range buffer {
			if b != 0 { t.Fatalf("refused count %d wrote byte %d", count, i) }
		}
		var with WithBound
		WithBoundReset(&with)
		with.Limit = 5
		with.ItemsCount = count
		bufferWith := make([]byte, WithBoundFixedMeasure(1))
		if n := WithBoundFixedSave([]WithBound{with}, bufferWith); n != -1 {
			t.Fatalf("WithBoundFixedSave accepted count %d past Max 2: %d", count, n)
		}
		if n, err := WithBoundFixedSaveReason([]WithBound{with}, bufferWith); n != -1 || err != TableRefuseInvalidValue {
			t.Fatalf("WithBoundFixedSaveReason(%d) = %d, %v", count, n, err)
		}
	}
}
`)
	})
	t.Run("fixed", func(t *testing.T) {
		t.Parallel()
		runGenerated(t, `package probe
fixed table Host {
    level int32 | min = 0, max = 1000
    marks [..4]int32
    name  string(8)
    tail  int32 = 7
}
`, `package probe
import ("testing")

func good() Host {
	var v Host
	HostReset(&v)
	v.Level = 1000
	v.MarksCount = 2
	v.Marks[0], v.Marks[1] = 11, 22
	v.NameLength = 3
	copy(v.Name[:], "abc")
	return v
}

func saves(v Host) (fixed, variable int64) {
	return HostFixedSave([]Host{v}, make([]byte, HostFixedMeasure(1))), HostSave(&v, make([]byte, 4096))
}

func TestFixedWriterRefuses(t *testing.T) {
	// THE NOT-VACUOUS CONTROL.
	for _, name := range []string{"abc", "\u00e9\u00e9", "12345678", ""} {
		v := good()
		v.NameLength = int32(len(name))
		copy(v.Name[:], name)
		b := make([]byte, HostFixedMeasure(1))
		if n := HostFixedSave([]Host{v}, b); n != int64(len(b)) {
			t.Fatalf("lawful fixed save %q refused: %d", name, n)
		}
		got := make([]Host, 1)
		var r TableReport
		if n := HostFixedLoad(got, b, make([]TableFixedEntry, 256), &r); n != 1 || r != (TableReport{}) || got[0].MarksCount != 2 || got[0].Level != 1000 || string(got[0].Name[:got[0].NameLength]) != name {
			t.Fatalf("lawful %q did not reload clean: %d %+v %+v", name, n, got[0], r)
		}
		vb := make([]byte, 4096)
		vn := HostSave(&v, vb)
		var vgot Host
		var vr TableReport
		if vn <= 0 || !HostLoad(&vgot, vb[:vn], &vr) || vr != (TableReport{}) || string(vgot.Name[:vgot.NameLength]) != name {
			t.Fatalf("lawful form-1 %q did not reload clean: %d %+v %+v", name, vn, vgot, vr)
		}
	}
	for _, count := range []int32{0, 4} {
		v := good()
		v.MarksCount = count
		if f, w := saves(v); f <= 0 || w <= 0 {
			t.Fatalf("count %d is within Max and must save: %d %d", count, f, w)
		}
	}
	cases := []struct {
		what string
		edit func(*Host)
	}{
		{"level over max", func(v *Host) { v.Level = 2000 }},
		{"level under min", func(v *Host) { v.Level = -5 }},
		{"count past Max", func(v *Host) { v.MarksCount = 9 }},
		{"negative count", func(v *Host) { v.MarksCount = -1 }},
		{"length past N", func(v *Host) { v.NameLength = 12 }},
		{"negative length", func(v *Host) { v.NameLength = -1 }},
		{"ill-formed UTF-8", func(v *Host) { v.NameLength = 2; v.Name[0], v.Name[1] = 0xff, 0xfe }},
		{"a NUL in text", func(v *Host) { v.NameLength = 3; copy(v.Name[:], "a\x00b") }},
	}
	for _, c := range cases {
		v := good()
		c.edit(&v)
		f, w := saves(v)
		if f != -1 {
			t.Fatalf("fixed save accepted %s (returned %d); its reader clamps or refuses it", c.what, f)
		}
		if w != -1 {
			t.Fatalf("form-1 save of a fixed table accepted %s (returned %d)", c.what, w)
		}
		if size, err := HostMeasureReason(&v); size != -1 || err != TableRefuseInvalidValue {
			t.Fatalf("measure of %s must be refused by name: %d %v", c.what, size, err)
		}
	}
	// A bad record anywhere in a batch refuses the batch, and writes nothing.
	batch := []Host{good(), good(), good()}
	batch[2].MarksCount = 9
	buf := make([]byte, HostFixedMeasure(3))
	if n := HostFixedSave(batch, buf); n != -1 {
		t.Fatalf("a batch with one out-of-contract record was accepted: %d", n)
	}
	for i, b := range buf {
		if b != 0 {
			t.Fatalf("a refused batch must leave the buffer untouched; byte %d is %#x", i, b)
		}
	}
}
`)
	})
	t.Run("fixed-shapes", func(t *testing.T) {
		t.Parallel()
		runGenerated(t, `package probe
enum Grade { Bronze, Gold }
type ArmA { n int32 = 0 | min = 0, max = 10 }
type ArmB { m int32 = 0 }
union Pick
{
    a ArmA
    b ArmB
}
fixed table Item { n int32 = 0 | min = 0, max = 10 }
fixed table Root {
    mask  bits(12)
    ratio float32 = 0.5 | min = 0, max = 1, resolution = 0.5
    items [..3]Item
    note  ?int32 | min = 0, max = 10
    pick  Pick
    grade Grade
    raw   bytes(4)
}
`, `package probe
import ("testing")

func good() Root {
	var v Root
	RootReset(&v)
	v.Mask = 4095
	v.Ratio = 1
	v.ItemsCount = 2
	v.Items[0].N, v.Items[1].N = 10, 0
	v.Pick.Type = PickTypeA
	v.Pick.A.N = 10
	v.Grade = GradeGold
	return v
}

func save(v Root) int64 {
	return RootFixedSave([]Root{v}, make([]byte, RootFixedMeasure(1)))
}

func TestFixedShapes(t *testing.T) {
	// THE CONTROLS: the boundary values and the slack the contract does not
	// reach all save, and reload with no clamp and no refusal.
	accepted := []struct {
		what string
		edit func(*Root)
	}{
		{"the lawful record", func(v *Root) {}},
		{"slack past the live count", func(v *Root) { v.Items[2].N = 99 }},
		{"an absent optional's payload", func(v *Root) { v.NotePresent = false; v.Note = 99 }},
		{"a present optional on its bound", func(v *Root) { v.NotePresent = true; v.Note = 10 }},
		{"an unselected arm", func(v *Root) { v.Pick.Type = PickTypeB; v.Pick.A.N = 99 }},
		{"the None tag", func(v *Root) { v.Pick.Type = PickTypeNone }},
		{"the enum's None", func(v *Root) { v.Grade = GradeNone }},
		{"raw bytes carry no text rule", func(v *Root) { v.RawLength = 4; v.Raw = [4]byte{0xff, 0, 0xfe, 0} }},
	}
	for _, c := range accepted {
		v := good()
		c.edit(&v)
		b := make([]byte, RootFixedMeasure(1))
		if n := RootFixedSave([]Root{v}, b); n != int64(len(b)) {
			t.Fatalf("%s must save, got %d", c.what, n)
		}
		got := make([]Root, 1)
		var r TableReport
		if n := RootFixedLoad(got, b, make([]TableFixedEntry, 256), &r); n != 1 || r != (TableReport{}) {
			t.Fatalf("%s did not reload clean: %d %+v", c.what, n, r)
		}
	}
	refused := []struct {
		what string
		edit func(*Root)
	}{
		{"bits(12) past its width", func(v *Root) { v.Mask = 5000 }},
		{"a float over its max", func(v *Root) { v.Ratio = 2 }},
		{"a float under its min", func(v *Root) { v.Ratio = -0.5 }},
		{"a live element off its range", func(v *Root) { v.Items[1].N = 99 }},
		{"a negative element", func(v *Root) { v.Items[0].N = -1 }},
		{"a present optional off its range", func(v *Root) { v.NotePresent = true; v.Note = 99 }},
		{"a union tag past the arm set", func(v *Root) { v.Pick.Type = PickType(7) }},
		{"the selected arm off its range", func(v *Root) { v.Pick.Type = PickTypeA; v.Pick.A.N = 99 }},
		{"an enum ordinal past the set", func(v *Root) { v.Grade = Grade(9) }},
		{"a bytes length past N", func(v *Root) { v.RawLength = 5 }},
	}
	for _, c := range refused {
		v := good()
		c.edit(&v)
		if n := save(v); n != -1 {
			t.Fatalf("fixed save accepted %s (returned %d)", c.what, n)
		}
	}
}
`)
	})
	// THE TEXT RULE AND THE LIVE EXTENT. Both writers' string rule is an in-line
	// word test (emitTextWords; the fixed writer's with an exact twin behind it),
	// so it is held here to its definition, utf8.Valid plus a NUL scan over the
	// used bytes, on many seeded strings at every length 0..N of fields narrower
	// than a word, one word wide and many words wide, at every live position,
	// through the fixed save and the form-1 save of the same value. And the
	// writer stores the LIVE extent only (docs/FIXED-FORM-ALGORITHM.md §3.1:
	// "Arrays write `count` elements and text and bytes `length` units, never all
	// `Max`"): the caller's storage past a length and past the count, and the
	// output buffer, are stained, and every slack byte on the wire is zero.
	t.Run("fixed-text-and-extent", func(t *testing.T) {
		t.Parallel()
		runGenerated(t, `package probe
fixed table Entry {
    kind string(5)
    card string(37)
    at   int64
}
fixed table Log {
    cursor string(16)
    done   [..6]Entry
}
`, `package probe
import (
	"bytes"
	"math/rand"
	"testing"
	"unicode/utf8"
)

// alphabet leans on the bytes the rule turns on: NUL, ASCII edges, UTF-8
// leads and continuations, surrogate and overlong starts, and 0xff.
var alphabet = []byte{0x00, 'a', 'z', 0x01, 0x7f, 0x80, 0xbf, 0xc2, 0xc3, 0xa9, 0xe2, 0x82, 0xac, 0xed, 0xa0, 0xf0, 0x9f, 0x98, 0x80, 0xf4, 0x90, 0xc0, 0xff}

func text(r *rand.Rand, n int) []byte {
	p := make([]byte, n)
	for i := range p {
		if r.Intn(3) == 0 {
			p[i] = alphabet[r.Intn(len(alphabet))]
		} else {
			p[i] = byte('a' + r.Intn(26))
		}
	}
	// some strings are made lawful on purpose, so both verdicts are common
	if r.Intn(2) == 0 {
		p = bytes.ToValidUTF8(bytes.ReplaceAll(p, []byte{0}, []byte{'0'}), []byte("\u00e9"))
		if len(p) > n {
			p = p[:n]
			for len(p) > 0 && !utf8.Valid(p) {
				p = p[:len(p)-1]
			}
		}
	}
	return p
}

func lawful(p []byte) bool { return utf8.Valid(p) && bytes.IndexByte(p, 0) < 0 }

func lawfulLog(count int) Log {
	var v Log
	LogReset(&v)
	v.CursorLength = int32(copy(v.Cursor[:], "c-1"))
	v.DoneCount = int32(count)
	for i := 0; i < count; i++ {
		e := &v.Done[i]
		e.KindLength = int32(copy(e.Kind[:], "w"))
		e.CardLength = int32(copy(e.Card[:], "card"))
		e.At = int64(i)
	}
	return v
}

func TestFixedTextRule(t *testing.T) {
	r := rand.New(rand.NewSource(20261016))
	buf := make([]byte, LogFixedMeasure(1))
	vbuf := make([]byte, 1<<16)
	set := func(dst []byte, n *int32, p []byte) {
		for i := range dst {
			dst[i] = 0x5A // the stain past the used bytes: never asked
		}
		*n = int32(copy(dst, p))
	}
	seen := map[bool]int{}
	for trial := 0; trial < 20000; trial++ {
		v := lawfulLog(1 + r.Intn(6))
		var p []byte
		switch field, at := r.Intn(3), r.Intn(int(v.DoneCount)); field {
		case 0:
			p = text(r, r.Intn(17))
			set(v.Cursor[:], &v.CursorLength, p)
		case 1:
			p = text(r, r.Intn(6))
			set(v.Done[at].Kind[:], &v.Done[at].KindLength, p)
		default:
			p = text(r, r.Intn(38))
			set(v.Done[at].Card[:], &v.Done[at].CardLength, p)
		}
		want := lawful(p)
		seen[want]++
		n := LogFixedSave([]Log{v}, buf)
		if got := n == int64(len(buf)); got != want {
			t.Fatalf("trial %d: text % x: the writer said %v (returned %d), utf8.Valid plus the NUL scan says %v", trial, p, got, n, want)
		}
		if vn := LogSave(&v, vbuf); (vn > 0) != want {
			t.Fatalf("trial %d: text % x: the form-1 writer returned %d, utf8.Valid plus the NUL scan says %v", trial, p, vn, want)
		}
		if !want {
			if _, err := LogFixedSaveReason([]Log{v}, buf); err != TableRefuseInvalidValue {
				t.Fatalf("trial %d: text % x is refused, but not by name: %v", trial, p, err)
			}
		}
	}
	if seen[true] < 1000 || seen[false] < 1000 {
		t.Fatalf("the trials did not exercise both verdicts: %v", seen)
	}
}

func TestFixedWriterStoresTheLiveExtent(t *testing.T) {
	if LogFixedBodyBytes != 20+4+6*58 {
		t.Fatalf("the record body moved (%d bytes), so the offsets below are stale", LogFixedBodyBytes)
	}
	v := lawfulLog(2)
	// the caller's storage is stained past every length and past the count
	for i := int(v.CursorLength); i < len(v.Cursor); i++ {
		v.Cursor[i] = 0x5A
	}
	for k := range v.Done {
		e := &v.Done[k]
		for i := int(e.KindLength); i < len(e.Kind); i++ {
			e.Kind[i] = 0x5A
		}
		for i := int(e.CardLength); i < len(e.Card); i++ {
			e.Card[i] = 0x5A
		}
	}
	v.Done[4].At = 0x5A5A5A5A
	buf := bytes.Repeat([]byte{0x5A}, int(LogFixedMeasure(1)))
	if n := LogFixedSave([]Log{v}, buf); n != int64(len(buf)) {
		t.Fatalf("the lawful log did not save: %d", n)
	}
	body := buf[len(buf)-LogFixedRecordBytes+8:]
	zero := func(what string, b []byte) {
		t.Helper()
		for i, c := range b {
			if c != 0 {
				t.Fatalf("%s: slack byte %d is %#x; a stain reached the wire (§3.1: the live extent only, on a zeroed template)", what, i, c)
			}
		}
	}
	zero("cursor past its length", body[4+3:20])
	for k := 0; k < 2; k++ {
		e := body[24+58*k:]
		zero("kind past its length", e[4+1:9])
		zero("card past its length", e[9+4+4:9+41])
	}
	zero("the dead slots past the count", body[24+58*2:])
	got := make([]Log, 1)
	var rep TableReport
	if n := LogFixedLoad(got, buf, make([]TableFixedEntry, 256), &rep); n != 1 || rep != (TableReport{}) {
		t.Fatalf("the log did not reload clean: %d %+v", n, rep)
	}
	if got[0].DoneCount != 2 || string(got[0].Done[1].Card[:got[0].Done[1].CardLength]) != "card" || got[0].Done[1].At != 1 {
		t.Fatalf("the live extent did not round-trip: %+v", got[0].Done[:2])
	}
}
`)
	})
	t.Run("variable-shapes", func(t *testing.T) {
		t.Parallel()
		runGenerated(t, `package probe
table V {
    ratio float32 = 0.5 | min = 0, max = 1, resolution = 0.5
    mask  bits(12)
    marks [..4]int32 | min = 0, max = 10
}
`, `package probe
import ("testing")

func good() V {
	var v V
	VReset(&v)
	v.Ratio = 1
	v.Mask = 4095
	v.MarksCount = 2
	v.Marks[0], v.Marks[1] = 10, 3
	return v
}

func TestVariableShapes(t *testing.T) {
	v := good()
	b := make([]byte, 4096)
	n := VSave(&v, b)
	if n <= 0 {
		t.Fatalf("the lawful record must save, got %d", n)
	}
	var r TableReport
	if got := VLoad(make([]byte, 4096), b[:n], &r); got == nil || r != (TableReport{}) || got.Mask != 4095 || got.MarksCount != 2 {
		t.Fatalf("the lawful record did not reload clean: %+v %+v", got, r)
	}
	slack := good()
	slack.Marks[3] = 99
	if n := VSave(&slack, make([]byte, 4096)); n <= 0 {
		t.Fatalf("slack past the live count is not the contract's: %d", n)
	}
	cases := []struct {
		what string
		edit func(*V)
	}{
		{"bits(12) past its width", func(v *V) { v.Mask = 5000 }},
		{"a float over its max", func(v *V) { v.Ratio = 2 }},
		{"a float under its min", func(v *V) { v.Ratio = -1 }},
		{"a live element off its range", func(v *V) { v.Marks[1] = 11 }},
		{"a count past Max", func(v *V) { v.MarksCount = 5 }},
		{"a negative count", func(v *V) { v.MarksCount = -1 }},
	}
	for _, c := range cases {
		v := good()
		c.edit(&v)
		if n := VSave(&v, make([]byte, 4096)); n != -1 {
			t.Fatalf("variable save accepted %s (returned %d)", c.what, n)
		}
		if size, err := VMeasureReason(&v); size != -1 || err != TableRefuseInvalidValue {
			t.Fatalf("variable measure of %s must be refused by name: %d %v", c.what, size, err)
		}
	}
}
`)
	})
}
