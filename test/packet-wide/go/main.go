package main

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/mas-bandwidth/serialize.go"
	p "packetwide"
)

func replay[T any](wire []byte, value *T, decode func(*serialize.ReadStream, *T) error, encode func(*serialize.WriteStream, *T) error, text func(*T) []uint16) {
	r := serialize.NewReadStream(wire)
	if decode(r, value) != nil {
		fmt.Println("REFUSE")
		return
	}
	var bytes [256]byte
	w := serialize.NewWriteStream(bytes[:])
	if err := encode(w, value); err != nil {
		panic(err)
	}
	w.Flush()
	if err := w.Err(); err != nil {
		panic(err)
	}
	var payload strings.Builder
	for _, unit := range text(value) {
		fmt.Fprintf(&payload, "%04x", unit)
	}
	if payload.Len() == 0 {
		payload.WriteString("-")
	}
	fmt.Printf("OK %d %s %d %s\n", r.BitsProcessed(), payload.String(), w.BitsProcessed(), hex.EncodeToString(w.Data()))
}

func main() {
	s := bufio.NewScanner(os.Stdin)
	for s.Scan() {
		bound, raw, ok := strings.Cut(s.Text(), " ")
		if !ok {
			panic("bound and wire required")
		}
		if raw == "-" {
			raw = ""
		}
		wire, err := hex.DecodeString(raw)
		if err != nil {
			panic(err)
		}
		switch bound {
		case "7":
			v := p.WideSeven{Text: [7]uint16{0x7f7f, 0x7f7f, 0x7f7f, 0x7f7f, 0x7f7f, 0x7f7f, 0x7f7f}}
			replay(wire, &v, p.ReadWideSeven, p.WriteWideSeven, func(v *p.WideSeven) []uint16 { return v.Text[:v.TextLength] })
		case "4":
			v := p.WideFour{Text: [4]uint16{0x7f7f, 0x7f7f, 0x7f7f, 0x7f7f}}
			replay(wire, &v, p.ReadWideFour, p.WriteWideFour, func(v *p.WideFour) []uint16 { return v.Text[:v.TextLength] })
		default:
			panic("unexpected bound")
		}
	}
	if err := s.Err(); err != nil {
		panic(err)
	}
}
