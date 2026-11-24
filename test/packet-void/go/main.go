// The Go leg of test/packet-void, and the family's oracle: with -write-pins
// it writes every accept and select case's pin from this writer (the refuse
// pins are hand-made; testdata/wire/packet-void/README.md). Otherwise it
// answers every case of cases.txt, one "case <name> ok|FAILED: <why>" line
// each, for tools/casegate.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	v "packetvoid"

	"github.com/mas-bandwidth/serialize.go/v2"
)

type codec struct {
	write func(*serialize.WriteStream) error
	read  func(*serialize.ReadStream) error
	again func(*serialize.WriteStream) error // writes what read read
}

func codecOf[T any](value T, write func(*serialize.WriteStream, *T) error, read func(*serialize.ReadStream, *T) error) codec {
	var got T
	return codec{
		write: func(s *serialize.WriteStream) error { return write(s, &value) },
		read:  func(s *serialize.ReadStream) error { return read(s, &got) },
		again: func(s *serialize.WriteStream) error { return write(s, &got) },
	}
}

// mixed selects arm t the Go API's way: a payload arm is a fresh
// NewDefaultArm, its declared defaults.
func mixed(t v.MixedType) v.Mixed {
	m := v.Mixed{Type: t}
	if t == v.MixedTypePayload {
		m.Payload = v.NewDefaultArm()
	}
	return m
}

// build is the value of each case cases.txt names, by its type and, for an
// accept or refuse-write case, its name.
func build(name, typ string) (codec, error) {
	switch typ {
	case "Mixed":
		m := mixed(v.MixedTypeNone)
		switch name {
		case "mixed-ping":
			m = mixed(v.MixedTypePing)
		case "mixed-payload":
			m = mixed(v.MixedTypePayload)
		case "mixed-write-tag-3":
			m.Type = 3
		case "mixed-write-marker-8":
			m = mixed(v.MixedTypePayload)
			m.Payload.Marker = 8
		}
		return codecOf(m, v.WriteMixed, v.ReadMixed), nil
	case "Late":
		l := v.Late{}
		switch name {
		case "late-payload":
			l.Type, l.Payload = v.LateTypePayload, v.NewDefaultArm()
		case "late-pong":
			l.Type = v.LateTypePong
		}
		return codecOf(l, v.WriteLate, v.ReadLate), nil
	case "Tags":
		t := v.Tags{}
		switch name {
		case "tags-ping":
			t.Type = v.TagsTypePing
		case "tags-pong":
			t.Type = v.TagsTypePong
		case "tags-write-tag-3":
			t.Type = 3
		}
		return codecOf(t, v.WriteTags, v.ReadTags), nil
	case "Single":
		s := v.Single{}
		if name == "single-ack" {
			s.Type = v.SingleTypeAck
		}
		return codecOf(s, v.WriteSingle, v.ReadSingle), nil
	case "Empty":
		e := v.Empty{}
		if name == "empty-write-tag-1" {
			e.Type = 1
		}
		return codecOf(e, v.WriteEmpty, v.ReadEmpty), nil
	case "Frame":
		return codecOf(v.Frame{Lead: 5, Choice: mixed(v.MixedTypePing), Tail: 6}, v.WriteFrame, v.ReadFrame), nil
	case "Batch":
		b := v.Batch{}
		switch name {
		case "batch-ping":
			b.Values[0], b.ValuesCount = mixed(v.MixedTypePing), 1
		case "batch-ping-payload":
			b.Values[0], b.Values[1], b.ValuesCount = mixed(v.MixedTypePing), mixed(v.MixedTypePayload), 2
		}
		return codecOf(b, v.WriteBatch, v.ReadBatch), nil
	}
	return codec{}, fmt.Errorf("unknown type %s", typ)
}

// accepted are the accept cases build knows; an accept case it does not
// know is FAILED, never written with some other case's value.
var accepted = map[string]bool{
	"batch-empty": true, "batch-ping": true, "batch-ping-payload": true, "empty-none": true,
	"frame-ping": true, "late-payload": true, "late-pong": true, "mixed-none": true,
	"mixed-payload": true, "mixed-ping": true, "single-ack": true, "single-none": true,
	"tags-none": true, "tags-ping": true, "tags-pong": true,
}

// refusedWrites are the refuse-write cases build changes the default value
// for. A Go tag is a uint8 enum and the payload a typed field, so the
// negative tag, the text tag and the payload of another type cannot be
// built: the compiler refuses them.
var refusedWrites = map[string]bool{
	"empty-write-tag-1": true, "mixed-write-marker-8": true, "mixed-write-tag-3": true, "tags-write-tag-3": true,
}

var notRepresentable = map[string]bool{
	"mixed-write-payload-type": true, "mixed-write-tag-negative": true, "mixed-write-tag-text": true,
}

