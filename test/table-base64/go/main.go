package main

import (
	b "base64test"
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
)

func packed(data []byte) string {
	if len(data) == 0 {
		return "-"
	}
	return hex.EncodeToString(data)
}

func main() {
	s := bufio.NewScanner(os.Stdin)
	s.Buffer(make([]byte, 65536), 1024*1024)
	for s.Scan() {
		text, err := hex.DecodeString(s.Text())
		if err != nil {
			panic(err)
		}
		var v b.Blob
		var r b.TableReport
		b.BlobFromJson(&v, text, &r)
		if r.Malformed {
			fmt.Println("1 0 0 - -")
			continue
		}
		out := make([]byte, b.BlobToJsonMeasure(&v))
		if b.BlobToJson(&v, out) != int64(len(out)) {
			panic("writer size")
		}
		fmt.Printf("0 %d %d %s %s\n", r.Clamped, r.KindMismatch, packed(v.Payload[:v.PayloadLength]), packed(out))
	}
	if err := s.Err(); err != nil {
		panic(err)
	}
}
