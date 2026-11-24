// The Go leg of test/packet-void, and the family's oracle: with -write-pins
// it writes every accept case's pin from this writer (the refuse pins are
// hand-made; testdata/wire/packet-void/README.md). Otherwise it answers every
// case of cases.txt, one "case <name> ok|FAILED: <why>" line each, for
// tools/casegate.
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

func mixed(t v.MixedType) v.Mixed {
	m := v.Mixed{Type: t}
	if t == v.MixedTypePayload {
		m.Payload = v.NewDefaultArm()
	}
	return m
}

// build is the value of each case cases.txt names, by its type and, for an
// accept case, its value column.
func build(name, typ string) (codec, error) {
	switch typ {
	case "Mixed":
		m := mixed(v.MixedTypeNone)
		switch name {
		case "mixed-ping":
			m = mixed(v.MixedTypePing)
		case "mixed-payload":
			m = mixed(v.MixedTypePayload)
		}
		return codecOf(m, v.WriteMixed, v.ReadMixed), nil
	case "Tags":
		t := v.Tags{}
		switch name {
		case "tags-ping":
			t.Type = v.TagsTypePing
		case "tags-pong":
			t.Type = v.TagsTypePong
		}
		return codecOf(t, v.WriteTags, v.ReadTags), nil
	case "Single":
		s := v.Single{}
		if name == "single-ack" {
			s.Type = v.SingleTypeAck
		}
		return codecOf(s, v.WriteSingle, v.ReadSingle), nil
	case "Empty":
		return codecOf(v.Empty{}, v.WriteEmpty, v.ReadEmpty), nil
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
	"frame-ping": true, "mixed-none": true, "mixed-payload": true, "mixed-ping": true,
	"single-ack": true, "single-none": true, "tags-none": true, "tags-ping": true, "tags-pong": true,
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

func answer(dir, name, typ, verdict, bitsText string, writePins bool) error {
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
		bits, err := strconv.ParseInt(bitsText, 10, 64)
		if err != nil {
			return fmt.Errorf("bits column %q", bitsText)
		}
		got, gotBits, err := write(c.write)
		if err != nil {
			return fmt.Errorf("write: %v", err)
		}
		if writePins {
			if err := os.WriteFile(pin, got, 0o644); err != nil {
				return err
			}
		}
		want, err := os.ReadFile(pin)
		if err != nil {
			return err
		}
		if !bytes.Equal(got, want) {
			return fmt.Errorf("write: % x, pin % x", got, want)
		}
		if gotBits != bits {
			return fmt.Errorf("write: %d bits, want %d", gotBits, bits)
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
	case "refuse":
		want, err := os.ReadFile(pin)
		if err != nil {
			return err
		}
		if err := c.read(serialize.NewReadStream(want)); err == nil {
			return errors.New("read accepted")
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
