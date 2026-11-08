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
		if !b.BlobFromJSON(&v, text, &r) {
			fmt.Println("1 - -")
			continue
		}
		out := make([]byte, b.BlobToJSONMeasure(&v))
		if b.BlobToJSON(&v, out) != int64(len(out)) {
			panic("writer size")
		}
		fmt.Printf("0 %s %s\n", packed(v.Payload[:v.PayloadLength]), packed(out))
	}
	if err := s.Err(); err != nil {
		panic(err)
	}
}
