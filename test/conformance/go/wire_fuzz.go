package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// The harness owns the mutations and oracle. This process only binds a
// roster entry to its generated codec and returns the report and saved value.
func wireFuzz(builder bool) error {
	in, out := bufio.NewReader(os.Stdin), bufio.NewWriter(os.Stdout)
	read := func(v any) error { return binary.Read(in, binary.LittleEndian, v) }
	write := func(v any) error { return binary.Write(out, binary.LittleEndian, v) }
	text := func() (string, error) {
		var n uint16
		if err := read(&n); err != nil {
			return "", err
		}
		b := make([]byte, n)
		_, err := io.ReadFull(in, b)
		return string(b), err
	}
	var count uint32
	if err := read(&count); err != nil {
		return err
	}
	roster := make([]*codec, count)
	messages := make([]*messageCodec, count)
	builders := make([]*builderCodec, count)
	for i := range roster {
		unit, err := text()
		if err != nil {
			return err
		}
		root, err := text()
		if err != nil {
			return err
		}
		form, err := in.ReadByte()
		if err != nil {
			return err
		}
		retain, err := in.ReadByte()
		if err != nil {
			return err
		}
		if form == 1 && retain == 0 {
			if builder {
				builders[i] = findBuilderCodec(unit, root)
			} else {
				roster[i] = findCodec(unit, root)
			}
		}
		if form == 2 && retain == 0 && !builder {
			messages[i] = findMessageCodec(unit, root)
		}
		available := byte(0)
		if roster[i] != nil || messages[i] != nil || builders[i] != nil {
			available = 1
		}
		if err := out.WriteByte(available); err != nil {
			return err
		}
	}
	if err := out.Flush(); err != nil {
		return err
	}
	for {
		var index, size uint32
		if err := read(&index); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if err := read(&size); err != nil {
			return err
		}
		if uint64(index) >= uint64(len(roster)) || roster[index] == nil && messages[index] == nil && builders[index] == nil {
			return fmt.Errorf("unsupported roster index %d", index)
		}
		wire := make([]byte, size)
		if _, err := io.ReadFull(in, wire); err != nil {
			return err
		}
		var rep report
		loaded := true
		regionBytes := int64(-1)
		saved := int64(-1)
		var buffer []byte
		if b := builders[index]; b != nil {
			buffer, rep, loaded = b.run(wire)
			if buffer != nil {
				saved = int64(len(buffer))
			}
		} else if m := messages[index]; m != nil {
			var available bool
			buffer, rep, available = m.run(nil, wire, true)
			if m.loadMeasure != nil {
				regionBytes = m.loadMeasure(wire)
				loaded = available
			}
			if buffer != nil {
				saved = int64(len(buffer))
			}
		} else {
			c := roster[index]
			value := c.fresh()
			loaded = c.load(value, wire, &rep)
			if c.loadMeasure != nil {
				regionBytes = c.loadMeasure(wire)
			} else {
				loaded = true
			}
			saved = c.measure(value)
			if saved >= 0 {
				buffer = make([]byte, saved)
				saved = c.save(value, buffer)
			}
		}
		for _, v := range []any{loaded, rep.unknown, rep.kindMismatch, rep.widened, rep.clamped, rep.duplicate, rep.malformed, rep.refused, regionBytes, int32(0), int32(0), saved} {
			if err := write(v); err != nil {
				return err
			}
		}
		if saved > 0 {
			if _, err := out.Write(buffer[:saved]); err != nil {
				return err
			}
		}
		if err := out.Flush(); err != nil {
			return err
		}
	}
}
