package main

import (
	"testing"

	"github.com/mas-bandwidth/serialize.go"
	w "packetwide"
	p "packetwide/shapes"
)

func writeValue[T any](t *testing.T, value *T, encode func(*serialize.WriteStream, *T) error) ([]byte, int) {
	t.Helper()
	var bytes [1024]byte
	s := serialize.NewWriteStream(bytes[:])
	if err := encode(s, value); err != nil {
		t.Fatal(err)
	}
	s.Flush()
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	return s.Data(), int(s.BitsProcessed())
}

func TestBoundsPairingAndTail(t *testing.T) {
	var v w.WideSeven
	for _, n := range []int32{-1, 8, 1} {
		v.TextLength = n
		if w.WriteWideSeven(serialize.NewWriteStream(make([]byte, 256)), &v) == nil {
			t.Fatal("writer accepted length/null misuse", n)
		}
	}
	v.Text[0] = 0xd800
	bytes, _ := writeValue(t, &v, w.WriteWideSeven)
	if w.ReadWideSeven(serialize.NewReadStream(bytes), &v) == nil {
		t.Fatal("unpaired high accepted")
	}
	v.Text[0] = 0xffff
	bytes, _ = writeValue(t, &v, w.WriteWideSeven)
	for i := range v.Text {
		v.Text[i] = 0x7f7f
	}
	if err := w.ReadWideSeven(serialize.NewReadStream(bytes), &v); err != nil {
		t.Fatal(err)
	}
	if v.Text != [7]uint16{0xffff, 0x7f7f, 0x7f7f, 0x7f7f, 0x7f7f, 0x7f7f, 0x7f7f} {
		t.Fatal("unused tail changed", v)
	}
	r := serialize.NewReadStream(bytes)
	if n := testing.AllocsPerRun(1000, func() {
		r.Reset(bytes)
		if w.ReadWideSeven(r, &v) != nil {
			panic("read")
		}
	}); n != 0 {
		t.Fatal("read allocated", n)
	}
}

func TestComposition(t *testing.T) {
	v := p.Conditional{Enabled: true, Text: [4]uint16{0xd800, 0xdc00}, TextLength: 2}
	bytes, bits := writeValue(t, &v, p.WriteConditional)
	if bits != 68 || bytes[0]&15 != 5 {
		t.Fatal("aligned wide groups", bits, bytes)
	}
	var out p.Conditional
	if err := p.ReadConditional(serialize.NewReadStream(bytes), &out); err != nil || out != v {
		t.Fatal("conditional", err, out)
	}
	v.Enabled = false
	bytes, _ = writeValue(t, &v, p.WriteConditional)
	if err := p.ReadConditional(serialize.NewReadStream(bytes), &out); err != nil || out != (p.Conditional{}) {
		t.Fatal("branch not zeroed", err, out)
	}
	text := p.Text{Value: [4]uint16{0xffff}, ValueLength: 1}
	choice := p.Choice{Type: p.ChoiceTypeText, Text: text}
	bytes, _ = writeValue(t, &choice, p.WriteChoice)
	var co p.Choice
	for i := 0; i < 2; i++ {
		co.Text.Value[3] = 0x7f7f
		if err := p.ReadChoice(serialize.NewReadStream(bytes), &co); err != nil || co.Text != text {
			t.Fatal("union not fresh", err, co)
		}
	}
	bx := p.NewBox()
	if bx.CountedCount != 1 {
		t.Fatal("born count")
	}
	bx.Items[0], bx.Counted[0], bx.Choice = text, text, choice
	bytes, _ = writeValue(t, &bx, p.WriteBox)
	bo := p.NewBox()
	bo.Counted[1].Value[0] = 0x7f7f
	if err := p.ReadBox(serialize.NewReadStream(bytes), &bo); err != nil {
		t.Fatal(err)
	}
	if bo.Items != bx.Items || bo.Counted[0] != text || bo.CountedCount != 1 || bo.Counted[1].Value[0] != 0x7f7f || bo.Choice != choice {
		t.Fatal("array/union composition", bo)
	}
}
