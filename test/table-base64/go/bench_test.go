package main

import (
	b64 "encoding/base64"
	"fmt"
	"testing"

	b "base64test"
)

// The public JSON API over 64 equal-length byte patterns. Parsing, clearing
// the destination and walking descriptors remain inside the measurement.
func BenchmarkBase64(bm *testing.B) {
	for _, size := range []int{16, 4096, 16384} {
		bm.Run(fmt.Sprint(size), func(bm *testing.B) {
			var texts [64][]byte
			for variant := range texts {
				payload := make([]byte, size)
				for i := range payload {
					payload[i] = byte(i*73 + variant)
				}
				texts[variant] = []byte(`{"payload":"` + b64.StdEncoding.EncodeToString(payload) + `"}`)
			}
			var value b.Blob
			var report b.TableReport
			bm.SetBytes(int64(size))
			bm.ReportAllocs()
			bm.ResetTimer()
			for i := 0; i < bm.N; i++ {
				if !b.BlobFromJson(&value, texts[i%64], &report) || value.PayloadLength != int32(size) {
					bm.Fatal("decode failed")
				}
			}
		})
	}
}