func write(f func(*serialize.WriteStream) error) ([]byte, int64, error) {
	s := serialize.NewWriteStream(make([]byte, 64))
	if err := f(s); err != nil {
		return nil, 0, err
	}
	bits := s.BitsProcessed()
	s.Flush()
	if err := s.Err(); err != nil {
		return nil, 0, err
	}
	return s.Data(), bits, nil
}

// pinned compares what was written with the case's pin, after writing the
// pin from it under -write-pins.
func pinned(pin string, got []byte, gotBits int64, bitsText string, writePins bool) ([]byte, error) {
	bits, err := strconv.ParseInt(bitsText, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("bits column %q", bitsText)
	}
	if writePins {
		if err := os.WriteFile(pin, got, 0o644); err != nil {
			return nil, err
		}
	}
	want, err := os.ReadFile(pin)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(got, want) {
		return nil, fmt.Errorf("write: % x, pin % x", got, want)
	}
	if gotBits != bits {
		return nil, fmt.Errorf("write: %d bits, want %d", gotBits, bits)
	}
	return want, nil
}

// selectPayload reads mixed-ping's pin, then selects the payload arm the Go
// API's way and writes the value.
func selectPayload(dir string) ([]byte, int64, error) {
	from, err := os.ReadFile(filepath.Join(dir, "mixed-ping.bin"))
	if err != nil {
		return nil, 0, err
	}
	var m v.Mixed
	if err := v.ReadMixed(serialize.NewReadStream(from), &m); err != nil {
		return nil, 0, fmt.Errorf("read mixed-ping: %v", err)
	}
	if m.Type != v.MixedTypePing {
		return nil, 0, fmt.Errorf("read mixed-ping: tag %d", m.Type)
	}
	m.Type, m.Payload = v.MixedTypePayload, v.NewDefaultArm()
	return write(func(s *serialize.WriteStream) error { return v.WriteMixed(s, &m) })
}

func answer(dir, name, typ, verdict, bitsText string, writePins bool) error {
	if notRepresentable[name] {
		if verdict != "refuse-write" {
			return fmt.Errorf("named not representable in Go, and its verdict is %s", verdict)
		}
		return nil
	}
	c, err := build(name, typ)
	if err != nil {
		return err
	}
	pin := filepath.Join(dir, name+".bin")
	switch verdict {
	case "accept":
		if !accepted[name] {
			return errors.New("unknown accept case")
		}
		got, gotBits, err := write(c.write)
		if err != nil {
			return fmt.Errorf("write: %v", err)
		}
		want, err := pinned(pin, got, gotBits, bitsText, writePins)
		if err != nil {
			return err
		}
		s := serialize.NewReadStream(want)
		if err := c.read(s); err != nil {
			return fmt.Errorf("read: %v", err)
		}
		if s.BitsProcessed() != int64(len(want))*8 {
			return fmt.Errorf("read consumed %d bits of %d bytes", s.BitsProcessed(), len(want))
		}
		again, _, err := write(c.again)
		if err != nil {
			return fmt.Errorf("write what was read: %v", err)
		}
		if !bytes.Equal(again, want) {
			return fmt.Errorf("write what was read: % x, pin % x", again, want)
		}
		return nil
	case "select":
		if name != "mixed-ping-select-payload" {
			return errors.New("unknown select case")
		}
		got, gotBits, err := selectPayload(dir)
		if err != nil {
			return err
		}
		_, err = pinned(pin, got, gotBits, bitsText, writePins)
		return err
	case "refuse":
		want, err := os.ReadFile(pin)
		if err != nil {
			return err
		}
		if err := c.read(serialize.NewReadStream(want)); err == nil {
			return errors.New("read accepted")
		}
		return nil
	case "refuse-write":
		if !refusedWrites[name] {
			return errors.New("unknown refuse-write case")
		}
		got, _, err := write(c.write)
		if err == nil {
			return fmt.Errorf("write accepted: % x", got)
		}
		if !errors.Is(err, serialize.ErrValueOutOfRange) {
			return fmt.Errorf("write refused with %v, want serialize.ErrValueOutOfRange", err)
		}
		return nil
	}
	return fmt.Errorf("verdict %q", verdict)
}

func main() {
	writePins := len(os.Args) == 3 && os.Args[2] == "-write-pins"
	if len(os.Args) != 2 && !writePins {
		fmt.Fprintln(os.Stderr, "usage: packet-void-go <testdata/wire/packet-void> [-write-pins]")
		os.Exit(2)
	}
	dir := os.Args[1]
	text, err := os.ReadFile(filepath.Join(dir, "cases.txt"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	for line := range strings.SplitSeq(string(text), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		if len(f) < 4 {
			fmt.Printf("case %s FAILED: a case line needs <name> <type> <verdict> <bits>\n", f[0])
			continue
		}
		if err := answer(dir, f[0], f[1], f[2], f[3], writePins); err != nil {
			fmt.Printf("case %s FAILED: %v\n", f[0], err)
			continue
		}
		fmt.Printf("case %s ok\n", f[0])
	}
}
