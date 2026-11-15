package main

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/mas-bandwidth/serialize.go/v2"
	p "packettext"
)

func hexBytes(b []byte) string {
	if len(b) == 0 {
		return "-"
	}
	return hex.EncodeToString(b)
}

func main() {
	s := bufio.NewScanner(os.Stdin)
	for s.Scan() {
		line := s.Text()
		if line == "-" {
			line = ""
		}
		wire, err := hex.DecodeString(line)
		if err != nil {
			panic(err)
		}
		r := serialize.NewReadStream(wire)
		var v p.Narrow
		if p.ReadNarrow(r, &v) != nil {
			fmt.Println("REFUSE")
			continue
		}
		// An accepted value the writer refuses is its own verdict, which the
		// harness reports as a disagreement with C++ on that line.
		var encoded [256]byte
		w := serialize.NewWriteStream(encoded[:])
		if err := p.WriteNarrow(w, &v); err != nil {
			fmt.Printf("ACCEPTED-UNWRITABLE %d %s\n", r.BitsProcessed(), hexBytes(v.Text[:v.TextLength]))
			continue
		}
		w.Flush()
		if err := w.Err(); err != nil {
			panic(err)
		}
		fmt.Printf("OK %d %s %d %s\n", r.BitsProcessed(), hexBytes(v.Text[:v.TextLength]), w.BitsProcessed(), hexBytes(w.Data()))
	}
	if err := s.Err(); err != nil {
		panic(err)
	}
}
